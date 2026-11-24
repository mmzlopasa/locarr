//go:build browser

// Package browsertest runs Locarr's indexer client against the real
// companion browser image and local fake sites behind a browser check. It
// needs the browser running and able to reach this test process:
//
//	./scripts/test-browser.sh
//
// LOCARR_TEST_BROWSER_URL and LOCARR_TEST_BROWSER_TOKEN reach the browser;
// LOCARR_TEST_SITE_HOST is the name the browser reaches this process by.
package browsertest

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/challengefake"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/indexers/native"
	"github.com/mmzlopasa/locarr/server/internal/indexers/native/sitefake"
	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
)

func env(t *testing.T, k string) string {
	v := os.Getenv(k)
	if v == "" {
		t.Skipf("%s is not set (run scripts/test-browser.sh)", k)
	}
	return v
}

type fixture struct {
	base    string
	gate    *challengefake.Gate
	browser *httpx.Browser
}

func setup(t *testing.T) *fixture {
	bu, tok, host := env(t, "LOCARR_TEST_BROWSER_URL"), env(t, "LOCARR_TEST_BROWSER_TOKEN"), env(t, "LOCARR_TEST_SITE_HOST")
	gate := challengefake.New(http.StripPrefix("/sites", sitefake.New()))
	mux := http.NewServeMux()
	mux.Handle("/sites/", gate)
	mux.Handle("/test", challengefake.New(challengefake.TestPage()))
	mux.Handle("/human/", challengefake.Human())
	// A page that sends the browser on to the cloud metadata address.
	mux.HandleFunc("/leak", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	})
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	port := ln.Addr().(*net.TCPAddr).Port
	b := &httpx.Browser{Config: func(context.Context) (httpx.BrowserConfig, error) {
		return httpx.BrowserConfig{URL: bu, Token: tok}, nil
	}}
	return &fixture{base: "http://" + host + ":" + strconv.Itoa(port), gate: gate, browser: b}
}

func TestSelfTest(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	r, err := f.browser.SelfTest(ctx, "")
	if err != nil || !r.Passed || !r.Challenged || r.Cookies == 0 {
		t.Fatalf("self-test: %+v %v", r, err)
	}
	r, err = f.browser.SelfTest(ctx, "human")
	if err != nil || !r.Passed || r.Outcome != "human" {
		t.Fatalf("human self-test: %+v %v", r, err)
	}
	st := f.browser.Status(ctx)
	if st.State != "connected" || st.Chromium == "" || st.Tabs < 1 || st.LastSolve == nil {
		t.Fatalf("status %+v", st)
	}
	t.Logf("browser %s, %s, sandbox %v", st.Version, st.Chromium, st.Sandbox)
}

// A 1337x-style site behind a JavaScript check: search and grab work through
// the browser, and later requests reuse the cookies without it.
func TestProtectedSiteSearch(t *testing.T) {
	f := setup(t)
	defs, err := native.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	var def *native.Definition
	for _, d := range defs {
		if d.ID == "1337x" {
			def = d
		}
	}
	hc, err := httpx.New(httpx.Options{Timeout: 90 * time.Second, Browser: f.browser})
	if err != nil {
		t.Fatal(err)
	}
	hc.SetChallenge(def.Challenge)
	c := &native.Client{Def: def, HTTP: hc, BaseURL: f.base + "/sites/1337x", Trackers: native.DefaultTrackers}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	page, err := c.Search(ctx, znab.Request{Query: "ubuntu"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 4 {
		t.Fatalf("%d results", len(page.Items))
	}
	checks := f.gate.Checks.Load()
	if checks == 0 {
		t.Fatal("the site's check was never shown")
	}
	// The grab (details page) and another search reuse the cookies.
	res, err := c.Fetch(ctx, page.Items[0].Link)
	if err != nil || !strings.HasPrefix(res.Magnet, "magnet:") {
		t.Fatalf("grab: %+v %v", res, err)
	}
	if _, err := c.Search(ctx, znab.Request{Query: "nothing"}); err != nil {
		t.Fatal(err)
	}
	if f.gate.Checks.Load() != checks {
		t.Fatalf("the check came back: %d → %d", checks, f.gate.Checks.Load())
	}
	if f.gate.Passed.Load() < 3 {
		t.Fatalf("passed %d", f.gate.Passed.Load())
	}
}

func TestHumanCheckReported(t *testing.T) {
	f := setup(t)
	hc, _ := httpx.New(httpx.Options{Timeout: 90 * time.Second, Browser: f.browser})
	_, err := hc.Get(context.Background(), f.base+"/human/search", nil)
	var ce *httpx.ChallengeError
	if !errors.As(err, &ce) || ce.Reason != httpx.ReasonHuman || !strings.Contains(ce.Message, "open it in your browser") {
		t.Fatalf("got %v", err)
	}
}

// Locarr checks the first address; the browser's own guard refuses where the
// page sends it next (here a redirect to the cloud metadata address).
func TestBrowserRefusesMetadata(t *testing.T) {
	f := setup(t)
	r, err := f.browser.TestURL(context.Background(), httpx.Policy{}, f.base+"/leak")
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome != "refused" || !strings.Contains(r.Message, "169.254.169.254") {
		t.Fatalf("got %+v", r)
	}
	if _, err := f.browser.TestURL(context.Background(), httpx.Policy{}, "http://169.254.169.254/"); !errors.Is(err, httpx.ErrBlocked) {
		t.Fatalf("Locarr must refuse before asking the browser: %v", err)
	}
}
