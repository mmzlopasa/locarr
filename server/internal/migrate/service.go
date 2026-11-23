package migrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Job kinds.
const (
	TaskAnalyse = "migrate-analyse"
	TaskApply   = "migrate-apply"
	TaskRefresh = "migrate-refresh"
)

// Errors.
var (
	ErrNotFound   = errors.New("not found")
	ErrInvalid    = errors.New("invalid")
	ErrExpired    = errors.New("what was read from the sources is no longer in memory; read them again")
	ErrBusy       = errors.New("another import is running")
	ErrWrongState = errors.New("the run is not in a state that allows this")
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Service keeps sources and runs and drives the import jobs.
type Service struct {
	L      *Locarr
	Jobs   *jobs.Queue
	Policy func(ctx context.Context) (httpx.Policy, error)
	Log    *slog.Logger
	Now    func() time.Time

	mu    sync.Mutex
	cache map[int64]*runCache
}

// runCache holds what a run read (snapshots are big; only memory).
type runCache struct {
	snaps   []*Snapshot
	secrets map[int64]string
	at      time.Time
}

const cacheTTL = 3 * time.Hour

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

func (s *Service) policy(ctx context.Context) (httpx.Policy, error) {
	if s.Policy != nil {
		return s.Policy(ctx)
	}
	return httpx.Policy{}, nil
}

// ---- sources ----

// Source is a saved application to import from.
type Source struct {
	ID         int64      `json:"id"`
	Kind       Kind       `json:"kind"`
	Name       string     `json:"name"`
	URL        string     `json:"url"`
	Username   string     `json:"username"`
	SecretSet  bool       `json:"secretSet"`
	AppName    string     `json:"appName"`
	Version    string     `json:"version"`
	LastTestAt *time.Time `json:"lastTestAt"`
	LastError  string     `json:"lastError"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// SourceInput adds or changes a source; Secret nil keeps the stored one.
type SourceInput struct {
	Kind     Kind
	Name     string
	URL      string
	Username string
	Secret   *string
}

func aad(id int64) string { return "migrate-source:" + strconv.FormatInt(id, 10) + ":secret" }

func (in *SourceInput) check() error {
	if !in.Kind.Valid() {
		return invalid("unknown application %q", in.Kind)
	}
	in.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.User != nil {
		return invalid("the address must be http(s)://host:port (with the URL base, if any)")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		in.Name = in.Kind.Label()
	}
	if len(in.Name) > 100 {
		return invalid("the name is too long")
	}
	in.Username = strings.TrimSpace(in.Username)
	return nil
}

func (s *Service) conn(in SourceInput, secret string) Conn {
	c := Conn{Kind: in.Kind, URL: in.URL, Username: in.Username}
	if in.Kind == QBittorrent {
		c.Password = secret
	} else {
		c.APIKey = secret
	}
	return c
}

const sourceCols = `id, kind, name, url, username, secret IS NOT NULL AND length(secret) > 0, app_name, version, last_test_at, last_error, created_at`

func scanSource(row interface{ Scan(...any) error }) (Source, error) {
	var (
		x       Source
		kind    string
		tested  sql.NullInt64
		created int64
	)
	err := row.Scan(&x.ID, &kind, &x.Name, &x.URL, &x.Username, &x.SecretSet, &x.AppName, &x.Version, &tested, &x.LastError, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	x.Kind, x.LastTestAt, x.CreatedAt = Kind(kind), db.NullTime(tested), db.Time(created)
	return x, err
}

// Sources lists the saved sources.
func (s *Service) Sources(ctx context.Context) ([]Source, error) {
	rows, err := s.L.DB.R.QueryContext(ctx, `SELECT `+sourceCols+` FROM migrate_sources ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		x, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Source returns one source.
func (s *Service) Source(ctx context.Context, id int64) (Source, error) {
	return scanSource(s.L.DB.R.QueryRowContext(ctx, `SELECT `+sourceCols+` FROM migrate_sources WHERE id = ?`, id))
}

func (s *Service) secret(ctx context.Context, id int64) (string, error) {
	var ct []byte
	if err := s.L.DB.R.QueryRowContext(ctx, `SELECT secret FROM migrate_sources WHERE id = ?`, id).Scan(&ct); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	if len(ct) == 0 {
		return "", nil
	}
	pt, err := s.L.Box.Open(ct, aad(id))
	return string(pt), err
}

// Test checks a source (saved: id > 0 with in overriding what is given).
func (s *Service) Test(ctx context.Context, id int64, in SourceInput) (Detected, error) {
	secret := ""
	if id > 0 {
		cur, err := s.Source(ctx, id)
		if err != nil {
			return Detected{}, err
		}
		if in.Kind == "" {
			in.Kind = cur.Kind
		}
		if in.URL == "" {
			in.URL = cur.URL
		}
		if in.Username == "" {
			in.Username = cur.Username
		}
		if in.Secret == nil {
			if secret, err = s.secret(ctx, id); err != nil {
				return Detected{}, err
			}
		}
	}
	if in.Secret != nil {
		secret = *in.Secret
	}
	if err := in.check(); err != nil {
		return Detected{}, err
	}
	pol, err := s.policy(ctx)
	if err != nil {
		return Detected{}, err
	}
	if err := pol.CheckURL(in.URL); err != nil {
		return Detected{}, invalid("%v", err)
	}
	d, err := Detect(ctx, s.conn(in, secret), pol)
	if id > 0 {
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		_, _ = s.L.DB.W.ExecContext(ctx, `UPDATE migrate_sources SET last_test_at = ?, last_error = ?, app_name = CASE WHEN ? = '' THEN app_name ELSE ? END,
			version = CASE WHEN ? = '' THEN version ELSE ? END WHERE id = ?`, db.Millis(s.now()), msg, d.AppName, d.AppName, d.Version, d.Version, id)
	}
	return d, err
}

// CreateSource tests and saves a source.
func (s *Service) CreateSource(ctx context.Context, in SourceInput) (Source, error) {
	if err := in.check(); err != nil {
		return Source{}, err
	}
	if in.Secret == nil || strings.TrimSpace(*in.Secret) == "" {
		if in.Kind != QBittorrent {
			return Source{}, invalid("the API key is required")
		}
		empty := ""
		in.Secret = &empty
	}
	d, err := s.Test(ctx, 0, in)
	if err != nil {
		return Source{}, err
	}
	now := db.Millis(s.now())
	var id int64
	err = s.L.DB.Tx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO migrate_sources (kind, name, url, username, app_name, version, last_test_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`, string(in.Kind), in.Name, in.URL, in.Username, d.AppName, d.Version, now, now, now).Scan(&id); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return invalid("this %s is already added", in.Kind.Label())
			}
			return err
		}
		ct, err := s.L.Box.Seal([]byte(*in.Secret), aad(id))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE migrate_sources SET secret = ? WHERE id = ?`, ct, id)
		return err
	})
	if err != nil {
		return Source{}, err
	}
	return s.Source(ctx, id)
}

