package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/challengefake"
)

const fakeUA = "Mozilla/5.0 (X11; Linux x86_64) FakeBrowser/1.0"

// fakeBrowser answers the control protocol like the companion browser: it
// "runs" the check by computing the cookie the fake site expects for its
// User-Agent and fetches the page with it.
type fakeBrowser struct {
	gate   *challengefake.Gate
	solves atomic.Int64
	srv    *httptest.Server
	last   SolveRequest
}

func newFakeBrowser(t *testing.T, gate *challengefake.Gate) *fakeBrowser {
	fb := &fakeBrowser{gate: gate}
	fb.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token-0123456789" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req SolveRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		fb.last = req
		fb.solves.Add(1)
		out := SolveResult{UserAgent: fakeUA, Cookies: []SolveCookie{}, Challenged: true}
		if strings.Contains(req.URL, "/human") {
			out.Outcome = "human"
		} else {
			tok := gate.Token(fakeUA)
			hreq, _ := http.NewRequest(http.MethodGet, req.URL, nil)
			hreq.Header.Set("User-Agent", fakeUA)
			hreq.Header.Set("Sec-Ch-Ua-Mobile", "?0")
			hreq.AddCookie(&http.Cookie{Name: challengefake.Cookie, Value: tok})
			res, err := http.DefaultClient.Do(hreq)
			if err != nil {
				t.Error(err)
				return
			}
			b, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			out.Outcome, out.Status, out.Body, out.ContentType = "solved", res.StatusCode, string(b), "text/html"
			out.Cookies = append(out.Cookies, SolveCookie{Name: challengefake.Cookie, Value: tok,
				Expires: float64(time.Now().Add(time.Hour).Unix())})
			out.Headers = map[string]string{"Sec-Ch-Ua-Mobile": "?0"}
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(fb.srv.Close)
	return fb
}

func (fb *fakeBrowser) browser() *Browser {
	return &Browser{ControlPolicy: loopback, Config: func(context.Context) (BrowserConfig, error) {
		return BrowserConfig{URL: fb.srv.URL, Token: "test-token-0123456789"}, nil
	}}
}

func protectedSite(t *testing.T) (*challengefake.Gate, *httptest.Server, *atomic.Int64) {
	var hits atomic.Int64
	gate := challengefake.New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Sec-Ch-Ua-Mobile") != "?0" && r.UserAgent() == fakeUA {
			t.Error("client hints of the browser were not sent with its cookies")
		}
		_, _ = io.WriteString(w, "<html><title>Results</title><table class=list><tr><td>row</td></tr></table></html>")
	}))
	site := httptest.NewServer(gate)
	t.Cleanup(site.Close)
	return gate, site, &hits
}

