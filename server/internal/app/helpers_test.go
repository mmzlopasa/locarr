package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/api"
	"github.com/mmzlopasa/locarr/server/internal/config"
	"github.com/mmzlopasa/locarr/server/internal/webui"
)

const testSetupCode = "TEST-CODE"

type env struct {
	t     *testing.T
	srv   *httptest.Server
	deps  *Deps
	data  string
	token string // bearer token used by do (empty = none)
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newEnv(t *testing.T, mod func(*RouterOptions)) *env {
	t.Helper()
	base := t.TempDir()
	data := filepath.Join(base, "data")
	for _, d := range []string{"media/movies", "media/tv", "downloads"} {
		if err := os.MkdirAll(filepath.Join(data, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	data, _ = filepath.EvalSymlinks(data)
	cfg := config.Config{ConfigDir: filepath.Join(base, "config"), DataRoots: []string{data}, LogLevel: "info"}
	ctx := context.Background()
	deps, err := Bootstrap(ctx, cfg, quietLog(), testSetupCode)
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.Jobs.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = deps.Close() })
	h := &api.Handler{DB: deps.DB, Auth: deps.Auth, Settings: deps.Settings, Jobs: deps.Jobs, RootFolders: deps.Roots, Quality: deps.Quality, Formats: deps.Formats,
		ConfigDir: cfg.ConfigDir, DataRoots: cfg.DataRoots, StartedAt: time.Now().UTC(), Log: quietLog()}
	web := webui.FromFS(fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>Locarr</title><div id=root></div>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	})
	opts := RouterOptions{Web: web, Log: quietLog()}
	if mod != nil {
		mod(&opts)
	}
	router, err := NewRouter(h, opts)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv, deps: deps, data: data}
}

type resp struct {
	*http.Response
	body []byte
}

func (r resp) json(v any) {
	if err := json.Unmarshal(r.body, v); err != nil {
		panic(string(r.body))
	}
}

func (r resp) code() string {
	var e struct{ Code string }
	_ = json.Unmarshal(r.body, &e)
	return e.Code
}

// req sends a request; hdr may set headers (a nil value deletes).
func (e *env) req(method, path string, body any, hdr map[string]string) resp {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			rd = bytes.NewBufferString(b)
		default:
			raw, _ := json.Marshal(b)
			rd = bytes.NewReader(raw)
		}
	}
	r, err := http.NewRequest(method, e.srv.URL+path, rd)
	if err != nil {
		e.t.Fatal(err)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if e.token != "" {
		r.Header.Set("Authorization", "Bearer "+e.token)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	res, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{res, b}
}

func (e *env) do(method, path string, body any) resp { return e.req(method, path, body, nil) }

func (e *env) expect(r resp, status int) resp {
	e.t.Helper()
	if r.StatusCode != status {
		e.t.Fatalf("%s %s: status %d, want %d: %s", r.Request.Method, r.Request.URL.Path, r.StatusCode, status, r.body)
	}
	return r
}

// setup creates the admin and keeps its bearer token.
func (e *env) setup() {
	e.t.Helper()
	r := e.expect(e.do("POST", "/api/v1/setup", map[string]any{"setupCode": testSetupCode, "username": "admin",
		"password": "admin-password", "language": "en"}), 200)
	var out struct{ Token string }
	r.json(&out)
	e.token = out.Token
}

func (e *env) login(user, pass string) string {
	e.t.Helper()
	r := e.expect(e.req("POST", "/api/v1/auth/login", map[string]any{"username": user, "password": pass}, map[string]string{"Authorization": ""}), 200)
	var out struct{ Token string }
	r.json(&out)
	return out.Token
}

func noLimits(o *RouterOptions) { o.AuthLimiter, o.AuthBans = nil, nil }
