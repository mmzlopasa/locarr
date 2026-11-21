package music

import (
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/mmzlopasa/locarr/server/internal/library/tags"
)

// QualityOf maps a file's stream facts to a music quality id.
func QualityOf(t *tags.Tags) string {
	switch t.Format {
	case tags.FormatFLAC:
		if t.BitDepth >= 24 {
			return "flac-24"
		}
		return "flac"
	case tags.FormatALAC:
		return "alac"
	case tags.FormatWAV:
		return "wav"
	case tags.FormatAAC:
		return "aac"
	case tags.FormatOpus:
		return "opus"
	case tags.FormatVorbis:
		return "vorbis"
	case tags.FormatMP3:
		if t.VBR {
			switch {
			case t.Bitrate >= 220:
				return "mp3-v0"
			case t.Bitrate >= 170:
				return "mp3-v2"
			}
			return "mp3"
		}
		switch {
		case t.Bitrate >= 315:
			return "mp3-320"
		case t.Bitrate >= 250:
			return "mp3-256"
		case t.Bitrate >= 160:
			return "mp3-192"
		case t.Bitrate >= 96:
			return "mp3-128"
		}
		return "mp3"
	}
	return "music-unknown"
}

// LocalFile is an audio file found for an import.
type LocalFile struct {
	Path string
	Rel  string
	Size int64
	Tags tags.Tags
	// Disc and Track are from the tags, else from the folder ("CD2", "Disc 2") and file name ("03 Title").
	Disc  int
	Track int
}

var (
	discFolder = regexp.MustCompile(`(?i)^(?:cd|disc|disk|dvd|vinyl|side)[ ._-]*(\d{1,2})\b`)
	leadNumber = regexp.MustCompile(`^(\d{1,3})(?:[ ._-]|$)`)
)

// Prepare fills Disc and Track from tags, folder and file name.
func (f *LocalFile) Prepare() {
	f.Disc, f.Track = f.Tags.Disc, f.Tags.Track
	if f.Disc == 0 {
		for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(f.Rel)), "/") {
			if m := discFolder.FindStringSubmatch(strings.TrimSpace(part)); m != nil {
				f.Disc, _ = strconv.Atoi(m[1])
			}
		}
	}
	if f.Track == 0 {
		base := strings.TrimSuffix(filepath.Base(f.Rel), filepath.Ext(f.Rel))
		if m := leadNumber.FindStringSubmatch(base); m != nil {
			n, _ := strconv.Atoi(m[1])
			// "103 Title" on a multi-disc set: disc 1, track 3.
			if n >= 100 && f.Disc == 0 {
				f.Disc, n = n/100, n%100
			}
			f.Track = n
		}
	}
	if f.Disc == 0 {
		f.Disc = 1
	}
}

// CandidateTrack is one track of a release to match against.
type CandidateTrack struct {
	Disc, Position int
	Title          string
	LengthMs       int
	RecordingMBID  string
	MBID           string
}

// Candidate is a release to match a folder against.
type Candidate struct {
	ReleaseMBID string
	GroupMBID   string
	Title       string
	Artist      string
	Aliases     []string
	Tracks      []CandidateTrack
}

// Pair is one file put on one track.
type Pair struct {
	File  int     `json:"file"`
	Track int     `json:"track"`
	Score float64 `json:"score"` // 0..1
	// DurationDiffMs is file minus track length (0 when either is unknown).
	DurationDiffMs int  `json:"durationDiffMs"`
	ByID           bool `json:"byId"`
}

// Match is how well a folder fits one release.
type Match struct {
	ReleaseMBID string `json:"releaseMbid"`
	// Score is 0..100; Locarr imports automatically from MinimumMatchScore (D-043).
	Score   int    `json:"score"`
	Pairs   []Pair `json:"pairs"`
	Missing []int  `json:"missing"` // track indexes without a file
	Extra   []int  `json:"extra"`   // file indexes without a track
	// Parts of the score (each 0..1) for the report.
	Coverage   float64  `json:"coverage"`
	TrackFit   float64  `json:"trackFit"`
	AlbumFit   float64  `json:"albumFit"`
	ArtistFit  float64  `json:"artistFit"`
	DurationOK float64  `json:"durationOk"`
	IDMatch    bool     `json:"idMatch"`
	Notes      []string `json:"notes"`
}

