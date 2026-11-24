package subtitles

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/translate"
)

// Translation settings.
var (
	// TranslationDefault is the translation service used when neither the
	// profile language nor TranslationLanguages names one ("" = the first enabled).
	TranslationDefault = settings.Key[string]{Name: "subtitles.translation.defaultTranslator", Default: ""}
	// TranslationLanguages maps a target language to its service.
	TranslationLanguages = settings.Key[map[string]string]{Name: "subtitles.translation.languageTranslators", Default: map[string]string{}}
	// TranslationSuffix marks translated files: "Movie.hr.translated.srt" ("" = "Movie.hr.srt").
	TranslationSuffix = settings.Key[string]{Name: "subtitles.translation.suffix", Default: TranslatedMarker}
	// TranslationSources are the source languages preferred, in order (then any).
	TranslationSources = settings.Key[[]string]{Name: "subtitles.translation.sourceLanguages", Default: []string{"en"}}
	// TranslationAutoSync re-times a translation made from a sidecar against the audio.
	TranslationAutoSync = settings.Key[bool]{Name: "subtitles.translation.autoSync", Default: false}
	// TranslationsPerHour caps the automatic translations of one hourly run.
	TranslationsPerHour = settings.Key[int]{Name: "subtitles.translation.perHour", Default: 10}
)

// TranslatedMarker is the default name part of machine-translated files.
const TranslatedMarker = "translated"

// TranslatorNone in a profile language means "never translate it".
const TranslatorNone = "none"

var reSuffix = regexp.MustCompile(`^[a-z0-9-]{0,20}$`)

// ValidSuffix reports whether a translated-file marker is usable.
func ValidSuffix(s string) bool { return reSuffix.MatchString(s) }

// TranslatorInfo describes a translation service for the settings page.
type TranslatorInfo struct {
	Name string `json:"name"`
	// Fields: baseUrl, apiKey, model, formality, glossaryId.
	Fields []string `json:"fields"`
	// NeedsKey / NeedsURL: required before it can be enabled.
	NeedsKey bool `json:"needsKey"`
	NeedsURL bool `json:"needsUrl"`
	// Local: runs on the user's own server (text stays at home).
	Local        bool     `json:"local"`
	Unit         string   `json:"unit"`
	DefaultModel string   `json:"defaultModel"`
	Models       []string `json:"models"`
	Website      string   `json:"website"`
}

// Translators are the supported services.
var Translators = []TranslatorInfo{
	{Name: translate.LibreTranslate, Fields: []string{"baseUrl", "apiKey"}, NeedsURL: true, Local: true, Unit: translate.UnitCharacters,
		Models: []string{}, Website: "https://libretranslate.com"},
	{Name: translate.DeepL, Fields: []string{"apiKey", "formality", "glossaryId"}, NeedsKey: true, Unit: translate.UnitCharacters,
		Models: []string{}, Website: "https://www.deepl.com/your-account/keys"},
	{Name: translate.OpenAI, Fields: []string{"apiKey", "model"}, NeedsKey: true, Unit: translate.UnitTokens,
		DefaultModel: translate.OpenAIDefaultModel, Models: translate.OpenAIModels, Website: "https://platform.openai.com/api-keys"},
	{Name: translate.Anthropic, Fields: []string{"apiKey", "model"}, NeedsKey: true, Unit: translate.UnitTokens,
		DefaultModel: translate.AnthropicDefaultModel, Models: translate.AnthropicModels, Website: "https://console.anthropic.com/settings/keys"},
	{Name: translate.Gemini, Fields: []string{"apiKey", "model"}, NeedsKey: true, Unit: translate.UnitTokens,
		DefaultModel: translate.GeminiDefaultModel, Models: translate.GeminiModels, Website: "https://aistudio.google.com/apikey"},
	{Name: translate.OpenAICompatible, Fields: []string{"baseUrl", "apiKey", "model"}, NeedsURL: true, Local: true, Unit: translate.UnitTokens,
		Models: []string{}, Website: "https://ollama.com"},
}

// TranslatorInfoOf returns a service by name.
func TranslatorInfoOf(name string) (TranslatorInfo, bool) {
	for _, t := range Translators {
		if t.Name == name {
			return t, true
		}
	}
	return TranslatorInfo{}, false
}

