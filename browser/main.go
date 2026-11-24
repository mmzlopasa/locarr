// Command locarr-browser is Locarr's companion browser: a small HTTP control
// server around one headless Chromium. Locarr asks it to open a page of a
// site that shows an automatic browser check (a JavaScript check, a waiting
// room or a managed challenge that passes on its own); it waits until the
// check is gone and returns the page, its cookies and the User-Agent, so
// Locarr can keep using its own HTTP client with those cookies. It never
// solves a CAPTCHA that needs a person: such pages are reported as "human".
//
// Environment:
//
//	LOCARR_BROWSER_TOKEN    shared secret Locarr sends (required, at least 16 characters)
//	LOCARR_BROWSER_ADDR     listen address (default :8490)
//	LOCARR_BROWSER_TABS     tabs at once (default 2)
//	LOCARR_BROWSER_IDLE     stop Chromium after this long without work (default 5m)
//	LOCARR_BROWSER_SANDBOX  auto, on or off (default auto: Chromium's sandbox when the container allows it)
//	LOCARR_BROWSER_CHROMIUM Chromium binary (default /usr/bin/chromium)
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Version is set at build time.
var Version = "dev"

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	addr := env("LOCARR_BROWSER_ADDR", ":8490")
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(addr))
	}
	token := os.Getenv("LOCARR_BROWSER_TOKEN")
	if len(token) < 16 {
		log.Error("LOCARR_BROWSER_TOKEN must be set to at least 16 characters (the same value Locarr uses)")
		os.Exit(2)
	}
	tabs, _ := strconv.Atoi(env("LOCARR_BROWSER_TABS", "2"))
	idle, err := time.ParseDuration(env("LOCARR_BROWSER_IDLE", "5m"))
	if err != nil {
		idle = 5 * time.Minute
	}
	pool := &Pool{Exec: env("LOCARR_BROWSER_CHROMIUM", "/usr/bin/chromium"), Tabs: max(1, min(tabs, 8)), Idle: idle,
		Sandbox: env("LOCARR_BROWSER_SANDBOX", "auto"), Log: log}
	st, err := StartSelfTest()
	if err != nil {
		log.Error("self-test page", "error", err)
		os.Exit(1)
	}
	api := &API{Token: token, Solver: &Solver{Pool: pool}, SelfTest: st, Log: log}
	srv := &http.Server{Addr: addr, Handler: api.Routes(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: maxTimeout + 30*time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Info("locarr-browser listening", "addr", addr, "version", Version, "tabs", pool.Tabs)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("listen", "error", err)
		pool.Close()
		os.Exit(1)
	}
	pool.Close()
}

func healthcheck(addr string) int {
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	c := &http.Client{Timeout: 3 * time.Second}
	res, err := c.Get("http://" + host + "/healthz")
	if err != nil {
		return 1
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// API is the control protocol.
type API struct {
	Token    string
	Solver   *Solver
	SelfTest *SelfTest
	Log      *slog.Logger

	mu     sync.Mutex
	solves int
	last   *LastSolve
}

// LastSolve describes the most recent solve.
type LastSolve struct {
	At        time.Time `json:"at"`
	Host      string    `json:"host"`
	Outcome   string    `json:"outcome"`
	ElapsedMs int64     `json:"elapsedMs"`
	Message   string    `json:"message,omitempty"`
}

// Status is the answer of GET /v1/status.
type Status struct {
	Version   string     `json:"version"`
	Browser   string     `json:"browser,omitempty"` // Chromium version, once started
	Running   bool       `json:"running"`
	Sandbox   bool       `json:"sandbox"`
	Tabs      int        `json:"tabs"`
	TabsInUse int        `json:"tabsInUse"`
	Restarts  int        `json:"restarts"`
	Solves    int        `json:"solves"`
	LastSolve *LastSolve `json:"lastSolve,omitempty"`
}

// Routes is the HTTP handler.
func (a *API) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("GET /v1/status", a.auth(a.status))
	mux.HandleFunc("POST /v1/solve", a.auth(a.solve))
	mux.HandleFunc("POST /v1/selftest", a.auth(a.selftest))
	return mux
}

func (a *API) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(a.Token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "wrong or missing token"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *API) status(w http.ResponseWriter, _ *http.Request) {
	ps := a.Solver.Pool.Status()
	a.mu.Lock()
	st := Status{Version: Version, Browser: ps.Version, Running: ps.Running, Sandbox: ps.Sandbox, Tabs: ps.Tabs,
		TabsInUse: ps.InUse, Restarts: ps.Restarts, Solves: a.solves, LastSolve: a.last}
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, st)
}

func (a *API) record(target string, res SolveResult) {
	host := target
	if u, err := url.Parse(target); err == nil {
		host = u.Host
	}
	a.mu.Lock()
	a.solves++
	a.last = &LastSolve{At: time.Now().UTC(), Host: host, Outcome: res.Outcome, ElapsedMs: res.ElapsedMs, Message: res.Message}
	a.mu.Unlock()
	a.Log.Info("solve", "host", host, "outcome", res.Outcome, "challenged", res.Challenged, "ms", res.ElapsedMs, "message", res.Message)
}

func (a *API) solve(w http.ResponseWriter, r *http.Request) {
	var req SolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
		return
	}
	res := a.Solver.Solve(r.Context(), req, nil)
	a.record(req.URL, res)
	writeJSON(w, http.StatusOK, res)
}

// SelfTestResult is the answer of POST /v1/selftest.
type SelfTestResult struct {
	Passed bool        `json:"passed"`
	Result SolveResult `json:"result"`
}

func (a *API) selftest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Variant string `json:"variant"` // "" or "human"
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	variant := ""
	if req.Variant == "human" {
		variant = "human"
	}
	target := a.SelfTest.URL(variant)
	pol := Policy{Target: "127.0.0.1", selfTest: a.SelfTest.AddrPort()}
	res := a.Solver.Solve(r.Context(), SolveRequest{URL: target, TimeoutMs: 40000}, &pol)
	res.Body = trimBody(res.Body)
	a.record(target, res)
	passed := res.Outcome == OutcomeSolved && strings.Contains(res.Body, `id="result">passed`)
	if variant == "human" {
		passed = res.Outcome == OutcomeHuman
	}
	if !passed && res.Message == "" {
		res.Message = fmt.Sprintf("the self-test ended with %q", res.Outcome)
	}
	writeJSON(w, http.StatusOK, SelfTestResult{Passed: passed, Result: res})
}

func trimBody(s string) string {
	if len(s) > 4096 {
		return s[:4096]
	}
	return s
}
