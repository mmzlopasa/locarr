package rootfolders

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/db"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
)

func setup(t *testing.T) (*Service, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "data")
	for _, d := range []string{"media/movies", "media/tv", "media/.hidden", "downloads"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "media", "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "outside"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "outside"), filepath.Join(root, "media", "escape")); err != nil {
		t.Fatal(err)
	}
	root, _ = filepath.EvalSymlinks(root)
	return &Service{DB: db.OpenTest(t), Roots: []string{root}}, root
}

func TestAddListRemove(t *testing.T) {
	s, root := setup(t)
	ctx := context.Background()
	movies := filepath.Join(root, "media", "movies")
	f, err := s.Add(ctx, movies+"/", KindMovies)
	if err != nil {
		t.Fatal(err)
	}
	if f.Path != movies || !f.Accessible || !f.Writable || f.Space == nil || f.Space.Total == 0 {
		t.Fatalf("%+v", f)
	}
	if _, err := s.Add(ctx, movies, KindMovies); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, filepath.Join(root, "media", "tv"), "books"); !errors.Is(err, ErrInvalidKind) {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, filepath.Join(root, "..", "outside"), KindMusic); !errors.Is(err, lfs.ErrNotAllowed) {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, filepath.Join(root, "media", "escape"), KindMusic); !errors.Is(err, lfs.ErrNotAllowed) {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, filepath.Join(root, "missing"), KindMusic); !errors.Is(err, lfs.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, filepath.Join(root, "media", "file.txt"), KindMusic); !errors.Is(err, ErrNotDir) {
		t.Fatal(err)
	}
	list, err := s.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	// A folder that disappeared is reported, not hidden.
	if err := os.Remove(movies); err != nil {
		t.Fatal(err)
	}
	list, _ = s.List(ctx)
	if list[0].Accessible || list[0].Space != nil {
		t.Fatalf("%+v", list[0])
	}
	if err := s.Remove(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, f.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestBrowse(t *testing.T) {
	s, root := setup(t)
	l, err := s.Browse("")
	if err != nil || len(l.Entries) != 1 || l.Entries[0].Path != root || l.Path != "" {
		t.Fatal(l, err)
	}
	l, err = s.Browse(filepath.Join(root, "media"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Parent != root || len(l.Entries) != 2 || l.Entries[0].Name != "movies" || l.Entries[1].Name != "tv" {
		t.Fatalf("%+v", l)
	}
	l, _ = s.Browse(root)
	if l.Parent != "" {
		t.Fatalf("parent outside the roots offered: %q", l.Parent)
	}
	if _, err := s.Browse("/etc"); !errors.Is(err, lfs.ErrNotAllowed) {
		t.Fatal(err)
	}
	if _, err := s.Browse(filepath.Join(root, "media", "escape")); !errors.Is(err, lfs.ErrNotAllowed) {
		t.Fatal(err)
	}
}
