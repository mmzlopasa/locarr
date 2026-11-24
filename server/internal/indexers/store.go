package indexers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
)

// Errors.
var (
	ErrNotFound      = errors.New("indexer not found")
	ErrNameTaken     = errors.New("an indexer with this name already exists")
	ErrGrabLimit     = errors.New("the indexer's daily grab limit is reached")
	ErrBadToken      = errors.New("invalid or expired download link")
	ErrNoDownload    = errors.New("the release has no download link")
	ErrSourceMissing = errors.New("indexer source not found")
)

// InvalidError is a validation error for one field.
type InvalidError struct {
	Field   string
	Message string
}

func (e *InvalidError) Error() string { return e.Field + ": " + e.Message }

func invalid(field, msg string) error { return &InvalidError{Field: field, Message: msg} }

// Categories are the category ids used per kind of media.
type Categories struct {
	Movies []int `json:"movies"`
	TV     []int `json:"tv"`
	Music  []int `json:"music"`
	Books  []int `json:"books"`
	// Adult (6xxx) is empty unless an administrator opts the indexer in
	// (D-103); adult searches only go to indexers that have it.
	Adult []int `json:"adult,omitempty"`
}

// For returns the categories for a search type (nil for a generic search:
// every configured category).
func (c Categories) For(t SearchType) []int {
	switch t {
	case SearchMovie:
		return c.Movies
	case SearchTV:
		return c.TV
	case SearchMusic:
		return c.Music
	case SearchBook:
		return c.Books
	case SearchAdult:
		return c.Adult
	}
	return nil
}

// All returns every configured category, sorted and unique.
func (c Categories) All() []int {
	out := slices.Concat(c.Movies, c.TV, c.Music, c.Books, c.Adult)
	slices.Sort(out)
	return slices.Compact(out)
}

// Empty reports whether no category is set.
func (c Categories) Empty() bool {
	return len(c.Movies)+len(c.TV)+len(c.Music)+len(c.Books) == 0
}

// Health is an indexer's recent track record.
type Health struct {
	Failures      int
	RetryAt       *time.Time
	LastError     string
	LastErrorCode string
	LastErrorAt   *time.Time
	LastSuccessAt *time.Time
	// ParseFailures counts result rows a built-in site returned that could
	// not be read since the last reset.
	ParseFailures int
	SelfTestAt    *time.Time
	SelfTestOK    *bool
}

// HealthStatus values.
const (
	HealthHealthy  = "healthy"
	HealthFailing  = "failing"
	HealthDisabled = "disabled"
	HealthUnknown  = "unknown"
)

