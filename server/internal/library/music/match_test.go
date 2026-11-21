package music

import (
	"fmt"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/library/tags"
)

func cand(id string, discs ...[]string) Candidate {
	c := Candidate{ReleaseMBID: id, GroupMBID: "g1", Title: "Free Album", Artist: "Open Source Artist", Aliases: []string{"OSA"}}
	n := 0
	for d, titles := range discs {
		for i, t := range titles {
			n++
			c.Tracks = append(c.Tracks, CandidateTrack{Disc: d + 1, Position: i + 1, Title: t, LengthMs: 180_000 + 10_000*n,
				RecordingMBID: fmt.Sprintf("rec-%d-%d", d+1, i+1)})
		}
	}
	return c
}

// filesFor makes local files for a candidate's tracks (with optional changes).
func filesFor(c Candidate, mod func(i int, f *LocalFile)) []LocalFile {
	var out []LocalFile
	for i, t := range c.Tracks {
		f := LocalFile{Rel: fmt.Sprintf("%02d %s.flac", t.Position, t.Title), Tags: tags.Tags{Title: t.Title, Album: c.Title, AlbumArtist: c.Artist,
			Track: t.Position, Disc: t.Disc, Duration: time.Duration(t.LengthMs) * time.Millisecond, Format: tags.FormatFLAC}}
		if mod != nil {
			mod(i, &f)
		}
		f.Prepare()
		out = append(out, f)
	}
	return out
}

var five = []string{"Opening", "Second Light", "Žuta Kuća", "Night Bus", "Closing"}

func TestMatchExact(t *testing.T) {
	c := cand("r1", five)
	m := MatchRelease(filesFor(c, nil), c)
	if m.Score < 95 || len(m.Pairs) != 5 || len(m.Missing) != 0 || len(m.Extra) != 0 {
		t.Fatalf("exact: %+v", m)
	}
}

func TestMatchWithoutTagsByNameAndDuration(t *testing.T) {
	c := cand("r1", five)
	files := filesFor(c, func(i int, f *LocalFile) {
		f.Tags = tags.Tags{Duration: f.Tags.Duration, Format: tags.FormatMP3}
		f.Rel = fmt.Sprintf("Free Album/%02d - Open Source Artist - %s.mp3", i+1, five[i])
	})
	m := MatchRelease(files, c)
	if len(m.Pairs) != 5 || m.Score < 75 {
		t.Fatalf("untagged: score %d pairs %d", m.Score, len(m.Pairs))
	}
	for _, p := range m.Pairs {
		if p.File != p.Track {
			t.Fatalf("wrong pairing %+v", p)
		}
	}
}

func TestMatchMultiDisc(t *testing.T) {
	deluxe := cand("deluxe", five, []string{"Bonus One", "Bonus Two", "Bonus Three"})
	standard := cand("standard", five)
	// Files say disc only through the folder: "CD1/01 …", "CD2/01 …".
	files := filesFor(deluxe, func(i int, f *LocalFile) {
		d := deluxe.Tracks[i].Disc
		f.Tags.Disc = 0
		f.Rel = fmt.Sprintf("CD%d/%02d %s.flac", d, deluxe.Tracks[i].Position, deluxe.Tracks[i].Title)
	})
	best, ms := BestMatch(files, []Candidate{standard, deluxe})
	if best != 1 {
		t.Fatalf("picked %d: standard %d deluxe %d", best, ms[0].Score, ms[1].Score)
	}
	m := ms[1]
	if m.Score < 90 || len(m.Missing) != 0 {
		t.Fatalf("deluxe: %+v", m)
	}
	for _, p := range m.Pairs {
		if files[p.File].Disc != deluxe.Tracks[p.Track].Disc {
			t.Fatalf("disc mix-up: %+v", p)
		}
	}
	// The standard edition leaves the bonus disc as extra files.
	if len(ms[0].Extra) != 3 || ms[0].Score >= m.Score {
		t.Fatalf("standard: %+v", ms[0])
	}
}

