package app

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/library/movies"
	"github.com/mmzlopasa/locarr/server/internal/parser"
)

// seedMovies inserts n movies straight into the database.
func seedMovies(t testing.TB, le *libraryEnv, n int) {
	t.Helper()
	now := time.Now().UnixMilli()
	err := le.deps.DB.Tx(context.Background(), func(tx *sql.Tx) error {
		item, err := tx.Prepare(`INSERT INTO media_items (kind, title, sort_title, year, monitored, quality_profile_id, root_folder_id, path, tags, added_at, updated_at)
			VALUES ('movie', ?, ?, ?, 1, ?, ?, ?, '["seed"]', ?, ?) RETURNING id`)
		if err != nil {
			return err
		}
		mv, err := tx.Prepare(`INSERT INTO movies (item_id, tmdb_id, imdb_id, status, digital_release, runtime) VALUES (?, ?, ?, 'Released', '2001-01-01', 100)`)
		if err != nil {
			return err
		}
		file, err := tx.Prepare(`INSERT INTO media_files (item_id, path, size, quality, added_at) VALUES (?, ?, 1000, 'webdl-1080p', ?)`)
		if err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			title := fmt.Sprintf("Seed Movie %05d", i)
			path := filepath.Join(le.movies, title)
			var id int64
			if err := item.QueryRow(title, fmt.Sprintf("seed movie %05d", i), 1950+i%70, le.profileID, le.rootID, path, now, now).Scan(&id); err != nil {
				return err
			}
			if _, err := mv.Exec(id, 1_000_000+i, fmt.Sprintf("tt9%06d", i)); err != nil {
				return err
			}
			if i%2 == 0 {
				if _, err := file.Exec(id, filepath.Join(path, title+".mkv"), now); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMovieListPerformance(t *testing.T) {
	le := newLibraryEnv(t)
	seedMovies(t, le, 10_000)
	for _, q := range []string{"", "?sort=year&desc=true", "?status=downloaded", "?tag=seed&offset=5000", "?q=movie%2009"} {
		le.do("GET", "/api/v1/movies"+q, nil) // warm up
		start := time.Now()
		r := le.expect(le.do("GET", "/api/v1/movies"+q+map[bool]string{true: "&", false: "?"}[q != ""]+"limit=100", nil), 200)
		took := time.Since(start)
		var list struct {
			Total int
			Items []struct{ ID int64 }
		}
		r.json(&list)
		if len(list.Items) == 0 {
			t.Fatalf("%s: empty", q)
		}
		t.Logf("GET /movies%s: %d of %d in %s", q, len(list.Items), list.Total, took)
		// The race detector slows everything several times; the target is for normal builds.
		if took > 100*time.Millisecond && !raceEnabled {
			t.Errorf("GET /movies%s took %s (target < 100 ms)", q, took)
		}
	}
}

func TestRSSMatcher(t *testing.T) {
	le := newLibraryEnv(t)
	m := le.addMovie(45745, false) // Sintel, alternate title Durian
	match, err := movies.WantedSource{S: le.deps.Movies}.Matcher(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		title string
		tmdb  int64
		want  bool
	}{
		{"Sintel.2010.1080p.BluRay.x264-A", 0, true},
		{"Durian.2010.720p.WEB-DL-B", 0, true},
		{"Sintel.2011.1080p.WEB-DL-C", 0, true},  // year ±1
		{"Sintel.2014.1080p.WEB-DL-D", 0, false}, // another year
		{"Totally.Different.2010.1080p-E", 45745, true},
		{"Sintel.S01E01.1080p.WEB-DL-F", 0, false}, // an episode
	}
	for _, c := range cases {
		r := indexers.Release{Title: c.title, TMDBID: c.tmdb}
		p := parser.Parse(c.title)
		refs := match(&r, &p)
		if got := len(refs) == 1 && refs[0].ID == m.ID; got != c.want {
			t.Errorf("%s: matched %v, want %v", c.title, refs, c.want)
		}
	}
}