// Indexer is a configured indexer. Secrets are never loaded into it.
type Indexer struct {
	ID                int64
	Name              string
	Kind              Kind
	BaseURL           string
	APIPath           string
	HasAPIKey         bool
	Enabled           bool
	EnableRSS         bool
	EnableAutomatic   bool
	EnableInteractive bool
	Priority          int
	Tags              []string
	Categories        Categories
	MinimumSeeders    int
	SeedRatio         *float64
	SeedTimeMinutes   *int
	RequestLimit      *int
	RequestPeriodSec  *int
	GrabLimitPerDay   *int
	TimeoutSeconds    int
	ProxyURL          string
	ProxyUsername     string
	HasProxyPassword  bool
	FlareSolverrURL   string
	Definition        string            // site definition (Native)
	Options           map[string]string // per-site settings (Native)
	SourceID          *int64
	SourceName        string
	SourceRef         string
	Caps              *znab.Caps
	CapsUpdatedAt     *time.Time
	Health            Health
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Protocol of the indexer.
func (i *Indexer) Protocol() Protocol { return i.Kind.Protocol() }

// HealthStatus summarises Health at now.
func (i *Indexer) HealthStatus() string {
	switch {
	case !i.Enabled:
		return HealthDisabled
	case i.Health.Failures > 0:
		return HealthFailing
	case i.Health.LastSuccessAt == nil:
		return HealthUnknown
	}
	return HealthHealthy
}

// InBackoff reports whether the indexer is skipped because of recent failures.
func (i *Indexer) InBackoff(now time.Time) bool {
	return i.Health.RetryAt != nil && now.Before(*i.Health.RetryAt)
}

// Input creates or changes an indexer. Nil secret pointers keep the stored
// value; an empty string removes it.
type Input struct {
	Name              string
	Kind              Kind
	BaseURL           string
	APIPath           string
	APIKey            *string
	Enabled           bool
	EnableRSS         bool
	EnableAutomatic   bool
	EnableInteractive bool
	Priority          int
	Tags              []string
	Categories        *Categories // nil: from caps (create) or unchanged (update)
	MinimumSeeders    int
	SeedRatio         *float64
	SeedTimeMinutes   *int
	RequestLimit      *int
	RequestPeriodSec  *int
	GrabLimitPerDay   *int
	TimeoutSeconds    int
	ProxyURL          string
	ProxyUsername     string
	ProxyPassword     *string
	FlareSolverrURL   string
	Definition        string
	Options           map[string]string
}

// DefaultInput returns an Input with the defaults the API documents.
func DefaultInput() Input {
	return Input{Enabled: true, EnableRSS: true, EnableAutomatic: true, EnableInteractive: true, Priority: 25,
		MinimumSeeders: 1, TimeoutSeconds: 30, APIPath: "/api"}
}

func (s *Service) normalize(ctx context.Context, in *Input) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 {
		return invalid("name", "must be 1 to 100 characters")
	}
	if in.Kind != Torznab && in.Kind != Newznab && in.Kind != Native {
		return invalid("kind", "must be torznab, newznab or native")
	}
	in.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	if in.Kind == Native {
		def, ok := s.Definition(in.Definition)
		if !ok {
			return invalid("definition", "unknown site definition")
		}
		if in.BaseURL == "" {
			in.BaseURL = def.DefaultURL()
		}
		in.APIPath = "/"
		if in.Options == nil {
			in.Options = map[string]string{}
		}
		if err := def.CheckOptions(in.Options); err != nil {
			return invalid("options", err.Error())
		}
		if in.APIKey != nil && *in.APIKey != "" {
			return invalid("apiKey", "built-in sites take no API key")
		}
		// A site without a list of latest releases has nothing for RSS sync.
		in.EnableRSS = in.EnableRSS && def.SupportsRSS()
	} else {
		in.Definition, in.Options = "", nil
	}
	pol, err := s.policy(ctx)
	if err != nil {
		return err
	}
	if err := pol.CheckURL(in.BaseURL); err != nil {
		return invalid("baseUrl", err.Error())
	}
	if u, _ := url.Parse(in.BaseURL); u.RawQuery != "" {
		return invalid("baseUrl", "must not contain query parameters (the API key has its own field)")
	}
	in.APIPath = strings.TrimSpace(in.APIPath)
	if in.APIPath == "" && in.Kind != Native {
		in.APIPath = "/api"
	}
	if !strings.HasPrefix(in.APIPath, "/") {
		in.APIPath = "/" + in.APIPath
	}
	if strings.ContainsAny(in.APIPath, "?#") {
		return invalid("apiPath", "must be a path")
	}
	if in.APIKey != nil {
		k := strings.TrimSpace(*in.APIKey)
		in.APIKey = &k
	}
	if in.Priority == 0 {
		in.Priority = 25
	}
	if in.Priority < 1 || in.Priority > 50 {
		return invalid("priority", "must be between 1 and 50")
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 30
	}
	if in.TimeoutSeconds < 2 || in.TimeoutSeconds > 120 {
		return invalid("timeoutSeconds", "must be between 2 and 120")
	}
	if (in.RequestLimit == nil) != (in.RequestPeriodSec == nil) {
		return invalid("requestLimit", "set both the request limit and its period, or neither")
	}
	tags := make([]string, 0, len(in.Tags))
	for _, t := range in.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	slices.Sort(tags)
	in.Tags = tags
	in.ProxyURL = strings.TrimSpace(in.ProxyURL)
	if in.ProxyURL != "" {
		u, err := url.Parse(in.ProxyURL)
		if err != nil || u.Host == "" || !slices.Contains([]string{"http", "https", "socks5", "socks5h"}, u.Scheme) {
			return invalid("proxyUrl", "must be an http://, https:// or socks5:// URL")
		}
		if u.User != nil {
			return invalid("proxyUrl", "put the user name and password in their own fields")
		}
		if err := pol.CheckURL(strings.Replace(in.ProxyURL, u.Scheme+"://", "http://", 1)); err != nil {
			return invalid("proxyUrl", err.Error())
		}
	}
	in.FlareSolverrURL = strings.TrimRight(strings.TrimSpace(in.FlareSolverrURL), "/")
	if in.FlareSolverrURL != "" {
		if err := pol.CheckURL(in.FlareSolverrURL); err != nil {
			return invalid("flaresolverrUrl", err.Error())
		}
	}
	if in.Categories != nil {
		c := *in.Categories
		for _, l := range [][]int{c.Movies, c.TV, c.Music, c.Books} {
			for _, id := range l {
				if id <= 0 {
					return invalid("categories", "ids must be positive")
				}
			}
		}
	}
	return nil
}