func knownTranslator(name string) bool {
	_, ok := TranslatorInfoOf(name)
	return ok
}

// TranslatorState is a service's settings, health and usage.
type TranslatorState struct {
	Name           string     `json:"name"`
	Enabled        bool       `json:"enabled"`
	BaseURL        string     `json:"baseUrl"`
	APIKeySet      bool       `json:"apiKeySet"`
	Model          string     `json:"model"`
	Formality      string     `json:"formality"`
	GlossaryID     string     `json:"glossaryId"`
	MonthlyCap     int64      `json:"monthlyCap"`
	Failures       int        `json:"failures"`
	RetryAt        *time.Time `json:"retryAt,omitempty"`
	LastError      string     `json:"lastError"`
	LastErrorCode  string     `json:"lastErrorCode"`
	LastErrorAt    *time.Time `json:"lastErrorAt,omitempty"`
	LastSuccessAt  *time.Time `json:"lastSuccessAt,omitempty"`
	QuotaUsed      *int64     `json:"quotaUsed,omitempty"`
	QuotaLimit     *int64     `json:"quotaLimit,omitempty"`
	QuotaCheckedAt *time.Time `json:"quotaCheckedAt,omitempty"`
	Month          MonthUsage `json:"month"`

	apiKey []byte
}

// MonthUsage is a service's usage this calendar month (UTC).
type MonthUsage struct {
	Month        string `json:"month"`
	Characters   int64  `json:"characters"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	Requests     int64  `json:"requests"`
	Lines        int64  `json:"lines"`
	Subtitles    int64  `json:"subtitles"`
}

// Used is the usage in the service's unit.
func (t TranslatorState) Used() int64 {
	info, _ := TranslatorInfoOf(t.Name)
	if info.Unit == translate.UnitTokens {
		return t.Month.InputTokens + t.Month.OutputTokens
	}
	return t.Month.Characters
}

// CapReached reports a used-up monthly cap.
func (t TranslatorState) CapReached() bool { return t.MonthlyCap > 0 && t.Used() >= t.MonthlyCap }

// Usable reports whether automatic translations may use the service now.
func (t TranslatorState) Usable(now time.Time) bool {
	return t.Enabled && !t.CapReached() && (t.RetryAt == nil || !now.Before(*t.RetryAt))
}

const translatorCols = `name, enabled, base_url, api_key, model, formality, glossary_id, monthly_cap, failures, retry_at, last_error, last_error_code,
	last_error_at, last_success_at, quota_used, quota_limit, quota_checked_at`

func monthOf(t time.Time) string { return t.UTC().Format("2006-01") }

func (s *Service) scanTranslator(ctx context.Context, row interface{ Scan(...any) error }) (TranslatorState, error) {
	var (
		t                                 TranslatorState
		retry, errAt, okAt, used, lim, ck sql.NullInt64
	)
	if err := row.Scan(&t.Name, &t.Enabled, &t.BaseURL, &t.apiKey, &t.Model, &t.Formality, &t.GlossaryID, &t.MonthlyCap, &t.Failures, &retry,
		&t.LastError, &t.LastErrorCode, &errAt, &okAt, &used, &lim, &ck); err != nil {
		return t, err
	}
	t.RetryAt, t.LastErrorAt, t.LastSuccessAt, t.QuotaCheckedAt = db.NullTime(retry), db.NullTime(errAt), db.NullTime(okAt), db.NullTime(ck)
	if used.Valid {
		t.QuotaUsed = &used.Int64
	}
	if lim.Valid {
		t.QuotaLimit = &lim.Int64
	}
	t.APIKeySet = len(t.apiKey) > 0
	t.Month.Month = monthOf(s.now())
	err := s.DB.R.QueryRowContext(ctx, `SELECT characters, input_tokens, output_tokens, requests, lines, subtitles FROM subtitle_translation_usage
		WHERE translator = ? AND month = ?`, t.Name, t.Month.Month).Scan(&t.Month.Characters, &t.Month.InputTokens, &t.Month.OutputTokens,
		&t.Month.Requests, &t.Month.Lines, &t.Month.Subtitles)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return t, err
	}
	return t, nil
}

// TranslatorStates lists the services in display order.
func (s *Service) TranslatorStates(ctx context.Context) ([]TranslatorState, error) {
	out := make([]TranslatorState, 0, len(Translators))
	for _, info := range Translators {
		t, err := s.Translator(ctx, info.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// Translator returns one service.
func (s *Service) Translator(ctx context.Context, name string) (TranslatorState, error) {
	if !knownTranslator(name) {
		return TranslatorState{}, ErrNotFound
	}
	t, err := s.scanTranslator(ctx, s.DB.R.QueryRowContext(ctx, `SELECT `+translatorCols+` FROM subtitle_translators WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// TranslatorInput changes a service. Nil fields stay; an empty key removes it.
type TranslatorInput struct {
	Enabled    *bool
	BaseURL    *string
	APIKey     *string
	Model      *string
	Formality  *string
	GlossaryID *string
	MonthlyCap *int64
}

func translatorAAD(name string) string { return "subtitle_translators:" + name + ":apiKey" }

func checkBaseURL(v string) error {
	if v == "" {
		return nil
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return invalid("the address must be an http:// or https:// URL without credentials or query, e.g. http://192.168.1.10:5000")
	}
	return nil
}

// UpdateTranslator saves a service's settings.
func (s *Service) UpdateTranslator(ctx context.Context, name string, in TranslatorInput) (TranslatorState, error) {
	info, ok := TranslatorInfoOf(name)
	if !ok {
		return TranslatorState{}, ErrNotFound
	}
	cur, err := s.Translator(ctx, name)
	if err != nil {
		return cur, err
	}
	enabled, base, key, model, formality, glossary, cap := cur.Enabled, cur.BaseURL, cur.apiKey, cur.Model, cur.Formality, cur.GlossaryID, cur.MonthlyCap
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if in.BaseURL != nil {
		base = strings.TrimRight(strings.TrimSpace(*in.BaseURL), "/")
		if err := checkBaseURL(base); err != nil {
			return cur, err
		}
	}
	if in.APIKey != nil {
		switch v := strings.TrimSpace(*in.APIKey); {
		case v == "":
			key = nil
		case len(v) > 512:
			return cur, invalid("the API key is too long")
		default:
			if key, err = s.Box.Seal([]byte(v), translatorAAD(name)); err != nil {
				return cur, err
			}
		}
	}
	if in.Model != nil {
		model = strings.TrimSpace(*in.Model)
		if len(model) > 100 {
			return cur, invalid("the model name is too long")
		}
	}
	if in.Formality != nil {
		switch *in.Formality {
		case "", "default", "prefer_more", "prefer_less":
			formality = *in.Formality
		default:
			return cur, invalid("formality must be default, prefer_more or prefer_less")
		}
	}
	if in.GlossaryID != nil {
		glossary = strings.TrimSpace(*in.GlossaryID)
		if len(glossary) > 100 {
			return cur, invalid("the glossary id is too long")
		}
	}
	if in.MonthlyCap != nil {
		if *in.MonthlyCap < 0 {
			return cur, invalid("the monthly cap cannot be negative")
		}
		cap = *in.MonthlyCap
	}
	if enabled && info.NeedsKey && len(key) == 0 {
		return cur, invalid("%s needs an API key before it can be enabled", name)
	}
	if enabled && info.NeedsURL && base == "" {
		return cur, invalid("%s needs the server address before it can be enabled", name)
	}
	if enabled && name == translate.OpenAICompatible && model == "" {
		return cur, invalid("choose the model the server should use")
	}
	_, err = s.DB.W.ExecContext(ctx, `UPDATE subtitle_translators SET enabled = ?, base_url = ?, api_key = ?, model = ?, formality = ?, glossary_id = ?,
		monthly_cap = ?, failures = 0, retry_at = NULL, updated_at = ? WHERE name = ?`, enabled, base, key, model, formality, glossary, cap,
		db.Millis(s.now()), name)
	if err != nil {
		return cur, err
	}
	return s.Translator(ctx, name)
}

// translatorClient builds the client of a service.
func (s *Service) translatorClient(t TranslatorState) (translate.Translator, error) {
	key := ""
	if len(t.apiKey) > 0 {
		b, err := s.Box.Open(t.apiKey, translatorAAD(t.Name))
		if err != nil {
			return nil, err
		}
		key = string(b)
	}
	base := t.BaseURL
	if v := s.URLs["translate:"+t.Name]; v != "" {
		base = v // tests and the fakes
	}
	pol := httpx.Policy{}
	if info, _ := TranslatorInfoOf(t.Name); !info.Local && s.URLs["translate:"+t.Name] == "" {
		pol.BlockPrivate = true // cloud services are never on the local network
	}
	if u, err := url.Parse(base); err == nil && u.Hostname() != "" {
		// A server the administrator configured on this machine (Ollama on
		// the host) is allowed even on loopback.
		h := u.Hostname()
		if a, err := netip.ParseAddr(h); err == nil && a.IsLoopback() {
			pol.Allowed = append(pol.Allowed, netip.PrefixFrom(a, a.BitLen()))
		} else if strings.EqualFold(h, "localhost") || s.URLs["translate:"+t.Name] != "" {
			if a, err := netip.ParseAddr(h); err == nil {
				pol.Allowed = append(pol.Allowed, netip.PrefixFrom(a, a.BitLen()))
			} else {
				pol.AllowedHosts = append(pol.AllowedHosts, strings.ToLower(h))
			}
		}
	}
	hc, err := httpx.New(httpx.Options{Policy: pol, Timeout: 3 * time.Minute, MaxBytes: 8 << 20, Secrets: []string{key}, UserAgent: s.UserAgent})
	if err != nil {
		return nil, err
	}
	cfg := translate.Config{BaseURL: base, APIKey: key, Model: t.Model, Formality: t.Formality, GlossaryID: t.GlossaryID, HTTP: hc, Sleep: s.TranslateSleep}
	switch t.Name {
	case translate.LibreTranslate:
		return translate.NewLibreTranslate(cfg), nil
	case translate.DeepL:
		return translate.NewDeepL(cfg), nil
	case translate.OpenAI:
		return translate.NewOpenAI(cfg), nil
	case translate.Anthropic:
		return translate.NewAnthropic(cfg), nil
	case translate.Gemini:
		return translate.NewGemini(cfg), nil
	case translate.OpenAICompatible:
		return translate.NewOpenAICompatible(cfg), nil
	}
	return nil, ErrNotFound
}

// addUsage counts a translation in this month's usage.
func (s *Service) addUsage(ctx context.Context, name string, u translate.Usage, lines, subs int) {
	_, err := s.DB.W.ExecContext(context.WithoutCancel(ctx), `INSERT INTO subtitle_translation_usage (translator, month, characters, input_tokens,
		output_tokens, requests, lines, subtitles) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (translator, month) DO UPDATE SET
		characters = characters + excluded.characters, input_tokens = input_tokens + excluded.input_tokens,
		output_tokens = output_tokens + excluded.output_tokens, requests = requests + excluded.requests, lines = lines + excluded.lines,
		subtitles = subtitles + excluded.subtitles`, name, monthOf(s.now()), u.Characters, u.InputTokens, u.OutputTokens, u.Requests, lines, subs)
	if err != nil {
		s.log().Warn("record translation usage", "translator", name, "err", err)
	}
}

// translatorFailed records an error; rate limits wait as asked, a refused key 12 h.
func (s *Service) translatorFailed(ctx context.Context, t TranslatorState, err error) {
	now := s.now()
	n := t.Failures + 1
	wait := backoff(n)
	var (
		q  *providers.QuotaError
		rl *providers.RateLimitError
		ce *CapError
	)
	switch {
	case errors.As(err, &ce):
		wait = time.Until(nextMonth(now))
		n = t.Failures
	case errors.As(err, &q):
		wait = 24 * time.Hour
	case errors.As(err, &rl):
		wait, n = max(rl.RetryAfter, time.Second), t.Failures
	case errors.Is(err, providers.ErrAuth):
		wait = 12 * time.Hour
	}
	if _, dberr := s.DB.W.ExecContext(context.WithoutCancel(ctx), `UPDATE subtitle_translators SET failures = ?, retry_at = ?, last_error = ?,
		last_error_code = ?, last_error_at = ? WHERE name = ?`, n, db.Millis(now.Add(wait)), err.Error(), TranslationErrorCode(err), db.Millis(now), t.Name); dberr != nil {
		s.log().Warn("record translator failure", "translator", t.Name, "err", dberr)
	}
}

func (s *Service) translatorOK(ctx context.Context, name string) {
	_, _ = s.DB.W.ExecContext(context.WithoutCancel(ctx), `UPDATE subtitle_translators SET failures = 0, retry_at = NULL, last_success_at = ?
		WHERE name = ?`, db.Millis(s.now()), name)
}

func nextMonth(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC)
}

