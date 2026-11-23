// Package fake is a small TMDb-compatible server for tests and manual
// testing, written from TMDb's public API documentation. It serves the v3
// endpoints Locarr uses and tiny generated posters:
//
//	/3/configuration  /3/search/movie  /3/movie/{id}  /3/find/{imdb}
//	/3/collection/{id}  /3/list/{id}  /t/p/{size}/{file}
//	/3/search/tv  /3/tv/{id}  /3/tv/{id}/season/{n}
//	and the discover endpoints (discover.go)
//
// The catalog matches the fake indexer's movies (internal/indexers/fake).
package fake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// APIKey is the v3 key the fake accepts; ReadToken is the v4 token.
const (
	APIKey    = "fake0000000000000000000000tmdb01"
	ReadToken = "eyJhbGciOiJIUzI1NiJ9.eyJhdWQiOiJmYWtlLXRtZGIifQ.ZmFrZS1zaWduYXR1cmUtZm9yLXRlc3RzLW9ubHk" // #nosec G101 -- a fake token for tests
)

// Movie is a catalog movie.
type Movie struct {
	ID               int64
	IMDb             string
	Title            string
	OriginalTitle    string
	OriginalLanguage string
	Overview         string
	Runtime          int
	Status           string
	Genres           []string
	Studio           string
	Theatrical       string // YYYY-MM-DD ("" = none)
	Digital          string
	Physical         string
	Collection       int64
	AltTitles        []string
	Popularity       float64
}

// CollectionID is the fake collection holding the open movies.
const CollectionID = 9001

// ListID is the fake public list.
const ListID = "8100"

// Catalog is the fake's movies. Dates relative to now are filled in by New.
func catalog(now time.Time) []Movie {
	future := now.AddDate(0, 3, 0).Format("2006-01-02")
	recent := now.AddDate(0, 0, -10).Format("2006-01-02")
	return []Movie{
		{ID: 10378, IMDb: "tt1254207", Title: "Big Buck Bunny", OriginalLanguage: "en", Runtime: 10, Status: "Released",
			Overview: "A large and lovable rabbit deals with three tiny bullies, led by a flying squirrel.",
			Genres:   []string{"Animation", "Comedy"}, Studio: "Blender Foundation", Theatrical: "2008-05-30", Digital: "2008-05-30",
			Collection: CollectionID, Popularity: 12.5},
		{ID: 45745, IMDb: "tt1727587", Title: "Sintel", OriginalLanguage: "en", Runtime: 15, Status: "Released",
			Overview: "A lonely young woman helps and befriends a dragon, whom she calls Scales.",
			Genres:   []string{"Animation", "Fantasy"}, Studio: "Blender Foundation", Theatrical: "2010-09-27", Digital: "2010-09-30",
			Collection: CollectionID, Popularity: 10.1, AltTitles: []string{"Durian"}},
		{ID: 133701, IMDb: "tt2285752", Title: "Tears of Steel", OriginalLanguage: "en", Runtime: 12, Status: "Released",
			Overview: "A group of warriors and scientists gather at the Oude Kerk in Amsterdam to stage a crucial event from the past.",
			Genres:   []string{"Science Fiction"}, Studio: "Blender Foundation", Theatrical: "2012-09-26", Physical: "2012-11-01",
			Collection: CollectionID, Popularity: 8.7},
		{ID: 900001, IMDb: "tt9000001", Title: "The Open Horizon", OriginalLanguage: "en", Runtime: 95, Status: "In Production",
			Overview: "A fictional upcoming open movie (fake catalog): announced, in cinemas in three months.",
			Genres:   []string{"Adventure"}, Studio: "Open Studio", Theatrical: future, Collection: CollectionID, Popularity: 3.2},
		{ID: 900002, IMDb: "tt9000002", Title: "Kino Platno", OriginalTitle: "Kino platno", OriginalLanguage: "hr", Runtime: 101,
			Status: "Released", Overview: "A fictional Croatian film (fake catalog) that is in cinemas now, with no home release yet.",
			Genres: []string{"Drama"}, Studio: "Zagreb Film", Theatrical: recent, Popularity: 2.1},
	}
}

// Server is the fake TMDb.
type Server struct {
	mu       sync.Mutex
	Movies   []Movie
	Series   []TVSeries
	Requests map[string]int // path -> count
	// ImageBase is advertised by /3/configuration (default: this server).
	ImageBase string
}

// New returns a fake with the default catalog.
func New() *Server {
	now := time.Now().UTC()
	return &Server{Movies: catalog(now), Series: tvCatalog(now), Requests: map[string]int{}}
}

// Count returns how often a path was requested.
func (s *Server) Count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Requests[path]
}

func (s *Server) find(id int64) (Movie, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.Movies {
		if m.ID == id {
			return m, true
		}
	}
	return Movie{}, false
}

func poster(id int64) string   { return fmt.Sprintf("poster%d.png", id) }
func backdrop(id int64) string { return fmt.Sprintf("backdrop%d.png", id) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "status_code": 34, "status_message": "The resource you requested could not be found."})
}

func authorized(r *http.Request) bool {
	if r.URL.Query().Get("api_key") == APIKey {
		return true
	}
	return r.Header.Get("Authorization") == "Bearer "+ReadToken
}

func releaseDate(m Movie) string {
	for _, d := range []string{m.Theatrical, m.Digital, m.Physical} {
		if d != "" {
			return d
		}
	}
	return ""
}

func (m Movie) result() map[string]any {
	orig := m.OriginalTitle
	if orig == "" {
		orig = m.Title
	}
	return map[string]any{"id": m.ID, "title": m.Title, "original_title": orig, "release_date": releaseDate(m), "overview": m.Overview,
		"poster_path": "/" + poster(m.ID), "backdrop_path": "/" + backdrop(m.ID), "popularity": m.Popularity, "vote_average": 7.1,
		"media_type": "movie", "adult": false}
}

