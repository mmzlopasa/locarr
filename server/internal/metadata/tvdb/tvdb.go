// Package tvdb is the optional TVDB v4 series provider, written from TVDB's
// public v4 API documentation: a project API key (and, for user-supported
// keys, a subscriber PIN) is exchanged at /v4/login for a bearer token, then
// /v4/search, /v4/series/{id}/extended, /v4/series/{id}/episodes/default and
// /v4/search/remoteid/{id} answer lookups. TVDB supplies absolute episode
// numbers and the network's air time, which TMDb does not.
package tvdb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
)

// DefaultBaseURL is TVDB's API host.
const DefaultBaseURL = "https://api4.thetvdb.com"

const (
	maxBody      = 8 << 20
	timeout      = 15 * time.Second
	providerName = "tvdb"
	maxPages     = 50
)

// Credentials are the project key and the optional subscriber PIN.
type Credentials struct {
	Key string
	PIN string
}

// Client talks to TVDB.
type Client struct {
	BaseURL string
	// Creds returns the stored key and PIN (empty key = none).
	Creds    func(ctx context.Context) (Credentials, error)
	Language func(ctx context.Context) string // e.g. "en-US"
	Cache    *metadata.Cache
	// Health records how requests go (nil = not recorded).
	Health  *metadata.Health
	Limiter *metadata.Limiter
	HTTP    *http.Client

	mu       sync.Mutex
	token    string
	tokenFor string
}

var _ metadata.SeriesProvider = (*Client)(nil)

// New returns a client for baseURL (empty = TVDB).
func New(baseURL string, creds func(context.Context) (Credentials, error), cache *metadata.Cache) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	c := &Client{BaseURL: strings.TrimRight(baseURL, "/"), Creds: creds, Cache: cache, Limiter: metadata.NewLimiter(10, 10)}
	c.HTTP = &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Host != via[0].URL.Host {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	return c
}

// Name implements metadata.SeriesProvider.
func (c *Client) Name() string { return providerName }

// lang3 turns "en-US" into TVDB's three-letter "eng" for the common ones.
func (c *Client) lang3(ctx context.Context) string {
	l := "en"
	if c.Language != nil {
		if v := c.Language(ctx); v != "" {
			l = strings.ToLower(strings.SplitN(v, "-", 2)[0])
		}
	}
	m := map[string]string{"en": "eng", "hr": "hrv", "de": "deu", "fr": "fra", "es": "spa", "it": "ita", "ja": "jpn", "sr": "srp",
		"bs": "bos", "sl": "slv", "nl": "nld", "pt": "por", "pl": "pol", "ru": "rus", "ko": "kor", "zh": "zho"}
	if v, ok := m[l]; ok {
		return v
	}
	return "eng"
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, token string) ([]byte, int, error) {
	if err := c.Limiter.Wait(ctx); err != nil {
		return nil, 0, err
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %s", metadata.ErrUnavailable, err.Error())
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %s", metadata.ErrUnavailable, err.Error())
	}
	if len(b) > maxBody {
		return nil, 0, fmt.Errorf("%w: answer too large", metadata.ErrUnavailable)
	}
	if res.StatusCode == http.StatusTooManyRequests {
		c.Limiter.Pause(time.Now().Add(2 * time.Second))
	}
	return b, res.StatusCode, nil
}

// login exchanges the key for a token (cached per key until refused).
func (c *Client) login(ctx context.Context, force bool) (string, error) {
	cr, err := c.Creds(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cr.Key) == "" {
		return "", metadata.ErrNoKey
	}
	c.mu.Lock()
	if !force && c.token != "" && c.tokenFor == cr.Key+"\x00"+cr.PIN {
		t := c.token
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()
	t, err := c.Validate(ctx, cr)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.token, c.tokenFor = t, cr.Key+"\x00"+cr.PIN
	c.mu.Unlock()
	return t, nil
}

// Validate logs in with creds and returns the token (used to check a key before storing it).
func (c *Client) Validate(ctx context.Context, cr Credentials) (string, error) {
	body := map[string]string{"apikey": strings.TrimSpace(cr.Key)}
	if cr.PIN != "" {
		body["pin"] = strings.TrimSpace(cr.PIN)
	}
	raw, _ := json.Marshal(body)
	b, status, err := c.do(ctx, http.MethodPost, "/v4/login", raw, "")
	if err != nil {
		return "", err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return "", metadata.ErrInvalidKey
	}
	if status >= 300 {
		return "", fmt.Errorf("%w: login HTTP %d", metadata.ErrUnavailable, status)
	}
	var res struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &res) != nil || res.Data.Token == "" {
		return "", fmt.Errorf("%w: bad login answer", metadata.ErrUnavailable)
	}
	return res.Data.Token, nil
}

