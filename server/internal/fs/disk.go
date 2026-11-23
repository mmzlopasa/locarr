package fs

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Space is the free and total space of the filesystem holding a path.
type Space struct{ Free, Total int64 }

// DiskSpace returns the space available to unprivileged users at path.
func DiskSpace(path string) (Space, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return Space{}, err
	}
	bs := int64(st.Bsize)                                                        // #nosec G115 -- block size is small
	return Space{Free: int64(st.Bavail) * bs, Total: int64(st.Blocks) * bs}, nil // #nosec G115
}

// WriteProblem says why a folder cannot be written.
type WriteProblem struct {
	// Reason is permission_denied, read_only_filesystem, not_found or other.
	Reason string
	Detail string
}

// CheckWritable creates and removes a temporary file in dir; it returns nil
// when that works, else why not.
func CheckWritable(dir string) *WriteProblem {
	f, err := os.CreateTemp(dir, ".locarr-write-test-*")
	if err != nil {
		p := &WriteProblem{Reason: "other", Detail: err.Error()}
		switch {
		case errors.Is(err, unix.EROFS):
			p.Reason = "read_only_filesystem"
		case errors.Is(err, os.ErrPermission):
			p.Reason = "permission_denied"
		case errors.Is(err, os.ErrNotExist):
			p.Reason = "not_found"
		}
		return p
	}
	name := f.Name()
	_ = f.Close()
	if err := os.Remove(filepath.Clean(name)); err != nil {
		return &WriteProblem{Reason: "other", Detail: err.Error()}
	}
	return nil
}

// Writable reports whether the process can create files in dir (it creates and
// removes a temporary file, which is the only reliable check with ACLs, read-only
// mounts and user namespaces).
func Writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".locarr-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(filepath.Clean(name)) == nil
}

// IsDir reports whether path is an existing directory that can be listed.
func IsDir(path string) bool {
	st, err := os.Stat(path)
	if err != nil || !st.IsDir() {
		return false
	}
	f, err := os.Open(path) // #nosec G304 -- callers pass SafePath results
	if err != nil {
		return false
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	return err == nil || errors.Is(err, io.EOF)
}