const indexerColumns = `i.id, i.name, i.kind, i.base_url, i.api_path, i.api_key IS NOT NULL, i.enabled, i.enable_rss,
	i.enable_automatic, i.enable_interactive, i.priority, i.tags, i.categories, i.minimum_seeders, i.seed_ratio,
	i.seed_time_minutes, i.request_limit, i.request_period_seconds, i.grab_limit_per_day, i.timeout_seconds,
	i.proxy_url, i.proxy_username, i.proxy_password IS NOT NULL, i.flaresolverr_url, i.source_id, s.name, i.source_ref,
	i.caps, i.caps_updated_at, i.failures, i.retry_at, i.last_error, i.last_error_code, i.last_error_at,
	i.last_success_at, i.created_at, i.updated_at, i.definition, i.options, i.parse_failures, i.self_test_at, i.self_test_ok`

const indexerFrom = ` FROM indexers i LEFT JOIN indexer_sources s ON s.id = i.source_id`

func scanIndexer(row interface{ Scan(...any) error }) (Indexer, error) {
	var (
		i                                     Indexer
		kind, tags, cats                      string
		seedRatio                             sql.NullFloat64
		seedTime, reqLimit, reqPeriod, grabs  sql.NullInt64
		proxyURL, proxyUser, fs, srcName, ref sql.NullString
		srcID                                 sql.NullInt64
		caps, lastErr, lastErrCode            sql.NullString
		capsAt, retry, errAt, okAt            sql.NullInt64
		created, updated                      int64
		definition                            sql.NullString
		options                               string
		selfAt, selfOK                        sql.NullInt64
	)
	if err := row.Scan(&i.ID, &i.Name, &kind, &i.BaseURL, &i.APIPath, &i.HasAPIKey, &i.Enabled, &i.EnableRSS,
		&i.EnableAutomatic, &i.EnableInteractive, &i.Priority, &tags, &cats, &i.MinimumSeeders, &seedRatio,
		&seedTime, &reqLimit, &reqPeriod, &grabs, &i.TimeoutSeconds, &proxyURL, &proxyUser, &i.HasProxyPassword, &fs,
		&srcID, &srcName, &ref, &caps, &capsAt, &i.Health.Failures, &retry, &lastErr, &lastErrCode, &errAt, &okAt,
		&created, &updated, &definition, &options, &i.Health.ParseFailures, &selfAt, &selfOK); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return i, ErrNotFound
		}
		return i, err
	}
	i.Kind = Kind(kind)
	if definition.Valid {
		i.Kind, i.Definition = Native, definition.String
		_ = json.Unmarshal([]byte(options), &i.Options)
		if i.Options == nil {
			i.Options = map[string]string{}
		}
	}
	i.Health.SelfTestAt = db.NullTime(selfAt)
	if selfOK.Valid {
		ok := selfOK.Int64 == 1
		i.Health.SelfTestOK = &ok
	}
	_ = json.Unmarshal([]byte(tags), &i.Tags)
	if i.Tags == nil {
		i.Tags = []string{}
	}
	_ = json.Unmarshal([]byte(cats), &i.Categories)
	if seedRatio.Valid {
		i.SeedRatio = &seedRatio.Float64
	}
	i.SeedTimeMinutes, i.RequestLimit, i.RequestPeriodSec, i.GrabLimitPerDay = nullInt(seedTime), nullInt(reqLimit), nullInt(reqPeriod), nullInt(grabs)
	i.ProxyURL, i.ProxyUsername, i.FlareSolverrURL = proxyURL.String, proxyUser.String, fs.String
	if srcID.Valid {
		i.SourceID = &srcID.Int64
		i.SourceName = srcName.String
		i.SourceRef = ref.String
	}
	if caps.Valid && caps.String != "" {
		var c znab.Caps
		if json.Unmarshal([]byte(caps.String), &c) == nil {
			i.Caps = &c
		}
	}
	i.CapsUpdatedAt = db.NullTime(capsAt)
	i.Health.RetryAt, i.Health.LastErrorAt, i.Health.LastSuccessAt = db.NullTime(retry), db.NullTime(errAt), db.NullTime(okAt)
	i.Health.LastError, i.Health.LastErrorCode = lastErr.String, lastErrCode.String
	i.CreatedAt, i.UpdatedAt = db.Time(created), db.Time(updated)
	return i, nil
}

