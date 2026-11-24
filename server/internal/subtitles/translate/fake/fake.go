// Package fake serves fake translation services for tests and the
// development fakes, written from each service's public API description:
//
//	/libretranslate  LibreTranslate: POST /translate (q array), GET /languages
//	/deepl           DeepL: POST /v2/translate, GET /v2/usage (key FreeKey or ProKey)
//	/openai          OpenAI: POST /v1/chat/completions (Bearer OpenAIKey)
//	/compatible      an OpenAI-compatible server (no key): POST /v1/chat/completions
//	/anthropic       Anthropic: POST /v1/messages (x-api-key AnthropicKey, anthropic-version)
//	/gemini          Gemini: POST /v1beta/models/{model}:generateContent (x-goog-api-key GeminiKey)
//
// The "translation" is deterministic: "[hr] " + the text (markup kept), so
// tests can check what came back where.
package fake

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

// Keys the fake accepts.
const (
	DeepLFreeKey = "fake-deepl-key:fx"
	DeepLProKey  = "fake-deepl-pro-key"
	OpenAIKey    = "fake-openai-key"
	AnthropicKey = "fake-anthropic-key"
	GeminiKey    = "fake-gemini-key"
	LibreKey     = "" // the fake LibreTranslate takes no key unless Server.LibreKey is set
)

// Request is one request the fake answered.
type Request struct {
	Service string
	Path    string
	Header  http.Header
	Body    map[string]any
}

// Server is the fake. Its fields inject faults; all are safe to change between requests.
type Server struct {
	mu sync.Mutex
	// RateLimit answers this many requests with 429 (Retry-After: 0) first.
	RateLimit int
	// DropLine makes the language models return one line too few this many
	// times (only for batches of more than one line).
	DropLine int
	// LibreKey, when set, is required by LibreTranslate.
	LibreKey string
	// DeepLLimit is the character limit of the fake DeepL account (0 = 500000).
	DeepLLimit int64
	// LibreLanguages limits the LibreTranslate targets (nil = every language).
	LibreLanguages []string

	deeplUsed int64
	requests  []Request
}

// New returns a fake.
func New() *Server { return &Server{} }

// Requests returns what was received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// Count is the number of requests to a service.
func (s *Server) Count(service string) int {
	n := 0
	for _, r := range s.Requests() {
		if r.Service == service {
			n++
		}
	}
	return n
}

// Reset forgets the requests and the DeepL usage.
func (s *Server) Reset() {
	s.mu.Lock()
	s.requests, s.deeplUsed = nil, 0
	s.mu.Unlock()
}

// Translate is the fake translation of one text.
func Translate(text, to string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	return "[" + to + "] " + text
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) take(n *int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if *n > 0 {
		*n--
		return true
	}
	return false
}

var reModel = regexp.MustCompile(`^/gemini/v1beta/models/([^/:]+):generateContent$`)

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	service, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	body := map[string]any{}
	if r.Method == http.MethodPost {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err := json.Unmarshal(b, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
	}
	s.mu.Lock()
	s.requests = append(s.requests, Request{Service: service, Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
	s.mu.Unlock()
	if s.take(&s.RateLimit) {
		w.Header().Set("Retry-After", "0")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "slow down"})
		return
	}
	switch {
	case r.URL.Path == "/libretranslate/translate" && r.Method == http.MethodPost:
		s.libre(w, body)
	case r.URL.Path == "/libretranslate/languages":
		s.libreLanguages(w)
	case strings.HasPrefix(r.URL.Path, "/deepl/v2/translate") && r.Method == http.MethodPost:
		s.deepl(w, r, body)
	case r.URL.Path == "/deepl/v2/usage":
		if !deeplAuth(r) {
			writeJSON(w, http.StatusForbidden, map[string]string{"message": "Wrong endpoint or key"})
			return
		}
		s.mu.Lock()
		used := s.deeplUsed
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]int64{"character_count": used, "character_limit": s.deeplLimit()})
	case (r.URL.Path == "/openai/v1/chat/completions" || r.URL.Path == "/compatible/v1/chat/completions") && r.Method == http.MethodPost:
		if service == "openai" && r.Header.Get("Authorization") != "Bearer "+OpenAIKey {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"message": "Incorrect API key provided", "type": "invalid_request_error"}})
			return
		}
		s.chat(w, body)
	case r.URL.Path == "/anthropic/v1/messages" && r.Method == http.MethodPost:
		s.anthropic(w, r, body)
	case reModel.MatchString(r.URL.Path) && r.Method == http.MethodPost:
		s.gemini(w, r, body)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func strs(v any) []string {
	var out []string
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			s, _ := e.(string)
			out = append(out, s)
		}
	case string:
		out = []string{x}
	}
	return out
}

func (s *Server) libre(w http.ResponseWriter, body map[string]any) {
	if s.LibreKey != "" && body["api_key"] != s.LibreKey {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Invalid API key"})
		return
	}
	target, _ := body["target"].(string)
	if !s.libreOffers(target) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": target + " is not supported"})
		return
	}
	q := body["q"]
	texts := strs(q)
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = Translate(t, target)
	}
	if _, one := q.(string); one {
		writeJSON(w, http.StatusOK, map[string]any{"translatedText": out[0]})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"translatedText": out})
}

func (s *Server) libreOffers(code string) bool {
	if s.LibreLanguages == nil {
		return true
	}
	for _, l := range s.LibreLanguages {
		if l == code {
			return true
		}
	}
	return false
}

