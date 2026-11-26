package db

import (
	"context"
	"path/filepath"
	"testing"
)

// TestMigration20AdultGalleries upgrades an install with scenes, a file,
// an exclusion and a deleted item: everything stays, ids are not reused,
// galleries and gallery exclusions can be stored.
func TestMigration20AdultGalleries(t *testing.T) {
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
	if _, err := p.UpTo(ctx, 19); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.W.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO quality_profiles (id, name, kind, data, created_at, updated_at) VALUES (7, 'P', 'movies', '{}', 1, 1)`)
	exec(`INSERT INTO adult_items (id, kind, title, sort_title, quality_profile_id, path, added_at, updated_at, stashdb_id)
		VALUES (5, 'scene', 'Scene Title', 'scene title', 7, '/a/s', 1, 1, 'sd-1'), (9, 'movie', 'Gone', 'gone', 7, '/a/g', 1, 1, '')`)
	exec(`INSERT INTO adult_files (item_id, path, size, quality, added_at) VALUES (5, '/a/s/x.mkv', 10, 'webdl-1080p', 1)`)
	exec(`INSERT INTO adult_exclusions (kind, stashdb_id, title, added_at) VALUES ('scene', 'sd-9', 'Old', 1)`)
	exec(`DELETE FROM adult_items WHERE id = 9`)

	if _, err := p.UpTo(ctx, 20); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = d.R.QueryRowContext(ctx, `SELECT count(*) FROM adult_items i JOIN adult_files f ON f.item_id = i.id WHERE i.id = 5 AND f.image_count = 0`).Scan(&n)
	if n != 1 {
		t.Fatal("scene or its file lost")
	}
	var id int64
	if err := d.W.QueryRowContext(ctx, `INSERT INTO adult_items (kind, title, sort_title, quality_profile_id, path, scene_id, added_at, updated_at)
		VALUES ('gallery', 'Scene Title', 'scene title', 7, '/a/g1', 5, 2, 2) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id <= 9 {
		t.Fatalf("id %d reuses a deleted item's id", id)
	}
	// A gallery exclusion with the same source id as a scene one is allowed;
	// a second scene one is not.
	exec(`INSERT INTO adult_exclusions (kind, stashdb_id, title, added_at) VALUES ('gallery', 'sd-9', 'Old', 2)`)
	if _, err := d.W.ExecContext(ctx, `INSERT INTO adult_exclusions (kind, stashdb_id, title, added_at) VALUES ('movie', 'sd-9', 'Old', 2)`); err == nil {
		t.Fatal("duplicate scene/movie exclusion accepted")
	}
	exec(`UPDATE adult_performers SET galleries = 1`)
	// Deleting the scene keeps its gallery, unlinked.
	exec(`DELETE FROM adult_items WHERE id = 5`)
	var scene *int64
	if err := d.R.QueryRowContext(ctx, `SELECT scene_id FROM adult_items WHERE id = ?`, id).Scan(&scene); err != nil || scene != nil {
		t.Fatalf("gallery after scene removal: %v %v", scene, err)
	}
}
