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
	"github.com/mmzlopasa/locarr/server/internal/library/music"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Music task kinds.
const (
	TaskImportAlbum    = "music-import-download"
	TaskRefreshArtist  = "music-refresh-artist"
	TaskRefreshArtists = "music-refresh-artists"
	TaskLinkExisting   = "music-link-existing"
)

type artistArgs struct {
	ArtistID int64 `json:"artistId"`
}

// buildMusic creates the MusicBrainz client, the cover cache and the music
// library, registers its wanted source and jobs, and imports album downloads.
func buildMusic(_ context.Context, d *Deps, cfg config.Config, log *slog.Logger) error {
	mb := musicbrainz.New(cfg.MusicBrainzURL, cfg.CoverArtURL, d.MetaCache)
	mb.Contact = func(ctx context.Context) string { v, _ := settings.Get(ctx, d.Settings, metadata.MusicBrainzContact); return v }
	d.MusicBrainz = mb
	d.CoverArt = &metadata.ImageCache{Dir: filepath.Join(cfg.ConfigDir, "cache", "images", "coverart"), Source: mb, Sizes: musicbrainz.CoverSizes}
	d.Music = &music.Service{DB: d.DB, Settings: d.Settings, Meta: mb, Covers: mb, Quality: d.Quality, Formats: d.Formats, Roots: d.Roots,
		DataRoots: cfg.DataRoots, Timeline: d.Timeline, Recycle: d.Recycle, Downloads: d.Downloads, Notifier: mediaserver.Noop{}, Log: log}
	d.Music.SearchRequested = func(ctx context.Context, ref library.Ref, reason string) {
		if err := d.Wanted.Enqueue(ctx, ref, reason); err != nil {
			log.Warn("queue search", "item", ref.String(), "err", err)
		}
	}
	d.Music.RefreshRequested = func(ctx context.Context, id int64) {
		if _, _, err := d.Jobs.Enqueue(ctx, TaskRefreshArtist, artistArgs{ArtistID: id},
			jobs.EnqueueOptions{UniqueKey: "artist-refresh:" + strconv.FormatInt(id, 10)}); err != nil {
			log.Warn("queue artist refresh", "artist", id, "err", err)
		}
	}
	d.Wanted.Register(music.WantedSource{S: d.Music})
	d.Jobs.Register(TaskImportAlbum, func(ctx context.Context, j *jobs.Job) error {
		var a importArgs
		if err := j.Decode(&a); err != nil {
			return jobs.Permanent(err)
		}
		return d.Music.ImportDownload(ctx, a.DownloadID)
	}, jobs.KindOptions{MaxAttempts: 3, Timeout: 2 * time.Hour})
	d.Jobs.Register(TaskRefreshArtist, func(ctx context.Context, j *jobs.Job) error {
		var a artistArgs
		if err := j.Decode(&a); err != nil {
			return jobs.Permanent(err)
		}
		_, err := d.Music.Refresh(ctx, a.ArtistID, true)
		return err
	}, jobs.KindOptions{MaxAttempts: 2, Timeout: time.Hour})
	d.Jobs.Register(TaskRefreshArtists, func(ctx context.Context, _ *jobs.Job) error {
		n, err := d.Music.RefreshAll(ctx)
		log.Info("artists refreshed", "count", n)
		return err
	}, jobs.KindOptions{MaxAttempts: 1, Timeout: 6 * time.Hour})
	d.Jobs.Register(TaskLinkExisting, func(ctx context.Context, j *jobs.Job) error {
		var a artistArgs
		if err := j.Decode(&a); err != nil {
			return jobs.Permanent(err)
		}
		if _, err := d.Music.Refresh(ctx, a.ArtistID, true); err != nil {
			return err
		}
		res, err := d.Music.LinkExisting(ctx, a.ArtistID)
		log.Info("existing music linked", "artist", a.ArtistID, "albums", res.Albums, "files", res.Files, "unknown", len(res.Unknown))
		return err
	}, jobs.KindOptions{MaxAttempts: 2, Timeout: 2 * time.Hour})
	d.Jobs.Schedule(TaskRefreshArtists, TaskRefreshArtists, 24*time.Hour)

	d.Downloads.Bus.Subscribe(func(ctx context.Context, ev downloads.Event) {
		if ev.Type != downloads.EventCompleted || library.RefFromJSON(ev.Download.ItemRef).Kind != library.RefAlbum {
			return
		}
		if _, _, err := d.Jobs.Enqueue(ctx, TaskImportAlbum, importArgs{DownloadID: ev.Download.ID},
			jobs.EnqueueOptions{UniqueKey: "import:" + strconv.FormatInt(ev.Download.ID, 10)}); err != nil {
			log.Warn("queue album import", "download", ev.Download.ID, "err", err)
		}
	})
	return nil
}

// attachMusic gives the API handler the music library.
func attachMusic(h *api.Handler, d *Deps, log *slog.Logger) {
	h.Music, h.CoverArt = d.Music, d.CoverArt
	h.LinkExisting = func(ctx context.Context, id int64) {
		if _, _, err := d.Jobs.Enqueue(ctx, TaskLinkExisting, artistArgs{ArtistID: id},
			jobs.EnqueueOptions{UniqueKey: "artist-link:" + strconv.FormatInt(id, 10)}); err != nil {
			log.Warn("queue existing music link", "artist", id, "err", err)
		}
	}
}
