// Command locarr-fakes runs the fake Torznab/Newznab indexers (and the
// Prowlarr/Jackett-style listings) for manual testing; see scripts/dev-fakes.sh.
//
//	LOCARR_FAKES_ADDR        listen address (default :9117)
//	LOCARR_FAKES_API_KEY     key of the direct indexers (default fakekey)
//	LOCARR_FAKES_SOURCE_KEY  key of the Prowlarr/Jackett endpoints (default sourcekey)
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/fake"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	addr := env("LOCARR_FAKES_ADDR", ":9117")
	srv := &http.Server{Addr: addr, Handler: fake.New(env("LOCARR_FAKES_API_KEY", "fakekey"), env("LOCARR_FAKES_SOURCE_KEY", "sourcekey")),
		ReadHeaderTimeout: 10 * time.Second}
	log.Printf("fake indexers listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}
