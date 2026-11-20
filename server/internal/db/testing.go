package db

import (
	"context"
	"path/filepath"
	"testing"
)

// OpenTest opens a migrated database in a temporary folder that is removed
// when the test ends.
func OpenTest(t testing.TB) *DB {
	t.Helper()
	d, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}
