// Package challengefake is a fake "browser check" in front of a site, for the
// tests of the built-in browser and the end-to-end fakes (locarr-fakes serves
// it under /protected). Without a valid cookie a visitor gets a "Just a
// moment..." page whose script works for a moment, sets a cookie bound to the
// visitor's User-Agent and reloads; with the cookie it gets the site. Like
// real checks, a request with the cookie but another User-Agent gets the
// check again. The human variant shows a CAPTCHA box nobody can pass.
package challengefake

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"sync/atomic"
)

// Cookie is the cookie the check sets.
const Cookie = "locarr_check"

// Gate puts the check in front of next.
type Gate struct {
	Next http.Handler
	Key  []byte
	// Checks counts check pages served; Passed counts requests let through.
	Checks atomic.Int64
	Passed atomic.Int64
}

// New returns a Gate with a fixed key.
func New(next http.Handler) *Gate { return &Gate{Next: next, Key: []byte("locarr-fake-check")} }

// Token is the cookie value for a User-Agent.
func (g *Gate) Token(ua string) string {
	m := hmac.New(sha256.New, g.Key)
	m.Write([]byte(ua))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

func (g *Gate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	want := g.Token(r.UserAgent())
	if c, err := r.Cookie(Cookie); err == nil && hmac.Equal([]byte(c.Value), []byte(want)) {
		g.Passed.Add(1)
		g.Next.ServeHTTP(w, r)
		return
	}
	g.Checks.Add(1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, CheckPage(want)) // #nosec G705 -- a hex token in a fixed test page
}

// CheckPage is the JavaScript check page.
func CheckPage(value string) string {
	return `<!doctype html><html><head><title>Just a moment...</title></head><body>
<noscript>Enable JavaScript and cookies to continue</noscript>
<p>Checking your browser before accessing the site. This process is automatic.</p>
<script>
(function () {
  var n = 0; for (var i = 0; i < 2000000; i++) { n = (n + i * 7) % 1000003; }
  setTimeout(function () {
    document.cookie = "` + Cookie + `=` + value + `; path=/; max-age=3600; SameSite=Lax";
    location.reload();
  }, 800 + (n % 7));
})();
</script></body></html>`
}

// Human serves a check that wants a person: a CAPTCHA box that never passes.
func Human() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, HumanPage)
	})
}

// HumanPage is the CAPTCHA page.
const HumanPage = `<!doctype html><html><head><title>Just a moment...</title></head><body>
<noscript>Enable JavaScript and cookies to continue</noscript>
<p>Verify you are human by completing the action below.</p>
<div class="h-captcha" data-sitekey="00000000-0000-0000-0000-000000000000"></div>
</body></html>`

// TestPage is the page behind the check for the browser test.
func TestPage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head><title>Protected test page</title></head><body><p id="result">passed</p></body></html>`)
	})
}
