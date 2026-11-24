// Package lastfm reads similar artists and tags from the Last.fm API
// (artist.getSimilar, artist.getTopTags), written from its public
// documentation. Every request carries the operator's API key; requests go
// through a limiter well under Last.fm's published limit, and its error codes
// (10/26 bad key, 6 unknown artist, 29 rate limit) are mapped to the metadata
// errors. Last.fm's images are not used: its terms exclude artwork.
package lastfm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/version"
)

// DefaultBaseURL is the API root.
const DefaultBaseURL = "https://ws.audioscrobbler.com/2.0/"

const (
	maxBody = 4 << 20
	timeout = 20 * time.Second
)

// Client talks to Last.fm.
type Client struct {
	BaseURL string
	// Key returns the API key ("" = not configured).
	Key     func(ctx context.Context) string
	Limiter *metadata.Limiter
	HTTP    *http.Client
	// RateLimitPause is how long to wait after error 29 (tests shorten it).
	RateLimitPause time.Duration
}

// New returns a client ("" = the public API).
func New(baseURL string, key func(ctx context.Context) string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{BaseURL: baseURL, Key: key, Limiter: metadata.NewLimiter(3, 3), RateLimitPause: 30 * time.Second,
		HTTP: &http.Client{Timeout: timeout, CheckRedirect: metadata.SameHost}}
}

// Artist is a similar artist.
type Artist struct {
	Name string
	MBID string
	URL  string
	// Match is Last.fm's similarity, 0 (not similar) to 1.
	Match float64
}

// Tag is an artist tag with its weight (0–100).
type Tag struct {
	Name  string
	Count int
}

// number reads a JSON number that Last.fm sometimes sends as a string.
type number float64

func (n *number) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*n = number(f)
	return nil
}

// list reads a JSON array that is a lone object when it has one element.
type list[T any] []T

func (l *list[T]) UnmarshalJSON(b []byte) error {
	b = []byte(strings.TrimSpace(string(b)))
	if len(b) > 0 && b[0] == '{' {
		var one T
		if err := json.Unmarshal(b, &one); err != nil {
			return err
		}
		*l = list[T]{one}
		return nil
	}
	var many []T
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*l = many
	return nil
}

type apiError struct {
	Code    int    `json:"error"`
	Message string `json:"message"`
}

func mapError(e apiError) error {
	switch e.Code {
	case 4, 10, 26:
		return fmt.Errorf("%w: %s", metadata.ErrInvalidKey, e.Message)
	case 6, 7:
		return fmt.Errorf("%w: %s", metadata.ErrNotFound, e.Message)
	case 29:
		return metadata.ErrRateLimited
	}
	return fmt.Errorf("%w: Last.fm error %d: %s", metadata.ErrUnavailable, e.Code, e.Message)
}

// call runs one method with key and returns the body.
func (c *Client) call(ctx context.Context, key string, q url.Values) ([]byte, error) {
	if key == "" {
		return nil, metadata.ErrNoKey
	}
	q.Set("api_key", key)
	q.Set("format", "json")
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if err := c.Limiter.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Locarr/"+version.Version)
		res, err := c.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", metadata.ErrUnavailable, err)
		}
		body, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
		_ = res.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", metadata.ErrUnavailable, err)
		}
		if len(body) > maxBody {
			return nil, fmt.Errorf("%w: answer too large", metadata.ErrUnavailable)
		}
		var e apiError
		if json.Unmarshal(body, &e) == nil && e.Code != 0 {
			last = mapError(e)
			if errors.Is(last, metadata.ErrRateLimited) {
				c.Limiter.Pause(time.Now().Add(c.RateLimitPause))
				continue
			}
			if e.Code == 11 || e.Code == 16 { // offline, temporary: try again
				continue
			}
			return nil, last
		}
		switch {
		case res.StatusCode == http.StatusTooManyRequests:
			c.Limiter.Pause(time.Now().Add(c.RateLimitPause))
			last = metadata.ErrRateLimited
			continue
		case res.StatusCode >= 300:
			return nil, fmt.Errorf("%w: HTTP %d", metadata.ErrUnavailable, res.StatusCode)
		}
		return body, nil
	}
	if errors.Is(last, metadata.ErrRateLimited) {
		return nil, fmt.Errorf("%w: %w", metadata.ErrUnavailable, metadata.ErrRateLimited)
	}
	return nil, last
}

