// Package config reads the server configuration from LOCARR_* environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the server configuration. Every field has a working default for the
// container image; deploy/.env.example documents each variable.
type Config struct {
	// HTTPAddr is the listen address (LOCARR_HTTP_ADDR, default ":8484").
	HTTPAddr string
	// ConfigDir holds the database and the secret key (LOCARR_CONFIG_DIR, default "/config").
	ConfigDir string
	// DataRoots are the folders that request paths may point into
	// (LOCARR_DATA_ROOTS, comma-separated, default "/data").
	DataRoots []string
	// LogLevel is debug, info, warn or error (LOCARR_LOG_LEVEL, default "info").
	LogLevel string
	// TMDbURL replaces TMDb's API address (LOCARR_TMDB_URL; tests and the
	// fake services only, default https://api.themoviedb.org).
	TMDbURL string
	// TVDBURL replaces TVDB's API address (LOCARR_TVDB_URL; tests and the
	// fake services only, default https://api4.thetvdb.com).
	TVDBURL string
	// MusicBrainzURL and CoverArtURL replace MusicBrainz's and the Cover Art
	// Archive's addresses (LOCARR_MUSICBRAINZ_URL, LOCARR_COVERART_URL; tests
	// and the fake services only).
	MusicBrainzURL string
	CoverArtURL    string
	// SubtitleURLs replace subtitle provider addresses (LOCARR_OPENSUBTITLES_URL,
	// LOCARR_SUBDL_URL, LOCARR_JIMAKU_URL; tests and the fake services only).
	SubtitleURLs map[string]string
	// FFmpeg is the ffmpeg binary used for subtitle sync (LOCARR_FFMPEG,
	// default "ffmpeg" in PATH; the image ships a small build, D-054).
	FFmpeg string
	// TrustedProxies are CIDRs whose X-Forwarded-For is believed (LOCARR_TRUSTED_PROXIES).
	TrustedProxies string
	// SetupCode fixes the first-run setup code instead of a random one (LOCARR_SETUP_CODE).
	SetupCode string
	// AuthRatePerMin and AuthRateBurst limit sign-in style requests per client
	// address (LOCARR_AUTH_RATE_LIMIT="perMinute,burst", default "10,10"; "0" turns it off).
	AuthRatePerMin int
	AuthRateBurst  int
}

// DatabasePath is the SQLite database file.
func (c Config) DatabasePath() string { return filepath.Join(c.ConfigDir, "locarr.db") }

// Load reads the configuration from the environment.
func Load() (Config, error) { return load(os.Getenv) }

func load(env func(string) string) (Config, error) {
	get := func(k, def string) string {
		if v := strings.TrimSpace(env(k)); v != "" {
			return v
		}
		return def
	}
	c := Config{
		HTTPAddr:       get("LOCARR_HTTP_ADDR", ":8484"),
		ConfigDir:      get("LOCARR_CONFIG_DIR", "/config"),
		LogLevel:       strings.ToLower(get("LOCARR_LOG_LEVEL", "info")),
		TrustedProxies: get("LOCARR_TRUSTED_PROXIES", ""),
		SetupCode:      get("LOCARR_SETUP_CODE", ""),
		TMDbURL:        get("LOCARR_TMDB_URL", ""),
		TVDBURL:        get("LOCARR_TVDB_URL", ""),
		MusicBrainzURL: get("LOCARR_MUSICBRAINZ_URL", ""),
		CoverArtURL:    get("LOCARR_COVERART_URL", ""),
		FFmpeg:         get("LOCARR_FFMPEG", ""),
		SubtitleURLs:   map[string]string{},
	}
	for name, k := range map[string]string{"opensubtitles": "LOCARR_OPENSUBTITLES_URL", "subdl": "LOCARR_SUBDL_URL", "jimaku": "LOCARR_JIMAKU_URL"} {
		if v := get(k, ""); v != "" {
			c.SubtitleURLs[name] = v
		}
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, fmt.Errorf("LOCARR_LOG_LEVEL: want debug, info, warn or error, got %q", c.LogLevel)
	}
	for _, r := range strings.Split(get("LOCARR_DATA_ROOTS", "/data"), ",") {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if !filepath.IsAbs(r) {
			return Config{}, fmt.Errorf("LOCARR_DATA_ROOTS: %q is not an absolute path", r)
		}
		c.DataRoots = append(c.DataRoots, filepath.Clean(r))
	}
	rate := get("LOCARR_AUTH_RATE_LIMIT", "10,10")
	if rate != "0" {
		per, burst, ok := strings.Cut(rate, ",")
		p, err1 := strconv.Atoi(strings.TrimSpace(per))
		b, err2 := strconv.Atoi(strings.TrimSpace(burst))
		if !ok || err1 != nil || err2 != nil || p <= 0 || b <= 0 {
			return Config{}, fmt.Errorf("LOCARR_AUTH_RATE_LIMIT: want \"perMinute,burst\" or \"0\", got %q", rate)
		}
		c.AuthRatePerMin, c.AuthRateBurst = p, b
	}
	return c, nil
}
