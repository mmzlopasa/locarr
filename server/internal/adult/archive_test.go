package adult

import (
	"archive/zip"
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// pngBytes is a small generated picture (no real content).
func pngBytes(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{R: shade, G: uint8(x * 6), B: uint8(y * 8), A: 255}) // #nosec G115 -- small test values
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type zipFile struct {
	name string
	data []byte
	mode os.FileMode
	// store: no compression
	store bool
}

func writeZip(t *testing.T, path string, files []zipFile) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for _, zf := range files {
		h := &zip.FileHeader{Name: zf.name, Method: zip.Deflate}
		if zf.store {
			h.Method = zip.Store
		}
		if zf.mode != 0 {
			h.SetMode(zf.mode)
		}
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(zf.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func entryNames(r archiveReader) []string {
	var out []string
	for _, e := range r.Entries() {
		out = append(out, e.Name)
	}
	return out
}

func TestSafeEntryName(t *testing.T) {
	for name, ok := range map[string]bool{
		"a.jpg": true, "set/a.jpg": true, "./set/a.jpg": true,
		"../a.jpg": false, "set/../../a.jpg": false, "/etc/a.jpg": false, `..\a.jpg`: false, `C:\a.jpg`: false,
		"set/\x00a.jpg": false, "": false, "set/..": false,
	} {
		if _, err := safeEntryName(name); (err == nil) != ok {
			t.Errorf("%q: err %v, want ok %v", name, err, ok)
		}
	}
}

// TestZipOnlyImages: folders, non-image files, hidden and system entries
// and symbolic links are left out; images keep their (cleaned) names.
func TestZipOnlyImages(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "set.zip")
	writeZip(t, p, []zipFile{
		{name: "set/", mode: os.ModeDir | 0o755},
		{name: "set/0002.png", data: pngBytes(t, 2)},
		{name: "set/0001.png", data: pngBytes(t, 1)},
		{name: "set/readme.txt", data: []byte("hello")},
		{name: "set/run.exe", data: []byte("MZ")},
		{name: "__MACOSX/set/._0001.png", data: []byte("x")},
		{name: "set/.hidden.png", data: pngBytes(t, 3)},
		{name: "set/link.png", data: []byte("/etc/passwd"), mode: os.ModeSymlink | 0o777},
	})
	r, err := openArchive(p, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if got := entryNames(r); !slices.Equal(got, []string{"set/0002.png", "set/0001.png"}) {
		t.Fatalf("entries %v", got)
	}
	out := t.TempDir()
	n := 0
	err = r.Extract(func(_ int, e archiveEntry, rd io.Reader) error {
		_, err := writeEntry([]string{out}, filepath.Join(out, filepath.Base(e.Name)), e, rd)
		n++
		return err
	})
	if err != nil || n != 2 {
		t.Fatalf("extract: %d %v", n, err)
	}
	if _, err := os.Lstat(filepath.Join(out, "link.png")); err == nil {
		t.Fatal("a symbolic link was extracted")
	}
}

// TestZipTraversal: one unsafe name refuses the whole archive.
func TestZipTraversal(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"../evil.png", "set/../../evil.png", "/abs/evil.png", `..\evil.png`} {
		p := filepath.Join(dir, "bad.zip")
		writeZip(t, p, []zipFile{{name: "ok.png", data: pngBytes(t, 1)}, {name: bad, data: pngBytes(t, 2)}})
		if _, err := openArchive(p, DefaultLimits); !errors.Is(err, ErrArchiveUnsafe) {
			t.Errorf("%q: err %v", bad, err)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.png")); err == nil {
		t.Fatal("written outside")
	}
}

// TestZipBomb: an entry that compresses far better than any picture, one
// larger than the per-file limit, too many entries or too many bytes in
// total refuse the archive before anything is written.
func TestZipBomb(t *testing.T) {
	dir := t.TempDir()
	zeros := make([]byte, 8<<20)
	p := filepath.Join(dir, "bomb.zip")
	writeZip(t, p, []zipFile{{name: "big.png", data: zeros}})
	if _, err := openArchive(p, DefaultLimits); !errors.Is(err, ErrArchiveLimit) {
		t.Fatalf("ratio: %v", err)
	}
	lim := Limits{MaxFiles: 2, MaxFileBytes: 1 << 20, MaxTotalBytes: 3 << 20, MaxRatio: 100}
	writeZip(t, p, []zipFile{{name: "big.png", data: make([]byte, 2<<20), store: true}})
	if _, err := openArchive(p, lim); !errors.Is(err, ErrArchiveLimit) {
		t.Fatalf("file size: %v", err)
	}
	img := pngBytes(t, 1)
	writeZip(t, p, []zipFile{{name: "1.png", data: img}, {name: "2.png", data: img}, {name: "3.png", data: img}})
	if _, err := openArchive(p, lim); !errors.Is(err, ErrArchiveLimit) {
		t.Fatalf("count: %v", err)
	}
	lim.MaxFiles, lim.MaxTotalBytes = 10, int64(len(img))*2
	if _, err := openArchive(p, lim); !errors.Is(err, ErrArchiveLimit) {
		t.Fatalf("total: %v", err)
	}
}

// TestWriteEntryRefusesMore: an entry that holds more than it declares is
// cut off and refused, and nothing is left behind.
func TestWriteEntryRefusesMore(t *testing.T) {
	out := t.TempDir()
	dst := filepath.Join(out, "a.png")
	if _, err := writeEntry([]string{out}, dst, archiveEntry{Name: "a.png", Size: 4}, bytes.NewReader([]byte("123456"))); !errors.Is(err, ErrArchiveLimit) {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("partial file kept")
	}
	if _, err := writeEntry([]string{out}, filepath.Join(filepath.Dir(out), "x.png"), archiveEntry{Size: 1}, bytes.NewReader([]byte("1"))); err == nil {
		t.Fatal("wrote outside the roots")
	}
}

// TestNotAnArchive: a .rar or .zip that is not one is refused cleanly.
func TestNotAnArchive(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"x.rar", "x.zip"} {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("not an archive"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := openArchive(p, DefaultLimits); !errors.Is(err, ErrArchiveUnknown) {
			t.Errorf("%s: %v", n, err)
		}
	}
}

// TestWalkImagesNoLinks: a folder's images are found, links are not
// followed, hidden folders are skipped.
func TestWalkImagesNoLinks(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "set", ".thumbs"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "set", "a.jpg"), []byte("x"), 0o600))
	must(os.WriteFile(filepath.Join(dir, "set", ".thumbs", "b.jpg"), []byte("x"), 0o600))
	must(os.WriteFile(filepath.Join(dir, "info.nfo"), []byte("x"), 0o600))
	must(os.WriteFile(filepath.Join(outside, "secret.jpg"), []byte("x"), 0o600))
	must(os.Symlink(filepath.Join(outside, "secret.jpg"), filepath.Join(dir, "set", "link.jpg")))
	must(os.Symlink(outside, filepath.Join(dir, "linkdir")))
	must(os.WriteFile(filepath.Join(dir, "pack.zip"), []byte("x"), 0o600))
	imgs, archives, err := walkImages(dir, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 1 || filepath.Base(imgs[0]) != "a.jpg" || len(archives) != 1 {
		t.Fatalf("images %v archives %v", imgs, archives)
	}
}

func TestGalleryNames(t *testing.T) {
	if got := galleryName(7, 120, "IMG_0007.JPEG", true); got != "0007.jpg" {
		t.Fatal(got)
	}
	if got := galleryName(7, 12000, "x.png", true); got != "00007.png" {
		t.Fatal(got)
	}
	if got := galleryName(1, 1, "set/My Pic.png", false); got != "My Pic.png" {
		t.Fatal(got)
	}
	list := []string{"10.jpg", "2.jpg", "1.jpg", "b/1.jpg", "a/10.jpg", "a/9.jpg"}
	slices.SortFunc(list, func(a, b string) int {
		if naturalLess(a, b) {
			return -1
		}
		if naturalLess(b, a) {
			return 1
		}
		return 0
	})
	if !slices.Equal(list, []string{"1.jpg", "2.jpg", "10.jpg", "a/9.jpg", "a/10.jpg", "b/1.jpg"}) {
		t.Fatal(list)
	}
	if !isCoverName("set/Cover.JPG") || isCoverName("covers.jpg") {
		t.Fatal("cover names")
	}
}

func TestScaleDown(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	out := scaleDown(src, ThumbSize)
	if b := out.Bounds(); b.Dx() != ThumbSize || b.Dy() != ThumbSize/2 {
		t.Fatal(b)
	}
}
