package downloads

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Tracking settings.
var (
	// PollSeconds is how often enabled clients are polled (10-3600).
	PollSeconds = settings.Key[int]{Name: "downloads.pollSeconds", Default: 30}
	// StalledHours marks a torrent that has downloaded nothing, with no seeds,
	// for this long as failed (0 = never).
	StalledHours = settings.Key[int]{Name: "downloads.stalledHours", Default: 24}
)

// watchPoll is the poll interval while someone has the queue page open.
const (
	watchPoll   = 5 * time.Second
	watchWindow = 45 * time.Second
	testEvery   = 5 * time.Minute
)

// Service owns download clients, path mappings, tracking, the blocklist and grabs.
type Service struct {
	DB        *db.DB
	Box       *secrets.Box
	Settings  *settings.Store
	DataRoots []string
	Log       *slog.Logger
	Bus       *Bus
	// HTTP options for client connections and release fetches.
	AllowLinkLocal bool
	// NewClient builds a client; tests replace it with fakes.
	NewClient func(impl downloaders.Implementation, cfg downloaders.Config) (downloaders.Client, error)
	Now       func() time.Time

	mu         sync.Mutex
	cache      map[int64]downloaders.Client
	snapshots  map[int64]*Snapshot
	categories map[int64]map[string]bool // categories known to exist per client
	rr         map[string]int
	watchUntil time.Time
	wake       chan struct{}
	pollMu     sync.Mutex
	block      *blockIndex
}

// New returns a Service.
func New(d *db.DB, box *secrets.Box, st *settings.Store, dataRoots []string, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{DB: d, Box: box, Settings: st, DataRoots: dataRoots, Log: log, Bus: &Bus{log: log}}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) init() {
	if s.cache == nil {
		s.cache = map[int64]downloaders.Client{}
		s.snapshots = map[int64]*Snapshot{}
		s.categories = map[int64]map[string]bool{}
		s.rr = map[string]int{}
		s.wake = make(chan struct{}, 1)
	}
	if s.Bus == nil {
		s.Bus = &Bus{log: s.Log}
	}
}

// forget drops cached connections and status for a client after a change.
func (s *Service) forget(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	delete(s.cache, id)
	delete(s.categories, id)
	delete(s.snapshots, id)
}

func (s *Service) build(impl downloaders.Implementation, cfg downloaders.Config) (downloaders.Client, error) {
	if cfg.HTTP == nil {
		cfg.HTTP = downloaders.NewHTTPClient(downloaders.HTTPOptions{AllowSelfSigned: cfg.AllowSelfSigned, AllowLinkLocal: s.AllowLinkLocal})
	}
	if s.NewClient != nil {
		return s.NewClient(impl, cfg)
	}
	return impl.New(cfg)
}

func configOf(c Client, password, apiKey string) downloaders.Config {
	return downloaders.Config{Host: c.Host, Port: c.Port, UseTLS: c.UseTLS, URLBase: c.URLBase, Username: c.Username,
		Password: password, APIKey: apiKey, Directory: c.Directory, AllowSelfSigned: c.AllowSelfSigned}
}

// connect returns the (cached) client connection for a stored client.
func (s *Service) connect(ctx context.Context, c Client) (downloaders.Client, error) {
	s.mu.Lock()
	s.init()
	if cl, ok := s.cache[c.ID]; ok {
		s.mu.Unlock()
		return cl, nil
	}
	s.mu.Unlock()
	impl, ok := downloaders.Lookup(c.Implementation)
	if !ok {
		return nil, invalid("unknown implementation %q", c.Implementation)
	}
	pw, key, err := s.secrets(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	cl, err := s.build(impl, configOf(c, pw, key))
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[c.ID] = cl
	s.mu.Unlock()
	return cl, nil
}

// Watch tells the tracker someone is looking at the queue: it polls every few
// seconds for a short while.
func (s *Service) Watch() {
	s.mu.Lock()
	s.init()
	was := s.now().Before(s.watchUntil)
	s.watchUntil = s.now().Add(watchWindow)
	s.mu.Unlock()
	if !was {
		s.Kick()
	}
}

// Kick asks the tracker to poll now.
func (s *Service) Kick() {
	s.mu.Lock()
	s.init()
	w := s.wake
	s.mu.Unlock()
	select {
	case w <- struct{}{}:
	default:
	}
}

func (s *Service) interval(ctx context.Context) time.Duration {
	s.mu.Lock()
	watching := s.now().Before(s.watchUntil)
	s.mu.Unlock()
	if watching {
		return watchPoll
	}
	n, err := settings.Get(ctx, s.Settings, PollSeconds)
	if err != nil || n < 10 {
		n = PollSeconds.Default
	}
	return time.Duration(min(n, 3600)) * time.Second
}

// Run polls the clients until ctx ends. It is a plain loop rather than a
// queued job so a fast poll does not fill the task history (D-012).
func (s *Service) Run(ctx context.Context) {
	s.mu.Lock()
	s.init()
	wake := s.wake
	s.mu.Unlock()
	for {
		if err := s.Poll(ctx); err != nil && ctx.Err() == nil {
			s.Log.Warn("download tracking", "err", err)
		}
		t := time.NewTimer(s.interval(ctx))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-wake:
			t.Stop()
		case <-t.C:
		}
	}
}
