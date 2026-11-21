//go:build clients

package sabnzbd

import (
	"os"
	"strconv"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/downloaders/contracttest"
)

func TestContract(t *testing.T) {
	host := os.Getenv("LOCARR_TEST_SABNZBD_HOST")
	if host == "" {
		t.Skip("LOCARR_TEST_SABNZBD_HOST not set")
	}
	port, _ := strconv.Atoi(os.Getenv("LOCARR_TEST_SABNZBD_PORT"))
	c, err := New(downloaders.Config{Host: host, Port: port, URLBase: os.Getenv("LOCARR_TEST_SABNZBD_URLBASE"),
		APIKey: os.Getenv("LOCARR_TEST_SABNZBD_APIKEY")})
	if err != nil {
		t.Fatal(err)
	}
	contracttest.Run(t, contracttest.Target{Client: c, Category: "locarr-test"})
}
