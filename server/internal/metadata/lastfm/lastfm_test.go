package lastfm_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/metadata/lastfm"
	"github.com/mmzlopasa/locarr/server/internal/metadata/lastfm/fake"
	mbfake "github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz/fake"
)

func newClient(t *testing.T, key string) (*lastfm.Client, *fake.Server) {
	t.Helper()
	f := fake.New()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := lastfm.New(srv.URL+"/2.0/", func(context.Context) string { return key })
	c.Limiter = metadata.NewLimiter(1000, 1000)
	c.RateLimitPause = 10 * time.Millisecond
	return c, f
}

func TestSimilarByMBIDAndName(t *testing.T) {
	c, _ := newClient(t, fake.Key)
	ctx := context.Background()
	list, err := c.Similar(ctx, mbfake.ArtistOpen, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "Creative Commons Choir" || list[0].MBID != mbfake.ArtistChoir || list[0].Match != 0.92 {
		t.Fatalf("similar: %+v", list)
	}
	if list[1].MBID != "" || list[1].Match != 0.61 {
		t.Fatalf("no mbid: %+v", list[1])
	}
	// An id Last.fm does not know falls back to the name.
	byName, err := c.Similar(ctx, "00000000-0000-0000-0000-000000000000", "Creative Commons Choir", 50)
	if err != nil || len(byName) != 1 || byName[0].MBID != mbfake.ArtistOpen {
		t.Fatalf("by name: %+v %v", byName, err)
	}
	if _, err := c.Similar(ctx, "", "Nobody At All", 50); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("unknown artist: %v", err)
	}
	tags, err := c.TopTags(ctx, mbfake.ArtistChoir, "")
	if err != nil || len(tags) != 3 || tags[0].Name != "choral" || tags[0].Count != 100 || tags[2].Name != "dream pop" {
		t.Fatalf("tags: %+v %v", tags, err)
	}
}

func TestKeys(t *testing.T) {
	c, _ := newClient(t, "")
	ctx := context.Background()
	if _, err := c.Similar(ctx, mbfake.ArtistOpen, "", 10); !errors.Is(err, metadata.ErrNoKey) {
		t.Fatalf("no key: %v", err)
	}
	bad, _ := newClient(t, "wrong")
	if _, err := bad.Similar(ctx, mbfake.ArtistOpen, "", 10); !errors.Is(err, metadata.ErrInvalidKey) {
		t.Fatalf("bad key: %v", err)
	}
	if err := c.Validate(ctx, fake.Key); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := c.Validate(ctx, "wrong"); !errors.Is(err, metadata.ErrInvalidKey) {
		t.Fatalf("validate bad: %v", err)
	}
}

func TestRateLimitError(t *testing.T) {
	c, f := newClient(t, fake.Key)
	f.RateLimited = 2
	list, err := c.Similar(context.Background(), mbfake.ArtistOpen, "", 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("after error 29: %+v %v", list, err)
	}
	if f.Count() != 3 {
		t.Fatalf("requests: %d", f.Count())
	}
	f.RateLimited = 5
	if _, err := c.Similar(context.Background(), mbfake.ArtistOpen, "", 10); !errors.Is(err, metadata.ErrRateLimited) {
		t.Fatalf("still limited: %v", err)
	}
}

// Last.fm sends a lone object instead of a one-element list, and numbers as strings.
func TestLoneObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"similarartists":{"artist":{"name":"Solo","mbid":"","match":"1","url":"https://www.last.fm/music/Solo"},"@attr":{"artist":"X"}}}`))
	}))
	defer srv.Close()
	c := lastfm.New(srv.URL, func(context.Context) string { return "k" })
	list, err := c.Similar(context.Background(), "", "X", 5)
	if err != nil || len(list) != 1 || list[0].Name != "Solo" || list[0].Match != 1 {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestLimiterSpacing(t *testing.T) {
	c, f := newClient(t, fake.Key)
	c.Limiter = metadata.NewLimiter(20, 1)
	for i := 0; i < 4; i++ {
		if _, err := c.TopTags(context.Background(), mbfake.ArtistOpen, ""); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i < len(f.Requests); i++ {
		if gap := f.Requests[i].Sub(f.Requests[i-1]); gap < 40*time.Millisecond {
			t.Fatalf("gap %v", gap)
		}
	}
}

func TestArtistURL(t *testing.T) {
	if got := lastfm.ArtistURL("Sigur Rós & Friends"); got != "https://www.last.fm/music/Sigur+R%C3%B3s+&+Friends" {
		t.Fatal(got)
	}
}
