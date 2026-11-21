package httpx

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

var loopback = Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}

func TestPolicy(t *testing.T) {
	var p Policy
	for _, ip := range []string{"127.0.0.1", "::1", "169.254.169.254", "0.0.0.0", "fe80::1", "100.100.100.200", "fd00:ec2::254", "224.0.0.1"} {
		if err := p.Check(netip.MustParseAddr(ip)); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s allowed", ip)
		}
	}
	for _, ip := range []string{"10.1.2.3", "172.18.0.5", "192.168.1.10", "8.8.8.8", "2001:db8::1"} {
		if err := p.Check(netip.MustParseAddr(ip)); err != nil {
			t.Errorf("%s blocked: %v", ip, err)
		}
	}
	strict := Policy{BlockPrivate: true}
	if strict.Check(netip.MustParseAddr("192.168.1.10")) == nil || strict.Check(netip.MustParseAddr("8.8.8.8")) != nil {
		t.Fatal("block private")
	}
	if loopback.Check(netip.MustParseAddr("127.0.0.1")) != nil || loopback.Check(netip.MustParseAddr("::ffff:127.0.0.1")) != nil {
		t.Fatal("allowed override")
	}
	pre, hosts, err := ParseAllowed([]string{"127.0.0.1", "10.0.0.0/8", "Prowlarr", " "})
	if err != nil || len(pre) != 2 || len(hosts) != 1 || hosts[0] != "prowlarr" {
		t.Fatal(pre, hosts, err)
	}
	if _, _, err := ParseAllowed([]string{"bad host!"}); err == nil {
		t.Fatal("bad entry accepted")
	}
	for raw, ok := range map[string]bool{"http://prowlarr:9696": true, "https://x.example/api": true, "ftp://x": false,
		"http://user:pw@x": false, "http://127.0.0.1": false, "http://localhost:9117": false, "http://169.254.169.254": false,
		"http://192.168.1.5:9117": true, "notaurl": false} {
		if err := p.CheckURL(raw); (err == nil) != ok {
			t.Errorf("%s: %v", raw, err)
		}
	}
	if (Policy{AllowedHosts: []string{"localhost"}}).CheckURL("http://localhost:9117") != nil {
		t.Fatal("allowed host name")
	}
}

func TestDialRefusesBlockedAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer srv.Close()
	c, _ := New(Options{})
	_, err := c.Get(context.Background(), srv.URL+"/?apikey=topsecretvalue", nil)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("loopback reached: %v", err)
	}
	if strings.Contains(err.Error(), "topsecretvalue") {
		t.Fatal(err)
	}
	c, _ = New(Options{Policy: loopback})
	res, err := c.Get(context.Background(), srv.URL, nil)
	if err != nil || string(res.Body) != "ok" {
		t.Fatal(res, err)
	}
}

func TestRedirectsAndSize(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("elsewhere")) }))
	defer other.Close()
	otherURL := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/cross":
			http.Redirect(w, r, otherURL, http.StatusFound)
		case "/big":
			_, _ = w.Write(bytes.Repeat([]byte("x"), 2048))
		case "/gzip":
			var b bytes.Buffer
			zw := gzip.NewWriter(&b)
			_, _ = zw.Write([]byte("<rss/>"))
			_ = zw.Close()
			_, _ = w.Write(b.Bytes()) // no Content-Encoding header
		case "/bomb":
			var b bytes.Buffer
			zw := gzip.NewWriter(&b)
			_, _ = zw.Write(bytes.Repeat([]byte("a"), 1<<20))
			_ = zw.Close()
			_, _ = w.Write(b.Bytes())
		default:
			_, _ = w.Write([]byte("final"))
		}
	}))
	defer srv.Close()
	c, _ := New(Options{Policy: Policy{Allowed: loopback.Allowed, AllowedHosts: []string{"localhost"}}, MaxBytes: 1024})
	ctx := context.Background()
	if res, err := c.Get(ctx, srv.URL+"/same", nil); err != nil || string(res.Body) != "final" {
		t.Fatal(res, err)
	}
	if _, err := c.Get(ctx, srv.URL+"/cross", nil); !errors.Is(err, ErrCrossHostRedirect) {
		t.Fatalf("cross-host redirect followed: %v", err)
	}
	if _, err := c.Get(ctx, srv.URL+"/big", nil); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	if res, err := c.Get(ctx, srv.URL+"/gzip", nil); err != nil || string(res.Body) != "<rss/>" {
		t.Fatal(res, err)
	}
	if _, err := c.Get(ctx, srv.URL+"/bomb", nil); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("gzip bomb: %v", err)
	}
}

func TestRedact(t *testing.T) {
	got := Redact("https://user:pw@x.example/api?t=search&apikey=abc123&q=film&passkey=zzz", "film-secret")
	if strings.Contains(got, "abc123") || strings.Contains(got, "zzz") || strings.Contains(got, "pw@") || !strings.Contains(got, "q=film") {
		t.Fatal(got)
	}
	if s := Scrub("key is s3cr3t+/= and s3cr3t%2B%2F%3D", "s3cr3t+/="); strings.Contains(s, "s3cr3t") {
		t.Fatal(s)
	}
}

func TestFlareSolverr(t *testing.T) {
	var solved bool
	fs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		solved = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","solution":{"cookies":[{"name":"cf_clearance","value":"yes"}],"userAgent":"Browser/1"}}`))
	}))
	defer fs.Close()
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("cf_clearance"); err == nil && c.Value == "yes" && r.UserAgent() == "Browser/1" {
			_, _ = w.Write([]byte("<rss/>"))
			return
		}
		w.Header().Set("Server", "cloudflare")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html><title>Just a moment...</title></html>"))
	}))
	defer site.Close()
	c, _ := New(Options{Policy: loopback, FlareSolverr: fs.URL})
	res, err := c.Get(context.Background(), site.URL+"/api", nil)
	if err != nil || string(res.Body) != "<rss/>" || !solved {
		t.Fatal(res, err, solved)
	}
}
