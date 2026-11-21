package app

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/series"
	"github.com/mmzlopasa/locarr/server/internal/parser"
)

// seedSeries inserts n series with seasons×episodes each straight into the database.
func seedSeries(t testing.TB, se *seriesEnv, n, seasons, episodes int) []int64 {
	t.Helper()
	now := time.Now()
	var ids []int64
	err := se.deps.DB.Tx(context.Background(), func(tx *sql.Tx) error {
		item, err := tx.Prepare(`INSERT INTO media_items (kind, title, sort_title, year, monitored, quality_profile_id, root_folder_id, path, tags, added_at, updated_at)
			VALUES ('series', ?, ?, ?, 1, ?, ?, ?, '["seed"]', ?, ?) RETURNING id`)
		if err != nil {
			return err
		}
		sr, err := tx.Prepare(`INSERT INTO series (item_id, tmdb_id, tvdb_id, status, network, runtime) VALUES (?, ?, ?, ?, 'Seed TV', 45)`)
		if err != nil {
			return err
		}
		sn, err := tx.Prepare(`INSERT INTO seasons (series_id, number, monitored) VALUES (?, ?, 1) RETURNING id`)
		if err != nil {
			return err
		}
		ep, err := tx.Prepare(`INSERT INTO episodes (series_id, season_id, season_number, episode_number, title, air_date, air_date_utc, monitored, added_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?)`)
		if err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			title := fmt.Sprintf("Seed Show %05d", i)
			var id int64
			if err := item.QueryRow(title, fmt.Sprintf("seed show %05d", i), 1990+i%30, se.tvProfile, se.tvRoot, filepath.Join(se.tv, title),
				now.UnixMilli(), now.UnixMilli()).Scan(&id); err != nil {
				return err
			}
			status := []string{"continuing", "ended", "upcoming"}[i%3]
			if _, err := sr.Exec(id, 2_000_000+i, 3_000_000+i, status); err != nil {
				return err
			}
			for s := 1; s <= seasons; s++ {
				var sid int64
				if err := sn.QueryRow(id, s).Scan(&sid); err != nil {
					return err
				}
				for e := 1; e <= episodes; e++ {
					air := now.AddDate(0, 0, -(seasons-s)*90-(episodes-e)*7+3)
					if _, err := ep.Exec(id, sid, s, e, fmt.Sprintf("Episode %d", e), air.Format("2006-01-02"), air.UnixMilli(), now.UnixMilli()); err != nil {
						return err
					}
				}
			}
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestSeriesListPerformance(t *testing.T) {
	se := newSeriesEnv(t)
	seedSeries(t, se, 5000, 2, 10)
	for _, q := range []string{"", "?sort=year&desc=true", "?status=ended", "?tag=seed&offset=2500", "?q=show%2004", "?missing=true", "?sort=nextAiring", "?sort=missing&desc=true", "?sort=size"} {
		se.do("GET", "/api/v1/series"+q, nil) // warm up
		start := time.Now()
		r := se.expect(se.do("GET", "/api/v1/series"+q+map[bool]string{true: "&", false: "?"}[q != ""]+"limit=100", nil), 200)
		took := time.Since(start)
		var list struct {
			Total int
			Items []struct {
				ID    int64
				Stats struct{ EpisodeCount, MissingCount int }
			}
		}
		r.json(&list)
		if len(list.Items) == 0 || list.Items[0].Stats.EpisodeCount != 20 {
			t.Fatalf("%s: %+v", q, list.Items)
		}
		t.Logf("GET /series%s: %d of %d in %s", q, len(list.Items), list.Total, took)
		// Sorting by episode facts aggregates every episode (5000 series × 20 here); the 100 ms target is the plain list.
		limit := 100 * time.Millisecond
		// So does the missing filter here, where every one of the 100 000 episodes is missing (the worst case).
		if strings.Contains(q, "sort=nextAiring") || strings.Contains(q, "sort=missing") || strings.Contains(q, "missing=true") {
			limit = 400 * time.Millisecond
		}
		if took > limit && !raceEnabled {
			t.Errorf("GET /series%s took %s (target < %s)", q, took, limit)
		}
	}
}

func TestSeriesDetailPerformance(t *testing.T) {
	se := newSeriesEnv(t)
	ids := seedSeries(t, se, 1, 30, 34) // 1020 episodes
	for _, p := range []string{"", "/episodes"} {
		path := fmt.Sprintf("/api/v1/series/%d%s", ids[0], p)
		se.do("GET", path, nil)
		start := time.Now()
		se.expect(se.do("GET", path, nil), 200)
		took := time.Since(start)
		t.Logf("GET %s in %s", path, took)
		if took > 300*time.Millisecond && !raceEnabled {
			t.Errorf("GET %s took %s", path, took)
		}
	}
}

func TestSeriesRSSMatcher(t *testing.T) {
	se := newSeriesEnv(t)
	s := se.add(800424, "standard", "all", false) // Elephants Dream, alternate title Machina
	e2 := se.episode(s.ID, 1, 2)
	var season1 int64
	_ = se.deps.DB.R.QueryRow(`SELECT id FROM seasons WHERE series_id = ? AND number = 1`, s.ID).Scan(&season1)
	match, err := series.EpisodeSource{S: se.deps.Series}.Matcher(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		title string
		tvdb  int64
		want  []library.Ref
	}{
		{"Elephants.Dream.S01E02.720p.WEB.h264-A", 0, []library.Ref{library.EpisodeRef(e2.ID)}},
		{"Machina.S01E02.1080p.WEB-DL-B", 0, []library.Ref{library.EpisodeRef(e2.ID)}},
		{"Some.Other.Name.S01E02.1080p.WEB-DL-C", 424242, []library.Ref{library.EpisodeRef(e2.ID)}},
		{"Elephants.Dream.S01.1080p.WEB-DL-D", 0, []library.Ref{library.SeasonRef(season1)}},
		{"Elephants.Dream.2006.1080p.BluRay-E", 0, nil}, // a movie
		{"Elephants.Dream.S05E01.1080p.WEB-DL-F", 0, nil},
	}
	for _, c := range cases {
		r := indexers.Release{Title: c.title, TVDBID: c.tvdb}
		p := parser.Parse(c.title)
		if got := match(&r, &p); fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s: %v, want %v", c.title, got, c.want)
		}
	}
}
