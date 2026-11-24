package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/challengefake"
	"github.com/mmzlopasa/locarr/server/internal/indexers/native/sitefake"
)

// TestBrowserForProtectedSites runs the built-in browser settings, status,
// test button, a protected 1337x-style site and the health explanations
// against a fake browser that speaks the control protocol.
func TestBrowserForProtectedSites(t *testing.T) {
	e := newEnv(t, noLimits)
	e.setup()
	gate := challengefake.New(http.StripPrefix("/sites", sitefake.New()))
	mux := http.NewServeMux()
	mux.Handle("/sites/", gate)
	mux.Handle("/human/", challengefake.Human())
	site := httptest.NewServer(mux)
	t.Cleanup(site.Close)

	const ua = "Mozilla/5.0 FakeBrowser"
	var solves atomic.Int64
	browser := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer browser-token-123456" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/status":
			_, _ = io.WriteString(w, `{"version":"1.0","browser":"Chrome/156","running":true,"sandbox":false,"tabs":2,"tabsInUse":0,"solves":3,"restarts":0,
				"lastSolve":{"at":"2026-11-24T10:00:00Z","host":"x","outcome":"solved","elapsedMs":2000}}`)
		case "/v1/selftest":
			_, _ = io.WriteString(w, `{"passed":true,"result":{"outcome":"solved","title":"Locarr browser self-test","challenged":true,"cookies":[{"name":"c","value":"v"}],"elapsedMs":2300}}`)
		case "/v1/solve":
			solves.Add(1)
			var req struct{ URL string }
			_ = json.NewDecoder(r.Body).Decode(&req)
			if strings.Contains(req.URL, "/human/") {
				_, _ = io.WriteString(w, `{"outcome":"human","cookies":[],"challenged":true}`)
				return
			}
			tok := gate.Token(ua)
			hreq, _ := http.NewRequest(http.MethodGet, req.URL, nil)
			hreq.Header.Set("User-Agent", ua)
			hreq.AddCookie(&http.Cookie{Name: challengefake.Cookie, Value: tok})
			res, err := http.DefaultClient.Do(hreq)
			if err != nil {
				t.Error(err)
				return
			}
			body, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			_ = json.NewEncoder(w).Encode(map[string]any{"outcome": "solved", "status": res.StatusCode, "body": string(body),
				"contentType": "text/html", "userAgent": ua, "challenged": true,
				"cookies": []map[string]any{{"name": challengefake.Cookie, "value": tok, "expires": time.Now().Add(time.Hour).Unix()}}})
		}
	}))
	t.Cleanup(browser.Close)

	type settingsT struct {
		BrowserUrl, BrowserDefaultUrl string
		BrowserToken                  struct{ Configured bool }
		BrowserTokenFromEnv           bool
	}
	var st settingsT
	e.expect(e.do("GET", "/api/v1/indexers/settings", nil), 200).json(&st)
	if st.BrowserUrl != "" || st.BrowserDefaultUrl != "http://locarr-browser:8490" || st.BrowserToken.Configured {
		t.Fatalf("%+v", st)
	}
	// Before it is set up the browser is "missing" with the compose command.
	var bs struct {
		State, Command, Url, Chromium string
		Tabs                          *int
		LastSolve                     *struct{ Outcome string }
	}
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"allowedHosts": []string{"127.0.0.1"},
		"browserUrl": "http://127.0.0.1:1"}), 200)
	e.expect(e.do("GET", "/api/v1/indexers/browser", nil), 200).json(&bs)
	if bs.State != "missing" || bs.Command != "docker compose --profile browser up -d" {
		t.Fatalf("%+v", bs)
	}
	// A bad address is refused.
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"browserUrl": "file:///etc"}), 400)

	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"browserUrl": browser.URL, "browserToken": "browser-token-123456"}), 200).json(&st)
	if st.BrowserUrl != browser.URL || !st.BrowserToken.Configured {
		t.Fatalf("%+v", st)
	}
	e.expect(e.do("GET", "/api/v1/indexers/browser", nil), 200).json(&bs)
	if bs.State != "connected" || bs.Chromium != "Chrome/156" || bs.Tabs == nil || *bs.Tabs != 2 || bs.LastSolve == nil {
		t.Fatalf("%+v", bs)
	}
	var tr struct {
		Passed, Challenged bool
		Outcome            string
		Cookies            int
	}
	e.expect(e.do("POST", "/api/v1/indexers/browser/test", map[string]any{}), 200).json(&tr)
	if !tr.Passed || tr.Outcome != "solved" || tr.Cookies != 1 {
		t.Fatalf("%+v", tr)
	}

	// A 1337x-style site behind a check: search through the browser, then
	// with its cookies.
	var x struct{ ID int64 }
	e.expect(e.do("POST", "/api/v1/indexers", map[string]any{"kind": "native", "definition": "1337x", "name": "Protected 1337x",
		"baseUrl": site.URL + "/sites/1337x", "enableRss": false, "enableAutomaticSearch": true, "enableInteractiveSearch": true}), 201).json(&x)
	var sr struct {
		Releases []struct{ Title string }
		Indexers []struct{ IndexerName, Status, ErrorCode string }
	}
	e.expect(e.do("GET", "/api/v1/search?q=ubuntu&indexerIds="+strconv.FormatInt(x.ID, 10), nil), 200).json(&sr)
	if len(sr.Releases) != 4 || solves.Load() != 1 {
		t.Fatalf("solves %d: %+v", solves.Load(), sr)
	}
	sr.Releases = nil
	e.expect(e.do("GET", "/api/v1/search?q=nothing+else&indexerIds="+strconv.FormatInt(x.ID, 10), nil), 200).json(&sr)
	if solves.Load() != 1 || gate.Passed.Load() < 2 {
		t.Fatalf("cookies not reused: solves %d passed %d", solves.Load(), gate.Passed.Load())
	}

	// A site that wants a person: reported, and the health check says why.
	var h struct{ ID int64 }
	e.expect(e.do("POST", "/api/v1/indexers", map[string]any{"kind": "native", "definition": "limetorrents", "name": "Human site",
		"baseUrl": site.URL + "/human/lime", "enableRss": false, "enableAutomaticSearch": true, "enableInteractiveSearch": true}), 201).json(&h)
	sr.Indexers = nil
	e.expect(e.do("GET", "/api/v1/search?q=ubuntu&indexerIds="+strconv.FormatInt(h.ID, 10), nil), 200).json(&sr)
	if len(sr.Indexers) != 1 || sr.Indexers[0].ErrorCode != "challenge" {
		t.Fatalf("%+v", sr.Indexers)
	}
	var rep struct {
		Issues []struct {
			Code, Docs string
			Params     map[string]any
		}
	}
	e.expect(e.do("POST", "/api/v1/system/health/run", nil), 200).json(&rep)
	found := false
	for _, is := range rep.Issues {
		if is.Code == "indexer_challenge" {
			found = is.Params["reason"] == "human" && is.Docs == "indexer-challenge"
		}
	}
	if !found {
		t.Fatalf("health: %+v", rep.Issues)
	}
}
