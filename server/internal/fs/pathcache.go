package fs

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// PathCache is SafePath for many files in the same folders (importing a
// whole library): each folder is resolved once, and a file is resolved in
// full only when it is itself a symbolic link. The result is the same as
// SafePath's.
type PathCache struct {
	Roots []string

	mu   sync.Mutex
	dirs map[string]cachedDir
}

type cachedDir struct {
	path string
	err  error
}

// SafePath resolves p like SafePath(c.Roots, p).
func (c *PathCache) SafePath(p string) (string, error) {
	if p == "" || strings.ContainsRune(p, 0) || !filepath.IsAbs(p) {
		return "", ErrNotAllowed
	}
	p = filepath.Clean(p)
	dir, base := filepath.Split(p)
	if base == "" || base == "." || base == ".." {
		return SafePath(c.Roots, p)
	}
	dir = filepath.Clean(dir)
	c.mu.Lock()
	if c.dirs == nil {
		c.dirs = map[string]cachedDir{}
	}
	r, ok := c.dirs[dir]
	c.mu.Unlock()
	if !ok {
		s, err := SafePath(c.Roots, dir)
		r = cachedDir{s, err}
		c.mu.Lock()
		c.dirs[dir] = r
		c.mu.Unlock()
	}
	if r.err != nil {
		// The folder itself is outside, but p may be a root.
		return SafePath(c.Roots, p)
	}
	full := filepath.Join(r.path, base)
	if st, err := os.Lstat(full); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return SafePath(c.Roots, p)
	}
	return full, nil
}