func nullInt(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

func intArg(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func floatArg(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func strArg(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// List returns every indexer by priority, then name.
func (s *Service) List(ctx context.Context) ([]Indexer, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+indexerColumns+indexerFrom+` ORDER BY i.priority, i.name_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Indexer{}
	for rows.Next() {
		i, err := scanIndexer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// Get returns one indexer.
func (s *Service) Get(ctx context.Context, id int64) (Indexer, error) {
	return scanIndexer(s.DB.R.QueryRowContext(ctx, `SELECT `+indexerColumns+indexerFrom+` WHERE i.id = ?`, id))
}

func secretAAD(id int64, field string) string {
	return "indexer:" + strconv.FormatInt(id, 10) + ":" + field
}

// Create adds an indexer. Empty categories are filled from the indexer's
// capabilities when they can be fetched (failure to fetch is not an error).
func (s *Service) Create(ctx context.Context, in Input) (Indexer, error) {
	if err := s.normalize(ctx, &in); err != nil {
		return Indexer{}, err
	}
	var caps *znab.Caps
	if in.Categories == nil || in.Categories.Empty() {
		cctx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutSeconds)*time.Second)
		if c, err := s.capsFor(cctx, in, nil); err == nil {
			caps = c
			def := DefaultCategories(c)
			in.Categories = &def
		}
		cancel()
	}
	if in.Categories == nil {
		in.Categories = &Categories{}
	}
	return s.insert(ctx, in, caps, nil, "")
}

func (s *Service) insert(ctx context.Context, in Input, caps *znab.Caps, sourceID *int64, sourceRef string) (Indexer, error) {
	now := s.now()
	tags, _ := json.Marshal(in.Tags)
	cats, _ := json.Marshal(normCats(*in.Categories))
	var capsJSON, capsAt any
	if caps != nil {
		b, _ := json.Marshal(caps)
		capsJSON, capsAt = string(b), db.Millis(now)
	}
	var id int64
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM indexers WHERE name_key = ?`, strings.ToLower(in.Name)).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrNameTaken
		}
		var src any
		if sourceID != nil {
			src = *sourceID
		}
		err := tx.QueryRowContext(ctx, `INSERT INTO indexers (name, name_key, kind, protocol, base_url, api_path, enabled,
			enable_rss, enable_automatic, enable_interactive, priority, tags, categories, minimum_seeders, seed_ratio,
			seed_time_minutes, request_limit, request_period_seconds, grab_limit_per_day, timeout_seconds, proxy_url,
			proxy_username, flaresolverr_url, source_id, source_ref, caps, caps_updated_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			in.Name, strings.ToLower(in.Name), string(in.Kind.stored()), string(in.Kind.Protocol()), in.BaseURL, in.APIPath, in.Enabled,
			in.EnableRSS, in.EnableAutomatic, in.EnableInteractive, in.Priority, string(tags), string(cats), in.MinimumSeeders,
			floatArg(in.SeedRatio), intArg(in.SeedTimeMinutes), intArg(in.RequestLimit), intArg(in.RequestPeriodSec),
			intArg(in.GrabLimitPerDay), in.TimeoutSeconds, strArg(in.ProxyURL), strArg(in.ProxyUsername),
			strArg(in.FlareSolverrURL), src, strArg(sourceRef), capsJSON, capsAt, db.Millis(now), db.Millis(now)).Scan(&id)
		if err != nil {
			return err
		}
		if err := s.writeNative(ctx, tx, id, in); err != nil {
			return err
		}
		return s.writeSecrets(ctx, tx, id, in)
	})
	if err != nil {
		return Indexer{}, err
	}
	s.forget(id)
	return s.Get(ctx, id)
}

// writeNative stores a built-in site's definition and settings.
func (s *Service) writeNative(ctx context.Context, q db.Querier, id int64, in Input) error {
	if in.Kind != Native {
		_, err := q.ExecContext(ctx, `UPDATE indexers SET definition = NULL, options = '{}' WHERE id = ?`, id)
		return err
	}
	opts, _ := json.Marshal(in.Options)
	_, err := q.ExecContext(ctx, `UPDATE indexers SET definition = ?, options = ? WHERE id = ?`, in.Definition, string(opts), id)
	return err
}

func (s *Service) writeSecrets(ctx context.Context, q db.Querier, id int64, in Input) error {
	set := func(col, field string, v *string) error {
		if v == nil {
			return nil
		}
		if *v == "" {
			_, err := q.ExecContext(ctx, `UPDATE indexers SET `+col+` = NULL WHERE id = ?`, id)
			return err
		}
		ct, err := s.Box.Seal([]byte(*v), secretAAD(id, field))
		if err != nil {
			return err
		}
		_, err = q.ExecContext(ctx, `UPDATE indexers SET `+col+` = ? WHERE id = ?`, ct, id)
		return err
	}
	if err := set("api_key", "apiKey", in.APIKey); err != nil {
		return err
	}
	return set("proxy_password", "proxyPassword", in.ProxyPassword)
}

// Update changes an indexer.
func (s *Service) Update(ctx context.Context, id int64, in Input) (Indexer, error) {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return Indexer{}, err
	}
	if err := s.normalize(ctx, &in); err != nil {
		return Indexer{}, err
	}
	if in.Categories == nil {
		in.Categories = &cur.Categories
	}
	tags, _ := json.Marshal(in.Tags)
	cats, _ := json.Marshal(normCats(*in.Categories))
	// A changed address or key makes the stored capabilities and health stale.
	endpointChanged := in.BaseURL != cur.BaseURL || in.APIPath != cur.APIPath || in.Kind != cur.Kind || in.APIKey != nil ||
		in.Definition != cur.Definition
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM indexers WHERE name_key = ? AND id <> ?`,
			strings.ToLower(in.Name), id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrNameTaken
		}
		_, err := tx.ExecContext(ctx, `UPDATE indexers SET name = ?, name_key = ?, kind = ?, protocol = ?, base_url = ?,
			api_path = ?, enabled = ?, enable_rss = ?, enable_automatic = ?, enable_interactive = ?, priority = ?, tags = ?,
			categories = ?, minimum_seeders = ?, seed_ratio = ?, seed_time_minutes = ?, request_limit = ?,
			request_period_seconds = ?, grab_limit_per_day = ?, timeout_seconds = ?, proxy_url = ?, proxy_username = ?,
			flaresolverr_url = ?, updated_at = ? WHERE id = ?`,
			in.Name, strings.ToLower(in.Name), string(in.Kind.stored()), string(in.Kind.Protocol()), in.BaseURL, in.APIPath, in.Enabled,
			in.EnableRSS, in.EnableAutomatic, in.EnableInteractive, in.Priority, string(tags), string(cats), in.MinimumSeeders,
			floatArg(in.SeedRatio), intArg(in.SeedTimeMinutes), intArg(in.RequestLimit), intArg(in.RequestPeriodSec),
			intArg(in.GrabLimitPerDay), in.TimeoutSeconds, strArg(in.ProxyURL), strArg(in.ProxyUsername),
			strArg(in.FlareSolverrURL), db.Millis(s.now()), id)
		if err != nil {
			return err
		}
		if endpointChanged {
			if _, err := tx.ExecContext(ctx, `UPDATE indexers SET failures = 0, retry_at = NULL WHERE id = ?`, id); err != nil {
				return err
			}
		}
		if err := s.writeNative(ctx, tx, id, in); err != nil {
			return err
		}
		return s.writeSecrets(ctx, tx, id, in)
	})
	if err != nil {
		return Indexer{}, err
	}
	s.forget(id)
	return s.Get(ctx, id)
}

