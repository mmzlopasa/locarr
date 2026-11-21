package customformats

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
)

func input(name string, size int64, flags ...string) Input {
	r := parser.Parse(name)
	return Input{Title: name, Release: &r, Quality: quality.Identify(&r, quality.Movies), Size: size, IndexerFlags: flags, OriginalLanguage: "en"}
}

func importFixture(t *testing.T) (ImportResult, map[string]*Compiled) {
	t.Helper()
	b, err := os.ReadFile("testdata/shared-formats.json")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Import(b, FlavorMovies)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]*Compiled{}
	for _, f := range res.Formats {
		c, err := Compile(f)
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		m[f.Name] = c
	}
	return res, m
}

func TestImportSharedJSON(t *testing.T) {
	res, m := importFixture(t)
	if len(res.Formats) != 8 {
		t.Fatalf("imported %d formats", len(res.Formats))
	}
	if len(res.Warnings) != 1 || len(m["Future Thing"].Unsupported) != 1 {
		t.Fatalf("warnings %v", res.Warnings)
	}
	if v := m["DV (WEBDL)"].Specs[1].Value(); v != "webdl" {
		t.Errorf("source 7 imported as %v", v)
	}
	if v := m["Remux Tier"].Specs[0].Value(); v != "remux" {
		t.Errorf("modifier 5 imported as %v", v)
	}
	if v := m["Not Original or English"].Specs[1].Value(); v != "original" {
		t.Errorf("language -2 imported as %v", v)
	}
	if v := m["Freeleech"].Specs[0].Value(); v != "freeleech" {
		t.Errorf("list-style fields imported as %v", v)
	}

	cases := []struct {
		format, name string
		size         int64
		flags        []string
		want         bool
	}{
		{"x265 (HD)", "Movie.2020.1080p.BluRay.x265-GRP", 0, nil, true},
		{"x265 (HD)", "Movie.2020.2160p.BluRay.x265-GRP", 0, nil, false},
		{"x265 (HD)", "Movie.2020.1080p.BluRay.x264-GRP", 0, nil, false},
		{"Remux Tier", "Movie.2020.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-FGT", 0, nil, true},
		{"Remux Tier", "Movie.2020.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-OTHER", 0, nil, false},
		{"Remux Tier", "Movie.2020.1080p.BluRay.x264-FGT", 0, nil, false},
		{"DV (WEBDL)", "Movie.2020.2160p.WEB-DL.DDP5.1.DV.H.265-GRP", 0, nil, true},
		{"DV (WEBDL)", "Movie.2020.2160p.BluRay.REMUX.DV.HEVC-GRP", 0, nil, false},
		{"Not Original or English", "Movie.2020.1080p.BluRay.x264-GRP", 0, nil, false},
		{"Not Original or English", "Movie.2020.GERMAN.1080p.BluRay.x264-GRP", 0, nil, true},
		{"Freeleech", "Movie.2020.1080p.BluRay.x264-GRP", 0, []string{"freeleech"}, true},
		{"Freeleech", "Movie.2020.1080p.BluRay.x264-GRP", 0, nil, false},
		{"Big", "Movie.2020.2160p.BluRay.REMUX-GRP", 50 << 30, nil, true},
		{"Big", "Movie.2020.2160p.BluRay.REMUX-GRP", 20 << 30, nil, false},
		{"Bad Dual Groups", "Movie.2020.1080p.WEB-DL.DUAL.BlackBit", 0, nil, true},
		{"Future Thing", "Movie.2020.1080p.AMZN.WEB-DL-GRP", 0, nil, true},
		{"Future Thing", "Movie.2020.1080p.NF.WEB-DL-GRP", 0, nil, false},
	}
	for _, c := range cases {
		if got := m[c.format].Matches(input(c.name, c.size, c.flags...)); got != c.want {
			_, why := m[c.format].Evaluate(input(c.name, c.size, c.flags...))
			t.Errorf("%s on %s: got %v want %v (%+v)", c.format, c.name, got, c.want, why)
		}
	}
}

func TestImportErrors(t *testing.T) {
	bad := []string{"", "{", `{"specifications": []}`, `[{"name": "x", "specifications": [{"name": "r", "implementation": "ReleaseTitleSpecification", "fields": {"value": "("}}]}]`,
		`{"name": "x", "specifications": [{"implementation": "SizeSpecification", "fields": {"min": 10, "max": 5}}]}`,
		`{"name": "x", "specifications": [{"implementation": "SourceSpecification", "fields": {"value": "laserdisc"}}]}`}
	for _, b := range bad {
		var ie *InvalidError
		if _, err := Import([]byte(b), FlavorMovies); !errors.As(err, &ie) {
			t.Errorf("%q: got %v", b, err)
		}
	}
}

