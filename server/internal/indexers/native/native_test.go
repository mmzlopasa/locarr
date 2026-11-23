package native_test

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/indexers/native"
	"github.com/mmzlopasa/locarr/server/internal/indexers/native/sitefake"
	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
)

var testNow = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

func localHTTP(t *testing.T) *httpx.Client {
	t.Helper()
	hc, err := httpx.New(httpx.Options{Policy: httpx.Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("::1/128")}}, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return hc
}

func registry(t *testing.T) *native.Registry {
	t.Helper()
	r, err := native.NewRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// siteClient returns a client for a built-in definition backed by the
// recorded answers.
func siteClient(t *testing.T, id string) (*native.Client, *sitefake.Handler) {
	t.Helper()
	fake := sitefake.New()
	srv := httptest.NewServer(http.StripPrefix("/sites", fake))
	t.Cleanup(srv.Close)
	def, ok := registry(t).Get(id)
	if !ok {
		t.Fatalf("no definition %s", id)
	}
	d := *def
	d.DelayMillis = 0
	c := &native.Client{Def: &d, HTTP: localHTTP(t), BaseURL: srv.URL + "/sites/" + id, Trackers: native.DefaultTrackers,
		Now: func() time.Time { return testNow }}
	return c, fake
}

func search(t *testing.T, c *native.Client, r znab.Request) []znab.Item {
	t.Helper()
	p, err := c.Search(context.Background(), r)
	if err != nil {
		t.Fatalf("search %+v: %v", r, err)
	}
	if p.Dropped != 0 {
		t.Errorf("%d rows dropped", p.Dropped)
	}
	return p.Items
}

// checkItems asserts what every built-in result must have.
func checkItems(t *testing.T, items []znab.Item, least int) {
	t.Helper()
	if len(items) < least {
		t.Fatalf("got %d items, want at least %d", len(items), least)
	}
	for _, it := range items {
		if it.Title == "" || it.GUID == "" {
			t.Errorf("item without title or guid: %+v", it)
		}
		if it.MagnetURL == "" && it.Link == "" {
			t.Errorf("%q: no way to download", it.Title)
		}
		if it.MagnetURL != "" && (!strings.HasPrefix(it.MagnetURL, "magnet:?xt=urn:btih:") || it.InfoHash == "") {
			t.Errorf("%q: bad magnet %q / hash %q", it.Title, it.MagnetURL, it.InfoHash)
		}
		if it.InfoHash != "" && len(it.InfoHash) != 40 {
			t.Errorf("%q: hash %q", it.Title, it.InfoHash)
		}
		if it.PublishDate.IsZero() {
			t.Errorf("%q: no date", it.Title)
		}
	}
}

func TestBuiltinsLoad(t *testing.T) {
	defs, err := native.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1337x", "bitsearch", "eztv", "knaben", "limetorrents", "nyaa", "subsplease", "thepiratebay", "tokyotosho", "yts"}
	if len(defs) != len(want) {
		t.Fatalf("%d built-ins", len(defs))
	}
	for _, d := range defs {
		if d.Verified == "" || d.RateLimit.Requests == 0 || d.Description == "" {
			t.Errorf("%s: verified, rate limit and description are required for built-ins", d.ID)
		}
	}
}

func TestYTS(t *testing.T) {
	c, _ := siteClient(t, "yts")
	items := search(t, c, znab.Request{Type: znab.SearchMovie, IMDBID: "0111161", Categories: []int{2000}})
	checkItems(t, items, 2)
	it := items[0]
	if !strings.HasPrefix(it.Title, "The Shawshank Redemption 1994 720p BluRay x264-YTS") || it.IMDBID != "tt0111161" ||
		it.Size != 890199081 || *it.Seeders != 100 || it.InfoHash != "ac418db33fa5cea4fab11bc58008fe08f291c9be" {
		t.Fatalf("%+v", it)
	}
	if !strings.Contains(it.MagnetURL, "tracker.opentrackr.org") || it.Link != "" {
		t.Fatalf("magnet %q link %q", it.MagnetURL, it.Link)
	}
	if items := search(t, c, znab.Request{Type: znab.SearchTV, Query: "anything"}); len(items) != 0 {
		t.Fatal("YTS answered a TV search")
	}
	checkItems(t, search(t, c, znab.Request{}), 1) // RSS: latest movies
	if items := search(t, c, znab.Request{Type: znab.SearchMovie, Query: "nothing like this"}); len(items) != 0 {
		t.Fatalf("%d", len(items))
	}
}

func TestEZTV(t *testing.T) {
	c, _ := siteClient(t, "eztv")
	items := search(t, c, znab.Request{Type: znab.SearchTV, IMDBID: "tt0944947", Season: "1", Episode: "10"})
	checkItems(t, items, 1)
	if len(items) != 1 || items[0].Season != "1" || items[0].Episode != "10" || !strings.Contains(items[0].Title, "S01E10") {
		t.Fatalf("%+v", items)
	}
	if _, err := c.Search(context.Background(), znab.Request{Type: znab.SearchTV, Query: "game of thrones"}); znab.KindOf(err) != znab.KindUnsupported {
		t.Fatalf("text search: %v", err)
	}
	checkItems(t, search(t, c, znab.Request{}), 3)
}

func TestThePirateBay(t *testing.T) {
	c, fake := siteClient(t, "thepiratebay")
	items := search(t, c, znab.Request{Type: znab.SearchMovie, IMDBID: "tt0111161"})
	checkItems(t, items, 4)
	if items[0].Title != "The Shawshank Redemption (1994) 1080p BrRip - 1.6GB - YIFY" || items[0].Categories[0] != 2040 ||
		items[0].IMDBID != "tt0111161" || !strings.HasPrefix(items[0].InfoURL, "https://thepiratebay.org/description.php?id=") {
		t.Fatalf("%+v", items[0])
	}
	checkItems(t, search(t, c, znab.Request{Type: znab.SearchGeneric, Query: "ubuntu"}), 4)
	checkItems(t, search(t, c, znab.Request{}), 1)
	if items := search(t, c, znab.Request{Query: "nothing at all"}); len(items) != 0 {
		t.Fatalf("placeholder row kept: %+v", items)
	}
	// Movie categories: book results of a keyword search are left out.
	for _, it := range search(t, c, znab.Request{Type: znab.SearchMovie, Query: "ubuntu", Categories: []int{2000}}) {
		if it.Categories[0]/1000 != 2 {
			t.Fatalf("category filter: %+v", it)
		}
	}
	if fake.Requests.Load() == 0 {
		t.Fatal("no request reached the fake")
	}
}

func TestSubsPlease(t *testing.T) {
	c, _ := siteClient(t, "subsplease")
	items := search(t, c, znab.Request{Type: znab.SearchTV, Query: "frieren"})
	checkItems(t, items, 3)
	if !strings.HasPrefix(items[0].Title, "[SubsPlease] ") || items[0].Size == 0 || items[0].Categories[0] != 5070 {
		t.Fatalf("%+v", items[0])
	}
	c.Config = map[string]string{"resolution": "1080"}
	for _, it := range search(t, c, znab.Request{Type: znab.SearchTV, Query: "frieren"}) {
		if !strings.Contains(it.Title, "1080p") {
			t.Fatalf("resolution filter: %s", it.Title)
		}
	}
	checkItems(t, search(t, c, znab.Request{}), 2)
	if items := search(t, c, znab.Request{Type: znab.SearchTV, Query: "unknown show"}); len(items) != 0 {
		t.Fatal(items)
	}
}

func TestKnaben(t *testing.T) {
	c, _ := siteClient(t, "knaben")
	items := search(t, c, znab.Request{Query: "ubuntu"})
	checkItems(t, items, 4)
	checkItems(t, search(t, c, znab.Request{}), 3)
	if items := search(t, c, znab.Request{Query: "nothing"}); len(items) != 0 {
		t.Fatal(items)
	}
}

func TestNyaa(t *testing.T) {
	c, _ := siteClient(t, "nyaa")
	items := search(t, c, znab.Request{Type: znab.SearchTV, Query: "frieren", Categories: []int{5070}})
	checkItems(t, items, 4)
	it := items[0]
	if !strings.HasPrefix(it.Title, "[Erai-raws] Sousou no Frieren 2nd Season") || it.InfoURL != "https://nyaa.si/view/2160092" ||
		it.InfoHash != "ef3e7ad1b12bdd9fc341691d8866cd1fa8374a4b" || *it.Seeders != 40 || *it.Leechers != 3 || *it.Grabs != 1701 ||
		it.Size != int64(math.Round(6.6*(1<<30))) || it.Categories[0] != 5070 || it.Link != "" {
		t.Fatalf("%+v", it)
	}
	if items := search(t, c, znab.Request{Query: "no such anime"}); len(items) != 0 {
		t.Fatal(items)
	}
}

func TestTokyoToshokan(t *testing.T) {
	c, _ := siteClient(t, "tokyotosho")
	items := search(t, c, znab.Request{Type: znab.SearchTV, Query: "subsplease"})
	checkItems(t, items, 3)
	for _, it := range items {
		if it.Size == 0 || it.InfoURL == "" || len(it.Categories) == 0 {
			t.Fatalf("%+v", it)
		}
	}
}

func TestLimeTorrents(t *testing.T) {
	c, _ := siteClient(t, "limetorrents")
	items := search(t, c, znab.Request{Query: "ubuntu"})
	checkItems(t, items, 4)
	it := items[0]
	if it.Title == "" || !strings.HasPrefix(it.InfoURL, c.BaseURL+"/") || it.Size == 0 || it.Seeders == nil || it.Link != "" {
		t.Fatalf("%+v", it)
	}
	if items := search(t, c, znab.Request{Query: "nothing"}); len(items) != 0 {
		t.Fatal(items)
	}
	checkItems(t, search(t, c, znab.Request{}), 3)
}

func TestX1337(t *testing.T) {
	c, _ := siteClient(t, "1337x")
	items := search(t, c, znab.Request{Query: "ubuntu"})
	checkItems(t, items, 4)
	it := items[0]
	if it.Title != "Ubuntu Linux Bible 11E by David Clinton (epub)(Nonfiction)" || it.MagnetURL != "" || it.Link != it.InfoURL ||
		*it.Seeders != 93 || it.Size != int64(math.Round(8.1*(1<<20))) {
		t.Fatalf("%+v", it)
	}
	res, err := c.Fetch(context.Background(), it.Link)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Magnet, "magnet:?xt=urn:btih:45BB7B3AC77116EF4E7EC743E2A8319CADDF87B1") {
		t.Fatalf("%+v", res)
	}
	if items := search(t, c, znab.Request{Query: "nothing"}); len(items) != 0 {
		t.Fatal(items)
	}
	// RSS is not offered: an empty request returns nothing without a request.
	if items := search(t, c, znab.Request{}); len(items) != 0 {
		t.Fatal(items)
	}
}

