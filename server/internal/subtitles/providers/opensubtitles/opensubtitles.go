// Package opensubtitles is a client for the OpenSubtitles.com REST API,
// written from its public documentation (api.opensubtitles.com/api/v1):
// an Api-Key header on every request, an application User-Agent, an
// optional user login for a personal download quota (the JWT is sent as a
// bearer token and requests continue on the base_url the login returns),
// GET /subtitles with parameters sorted and in lower case, POST /download
// for a temporary link, and the published limits: 5 requests per second, 1
// login per second, quota reported with every download.
package opensubtitles

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
)

// Name is the provider id.
const Name = "opensubtitles"

// DefaultURL is the public API address.
const DefaultURL = "https://api.opensubtitles.com/api/v1"

// Client talks to OpenSubtitles.com.
type Client struct {
	cfg   providers.Config
	pacer *providers.Pacer
	login *providers.Pacer
	now   func() time.Time

	mu      sync.Mutex
	token   string
	base    string
	expires time.Time
	authErr error
}

// New returns a client.
func New(cfg providers.Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	// 5 requests per second per IP are allowed; stay below.
	return &Client{cfg: cfg, pacer: &providers.Pacer{Interval: 250 * time.Millisecond}, login: &providers.Pacer{Interval: 1100 * time.Millisecond}, now: time.Now}
}

// Name implements providers.Provider.
func (c *Client) Name() string { return Name }

// Hash is the OpenSubtitles moviehash of a file: its size plus the sums of
// the first and last 64 KiB as little-endian 64-bit words, as 16 hex digits.
func Hash(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- library path checked by the caller
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	const chunk = 64 << 10
	size := st.Size()
	if size < chunk {
		return "", errors.New("file too small for a moviehash")
	}
	h := uint64(size) // #nosec G115 -- sizes are positive
	buf := make([]byte, chunk)
	for _, off := range []int64{0, size - chunk} {
		if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		for i := 0; i < chunk; i += 8 {
			h += binary.LittleEndian.Uint64(buf[i:])
		}
	}
	return fmt.Sprintf("%016x", h), nil
}

func (c *Client) headers(token string) http.Header {
	h := http.Header{}
	h.Set("Api-Key", c.cfg.APIKey)
	h.Set("User-Agent", c.cfg.UserAgent)
	h.Set("Accept", "application/json")
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	return h
}

