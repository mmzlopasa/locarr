// Package fake is a small Last.fm API (artist.getSimilar, artist.getTopTags)
// for tests and the dev stack, written from the public documentation. The
// key is Key; anything else answers error 10. It is mounted anywhere: every
// request goes to the path it is mounted at, with ?method=…
package fake

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	mbfake "github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz/fake"
)

// Key is the accepted API key.
const Key = "lastfmkey"

// Similar is one similar artist of the catalog.
type Similar struct {
	Name, MBID string
	Match      float64
}

// Catalog: similar artists and tags by artist name (lower case).
var (
	SimilarTo = map[string][]Similar{
		"open source artist": {
			{Name: "Creative Commons Choir", MBID: mbfake.ArtistChoir, Match: 0.92},
			// Not in MusicBrainz: the name does not resolve and the artist is left out.
			{Name: "Public Domain Orchestra", Match: 0.61},
		},
		"creative commons choir": {{Name: "Open Source Artist", MBID: mbfake.ArtistOpen, Match: 0.88}},
	}
	Tags = map[string][]string{
		"open source artist":     {"electronic", "ambient"},
		"creative commons choir": {"choral", "ambient", "dream pop"},
		"radiohead":              {"alternative"},
	}
	names = map[string]string{mbfake.ArtistOpen: "open source artist", mbfake.ArtistChoir: "creative commons choir"}
)

// Server is the fake.
type Server struct {
	mu sync.Mutex
	// Requests are the arrival times.
	Requests []time.Time
	// RateLimited answers error 29 to that many requests first.
	RateLimited int
}

// New returns a fake.
func New() *Server { return &Server{} }

// Count is the number of requests so far.
func (s *Server) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Requests)
}

func fail(w http.ResponseWriter, status, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": code, "message": msg, "links": []any{}})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.mu.Lock()
	s.Requests = append(s.Requests, time.Now())
	limited := s.RateLimited > 0
	if limited {
		s.RateLimited--
	}
	s.mu.Unlock()
	if limited {
		fail(w, http.StatusTooManyRequests, 29, "Rate Limit Exceeded")
		return
	}
	if q.Get("api_key") != Key {
		fail(w, http.StatusForbidden, 10, "Invalid API key - You must be granted a valid key by last.fm")
		return
	}
	if q.Get("format") != "json" {
		fail(w, http.StatusBadRequest, 5, "Invalid format")
		return
	}
	name := strings.ToLower(strings.TrimSpace(q.Get("artist")))
	if id := strings.ToLower(q.Get("mbid")); id != "" {
		name = names[id]
	}
	_, known := Tags[name]
	if name == "" || !known {
		fail(w, http.StatusOK, 6, "The artist you supplied could not be found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch strings.ToLower(q.Get("method")) {
	case "artist.getsimilar":
		list := []map[string]any{}
		for _, a := range SimilarTo[name] {
			list = append(list, map[string]any{"name": a.Name, "mbid": a.MBID, "match": strings.TrimRight(strings.TrimRight(
				jsonFloat(a.Match), "0"), "."), "url": "https://www.last.fm/music/" + strings.ReplaceAll(a.Name, " ", "+"),
				"image": []map[string]any{{"#text": "", "size": "small"}}, "streamable": "0"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"similarartists": map[string]any{"artist": list, "@attr": map[string]any{"artist": name}}})
	case "artist.gettoptags":
		list := []map[string]any{}
		for i, t := range Tags[name] {
			list = append(list, map[string]any{"count": 100 - 20*i, "name": t, "url": "https://www.last.fm/tag/" + t})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"toptags": map[string]any{"tag": list, "@attr": map[string]any{"artist": name}}})
	default:
		fail(w, http.StatusBadRequest, 3, "Invalid Method - No method with that name in this package")
	}
}

func jsonFloat(f float64) string {
	b, _ := json.Marshal(f)
	s := string(b)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}