func TestMatchMissingAndExtra(t *testing.T) {
	c := cand("r1", five)
	files := filesFor(c, nil)[:3]
	files = append(files, LocalFile{Rel: "hidden track.flac", Tags: tags.Tags{Title: "Hidden Track", Duration: 30 * time.Second}})
	files[3].Prepare()
	m := MatchRelease(files, c)
	if len(m.Missing) != 2 || len(m.Extra) != 1 || m.Score >= 90 {
		t.Fatalf("partial: %+v", m)
	}
}

func TestMatchDurationTolerance(t *testing.T) {
	c := cand("r1", five)
	near := filesFor(c, func(_ int, f *LocalFile) { f.Tags.Duration += 3 * time.Second })
	far := filesFor(c, func(_ int, f *LocalFile) { f.Tags.Duration += 90 * time.Second })
	mn, mf := MatchRelease(near, c), MatchRelease(far, c)
	if mn.DurationOK != 1 || mf.DurationOK != 0 || mf.Score >= mn.Score {
		t.Fatalf("near %d (%.2f) far %d (%.2f)", mn.Score, mn.DurationOK, mf.Score, mf.DurationOK)
	}
	if DurationTolerance(60_000) != 5000 || DurationTolerance(600_000) != 30_000 {
		t.Fatal("tolerance")
	}
}

func TestMatchReleaseIDs(t *testing.T) {
	c := cand("r1", five)
	other := cand("r2", five)
	files := filesFor(c, func(i int, f *LocalFile) { f.Tags.ReleaseID = "r1"; f.Tags.RecordingID = c.Tracks[i].RecordingMBID })
	best, ms := BestMatch(files, []Candidate{other, c})
	if best != 1 || !ms[1].IDMatch || ms[1].Score != 100 || ms[0].Score >= ms[1].Score {
		t.Fatalf("ids: %d %+v %+v", best, ms[0], ms[1])
	}
	// Recording ids put files on tracks even with wrong numbers and titles.
	shuffled := filesFor(c, func(i int, f *LocalFile) {
		f.Tags.RecordingID, f.Tags.Track, f.Tags.Title = c.Tracks[4-i].RecordingMBID, i+1, "x"
	})
	m := MatchRelease(shuffled, c)
	for _, p := range m.Pairs {
		if p.Track != 4-p.File || !p.ByID {
			t.Fatalf("by id: %+v", p)
		}
	}
}

func TestMatchWrongAlbum(t *testing.T) {
	c := cand("r1", five)
	files := filesFor(cand("x", []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta", "Eta"}), func(i int, f *LocalFile) {
		f.Tags.Album, f.Tags.AlbumArtist = "Something Else", "Somebody"
		f.Tags.Duration = time.Duration(400+i*20) * time.Second
	})
	if m := MatchRelease(files, c); m.Score >= 50 {
		t.Fatalf("wrong album scored %d", m.Score)
	}
}

func TestSimilarity(t *testing.T) {
	cases := []struct {
		a, b string
		min  float64
	}{
		{"Žuta Kuća", "zuta kuca", 1}, {"Night Bus (Remastered 2009)", "Night Bus", 1}, {"Rock & Roll", "Rock and Roll", 1},
		{"Second Light", "Second Lite", 0.7},
	}
	for _, c := range cases {
		if s := Similarity(c.a, c.b); s < c.min {
			t.Errorf("%q vs %q: %.2f", c.a, c.b, s)
		}
	}
	if Similarity("Opening", "Closing") > 0.6 {
		t.Error("different titles too similar")
	}
}

func TestPrepareNumbers(t *testing.T) {
	cases := []struct {
		rel         string
		disc, track int
	}{
		{"CD2/03 Title.flac", 2, 3}, {"Disc 1/07 - Title.flac", 1, 7}, {"105 Title.mp3", 1, 5}, {"205. Title.mp3", 2, 5}, {"Title.flac", 1, 0},
	}
	for _, c := range cases {
		f := LocalFile{Rel: c.rel}
		f.Prepare()
		if f.Disc != c.disc || f.Track != c.track {
			t.Errorf("%s: disc %d track %d", c.rel, f.Disc, f.Track)
		}
	}
}