// session logs in when a username is set and returns the token and the
// API address to use.
func (c *Client) session(ctx context.Context) (string, string, error) {
	if c.cfg.APIKey == "" {
		return "", "", providers.ErrMissingKey
	}
	if c.cfg.Username == "" {
		return "", c.cfg.BaseURL, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.authErr != nil {
		// A refused login is not repeated with the same credentials (the docs ask for that).
		return "", "", c.authErr
	}
	if c.token != "" && c.now().Before(c.expires) {
		return c.token, c.base, nil
	}
	if err := c.login.Wait(ctx); err != nil {
		return "", "", err
	}
	body, _ := json.Marshal(map[string]string{"username": c.cfg.Username, "password": c.cfg.Password})
	h := c.headers("")
	h.Set("Content-Type", "application/json")
	res, err := c.cfg.HTTP.Do(ctx, http.MethodPost, c.cfg.BaseURL+"/login", h, body)
	if err != nil {
		return "", "", err
	}
	if err := c.status(res.Status, res.Header, res.Body); err != nil {
		if errors.Is(err, providers.ErrAuth) {
			c.authErr = err
		}
		return "", "", err
	}
	var v struct {
		Token   string `json:"token"`
		BaseURL string `json:"base_url"`
	}
	if err := json.Unmarshal(res.Body, &v); err != nil || v.Token == "" {
		return "", "", providers.ErrBadResponse
	}
	c.token, c.base, c.expires = v.Token, c.baseFor(v.BaseURL), c.now().Add(23*time.Hour)
	return c.token, c.base, nil
}

// baseFor turns the login's base_url host into an API address. Only
// OpenSubtitles hosts are followed; with a test address it stays put.
func (c *Client) baseFor(host string) string {
	u, err := url.Parse(c.cfg.BaseURL)
	if err != nil || host == "" || !strings.HasSuffix(u.Hostname(), "opensubtitles.com") {
		return c.cfg.BaseURL
	}
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	host = strings.TrimRight(host, "/")
	if h, _, _ := strings.Cut(host, "/"); !strings.HasSuffix(h, ".opensubtitles.com") {
		return c.cfg.BaseURL
	}
	return "https://" + strings.TrimSuffix(host, "/api/v1") + "/api/v1"
}

// Reset forgets the session (credentials changed).
func (c *Client) Reset() {
	c.mu.Lock()
	c.token, c.authErr = "", nil
	c.mu.Unlock()
}

func (c *Client) status(code int, h http.Header, body []byte) error {
	msg := message(body)
	switch {
	case code >= 200 && code < 300:
		return nil
	case code == http.StatusUnauthorized, code == http.StatusForbidden:
		return fmt.Errorf("%w: %s", providers.ErrAuth, msg)
	case code == http.StatusNotAcceptable:
		// Download quota used up; the answer says when it resets.
		var v struct {
			ResetUTC string `json:"reset_time_utc"`
		}
		_ = json.Unmarshal(body, &v)
		q := &providers.QuotaError{Message: msg}
		if t, err := time.Parse(time.RFC3339, v.ResetUTC); err == nil {
			q.ResetAt = t
		} else {
			q.ResetAt = c.now().Add(24 * time.Hour)
		}
		return q
	case code == http.StatusTooManyRequests:
		d := providers.RetryAfter(h, 2*time.Second, c.now())
		c.pacer.Block(d)
		return &providers.RateLimitError{RetryAfter: d}
	case code == http.StatusNotFound || code == http.StatusGone:
		return fmt.Errorf("%w: %s", providers.ErrNotFound, msg)
	}
	return &providers.HTTPError{Status: code, Body: msg}
}

func message(body []byte) string {
	var v struct {
		Message string   `json:"message"`
		Errors  []string `json:"errors"`
	}
	if json.Unmarshal(body, &v) == nil {
		if v.Message != "" {
			return v.Message
		}
		if len(v.Errors) > 0 {
			return strings.Join(v.Errors, "; ")
		}
	}
	s := string(body)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// lang maps catalogue codes to the API's language codes.
func lang(code string) string {
	switch code {
	case "pt":
		return "pt-pt"
	case "pt-br":
		return "pt-br"
	case "zh":
		return "zh-cn"
	case "es-419":
		return "ea"
	case "nl-be":
		return "nl"
	}
	return code
}

func unlang(code string) string {
	switch code = strings.ToLower(code); code {
	case "pt-pt":
		return "pt"
	case "zh-cn", "zh-tw":
		return "zh"
	case "ea":
		return "es-419"
	}
	return code
}

func trimID(id string) string {
	id = strings.TrimPrefix(strings.ToLower(id), "tt")
	return strings.TrimLeft(id, "0")
}

// params renders query parameters sorted, lower-case, with "+" for spaces
// (the documented way to avoid redirects).
func params(v map[string]string) string {
	keys := make([]string, 0, len(v))
	for k, x := range v {
		if x != "" && x != "0" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + strings.ToLower(url.QueryEscape(strings.ToLower(v[k])))
	}
	return strings.Join(parts, "&")
}

type searchResponse struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			SubtitleID        string  `json:"subtitle_id"`
			Language          string  `json:"language"`
			DownloadCount     int     `json:"download_count"`
			HearingImpaired   bool    `json:"hearing_impaired"`
			FPS               float64 `json:"fps"`
			Votes             int     `json:"votes"`
			Ratings           float64 `json:"ratings"`
			FromTrusted       bool    `json:"from_trusted"`
			ForeignPartsOnly  bool    `json:"foreign_parts_only"`
			MachineTranslated bool    `json:"machine_translated"`
			AITranslated      bool    `json:"ai_translated"`
			Release           string  `json:"release"`
			URL               string  `json:"url"`
			MoviehashMatch    bool    `json:"moviehash_match"`
			Uploader          struct {
				Name string `json:"name"`
				Rank string `json:"rank"`
			} `json:"uploader"`
			FeatureDetails struct {
				Year          int   `json:"year"`
				IMDbID        int64 `json:"imdb_id"`
				TMDbID        int64 `json:"tmdb_id"`
				SeasonNumber  int   `json:"season_number"`
				EpisodeNumber int   `json:"episode_number"`
			} `json:"feature_details"`
			Files []struct {
				FileID   int64  `json:"file_id"`
				FileName string `json:"file_name"`
			} `json:"files"`
		} `json:"attributes"`
	} `json:"data"`
}

