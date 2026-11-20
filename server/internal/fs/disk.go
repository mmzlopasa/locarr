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
