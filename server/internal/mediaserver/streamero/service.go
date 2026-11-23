package streamero

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/rootfolders"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// Settings keys.
var (
	ConfigKey = settings.Key[Config]{Name: "mediaserver.streamero", Default: Config{PathMappings: []PathMapping{}, LibraryMap: []LibraryMapping{}}}
	APIKey    = settings.SecretKey{Name: "mediaserver.streameroApiKey"}
)

// Config is the stored connection.
type Config struct {
	Enabled      bool             `json:"enabled"`
	URL          string           `json:"url"`
	PublicURL    string           `json:"publicUrl"`
	PathMappings []PathMapping    `json:"pathMappings"`
	LibraryMap   []LibraryMapping `json:"libraryMap"`
	SignIn       SignIn           `json:"signIn"`
}

// PathMapping translates a Locarr folder into the same folder as Streamero sees it.
type PathMapping struct {
	Locarr    string `json:"locarrPath"`
	Streamero string `json:"streameroPath"`
}

// LibraryMapping sends changes under a root folder to these libraries.
type LibraryMapping struct {
	RootFolderID int64    `json:"rootFolderId"`
	LibraryIDs   []string `json:"libraryIds"`
}

// SignIn configures "Sign in with Streamero".
type SignIn struct {
	Enabled    bool `json:"enabled"`
	AutoCreate bool `json:"autoCreate"`
}

// LinkURL is the address for browser links.
func (c Config) LinkURL() string {
	if c.PublicURL != "" {
		return c.PublicURL
	}
	return c.URL
}

// Task kinds.
const (
	TaskScan  = "streamero-scan"
	TaskIndex = "streamero-index"
)

// Service is the Streamero integration.
type Service struct {
	DB       *db.DB
	Settings *settings.Store
	Roots    *rootfolders.Service
	Timeline *timeline.Store
	Jobs     *jobs.Queue
	// Policy returns the outbound address policy (the indexers' one).
	Policy func(ctx context.Context) (httpx.Policy, error)
	Log    *slog.Logger
	Now    func() time.Time
	// Debounce is how long changes are collected before a scan (default 30 s).
	Debounce time.Duration
	// Health is told when Streamero starts or stops answering (may be nil).
	Health func(ctx context.Context, ok bool, reason string)

	mu     sync.Mutex
	status Status
}