// Delete removes an indexer (its seen releases go with it; the search log keeps its name).
func (s *Service) Delete(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM indexers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.forget(id)
	return nil
}

// secret decrypts one of an indexer's secrets ("" when unset).
func (s *Service) secret(ctx context.Context, id int64, field string) (string, error) {
	col := map[string]string{"apiKey": "api_key", "proxyPassword": "proxy_password"}[field]
	if col == "" {
		return "", fmt.Errorf("unknown secret %q", field)
	}
	var ct []byte
	if err := s.DB.R.QueryRowContext(ctx, `SELECT `+col+` FROM indexers WHERE id = ?`, id).Scan(&ct); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	if ct == nil {
		return "", nil
	}
	pt, err := s.Box.Open(ct, secretAAD(id, field))
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// inputOf turns a stored indexer back into an Input (secrets untouched).
func inputOf(i Indexer) Input {
	cats := i.Categories
	return Input{Name: i.Name, Kind: i.Kind, BaseURL: i.BaseURL, APIPath: i.APIPath, Enabled: i.Enabled,
		EnableRSS: i.EnableRSS, EnableAutomatic: i.EnableAutomatic, EnableInteractive: i.EnableInteractive,
		Priority: i.Priority, Tags: i.Tags, Categories: &cats, MinimumSeeders: i.MinimumSeeders, SeedRatio: i.SeedRatio,
		SeedTimeMinutes: i.SeedTimeMinutes, RequestLimit: i.RequestLimit, RequestPeriodSec: i.RequestPeriodSec,
		GrabLimitPerDay: i.GrabLimitPerDay, TimeoutSeconds: i.TimeoutSeconds, ProxyURL: i.ProxyURL,
		ProxyUsername: i.ProxyUsername, FlareSolverrURL: i.FlareSolverrURL, Definition: i.Definition, Options: i.Options}
}

func normCats(c Categories) Categories {
	norm := func(l []int) []int {
		out := slices.Clone(l)
		slices.Sort(out)
		out = slices.Compact(out)
		if out == nil {
			out = []int{}
		}
		return out
	}
	return Categories{Movies: norm(c.Movies), TV: norm(c.TV), Music: norm(c.Music), Books: norm(c.Books), Adult: norm(c.Adult)}
}

// saveCaps stores fetched capabilities.
func (s *Service) saveCaps(ctx context.Context, id int64, c *znab.Caps) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.DB.W.ExecContext(ctx, `UPDATE indexers SET caps = ?, caps_updated_at = ? WHERE id = ?`, string(b), db.Millis(s.now()), id)
	return err
}
