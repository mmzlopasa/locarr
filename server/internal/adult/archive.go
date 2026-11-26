package adult

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nwaples/rardecode/v2"

	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
)

// ImageExtensions are the image files a gallery takes (D-113).
var ImageExtensions = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true, ".avif": true, ".heic": true}

// archiveExtensions are read for images when a download has no loose ones.
var archiveExtensions = map[string]bool{".zip": true, ".rar": true}

// IsImage reports an image file name a gallery takes.
func IsImage(name string) bool { return ImageExtensions[strings.ToLower(filepath.Ext(name))] }

// Limits bound what one gallery import reads, so an archive cannot exhaust
// the disk or memory (zip bombs).
type Limits struct {
	// MaxFiles is the most images one gallery takes.
	MaxFiles int
	// MaxFileBytes is the largest single image.
	MaxFileBytes int64
	// MaxTotalBytes is the most all images of one gallery take together.
	MaxTotalBytes int64
	// MaxRatio is the largest uncompressed : compressed size of a zip entry
	// (images hardly compress; a bomb does).
	MaxRatio int64
}

// DefaultLimits are used by imports.
var DefaultLimits = Limits{MaxFiles: 10000, MaxFileBytes: 200 << 20, MaxTotalBytes: 32 << 30, MaxRatio: 100}

// Archive errors.
var (
	ErrArchiveLimit   = errors.New("the archive is larger than the gallery limits")
	ErrArchiveUnsafe  = errors.New("the archive holds an entry with an unsafe name")
	ErrArchiveUnknown = errors.New("not a zip or rar archive")
)

// archiveEntry is one image inside an archive.
type archiveEntry struct {
	Name string // the cleaned name inside the archive (slash separated)
	Size int64
}

// safeEntryName cleans an archive entry name and refuses absolute names,
// ".." elements, drive letters, backslash tricks and NUL bytes. The result is
// always relative and stays below the extraction folder.
func safeEntryName(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) {
		return "", ErrArchiveUnsafe
	}
	n := strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(n, "/") || (len(n) > 1 && n[1] == ':') {
		return "", ErrArchiveUnsafe
	}
	for _, part := range strings.Split(n, "/") {
		if part == ".." {
			return "", ErrArchiveUnsafe
		}
	}
	c := path.Clean(n)
	if c == "." || strings.HasPrefix(c, "../") || c == ".." {
		return "", ErrArchiveUnsafe
	}
	return c, nil
}

// skipEntry: folders, hidden and system files ("__MACOSX/…", "._x.jpg",
// "Thumbs.db") and everything that is not an image.
func skipEntry(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") || strings.EqualFold(part, "__MACOSX") {
			return true
		}
	}
	return !IsImage(name)
}

// openArchive lists an archive's image entries and returns a function that
// writes one of them (by index in the list) to w, never more than its size
// limit. Symbolic links, devices and other non-regular entries are skipped,
// unsafe names refuse the whole archive.
type archiveReader interface {
	Entries() []archiveEntry
	// Extract streams the entries in order; fn gets each entry and its data.
	Extract(fn func(i int, e archiveEntry, r io.Reader) error) error
	Close() error
}

func openArchive(p string, lim Limits) (archiveReader, error) {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".zip":
		return openZip(p, lim)
	case ".rar":
		return openRar(p, lim)
	}
	return nil, ErrArchiveUnknown
}

type zipArchive struct {
	rc      *zip.ReadCloser
	files   []*zip.File
	entries []archiveEntry
	lim     Limits
}

func openZip(p string, lim Limits) (*zipArchive, error) {
	rc, err := zip.OpenReader(p)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrArchiveUnknown, err)
	}
	z := &zipArchive{rc: rc, lim: lim}
	var total int64
	for _, f := range rc.File {
		name, err := safeEntryName(f.Name)
		if err != nil {
			_ = rc.Close()
			return nil, fmt.Errorf("%w: %q", err, f.Name)
		}
		mode := f.Mode()
		if !mode.IsRegular() || skipEntry(name) {
			continue // folders, symbolic links, devices, non-images
		}
		size := int64(f.UncompressedSize64) // #nosec G115 -- checked against the limits next
		if size < 0 || size > lim.MaxFileBytes {
			_ = rc.Close()
			return nil, fmt.Errorf("%w: %s is %d bytes", ErrArchiveLimit, name, f.UncompressedSize64)
		}
		if comp := int64(f.CompressedSize64); lim.MaxRatio > 0 && size > 1<<20 && (comp <= 0 || size/comp > lim.MaxRatio) { // #nosec G115
			_ = rc.Close()
			return nil, fmt.Errorf("%w: %s compresses %d:1", ErrArchiveLimit, name, size/max(comp, 1))
		}
		total += size
		if total > lim.MaxTotalBytes || len(z.entries) >= lim.MaxFiles {
			_ = rc.Close()
			return nil, fmt.Errorf("%w: more than %d images or %d bytes", ErrArchiveLimit, lim.MaxFiles, lim.MaxTotalBytes)
		}
		z.files = append(z.files, f)
		z.entries = append(z.entries, archiveEntry{Name: name, Size: size})
	}
	return z, nil
}

func (z *zipArchive) Entries() []archiveEntry { return z.entries }
func (z *zipArchive) Close() error            { return z.rc.Close() }

