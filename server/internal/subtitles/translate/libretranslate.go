package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
)

// libre is a LibreTranslate client (libretranslate.com/docs): POST
// {url}/translate with q as an array, source, target, format and the
// optional api_key; GET {url}/languages lists the language pairs.
type libre struct{ cfg Config }

// NewLibreTranslate returns the LibreTranslate translator for a
// (usually self-hosted) server address.
func NewLibreTranslate(cfg Config) Translator {
	cfg.BaseURL = trimURL(cfg.BaseURL, "")
	return &libre{cfg: cfg}
}

func (l *libre) Name() string { return LibreTranslate }

// Limits: modest batches; a server may set its own character limit.
func (l *libre) Limits() Limits { return Limits{MaxTexts: 25, MaxChars: 4000} }

// libreCode maps a catalogue code to LibreTranslate's codes.
func libreCode(code string) string {
	switch code {
	case "pt-br":
		return "pt-BR"
	case "zh":
		return "zh-Hans"
	}
	base, _, _ := strings.Cut(code, "-")
	return base
}

// Translate implements Translator.
func (l *libre) Translate(ctx context.Context, r Request) ([]string, Usage, error) {
	if l.cfg.BaseURL == "" {
		return nil, Usage{}, fmt.Errorf("%w: no LibreTranslate address", providers.ErrMissingKey)
	}
	texts := make([]string, len(r.Texts))
	format := "text"
	for i, t := range r.Texts {
		texts[i] = StripASS(t)
		if len(Tags(texts[i])) > 0 {
			format = "html"
		}
	}
	body := map[string]any{"q": texts, "source": libreCode(r.From), "target": libreCode(r.To), "format": format}
	if l.cfg.APIKey != "" {
		body["api_key"] = l.cfg.APIKey
	}
	res, err := l.cfg.post(ctx, l.cfg.BaseURL+"/translate", nil, body)
	if err != nil {
		return nil, Usage{}, err
	}
	if res.Status == http.StatusBadRequest {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(res.Body, &e)
		if strings.Contains(strings.ToLower(e.Error), "not supported") {
			return nil, Usage{}, fmt.Errorf("%w: %s", ErrUnsupported, e.Error)
		}
	}
	if err := classify(res); err != nil {
		return nil, Usage{}, err
	}
	var out struct {
		TranslatedText json.RawMessage `json:"translatedText"`
	}
	if err := json.Unmarshal(res.Body, &out); err != nil {
		return nil, Usage{}, fmt.Errorf("%w: %v", providers.ErrBadResponse, err)
	}
	var lines []string
	if err := json.Unmarshal(out.TranslatedText, &lines); err != nil {
		var one string
		if json.Unmarshal(out.TranslatedText, &one) != nil || len(texts) != 1 {
			return nil, Usage{}, fmt.Errorf("%w: no translatedText list", providers.ErrBadResponse)
		}
		lines = []string{one}
	}
	if len(lines) != len(texts) {
		return nil, Usage{}, fmt.Errorf("%w: %d for %d", ErrCountMismatch, len(lines), len(texts))
	}
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
		if !SameTags(texts[i], lines[i]) {
			lines[i] = StripTags(lines[i])
		}
	}
	return lines, Usage{Characters: Chars(texts...), Requests: 1}, nil
}

// Languages returns the codes the server translates from (to its targets).
func (l *libre) Languages(ctx context.Context) (map[string][]string, error) {
	if l.cfg.BaseURL == "" {
		return nil, fmt.Errorf("%w: no LibreTranslate address", providers.ErrMissingKey)
	}
	res, err := l.cfg.send(ctx, http.MethodGet, l.cfg.BaseURL+"/languages", http.Header{"Accept": {"application/json"}}, nil)
	if err != nil {
		return nil, err
	}
	if err := classify(res); err != nil {
		return nil, err
	}
	var list []struct {
		Code    string   `json:"code"`
		Targets []string `json:"targets"`
	}
	if err := json.Unmarshal(res.Body, &list); err != nil {
		return nil, fmt.Errorf("%w: %v", providers.ErrBadResponse, err)
	}
	out := map[string][]string{}
	for _, x := range list {
		out[x.Code] = x.Targets
	}
	return out, nil
}

// Supports checks a pair against /languages (true when the list is unknown).
func (l *libre) Supports(ctx context.Context, from, to string) (bool, error) {
	langs, err := l.Languages(ctx)
	if err != nil {
		return false, err
	}
	targets, ok := langs[libreCode(from)]
	if !ok {
		return false, nil
	}
	if len(targets) == 0 {
		_, ok := langs[libreCode(to)]
		return ok, nil
	}
	for _, t := range targets {
		if t == libreCode(to) {
			return true, nil
		}
	}
	return false, nil
}

// Pairs is a translator that can say which language pairs it offers.
type Pairs interface {
	Supports(ctx context.Context, from, to string) (bool, error)
}
