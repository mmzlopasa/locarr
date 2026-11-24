// Package meta is the adult metadata interface (D-102): StashDB (GraphQL)
// and ThePornDB (REST), both written from their public API documentation,
// both needing the owner's own API key. No other source is used: no
// scraping of tube or other sites.
package meta

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
)

// Source names.
const (
	StashDB = "stashdb"
	TPDB    = "tpdb"
)

// Errors.
var (
	ErrNoKey    = errors.New("no API key is set for this metadata source")
	ErrBadKey   = errors.New("the metadata source refused the API key")
	ErrNotFound = errors.New("not found at the metadata source")
	// ErrRemoved: the source flags the entry as deleted or disputed; it is
	// never imported (D-102).
	ErrRemoved = errors.New("the metadata source flags this entry as removed")
)

// Studio is a studio or site.
type Studio struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	URL    string `json:"url,omitempty"`
	Image  string `json:"image,omitempty"` // remote image URL (never sent to clients)
	Parent string `json:"parent,omitempty"`
}

// Performer is a performer.
type Performer struct {
	Source         string   `json:"source"`
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Disambiguation string   `json:"disambiguation,omitempty"`
	Aliases        []string `json:"aliases,omitempty"`
	Image          string   `json:"image,omitempty"`
}

// Scene is a scene or (with Movie set) a movie.
type Scene struct {
	Source     string      `json:"source"`
	ID         string      `json:"id"`
	Movie      bool        `json:"movie,omitempty"`
	Title      string      `json:"title"`
	Date       string      `json:"date,omitempty"` // YYYY-MM-DD
	Duration   int         `json:"duration,omitempty"`
	Overview   string      `json:"overview,omitempty"`
	Studio     *Studio     `json:"studio,omitempty"`
	Performers []Performer `json:"performers,omitempty"`
	Tags       []string    `json:"tags,omitempty"`
	PHash      string      `json:"phash,omitempty"`
	Image      string      `json:"image,omitempty"`
	// Removed: the source marks the entry deleted, removed or disputed.
	Removed bool `json:"removed,omitempty"`
}

// Year of the scene's date.
func (s *Scene) Year() int {
	var y int
	if len(s.Date) >= 4 {
		_, _ = fmt.Sscanf(s.Date[:4], "%d", &y)
	}
	return y
}

// Provider is one adult metadata source.
type Provider interface {
	Name() string
	Validate(ctx context.Context, key string) error
	SearchScenes(ctx context.Context, term string) ([]Scene, error)
	SearchMovies(ctx context.Context, term string) ([]Scene, error)
	SearchPerformers(ctx context.Context, term string) ([]Performer, error)
	SearchStudios(ctx context.Context, term string) ([]Studio, error)
	Scene(ctx context.Context, id string) (Scene, error)
	Movie(ctx context.Context, id string) (Scene, error)
	Studio(ctx context.Context, id string) (Studio, error)
	Performer(ctx context.Context, id string) (Performer, error)
	// StudioScenes and PerformerScenes list the newest scenes first (one page).
	StudioScenes(ctx context.Context, id string, page int) ([]Scene, error)
	PerformerScenes(ctx context.Context, id string, page int) ([]Scene, error)
}

// Limits of every request to a source.
const (
	timeout  = 15 * time.Second
	maxBody  = 4 << 20
	maxImage = 10 << 20
)

// HTTP is the shared client: timeout, size limit, no redirects to another
// host, the key never in errors, a rate limit and the metadata cache.
type HTTP struct {
	Base    string
	Key     func(ctx context.Context) (string, error)
	Cache   *metadata.Cache
	Limiter *metadata.Limiter
	Client  *http.Client
}

func newHTTP(base string, key func(context.Context) (string, error), cache *metadata.Cache, rate float64) *HTTP {
	h := &HTTP{Base: strings.TrimRight(base, "/"), Key: key, Cache: cache, Limiter: metadata.NewLimiter(rate, 2)}
	h.Client = &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Host != via[0].URL.Host {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	return h
}

// do sends one request with the auth header set by auth and returns the body.
func (h *HTTP) do(ctx context.Context, method, path string, body []byte, auth func(*http.Request, string), key string) ([]byte, error) {
	if key == "" {
		var err error
		if key, err = h.Key(ctx); err != nil {
			return nil, err
		}
	}
	if key == "" {
		return nil, ErrNoKey
	}
	if h.Limiter != nil {
		if err := h.Limiter.Wait(ctx); err != nil {
			return nil, err
		}
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.Base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Locarr")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	auth(req, key)
	res, err := h.Client.Do(req)
	if err != nil {
		return nil, errors.New(redact(err.Error(), key))
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBody {
		return nil, errors.New("metadata answer too large")
	}
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return nil, ErrBadKey
	case res.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case res.StatusCode == http.StatusTooManyRequests:
		if h.Limiter != nil {
			h.Limiter.Pause(time.Now().Add(10 * time.Second))
		}
		return nil, fmt.Errorf("metadata source rate limit (HTTP 429)")
	case res.StatusCode >= 300:
		return nil, fmt.Errorf("metadata source answered HTTP %d", res.StatusCode)
	}
	return data, nil
}

func (h *HTTP) cached(ctx context.Context, key string, ttl time.Duration, fetch func() ([]byte, error)) ([]byte, error) {
	if h.Cache != nil {
		if b, ok := h.Cache.Get(ctx, key); ok {
			return b, nil
		}
	}
	b, err := fetch()
	if err != nil {
		return nil, err
	}
	if h.Cache != nil {
		h.Cache.Put(ctx, key, b, ttl)
	}
	return b, nil
}

// FetchImage downloads a remote image (same host rules, at most 10 MiB);
// it never sends the API key to image hosts.
func FetchImage(ctx context.Context, raw string) ([]byte, string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, "", errors.New("bad image address")
	}
	c := &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Host != via[0].URL.Host {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("image answered HTTP %d", res.StatusCode)
	}
	ct := res.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		return nil, "", errors.New("not an image")
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxImage+1))
	if err != nil {
		return nil, "", err
	}
	if len(b) > maxImage {
		return nil, "", errors.New("image too large")
	}
	return b, ct, nil
}

func redact(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, "***")
}
