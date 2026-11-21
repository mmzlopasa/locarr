package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library/fsops"
)

// Permissions applied after a transfer (zero = leave as is).
type Permissions struct {
	File   os.FileMode
	Folder os.FileMode
}

// ParseMode reads an octal mode such as "0644" (0 when invalid).
func ParseMode(s string) os.FileMode {
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil || v > 0o777 {
		return 0
	}
	return os.FileMode(v)
}

// Placement puts one main file (and its sidecars) into the library.
type Placement struct {
	Roots []string // data roots; every path must lie inside them
	// Source is the main file; Sidecars travel with it.
	Source   string
	Sidecars []string
	// Folder is the destination folder; Name the file name without extension.
	Folder string
	Name   string
	Mode   string // hardlink, copy, move
	Perms  Permissions
	// Replace, when set, is called with each existing destination path that
	// must make room (the old file of an upgrade); it must move it away
	// (recycle bin) and return nil, or return an error to stop.
	Replace func(path string) error
}

// Placed is a file now in the library.
type Placed struct {
	Path string
	Size int64
	Mode string // what really happened (hardlink may become copy)
	// Source is where it came from.
	Source string
}

// Place transfers the files and verifies them. The source is never removed
// unless Mode is move, and then only after the destination is verified.
func Place(p Placement) (Placed, []Placed, error) {
	src, err := lfs.MustExist(p.Roots, p.Source)
	if err != nil {
		return Placed{}, nil, fmt.Errorf("source %s: %w", p.Source, err)
	}
	folder, err := lfs.SafePath(p.Roots, p.Folder)
	if err != nil {
		return Placed{}, nil, fmt.Errorf("destination %s: %w", p.Folder, err)
	}
	dirMode := p.Perms.Folder
	if err := os.MkdirAll(folder, max(dirMode, 0o755)); err != nil {
		return Placed{}, nil, err
	}
	if dirMode != 0 {
		_ = os.Chmod(folder, dirMode)
	}
	dst := filepath.Join(folder, p.Name+lowerExt(src))
	main, err := place(p, src, dst)
	if err != nil {
		return Placed{}, nil, err
	}
	var sides []Placed
	for _, s := range p.Sidecars {
		ss, err := lfs.MustExist(p.Roots, s)
		if err != nil {
			continue
		}
		sd := filepath.Join(folder, p.Name+SidecarSuffix(src, ss))
		placed, err := place(p, ss, sd)
		if err != nil {
			// A sidecar that cannot be placed does not undo the main file.
			continue
		}
		sides = append(sides, placed)
	}
	return main, sides, nil
}

func lowerExt(p string) string {
	ext := filepath.Ext(p)
	b := []byte(ext)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func place(p Placement, src, dst string) (Placed, error) {
	if same, _ := sameFile(src, dst); same {
		// Already in place (re-import of a library file).
		st, err := os.Stat(dst)
		if err != nil {
			return Placed{}, err
		}
		return Placed{Path: dst, Size: st.Size(), Mode: "existing", Source: src}, nil
	}
	target := dst
	_, err := os.Lstat(dst)
	taken := err == nil
	if taken {
		if p.Replace == nil {
			return Placed{}, fmt.Errorf("%w: %s", fsops.ErrExists, dst)
		}
		// Bring the new file in next to the old one first, so the library is
		// never without a file if the transfer fails.
		target = dst + ".locarr-new"
		_ = os.Remove(target)
	}
	res, err := fsops.Transfer(src, target, p.Mode, p.Perms.Folder)
	if err != nil {
		return Placed{}, err
	}
	if taken {
		if err := p.Replace(dst); err != nil {
			if res.Mode != fsops.Move {
				_ = os.Remove(target)
			}
			return Placed{}, fmt.Errorf("make room for the new file: %w", err)
		}
		if err := os.Rename(target, dst); err != nil {
			return Placed{}, err
		}
	}
	if p.Perms.File != 0 {
		_ = os.Chmod(dst, p.Perms.File)
	}
	return Placed{Path: dst, Size: res.Size, Mode: res.Mode, Source: src}, nil
}

func sameFile(a, b string) (bool, error) {
	sa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(sa, sb), nil
}
