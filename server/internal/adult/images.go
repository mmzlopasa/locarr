package adult

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/adult/meta"
)

var imageKey = regexp.MustCompile(`^[0-9a-f]{32}$`)

// cacheImage downloads a source image once into the private image folder
// and returns its key ("" when there is none or it failed). Images are only
// served through the adult endpoints to permitted users, never by a public
// URL (D-103).
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
	// A unique temporary name: several searches may fetch the same image at once.
	f, err := os.CreateTemp(s.ImageDir, key+".*.tmp")
	if err != nil {
		return ""
	}
	tmp := f.Name()
	_, werr := f.Write(body)
	if cerr := f.Close(); werr != nil || cerr != nil {
		_ = os.Remove(tmp)
		return ""
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
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

// Images that could not be fetched when an entry was added (a host the
// runtime could not verify, a timeout) are fetched again on refresh.

func (s *Service) backfillItemImage(ctx context.Context, sc *meta.Scene) {
	if sc.Image == "" {
		return
	}
	stash, tpdb := ids(sc.Source, sc.ID)
	var n int
	_ = s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM adult_items WHERE image = '' AND ((stashdb_id <> '' AND stashdb_id = ?)
		OR (tpdb_id <> '' AND tpdb_id = ?))`, stash, tpdb).Scan(&n)
	if n == 0 {
		return
	}
	if key := s.cacheImage(ctx, sc.Image); key != "" {
		_, _ = s.DB.W.ExecContext(ctx, `UPDATE adult_items SET image = ? WHERE image = '' AND ((stashdb_id <> '' AND stashdb_id = ?)
			OR (tpdb_id <> '' AND tpdb_id = ?))`, key, stash, tpdb)
	}
	for i := range sc.Performers {
		p := &sc.Performers[i]
		if p.Image == "" {
			continue
		}
		ps, pt := ids(p.Source, p.ID)
		var m int
		_ = s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM adult_performers WHERE image = '' AND ((stashdb_id <> '' AND stashdb_id = ?)
			OR (tpdb_id <> '' AND tpdb_id = ?))`, ps, pt).Scan(&m)
		if m == 0 {
			continue
		}
		if key := s.cacheImage(ctx, p.Image); key != "" {
			_, _ = s.DB.W.ExecContext(ctx, `UPDATE adult_performers SET image = ? WHERE image = '' AND ((stashdb_id <> '' AND stashdb_id = ?)
				OR (tpdb_id <> '' AND tpdb_id = ?))`, key, ps, pt)
		}
	}
}

func (s *Service) backfillStudioImage(ctx context.Context, p meta.Provider, id int64, sourceID string) {
	ms, err := p.Studio(ctx, sourceID)
	if err != nil {
		return
	}
	if key := s.cacheImage(ctx, ms.Image); key != "" {
		_, _ = s.DB.W.ExecContext(ctx, `UPDATE adult_studios SET image = ? WHERE id = ? AND image = ''`, key, id)
	}
}

func (s *Service) backfillPerformerImage(ctx context.Context, p meta.Provider, id int64, sourceID string) {
	mp, err := p.Performer(ctx, sourceID)
	if err != nil {
		return
	}
	if key := s.cacheImage(ctx, mp.Image); key != "" {
		_, _ = s.DB.W.ExecContext(ctx, `UPDATE adult_performers SET image = ? WHERE id = ? AND image = ''`, key, id)
	}
}

// imageKeep is how long a cached image nothing refers to (a search
// thumbnail, a removed scene) is kept.
const imageKeep = 7 * 24 * time.Hour

// PruneImages removes cached images that no studio, performer or item uses
// and that are older than imageKeep. They are cache files Locarr fetched
// itself, never the user's files.
func (s *Service) PruneImages(ctx context.Context) (int, error) {
	if s.ImageDir == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(s.ImageDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	used := map[string]bool{}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT image FROM adult_items WHERE image <> '' UNION SELECT image FROM adult_studios WHERE image <> ''
		UNION SELECT image FROM adult_performers WHERE image <> ''`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			used[k] = true
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	cutoff := s.now().Add(-imageKeep)
	for _, e := range entries {
		if e.IsDir() || !imageKey.MatchString(e.Name()) || used[e.Name()] {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if os.Remove(filepath.Join(s.ImageDir, e.Name())) == nil {
			n++
		}
	}
	return n, nil
}
