package znab

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseCaps(t *testing.T) {
	c, err := ParseCaps(read(t, "caps-full.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerTitle != "Example & Co" || c.LimitMax != 100 || c.LimitDefault != 50 {
		t.Fatalf("%+v", c)
	}
	movie := c.Search(SearchMovie)
	if !movie.Available || !movie.Supports("imdbid") || !movie.Supports("tmdbid") || !movie.Supports("q") {
		t.Fatalf("movie %+v", movie)
	}
	if tv := c.Search(SearchTV); !tv.Supports("tvmazeid") || !tv.Supports("ep") {
		t.Fatalf("tv %+v", tv)
	}
	if c.Search(SearchMusic).Available {
		t.Fatal("audio-search is not available")
	}
	if b := c.Search(SearchBook); !b.Available || !b.Supports("q") {
		t.Fatalf("book without params defaults to q: %+v", b)
	}
	if len(c.Categories) != 3 || len(c.Categories[0].Subcat) != 1 || c.Categories[0].Subcat[0].ID != 2040 {
		t.Fatalf("categories %+v", c.Categories)
	}
	if ids := c.CategoryIDs(); len(ids) != 5 {
		t.Fatalf("ids %v", ids)
	}
	if len(c.Tags) != 1 || c.Tags[0] != "freeleech" {
		t.Fatal(c.Tags)
	}

	m, err := ParseCaps(read(t, "caps-minimal.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if s := m.Search(SearchGeneric); !s.Available || !s.Supports("q") {
		t.Fatalf("basic search is assumed: %+v", s)
	}
}

func TestErrors(t *testing.T) {
	_, err := ParseCaps(read(t, "error-auth.xml"))
	var ze *Error
	if !errors.As(err, &ze) || ze.Kind != KindAuth || ze.Code != 100 || ze.Message != "Incorrect user credentials" {
		t.Fatalf("%v", err)
	}
	cases := map[string]Kind{
		`<error code="101" description="Account suspended"/>`:     KindDisabled,
		`<error code="910" description="API disabled"/>`:          KindDisabled,
		`<error code="203" description="Function not available"/>`: KindUnsupported,
		`<error code="500" description="Request limit reached"/>`: KindRateLimit,
		`<error code="501" description="Download limit reached"/>`: KindRateLimit,
		`<error code="300" description="No such item"/>`:          KindNotFound,
		`<?xml version="1.0"?>` + "\n" + `<error code="900">Unknown</error>`: KindUnknown,
	}
	for body, want := range cases {
		if _, err := ParseFeed([]byte(body)); KindOf(err) != want {
			t.Errorf("%s: kind %s, want %s (%v)", body, KindOf(err), want, err)
		}
	}
	// A title mentioning <error is not an error document.
	if _, err := ParseFeed([]byte(`<rss><channel><item><title>&lt;error test</title><link>http://x/1</link></item></channel></rss>`)); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFeed([]byte("<html><body>Bad gateway")); KindOf(err) != KindBadResponse && err != nil {
		t.Fatalf("%v", err)
	}
	for status, want := range map[int]Kind{401: KindAuth, 403: KindAuth, 429: KindRateLimit, 503: KindUnreachable, 404: KindBadResponse} {
		if k := httpError(status, "").Kind; k != want {
			t.Errorf("HTTP %d: %s", status, k)
		}
	}
	if e := httpError(429, "30"); e.RetryAfter != 30 {
		t.Fatal(e.RetryAfter)
	}
}

func TestParseNewznabFeed(t *testing.T) {
	p, err := ParseFeed(read(t, "newznab.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 2 || len(p.Items) != 2 {
		t.Fatalf("total %d items %d", p.Total, len(p.Items))
	}
	a := p.Items[0]
	if a.Size != 987654321 || *a.Grabs != 17 || a.TVDBID != 12345 || a.IMDBID != "tt0944947" {
		t.Fatalf("%+v", a)
	}
	if a.Link != "https://nz.example/getnzb/abc.nzb&i=1&r=SECRETKEY" || a.InfoURL != "https://nz.example/details/abc#comments" {
		t.Fatalf("link %q info %q", a.Link, a.InfoURL)
	}
	if len(a.Categories) != 2 || a.Categories[0] != 5000 || a.Categories[1] != 5040 {
		t.Fatal(a.Categories)
	}
	if !a.PublishDate.Equal(time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)) {
		t.Fatal(a.PublishDate)
	}
	if a.GUID != "https://nz.example/details/abc" || a.Season != "S02" || a.Episode != "E05" {
		t.Fatalf("%+v", a)
	}
	b := p.Items[1]
	if b.Title != "Café Movie 2020 1080p" || b.Size != 0 || b.GUID != b.Link {
		t.Fatalf("%+v", b)
	}
	if !b.PublishDate.Equal(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("falls back to usenetdate: %v", b.PublishDate)
	}
}

func TestParseQuirkyTorznabFeed(t *testing.T) {
	p, err := ParseFeed(read(t, "torznab-quirky.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 {
		t.Fatal(len(p.Items))
	}
	a := p.Items[0]
	if a.Title != "Movie & Friends (2019) – 2160p" {
		t.Fatalf("title %q", a.Title)
	}
	if a.Link != "" || !strings.HasPrefix(a.MagnetURL, "magnet:") || a.InfoHash != "abcdefabcdefabcdefabcdefabcdefabcdefabcd" || a.GUID != a.InfoHash {
		t.Fatalf("%+v", a)
	}
	if *a.Seeders != 12 || *a.Peers != 20 || *a.Leechers != 8 || *a.DownloadVolumeFactor != 0 || *a.UploadVolumeFactor != 2 {
		t.Fatalf("%+v", a)
	}
	if len(a.Tags) != 1 || a.Tags[0] != "internal" {
		t.Fatal(a.Tags)
	}
	if !a.PublishDate.Equal(time.Date(2026, 11, 3, 8, 5, 1, 0, time.UTC)) {
		t.Fatal(a.PublishDate)
	}
	b := p.Items[1]
	if b.Title != "Other Release" || b.Size != 1024 || b.Seeders != nil || b.MinimumSeedTime == nil || *b.MinimumSeedTime != time.Hour {
		t.Fatalf("%+v", b)
	}
	if b.IMDBID != "tt0012345" || b.InfoHash != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("%+v", b)
	}
}

func TestParseDate(t *testing.T) {
	want := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	for _, s := range []string{
		"Fri, 02 Jan 2026 15:04:05 +0000", "Fri, 2 Jan 2026 15:04:05 GMT", "Fri, 02 Jan 2026 16:04:05 +01:00",
		"02 Jan 2026 15:04:05 +0000", "2026-01-02T15:04:05Z", "2026-01-02T17:04:05+02:00", "2026-01-02 15:04:05",
		"Fri, 02 Jan 2026 10:04:05 EST", "Mon, 02 Jan 2026 15:04:05 +0000", "  Fri,  02 Jan 2026   15:04:05 UT ",
	} {
		if got := ParseDate(s); !got.Equal(want) {
			t.Errorf("%q: %v", s, got)
		}
	}
	for _, s := range []string{"", "not a date", "yesterday"} {
		if !ParseDate(s).IsZero() {
			t.Errorf("%q parsed", s)
		}
	}
}

func TestRequestValues(t *testing.T) {
	v := Request{Type: SearchTV, Query: " show ", Categories: []int{5000, 5040}, IMDBID: "tt0944947", TVDBID: 5, Season: "1",
		Episode: "2", Offset: 100, Limit: 50}.Values()
	want := map[string]string{"t": "tvsearch", "q": "show", "cat": "5000,5040", "imdbid": "0944947", "tvdbid": "5", "season": "1",
		"ep": "2", "offset": "100", "limit": "50", "extended": "1"}
	for k, w := range want {
		if v.Get(k) != w {
			t.Errorf("%s=%q want %q", k, v.Get(k), w)
		}
	}
	if v.Has("tmdbid") || v.Has("apikey") {
		t.Fatal(v)
	}
}

func TestClientKeepsKeyOutOfErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "supersecretkey" {
			t.Errorf("key not sent: %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	hc, err := httpx.New(httpx.Options{Policy: httpx.Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}},
		Secrets: []string{"supersecretkey"}})
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{HTTP: hc, BaseURL: srv.URL, APIKey: "supersecretkey"}
	_, err = c.Caps(context.Background())
	if KindOf(err) != KindUnreachable {
		t.Fatal(err)
	}
	// An unreachable host: the transport error names the URL; the key must not appear.
	c.BaseURL = "http://127.0.0.1:1"
	_, err = c.Search(context.Background(), Request{Query: "x"})
	if err == nil || strings.Contains(err.Error(), "supersecretkey") || KindOf(err) != KindUnreachable {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(err.Error(), "apikey=redacted") {
		t.Fatalf("expected redacted URL: %v", err)
	}
}