// DurationTolerance is how far a file may be from the track length and
// still count as the same length: 5 s or 5 %, whichever is more.
func DurationTolerance(lengthMs int) int { return max(5000, lengthMs/20) }

// normText lowers, strips accents and punctuation, and drops bracketed
// suffixes like "(Remastered 2009)" when asked.
func normText(s string, dropBrackets bool) string {
	s = strings.ToLower(s)
	if dropBrackets {
		for _, br := range [][2]string{{"(", ")"}, {"[", "]"}} {
			for {
				i := strings.Index(s, br[0])
				j := strings.Index(s, br[1])
				if i < 0 || j < i {
					break
				}
				inner := s[i+1 : j]
				if !strings.Contains(inner, "remaster") && !strings.Contains(inner, "bonus") && !strings.Contains(inner, "feat") &&
					!strings.Contains(inner, "version") && !strings.Contains(inner, "edit") && !strings.Contains(inner, "deluxe") {
					break
				}
				s = s[:i] + s[j+1:]
			}
		}
	}
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == 'đ':
			b.WriteString("d")
		case r == '&':
			b.WriteString(" and ")
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Similarity is 1 - normalised edit distance between two texts (after normText).
func Similarity(a, b string) float64 {
	a, b = normText(a, true), normText(b, true)
	if a == b {
		return 1
	}
	if a == "" || b == "" {
		return 0
	}
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return 1 - float64(prev[len(rb)])/float64(max(len(ra), len(rb)))
}

func durationFit(fileMs, trackMs int) float64 {
	if fileMs <= 0 || trackMs <= 0 {
		return 0.5
	}
	d := int(math.Abs(float64(fileMs - trackMs)))
	tol := DurationTolerance(trackMs)
	switch {
	case d <= tol:
		return 1
	case d >= 6*tol:
		return 0
	}
	return 1 - float64(d-tol)/float64(5*tol)
}

// pairScore is how well one file fits one track.
func pairScore(f *LocalFile, t *CandidateTrack) (float64, bool) {
	if f.Tags.RecordingID != "" && f.Tags.RecordingID == t.RecordingMBID {
		return 1, true
	}
	if f.Tags.TrackID != "" && f.Tags.TrackID == t.MBID {
		return 1, true
	}
	title := f.Tags.Title
	if title == "" {
		base := strings.TrimSuffix(filepath.Base(f.Rel), filepath.Ext(f.Rel))
		title = strings.TrimSpace(leadNumber.ReplaceAllString(base, ""))
		// "Artist - Title" file names.
		if i := strings.LastIndex(title, " - "); i >= 0 {
			title = title[i+3:]
		}
	}
	ts := Similarity(title, t.Title)
	pos := 0.0
	switch {
	case f.Track == t.Position && f.Disc == t.Disc:
		pos = 1
	case f.Track == t.Position:
		pos = 0.5
	}
	score := 0.5*ts + 0.3*pos + 0.2*durationFit(int(f.Tags.Duration.Milliseconds()), t.LengthMs)
	// A file whose title is clearly another song is not this track, whatever
	// its number and length say.
	if title != "" && ts < 0.5 {
		score *= 0.5
	}
	return score, false
}

// minPair: weaker pairs are not the same track.
const minPair = 0.45

// MatchRelease scores how well files fit a release and puts each file on a track.
func MatchRelease(files []LocalFile, c Candidate) Match {
	m := Match{ReleaseMBID: c.ReleaseMBID, Pairs: []Pair{}, Missing: []int{}, Extra: []int{}, Notes: []string{}}
	type cand struct {
		f, t int
		s    float64
		id   bool
	}
	var all []cand
	for i := range files {
		for j := range c.Tracks {
			s, id := pairScore(&files[i], &c.Tracks[j])
			if s >= minPair {
				all = append(all, cand{i, j, s, id})
			}
		}
	}
	sort.SliceStable(all, func(a, b int) bool {
		if all[a].s != all[b].s {
			return all[a].s > all[b].s
		}
		if all[a].f != all[b].f {
			return all[a].f < all[b].f
		}
		return all[a].t < all[b].t
	})
	usedF, usedT := map[int]bool{}, map[int]bool{}
	sum, durOK := 0.0, 0
	for _, p := range all {
		if usedF[p.f] || usedT[p.t] {
			continue
		}
		usedF[p.f], usedT[p.t] = true, true
		diff := 0
		fm, tm := int(files[p.f].Tags.Duration.Milliseconds()), c.Tracks[p.t].LengthMs
		if fm > 0 && tm > 0 {
			diff = fm - tm
			if int(math.Abs(float64(diff))) <= DurationTolerance(tm) {
				durOK++
			}
		} else {
			durOK++ // unknown lengths do not count against a match
		}
		m.Pairs = append(m.Pairs, Pair{File: p.f, Track: p.t, Score: p.s, DurationDiffMs: diff, ByID: p.id})
		sum += p.s
	}
	sort.Slice(m.Pairs, func(a, b int) bool { return m.Pairs[a].Track < m.Pairs[b].Track })
	for j := range c.Tracks {
		if !usedT[j] {
			m.Missing = append(m.Missing, j)
		}
	}
	for i := range files {
		if !usedF[i] {
			m.Extra = append(m.Extra, i)
		}
	}
	if len(c.Tracks) == 0 || len(files) == 0 {
		return m
	}
	matched := float64(len(m.Pairs))
	m.Coverage = matched / float64(len(c.Tracks))
	precision := matched / float64(len(files))
	if matched > 0 {
		m.TrackFit = sum / matched
		m.DurationOK = float64(durOK) / matched
	}
	// Album and artist from the tags most files agree on.
	albumTag, artistTag, releaseTag, groupTag := majority(files, func(f *LocalFile) string { return f.Tags.Album }),
		majority(files, func(f *LocalFile) string { return first(f.Tags.AlbumArtist, f.Tags.Artist) }),
		majority(files, func(f *LocalFile) string { return f.Tags.ReleaseID }),
		majority(files, func(f *LocalFile) string { return f.Tags.ReleaseGroupID })
	m.AlbumFit = 0.5
	if albumTag != "" {
		m.AlbumFit = Similarity(albumTag, c.Title)
	}
	m.ArtistFit = 0.5
	if artistTag != "" {
		m.ArtistFit = Similarity(artistTag, c.Artist)
		for _, al := range c.Aliases {
			m.ArtistFit = max(m.ArtistFit, Similarity(artistTag, al))
		}
		if strings.Contains(strings.ToLower(artistTag), "various") {
			m.ArtistFit = max(m.ArtistFit, 0.7)
		}
	}
	score := 30*m.Coverage + 10*precision + 30*m.TrackFit + 15*m.AlbumFit + 10*m.ArtistFit + 5*m.DurationOK
	switch {
	case releaseTag != "" && releaseTag == c.ReleaseMBID:
		m.IDMatch = true
		score += 10
		m.Notes = append(m.Notes, "release_id_in_tags")
	case releaseTag != "" && groupTag != "" && groupTag == c.GroupMBID:
		score += 3
		m.Notes = append(m.Notes, "other_edition_in_tags")
	case releaseTag != "":
		score -= 10
		m.Notes = append(m.Notes, "tags_name_another_release")
	}
	if len(m.Missing) > 0 {
		m.Notes = append(m.Notes, "missing_tracks")
	}
	if len(m.Extra) > 0 {
		m.Notes = append(m.Notes, "extra_files")
	}
	if m.DurationOK < 1 {
		m.Notes = append(m.Notes, "duration_mismatch")
	}
	m.Score = int(math.Round(math.Max(0, math.Min(100, score))))
	return m
}

func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func majority(files []LocalFile, get func(*LocalFile) string) string {
	counts := map[string]int{}
	best, n := "", 0
	for i := range files {
		v := strings.TrimSpace(get(&files[i]))
		if v == "" {
			continue
		}
		counts[v]++
		if counts[v] > n || (counts[v] == n && v < best) {
			best, n = v, counts[v]
		}
	}
	return best
}

// BestMatch returns the index of the best-scoring candidate (-1 = none) and all matches.
func BestMatch(files []LocalFile, cands []Candidate) (int, []Match) {
	out := make([]Match, len(cands))
	best := -1
	for i := range cands {
		out[i] = MatchRelease(files, cands[i])
		if best < 0 || out[i].Score > out[best].Score ||
			(out[i].Score == out[best].Score && len(out[i].Missing) < len(out[best].Missing)) {
			best = i
		}
	}
	return best, out
}
