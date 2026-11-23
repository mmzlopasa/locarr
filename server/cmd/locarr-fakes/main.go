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
// /musicbrainz/ws/2, covers at /musicbrainz/coverart) and a fake Streamero under
// /streamero (accounts and keys in internal/mediaserver/streamero/fake; state at
// /streamero/_fake/state, reset with POST /streamero/_fake/reset).
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/fake"
	stfake "github.com/mmzlopasa/locarr/server/internal/mediaserver/streamero/fake"
	mbfake "github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz/fake"
	tmdbfake "github.com/mmzlopasa/locarr/server/internal/metadata/tmdb/fake"
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
	mux.Handle("/streamero/", http.StripPrefix("/streamero", stfake.New()))
	mux.Handle("/", ix)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("fake indexers listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}
