// Package fs holds the one function every request path goes through (SafePath)
// and small filesystem helpers.
package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Errors returned by SafePath.
var (
	ErrNotAllowed = errors.New("path is outside the allowed folders")
	ErrNotFound   = errors.New("path does not exist")
)

// SafePath resolves p (which must be absolute) and returns it when it lies
// inside one of roots after cleaning and following symbolic links. Every path
// that arrives in a request must pass through it before it is used on disk.
// A path that does not exist yet is resolved through its closest existing
// parent, so it may name a folder to be created; callers that need an existing
// path check ErrNotFound with MustExist.
func SafePath(roots []string, p string) (string, error) {
	if p == "" || strings.ContainsRune(p, 0) || !filepath.IsAbs(p) {
		return "", ErrNotAllowed
	}
	resolved, err := resolve(filepath.Clean(p))
	if err != nil {
		return "", err
	}
	for _, r := range roots {
		root, err := resolve(filepath.Clean(r))
		if err != nil {
			continue
		}
		if within(root, resolved) {
			return resolved, nil
		}
	}
	return "", ErrNotAllowed
}

// MustExist is SafePath for a path that has to exist.
func MustExist(roots []string, p string) (string, error) {
	s, err := SafePath(roots, p)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(s); err != nil {
		return "", ErrNotFound
	}
	return s, nil
}

func within(root, p string) bool {
	if p == root {
		return true
	}
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// resolve follows symbolic links of the longest existing prefix of p and
// appends the rest (which cannot contain links because it does not exist).
func resolve(p string) (string, error) {
	rest := ""
	cur := p
	for {
		r, err := filepath.EvalSymlinks(cur)
		if err == nil {
			if rest == "" {
				return r, nil
			}
			return filepath.Join(r, rest), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("resolve %s: %w", p, err)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", ErrNotFound
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
