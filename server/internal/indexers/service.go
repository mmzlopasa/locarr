package indexers

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Indexer-wide settings.
var (
	RSSIntervalMinutes   = settings.Key[int]{Name: "indexers.rssSyncIntervalMinutes", Default: 15}
	ReleaseRetentionDays = settings.Key[int]{Name: "indexers.releaseRetentionDays", Default: 14}
	SearchTimeoutSeconds = settings.Key[int]{Name: "indexers.searchTimeoutSeconds", Default: 45}
	SearchCacheSeconds   = settings.Key[int]{Name: "indexers.searchCacheSeconds", Default: 120}
	AllowedHosts         = settings.Key[[]string]{Name: "indexers.allowedHosts", Default: []string{}}
	BlockPrivateNetworks = settings.Key[bool]{Name: "indexers.blockPrivateNetworks", Default: false}
)

// TaskRSSSync is the scheduled RSS sync task.
const TaskRSSSync = "rss-sync"

// capsMaxAge is how long stored capabilities are trusted before a search refreshes them.
const capsMaxAge = 7 * 24 * time.Hour

// Service is the indexer subsystem.
type Service struct {
	DB       *db.DB
	Box      *secrets.Box
	Settings *settings.Store
	Log      *slog.Logger
	Now      func() time.Time
	// PolicyOverride replaces the address policy from the settings (tests).
	PolicyOverride *httpx.Policy

	limiter *limiter
	cache   *cache
	bus     *bus

	mu      sync.Mutex
	clients map[int64]*cachedClient
}

type cachedClient struct {
	updated time.Time
	policy  string
	c       *znab.Client
}

// New returns a Service.
func New(d *db.DB, box *secrets.Box, st *settings.Store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{DB: d, Box: box, Settings: st, Log: log, Now: time.Now, limiter: newLimiter(),
		cache: newCache(), bus: newBus(), clients: map[int64]*cachedClient{}}
}

func (s *Service) now() time.Time { return s.Now().UTC() }

// HTTPPolicy is the outbound address policy administrators set for indexers;
// other features talking to applications on the same network reuse it.
func (s *Service) HTTPPolicy(ctx context.Context) (httpx.Policy, error) { return s.policy(ctx) }

// policy returns the outbound address policy from the settings.
func (s *Service) policy(ctx context.Context) (httpx.Policy, error) {
	if s.PolicyOverride != nil {
		return *s.PolicyOverride, nil
	}
	hosts, err := settings.Get(ctx, s.Settings, AllowedHosts)
	if err != nil {
		return httpx.Policy{}, err
	}
	block, err := settings.Get(ctx, s.Settings, BlockPrivateNetworks)
	if err != nil {
		return httpx.Policy{}, err
	}
	prefixes, names, err := httpx.ParseAllowed(hosts)
	if err != nil {
		return httpx.Policy{}, err
	}
	return httpx.Policy{BlockPrivate: block, Allowed: prefixes, AllowedHosts: names}, nil
}

func policyKey(p httpx.Policy) string {
	var b strings.Builder
	if p.BlockPrivate {
		b.WriteString("block;")
	}
	for _, a := range p.Allowed {
		b.WriteString(a.String() + ";")
	}
	b.WriteString(strings.Join(p.AllowedHosts, ";"))
	return b.String()
}

// forget drops the cached client (and its FlareSolverr cookies) of an indexer.
func (s *Service) forget(id int64) {
	s.mu.Lock()
	delete(s.clients, id)
	s.mu.Unlock()
}

// buildClient makes a protocol client from an input and its secrets.
func (s *Service) buildClient(ctx context.Context, in Input, apiKey, proxyPassword string) (*znab.Client, error) {
	pol, err := s.policy(ctx)
	if err != nil {
		return nil, err
	}
	proxy := ""
	if in.ProxyURL != "" {
		u, err := url.Parse(in.ProxyURL)
		if err != nil {
			return nil, httpx.ErrBadProxy
		}
		if in.ProxyUsername != "" {
			u.User = url.UserPassword(in.ProxyUsername, proxyPassword)
		}
		proxy = u.String()
	}
	timeout := time.Duration(in.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = httpx.DefaultTimeout
	}
	hc, err := httpx.New(httpx.Options{Policy: pol, Timeout: timeout, Proxy: proxy, FlareSolverr: in.FlareSolverrURL,
		Secrets: []string{apiKey, proxyPassword}})
	if err != nil {
		return nil, err
	}
	return &znab.Client{HTTP: hc, BaseURL: in.BaseURL, APIPath: in.APIPath, APIKey: apiKey}, nil
}

