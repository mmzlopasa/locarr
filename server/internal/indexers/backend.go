package indexers

import (
	"context"
	"net/url"
	"slices"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/indexers/native"
	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Settings for the built-in sites.
var (
	// PublicTrackers are added to magnets built from a bare info hash.
	PublicTrackers = settings.Key[[]string]{Name: "indexers.publicTrackers", Default: native.DefaultTrackers}
	// UserAgent is sent to every indexer ("" = "Locarr").
	UserAgent = settings.Key[string]{Name: "indexers.userAgent", Default: ""}
)

// backend is what the service needs from an indexer: a Torznab/Newznab
// client or a built-in site.
type backend interface {
	Caps(ctx context.Context) (*znab.Caps, error)
	Search(ctx context.Context, r znab.Request) (*znab.Page, error)
	Fetch(ctx context.Context, link string) (*httpx.Response, error)
	// secret is the API key that must not leave the server ("" for none).
	secret() string
	// probe is the search a test runs.
	probe(cats []int) znab.Request
}

type znabBackend struct{ *znab.Client }

func (z znabBackend) secret() string { return z.APIKey }

func (z znabBackend) probe(cats []int) znab.Request {
	return znab.Request{Type: znab.SearchGeneric, Limit: 10, Categories: cats}
}

type nativeBackend struct{ *native.Client }

func (nativeBackend) secret() string { return "" }

// probe runs the definition's known query (sites search all categories for it).
func (n nativeBackend) probe([]int) znab.Request { return n.TestRequest() }

// Definition returns a built-in or custom site definition.
func (s *Service) Definition(id string) (*native.Definition, bool) {
	if s.Natives == nil {
		return nil, false
	}
	return s.Natives.Get(id)
}

// Definitions returns every site definition.
func (s *Service) Definitions() []*native.Definition {
	if s.Natives == nil {
		return nil
	}
	return s.Natives.List()
}

// DefinitionErrors returns the custom definition files that failed to load.
func (s *Service) DefinitionErrors() []native.LoadError {
	if s.Natives == nil {
		return nil
	}
	return s.Natives.Errors()
}

// ReloadDefinitions re-reads the custom definitions folder.
func (s *Service) ReloadDefinitions() error {
	if s.Natives == nil {
		return nil
	}
	if err := s.Natives.Reload(); err != nil {
		return err
	}
	s.mu.Lock()
	s.clients = map[int64]*cachedClient{}
	s.mu.Unlock()
	return nil
}

// NativeInput returns the defaults of a new indexer for a site: its name,
// address, request limit, timeout and RSS (off when the site has no list of
// latest releases).
func (s *Service) NativeInput(def *native.Definition) Input {
	in := DefaultInput()
	in.Name, in.Kind, in.Definition, in.BaseURL, in.APIPath = def.Name, Native, def.ID, def.DefaultURL(), "/"
	in.EnableRSS = def.SupportsRSS()
	if def.RateLimit.Requests > 0 {
		n, p := def.RateLimit.Requests, def.RateLimit.PeriodSeconds
		in.RequestLimit, in.RequestPeriodSec = &n, &p
	}
	if def.TimeoutSeconds > 0 {
		in.TimeoutSeconds = def.TimeoutSeconds
	}
	in.Options = map[string]string{}
	return in
}

func (s *Service) nativeClient(ctx context.Context, in Input, hc *httpx.Client) (backend, error) {
	def, ok := s.Definition(in.Definition)
	if !ok {
		return nil, invalid("definition", "no site definition "+in.Definition)
	}
	trackers, err := settings.Get(ctx, s.Settings, PublicTrackers)
	if err != nil {
		return nil, err
	}
	base := in.BaseURL
	if strings.TrimRight(base, "/") == def.DefaultURL() {
		base = ""
	}
	return nativeBackend{&native.Client{Def: def, HTTP: hc, BaseURL: base, Config: in.Options, Trackers: trackers, Now: s.Now}}, nil
}

// TaskSelfTest is the weekly check of every enabled built-in site.
const TaskSelfTest = "indexer-selftest"

// SelfTestResult counts a self-test run.
type SelfTestResult struct{ Tested, Failed int }

// SelfTest runs each enabled built-in site's known query (one site at a
// time, gently) and records the outcome next to its health.
func (s *Service) SelfTest(ctx context.Context) (SelfTestResult, error) {
	all, err := s.List(ctx)
	if err != nil {
		return SelfTestResult{}, err
	}
	var res SelfTestResult
	for _, ind := range all {
		if ind.Kind != Native || !ind.Enabled {
			continue
		}
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		tr, err := s.Test(ctx, ind.ID)
		if err != nil {
			s.Log.Warn("indexer self-test", "indexer", ind.Name, "err", err)
			continue
		}
		res.Tested++
		if !tr.OK {
			res.Failed++
		}
		if _, err := s.DB.W.ExecContext(ctx, `UPDATE indexers SET self_test_at = ?, self_test_ok = ? WHERE id = ?`,
			db.Millis(s.now()), tr.OK, ind.ID); err != nil {
			return res, err
		}
	}
	return res, nil
}

// ValidateTracker checks a tracker announce URL for the public tracker list.
func ValidateTracker(t string) error {
	u, err := url.Parse(t)
	if err != nil || u.Host == "" || !slices.Contains([]string{"udp", "http", "https", "wss"}, u.Scheme) {
		return invalid("publicTrackers", "trackers must be udp://, http(s):// or wss:// announce URLs")
	}
	return nil
}
