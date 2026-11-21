package app

import (
	"context"
	"log/slog"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/api"
	"github.com/mmzlopasa/locarr/server/internal/config"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/movies"
	"github.com/mmzlopasa/locarr/server/internal/library/recycle"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/metadata/tmdb"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
	"github.com/mmzlopasa/locarr/server/internal/wanted"
)

// Library task kinds.
const (
	TaskImportDownload = "import-download"
	TaskRefreshMovies  = "refresh-movies"
	TaskImportLists    = "import-lists-sync"
	TaskRecycleCleanup = "recycle-bin-cleanup"
)

type importArgs struct {
	DownloadID int64 `json:"downloadId"`
}

// buildLibrary creates the metadata provider, timeline, recycle bin, wanted
// orchestration and the movie library, and registers their jobs.
func buildLibrary(ctx context.Context, d *Deps, cfg config.Config, log *slog.Logger) error {
	d.MetaCache = &metadata.Cache{DB: d.DB}
	client := tmdb.New(cfg.TMDbURL, func(ctx context.Context) (string, error) {
		key, _, err := d.Settings.Secret(ctx, settings.TMDbAPIKey)
		return key, err
	}, d.MetaCache)
	client.Language = func(ctx context.Context) string { v, _ := settings.Get(ctx, d.Settings, metadata.Language); return v }
	client.Region = func(ctx context.Context) string { v, _ := settings.Get(ctx, d.Settings, metadata.Region); return v }
	d.TMDb = client
	d.Images = &metadata.ImageCache{Dir: filepath.Join(cfg.ConfigDir, "cache", "images", "tmdb"), Source: client}
	d.Timeline = &timeline.Store{DB: d.DB}
	d.Recycle = &recycle.Bin{DB: d.DB, Settings: d.Settings, Roots: cfg.DataRoots}
	d.Wanted = &wanted.Service{DB: d.DB, Indexers: d.Indexers, Downloads: d.Downloads, Quality: d.Quality, Formats: d.Formats,
		Timeline: d.Timeline, Settings: d.Settings, Jobs: d.Jobs, Log: log}
	d.Movies = &movies.Service{DB: d.DB, Settings: d.Settings, Meta: client, Quality: d.Quality, Formats: d.Formats, Roots: d.Roots,
		DataRoots: cfg.DataRoots, Timeline: d.Timeline, Recycle: d.Recycle, Downloads: d.Downloads, Notifier: mediaserver.Noop{}, Log: log}
	d.Movies.SearchRequested = func(ctx context.Context, ref library.Ref, reason string) {
		if err := d.Wanted.Enqueue(ctx, ref, reason); err != nil {
			log.Warn("queue search", "item", ref.String(), "err", err)
		}
	}
	d.Wanted.Register(movies.WantedSource{S: d.Movies})
	if err := d.Wanted.RegisterTasks(ctx); err != nil {
		return err
	}
	d.Jobs.Register(TaskImportDownload, func(ctx context.Context, j *jobs.Job) error {
		var a importArgs
		if err := j.Decode(&a); err != nil {
			return jobs.Permanent(err)
		}
		return d.Movies.ImportDownload(ctx, a.DownloadID)
	}, jobs.KindOptions{MaxAttempts: 3, Timeout: 2 * time.Hour})
	d.Jobs.Register(TaskRefreshMovies, func(ctx context.Context, _ *jobs.Job) error {
		n, err := d.Movies.RefreshAll(ctx)
		log.Info("movies refreshed", "count", n)
		if err != nil {
			return jobs.Permanent(err)
		}
		return nil
	}, jobs.KindOptions{MaxAttempts: 1, Timeout: 2 * time.Hour})
	d.Jobs.Register(TaskImportLists, func(ctx context.Context, _ *jobs.Job) error {
		res, err := d.Movies.SyncLists(ctx)
		added := 0
		for _, r := range res {
			added += len(r.Added)
		}
		log.Info("import lists synced", "lists", len(res), "added", added)
		return err
	}, jobs.KindOptions{MaxAttempts: 1, Timeout: 30 * time.Minute})
	d.Jobs.Register(TaskRecycleCleanup, func(ctx context.Context, _ *jobs.Job) error {
		n, err := d.Recycle.Cleanup(ctx)
		if n > 0 {
			log.Info("recycle bin cleaned up", "entries", n)
		}
		if _, perr := d.MetaCache.Prune(ctx); perr != nil {
			log.Warn("prune metadata cache", "err", perr)
		}
		return err
	}, jobs.KindOptions{MaxAttempts: 2, Timeout: 30 * time.Minute})
	d.Jobs.Schedule(TaskRefreshMovies, TaskRefreshMovies, 24*time.Hour)
	d.Jobs.Schedule(TaskImportLists, TaskImportLists, 6*time.Hour)
	d.Jobs.Schedule(TaskRecycleCleanup, TaskRecycleCleanup, 24*time.Hour)

	// Completed downloads are imported by a job (bus handlers must be quick);
	// failed ones are blocklisted and searched again.
	d.Downloads.Bus.Subscribe(func(ctx context.Context, ev downloads.Event) {
		switch ev.Type {
		case downloads.EventCompleted:
			if library.RefFromJSON(ev.Download.ItemRef).Kind != library.RefMovie {
				return
			}
			if _, _, err := d.Jobs.Enqueue(ctx, TaskImportDownload, importArgs{DownloadID: ev.Download.ID},
				jobs.EnqueueOptions{UniqueKey: "import:" + strconv.FormatInt(ev.Download.ID, 10)}); err != nil {
				log.Warn("queue import", "download", ev.Download.ID, "err", err)
			}
		case downloads.EventFailed:
			d.Wanted.HandleDownloadEvent(ctx, ev)
		}
	})
	return nil
}

// NewHandler builds the API handler for the services.
func NewHandler(d *Deps, cfg config.Config, started time.Time, log *slog.Logger) *api.Handler {
	return &api.Handler{DB: d.DB, Auth: d.Auth, Settings: d.Settings, Jobs: d.Jobs, RootFolders: d.Roots, Downloads: d.Downloads,
		Indexers: d.Indexers, Quality: d.Quality, Formats: d.Formats, Movies: d.Movies, Wanted: d.Wanted, Timeline: d.Timeline,
		Recycle: d.Recycle, Images: d.Images, TMDb: d.TMDb, MetaCache: d.MetaCache,
		ConfigDir: cfg.ConfigDir, DataRoots: cfg.DataRoots, StartedAt: started, Log: log}
}

// runBackground starts the RSS consumer; the returned function waits for it.
func runBackground(ctx context.Context, d *Deps) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.Wanted.RunRSS(ctx)
	}()
	return func() { <-done }
}
