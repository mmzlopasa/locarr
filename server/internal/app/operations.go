package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/api"
	"github.com/mmzlopasa/locarr/server/internal/backup"
	"github.com/mmzlopasa/locarr/server/internal/board"
	"github.com/mmzlopasa/locarr/server/internal/config"
	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/health"
	"github.com/mmzlopasa/locarr/server/internal/history"
	"github.com/mmzlopasa/locarr/server/internal/housekeeping"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/movies"
	"github.com/mmzlopasa/locarr/server/internal/notify"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/stats"
	"github.com/mmzlopasa/locarr/server/internal/version"
)

// Operations task kinds (P11).
const (
	TaskBoardRebuild = "board-rebuild"
	TaskHealthCheck  = "health-check"
	TaskBackup       = "backup"
)

// buildOperations creates the board, history, health checks, backups, stats
// and housekeeping (P11) and registers their jobs. It runs after every
// library and the notification hub exist.
func buildOperations(_ context.Context, d *Deps, cfg config.Config, log *slog.Logger) {
	d.Board = &board.Service{DB: d.DB, Timeline: d.Timeline, Movies: d.Movies, Series: d.Series, Music: d.Music, Log: log}
	d.Timeline.OnRecord = func(ref string) { d.Board.Mark(ref) }
	d.Downloads.Bus.Subscribe(func(ctx context.Context, ev downloads.Event) {
		refs := []string{library.RefFromJSON(ev.Download.ItemRef).String()}
		rows, err := d.DB.R.QueryContext(ctx, `SELECT item_ref FROM download_refs WHERE download_id = ?`, ev.Download.ID)
		if err == nil {
			for rows.Next() {
				var r string
				if rows.Scan(&r) == nil {
					refs = append(refs, r)
				}
			}
			_ = rows.Close()
		}
		d.Board.Mark(refs...)
	})
	d.Jobs.Register(TaskBoardRebuild, func(ctx context.Context, _ *jobs.Job) error {
		start := time.Now()
		n, err := d.Board.Rebuild(ctx)
		if err == nil {
			log.Info("board rebuilt", "items", n, "took_ms", time.Since(start).Milliseconds())
		}
		return err
	}, jobs.KindOptions{MaxAttempts: 2, Timeout: 30 * time.Minute})
	d.Jobs.Schedule(TaskBoardRebuild, TaskBoardRebuild, board.RebuildInterval)

	d.History = &history.Store{DB: d.DB}

	// Health checks; new warnings and errors go to the channels once.
	d.Version = &health.VersionChecker{Settings: d.Settings, Current: version.Version, URL: cfg.ReleasesURL,
		Fetch: func(ctx context.Context, url string) (*health.Fetched, error) {
			pol := httpx.Policy{BlockPrivate: true}
			if url != health.ReleasesURL {
				// Tests point it at a local server.
				pol = httpx.Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}}
			}
			c, err := httpx.New(httpx.Options{Policy: pol, Timeout: 15 * time.Second, MaxBytes: 1 << 20})
			if err != nil {
				return nil, err
			}
			res, err := c.Get(ctx, url, http.Header{"Accept": {"application/vnd.github+json"}})
			if err != nil {
				return nil, err
			}
			return &health.Fetched{Status: res.Status, Header: res.Header, Body: res.Body}, nil
		}}
	d.Health = &health.Service{Log: log, OnNew: func(ctx context.Context, is health.Issue) {
		if d.Hub == nil {
			return
		}
		d.Hub.Publish(ctx, notify.Event{Type: notify.HealthWarning, Title: "Locarr", Message: is.Message,
			Params: map[string]any{"source": is.Key, "code": is.Code, "severity": is.Severity}})
	}}
	d.Health.RegisterStandard(&health.Deps{DB: d.DB, Settings: d.Settings, Indexers: d.Indexers, Counts: d.Timeline.Counts,
		Downloads: d.Downloads, Roots: d.Roots, Metadata: d.MetaHealth, Version: d.Version,
		TMDbConfigured: func(ctx context.Context) bool {
			ok, _ := d.Settings.Configured(ctx, settings.TMDbAPIKey)
			return ok
		}})
	d.Streamero.Health = func(_ context.Context, ok bool, reason string) {
		d.Health.SetStatus("streamero", ok, health.StreameroIssue(reason))
	}
	d.Jobs.Register(TaskHealthCheck, func(ctx context.Context, _ *jobs.Job) error {
		d.Health.Run(ctx)
		return nil
	}, jobs.KindOptions{MaxAttempts: 1, Timeout: 5 * time.Minute})
	d.Jobs.Schedule(TaskHealthCheck, TaskHealthCheck, 5*time.Minute)

	// Backups: the job runs hourly and makes a backup when the interval has passed.
	d.Backups = &backup.Service{DB: d.DB, ConfigDir: cfg.ConfigDir, Version: version.Version, Commit: version.Commit}
	d.Jobs.Register(TaskBackup, func(ctx context.Context, _ *jobs.Job) error {
		made, err := scheduledBackup(ctx, d)
		if err != nil {
			d.Health.SetStatus("backup", false, health.BackupIssue(err.Error()))
			return err
		}
		d.Health.SetStatus("backup", true, health.Issue{})
		if made != "" {
			log.Info("scheduled backup made", "name", made)
		}
		return nil
	}, jobs.KindOptions{MaxAttempts: 1, Timeout: time.Hour})
	d.Jobs.Schedule(TaskBackup, TaskBackup, time.Hour)

	d.Housekeeping = &housekeeping.Service{DB: d.DB, Settings: d.Settings, ImageDir: filepath.Join(cfg.ConfigDir, "cache", "images")}

	d.Stats = &stats.Service{DB: d.DB, Indexers: d.Indexers, Roots: d.Roots, Health: d.Health.Counts,
		WantedMovies: func(ctx context.Context) (int, error) {
			_, n, err := d.Movies.List(ctx, movies.ListFilter{Status: movies.StatusMissing, Limit: 1})
			return n, err
		}}
}

