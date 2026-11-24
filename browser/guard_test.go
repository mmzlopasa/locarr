package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func TestPolicy(t *testing.T) {
	p, err := parsePolicy("site.example", true, []string{"10.9.0.0/16", "nas.lan"})
	if err != nil {
		t.Fatal(err)
	}
	ap := func(s string) netip.AddrPort { return netip.AddrPortFrom(netip.MustParseAddr(s), 443) }
	for _, tc := range []struct {
		host, ip string
		ok       bool
	}{
		{"example.com", "93.184.216.34", true},
		{"example.com", "127.0.0.1", false},
		{"example.com", "169.254.169.254", false},
		{"example.com", "100.100.100.200", false},
		{"example.com", "0.0.0.0", false},
		{"example.com", "::1", false},
		{"example.com", "fe80::1", false},
		{"example.com", "192.168.1.1", false}, // private, not the target
		{"site.example", "192.168.1.1", true}, // the target may be private
		{"site.example", "127.0.0.1", false},  // but never loopback
		{"other", "10.9.3.4", true},           // allowed network
	} {
		if err := p.check(tc.host, ap(tc.ip)); (err == nil) != tc.ok {
			t.Errorf("%s at %s: err=%v, want ok=%v", tc.host, tc.ip, err, tc.ok)
		}
	}
	if !p.hostAllowed("NAS.lan.") {
		t.Error("allowed host names ignore case and the trailing dot")
	}
	if _, err := parsePolicy("x", false, []string{"bad host/name"}); err == nil {
		t.Error("an invalid entry must fail")
	}
}

// The guard refuses loopback through both CONNECT and plain forwarding, and
// forwards allowed requests.
func TestGuardRefusesLoopback(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "secret") }))
	defer site.Close()
	g, err := newGuard(Policy{Target: "example.com"}, "", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer g.close()
	pu, _ := url.Parse(g.addr())
	hc := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}}
	res, err := hc.Get(site.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusForbidden || strings.Contains(string(b), "secret") || res.Header.Get(guardHeader) == "" {
		t.Fatalf("plain request to loopback: %d %q", res.StatusCode, b)
	}
	c, err := net.Dial("tcp", pu.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", site.Listener.Addr(), site.Listener.Addr())
	r, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("CONNECT to loopback answered %d", r.StatusCode)
	}
	if g.refused.Load() != 2 {
		t.Fatalf("refused %d, want 2", g.refused.Load())
	}

	// Allowed explicitly: forwarded.
	g2, err := newGuard(Policy{Target: "x", Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}, "", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer g2.close()
	pu2, _ := url.Parse(g2.addr())
	hc2 := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu2)}}
	res, err = hc2.Get(site.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(res.Body)
	_ = res.Body.Close()
	if string(b) != "secret" {
		t.Fatalf("allowed request got %d %q", res.StatusCode, b)
	}
}

func TestAPIToken(t *testing.T) {
	a := &API{Token: "0123456789abcdef", Solver: &Solver{Pool: &Pool{Tabs: 1}}}
	srv := httptest.NewServer(a.Routes())
	defer srv.Close()
	for _, tc := range []struct {
		auth string
		want int
	}{{"", 401}, {"Bearer wrong", 401}, {"Bearer 0123456789abcdef", 200}} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/status", nil)
		if tc.auth != "" {
			req.Header.Set("Authorization", tc.auth)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != tc.want {
			t.Errorf("%q: %d, want %d", tc.auth, res.StatusCode, tc.want)
		}
	}
	res, err := http.Get(srv.URL + "/healthz")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("healthz: %v", err)
	}
	_ = res.Body.Close()
}

func TestCheckTarget(t *testing.T) {
	for _, bad := range []string{"file:///etc/passwd", "chrome://settings", "javascript:alert(1)", "http://user:pw@x/", "ftp://x/", "http:///"} {
		if _, err := CheckTarget(bad); err == nil {
			t.Errorf("%s must be refused", bad)
		}
	}
	if _, err := CheckTarget("https://1337x.to/search/x/1/"); err != nil {
		t.Error(err)
	}
}
