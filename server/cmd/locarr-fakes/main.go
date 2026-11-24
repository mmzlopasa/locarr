// Command locarr-fakes runs the fake Torznab/Newznab indexers (and the
// Prowlarr/Jackett-style listings) for manual testing; see scripts/dev-fakes.sh.
//
//	LOCARR_FAKES_ADDR        listen address (default :9117)
//	LOCARR_FAKES_API_KEY     key of the direct indexers (default fakekey)
//	LOCARR_FAKES_SOURCE_KEY  key of the Prowlarr/Jackett endpoints (default sourcekey)
//	LOCARR_FAKES_SEED_DIR    serve real tiny torrents whose data is written here
//
// It also serves a fake TMDb under /tmdb (API key in internal/metadata/tmdb/fake)
// and a fake MusicBrainz and Cover Art Archive under /musicbrainz (ws/2 at
// /musicbrainz/ws/2, covers at /musicbrainz/coverart), fake subtitle
// providers: OpenSubtitles.com under /opensubtitles/api/v1, SubDL under
// /subdl/api/v1 and Jimaku under /jimaku/api (keys in
// internal/subtitles/providers/fake), and a fake Streamero under /streamero
// (accounts and keys in internal/mediaserver/streamero/fake; state at
// /streamero/_fake/state, reset with POST /streamero/_fake/reset).
// Music discovery's providers are faked too: Last.fm at /lastfm (key in
// internal/metadata/lastfm/fake), ListenBrainz (API and labs) at
// /listenbrainz and Spotify at /spotify (accounts at /spotify, API at
// /spotify/v1; credentials in internal/metadata/spotify/fake).
// The applications an existing setup is imported from are faked under
// /arr/<kind> (API key migratekey; qBittorrent admin / migratepass); with a
// seed folder their files go to <seed>/migrate. The built-in public sites'
// recorded answers are served under /sites/<definition id>.
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/fake"
	"github.com/mmzlopasa/locarr/server/internal/indexers/native/sitefake"
	stfake "github.com/mmzlopasa/locarr/server/internal/mediaserver/streamero/fake"
	lfmfake "github.com/mmzlopasa/locarr/server/internal/metadata/lastfm/fake"
	lbfake "github.com/mmzlopasa/locarr/server/internal/metadata/listenbrainz/fake"
	mbfake "github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz/fake"
	spfake "github.com/mmzlopasa/locarr/server/internal/metadata/spotify/fake"
	tmdbfake "github.com/mmzlopasa/locarr/server/internal/metadata/tmdb/fake"
	migratefake "github.com/mmzlopasa/locarr/server/internal/migrate/fake"
	subfake "github.com/mmzlopasa/locarr/server/internal/subtitles/providers/fake"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	addr := env("LOCARR_FAKES_ADDR", ":9117")
	ix := fake.New(env("LOCARR_FAKES_API_KEY", "fakekey"), env("LOCARR_FAKES_SOURCE_KEY", "sourcekey"))
	ix.SeedDir = os.Getenv("LOCARR_FAKES_SEED_DIR")
	mux := http.NewServeMux()
	mux.Handle("/tmdb/", tmdbfake.New())
	mb := mbfake.New()
	// Like MusicBrainz, refuse clients that send more than one request per second.
	mb.MinInterval = 950 * time.Millisecond
	mux.Handle("/musicbrainz/", mb)
	mux.Handle("/lastfm/", lfmfake.New())
	lb := lbfake.New()
	lb.MinInterval = 950 * time.Millisecond
	mux.Handle("/listenbrainz/", lb)
	mux.Handle("/spotify/", spfake.New())
	mux.Handle("/streamero/", http.StripPrefix("/streamero", stfake.New()))
	subs := subfake.New()
	for _, p := range []string{"/opensubtitles/", "/subdl/", "/jimaku/"} {
		mux.Handle(p, subs)
	}
	// The applications an existing setup is imported from (P10), each under
	// /arr/<kind> with API key "migratekey" (qBittorrent: admin / migratepass).
	// With a seed folder their library's files are written to <seed>/migrate.
	lib := migratefake.Separate()
	if seed := os.Getenv("LOCARR_FAKES_SEED_DIR"); seed != "" {
		root := filepath.Join(seed, "migrate")
		if err := lib.WriteFiles(filepath.Join(root, "movies"), filepath.Join(root, "tv"), filepath.Join(root, "music"), nil); err != nil {
			log.Printf("migrate fixtures: %v", err)
		}
	}
	for _, k := range []string{"sonarr", "radarr", "lidarr", "prowlarr", "bazarr", "overseerr", "seerr", "qbittorrent", "sabnzbd"} {
		f := migratefake.New(k, "migratekey", lib)
		f.Prefix = "/arr/" + k
		if k == "qbittorrent" {
			f.Username, f.Password = "admin", "migratepass"
		}
		mux.Handle(f.Prefix+"/", f)
	}
	// Recorded answers of the built-in public sites, one per /sites/<id>
	// (point a built-in indexer's address at http://fake-indexer:9117/sites/<id>).
	mux.Handle("/sites/", http.StripPrefix("/sites", sitefake.New()))
	mux.Handle("/", ix)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("fake indexers listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}
