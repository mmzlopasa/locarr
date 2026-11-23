package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/indexers/native/sitefake"
)

// TestBuiltinIndexers runs built-in sites (backed by their recorded answers)
// through the API: catalog, one-click add with the site's defaults, test,
// search, grab of a magnet result and of a details-page result, custom
// definitions and the self-test.
func TestBuiltinIndexers(t *testing.T) {
	e := newEnv(t, noLimits)
	e.setup()
	sites := sitefake.New()
	srv := httptest.NewServer(http.StripPrefix("/sites", sites))
	t.Cleanup(srv.Close)
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"allowedHosts": []string{"127.0.0.1"}}), 200)

	var cat struct {
		Definitions []struct {
			ID, Name, Verified, Flaresolverr string
			Builtin, Rss                     bool
			RateLimit                        struct{ Requests, PeriodSeconds int }
			Searching                        []struct{ Type string }
		}
		Presets []struct{ ID, BaseUrl, ApiPath string }
		Errors  []struct{ File, Message string }
	}
	e.expect(e.do("GET", "/api/v1/indexers/definitions", nil), 200).json(&cat)
	if len(cat.Definitions) != 10 || len(cat.Presets) < 5 || len(cat.Errors) != 0 {
		t.Fatalf("%+v", cat)
	}
	for _, d := range cat.Definitions {
		if !d.Builtin || d.Verified == "" || d.RateLimit.Requests == 0 || len(d.Searching) == 0 {
			t.Errorf("%+v", d)
		}
		if d.ID == "1337x" && (d.Flaresolverr != "always" || d.Rss) {
			t.Errorf("1337x: %+v", d)
		}
	}

	type indexer struct {
		ID                   int64
		Kind                 string
		BaseUrl              string
		EnableRss            bool
		RequestLimit         int
		RequestPeriodSeconds int
		TimeoutSeconds       int
		Categories           struct{ Tv, Movies []int }
		Definition           struct {
			ID      string
			Version int
			Builtin bool
		}
		Options map[string]string
		Health  struct{ Status string }
	}
	add := func(def string, extra map[string]any) indexer {
		body := map[string]any{"name": "Site " + def, "kind": "native", "definition": def, "baseUrl": srv.URL + "/sites/" + def}
		for k, v := range extra {
			body[k] = v
		}
		var ind indexer
		e.expect(e.do("POST", "/api/v1/indexers", body), 201).json(&ind)
		return ind
	}
	nyaa := add("nyaa", map[string]any{"options": map[string]string{"filter": "2"}})
	if nyaa.Kind != "native" || nyaa.Definition.ID != "nyaa" || !nyaa.Definition.Builtin || nyaa.RequestLimit != 12 ||
		nyaa.RequestPeriodSeconds != 60 || !nyaa.EnableRss || len(nyaa.Categories.Tv) == 0 || nyaa.Options["filter"] != "2" {
		t.Fatalf("%+v", nyaa)
	}
	x := add("1337x", nil)
	if x.EnableRss || x.TimeoutSeconds != 60 {
		t.Fatalf("1337x defaults: %+v", x)
	}
	// Validation: unknown definitions and options, API keys.
	for _, bad := range []map[string]any{
		{"name": "x1", "kind": "native", "definition": "nope"},
		{"name": "x2", "kind": "native", "definition": "nyaa", "options": map[string]string{"filter": "9"}},
		{"name": "x3", "kind": "native", "definition": "nyaa", "options": map[string]string{"unknown": "1"}},
		{"name": "x4", "kind": "native", "definition": "nyaa", "apiKey": "secret"},
		{"name": "x5", "kind": "torznab"},
	} {
		if r := e.expect(e.do("POST", "/api/v1/indexers", bad), 400); r.code() != "validation" {
			t.Fatalf("%v: %s", bad, r.body)
		}
	}

	var tr struct {
		Ok           bool
		ProbeResults int
		ErrorCode    string
	}
	e.expect(e.do("POST", "/api/v1/indexers/"+strconv.FormatInt(nyaa.ID, 10)+"/test", nil), 200).json(&tr)
	if !tr.Ok || tr.ProbeResults == 0 {
		t.Fatalf("test: %+v", tr)
	}

	var sr struct {
		Releases []struct {
			Title, MagnetUrl, DownloadLink, InfoHash, IndexerName string
			IndexerId                                             int64
			Seeders                                               *int
		}
		Indexers []struct{ IndexerName, Status, ErrorCode string }
	}
	e.expect(e.do("GET", "/api/v1/search?q=frieren&type=tv", nil), 200).json(&sr)
	if len(sr.Releases) < 4 || sr.Releases[0].MagnetUrl == "" || sr.Releases[0].InfoHash == "" || sr.Releases[0].Seeders == nil {
		t.Fatalf("%+v", sr)
	}

	fake := &apiFake{items: map[string]downloaders.Item{}}
	e.deps.Downloads.NewClient = func(downloaders.Implementation, downloaders.Config) (downloaders.Client, error) { return fake, nil }
	e.expect(e.do("POST", "/api/v1/downloadclients", map[string]any{"name": "Fake", "implementation": "apifake", "host": "fake.lan",
		"password": "pw", "categories": map[string]string{"movies": "movies", "series": "tv", "music": ""}}), 201)

	rel := sr.Releases[0]
	e.expect(e.do("POST", "/api/v1/queue/grab", map[string]any{"downloadUrl": rel.MagnetUrl, "protocol": "torrent", "title": rel.Title,
		"indexerId": rel.IndexerId, "mediaKind": "series"}), 201)
	if _, ok := fake.items[rel.InfoHash]; !ok {
		t.Fatalf("magnet grab did not reach the client: %+v", fake.items)
	}

	// 1337x: the magnet comes from the details page when grabbing.
	sr.Releases, sr.Indexers = nil, nil
	e.expect(e.do("GET", "/api/v1/search?q=ubuntu&indexerIds="+strconv.FormatInt(x.ID, 10), nil), 200).json(&sr)
	if len(sr.Releases) != 4 || sr.Releases[0].MagnetUrl != "" || sr.Releases[0].DownloadLink == "" {
		t.Fatalf("%+v", sr)
	}
	u, _ := url.Parse(sr.Releases[0].DownloadLink)
	e.expect(e.do("POST", "/api/v1/queue/grab", map[string]any{"downloadToken": u.Query().Get("token"), "protocol": "torrent",
		"title": sr.Releases[0].Title, "indexerId": x.ID}), 201)
	if _, ok := fake.items["45bb7b3ac77116ef4e7ec743e2a8319caddf87b1"]; !ok {
		t.Fatalf("details-page grab: %+v", fake.items)
	}
	// The browser download of such a result is the magnet as text.
	r := e.expect(e.do("GET", sr.Releases[0].DownloadLink, nil), 200)
	if !strings.HasPrefix(string(r.body), "magnet:?xt=urn:btih:45BB7B3AC77116EF4E7EC743E2A8319CADDF87B1") {
		t.Fatalf("%s", r.body)
	}

	// RSS sync reads the sites with a latest list and skips the others.
	if _, err := e.deps.Indexers.SyncRSS(context.Background()); err != nil {
		t.Fatal(err)
	}

	// A broken mirror: health says the layout changed.
	add("bitsearch", map[string]any{"baseUrl": srv.URL + "/sites/nyaa"})
	sr.Releases, sr.Indexers = nil, nil
	e.expect(e.do("GET", "/api/v1/search?q=ubuntu", nil), 200).json(&sr)
	found := false
	for _, ir := range sr.Indexers {
		if ir.IndexerName == "Site bitsearch" {
			found = ir.Status == "error" && ir.ErrorCode == "layout"
		}
	}
	if !found {
		t.Fatalf("layout error not reported: %+v", sr.Indexers)
	}

	// Self-test: every enabled built-in, outcome stored.
	res, err := e.deps.Indexers.SelfTest(context.Background())
	if err != nil || res.Tested != 3 || res.Failed != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	var list []struct {
		Name   string
		Health struct {
			SelfTestOk    *bool
			SelfTestAt    string
			ParseFailures *int
		}
	}
	e.expect(e.do("GET", "/api/v1/indexers", nil), 200).json(&list)
	for _, l := range list {
		if l.Health.SelfTestOk == nil || l.Health.SelfTestAt == "" || l.Health.ParseFailures == nil {
			t.Fatalf("%+v", l)
		}
	}

	// Custom definitions: dropped into the folder, errors listed, reload.
	dir := filepath.Join(e.deps.Indexers.Natives.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.yml"), []byte("id: broken\nname: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	custom := strings.ReplaceAll(string(sitefakeDefinition), "{{BASE}}", srv.URL)
	if err := os.WriteFile(filepath.Join(dir, "mysite.yml"), []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	e.expect(e.do("POST", "/api/v1/indexers/definitions/reload", nil), 200).json(&cat)
	if len(cat.Definitions) != 11 || len(cat.Errors) != 1 || cat.Errors[0].File != "broken.yml" {
		t.Fatalf("%+v", cat)
	}
	my := add("mysite", map[string]any{"baseUrl": ""})
	sr.Releases, sr.Indexers = nil, nil
	e.expect(e.do("GET", "/api/v1/search?q=ubuntu&indexerIds="+strconv.FormatInt(my.ID, 10), nil), 200).json(&sr)
	if len(sr.Releases) != 4 {
		t.Fatalf("custom definition: %+v", sr)
	}
}

// sitefakeDefinition is a custom definition (The Pirate Bay's recorded
// answers read declaratively).
var sitefakeDefinition = []byte(`id: mysite
name: My site
version: 1
links: ["{{BASE}}/sites/thepiratebay"]
verified: test
modes: {search: [q]}
search:
  requests: [{path: "/q.php?q={{ .Keywords | urlquery }}"}]
  response: json
  rows: ""
  skip: {infohash: "^0+$"}
  fields:
    title: {select: name}
    infohash: {select: info_hash}
    size: {select: size}
    date: {select: added}
    seeders: {select: seeders}
test: {query: ubuntu}
`)
