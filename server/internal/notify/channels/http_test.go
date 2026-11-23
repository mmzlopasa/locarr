package channels

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/notify"
)

type captured struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   []byte
}

type receiver struct {
	srv    *httptest.Server
	mu     sync.Mutex
	got    []captured
	status int
	answer string
}

func newReceiver(t *testing.T, status int, answer string) *receiver {
	t.Helper()
	r := &receiver{status: status, answer: answer}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.got = append(r.got, captured{Method: req.Method, Path: req.URL.Path, Query: req.URL.RawQuery, Header: req.Header.Clone(), Body: b})
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(r.status)
		_, _ = io.WriteString(w, r.answer)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) last(t *testing.T) captured {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.got) != 1 {
		t.Fatalf("receiver got %d requests, want 1", len(r.got))
	}
	return r.got[0]
}

func testEnv(t *testing.T, sec Secrets) Env {
	t.Helper()
	hc, err := httpx.New(httpx.Options{Policy: httpx.Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("::1/128")}}, Secrets: sec.List()})
	if err != nil {
		t.Fatal(err)
	}
	return Env{HTTP: hc, Now: func() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) }}
}

func sampleMessage() Message {
	ev := notify.Event{Type: notify.RequestAvailable, At: time.Date(2026, 11, 23, 11, 0, 0, 0, time.UTC), Title: "Big Buck Bunny (2008)",
		Poster: "/bbb.jpg", MediaKind: "movie", RequestID: 7, Actor: &notify.User{ID: 2, Username: "ana"}, Params: map[string]any{"is4k": false}}
	return Compose(ev, "en", "http://locarr.test")
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, b)
	}
	return m
}

func TestWebhook(t *testing.T) {
	r := newReceiver(t, 200, `{}`)
	sec := Secrets{Secret: "hook-secret-123"}
	msg := sampleMessage()
	if err := Send(context.Background(), Webhook, Config{URL: r.srv.URL + "/hook"}, sec, testEnv(t, sec), msg); err != nil {
		t.Fatal(err)
	}
	c := r.last(t)
	if c.Method != http.MethodPost || c.Path != "/hook" {
		t.Fatalf("got %s %s", c.Method, c.Path)
	}
	if c.Header.Get("Content-Type") != "application/json" || c.Header.Get("X-Locarr-Event") != "request.available" {
		t.Fatalf("headers %v", c.Header)
	}
	m := hmac.New(sha256.New, []byte(sec.Secret))
	m.Write(c.Body)
	if want := "sha256=" + hex.EncodeToString(m.Sum(nil)); c.Header.Get(SignatureHeader) != want {
		t.Fatalf("signature %q, want %q", c.Header.Get(SignatureHeader), want)
	}
	body := decode(t, c.Body)
	if body["event"] != "request.available" || body["title"] != "Big Buck Bunny (2008)" || body["url"] != "http://locarr.test/requests/7" ||
		body["poster"] != "https://image.tmdb.org/t/p/w342/bbb.jpg" || body["at"] != "2026-11-23T11:00:00Z" || body["requestId"] != float64(7) {
		t.Fatalf("payload %v", body)
	}
	if a, _ := body["actor"].(map[string]any); a["username"] != "ana" {
		t.Fatalf("actor %v", body["actor"])
	}
	// PUT and no signature without a secret.
	r2 := newReceiver(t, 204, ``)
	if err := Send(context.Background(), Webhook, Config{URL: r2.srv.URL, Method: "PUT"}, Secrets{}, testEnv(t, Secrets{}), msg); err != nil {
		t.Fatal(err)
	}
	if c := r2.last(t); c.Method != http.MethodPut || c.Header.Get(SignatureHeader) != "" {
		t.Fatalf("got %s with signature %q", c.Method, c.Header.Get(SignatureHeader))
	}
}

