package listenbrainz_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/metadata/listenbrainz"
	"github.com/mmzlopasa/locarr/server/internal/metadata/listenbrainz/fake"
	mbfake "github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz/fake"
)

func newClient(t *testing.T) (*listenbrainz.Client, *fake.Server) {
	t.Helper()
	f := fake.New()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := listenbrainz.New(srv.URL, srv.URL)
	c.Contact = func(context.Context) string { return "ops@example.org" }
	return c, f
}

func TestSimilarAndTopGroup(t *testing.T) {
	c, f := newClient(t)
	c.Limiter = metadata.NewLimiter(1000, 1000)
	ctx := context.Background()
	list, err := c.Similar(ctx, mbfake.ArtistOpen)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].MBID != mbfake.ArtistChoir || list[0].Score != 812 {
		t.Fatalf("similar: %+v", list)
	}
	g, err := c.TopReleaseGroup(ctx, mbfake.ArtistChoir)
	if err != nil || g != mbfake.GroupChoral {
		t.Fatalf("top group: %q %v", g, err)
	}
	none, err := c.TopReleaseGroup(ctx, "00000000-0000-0000-0000-000000000000")
	if err != nil || none != "" {
		t.Fatalf("no group: %q %v", none, err)
	}
	if !strings.Contains(f.UserAgents[0], "Locarr/") || !strings.Contains(f.UserAgents[0], "ops@example.org") {
		t.Fatalf("user agent: %q", f.UserAgents[0])
	}
}

// ListenBrainz asks for one request per second; the client keeps to it and
// waits for X-RateLimit-Reset-In after a 429.
func TestRateLimit(t *testing.T) {
	c, f := newClient(t)
	f.MinInterval = 200 * time.Millisecond
	c.Limiter = metadata.NewLimiter(4, 1) // 250 ms apart
	for i := 0; i < 3; i++ {
		if _, err := c.Similar(context.Background(), mbfake.ArtistOpen); err != nil {
			t.Fatal(err)
		}
	}
	if f.Violations != 0 {
		t.Fatalf("violations: %d", f.Violations)
	}
	// Faster than the fake allows: the 429 pauses the client, which then succeeds.
	f.MinInterval = 300 * time.Millisecond
	c.Limiter = metadata.NewLimiter(1000, 1000)
	start := time.Now()
	if _, err := c.Similar(context.Background(), mbfake.ArtistOpen); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Similar(context.Background(), mbfake.ArtistOpen); err != nil && !errors.Is(err, metadata.ErrRateLimited) {
		t.Fatal(err)
	}
	if f.Violations == 0 || time.Since(start) < 900*time.Millisecond {
		t.Fatalf("expected a 429 and a pause: %d %v", f.Violations, time.Since(start))
	}
}
