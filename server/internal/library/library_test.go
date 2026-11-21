package library

import (
	"testing"
	"time"
)

func TestRefs(t *testing.T) {
	r, err := ParseRef("movie:42")
	if err != nil || r != MovieRef(42) || r.String() != "movie:42" {
		t.Fatal(r, err)
	}
	if string(r.JSON()) != `{"ref":"movie:42"}` || RefFromJSON(r.JSON()) != r {
		t.Fatal(string(r.JSON()))
	}
	for _, bad := range []string{"", "movie", "movie:", "movie:0", "movie:-1", "film:1", "movie:1x"} {
		if _, err := ParseRef(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	if !RefFromJSON([]byte(`{}`)).IsZero() || !RefFromJSON([]byte(`garbage`)).IsZero() {
		t.Fatal("bad JSON must give a zero ref")
	}
}

func TestAvailability(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	d := func(s string) time.Time { return ParseDate(s) }
	cinemaOnly := ReleaseDates{InCinemas: d("2026-05-01")}
	future := ReleaseDates{InCinemas: d("2026-09-01")}
	home := ReleaseDates{InCinemas: d("2026-01-01"), Digital: d("2026-05-20")}
	cases := []struct {
		name  string
		d     ReleaseDates
		min   Availability
		delay time.Duration
		want  bool
	}{
		{"announced is always available", future, Announced, 0, true},
		{"in cinemas once the cinema date passed", cinemaOnly, InCinemas, 0, true},
		{"not in cinemas before the date", future, InCinemas, 0, false},
		{"released needs a home release", cinemaOnly, Released, 0, false},
		{"released by digital date", home, Released, 0, true},
		{"released guessed 90 days after cinemas", ReleaseDates{InCinemas: d("2026-02-01")}, Released, 0, true},
		{"delay pushes availability back", home, Released, 30 * 24 * time.Hour, false},
		{"negative delay searches early", ReleaseDates{Digital: d("2026-06-05")}, Released, -7 * 24 * time.Hour, true},
		{"no dates, released status", ReleaseDates{Released: true}, Released, 0, true},
		{"no dates, not released", ReleaseDates{}, InCinemas, 0, false},
		{"physical counts too", ReleaseDates{Physical: d("2026-05-31")}, Released, 0, true},
	}
	for _, c := range cases {
		if got := IsAvailable(c.d, c.min, c.delay, now); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
	if at := AvailableFrom(cinemaOnly, Released, 0); !at.Equal(d("2026-05-01").Add(TheatricalToRelease)) {
		t.Fatal(at)
	}
}
