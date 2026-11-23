package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// registerIndexerTasks registers the RSS sync with the configured interval.
func registerIndexerTasks(ctx context.Context, d *Deps, log *slog.Logger) error {
	every, err := settings.Get(ctx, d.Settings, indexers.RSSIntervalMinutes)
	if err != nil {
		return err
	}
	d.Jobs.Register(indexers.TaskRSSSync, func(ctx context.Context, _ *jobs.Job) error {
		res, err := d.Indexers.SyncRSS(ctx)
		if err != nil {
			return err
		}
		log.Info("rss sync done", "indexers", res.Indexers, "fetched", res.Fetched, "new", res.New,
			"pruned", res.Pruned, "errors", res.Errors)
		return nil
	}, jobs.KindOptions{MaxAttempts: 2, Timeout: 10 * time.Minute})
	d.Jobs.Schedule(indexers.TaskRSSSync, indexers.TaskRSSSync, time.Duration(max(every, 10))*time.Minute)
	return nil
}

// streamPaths are API endpoints that stream (Server-Sent Events).
var streamPaths = map[string]bool{"/api/v1/search/stream": true, "/api/v1/system/logs/stream": true}

// flushStream flushes every write of a streaming endpoint so that events
// reach the client as they happen, and tells proxies not to buffer it.
func flushStream(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !streamPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("X-Accel-Buffering", "no")
		next.ServeHTTP(&flushWriter{ResponseWriter: w, rc: http.NewResponseController(w)}, r)
	})
}

type flushWriter struct {
	http.ResponseWriter
	rc *http.ResponseController
}

func (f *flushWriter) Write(p []byte) (int, error) {
	n, err := f.ResponseWriter.Write(p)
	if err == nil {
		_ = f.rc.Flush()
	}
	return n, err
}

func (f *flushWriter) Unwrap() http.ResponseWriter { return f.ResponseWriter }
