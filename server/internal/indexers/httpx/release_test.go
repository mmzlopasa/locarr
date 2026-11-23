package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const torrentBody = "d8:announce3:x/y4:infod4:name1:a6:lengthi1e12:piece lengthi1e6:pieces0:ee"

// A Prowlarr-like indexer manager answers /download with redirects.
func TestFetchReleaseRedirects(t *testing.T) {
	var tracker struct {
		hits                int
		query, auth, apiKey string
		cookie              bool
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tracker.hits++
		tracker.query, tracker.auth, tracker.apiKey = r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("X-Api-Key")
		_, tracker.cookie = r.Header["Cookie"]
		if r.URL.Path == "/page" {
			_, _ = w.Write([]byte("<!doctype html><html>login</html>"))
			return
		}
		_, _ = w.Write([]byte(torrentBody))
	}))
	defer other.Close()
	otherURL := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "manager-key-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/12/download":
			http.Redirect(w, r, "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Film", http.StatusMovedPermanently)
		case "/13/download":
			http.Redirect(w, r, otherURL+"/get/film.torrent?apikey=manager-key-123&api_key=x&passkey=trk&id=9", http.StatusFound)
		case "/14/download":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
		case "/15/download":
			http.Redirect(w, r, r.URL.String(), http.StatusFound)
		case "/16/download":
			http.Redirect(w, r, "ftp://x.example/f.torrent", http.StatusFound)
		default:
			_, _ = w.Write([]byte(torrentBody))
		}
	}))
	defer srv.Close()
	c, _ := New(Options{Policy: Policy{Allowed: loopback.Allowed, AllowedHosts: []string{"localhost"}}, Secrets: []string{"manager-key-123"}})
	ctx := context.Background()
	hdr := http.Header{"X-Api-Key": {"manager-key-123"}, "Authorization": {"Bearer t"}, "Cookie": {"s=1"}}
	q := "?apikey=manager-key-123&file=Film&link=QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo"

	rf, err := c.FetchRelease(ctx, srv.URL+"/12/download"+q, hdr)
	if err != nil || !strings.HasPrefix(rf.Magnet, "magnet:?xt=urn:btih:0123") || rf.Response != nil {
		t.Fatalf("magnet: %+v %v", rf, err)
	}

	rf, err = c.FetchRelease(ctx, srv.URL+"/13/download"+q, hdr)
	if err != nil || string(rf.Body) != torrentBody || !rf.CrossHost || ReleaseKind(rf.Body) != "torrent" {
		t.Fatalf("cross host: %+v %v", rf, err)
	}
	if tracker.hits != 1 || strings.Contains(tracker.query, "manager-key-123") || strings.Contains(tracker.query, "api_key") ||
		!strings.Contains(tracker.query, "passkey=trk") || tracker.auth != "" || tracker.apiKey != "" || tracker.cookie {
		t.Fatalf("credentials reached the other host: %+v", tracker)
	}

	_, err = c.FetchRelease(ctx, srv.URL+"/14/download"+q, hdr)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("metadata address reached: %v", err)
	}
	_, err = c.FetchRelease(ctx, srv.URL+"/15/download"+q, hdr)
	if !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("loop: %v", err)
	}
	_, err = c.FetchRelease(ctx, srv.URL+"/16/download"+q, hdr)
	if !errors.Is(err, ErrBadRedirect) {
		t.Fatalf("ftp redirect: %v", err)
	}
	// Errors carry no query string, no key and no encoded link.
	for _, s := range []string{"manager-key-123", "link=", "file=", "?"} {
		if strings.Contains(err.Error(), s) {
			t.Fatalf("error leaks %q: %v", s, err)
		}
	}
	// Everything else still refuses other hosts.
	if _, err := c.Get(ctx, srv.URL+"/13/download"+q, nil); !errors.Is(err, ErrCrossHostRedirect) {
		t.Fatalf("plain request followed a cross-host redirect: %v", err)
	}
	rf, err = c.FetchRelease(ctx, otherURL+"/page", nil)
	if err != nil || ReleaseKind(rf.Body) != "" {
		t.Fatalf("html page: %v %q", err, ReleaseKind(rf.Body))
	}
}

func TestReleaseKindAndShortURL(t *testing.T) {
	for body, want := range map[string]string{torrentBody: "torrent", "  magnet:?xt=urn:btih:abc": "magnet",
		`<?xml version="1.0"?><nzb xmlns="x"></nzb>`: "nzb", "<html></html>": "", "": "", "de": "torrent", "dog": ""} {
		if got := ReleaseKind([]byte(body)); got != want {
			t.Errorf("%q: %q, want %q", body, got, want)
		}
	}
	got := ShortURL("https://pro.example.net/12/download?apikey=k&file=Film&link=AAAA#x", "secretpath")
	if got != "https://pro.example.net/12/download" {
		t.Fatal(got)
	}
	if got := ShortURL("https://t.example/dl/secretpath/f.torrent?x=1", "secretpath"); got != "https://t.example/dl/redacted/f.torrent" {
		t.Fatal(got)
	}
}
