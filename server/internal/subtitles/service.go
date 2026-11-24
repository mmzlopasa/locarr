// Package subtitles finds, scores, downloads, saves and re-times subtitles
// for the movies and episodes in the library (P7). Language profiles say
// what is wanted; providers (OpenSubtitles.com, SubDL, Jimaku) are searched
// in priority order with health, backoff, rate limits and download quotas
// respected; every step lands on the item's timeline.
package subtitles

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/library/recycle"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	subsync "github.com/mmzlopasa/locarr/server/internal/subtitles/sync"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// Settings.
var (
	DefaultProfileMovies = settings.Key[int64]{Name: "subtitles.defaultProfileMovies", Default: 0}
	DefaultProfileSeries = settings.Key[int64]{Name: "subtitles.defaultProfileSeries", Default: 0}
	MinScoreMovies       = settings.Key[int]{Name: "subtitles.minScoreMovies", Default: 60}
	MinScoreEpisodes     = settings.Key[int]{Name: "subtitles.minScoreEpisodes", Default: 60}
	// PreferredFormat: "" keeps the provider's format, "srt" converts.
	PreferredFormat = settings.Key[string]{Name: "subtitles.preferredFormat", Default: ""}
	// UpgradeDays: search for better subtitles this many days after the file arrived (0 = never).
	UpgradeDays = settings.Key[int]{Name: "subtitles.upgradeDays", Default: 7}
	// SearchesPerHour caps the scheduled searches, spreading provider quotas over the day.
	SearchesPerHour = settings.Key[int]{Name: "subtitles.searchesPerHour", Default: 30}
	// AutoSync re-times downloaded subtitles that are not a hash match against the audio.
	AutoSync = settings.Key[bool]{Name: "subtitles.autoSync", Default: false}
)

// Timeline event types.
const (
	TypeDownloaded = "subtitle_downloaded"
	TypeUpgraded   = "subtitle_upgraded"
	TypeNotFound   = "subtitle_not_found"
	TypeFailed     = "subtitle_failed"
	TypeDeleted    = "subtitle_deleted"
	TypeSynced     = "subtitle_synced"
	TypeEdited     = "subtitle_edited"
	TypeFound      = "subtitle_found_on_disk"
)

// Job kinds.
const (
	TaskSearchItem = "subtitles-search-item"
	TaskMissing    = "subtitles-missing"
	TaskScan       = "subtitles-scan"
	TaskSync       = "subtitles-sync"
	Queue          = "subtitles"
)

// Errors.
var (
	ErrNotFound        = errors.New("not found")
	ErrNoFile          = errors.New("the item has no file in the library")
	ErrProfileInUse    = errors.New("the profile is a default; choose another default first")
	ErrResultExpired   = errors.New("the search result has expired; search again")
	ErrSyncUnavailable = errors.New("audio sync needs ffmpeg, which is not available")
	ErrBadLanguage     = errors.New("unknown language")
)

// Service is the subtitle subsystem; missing languages can also be
// machine-translated from a subtitle in another language (translation.go).
type Service struct {
	DB        *db.DB
	Box       *secrets.Box
	Settings  *settings.Store
	Jobs      *jobs.Queue
	Timeline  *timeline.Store
	Recycle   *recycle.Bin
	DataRoots []string
	Log       *slog.Logger
	// FFmpeg decodes audio and embedded text tracks for sync (zero = unavailable).
	FFmpeg subsync.FFmpeg
	// URLs replaces provider addresses (tests and the fakes); keys are provider names.
	URLs map[string]string
	// UserAgent is "Locarr v<version>".
	UserAgent string
	// Notify is told about every timeline entry (P9 sends some of them to
	// the notification channels); nil = nobody.
	Notify func(ctx context.Context, ref, typ, msg string)
	Now    func() time.Time
	// TranslateSleep waits between retries of translation services (tests); nil = real time.
	TranslateSleep func(ctx context.Context, d time.Duration) error

	mu       sync.Mutex
	clients  map[string]clientEntry
	manual   map[string]manualEntry
	progress map[string]TranslationProgress
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

// SyncAvailable reports whether audio sync can run.
func (s *Service) SyncAvailable() bool { return s.FFmpeg.Path != "" }

func (s *Service) record(ctx context.Context, ref, typ, msg string, data any) {
	if s.Timeline == nil {
		return
	}
	if err := s.Timeline.Record(ctx, ref, typ, msg, data, nil); err != nil {
		s.log().Warn("record subtitle event", "item", ref, "err", err)
	}
	if s.Notify != nil {
		s.Notify(ctx, ref, typ, msg)
	}
}