// get fetches path through the cache, logging in (again) when needed.
func (c *Client) get(ctx context.Context, path string, ttl time.Duration, out any) (err error) {
	key := metadata.CacheKey(providerName, path)
	if body, ok := c.Cache.Get(ctx, key); ok {
		return json.Unmarshal(body, out)
	}
	defer func() { c.Health.Record(providerName, err) }()
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := c.login(ctx, attempt > 0)
		if err != nil {
			return err
		}
		b, status, err := c.do(ctx, http.MethodGet, path, nil, tok)
		if err != nil {
			return err
		}
		switch {
		case status == http.StatusUnauthorized && attempt == 0:
			continue
		case status == http.StatusUnauthorized:
			return metadata.ErrInvalidKey
		case status == http.StatusNotFound:
			return metadata.ErrNotFound
		case status >= 300:
			return fmt.Errorf("%w: HTTP %d", metadata.ErrUnavailable, status)
		}
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("%w: bad answer from %s: %w", metadata.ErrUnavailable, path, err)
		}
		c.Cache.Put(ctx, key, b, ttl)
		return nil
	}
	return metadata.ErrInvalidKey
}

func atoi64(s string) int64 { n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64); return n }

func year(date string) int {
	if len(date) >= 4 {
		y, _ := strconv.Atoi(date[:4])
		return y
	}
	return 0
}

