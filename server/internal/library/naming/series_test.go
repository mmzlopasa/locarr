package naming

import (
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
)

func TestEpisodeCodeStyles(t *testing.T) {
	eps := []EpisodeInfo{{Season: 1, Number: 3}, {Season: 1, Number: 1}, {Season: 1, Number: 2}}
	want := map[string]string{
		MultiExtend: "S01E01-E02-E03", MultiDuplicate: "S01E01.S01E02.S01E03", MultiRepeat: "S01E01E02E03",
		MultiScene: "S01E01-02-03", MultiRange: "S01E01-03", MultiPrefixedRange: "S01E01-E03",
	}
	for style, w := range want {
		if got := EpisodeCode(eps, style); got != w {
			t.Errorf("%s: %s, want %s", style, got, w)
		}
	}
	if got := EpisodeCode(eps[:1], MultiExtend); got != "S01E03" {
		t.Errorf("single: %s", got)
	}
}

func file(name string, kind quality.Kind) *FileInfo {
	r := parser.Parse(name)
	return &FileInfo{Release: &r, Quality: quality.Identify(&r, kind)}
}

func TestEpisodeNames(t *testing.T) {
	s := SeriesInfo{Title: "The Expanse", Year: 2015, TVDbID: 280619}
	tmpl := "{SeriesTitle} - {EpisodeCode} - {EpisodeTitle} {[QualityFull]}{-ReleaseGroup}"
	f := file("The.Expanse.S01E01.1080p.WEB-DL.DDP5.1.H.264-GROUP", quality.Series)
	got, err := Render(tmpl, EpisodeValues(s, []EpisodeInfo{{Season: 1, Number: 1, Title: "Dulcinea"}}, f, MultiPrefixedRange), Options{})
	if err != nil || got != "The Expanse - S01E01 - Dulcinea [WEBDL-1080p]-GROUP" {
		t.Fatalf("standard: %q %v", got, err)
	}
	multi := []EpisodeInfo{{Season: 1, Number: 1, Title: "Pilot (1)"}, {Season: 1, Number: 2, Title: "Pilot (2)"}}
	got, _ = Render(tmpl, EpisodeValues(s, multi, f, MultiPrefixedRange), Options{})
	if got != "The Expanse - S01E01-E02 - Pilot [WEBDL-1080p]-GROUP" {
		t.Fatalf("multi: %q", got)
	}
	two := []EpisodeInfo{{Season: 1, Number: 1, Title: "A"}, {Season: 1, Number: 2, Title: "B"}}
	got, _ = Render(tmpl, EpisodeValues(s, two, nil, MultiExtend), Options{})
	if got != "The Expanse - S01E01-E02 - A + B" {
		t.Fatalf("titles: %q", got)
	}
	daily := "{SeriesTitle} - {AirDate} - {EpisodeTitle}"
	got, _ = Render(daily, EpisodeValues(SeriesInfo{Title: "The Open Desk"}, []EpisodeInfo{{Season: 2025, Number: 2, AirDate: "2025-03-14", Title: "March 14"}}, nil, ""), Options{})
	if got != "The Open Desk - 2025-03-14 - March 14" {
		t.Fatalf("daily: %q", got)
	}
	anime := "{SeriesTitle} - {EpisodeCode} - {Absolute} - {EpisodeTitle}"
	got, _ = Render(anime, EpisodeValues(SeriesInfo{Title: "Spring Kurenai"}, []EpisodeInfo{{Season: 2, Number: 1, Absolute: 3, Title: "Bloom 1"}}, nil, ""), Options{})
	if got != "Spring Kurenai - S02E01 - 003 - Bloom 1" {
		t.Fatalf("anime: %q", got)
	}
	got, _ = Render("{SeriesTitleYear}", SeriesValues(s), Options{})
	if got != "The Expanse (2015)" {
		t.Fatalf("folder: %q", got)
	}
	got, _ = Render("Season {Season}", SeasonValues(s, 3), Options{})
	if got != "Season 03" {
		t.Fatalf("season: %q", got)
	}
	for _, tm := range []string{tmpl, daily, anime} {
		if err := Validate(tm, EpisodeTokens); err != nil {
			t.Fatal(err)
		}
	}
	if err := Validate("{Title}", EpisodeTokens); err == nil {
		t.Fatal("movie token accepted in an episode template")
	}
}
