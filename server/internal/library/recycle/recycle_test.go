package recycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

func newBin(t *testing.T) (*Bin, string) {
	t.Helper()
	d := db.OpenTest(t)
	box, err := secrets.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	return &Bin{DB: d, Settings: settings.New(d, box), Roots: []string{root}}, root
}

func TestRecycleRestoreAndRetention(t *testing.T) {
	ctx := context.Background()
	b, root := newBin(t)
	movie := filepath.Join(root, "media", "movies", "Movie (2020)", "Movie (2020).mkv")
	if err := os.MkdirAll(filepath.Dir(movie), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(movie, []byte("film"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := b.Recycle(ctx, movie, "movie:1", "replaced")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(movie); !os.IsNotExist(err) {
		t.Fatal("still in the library")
	}
	if filepath.Dir(filepath.Dir(e.RecycledPath)) != filepath.Join(root, DefaultFolder) || e.Size != 4 {
		t.Fatalf("entry: %+v", e)
	}
	// Restore puts it back; a taken place is refused.
	if _, err := b.Restore(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(movie); err != nil {
		t.Fatal("not restored")
	}
	e, _ = b.Recycle(ctx, movie, "movie:1", "replaced")
	_ = os.WriteFile(movie, []byte("new"), 0o644)
	if _, err := b.Restore(ctx, e.ID); !errors.Is(err, ErrRestoreTaken) {
		t.Fatal(err)
	}
	// Retention removes only old entries, and only inside the bin.
	b.Now = func() time.Time { return time.Now().Add(10 * 24 * time.Hour) }
	if n, err := b.Cleanup(ctx); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := os.Stat(e.RecycledPath); !os.IsNotExist(err) {
		t.Fatal("old entry kept")
	}
	if _, err := os.Stat(movie); err != nil {
		t.Fatal("retention touched the library")
	}
	if err := settings.Set(ctx, b.Settings, nil, library.RecycleBinDays, 0); err != nil {
		t.Fatal(err)
	}
	e, _ = b.Recycle(ctx, movie, "", "")
	if n, _ := b.Cleanup(ctx); n != 0 {
		t.Fatal("retention 0 must keep everything")
	}
	if n, err := b.Empty(ctx); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}

func TestNeverOutsideRoots(t *testing.T) {
	ctx := context.Background()
	b, root := newBin(t)
	outside := filepath.Join(t.TempDir(), "x.mkv")
	_ = os.WriteFile(outside, []byte("x"), 0o644)
	if _, err := b.Recycle(ctx, outside, "", ""); !errors.Is(err, lfs.ErrNotAllowed) {
		t.Fatal(err)
	}
	if err := settings.Set(ctx, b.Settings, nil, library.RecycleBinPath, "/etc"); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "a.mkv")
	_ = os.WriteFile(inside, []byte("x"), 0o644)
	if _, err := b.Recycle(ctx, inside, "", ""); !errors.Is(err, lfs.ErrNotAllowed) {
		t.Fatal(err)
	}
	// A tampered entry pointing outside the bin is never deleted.
	_ = settings.Set(ctx, b.Settings, nil, library.RecycleBinPath, "")
	if _, err := b.DB.W.Exec(`INSERT INTO recycle_bin (original_path, recycled_path, recycled_at) VALUES (?, ?, 0)`, inside, inside); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Empty(ctx); err == nil {
		t.Fatal("deleted a file outside the bin")
	}
	if _, err := os.Stat(inside); err != nil {
		t.Fatal("file outside the bin was deleted")
	}
}