func TestBitSearch(t *testing.T) {
	c, _ := siteClient(t, "bitsearch")
	items := search(t, c, znab.Request{Query: "ubuntu"})
	checkItems(t, items, 3)
	if items[0].Title != "ubuntu-19.04-desktop-amd64.iso" || *items[0].Seeders != 28 || *items[0].Grabs != 1403 ||
		items[0].PublishDate.Year() != 2019 {
		t.Fatalf("%+v", items[0])
	}
	if items := search(t, c, znab.Request{Query: "nothing"}); len(items) != 0 {
		t.Fatal(items)
	}
}

func TestCapsFromDefinition(t *testing.T) {
	c, _ := siteClient(t, "nyaa")
	caps, err := c.Caps(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !caps.Search(znab.SearchTV).Supports("season") || caps.Search(znab.SearchMovie).Supports("imdbid") {
		t.Fatalf("%+v", caps.Searching)
	}
	ids := caps.CategoryIDs()
	for _, want := range []int{3000, 3040, 5000, 5070, 7000} {
		found := false
		for _, id := range ids {
			found = found || id == want
		}
		if !found {
			t.Fatalf("caps categories %v miss %d", ids, want)
		}
	}
}

func TestLayoutChangeAndChallenge(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/changed/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body><div class='new-layout'>results moved</div></body></html>"))
	})
	mux.HandleFunc("/cf/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html><title>Just a moment...</title><script src='/cdn-cgi/challenge-platform/x'></script></html>"))
	})
	mux.HandleFunc("/limited/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	mux.HandleFunc("/down/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(522) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	def, _ := registry(t).Get("1337x")
	for path, want := range map[string]znab.Kind{"/changed": znab.KindLayout, "/cf": znab.KindChallenge,
		"/limited": znab.KindRateLimit, "/down": znab.KindUnreachable} {
		d := *def
		d.DelayMillis = 0
		c := &native.Client{Def: &d, HTTP: localHTTP(t), BaseURL: srv.URL + path}
		_, err := c.Search(context.Background(), znab.Request{Query: "ubuntu"})
		var ze *znab.Error
		if !errors.As(err, &ze) || ze.Kind != want {
			t.Errorf("%s: %v, want %s", path, err, want)
		}
		if want == znab.KindRateLimit && ze != nil && ze.RetryAfter != 120 {
			t.Errorf("retry-after %d", ze.RetryAfter)
		}
	}
}