// client returns the (cached) protocol client of a saved indexer.
func (s *Service) client(ctx context.Context, ind Indexer) (*znab.Client, error) {
	pol, err := s.policy(ctx)
	if err != nil {
		return nil, err
	}
	pk := policyKey(pol)
	s.mu.Lock()
	cc := s.clients[ind.ID]
	s.mu.Unlock()
	if cc != nil && cc.updated.Equal(ind.UpdatedAt) && cc.policy == pk {
		return cc.c, nil
	}
	key, err := s.secret(ctx, ind.ID, "apiKey")
	if err != nil {
		return nil, err
	}
	pw, err := s.secret(ctx, ind.ID, "proxyPassword")
	if err != nil {
		return nil, err
	}
	c, err := s.buildClient(ctx, inputOf(ind), key, pw)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.clients[ind.ID] = &cachedClient{updated: ind.UpdatedAt, policy: pk, c: c}
	s.mu.Unlock()
	return c, nil
}

// capsFor fetches the capabilities of an unsaved configuration; secrets that
// are not in the input come from the saved indexer existingID.
func (s *Service) capsFor(ctx context.Context, in Input, existingID *int64) (*znab.Caps, error) {
	c, err := s.inputClient(ctx, in, existingID)
	if err != nil {
		return nil, err
	}
	return c.Caps(ctx)
}

func (s *Service) inputClient(ctx context.Context, in Input, existingID *int64) (*znab.Client, error) {
	var key, pw string
	if in.APIKey != nil {
		key = *in.APIKey
	} else if existingID != nil {
		k, err := s.secret(ctx, *existingID, "apiKey")
		if err != nil {
			return nil, err
		}
		key = k
	}
	if in.ProxyPassword != nil {
		pw = *in.ProxyPassword
	} else if existingID != nil {
		p, err := s.secret(ctx, *existingID, "proxyPassword")
		if err != nil {
			return nil, err
		}
		pw = p
	}
	return s.buildClient(ctx, in, key, pw)
}

// Backoff is how long an indexer is skipped after n consecutive failures:
// 1 minute after the first, then 5 minutes doubling up to 12 hours.
func Backoff(n int) time.Duration {
	if n <= 0 {
		return 0
	}
	if n == 1 {
		return time.Minute
	}
	d := 5 * time.Minute * time.Duration(math.Pow(2, float64(min(n-2, 10))))
	return min(d, 12*time.Hour)
}

// countsAsFailure reports whether an error says something about the indexer's
// health (a parameter it does not support or our own cancellation does not).
func countsAsFailure(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return false
	}
	switch znab.KindOf(err) {
	case znab.KindUnsupported, znab.KindNotFound:
		return false
	}
	return true
}

