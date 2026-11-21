// Package fsops moves media files safely: hardlinks when source and
// destination share a filesystem, verified copies otherwise, and moves that
// never remove the source before the destination is verified.
package fsops

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Modes.
const (
	Hardlink = "hardlink"
	Copy     = "copy"
	Move     = "move"
)

// Errors.
var (
	ErrExists   = errors.New("destination already exists")
	ErrVerify   = errors.New("destination does not match the source after the transfer")
	ErrBadMode  = errors.New("unknown transfer mode")
	ErrNotFound = errors.New("source does not exist")
)

// Result says what was done.
type Result struct {
	// Mode is what actually happened: hardlink, copy or move (a hardlink
	// request across filesystems becomes a copy).
	Mode string
	Size int64
}

// SameFilesystem reports whether a and b (b may not exist yet: its closest
// existing parent is used) are on the same device.
func SameFilesystem(a, b string) bool {
	da, ok1 := device(a)
	db, ok2 := device(existingParent(b))
	return ok1 && ok2 && da == db
}

func existingParent(p string) string {
	for {
		if _, err := os.Lstat(p); err == nil {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		p = parent
	}
}

func device(p string) (uint64, bool) {
	st, err := os.Stat(p)
	if err != nil {
		return 0, false
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(sys.Dev), true // #nosec G115 -- device ids are non-negative
}

// Transfer puts the file src at dst (which must not exist) with mode and
// verifies the size before it reports success. The source is removed only for
// Move, and only after the destination is verified.
func Transfer(src, dst, mode string, dirMode os.FileMode) (Result, error) {
	st, err := os.Stat(src)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %s", ErrNotFound, src)
	}
	if st.IsDir() {
		return Result{}, fmt.Errorf("transfer %s: is a folder", src)
	}
	if _, err := os.Lstat(dst); err == nil {
		return Result{}, fmt.Errorf("%w: %s", ErrExists, dst)
	}
	if dirMode == 0 {
		dirMode = 0o755
	}
	if err := os.MkdirAll(filepath.Dir(dst), dirMode); err != nil {
		return Result{}, err
	}
	res := Result{Mode: mode, Size: st.Size()}
	switch mode {
	case Hardlink:
		if err := os.Link(src, dst); err != nil {
			// Another filesystem (EXDEV), or links not supported: copy instead.
			if err := copyFile(src, dst, st); err != nil {
				return Result{}, err
			}
			res.Mode = Copy
		}
	case Copy:
		if err := copyFile(src, dst, st); err != nil {
			return Result{}, err
		}
	case Move:
		if err := os.Rename(src, dst); err != nil {
			if err := copyFile(src, dst, st); err != nil {
				return Result{}, err
			}
			if err := Verify(dst, st.Size()); err != nil {
				_ = os.Remove(dst)
				return Result{}, err
			}
			if err := os.Remove(src); err != nil {
				return res, fmt.Errorf("copied, but could not remove the source: %w", err)
			}
			return res, nil
		}
	default:
		return Result{}, fmt.Errorf("%w: %q", ErrBadMode, mode)
	}
	if err := Verify(dst, st.Size()); err != nil {
		if res.Mode != Move {
			_ = os.Remove(dst)
		}
		return Result{}, err
	}
	return res, nil
}

// Verify checks that p exists, is a regular file and has size bytes.
func Verify(p string, size int64) error {
	st, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrVerify, err)
	}
	if !st.Mode().IsRegular() || st.Size() != size {
		return fmt.Errorf("%w: %s is %d bytes, expected %d", ErrVerify, p, st.Size(), size)
	}
	return nil
}

// copyFile copies into a temporary file next to dst, syncs it and renames it
// into place, so a half-written file never carries the final name.
func copyFile(src, dst string, st fs.FileInfo) error {
	in, err := os.Open(src) // #nosec G304 -- paths are checked by the caller (fs.SafePath)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".locarr-partial"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, st.Mode().Perm()) // #nosec G304
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := Verify(tmp, st.Size()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chtimes(tmp, st.ModTime(), st.ModTime())
	return os.Rename(tmp, dst)
}

// MoveTree moves a file or folder to dst (rename, or verified copy and then
// removal of the source when they are on different filesystems).
func MoveTree(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	st, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNotFound, src)
	}
	if !st.IsDir() {
		if _, err := Transfer(src, dst, Move, 0); err != nil {
			return err
		}
		return nil
	}
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil // symbolic links and devices are not media
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(p, target, info)
	})
	if err != nil {
		return err
	}
	srcSize, err := Size(src)
	if err != nil {
		return err
	}
	dstSize, err := Size(dst)
	if err != nil || dstSize != srcSize {
		return fmt.Errorf("%w: %s", ErrVerify, dst)
	}
	return os.RemoveAll(src)
}

// Size is the total size of the regular files under p.
func Size(p string) (int64, error) {
	var total int64
	err := filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}
