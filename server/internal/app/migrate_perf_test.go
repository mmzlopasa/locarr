package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/migrate/fake"
)

// TestMigrateLargeLibrary imports 10 000 movies, 2 000 series (40 000
// episodes with files) and 1 000 artists (6 000 track files) from the fakes
// and checks it stays within minutes (smaller under the race detector).
func TestMigrateLargeLibrary(t *testing.T) {
	if testing.Short() {
		t.Skip("large import")
	}
	movies, series, artists := 10000, 2000, 1000
	if raceEnabled {
		movies, series, artists = 1000, 200, 100
	}
	lib := fake.Big(movies, series, artists)
	me := newMigrateEnv(t, lib, nil)
	me.wait = 15 * time.Minute
	start := time.Now()
	r := me.analyse()
	read := time.Since(start)
	start = time.Now()
	done := me.importRun(r.ID)
	took := time.Since(start)
	t.Logf("read and planned in %s, imported in %s (%d items)", read.Round(time.Millisecond), took.Round(time.Millisecond), done.Progress.Total)
	if done.Summary.Statuses["failed"] > 0 {
		for _, it := range me.items(r.ID, "&status=failed") {
			t.Errorf("%s: %s", it.Name, it.Error)
		}
		t.FailNow()
	}
	var n, files, eps int
	_ = me.deps.DB.R.QueryRow(`SELECT count(*) FROM media_items`).Scan(&n)
	_ = me.deps.DB.R.QueryRow(`SELECT count(*) FROM media_files`).Scan(&files)
	_ = me.deps.DB.R.QueryRow(`SELECT count(*) FROM episodes WHERE file_id IS NOT NULL`).Scan(&eps)
	if n != movies+series+artists || eps != series*20 || files != movies+series*20+artists*6 {
		t.Fatalf("items %d files %d episodes with files %d", n, files, eps)
	}
	if !raceEnabled && read+took > 5*time.Minute {
		t.Fatalf("too slow: %s", fmt.Sprint(read+took))
	}
}
