package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// Cache stores provider answers in metadata_cache.
type Cache struct {
	DB  *db.DB
	Now func() time.Time
}

func (c *Cache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Get returns a cached body that has not expired (ok=false otherwise).
func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool) {
	if c == nil || c.DB == nil || IsFresh(ctx) {
		return nil, false
	}
	var body []byte
	err := c.DB.R.QueryRowContext(ctx, `SELECT body FROM metadata_cache WHERE key = ? AND expires_at > ?`, key, db.Millis(c.now())).Scan(&body)
	if err != nil {
		return nil, false
	}
	return body, true
}

// Put stores a body for ttl.
func (c *Cache) Put(ctx context.Context, key string, body []byte, ttl time.Duration) {
	if c == nil || c.DB == nil {
		return
	}
	now := c.now()
	_, _ = c.DB.W.ExecContext(ctx, `INSERT INTO metadata_cache (key, body, fetched_at, expires_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET body = excluded.body, fetched_at = excluded.fetched_at, expires_at = excluded.expires_at`,
		key, body, db.Millis(now), db.Millis(now.Add(ttl)))
}

// Prune removes expired answers.
func (c *Cache) Prune(ctx context.Context) (int64, error) {
	res, err := c.DB.W.ExecContext(ctx, `DELETE FROM metadata_cache WHERE expires_at <= ?`, db.Millis(c.now()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Clear removes every cached answer (after the language changes).
func (c *Cache) Clear(ctx context.Context) error {
	_, err := c.DB.W.ExecContext(ctx, `DELETE FROM metadata_cache`)
	return err
}

// Limiter is a token bucket: rate per second, burst tokens.
type Limiter struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
	// pausedUntil is set from a provider's Retry-After.
	pausedUntil time.Time
}

// NewLimiter returns a limiter.
func NewLimiter(rate float64, burst int) *Limiter {
	return &Limiter{rate: rate, burst: float64(burst), tokens: float64(burst)}
}

// Pause stops requests until t (a 429 with Retry-After).
func (l *Limiter) Pause(t time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if t.After(l.pausedUntil) {
		l.pausedUntil = t
	}
}

// Wait blocks until a request may go out or ctx ends.
func (l *Limiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now()
		if l.last.IsZero() {
			l.last = now
		}
		l.tokens = min(l.burst, l.tokens+now.Sub(l.last).Seconds()*l.rate)
		l.last = now
		var wait time.Duration
		switch {
		case now.Before(l.pausedUntil):
			wait = l.pausedUntil.Sub(now)
		case l.tokens >= 1:
			l.tokens--
			l.mu.Unlock()
			return nil
		default:
			wait = time.Duration((1 - l.tokens) / l.rate * float64(time.Second))
		}
		l.mu.Unlock()
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// ImageCache keeps provider images on disk under Dir.
type ImageCache struct {
	Dir    string
	Source ImageSource
	// Sizes replaces ImageSizes for this cache (nil = ImageSizes).
	Sizes map[string]bool
	mu     sync.Mutex
	// inflight de-duplicates concurrent fetches of one image.
	inflight map[string]*sync.WaitGroup
}

// Allowed image sizes and file names.
var (
	ImageSizes = map[string]bool{"w92": true, "w154": true, "w185": true, "w300": true, "w342": true, "w500": true,
		"w780": true, "w1280": true, "original": true}
	imageFile = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}\.(jpg|jpeg|png|webp)$`)
	// ErrBadImage is returned for a size or file name that is not allowed.
	ErrBadImage = errors.New("invalid image name")
)

// Image is a cached image.
type Image struct {
	Path        string
	ContentType string
	ETag        string
	ModTime     time.Time
}

func contentType(file string) string {
	switch filepath.Ext(file) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	}
	return "image/jpeg"
}

// ETag is stable per size and file: provider image names change with content.
func ETag(size, file string) string {
	sum := sha256.Sum256([]byte(size + "/" + file))
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

// Get returns the image, fetching and storing it on first use.
func (c *ImageCache) Get(ctx context.Context, size, file string) (Image, error) {
	sizes := c.Sizes
	if sizes == nil {
		sizes = ImageSizes
	}
	if !sizes[size] || !imageFile.MatchString(file) {
		return Image{}, ErrBadImage
	}
	p := filepath.Join(c.Dir, size, file)
	img := Image{Path: p, ContentType: contentType(file), ETag: ETag(size, file)}
	if st, err := os.Stat(p); err == nil {
		img.ModTime = st.ModTime()
		return img, nil
	}
	key := size + "/" + file
	c.mu.Lock()
	if c.inflight == nil {
		c.inflight = map[string]*sync.WaitGroup{}
	}
	if wg, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		wg.Wait()
		if st, err := os.Stat(p); err == nil {
			img.ModTime = st.ModTime()
			return img, nil
		}
		return Image{}, ErrNotFound
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	c.inflight[key] = wg
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.inflight, key)
		c.mu.Unlock()
		wg.Done()
	}()
	if c.Source == nil {
		return Image{}, ErrUnavailable
	}
	body, _, err := c.Source.FetchImage(ctx, size, file)
	if err != nil {
		return Image{}, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return Image{}, err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return Image{}, err
	}
	if err := os.Rename(tmp, p); err != nil {
		return Image{}, fmt.Errorf("store image: %w", err)
	}
	img.ModTime = time.Now()
	return img, nil
}

// CacheKey builds a cache key that never contains secrets.
func CacheKey(provider, path string) string { return provider + ":" + path }
