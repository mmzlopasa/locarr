// Package fake is a small ListenBrainz for tests and the dev stack, written
// from the public documentation:
//
//	GET {prefix}/similar-artists/json?artist_mbids=…&algorithm=…   (labs)
//	GET {prefix}/1/popularity/top-release-groups-for-artist/{mbid}
//
// Both are served from one handler (point the API and labs URLs at the same prefix).
package fake

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	mbfake "github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz/fake"
)

// Similar is a catalog entry.
type Similar struct {
	MBID, Name string
	Score      float64
}

// Catalog.
var (
	SimilarTo = map[string][]Similar{
		mbfake.ArtistOpen:  {{MBID: mbfake.ArtistChoir, Name: "Creative Commons Choir", Score: 812}, {MBID: mbfake.ArtistOther, Name: "Open Source Artist", Score: 203}},
		mbfake.ArtistChoir: {{MBID: mbfake.ArtistOpen, Name: "Open Source Artist", Score: 640}},
	}
	TopGroups = map[string]string{mbfake.ArtistChoir: mbfake.GroupChoral, mbfake.ArtistOther: mbfake.GroupTrioAlbum, mbfake.ArtistOpen: mbfake.GroupFreeAlbum}
)

// Server is the fake.
type Server struct {
	mu sync.Mutex
	// MinInterval answers 429 to requests closer together than this.
	MinInterval time.Duration
	Requests    []time.Time
	UserAgents  []string
	Violations  int
}

// New returns a fake.
func New() *Server { return &Server{} }

// Count is the number of requests so far.
func (s *Server) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Requests)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	s.mu.Lock()
	tooFast := s.MinInterval > 0 && len(s.Requests) > 0 && now.Sub(s.Requests[len(s.Requests)-1]) < s.MinInterval
	s.Requests = append(s.Requests, now)
	s.UserAgents = append(s.UserAgents, r.Header.Get("User-Agent"))
	if tooFast {
		s.Violations++
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if tooFast {
		w.Header().Set("X-RateLimit-Reset-In", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":429,"error":"Too Many Requests"}`))
		return
	}
	p := r.URL.Path
	switch {
	case strings.HasSuffix(p, "/similar-artists/json"):
		id := strings.ToLower(r.URL.Query().Get("artist_mbids"))
		if r.URL.Query().Get("algorithm") == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"algorithm is required"}`))
			return
		}
		out := []map[string]any{}
		for _, a := range SimilarTo[id] {
			out = append(out, map[string]any{"artist_mbid": a.MBID, "name": a.Name, "comment": "", "type": "Group", "gender": nil,
				"score": a.Score, "reference_mbid": id})
		}
		_ = json.NewEncoder(w).Encode(out)
	case strings.Contains(p, "/1/popularity/top-release-groups-for-artist/"):
		id := strings.ToLower(p[strings.LastIndex(p, "/")+1:])
		out := []map[string]any{}
		if g, ok := TopGroups[id]; ok {
			out = append(out, map[string]any{"release_group_mbid": g, "release_group": map[string]any{"type": "Album", "caa_id": 1, "name": "x"},
				"total_listen_count": 10, "total_user_count": 2})
		}
		_ = json.NewEncoder(w).Encode(out)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"error":"Not found"}`))
	}
}
