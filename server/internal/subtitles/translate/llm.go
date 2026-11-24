package translate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
)

// completion is one answer of a language model.
type completion struct {
	Text      string
	Usage     Usage
	Truncated bool // the model stopped at the output limit
	Refused   bool // the model declined
}

// completer sends one prompt to a language model.
type completer interface {
	complete(ctx context.Context, system, user string, maxTokens int) (completion, error)
}

// llm is the Translator around a language model: numbered lines in, a JSON
// object with exactly as many lines out, validated, and retried in smaller
// batches when the count, the markup or the shape is off.
type llm struct {
	name   string
	c      completer
	limits Limits
}

func (l *llm) Name() string   { return l.name }
func (l *llm) Limits() Limits { return l.limits }

// SystemPrompt is the instruction sent with every batch.
func SystemPrompt(from, to string) string {
	return fmt.Sprintf(`You translate film and TV subtitles from %[1]s to %[2]s.
The user message is JSON: {"title": "...", "previous": ["..."], "lines": [{"n": 1, "text": "..."}, ...]}.
Answer with JSON only, in the form {"lines": ["...", "..."]}: exactly one string per input line, in the same order, with no numbers in front.
Rules:
- Translate each line on its own. Never merge, split, drop, reorder or add lines.
- Keep markup exactly as it is: tags like <i>, </i>, <b>, <font color="..."> and override blocks in braces like {\an8} stay where they are; translate only the words.
- Write natural spoken %[2]s as used in subtitles: short, clear, in the register of the speaker. Keep names. No notes, explanations or quotation marks around lines.
- A line that is only a sound, music or a name may stay as it is.
- "title" and "previous" are context only; do not translate or return them.`, LanguageName(from), LanguageName(to))
}

type llmLine struct {
	N    int    `json:"n"`
	Text string `json:"text"`
}

type llmInput struct {
	Title    string    `json:"title,omitempty"`
	Previous []string  `json:"previous,omitempty"`
	Lines    []llmLine `json:"lines"`
}

// UserMessage is the JSON body of a batch.
func UserMessage(r Request) string {
	in := llmInput{Title: r.Title, Previous: r.Previous, Lines: make([]llmLine, len(r.Texts))}
	for i, t := range r.Texts {
		in.Lines[i] = llmLine{N: i + 1, Text: t}
	}
	b, _ := json.Marshal(in)
	return string(b)
}

var reNumbered = regexp.MustCompile(`^\s*(\d+)\s*[.|):]\s*`)

// ParseLines reads {"lines": [...]} from a model's answer (code fences and
// text around the object are ignored) and checks it against the input:
// the same count, nothing empty where there was text, the same markup and
// no line that swallowed its neighbours.
func ParseLines(answer string, input []string) ([]string, error) {
	s := strings.TrimSpace(answer)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	var out struct {
		Lines []json.RawMessage `json:"lines"`
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("%w: not the JSON object asked for", providers.ErrBadResponse)
	}
	if len(out.Lines) != len(input) {
		return nil, fmt.Errorf("%w: %d lines for %d", ErrCountMismatch, len(out.Lines), len(input))
	}
	lines := make([]string, len(out.Lines))
	for i, src := range input {
		raw := out.Lines[i]
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			// {"n": 1, "text": "..."} instead of a string.
			var o llmLine
			if err := json.Unmarshal(raw, &o); err != nil {
				return nil, fmt.Errorf("%w: line %d is not text", providers.ErrBadResponse, i+1)
			}
			v = o.Text
		}
		if m := reNumbered.FindStringSubmatch(v); m != nil && m[1] == fmt.Sprint(i+1) && !reNumbered.MatchString(src) {
			v = v[len(m[0]):]
		}
		v = strings.TrimSpace(strings.ReplaceAll(v, "\n", " "))
		if v == "" && strings.TrimSpace(src) != "" {
			return nil, fmt.Errorf("%w: line %d is empty", ErrCountMismatch, i+1)
		}
		if !SameTags(src, v) {
			return nil, fmt.Errorf("%w: line %d lost or changed its markup", ErrCountMismatch, i+1)
		}
		in, o := utf8.RuneCountInString(src), utf8.RuneCountInString(v)
		if len(input) > 1 && o > 3*in+40 {
			return nil, fmt.Errorf("%w: line %d looks merged with others", ErrCountMismatch, i+1)
		}
		lines[i] = v
	}
	return lines, nil
}

