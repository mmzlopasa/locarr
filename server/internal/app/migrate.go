package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/api"
	"github.com/mmzlopasa/locarr/server/internal/config"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/migrate"
)

// buildMigrate creates the importer of existing setups (P10) and its jobs.
func buildMigrate(d *Deps, cfg config.Config, log *slog.Logger) {
	l := &migrate.Locarr{DB: d.DB, Box: d.Box, Auth: d.Auth, Settings: d.Settings, Roots: d.Roots, Quality: d.Quality, Formats: d.Formats,
		Downloads: d.Downloads, Indexers: d.Indexers, Movies: d.Movies, Series: d.Series, Music: d.Music, Requests: d.Requests,
		DataRoots: cfg.DataRoots}
	if d.Subtitles != nil {
		l.Subtitles = migrate.SubtitlesAdapter{S: d.Subtitles}
	}
	d.Migrate = &migrate.Service{L: l, Jobs: d.Jobs, Log: log, Hub: d.Hub,
		// The applications live on the same network as the indexers: the
		// administrator's outbound address policy applies to them too.
		Policy: func(ctx context.Context) (httpx.Policy, error) { return d.Indexers.HTTPPolicy(ctx) }}
	run := func(f func(context.Context, int64) error) jobs.Handler {
		return func(ctx context.Context, j *jobs.Job) error {
			var a struct {
				RunID int64 `json:"runId"`
			}
			if err := j.Decode(&a); err != nil {
				return jobs.Permanent(err)
			}
			return f(ctx, a.RunID)
		}
	}
	d.Jobs.Register(migrate.TaskAnalyse, run(d.Migrate.RunAnalyse), jobs.KindOptions{Queue: "migrate", MaxAttempts: 1, Timeout: 2 * time.Hour})
	d.Jobs.Register(migrate.TaskApply, run(d.Migrate.RunApply), jobs.KindOptions{Queue: "migrate", MaxAttempts: 1, Timeout: 6 * time.Hour})
	d.Jobs.Register(migrate.TaskRefresh, d.Migrate.RunRefresh, jobs.KindOptions{Queue: "migrate-refresh", MaxAttempts: 1, Timeout: 24 * time.Hour})
}

func attachMigrate(h *api.Handler, d *Deps) { h.Migrate = d.Migrate }
