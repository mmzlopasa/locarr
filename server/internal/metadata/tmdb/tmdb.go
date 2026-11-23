// Package tmdb is the TMDb movie provider, written from TMDb's public API
// documentation (v3 endpoints). It accepts a v3 API key (sent as api_key) or
// a v4 read access token (sent as a bearer token).
package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
)

// DefaultBaseURL is TMDb's API host.
const DefaultBaseURL = "https://api.themoviedb.org"

// DefaultImageURL is used until /configuration says otherwise.
const DefaultImageURL = "https://image.tmdb.org/t/p/"

const (
	maxBody      = 4 << 20
	maxImage     = 10 << 20
	timeout      = 15 * time.Second
	providerName = "tmdb"
)

// Client talks to TMDb.
type Client struct {
	BaseURL string
	// Key returns the stored API key ("" = none).
	Key func(ctx context.Context) (string, error)
	// Language and Region return the settings (e.g. "en-US", "US").
	Language func(ctx context.Context) string
	Region   func(ctx context.Context) string
	Cache    *metadata.Cache
	// Health records how requests go (nil = not recorded).
	Health  *metadata.Health
	Limiter *metadata.Limiter
	HTTP    *http.Client
}

var _ metadata.MovieProvider = (*Client)(nil)
var _ metadata.ImageSource = (*Client)(nil)

// New returns a client for baseURL (empty = TMDb).
func New(baseURL string, key func(context.Context) (string, error), cache *metadata.Cache) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	c := &Client{BaseURL: strings.TrimRight(baseURL, "/"), Key: key, Cache: cache,
		// TMDb asks clients to stay well below ~50 requests per second.
		Limiter: metadata.NewLimiter(20, 20)}
	c.HTTP = &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Host != via[0].URL.Host {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	return c
}

func (c *Client) lang(ctx context.Context) string {
	if c.Language != nil {
		if l := c.Language(ctx); l != "" {
			return l
		}
	}
	return "en-US"
}

func (c *Client) region(ctx context.Context) string {
	if c.Region != nil {
		return strings.ToUpper(c.Region(ctx))
	}
	return ""
}

// isBearer tells a v4 read token (a JWT) from a v3 key (32 hex characters).
func isBearer(key string) bool { return strings.Count(key, ".") == 2 && len(key) > 40 }

// get fetches path (with query) and decodes JSON into out, through the cache.
func (c *Client) get(ctx context.Context, path string, q url.Values, ttl time.Duration, out any) error {
	key, err := c.Key(ctx)
	if err != nil {
		return err
	}
	return c.getWith(ctx, key, path, q, ttl, out)
}

func (c *Client) getWith(ctx context.Context, key, path string, q url.Values, ttl time.Duration, out any) error {
	if key == "" {
		return metadata.ErrNoKey
	}
	if q == nil {
		q = url.Values{}
	}
	cacheKey := metadata.CacheKey(providerName, path+"?"+q.Encode())
	if ttl > 0 {
		if body, ok := c.Cache.Get(ctx, cacheKey); ok {
			return json.Unmarshal(body, out)
		}
	}
	body, err := c.fetch(ctx, key, path, q)
	c.Health.Record(providerName, err)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: bad answer from %s: %w", metadata.ErrUnavailable, path, err)
	}
	if ttl > 0 {
		c.Cache.Put(ctx, cacheKey, body, ttl)
	}
	return nil
}

