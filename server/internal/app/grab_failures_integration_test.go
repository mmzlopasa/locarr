package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders/testkit"
	"github.com/mmzlopasa/locarr/server/internal/indexers/fake"
)

// moreBunnies adds accepted releases of the same movie, so a failing grab
// has a next best one to try.
func moreBunnies(t *testing.T) {
	t.Helper()
	saved := fake.Catalog
	extra := append([]fake.Release{}, saved...)
	for i, g := range []string{"ALPHA", "BRAVO", "CHARLIE", "DELTA", "ECHO"} {
		extra = append(extra, fake.Release{Title: "Big.Buck.Bunny.2008.720p.WEBRip.x264-" + g, Cat: 2040, Size: 400_000_000 + int64(i),
			Seeders: 50 - i, Peers: 60, IMDB: "tt1254207", TMDB: 10378, Hash: fmt.Sprintf("%02dbbc3d4e5f60718293a4b5c6d7e8f9012345678", i),
			Age: 5 * time.Hour})
	}
	fake.Catalog = extra
	t.Cleanup(func() { fake.Catalog = saved })
}

// serveDownloads wraps the fake indexer and answers release downloads with dl.
func serveDownloads(hits *atomic.Int32, dl http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/dl/") {
				hits.Add(1)
				dl(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type grabEvent struct {
	Code        string
	Indexer     string
	Error       string
	Until       *time.Time
	Blocklisted bool
	Skipped     bool
}

func (tl apiTimeline) grabFailures(t *testing.T) []grabEvent {
	t.Helper()
	var out []grabEvent
	for _, ev := range tl.Events {
		if ev.Type == "grab_failed" {
			var g grabEvent
			if err := json.Unmarshal(ev.Data, &g); err != nil {
				t.Fatal(err)
			}
			out = append(out, g)
		}
	}
	return out
}

// An indexer manager that says "disabled till …" stops the cycle for that
// indexer at once, backs it off until then and blocklists nothing.
func TestGrabIndexerDisabledStopsAndBacksOff(t *testing.T) {
	moreBunnies(t)
	var hits atomic.Int32
	till := time.Now().Add(10 * time.Minute).Truncate(time.Second)
	le := newLibraryEnvWrapped(t, serveDownloads(&hits, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><error code="429" description="Indexer is disabled till %s due to recent failures." />`,
			till.Format("2006-01-02 15:04:05"))
	}))
	m := le.addMovie(10378, true)
	waitFor(t, "the failed grab", func() bool { return len(le.timeline(m.ID).grabFailures(t)) > 0 })
	time.Sleep(300 * time.Millisecond)
	if n := hits.Load(); n != 1 {
		t.Fatalf("%d download requests after the indexer said it is disabled", n)
	}
	g := le.timeline(m.ID).grabFailures(t)
	if len(g) != 1 || g[0].Code != "indexer_disabled" || g[0].Until == nil || !g[0].Until.Equal(till) || g[0].Blocklisted {
		t.Fatalf("grab failures: %+v", g)
	}
	if strings.Contains(g[0].Error, "apikey") || strings.Contains(g[0].Error, "?") {
		t.Fatalf("error carries the query: %s", g[0].Error)
	}
	var bl []struct{ Title string }
	le.expect(le.do("GET", "/api/v1/queue/blocklist", nil), 200).json(&bl)
	if len(bl) != 0 {
		t.Fatalf("a limited indexer blocklisted releases: %+v", bl)
	}
	var retryAt int64
	if err := le.deps.DB.R.QueryRow(`SELECT retry_at FROM indexers`).Scan(&retryAt); err != nil || time.UnixMilli(retryAt).Before(till.Add(-time.Second)) {
		t.Fatalf("indexer backoff until %v, want %v (%v)", time.UnixMilli(retryAt), till, err)
	}
	var ex struct {
		Code   string
		Params map[string]any
	}
	le.expect(le.do("GET", fmt.Sprintf("/api/v1/timeline/explain?item=movie:%d", m.ID), nil), 200).json(&ex)
	if ex.Code != "grab_failed" || ex.Params["failCode"] != "indexer_disabled" || ex.Params["accepted"] == nil {
		t.Fatalf("explain: %+v", ex)
	}
	// Retry now runs again at once, and still leaves the resting indexer alone.
	var before int
	_ = le.deps.DB.R.QueryRow(`SELECT count(*) FROM timeline_events WHERE type = 'decision'`).Scan(&before)
	le.expect(le.do("POST", fmt.Sprintf("/api/v1/timeline/retry?item=movie:%d", m.ID), nil), 202)
	waitFor(t, "the retried search", func() bool {
		var n int
		_ = le.deps.DB.R.QueryRow(`SELECT count(*) FROM timeline_events WHERE type = 'decision'`).Scan(&n)
		return n > before
	})
	time.Sleep(200 * time.Millisecond)
	if n := hits.Load(); n != 1 {
		t.Fatalf("retry hit the disabled indexer: %d requests", n)
	}
	le.expect(le.do("POST", "/api/v1/timeline/retry?item=movie:999999", nil), 404)
}

// A Prowlarr-style redirect to the tracker's own host is followed, without
// the indexer's key.
func TestGrabFollowsRedirectToTracker(t *testing.T) {
	tor := testkit.RandomTorrent("tracker", 1000)
	var mu sync.Mutex
	var trackerQuery string
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		trackerQuery = r.URL.RawQuery
		mu.Unlock()
		_, _ = w.Write(tor.File)
	}))
	defer tracker.Close()
	var hits atomic.Int32
	le := newLibraryEnvWrapped(t, serveDownloads(&hits, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(tracker.URL, "127.0.0.1", "localhost", 1)+"/get/file.torrent?id=5&apikey="+r.URL.Query().Get("apikey"), http.StatusFound)
	}))
	m := le.addMovie(10378, true)
	waitFor(t, "the grab", func() bool { return le.downloadCount(m.ID) == 1 })
	mu.Lock()
	defer mu.Unlock()
	if trackerQuery != "id=5" {
		t.Fatalf("the tracker saw %q", trackerQuery)
	}
	if g := le.timeline(m.ID).grabFailures(t); len(g) != 0 {
		t.Fatalf("grab failures: %+v", g)
	}
}

// Bad files are content errors: blocklisted, and a cycle tries at most three.
func TestGrabBadFilesBlocklistedAndCapped(t *testing.T) {
	moreBunnies(t)
	var hits atomic.Int32
	le := newLibraryEnvWrapped(t, serveDownloads(&hits, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html><html><body>Please log in</body></html>"))
	}))
	m := le.addMovie(10378, true)
	waitFor(t, "three failed grabs", func() bool {
		n := 0
		for _, g := range le.timeline(m.ID).grabFailures(t) {
			if g.Code == "bad_file" {
				n++
			}
		}
		return n == 3
	})
	time.Sleep(300 * time.Millisecond)
	if n := hits.Load(); n != 3 {
		t.Fatalf("%d download attempts, want 3", n)
	}
	var bl []struct{ Title, Reason string }
	le.expect(le.do("GET", "/api/v1/queue/blocklist", nil), 200).json(&bl)
	if len(bl) != 3 || bl[0].Reason != "bad_release" {
		t.Fatalf("blocklist: %+v", bl)
	}
	var healthFailures int
	_ = le.deps.DB.R.QueryRow(`SELECT failures FROM indexers`).Scan(&healthFailures)
	if healthFailures != 0 {
		t.Fatalf("bad files counted against the indexer's health: %d", healthFailures)
	}
}