func TestQualityOf(t *testing.T) {
	cases := []struct {
		t    tags.Tags
		want string
	}{
		{tags.Tags{Format: tags.FormatFLAC, BitDepth: 24}, "flac-24"}, {tags.Tags{Format: tags.FormatFLAC, BitDepth: 16}, "flac"},
		{tags.Tags{Format: tags.FormatMP3, Bitrate: 320}, "mp3-320"}, {tags.Tags{Format: tags.FormatMP3, Bitrate: 245, VBR: true}, "mp3-v0"},
		{tags.Tags{Format: tags.FormatMP3, Bitrate: 190, VBR: true}, "mp3-v2"}, {tags.Tags{Format: tags.FormatMP3, Bitrate: 192}, "mp3-192"},
		{tags.Tags{Format: tags.FormatALAC, BitDepth: 24}, "alac"}, {tags.Tags{Format: tags.FormatAAC}, "aac"},
		{tags.Tags{Format: tags.FormatOpus}, "opus"}, {tags.Tags{Format: tags.FormatVorbis}, "vorbis"},
	}
	for _, c := range cases {
		if got := QualityOf(&c.t); got != c.want {
			t.Errorf("%+v: %s, want %s", c.t, got, c.want)
		}
	}
}

func TestMetadataProfileFilters(t *testing.T) {
	std := MetadataProfile{Name: "Standard", PrimaryTypes: []string{"album", "ep"}, SecondaryTypes: []string{"studio"}, ReleaseStatuses: []string{"official"}}
	live := MetadataProfile{Name: "Live too", PrimaryTypes: []string{"album"}, SecondaryTypes: []string{"studio", "live"}, ReleaseStatuses: []string{"official", "bootleg"}}
	cases := []struct {
		p         *MetadataProfile
		primary   string
		secondary []string
		want      bool
	}{
		{&std, "Album", nil, true}, {&std, "EP", nil, true}, {&std, "Single", nil, false}, {&std, "Album", []string{"Live"}, false},
		{&std, "Album", []string{"Compilation"}, false}, {&live, "Album", []string{"Live"}, true}, {&live, "Album", []string{"Live", "Remix"}, false},
		{&live, "", nil, false}, {&MetadataProfile{PrimaryTypes: []string{"other"}, SecondaryTypes: []string{"studio"}}, "", nil, true},
	}
	for _, c := range cases {
		if got := c.p.WantsGroup(c.primary, c.secondary); got != c.want {
			t.Errorf("%s %s %v: %v", c.p.Name, c.primary, c.secondary, got)
		}
	}
	if !std.WantsStatus("Official") || std.WantsStatus("Bootleg") || !live.WantsStatus("bootleg") || !std.WantsStatus("") {
		t.Error("statuses")
	}
	bad := MetadataProfile{Name: "x", PrimaryTypes: []string{"album"}, SecondaryTypes: []string{"karaoke"}, ReleaseStatuses: []string{"official"}}
	if bad.Validate() == nil {
		t.Error("unknown secondary type accepted")
	}
	empty := MetadataProfile{Name: "x", PrimaryTypes: []string{" Album ", "album"}, SecondaryTypes: []string{"studio"}, ReleaseStatuses: nil}
	if empty.Validate() == nil {
		t.Error("no status accepted")
	}
}