// scheduledBackup makes a backup when the newest scheduled one is older than
// the interval, then prunes old ones. It returns the new backup's name.
func scheduledBackup(ctx context.Context, d *Deps) (string, error) {
	hours, _ := settings.Get(ctx, d.Settings, settings.BackupIntervalHours)
	if hours <= 0 {
		return "", nil
	}
	list, err := d.Backups.List()
	if err != nil {
		return "", err
	}
	for _, b := range list {
		if b.Kind == backup.KindScheduled && time.Since(b.CreatedAt) < time.Duration(hours)*time.Hour-5*time.Minute {
			return "", nil
		}
	}
	in, err := d.Backups.Create(ctx, backup.KindScheduled)
	if err != nil {
		return "", err
	}
	keep, _ := settings.Get(ctx, d.Settings, settings.BackupKeep)
	_, err = d.Backups.Prune(max(keep, 1))
	return in.Name, err
}

// housekeepingExtra runs the retention, image cache and database compaction
// part of the housekeeping task.
func housekeepingExtra(ctx context.Context, d *Deps, log *slog.Logger) error {
	if d.Housekeeping == nil {
		return nil
	}
	r, err := d.Housekeeping.Run(ctx)
	log.Info("retention applied", "timeline", r.Timeline, "search_log", r.SearchLog, "download_events", r.DownloadEvents,
		"releases_seen", r.ReleasesSeen, "images", r.ImageFiles, "image_bytes", r.ImageBytes, "vacuumed", r.Vacuumed)
	return err
}

// boardActions are the board's bulk actions through the other services.
func boardActions(d *Deps) board.Actions {
	return board.Actions{
		Search: func(ctx context.Context, ref library.Ref) error {
			d.Wanted.ResetBackoff(ctx, ref)
			return d.Wanted.Enqueue(ctx, ref, "board")
		},
		SearchSubtitles: func(ctx context.Context, ref library.Ref) error {
			if d.Subtitles == nil {
				return board.ErrNotSupported
			}
			_, err := d.Subtitles.QueueSearch(ctx, []library.Ref{ref}, "manual")
			return err
		},
		RemoveAndBlock: func(ctx context.Context, id int64, msg string) error {
			return d.Downloads.Remove(ctx, id, downloads.RemoveOptions{Blocklist: true, Message: msg})
		},
		RetryImport: func(ctx context.Context, id int64) error { return retryImport(ctx, d, id) },
	}
}

// retryImport imports a finished download again, or polls the clients when
// its files were not visible (after a path mapping was fixed).
func retryImport(ctx context.Context, d *Deps, id int64) error {
	dl, err := d.Downloads.Get(ctx, id)
	if err != nil {
		return err
	}
	switch dl.Status {
	case downloads.StatusCompleted:
		task := TaskImportDownload
		if library.RefFromJSON(dl.ItemRef).Kind == library.RefAlbum {
			task = TaskImportAlbum
		}
		_, _, err := d.Jobs.Enqueue(ctx, task, importArgs{DownloadID: id},
			jobs.EnqueueOptions{UniqueKey: "import:" + strconv.FormatInt(id, 10)})
		return err
	case downloads.StatusActive:
		// Polling resolves the path again; a finished download then moves on to the import.
		if err := d.Downloads.Poll(ctx); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("%w: the download is %s", board.ErrNotSupported, dl.Status)
}

// attachOperations gives the API handler the P11 services.
func attachOperations(h *api.Handler, d *Deps) {
	h.Board, h.BoardActions, h.History, h.Health, h.Backups, h.Stats, h.Logs =
		d.Board, boardActions(d), d.History, d.Health, d.Backups, d.Stats, d.Logs
	h.MaxSchema = db.LatestVersion()
}
