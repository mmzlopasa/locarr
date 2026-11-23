package subtitles

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers/jimaku"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers/opensubtitles"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers/subdl"
)

// ProviderInfo describes an implementation for the settings page.
type ProviderInfo struct {
	Name string `json:"name"`
	// Fields the provider uses: apiKey, username, password.
	Fields    []string `json:"fields"`
	Languages []string `json:"languages,omitempty"` // empty = all
	Website   string   `json:"website"`
	// NeedsKey: the provider does not work without an API key.
	NeedsKey bool `json:"needsKey"`
}

// Implementations are the supported providers (D-063).
var Implementations = []ProviderInfo{
	{Name: opensubtitles.Name, Fields: []string{"apiKey", "username", "password"}, Website: "https://www.opensubtitles.com/consumers", NeedsKey: true},
	{Name: subdl.Name, Fields: []string{"apiKey"}, Website: "https://subdl.com/panel/api", NeedsKey: true},
	{Name: jimaku.Name, Fields: []string{"apiKey"}, Languages: []string{"ja"}, Website: "https://jimaku.cc/account", NeedsKey: true},
}

// ImplementationOf returns a provider implementation by name.
func ImplementationOf(name string) (ProviderInfo, bool) { return implementation(name) }

func implementation(name string) (ProviderInfo, bool) {
	for _, p := range Implementations {
		if p.Name == name {
			return p, true
		}
	}
	return ProviderInfo{}, false
}

// ProviderState is a provider row with its health and quota.
type ProviderState struct {
	Name           string     `json:"name"`
	Enabled        bool       `json:"enabled"`
	Priority       int        `json:"priority"`
	Username       string     `json:"username"`
	APIKeySet      bool       `json:"apiKeySet"`
	PasswordSet    bool       `json:"passwordSet"`
	Failures       int        `json:"failures"`
	RetryAt        *time.Time `json:"retryAt,omitempty"`
	LastError      string     `json:"lastError"`
	LastErrorCode  string     `json:"lastErrorCode"`
	LastErrorAt    *time.Time `json:"lastErrorAt,omitempty"`
	LastSuccessAt  *time.Time `json:"lastSuccessAt,omitempty"`
	QuotaRemaining *int       `json:"quotaRemaining,omitempty"`
	QuotaLimit     *int       `json:"quotaLimit,omitempty"`
	QuotaResetAt   *time.Time `json:"quotaResetAt,omitempty"`
	Account        string     `json:"account"`
	DownloadsToday int        `json:"downloadsToday"`

	apiKey, password []byte
}

// InBackoff reports whether the provider is skipped now.
func (p ProviderState) InBackoff(now time.Time) bool {
	return p.RetryAt != nil && now.Before(*p.RetryAt)
}

// QuotaUsedUp reports a known exhausted download quota.
func (p ProviderState) QuotaUsedUp(now time.Time) bool {
	return p.QuotaRemaining != nil && *p.QuotaRemaining <= 0 && (p.QuotaResetAt == nil || now.Before(*p.QuotaResetAt))
}

const providerCols = `name, enabled, priority, username, api_key, password, failures, retry_at, last_error, last_error_code, last_error_at,
	last_success_at, quota_remaining, quota_limit, quota_reset_at, account`

func scanProvider(row interface{ Scan(...any) error }) (ProviderState, error) {
	var (
		p                                   ProviderState
		retry, errAt, okAt, reset, rem, lim sql.NullInt64
	)
	err := row.Scan(&p.Name, &p.Enabled, &p.Priority, &p.Username, &p.apiKey, &p.password, &p.Failures, &retry, &p.LastError, &p.LastErrorCode,
		&errAt, &okAt, &rem, &lim, &reset, &p.Account)
	if err != nil {
		return p, err
	}
	p.RetryAt, p.LastErrorAt, p.LastSuccessAt, p.QuotaResetAt = db.NullTime(retry), db.NullTime(errAt), db.NullTime(okAt), db.NullTime(reset)
	if rem.Valid {
		v := int(rem.Int64)
		p.QuotaRemaining = &v
	}
	if lim.Valid {
		v := int(lim.Int64)
		p.QuotaLimit = &v
	}
	p.APIKeySet, p.PasswordSet = len(p.apiKey) > 0, len(p.password) > 0
	return p, nil
}

// Providers lists the providers by priority.
func (s *Service) Providers(ctx context.Context) ([]ProviderState, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+providerCols+` FROM subtitle_providers ORDER BY priority, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderState
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		if _, ok := implementation(p.Name); ok {
			out = append(out, p)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	since := db.Millis(s.now().Add(-24 * time.Hour))
	for i := range out {
		_ = s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM subtitle_history WHERE provider = ? AND at >= ?`, out[i].Name, since).Scan(&out[i].DownloadsToday)
	}
	return out, nil
}

