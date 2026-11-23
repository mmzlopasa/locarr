package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathCacheMatchesSafePath(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "b", "f.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "a", "b", "link.mkv")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "a", "out")); err != nil {
		t.Fatal(err)
	}
	c := &PathCache{Roots: []string{root}}
	for _, p := range []string{filepath.Join(root, "a", "b", "f.mkv"), filepath.Join(root, "a", "b", "link.mkv"),
		filepath.Join(root, "a", "out", "secret"), filepath.Join(root, "a", "b", "..", "..", "..", "etc"), filepath.Join(root, "new", "x.mkv"),
		"relative/x", "", root, filepath.Dir(root)} {
		for range 2 { // second time from the cache
			got, err := c.SafePath(p)
			want, werr := SafePath([]string{root}, p)
			if got != want || (err == nil) != (werr == nil) {
				t.Fatalf("%s: cache %q %v, SafePath %q %v", p, got, err, want, werr)
			}
		}
	}
}