// SearchSeries implements metadata.SeriesProvider.
func (c *Client) SearchSeries(ctx context.Context, query string, y int) ([]metadata.SeriesSummary, error) {
	q := url.Values{"query": {query}, "type": {"series"}}
	if y > 0 {
		q.Set("year", strconv.Itoa(y))
	}
	var res struct {
		Data []struct {
			TVDBID       string `json:"tvdb_id"`
			Name         string `json:"name"`
			Year         string `json:"year"`
			Overview     string `json:"overview"`
			FirstAirTime string `json:"first_air_time"`
			Network      string `json:"network"`
			Status       string `json:"status"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/v4/search?"+q.Encode(), metadata.TTLSearch, &res); err != nil {
		return nil, err
	}
	out := make([]metadata.SeriesSummary, 0, len(res.Data))
	for _, r := range res.Data {
		yr, _ := strconv.Atoi(r.Year)
		if yr == 0 {
			yr = year(r.FirstAirTime)
		}
		out = append(out, metadata.SeriesSummary{TVDbID: atoi64(r.TVDBID), Title: r.Name, Year: yr, FirstAired: r.FirstAirTime,
			Overview: r.Overview, Network: r.Network, Status: metadata.NormaliseSeriesStatus(r.Status)})
	}
	return out, nil
}

type extended struct {
	Data struct {
		ID               int64  `json:"id"`
		Name             string `json:"name"`
		Overview         string `json:"overview"`
		FirstAired       string `json:"firstAired"`
		LastAired        string `json:"lastAired"`
		Year             string `json:"year"`
		OriginalLanguage string `json:"originalLanguage"`
		OriginalCountry  string `json:"originalCountry"`
		AirsTime         string `json:"airsTime"`
		AverageRuntime   int    `json:"averageRuntime"`
		Score            float64
		Status           struct {
			Name string `json:"name"`
		} `json:"status"`
		OriginalNetwork *struct {
			Name    string `json:"name"`
			Country string `json:"country"`
		} `json:"originalNetwork"`
		Genres []struct {
			Name string `json:"name"`
		} `json:"genres"`
		Aliases []struct {
			Language string `json:"language"`
			Name     string `json:"name"`
		} `json:"aliases"`
		RemoteIDs []struct {
			ID         string `json:"id"`
			SourceName string `json:"sourceName"`
		} `json:"remoteIds"`
		Translations *struct {
			NameTranslations []struct {
				Language string `json:"language"`
				Name     string `json:"name"`
				Overview string `json:"overview"`
			} `json:"nameTranslations"`
		} `json:"translations"`
	} `json:"data"`
}

type episodesPage struct {
	Data struct {
		Episodes []struct {
			ID             int64  `json:"id"`
			SeasonNumber   int    `json:"seasonNumber"`
			Number         int    `json:"number"`
			AbsoluteNumber int    `json:"absoluteNumber"`
			Name           string `json:"name"`
			Overview       string `json:"overview"`
			Aired          string `json:"aired"`
			Runtime        int    `json:"runtime"`
		} `json:"episodes"`
	} `json:"data"`
	Links struct {
		Next *string `json:"next"`
	} `json:"links"`
}

// Series implements metadata.SeriesProvider.
func (c *Client) Series(ctx context.Context, id int64) (metadata.Series, error) {
	var x extended
	sid := strconv.FormatInt(id, 10)
	if err := c.get(ctx, "/v4/series/"+sid+"/extended?short=true&meta=translations", metadata.TTLSeries, &x); err != nil {
		return metadata.Series{}, err
	}
	d := x.Data
	s := metadata.Series{Source: providerName, OriginalLanguage: d.OriginalLanguage, AirTime: d.AirsTime, Runtime: d.AverageRuntime,
		LastAired: d.LastAired}
	s.TVDbID, s.Title, s.Overview, s.FirstAired = d.ID, d.Name, d.Overview, d.FirstAired
	s.Year, _ = strconv.Atoi(d.Year)
	if s.Year == 0 {
		s.Year = year(d.FirstAired)
	}
	s.Status = metadata.NormaliseSeriesStatus(d.Status.Name)
	lang := c.lang3(ctx)
	if d.Translations != nil {
		for _, t := range d.Translations.NameTranslations {
			if t.Language == lang && t.Name != "" {
				if t.Name != s.Title {
					s.OriginalTitle = s.Title
				}
				s.Title = t.Name
				if t.Overview != "" {
					s.Overview = t.Overview
				}
			}
		}
	}
	country := d.OriginalCountry
	if d.OriginalNetwork != nil {
		s.Network = d.OriginalNetwork.Name
		if d.OriginalNetwork.Country != "" {
			country = d.OriginalNetwork.Country
		}
	}
	s.TimeZone = metadata.CountryZone(country)
	for _, g := range d.Genres {
		s.Genres = append(s.Genres, g.Name)
	}
	seen := map[string]bool{strings.ToLower(s.Title): true}
	for _, a := range d.Aliases {
		if k := strings.ToLower(a.Name); a.Name != "" && !seen[k] {
			seen[k] = true
			s.AlternateTitles = append(s.AlternateTitles, a.Name)
		}
	}
	for _, r := range d.RemoteIDs {
		switch strings.ToLower(r.SourceName) {
		case "imdb":
			s.IMDbID = r.ID
		case "themoviedb", "themoviedb.com", "tmdb":
			s.TMDbID = atoi64(r.ID)
		case "tv maze", "tvmaze":
			s.TVMazeID = atoi64(r.ID)
		}
	}
	seasons := map[int]*metadata.Season{}
	for page := 0; page < maxPages; page++ {
		var p episodesPage
		if err := c.get(ctx, fmt.Sprintf("/v4/series/%s/episodes/default?page=%d", sid, page), metadata.TTLSeason, &p); err != nil {
			return metadata.Series{}, err
		}
		for _, e := range p.Data.Episodes {
			sn := seasons[e.SeasonNumber]
			if sn == nil {
				sn = &metadata.Season{Number: e.SeasonNumber, Title: fmt.Sprintf("Season %d", e.SeasonNumber)}
				if e.SeasonNumber == 0 {
					sn.Title = "Specials"
				}
				seasons[e.SeasonNumber] = sn
			}
			rt := e.Runtime
			if rt == 0 {
				rt = s.Runtime
			}
			sn.Episodes = append(sn.Episodes, metadata.Episode{Season: e.SeasonNumber, Number: e.Number, Absolute: e.AbsoluteNumber,
				TVDbID: e.ID, Title: e.Name, Overview: e.Overview, AirDate: e.Aired, Runtime: rt})
			if sn.AirDate == "" || (e.Aired != "" && e.Aired < sn.AirDate) {
				sn.AirDate = e.Aired
			}
		}
		if p.Links.Next == nil || *p.Links.Next == "" || len(p.Data.Episodes) == 0 {
			break
		}
	}
	for _, sn := range seasons {
		sort.Slice(sn.Episodes, func(i, j int) bool { return sn.Episodes[i].Number < sn.Episodes[j].Number })
		s.Seasons = append(s.Seasons, *sn)
	}
	sort.Slice(s.Seasons, func(i, j int) bool { return s.Seasons[i].Number < s.Seasons[j].Number })
	return s, nil
}

// FindSeries implements metadata.SeriesProvider (imdb/tmdb → TVDB id).
func (c *Client) FindSeries(ctx context.Context, source, id string) (int64, error) {
	id = strings.TrimSpace(id)
	if source == "tvdb" {
		if n := atoi64(id); n > 0 {
			return n, nil
		}
		return 0, metadata.ErrNotFound
	}
	if id == "" || strings.ContainsAny(id, "/?#") {
		return 0, metadata.ErrNotFound
	}
	var res struct {
		Data []struct {
			Series *struct {
				ID int64 `json:"id"`
			} `json:"series"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/v4/search/remoteid/"+url.PathEscape(id), metadata.TTLSeries, &res); err != nil {
		return 0, err
	}
	for _, r := range res.Data {
		if r.Series != nil && r.Series.ID > 0 {
			return r.Series.ID, nil
		}
	}
	return 0, metadata.ErrNotFound
}

// ValidateKey checks a key and PIN at TVDB without storing them.
func (c *Client) ValidateKey(ctx context.Context, key, pin string) error {
	_, err := c.Validate(ctx, Credentials{Key: key, PIN: pin})
	return err
}