func (c *Client) fetch(ctx context.Context, key, path string, q url.Values) ([]byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if err := c.Limiter.Wait(ctx); err != nil {
			return nil, err
		}
		qq := url.Values{}
		for k, v := range q {
			qq[k] = v
		}
		if !isBearer(key) {
			qq.Set("api_key", key)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path+"?"+qq.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		if isBearer(key) {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		res, err := c.HTTP.Do(req)
		if err != nil {
			// The error text contains the URL with the key; never pass it on.
			return nil, fmt.Errorf("%w: %s", metadata.ErrUnavailable, redact(err.Error(), key))
		}
		body, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
		_ = res.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("%w: %s", metadata.ErrUnavailable, redact(err.Error(), key))
		}
		if len(body) > maxBody {
			return nil, fmt.Errorf("%w: answer too large", metadata.ErrUnavailable)
		}
		switch {
		case res.StatusCode == http.StatusTooManyRequests:
			wait := 2 * time.Second
			if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && s > 0 && s < 60 {
				wait = time.Duration(s) * time.Second
			}
			c.Limiter.Pause(time.Now().Add(wait))
			continue
		case res.StatusCode == http.StatusUnauthorized:
			return nil, metadata.ErrInvalidKey
		case res.StatusCode == http.StatusNotFound:
			return nil, metadata.ErrNotFound
		case res.StatusCode >= 300:
			return nil, fmt.Errorf("%w: HTTP %d", metadata.ErrUnavailable, res.StatusCode)
		}
		return body, nil
	}
	return nil, fmt.Errorf("%w: %w", metadata.ErrUnavailable, metadata.ErrRateLimited)
}

func redact(s, key string) string {
	if key != "" {
		s = strings.ReplaceAll(s, key, "***")
	}
	return s
}

// Validate implements metadata.MovieProvider.
func (c *Client) Validate(ctx context.Context, key string) error {
	var cfg configuration
	return c.getWith(ctx, strings.TrimSpace(key), "/3/configuration", nil, 0, &cfg)
}

type configuration struct {
	Images struct {
		SecureBaseURL string `json:"secure_base_url"`
	} `json:"images"`
}

func year(date string) int {
	if len(date) >= 4 {
		y, _ := strconv.Atoi(date[:4])
		return y
	}
	return 0
}

