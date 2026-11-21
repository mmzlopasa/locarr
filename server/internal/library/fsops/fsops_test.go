package fsops

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, bytes.Repeat([]byte("x"), n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHardlinkOnSameFilesystem(t *testing.T) {
	d := t.TempDir()
	src, dst := filepath.Join(d, "dl", "a.mkv"), filepath.Join(d, "lib", "Movie", "a.mkv")
	write(t, src, 1000)
	if !SameFilesystem(src, dst) {
		t.Fatal("same temp dir reported as different filesystems")
	}
	res, err := Transfer(src, dst, Hardlink, 0)
	if err != nil || res.Mode != Hardlink || res.Size != 1000 {
		t.Fatal(res, err)
	}
	a, _ := os.Stat(src)
	b, _ := os.Stat(dst)
	if !os.SameFile(a, b) {
		t.Fatal("not a hardlink")
	}
	if _, err := Transfer(src, dst, Hardlink, 0); !errors.Is(err, ErrExists) {
		t.Fatalf("overwrite: %v", err)
	}
}

// otherFS returns a folder on another filesystem than the temp dir, or skips.
func otherFS(t *testing.T) string {
	t.Helper()
	for _, base := range []string{"/dev/shm", os.Getenv("LOCARR_TEST_OTHER_FS")} {
		if base == "" {
			continue
		}
		d, err := os.MkdirTemp(base, "locarr-fsops-")
		if err != nil {
			continue
		}
		t.Cleanup(func() { _ = os.RemoveAll(d) })
		if !SameFilesystem(d, t.TempDir()) {
			return d
		}
	}
	t.Skip("no second filesystem available")
	return ""
}

func TestHardlinkFallsBackToCopyAcrossFilesystems(t *testing.T) {
	other := otherFS(t)
	src, dst := filepath.Join(t.TempDir(), "a.mkv"), filepath.Join(other, "lib", "a.mkv")
	write(t, src, 4096)
	if SameFilesystem(src, dst) {
		t.Fatal("expected different filesystems")
	}
	res, err := Transfer(src, dst, Hardlink, 0)
	if err != nil || res.Mode != Copy {
		t.Fatal(res, err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("copy removed the source")
	}
	if _, err := os.Stat(dst + ".locarr-partial"); !os.IsNotExist(err) {
		t.Fatal("partial file left behind")
	}
	// Move across filesystems: verified copy, then the source goes.
	src2 := filepath.Join(t.TempDir(), "b.mkv")
	write(t, src2, 5000)
	if _, err := Transfer(src2, filepath.Join(other, "lib", "b.mkv"), Move, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src2); !os.IsNotExist(err) {
		t.Fatal("move left the source")
	}
	if err := Verify(filepath.Join(other, "lib", "b.mkv"), 5000); err != nil {
		t.Fatal(err)
	}
}

func TestCopyAndMove(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "a.mkv")
	write(t, src, 10)
	if res, err := Transfer(src, filepath.Join(d, "c", "a.mkv"), Copy, 0); err != nil || res.Mode != Copy {
		t.Fatal(res, err)
	}
	a, _ := os.Stat(src)
	b, _ := os.Stat(filepath.Join(d, "c", "a.mkv"))
	if os.SameFile(a, b) {
		t.Fatal("copy made a link")
	}
	if _, err := Transfer(src, filepath.Join(d, "m", "a.mkv"), Move, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("move kept the source")
	}
	if _, err := Transfer(filepath.Join(d, "missing"), filepath.Join(d, "x"), Copy, 0); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := Transfer(filepath.Join(d, "m", "a.mkv"), filepath.Join(d, "y"), "teleport", 0); !errors.Is(err, ErrBadMode) {
		t.Fatal(err)
	}
	if err := Verify(filepath.Join(d, "m", "a.mkv"), 11); !errors.Is(err, ErrVerify) {
		t.Fatal(err)
	}
}

func TestMoveTreeAcrossFilesystems(t *testing.T) {
	other := otherFS(t)
	src := filepath.Join(t.TempDir(), "Movie (2020)")
	write(t, filepath.Join(src, "a.mkv"), 100)
	write(t, filepath.Join(src, "subs", "a.srt"), 10)
	dst := filepath.Join(other, "bin", "Movie (2020)")
	if err := MoveTree(src, dst); err != nil {
		t.Fatal(err)
	}
	if n, _ := Size(dst); n != 110 {
		t.Fatalf("size %d", n)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source left")
	}
}