// Status is what the settings page shows.
type Status struct {
	IndexedAt     *time.Time
	IndexError    string
	LastScanAt    *time.Time
	LastScanError string
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// ErrNotConfigured is returned while there is no address or key.
var ErrNotConfigured = errors.New("streamero is not set up")

// Load returns the configuration.
func (s *Service) Load(ctx context.Context) (Config, error) {
	c, err := settings.Get(ctx, s.Settings, ConfigKey)
	if c.PathMappings == nil {
		c.PathMappings = []PathMapping{}
	}
	if c.LibraryMap == nil {
		c.LibraryMap = []LibraryMapping{}
	}
	return c, err
}

// Client returns a client for the stored configuration (ErrNotConfigured when off).
func (s *Service) Client(ctx context.Context) (*Client, Config, error) {
	c, err := s.Load(ctx)
	if err != nil {
		return nil, c, err
	}
	key, ok, err := s.Settings.Secret(ctx, APIKey)
	if err != nil {
		return nil, c, err
	}
	if !c.Enabled || c.URL == "" || !ok {
		return nil, c, ErrNotConfigured
	}
	cl, err := s.NewClient(ctx, c.URL, key)
	return cl, c, err
}

// NewClient builds a client with the outbound policy.
func (s *Service) NewClient(ctx context.Context, baseURL, key string) (*Client, error) {
	pol := httpx.Policy{}
	if s.Policy != nil {
		var err error
		if pol, err = s.Policy(ctx); err != nil {
			return nil, err
		}
	}
	return New(baseURL, key, pol)
}

// Enabled reports whether Streamero is set up and switched on.
func (s *Service) Enabled(ctx context.Context) bool {
	c, err := s.Load(ctx)
	if err != nil || !c.Enabled || c.URL == "" {
		return false
	}
	ok, _ := s.Settings.Configured(ctx, APIKey)
	return ok
}

// Input changes the configuration; APIKey nil keeps the stored key.
type Input struct {
	Config Config
	APIKey *string
}

// Save validates (and, when switched on, tests) and stores the configuration.
func (s *Service) Save(ctx context.Context, in Input) (Config, error) {
	c := in.Config
	if c.URL != "" || c.Enabled {
		u, err := NormalizeURL(c.URL)
		if err != nil {
			return c, err
		}
		c.URL = u
	}
	if c.PublicURL != "" {
		u, err := NormalizeURL(c.PublicURL)
		if err != nil {
			return c, err
		}
		c.PublicURL = u
	}
	for i, m := range c.PathMappings {
		m.Locarr, m.Streamero = cleanDir(m.Locarr), cleanDir(m.Streamero)
		if !strings.HasPrefix(m.Locarr, "/") || !strings.HasPrefix(m.Streamero, "/") {
			return c, ErrBadMapping
		}
		c.PathMappings[i] = m
	}
	if c.PathMappings == nil {
		c.PathMappings = []PathMapping{}
	}
	if c.LibraryMap == nil {
		c.LibraryMap = []LibraryMapping{}
	}
	key := ""
	if in.APIKey != nil {
		key = strings.TrimSpace(*in.APIKey)
	} else {
		k, _, err := s.Settings.Secret(ctx, APIKey)
		if err != nil {
			return c, err
		}
		key = k
	}
	if c.Enabled {
		if key == "" {
			return c, ErrNotConfigured
		}
		if _, err := s.Check(ctx, c.URL, key); err != nil {
			return c, err
		}
	}
	if in.APIKey != nil {
		if err := s.Settings.SetSecret(ctx, APIKey, key); err != nil {
			return c, err
		}
	}
	if err := settings.Set(ctx, s.Settings, nil, ConfigKey, c); err != nil {
		return c, err
	}
	if c.Enabled {
		s.QueueIndex(ctx, 0)
	}
	return c, nil
}

// ErrBadMapping is returned for a path mapping that is not two absolute folders.
var ErrBadMapping = errors.New("path mappings need two absolute folders")

func cleanDir(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return path.Clean(p)
}

// TestResult is the outcome of a check.
type TestResult struct {
	Info      SystemInfo
	Libraries []Library
}

// Check verifies an address and key: it is Streamero, the key works and
// belongs to an administrator, and the libraries can be listed.
func (s *Service) Check(ctx context.Context, baseURL, key string) (TestResult, error) {
	u, err := NormalizeURL(baseURL)
	if err != nil {
		return TestResult{}, err
	}
	cl, err := s.NewClient(ctx, u, key)
	if err != nil {
		return TestResult{}, err
	}
	info, err := cl.Info(ctx)
	if err != nil {
		return TestResult{}, err
	}
	if key == "" {
		return TestResult{Info: info}, ErrAuth
	}
	me, err := cl.Me(ctx)
	if err != nil {
		return TestResult{Info: info}, err
	}
	if !me.IsAdmin {
		return TestResult{Info: info}, ErrNotAdmin
	}
	libs, err := cl.Libraries(ctx)
	if err != nil {
		return TestResult{Info: info}, err
	}
	return TestResult{Info: info, Libraries: libs}, nil
}

// StoredKey returns the stored API key.
func (s *Service) StoredKey(ctx context.Context) (string, error) {
	k, _, err := s.Settings.Secret(ctx, APIKey)
	return k, err
}

// ToStreamero translates a Locarr path with the longest matching mapping
// (unchanged when none matches).
func ToStreamero(p string, maps []PathMapping) string {
	p = path.Clean(p)
	best, out := -1, p
	for _, m := range maps {
		if m.Locarr == "" {
			continue
		}
		if under(p, m.Locarr) && len(m.Locarr) > best {
			best = len(m.Locarr)
			out = path.Join(m.Streamero, strings.TrimPrefix(p, m.Locarr))
		}
	}
	return out
}

// under reports whether p is dir or inside it.
func under(p, dir string) bool {
	dir = strings.TrimRight(dir, "/")
	if dir == "" {
		return strings.HasPrefix(p, "/")
	}
	return p == dir || strings.HasPrefix(p, dir+"/")
}

// LibraryView is a library with the root folders that match it.
type LibraryView struct {
	Library
	Suggested []int64
}

// Suggest pairs libraries with root folders whose (mapped) path is inside one
// of the library's folders, or holds one.
func Suggest(libs []Library, roots []rootfolders.Folder, maps []PathMapping) []LibraryView {
	out := make([]LibraryView, 0, len(libs))
	for _, l := range libs {
		v := LibraryView{Library: l, Suggested: []int64{}}
		if v.Paths == nil {
			v.Paths = []string{}
		}
		for _, r := range roots {
			rp := ToStreamero(r.Path, maps)
			for _, lp := range l.Paths {
				lp = path.Clean(lp)
				if under(rp, lp) || under(lp, rp) {
					v.Suggested = append(v.Suggested, r.ID)
					break
				}
			}
		}
		out = append(out, v)
	}
	return out
}

// Libraries returns Streamero's libraries with suggestions.
func (s *Service) Libraries(ctx context.Context) ([]LibraryView, error) {
	cl, c, err := s.Client(ctx)
	if err != nil {
		return nil, err
	}
	libs, err := cl.Libraries(ctx)
	if err != nil {
		return nil, err
	}
	roots, err := s.Roots.List(ctx)
	if err != nil {
		return nil, err
	}
	return Suggest(libs, roots, c.PathMappings), nil
}

// StatusInfo returns the status shown on the settings page.
func (s *Service) StatusInfo(ctx context.Context) (Status, int, int, error) {
	s.mu.Lock()
	st := s.status
	s.mu.Unlock()
	var items, pending int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM streamero_items), (SELECT count(*) FROM mediaserver_pending)`).
		Scan(&items, &pending); err != nil {
		return st, 0, 0, err
	}
	if st.IndexedAt == nil {
		var at sql.NullInt64
		if err := s.DB.R.QueryRowContext(ctx, `SELECT max(checked_at) FROM streamero_items`).Scan(&at); err == nil {
			st.IndexedAt = db.NullTime(at)
		}
	}
	return st, items, pending, nil
}

func (s *Service) setStatus(f func(*Status)) {
	s.mu.Lock()
	f(&s.status)
	s.mu.Unlock()
}

// UserView is a Streamero account with its Locarr link.
type UserView struct {
	User
	LinkedUserID   int64
	LinkedUsername string
}

// Users lists Streamero's accounts and who they are linked to.
func (s *Service) Users(ctx context.Context) ([]UserView, error) {
	cl, _, err := s.Client(ctx)
	if err != nil {
		return nil, err
	}
	list, err := cl.Users(ctx)
	if err != nil {
		return nil, err
	}
	links := map[string][2]any{}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT i.subject, u.id, u.username FROM user_identities i JOIN users u ON u.id = i.user_id
		WHERE i.provider = ?`, ProviderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			sub  string
			id   int64
			name string
		)
		if err := rows.Scan(&sub, &id, &name); err != nil {
			return nil, err
		}
		links[sub] = [2]any{id, name}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]UserView, 0, len(list))
	for _, u := range list {
		v := UserView{User: u}
		if l, ok := links[u.ID]; ok {
			v.LinkedUserID, v.LinkedUsername = l[0].(int64), l[1].(string)
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Username) < strings.ToLower(out[j].Username) })
	return out, nil
}
