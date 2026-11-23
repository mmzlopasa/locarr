// Package providers defines the subtitle provider interface and the shared
// pieces of the provider clients (OpenSubtitles.com, SubDL, Jimaku; D-063):
// queries, results, typed errors for authentication, quota and rate limits,
// a request pacer and archive unpacking. Each client is written from its
// provider's public API documentation only.
package providers

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/parser"
)

// Kinds of media a query is for.
const (
	KindMovie   = "movie"
	KindEpisode = "episode"
)

// Query is what Locarr knows about the video a subtitle is wanted for.
type Query struct {
	Kind      string
	Languages []string // catalogue codes (internal/languages)
	Title     string   // movie title or episode's series title
	Year      int
	IMDbID    string // movie or episode IMDb id ("tt0133093")
	TMDbID    int64  // movie TMDb id
	// Series ids for episodes.
	SeriesIMDbID string
	SeriesTMDbID int64
	Season       int
	Episode      int
	Absolute     int
	Anime        bool
	// File facts.
	FileName string // release or file name
	FileSize int64
	Hash     string // OpenSubtitles moviehash ("" when unknown)
}

// Result is one subtitle a provider offers.
type Result struct {
	Provider string `json:"provider"`
	// ID identifies the subtitle at the provider (used for "never twice" and the blacklist).
	ID       string   `json:"id"`
	Language string   `json:"language"`
	Releases []string `json:"releases"`
	FileName string   `json:"fileName"`
	// Handle is what Download needs (a file id, a path); never shown to users.
	Handle            string  `json:"-"`
	HearingImpaired   bool    `json:"hearingImpaired"`
	Forced            bool    `json:"forced"`
	HashMatch         bool    `json:"hashMatch"`
	FPS               float64 `json:"fps"`
	Downloads         int     `json:"downloads"`
	Rating            float64 `json:"rating"`
	Votes             int     `json:"votes"`
	Trusted           bool    `json:"trusted"`
	Uploader          string  `json:"uploader"`
	MachineTranslated bool    `json:"machineTranslated"`
	IMDbID            string  `json:"imdbId,omitempty"`
	TMDbID            int64   `json:"tmdbId,omitempty"`
	Year              int     `json:"year,omitempty"`
	Season            int     `json:"season,omitempty"`
	Episode           int     `json:"episode,omitempty"`
	// SeasonPack is a subtitle archive for a whole season.
	SeasonPack bool   `json:"seasonPack,omitempty"`
	PageURL    string `json:"pageUrl,omitempty"`
}

// File is a downloaded subtitle file (still in its original encoding).
type File struct {
	Name string
	Data []byte
}

// Status is a provider's account state (quota and who is signed in).
type Status struct {
	Account        string     `json:"account,omitempty"`
	QuotaRemaining *int       `json:"quotaRemaining,omitempty"`
	QuotaLimit     *int       `json:"quotaLimit,omitempty"`
	QuotaResetAt   *time.Time `json:"quotaResetAt,omitempty"`
}

// Provider is one subtitle source.
type Provider interface {
	Name() string
	Search(ctx context.Context, q Query) ([]Result, error)
	// Download fetches the subtitle file; st carries the quota the provider reported.
	Download(ctx context.Context, r Result, q Query) (File, Status, error)
	// Test checks credentials and returns the account state.
	Test(ctx context.Context) (Status, error)
}

// Errors shared by all providers.
var (
	ErrAuth        = errors.New("the provider refused the credentials")
	ErrNotFound    = errors.New("the subtitle is no longer available at the provider")
	ErrBadResponse = errors.New("the provider sent an answer Locarr cannot read")
	ErrMissingKey  = errors.New("the provider needs an API key")
)

// QuotaError means the download quota is used up until ResetAt.
type QuotaError struct {
	ResetAt time.Time
	Message string
}