// Search implements providers.Provider: by moviehash and ids first, by name
// when that finds nothing.
func (c *Client) Search(ctx context.Context, q providers.Query) ([]providers.Result, error) {
	token, base, err := c.session(ctx)
	if err != nil {
		return nil, err
	}
	langs := make([]string, 0, len(q.Languages))
	for _, l := range q.Languages {
		langs = append(langs, lang(l))
	}
	sort.Strings(langs)
	p := map[string]string{"languages": strings.Join(langs, ","), "moviehash": q.Hash}
	if q.Kind == providers.KindEpisode {
		p["type"] = "episode"
		if q.SeriesIMDbID != "" {
			p["parent_imdb_id"] = trimID(q.SeriesIMDbID)
		} else if q.SeriesTMDbID > 0 {
			p["parent_tmdb_id"] = strconv.FormatInt(q.SeriesTMDbID, 10)
		}
		p["season_number"], p["episode_number"] = strconv.Itoa(q.Season), strconv.Itoa(q.Episode)
	} else {
		p["type"] = "movie"
		if q.IMDbID != "" {
			p["imdb_id"] = trimID(q.IMDbID)
		} else if q.TMDbID > 0 {
			p["tmdb_id"] = strconv.FormatInt(q.TMDbID, 10)
		}
	}
	if p["imdb_id"] == "" && p["tmdb_id"] == "" && p["parent_imdb_id"] == "" && p["parent_tmdb_id"] == "" {
		p["query"] = c.text(q)
		if q.Kind == providers.KindMovie && q.Year > 0 {
			p["year"] = strconv.Itoa(q.Year)
		}
	}
	out, err := c.search(ctx, base, token, p)
	if err != nil || len(out) > 0 || p["query"] != "" {
		return out, err
	}
	// Nothing by id: try the name (the ids of a feature are sometimes missing).
	delete(p, "imdb_id")
	delete(p, "tmdb_id")
	delete(p, "parent_imdb_id")
	delete(p, "parent_tmdb_id")
	p["query"] = c.text(q)
	if q.Kind == providers.KindMovie && q.Year > 0 {
		p["year"] = strconv.Itoa(q.Year)
	}
	return c.search(ctx, base, token, p)
}

func (c *Client) text(q providers.Query) string {
	return strings.TrimSpace(q.Title)
}

func (c *Client) search(ctx context.Context, base, token string, p map[string]string) ([]providers.Result, error) {
	if err := c.pacer.Wait(ctx); err != nil {
		return nil, err
	}
	res, err := c.cfg.HTTP.Get(ctx, base+"/subtitles?"+params(p), c.headers(token))
	if err != nil {
		return nil, err
	}
	if err := c.status(res.Status, res.Header, res.Body); err != nil {
		return nil, err
	}
	var v searchResponse
	if err := json.Unmarshal(res.Body, &v); err != nil {
		return nil, fmt.Errorf("%w: %w", providers.ErrBadResponse, err)
	}
	out := make([]providers.Result, 0, len(v.Data))
	for _, d := range v.Data {
		a := d.Attributes
		if len(a.Files) == 0 {
			continue
		}
		r := providers.Result{Provider: Name, ID: a.SubtitleID, Language: unlang(a.Language), FileName: a.Files[0].FileName,
			Handle: strconv.FormatInt(a.Files[0].FileID, 10), HearingImpaired: a.HearingImpaired, Forced: a.ForeignPartsOnly,
			HashMatch: a.MoviehashMatch, FPS: a.FPS, Downloads: a.DownloadCount, Rating: a.Ratings, Votes: a.Votes,
			Trusted:  a.FromTrusted || a.Uploader.Rank == "trusted" || a.Uploader.Rank == "administrator",
			Uploader: a.Uploader.Name, MachineTranslated: a.MachineTranslated || a.AITranslated, Year: a.FeatureDetails.Year,
			TMDbID: a.FeatureDetails.TMDbID, Season: a.FeatureDetails.SeasonNumber, Episode: a.FeatureDetails.EpisodeNumber, PageURL: a.URL}
		if r.ID == "" {
			r.ID = d.ID
		}
		if a.FeatureDetails.IMDbID > 0 {
			r.IMDbID = fmt.Sprintf("tt%07d", a.FeatureDetails.IMDbID)
		}
		if a.Release != "" {
			r.Releases = []string{a.Release}
		}
		out = append(out, r)
	}
	return out, nil
}