func TestSeriesFlavorAndExport(t *testing.T) {
	res, err := Import([]byte(`{"name": "WEB", "specifications": [{"name": "web", "implementation": "SourceSpecification", "required": true, "fields": {"value": 3}}]}`), FlavorSeries)
	if err != nil {
		t.Fatal(err)
	}
	if v := res.Formats[0].Specs[0].Value(); v != "webdl" {
		t.Fatalf("series source 3 imported as %v", v)
	}
	exp := Export(res.Formats[0], FlavorMovies)
	b, _ := json.Marshal(exp)
	again, err := Import(b, FlavorMovies)
	if err != nil || again.Formats[0].Specs[0].Value() != "webdl" {
		t.Fatalf("round trip: %s %v", b, err)
	}
	if specs := exp["specifications"].([]map[string]any); specs[0]["fields"].(map[string]any)["value"] != 7 {
		t.Fatalf("export numbering: %s", b)
	}
}

func TestNativeConditions(t *testing.T) {
	f := Format{Name: "UHD DV Atmos NF", Specs: []Spec{
		{Implementation: HDR, Required: true, Fields: map[string]any{"value": "DV"}},
		{Implementation: AudioCodec, Required: true, Fields: map[string]any{"value": "atmos"}},
		{Implementation: StreamingService, Fields: map[string]any{"value": "Netflix"}},
		{Implementation: StreamingService, Fields: map[string]any{"value": "Disney+"}},
		{Implementation: VideoCodec, Required: true, Fields: map[string]any{"value": "h265"}},
		{Implementation: Edition, Negate: true, Required: true, Fields: map[string]any{"value": "IMAX"}},
		{Implementation: Year, Fields: map[string]any{"min": 2000.0, "max": 2030.0}},
	}}
	c, err := Compile(f)
	if err != nil {
		t.Fatal(err)
	}
	yes := "Movie.2020.2160p.NF.WEB-DL.DDP5.1.Atmos.DV.H.265-GRP"
	no := []string{"Movie.2020.2160p.AMZN.WEB-DL.DDP5.1.Atmos.DV.H.265-GRP", "Movie.2020.IMAX.2160p.NF.WEB-DL.DDP5.1.Atmos.DV.H.265-GRP",
		"Movie.2020.2160p.NF.WEB-DL.DDP5.1.DV.H.265-GRP", "Movie.1990.2160p.NF.WEB-DL.DDP5.1.Atmos.DV.H.265-GRP"}
	if !c.Matches(input(yes, 0)) {
		t.Errorf("%s should match", yes)
	}
	for _, n := range no {
		if c.Matches(input(n, 0)) {
			t.Errorf("%s should not match", n)
		}
	}
	p := &quality.Profile{FormatScores: []quality.FormatScore{{FormatID: 7, Score: 150}}}
	c.ID = 7
	matches, total := Score([]*Compiled{c}, p, input(yes, 0))
	if total != 150 || len(matches) != 1 || matches[0].Score != 150 {
		t.Fatalf("score %d %+v", total, matches)
	}
}

func TestStore(t *testing.T) {
	ctx := context.Background()
	s := NewStore(db.OpenTest(t))
	f, err := s.Save(ctx, Format{Name: "x265", Specs: []Spec{{Implementation: ReleaseTitle, Fields: map[string]any{"value": "x265"}}}})
	if err != nil || f.ID == 0 {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, Format{Name: "X265", Specs: f.Specs}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("case-insensitive name: %v", err)
	}
	if _, err := s.SaveAll(ctx, []Format{{Name: "x265", Specs: f.Specs}}, false); !errors.Is(err, ErrNameTaken) {
		t.Fatal(err)
	}
	out, err := s.SaveAll(ctx, []Format{{Name: "x265", Specs: f.Specs}, {Name: "new", Specs: f.Specs}}, true)
	if err != nil || out[0].ID != f.ID || out[1].ID == 0 {
		t.Fatalf("replace import: %+v %v", out, err)
	}
	list, _ := s.List(ctx)
	if len(list) != 2 {
		t.Fatalf("list %d", len(list))
	}
	if err := s.Delete(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, f.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
