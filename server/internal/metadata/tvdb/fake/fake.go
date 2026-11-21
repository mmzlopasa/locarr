// Package fake is a small TVDB v4-compatible server for tests, written from
// TVDB's public v4 API documentation: /v4/login, /v4/search,
// /v4/series/{id}/extended, /v4/series/{id}/episodes/default and
// /v4/search/remoteid/{id}. It serves one anime series with absolute numbers.
package fake

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// APIKey and PIN are the credentials the fake accepts.
const (
	APIKey = "fake-tvdb-project-key" // #nosec G101 -- a fake key for tests
	PIN    = "1234"
	token  = "fake-tvdb-token" // #nosec G101 -- a fake token for tests
)

// SeriesID is the fake's series (matches the fake TMDb's "Spring Kurenai").
const SeriesID = 454545

// Server is the fake TVDB.
type Server struct {
	mu     sync.Mutex
	Logins int
	// Expire makes the next authenticated request answer 401 once.
	Expire bool
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/tvdb")
	if path == "/v4/login" && r.Method == http.MethodPost {
		var body struct {
			APIKey string `json:"apikey"`
			PIN    string `json:"pin"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.APIKey != APIKey || (body.PIN != "" && body.PIN != PIN) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"status": "failure", "message": "Unauthorized"})
			return
		}
		s.mu.Lock()
		s.Logins++
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "data": map[string]any{"token": token}})
		return
	}
	s.mu.Lock()
	expire := s.Expire
	s.Expire = false
	s.mu.Unlock()
	if expire || r.Header.Get("Authorization") != "Bearer "+token {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"status": "failure", "message": "Unauthorized"})
		return
	}
	switch {
	case path == "/v4/search":
		q := strings.ToLower(r.URL.Query().Get("query"))
		data := []map[string]any{}
		if strings.Contains("spring kurenai haru kurenai", q) && q != "" {
			data = append(data, map[string]any{"tvdb_id": "454545", "name": "Spring Kurenai", "year": "2025", "first_air_time": "2025-01-05",
				"overview": "A fictional anime (fake TVDB).", "network": "Open Anime", "status": "Ended", "type": "series"})
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "data": data})
	case path == "/v4/series/454545/extended":
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "data": map[string]any{
			"id": SeriesID, "name": "Haru Kurenai", "overview": "A fictional anime (fake TVDB).", "firstAired": "2025-01-05", "lastAired": "2025-03-30",
			"year": "2025", "originalLanguage": "jpn", "originalCountry": "jpn", "airsTime": "23:30", "averageRuntime": 24,
			"status":          map[string]any{"name": "Ended"},
			"originalNetwork": map[string]any{"name": "Open Anime", "country": "jp"},
			"genres":          []map[string]any{{"name": "Animation"}, {"name": "Anime"}},
			"aliases":         []map[string]any{{"language": "eng", "name": "Spring Kurenai"}, {"language": "eng", "name": "Kurenai"}},
			"remoteIds": []map[string]any{{"id": "tt9100454", "sourceName": "IMDB"}, {"id": "800454", "sourceName": "TheMovieDB.com"},
				{"id": "99454", "sourceName": "TV Maze"}},
			"translations": map[string]any{"nameTranslations": []map[string]any{{"language": "eng", "name": "Spring Kurenai", "overview": "A fictional anime (fake TVDB)."}}},
		}})
	case path == "/v4/series/454545/episodes/default":
		page := r.URL.Query().Get("page")
		eps := []map[string]any{}
		var next any
		switch page {
		case "", "0":
			eps = append(eps,
				map[string]any{"id": 9001, "seasonNumber": 1, "number": 1, "absoluteNumber": 1, "name": "Spring 1", "aired": "2025-01-05", "runtime": 24},
				map[string]any{"id": 9002, "seasonNumber": 1, "number": 2, "absoluteNumber": 2, "name": "Spring 2", "aired": "2025-01-12", "runtime": 24},
				map[string]any{"id": 9000, "seasonNumber": 0, "number": 1, "absoluteNumber": 0, "name": "OVA", "aired": "2025-02-01", "runtime": 24})
			next = "https://api4.thetvdb.com/v4/series/454545/episodes/default?page=1"
		case "1":
			eps = append(eps,
				map[string]any{"id": 9003, "seasonNumber": 2, "number": 1, "absoluteNumber": 3, "name": "Bloom 1", "aired": "2025-03-23", "runtime": 24},
				map[string]any{"id": 9004, "seasonNumber": 2, "number": 2, "absoluteNumber": 4, "name": "Bloom 2", "aired": "2025-03-30", "runtime": 24})
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "data": map[string]any{"episodes": eps}, "links": map[string]any{"next": next}})
	case path == "/v4/search/remoteid/tt9100454" || path == "/v4/search/remoteid/800454":
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "data": []map[string]any{{"series": map[string]any{"id": SeriesID}}}})
	case strings.HasPrefix(path, "/v4/search/remoteid/"):
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "data": []any{}})
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"status": "failure", "message": "NotFoundException"})
	}
}