func TestCustomDefinitions(t *testing.T) {
	dir := t.TempDir()
	good := `id: mysite
name: My site
version: 2
links: [https://my.example]
verified: by hand
modes: {search: [q]}
search:
  requests: [{path: "/s?q={{ .Keywords | urlquery }}"}]
  response: json
  rows: results
  fields:
    title: {select: name}
    infohash: {select: hash}
test: {query: test}
`
	override := strings.Replace(strings.Replace(good, "id: mysite", "id: nyaa", 1), "name: My site", "name: Nyaa (fixed)", 1)
	files := map[string]string{"good.yml": good, "nyaa.yml": override, "broken.yml": "id: x\nname: [", "bad.yaml": "id: Bad Id\nname: x\n",
		"dupe.yml": good, "readme.txt": "ignored", "go.yml": strings.Replace(good, "search:", "impl: yts\nsearch:", 1)}
	for n, s := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r, err := native.NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := r.Get("mysite")
	if !ok || d.Builtin || d.Version != 2 {
		t.Fatalf("%+v %v", d, ok)
	}
	n, _ := r.Get("nyaa")
	if !n.Overrides || n.Name != "Nyaa (fixed)" {
		t.Fatalf("override %+v", n)
	}
	errs := r.Errors()
	if len(errs) != 4 { // broken, bad, dupe (or good), go
		t.Fatalf("%+v", errs)
	}
	if err := os.Remove(filepath.Join(dir, "nyaa.yml")); err != nil {
		t.Fatal(err)
	}
	if err := r.Reload(); err != nil {
		t.Fatal(err)
	}
	if n, _ := r.Get("nyaa"); !n.Builtin {
		t.Fatal("built-in not restored after reload")
	}
}

func TestMatch(t *testing.T) {
	r := registry(t)
	for _, c := range []struct {
		names, urls []string
		want        string
	}{
		{[]string{"Nyaa.si"}, nil, "nyaa"},
		{[]string{"The Pirate Bay"}, nil, "thepiratebay"},
		{[]string{"My 1337x"}, []string{"https://1337x.to/"}, "1337x"},
		{[]string{"LimeTorrents"}, nil, "limetorrents"},
		{[]string{"YTS"}, nil, "yts"},
		{[]string{"Tokyo Toshokan"}, nil, "tokyotosho"},
	} {
		d, ok := r.Match(c.names, c.urls)
		if !ok || d.ID != c.want {
			t.Errorf("%v %v: %v", c.names, c.urls, d)
		}
	}
	if _, ok := r.Match([]string{"World-torrent"}, []string{"https://world-torrent.example"}); ok {
		t.Fatal("matched an unknown site")
	}
}