func TestNtfy(t *testing.T) {
	r := newReceiver(t, 200, `{"id":"x"}`)
	sec := Secrets{Token: "tk_ntfytoken"}
	cfg := Config{URL: r.srv.URL, Topic: "family-media", Priority: 4, Tags: []string{"tada", "movie"}}
	if err := Send(context.Background(), Ntfy, cfg, sec, testEnv(t, sec), sampleMessage()); err != nil {
		t.Fatal(err)
	}
	c := r.last(t)
	if c.Method != http.MethodPost || c.Path != "/" || c.Header.Get("Authorization") != "Bearer tk_ntfytoken" {
		t.Fatalf("got %s %s auth %q", c.Method, c.Path, c.Header.Get("Authorization"))
	}
	b := decode(t, c.Body)
	if b["topic"] != "family-media" || b["title"] != "Now available: Big Buck Bunny (2008)" || b["priority"] != float64(4) ||
		b["click"] != "http://locarr.test/requests/7" || len(b["tags"].([]any)) != 2 || !strings.Contains(b["message"].(string), "ana") {
		t.Fatalf("payload %v", b)
	}
	// Basic auth with user name and password.
	r2 := newReceiver(t, 200, `{}`)
	sec2 := Secrets{Password: "pw-secret"}
	if err := Send(context.Background(), Ntfy, Config{URL: r2.srv.URL, Topic: "t", Username: "me"}, sec2, testEnv(t, sec2), sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if u, p, ok := (&http.Request{Header: r2.last(t).Header}).BasicAuth(); !ok || u != "me" || p != "pw-secret" {
		t.Fatalf("basic auth %q %q %v", u, p, ok)
	}
}

func TestGotifyTokenOnlyInHeader(t *testing.T) {
	r := newReceiver(t, 200, `{"id":1}`)
	sec := Secrets{Token: "AbCgotifyToken"}
	if err := Send(context.Background(), Gotify, Config{URL: r.srv.URL + "/gotify/", Priority: 6}, sec, testEnv(t, sec), sampleMessage()); err != nil {
		t.Fatal(err)
	}
	c := r.last(t)
	if c.Path != "/gotify/message" || c.Header.Get("X-Gotify-Key") != "AbCgotifyToken" {
		t.Fatalf("got %s key %q", c.Path, c.Header.Get("X-Gotify-Key"))
	}
	if strings.Contains(c.Path+"?"+c.Query, "AbCgotifyToken") || c.Query != "" {
		t.Fatalf("token leaked into the URL: %s?%s", c.Path, c.Query)
	}
	b := decode(t, c.Body)
	click := b["extras"].(map[string]any)["client::notification"].(map[string]any)["click"].(map[string]any)["url"]
	if b["priority"] != float64(6) || click != "http://locarr.test/requests/7" {
		t.Fatalf("payload %v", b)
	}
}

func TestDiscord(t *testing.T) {
	r := newReceiver(t, 204, ``)
	sec := Secrets{WebhookURL: r.srv.URL + "/api/webhooks/1/secrettoken"}
	long := Message{Event: notify.Event{Type: notify.RequestFailed}, Headline: strings.Repeat("h", 300), Body: strings.Repeat("b", 5000),
		URL: "http://locarr.test/requests/1", PosterURL: "https://image.tmdb.org/t/p/w342/p.jpg"}
	if err := Send(context.Background(), Discord, Config{Username: "Media"}, sec, testEnv(t, sec), long); err != nil {
		t.Fatal(err)
	}
	c := r.last(t)
	if c.Path != "/api/webhooks/1/secrettoken" {
		t.Fatalf("path %s", c.Path)
	}
	b := decode(t, c.Body)
	e := b["embeds"].([]any)[0].(map[string]any)
	if b["username"] != "Media" || len([]rune(e["title"].(string))) != 256 || len([]rune(e["description"].(string))) != 4096 ||
		e["thumbnail"].(map[string]any)["url"] != "https://image.tmdb.org/t/p/w342/p.jpg" || e["url"] != "http://locarr.test/requests/1" ||
		e["color"] != float64(0xd1242f) {
		t.Fatalf("embed %v", e)
	}
}

func TestMarkdownV2Escaping(t *testing.T) {
	for in, want := range map[string]string{
		"Big Buck Bunny (2008).":  `Big Buck Bunny \(2008\)\.`,
		"Spider-Man!":             `Spider\-Man\!`,
		"a_b*c[d]e~f`g>h#i+j=k|l": "a\\_b\\*c\\[d\\]e\\~f\\`g\\>h\\#i\\+j\\=k\\|l",
		`{x}\y`:                   `\{x\}\\y`,
		"Čovjek bez sjene":        "Čovjek bez sjene",
	} {
		if got := EscapeMarkdownV2(in); got != want {
			t.Errorf("EscapeMarkdownV2(%q) = %q, want %q", in, got, want)
		}
	}
	msg := Message{Headline: "Now available: Up (2009)", Body: "By: a_b", URL: `http://l.test/x_(1)\y`}
	want := "*Now available: Up \\(2009\\)*\n\nBy: a\\_b\n\n[Open in Locarr](http://l.test/x_(1\\)\\\\y)"
	if got := TelegramText(msg, "en"); got != want {
		t.Fatalf("TelegramText:\n%s\nwant\n%s", got, want)
	}
}

func TestTelegram(t *testing.T) {
	r := newReceiver(t, 200, `{"ok":true,"result":{}}`)
	sec := Secrets{BotToken: "123456:ABC-secret"}
	env := testEnv(t, sec)
	env.TelegramAPI = r.srv.URL
	if err := Send(context.Background(), Telegram, Config{ChatID: "-1001"}, sec, env, sampleMessage()); err != nil {
		t.Fatal(err)
	}
	c := r.last(t)
	if c.Path != "/bot123456:ABC-secret/sendMessage" {
		t.Fatalf("path %s", c.Path)
	}
	b := decode(t, c.Body)
	if b["chat_id"] != "-1001" || b["parse_mode"] != "MarkdownV2" || b["disable_web_page_preview"] != true ||
		!strings.HasPrefix(b["text"].(string), `*Now available: Big Buck Bunny \(2008\)*`) {
		t.Fatalf("payload %v", b)
	}
	// ok:false with 200, and an error status: the description is shown, the token never.
	for _, tc := range []struct {
		status int
		answer string
	}{{200, `{"ok":false,"description":"Bad Request: chat not found"}`}, {401, `{"ok":false,"description":"Unauthorized"}`}} {
		r := newReceiver(t, tc.status, tc.answer)
		env.TelegramAPI = r.srv.URL
		err := Send(context.Background(), Telegram, Config{ChatID: "1"}, sec, env, sampleMessage())
		if err == nil || strings.Contains(err.Error(), sec.BotToken) || strings.Contains(err.Error(), "ABC-secret") {
			t.Fatalf("error %v", err)
		}
		var ans struct{ Description string }
		_ = json.Unmarshal([]byte(tc.answer), &ans)
		if !strings.Contains(err.Error(), ans.Description) {
			t.Fatalf("error %v does not explain %q", err, ans.Description)
		}
	}
	// Unreachable server: the token is in the URL path and must not leak.
	env.TelegramAPI = "http://127.0.0.1:1"
	if err := Send(context.Background(), Telegram, Config{ChatID: "1"}, sec, env, sampleMessage()); err == nil || strings.Contains(err.Error(), "ABC-secret") {
		t.Fatalf("error %v", err)
	}
}

func TestPushover(t *testing.T) {
	r := newReceiver(t, 200, `{"status":1,"request":"x"}`)
	sec := Secrets{Token: "apptoken123", UserKey: "userkey456"}
	env := testEnv(t, sec)
	env.PushoverAPI = r.srv.URL
	if err := Send(context.Background(), Pushover, Config{Priority: 1, Sound: "magic", Device: "phone"}, sec, env, sampleMessage()); err != nil {
		t.Fatal(err)
	}
	c := r.last(t)
	f, err := url.ParseQuery(string(c.Body))
	if err != nil || c.Path != "/1/messages.json" || c.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Fatalf("got %s %v %v", c.Path, c.Header, err)
	}
	if f.Get("token") != "apptoken123" || f.Get("user") != "userkey456" || f.Get("title") != "Now available: Big Buck Bunny (2008)" ||
		f.Get("url") != "http://locarr.test/requests/7" || f.Get("priority") != "1" || f.Get("sound") != "magic" || f.Get("device") != "phone" ||
		f.Get("message") == "" {
		t.Fatalf("form %v", f)
	}
	for _, tc := range []struct {
		status int
		answer string
	}{{200, `{"status":0,"errors":["user identifier is invalid"]}`}, {400, `{"status":0,"errors":["application token is invalid"]}`}} {
		r := newReceiver(t, tc.status, tc.answer)
		env.PushoverAPI = r.srv.URL
		err := Send(context.Background(), Pushover, Config{}, sec, env, sampleMessage())
		if err == nil || !strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "apptoken123") || strings.Contains(err.Error(), "userkey456") {
			t.Fatalf("error %v", err)
		}
	}
}