func TestChooseRelease(t *testing.T) {
	p := &MetadataProfile{ReleaseStatuses: []string{"official"}}
	rels := []Release{
		{ID: 1, MBID: "a", Status: "official", Country: "GB", Date: "2020-11-20", Formats: []string{"CD", "CD"}, TrackCount: 8},
		{ID: 2, MBID: "b", Status: "official", Country: "XW", Date: "2020-03-14", Formats: []string{"Digital Media"}, TrackCount: 5},
		{ID: 3, MBID: "c", Status: "official", Country: "US", Date: "2020-05-01", Formats: []string{"12\" Vinyl"}, TrackCount: 5},
		{ID: 4, MBID: "d", Status: "bootleg", Country: "US", Date: "2019-01-01", Formats: []string{"CD"}, TrackCount: 5},
		{ID: 5, MBID: "e", Status: "official", Country: "US", Date: "2021-01-01", Formats: []string{"CD"}, TrackCount: 5},
	}
	if r := ChooseRelease(rels, p, "US"); r == nil || r.ID != 5 {
		t.Fatalf("US: %+v", r)
	}
	if r := ChooseRelease(rels, p, "HR"); r == nil || r.ID != 2 {
		t.Fatalf("HR (worldwide wins): %+v", r)
	}
	if r := ChooseRelease(rels[3:4], p, "US"); r == nil || r.ID != 4 {
		t.Fatal("only a bootleg: it is still chosen")
	}
	if ChooseRelease(nil, p, "") != nil {
		t.Fatal("none")
	}
}

func TestReleaseTime(t *testing.T) {
	if ReleaseTime("2020").Format("2006-01-02") != "2020-12-31" || ReleaseTime("2020-02").Format("2006-01-02") != "2020-02-29" ||
		ReleaseTime("2020-03-14").Format("2006-01-02") != "2020-03-14" || !ReleaseTime("").IsZero() {
		t.Fatal("release times")
	}
}

func TestMusicNaming(t *testing.T) {
	a := naming.ArtistInfo{Name: "The Open: Source", MBID: "x"}
	al := naming.AlbumInfo{Title: "Free/Album?", Type: "album", ReleaseDate: "2020-03-14", Discs: 2, Tracks: 8}
	tr := naming.TrackInfo{Disc: 2, Number: 3, Title: "Bonus: Three", Format: "flac", BitDepth: 24, SampleRate: 96000, Quality: "FLAC 24bit", Extension: ".flac"}
	o := naming.Options{Colon: naming.ColonDash}
	render := func(tmpl string, v naming.Values) string {
		s, err := naming.Render(tmpl, v, o)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	if got := render("{ArtistNameThe}", naming.MusicValues(a, nil, nil)); got != "Open - Source, The" {
		t.Errorf("artist: %q", got)
	}
	if got := render("{AlbumTitle} ({ReleaseYear}) {[AudioFormat]}", naming.MusicValues(a, &al, &tr)); got != "Free Album (2020) [FLAC]" {
		t.Errorf("album: %q", got)
	}
	if got := render("{DiscNumber}-{TrackNumber} - {TrackTitle} {[AudioBitDepth]}{[AudioSampleRate]}", naming.MusicValues(a, &al, &tr)); got != "2-03 - Bonus - Three [24bit][96kHz]" {
		t.Errorf("track: %q", got)
	}
	if got := render("{AlbumType} - {AlbumTitle} {[ReleaseDisambiguation]}", naming.MusicValues(a, &al, nil)); got != "Album - Free Album" {
		t.Errorf("empty token: %q", got)
	}
	for _, tmpl := range []string{"{ArtistName}", "{AlbumTitle} ({ReleaseYear})", "{TrackNumber} - {TrackTitle}", "Disc {DiscNumber}"} {
		if err := naming.Validate(tmpl, naming.TrackTokens); err != nil {
			t.Errorf("%s: %v", tmpl, err)
		}
	}
	if naming.Validate("{TrackTitle}", naming.ArtistTokens) == nil {
		t.Error("track token accepted for the artist folder")
	}
	tr.SampleRate = 44100
	if v := naming.MusicValues(a, &al, &tr); v["AudioSampleRate"] != "44.1kHz" {
		t.Errorf("sample rate %q", v["AudioSampleRate"])
	}
}
