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
	"github.com/mmzlopasa/locarr/server/internal/library/series"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/metadata/tmdb"
	"github.com/mmzlopasa/locarr/server/internal/metadata/tvdb"
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
	TaskRefreshSeries  = "refresh-series"
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
	client.Health = d.MetaHealth
	client.Language = func(ctx context.Context) string {
		if l := metadata.LanguageFrom(ctx); l != "" {
			return l
		}
		v, _ := settings.Get(ctx, d.Settings, metadata.Language)
		return v
	}
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

	// Series (P5): TMDb TV, optionally TVDB, episodes and season packs.
	d.TVDB = tvdb.New(cfg.TVDBURL, func(ctx context.Context) (tvdb.Credentials, error) {
		key, _, err := d.Settings.Secret(ctx, settings.TVDBAPIKey)
		if err != nil {
			return tvdb.Credentials{}, err
		}
		pin, _, err := d.Settings.Secret(ctx, settings.TVDBPin)
		return tvdb.Credentials{Key: key, PIN: pin}, err
	}, d.MetaCache)
	d.TVDB.Language = client.Language
	d.TVDB.Health = d.MetaHealth
	d.Series = &series.Service{DB: d.DB, Settings: d.Settings, TMDb: client, TVDB: d.TVDB, Quality: d.Quality, Formats: d.Formats,
		Roots: d.Roots, DataRoots: cfg.DataRoots, Timeline: d.Timeline, Recycle: d.Recycle, Downloads: d.Downloads,
		Notifier: mediaserver.Noop{}, Log: log}
	d.Series.SearchRequested = func(ctx context.Context, ref library.Ref, reason string) {
		refs := []library.Ref{ref}
		if ref.Kind == library.RefSeries {
			var err error
			if refs, err = d.Series.SearchTargets(ctx, ref.ID); err != nil {
				log.Warn("series search targets", "series", ref.ID, "err", err)
				return
			}
		}
		for _, r := range refs {
			if err := d.Wanted.Enqueue(ctx, r, reason); err != nil {
				log.Warn("queue search", "item", r.String(), "err", err)
			}
		}
	}
	d.Series.MakeDue = d.Wanted.ResetBackoff
	d.Wanted.Register(series.EpisodeSource{S: d.Series})
	d.Wanted.Register(series.SeasonSource{S: d.Series})
	if err := d.Wanted.RegisterTasks(ctx); err != nil {
		return err
	}
	d.Jobs.Register(TaskImportDownload, func(ctx context.Context, j *jobs.Job) error {
		var a importArgs
		if err := j.Decode(&a); err != nil {
			return jobs.Permanent(err)
		}
		dl, err := d.Downloads.Get(ctx, a.DownloadID)
		if err != nil {
			return jobs.Permanent(err)
		}
		switch library.RefFromJSON(dl.ItemRef).Kind {
		case library.RefEpisode, library.RefSeason, library.RefSeries:
			return d.Series.ImportDownload(ctx, a.DownloadID)
		}
		return d.Movies.ImportDownload(ctx, a.DownloadID)
	}, jobs.KindOptions{MaxAttempts: 3, Timeout: 2 * time.Hour})
	d.Jobs.Register(TaskRefreshSeries, func(ctx context.Context, _ *jobs.Job) error {
		n, err := d.Series.RefreshAll(ctx)
		log.Info("series refreshed", "count", n)
		if err != nil {
			return jobs.Permanent(err)
		}
		return nil
	}, jobs.KindOptions{MaxAttempts: 1, Timeout: 4 * time.Hour})
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
	d.Jobs.Schedule(TaskRefreshSeries, TaskRefreshSeries, 12*time.Hour)
	d.Jobs.Schedule(TaskImportLists, TaskImportLists, 6*time.Hour)
	d.Jobs.Schedule(TaskRecycleCleanup, TaskRecycleCleanup, 24*time.Hour)

	// Completed downloads are imported by a job (bus handlers must be quick);
	// failed ones are blocklisted and searched again.
	d.Downloads.Bus.Subscribe(func(ctx context.Context, ev downloads.Event) {
		switch ev.Type {
		case downloads.EventCompleted:
			switch library.RefFromJSON(ev.Download.ItemRef).Kind {
			case library.RefMovie, library.RefEpisode, library.RefSeason, library.RefSeries:
			default:
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
	h := &api.Handler{DB: d.DB, Auth: d.Auth, Settings: d.Settings, Jobs: d.Jobs, RootFolders: d.Roots, Downloads: d.Downloads,
		Indexers: d.Indexers, Quality: d.Quality, Formats: d.Formats, Movies: d.Movies, Series: d.Series, TVDB: d.TVDB, Wanted: d.Wanted, Timeline: d.Timeline,
		Recycle: d.Recycle, Images: d.Images, TMDb: d.TMDb, MetaCache: d.MetaCache,
		ConfigDir: cfg.ConfigDir, DataRoots: cfg.DataRoots, StartedAt: started, Log: log}
	attachMusic(h, d, log)
	attachSubtitles(h, d)
	attachRequests(h, d)
	attachMediaServer(h, d)
	attachOperations(h, d)
	attachMigrate(h, d)
	h.Adult = d.Adult
	return h
}

// runBackground starts the RSS consumer and the request sync; the returned
// function waits for them.
func runBackground(ctx context.Context, d *Deps) func() {
	done := make(chan struct{})
	reqs := make(chan struct{})
	go func() {
		defer close(done)
		d.Wanted.RunRSS(ctx)
	}()
	go func() {
		defer close(reqs)
		d.Requests.Run(ctx)
	}()
	boardDone := make(chan struct{})
	go func() {
		defer close(boardDone)
		if d.Board != nil {
			d.Board.Run(ctx)
		}
	}()
	return func() { <-done; <-reqs; <-boardDone }
}
