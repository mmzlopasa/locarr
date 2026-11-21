package musicbrainz_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz"
	"github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz/fake"
)

func newClient(t *testing.T, fast bool) (*musicbrainz.Client, *fake.Server) {
	t.Helper()
	f := fake.New()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := musicbrainz.New(srv.URL+"/mb", srv.URL+"/mb/coverart", &metadata.Cache{DB: db.OpenTest(t)})
	c.Contact = func(context.Context) string { return "ops@example.org" }
	if fast {
		c.Limiter = metadata.NewLimiter(1000, 1000)
	}
	return c, f
}

func TestLookups(t *testing.T) {
	c, f := newClient(t, true)
	ctx := context.Background()
	res, err := c.SearchArtists(ctx, "Open Source Artist")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Disambiguation == res[1].Disambiguation || res[0].Score < res[1].Score {
		t.Fatalf("search: %+v", res)
	}
	byID, err := c.SearchArtists(ctx, "mbid:"+fake.ArtistOpen)
	if err != nil || len(byID) != 1 || byID[0].MBID != fake.ArtistOpen {
		t.Fatalf("by id: %+v %v", byID, err)
	}
	a, err := c.Artist(ctx, fake.ArtistOpen)
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Open Source Artist" || len(a.Aliases) != 2 || a.Genres[0] != "electronic" || a.Type != "Group" {
		t.Fatalf("artist: %+v", a)
	}
	groups, err := c.ReleaseGroups(ctx, fake.ArtistOpen)
	if err != nil || len(groups) != 6 {
		t.Fatalf("groups: %d %v", len(groups), err)
	}
	for _, g := range groups {
		if g.MBID == fake.GroupRemixes && (g.PrimaryType != "album" || strings.Join(g.SecondaryTypes, ",") != "compilation,remix") {
			t.Fatalf("types: %+v", g)
		}
	}
	rels, err := c.Releases(ctx, fake.ArtistOpen)
	if err != nil || len(rels) != 8 {
		t.Fatalf("releases: %d %v", len(rels), err)
	}
	for _, r := range rels {
		if r.MBID == fake.ReleaseDeluxe && (r.TrackCount() != 8 || len(r.Media) != 2 || r.ReleaseGroupMBID != fake.GroupFreeAlbum || r.HasTracks ||
			r.Status != "official" || r.Label != "Commons Records") {
			t.Fatalf("deluxe: %+v", r)
		}
	}
	rel, err := c.Release(ctx, fake.ReleaseDeluxe)
	if err != nil {
		t.Fatal(err)
	}
	if !rel.HasTracks || len(rel.Media[1].Tracks) != 3 || rel.Media[0].Tracks[2].Title != "Žuta Kuća" || rel.Media[0].Tracks[0].LengthMs == 0 ||
		rel.Media[0].Tracks[0].RecordingMBID == "" || rel.Media[0].Tracks[0].Artist != "Open Source Artist" {
		t.Fatalf("release: %+v", rel)
	}
	if _, err := c.Artist(ctx, "5a8e07d5-d932-4484-a7f7-e70079300000"); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("missing artist: %v", err)
	}
	if _, err := c.Artist(ctx, "../../etc"); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("bad id: %v", err)
	}
	// Answers are cached: the same lookup again sends nothing.
	n := f.Count()
	if _, err := c.Release(ctx, fake.ReleaseDeluxe); err != nil || f.Count() != n {
		t.Fatalf("cache: %d -> %d", n, f.Count())
	}
	for _, ua := range f.UserAgents {
		if !strings.HasPrefix(ua, "Locarr/") || !strings.Contains(ua, "( ops@example.org )") {
			t.Fatalf("user agent %q", ua)
		}
	}
}

func TestCoverArt(t *testing.T) {
	c, _ := newClient(t, true)
	b, ct, err := c.FetchImage(context.Background(), "500", musicbrainz.CoverName(fake.GroupFreeAlbum))
	if err != nil || ct != "image/jpeg" || len(b) < 100 {
		t.Fatalf("cover: %s %d %v", ct, len(b), err)
	}
	if _, _, err := c.FetchImage(context.Background(), "500", musicbrainz.CoverName(fake.GroupNextAlbum)); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("no cover: %v", err)
	}
	if _, _, err := c.FetchImage(context.Background(), "77", musicbrainz.CoverName(fake.GroupFreeAlbum)); !errors.Is(err, metadata.ErrBadImage) {
		t.Fatalf("bad size: %v", err)
	}
	if b, err := c.FetchCover(context.Background(), fake.ReleaseDeluxe, fake.GroupFreeAlbum, "250"); err != nil || len(b) == 0 {
		t.Fatalf("release cover: %v", err)
	}
}

// TestRateLimit proves the client never sends MusicBrainz more than one
// request per second, even when many lookups run at once: the fake refuses
// (503) any request closer than 990 ms to the previous one.
func TestRateLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("takes a few seconds")
	}
	c, f := newClient(t, false)
	f.MinInterval = 990 * time.Millisecond
	ctx := context.Background()
	ids := []string{fake.ArtistOpen, fake.ArtistOther, fake.ReleaseDeluxe, fake.ReleaseStandard}
	var wg sync.WaitGroup
	errs := make([]error, len(ids))
	start := time.Now()
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i < 2 {
				_, errs[i] = c.Artist(ctx, id)
			} else {
				_, errs[i] = c.Release(ctx, id)
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.Violations != 0 {
		t.Fatalf("%d requests came faster than one per second", f.Violations)
	}
	for i := 1; i < len(f.Requests); i++ {
		if gap := f.Requests[i].Sub(f.Requests[i-1]); gap < 990*time.Millisecond {
			t.Fatalf("gap %v between requests %d and %d", gap, i-1, i)
		}
	}
	if took := time.Since(start); took < 2900*time.Millisecond {
		t.Fatalf("4 requests in %v", took)
	}
}

// TestRetryAfter503 shows a 503 makes the client wait and try again.
func TestRetryAfter503(t *testing.T) {
	c, f := newClient(t, true)
	f.MinInterval = 300 * time.Millisecond
	ctx := context.Background()
	if _, err := c.Artist(ctx, fake.ArtistOpen); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Artist(ctx, fake.ArtistOther); err != nil {
		t.Fatal(err)
	}
	if f.Violations != 1 || f.Count() != 3 {
		t.Fatalf("violations %d requests %d", f.Violations, f.Count())
	}
}
