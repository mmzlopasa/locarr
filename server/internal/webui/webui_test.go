package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestServesAppAndRejectsProbes(t *testing.T) {
	h := FromFS(fstest.MapFS{
		"index.html":      {Data: []byte("<html>app</html>")},
		"assets/app-1.js": {Data: []byte("js")},
	})
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		return rec
	}
	if r := get("/movies/12"); r.Code != 200 || !strings.Contains(r.Body.String(), "app") || r.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal("client route", r.Code)
	}
	if r := get("/assets/app-1.js"); r.Code != 200 || !strings.Contains(r.Header().Get("Cache-Control"), "immutable") {
		t.Fatal("asset caching")
	}
	for _, p := range []string{"/.env", "/.git/HEAD", "/admin.php", "/config.yml", "/db.sqlite"} {
		if r := get(p); r.Code != 404 {
			t.Errorf("%s: %d", p, r.Code)
		}
	}
	if r := get("/robots.txt"); !strings.Contains(r.Body.String(), "Disallow: /") {
		t.Fatal("robots")
	}
}

func TestNotBuilt(t *testing.T) {
	h := FromFS(fstest.MapFS{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rec.Body.String(), "not built") {
		t.Fatal(rec.Body.String())
	}
}
