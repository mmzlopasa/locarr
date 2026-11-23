// Package subdl is a client for the SubDL API, written from its public
// documentation (subdl.com/api-doc): GET api.subdl.com/api/v1/subtitles with
// the api_key and search parameters (imdb_id, tmdb_id, film_name,
// file_name, type, season_number, episode_number, languages,
// subs_per_page, releases, hi), downloads as ZIP archives from dl.subdl.com
// plus the subtitle's url. Free keys allow 2000 requests a day.
package subdl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
)

// Name is the provider id.
const Name = "subdl"

// Public addresses.
const (
	DefaultURL      = "https://api.subdl.com/api/v1"
	DefaultDownload = "https://dl.subdl.com"
)

// Client talks to SubDL.
type Client struct {
	cfg      providers.Config
	download string
	pacer    *providers.Pacer
}

// New returns a client. With a BaseURL (tests), downloads come from the
// same host under /dl.
func New(cfg providers.Config) *Client {
	dl := DefaultDownload
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultURL
	} else {
		u, err := url.Parse(cfg.BaseURL)
		if err == nil {
			dl = u.Scheme + "://" + u.Host + strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/api/v1") + "/dl"
		}
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg, download: dl, pacer: &providers.Pacer{Interval: 500 * time.Millisecond}}
}

// Name implements providers.Provider.
func (c *Client) Name() string { return Name }

// lang maps catalogue codes to SubDL's language codes.
func lang(code string) string {
	switch code {
	case "pt-br":
		return "BR_PT"
	case "es-419":
		return "ES"
	case "nl-be":
		return "NL"
	}
	return strings.ToUpper(code)
}

type response struct {
	Status  bool   `json:"status"`
	Error   string `json:"error"`
	Results []struct {
		IMDbID string `json:"imdb_id"`
		TMDbID int64  `json:"tmdb_id"`
		Year   int    `json:"year"`
	} `json:"results"`
	Subtitles []struct {
		ReleaseName string   `json:"release_name"`
		Name        string   `json:"name"`
		Lang        string   `json:"lang"`
		Language    string   `json:"language"`
		Author      string   `json:"author"`
		URL         string   `json:"url"`
		Season      *int     `json:"season"`
		Episode     *int     `json:"episode"`
		HI          bool     `json:"hi"`
		FullSeason  bool     `json:"full_season"`
		EpisodeFrom *int     `json:"episode_from"`
		EpisodeEnd  *int     `json:"episode_end"`
		Releases    []string `json:"releases"`
		FPS         string   `json:"fps"`
	} `json:"subtitles"`
}

func (c *Client) get(ctx context.Context, p url.Values) (response, error) {
	var v response
	if c.cfg.APIKey == "" {
		return v, providers.ErrMissingKey
	}
	if err := c.pacer.Wait(ctx); err != nil {
		return v, err
	}
	p.Set("api_key", c.cfg.APIKey)
	res, err := c.cfg.HTTP.Get(ctx, c.cfg.BaseURL+"/subtitles?"+p.Encode(), http.Header{"Accept": {"application/json"}, "User-Agent": {c.cfg.UserAgent}})
	if err != nil {
		return v, err
	}
	switch res.Status {
	case http.StatusTooManyRequests:
		d := providers.RetryAfter(res.Header, 30*time.Second, time.Now())
		c.pacer.Block(d)
		return v, &providers.RateLimitError{RetryAfter: d}
	case http.StatusUnauthorized, http.StatusForbidden:
		return v, fmt.Errorf("%w: HTTP %d", providers.ErrAuth, res.Status)
	}
	if err := json.Unmarshal(res.Body, &v); err != nil {
		if res.Status >= 300 {
			return v, &providers.HTTPError{Status: res.Status, Body: string(res.Body)}
		}
		return v, fmt.Errorf("%w: %w", providers.ErrBadResponse, err)
	}
	if !v.Status {
		msg := strings.ToLower(v.Error)
		switch {
		case strings.Contains(msg, "api key") || strings.Contains(msg, "api_key") || strings.Contains(msg, "unauthor"):
			return v, fmt.Errorf("%w: %s", providers.ErrAuth, v.Error)
		case strings.Contains(msg, "limit") || strings.Contains(msg, "quota"):
			return v, &providers.QuotaError{ResetAt: nextMidnightUTC(time.Now()), Message: v.Error}
		case strings.Contains(msg, "not found") || strings.Contains(msg, "can't find") || strings.Contains(msg, "no subtitle"):
			return response{Status: true}, nil
		}
		return v, &providers.HTTPError{Status: res.Status, Body: v.Error}
	}
	return v, nil
}

