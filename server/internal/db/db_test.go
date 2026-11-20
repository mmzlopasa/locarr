package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOpenPragmasAndMigrate(t *testing.T) {
	d := OpenTest(t)
	ctx := context.Background()
	var mode string
	if err := d.W.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode %q %v", mode, err)
	}
	for _, conn := range []Querier{d.W, d.R} {
		var fk int
		if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
			t.Fatalf("foreign_keys %d %v", fk, err)
		}
		var busy int
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil || busy != 10000 {
			t.Fatalf("busy_timeout %d %v", busy, err)
		}
	}
	v, err := d.Version(ctx)
	if err != nil || v < 1 {
		t.Fatalf("version %d %v", v, err)
	}
	// Migrating again is a no-op.
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestReaderIsReadOnly(t *testing.T) {
	d := OpenTest(t)
	_, err := d.R.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('x', '1', 0)`)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "readonly") {
		t.Fatalf("want read-only error, got %v", err)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	d := OpenTest(t)
	_, err := d.W.Exec(`INSERT INTO sessions (user_id, token_hash, device_name, client, ip, created_at, last_seen_at, expires_at)
		VALUES (999, x'00', 'd', 'c', '', 0, 0, 0)`)
	if err == nil {
		t.Fatal("orphan session inserted")
	}
}

func TestConcurrentWritesAndReads(t *testing.T) {
	d := OpenTest(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for i := range 100 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			errs <- d.Tx(ctx, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, '1', ?)`,
					"k"+string(rune('a'+i%26))+time.Now().Format("150405.000000000"), Millis(time.Now()))
				return err
			})
		}()
		go func() {
			defer wg.Done()
			var n int
			errs <- d.R.QueryRowContext(ctx, `SELECT count(*) FROM settings`).Scan(&n)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestPersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "p.db")
	d, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('a', '"b"', 1)`); err != nil {
		t.Fatal(err)
	}
	if d.Size() == 0 {
		t.Fatal("size 0")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var v string
	if err := d.R.QueryRow(`SELECT value FROM settings WHERE key = 'a'`).Scan(&v); err != nil || v != `"b"` {
		t.Fatalf("%q %v", v, err)
	}
}

func TestTimeHelpers(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	if !Time(Millis(now)).Equal(now) {
		t.Fatal("round trip")
	}
}
