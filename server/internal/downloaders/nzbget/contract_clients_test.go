//go:build clients

package nzbget

import (
	"os"
	"strconv"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/downloaders/contracttest"
)

func TestContract(t *testing.T) {
	host := os.Getenv("LOCARR_TEST_NZBGET_HOST")
	if host == "" {
		t.Skip("LOCARR_TEST_NZBGET_HOST not set")
	}
	port, _ := strconv.Atoi(os.Getenv("LOCARR_TEST_NZBGET_PORT"))
	c, err := New(downloaders.Config{Host: host, Port: port, URLBase: os.Getenv("LOCARR_TEST_NZBGET_URLBASE"),
		Username: os.Getenv("LOCARR_TEST_NZBGET_USER"), Password: os.Getenv("LOCARR_TEST_NZBGET_PASS")})
	if err != nil {
		t.Fatal(err)
	}
	// NZBGet categories cannot be created through the API; the runner script
	// configures this one.
	contracttest.Run(t, contracttest.Target{Client: c, Category: "locarr-test"})
}
