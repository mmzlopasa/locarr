package compat

import (
	"encoding/json"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/discover"
	"github.com/mmzlopasa/locarr/server/internal/requests"
)

func TestStatusCodes(t *testing.T) {
	for st, want := range map[discover.Status]int{discover.None: 1, "": 1, discover.Pending: 2, discover.Processing: 3, discover.InLibrary: 3,
		discover.Partial: 4, discover.Available: 5} {
		if got := mediaStatus(st); got != want {
			t.Errorf("media %q: %d, want %d", st, got, want)
		}
	}
	for st, want := range map[requests.Status]int{requests.StatusPending: 1, requests.StatusApproved: 2, requests.StatusProcessing: 2,
		requests.StatusFailed: 2, requests.StatusAvailable: 2, requests.StatusDeclined: 3, requests.StatusCancelled: 3} {
		if got := requestStatus(st); got != want {
			t.Errorf("request %q: %d, want %d", st, got, want)
		}
	}
}

func TestParseSeasons(t *testing.T) {
	for raw, want := range map[string]bool{``: true, `null`: true, `"all"`: true, `[1,2]`: false} {
		all, _, err := parseSeasons(json.RawMessage(raw))
		if err != nil || all != want {
			t.Errorf("%s: all=%v err=%v", raw, all, err)
		}
	}
	if _, _, err := parseSeasons(json.RawMessage(`"some"`)); err == nil {
		t.Error("bad seasons accepted")
	}
}

func TestImages(t *testing.T) {
	if got := posterImages("abc.jpg"); len(got) != 1 || got[0].RemoteURL != "https://image.tmdb.org/t/p/w500/abc.jpg" {
		t.Errorf("tmdb: %+v", got)
	}
	if got := posterImages("https://artworks.example/p.jpg"); len(got) != 1 || got[0].RemoteURL != "https://artworks.example/p.jpg" {
		t.Errorf("remote: %+v", got)
	}
	for _, p := range []string{"", "a/b.jpg", "x.jpg?y", "http://plain/p.jpg"} {
		if got := posterImages(p); len(got) != 0 {
			t.Errorf("%q: %+v", p, got)
		}
	}
	if tmdbPath("/abc.jpg") != "/abc.jpg" || tmdbPath("abc.jpg") != "/abc.jpg" || tmdbPath("") != "" {
		t.Error("tmdb path")
	}
}