func (s *Server) libreLanguages(w http.ResponseWriter) {
	langs := s.LibreLanguages
	if langs == nil {
		langs = []string{"en", "hr", "de", "fr", "es", "it", "sr", "sl"}
	}
	var out []map[string]any
	for _, l := range append([]string{"en"}, langs...) {
		out = append(out, map[string]any{"code": l, "name": l, "targets": langs})
	}
	writeJSON(w, http.StatusOK, out)
}

func deeplAuth(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	return h == "DeepL-Auth-Key "+DeepLFreeKey || h == "DeepL-Auth-Key "+DeepLProKey
}

func (s *Server) deeplLimit() int64 {
	if s.DeepLLimit > 0 {
		return s.DeepLLimit
	}
	return 500000
}

func (s *Server) deepl(w http.ResponseWriter, r *http.Request, body map[string]any) {
	if !deeplAuth(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "Wrong endpoint or key"})
		return
	}
	texts := strs(body["text"])
	target, _ := body["target_lang"].(string)
	n := int64(0)
	for _, t := range texts {
		n += int64(len([]rune(t)))
	}
	s.mu.Lock()
	over := s.deeplUsed+n > s.deeplLimit()
	if !over {
		s.deeplUsed += n
	}
	s.mu.Unlock()
	if over {
		writeJSON(w, 456, map[string]string{"message": "Quota exceeded"})
		return
	}
	var out []map[string]string
	for _, t := range texts {
		out = append(out, map[string]string{"detected_source_language": "EN", "text": Translate(t, strings.ToLower(target))})
	}
	writeJSON(w, http.StatusOK, map[string]any{"translations": out})
}

// llmLines reads the numbered lines and the target language of a prompt.
func llmLines(system, user string) ([]string, string) {
	var in struct {
		Lines []struct {
			Text string `json:"text"`
		} `json:"lines"`
	}
	_ = json.Unmarshal([]byte(user), &in)
	to := "xx"
	if _, rest, ok := strings.Cut(system, " to "); ok {
		name, _, _ := strings.Cut(rest, ".")
		to = langCode(name)
	}
	out := make([]string, len(in.Lines))
	for i, l := range in.Lines {
		out[i] = l.Text
	}
	return out, to
}

func langCode(name string) string {
	switch strings.TrimSpace(name) {
	case "Croatian":
		return "hr"
	case "English":
		return "en"
	case "German":
		return "de"
	case "Serbian":
		return "sr"
	}
	return strings.ToLower(name)
}

func (s *Server) answer(system, user string) string {
	lines, to := llmLines(system, user)
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = Translate(l, to)
	}
	if len(out) > 1 && s.take(&s.DropLine) {
		out = out[:len(out)-1]
	}
	b, _ := json.Marshal(map[string]any{"lines": out})
	return string(b)
}

func (s *Server) chat(w http.ResponseWriter, body map[string]any) {
	msgs, _ := body["messages"].([]any)
	var system, user string
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		c, _ := mm["content"].(string)
		switch mm["role"] {
		case "system":
			system = c
		case "user":
			user = c
		}
	}
	text := s.answer(system, user)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": "chatcmpl-fake", "object": "chat.completion",
		"choices": []map[string]any{{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": text}}},
		"usage":   map[string]int{"prompt_tokens": len(system+user) / 4, "completion_tokens": len(text) / 4, "total_tokens": len(system+user+text) / 4},
	})
}

func (s *Server) anthropic(w http.ResponseWriter, r *http.Request, body map[string]any) {
	if r.Header.Get("x-api-key") != AnthropicKey {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"type": "error", "error": map[string]string{"type": "authentication_error", "message": "invalid x-api-key"}})
		return
	}
	if r.Header.Get("anthropic-version") == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"type": "error", "error": map[string]string{"type": "invalid_request_error", "message": "anthropic-version header is required"}})
		return
	}
	if _, ok := body["temperature"]; ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"type": "error", "error": map[string]string{"type": "invalid_request_error", "message": "temperature is not supported for this model"}})
		return
	}
	system, _ := body["system"].(string)
	var user string
	if msgs, _ := body["messages"].([]any); len(msgs) > 0 {
		m, _ := msgs[0].(map[string]any)
		user, _ = m["content"].(string)
	}
	text := s.answer(system, user)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": "msg_fake", "type": "message", "role": "assistant", "model": body["model"], "stop_reason": "end_turn",
		"content": []map[string]any{{"type": "text", "text": text}},
		"usage":   map[string]int{"input_tokens": len(system+user) / 4, "output_tokens": len(text) / 4},
	})
}

func (s *Server) gemini(w http.ResponseWriter, r *http.Request, body map[string]any) {
	if r.Header.Get("x-goog-api-key") != GeminiKey {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": 400, "status": "INVALID_ARGUMENT", "message": "API key not valid.",
			"details": []map[string]string{{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "reason": "API_KEY_INVALID"}}}})
		return
	}
	text := func(v any) string {
		m, _ := v.(map[string]any)
		parts, _ := m["parts"].([]any)
		if len(parts) == 0 {
			return ""
		}
		p, _ := parts[0].(map[string]any)
		t, _ := p["text"].(string)
		return t
	}
	system := text(body["systemInstruction"])
	var user string
	if c, _ := body["contents"].([]any); len(c) > 0 {
		user = text(c[0])
	}
	out := s.answer(system, user)
	writeJSON(w, http.StatusOK, map[string]any{
		"candidates":    []map[string]any{{"content": map[string]any{"role": "model", "parts": []map[string]string{{"text": out}}}, "finishReason": "STOP"}},
		"usageMetadata": map[string]int{"promptTokenCount": len(system+user) / 4, "candidatesTokenCount": len(out) / 4},
	})
}

// String describes the fake (for logs).
func (s *Server) String() string {
	return fmt.Sprintf("fake translators (%d requests)", len(s.Requests()))
}
