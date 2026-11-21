package quality

import (
	"context"
	"errors"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/parser"
)

func TestIdentify(t *testing.T) {
	cases := []struct {
		name string
		kind Kind
		want string
	}{
		{"Movie.2020.1080p.BluRay.x264-GRP", Movies, "bluray-1080p"},
		{"Movie.2020.2160p.UHD.BluRay.REMUX.HEVC-GRP", Movies, "remux-2160p"},
		{"Movie.2020.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-GRP", Movies, "remux-1080p"},
		{"Movie.2020.1080p.Blu-ray.AVC.DTS-HD.MA.5.1-GRP", Movies, "brdisk"},
		{"Movie.2020.720p.WEB-DL.x264-GRP", Movies, "webdl-720p"},
		{"Movie.2020.1080p.WEBRip.x265-GRP", Movies, "webrip-1080p"},
		{"Movie.2020.WEB.H264-GRP", Movies, "webdl-480p"},
		{"Movie.2020.HDCAM.x264-GRP", Movies, "cam"},
		{"Movie.2020.DVDSCR.XviD-GRP", Movies, "screener"},
		{"Movie.2020.DVDRip.XviD-GRP", Movies, "dvd"},
		{"Movie.2020.PAL.DVDR-GRP", Movies, "dvdr"},
		{"Movie.2020.576p.BDRip.x264-GRP", Movies, "bluray-576p"},
		{"Movie.2020.BDRip.XviD-GRP", Movies, "bluray-480p"},
		{"Movie.2020.8K.WEB.HEVC-GRP", Movies, "webdl-4320p"},
		{"Movie.2020.1080p.x264-GRP", Movies, "webdl-1080p"},
		{"Show.S01E01.720p.x264-GRP", Series, "hdtv-720p"},
		{"Show.S01E01.HDTV.x264-GRP", Series, "sdtv"},
		{"Show.S01E01.1080p.HDTV.x264-GRP", Series, "hdtv-1080p"},
		{"Show.S01E01.Raw-HD.1080i.MPEG2-GRP", Series, "rawhd"},
		{"Show.S01E01.x264-GRP", Series, "unknown"},
		{"Artist - Album (2020) [FLAC]", Music, "flac"},
		{"Artist - Album (2020) [FLAC 24bit-96kHz]", Music, "flac-24"},
		{"Artist - Album (2020) [MP3 320]", Music, "mp3-320"},
		{"Artist - Album (2020) [MP3 V0]", Music, "mp3-v0"},
		{"Artist-Album-WEB-2020-GRP", Music, "mp3"},
		{"Artist - Album (2020) [ALAC]", Music, "alac"},
		{"Artist - Album (2020)", Music, UnknownMusic},
	}
	for _, c := range cases {
		r := parser.Parse(c.name)
		if got := Identify(&r, c.kind).ID; got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestDefaultsAreValid(t *testing.T) {
	for _, d := range DefaultDefinitions() {
		if err := d.Validate(); err != nil {
			t.Errorf("%+v: %v", d, err)
		}
	}
	seen := map[string]bool{}
	for _, p := range DefaultProfiles() {
		p := p
		if err := p.Validate(); err != nil {
			t.Errorf("%s/%s: %v", p.Kind, p.Name, err)
		}
		if seen[string(p.Kind)+p.Name] {
			t.Errorf("duplicate default %s", p.Name)
		}
		seen[string(p.Kind)+p.Name] = true
		if n := len(p.Items); n == 0 {
			t.Errorf("%s has no items", p.Name)
		}
	}
	if len(ForKind(Music))+len(ForKind(Movies)) != len(All()) {
		t.Error("every quality is either video or music")
	}
}

func TestProfileValidation(t *testing.T) {
	base := func() Profile {
		return Profile{Name: "P", Kind: Movies, Items: []Item{{Quality: "bluray-1080p", Allowed: true},
			{Name: "WEB", Qualities: []string{"webdl-1080p", "webrip-1080p"}, Allowed: true}}, Cutoff: "group:WEB"}
	}
	bad := map[string]func(p *Profile){
		"empty name":       func(p *Profile) { p.Name = " " },
		"bad kind":         func(p *Profile) { p.Kind = "books" },
		"music quality":    func(p *Profile) { p.Items = append(p.Items, Item{Quality: "flac"}) },
		"twice":            func(p *Profile) { p.Items = append(p.Items, Item{Quality: "bluray-1080p"}) },
		"none allowed":     func(p *Profile) { p.Items[0].Allowed, p.Items[1].Allowed = false, false },
		"cutoff missing":   func(p *Profile) { p.Cutoff = "remux-2160p" },
		"cutoff disallow":  func(p *Profile) { p.Items[1].Allowed = false },
		"both":             func(p *Profile) { p.Items[0].Qualities = []string{"hdtv-1080p"} },
		"empty group":      func(p *Profile) { p.Items[1].Qualities = nil },
		"language":         func(p *Profile) { p.Languages = []string{"klingon"} },
		"scores":           func(p *Profile) { p.MinFormatScore, p.CutoffFormatScore = 100, 10 },
		"duplicate format": func(p *Profile) { p.FormatScores = []FormatScore{{1, 1}, {1, 2}} },
	}
	ok := base()
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mut := range bad {
		p := base()
		mut(&p)
		var ie *InvalidError
		if err := p.Validate(); !errors.As(err, &ie) {
			t.Errorf("%s: want InvalidError, got %v", name, err)
		}
	}
	p := base()
	p.Normalize()
	if pos, allowed := p.Position("webrip-1080p"); pos != 1 || !allowed {
		t.Errorf("position %d %v", pos, allowed)
	}
	if pos, allowed := p.Position("cam"); pos < 2 || allowed {
		t.Errorf("normalised cam at %d allowed=%v", pos, allowed)
	}
}

func TestTerms(t *testing.T) {
	cases := []struct {
		term, name string
		want       bool
	}{
		{"x265", "Movie.2020.1080p.x265-GRP", true},
		{"X265", "movie.2020.x265", true},
		{"/\\bHEVC\\b/", "Movie 2020 HEVC", true},
		{"/(?<!DD)\\+/", "Movie DD+5.1", false},
		{"/^Movie/i", "movie.2020", true},
		{"/[/", "x", false},
	}
	for _, c := range cases {
		term, err := CompileTerm(c.term)
		if err != nil {
			if c.term != "/[/" {
				t.Errorf("%s: %v", c.term, err)
			}
			continue
		}
		if got := term.Match(c.name); got != c.want {
			t.Errorf("%s in %s: got %v", c.term, c.name, got)
		}
	}
}

func TestStore(t *testing.T) {
	ctx := context.Background()
	s := NewStore(db.OpenTest(t))
	if err := s.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Seed(ctx); err != nil {
		t.Fatal("seeding twice:", err)
	}
	profiles, err := s.Profiles(ctx)
	if err != nil || len(profiles) != len(DefaultProfiles()) {
		t.Fatalf("profiles %d %v", len(profiles), err)
	}
	// Deleting every profile does not bring the defaults back.
	for _, p := range profiles {
		if err := s.DeleteProfile(ctx, p.ID); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.Seed(ctx)
	if profiles, _ = s.Profiles(ctx); len(profiles) != 0 {
		t.Fatal("defaults came back")
	}
	p, err := s.CreateProfile(ctx, DefaultProfiles()[2])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProfile(ctx, DefaultProfiles()[2]); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("duplicate name: %v", err)
	}
	p.Name = "Renamed"
	p.Kind = Music // ignored
	if p, err = s.UpdateProfile(ctx, p); err != nil || p.Kind != Movies {
		t.Fatalf("update: %v %s", err, p.Kind)
	}
	if err := s.DeleteProfile(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}

	defs, err := s.Definitions(ctx)
	if err != nil || len(defs) != len(DefaultDefinitions()) {
		t.Fatalf("definitions %d %v", len(defs), err)
	}
	if err := s.UpdateDefinitions(ctx, []Definition{{Kind: Movies, Quality: "bluray-1080p", Min: 10, Preferred: 20, Max: 30}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDefinitions(ctx, []Definition{{Kind: Movies, Quality: "bluray-1080p", Min: 40, Max: 30}}); err == nil {
		t.Fatal("min above max accepted")
	}
	m := DefinitionMap(mustDefs(t, s))
	if m[Movies]["bluray-1080p"].Max != 30 || m[Series]["bluray-1080p"].Max != 155 {
		t.Fatalf("definition not stored per kind: %+v", m[Movies]["bluray-1080p"])
	}
	if err := s.ResetDefinitions(ctx, Movies); err != nil {
		t.Fatal(err)
	}
	if DefinitionMap(mustDefs(t, s))[Movies]["bluray-1080p"].Max != 155 {
		t.Fatal("reset did not restore")
	}

	rp, err := s.SaveReleaseProfile(ctx, ReleaseProfile{Name: "No x265", Enabled: true, Ignored: []string{" x265 ", "/hevc/"}, Tags: []string{"Anime", "anime"}})
	if err != nil || rp.ID == 0 || len(rp.Ignored) != 2 || len(rp.Tags) != 1 {
		t.Fatalf("release profile %+v %v", rp, err)
	}
	if _, err := s.SaveReleaseProfile(ctx, ReleaseProfile{Name: "Empty"}); err == nil {
		t.Fatal("empty release profile accepted")
	}

	delays, _ := s.DelayProfiles(ctx)
	if len(delays) != 1 || !delays[0].Default {
		t.Fatalf("default delay profile: %+v", delays)
	}
	if err := s.DeleteDelayProfile(ctx, delays[0].ID); !errors.Is(err, ErrDefault) {
		t.Fatal(err)
	}
	a, err := s.SaveDelayProfile(ctx, DelayProfile{Name: "Anime", Enabled: true, PreferredProtocol: Torrent, TorrentEnabled: true,
		TorrentDelay: 60, Tags: []string{"anime"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.SaveDelayProfile(ctx, DelayProfile{Name: "4K", Enabled: true, PreferredProtocol: Usenet, UsenetEnabled: true, Tags: []string{"4k"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderDelayProfiles(ctx, []int64{b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	delays, _ = s.DelayProfiles(ctx)
	if delays[0].ID != b.ID || delays[1].ID != a.ID || !delays[2].Default {
		t.Fatalf("order: %+v", delays)
	}
	if got := PickDelayProfile(delays, []string{"anime"}); got.ID != a.ID {
		t.Fatalf("picked %s", got.Name)
	}
	if got := PickDelayProfile(delays, []string{"other"}); !got.Default {
		t.Fatalf("picked %s", got.Name)
	}
	if _, err := s.SaveDelayProfile(ctx, DelayProfile{Name: "No tags", PreferredProtocol: Usenet, UsenetEnabled: true}); err == nil {
		t.Fatal("untagged non-default accepted")
	}
}

func mustDefs(t *testing.T, s *Store) []Definition {
	t.Helper()
	d, err := s.Definitions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return d
}
