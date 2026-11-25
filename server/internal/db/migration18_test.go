package db

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
)

// TestMigration18AdultProfileBlocklist upgrades an install with imported
// profiles, adult items and a seeded blocklist the owner has edited.
func TestMigration18AdultProfileBlocklist(t *testing.T) {
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
	if _, err := p.UpTo(ctx, 17); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.W.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	// A profile imported from another application, used by an adult scene.
	exec(`INSERT INTO quality_profiles (id, name, kind, data, created_at, updated_at) VALUES (7, 'HD-1080p', 'movies', '{}', 1, 1)`)
	exec(`INSERT INTO adult_items (kind, title, sort_title, quality_profile_id, path, added_at, updated_at, monitored)
		VALUES ('scene', 'Scene Title', 'scene title', 7, '/a/s', 1, 1, 1)`)
	// The old seed (builtin), one seeded term the owner removed, and the
	// owner's own terms (one a copy of a protected term).
	exec(`INSERT INTO adult_tag_blocklist (term, builtin, added_at) VALUES ('teens', 1, 1), ('underage', 1, 1), ('mytag', 0, 2), ('child', 0, 2)`)

	if _, err := p.UpTo(ctx, 18); err != nil {
		t.Fatal(err)
	}
	var (
		openID int64
		data   string
	)
	if err := d.R.QueryRowContext(ctx, `SELECT id, data FROM quality_profiles WHERE kind = 'movies' AND name = 'Any (Unknown allowed)'`).
		Scan(&openID, &data); err != nil {
		t.Fatal(err)
	}
	var prof struct {
		Items []struct {
			Quality string `json:"quality"`
			Allowed bool   `json:"allowed"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(data), &prof); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(prof.Items, func(i struct {
		Quality string `json:"quality"`
		Allowed bool   `json:"allowed"`
	}) bool {
		return i.Quality == "unknown" && i.Allowed
	}) {
		t.Fatalf("Unknown not allowed: %s", data)
	}
	setting := func(k string) string {
		var v string
		_ = d.R.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, k).Scan(&v)
		return v
	}
	if got := setting("adult.openProfileId"); got != itoa(openID) {
		t.Errorf("openProfileId %q, want %d", got, openID)
	}
	if got := setting("adult.defaultProfileId"); got != itoa(openID) {
		t.Errorf("defaultProfileId %q, want %d (nothing was chosen before)", got, openID)
	}
	// The existing item keeps its profile.
	var pid int64
	if err := d.R.QueryRowContext(ctx, `SELECT quality_profile_id FROM adult_items`).Scan(&pid); err != nil || pid != 7 {
		t.Errorf("item profile %d %v, want 7", pid, err)
	}
	var terms []string
	rows, err := d.R.QueryContext(ctx, `SELECT term FROM adult_tag_blocklist ORDER BY term`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		terms = append(terms, s)
	}
	_ = rows.Close()
	if !slices.Equal(terms, []string{"mytag"}) {
		t.Errorf("terms %v, want only the owner's own non-protected term", terms)
	}
}

// TestMigration18KeepsChosenDefault: an administrator's choice stays.
func TestMigration18KeepsChosenDefault(t *testing.T) {
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
	if _, err := p.UpTo(ctx, 17); err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ('adult.defaultProfileId', '7', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 18); err != nil {
		t.Fatal(err)
	}
	var v string
	if err := d.R.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'adult.defaultProfileId'`).Scan(&v); err != nil || v != "7" {
		t.Fatalf("defaultProfileId %q %v, want 7", v, err)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