// byArtist asks by MusicBrainz id first and by name when Last.fm does not know the id.
func (c *Client) byArtist(ctx context.Context, method, mbid, name string, extra url.Values) ([]byte, error) {
	key := ""
	if c.Key != nil {
		key = strings.TrimSpace(c.Key(ctx))
	}
	q := func(by, v string) url.Values {
		out := url.Values{"method": {method}, by: {v}, "autocorrect": {"1"}}
		for k, vv := range extra {
			out[k] = vv
		}
		return out
	}
	if mbid != "" {
		body, err := c.call(ctx, key, q("mbid", mbid))
		if err == nil || !errors.Is(err, metadata.ErrNotFound) || name == "" {
			return body, err
		}
	}
	if name == "" {
		return nil, metadata.ErrNotFound
	}
	return c.call(ctx, key, q("artist", name))
}

// Similar returns up to limit artists similar to the artist (MusicBrainz id
// and/or name), most similar first.
func (c *Client) Similar(ctx context.Context, mbid, name string, limit int) ([]Artist, error) {
	body, err := c.byArtist(ctx, "artist.getSimilar", mbid, name, url.Values{"limit": {strconv.Itoa(limit)}})
	if err != nil {
		return nil, err
	}
	var res struct {
		Similar struct {
			Artist list[struct {
				Name  string `json:"name"`
				MBID  string `json:"mbid"`
				Match number `json:"match"`
				URL   string `json:"url"`
			}] `json:"artist"`
		} `json:"similarartists"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("%w: bad answer: %w", metadata.ErrUnavailable, err)
	}
	out := make([]Artist, 0, len(res.Similar.Artist))
	for _, a := range res.Similar.Artist {
		if strings.TrimSpace(a.Name) == "" {
			continue
		}
		out = append(out, Artist{Name: a.Name, MBID: strings.ToLower(strings.TrimSpace(a.MBID)), URL: a.URL,
			Match: min(1, max(0, float64(a.Match)))})
	}
	return out, nil
}

// TopTags returns the artist's tags, most used first.
func (c *Client) TopTags(ctx context.Context, mbid, name string) ([]Tag, error) {
	body, err := c.byArtist(ctx, "artist.getTopTags", mbid, name, nil)
	if err != nil {
		return nil, err
	}
	var res struct {
		TopTags struct {
			Tag list[struct {
				Name  string `json:"name"`
				Count number `json:"count"`
			}] `json:"tag"`
		} `json:"toptags"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("%w: bad answer: %w", metadata.ErrUnavailable, err)
	}
	out := make([]Tag, 0, len(res.TopTags.Tag))
	for _, t := range res.TopTags.Tag {
		if n := strings.TrimSpace(t.Name); n != "" {
			out = append(out, Tag{Name: strings.ToLower(n), Count: int(t.Count)})
		}
	}
	return out, nil
}

// Validate checks a key without storing it.
func (c *Client) Validate(ctx context.Context, key string) error {
	_, err := c.call(ctx, strings.TrimSpace(key), url.Values{"method": {"artist.getTopTags"}, "artist": {"Radiohead"}})
	if errors.Is(err, metadata.ErrNotFound) {
		return nil
	}
	return err
}

// ArtistURL is the artist's Last.fm page by name (the catalogue page Last.fm asks to link to).
func ArtistURL(name string) string {
	return "https://www.last.fm/music/" + strings.ReplaceAll(url.PathEscape(name), "%20", "+")
}