func (s *Server) details(m Movie) map[string]any {
	out := m.result()
	out["imdb_id"] = m.IMDb
	out["original_language"] = m.OriginalLanguage
	out["runtime"] = m.Runtime
	out["status"] = m.Status
	out["vote_count"] = 321
	var genres []map[string]any
	for i, g := range m.Genres {
		genres = append(genres, map[string]any{"id": i + 1, "name": g})
	}
	out["genres"] = genres
	out["production_companies"] = []map[string]any{{"id": 1, "name": m.Studio}}
	if m.Collection != 0 {
		out["belongs_to_collection"] = map[string]any{"id": m.Collection, "name": "Open Movies Collection"}
	}
	var dates []map[string]any
	add := func(d string, typ int) {
		if d != "" {
			dates = append(dates, map[string]any{"certification": "PG", "release_date": d + "T00:00:00.000Z", "type": typ})
		}
	}
	add(m.Theatrical, 3)
	add(m.Digital, 4)
	add(m.Physical, 5)
	out["release_dates"] = map[string]any{"results": []map[string]any{{"iso_3166_1": "US", "release_dates": dates}}}
	var alts []map[string]any
	for _, a := range m.AltTitles {
		alts = append(alts, map[string]any{"iso_3166_1": "US", "title": a})
	}
	out["alternative_titles"] = map[string]any{"titles": alts}
	extras(out, "movie", m.ID, s.castOf("movie", m.ID))
	return out
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/tmdb")
	s.mu.Lock()
	s.Requests[path]++
	s.mu.Unlock()
	if strings.HasPrefix(path, "/t/p/") {
		s.image(w, path)
		return
	}
	if !authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "status_code": 7, "status_message": "Invalid API key: You must be granted a valid key."})
		return
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if s.serveDiscover(w, r, path, parts) || s.serveTV(w, r, path, parts) {
		return
	}
	switch {
	case path == "/3/configuration":
		base := s.ImageBase
		if base == "" {
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			prefix := ""
			if strings.HasPrefix(r.URL.Path, "/tmdb/") {
				prefix = "/tmdb"
			}
			base = scheme + "://" + r.Host + prefix + "/t/p/"
		}
		writeJSON(w, http.StatusOK, map[string]any{"images": map[string]any{"base_url": base, "secure_base_url": base,
			"poster_sizes": []string{"w92", "w154", "w185", "w342", "w500", "w780", "original"}}})
	case path == "/3/search/movie":
		q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
		year := r.URL.Query().Get("year")
		var results []map[string]any
		s.mu.Lock()
		for _, m := range s.Movies {
			hit := strings.Contains(strings.ToLower(m.Title), q)
			for _, a := range m.AltTitles {
				hit = hit || strings.Contains(strings.ToLower(a), q)
			}
			if q == "" || !hit || (year != "" && !strings.HasPrefix(releaseDate(m), year)) {
				continue
			}
			results = append(results, m.result())
		}
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"page": 1, "results": orEmpty(results), "total_pages": 1, "total_results": len(results)})
	case len(parts) == 3 && parts[0] == "3" && parts[1] == "movie":
		id, _ := strconv.ParseInt(parts[2], 10, 64)
		m, ok := s.find(id)
		if !ok {
			notFound(w)
			return
		}
		writeJSON(w, http.StatusOK, s.details(m))
	case len(parts) == 3 && parts[0] == "3" && parts[1] == "find":
		var results []map[string]any
		s.mu.Lock()
		for _, m := range s.Movies {
			if r.URL.Query().Get("external_source") != "tvdb_id" && strings.EqualFold(m.IMDb, parts[2]) {
				results = append(results, m.result())
			}
		}
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"movie_results": orEmpty(results),
			"tv_results": s.findTVResults(r.URL.Query().Get("external_source"), parts[2])})
	case len(parts) == 3 && parts[0] == "3" && parts[1] == "collection":
		id, _ := strconv.ParseInt(parts[2], 10, 64)
		if id != CollectionID {
			notFound(w)
			return
		}
		var results []map[string]any
		s.mu.Lock()
		for _, m := range s.Movies {
			if m.Collection == id {
				results = append(results, m.result())
			}
		}
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": "Open Movies Collection", "overview": "Open movies made in the open.",
			"poster_path": "/" + poster(id), "backdrop_path": "/" + backdrop(id), "parts": results})
	case len(parts) == 3 && parts[0] == "3" && parts[1] == "list":
		if parts[2] != ListID {
			notFound(w)
			return
		}
		var items []map[string]any
		for _, id := range []int64{45745, 133701} {
			if m, ok := s.find(id); ok {
				items = append(items, m.result())
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": ListID, "name": "Locarr picks", "items": items, "page": 1, "total_pages": 1})
	default:
		notFound(w)
	}
}

func orEmpty(v []map[string]any) []map[string]any {
	if v == nil {
		return []map[string]any{}
	}
	return v
}

// image serves a small generated poster in a colour derived from the name.
func (s *Server) image(w http.ResponseWriter, path string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 || !strings.HasSuffix(parts[3], ".png") {
		http.NotFound(w, nil)
		return
	}
	sum := crc32.ChecksumIEEE([]byte(parts[3]))
	img := image.NewRGBA(image.Rect(0, 0, 40, 60))
	c := color.RGBA{R: uint8(sum & 0xff), G: uint8((sum >> 8) & 0xff), B: uint8((sum >> 16) & 0xff), A: 255} // #nosec G115 -- masked to a byte
	for y := 0; y < 60; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(buf.Bytes())
}
