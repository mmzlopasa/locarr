package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/api"
	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/config"
	"github.com/mmzlopasa/locarr/server/internal/db"
	_ "github.com/mmzlopasa/locarr/server/internal/downloaders/all" // registers the download clients
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/netx"
	"github.com/mmzlopasa/locarr/server/internal/rootfolders"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/version"
)

// Built-in task kinds.
const (
	TaskPing         = "ping"
	TaskHousekeeping = "housekeeping"
)

// jobHistory is how long finished jobs stay in the task history.
const jobHistory = 14 * 24 * time.Hour

// Deps are the opened storage and services.
type Deps struct {
	DB       *db.DB
	Box      *secrets.Box
	Auth     *auth.Service
	Settings *settings.Store
	Jobs     *jobs.Queue
	Roots    *rootfolders.Service
	// Downloads keeps download clients and tracks downloads.
	Downloads *downloads.Service
	Indexers  *indexers.Service
}

// Close stops the jobs and closes the database.
func (d *Deps) Close() error {
	if d.Jobs != nil {
		d.Jobs.Stop()
	}
	if d.DB != nil {
		return d.DB.Close()
	}
	return nil
}

// OpenStorage creates the config folder, loads (or creates) the secret key and
// opens and migrates the database.
func OpenStorage(ctx context.Context, cfg config.Config) (*db.DB, *secrets.Box, error) {
	if err := db.EnsureDir(cfg.ConfigDir); err != nil {
		return nil, nil, err
	}
	box, err := secrets.LoadOrCreate(cfg.ConfigDir)
	if err != nil {
		return nil, nil, err
	}
	d, err := db.Open(ctx, cfg.DatabasePath())
	if err != nil {
		return nil, nil, err
	}
	if err := d.Migrate(ctx); err != nil {
		_ = d.Close()
		return nil, nil, fmt.Errorf("migrate database: %w", err)
	}
	return d, box, nil
}

// Bootstrap opens storage and builds the services. The job queue is
// registered but not started.
func Bootstrap(ctx context.Context, cfg config.Config, log *slog.Logger, setupCode string) (*Deps, error) {
	d, box, err := OpenStorage(ctx, cfg)
	if err != nil {
		return nil, err
	}
	deps := &Deps{DB: d, Box: box, Auth: auth.NewService(d, box, setupCode), Settings: settings.New(d, box),
		Roots: &rootfolders.Service{DB: d, Roots: cfg.DataRoots}}
	deps.Downloads = downloads.New(d, box, deps.Settings, cfg.DataRoots, log)
	deps.Jobs = jobs.New(d, log, jobs.Options{Concurrency: map[string]int{jobs.DefaultQueue: 2}})
	deps.Indexers = indexers.New(d, box, deps.Settings, log)
	deps.Downloads.Releases = releaseSource{ix: deps.Indexers}
	registerTasks(deps, log)
	if err := registerIndexerTasks(ctx, deps, log); err != nil {
		_ = d.Close()
		return nil, err
	}
	return deps, nil
}

func registerTasks(d *Deps, log *slog.Logger) {
	d.Jobs.Register(TaskPing, func(ctx context.Context, _ *jobs.Job) error {
		// Proves the queue, the scheduler and the database work end to end.
		var one int
		if err := d.DB.R.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
			return err
		}
		log.Debug("ping")
		return nil
	}, jobs.KindOptions{MaxAttempts: 3, Timeout: time.Minute})
	d.Jobs.Register(TaskHousekeeping, func(ctx context.Context, _ *jobs.Job) error {
		sessions, err := d.Auth.PruneSessions(ctx)
		if err != nil {
			return err
		}
		history, err := d.Jobs.Prune(ctx, time.Now().Add(-jobHistory))
		if err != nil {
			return err
		}
		if _, err := d.DB.W.ExecContext(ctx, `PRAGMA optimize`); err != nil {
			return err
		}
		log.Info("housekeeping done", "expired_sessions", sessions, "old_jobs", history)
		return nil
	}, jobs.KindOptions{MaxAttempts: 3, Timeout: 10 * time.Minute})
	d.Jobs.Schedule(TaskPing, TaskPing, 15*time.Minute)
	d.Jobs.Schedule(TaskHousekeeping, TaskHousekeeping, 6*time.Hour)
}

// Serve runs the server until ctx is cancelled. It listens at once (so health
// checks answer while storage opens), then becomes ready.
func Serve(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	started := time.Now().UTC()
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return err
	}
	var current atomic.Pointer[http.Handler]
	starting := startingHandler()
	current.Store(&starting)
	srv := &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { (*current.Load()).ServeHTTP(w, r) }),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelDebug),
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("http listening", "addr", ln.Addr().String(), "version", version.Version, "commit", version.Commit)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	shutdown := func() {
		sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}

	setupCode := cfg.SetupCode
	if setupCode == "" {
		if setupCode, err = auth.NewSetupCode(); err != nil {
			shutdown()
			return err
		}
	}
	deps, err := Bootstrap(ctx, cfg, log, setupCode)
	if err != nil {
		shutdown()
		return err
	}
	defer func() {
		if err := deps.Close(); err != nil {
			log.Error("close", "err", err)
		}
	}()
	if err := deps.Jobs.Start(ctx); err != nil {
		shutdown()
		return err
	}
	// Download tracking stops before the database closes (defers run last-in, first-out).
	trackCtx, stopTracking := context.WithCancel(ctx)
	tracking := make(chan struct{})
	go func() {
		defer close(tracking)
		deps.Downloads.Run(trackCtx)
	}()
	defer func() {
		stopTracking()
		<-tracking
	}()
	proxies, err := netx.ParsePrefixes(cfg.TrustedProxies)
	if err != nil {
		shutdown()
		return err
	}
	opts := RouterOptions{TrustedProxies: proxies, Log: log}
	if cfg.AuthRatePerMin > 0 {
		opts.AuthLimiter = netx.NewLimiter(cfg.AuthRatePerMin, cfg.AuthRateBurst)
		// Fifteen refused passwords or codes within ten minutes: no more tries for an hour.
		opts.AuthBans = netx.NewBanlist(15, 10*time.Minute, time.Hour)
		opts.AuthBans.OnBan = func(ip netip.Addr, until time.Time) {
			log.Warn("client banned from sign-in after repeated failures", "ip", ip.String(), "until", until.UTC().Format(time.RFC3339))
		}
	}
	h := &api.Handler{DB: deps.DB, Auth: deps.Auth, Settings: deps.Settings, Jobs: deps.Jobs, RootFolders: deps.Roots, Downloads: deps.Downloads, Indexers: deps.Indexers,
		ConfigDir: cfg.ConfigDir, DataRoots: cfg.DataRoots, StartedAt: started, Log: log}
	router, err := NewRouter(h, opts)
	if err != nil {
		shutdown()
		return err
	}
	current.Store(&router)
	log.Info("ready", "took_ms", time.Since(started).Milliseconds())
	if required, err := deps.Auth.SetupRequired(ctx); err == nil && required {
		// Printed in the message so it is easy to find with `docker compose logs locarr`.
		log.Warn("first-run setup: open the web interface and enter setup code " + setupCode)
	}

	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err = <-errc:
	}
	shutdown()
	return err
}
