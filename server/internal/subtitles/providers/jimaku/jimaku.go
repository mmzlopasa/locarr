// Package jimaku is a client for the Jimaku API (Japanese subtitles for
// anime), written from its public OpenAPI documentation (jimaku.cc/api/docs):
// an API key in the Authorization header, GET /api/entries/search (by TMDB
// id "tv:N"/"movie:N" or a fuzzy query, anime only), GET
// /api/entries/{id}/files (optionally by episode) and the documented
// x-ratelimit-* headers on 429.
package jimaku

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
)

// Name is the provider id.
const Name = "jimaku"

// DefaultURL is the public API address.
const DefaultURL = "https://jimaku.cc/api"

// Client talks to Jimaku.
type Client struct {
	cfg   providers.Config
	pacer *providers.Pacer
}

// New returns a client.
func New(cfg providers.Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg, pacer: &providers.Pacer{Interval: 300 * time.Millisecond}}
}

// Name implements providers.Provider.
func (c *Client) Name() string { return Name }

func (c *Client) get(ctx context.Context, p string, v any) error {
	if c.cfg.APIKey == "" {
		return providers.ErrMissingKey
	}
	if err := c.pacer.Wait(ctx); err != nil {
		return err
	}
	res, err := c.cfg.HTTP.Get(ctx, c.cfg.BaseURL+p, http.Header{"Authorization": {c.cfg.APIKey}, "Accept": {"application/json"},
		"User-Agent": {c.cfg.UserAgent}})
	if err != nil {
		return err
	}
	switch {
	case res.Status == http.StatusTooManyRequests:
		d := providers.RetryAfter(res.Header, 5*time.Second, time.Now())
		c.pacer.Block(d)
		return &providers.RateLimitError{RetryAfter: d}
	case res.Status == http.StatusUnauthorized || res.Status == http.StatusForbidden:
		return fmt.Errorf("%w: HTTP %d", providers.ErrAuth, res.Status)
	case res.Status == http.StatusNotFound:
		return providers.ErrNotFound
	case res.Status >= 300:
		return &providers.HTTPError{Status: res.Status, Body: string(res.Body)}
	}
	if err := json.Unmarshal(res.Body, v); err != nil {
		return fmt.Errorf("%w: %w", providers.ErrBadResponse, err)
	}
	return nil
}

type entry struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	TMDbID string `json:"tmdb_id"`
	Flags  struct {
		Anime bool `json:"anime"`
		Movie bool `json:"movie"`
	} `json:"flags"`
}

type fileEntry struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Search implements providers.Provider. Only Japanese is offered, and only
// for anime.
func (c *Client) Search(ctx context.Context, q providers.Query) ([]providers.Result, error) {
	wants := false
	for _, l := range q.Languages {
		if l == "ja" {
			wants = true
		}
	}
	if !wants || !q.Anime {
		return nil, nil
	}
	p := url.Values{}
	p.Set("anime", "true")
	switch {
	case q.Kind == providers.KindEpisode && q.SeriesTMDbID > 0:
		p.Set("tmdb_id", "tv:"+strconv.FormatInt(q.SeriesTMDbID, 10))
	case q.Kind == providers.KindMovie && q.TMDbID > 0:
		p.Set("tmdb_id", "movie:"+strconv.FormatInt(q.TMDbID, 10))
	default:
		p.Set("query", q.Title)
	}
	var entries []entry
	if err := c.get(ctx, "/entries/search?"+p.Encode(), &entries); err != nil {
		return nil, err
	}
	if len(entries) == 0 && p.Get("tmdb_id") != "" {
		p.Del("tmdb_id")
		p.Set("query", q.Title)
		if err := c.get(ctx, "/entries/search?"+p.Encode(), &entries); err != nil {
			return nil, err
		}
	}
	if len(entries) == 0 {
		return nil, nil
	}
	e := entries[0]
	fp := fmt.Sprintf("/entries/%d/files", e.ID)
	ep := q.Episode
	if q.Absolute > 0 {
		ep = q.Absolute
	}
	if q.Kind == providers.KindEpisode && ep > 0 {
		fp += "?episode=" + strconv.Itoa(ep)
	}
	var files []fileEntry
	if err := c.get(ctx, fp, &files); err != nil {
		return nil, err
	}
	var out []providers.Result
	for _, f := range files {
		ext := strings.ToLower(path.Ext(f.Name))
		if !providers.SubtitleExts[ext] && ext != ".zip" {
			continue // RAR/7z archives are not unpacked
		}
		r := providers.Result{Provider: Name, ID: fmt.Sprintf("%d/%s", e.ID, f.Name), Language: "ja", FileName: f.Name,
			Handle: f.URL, Releases: []string{strings.TrimSuffix(f.Name, path.Ext(f.Name))}, SeasonPack: ext == ".zip"}
		if q.Kind == providers.KindEpisode {
			rel := parser.Parse(f.Name)
			switch {
			case len(rel.Episodes) > 0:
				r.Episode = rel.Episodes[0]
				if len(rel.Seasons) > 0 {
					r.Season = rel.Seasons[0]
				}
			case len(rel.AbsoluteEpisodes) > 0:
				r.Episode = rel.AbsoluteEpisodes[0]
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// Download implements providers.Provider.
func (c *Client) Download(ctx context.Context, r providers.Result, q providers.Query) (providers.File, providers.Status, error) {
	u, err := url.Parse(r.Handle)
	if err != nil {
		return providers.File{}, providers.Status{}, providers.ErrNotFound
	}
	base, _ := url.Parse(c.cfg.BaseURL)
	if base == nil || u.Host != base.Host && u.Hostname() != c.cfg.AllowHost {
		return providers.File{}, providers.Status{}, fmt.Errorf("%w: download link on an unexpected host", providers.ErrBadResponse)
	}
	if err := c.pacer.Wait(ctx); err != nil {
		return providers.File{}, providers.Status{}, err
	}
	res, err := c.cfg.HTTP.Get(ctx, r.Handle, http.Header{"Authorization": {c.cfg.APIKey}, "User-Agent": {c.cfg.UserAgent}})
	if err != nil {
		return providers.File{}, providers.Status{}, err
	}
	switch {
	case res.Status == http.StatusTooManyRequests:
		d := providers.RetryAfter(res.Header, 5*time.Second, time.Now())
		c.pacer.Block(d)
		return providers.File{}, providers.Status{}, &providers.RateLimitError{RetryAfter: d}
	case res.Status == http.StatusNotFound:
		return providers.File{}, providers.Status{}, providers.ErrNotFound
	case res.Status >= 300:
		return providers.File{}, providers.Status{}, &providers.HTTPError{Status: res.Status, Body: string(res.Body)}
	}
	f, err := providers.Unpack(r.FileName, res.Body, q)
	return f, providers.Status{}, err
}

// Test implements providers.Provider with a small search.
func (c *Client) Test(ctx context.Context) (providers.Status, error) {
	var entries []entry
	err := c.get(ctx, "/entries/search?anime=true&query=frieren", &entries)
	return providers.Status{}, err
}
