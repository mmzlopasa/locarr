package adult

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/decision"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/fsops"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// CoverName is the cover file a gallery folder may hold next to its numbered
// images (cover.jpg, cover.png, …); it is a copy of one of them.
const CoverName = "cover"

// GalleryImport is the result of placing a gallery.
type GalleryImport struct {
	Folder string
	Images int
	Size   int64
	Mode   string
}

func nonEmptyDir(p string) bool {
	entries, err := os.ReadDir(p)
	return err == nil && len(entries) > 0
}

// naturalLess orders names with numbers by value ("2.jpg" before "10.jpg").
func naturalLess(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for a != "" && b != "" {
		ca, cb := rune(a[0]), rune(b[0])
		if unicode.IsDigit(ca) && unicode.IsDigit(cb) {
			na, ra := leadingNumber(a)
			nb, rb := leadingNumber(b)
			if na != nb {
				return na < nb
			}
			a, b = ra, rb
			continue
		}
		if ca != cb {
			return ca < cb
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func leadingNumber(s string) (uint64, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	n, _ := strconv.ParseUint(strings.TrimLeft(s[:i], "0")+"0", 10, 64)
	return n / 10, s[i:]
}

// isCoverName: the set's own cover ("cover.jpg", "folder.jpg", "poster.png").
func isCoverName(name string) bool {
	base := strings.ToLower(strings.TrimSuffix(path.Base(name), path.Ext(name)))
	return base == "cover" || base == "folder" || base == "poster"
}

// galleryName is the file name of image number i (1-based) of n.
func galleryName(i, n int, original string, rename bool) string {
	ext := strings.ToLower(filepath.Ext(original))
	if ext == ".jpeg" {
		ext = ".jpg"
	}
	if !rename {
		base := naming.Sanitize(strings.TrimSuffix(path.Base(original), filepath.Ext(original)), naming.Options{})
		if base == "" || strings.EqualFold(base, CoverName) {
			base = "image"
		}
		return base + ext
	}
	width := max(4, len(strconv.Itoa(n)))
	return fmt.Sprintf("%0*d%s", width, i, ext)
}

func randomToken() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// galleryImage is one image to place: a file on disk or an archive entry.
type galleryImage struct {
	name    string // for ordering and the original name
	file    string // on disk
	archive int    // index into archives (file == "")
	entry   int
	size    int64
}

// ImportGallery places an image set for a gallery (D-113): the images of a
// folder (or a single image), or when it has none the images inside its zip
// and rar archives. Archives are read with the safe-name, link, size and
// count rules of archive.go and stay where they are (seeding). Files go to
// "<adult root>/<gallery folder>/0001.jpg …" (linked when possible, else copied), a
// cover.<ext> is added, and an older set of the gallery goes to the recycle
// bin.
func (s *Service) ImportGallery(ctx context.Context, it *Item, src, release string, dl *int64) (GalleryImport, error) {
	return s.importGallery(ctx, it, src, release, dl, DefaultLimits)
}

func (s *Service) importGallery(ctx context.Context, it *Item, src, release string, dl *int64, lim Limits) (GalleryImport, error) {
	var out GalleryImport
	if it.Kind != KindGallery {
		return out, refuse("not_gallery", "the item is not a gallery")
	}
	src, err := lfs.MustExist(s.DataRoots, src)
	if err != nil {
		return out, err
	}
	names := make([]string, 0, len(it.Performers))
	for _, p := range it.Performers {
		names = append(names, p.Name)
	}
	if term, _, ok := s.BlockedMatch(ctx, names, release, filepath.Base(src)); ok {
		return out, refuse(decision.CodeBlockedTag, "not imported: the name contains the blocked term %q", term)
	}
	files, archives, err := walkImages(src, lim)
	if err != nil {
		return out, refuse("gallery_limit", "not imported: %v", err)
	}
	var imgs []galleryImage
	var readers []archiveReader
	defer func() {
		for _, r := range readers {
			_ = r.Close()
		}
	}()
	if len(files) > 0 {
		for _, f := range files {
			rel, _ := filepath.Rel(src, f)
			if rel == "." {
				rel = filepath.Base(f)
			}
			st, err := os.Stat(f)
			if err != nil {
				return out, err
			}
			imgs = append(imgs, galleryImage{name: filepath.ToSlash(rel), file: f, size: st.Size()})
		}
	} else {
		sort.Slice(archives, func(i, j int) bool { return naturalLess(archives[i], archives[j]) })
		for _, a := range archives {
			r, err := openArchive(a, lim)
			if err != nil {
				return out, refuse("archive_refused", "not imported: %s: %v", filepath.Base(a), err)
			}
			readers = append(readers, r)
			for j, e := range r.Entries() {
				imgs = append(imgs, galleryImage{name: filepath.Base(a) + "/" + e.Name, archive: len(readers) - 1, entry: j, size: e.Size})
			}
		}
	}
	var total int64
	for _, im := range imgs {
		total += im.size
	}
	if len(imgs) > lim.MaxFiles || total > lim.MaxTotalBytes {
		return out, refuse("gallery_limit", "not imported: %d images, %d bytes is more than a gallery takes", len(imgs), total)
	}
	// The set's own cover is kept apart from the numbered images.
	coverAt := -1
	sort.SliceStable(imgs, func(i, j int) bool { return naturalLess(imgs[i].name, imgs[j].name) })
	for i, im := range imgs {
		if isCoverName(im.name) {
			coverAt = i
			break
		}
	}
	numbered := len(imgs)
	if coverAt >= 0 {
		numbered--
	}
	if numbered <= 0 {
		return out, refuse("no_images", "no images in %s", src)
	}
	for i, im := range imgs {
		if i != coverAt {
			if term, _, ok := s.BlockedMatch(ctx, names, im.name); ok {
				return out, refuse(decision.CodeBlockedTag, "not imported: an image name contains the blocked term %q", term)
			}
		}
	}

	rename, _ := settings.Get(ctx, s.Settings, GalleryRename)
	withCover, _ := settings.Get(ctx, s.Settings, GalleryCover)
	mode, _ := settings.Get(ctx, s.Settings, library.TransferMode)
	folder, err := lfs.SafePath(s.DataRoots, it.Path)
	if err != nil {
		return out, err
	}
	staging, err := lfs.SafePath(s.DataRoots, folder+".locarr-partial-"+randomToken())
	if err != nil {
		return out, err
	}
	if err := os.MkdirAll(staging, 0o775); err != nil { // #nosec G301 -- media folders are shared
		return out, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(staging) // only what this import wrote
		}
	}()

	// Destination names: numbered images in order, the cover apart.
	dst := make([]string, len(imgs))
	used := map[string]bool{}
	n := 0
	for i, im := range imgs {
		if i == coverAt {
			continue
		}
		n++
		name := galleryName(n, numbered, im.name, rename)
		for k := 2; used[strings.ToLower(name)]; k++ {
			ext := filepath.Ext(name)
			name = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(galleryName(n, numbered, im.name, rename), ext), k, ext)
		}
		used[strings.ToLower(name)] = true
		dst[i] = filepath.Join(staging, name)
	}
	if coverAt >= 0 && withCover {
		dst[coverAt] = filepath.Join(staging, CoverName+galleryExt(imgs[coverAt].name))
	}

	placedMode := ""
	for i, im := range imgs {
		if im.file == "" || dst[i] == "" {
			continue
		}
		res, err := fsops.Transfer(im.file, dst[i], mode, 0o775)
		if err != nil {
			return out, err
		}
		out.Size += res.Size
		if placedMode == "" || res.Mode == fsops.Copy {
			placedMode = res.Mode
		}
	}
	// Archive entries, streamed archive by archive.
	for ai, r := range readers {
		byEntry := map[int]int{}
		for i, im := range imgs {
			if im.file == "" && im.archive == ai {
				byEntry[im.entry] = i
			}
		}
		err := r.Extract(func(j int, e archiveEntry, rd io.Reader) error {
			i, ok := byEntry[j]
			if !ok || dst[i] == "" {
				return nil
			}
			written, err := writeEntry(s.DataRoots, dst[i], e, rd)
			out.Size += written
			return err
		})
		if err != nil {
			return out, refuse("archive_refused", "not imported: %v", err)
		}
		placedMode = "extracted"
	}
	if withCover && (coverAt < 0 || dst[coverAt] == "") {
		// No cover of its own: the first image.
		for i := range imgs {
			if i != coverAt && dst[i] != "" {
				if err := linkOrCopy(dst[i], filepath.Join(staging, CoverName+galleryExt(imgs[i].name))); err != nil {
					return out, err
				}
				break
			}
		}
	}

	// An older set of this gallery goes to the recycle bin; a folder of the
	// same name with other content is never touched (a new name is used).
	ref := it.Ref().String()
	if it.File != nil {
		if _, err := os.Stat(it.File.Path); err == nil && s.Recycle != nil {
			if _, err := s.Recycle.Recycle(ctx, it.File.Path, ref, "replaced by a new set"); err != nil {
				return out, err
			}
		}
	}
	if nonEmptyDir(folder) {
		root := filepath.Dir(folder)
		if folder, err = s.uniqueGalleryFolder(ctx, root, filepath.Base(it.Path)); err != nil {
			return out, err
		}
	} else {
		_ = os.Remove(folder) // an empty leftover
	}
	if err := os.Rename(staging, folder); err != nil {
		return out, err
	}
	ok = true
	out.Folder, out.Images, out.Mode = folder, numbered, placedMode

	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE adult_items SET path = ?, updated_at = ? WHERE id = ?`, folder, db.Millis(s.now()), it.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO adult_files (item_id, path, size, quality, score, release_group, scene_name, download_id, added_at, image_count)
			VALUES (?, ?, ?, ?, 0, '', ?, ?, ?, ?) ON CONFLICT (item_id) DO UPDATE SET path = excluded.path, size = excluded.size,
			quality = excluded.quality, scene_name = excluded.scene_name, download_id = excluded.download_id, added_at = excluded.added_at,
			image_count = excluded.image_count`, it.ID, folder, out.Size, GalleryQuality, release, dl, db.Millis(s.now()), numbered)
		return err
	})
	if err != nil {
		return out, err
	}
	s.setGalleryCover(ctx, it.ID, s.cacheLocalCover(folder, it.ID))
	s.record(ctx, ref, timeline.TypeFileImported, fmt.Sprintf("imported %d images into %s (%s)", numbered, filepath.Base(folder), placedMode),
		map[string]any{"source": src, "path": folder, "mode": placedMode, "images": numbered, "size": out.Size}, dl)
	action := "imported"
	if it.File != nil {
		action = "upgraded"
	}
	s.afterImport(ctx, it, action, folder)
	return out, nil
}

func galleryExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".jpeg" {
		return ".jpg"
	}
	return ext
}

func linkOrCopy(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	_, err := fsops.Transfer(src, dst, fsops.Copy, 0o775)
	return err
}

// cacheLocalCover copies a gallery's cover into the private image cache, so
// cards show it like any other adult image (D-103). The key changes with
// every import.
func (s *Service) cacheLocalCover(folder string, id int64) string {
	if s.ImageDir == "" {
		return ""
	}
	cover := ""
	if entries, err := os.ReadDir(folder); err == nil {
		for _, e := range entries {
			if e.Type().IsRegular() && strings.EqualFold(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), CoverName) {
				cover = filepath.Join(folder, e.Name())
				break
			}
		}
		if cover == "" {
			for _, e := range entries {
				if e.Type().IsRegular() && IsImage(e.Name()) {
					cover = filepath.Join(folder, e.Name())
					break
				}
			}
		}
	}
	if cover == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("gallery:%d:%s:%d", id, folder, s.now().UnixNano())))
	key := hex.EncodeToString(sum[:16])
	if err := os.MkdirAll(s.ImageDir, 0o700); err != nil {
		return ""
	}
	in, err := os.Open(cover) // #nosec G304 -- the gallery folder, inside the data roots
	if err != nil {
		return ""
	}
	defer in.Close()
	tmp := filepath.Join(s.ImageDir, key+".tmp")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- private cache
	if err != nil {
		return ""
	}
	_, werr := io.Copy(f, io.LimitReader(in, 64<<20))
	if cerr := f.Close(); werr != nil || cerr != nil {
		_ = os.Remove(tmp)
		return ""
	}
	if err := os.Rename(tmp, filepath.Join(s.ImageDir, key)); err != nil {
		_ = os.Remove(tmp)
		return ""
	}
	return key
}
