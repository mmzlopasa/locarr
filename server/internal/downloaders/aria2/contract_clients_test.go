//go:build clients

package aria2

import (
	"os"
	"strconv"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/downloaders/contracttest"
)

// TestContract runs the shared suite against a real instance described by
// LOCARR_TEST_ARIA2_* (scripts/test-clients/aria2.sh starts one).
func TestContract(t *testing.T) {
	env := func(k string) string { return os.Getenv("LOCARR_TEST_ARIA2_" + k) }
	if env("HOST") == "" {
		t.Skip("LOCARR_TEST_ARIA2_HOST not set")
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
