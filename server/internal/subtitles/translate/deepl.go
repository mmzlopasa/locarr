package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
)

// DeepL API addresses: keys of the free plan end in ":fx" and use the free
// endpoint (developers.deepl.com, "Authentication").
const (
	DeepLFreeURL = "https://api-free.deepl.com"
	DeepLProURL  = "https://api.deepl.com"
)

// DeepLURL is the endpoint for a key.
func DeepLURL(key string) string {
	if strings.HasSuffix(strings.TrimSpace(key), ":fx") {
		return DeepLFreeURL
	}
	return DeepLProURL
}

type deepl struct{ cfg Config }

// NewDeepL returns the DeepL translator; with an empty BaseURL the endpoint
// follows the key (free or Pro).
func NewDeepL(cfg Config) Translator {
	cfg.BaseURL = trimURL(cfg.BaseURL, DeepLURL(cfg.APIKey))
	return &deepl{cfg: cfg}
}

func (d *deepl) Name() string { return DeepL }

// Limits: up to 50 texts and 128 KiB per request.
func (d *deepl) Limits() Limits { return Limits{MaxTexts: 50, MaxChars: 30000} }

// deeplSource maps a catalogue code to a DeepL source language.
func deeplSource(code string) string {
	base, _, _ := strings.Cut(code, "-")
	return strings.ToUpper(base)
}

// deeplTarget maps a catalogue code to a DeepL target language (English
// and Portuguese need a variant, Chinese a script).
func deeplTarget(code string) string {
	switch code {
	case "en":
		return "EN-US"
	case "pt":
		return "PT-PT"
	case "pt-br":
		return "PT-BR"
	case "zh":
		return "ZH-HANS"
	}
	return strings.ToUpper(code)
}

func (d *deepl) header() http.Header {
	h := http.Header{}
	h.Set("Authorization", "DeepL-Auth-Key "+d.cfg.APIKey)
	return h
}

// Translate implements Translator: POST /v2/translate with every unit as
// one entry of "text", HTML tag handling for <i>/<b>, the title and the
// previous lines as (unbilled) context, formality and glossary when set.
func (d *deepl) Translate(ctx context.Context, r Request) ([]string, Usage, error) {
	if d.cfg.APIKey == "" {
		return nil, Usage{}, providers.ErrMissingKey
	}
	texts := make([]string, len(r.Texts))
	tags := false
	for i, t := range r.Texts {
		texts[i] = StripASS(t)
		tags = tags || len(Tags(texts[i])) > 0
	}
	body := map[string]any{"text": texts, "source_lang": deeplSource(r.From), "target_lang": deeplTarget(r.To)}
	if tags {
		body["tag_handling"] = "html"
	}
	if ctxText := deeplContext(r); ctxText != "" {
		body["context"] = ctxText
	}
	if f := d.cfg.Formality; f != "" && f != "default" {
		body["formality"] = f
	}
	if d.cfg.GlossaryID != "" {
		body["glossary_id"] = d.cfg.GlossaryID
	}
	res, err := d.cfg.post(ctx, d.cfg.BaseURL+"/v2/translate", d.header(), body)
	if err != nil {
		return nil, Usage{}, err
	}
	if res.Status == 456 {
		return nil, Usage{}, &providers.QuotaError{Message: "the DeepL character quota of this billing period is used up"}
	}
	if res.Status == http.StatusBadRequest && strings.Contains(strings.ToLower(string(res.Body)), "target_lang") {
		return nil, Usage{}, fmt.Errorf("%w: DeepL does not translate into %s", ErrUnsupported, LanguageName(r.To))
	}
	if err := classify(res); err != nil {
		return nil, Usage{}, err
	}
	var out struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if err := json.Unmarshal(res.Body, &out); err != nil {
		return nil, Usage{}, fmt.Errorf("%w: %w", providers.ErrBadResponse, err)
	}
	if len(out.Translations) != len(texts) {
		return nil, Usage{}, fmt.Errorf("%w: %d for %d", ErrCountMismatch, len(out.Translations), len(texts))
	}
	lines := make([]string, len(texts))
	for i, t := range out.Translations {
		v := strings.TrimSpace(t.Text)
		if !SameTags(texts[i], v) {
			v = StripTags(v)
		}
		lines[i] = v
	}
	return lines, Usage{Characters: Chars(texts...), Requests: 1}, nil
}

func deeplContext(r Request) string {
	var b strings.Builder
	if r.Title != "" {
		b.WriteString("Subtitles of " + r.Title + ".")
	}
	for _, p := range r.Previous {
		b.WriteString(" " + StripTags(p))
	}
	return strings.TrimSpace(b.String())
}

// Quota implements Quota: GET /v2/usage.
func (d *deepl) Quota(ctx context.Context) (int64, int64, error) {
	if d.cfg.APIKey == "" {
		return 0, 0, providers.ErrMissingKey
	}
	res, err := d.cfg.send(ctx, http.MethodGet, d.cfg.BaseURL+"/v2/usage", d.header(), nil)
	if err != nil {
		return 0, 0, err
	}
	if err := classify(res); err != nil {
		return 0, 0, err
	}
	var u struct {
		CharacterCount int64 `json:"character_count"`
		CharacterLimit int64 `json:"character_limit"`
	}
	if err := json.Unmarshal(res.Body, &u); err != nil {
		return 0, 0, fmt.Errorf("%w: %w", providers.ErrBadResponse, err)
	}
	return u.CharacterCount, u.CharacterLimit, nil
}