func (e *QuotaError) Error() string {
	msg := "download quota used up"
	if !e.ResetAt.IsZero() {
		msg += " until " + e.ResetAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// RateLimitError means too many requests; try again after RetryAfter.
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string {
	return "too many requests; the provider asks to wait " + e.RetryAfter.Round(time.Second).String()
}

// RetryAfter reads Retry-After (seconds or HTTP date) and the common
// rate-limit reset headers; def when none is present.
func RetryAfter(h http.Header, def time.Duration, now time.Time) time.Duration {
	for _, k := range []string{"Retry-After", "X-Ratelimit-Reset-After", "Ratelimit-Reset"} {
		v := strings.TrimSpace(h.Get(k))
		if v == "" {
			continue
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			// Values that look like a UNIX time are a moment, not a delay.
			if f > 1e9 {
				return max(time.Until(time.Unix(int64(f), 0)), time.Second)
			}
			return max(time.Duration(f*float64(time.Second)), time.Second)
		}
		if t, err := http.ParseTime(v); err == nil {
			return max(t.Sub(now), time.Second)
		}
	}
	return def
}

// Pacer spaces requests: at most one per Interval, and none while a rate
// limit the provider announced lasts.
type Pacer struct {
	Interval time.Duration
	mu       sync.Mutex
	next     time.Time
	blocked  time.Time
}

// Wait blocks until the next request may go out.
func (p *Pacer) Wait(ctx context.Context) error {
	p.mu.Lock()
	now := time.Now()
	at := max64(p.next, p.blocked, now)
	p.next = at.Add(p.Interval)
	p.mu.Unlock()
	if d := time.Until(at); d > 0 {
		if d > time.Minute {
			return &RateLimitError{RetryAfter: d}
		}
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	return nil
}

// Block stops requests for d.
func (p *Pacer) Block(d time.Duration) {
	p.mu.Lock()
	if t := time.Now().Add(d); t.After(p.blocked) {
		p.blocked = t
	}
	p.mu.Unlock()
}

func max64(ts ...time.Time) time.Time {
	m := ts[0]
	for _, t := range ts[1:] {
		if t.After(m) {
			m = t
		}
	}
	return m
}

// Config is what the service hands a provider client.
type Config struct {
	BaseURL  string // "" = the provider's public address
	APIKey   string
	Username string
	Password string
	HTTP     *httpx.Client
	// UserAgent names the application and version ("Locarr v1.2.3").
	UserAgent string
	// AllowHost is an extra host downloads may come from (tests and fakes).
	AllowHost string
}

// SubtitleExts are the subtitle file extensions Locarr saves.
var SubtitleExts = map[string]bool{".srt": true, ".ass": true, ".ssa": true, ".vtt": true}

// Archive limits.
const (
	maxEntries   = 500
	maxEntrySize = 8 << 20
)

// Unpack returns the subtitle file of data: data itself when it is not a
// ZIP archive, otherwise the archive member that best fits the query (the
// episode's file in a season pack, else the largest subtitle).
func Unpack(name string, data []byte, q Query) (File, error) {
	if len(data) < 4 || !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		if bytes.HasPrefix(data, []byte("Rar!")) || bytes.HasPrefix(data, []byte{'7', 'z', 0xBC, 0xAF}) {
			return File{}, errors.New("the subtitle is packed as RAR or 7z, which Locarr does not unpack")
		}
		return File{Name: name, Data: data}, nil
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return File{}, fmt.Errorf("damaged ZIP archive: %w", err)
	}
	if len(zr.File) > maxEntries {
		return File{}, errors.New("the archive has too many files")
	}
	var best *zip.File
	bestScore := -1
	for _, f := range zr.File {
		ext := strings.ToLower(path.Ext(f.Name))
		if !SubtitleExts[ext] || f.UncompressedSize64 > maxEntrySize || f.FileInfo().IsDir() {
			continue
		}
		sc := int(min(f.UncompressedSize64/1024, 1000)) // #nosec G115 -- bounded
		if q.Kind == KindEpisode && q.Episode > 0 {
			rel := parser.Parse(path.Base(f.Name))
			switch {
			case containsInt(rel.Episodes, q.Episode) && (len(rel.Seasons) == 0 || containsInt(rel.Seasons, q.Season)):
				sc += 100000
			case q.Absolute > 0 && containsInt(rel.AbsoluteEpisodes, q.Absolute):
				sc += 100000
			case len(rel.Episodes) > 0 || len(rel.AbsoluteEpisodes) > 0:
				sc -= 100000 // another episode of the pack
			}
		}
		if sc > bestScore {
			best, bestScore = f, sc
		}
	}
	if best == nil || bestScore < -1000 {
		return File{}, errors.New("the archive has no subtitle file for this video")
	}
	rc, err := best.Open()
	if err != nil {
		return File{}, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, maxEntrySize+1))
	if err != nil {
		return File{}, err
	}
	if len(b) > maxEntrySize {
		return File{}, errors.New("the subtitle file in the archive is too large")
	}
	return File{Name: path.Base(best.Name), Data: b}, nil
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// HTTPError is an unexpected HTTP status.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	b := strings.TrimSpace(e.Body)
	if len(b) > 200 {
		b = b[:200]
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, b)
}
