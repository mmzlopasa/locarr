package fake

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The discover endpoints (P8), from TMDb's public documentation:
//
//	/3/trending/{all|movie|tv}/{day|week}
//	/3/movie/{popular|upcoming|now_playing|top_rated}  /3/tv/{popular|top_rated|on_the_air}
//	/3/discover/movie?with_genres=&with_companies=     /3/discover/tv?with_genres=&with_networks=
//	/3/genre/{movie|tv}/list  /3/search/{multi|person}  /3/person/{id}
//
// Movie and TV details also carry credits, videos and a tagline.

// Person is a catalog person.
type Person struct {
	ID         int64
	Name       string
	Department string
	Biography  string
	Movies     []int64 // TMDb movie ids
	Series     []int64
}

// People are the fake's people.
var People = []Person{
	{ID: 5001, Name: "Ton Roosendaal", Department: "Production", Biography: "A fictional producer of the open movies (fake catalog).",
		Movies: []int64{10378, 45745, 133701}, Series: []int64{800434}},
	{ID: 5002, Name: "Colin Levy", Department: "Directing", Biography: "A fictional director (fake catalog).",
		Movies: []int64{45745}, Series: []int64{800424}},
}

// Genre ids by name (TMDb's own ids for these names).
var genreIDs = map[string]int64{"Action": 28, "Adventure": 12, "Animation": 16, "Comedy": 35, "Drama": 18, "Fantasy": 14,
	"Science Fiction": 878, "Talk": 10767}

// Company and network ids of the fake catalog.
var companyIDs = map[string]int64{"Blender Foundation": 7001, "Open Studio": 7002, "Zagreb Film": 7003}

// NetworkIDs are the fake series' network ids.
var NetworkIDs = map[string]int64{"Blender": 9001, "Orange Open Movie": 9002, "Open Anime": 9003, "Open News": 9004}

func personJSON(p Person) map[string]any {
	return map[string]any{"id": p.ID, "name": p.Name, "known_for_department": p.Department, "profile_path": "/person" + strconv.FormatInt(p.ID, 10) + ".png",
		"popularity": 1.0, "media_type": "person", "adult": false}
}

func (s *Server) castOf(kind string, id int64) []map[string]any {
	var out []map[string]any
	for i, p := range People {
		list := p.Movies
		if kind == "tv" {
			list = p.Series
		}
		for _, x := range list {
			if x == id {
				out = append(out, map[string]any{"id": p.ID, "name": p.Name, "character": "Themselves", "order": i,
					"profile_path": "/person" + strconv.FormatInt(p.ID, 10) + ".png"})
			}
		}
	}
	return orEmpty(out)
}

func extras(out map[string]any, kind string, id int64, cast []map[string]any) {
	out["tagline"] = "An open " + kind + " (fake catalog)."
	out["credits"] = map[string]any{"cast": cast, "crew": []any{}}
	out["videos"] = map[string]any{"results": []map[string]any{{"key": "fake" + strconv.FormatInt(id, 10), "site": "YouTube",
		"type": "Trailer", "official": true}}}
}

func page(results []map[string]any) map[string]any {
	return map[string]any{"page": 1, "results": orEmpty(results), "total_pages": 1, "total_results": len(results)}
}

func (s *Server) movieResults(keep func(Movie) bool) []map[string]any {
	s.mu.Lock()
	list := append([]Movie{}, s.Movies...)
	s.mu.Unlock()
	sort.SliceStable(list, func(i, j int) bool { return list[i].Popularity > list[j].Popularity })
	var out []map[string]any
	for _, m := range list {
		if keep == nil || keep(m) {
			out = append(out, m.result())
		}
	}
	return out
}

func (s *Server) tvResults(keep func(TVSeries) bool) []map[string]any {
	s.mu.Lock()
	list := append([]TVSeries{}, s.Series...)
	s.mu.Unlock()
	sort.SliceStable(list, func(i, j int) bool { return list[i].Popularity > list[j].Popularity })
	var out []map[string]any
	for _, t := range list {
		if keep == nil || keep(t) {
			out = append(out, t.result())
		}
	}
	return out
}

func hasGenre(genres []string, id string) bool {
	for _, g := range genres {
		if strconv.FormatInt(genreIDs[g], 10) == id {
			return true
		}
	}
	return false
}

