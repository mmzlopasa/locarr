//go:build clients

package qbittorrent

import (
	"os"
	"strconv"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/downloaders/contracttest"
)

// TestContract runs the shared suite against a real instance
// (scripts/test-clients/qbittorrent.sh starts one).
func TestContract(t *testing.T) {
	env := func(k string) string { return os.Getenv("LOCARR_TEST_QBITTORRENT_" + k) }
	if env("HOST") == "" {
		t.Skip("LOCARR_TEST_QBITTORRENT_HOST is not set")
	}
	port, _ := strconv.Atoi(env("PORT"))
	c, err := New(downloaders.Config{Host: env("HOST"), Port: port, URLBase: env("URLBASE"),
		Username: env("USER"), Password: env("PASS")})
	if err != nil {
		t.Fatal(err)
	}
	contracttest.Run(t, contracttest.Target{Client: c, ClientSeedDir: env("SEED_CLIENT"), LocalSeedDir: env("SEED_LOCAL")})
}
