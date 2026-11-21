//go:build clients

package app

import (
	"fmt"
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

// TestSeasonPackTransmission grabs a season pack (a real multi-file torrent
// whose data the fake indexer writes into the client's folder), lets a real
// Transmission verify and seed it and imports every episode by hardlink.
func TestSeasonPackTransmission(t *testing.T) {
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
	setTransmissionDir(t, fmt.Sprintf("http://%s:%d/transmission/rpc", env("HOST"), port), env("USER"), env("PASS"), seedClient)
	e.expect(e.do("POST", "/api/v1/downloadclients", map[string]any{"name": "Transmission", "implementation": "transmission",
		"host": env("HOST"), "port": port, "username": env("USER"), "password": env("PASS"), "directory": seedClient,
		"categories": map[string]string{"movies": "", "series": "locarr-tv", "music": ""}}), 201)
	e.expect(e.do("POST", "/api/v1/remotepathmappings", map[string]any{"host": env("HOST"), "remotePath": seedClient, "localPath": seedLocal}), 201)
	tv := filepath.Join(seedLocal, "library", "tv")
	if err := os.MkdirAll(tv, 0o755); err != nil {
		t.Fatal(err)
	}
	var rf struct{ ID int64 }
	e.expect(e.do("POST", "/api/v1/rootfolders", map[string]any{"path": tv, "kind": "series"}), 201).json(&rf)
	var profiles []struct {
		ID         int64
		Name, Kind string
	}
	e.expect(e.do("GET", "/api/v1/qualityprofiles", nil), 200).json(&profiles)
	se := &seriesEnv{libraryEnv: &libraryEnv{env: e}, tv: tv, tvRoot: rf.ID}
	for _, p := range profiles {
		if p.Kind == "series" && p.Name == "HD-720p/1080p" {
			se.tvProfile = p.ID
		}
	}
	s := se.add(800434, "standard", "all", true)
	deadline := time.Now().Add(120 * time.Second)
	for se.episode(s.ID, 1, 3).File == nil {
		if time.Now().After(deadline) {
			tl := se.timeline(se.episode(s.ID, 1, 3).ItemRef)
			t.Fatalf("not imported; timeline: %s; explanation: %+v", tl.types(), tl.Explanation)
		}
		se.poll()
		time.Sleep(time.Second)
	}
	pack := filepath.Join(seedLocal, "Cosmos.Laundromat.S01.720p.WEB-DL.AAC2.0.x264-LOCARR")
	for n := 1; n <= 3; n++ {
		f := se.episode(s.ID, 1, n).File
		if f == nil || !strings.Contains(f.Path, filepath.Join("Season 01", fmt.Sprintf("Cosmos Laundromat - S01E%02d", n))) {
			t.Fatalf("S01E%02d: %+v", n, f)
		}
		src, _ := os.Stat(filepath.Join(pack, fmt.Sprintf("Cosmos.Laundromat.S01E%02d.720p.WEB-DL.AAC2.0.x264-LOCARR.mkv", n)))
		dst, _ := os.Stat(f.Path)
		if src == nil || dst == nil || !os.SameFile(src, dst) {
			t.Fatalf("S01E%02d not hardlinked from the client's folder", n)
		}
	}
	var q struct{ Items []struct{ ID int64 } }
	e.expect(e.do("GET", "/api/v1/queue", nil), 200).json(&q)
	for _, it := range q.Items {
		e.do("DELETE", fmt.Sprintf("/api/v1/queue/%d", it.ID), nil)
	}
}
