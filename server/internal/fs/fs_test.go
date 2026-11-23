package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSafePath(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "data")
	outside := filepath.Join(base, "secret")
	for _, d := range []string{filepath.Join(root, "media", "movies"), outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A link inside the root that points out of it.
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	// A link inside the root that stays inside.
	if err := os.Symlink(filepath.Join(root, "media"), filepath.Join(root, "m")); err != nil {
		t.Fatal(err)
	}
	roots := []string{root}
	ok := map[string]string{
		root:                                   root,
		filepath.Join(root, "media", "movies"): filepath.Join(root, "media", "movies"),
		root + "/media/../media/movies/":       filepath.Join(root, "media", "movies"),
		filepath.Join(root, "m", "movies"):     filepath.Join(root, "media", "movies"),
		filepath.Join(root, "new", "folder"):   filepath.Join(root, "new", "folder"),
	}
	for in, want := range ok {
		got, err := SafePath(roots, in)
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		wantR, _ := filepath.EvalSymlinks(filepath.Dir(want))
		if filepath.Join(wantR, filepath.Base(want)) != got && got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
	bad := []string{
		"", "relative/path", outside, root + "/../secret", root + "/media/../../secret",
		filepath.Join(root, "escape"), filepath.Join(root, "escape", "file"), root + "x", "/etc/passwd", root + "/a\x00b",
	}
	for _, in := range bad {
		if _, err := SafePath(roots, in); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%q: want ErrNotAllowed, got %v", in, err)
		}
	}
	if _, err := MustExist(roots, filepath.Join(root, "nope")); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
	if _, err := MustExist(roots, filepath.Join(root, "media")); err != nil {
		t.Error(err)
	}
}

func TestDiskHelpers(t *testing.T) {
	dir := t.TempDir()
	sp, err := DiskSpace(dir)
	if err != nil || sp.Total <= 0 || sp.Free < 0 {
		t.Fatalf("%+v %v", sp, err)
	}
	if !Writable(dir) || !IsDir(dir) {
		t.Fatal("temp dir not writable/dir")
	}
	if IsDir(filepath.Join(dir, "missing")) || Writable(filepath.Join(dir, "missing")) {
		t.Fatal("missing dir")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("write test left a file")
	}
}

func TestCheckWritableReasons(t *testing.T) {
	dir := t.TempDir()
	if p := CheckWritable(dir); p != nil {
		t.Fatalf("writable dir: %+v", p)
	}
	if p := CheckWritable(filepath.Join(dir, "missing")); p == nil || p.Reason != "not_found" {
		t.Fatalf("missing: %+v", p)
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
	if p := CheckWritable(ro); p == nil || p.Reason != "permission_denied" || p.Detail == "" {
		t.Fatalf("read-only folder: %+v", p)
	}
}
