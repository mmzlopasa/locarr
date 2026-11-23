package channels

import (
	"strings"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/notify"
)

func TestComposeLanguages(t *testing.T) {
	ev := notify.Event{Type: notify.RequestDeclined, Title: "Dune (2021)", Message: "already owned", RequestID: 3,
		Actor: &notify.User{ID: 1, Username: "admin"}, Params: map[string]any{"seasons": []any{float64(1), float64(2)}, "is4k": true}}
	en := Compose(ev, "en", "https://l.test/")
	if en.Headline != "Request declined: Dune (2021) (4K)" || en.URL != "https://l.test/requests/3" {
		t.Fatalf("en %+v", en)
	}
	for _, want := range []string{"Reason: already owned", "Seasons: 1, 2", "By: admin"} {
		if !strings.Contains(en.Body, want) {
			t.Errorf("en body %q lacks %q", en.Body, want)
		}
	}
	hr := Compose(ev, "hr", "https://l.test")
	if hr.Headline != "Zahtjev odbijen: Dune (2021) (4K)" {
		t.Fatalf("hr %q", hr.Headline)
	}
	for _, want := range []string{"Razlog: already owned", "Sezone: 1, 2", "Korisnik: admin"} {
		if !strings.Contains(hr.Body, want) {
			t.Errorf("hr body %q lacks %q", hr.Body, want)
		}
	}
	// Every known type has its own headline in both languages.
	all := append(append([]notify.Type{}, notify.Types...), notify.LibraryTypes...)
	all = append(all, notify.Test)
	for _, l := range []string{"en", "hr"} {
		for _, typ := range all {
			m := Compose(notify.Event{Type: typ, Title: "X"}, l, "")
			if strings.HasPrefix(m.Headline, string(typ)) || m.URL != "" {
				t.Errorf("%s %s: %+v", l, typ, m)
			}
		}
	}
	if m := Compose(notify.Event{Type: notify.Test}, "hr", "http://l"); m.Headline != "Probna poruka iz Locarra" || m.URL != "http://l/" ||
		m.Body != "Ako ovo vidite, kanal radi." {
		t.Fatalf("test hr %+v", m)
	}
	if m := Compose(notify.Event{Type: notify.Test}, "", ""); m.Headline != "Test message from Locarr" {
		t.Fatalf("test en %+v", m)
	}
}

func TestComposeLinksAndPosters(t *testing.T) {
	for _, c := range []struct {
		ev   notify.Event
		want string
	}{
		{notify.Event{Type: notify.IssueCreated, IssueID: 9}, "http://l/issues/9"},
		{notify.Event{Type: notify.LibraryImported, ItemRef: "movie:4"}, "http://l/movies/4"},
		{notify.Event{Type: notify.LibraryUpgraded, ItemRef: "series:5"}, "http://l/series/5"},
		{notify.Event{Type: notify.LibraryImported, ItemRef: "album:6"}, "http://l/music/albums/6"},
		{notify.Event{Type: notify.LibraryGrabbed, ItemRef: "artist:7"}, "http://l/music/artists/7"},
		{notify.Event{Type: notify.LibraryDownloadFailed, ItemRef: "episode:8"}, "http://l/activity/queue"},
		{notify.Event{Type: notify.HealthWarning}, "http://l/system/health"},
	} {
		if got := Compose(c.ev, "en", "http://l").URL; got != c.want {
			t.Errorf("%+v: %q, want %q", c.ev, got, c.want)
		}
	}
	for in, want := range map[string]string{
		"/abc.jpg": "https://image.tmdb.org/t/p/w342/abc.jpg",
		"rg-0d4a7a2c-1f39-4b2b-9b5a-3c3c1d4e5f60.jpg": "https://coverartarchive.org/release-group/0d4a7a2c-1f39-4b2b-9b5a-3c3c1d4e5f60/front-250",
		"":               "",
		"/../etc/passwd": "",
		"rg-bad.jpg":     "",
		"http://x/y.jpg": "",
	} {
		if got := PosterURL(in); got != want {
			t.Errorf("PosterURL(%q) = %q, want %q", in, got, want)
		}
	}
}
