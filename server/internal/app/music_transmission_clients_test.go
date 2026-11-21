//go:build clients

package app

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/config"
	"github.com/mmzlopasa/locarr/server/internal/indexers/fake"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	mbfake "github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz/fake"
)

// TestMusicFlowTransmission runs the album flow against a real Transmission:
// the fake indexer serves a multi-file torrent of tagged FLAC files that it
// writes into the client's folder, Transmission verifies and seeds it, and
// Locarr matches the files to the release and hardlinks them into the library.
func TestMusicFlowTransmission(t *testing.T) {
	env := func(k string) string { return os.Getenv("LOCARR_TEST_TRANSMISSION_" + k) }
	if env("HOST") == "" {
		t.Skip("LOCARR_TEST_TRANSMISSION_HOST not set")
	}
	seedLocal, seedClient := env("SEED_LOCAL"), env("SEED_CLIENT")
	seedLocal, _ = filepath.EvalSymlinks(seedLocal)
	mb := mbfake.New()
	mbSrv := httptest.NewServer(mb)
	t.Cleanup(mbSrv.Close)
	e := newEnvWith(t, func(c *config.Config) {
		c.DataRoots = append(c.DataRoots, seedLocal)
		c.MusicBrainzURL, c.CoverArtURL = mbSrv.URL+"/mb", mbSrv.URL+"/mb/coverart"
	}, noLimits)
	e.deps.MusicBrainz.Limiter = metadata.NewLimiter(1000, 1000)
	e.setup()
	ix := fake.New("fakekey", "sourcekey")
	ix.SeedDir = seedLocal
	srv := httptest.NewServer(ix)
	t.Cleanup(srv.Close)
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"allowedHosts": []string{"127.0.0.1"}}), 200)
	e.expect(e.do("POST", "/api/v1/indexers", map[string]any{"name": "Fake Torznab", "kind": "torznab", "baseUrl": srv.URL + "/torznab", "apiKey": "fakekey"}), 201)
	port, _ := strconv.Atoi(env("PORT"))
	setTransmissionDir(t, fmt.Sprintf("http://%s:%d/transmission/rpc", env("HOST"), port), env("USER"), env("PASS"), seedClient)
	e.expect(e.do("POST", "/api/v1/downloadclients", map[string]any{"name": "Transmission", "implementation": "transmission",
		"host": env("HOST"), "port": port, "username": env("USER"), "password": env("PASS"), "directory": seedClient,
		"categories": map[string]string{"movies": "", "series": "", "music": "locarr-music"}}), 201)
	e.expect(e.do("POST", "/api/v1/remotepathmappings", map[string]any{"host": env("HOST"), "remotePath": seedClient, "localPath": seedLocal}), 201)
	lib := filepath.Join(seedLocal, "library", "music")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	me := &musicEnv{env: e, mb: mb, music: lib}
	var rf struct{ ID int64 }
	e.expect(e.do("POST", "/api/v1/rootfolders", map[string]any{"path": lib, "kind": "music"}), 201).json(&rf)
	me.rootID = rf.ID
	var profiles []struct {
		ID         int64
		Name, Kind string
	}
	e.expect(e.do("GET", "/api/v1/qualityprofiles", nil), 200).json(&profiles)
	for _, p := range profiles {
		if p.Kind == "music" && p.Name == "Lossless" {
			me.lossless = p.ID
		}
	}
	me.standard = 1
	a := me.addArtist(mbfake.ArtistOpen, true, "all")
	free := me.albums(a.ID)["Free Album"]
	deadline := time.Now().Add(120 * time.Second)
	for me.album(free.ID).Album.LibraryStatus != "downloaded" {
		if time.Now().After(deadline) {
			tl := me.timeline(free.ID)
			t.Fatalf("not imported; timeline: %s; explanation: %+v", tl.types(), tl.Explanation)
		}
		me.poll()
		time.Sleep(time.Second)
	}
	d := me.album(free.ID)
	src, _ := os.Stat(filepath.Join(seedLocal, "Open Source Artist - Free Album (2020) [FLAC]", "01. Open Source Artist - Opening.flac"))
	dst, _ := os.Stat(d.Tracks[0].File.Path)
	if src == nil || dst == nil || !os.SameFile(src, dst) {
		t.Fatalf("not hardlinked from the client's folder: %s", d.Tracks[0].File.Path)
	}
	var q struct{ Items []struct{ ID int64 } }
	e.expect(e.do("GET", "/api/v1/queue", nil), 200).json(&q)
	for _, it := range q.Items {
		e.do("DELETE", fmt.Sprintf("/api/v1/queue/%d", it.ID), nil)
	}
}
