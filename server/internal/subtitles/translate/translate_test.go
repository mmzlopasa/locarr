package translate_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/translate"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/translate/fake"
)

func client(t *testing.T, base string) *httpx.Client {
	t.Helper()
	u, _ := url.Parse(base)
	a := netip.MustParseAddr(u.Hostname())
	hc, err := httpx.New(httpx.Options{Policy: httpx.Policy{Allowed: []netip.Prefix{netip.PrefixFrom(a, a.BitLen())}}, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return hc
}

type env struct {
	f     *fake.Server
	url   string
	hc    *httpx.Client
	slept []time.Duration
}

func newEnv(t *testing.T) *env {
	f := fake.New()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &env{f: f, url: srv.URL, hc: client(t, srv.URL)}
}

func (e *env) cfg(path, key string) translate.Config {
	return translate.Config{BaseURL: e.url + path, APIKey: key, HTTP: e.hc, Sleep: func(_ context.Context, d time.Duration) error {
		e.slept = append(e.slept, d)
		return nil
	}}
}

var sample = []string{"Where are you going?", "<i>Wait for me!</i>", "{\\an8}Upstairs."}

func TestEveryTranslatorKeepsCountAndMarkup(t *testing.T) {
	e := newEnv(t)
	cases := []translate.Translator{
		translate.NewLibreTranslate(e.cfg("/libretranslate", "")),
		translate.NewDeepL(e.cfg("/deepl", fake.DeepLFreeKey)),
		translate.NewOpenAI(e.cfg("/openai/v1", fake.OpenAIKey)),
		translate.NewOpenAICompatible(func() translate.Config { c := e.cfg("/compatible/v1", ""); c.Model = "llama3"; return c }()),
		translate.NewAnthropic(e.cfg("/anthropic", fake.AnthropicKey)),
		translate.NewGemini(e.cfg("/gemini", fake.GeminiKey)),
	}
	for _, tr := range cases {
		t.Run(tr.Name(), func(t *testing.T) {
			out, u, err := tr.Translate(context.Background(), translate.Request{Texts: sample, From: "en", To: "hr", Title: "Sintel (2010)"})
			if err != nil {
				t.Fatal(err)
			}
			if len(out) != len(sample) {
				t.Fatalf("got %d lines", len(out))
			}
			if out[0] != "[hr] Where are you going?" || !strings.Contains(out[1], "<i>") {
				t.Fatalf("translations %q", out)
			}
			if u.Requests == 0 || u.Characters == 0 && u.InputTokens == 0 {
				t.Fatalf("usage %+v", u)
			}
		})
	}
}

func TestDeepLFreeAndProEndpoints(t *testing.T) {
	if got := translate.DeepLURL("abc:fx"); got != translate.DeepLFreeURL {
		t.Fatalf("free key → %s", got)
	}
	if got := translate.DeepLURL("abc"); got != translate.DeepLProURL {
		t.Fatalf("pro key → %s", got)
	}
	e := newEnv(t)
	tr := translate.NewDeepL(e.cfg("/deepl", fake.DeepLProKey))
	if _, _, err := tr.Translate(context.Background(), translate.Request{Texts: []string{"Hi."}, From: "en", To: "pt-br", Title: "X"}); err != nil {
		t.Fatal(err)
	}
	r := e.f.Requests()[0]
	if r.Body["target_lang"] != "PT-BR" || r.Body["source_lang"] != "EN" || r.Body["context"] != "Subtitles of X." {
		t.Fatalf("body %v", r.Body)
	}
	if _, ok := r.Body["tag_handling"]; ok {
		t.Fatal("tag_handling without tags")
	}
	used, limit, err := tr.(translate.Quota).Quota(context.Background())
	if err != nil || used != 3 || limit != 500000 {
		t.Fatalf("quota %d/%d %v", used, limit, err)
	}
	e.f.DeepLLimit = 4
	_, _, err = tr.Translate(context.Background(), translate.Request{Texts: []string{"Hello there."}, From: "en", To: "hr"})
	var q *providers.QuotaError
	if !errors.As(err, &q) {
		t.Fatalf("over the quota: %v", err)
	}
	bad := translate.NewDeepL(e.cfg("/deepl", "wrong:fx"))
	if _, _, err := bad.Translate(context.Background(), translate.Request{Texts: []string{"Hi."}, From: "en", To: "hr"}); !errors.Is(err, providers.ErrAuth) {
		t.Fatalf("wrong key: %v", err)
	}
}

func TestLibreTranslateSendsArrays(t *testing.T) {
	e := newEnv(t)
	e.f.LibreKey = "secret"
	tr := translate.NewLibreTranslate(e.cfg("/libretranslate", "secret"))
	out, _, err := tr.Translate(context.Background(), translate.Request{Texts: sample[:2], From: "en", To: "hr"})
	if err != nil || len(out) != 2 {
		t.Fatalf("%q %v", out, err)
	}
	r := e.f.Requests()[0]
	if q, ok := r.Body["q"].([]any); !ok || len(q) != 2 || r.Body["format"] != "html" || r.Body["api_key"] != "secret" {
		t.Fatalf("body %v", r.Body)
	}
	e.f.LibreLanguages = []string{"de"}
	if ok, err := tr.(translate.Pairs).Supports(context.Background(), "en", "hr"); err != nil || ok {
		t.Fatalf("supports hr: %v %v", ok, err)
	}
	if _, _, err := tr.Translate(context.Background(), translate.Request{Texts: []string{"Hi"}, From: "en", To: "hr"}); !errors.Is(err, translate.ErrUnsupported) {
		t.Fatalf("unsupported: %v", err)
	}
}

func TestRateLimitBackoff(t *testing.T) {
	e := newEnv(t)
	e.f.RateLimit = 2
	tr := translate.NewOpenAI(e.cfg("/openai/v1", fake.OpenAIKey))
	if _, _, err := tr.Translate(context.Background(), translate.Request{Texts: []string{"Hi."}, From: "en", To: "hr"}); err != nil {
		t.Fatal(err)
	}
	if len(e.slept) != 2 || e.f.Count("openai") != 3 {
		t.Fatalf("slept %v, %d requests", e.slept, e.f.Count("openai"))
	}
	e.f.RateLimit = 10
	_, _, err := tr.Translate(context.Background(), translate.Request{Texts: []string{"Hi."}, From: "en", To: "hr"})
	var rl *providers.RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("rate limit not reported: %v", err)
	}
}

func TestLLMRetriesSmallerBatchesOnMismatch(t *testing.T) {
	e := newEnv(t)
	e.f.DropLine = 1
	tr := translate.NewAnthropic(e.cfg("/anthropic", fake.AnthropicKey))
	texts := []string{"One.", "Two.", "Three.", "Four."}
	out, u, err := tr.Translate(context.Background(), translate.Request{Texts: texts, From: "en", To: "hr"})
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range out {
		if l != "[hr] "+texts[i] {
			t.Fatalf("line %d = %q", i, l)
		}
	}
	// One failed batch of 4, then two halves.
	if n := e.f.Count("anthropic"); n != 3 || u.Requests != 3 {
		t.Fatalf("%d requests, usage %+v", n, u)
	}
	req := e.f.Requests()[0]
	if req.Header.Get("anthropic-version") == "" || req.Body["model"] != translate.AnthropicDefaultModel {
		t.Fatalf("request %v %v", req.Header, req.Body)
	}
	if _, ok := req.Body["temperature"]; ok {
		t.Fatal("temperature sent to Anthropic")
	}
	// Only subtitle text and the title leave Locarr.
	sys, _ := req.Body["system"].(string)
	if !strings.Contains(sys, "Croatian") {
		t.Fatalf("system %q", sys)
	}
}

func TestParseLines(t *testing.T) {
	in := []string{"Hello.", "<i>Run!</i>"}
	if _, err := translate.ParseLines("```json\n{\"lines\":[\"Bok.\",\"<i>Bježi!</i>\"]}\n```", in); err != nil {
		t.Fatal(err)
	}
	for name, ans := range map[string]string{
		"count":    `{"lines":["Bok."]}`,
		"markup":   `{"lines":["Bok.","Bježi!"]}`,
		"empty":    `{"lines":["Bok.",""]}`,
		"not json": `Bok. Bježi!`,
	} {
		if _, err := translate.ParseLines(ans, in); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	out, err := translate.ParseLines(`{"lines":["1. Bok.","2. <i>Bježi!</i>"]}`, in)
	if err != nil || out[0] != "Bok." {
		t.Fatalf("numbering kept: %q %v", out, err)
	}
}

func TestSplitAndJoinCue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Where are you\ngoing?", "[hr] Where are you going?"},
		{"<i>I told you\nalready.</i>", "<i>[hr] I told you already.</i>"},
		{"- Who is it?\n- Me.", "- [hr] Who is it?\n- [hr] Me."},
		{"{\\an8}Upstairs.", "{\\an8}[hr] Upstairs."},
		{"♪ ♪", "♪ ♪"},
	}
	for _, c := range cases {
		ps := translate.SplitCue(c.in)
		texts := make([]string, len(ps))
		for i, p := range ps {
			if !p.Keep {
				texts[i] = fake.Translate(p.Text, "hr")
			}
		}
		if got := translate.JoinCue(ps, texts, 42); got != c.want {
			t.Errorf("%q → %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRewrap(t *testing.T) {
	got := translate.Rewrap("Ne mogu vjerovati da si to napravio, nakon svega što smo prošli zajedno.", 42)
	if len(got) != 2 {
		t.Fatalf("%q", got)
	}
	for _, l := range got {
		if len([]rune(l)) > 42 {
			t.Fatalf("line too long: %q", l)
		}
	}
	if got[0] != "Ne mogu vjerovati da si to napravio," {
		t.Fatalf("break not after the comma: %q", got)
	}
	if got := translate.Rewrap("Kratko.", 42); len(got) != 1 {
		t.Fatalf("%q", got)
	}
	if got := translate.Rewrap("<i>Ovo je jedna rečenica koja je dugačka i ima puno riječi u sebi</i>", 42); len(got) != 2 ||
		!strings.HasPrefix(got[0], "<i>") || !strings.HasSuffix(got[1], "</i>") {
		t.Fatalf("markup split: %q", got)
	}
	if got := translate.Rewrap("今日はとても良い天気ですね散歩に行きましょうか", 16); len(got) != 2 {
		t.Fatalf("Japanese: %q", got)
	}
}

func TestBatches(t *testing.T) {
	b := translate.Batches([]string{"aaaa", "bbbb", "cccc", "dd"}, translate.Limits{MaxTexts: 2, MaxChars: 7})
	if len(b) != 3 || b[0] != [2]int{0, 1} {
		t.Fatalf("%v", b)
	}
}
