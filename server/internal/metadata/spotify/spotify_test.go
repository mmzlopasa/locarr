package spotify_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
	mbfake "github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz/fake"
	"github.com/mmzlopasa/locarr/server/internal/metadata/spotify"
	"github.com/mmzlopasa/locarr/server/internal/metadata/spotify/fake"
)

func newClient(t *testing.T, cr spotify.Credentials) (*spotify.Client, *fake.Server) {
	t.Helper()
	f := fake.New()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := spotify.New(srv.URL+"/v1", srv.URL, func(context.Context) spotify.Credentials { return cr })
	c.Limiter = metadata.NewLimiter(1000, 1000)
	return c, f
}

var good = spotify.Credentials{ID: fake.ClientID, Secret: fake.ClientSecret}

func TestSearchAndToken(t *testing.T) {
	c, f := newClient(t, good)
	ctx := context.Background()
	list, err := c.SearchArtists(ctx, "Creative Commons Choir")
	if err != nil || len(list) != 1 || list[0].ID != mbfake.SpotifyChoir || list[0].Genres[0] != "choral" {
		t.Fatalf("search: %+v %v", list, err)
	}
	if list[0].URL() != "https://open.spotify.com/artist/"+mbfake.SpotifyChoir {
		t.Fatal(list[0].URL())
	}
	if _, err := c.SearchArtists(ctx, "Radiohead"); err != nil {
		t.Fatal(err)
	}
	if f.Tokens != 1 {
		t.Fatalf("token reused: %d", f.Tokens)
	}
}

// New apps get 403 for related artists (Spotify, November 2024).
func TestRelatedRestricted(t *testing.T) {
	c, f := newClient(t, good)
	ctx := context.Background()
	if _, err := c.Related(ctx, "4OpenSourceSpotifyId000"); !errors.Is(err, spotify.ErrRestricted) {
		t.Fatalf("restricted: %v", err)
	}
	caps, err := c.Validate(ctx, good)
	if err != nil || caps.Related {
		t.Fatalf("validate: %+v %v", caps, err)
	}
	f.Related = true
	list, err := c.Related(ctx, "4OpenSourceSpotifyId000")
	if err != nil || len(list) != 1 || list[0].ID != mbfake.SpotifyChoir {
		t.Fatalf("related: %+v %v", list, err)
	}
	caps, err = c.Validate(ctx, good)
	if err != nil || !caps.Related {
		t.Fatalf("validate extended: %+v %v", caps, err)
	}
}

func TestCredentials(t *testing.T) {
	c, _ := newClient(t, spotify.Credentials{})
	ctx := context.Background()
	if _, err := c.SearchArtists(ctx, "x"); !errors.Is(err, metadata.ErrNoKey) {
		t.Fatalf("none: %v", err)
	}
	if _, err := c.Validate(ctx, spotify.Credentials{ID: "a", Secret: "b"}); !errors.Is(err, metadata.ErrInvalidKey) {
		t.Fatalf("bad: %v", err)
	}
}
