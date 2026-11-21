package naming

import (
	"strings"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
)

func TestRenderTokensAndOptionalParts(t *testing.T) {
	r := parser.Parse("Blade.Runner.2049.2017.Final.Cut.2160p.UHD.BluRay.REMUX.HDR10.DV.TrueHD.Atmos.7.1.HEVC-GRP")
	q := quality.Identify(&r, quality.Movies)
	v := MovieValues(MovieInfo{Title: "Blade Runner 2049", Year: 2017, TMDbID: 335984, IMDbID: "tt1856101"},
		&FileInfo{Release: &r, Quality: q, CustomFormats: []string{"DV", "Remux"}})
	cases := map[string]string{
		"{Title} ({Year})": "Blade Runner 2049 (2017)",
		"{Title} ({Year}) {[Edition]}{-ReleaseGroup}": "Blade Runner 2049 (2017) [Final Cut]-GRP",
		"{TitleThe} {tmdb-{TmdbId}}":                  "",
		"{Title} {[QualityFull]}{[HDR]}":              "Blade Runner 2049 [" + q.Name + "][" + strings.Join(r.Video.HDR, " ") + "]",
		"{Title} [imdb-{ImdbId}] {[CustomFormats]}":   "Blade Runner 2049 [imdb-tt1856101] [DV Remux]",
		"{Title} {AudioCodec} {AudioChannels}":        "Blade Runner 2049 TrueHD Atmos 7.1",
	}
	for tmpl, want := range cases {
		got, err := Render(tmpl, v, Options{})
		if want == "" {
			if err == nil {
				t.Errorf("%s: want an error, got %q", tmpl, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", tmpl, got, err, want)
		}
	}
	// Empty tokens leave no brackets or doubled spaces behind.
	got, _ := Render("{Title} ({Year}) {[Edition]} {[QualityFull]}{-ReleaseGroup}", MovieValues(MovieInfo{Title: "Up"}, nil), Options{})
	if got != "Up" {
		t.Fatalf("empty tokens: %q", got)
	}
}

func TestValidate(t *testing.T) {
	for _, bad := range []string{"", "{Title", "Title}", "{Nope}", "plain text", "{Title}/{Year}", "{Title Year}", "{{Title}}"} {
		if err := Validate(bad, MovieTokens); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := Validate("{TitleThe} ({Year}) {[Edition]}", MovieTokens); err != nil {
		t.Fatal(err)
	}
}

func TestSanitize(t *testing.T) {
	cases := []struct {
		in, colon, want string
	}{
		{"Mission: Impossible", ColonDash, "Mission - Impossible"},
		{"Mission: Impossible", ColonDelete, "Mission Impossible"},
		{"Re:Zero", ColonDash, "Re-Zero"},
		{"Re:Zero", ColonSpaceDash, "Re - Zero"},
		{`What? "Why" <Who>|*`, ColonDash, "What 'Why' Who"},
		{"AC/DC Live", ColonDash, "AC DC Live"},
		{"Trailing dots...", ColonDash, "Trailing dots"},
		{"CON", ColonDash, "_CON"},
		{"nul.txt", ColonDash, "_nul.txt"},
		{"..", ColonDash, "_"},
		{"tab\there\x00", ColonDash, "tabhere"},
		{"Amélie", ColonDash, "Amélie"},
	}
	for _, c := range cases {
		if got := Sanitize(c.in, Options{Colon: c.colon}); got != c.want {
			t.Errorf("Sanitize(%q, %s) = %q, want %q", c.in, c.colon, got, c.want)
		}
	}
	long := strings.Repeat("Ž", 200) // 400 bytes
	got := Sanitize(long, Options{})
	if len(got) > MaxSegment || !strings.HasPrefix(long, got) {
		t.Fatalf("truncate: %d bytes", len(got))
	}
	name := WithExtension(strings.Repeat("a", 300), ".MKV")
	if len(name) != MaxSegment || !strings.HasSuffix(name, ".mkv") {
		t.Fatalf("WithExtension: %d %s", len(name), name[len(name)-5:])
	}
}

func TestTitles(t *testing.T) {
	if TitleThe("The Matrix") != "Matrix, The" || TitleThe("Theory") != "Theory" || TitleThe("An American Tail") != "American Tail, An" {
		t.Fatal("TitleThe")
	}
	if SortTitle("The Matrix") != "matrix" || CleanTitle("Mission: Impossible - Fallout") != "Mission Impossible Fallout" ||
		CleanTitle("Fast & Furious") != "Fast and Furious" {
		t.Fatal("clean/sort")
	}
	if _, err := Join("/data/movies", "..", "x.mkv"); err == nil {
		t.Fatal("Join accepted ..")
	}
}