func (z *zipArchive) Extract(fn func(int, archiveEntry, io.Reader) error) error {
	for i, f := range z.files {
		r, err := f.Open()
		if err != nil {
			return err
		}
		// The declared size can lie: never read more than it (+1 to notice).
		err = fn(i, z.entries[i], io.LimitReader(r, z.entries[i].Size+1))
		_ = r.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// rarDictionary bounds the memory one RAR entry may need to decode.
const rarDictionary = 256 << 20

// rarArchive reads RAR (1.5 – 5) with a pure Go decoder. RAR is a stream, so
// the listing is a first pass and extraction a second one.
type rarArchive struct {
	path    string
	entries []archiveEntry
	names   map[string]int
	lim     Limits
}

func openRar(p string, lim Limits) (*rarArchive, error) {
	r, err := rardecode.OpenReader(p, rardecode.SkipCheck, rardecode.MaxDictionarySize(rarDictionary))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrArchiveUnknown, err)
	}
	defer func() { _ = r.Close() }()
	a := &rarArchive{path: p, names: map[string]int{}, lim: lim}
	var total int64
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrArchiveUnknown, err)
		}
		name, err := safeEntryName(h.Name)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", err, h.Name)
		}
		if h.IsDir || h.LinkType != 0 || h.Encrypted || !h.Mode().IsRegular() || skipEntry(name) {
			continue // folders, links, password-protected entries, non-images
		}
		if h.UnKnownSize || h.UnPackedSize < 0 || h.UnPackedSize > lim.MaxFileBytes {
			return nil, fmt.Errorf("%w: %s", ErrArchiveLimit, name)
		}
		total += h.UnPackedSize
		if total > lim.MaxTotalBytes || len(a.entries) >= lim.MaxFiles {
			return nil, fmt.Errorf("%w: more than %d images or %d bytes", ErrArchiveLimit, lim.MaxFiles, lim.MaxTotalBytes)
		}
		if _, dup := a.names[name]; dup {
			continue
		}
		a.names[name] = len(a.entries)
		a.entries = append(a.entries, archiveEntry{Name: name, Size: h.UnPackedSize})
	}
	return a, nil
}

func (a *rarArchive) Entries() []archiveEntry { return a.entries }
func (a *rarArchive) Close() error            { return nil }

func (a *rarArchive) Extract(fn func(int, archiveEntry, io.Reader) error) error {
	r, err := rardecode.OpenReader(a.path, rardecode.MaxDictionarySize(rarDictionary))
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name, err := safeEntryName(h.Name)
		if err != nil {
			return err
		}
		i, ok := a.names[name]
		if !ok || h.IsDir || h.LinkType != 0 || h.Encrypted || !h.Mode().IsRegular() {
			continue
		}
		delete(a.names, name)
		if err := fn(i, a.entries[i], io.LimitReader(r, a.entries[i].Size+1)); err != nil {
			return err
		}
	}
}

// writeEntry writes one archive entry to dst (inside roots, new file only),
// refusing more bytes than declared.
func writeEntry(roots []string, dst string, e archiveEntry, r io.Reader) (int64, error) {
	safe, err := lfs.SafePath(roots, dst)
	if err != nil {
		return 0, err
	}
	f, err := os.OpenFile(safe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) // #nosec G302 G304 -- inside the data roots; media files are shared
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != e.Size {
		err = fmt.Errorf("%w: %s holds %d bytes, not the declared %d", ErrArchiveLimit, e.Name, n, e.Size)
	}
	if err != nil {
		_ = os.Remove(safe)
		return 0, err
	}
	return n, nil
}

// walkImages lists the image files and archives under p (a file or a
// folder) without following symbolic links.
func walkImages(p string, lim Limits) (images, archives []string, err error) {
	st, err := os.Lstat(p)
	if err != nil {
		return nil, nil, err
	}
	if st.Mode().IsRegular() {
		switch ext := strings.ToLower(filepath.Ext(p)); {
		case ImageExtensions[ext]:
			return []string{p}, nil, nil
		case archiveExtensions[ext]:
			return nil, []string{p}, nil
		}
		return nil, nil, nil
	}
	if !st.IsDir() {
		return nil, nil, nil
	}
	err = filepath.WalkDir(p, func(q string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if q != p && (strings.HasPrefix(name, ".") || strings.EqualFold(name, "__MACOSX")) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // folders are walked; links, devices and sockets are not followed
		}
		ext := strings.ToLower(filepath.Ext(name))
		switch {
		case ImageExtensions[ext]:
			if len(images) >= lim.MaxFiles {
				return fmt.Errorf("%w: more than %d images", ErrArchiveLimit, lim.MaxFiles)
			}
			images = append(images, q)
		case ext == ".zip":
			archives = append(archives, q)
		case ext == ".rar":
			// A multi-volume set is read from its first part (name.part1.rar).
			low := strings.ToLower(name)
			if !strings.Contains(low, ".part") || strings.HasSuffix(low, ".part1.rar") || strings.HasSuffix(low, ".part01.rar") ||
				strings.HasSuffix(low, ".part001.rar") {
				archives = append(archives, q)
			}
		}
		return nil
	})
	return images, archives, err
}