func nextMidnightUTC(now time.Time) time.Time {
	y, m, d := now.UTC().Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC)
}

// Search implements providers.Provider (one request per language, so each
// result's language is known for sure).
func (c *Client) Search(ctx context.Context, q providers.Query) ([]providers.Result, error) {
	var out []providers.Result
	for _, l := range q.Languages {
		p := url.Values{}
		p.Set("languages", lang(l))
		p.Set("subs_per_page", "30")
		p.Set("releases", "1")
		if q.Kind == providers.KindEpisode {
			p.Set("type", "tv")
			if q.SeriesIMDbID != "" {
				p.Set("imdb_id", q.SeriesIMDbID)
			} else if q.SeriesTMDbID > 0 {
				p.Set("tmdb_id", strconv.FormatInt(q.SeriesTMDbID, 10))
			} else {
				p.Set("film_name", q.Title)
			}
			p.Set("season_number", strconv.Itoa(q.Season))
			p.Set("episode_number", strconv.Itoa(q.Episode))
		} else {
			p.Set("type", "movie")
			if q.IMDbID != "" {
				p.Set("imdb_id", q.IMDbID)
			} else if q.TMDbID > 0 {
				p.Set("tmdb_id", strconv.FormatInt(q.TMDbID, 10))
			} else {
				p.Set("film_name", q.Title)
				if q.Year > 0 {
					p.Set("year", strconv.Itoa(q.Year))
				}
			}
		}
		v, err := c.get(ctx, p)
		if err != nil {
			return out, err
		}
		imdb, tmdb, year := "", int64(0), 0
		if len(v.Results) > 0 {
			imdb, tmdb, year = v.Results[0].IMDbID, v.Results[0].TMDbID, v.Results[0].Year
		}
		for _, s := range v.Subtitles {
			if s.URL == "" {
				continue
			}
			r := providers.Result{Provider: Name, ID: strings.TrimSuffix(strings.TrimPrefix(s.URL, "/subtitle/"), ".zip"), Language: l,
				FileName: s.Name, Handle: s.URL, HearingImpaired: s.HI, Uploader: s.Author, IMDbID: imdb, TMDbID: tmdb, Year: year,
				SeasonPack: s.FullSeason}
			if s.FPS != "" {
				r.FPS, _ = strconv.ParseFloat(s.FPS, 64)
			}
			if s.Season != nil {
				r.Season = *s.Season
			}
			if s.Episode != nil && !s.FullSeason {
				r.Episode = *s.Episode
			}
			rel := append([]string{}, s.Releases...)
			if s.ReleaseName != "" {
				rel = append([]string{s.ReleaseName}, rel...)
			}
			r.Releases = rel
			low := strings.ToLower(s.ReleaseName + " " + s.Name)
			r.Forced = strings.Contains(low, "forced")
			out = append(out, r)
		}
	}
	return out, nil
}

// Download implements providers.Provider.
func (c *Client) Download(ctx context.Context, r providers.Result, q providers.Query) (providers.File, providers.Status, error) {
	if !strings.HasPrefix(r.Handle, "/subtitle/") || strings.Contains(r.Handle, "..") {
		return providers.File{}, providers.Status{}, providers.ErrNotFound
	}
	if err := c.pacer.Wait(ctx); err != nil {
		return providers.File{}, providers.Status{}, err
	}
	res, err := c.cfg.HTTP.Get(ctx, c.download+r.Handle, http.Header{"User-Agent": {c.cfg.UserAgent}})
	if err != nil {
		return providers.File{}, providers.Status{}, err
	}
	switch {
	case res.Status == http.StatusTooManyRequests:
		d := providers.RetryAfter(res.Header, time.Hour, time.Now())
		// The anonymous download limit is per day; treat a long wait as quota.
		if d > 10*time.Minute {
			return providers.File{}, providers.Status{}, &providers.QuotaError{ResetAt: time.Now().Add(d)}
		}
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
	p := url.Values{}
	p.Set("film_name", "the matrix")
	p.Set("type", "movie")
	p.Set("languages", "EN")
	p.Set("subs_per_page", "1")
	_, err := c.get(ctx, p)
	return providers.Status{}, err
}