// Provider returns one provider.
func (s *Service) Provider(ctx context.Context, name string) (ProviderState, error) {
	p, err := scanProvider(s.DB.R.QueryRowContext(ctx, `SELECT `+providerCols+` FROM subtitle_providers WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// ProviderInput changes a provider. Nil fields stay; an empty secret removes it.
type ProviderInput struct {
	Enabled  *bool
	Priority *int
	Username *string
	APIKey   *string
	Password *string
}

func aad(name, field string) string { return "subtitle_providers:" + name + ":" + field }

// UpdateProvider saves a provider's settings.
func (s *Service) UpdateProvider(ctx context.Context, name string, in ProviderInput) (ProviderState, error) {
	info, ok := implementation(name)
	if !ok {
		return ProviderState{}, ErrNotFound
	}
	cur, err := s.Provider(ctx, name)
	if err != nil {
		return cur, err
	}
	if in.Priority != nil && (*in.Priority < 1 || *in.Priority > 100) {
		return cur, invalid("priority must be between 1 and 100")
	}
	seal := func(v *string, field string, old []byte) ([]byte, error) {
		if v == nil {
			return old, nil
		}
		if *v == "" {
			return nil, nil
		}
		if len(*v) > 512 {
			return nil, invalid("%s is too long", field)
		}
		return s.Box.Seal([]byte(*v), aad(name, field))
	}
	key, err := seal(in.APIKey, "apiKey", cur.apiKey)
	if err != nil {
		return cur, err
	}
	pw, err := seal(in.Password, "password", cur.password)
	if err != nil {
		return cur, err
	}
	enabled, prio, user := cur.Enabled, cur.Priority, cur.Username
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if in.Priority != nil {
		prio = *in.Priority
	}
	if in.Username != nil {
		user = strings.TrimSpace(*in.Username)
	}
	if enabled && info.NeedsKey && len(key) == 0 {
		return cur, invalid("%s needs an API key before it can be enabled", name)
	}
	_, err = s.DB.W.ExecContext(ctx, `UPDATE subtitle_providers SET enabled = ?, priority = ?, username = ?, api_key = ?, password = ?,
		failures = 0, retry_at = NULL, updated_at = ? WHERE name = ?`, enabled, prio, user, key, pw, db.Millis(s.now()), name)
	if err != nil {
		return cur, err
	}
	s.forget(name)
	return s.Provider(ctx, name)
}

// ResetHealth clears a provider's failures and backoff.
func (s *Service) ResetHealth(ctx context.Context, name string) (ProviderState, error) {
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE subtitle_providers SET failures = 0, retry_at = NULL WHERE name = ?`, name); err != nil {
		return ProviderState{}, err
	}
	s.forget(name)
	return s.Provider(ctx, name)
}

type clientEntry struct {
	p   providers.Provider
	key string
}

func (s *Service) forget(name string) {
	s.mu.Lock()
	delete(s.clients, name)
	s.mu.Unlock()
}

func (s *Service) open(p ProviderState, field string, b []byte) (string, error) {
	if len(b) == 0 {
		return "", nil
	}
	v, err := s.Box.Open(b, aad(p.Name, field))
	return string(v), err
}

// client returns the (cached) client of a provider.
func (s *Service) client(p ProviderState) (providers.Provider, error) {
	key, err := s.open(p, "apiKey", p.apiKey)
	if err != nil {
		return nil, err
	}
	pw, err := s.open(p, "password", p.password)
	if err != nil {
		return nil, err
	}
	cacheKey := key + "\x00" + p.Username + "\x00" + pw
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[p.Name]; ok && c.key == cacheKey {
		return c.p, nil
	}
	base := s.URLs[p.Name]
	pol := httpx.Policy{}
	allow := ""
	if base != "" {
		// A test or fake address is allowed even on loopback.
		if u, err := url.Parse(base); err == nil {
			allow = u.Hostname()
			if a, err := netip.ParseAddr(allow); err == nil {
				pol.Allowed = append(pol.Allowed, netip.PrefixFrom(a, a.BitLen()))
			} else {
				pol.AllowedHosts = append(pol.AllowedHosts, strings.ToLower(allow))
			}
		}
	}
	hc, err := httpx.New(httpx.Options{Policy: pol, Timeout: 30 * time.Second, MaxBytes: 16 << 20, Secrets: []string{key, pw}})
	if err != nil {
		return nil, err
	}
	ua := s.UserAgent
	if ua == "" {
		ua = "Locarr v0"
	}
	cfg := providers.Config{BaseURL: base, APIKey: key, Username: p.Username, Password: pw, HTTP: hc, UserAgent: ua, AllowHost: allow}
	var c providers.Provider
	switch p.Name {
	case opensubtitles.Name:
		c = opensubtitles.New(cfg)
	case subdl.Name:
		c = subdl.New(cfg)
	case jimaku.Name:
		c = jimaku.New(cfg)
	default:
		return nil, ErrNotFound
	}
	if s.clients == nil {
		s.clients = map[string]clientEntry{}
	}
	s.clients[p.Name] = clientEntry{p: c, key: cacheKey}
	return c, nil
}

// TestProvider checks a provider's credentials and stores its quota.
func (s *Service) TestProvider(ctx context.Context, name string) (ProviderState, error) {
	p, err := s.Provider(ctx, name)
	if err != nil {
		return p, err
	}
	s.forget(name)
	c, err := s.client(p)
	if err != nil {
		return p, err
	}
	st, err := c.Test(ctx)
	if err != nil {
		s.recordFailure(ctx, p, err)
		p2, _ := s.Provider(ctx, name)
		return p2, err
	}
	s.recordSuccess(ctx, name, &st)
	return s.Provider(ctx, name)
}

// backoff after n consecutive failures: 1 minute, then 5 minutes doubling up to 12 hours.
func backoff(n int) time.Duration {
	if n <= 1 {
		return time.Minute
	}
	return min(5*time.Minute*time.Duration(math.Pow(2, float64(min(n-2, 10)))), 12*time.Hour)
}

// ErrorCode classifies a provider error for the UI.
func ErrorCode(err error) string {
	var (
		q  *providers.QuotaError
		rl *providers.RateLimitError
	)
	switch {
	case errors.As(err, &q):
		return "quota"
	case errors.As(err, &rl):
		return "rate_limited"
	case errors.Is(err, providers.ErrAuth):
		return "auth"
	case errors.Is(err, providers.ErrMissingKey):
		return "missing_key"
	case errors.Is(err, providers.ErrBadResponse):
		return "bad_response"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	var re *httpx.RequestError
	if errors.As(err, &re) {
		if re.Timeout() {
			return "timeout"
		}
		return "connection"
	}
	return "error"
}

func (s *Service) recordFailure(ctx context.Context, p ProviderState, err error) {
	now := s.now()
	n := p.Failures + 1
	wait := backoff(n)
	var (
		q  *providers.QuotaError
		rl *providers.RateLimitError
	)
	switch {
	case errors.As(err, &q):
		// Not a failure of the provider: no more downloads until the reset.
		reset := q.ResetAt
		if reset.IsZero() {
			reset = now.Add(24 * time.Hour)
		}
		_, _ = s.DB.W.ExecContext(context.WithoutCancel(ctx), `UPDATE subtitle_providers SET quota_remaining = 0, quota_reset_at = ?,
			last_error = ?, last_error_code = 'quota', last_error_at = ? WHERE name = ?`, db.Millis(reset), err.Error(), db.Millis(now), p.Name)
		return
	case errors.As(err, &rl):
		wait = max(rl.RetryAfter, time.Second)
		n = p.Failures // rate limits do not count as failures
	case errors.Is(err, providers.ErrAuth):
		wait = 12 * time.Hour
	}
	_, dberr := s.DB.W.ExecContext(context.WithoutCancel(ctx), `UPDATE subtitle_providers SET failures = ?, retry_at = ?, last_error = ?,
		last_error_code = ?, last_error_at = ? WHERE name = ?`, n, db.Millis(now.Add(wait)), err.Error(), ErrorCode(err), db.Millis(now), p.Name)
	if dberr != nil {
		s.log().Warn("record provider failure", "provider", p.Name, "err", dberr)
	}
	s.log().Warn("subtitle provider failed", "provider", p.Name, "code", ErrorCode(err), "retry_in", wait.String(), "err", err)
}

func (s *Service) recordSuccess(ctx context.Context, name string, st *providers.Status) {
	q := `UPDATE subtitle_providers SET failures = 0, retry_at = NULL, last_success_at = ?`
	args := []any{db.Millis(s.now())}
	if st != nil {
		if st.QuotaRemaining != nil {
			q += `, quota_remaining = ?`
			args = append(args, *st.QuotaRemaining)
		}
		if st.QuotaLimit != nil {
			q += `, quota_limit = ?`
			args = append(args, *st.QuotaLimit)
		}
		if st.QuotaResetAt != nil {
			q += `, quota_reset_at = ?`
			args = append(args, db.Millis(*st.QuotaResetAt))
		}
		if st.Account != "" {
			q += `, account = ?`
			args = append(args, st.Account)
		}
	}
	args = append(args, name)
	if _, err := s.DB.W.ExecContext(context.WithoutCancel(ctx), q+` WHERE name = ?`, args...); err != nil {
		s.log().Warn("record provider success", "provider", name, "err", err)
	}
}

// usable returns the enabled providers that are not in backoff, by priority;
// skipped lists the others with the reason.
func (s *Service) usable(ctx context.Context) ([]ProviderState, map[string]string, error) {
	all, err := s.Providers(ctx)
	if err != nil {
		return nil, nil, err
	}
	now := s.now()
	var out []ProviderState
	skipped := map[string]string{}
	for _, p := range all {
		switch {
		case !p.Enabled:
		case p.InBackoff(now):
			skipped[p.Name] = fmt.Sprintf("in backoff until %s after: %s", p.RetryAt.UTC().Format("15:04 UTC"), p.LastError)
		default:
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out, skipped, nil
}
