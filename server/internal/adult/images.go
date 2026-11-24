package adult

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"

	"github.com/mmzlopasa/locarr/server/internal/adult/meta"
)

var imageKey = regexp.MustCompile(`^[0-9a-f]{32}$`)

// cacheImage downloads a source image once into the private image folder
// and returns its key ("" when there is none or it failed). Images are only
// served through the adult endpoints to permitted users, never by a public
// URL (D-102).
func (s *Service) cacheImage(ctx context.Context, url string) string {
	if url == "" || s.ImageDir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(url))
	key := hex.EncodeToString(sum[:16])
	path := filepath.Join(s.ImageDir, key)
	if _, err := os.Stat(path); err == nil {
		return key
	}
	body, _, err := meta.FetchImage(ctx, url)
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("adult image", "err", err)
		}
		return ""
	}
	if err := os.MkdirAll(s.ImageDir, 0o700); err != nil {
		return ""
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return ""
	}
	if err := os.Rename(tmp, path); err != nil {
		return ""
	}
	return key
}

// ImagePath returns the cached file of an image key.
func (s *Service) ImagePath(key string) (string, error) {
	if !imageKey.MatchString(key) || s.ImageDir == "" {
		return "", ErrNotFound
	}
	p := filepath.Join(s.ImageDir, key)
	if _, err := os.Stat(p); err != nil {
		return "", ErrNotFound
	}
	return p, nil
}