func TestApprise(t *testing.T) {
	r := newReceiver(t, 200, `{}`)
	msg := sampleMessage()
	if err := Send(context.Background(), Apprise, Config{URL: r.srv.URL + "/notify/apprise", Tag: "family"}, Secrets{}, testEnv(t, Secrets{}), msg); err != nil {
		t.Fatal(err)
	}
	c := r.last(t)
	b := decode(t, c.Body)
	if c.Path != "/notify/apprise" || b["tag"] != "family" || b["type"] != "success" || b["format"] != "text" || b["title"] != msg.Headline {
		t.Fatalf("got %s %v", c.Path, b)
	}
	r2 := newReceiver(t, 200, `{}`)
	if err := Send(context.Background(), Apprise, Config{URL: r2.srv.URL}, Secrets{}, testEnv(t, Secrets{}), msg); err != nil {
		t.Fatal(err)
	}
	if _, ok := decode(t, r2.last(t).Body)["tag"]; ok {
		t.Fatal("empty tag was sent")
	}
}

func TestHTTPErrorsAreRedacted(t *testing.T) {
	sec := Secrets{Secret: "s3cr3t-value", Token: "tok3n-value", WebhookURL: "", BotToken: ""}
	r := newReceiver(t, 500, `{"error":"boom s3cr3t-value tok3n-value `+strings.Repeat("x", 400)+`"}`)
	for _, k := range []Kind{Webhook, Apprise} {
		err := Send(context.Background(), k, Config{URL: r.srv.URL}, sec, testEnv(t, sec), sampleMessage())
		if err == nil || !strings.Contains(err.Error(), "HTTP 500") || strings.Contains(err.Error(), "s3cr3t-value") ||
			strings.Contains(err.Error(), "tok3n-value") || len(err.Error()) > 300 {
			t.Fatalf("%s: error %v", k, err)
		}
	}
	gs := Secrets{Token: "gotify-tok3n"}
	g := newReceiver(t, 401, `{"error":"Unauthorized","errorDescription":"you need to provide a valid access token"}`)
	if err := Send(context.Background(), Gotify, Config{URL: g.srv.URL}, gs, testEnv(t, gs), sampleMessage()); err == nil ||
		!strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "gotify-tok3n") {
		t.Fatalf("gotify error %v", err)
	}
	ds := Secrets{WebhookURL: "http://127.0.0.1:1/api/webhooks/1/discord-secret"}
	if err := Send(context.Background(), Discord, Config{}, ds, testEnv(t, ds), sampleMessage()); err == nil || strings.Contains(err.Error(), "discord-secret") {
		t.Fatalf("discord error %v", err)
	}
}

