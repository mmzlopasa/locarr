package logging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// Rotation defaults: 10 MiB per file, the current one plus five older ones.
const (
	DefaultMaxBytes = 10 << 20
	DefaultKeep     = 5
	// FileName is the current log file inside the log folder.
	FileName = "locarr.log"
)

// FileNamePattern matches the current and the rotated log files.
var FileNamePattern = regexp.MustCompile(`^locarr(\.[0-9]{1,3})?\.log$`)

// RotatingFile writes to dir/locarr.log and, when it would grow past MaxBytes,
// renames it to locarr.1.log (shifting older ones up, dropping those beyond
// Keep) and starts a new one. Write errors never reach the logger: a full or
// read-only disk must not stop the server from logging to stdout.
type RotatingFile struct {
	Dir      string
	MaxBytes int64
	Keep     int

	mu   sync.Mutex
	f    *os.File
	size int64
}

// OpenRotating creates dir (0700) and opens the current log file.
func OpenRotating(dir string, maxBytes int64, keep int) (*RotatingFile, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if keep <= 0 {
		keep = DefaultKeep
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	r := &RotatingFile{Dir: dir, MaxBytes: maxBytes, Keep: keep}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *RotatingFile) open() error {
	f, err := os.OpenFile(filepath.Join(r.Dir, FileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) // #nosec G304 -- fixed name in the config folder
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	r.f, r.size = f, st.Size()
	return nil
}

func (r *RotatingFile) rotated(n int) string {
	return filepath.Join(r.Dir, fmt.Sprintf("locarr.%d.log", n))
}

func (r *RotatingFile) rotate() error {
	if r.f != nil {
		_ = r.f.Close()
		r.f = nil
	}
	_ = os.Remove(r.rotated(r.Keep))
	for i := r.Keep - 1; i >= 1; i-- {
		if err := os.Rename(r.rotated(i), r.rotated(i+1)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Rename(filepath.Join(r.Dir, FileName), r.rotated(1)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return r.open()
}

// Write appends p (one log record) and rotates first when needed.
func (r *RotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		if err := r.open(); err != nil {
			return len(p), nil
		}
	}
	if r.size > 0 && r.size+int64(len(p)) > r.MaxBytes {
		if err := r.rotate(); err != nil || r.f == nil {
			return len(p), nil
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	_ = err
	return len(p), nil
}

// Close closes the current file.
func (r *RotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

// File is a log file in the log folder.
type File struct {
	Name       string
	Size       int64
	ModifiedAt time.Time
}

// Files lists the log files in dir, newest first.
func Files(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []File{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []File{}
	for _, e := range entries {
		if e.IsDir() || !FileNamePattern.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, File{Name: e.Name(), Size: info.Size(), ModifiedAt: info.ModTime().UTC()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModifiedAt.After(out[j].ModifiedAt) })
	return out, nil
}

// OpenFile opens one log file by name (only names Files returns).
func OpenFile(dir, name string) (*os.File, error) {
	if !FileNamePattern.MatchString(name) {
		return nil, os.ErrNotExist
	}
	return os.Open(filepath.Join(dir, filepath.Base(name))) // #nosec G304 -- name checked against the pattern
}