// serveDiscover handles the discover endpoints; ok is false when the path is not one.
func (s *Server) serveDiscover(w http.ResponseWriter, r *http.Request, path string, parts []string) bool {
	q := r.URL.Query()
	today := time.Now().UTC().Format("2006-01-02")
	monthAgo := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")
	switch {
	case len(parts) == 4 && parts[0] == "3" && parts[1] == "trending":
		var res []map[string]any
		if parts[2] == "all" || parts[2] == "movie" {
			res = append(res, s.movieResults(nil)...)
		}
		if parts[2] == "all" || parts[2] == "tv" {
			res = append(res, s.tvResults(nil)...)
		}
		writeJSON(w, http.StatusOK, page(res))
	case path == "/3/movie/popular", path == "/3/movie/top_rated":
		writeJSON(w, http.StatusOK, page(s.movieResults(func(m Movie) bool { return m.Status == "Released" })))
	case path == "/3/movie/upcoming":
		writeJSON(w, http.StatusOK, page(s.movieResults(func(m Movie) bool { return releaseDate(m) > today })))
	case path == "/3/movie/now_playing":
		writeJSON(w, http.StatusOK, page(s.movieResults(func(m Movie) bool {
			return m.Theatrical >= monthAgo && m.Theatrical <= today
		})))
	case path == "/3/tv/popular", path == "/3/tv/top_rated":
		writeJSON(w, http.StatusOK, page(s.tvResults(nil)))
	case path == "/3/tv/on_the_air":
		writeJSON(w, http.StatusOK, page(s.tvResults(func(t TVSeries) bool { return t.Status == "Returning Series" })))
	case path == "/3/discover/movie":
		writeJSON(w, http.StatusOK, page(s.movieResults(func(m Movie) bool {
			if g := q.Get("with_genres"); g != "" && !hasGenre(m.Genres, g) {
				return false
			}
			if c := q.Get("with_companies"); c != "" && strconv.FormatInt(companyIDs[m.Studio], 10) != c {
				return false
			}
			return true
		})))
	case path == "/3/discover/tv":
		writeJSON(w, http.StatusOK, page(s.tvResults(func(t TVSeries) bool {
			if g := q.Get("with_genres"); g != "" && !hasGenre(t.Genres, g) {
				return false
			}
			if n := q.Get("with_networks"); n != "" && strconv.FormatInt(NetworkIDs[t.Network], 10) != n {
				return false
			}
			return true
		})))
	case path == "/3/genre/movie/list", path == "/3/genre/tv/list":
		names := map[string]bool{}
		s.mu.Lock()
		defer s.mu.Unlock()
		if parts[2] == "movie" {
			for _, m := range s.Movies {
				for _, g := range m.Genres {
					names[g] = true
				}
			}
		} else {
			for _, t := range s.Series {
				for _, g := range t.Genres {
					names[g] = true
				}
			}
		}
		var genres []map[string]any
		for n := range names {
			if id, ok := genreIDs[n]; ok {
				genres = append(genres, map[string]any{"id": id, "name": n})
			}
		}
		sort.Slice(genres, func(i, j int) bool { return genres[i]["name"].(string) < genres[j]["name"].(string) })
		writeJSON(w, http.StatusOK, map[string]any{"genres": orEmpty(genres)})
	case path == "/3/search/multi", path == "/3/search/person":
		term := strings.ToLower(strings.TrimSpace(q.Get("query")))
		var res []map[string]any
		if term != "" && path == "/3/search/multi" {
			res = append(res, s.movieResults(func(m Movie) bool { return strings.Contains(strings.ToLower(m.Title), term) })...)
			res = append(res, s.tvResults(func(t TVSeries) bool { return strings.Contains(strings.ToLower(t.Title), term) })...)
		}
		for _, p := range People {
			if term != "" && strings.Contains(strings.ToLower(p.Name), term) {
				res = append(res, personJSON(p))
			}
		}
		writeJSON(w, http.StatusOK, page(res))
	case len(parts) == 3 && parts[0] == "3" && parts[1] == "person":
		id, _ := strconv.ParseInt(parts[2], 10, 64)
		for _, p := range People {
			if p.ID != id {
				continue
			}
			out := personJSON(p)
			out["biography"], out["birthday"] = p.Biography, "1960-03-20"
			var cast []map[string]any
			for _, mid := range p.Movies {
				if m, ok := s.find(mid); ok {
					cast = append(cast, m.result())
				}
			}
			for _, tid := range p.Series {
				if t, ok := s.findTV(tid); ok {
					cast = append(cast, t.result())
				}
			}
			out["combined_credits"] = map[string]any{"cast": orEmpty(cast), "crew": []any{}}
			writeJSON(w, http.StatusOK, out)
			return true
		}
		notFound(w)
	default:
		return false
	}
	return true
}
