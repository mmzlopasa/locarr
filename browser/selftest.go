package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"html"
	"net"
	"net/http"
	"net/netip"
	"time"
)

// SelfTest is a tiny site on a loopback address inside the container that
// behaves like a browser check: the first visit gets a "Just a moment..."
// page whose script works for a moment, sets a cookie bound to the
// User-Agent and reloads; with the cookie the visit gets the content.
// /human shows a page that asks for a CAPTCHA and never lets anyone in.
type SelfTest struct {
	ln  net.Listener
	key []byte
}

// SelfTestCookie is the cookie the self-test page sets.
const SelfTestCookie = "locarr_check"

// StartSelfTest serves the self-test site on 127.0.0.1.
func StartSelfTest() (*SelfTest, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	st := &SelfTest{ln: ln, key: key}
	srv := &http.Server{Handler: st, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return st, nil
}

// URL is the address of a self-test page ("" or "human").
func (st *SelfTest) URL(variant string) string {
	return "http://" + st.ln.Addr().String() + "/" + variant
}

// AddrPort is where the site listens.
func (st *SelfTest) AddrPort() netip.AddrPort { return netip.MustParseAddrPort(st.ln.Addr().String()) }

func (st *SelfTest) token(ua string) string {
	m := hmac.New(sha256.New, st.key)
	m.Write([]byte(ua))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

func (st *SelfTest) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.URL.Path == "/human" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(HumanPage))
		return
	}
	want := st.token(r.UserAgent())
	if c, err := r.Cookie(SelfTestCookie); err == nil && hmac.Equal([]byte(c.Value), []byte(want)) {
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Locarr browser self-test</title></head><body><p id="result">passed</p></body></html>`))
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(CheckPage(SelfTestCookie, html.EscapeString(want))))
}

// CheckPage is a JavaScript check page: it spends a little CPU like real
// checks do, then sets the cookie and reloads.
func CheckPage(cookie, value string) string {
	return `<!doctype html><html><head><title>Just a moment...</title></head><body>
<noscript>Enable JavaScript and cookies to continue</noscript>
<p>Checking your browser before accessing the site. This process is automatic.</p>
<script>
(function () {
  var n = 0; for (var i = 0; i < 2000000; i++) { n = (n + i * 7) % 1000003; }
  setTimeout(function () {
    document.cookie = "` + cookie + `=` + value + `; path=/; max-age=3600; SameSite=Lax";
    location.reload();
  }, 800 + (n % 7));
})();
</script></body></html>`
}

// HumanPage is a check that wants a person: it shows a CAPTCHA box and
// nothing happens until someone solves it.
const HumanPage = `<!doctype html><html><head><title>Just a moment...</title></head><body>
<noscript>Enable JavaScript and cookies to continue</noscript>
<p>Verify you are human by completing the action below.</p>
<div class="h-captcha" data-sitekey="00000000-0000-0000-0000-000000000000"></div>
</body></html>`
