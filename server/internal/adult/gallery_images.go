package adult

import (
	"context"
	"errors"
	"image"
	"image/color"
	_ "image/gif" // GIF decoder for thumbnails
	"image/jpeg"
	_ "image/png" // PNG decoder for thumbnails
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
)

// GalleryImage is one image of a gallery's folder.
type GalleryImage struct {
	Index int    `json:"index"` // 1-based, in name order
	Name  string `json:"name"`
	Size  int64  `json:"size"`
}

// GalleryImages lists the images in a gallery's folder (the cover file is
// left out), in name order.
func (s *Service) GalleryImages(ctx context.Context, id int64) (Item, []GalleryImage, error) {
	it, err := s.Item(ctx, id)
	if err != nil {
		return it, nil, err
	}
	if it.Kind != KindGallery {
		return it, nil, ErrNotFound
	}
	if it.File == nil {
		return it, []GalleryImage{}, nil
	}
	folder, err := lfs.MustExist(s.DataRoots, it.File.Path)
	if err != nil {
		return it, []GalleryImage{}, nil
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return it, nil, err
	}
	out := []GalleryImage{}
	for _, e := range entries {
		if !e.Type().IsRegular() || !IsImage(e.Name()) || strings.EqualFold(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), CoverName) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, GalleryImage{Name: e.Name(), Size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return naturalLess(out[i].Name, out[j].Name) })
	for i := range out {
		out[i].Index = i + 1
	}
	return it, out, nil
}

// GalleryImagePath returns the file of image index (1-based) of a gallery,
// checked to lie inside the data roots and to be a regular file.
func (s *Service) GalleryImagePath(ctx context.Context, id int64, index int) (string, error) {
	it, list, err := s.GalleryImages(ctx, id)
	if err != nil {
		return "", err
	}
	if index < 1 || index > len(list) || it.File == nil {
		return "", ErrNotFound
	}
	p, err := lfs.MustExist(s.DataRoots, filepath.Join(it.File.Path, list[index-1].Name))
	if err != nil {
		return "", ErrNotFound
	}
	st, err := os.Lstat(p)
	if err != nil || !st.Mode().IsRegular() {
		return "", ErrNotFound
	}
	return p, nil
}

// ThumbSize is the long side of gallery thumbnails.
const ThumbSize = 360

// GalleryThumbPath returns a cached JPEG thumbnail of image index (made on
// first use in the private image folder). Formats the standard library
// cannot decode (WebP, AVIF, HEIC) are served as they are.
func (s *Service) GalleryThumbPath(ctx context.Context, id int64, index int) (string, error) {
	src, err := s.GalleryImagePath(ctx, id, index)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(filepath.Ext(src)) {
	case ".jpg", ".jpeg", ".png", ".gif":
	default:
		return src, nil
	}
	if s.ImageDir == "" {
		return src, nil
	}
	st, err := os.Stat(src)
	if err != nil {
		return "", ErrNotFound
	}
	dir := filepath.Join(s.ImageDir, "gallery-thumbs", strconv.FormatInt(id, 10))
	name := strconv.Itoa(index) + "-" + strconv.FormatInt(st.Size(), 36) + "-" + strconv.FormatInt(st.ModTime().Unix(), 36) + ".jpg"
	thumb := filepath.Join(dir, name)
	if _, err := os.Stat(thumb); err == nil {
		return thumb, nil
	}
	if err := makeThumb(src, thumb); err != nil {
		return src, nil //nolint:nilerr // a picture that cannot be scaled is shown as it is
	}
	return thumb, nil
}

// maxThumbPixels refuses to decode pictures larger than this (memory).
const maxThumbPixels = 80_000_000

func makeThumb(src, dst string) error {
	f, err := os.Open(src) // #nosec G304 -- a gallery file checked by GalleryImagePath
	if err != nil {
		return err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxThumbPixels {
		return errors.New("too large to scale")
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	out := scaleDown(img, ThumbSize)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp := dst + ".tmp" + randomToken()
	w, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- private cache
	if err != nil {
		return err
	}
	err = jpeg.Encode(w, out, &jpeg.Options{Quality: 82})
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// scaleDown averages boxes of source pixels into an image whose long side
// is at most size (pictures already smaller are copied).
func scaleDown(src image.Image, size int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := w, h
	if w >= h && w > size {
		nw, nh = size, max(1, h*size/w)
	} else if h > w && h > size {
		nw, nh = max(1, w*size/h), size
	}
	out := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0, y1 := b.Min.Y+y*h/nh, b.Min.Y+max((y+1)*h/nh, y*h/nh+1)
		for x := 0; x < nw; x++ {
			x0, x1 := b.Min.X+x*w/nw, b.Min.X+max((x+1)*w/nw, x*w/nw+1)
			var r, g, bl, a, n uint64
			// Sample at most 4x4 points per box: fast and smooth enough.
			sy := max(1, (y1-y0)/4)
			sx := max(1, (x1-x0)/4)
			for yy := y0; yy < y1; yy += sy {
				for xx := x0; xx < x1; xx += sx {
					cr, cg, cb, ca := src.At(xx, yy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			out.SetRGBA(x, y, color.RGBA{R: uint8(r / n >> 8), G: uint8(g / n >> 8), B: uint8(bl / n >> 8), A: uint8(a / n >> 8)}) // #nosec G115 -- 16-bit averages
		}
	}
	return out
}
