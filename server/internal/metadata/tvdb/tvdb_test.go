package tvdb

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/metadata/tvdb/fake"
)

func client(t *testing.T, key, pin string) (*Client, *fake.Server) {
	t.Helper()
	f := &fake.Server{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := New(srv.URL, func(context.Context) (Credentials, error) { return Credentials{Key: key, PIN: pin}, nil }, nil)
	return c, f
}

func TestSeriesWithAbsoluteNumbersAndAirTime(t *testing.T) {
	c, f := client(t, fake.APIKey, fake.PIN)
	ctx := context.Background()
	list, err := c.SearchSeries(ctx, "kurenai", 0)
	if err != nil || len(list) != 1 || list[0].TVDbID != fake.SeriesID || list[0].Status != metadata.SeriesEnded {
		t.Fatalf("search: %+v %v", list, err)
	}
	s, err := c.Series(ctx, fake.SeriesID)
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "Spring Kurenai" || s.OriginalTitle != "Haru Kurenai" || s.TMDbID != 800454 || s.IMDbID != "tt9100454" || s.TVMazeID != 99454 {
		t.Fatalf("ids/titles: %+v", s.SeriesSummary)
	}
	if s.TimeZone != "Asia/Tokyo" || s.AirTime != "23:30" || s.Runtime != 24 {
		t.Fatalf("zone %q time %q", s.TimeZone, s.AirTime)
	}
	if len(s.Seasons) != 3 || s.Seasons[0].Number != 0 || s.Seasons[2].Episodes[1].Absolute != 4 {
		t.Fatalf("seasons (both pages): %+v", s.Seasons)
	}
	at, ok := metadata.AirTimeUTC(s.Seasons[1].Episodes[0].AirDate, s.AirTime, s.TimeZone)
	if !ok || !at.Equal(time.Date(2025, 1, 5, 14, 30, 0, 0, time.UTC)) {
		t.Fatalf("air time UTC %v", at)
	}
	if id, err := c.FindSeries(ctx, "imdb", "tt9100454"); err != nil || id != fake.SeriesID {
		t.Fatalf("find: %d %v", id, err)
	}
	if _, err := c.FindSeries(ctx, "imdb", "tt0000001"); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("find unknown: %v", err)
	}
	// An expired token is renewed once.
	f.Expire = true
	if _, err := c.SearchSeries(ctx, "spring", 0); err != nil {
		t.Fatal(err)
	}
	if f.Logins != 2 {
		t.Fatalf("logins %d", f.Logins)
	}
}

func TestBadKey(t *testing.T) {
	c, _ := client(t, "wrong", "")
	if _, err := c.SearchSeries(context.Background(), "x", 0); !errors.Is(err, metadata.ErrInvalidKey) {
		t.Fatalf("err %v", err)
	}
	c2, _ := client(t, "", "")
	if _, err := c2.Series(context.Background(), 1); !errors.Is(err, metadata.ErrNoKey) {
		t.Fatalf("err %v", err)
	}
}