func TestValidate(t *testing.T) {
	ok := []struct {
		k   Kind
		cfg Config
		sec Secrets
	}{
		{Webhook, Config{URL: "https://h.test/x"}, Secrets{}},
		{Ntfy, Config{Topic: "my_topic-1"}, Secrets{}},
		{Gotify, Config{URL: "http://gotify:80"}, Secrets{Token: "t"}},
		{Discord, Config{}, Secrets{WebhookURL: "https://discord.com/api/webhooks/1/x"}},
		{Telegram, Config{ChatID: "42"}, Secrets{BotToken: "1:a"}},
		{Pushover, Config{Priority: -2}, Secrets{Token: "a", UserKey: "u"}},
		{Email, Config{To: []string{"Ana <ana@example.com>"}}, Secrets{}},
		{Apprise, Config{URL: "http://apprise:8000/notify/apprise"}, Secrets{}},
	}
	for _, c := range ok {
		if err := Validate(c.k, c.cfg, c.sec); err != nil {
			t.Errorf("%s: %v", c.k, err)
		}
	}
	bad := []struct {
		k   Kind
		cfg Config
		sec Secrets
	}{
		{Webhook, Config{URL: "ftp://h.test"}, Secrets{}},
		{Webhook, Config{URL: "https://h.test", Method: "GET"}, Secrets{}},
		{Ntfy, Config{Topic: "bad topic"}, Secrets{}},
		{Ntfy, Config{Topic: "t", Priority: 9}, Secrets{}},
		{Gotify, Config{URL: "http://g"}, Secrets{}},
		{Discord, Config{}, Secrets{}},
		{Telegram, Config{}, Secrets{BotToken: "1:a"}},
		{Pushover, Config{Priority: 3}, Secrets{Token: "a", UserKey: "u"}},
		{Email, Config{To: []string{"not-an-address"}}, Secrets{}},
		{Email, Config{To: []string{"a@b.test\r\nBcc: x@y.test"}}, Secrets{}},
		{Email, Config{}, Secrets{}},
		{Apprise, Config{}, Secrets{}},
		{Webhook, Config{URL: "https://h.test", Language: "de"}, Secrets{}},
	}
	for _, c := range bad {
		if err := Validate(c.k, c.cfg, c.sec); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s %+v: want ErrInvalid, got %v", c.k, c.cfg, err)
		}
	}
	if _, err := Get("sms"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Get(sms) = %v", err)
	}
	many := make([]string, 21)
	for i := range many {
		many[i] = "a@b.test"
	}
	if err := Validate(Email, Config{To: many}, Secrets{}); !errors.Is(err, ErrInvalid) {
		t.Fatal("21 recipients accepted")
	}
}