// CapError means the user's monthly cap of a service is reached.
type CapError struct {
	Translator string
	Cap, Used  int64
	Unit       string
}

func (e *CapError) Error() string {
	return fmt.Sprintf("the monthly cap of %s is reached (%d of %d %s); translations wait until next month or a higher cap", e.Translator, e.Used, e.Cap, e.Unit)
}

// TranslationErrorCode classifies a translation error for the UI.
func TranslationErrorCode(err error) string {
	var ce *CapError
	switch {
	case errors.As(err, &ce):
		return "cap"
	case errors.Is(err, translate.ErrUnsupported):
		return "unsupported_language"
	case errors.Is(err, translate.ErrCountMismatch):
		return "line_mismatch"
	case errors.Is(err, ErrNoSource):
		return "no_source"
	}
	return ErrorCode(err)
}

// ResetTranslatorHealth clears a service's backoff.
func (s *Service) ResetTranslatorHealth(ctx context.Context, name string) (TranslatorState, error) {
	if !knownTranslator(name) {
		return TranslatorState{}, ErrNotFound
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE subtitle_translators SET failures = 0, retry_at = NULL WHERE name = ?`, name); err != nil {
		return TranslatorState{}, err
	}
	return s.Translator(ctx, name)
}

// TranslatorTest is the result of a test translation.
type TranslatorTest struct {
	Input      string          `json:"input"`
	Output     string          `json:"output"`
	From       string          `json:"from"`
	To         string          `json:"to"`
	Usage      translate.Usage `json:"usage"`
	DurationMs int64           `json:"durationMs"`
}

// TestSample is the line a Test translates by default.
const TestSample = "Where are you going? <i>Wait for me!</i>"

// TestTranslator translates a sample line (and reads the account usage
// where the service reports it).
func (s *Service) TestTranslator(ctx context.Context, name, text, from, to string) (TranslatorTest, TranslatorState, error) {
	t, err := s.Translator(ctx, name)
	if err != nil {
		return TranslatorTest{}, t, err
	}
	if text = strings.TrimSpace(text); text == "" {
		text = TestSample
	}
	if from == "" {
		from = "en"
	}
	if to == "" {
		to = "hr"
	}
	for _, c := range []string{from, to} {
		if !knownLanguage(c) {
			return TranslatorTest{}, t, ErrBadLanguage
		}
	}
	c, err := s.translatorClient(t)
	if err != nil {
		return TranslatorTest{}, t, err
	}
	start := time.Now()
	out, u, err := c.Translate(ctx, translate.Request{Texts: []string{text}, From: from, To: to})
	res := TranslatorTest{Input: text, From: from, To: to, Usage: u, DurationMs: time.Since(start).Milliseconds()}
	s.addUsage(ctx, name, u, 0, 0)
	if err != nil {
		s.translatorFailed(ctx, t, err)
		t2, _ := s.Translator(ctx, name)
		return res, t2, err
	}
	res.Output = out[0]
	s.translatorOK(ctx, name)
	s.refreshQuota(ctx, name, c)
	t, err = s.Translator(ctx, name)
	return res, t, err
}

func (s *Service) refreshQuota(ctx context.Context, name string, c translate.Translator) {
	q, ok := c.(translate.Quota)
	if !ok {
		return
	}
	used, limit, err := q.Quota(ctx)
	if err != nil {
		return
	}
	_, _ = s.DB.W.ExecContext(context.WithoutCancel(ctx), `UPDATE subtitle_translators SET quota_used = ?, quota_limit = ?, quota_checked_at = ?
		WHERE name = ?`, used, limit, db.Millis(s.now()), name)
}

// TranslationSettings are the behaviour options.
type TranslationSettings struct {
	DefaultTranslator   string            `json:"defaultTranslator"`
	LanguageTranslators map[string]string `json:"languageTranslators"`
	Suffix              string            `json:"suffix"`
	SourceLanguages     []string          `json:"sourceLanguages"`
	AutoSync            bool              `json:"autoSync"`
	PerHour             int               `json:"perHour"`
}

// GetTranslationSettings reads the options.
func (s *Service) GetTranslationSettings(ctx context.Context) (TranslationSettings, error) {
	var (
		out TranslationSettings
		err error
	)
	if out.DefaultTranslator, err = settings.Get(ctx, s.Settings, TranslationDefault); err != nil {
		return out, err
	}
	if out.LanguageTranslators, err = settings.Get(ctx, s.Settings, TranslationLanguages); err != nil {
		return out, err
	}
	if out.LanguageTranslators == nil {
		out.LanguageTranslators = map[string]string{}
	}
	if out.Suffix, err = settings.Get(ctx, s.Settings, TranslationSuffix); err != nil {
		return out, err
	}
	if out.SourceLanguages, err = settings.Get(ctx, s.Settings, TranslationSources); err != nil {
		return out, err
	}
	if out.SourceLanguages == nil {
		out.SourceLanguages = []string{}
	}
	if out.AutoSync, err = settings.Get(ctx, s.Settings, TranslationAutoSync); err != nil {
		return out, err
	}
	out.PerHour, err = settings.Get(ctx, s.Settings, TranslationsPerHour)
	return out, err
}

// TranslationSettingsInput changes the options (nil = unchanged).
type TranslationSettingsInput struct {
	DefaultTranslator   *string
	LanguageTranslators map[string]string
	Suffix              *string
	SourceLanguages     []string
	AutoSync            *bool
	PerHour             *int
}

// UpdateTranslationSettings checks and saves the options.
func (s *Service) UpdateTranslationSettings(ctx context.Context, in TranslationSettingsInput) (TranslationSettings, error) {
	if in.DefaultTranslator != nil && *in.DefaultTranslator != "" && !knownTranslator(*in.DefaultTranslator) {
		return TranslationSettings{}, invalid("unknown translation service %q", *in.DefaultTranslator)
	}
	for lang, name := range in.LanguageTranslators {
		if !knownLanguage(lang) {
			return TranslationSettings{}, invalid("unknown language %q", lang)
		}
		if name != "" && name != TranslatorNone && !knownTranslator(name) {
			return TranslationSettings{}, invalid("unknown translation service %q", name)
		}
	}
	if in.Suffix != nil && !ValidSuffix(*in.Suffix) {
		return TranslationSettings{}, invalid("the file marker may only have lower-case letters, digits and dashes (at most 20)")
	}
	for _, l := range in.SourceLanguages {
		if !knownLanguage(l) {
			return TranslationSettings{}, invalid("unknown language %q", l)
		}
	}
	if in.PerHour != nil && (*in.PerHour < 0 || *in.PerHour > 500) {
		return TranslationSettings{}, invalid("translations per hour must be between 0 and 500")
	}
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if in.DefaultTranslator != nil {
			if err := settings.Set(ctx, s.Settings, tx, TranslationDefault, *in.DefaultTranslator); err != nil {
				return err
			}
		}
		if in.LanguageTranslators != nil {
			m := map[string]string{}
			for k, v := range in.LanguageTranslators {
				if v != "" {
					m[k] = v
				}
			}
			if err := settings.Set(ctx, s.Settings, tx, TranslationLanguages, m); err != nil {
				return err
			}
		}
		if in.Suffix != nil {
			if err := settings.Set(ctx, s.Settings, tx, TranslationSuffix, *in.Suffix); err != nil {
				return err
			}
		}
		if in.SourceLanguages != nil {
			if err := settings.Set(ctx, s.Settings, tx, TranslationSources, in.SourceLanguages); err != nil {
				return err
			}
		}
		if in.AutoSync != nil {
			if err := settings.Set(ctx, s.Settings, tx, TranslationAutoSync, *in.AutoSync); err != nil {
				return err
			}
		}
		if in.PerHour != nil {
			return settings.Set(ctx, s.Settings, tx, TranslationsPerHour, *in.PerHour)
		}
		return nil
	})
	if err != nil {
		return TranslationSettings{}, err
	}
	return s.GetTranslationSettings(ctx)
}
