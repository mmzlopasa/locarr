package series

import (
	"fmt"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/parser"
)

func ip(n int) *int { return &n }

func catalog() []Episode {
	var eps []Episode
	id := int64(1)
	for s := 0; s <= 2; s++ {
		for n := 1; n <= 4; n++ {
			eps = append(eps, Episode{ID: id, Season: s, Number: n, Absolute: map[bool]int{true: 0, false: (s-1)*4 + n}[s == 0],
				AirDate: fmt.Sprintf("2024-0%d-%02d", s+1, n*7)})
			id++
		}
	}
	return eps
}

func ids(list []*Episode) string {
	out := ""
	for _, e := range list {
		out += fmt.Sprintf("S%02dE%02d ", e.Season, e.Number)
	}
	return out
}

func TestMatchRelease(t *testing.T) {
	eps := catalog()
	cases := []struct {
		typ, name, want string
	}{
		{TypeStandard, "Show.S01E02.1080p.WEB.h264-GRP", "S01E02 "},
		{TypeStandard, "Show.S01E02E03.1080p.WEB.h264-GRP", "S01E02 S01E03 "},
		{TypeStandard, "Show.S01E01-E03.720p.HDTV.x264-GRP", "S01E01 S01E02 S01E03 "},
		{TypeStandard, "Show.S02.1080p.BluRay.x264-GRP", "S02E01 S02E02 S02E03 S02E04 "},
		{TypeStandard, "Show.S00E02.Special.720p.WEB.h264-GRP", "S00E02 "},
		{TypeStandard, "Show.S03E01.720p.WEB.h264-GRP", ""},
		{TypeDaily, "Show.2024.02.14.720p.WEB.h264-GRP", "S01E02 "},
		{TypeAnime, "[Group] Show - 06 [1080p]", "S02E02 "},
		{TypeAnime, "[Group] Show - 01-03 [1080p]", "S01E01 S01E02 S01E03 "},
		{TypeStandard, "Show.Complete.Series.1080p.WEB.h264-GRP", "S01E01 S01E02 S01E03 S01E04 S02E01 S02E02 S02E03 S02E04 "},
	}
	for _, c := range cases {
		r := parser.Parse(c.name)
		if got := ids(MatchRelease(c.typ, eps, &r)); got != c.want {
			t.Errorf("%s (%s): %q, want %q (parsed %+v %+v %+v %q)", c.name, c.typ, got, c.want, r.Seasons, r.Episodes, r.AbsoluteEpisodes, r.AirDate)
		}
	}
	// A scene mapping wins: releases call S02E01 "S01E05".
	eps[8].SceneSeason, eps[8].SceneEpisode = ip(1), ip(5)
	r := parser.Parse("Show.S01E05.720p.WEB.h264-GRP")
	if got := ids(MatchRelease(TypeStandard, eps, &r)); got != "S02E01 " {
		t.Fatalf("scene mapping: %q", got)
	}
	r = parser.Parse("Show.S02E01.720p.WEB.h264-GRP")
	if got := ids(MatchRelease(TypeStandard, eps, &r)); got != "" {
		t.Fatalf("a mapped episode answers to its scene numbers only: %q", got)
	}
	eps[5].SceneAbsolute = ip(42)
	r = parser.Parse("[Group] Show - 42 [1080p]")
	if got := ids(MatchRelease(TypeAnime, eps, &r)); got != "S01E02 " {
		t.Fatalf("scene absolute: %q", got)
	}
}

func TestIsPack(t *testing.T) {
	for name, want := range map[string]bool{
		"Show.S01.1080p.WEB.h264-GRP":         true,
		"Show.S01-S03.1080p.BluRay.x264-GRP":  true,
		"Show.Season.2.720p.HDTV.x264-GRP":    true,
		"Show.S01E01.1080p.WEB.h264-GRP":      false,
		"Show.2024.02.14.720p.WEB.h264-GRP":   false,
		"[Group] Show - 05 [1080p]":           false,
		"Show.Complete.Series.720p.WEB-DL-GR": true,
	} {
		r := parser.Parse(name)
		if got := IsPack(&r); got != want {
			t.Errorf("%s: %v (parsed %+v)", name, got, r.Seasons)
		}
	}
}

