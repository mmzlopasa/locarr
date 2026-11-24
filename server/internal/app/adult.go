package app

import (
	"context"
	"log/slog"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/adult"
	"github.com/mmzlopasa/locarr/server/internal/adult/meta"
	"github.com/mmzlopasa/locarr/server/internal/config"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver/streamero"
	"github.com/mmzlopasa/locarr/server/internal/notify"
)

// Adult tasks.
const (
	TaskImportAdult  = "adult-import"
	TaskRefreshAdult = "adult-refresh"
)

// buildAdult wires the optional adult library (D-100). It is always built
// (so enabling needs no restart) but does nothing while the setting is off.
func buildAdult(_ context.Context, d *Deps, cfg config.Config, log *slog.Logger) {
	stashKey := func(ctx context.Context) (string, error) {
		v, _, err := d.Settings.Secret(ctx, adult.StashDBKey)
		return v, err
	}
	tpdbKey := func(ctx context.Context) (string, error) {
		v, _, err := d.Settings.Secret(ctx, adult.TPDBKey)
		return v, err
	}
	a := &adult.Service{DB: d.DB, Settings: d.Settings, Quality: d.Quality, Downloads: d.Downloads, Wanted: d.Wanted, Timeline: d.Timeline,
		Recycle: d.Recycle, Jobs: d.Jobs, Notifier: streamero.Notifier{S: d.Streamero}, DataRoots: cfg.DataRoots,
		ImageDir: filepath.Join(cfg.ConfigDir, "adult", "images"), Log: log,
		Providers: map[string]meta.Provider{
			meta.StashDB: meta.NewStashDB(cfg.StashDBURL, stashKey, d.MetaCache),
			meta.TPDB:    meta.NewTPDB(cfg.TPDBURL, tpdbKey, d.MetaCache),
		}}
	a.Events = func(ctx context.Context, typ, title, message, ref string) {
		if h := d.Hub; h != nil && a.On(ctx) {
			// No recipients: only channels that chose adult events get it.
			h.Publish(ctx, notify.Event{Type: notify.Type(typ), Title: title, Message: message, MediaKind: "adult", ItemRef: ref})
		}
	}
	d.Adult = a
	if d.Stats != nil {
		d.Stats.AdultTotal = a.StatsTotal
	}
	d.Wanted.Register(adult.Source{S: a})
	d.Wanted.Register(adult.Source{S: a, Movie: true})

	d.Jobs.Register(TaskImportAdult, func(ctx context.Context, j *jobs.Job) error {
		var args importArgs
		if err := j.Decode(&args); err != nil {
			return jobs.Permanent(err)
		}
		return a.ImportDownload(ctx, args.DownloadID)
	}, jobs.KindOptions{MaxAttempts: 3, Timeout: 2 * time.Hour})
	d.Jobs.Register(TaskRefreshAdult, func(ctx context.Context, _ *jobs.Job) error {
		return a.RefreshAll(ctx)
	}, jobs.KindOptions{MaxAttempts: 1, Timeout: 2 * time.Hour})
	d.Jobs.Schedule(TaskRefreshAdult, TaskRefreshAdult, 12*time.Hour)

	d.Downloads.Bus.Subscribe(func(ctx context.Context, ev downloads.Event) {
		if ev.Type != downloads.EventCompleted || !library.IsAdult(library.RefFromJSON(ev.Download.ItemRef).Kind) {
			return
		}
		if _, _, err := d.Jobs.Enqueue(ctx, TaskImportAdult, importArgs{DownloadID: ev.Download.ID},
			jobs.EnqueueOptions{UniqueKey: "import:" + strconv.FormatInt(ev.Download.ID, 10)}); err != nil {
			log.Warn("queue adult import", "download", ev.Download.ID, "err", err)
		}
	})
}