func TestBrowserSolvesAndReusesCookies(t *testing.T) {
	gate, site, _ := protectedSite(t)
	fb := newFakeBrowser(t, gate)
	b := fb.browser()
	c, _ := New(Options{Policy: loopback, Browser: b, Challenge: "sometimes"})
	ctx := context.Background()

	res, err := c.Get(ctx, site.URL+"/search/ubuntu/", nil)
	if err != nil || res.Status != 200 || !strings.Contains(string(res.Body), "class=list") {
		t.Fatalf("first search: %v %+v", err, res)
	}
	if fb.solves.Load() != 1 || gate.Checks.Load() != 1 {
		t.Fatalf("solves %d, checks %d", fb.solves.Load(), gate.Checks.Load())
	}
	if !fb.last.TargetPrivate || len(fb.last.Allowed) != 1 || fb.last.Allowed[0] != "127.0.0.0/8" {
		t.Fatalf("policy not passed to the browser: %+v", fb.last)
	}
	// The next requests use the cookies with the browser's User-Agent: no browser.
	for i := 0; i < 3; i++ {
		res, err = c.Get(ctx, site.URL+"/search/other/", nil)
		if err != nil || res.Status != 200 {
			t.Fatalf("reuse %d: %v %+v", i, err, res)
		}
	}
	if fb.solves.Load() != 1 || gate.Passed.Load() != 4 {
		t.Fatalf("cookies not reused: solves %d, passed %d", fb.solves.Load(), gate.Passed.Load())
	}
	// Another client of the same site (another indexer) shares the cookies.
	c2, _ := New(Options{Policy: loopback, Browser: b})
	if _, err := c2.Get(ctx, site.URL+"/", nil); err != nil || fb.solves.Load() != 1 {
		t.Fatalf("shared cookies: %v, solves %d", err, fb.solves.Load())
	}

	// The site no longer accepts the cookies (it ties them to the browser):
	// the check comes back, the browser answers, and later requests go
	// through it directly.
	gate.Key = []byte("rotated")
	if _, err := c.Get(ctx, site.URL+"/a/", nil); err != nil {
		t.Fatal(err)
	}
	checks := gate.Checks.Load()
	if _, err := c.Get(ctx, site.URL+"/b/", nil); err != nil {
		t.Fatal(err)
	}
	if !b.direct(hostOf(site.URL), "") || gate.Checks.Load() != checks || fb.solves.Load() != 3 {
		t.Fatalf("direct mode: %v checks %d→%d solves %d", b.direct(hostOf(site.URL), ""), checks, gate.Checks.Load(), fb.solves.Load())
	}
}

func TestBrowserAlwaysGoesFirst(t *testing.T) {
	gate, site, _ := protectedSite(t)
	fb := newFakeBrowser(t, gate)
	c, _ := New(Options{Policy: loopback, Browser: fb.browser(), Challenge: "always"})
	if _, err := c.Get(context.Background(), site.URL+"/", nil); err != nil {
		t.Fatal(err)
	}
	if gate.Checks.Load() != 0 || fb.solves.Load() != 1 {
		t.Fatalf("an always-protected site should go to the browser first: checks %d", gate.Checks.Load())
	}
}

func TestBrowserHumanCheckBacksOff(t *testing.T) {
	gate, _, _ := protectedSite(t)
	fb := newFakeBrowser(t, gate)
	human := httptest.NewServer(challengefake.Human())
	defer human.Close()
	b := fb.browser()
	c, _ := New(Options{Policy: loopback, Browser: b})
	_, err := c.Get(context.Background(), human.URL+"/human/search", nil)
	var ce *ChallengeError
	if !errors.As(err, &ce) || ce.Reason != ReasonHuman || !errors.Is(err, ErrChallenge) || !strings.Contains(ce.Message, "human check") {
		t.Fatalf("got %v", err)
	}
	// Backed off: the browser is not asked again.
	_, err = c.Get(context.Background(), human.URL+"/human/search", nil)
	if !errors.As(err, &ce) || ce.Reason != ReasonHuman || fb.solves.Load() != 1 {
		t.Fatalf("no backoff: %v, solves %d", err, fb.solves.Load())
	}
	if r, _, ok := b.HostIssue(hostOf(human.URL)); !ok || r != ReasonHuman {
		t.Fatal("host issue not recorded")
	}
}

func TestBrowserMissing(t *testing.T) {
	_, site, _ := protectedSite(t)
	dead := httptest.NewServer(http.NotFoundHandler())
	addr := dead.URL
	dead.Close()
	b := &Browser{ControlPolicy: loopback, Config: func(context.Context) (BrowserConfig, error) {
		return BrowserConfig{URL: addr, Token: "x"}, nil
	}}
	c, _ := New(Options{Policy: loopback, Browser: b})
	_, err := c.Get(context.Background(), site.URL+"/", nil)
	var ce *ChallengeError
	if !errors.As(err, &ce) || ce.Reason != ReasonMissing || !strings.Contains(ce.Message, BrowserCommand) {
		t.Fatalf("got %v", err)
	}
	if ok, known := b.Reachable(); ok || !known {
		t.Fatal("reachability not recorded")
	}
	if st := b.Status(context.Background()); st.State != "missing" {
		t.Fatalf("status %+v", st)
	}
}

