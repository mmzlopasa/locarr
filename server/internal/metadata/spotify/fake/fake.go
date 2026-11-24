// Package fake is a small Spotify Web API and accounts service for tests and
// the dev stack, written from the public documentation:
//
//	POST {prefix}/api/token                         client credentials (Basic ClientID:ClientSecret)
//	GET  {prefix}/v1/search?q=…&type=artist&limit=…
//	GET  {prefix}/v1/artists/{id}/related-artists   403 unless Related is set, like for new apps
//
// Point the accounts URL at {prefix} and the API URL at {prefix}/v1.
package fake

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	mbfake "github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz/fake"
)

// Credentials the fake accepts, and the token it hands out.
const (
	ClientID     = "spotifyid"
	ClientSecret = "spotifysecret"
	token        = "fake-spotify-token" // #nosec G101 -- a test fake's token
)

// Artist is a catalog artist.
type Artist struct {
	ID, Name string
	Genres   []string
}

// Catalog.
var (
	Artists = []Artist{
		{ID: "4OpenSourceSpotifyId000", Name: "Open Source Artist", Genres: []string{"electronica"}},
		{ID: mbfake.SpotifyChoir, Name: "Creative Commons Choir", Genres: []string{"choral", "chamber choir"}},
		{ID: "4RadioheadSpotifyId0000", Name: "Radiohead", Genres: []string{"alternative rock"}},
	}
	RelatedTo = map[string][]string{"4OpenSourceSpotifyId000": {mbfake.SpotifyChoir}}
)

// Server is the fake.
type Server struct {
	mu sync.Mutex
	// Related serves related artists (an app with extended access); off by default.
	Related bool
	// Tokens counts issued tokens; Calls API requests.
	Tokens, Calls int
}

// New returns a fake.
func New() *Server { return &Server{} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func artistJSON(a Artist) map[string]any {
	return map[string]any{"id": a.ID, "name": a.Name, "genres": a.Genres, "type": "artist", "uri": "spotify:artist:" + a.ID,
		"external_urls": map[string]any{"spotify": "https://open.spotify.com/artist/" + a.ID}, "images": []any{}}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if strings.HasSuffix(p, "/api/token") {
		id, secret, ok := r.BasicAuth()
		_ = r.ParseForm()
		if !ok || id != ClientID || secret != ClientSecret || r.PostForm.Get("grant_type") != "client_credentials" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_client", "error_description": "Invalid client"})
			return
		}
		s.mu.Lock()
		s.Tokens++
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": 3600})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+token {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"status": 401, "message": "Invalid access token"}})
		return
	}
	s.mu.Lock()
	s.Calls++
	related := s.Related
	s.mu.Unlock()
	i := strings.Index(p, "/v1/")
	if i < 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"status": 404, "message": "Service not found"}})
		return
	}
	rest := p[i+len("/v1/"):]
	switch {
	case rest == "search":
		q := strings.ToLower(r.URL.Query().Get("q"))
		items := []map[string]any{}
		for _, a := range Artists {
			if q != "" && strings.Contains(strings.ToLower(a.Name), q) {
				items = append(items, artistJSON(a))
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"artists": map[string]any{"items": items, "total": len(items), "limit": 10, "offset": 0}})
	case strings.HasPrefix(rest, "artists/") && strings.HasSuffix(rest, "/related-artists"):
		if !related {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"status": 403, "message": "Forbidden"}})
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(rest, "artists/"), "/related-artists")
		list := []map[string]any{}
		for _, rid := range RelatedTo[id] {
			for _, a := range Artists {
				if a.ID == rid {
					list = append(list, artistJSON(a))
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"artists": list})
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"status": 404, "message": "Service not found"}})
	}
}
