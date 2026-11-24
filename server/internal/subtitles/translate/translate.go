// Package translate turns subtitle lines into another language with a
// translation service: LibreTranslate (self-hosted), DeepL, OpenAI,
// Anthropic, Google Gemini or any OpenAI-compatible server (Ollama, LM
// Studio, vLLM). Every client is written from its service's public API
// documentation only. A Translator receives a batch of subtitle texts (one
// per unit, no line breaks) and returns exactly as many translations; the
// pipeline in package subtitles splits cues into units, batches them within
// each service's limits and re-wraps the result.
package translate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/languages"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
)

// Translator names.
const (
	LibreTranslate   = "libretranslate"
	DeepL            = "deepl"
	OpenAI           = "openai"
	Anthropic        = "anthropic"
	Gemini           = "gemini"
	OpenAICompatible = "openai-compatible"
)

// Names lists every translator in the order the settings page shows them.
var Names = []string{LibreTranslate, DeepL, OpenAI, Anthropic, Gemini, OpenAICompatible}

// Units a translator's usage is counted in.
const (
	UnitCharacters = "characters"
	UnitTokens     = "tokens"
)

// Request is one batch to translate.
type Request struct {
	// Texts are the units, each a single line (markup kept).
	Texts []string
	// From and To are catalogue language codes (internal/languages).
	From, To string
	// Title is the movie or series the subtitle belongs to (context only).
	Title string
	// Previous are the units right before this batch (context only).
	Previous []string
}

// Usage is what a call cost.
type Usage struct {
	Characters   int64 `json:"characters"`
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
	Requests     int64 `json:"requests"`
}

// Add sums usages.
func (u *Usage) Add(o Usage) {
	u.Characters += o.Characters
	u.InputTokens += o.InputTokens
	u.OutputTokens += o.OutputTokens
	u.Requests += o.Requests
}

// Amount is the usage in unit.
func (u Usage) Amount(unit string) int64 {
	if unit == UnitTokens {
		return u.InputTokens + u.OutputTokens
	}
	return u.Characters
}

// Limits bound one batch.
type Limits struct {
	MaxTexts int // units per request
	MaxChars int // characters per request
}

// Translator is one translation service.
type Translator interface {
	Name() string
	Limits() Limits
	// Translate returns exactly len(r.Texts) translations in order.
	Translate(ctx context.Context, r Request) ([]string, Usage, error)
}

// Quota is a translator that can report the account's usage (DeepL).
type Quota interface {
	Quota(ctx context.Context) (used, limit int64, err error)
}

// Config is what a client needs.
type Config struct {
	BaseURL string // "" = the service's public address
	APIKey  string
	Model   string
	// Formality is DeepL's formality option ("", default, prefer_more, prefer_less).
	Formality string
	// GlossaryID is a DeepL glossary.
	GlossaryID string
	HTTP       *httpx.Client
	// Sleep waits between retries (tests make it instant); nil = real time.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Errors.
var (
	ErrCountMismatch = errors.New("the translation service returned a different number of lines")
	ErrUnsupported   = errors.New("the translation service does not offer this language")
)

func (c Config) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// maxWait is the longest a client waits for a rate limit itself; longer
// waits are returned as a RateLimitError for the caller to schedule.
const maxWait = 30 * time.Second

// post sends a JSON body and retries 429 and 5xx answers with backoff (1 s,
// 2 s, 4 s, or what Retry-After asks for). The final answer is returned
// as it is; classify turns statuses into errors.
func (c Config) post(ctx context.Context, url string, hdr http.Header, body any) (*httpx.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	h := hdr.Clone()
	if h == nil {
		h = http.Header{}
	}
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json")
	return c.send(ctx, http.MethodPost, url, h, b)
}

func (c Config) send(ctx context.Context, method, url string, h http.Header, b []byte) (*httpx.Response, error) {
	if c.HTTP == nil {
		return nil, errors.New("no HTTP client")
	}
	wait := time.Second
	for attempt := 0; ; attempt++ {
		res, err := c.HTTP.Do(ctx, method, url, h, b)
		if err != nil {
			if ctx.Err() != nil || attempt >= 2 {
				return nil, err
			}
		} else if !retryable(res.Status) || attempt >= 3 {
			return res, nil
		}
		d := wait
		if res != nil {
			d = providers.RetryAfter(res.Header, wait, time.Now())
		}
		if d > maxWait {
			return res, nil
		}
		if err := c.sleep(ctx, d); err != nil {
			return nil, err
		}
		wait *= 2
	}
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusInternalServerError || status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout || status == 529
}

// classify turns an unsuccessful answer into a typed error.
func classify(res *httpx.Response) error {
	switch {
	case res.Status >= 200 && res.Status < 300:
		return nil
	case res.Status == http.StatusUnauthorized || res.Status == http.StatusForbidden:
		return fmt.Errorf("%w (HTTP %d)", providers.ErrAuth, res.Status)
	case res.Status == http.StatusTooManyRequests || res.Status == 529 || res.Status == http.StatusServiceUnavailable:
		return &providers.RateLimitError{RetryAfter: providers.RetryAfter(res.Header, time.Minute, time.Now())}
	}
	return &providers.HTTPError{Status: res.Status, Body: string(res.Body)}
}

// Chars counts characters the way the services bill them (runes).
func Chars(texts ...string) int64 {
	n := 0
	for _, t := range texts {
		n += utf8.RuneCountInString(t)
	}
	return int64(n)
}

// LanguageName is the English name of a catalogue code (what LLM prompts use).
func LanguageName(code string) string {
	if l, ok := languages.ByCode(code); ok {
		return l.Name
	}
	return code
}

// Batches splits texts into runs that fit l.
func Batches(texts []string, l Limits) [][2]int {
	var out [][2]int
	start, chars := 0, 0
	for i, t := range texts {
		n := utf8.RuneCountInString(t)
		if i > start && ((l.MaxTexts > 0 && i-start >= l.MaxTexts) || (l.MaxChars > 0 && chars+n > l.MaxChars)) {
			out = append(out, [2]int{start, i})
			start, chars = i, 0
		}
		chars += n
	}
	if start < len(texts) {
		out = append(out, [2]int{start, len(texts)})
	}
	return out
}

func trimURL(u, def string) string {
	if strings.TrimSpace(u) == "" {
		u = def
	}
	return strings.TrimRight(strings.TrimSpace(u), "/")
}