func TestBrowserWrongToken(t *testing.T) {
	gate, site, _ := protectedSite(t)
	fb := newFakeBrowser(t, gate)
	b := &Browser{ControlPolicy: loopback, Config: func(context.Context) (BrowserConfig, error) {
		return BrowserConfig{URL: fb.srv.URL, Token: "wrong"}, nil
	}}
	c, _ := New(Options{Policy: loopback, Browser: b})
	_, err := c.Get(context.Background(), site.URL+"/", nil)
	var ce *ChallengeError
	if !errors.As(err, &ce) || ce.Reason != ReasonFailed || !strings.Contains(ce.Message, "token") {
		t.Fatalf("got %v", err)
	}
	if st := b.Status(context.Background()); st.State != "unauthorized" {
		t.Fatalf("status %+v", st)
	}
}

// Locarr checks the address before it asks the browser.
func TestBrowserTargetPolicy(t *testing.T) {
	gate, site, _ := protectedSite(t)
	fb := newFakeBrowser(t, gate)
	c, _ := New(Options{Policy: loopback, Browser: fb.browser(), Challenge: "always"})
	c.o.Policy = Policy{} // loopback not allowed for the site
	if _, err := c.Get(context.Background(), site.URL+"/", nil); !errors.Is(err, ErrBlocked) {
		t.Fatalf("got %v", err)
	}
	if fb.solves.Load() != 0 {
		t.Fatal("the browser was asked for a blocked address")
	}
}

func TestIsChallenge(t *testing.T) {
	h := func(kv ...string) http.Header {
		out := http.Header{}
		for i := 0; i+1 < len(kv); i += 2 {
			out.Set(kv[i], kv[i+1])
		}
		return out
	}
	big := strings.Repeat("x", 300<<10)
	for _, tc := range []struct {
		name string
		r    Response
		want bool
	}{
		{"cloudflare", Response{Status: 403, Header: h("Server", "cloudflare"), Body: []byte("<html><title>Just a moment...</title><script>window._cf_chl_opt={}</script>")}, true},
		{"cf-mitigated", Response{Status: 403, Header: h("Cf-Mitigated", "challenge"), Body: []byte("x")}, true},
		{"ddos-guard", Response{Status: 403, Header: h("Server", "ddos-guard"), Body: []byte("<script src='/.well-known/ddos-guard/js-challenge/index.js'></script>")}, true},
		{"generic 503 check", Response{Status: 503, Header: h(), Body: []byte(challengefake.CheckPage("abc"))}, true},
		{"waiting room 200", Response{Status: 200, Header: h(), Body: []byte("<title>Just a moment...</title><noscript>Enable JavaScript and cookies to continue</noscript>")}, true},
		{"cf 429 check", Response{Status: 429, Header: h("Server", "cloudflare"), Body: []byte("<title>Just a moment...</title>")}, true},
		{"plain 403", Response{Status: 403, Header: h("Server", "cloudflare"), Body: []byte("<title>403 Forbidden</title>")}, false},
		{"plain 429", Response{Status: 429, Header: h(), Body: []byte("slow down")}, false},
		{"content with bot script", Response{Status: 200, Header: h("Server", "cloudflare"), Body: []byte(`<title>1337x</title><script src="/cdn-cgi/challenge-platform/h/b/scripts/jsd/x/main.js"></script>`)}, false},
		{"big 403 page with marker", Response{Status: 403, Header: h(), Body: []byte("cf_chl_" + big)}, false},
		{"normal results", Response{Status: 200, Header: h(), Body: []byte("<title>ubuntu - search</title><table></table>")}, false},
	} {
		if got := IsChallenge(&tc.r); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}
