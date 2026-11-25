// Package adult is the optional adult library (D-103 .. D-106): studios
// (sites) with date-keyed scenes, adult movies and performers, on top of the
// shared core (decision engine, wanted orchestration, importer, naming,
// timeline, recycle bin). It is off by default; an administrator enables it
// with a confirmation that every user with access is an adult, and access is
// the separate view_adult permission. Everything is stored in adult_* tables,
// so nothing outside this package and its endpoints can list adult rows.
package adult

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/adult/meta"
	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/library/recycle"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
	"github.com/mmzlopasa/locarr/server/internal/wanted"
)

// Settings.
var (
	// Enabled switches the whole feature; off = every adult endpoint is 404.
	Enabled = settings.Key[bool]{Name: "adult.enabled", Default: false}
	// ConfirmedAt/ConfirmedBy record the administrator's confirmation.
	ConfirmedAt = settings.Key[string]{Name: "adult.confirmedAt", Default: ""}
	ConfirmedBy = settings.Key[int64]{Name: "adult.confirmedBy", Default: 0}
	// DefaultSource is where Add searches first (stashdb or tpdb).
	DefaultSource = settings.Key[string]{Name: "adult.defaultSource", Default: meta.StashDB}
	// DefaultProfile and DefaultRoot preselect Add.
	DefaultProfile = settings.Key[int64]{Name: "adult.defaultProfileId", Default: 0}
	DefaultRoot    = settings.Key[int64]{Name: "adult.defaultRootFolderId", Default: 0}
	// OpenProfile is the profile that allows every quality, Unknown
	// included (D-109), recorded by migration 00018.
	OpenProfile = settings.Key[int64]{Name: "adult.openProfileId", Default: 0}
	// Naming templates (D-106).
	SceneFolder = settings.Key[string]{Name: "adult.sceneFolder", Default: "{Studio}"}
	SceneFile   = settings.Key[string]{Name: "adult.sceneFile", Default: "{Studio} - {Date} - {Title}{ [Performers]} {Quality}"}
	MovieFolder = settings.Key[string]{Name: "adult.movieFolder", Default: "{Title} ({Year})"}
	MovieFile   = settings.Key[string]{Name: "adult.movieFile", Default: "{Title} ({Year}) {Quality}"}
	// StatsAggregate adds one total count of adult items to /api/stats
	// (never a breakdown).
	StatsAggregate = settings.Key[bool]{Name: "adult.statsAggregate", Default: false}
	// NotifyStreamero tells the media server about adult imports; off unless
	// the administrator maps an adult Streamero library.
	NotifyStreamero = settings.Key[bool]{Name: "adult.notifyStreamero", Default: false}
)

// Secret keys.
var (
	StashDBKey = settings.SecretKey{Name: "adult.stashdbApiKey"}
	TPDBKey    = settings.SecretKey{Name: "adult.tpdbApiKey"}
)

// BlurKey is a user's "blur adult images" preference (default on).
func BlurKey(userID int64) settings.Key[bool] {
	return settings.Key[bool]{Name: "adult.blur." + strconv.FormatInt(userID, 10), Default: true}
}

// Errors.
var (
	ErrHidden        = errors.New("not found")
	ErrNotFound      = errors.New("adult item not found")
	ErrConfirm       = errors.New("confirm that every user with access to adult content is an adult")
	ErrExists        = errors.New("already in the library")
	ErrRemoved       = errors.New("the metadata source flags this entry as removed or disputed; it is not imported")
	ErrBlocked       = errors.New("the entry carries a blocked tag")
	ErrNoRoot        = errors.New("add an adult root folder first")
	ErrNoProfile     = errors.New("unknown quality profile")
	ErrUnknownSource = errors.New("unknown metadata source")
	ErrBadTerm       = errors.New("a blocklist term needs at least two letters")
	ErrProtectedTerm = errors.New("this term is always blocked")
	ErrBadAction     = errors.New("unknown bulk action")
)

// Service is the adult library.
type Service struct {
	DB        *db.DB
	Settings  *settings.Store
	Quality   *quality.Store
	Downloads *downloads.Service
	Wanted    *wanted.Service
	Timeline  *timeline.Store
	Recycle   *recycle.Bin
	Jobs      *jobs.Queue
	// Notifier is the media server (used only with NotifyStreamero).
	Notifier mediaserver.Notifier
	// Events publishes adult events to channels that opted in.
	Events    func(ctx context.Context, typ, title, message, ref string)
	Providers map[string]meta.Provider
	DataRoots []string
	ImageDir  string
	Log       *slog.Logger
	Now       func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// On reports whether the feature is enabled.
func (s *Service) On(ctx context.Context) bool {
	if s == nil {
		return false
	}
	on, err := settings.Get(ctx, s.Settings, Enabled)
	return err == nil && on
}

// Visible reports whether p may see adult content now: the feature is on
// and the account holds view_adult (requesters never; API keys only with
// "include adult").
func (s *Service) Visible(ctx context.Context, p auth.Principal) bool {
	return p.CanViewAdult() && s.On(ctx)
}

// Guard returns ErrHidden unless p may see adult content.
func (s *Service) Guard(ctx context.Context, p auth.Principal) error {
	if !s.Visible(ctx, p) {
		return ErrHidden
	}
	return nil
}

// Enable switches the feature on (with confirmation) or off.
func (s *Service) Enable(ctx context.Context, by int64, on, confirmed bool) error {
	if on && !confirmed {
		return ErrConfirm
	}
	if err := settings.Set(ctx, s.Settings, s.DB.W, Enabled, on); err != nil {
		return err
	}
	if on {
		if err := settings.Set(ctx, s.Settings, s.DB.W, ConfirmedAt, s.now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
		return settings.Set(ctx, s.Settings, s.DB.W, ConfirmedBy, by)
	}
	return nil
}

// Provider returns a metadata source by name.
func (s *Service) Provider(name string) (meta.Provider, error) {
	p, ok := s.Providers[name]
	if !ok {
		return nil, ErrUnknownSource
	}
	return p, nil
}

func (s *Service) record(ctx context.Context, ref, typ, msg string, data any, dl *int64) {
	if s.Timeline == nil {
		return
	}
	if err := s.Timeline.Record(ctx, ref, typ, msg, data, dl); err != nil && s.Log != nil {
		s.Log.Warn("timeline", "err", err)
	}
}

// StatsTotal is the opt-in aggregate for /api/stats: one number, only while
// the feature is on and adult.statsAggregate is set.
func (s *Service) StatsTotal(ctx context.Context) (int, bool) {
	if !s.On(ctx) {
		return 0, false
	}
	if on, _ := settings.Get(ctx, s.Settings, StatsAggregate); !on {
		return 0, false
	}
	n, err := s.Count(ctx)
	return n, err == nil
}
