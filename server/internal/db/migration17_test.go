package db

import (
	"context"
	"path/filepath"
	"testing"
)

// TestMigration17HistoryTitles reproduces the reuse of a deleted movie's id
// under the old schema and checks what migration 00017 makes of it.
func TestMigration17HistoryTitles(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, err := d.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 16); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.W.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO quality_profiles (id, name, kind, data, created_at, updated_at) VALUES (100, 'p', 'movies', '{}', 1, 1)`)
	exec(`INSERT INTO users (id, username, username_key, password_hash, role, created_at, updated_at) VALUES (1, 'a', 'a', 'x', 'admin', 1, 1)`)
	// Movie 1 stays; Iron Man gets id 2, fails to grab, is deleted; Avengers
	// is added next and (old schema) gets id 2 again.
	exec(`INSERT INTO media_items (id, kind, title, sort_title, year, quality_profile_id, path, added_at, updated_at)
		VALUES (1, 'movie', 'Sintel', 'sintel', 2010, 100, '/m/sintel', 1000, 1000)`)
	exec(`INSERT INTO movies (item_id, tmdb_id) VALUES (1, 45745)`)
	exec(`INSERT INTO media_items (kind, title, sort_title, year, quality_profile_id, path, added_at, updated_at)
		VALUES ('movie', 'Iron Man', 'iron man', 2008, 100, '/m/ironman', 2000, 2000)`)
	exec(`INSERT INTO movies (item_id, tmdb_id) VALUES (2, 1726)`)
	exec(`INSERT INTO timeline_events (item_ref, at, type, message) VALUES ('movie:1', 1500, 'file_imported', 'imported')`)
	exec(`INSERT INTO timeline_events (item_ref, at, type, message) VALUES ('movie:2', 2100, 'grab_failed', 'could not grab Iron.Man.2008.720p: x')`)
	exec(`INSERT INTO timeline_events (item_ref, at, type, message) VALUES ('movie:2', 2200, 'deleted', 'removed from the library')`)
	exec(`INSERT INTO search_log (started_at, duration_ms, source, search_type, query, params, item_ref, result_count, error_count)
		VALUES (2050, 1, 'automatic', 'movie', 'Iron Man', '{}', 'movie:2', 3, 0)`)
	exec(`INSERT INTO download_clients (id, name, implementation, protocol, host, port, created_at, updated_at)
		VALUES (1, 'c', 'transmission', 'torrent', 'h', 1, 1, 1)`)
	exec(`INSERT INTO downloads (client_id, client_name, client_item_id, protocol, title, item_ref, status, state, added_at, updated_at)
		VALUES (1, 'c', 'a', 'torrent', 'Iron.Man.2008.1080p', '{"ref":"movie:2"}', 'failed', 'failed', 2150, 2150)`)
	exec(`INSERT INTO requests (kind, status, tmdb_id, title, requested_by, item_ref, created_at, updated_at)
		SELECT 'movie', 'available', 1726, 'Iron Man', id, 'movie:2', 1900, 1900 FROM users LIMIT 1`)
	exec(`DELETE FROM media_items WHERE id = 2`)
	exec(`INSERT INTO media_items (kind, title, sort_title, year, quality_profile_id, path, added_at, updated_at)
		VALUES ('movie', 'Avengers: Doomsday', 'avengers doomsday', 2026, 100, '/m/avengers', 3000, 3000)`)
	var reused int64
	if err := d.W.QueryRowContext(ctx, `SELECT id FROM media_items WHERE path = '/m/avengers'`).Scan(&reused); err != nil || reused != 2 {
		t.Fatalf("expected the old schema to reuse id 2, got %d %v", reused, err)
	}
	exec(`INSERT INTO movies (item_id, tmdb_id) VALUES (2, 1003596)`)
	exec(`INSERT INTO timeline_events (item_ref, at, type, message) VALUES ('movie:2', 3001, 'added', 'added')`)
	// Delete the newest again: before the fix the next movie would get 2 again.
	exec(`INSERT INTO media_items (kind, title, sort_title, year, quality_profile_id, path, added_at, updated_at)
		VALUES ('movie', 'Gone', 'gone', 2020, 100, '/m/gone', 4000, 4000)`)
	exec(`INSERT INTO timeline_events (item_ref, at, type, message) VALUES ('movie:3', 4001, 'deleted', 'removed')`)
	exec(`DELETE FROM media_items WHERE id = 3`)

	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	type ev struct{ ref, title string }
	rows, err := d.R.QueryContext(ctx, `SELECT item_ref, item_title FROM timeline_events ORDER BY at`)
	if err != nil {
		t.Fatal(err)
	}
	var got []ev
	for rows.Next() {
		var e ev
		if err := rows.Scan(&e.ref, &e.title); err != nil {
			t.Fatal(err)
		}
		got = append(got, e)
	}
	_ = rows.Close()
	want := []ev{{"movie:1", "Sintel (2010)"}, {"movie:2#removed", ""}, {"movie:2#removed", ""}, {"movie:2", "Avengers: Doomsday (2026)"}, {"movie:3", ""}}
	if len(got) != len(want) {
		t.Fatalf("events %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %v, want %v", i, got[i], want[i])
		}
	}
	var s string
	_ = d.R.QueryRowContext(ctx, `SELECT item_ref FROM search_log`).Scan(&s)
	if s != "movie:2#removed" {
		t.Errorf("search_log ref %q", s)
	}
	var dref, dtitle string
	_ = d.R.QueryRowContext(ctx, `SELECT json_extract(item_ref, '$.ref'), item_title FROM downloads`).Scan(&dref, &dtitle)
	if dref != "movie:2#removed" || dtitle != "" {
		t.Errorf("download ref %q title %q", dref, dtitle)
	}
	var n int
	_ = d.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_refs WHERE item_ref = 'movie:2'`).Scan(&n)
	if n != 0 {
		t.Errorf("download_refs still point at the new movie")
	}
	_ = d.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM requests WHERE item_ref = 'movie:2#removed'`).Scan(&n)
	if n != 1 {
		t.Errorf("request not detached")
	}
	// Children survived the rebuild.
	_ = d.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM movies`).Scan(&n)
	if n != 2 {
		t.Errorf("movies rows %d", n)
	}
	// The next movie gets an id no ref ever used.
	exec(`INSERT INTO media_items (kind, title, sort_title, year, quality_profile_id, path, added_at, updated_at)
		VALUES ('movie', 'Next', 'next', 2027, 100, '/m/next', 5000, 5000)`)
	var next int64
	_ = d.R.QueryRowContext(ctx, `SELECT id FROM media_items WHERE path = '/m/next'`).Scan(&next)
	if next != 4 {
		t.Errorf("next id %d, want 4", next)
	}
	// Deleting the newest and adding another never reuses its id.
	exec(`DELETE FROM media_items WHERE id = 4`)
	exec(`INSERT INTO media_items (kind, title, sort_title, year, quality_profile_id, path, added_at, updated_at)
		VALUES ('movie', 'After', 'after', 2027, 100, '/m/after', 6000, 6000)`)
	_ = d.R.QueryRowContext(ctx, `SELECT id FROM media_items WHERE path = '/m/after'`).Scan(&next)
	if next != 5 {
		t.Errorf("id after delete %d, want 5", next)
	}
	// Foreign keys are on again and enforced.
	var fk int
	_ = d.W.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk)
	if fk != 1 {
		t.Fatal("foreign keys left off")
	}
	if _, err := d.W.ExecContext(ctx, `INSERT INTO movies (item_id, tmdb_id) VALUES (999, 1)`); err == nil {
		t.Fatal("foreign key not enforced")
	}
	exec(`DELETE FROM media_items WHERE id = 1`)
	_ = d.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM movies WHERE item_id = 1`).Scan(&n)
	if n != 0 {
		t.Fatal("cascade lost")
	}
}