// Download implements providers.Provider.
func (c *Client) Download(ctx context.Context, r providers.Result, q providers.Query) (providers.File, providers.Status, error) {
	token, base, err := c.session(ctx)
	if err != nil {
		return providers.File{}, providers.Status{}, err
	}
	id, err := strconv.ParseInt(r.Handle, 10, 64)
	if err != nil {
		return providers.File{}, providers.Status{}, providers.ErrNotFound
	}
	if err := c.pacer.Wait(ctx); err != nil {
		return providers.File{}, providers.Status{}, err
	}
	body, _ := json.Marshal(map[string]int64{"file_id": id})
	h := c.headers(token)
	h.Set("Content-Type", "application/json")
	res, err := c.cfg.HTTP.Do(ctx, http.MethodPost, base+"/download", h, body)
	if err != nil {
		return providers.File{}, providers.Status{}, err
	}
	if err := c.status(res.Status, res.Header, res.Body); err != nil {
		return providers.File{}, providers.Status{}, err
	}
	var v struct {
		Link      string `json:"link"`
		FileName  string `json:"file_name"`
		Requests  int    `json:"requests"`
		Remaining int    `json:"remaining"`
		ResetUTC  string `json:"reset_time_utc"`
	}
	if err := json.Unmarshal(res.Body, &v); err != nil || v.Link == "" {
		return providers.File{}, providers.Status{}, providers.ErrBadResponse
	}
	st := providers.Status{QuotaRemaining: &v.Remaining}
	limit := v.Requests + v.Remaining
	st.QuotaLimit = &limit
	if t, err := time.Parse(time.RFC3339, v.ResetUTC); err == nil {
		st.QuotaResetAt = &t
	}
	if !c.linkAllowed(v.Link) {
		return providers.File{}, st, fmt.Errorf("%w: download link on an unexpected host", providers.ErrBadResponse)
	}
	fr, err := c.cfg.HTTP.Get(ctx, v.Link, http.Header{"User-Agent": {c.cfg.UserAgent}})
	if err != nil {
		return providers.File{}, st, err
	}
	if err := c.status(fr.Status, fr.Header, fr.Body); err != nil {
		return providers.File{}, st, err
	}
	name := v.FileName
	if name == "" {
		name = r.FileName
	}
	f, err := providers.Unpack(name, fr.Body, q)
	return f, st, err
}

func (c *Client) linkAllowed(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if c.cfg.AllowHost != "" && host == c.cfg.AllowHost {
		return true
	}
	base, _ := url.Parse(c.cfg.BaseURL)
	if base != nil && host == base.Hostname() {
		return true
	}
	return u.Scheme == "https" && (host == "opensubtitles.com" || strings.HasSuffix(host, ".opensubtitles.com") ||
		host == "opensubtitles.org" || strings.HasSuffix(host, ".opensubtitles.org"))
}

// Test implements providers.Provider: logs in (when a user is set) and reads
// the account's quota.
func (c *Client) Test(ctx context.Context) (providers.Status, error) {
	c.Reset()
	token, base, err := c.session(ctx)
	if err != nil {
		return providers.Status{}, err
	}
	if token == "" {
		// Without a login the key is checked with a small search.
		if err := c.pacer.Wait(ctx); err != nil {
			return providers.Status{}, err
		}
		res, err := c.cfg.HTTP.Get(ctx, base+"/infos/formats", c.headers(""))
		if err != nil {
			return providers.Status{}, err
		}
		return providers.Status{}, c.status(res.Status, res.Header, res.Body)
	}
	if err := c.pacer.Wait(ctx); err != nil {
		return providers.Status{}, err
	}
	res, err := c.cfg.HTTP.Get(ctx, base+"/infos/user", c.headers(token))
	if err != nil {
		return providers.Status{}, err
	}
	if err := c.status(res.Status, res.Header, res.Body); err != nil {
		return providers.Status{}, err
	}
	var v struct {
		Data struct {
			AllowedDownloads   int    `json:"allowed_downloads"`
			RemainingDownloads int    `json:"remaining_downloads"`
			Level              string `json:"level"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body, &v); err != nil {
		return providers.Status{}, providers.ErrBadResponse
	}
	return providers.Status{Account: c.cfg.Username + " (" + v.Data.Level + ")", QuotaRemaining: &v.Data.RemainingDownloads,
		QuotaLimit: &v.Data.AllowedDownloads}, nil
}