// UpdateSource changes a source (tested again).
func (s *Service) UpdateSource(ctx context.Context, id int64, in SourceInput) (Source, error) {
	cur, err := s.Source(ctx, id)
	if err != nil {
		return cur, err
	}
	in.Kind = cur.Kind
	if err := in.check(); err != nil {
		return cur, err
	}
	d, err := s.Test(ctx, id, in)
	if err != nil {
		return cur, err
	}
	err = s.L.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE migrate_sources SET name = ?, url = ?, username = ?, app_name = ?, version = ?, last_error = '',
			updated_at = ? WHERE id = ?`, in.Name, in.URL, in.Username, d.AppName, d.Version, db.Millis(s.now()), id); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return invalid("this %s is already added", in.Kind.Label())
			}
			return err
		}
		if in.Secret != nil {
			ct, err := s.L.Box.Seal([]byte(*in.Secret), aad(id))
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE migrate_sources SET secret = ? WHERE id = ?`, ct, id)
			return err
		}
		return nil
	})
	if err != nil {
		return cur, err
	}
	return s.Source(ctx, id)
}

// DeleteSource forgets a source (what was imported stays, and so do the
// marks, so adding it again does not duplicate anything).
func (s *Service) DeleteSource(ctx context.Context, id int64) error {
	res, err := s.L.DB.W.ExecContext(ctx, `DELETE FROM migrate_sources WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- runs ----

// RunProgress is how far a run is.
type RunProgress struct {
	Phase   string `json:"phase"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Message string `json:"message"`
}

// SourceSummary says what was read from one source.
type SourceSummary struct {
	ID      int64          `json:"id"`
	Kind    Kind           `json:"kind"`
	Name    string         `json:"name"`
	Version string         `json:"version"`
	Read    map[string]int `json:"read"`
	Roots   []string       `json:"roots"`
}

// RefreshState is the background metadata refresh after an import.
type RefreshState struct {
	Total  int    `json:"total"`
	Done   int    `json:"done"`
	Failed int    `json:"failed"`
	Error  string `json:"error,omitempty"`
}

// Summary is a run's overview.
type Summary struct {
	Sources    []SourceSummary `json:"sources"`
	PathChecks []PathCheck     `json:"pathChecks"`
	Suggested  []PathMapping   `json:"suggested"`
	Counts     Counts          `json:"counts"`
	Statuses   map[string]int  `json:"statuses"`
	Attention  map[string]int  `json:"attention"`
	Conflicts  int             `json:"conflicts"`
	Warnings   []string        `json:"warnings"`
	Refresh    *RefreshState   `json:"refresh,omitempty"`
	DurationMS int64           `json:"durationMs,omitempty"`
}

// Run is one pass of the wizard.
type Run struct {
	ID         int64       `json:"id"`
	Status     string      `json:"status"`
	Options    Options     `json:"options"`
	Progress   RunProgress `json:"progress"`
	Summary    Summary     `json:"summary"`
	Error      string      `json:"error"`
	CreatedAt  time.Time   `json:"createdAt"`
	UpdatedAt  time.Time   `json:"updatedAt"`
	FinishedAt *time.Time  `json:"finishedAt"`
}

const runCols = `id, status, options, progress, summary, error, created_at, updated_at, finished_at`

func scanRun(row interface{ Scan(...any) error }) (Run, error) {
	var (
		r                Run
		opts, prog, sum  string
		created, updated int64
		finished         sql.NullInt64
	)
	err := row.Scan(&r.ID, &r.Status, &opts, &prog, &sum, &r.Error, &created, &updated, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	_ = json.Unmarshal([]byte(opts), &r.Options)
	_ = json.Unmarshal([]byte(prog), &r.Progress)
	_ = json.Unmarshal([]byte(sum), &r.Summary)
	r.CreatedAt, r.UpdatedAt, r.FinishedAt = db.Time(created), db.Time(updated), db.NullTime(finished)
	return r, nil
}

// Run returns one run.
func (s *Service) Run(ctx context.Context, id int64) (Run, error) {
	return scanRun(s.L.DB.R.QueryRowContext(ctx, `SELECT `+runCols+` FROM migrate_runs WHERE id = ?`, id))
}

// Runs lists recent runs.
func (s *Service) Runs(ctx context.Context, limit int) ([]Run, error) {
	rows, err := s.L.DB.R.QueryContext(ctx, `SELECT `+runCols+` FROM migrate_runs ORDER BY id DESC LIMIT ?`, max(limit, 1))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func jsonText(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *Service) setRun(ctx context.Context, id int64, status string, prog *RunProgress, sum *Summary, errMsg *string, finished bool) {
	sets, args := []string{"updated_at = ?"}, []any{db.Millis(s.now())}
	if status != "" {
		sets, args = append(sets, "status = ?"), append(args, status)
	}
	if prog != nil {
		sets, args = append(sets, "progress = ?"), append(args, jsonText(prog))
	}
	if sum != nil {
		sets, args = append(sets, "summary = ?"), append(args, jsonText(sum))
	}
	if errMsg != nil {
		sets, args = append(sets, "error = ?"), append(args, *errMsg)
	}
	if finished {
		sets, args = append(sets, "finished_at = ?"), append(args, db.Millis(s.now()))
	}
	args = append(args, id)
	if _, err := s.L.DB.W.ExecContext(context.WithoutCancel(ctx), `UPDATE migrate_runs SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil { // #nosec G202 -- constant column names
		s.log().Warn("migrate run update", "run", id, "err", err)
	}
}

func (s *Service) busy(ctx context.Context) error {
	var n int
	if err := s.L.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM migrate_runs WHERE status IN ('analysing', 'importing')`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrBusy
	}
	return nil
}

func (s *Service) checkOptions(ctx context.Context, o *Options) error {
	if len(o.Sources) == 0 {
		return invalid("choose at least one source")
	}
	seen := map[int64]bool{}
	for i := range o.Sources {
		so := &o.Sources[i]
		src, err := s.Source(ctx, so.ID)
		if err != nil {
			return err
		}
		if seen[so.ID] {
			return invalid("a source is listed twice")
		}
		seen[so.ID] = true
		sup := src.Kind.Supported()
		so.Settings, so.Library, so.Users, so.History = so.Settings && sup.Settings, so.Library && sup.Library, so.Users && sup.Users,
			so.History && sup.History
		if so.ProwlarrMode != "" && so.ProwlarrMode != "source" && so.ProwlarrMode != "direct" {
			return invalid("prowlarrMode must be source or direct")
		}
	}
	if o.HistoryDays <= 0 {
		o.HistoryDays = 90
	}
	if o.HistoryDays > 3650 {
		o.HistoryDays = 3650
	}
	for i := range o.PathMappings {
		m := &o.PathMappings[i]
		m.From, m.To = strings.TrimSpace(m.From), strings.TrimSpace(m.To)
		if m.From == "" || m.To == "" || !strings.HasPrefix(m.To, "/") {
			return invalid("a path mapping needs a source folder and an absolute Locarr folder")
		}
	}
	if o.Resolutions == nil {
		o.Resolutions = map[string]string{}
	}
	return nil
}

type runArgs struct {
	RunID int64 `json:"runId"`
}

// Analyse starts reading the sources (a background job); the plan is ready
// when the run is "planned".
func (s *Service) Analyse(ctx context.Context, actor int64, o Options) (Run, error) {
	if err := s.busy(ctx); err != nil {
		return Run{}, err
	}
	o.ActorID = actor
	if err := s.checkOptions(ctx, &o); err != nil {
		return Run{}, err
	}
	now := db.Millis(s.now())
	var id int64
	err := s.L.DB.W.QueryRowContext(ctx, `INSERT INTO migrate_runs (status, options, progress, created_by, created_at, updated_at)
		VALUES ('analysing', ?, ?, ?, ?, ?) RETURNING id`, jsonText(o), jsonText(RunProgress{Phase: "reading"}), actor, now, now).Scan(&id)
	if err != nil {
		return Run{}, err
	}
	if _, _, err := s.Jobs.Enqueue(ctx, TaskAnalyse, runArgs{RunID: id}, jobs.EnqueueOptions{UniqueKey: "migrate:" + strconv.FormatInt(id, 10)}); err != nil {
		return Run{}, err
	}
	return s.Run(ctx, id)
}

// RunAnalyse is the analyse job.
func (s *Service) RunAnalyse(ctx context.Context, id int64) error {
	r, err := s.Run(ctx, id)
	if err != nil {
		return jobs.Permanent(err)
	}
	fail := func(err error) error {
		msg := err.Error()
		s.setRun(ctx, id, "failed", &RunProgress{Phase: "failed", Message: msg}, nil, &msg, true)
		return jobs.Permanent(err)
	}
	pol, err := s.policy(ctx)
	if err != nil {
		return fail(err)
	}
	var snaps []*Snapshot
	secrets := map[int64]string{}
	sum := Summary{}
	for i, so := range r.Options.Sources {
		src, err := s.Source(ctx, so.ID)
		if err != nil {
			return fail(err)
		}
		secret, err := s.secret(ctx, so.ID)
		if err != nil {
			return fail(err)
		}
		secrets[so.ID] = secret
		in := SourceInput{Kind: src.Kind, URL: src.URL, Username: src.Username}
		label := src.Name
		ro := ReadOptions{Scopes: Scopes{Settings: so.Settings, Library: so.Library, Users: so.Users, History: so.History},
			HistorySince: s.now().AddDate(0, 0, -r.Options.HistoryDays),
			Progress: func(done, total int, what string) {
				s.setRun(ctx, id, "", &RunProgress{Phase: "reading", Done: done, Total: total, Message: label + ": " + what}, nil, nil, false)
			}}
		s.setRun(ctx, id, "", &RunProgress{Phase: "reading", Done: i, Total: len(r.Options.Sources), Message: label}, nil, nil, false)
		snap, err := Read(ctx, s.conn(in, secret), pol, ro)
		if err != nil {
			return fail(fmt.Errorf("%s: %w", label, err))
		}
		snap.SourceID, snap.Name = src.ID, src.Name
		snaps = append(snaps, snap)
		sum.Sources = append(sum.Sources, summarize(snap))
	}
	s.mu.Lock()
	if s.cache == nil {
		s.cache = map[int64]*runCache{}
	}
	for k, c := range s.cache {
		if s.now().Sub(c.at) > cacheTTL || len(s.cache) > 3 {
			delete(s.cache, k)
		}
	}
	s.cache[id] = &runCache{snaps: snaps, secrets: secrets, at: s.now()}
	s.mu.Unlock()
	// Suggested mappings for roots Locarr cannot see as they are.
	if len(r.Options.PathMappings) == 0 {
		var local []string
		if roots, err := s.L.Roots.List(ctx); err == nil {
			for _, f := range roots {
				local = append(local, f.Path)
			}
		}
		for _, snap := range snaps {
			var src []string
			for _, rf := range snap.RootFolders {
				src = append(src, rf.Path)
			}
			sum.Suggested = append(sum.Suggested, SuggestMappings(snap.SourceID, src, local, s.L.DataRoots)...)
		}
		r.Options.PathMappings = sum.Suggested
		s.saveOptions(ctx, id, r.Options)
	}
	s.setRun(ctx, id, "analysed", &RunProgress{Phase: "planning"}, &sum, nil, false)
	if _, err := s.plan(ctx, id, r.Options, &sum); err != nil {
		return fail(err)
	}
	return nil
}

func (s *Service) saveOptions(ctx context.Context, id int64, o Options) {
	_, _ = s.L.DB.W.ExecContext(ctx, `UPDATE migrate_runs SET options = ? WHERE id = ?`, jsonText(o), id)
}

func summarize(s *Snapshot) SourceSummary {
	ss := SourceSummary{ID: s.SourceID, Kind: s.Kind, Name: s.Name, Version: s.Version, Read: map[string]int{}}
	add := func(k string, n int) {
		if n > 0 {
			ss.Read[k] = n
		}
	}
	files := 0
	for _, m := range s.Movies {
		if m.File != nil {
			files++
		}
	}
	for _, v := range s.Series {
		files += len(v.Files)
	}
	for _, a := range s.Artists {
		for _, al := range a.Albums {
			files += len(al.Files)
		}
	}
	add("qualityProfiles", len(s.QualityProfiles))
	add("customFormats", len(s.CustomFormats))
	add("indexers", len(s.Indexers))
	add("downloadClients", len(s.DownloadClients))
	add("rootFolders", len(s.RootFolders))
	add("movies", len(s.Movies))
	add("series", len(s.Series))
	add("artists", len(s.Artists))
	add("files", files)
	add("users", len(s.Users))
	add("requests", len(s.Requests))
	add("issues", len(s.Issues))
	add("subtitleProfiles", len(s.SubtitleProfiles))
	add("history", len(s.History))
	add("blocklist", len(s.Blocklist))
	if s.Client != nil {
		add("categories", len(s.Client.Categories))
	}
	for _, r := range s.RootFolders {
		ss.Roots = append(ss.Roots, r.Path)
	}
	return ss
}

func (s *Service) cached(id int64) (*runCache, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cache[id]
	if !ok || s.now().Sub(c.at) > cacheTTL {
		return nil, ErrExpired
	}
	return c, nil
}

// plan builds and stores the plan of a run from its cached snapshots.
func (s *Service) plan(ctx context.Context, id int64, o Options, sum *Summary) (*planner, error) {
	c, err := s.cached(id)
	if err != nil {
		return nil, err
	}
	snaps := make([]*Snapshot, len(c.snaps))
	copy(snaps, c.snaps)
	plan, p, err := Build(ctx, s.L, o, snaps, c.secrets)
	if err != nil {
		return nil, err
	}
	sum.PathChecks = CheckPaths(Translator{Mappings: o.PathMappings, DataRoots: s.L.DataRoots, Cache: &lfs.PathCache{Roots: s.L.DataRoots}}, snaps)
	fillSummary(sum, plan)
	if err := s.storeItems(ctx, id, plan.Items); err != nil {
		return nil, err
	}
	s.setRun(ctx, id, "planned", &RunProgress{Phase: "planned", Done: len(plan.Items), Total: len(plan.Items)}, sum, nil, false)
	return p, nil
}

func fillSummary(sum *Summary, plan *Plan) {
	sum.Counts = plan.Counts()
	sum.Warnings = plan.Warnings
	sum.Attention, sum.Statuses, sum.Conflicts = map[string]int{}, map[string]int{}, 0
	for _, it := range plan.Items {
		for _, a := range it.Attention {
			sum.Attention[a]++
		}
		sum.Statuses[it.Status]++
		if it.Conflict != nil {
			sum.Conflicts++
		}
	}
}

func (s *Service) storeItems(ctx context.Context, id int64, items []*Item) error {
	return s.L.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM migrate_items WHERE run_id = ?`, id); err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO migrate_items (run_id, seq, key, source_id, category, name, action, reason, message,
			conflict, attention, status, local_ref, error) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, it := range items {
			conflict := ""
			if it.Conflict != nil {
				conflict = jsonText(it.Conflict)
			}
			att := it.Attention
			if att == nil {
				att = []string{}
			}
			if _, err := stmt.ExecContext(ctx, id, it.Seq, it.Key, it.SourceID, it.Category, truncate(it.Name, 500), it.Action, it.Reason,
				truncate(it.Message, 2000), conflict, jsonText(att), it.Status, it.LocalRef, truncate(it.Error, 1000)); err != nil {
				return err
			}
		}
		return nil
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Replan changes the path mappings and conflict choices of an analysed run
// and plans again (nothing is read from the sources).
func (s *Service) Replan(ctx context.Context, id int64, mappings []PathMapping, resolutions map[string]string, prowlarr map[int64]string) (Run, error) {
	r, err := s.Run(ctx, id)
	if err != nil {
		return r, err
	}
	if r.Status != "analysed" && r.Status != "planned" {
		return r, ErrWrongState
	}
	if mappings != nil {
		r.Options.PathMappings = mappings
	}
	if resolutions != nil {
		r.Options.Resolutions = resolutions
	}
	for i := range r.Options.Sources {
		if m, ok := prowlarr[r.Options.Sources[i].ID]; ok {
			r.Options.Sources[i].ProwlarrMode = m
		}
	}
	if err := s.checkOptions(ctx, &r.Options); err != nil {
		return r, err
	}
	s.saveOptions(ctx, id, r.Options)
	sum := r.Summary
	if _, err := s.plan(ctx, id, r.Options, &sum); err != nil {
		return r, err
	}
	return s.Run(ctx, id)
}

// CheckPaths counts the files found with the given mappings (live check).
func (s *Service) CheckPaths(_ context.Context, id int64, mappings []PathMapping) ([]PathCheck, error) {
	c, err := s.cached(id)
	if err != nil {
		return nil, err
	}
	return CheckPaths(Translator{Mappings: mappings, DataRoots: s.L.DataRoots, Cache: &lfs.PathCache{Roots: s.L.DataRoots}}, c.snaps), nil
}

// Import starts applying a planned run.
func (s *Service) Import(ctx context.Context, id int64) (Run, error) {
	r, err := s.Run(ctx, id)
	if err != nil {
		return r, err
	}
	if r.Status != "planned" {
		return r, ErrWrongState
	}
	if err := s.busy(ctx); err != nil {
		return r, err
	}
	if _, err := s.cached(id); err != nil {
		return r, err
	}
	s.setRun(ctx, id, "importing", &RunProgress{Phase: "importing"}, nil, nil, false)
	if _, _, err := s.Jobs.Enqueue(ctx, TaskApply, runArgs{RunID: id}, jobs.EnqueueOptions{UniqueKey: "migrate:" + strconv.FormatInt(id, 10),
		MaxAttempts: 1}); err != nil {
		return r, err
	}
	return s.Run(ctx, id)
}

// RunApply is the import job: the plan is built again from the same
// snapshots and options (so the import does what the preview showed) and
// applied.
func (s *Service) RunApply(ctx context.Context, id int64) error {
	started := s.now()
	r, err := s.Run(ctx, id)
	if err != nil {
		return jobs.Permanent(err)
	}
	sum := r.Summary
	p, err := s.plan(ctx, id, r.Options, &sum)
	if err != nil {
		msg := err.Error()
		s.setRun(ctx, id, "failed", &RunProgress{Phase: "failed", Message: msg}, nil, &msg, true)
		return jobs.Permanent(err)
	}
	s.setRun(ctx, id, "importing", &RunProgress{Phase: "importing", Total: len(p.plan.Items)}, nil, nil, false)
	last := time.Time{}
	err = p.Apply(ctx, id, func(done, total int, what string) {
		if time.Since(last) < 500*time.Millisecond && done < total {
			return
		}
		last = time.Now()
		s.setRun(ctx, id, "", &RunProgress{Phase: "importing", Done: done, Total: total, Message: what}, nil, nil, false)
	})
	fillSummary(&sum, p.plan)
	sum.DurationMS = s.now().Sub(started).Milliseconds()
	refs := p.Refresh()
	if len(refs) > 0 {
		sum.Refresh = &RefreshState{Total: len(refs)}
	}
	_ = s.storeItems(context.WithoutCancel(ctx), id, p.plan.Items)
	if err != nil {
		msg := err.Error()
		status := "failed"
		if errors.Is(err, context.Canceled) {
			status, msg = "cancelled", "cancelled"
		}
		s.setRun(ctx, id, status, &RunProgress{Phase: status, Message: msg}, &sum, &msg, true)
		return jobs.Permanent(err)
	}
	s.setRun(ctx, id, "done", &RunProgress{Phase: "done", Done: len(p.plan.Items), Total: len(p.plan.Items)}, &sum, nil, true)
	s.log().Info("import finished", "run", id, "items", len(p.plan.Items), "took_ms", sum.DurationMS)
	if len(refs) > 0 && s.Jobs != nil {
		if _, _, err := s.Jobs.Enqueue(context.WithoutCancel(ctx), TaskRefresh, refreshArgs{RunID: id, Refs: refs},
			jobs.EnqueueOptions{UniqueKey: "migrate-refresh:" + strconv.FormatInt(id, 10), MaxAttempts: 1}); err != nil {
			s.log().Warn("queue metadata refresh", "err", err)
		}
	}
	return nil
}

type refreshArgs struct {
	RunID int64    `json:"runId"`
	Refs  []string `json:"refs"`
}

// RunRefresh refreshes the metadata of what an import added, one item at a
// time within the providers' rate limits (D-064).
func (s *Service) RunRefresh(ctx context.Context, j *jobs.Job) error {
	var a refreshArgs
	if err := j.Decode(&a); err != nil {
		return jobs.Permanent(err)
	}
	st := &RefreshState{Total: len(a.Refs)}
	save := func() {
		r, err := s.Run(ctx, a.RunID)
		if err != nil {
			return
		}
		sum := r.Summary
		sum.Refresh = st
		s.setRun(ctx, a.RunID, "", nil, &sum, nil, false)
	}
	last := time.Time{}
	for i, ref := range a.Refs {
		if ctx.Err() != nil {
			break
		}
		r, err := library.ParseRef(ref)
		if err != nil {
			continue
		}
		switch r.Kind {
		case library.RefMovie:
			_, err = s.L.Movies.Refresh(ctx, r.ID)
		case library.RefSeries:
			err = s.L.Series.RefreshImported(ctx, r.ID)
		case library.RefArtist:
			if s.L.Music != nil {
				_, err = s.L.Music.Refresh(ctx, r.ID, true)
			}
		}
		st.Done = i + 1
		if err != nil {
			st.Failed++
			st.Error = err.Error()
		}
		if time.Since(last) > 2*time.Second {
			last = time.Now()
			save()
		}
	}
	save()
	return nil
}

// Cancel stops a running analyse or import.
func (s *Service) Cancel(ctx context.Context, id int64) (Run, error) {
	r, err := s.Run(ctx, id)
	if err != nil {
		return r, err
	}
	if r.Status != "analysing" && r.Status != "importing" && r.Status != "analysed" && r.Status != "planned" {
		return r, ErrWrongState
	}
	if s.Jobs != nil {
		if list, err := s.Jobs.List(ctx, jobs.Running, 100); err == nil {
			for _, j := range list {
				var a runArgs
				if (j.Kind == TaskAnalyse || j.Kind == TaskApply) && json.Unmarshal(j.Args, &a) == nil && a.RunID == id {
					_ = s.Jobs.Cancel(ctx, j.ID)
				}
			}
		}
		if list, err := s.Jobs.List(ctx, jobs.Queued, 100); err == nil {
			for _, j := range list {
				var a runArgs
				if (j.Kind == TaskAnalyse || j.Kind == TaskApply) && json.Unmarshal(j.Args, &a) == nil && a.RunID == id {
					_ = s.Jobs.Cancel(ctx, j.ID)
				}
			}
		}
	}
	msg := "cancelled"
	s.setRun(ctx, id, "cancelled", &RunProgress{Phase: "cancelled"}, nil, &msg, true)
	return s.Run(ctx, id)
}

// ItemFilter selects plan items.
type ItemFilter struct {
	Category  string
	Action    string
	Status    string
	Attention bool
	Conflicts bool
	SourceID  int64
	Query     string
	Offset    int
	Limit     int
}

// Items lists a run's plan items.
func (s *Service) Items(ctx context.Context, id int64, f ItemFilter) ([]Item, int, error) {
	where, args := []string{"run_id = ?"}, []any{id}
	if f.Category != "" {
		where, args = append(where, "category = ?"), append(args, f.Category)
	}
	if f.Action != "" {
		where, args = append(where, "action = ?"), append(args, f.Action)
	}
	if f.Status != "" {
		where, args = append(where, "status = ?"), append(args, f.Status)
	}
	if f.SourceID > 0 {
		where, args = append(where, "source_id = ?"), append(args, f.SourceID)
	}
	if f.Attention {
		where = append(where, "attention <> '[]'")
	}
	if f.Conflicts {
		where = append(where, "conflict <> ''")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where, args = append(where, "name LIKE ? ESCAPE '\\'"), append(args, "%"+likeEscape(q)+"%")
	}
	w := strings.Join(where, " AND ")
	var total int
	if err := s.L.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM migrate_items WHERE `+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	rows, err := s.L.DB.R.QueryContext(ctx, `SELECT seq, key, source_id, category, name, action, reason, message, conflict, attention, status,
		local_ref, error FROM migrate_items WHERE `+w+` ORDER BY seq LIMIT ? OFFSET ?`, append(args, f.Limit, max(f.Offset, 0))...) // #nosec G202 -- constant fragments with placeholders
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		var it Item
		var conflict, att string
		if err := rows.Scan(&it.Seq, &it.Key, &it.SourceID, &it.Category, &it.Name, &it.Action, &it.Reason, &it.Message, &conflict, &att,
			&it.Status, &it.LocalRef, &it.Error); err != nil {
			return nil, 0, err
		}
		if conflict != "" {
			it.Conflict = &Conflict{}
			_ = json.Unmarshal([]byte(conflict), it.Conflict)
		}
		_ = json.Unmarshal([]byte(att), &it.Attention)
		out = append(out, it)
	}
	return out, total, rows.Err()
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Report is the downloadable record of a run.
type Report struct {
	Run       Run      `json:"run"`
	Sources   []Source `json:"sources"`
	Items     []Item   `json:"items"`
	Generated string   `json:"generated"`
}

// Report returns everything about a run (no secrets).
func (s *Service) Report(ctx context.Context, id int64) (Report, error) {
	r, err := s.Run(ctx, id)
	if err != nil {
		return Report{}, err
	}
	items, _, err := s.Items(ctx, id, ItemFilter{Limit: 1000})
	if err != nil {
		return Report{}, err
	}
	for off := len(items); ; off += 1000 {
		more, total, err := s.Items(ctx, id, ItemFilter{Limit: 1000, Offset: off})
		if err != nil {
			return Report{}, err
		}
		items = append(items, more...)
		if len(more) == 0 || len(items) >= total {
			break
		}
	}
	srcs, _ := s.Sources(ctx)
	return Report{Run: r, Sources: srcs, Items: items, Generated: s.now().UTC().Format(time.RFC3339)}, nil
}

// CheckItem is one line of the switch-over checklist.
type CheckItem struct {
	Key    string         `json:"key"`
	Done   bool           `json:"done"`
	Manual bool           `json:"manual"`
	Params map[string]any `json:"params,omitempty"`
}

// Checklist is what to do after importing so the old applications stop and
// Locarr takes over. Steps on the old applications are instructions only:
// Locarr never changes them.
func (s *Service) Checklist(ctx context.Context) ([]CheckItem, error) {
	srcs, err := s.Sources(ctx)
	if err != nil {
		return nil, err
	}
	var out []CheckItem
	var finished int64
	_ = s.L.DB.R.QueryRowContext(ctx, `SELECT COALESCE(max(finished_at), 0) FROM migrate_runs WHERE status = 'done'`).Scan(&finished)
	out = append(out, CheckItem{Key: "import_done", Done: finished > 0})
	kinds := map[Kind][]string{}
	for _, src := range srcs {
		kinds[src.Kind] = append(kinds[src.Kind], src.Name)
	}
	for _, k := range []Kind{Sonarr, Radarr, Lidarr} {
		if names, ok := kinds[k]; ok {
			out = append(out, CheckItem{Key: "stop_" + string(k), Manual: true, Params: map[string]any{"names": strings.Join(names, ", ")}})
		}
	}
	if names, ok := kinds[Prowlarr]; ok {
		out = append(out, CheckItem{Key: "prowlarr_apps", Manual: true, Params: map[string]any{"names": strings.Join(names, ", ")}})
	}
	if names, ok := append(kinds[Overseerr], kinds[Seerr]...), len(kinds[Overseerr])+len(kinds[Seerr]) > 0; ok {
		out = append(out, CheckItem{Key: "stop_requests", Manual: true, Params: map[string]any{"names": strings.Join(names, ", ")}})
	}
	if names, ok := kinds[Bazarr]; ok {
		out = append(out, CheckItem{Key: "stop_bazarr", Manual: true, Params: map[string]any{"names": strings.Join(names, ", ")}})
	}
	clients, _ := s.L.Downloads.Clients(ctx)
	enabled, cats := 0, []string{}
	for _, c := range clients {
		if c.Enabled {
			enabled++
			for _, x := range []string{c.CategoryMovies, c.CategorySeries, c.CategoryMusic} {
				if x != "" && !contains(cats, x) {
					cats = append(cats, x)
				}
			}
		}
	}
	sort.Strings(cats)
	out = append(out, CheckItem{Key: "download_clients", Done: enabled > 0, Params: map[string]any{"clients": enabled, "categories": strings.Join(cats, ", ")}})
	ix, _ := s.L.Indexers.List(ctx)
	rssIx := 0
	for _, x := range ix {
		if x.Enabled && x.EnableRSS {
			rssIx++
		}
	}
	interval, _ := settings.Get(ctx, s.L.Settings, indexers.RSSIntervalMinutes)
	out = append(out, CheckItem{Key: "rss", Done: rssIx > 0 && interval > 0, Params: map[string]any{"indexers": rssIx, "minutes": interval}})
	var searches int
	_ = s.L.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM search_log WHERE source IN ('interactive', 'automatic') AND started_at > ?`, finished).Scan(&searches)
	out = append(out, CheckItem{Key: "test_search", Done: finished > 0 && searches > 0})
	var secrets int
	_ = s.L.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM migrate_items WHERE run_id = (SELECT max(id) FROM migrate_runs WHERE status = 'done')
		AND attention LIKE '%needs_secret%'`).Scan(&secrets)
	out = append(out, CheckItem{Key: "secrets", Done: finished > 0 && secrets == 0, Params: map[string]any{"count": secrets}})
	return out, nil
}
