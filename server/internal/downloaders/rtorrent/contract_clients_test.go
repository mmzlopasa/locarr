//go:build clients

package rtorrent

import (
	"os"
	"strconv"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/downloaders/contracttest"
)

// TestContract runs the shared suite against a real instance described by
// LOCARR_TEST_RTORRENT_* (scripts/test-clients/rtorrent.sh starts one).
func TestContract(t *testing.T) {
	env := func(k string) string { return os.Getenv("LOCARR_TEST_RTORRENT_" + k) }
	if env("HOST") == "" {
		t.Skip("LOCARR_TEST_RTORRENT_HOST not set")
	}
	port, err := strconv.Atoi(env("PORT"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(downloaders.Config{Host: env("HOST"), Port: port, URLBase: env("URLBASE"), Username: env("USER"),
		Password: env("PASS"), APIKey: env("APIKEY"), Directory: env("DIR")})
	if err != nil {
		t.Fatal(err)
	}
	contracttest.Run(t, contracttest.Target{Client: c, Category: env("CATEGORY"),
		ClientSeedDir: env("SEED_CLIENT"), LocalSeedDir: env("SEED_LOCAL")})
}
