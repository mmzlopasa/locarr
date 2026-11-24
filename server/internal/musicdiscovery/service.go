// Package musicdiscovery suggests artists for the music library: it asks
// several providers for artists similar to the library's artists (Last.fm,
// ListenBrainz, MusicBrainz relationships and, where Spotify still allows
// it, Spotify's related artists), maps every answer to a MusicBrainz artist
// id, stores the answers and combines them into one ranked list that leaves
// out what is already in the library and what someone marked "not
// interested" (D-101).
package musicdiscovery

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/metadata/lastfm"
	"github.com/mmzlopasa/locarr/server/internal/metadata/listenbrainz"
	"github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz"
	"github.com/mmzlopasa/locarr/server/internal/metadata/spotify"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Provider ids.
const (
	LastFM       = "lastfm"
	ListenBrainz = "listenbrainz"
	MusicBrainz  = "musicbrainz"
	Spotify      = "spotify"
)

// Providers in display order.
var Providers = []string{LastFM, ListenBrainz, MusicBrainz, Spotify}

// Weights say how much one provider's "similar" counts: listening data
// (Last.fm, ListenBrainz) more than Spotify's related artists, and those more
// than MusicBrainz relationships, which say two artists worked together
// rather than that they sound alike.
var Weights = map[string]float64{LastFM: 1.0, ListenBrainz: 0.9, Spotify: 0.8, MusicBrainz: 0.6}

// Limits.
const (
	// PerSeed is how many similar artists are kept per artist and provider.
	PerSeed = 50
	// MinConfidence drops answers whose MusicBrainz id is a guess.
	MinConfidence = 0.5
	// EnrichPerRun bounds the tag and cover lookups of one refresh.
	EnrichPerRun = 150
	// enrichAge is how old tags and covers may get.
	enrichAge = 30 * 24 * time.Hour
	// retryFailed is when a failed lookup is tried again.
	retryFailed = 6 * time.Hour
)

// Config is the stored configuration.
type Config struct {
	LastFM       bool `json:"lastfm"`
	ListenBrainz bool `json:"listenbrainz"`
	MusicBrainz  bool `json:"musicbrainz"`
	Spotify      bool `json:"spotify"`
	// RefreshDays is how old an artist's answers may get before they are asked again.
	RefreshDays int `json:"refreshDays"`
}

// Enabled reports whether provider p is switched on.
func (c Config) Enabled(p string) bool {
	switch p {
	case LastFM:
		return c.LastFM
	case ListenBrainz:
		return c.ListenBrainz
	case MusicBrainz:
		return c.MusicBrainz
	case Spotify:
		return c.Spotify
	}
	return false
}

// Settings.
var (
	ConfigKey = settings.Key[Config]{Name: "musicDiscovery.config",
		Default: Config{LastFM: true, ListenBrainz: true, MusicBrainz: true, Spotify: true, RefreshDays: 7}}
	LastFMKey     = settings.SecretKey{Name: "musicDiscovery.lastfmApiKey"}
	SpotifyID     = settings.SecretKey{Name: "musicDiscovery.spotifyClientId"}
	SpotifySecret = settings.SecretKey{Name: "musicDiscovery.spotifyClientSecret"}
)

// Service is music discovery.
type Service struct {
	DB       *db.DB
	Settings *settings.Store
	Cache    *metadata.Cache
	MB       *musicbrainz.Client
	LastFM   *lastfm.Client
	LB       *listenbrainz.Client
	Spotify  *spotify.Client
	Log      *slog.Logger
	Now      func() time.Time
	// RefreshRequested queues a background refresh (nil = none).
	RefreshRequested func(ctx context.Context, seeds []string) error

	running atomic.Bool
	version atomic.Int64
	mu      sync.Mutex
	cache   map[string]cached
	// spotifyRelated: 0 unknown, 1 available, -1 refused by Spotify.
	spotifyRelated   atomic.Int32
	spotifyCheckedAt atomic.Int64
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Config returns the stored configuration (refresh days within 1–90).
func (s *Service) Config(ctx context.Context) (Config, error) {
	c, err := settings.Get(ctx, s.Settings, ConfigKey)
	if c.RefreshDays < 1 || c.RefreshDays > 90 {
		c.RefreshDays = ConfigKey.Default.RefreshDays
	}
	return c, err
}

// Configured reports whether provider p has what it needs (keys).
func (s *Service) Configured(ctx context.Context, p string) bool {
	switch p {
	case LastFM:
		ok, _ := s.Settings.Configured(ctx, LastFMKey)
		return ok && s.LastFM != nil
	case Spotify:
		id, _ := s.Settings.Configured(ctx, SpotifyID)
		secret, _ := s.Settings.Configured(ctx, SpotifySecret)
		return id && secret && s.Spotify != nil
	case ListenBrainz:
		return s.LB != nil
	case MusicBrainz:
		return s.MB != nil
	}
	return false
}

// Active lists the providers that are switched on and configured.
func (s *Service) Active(ctx context.Context) ([]string, error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, p := range Providers {
		if cfg.Enabled(p) && s.Configured(ctx, p) {
			out = append(out, p)
		}
	}
	return out, nil
}

// SpotifyRelated is "available", "restricted" or "unknown".
func (s *Service) SpotifyRelated() string {
	switch s.spotifyRelated.Load() {
	case 1:
		return "available"
	case -1:
		return "restricted"
	}
	return "unknown"
}

// SetSpotifyRelated records what Spotify answered (a credentials test or a lookup).
func (s *Service) SetSpotifyRelated(available bool) {
	v := int32(-1)
	if available {
		v = 1
	}
	s.spotifyRelated.Store(v)
	s.spotifyCheckedAt.Store(s.now().UnixMilli())
}

// ResetSpotify forgets what Spotify answered (new credentials).
func (s *Service) ResetSpotify() {
	s.spotifyRelated.Store(0)
	s.spotifyCheckedAt.Store(0)
}

// Invalidate drops cached suggestion lists (the library or the answers changed).
func (s *Service) Invalidate() { s.version.Add(1) }

// Normalize makes artist names comparable: no diacritics, case,
// punctuation, "&"/"and" or a leading "the".
func Normalize(name string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	s, _, err := transform.String(t, name)
	if err != nil {
		s = name
	}
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "&", " and ")
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		default:
			space = true
		}
	}
	out := b.String()
	out = strings.TrimPrefix(out, "the ")
	return out
}

// errStop ends a provider's run (bad key, Spotify refusing).
func errStop(err error) bool {
	return errors.Is(err, metadata.ErrInvalidKey) || errors.Is(err, metadata.ErrNoKey) || errors.Is(err, spotify.ErrRestricted)
}

// LastFMKeyValue is the stored Last.fm key ("" = none).
func (s *Service) LastFMKeyValue(ctx context.Context) string {
	v, _, _ := s.Settings.Secret(ctx, LastFMKey)
	return v
}

// SpotifyCreds are the stored Spotify credentials.
func (s *Service) SpotifyCreds(ctx context.Context) spotify.Credentials {
	id, _, _ := s.Settings.Secret(ctx, SpotifyID)
	secret, _, _ := s.Settings.Secret(ctx, SpotifySecret)
	return spotify.Credentials{ID: id, Secret: secret}
}

// RequestRefresh queues a background refresh when one is wired up.
func (s *Service) RequestRefresh(ctx context.Context, seeds []string) error {
	if s.RefreshRequested == nil {
		return nil
	}
	return s.RefreshRequested(ctx, seeds)
}
