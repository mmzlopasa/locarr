package app

import (
	"context"
	"log/slog"

	"github.com/mmzlopasa/locarr/server/internal/api"
	"github.com/mmzlopasa/locarr/server/internal/config"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/subtitles"
	subsync "github.com/mmzlopasa/locarr/server/internal/subtitles/sync"
	"github.com/mmzlopasa/locarr/server/internal/version"
)

// buildSubtitles creates the subtitle service (P7), registers its jobs and
// subscribes it to movie and episode imports.
func buildSubtitles(d *Deps, cfg config.Config, log *slog.Logger) {
	ff, ok := subsync.Find(cfg.FFmpeg)
	if !ok {
		log.Warn("ffmpeg not found: subtitle sync against the audio is not available", "path", cfg.FFmpeg)
	}
	d.Subtitles = &subtitles.Service{DB: d.DB, Box: d.Box, Settings: d.Settings, Jobs: d.Jobs, Timeline: d.Timeline, Recycle: d.Recycle,
		DataRoots: cfg.DataRoots, Log: log, FFmpeg: ff, URLs: cfg.SubtitleURLs, UserAgent: "Locarr v" + version.Version}
	d.Subtitles.RegisterTasks()
	imported := func(ctx context.Context, refs []library.Ref) { d.Subtitles.Imported(ctx, refs) }
	d.Movies.Imported = imported
	d.Series.Imported = imported
}

func attachSubtitles(h *api.Handler, d *Deps) { h.Subtitles = d.Subtitles }