// recordSuccess clears an indexer's failures.
func (s *Service) recordSuccess(ctx context.Context, id int64) {
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE indexers SET failures = 0, retry_at = NULL, last_success_at = ? WHERE id = ?`,
		db.Millis(s.now()), id); err != nil {
		s.Log.Warn("record indexer success", "indexer", id, "err", err)
	}
}

// recordFailure counts a failure and moves the indexer into backoff.
func (s *Service) recordFailure(ctx context.Context, ind Indexer, err error) {
	n := ind.Health.Failures + 1
	wait := Backoff(n)
	var ze *znab.Error
	now := s.now()
	if errors.As(err, &ze) && ze.RetryAfter > 0 {
		wait = max(wait, time.Duration(min(ze.RetryAfter, 86400))*time.Second)
	}
	if until, ok := LimitedUntil(err, now); ok {
		wait = max(wait, until.Sub(now))
	}
	msg := errorText(err)
	code := string(znab.KindOf(err))
	if _, dberr := s.DB.W.ExecContext(context.WithoutCancel(ctx), `UPDATE indexers SET failures = failures + 1, retry_at = ?,
		last_error = ?, last_error_code = ?, last_error_at = ? WHERE id = ?`,
		db.Millis(now.Add(wait)), msg, code, db.Millis(now), ind.ID); dberr != nil {
		s.Log.Warn("record indexer failure", "indexer", ind.ID, "err", dberr)
	}
	s.Log.Warn("indexer request failed", "indexer", ind.Name, "kind", code, "failures", n, "retry_in", wait.String(), "err", msg)
}

// LimitedUntil reads when an indexer that refused a request will take
// requests again: a Retry-After, or the time in an "indexer is disabled till
// <time>" message (Prowlarr's answer after repeated failures, in its local
// time). At most a day ahead; ok is false when the error names no time.
func LimitedUntil(err error, now time.Time) (time.Time, bool) {
	var ze *znab.Error
	if !errors.As(err, &ze) {
		return time.Time{}, false
	}
	if t, ok := znab.DisabledUntil(ze.Message, time.Local); ok && t.After(now) {
		return minTime(t, now.Add(24*time.Hour)), true
	}
	if ze.RetryAfter > 0 {
		return now.Add(time.Duration(min(ze.RetryAfter, 86400)) * time.Second), true
	}
	return time.Time{}, false
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// ResetHealth clears failures and backoff.
func (s *Service) ResetHealth(ctx context.Context, id int64) (Indexer, error) {
	res, err := s.DB.W.ExecContext(ctx, `UPDATE indexers SET failures = 0, retry_at = NULL WHERE id = ?`, id)
	if err != nil {
		return Indexer{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Indexer{}, ErrNotFound
	}
	return s.Get(ctx, id)
}

// errorText is a short, secret-free error message (httpx already redacts URLs).
func errorText(err error) string {
	var ze *znab.Error
	if errors.As(err, &ze) {
		return truncate(ze.Message, 500)
	}
	return truncate(err.Error(), 500)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// TestResult is the outcome of Test.
type TestResult struct {
	OK                bool
	Duration          time.Duration
	Caps              *znab.Caps
	DefaultCategories Categories
	ProbeResults      int
	ErrorCode         string
	Error             string
}

// TestInput checks an unsaved configuration: capabilities, then a probe
// search. existingID supplies secrets that the input leaves out.
func (s *Service) TestInput(ctx context.Context, in Input, existingID *int64) (TestResult, error) {
	if err := s.normalize(ctx, &in); err != nil {
		return TestResult{}, err
	}
	if existingID != nil {
		if _, err := s.Get(ctx, *existingID); err != nil {
			return TestResult{}, err
		}
	}
	start := time.Now()
	c, err := s.inputClient(ctx, in, existingID)
	if err != nil {
		return TestResult{}, err
	}
	res := s.probe(ctx, c, in.Categories)
	res.Duration = time.Since(start)
	return res.TestResult, nil
}

// Test checks a saved indexer, stores its capabilities and updates its health.
func (s *Service) Test(ctx context.Context, id int64) (TestResult, error) {
	ind, err := s.Get(ctx, id)
	if err != nil {
		return TestResult{}, err
	}
	start := time.Now()
	c, err := s.client(ctx, ind)
	if err != nil {
		return TestResult{}, err
	}
	cats := ind.Categories
	res := s.probe(ctx, c, &cats)
	res.Duration = time.Since(start)
	if res.Caps != nil {
		if err := s.saveCaps(ctx, id, res.Caps); err != nil {
			return res.TestResult, err
		}
	}
	if res.OK {
		s.recordSuccess(ctx, id)
	} else if res.err != nil && countsAsFailure(ctx, res.err) {
		s.recordFailure(ctx, ind, res.err)
	}
	s.forget(id)
	return res.TestResult, nil
}

type probeResult struct {
	TestResult
	err error
}

func (s *Service) probe(ctx context.Context, c *znab.Client, cats *Categories) probeResult {
	fail := func(err error) probeResult {
		return probeResult{TestResult: TestResult{ErrorCode: string(znab.KindOf(err)), Error: errorText(err)}, err: err}
	}
	caps, err := c.Caps(ctx)
	if err != nil {
		return fail(err)
	}
	r := znab.Request{Type: znab.SearchGeneric, Limit: 10}
	if cats != nil && !cats.Empty() {
		r.Categories = searchCategories(cats.All())
	}
	page, err := c.Search(ctx, r)
	if err != nil {
		res := fail(err)
		res.Caps = caps
		res.DefaultCategories = DefaultCategories(caps)
		return res
	}
	return probeResult{TestResult: TestResult{OK: true, Caps: caps, DefaultCategories: DefaultCategories(caps), ProbeResults: len(page.Items)}}
}

// ValidateAllowedHosts checks administrator entries for AllowedHosts.
func ValidateAllowedHosts(hosts []string) error {
	if _, _, err := httpx.ParseAllowed(hosts); err != nil {
		return invalid("allowedHosts", err.Error())
	}
	return nil
}

// Policy returns the outbound address policy from the settings (also used
// for Streamero and the notification channels).
func (s *Service) Policy(ctx context.Context) (httpx.Policy, error) { return s.policy(ctx) }