type movieResult struct {
	ID            int64   `json:"id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	ReleaseDate   string  `json:"release_date"`
	Overview      string  `json:"overview"`
	PosterPath    string  `json:"poster_path"`
	BackdropPath  string  `json:"backdrop_path"`
	Popularity    float64 `json:"popularity"`
	VoteAverage   float64 `json:"vote_average"`
	MediaType     string  `json:"media_type"`
}

func file(p string) string { return strings.TrimPrefix(p, "/") }

func (m movieResult) summary() metadata.MovieSummary {
	return metadata.MovieSummary{TMDbID: m.ID, Title: m.Title, OriginalTitle: m.OriginalTitle, Year: year(m.ReleaseDate),
		ReleaseDate: m.ReleaseDate, Overview: m.Overview, Poster: file(m.PosterPath), Backdrop: file(m.BackdropPath),
		Popularity: m.Popularity, Rating: m.VoteAverage}
}

// SearchMovies implements metadata.MovieProvider.
func (c *Client) SearchMovies(ctx context.Context, query string, y int) ([]metadata.MovieSummary, error) {
	q := url.Values{"query": {query}, "include_adult": {"false"}, "language": {c.lang(ctx)}, "page": {"1"}}
	if y > 0 {
		q.Set("year", strconv.Itoa(y))
	}
	var res struct {
		Results []movieResult `json:"results"`
	}
	if err := c.get(ctx, "/3/search/movie", q, metadata.TTLSearch, &res); err != nil {
		return nil, err
	}
	out := make([]metadata.MovieSummary, 0, len(res.Results))
	for _, r := range res.Results {
		out = append(out, r.summary())
	}
	return out, nil
}

type movieDetails struct {
	movieResult
	IMDbID           string `json:"imdb_id"`
	OriginalLanguage string `json:"original_language"`
	Runtime          int    `json:"runtime"`
	Status           string `json:"status"`
	VoteCount        int    `json:"vote_count"`
	Genres           []struct {
		Name string `json:"name"`
	} `json:"genres"`
	ProductionCompanies []struct {
		Name string `json:"name"`
	} `json:"production_companies"`
	BelongsToCollection *struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"belongs_to_collection"`
	ReleaseDates struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Dates   []struct {
				Certification string `json:"certification"`
				ReleaseDate   string `json:"release_date"`
				Type          int    `json:"type"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
	AlternativeTitles struct {
		Titles []struct {
			Title string `json:"title"`
		} `json:"titles"`
	} `json:"alternative_titles"`
}

// TMDb release types.
const (
	typePremiere   = 1
	typeLimited    = 2
	typeTheatrical = 3
	typeDigital    = 4
	typePhysical   = 5
)

// Movie implements metadata.MovieProvider.
func (c *Client) Movie(ctx context.Context, id int64) (metadata.Movie, error) {
	q := url.Values{"language": {c.lang(ctx)}, "append_to_response": {"release_dates,alternative_titles"}}
	var d movieDetails
	if err := c.get(ctx, "/3/movie/"+strconv.FormatInt(id, 10), q, metadata.TTLMovie, &d); err != nil {
		return metadata.Movie{}, err
	}
	m := metadata.Movie{MovieSummary: d.summary(), IMDbID: d.IMDbID, OriginalLanguage: d.OriginalLanguage, Runtime: d.Runtime,
		Status: d.Status, Votes: d.VoteCount}
	for _, g := range d.Genres {
		m.Genres = append(m.Genres, g.Name)
	}
	if len(d.ProductionCompanies) > 0 {
		m.Studio = d.ProductionCompanies[0].Name
	}
	if d.BelongsToCollection != nil {
		m.CollectionID, m.CollectionName = d.BelongsToCollection.ID, d.BelongsToCollection.Name
	}
	seen := map[string]bool{strings.ToLower(m.Title): true}
	for _, t := range d.AlternativeTitles.Titles {
		if k := strings.ToLower(t.Title); t.Title != "" && !seen[k] {
			seen[k] = true
			m.AlternateTitles = append(m.AlternateTitles, t.Title)
		}
	}
	if m.OriginalTitle != "" && !seen[strings.ToLower(m.OriginalTitle)] {
		m.AlternateTitles = append(m.AlternateTitles, m.OriginalTitle)
	}
	// Dates: the region's when it has them, else the earliest anywhere.
	region := c.region(ctx)
	pick := func(types ...int) string {
		best, bestRegion := "", ""
		for _, r := range d.ReleaseDates.Results {
			for _, rd := range r.Dates {
				for _, t := range types {
					if rd.Type != t || len(rd.ReleaseDate) < 10 {
						continue
					}
					day := rd.ReleaseDate[:10]
					if r.Country == region && (bestRegion == "" || day < bestRegion) {
						bestRegion = day
					}
					if best == "" || day < best {
						best = day
					}
				}
			}
		}
		if bestRegion != "" {
			return bestRegion
		}
		return best
	}
	m.InCinemas = pick(typeLimited, typeTheatrical)
	if m.InCinemas == "" {
		m.InCinemas = pick(typePremiere)
	}
	m.DigitalRelease = pick(typeDigital)
	m.PhysicalRelease = pick(typePhysical)
	if m.InCinemas == "" && m.DigitalRelease == "" && m.PhysicalRelease == "" && len(d.ReleaseDate) >= 10 {
		m.InCinemas = d.ReleaseDate[:10]
	}
	var certs []string
	for _, r := range d.ReleaseDates.Results {
		if r.Country != region {
			continue
		}
		for _, rd := range r.Dates {
			if rd.Certification != "" {
				certs = append(certs, rd.Certification)
			}
		}
	}
	sort.Strings(certs)
	if len(certs) > 0 {
		m.Certification = certs[0]
	}
	return m, nil
}

// FindByIMDb implements metadata.MovieProvider.
func (c *Client) FindByIMDb(ctx context.Context, imdbID string) (int64, error) {
	imdbID = strings.ToLower(strings.TrimSpace(imdbID))
	if !strings.HasPrefix(imdbID, "tt") || len(imdbID) < 4 || len(imdbID) > 12 {
		return 0, metadata.ErrNotFound
	}
	var res struct {
		MovieResults []movieResult `json:"movie_results"`
	}
	if err := c.get(ctx, "/3/find/"+url.PathEscape(imdbID), url.Values{"external_source": {"imdb_id"}}, metadata.TTLMovie, &res); err != nil {
		return 0, err
	}
	if len(res.MovieResults) == 0 {
		return 0, metadata.ErrNotFound
	}
	return res.MovieResults[0].ID, nil
}

// Collection implements metadata.MovieProvider.
func (c *Client) Collection(ctx context.Context, id int64) (metadata.Collection, error) {
	var res struct {
		ID           int64         `json:"id"`
		Name         string        `json:"name"`
		Overview     string        `json:"overview"`
		PosterPath   string        `json:"poster_path"`
		BackdropPath string        `json:"backdrop_path"`
		Parts        []movieResult `json:"parts"`
	}
	if err := c.get(ctx, "/3/collection/"+strconv.FormatInt(id, 10), url.Values{"language": {c.lang(ctx)}}, metadata.TTLCollection, &res); err != nil {
		return metadata.Collection{}, err
	}
	out := metadata.Collection{ID: res.ID, Name: res.Name, Overview: res.Overview, Poster: file(res.PosterPath), Backdrop: file(res.BackdropPath)}
	for _, p := range res.Parts {
		out.Parts = append(out.Parts, p.summary())
	}
	sort.SliceStable(out.Parts, func(i, j int) bool {
		a, b := out.Parts[i].ReleaseDate, out.Parts[j].ReleaseDate
		if a == "" || b == "" {
			return a != "" // undated last
		}
		return a < b
	})
	return out, nil
}

// List implements metadata.MovieProvider (a public v3 list, movies only).
func (c *Client) List(ctx context.Context, id string) ([]metadata.MovieSummary, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, "/?#") {
		return nil, metadata.ErrNotFound
	}
	var out []metadata.MovieSummary
	for page := 1; page <= 20; page++ {
		var res struct {
			Items      []movieResult `json:"items"`
			TotalPages int           `json:"total_pages"`
		}
		q := url.Values{"language": {c.lang(ctx)}, "page": {strconv.Itoa(page)}}
		if err := c.get(ctx, "/3/list/"+url.PathEscape(id), q, metadata.TTLList, &res); err != nil {
			return nil, err
		}
		for _, it := range res.Items {
			if it.MediaType == "" || it.MediaType == "movie" {
				out = append(out, it.summary())
			}
		}
		if page >= res.TotalPages {
			break
		}
	}
	return out, nil
}

// imageBase returns the image host from /configuration.
func (c *Client) imageBase(ctx context.Context) string {
	var cfg configuration
	if err := c.get(ctx, "/3/configuration", nil, metadata.TTLConfig, &cfg); err == nil && strings.HasPrefix(cfg.Images.SecureBaseURL, "http") {
		return strings.TrimRight(cfg.Images.SecureBaseURL, "/") + "/"
	}
	return DefaultImageURL
}

// FetchImage implements metadata.ImageSource.
func (c *Client) FetchImage(ctx context.Context, size, name string) ([]byte, string, error) {
	base := c.imageBase(ctx)
	u, err := url.Parse(base + size + "/" + name)
	if err != nil {
		return nil, "", err
	}
	if err := c.Limiter.Wait(ctx); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", metadata.ErrUnavailable, err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, "", metadata.ErrNotFound
	}
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%w: image HTTP %d", metadata.ErrUnavailable, res.StatusCode)
	}
	ct := res.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		return nil, "", errors.New("the image host did not send an image")
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxImage+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) > maxImage {
		return nil, "", errors.New("image too large")
	}
	return body, ct, nil
}
