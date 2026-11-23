package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/api"
	"github.com/mmzlopasa/locarr/server/internal/discover"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/notify"
	"github.com/mmzlopasa/locarr/server/internal/requests"
)

// Request task kinds.
const (
	TaskRequestsSync       = "requests-sync"
	TaskNotificationsPrune = "notifications-prune"
)

// notificationHistory is how long read notifications are kept.
const notificationHistory = 90 * 24 * time.Hour

// arrivals passes library changes on to the media server notifier and
// tells the requests to look at the library again.
type arrivals struct {
	next mediaserver.Notifier
	kick func()
}

func (a arrivals) Name() string { return a.next.Name() }

// RecordsOutcome forwards the wrapped notifier's mediaserver.SelfRecording.
func (a arrivals) RecordsOutcome() bool { return mediaserver.RecordsOutcome(a.next) }

func (a arrivals) Notify(ctx context.Context, c mediaserver.Change) error {
	err := a.next.Notify(ctx, c)
	a.kick()
	return err
}

// buildRequests creates notifications, requests, issues and discover, and
// hooks them to the library: imports and download events make requests
// follow the library at once.
func buildRequests(_ context.Context, d *Deps, log *slog.Logger) {
	d.Hub = &notify.Hub{DB: d.DB, Log: log}
	tv, _ := d.TMDb.(metadata.SeriesProvider)
	disc, _ := d.TMDb.(metadata.DiscoverProvider)
	bridge := &requests.Bridge{DB: d.DB, Movies: d.Movies, Series: d.Series, Music: d.Music, Wanted: d.Wanted, Downloads: d.Downloads,
		Recycle: d.Recycle, Timeline: d.Timeline, TMDb: d.TMDb, TV: tv, MB: d.MusicBrainz}
	d.Requests = &requests.Service{DB: d.DB, Store: d.Settings, Library: bridge, Hub: d.Hub, Timeline: d.Timeline, Log: log}
	d.Discover = &discover.Service{DB: d.DB, TMDb: disc, Movies: d.TMDb, TV: tv, MB: d.MusicBrainz, Requests: d.Requests, Library: bridge}

	kick := d.Requests.Kick
	d.Movies.Notifier = arrivals{next: d.Movies.Notifier, kick: kick}
	d.Series.Notifier = arrivals{next: d.Series.Notifier, kick: kick}
	d.Music.Notifier = arrivals{next: d.Music.Notifier, kick: kick}
	d.Downloads.Bus.Subscribe(func(_ context.Context, ev downloads.Event) {
		switch ev.Type {
		case downloads.EventGrabbed, downloads.EventFailed, downloads.EventImported, downloads.EventImportFailed, downloads.EventRemoved:
			kick()
		}
	})
	d.Jobs.Register(TaskRequestsSync, func(ctx context.Context, _ *jobs.Job) error {
		n, err := d.Requests.Sync(ctx)
		log.Info("requests synced", "changed", n)
		return err
	}, jobs.KindOptions{MaxAttempts: 1, Timeout: 30 * time.Minute})
	d.Jobs.Register(TaskNotificationsPrune, func(ctx context.Context, _ *jobs.Job) error {
		n, err := d.Hub.Prune(ctx, time.Now().Add(-notificationHistory))
		if n > 0 {
			log.Info("old notifications removed", "count", n)
		}
		return err
	}, jobs.KindOptions{MaxAttempts: 2, Timeout: 10 * time.Minute})
	d.Jobs.Schedule(TaskNotificationsPrune, TaskNotificationsPrune, 24*time.Hour)
}

// attachRequests gives the API handler requests, discover and notifications.
func attachRequests(h *api.Handler, d *Deps) {
	h.Requests, h.Discover, h.Hub = d.Requests, d.Discover, d.Hub
}
