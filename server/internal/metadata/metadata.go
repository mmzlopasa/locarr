// Package metadata is the provider abstraction for titles, dates and images.
// TMDb (internal/metadata/tmdb) implements the movie provider now; series
// (P5) add a SeriesProvider next to it and music (P6) a MusicBrainz one. Every
// provider goes through the shared Cache (SQLite, per-answer expiry) and its
// own rate limiter, and images are cached on disk by the ImageCache.
package metadata

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Errors every provider returns.
var (
	ErrNotFound    = errors.New("not found at the metadata provider")
	ErrNoKey       = errors.New("no metadata API key is set (Settings → Metadata)")
	ErrInvalidKey  = errors.New("the metadata provider refused the API key")
	ErrUnavailable = errors.New("the metadata provider is not reachable")
)

// Settings.
var (
	Language = settings.Key[string]{Name: "metadata.language", Default: "en-US"}
	Region   = settings.Key[string]{Name: "metadata.region", Default: "US"}
)

// MovieSummary is a search or list result.
type MovieSummary struct {
	TMDbID        int64   `json:"tmdbId"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"originalTitle"`
	Year          int     `json:"year"`
	ReleaseDate   string  `json:"releaseDate"`
	Overview      string  `json:"overview"`
	Poster        string  `json:"poster"`
	Backdrop      string  `json:"backdrop"`
	Popularity    float64 `json:"popularity"`
	Rating        float64 `json:"rating"`
}

// Movie is a full movie record.
type Movie struct {
	MovieSummary
	IMDbID           string   `json:"imdbId"`
	OriginalLanguage string   `json:"originalLanguage"`
	AlternateTitles  []string `json:"alternateTitles"`
	Runtime          int      `json:"runtime"`
	Status           string   `json:"status"`
	Genres           []string `json:"genres"`
	Studio           string   `json:"studio"`
	Certification    string   `json:"certification"`
	Votes            int      `json:"votes"`
	InCinemas        string   `json:"inCinemas"`
	DigitalRelease   string   `json:"digitalRelease"`
	PhysicalRelease  string   `json:"physicalRelease"`
	CollectionID     int64    `json:"collectionId"`
	CollectionName   string   `json:"collectionName"`
}

// Collection is a set of related movies.
type Collection struct {
	ID       int64          `json:"id"`
	Name     string         `json:"name"`
	Overview string         `json:"overview"`
	Poster   string         `json:"poster"`
	Backdrop string         `json:"backdrop"`
	Parts    []MovieSummary `json:"parts"`
}

// MovieProvider looks movies up.
type MovieProvider interface {
	SearchMovies(ctx context.Context, query string, year int) ([]MovieSummary, error)
	Movie(ctx context.Context, tmdbID int64) (Movie, error)
	// FindByIMDb returns the TMDb id for an IMDb id.
	FindByIMDb(ctx context.Context, imdbID string) (int64, error)
	Collection(ctx context.Context, id int64) (Collection, error)
	// List returns the movies of a public list.
	List(ctx context.Context, id string) ([]MovieSummary, error)
	// Validate checks an API key without storing it.
	Validate(ctx context.Context, key string) error
}

// ImageSource fetches provider images.
type ImageSource interface {
	// FetchImage returns the image bytes and content type.
	FetchImage(ctx context.Context, size, file string) ([]byte, string, error)
}

type freshKey struct{}

// Fresh makes provider calls with ctx skip cached answers (refresh jobs).
func Fresh(ctx context.Context) context.Context { return context.WithValue(ctx, freshKey{}, true) }

// IsFresh reports whether ctx asks for fresh answers.
func IsFresh(ctx context.Context) bool { v, _ := ctx.Value(freshKey{}).(bool); return v }

// TTLs of cached answers.
const (
	TTLSearch     = time.Hour
	TTLMovie      = 12 * time.Hour
	TTLCollection = 24 * time.Hour
	TTLList       = time.Hour
	TTLConfig     = 24 * time.Hour
)

// SameHost is an http.Client CheckRedirect that follows at most five
// redirects and only on the host the request went to.
func SameHost(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 || !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
		return http.ErrUseLastResponse
	}
	return nil
}