func TestPackWanted(t *testing.T) {
	cases := []struct {
		typ                      string
		season, missing, unaired int
		want                     bool
	}{
		{TypeStandard, 1, 3, 2, true},  // threshold reached while airing
		{TypeStandard, 1, 2, 2, false}, // below the threshold, still airing
		{TypeStandard, 1, 2, 0, true},  // finished airing
		{TypeStandard, 1, 1, 0, false}, // one episode is searched alone
		{TypeStandard, 0, 9, 0, false}, // never specials
		{TypeDaily, 1, 9, 0, false},    // never daily shows
		{TypeAnime, 2, 4, 0, true},
	}
	for _, c := range cases {
		if got := PackWanted(c.typ, c.season, c.missing, c.unaired, 3); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
}

func TestApplyMonitor(t *testing.T) {
	now := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	eps := []EpisodeState{
		{Season: 0, Number: 1, AirUTC: &past},
		{Season: 1, Number: 1, AirUTC: &past, HasFile: true},
		{Season: 1, Number: 2, AirUTC: &past},
		{Season: 2, Number: 1, AirUTC: &past},
		{Season: 2, Number: 2, AirUTC: &future},
		{Season: 2, Number: 3},
	}
	seasons := []int{0, 1, 2, 3}
	want := map[string]string{
		MonitorAll:      "011111",
		MonitorFuture:   "000011",
		MonitorMissing:  "001111",
		MonitorExisting: "010000",
		MonitorFirst:    "011000",
		MonitorLatest:   "000000", // season 3 has no episodes yet
		MonitorPilot:    "010000",
		MonitorNone:     "000000",
	}
	for opt, w := range want {
		got, sm := ApplyMonitor(opt, eps, seasons, now)
		s := ""
		for _, b := range got {
			s += map[bool]string{true: "1", false: "0"}[b]
		}
		if s != w {
			t.Errorf("%s: %s, want %s", opt, s, w)
		}
		if sm[0] {
			t.Errorf("%s monitors specials", opt)
		}
		if opt == MonitorLatest && !sm[3] {
			t.Errorf("latest season without episodes should be monitored")
		}
		if opt == MonitorFuture && (!sm[2] || sm[1]) {
			t.Errorf("future seasons: %+v", sm)
		}
	}
}

func TestPlanMerge(t *testing.T) {
	fid := int64(7)
	existing := []Episode{
		{ID: 1, Season: 1, Number: 1, TMDbID: 101, FileID: &fid},
		{ID: 2, Season: 1, Number: 2, TMDbID: 102},
		{ID: 3, Season: 1, Number: 3, TMDbID: 103, FileID: &fid}, // gone, has a file
		{ID: 4, Season: 1, Number: 4, TMDbID: 104},               // gone, no file
		{ID: 5, Season: 1, Number: 5, TMDbID: 0},                 // no provider id: by number
	}
	provider := []metadata.Season{
		{Number: 1, Episodes: []metadata.Episode{
			{Season: 1, Number: 1, TMDbID: 101, Title: "One"},
			{Season: 1, Number: 3, TMDbID: 102, Title: "Two, moved"}, // renumbered
			{Season: 1, Number: 5, TMDbID: 105, Title: "Five"},
			{Season: 1, Number: 6, TMDbID: 106, Title: "New in a monitored season"},
		}},
		{Number: 2, Episodes: []metadata.Episode{{Season: 2, Number: 1, TMDbID: 201}}},
		{Number: 0, Episodes: []metadata.Episode{{Season: 0, Number: 1, TMDbID: 1}}},
	}
	plan := PlanMerge(existing, provider, "tmdb", map[int]bool{1: true}, true)
	upd := map[int64]EpisodeUpdate{}
	for _, u := range plan.Update {
		upd[u.ID] = u
	}
	if u := upd[2]; !u.Renumbered || u.Number != 3 || u.FromNumber != 2 {
		t.Fatalf("renumber: %+v", u)
	}
	if u, ok := upd[5]; !ok || u.Renumbered || u.TMDbID != 105 {
		t.Fatalf("match by number: %+v", u)
	}
	if len(plan.Delete) != 1 || plan.Delete[0] != 4 || len(plan.Keep) != 1 || plan.Keep[0] != 3 {
		t.Fatalf("delete %v keep %v", plan.Delete, plan.Keep)
	}
	mon := map[string]bool{}
	for _, n := range plan.Insert {
		mon[fmt.Sprintf("S%dE%d", n.Season, n.Number)] = n.Monitored
	}
	if len(mon) != 3 || !mon["S1E6"] || !mon["S2E1"] || mon["S0E1"] {
		t.Fatalf("inserts: %+v", mon)
	}
	if fmt.Sprint(plan.NewSeasons) != "[2 0]" {
		t.Fatalf("new seasons %v", plan.NewSeasons)
	}
	// New seasons stay unmonitored when the series does not monitor them.
	plan = PlanMerge(existing, provider, "tmdb", map[int]bool{1: false}, false)
	for _, n := range plan.Insert {
		if n.Monitored {
			t.Fatalf("monitored %+v", n)
		}
	}
}

func TestFillAbsolute(t *testing.T) {
	seasons := []metadata.Season{
		{Number: 2, Episodes: []metadata.Episode{{Season: 2, Number: 1}}},
		{Number: 0, Episodes: []metadata.Episode{{Season: 0, Number: 1}}},
		{Number: 1, Episodes: []metadata.Episode{{Season: 1, Number: 1}, {Season: 1, Number: 2}}},
	}
	FillAbsolute(seasons)
	if seasons[0].Number != 0 || seasons[0].Episodes[0].Absolute != 0 || seasons[1].Episodes[1].Absolute != 2 || seasons[2].Episodes[0].Absolute != 3 {
		t.Fatalf("%+v", seasons)
	}
}
