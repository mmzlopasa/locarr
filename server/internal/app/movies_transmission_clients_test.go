//go:build clients

package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/config"
	"github.com/mmzlopasa/locarr/server/internal/indexers/fake"
	tmdbfake "github.com/mmzlopasa/locarr/server/internal/metadata/tmdb/fake"
)

// TestMovieFlowTransmission runs the acceptance flow against a real
// Transmission (scripts/test-clients/transmission.sh starts it): the fake
// indexer serves a tiny torrent whose data it writes into the folder the
// client saves to, so Transmission verifies and seeds it without network.
func TestMovieFlowTransmission(t *testing.T) {
	env := func(k string) string { return os.Getenv("LOCARR_TEST_TRANSMISSION_" + k) }
	if env("HOST") == "" {
		t.Skip("LOCARR_TEST_TRANSMISSION_HOST not set")
	}
	seedLocal, seedClient := env("SEED_LOCAL"), env("SEED_CLIENT")
	seedLocal, _ = filepath.EvalSymlinks(seedLocal)
	e := newEnvWith(t, func(c *config.Config) { c.DataRoots = append(c.DataRoots, seedLocal) }, noLimits)
	e.setup()
	ix := fake.New("fakekey", "sourcekey")
	ix.SeedDir = seedLocal
	srv := httptest.NewServer(ix)
	t.Cleanup(srv.Close)

	e.expect(e.do("PUT", "/api/v1/settings/metadata", map[string]any{"tmdbApiKey": tmdbfake.APIKey}), 200)
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"allowedHosts": []string{"127.0.0.1"}}), 200)
	e.expect(e.do("POST", "/api/v1/indexers", map[string]any{"name": "Fake Torznab", "kind": "torznab", "baseUrl": srv.URL + "/torznab", "apiKey": "fakekey"}), 201)
	port, _ := strconv.Atoi(env("PORT"))
	// Locarr does not choose the client's folder: make the seed folder the
	// client's default download folder, as a user would.
	setTransmissionDir(t, fmt.Sprintf("http://%s:%d/transmission/rpc", env("HOST"), port), env("USER"), env("PASS"), seedClient)
	e.expect(e.do("POST", "/api/v1/downloadclients", map[string]any{"name": "Transmission", "implementation": "transmission",
		"host": env("HOST"), "port": port, "username": env("USER"), "password": env("PASS"), "directory": seedClient,
		"categories": map[string]string{"movies": "locarr-movies", "series": "", "music": ""}}), 201)
	e.expect(e.do("POST", "/api/v1/remotepathmappings", map[string]any{"host": env("HOST"), "remotePath": seedClient, "localPath": seedLocal}), 201)
	movies := filepath.Join(seedLocal, "library", "movies")
	if err := os.MkdirAll(movies, 0o755); err != nil {
		t.Fatal(err)
	}
	var rf struct{ ID int64 }
	e.expect(e.do("POST", "/api/v1/rootfolders", map[string]any{"path": movies, "kind": "movies"}), 201).json(&rf)
	var profiles []struct {
		ID         int64
		Name, Kind string
	}
	e.expect(e.do("GET", "/api/v1/qualityprofiles", nil), 200).json(&profiles)
	var profile int64
	for _, p := range profiles {
		if p.Kind == "movies" && p.Name == "HD-720p/1080p" {
			profile = p.ID
		}
	}
	var m apiMovie
	e.expect(e.do("POST", "/api/v1/movies", map[string]any{"tmdbId": 10378, "rootFolderId": rf.ID, "qualityProfileId": profile,
		"searchOnAdd": true}), 201).json(&m)
	le := &libraryEnv{env: e}
	deadline := time.Now().Add(90 * time.Second)
	for le.movieFile(m.ID) == "" {
		if time.Now().After(deadline) {
			tl := le.timeline(m.ID)
			t.Fatalf("not imported; timeline: %s; explanation: %+v", tl.types(), tl.Explanation)
		}
		le.poll()
		time.Sleep(time.Second)
	}
	file := le.movieFile(m.ID)
	if !strings.HasSuffix(file, "Big Buck Bunny (2008) [Bluray-1080p][x264]-LOCARR.mkv") {
		t.Fatalf("file %s", file)
	}
	src, _ := os.Stat(filepath.Join(seedLocal, "Big.Buck.Bunny.2008.1080p.BluRay.x264-LOCARR.mkv"))
	dst, _ := os.Stat(file)
	if src == nil || dst == nil || !os.SameFile(src, dst) {
		t.Fatal("not hardlinked from the client's folder")
	}
	tl := le.timeline(m.ID)
	for _, typ := range []string{"search", "decision", "download_grabbed", "download_completed", "file_imported", "download_imported"} {
		if !strings.Contains(tl.types(), typ) {
			t.Errorf("timeline misses %s: %s", typ, tl.types())
		}
	}
	// Leave the client clean.
	var q struct{ Items []struct{ ID int64 } }
	e.expect(e.do("GET", "/api/v1/queue", nil), 200).json(&q)
	for _, it := range q.Items {
		e.do("DELETE", fmt.Sprintf("/api/v1/queue/%d", it.ID), nil)
	}
}

func (le *libraryEnv) movieFile(id int64) string {
	m := le.movie(id)
	if m.File == nil {
		return ""
	}
	return m.File.Path
}

func setTransmissionDir(t *testing.T, url, user, pass, dir string) {
	t.Helper()
	body := fmt.Sprintf(`{"method":"session-set","arguments":{"download-dir":%q,"incomplete-dir-enabled":false}}`, dir)
	sid := ""
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("POST", url, strings.NewReader(body))
		req.SetBasicAuth(user, pass)
		req.Header.Set("X-Transmission-Session-Id", sid)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode == http.StatusConflict {
			sid = res.Header.Get("X-Transmission-Session-Id")
			continue
		}
		if res.StatusCode != http.StatusOK {
			t.Fatalf("session-set: %s", res.Status)
		}
		return
	}
	t.Fatal("session-set: no session id")
}
