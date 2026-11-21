package music

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/fsops"
	"github.com/mmzlopasa/locarr/server/internal/library/importer"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/library/tags"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// Import rejection codes.
const (
	RejectNoAudio      = "no_audio_files"
	RejectCue          = "cue_single_file"
	RejectWeakMatch    = "match_too_weak"
	RejectMissing      = "missing_tracks"
	RejectNotUpgrade   = "not_an_upgrade"
	RejectNoAlbum      = "no_matching_album"
	RejectNoRelease    = "no_release"
	RejectTrackListErr = "track_list_unavailable"
)

// ImportError is an import that cannot go ahead, with a stable code.
type ImportError struct {
	Code    string
	Message string
}

func (e *ImportError) Error() string { return e.Message }

func rejectf(code, format string, a ...any) error {
	return &ImportError{Code: code, Message: fmt.Sprintf(format, a...)}
}

// IsImportError unwraps an ImportError.
func IsImportError(err error) (*ImportError, bool) {
	var ie *ImportError
	ok := errors.As(err, &ie)
	return ie, ok
}

// coverNames are picked up as the album cover, best first.
var coverNames = []string{"cover", "folder", "front", "albumart", "album"}

// Folder is what a download or album folder holds.
type Folder struct {
	Root       string
	Files      []LocalFile
	Unreadable []string
	Cue        []string
	Cover      string
}

// maxAudioFiles bounds one import (a box set of 20 discs fits).
const maxAudioFiles = 600

// ScanFolder reads the audio files (and their tags) under root.
func ScanFolder(root string) (Folder, error) {
	f := Folder{Root: root}
	st, err := os.Stat(root)
	if err != nil {
		return f, err
	}
	var paths []string
	coverRank := len(coverNames) + 1
	add := func(p, rel string, info fs.FileInfo) {
		ext := strings.ToLower(filepath.Ext(p))
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)))
		switch {
		case tags.IsAudio(p):
			paths = append(paths, p)
		case ext == ".cue":
			f.Cue = append(f.Cue, p)
		case ext == ".jpg" || ext == ".jpeg" || ext == ".png":
			for i, n := range coverNames {
				if base == n && i < coverRank && info.Size() < 20<<20 {
					f.Cover, coverRank = p, i
				}
			}
		}
		_ = rel
	}
	if !st.IsDir() {
		add(root, filepath.Base(root), st)
	} else {
		err = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				if p == root {
					return err
				}
				return nil
			}
			if e.IsDir() {
				if p != root && (strings.HasPrefix(e.Name(), ".") || strings.Count(rel(root, p), string(filepath.Separator)) >= 3) {
					return filepath.SkipDir
				}
				return nil
			}
			if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") {
				return nil
			}
			info, err := e.Info()
			if err != nil {
				return nil
			}
			add(p, rel(root, p), info)
			return nil
		})
		if err != nil {
			return f, err
		}
	}
	sort.Strings(paths)
	if len(paths) > maxAudioFiles {
		paths = paths[:maxAudioFiles]
	}
	for _, p := range paths {
		t, err := tags.Read(p)
		if err != nil {
			f.Unreadable = append(f.Unreadable, p)
			continue
		}
		info, _ := os.Stat(p)
		lf := LocalFile{Path: p, Rel: rel(root, p), Tags: t}
		if info != nil {
			lf.Size = info.Size()
		}
		if !st.IsDir() {
			lf.Rel = filepath.Base(p)
		}
		lf.Prepare()
		f.Files = append(f.Files, lf)
	}
	return f, nil
}

func rel(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return filepath.Base(p)
	}
	return r
}

// IsCueImage reports a cue sheet with one audio file for the whole album
// (or far fewer files than tracks): not supported (D-044).
func (f *Folder) IsCueImage(tracks int) bool {
	return len(f.Cue) > 0 && (len(f.Files) == 1 || (tracks > 1 && len(f.Files) <= tracks/3))
}

// ReleaseMatch is one candidate edition and how the folder fits it.
type ReleaseMatch struct {
	Release Release
	Tracks  []Track
	Match   Match
}

