//go:build live

package native_test

// The live smoke test asks every built-in site its known query, gently (one
// site at a time, one request each, with the definitions' own pacing). It
// is not part of CI:
//
//	go test -tags live -run TestLive ./internal/indexers/native/
//
// LOCARR_LIVE_BROWSER=http://127.0.0.1:8490 with LOCARR_LIVE_BROWSER_TOKEN
// lets sites behind a browser check go through the companion browser
// (browser/), LOCARR_LIVE_FLARESOLVERR=http://127.0.0.1:8191 through
// FlareSolverr; without either they are reported as skipped. A site that
// asks for a human check is reported as skipped with that reason.

import (
	"context"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/indexers/native"
	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
)

func TestLive(t *testing.T) {
	defs, err := native.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	fs := os.Getenv("LOCARR_LIVE_FLARESOLVERR")
	var br *httpx.Browser
	if u := os.Getenv("LOCARR_LIVE_BROWSER"); u != "" {
		br = &httpx.Browser{ControlPolicy: httpx.Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}},
			Config: func(context.Context) (httpx.BrowserConfig, error) {
				return httpx.BrowserConfig{URL: u, Token: os.Getenv("LOCARR_LIVE_BROWSER_TOKEN")}, nil
			}}
	}
	for _, d := range defs {
		t.Run(d.ID, func(t *testing.T) {
			timeout := 30 * time.Second
			if d.TimeoutSeconds > 0 {
				timeout = time.Duration(d.TimeoutSeconds) * time.Second
			}
			// A local FlareSolverr is allowed; the sites themselves are public.
			pol := httpx.Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}
			hc, err := httpx.New(httpx.Options{Policy: pol, Timeout: max(timeout, 90*time.Second), FlareSolverr: fs, Browser: br,
				Challenge: d.Challenge})
			if err != nil {
				t.Fatal(err)
			}
			c := &native.Client{Def: d, HTTP: hc, Trackers: native.DefaultTrackers}
			ctx, cancel := context.WithTimeout(context.Background(), 2*timeout)
			defer cancel()
			page, err := c.Search(ctx, c.TestRequest())
			switch znab.KindOf(err) {
			case znab.KindChallenge:
				t.Skipf("browser check not passed (set LOCARR_LIVE_BROWSER or LOCARR_LIVE_FLARESOLVERR): %v", err)
			case znab.KindUnreachable:
				t.Skipf("site unreachable: %v", err)
			}
			if err != nil {
				t.Fatalf("%v", err)
			}
			if len(page.Items) == 0 || page.Dropped > 0 {
				t.Fatalf("%d results, %d rows unreadable", len(page.Items), page.Dropped)
			}
			it := page.Items[0]
			t.Logf("%d results; first: %q (%d bytes, magnet %v)", len(page.Items), it.Title, it.Size, it.MagnetURL != "")
		})
	}
}