func maxTokensFor(texts []string) int {
	est := int(Chars(texts...))/2 + 1024
	return min(max(est, 2048), 16000)
}

// Translate implements Translator.
func (l *llm) Translate(ctx context.Context, r Request) ([]string, Usage, error) {
	var total Usage
	out, err := l.translate(ctx, r, &total, 0)
	return out, total, err
}

func (l *llm) translate(ctx context.Context, r Request, total *Usage, depth int) ([]string, error) {
	if len(r.Texts) == 0 {
		return nil, nil
	}
	attempts := 1
	if len(r.Texts) == 1 {
		attempts = 2
	}
	var lastErr error
	for a := 0; a < attempts; a++ {
		res, err := l.c.complete(ctx, SystemPrompt(r.From, r.To), UserMessage(r), maxTokensFor(r.Texts))
		if err != nil {
			return nil, err
		}
		total.Add(res.Usage)
		if res.Refused {
			lastErr = fmt.Errorf("%w: the model declined to translate", providers.ErrBadResponse)
			continue
		}
		if res.Truncated {
			lastErr = fmt.Errorf("%w: the answer was cut off", ErrCountMismatch)
			break
		}
		lines, err := ParseLines(res.Text, r.Texts)
		if err == nil {
			return lines, nil
		}
		lastErr = err
	}
	if len(r.Texts) == 1 {
		// Last resort for a single line: accept it without its markup.
		if errors.Is(lastErr, ErrCountMismatch) {
			res, err := l.c.complete(ctx, SystemPrompt(r.From, r.To), UserMessage(Request{Texts: []string{StripTags(r.Texts[0])}, From: r.From,
				To: r.To, Title: r.Title}), maxTokensFor(r.Texts))
			if err == nil {
				total.Add(res.Usage)
				if lines, err := ParseLines(res.Text, []string{StripTags(r.Texts[0])}); err == nil {
					return lines, nil
				}
			}
		}
		return nil, lastErr
	}
	if depth > 8 {
		return nil, lastErr
	}
	// Smaller batches: halves, each with the lines before it as context.
	mid := len(r.Texts) / 2
	a := r
	a.Texts = r.Texts[:mid]
	first, err := l.translate(ctx, a, total, depth+1)
	if err != nil {
		return nil, err
	}
	b := r
	b.Texts = r.Texts[mid:]
	b.Previous = tail(append(append([]string{}, r.Previous...), r.Texts[:mid]...), 3)
	second, err := l.translate(ctx, b, total, depth+1)
	if err != nil {
		return nil, err
	}
	return append(first, second...), nil
}

func tail(s []string, n int) []string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// postJSON posts body; on a 400 that names one of the optional fields (a
// model that does not take temperature, a server without JSON mode) the
// field is dropped and the request sent again.
func postJSON(ctx context.Context, cfg Config, url string, hdr http.Header, body map[string]any, optional ...string) (*httpx.Response, error) {
	for {
		res, err := cfg.post(ctx, url, hdr, body)
		if err != nil {
			return nil, err
		}
		if res.Status != http.StatusBadRequest && res.Status != http.StatusUnprocessableEntity {
			return res, nil
		}
		dropped := false
		msg := strings.ToLower(string(res.Body))
		for i, f := range optional {
			if _, ok := body[f]; ok && strings.Contains(msg, strings.ToLower(f)) {
				delete(body, f)
				optional = append(optional[:i:i], optional[i+1:]...)
				dropped = true
				break
			}
		}
		if !dropped {
			return res, nil
		}
	}
}