// Analysis is what Locarr thinks a folder is.
type Analysis struct {
	Album   Album
	Artist  Artist
	Folder  Folder
	Matches []ReleaseMatch // best first
	// Rejection is why it would not import automatically (nil = it would).
	Rejection *ImportError
}

// Best is the best-fitting edition (nil = none).
func (a *Analysis) Best() *ReleaseMatch {
	if len(a.Matches) == 0 {
		return nil
	}
	return &a.Matches[0]
}

// maxCandidates bounds the editions a folder is compared with (each needs
// its track list, one MusicBrainz request when not loaded yet).
const maxCandidates = 6

func candidate(a *Artist, al *Album, r *Release, tracks []Track) Candidate {
	c := Candidate{ReleaseMBID: r.MBID, GroupMBID: al.MBID, Title: r.Title, Artist: a.Name, Aliases: a.Aliases}
	for _, t := range tracks {
		c.Tracks = append(c.Tracks, CandidateTrack{Disc: t.Disc, Position: t.Position, Title: t.Title, LengthMs: t.LengthMs,
			RecordingMBID: t.RecordingMBID, MBID: t.MBID})
	}
	return c
}

// Analyse matches a folder to an album's editions. releaseID forces one
// edition (0 = the chosen one plus others that may fit better, unless the
// album's edition is locked).
func (s *Service) Analyse(ctx context.Context, albumID int64, root string, releaseID int64) (Analysis, error) {
	var an Analysis
	al, err := s.Album(ctx, albumID)
	if err != nil {
		return an, err
	}
	a, err := s.Artist(ctx, al.ArtistID)
	if err != nil {
		return an, err
	}
	an.Album, an.Artist = al, a
	src, err := lfs.MustExist(s.DataRoots, root)
	if err != nil {
		return an, err
	}
	if an.Folder, err = ScanFolder(src); err != nil {
		return an, err
	}
	if len(an.Folder.Files) == 0 {
		msg := fmt.Sprintf("no readable audio files in %s", src)
		if len(an.Folder.Unreadable) > 0 {
			msg += fmt.Sprintf(" (%d files could not be read)", len(an.Folder.Unreadable))
		}
		an.Rejection = &ImportError{Code: RejectNoAudio, Message: msg}
		return an, nil
	}
	rels, err := s.EnsureReleases(ctx, &al)
	if err != nil {
		return an, err
	}
	if len(rels) == 0 {
		an.Rejection = &ImportError{Code: RejectNoRelease, Message: "MusicBrainz lists no edition of this album"}
		return an, nil
	}
	al, _ = s.Album(ctx, albumID)
	an.Album = al
	// Which editions to compare: forced, or the chosen one first and then
	// those whose track count is near the number of files or named in the tags.
	tagRelease := majority(an.Folder.Files, func(f *LocalFile) string { return f.Tags.ReleaseID })
	var picks []Release
	for _, r := range rels {
		switch {
		case releaseID > 0:
			if r.ID == releaseID {
				picks = append(picks, r)
			}
		case al.ReleaseID != nil && r.ID == *al.ReleaseID:
			picks = append([]Release{r}, picks...)
		case al.ReleaseLocked:
		case r.MBID == tagRelease:
			picks = append(picks, r)
		}
	}
	if releaseID == 0 && !al.ReleaseLocked {
		n := len(an.Folder.Files)
		others := []Release{}
		for _, r := range rels {
			if (al.ReleaseID != nil && r.ID == *al.ReleaseID) || r.MBID == tagRelease {
				continue
			}
			if d := r.TrackCount - n; d >= -3 && d <= 3 {
				others = append(others, r)
			}
		}
		sort.SliceStable(others, func(i, j int) bool {
			di, dj := abs(others[i].TrackCount-n), abs(others[j].TrackCount-n)
			if di != dj {
				return di < dj
			}
			return others[i].TracksLoaded && !others[j].TracksLoaded
		})
		picks = append(picks, others...)
	}
	if len(picks) == 0 {
		return an, invalid("release %d is not an edition of this album", releaseID)
	}
	if len(picks) > maxCandidates {
		picks = picks[:maxCandidates]
	}
	for _, r := range picks {
		tracks, err := s.EnsureTracks(ctx, r.ID)
		if err != nil {
			s.log().Warn("track list", "release", r.MBID, "err", err)
			continue
		}
		r2, _ := s.ReleaseByID(ctx, r.ID)
		an.Matches = append(an.Matches, ReleaseMatch{Release: r2, Tracks: tracks, Match: MatchRelease(an.Folder.Files, candidate(&a, &al, &r2, tracks))})
	}
	if len(an.Matches) == 0 {
		an.Rejection = &ImportError{Code: RejectTrackListErr, Message: "could not load a track list from MusicBrainz; try again later"}
		return an, nil
	}
	sort.SliceStable(an.Matches, func(i, j int) bool {
		a, b := an.Matches[i].Match, an.Matches[j].Match
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return len(a.Missing) < len(b.Missing)
	})
	best := an.Best()
	if an.Folder.IsCueImage(len(best.Tracks)) {
		an.Rejection = &ImportError{Code: RejectCue, Message: fmt.Sprintf("%s holds a cue sheet with one audio file for the whole album; "+
			"split it into tracks first (cue images are not supported)", filepath.Base(src))}
		return an, nil
	}
	minScore, _ := settings.Get(ctx, s.Settings, MinimumMatchScore)
	if best.Match.Score < minScore {
		an.Rejection = &ImportError{Code: RejectWeakMatch, Message: fmt.Sprintf("the files do not match %s well enough: best score %d of 100 (%s, %d of %d tracks); "+
			"import manually to choose", al.Title, best.Match.Score, best.Release.FormatSummary(), len(best.Match.Pairs), len(best.Tracks))}
	}
	return an, nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ImportOptions control one import.
type ImportOptions struct {
	// ReleaseID forces an edition (0 = the best match).
	ReleaseID int64
	// AllowPartial overrides the setting (nil = the setting).
	AllowPartial *bool
	// Force imports below the minimum match score (manual import of a chosen edition).
	Force bool
	Mode  string
	// ReleaseTitle is the download's name (release group for names).
	ReleaseTitle string
	DownloadID   *int64
}

// ImportResult says what happened.
type ImportResult struct {
	AlbumID   int64
	Release   Release
	Score     int
	Imported  []string
	Skipped   []SkippedFile
	Recycled  []string
	Missing   int
	Upgraded  int
	CoverPath string
}

// SkippedFile is a file left out, with why.
type SkippedFile struct {
	Path   string
	Reason string
}

func (s *Service) transferMode(ctx context.Context, requested string) (string, error) {
	if requested != "" {
		switch requested {
		case library.TransferHardlink, library.TransferCopy, library.TransferMove:
			return requested, nil
		}
		return "", invalid("unknown transfer mode %q", requested)
	}
	return settings.Get(ctx, s.Settings, library.TransferMode)
}

func (s *Service) perms(ctx context.Context) importer.Permissions {
	on, _ := settings.Get(ctx, s.Settings, library.SetPermissions)
	if !on {
		return importer.Permissions{}
	}
	f, _ := settings.Get(ctx, s.Settings, library.FileMode)
	d, _ := settings.Get(ctx, s.Settings, library.FolderMode)
	return importer.Permissions{File: importer.ParseMode(f), Folder: importer.ParseMode(d)}
}

func qualityName(id string) string {
	if q, ok := quality.ByID(id); ok {
		return q.Name
	}
	return id
}

// better reports whether quality a beats b under p.
func better(p *quality.Profile, a, b string) bool {
	pa, _ := p.Position(a)
	pb, _ := p.Position(b)
	if pa < 0 {
		pa = len(p.Items) + 1
	}
	if pb < 0 {
		pb = len(p.Items) + 1
	}
	return pa < pb
}

// namer renders album folders and track names.
type namer struct {
	artist      naming.ArtistInfo
	album       naming.AlbumInfo
	albumTmpl   string
	trackTmpl   string
	discTmpl    string
	discFolders bool
	rename      bool
	colon       string
}

func (s *Service) newNamer(ctx context.Context, a *Artist, al *Album, r *Release) (namer, error) {
	n := namer{artist: s.artistInfo(a), album: naming.AlbumInfo{Title: al.Title, Type: al.PrimaryType, Disambiguation: al.Disambiguation,
		ReleaseDate: al.ReleaseDate, MBID: al.MBID, EditionDate: r.Date, Country: r.Country, Format: r.FormatSummary(),
		ReleaseDisambiguation: r.Disambiguation, Label: r.Label, Discs: r.MediaCount, Tracks: r.TrackCount}, colon: s.colon(ctx)}
	var err error
	get := func(k settings.Key[string], dst *string) {
		if err == nil {
			*dst, err = settings.Get(ctx, s.Settings, k)
		}
	}
	get(AlbumFolderFormat, &n.albumTmpl)
	if r.MediaCount > 1 {
		get(MultiDiscTrackFormat, &n.trackTmpl)
	} else {
		get(TrackFormat, &n.trackTmpl)
	}
	get(DiscFolderFormat, &n.discTmpl)
	if err != nil {
		return n, err
	}
	n.discFolders, _ = settings.Get(ctx, s.Settings, DiscFolders)
	n.rename, _ = settings.Get(ctx, s.Settings, RenameTracks)
	if n.discFolders && r.MediaCount > 1 {
		// With disc folders the track name needs no disc number.
		n.trackTmpl, _ = settings.Get(ctx, s.Settings, TrackFormat)
	}
	return n, nil
}

func (n *namer) albumFolder(first *naming.TrackInfo) (string, error) {
	return naming.Render(n.albumTmpl, naming.MusicValues(n.artist, &n.album, first), naming.Options{Colon: n.colon})
}

// trackPath returns the folder (relative to the album folder, "" = itself) and file name without extension.
func (n *namer) trackPath(t *naming.TrackInfo, original string) (string, string, error) {
	sub := ""
	if n.discFolders && n.album.Discs > 1 {
		d, err := naming.Render(n.discTmpl, naming.MusicValues(n.artist, &n.album, t), naming.Options{Colon: n.colon})
		if err != nil {
			return "", "", err
		}
		sub = d
	}
	if !n.rename {
		return sub, strings.TrimSuffix(filepath.Base(original), filepath.Ext(original)), nil
	}
	name, err := naming.Render(n.trackTmpl, naming.MusicValues(n.artist, &n.album, t), naming.Options{Colon: n.colon})
	return sub, name, err
}

func trackInfo(t *Track, f *LocalFile, releaseGroup string) naming.TrackInfo {
	q := QualityOf(&f.Tags)
	return naming.TrackInfo{Disc: t.Disc, Number: t.Position, Title: t.Title, Artist: t.Artist, Quality: qualityName(q), Format: f.Tags.Format,
		BitDepth: f.Tags.BitDepth, SampleRate: f.Tags.SampleRate, Bitrate: f.Tags.Bitrate, ReleaseGroup: releaseGroup, Extension: filepath.Ext(f.Path)}
}

// Import imports a folder into an album: matches it to an edition, checks
// each track is new or an upgrade, transfers and renames the files, moves
// replaced files to the recycle bin, saves a cover, and records every step.
func (s *Service) Import(ctx context.Context, albumID int64, root string, o ImportOptions) (ImportResult, error) {
	an, err := s.Analyse(ctx, albumID, root, o.ReleaseID)
	if err != nil {
		return ImportResult{}, err
	}
	al, a := an.Album, an.Artist
	ref := al.Ref()
	res := ImportResult{AlbumID: al.ID}
	if an.Rejection != nil && (!o.Force || an.Rejection.Code != RejectWeakMatch) {
		return res, an.Rejection
	}
	best := an.Best()
	m := best.Match
	res.Release, res.Score, res.Missing = best.Release, m.Score, len(m.Missing)
	partial := o.AllowPartial
	if partial == nil {
		v, _ := settings.Get(ctx, s.Settings, ImportPartial)
		partial = &v
	}
	// Tracks the library already has do not count as missing.
	existing, err := s.Files(ctx, al.ID)
	if err != nil {
		return res, err
	}
	tracks := append([]Track{}, best.Tracks...)
	LinkFiles(tracks, existing)
	missing := 0
	for _, ti := range m.Missing {
		if tracks[ti].File == nil {
			missing++
		}
	}
	if missing > 0 && !*partial {
		return res, rejectf(RejectMissing, "%d of %d tracks of %s are not in the download (score %d); allow partial albums to import the rest",
			missing, len(tracks), best.Release.FormatSummary(), m.Score)
	}
	// Switch the album's edition when another fits the files better.
	if al.ReleaseID == nil || *al.ReleaseID != best.Release.ID {
		if !al.ReleaseLocked || o.ReleaseID > 0 {
			if _, err := s.DB.W.ExecContext(ctx, `UPDATE albums SET release_id = ?, release_locked = ?, updated_at = ? WHERE id = ?`,
				best.Release.ID, al.ReleaseLocked || o.ReleaseID > 0, db.Millis(s.now()), al.ID); err != nil {
				return res, err
			}
			s.record(ctx, ref, TypeReleaseChanged, fmt.Sprintf("edition set to %s (%s, %s, %d tracks): it fits the files best",
				best.Release.Title, best.Release.Country, best.Release.FormatSummary(), best.Release.TrackCount),
				map[string]any{"releaseId": best.Release.ID, "mbid": best.Release.MBID, "score": m.Score}, o.DownloadID)
			// Files of the old edition stay linked to the album (by recording or position).
			tracks = append([]Track{}, best.Tracks...)
			LinkFiles(tracks, existing)
		}
	}
	scores := []map[string]any{}
	for _, rm := range an.Matches {
		scores = append(scores, map[string]any{"release": rm.Release.MBID, "title": rm.Release.Title, "country": rm.Release.Country,
			"format": rm.Release.FormatSummary(), "tracks": len(rm.Tracks), "score": rm.Match.Score})
	}
	s.record(ctx, ref, TypeMatched, fmt.Sprintf("matched %d files to %s (%s, %s): score %d, %d of %d tracks",
		len(an.Folder.Files), best.Release.Title, best.Release.Country, best.Release.FormatSummary(), m.Score, len(m.Pairs), len(best.Tracks)),
		map[string]any{"score": m.Score, "releases": scores, "missing": len(m.Missing), "extra": len(m.Extra), "notes": m.Notes,
			"coverage": m.Coverage, "trackFit": m.TrackFit, "albumFit": m.AlbumFit, "artistFit": m.ArtistFit, "durationOk": m.DurationOK}, o.DownloadID)

	p, err := s.Quality.Profile(ctx, a.QualityProfileID)
	if err != nil {
		return res, err
	}
	p.Normalize()
	mode, err := s.transferMode(ctx, o.Mode)
	if err != nil {
		return res, err
	}
	writeTags, _ := settings.Get(ctx, s.Settings, WriteTags)
	nm, err := s.newNamer(ctx, &a, &al, &best.Release)
	if err != nil {
		return res, err
	}
	group := ""
	if o.ReleaseTitle != "" {
		group = parser.Parse(o.ReleaseTitle).Group
	}
	// Decide per track first: new, upgrade, or not better.
	type job struct {
		pair  Pair
		track *Track
		file  *LocalFile
		info  naming.TrackInfo
		q     string
	}
	var jobs []job
	for _, pr := range m.Pairs {
		t := &tracks[pr.Track]
		f := &an.Folder.Files[pr.File]
		q := QualityOf(&f.Tags)
		if t.File != nil {
			if same, _ := sameFile(f.Path, t.File.Path); same {
				res.Skipped = append(res.Skipped, SkippedFile{Path: f.Path, Reason: "already_imported"})
				continue
			}
			if !better(&p, q, t.File.Quality) {
				res.Skipped = append(res.Skipped, SkippedFile{Path: f.Path, Reason: RejectNotUpgrade})
				continue
			}
		}
		jobs = append(jobs, job{pair: pr, track: t, file: f, info: trackInfo(t, f, group), q: q})
	}
	for _, i := range m.Extra {
		res.Skipped = append(res.Skipped, SkippedFile{Path: an.Folder.Files[i].Path, Reason: "no_matching_track"})
	}
	if len(jobs) == 0 {
		why := "every track is already in the library at the same or a better quality"
		return res, rejectf(RejectNotUpgrade, "not imported: %s", why)
	}
	folderName, err := nm.albumFolder(&jobs[0].info)
	if err != nil {
		return res, err
	}
	albumDir, err := naming.Join(a.Path, folderName, "x")
	if err != nil {
		return res, err
	}
	albumDir = filepath.Dir(albumDir)
	perms := s.perms(ctx)
	now := s.now()
	renamed := 0
	for _, j := range jobs {
		sub, name, err := nm.trackPath(&j.info, j.file.Path)
		if err != nil {
			return res, err
		}
		dir := albumDir
		if sub != "" {
			dir = filepath.Join(albumDir, sub)
		}
		fileMode := mode
		if writeTags && j.file.Tags.Format == tags.FormatFLAC && mode == library.TransferHardlink {
			fileMode = library.TransferCopy // never write into a file shared with a seeding download
		}
		old := j.track.File
		replace := func(path string) error {
			e, err := s.Recycle.Recycle(ctx, path, ref.String(), "replaced by an upgrade")
			if err != nil {
				return err
			}
			res.Recycled = append(res.Recycled, e.RecycledPath)
			return nil
		}
		placed, _, err := importer.Place(importer.Placement{Roots: s.DataRoots, Source: j.file.Path, Folder: dir, Name: name, Mode: fileMode,
			Perms: perms, Replace: replace})
		if err != nil {
			if errors.Is(err, fsops.ErrExists) {
				res.Skipped = append(res.Skipped, SkippedFile{Path: j.file.Path, Reason: "destination_exists"})
				continue
			}
			return res, err
		}
		if old != nil && old.Path != placed.Path {
			if _, err := os.Stat(old.Path); err == nil {
				if err := replace(old.Path); err != nil {
					s.record(ctx, ref, timeline.TypeImportFailed, "imported the new file but could not move the old one to the recycle bin: "+err.Error(),
						map[string]any{"path": old.Path}, o.DownloadID)
				}
			}
		}
		if writeTags && j.file.Tags.Format == tags.FormatFLAC && placed.Mode != "hardlink" && placed.Mode != "existing" {
			if err := tags.WriteFLAC(placed.Path, correctedTags(&a, &al, &best.Release, j.track, len(tracks))); err != nil {
				s.log().Warn("write tags", "path", placed.Path, "err", err)
			}
		}
		if err := s.saveFile(ctx, &a, &al, j.track, j.file, placed, j.q, o, old, now); err != nil {
			return res, err
		}
		res.Imported = append(res.Imported, placed.Path)
		if filepath.Base(j.file.Path) != filepath.Base(placed.Path) {
			renamed++
		}
		if old != nil {
			res.Upgraded++
			s.record(ctx, ref, timeline.TypeUpgraded, fmt.Sprintf("track %d-%d upgraded from %s to %s", j.track.Disc, j.track.Position,
				qualityName(old.Quality), qualityName(j.q)), map[string]any{"from": qualityName(old.Quality), "to": qualityName(j.q), "oldPath": old.Path}, o.DownloadID)
		}
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE albums SET path = ?, updated_at = ? WHERE id = ?`, albumDir, db.Millis(now), al.ID); err != nil {
		return res, err
	}
	res.CoverPath = s.saveCover(ctx, &al, &best.Release, an.Folder.Cover, albumDir, mode, perms, o.DownloadID)
	qualities := map[string]int{}
	for _, j := range jobs {
		qualities[qualityName(j.q)]++
	}
	s.record(ctx, ref, timeline.TypeFileImported, fmt.Sprintf("imported %d tracks into %s (%s)", len(res.Imported), albumDir, mode),
		map[string]any{"files": res.Imported, "skipped": res.Skipped, "folder": albumDir, "mode": mode, "qualities": qualities,
			"release": best.Release.MBID, "score": m.Score, "missing": missing, "cover": res.CoverPath}, o.DownloadID)
	if renamed > 0 {
		s.record(ctx, ref, timeline.TypeRenamed, fmt.Sprintf("renamed %d files by the track naming template", renamed),
			map[string]any{"count": renamed, "example": filepath.Base(res.Imported[0])}, o.DownloadID)
	}
	for _, r := range res.Recycled {
		s.record(ctx, ref, timeline.TypeRecycled, "moved the replaced file to the recycle bin: "+r, map[string]any{"path": r}, o.DownloadID)
	}
	s.notify(ctx, &a, &al, map[bool]string{true: "upgraded", false: "imported"}[res.Upgraded > 0], albumDir)
	return res, nil
}

func correctedTags(a *Artist, al *Album, r *Release, t *Track, total int) map[string]string {
	artist := t.Artist
	if artist == "" {
		artist = a.Name
	}
	return map[string]string{"TITLE": t.Title, "ARTIST": artist, "ALBUMARTIST": a.Name, "ALBUM": al.Title, "DATE": first(r.Date, al.ReleaseDate),
		"TRACKNUMBER": strconv.Itoa(t.Position), "DISCNUMBER": strconv.Itoa(t.Disc), "DISCTOTAL": strconv.Itoa(max(r.MediaCount, 1)),
		"TRACKTOTAL": strconv.Itoa(total), "MUSICBRAINZ_TRACKID": t.RecordingMBID, "MUSICBRAINZ_RELEASETRACKID": t.MBID,
		"MUSICBRAINZ_ALBUMID": r.MBID, "MUSICBRAINZ_RELEASEGROUPID": al.MBID, "MUSICBRAINZ_ARTISTID": a.MBID, "MUSICBRAINZ_ALBUMARTISTID": a.MBID}
}

func (s *Service) saveFile(ctx context.Context, a *Artist, al *Album, t *Track, f *LocalFile, placed importer.Placed, q string, o ImportOptions,
	old *TrackFile, now time.Time) error {
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if old != nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM media_files WHERE id = ?`, old.ID); err != nil {
				return err
			}
		}
		// A row for the same path (re-import) is replaced.
		if _, err := tx.ExecContext(ctx, `DELETE FROM media_files WHERE path = ?`, placed.Path); err != nil {
			return err
		}
		group := ""
		if o.ReleaseTitle != "" {
			group = parser.Parse(o.ReleaseTitle).Group
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO media_files (item_id, path, size, quality, release_group, scene_name, download_id, media_info, added_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, a.ID, placed.Path, placed.Size, q, group, o.ReleaseTitle, o.DownloadID,
			fmt.Sprintf(`{"source":"tags","format":%q,"bitDepth":%d,"sampleRate":%d,"bitrate":%d,"channels":%d}`, f.Tags.Format, f.Tags.BitDepth,
				f.Tags.SampleRate, f.Tags.Bitrate, f.Tags.Channels), db.Millis(now))
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		_, err = tx.ExecContext(ctx, `INSERT INTO album_files (file_id, album_id, disc, position, recording_mbid, title, duration_ms, format,
			bit_depth, sample_rate, bitrate, tags) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, al.ID, t.Disc, t.Position, t.RecordingMBID,
			t.Title, f.Tags.Duration.Milliseconds(), f.Tags.Format, f.Tags.BitDepth, f.Tags.SampleRate, f.Tags.Bitrate, tagJSON(&f.Tags))
		return err
	})
}

func tagJSON(t *tags.Tags) string {
	return jsonObject(map[string]any{"title": t.Title, "artist": t.Artist, "albumArtist": t.AlbumArtist, "album": t.Album, "date": t.Date,
		"track": t.Track, "disc": t.Disc, "recordingId": t.RecordingID, "releaseId": t.ReleaseID})
}

// saveCover puts cover.jpg into the album folder: the download's own cover
// image, else the Cover Art Archive front (when enabled). An existing cover
// is replaced only by one from a download (the old one goes to the bin).
func (s *Service) saveCover(ctx context.Context, al *Album, r *Release, found, dir, mode string, perms importer.Permissions, downloadID *int64) string {
	ext := strings.ToLower(filepath.Ext(found))
	if ext == ".jpeg" {
		ext = ".jpg"
	}
	if found != "" {
		dst := filepath.Join(dir, "cover"+ext)
		if same, _ := sameFile(found, dst); same {
			return dst
		}
		if _, err := os.Stat(dst); err == nil {
			if _, err := s.Recycle.Recycle(ctx, dst, al.Ref().String(), "replaced by the download's cover"); err != nil {
				return ""
			}
		}
		if mode == library.TransferMove {
			mode = library.TransferCopy // the download keeps its image
		}
		if _, err := fsops.Transfer(found, dst, mode, perms.Folder); err != nil {
			s.log().Warn("cover", "err", err)
			return ""
		}
		s.record(ctx, al.Ref(), "cover_saved", "saved the download's cover as "+filepath.Base(dst), map[string]any{"path": dst, "source": "download"}, downloadID)
		return dst
	}
	dst := filepath.Join(dir, "cover.jpg")
	if _, err := os.Stat(dst); err == nil {
		return dst
	}
	if on, _ := settings.Get(ctx, s.Settings, SaveCoverArt); !on || s.Covers == nil {
		return ""
	}
	b, err := s.Covers.FetchCover(ctx, r.MBID, al.MBID, "1200")
	if err != nil {
		s.log().Info("no cover art", "album", al.Title, "err", err)
		return ""
	}
	if _, err := lfs.SafePath(s.DataRoots, dst); err != nil {
		return ""
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil { // #nosec G306 -- library files are world-readable for media servers
		return ""
	}
	if perms.File != 0 {
		_ = os.Chmod(dst, perms.File)
	}
	s.record(ctx, al.Ref(), "cover_saved", "saved the Cover Art Archive front cover as cover.jpg", map[string]any{"path": dst, "source": "coverartarchive"}, downloadID)
	return dst
}

func (s *Service) notify(ctx context.Context, a *Artist, al *Album, action, path string) {
	if s.Notifier == nil || s.Notifier.Name() == "" {
		return
	}
	err := s.Notifier.Notify(ctx, mediaserver.Change{ItemRef: al.Ref().String(), Kind: "album", Action: action, Title: a.Name + " - " + al.Title,
		Year: al.Year(), Paths: []string{path}, IDs: map[string]string{"musicbrainz": al.MBID}})
	msg := "notified " + s.Notifier.Name()
	if err != nil {
		msg = "could not notify " + s.Notifier.Name() + ": " + err.Error()
	}
	s.record(ctx, al.Ref(), timeline.TypeNotified, msg, map[string]any{"server": s.Notifier.Name(), "ok": err == nil}, nil)
}

func sameFile(a, b string) (bool, error) {
	sa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(sa, sb), nil
}

// ImportDownload imports a completed download grabbed for an album; it
// always ends with MarkImported or MarkImportFailed.
func (s *Service) ImportDownload(ctx context.Context, downloadID int64) error {
	d, err := s.Downloads.Get(ctx, downloadID)
	if err != nil {
		return err
	}
	if d.Status != downloads.StatusCompleted {
		return nil
	}
	ref := library.RefFromJSON(d.ItemRef)
	fail := func(albumRef *library.Ref, err error) error {
		if albumRef != nil {
			data := map[string]any{"download": d.Title, "path": d.LocalPath}
			if ie, ok := IsImportError(err); ok {
				data["code"] = ie.Code
			}
			s.record(ctx, *albumRef, timeline.TypeImportFailed, err.Error(), data, &d.ID)
		}
		return s.Downloads.MarkImportFailed(ctx, d.ID, err.Error())
	}
	if ref.Kind != library.RefAlbum {
		return fail(nil, rejectf(RejectNoAlbum, "the download is not linked to an album"))
	}
	if _, err := s.Album(ctx, ref.ID); errors.Is(err, ErrNotFound) {
		return fail(nil, rejectf(RejectNoAlbum, "the album was removed from the library"))
	} else if err != nil {
		return err
	}
	res, err := s.Import(ctx, ref.ID, d.LocalPath, ImportOptions{ReleaseTitle: d.Title, DownloadID: &d.ID})
	if err != nil {
		return fail(&ref, err)
	}
	return s.Downloads.MarkImported(ctx, d.ID, fmt.Sprintf("imported %d tracks (score %d)", len(res.Imported), res.Score))
}
