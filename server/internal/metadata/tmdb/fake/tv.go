package fake

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// TVEpisode is a catalog episode.
type TVEpisode struct {
	ID      int64
	Number  int
	Title   string
	AirDate string // YYYY-MM-DD ("" = not announced)
	Runtime int
}

// TVSeason is a catalog season.
type TVSeason struct {
	Number   int
	Title    string
	Episodes []TVEpisode
}

// TVSeries is a catalog series.
type TVSeries struct {
	ID               int64
	TVDB             int64
	IMDb             string
	Title            string
	OriginalTitle    string
	OriginalLanguage string
	Overview         string
	Status           string // TMDb words: Returning Series, Ended, In Production, Canceled
	Network          string
	Country          string
	Runtime          int
	Genres           []string
	AltTitles        []string
	Seasons          []TVSeason
	Popularity       float64
}

func eps(base int64, n int, firstAir time.Time, step time.Duration, title string) []TVEpisode {
	out := make([]TVEpisode, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, TVEpisode{ID: base + int64(i), Number: i, Title: fmt.Sprintf("%s %d", title, i),
			AirDate: firstAir.Add(time.Duration(i-1) * step).Format("2006-01-02"), Runtime: 0})
	}
	return out
}

// tvCatalog matches the fake indexer's series releases.
func tvCatalog(now time.Time) []TVSeries {
	day := 24 * time.Hour
	past := now.AddDate(-1, 0, 0)
	elephants := eps(4240100, 4, now.Add(-13*day), 7*day, "Proog")
	elephants[0].Title, elephants[1].Title = "The Machine", "Emo"
	return []TVSeries{
		{ID: 800434, TVDB: 434343, IMDb: "tt9100434", Title: "Cosmos Laundromat", OriginalLanguage: "en", Status: "Ended", Network: "Blender",
			Country: "NL", Runtime: 24, Genres: []string{"Animation", "Drama"}, Popularity: 9.4,
			Overview: "On a desolate island, a suicidal sheep named Franck meets his fate in a quirky salesman (fake catalog).",
			Seasons: []TVSeason{
				{Number: 0, Title: "Specials", Episodes: []TVEpisode{{ID: 4343001, Number: 1, Title: "Making Of", AirDate: past.Add(-30 * day).Format("2006-01-02")}}},
				{Number: 1, Title: "Season 1", Episodes: []TVEpisode{
					{ID: 4343101, Number: 1, Title: "First Cycle", AirDate: past.Format("2006-01-02")},
					{ID: 4343102, Number: 2, Title: "Second Cycle", AirDate: past.Add(7 * day).Format("2006-01-02")},
					{ID: 4343103, Number: 3, Title: "Third Cycle", AirDate: past.Add(14 * day).Format("2006-01-02")},
				}},
			}},
		{ID: 800424, TVDB: 424242, IMDb: "tt9100424", Title: "Elephants Dream", OriginalLanguage: "en", Status: "Returning Series",
			Network: "Orange Open Movie", Country: "US", Runtime: 11, Genres: []string{"Animation", "Science Fiction"}, Popularity: 7.7,
			AltTitles: []string{"Machina"}, Overview: "Two strange characters explore a capricious and seemingly infinite machine (fake catalog).",
			Seasons: []TVSeason{{Number: 1, Title: "Season 1", Episodes: elephants}}},
		{ID: 800454, TVDB: 454545, Title: "Spring Kurenai", OriginalTitle: "Haru Kurenai", OriginalLanguage: "ja", Status: "Ended",
			Network: "Open Anime", Country: "JP", Runtime: 24, Genres: []string{"Animation"}, Popularity: 5.5,
			Overview: "A fictional anime (fake catalog) whose releases use absolute episode numbers.",
			Seasons: []TVSeason{{Number: 1, Title: "Season 1", Episodes: eps(4545100, 2, past, 7*day, "Spring")},
				{Number: 2, Title: "Season 2", Episodes: eps(4545200, 2, past.Add(60*day), 7*day, "Bloom")}}},
		{ID: 800464, TVDB: 464646, Title: "The Open Desk", OriginalLanguage: "en", Status: "Returning Series", Network: "Open News",
			Country: "US", Runtime: 30, Genres: []string{"Talk"}, Popularity: 3.3,
			Overview: "A fictional daily talk show (fake catalog); releases are named by air date.",
			Seasons: []TVSeason{{Number: 2025, Title: "2025", Episodes: []TVEpisode{
				{ID: 4646001, Number: 1, Title: "March 13", AirDate: "2025-03-13"},
				{ID: 4646002, Number: 2, Title: "March 14", AirDate: "2025-03-14"},
			}}}},
	}
}

func (s *Server) findTV(id int64) (TVSeries, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.Series {
		if t.ID == id {
			return t, true
		}
	}
	return TVSeries{}, false
}

func (t TVSeries) firstAir() string {
	for _, sn := range t.Seasons {
		if sn.Number == 0 {
			continue
		}
		for _, e := range sn.Episodes {
			if e.AirDate != "" {
				return e.AirDate
			}
		}
	}
	return ""
}

func (t TVSeries) result() map[string]any {
	orig := t.OriginalTitle
	if orig == "" {
		orig = t.Title
	}
	return map[string]any{"id": t.ID, "name": t.Name(), "original_name": orig, "first_air_date": t.firstAir(), "overview": t.Overview,
		"poster_path": "/" + poster(t.ID), "backdrop_path": "/" + backdrop(t.ID), "popularity": t.Popularity, "vote_average": 7.4,
		"origin_country": []string{t.Country}, "media_type": "tv"}
}

// Name is the series title.
func (t TVSeries) Name() string { return t.Title }

func (t TVSeries) details() map[string]any {
	out := t.result()
	out["original_language"] = t.OriginalLanguage
	out["status"] = t.Status
	out["episode_run_time"] = []int{t.Runtime}
	out["vote_count"] = 120
	var genres []map[string]any
	for i, g := range t.Genres {
		genres = append(genres, map[string]any{"id": i + 1, "name": g})
	}
	out["genres"] = genres
	out["networks"] = []map[string]any{{"id": 1, "name": t.Network, "origin_country": t.Country}}
	last := ""
	var seasons []map[string]any
	for _, sn := range t.Seasons {
		air := ""
		if len(sn.Episodes) > 0 {
			air = sn.Episodes[0].AirDate
		}
		for _, e := range sn.Episodes {
			if e.AirDate != "" && e.AirDate > last && e.AirDate <= time.Now().UTC().Format("2006-01-02") {
				last = e.AirDate
			}
		}
		seasons = append(seasons, map[string]any{"season_number": sn.Number, "name": sn.Title, "overview": "", "poster_path": "/" + poster(t.ID*100+int64(sn.Number)),
			"air_date": air, "episode_count": len(sn.Episodes)})
	}
	out["seasons"] = seasons
	out["last_air_date"] = last
	out["external_ids"] = map[string]any{"imdb_id": t.IMDb, "tvdb_id": t.TVDB}
	var alts []map[string]any
	for _, a := range t.AltTitles {
		alts = append(alts, map[string]any{"iso_3166_1": "US", "title": a})
	}
	out["alternative_titles"] = map[string]any{"results": alts}
	out["content_ratings"] = map[string]any{"results": []map[string]any{{"iso_3166_1": "US", "rating": "TV-PG"}}}
	return out
}

// serveTV handles the TV endpoints; ok is false when the path is not one.
func (s *Server) serveTV(w http.ResponseWriter, r *http.Request, path string, parts []string) bool {
	switch {
	case path == "/3/search/tv":
		q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
		year := r.URL.Query().Get("first_air_date_year")
		var results []map[string]any
		s.mu.Lock()
		list := append([]TVSeries{}, s.Series...)
		s.mu.Unlock()
		for _, t := range list {
			hit := strings.Contains(strings.ToLower(t.Title), q)
			for _, a := range append(t.AltTitles, t.OriginalTitle) {
				hit = hit || (a != "" && strings.Contains(strings.ToLower(a), q))
			}
			if q == "" || !hit || (year != "" && !strings.HasPrefix(t.firstAir(), year)) {
				continue
			}
			results = append(results, t.result())
		}
		writeJSON(w, http.StatusOK, map[string]any{"page": 1, "results": orEmpty(results), "total_pages": 1, "total_results": len(results)})
	case len(parts) == 3 && parts[0] == "3" && parts[1] == "tv":
		id, _ := strconv.ParseInt(parts[2], 10, 64)
		t, ok := s.findTV(id)
		if !ok {
			notFound(w)
			return true
		}
		writeJSON(w, http.StatusOK, t.details())
	case len(parts) == 5 && parts[0] == "3" && parts[1] == "tv" && parts[3] == "season":
		id, _ := strconv.ParseInt(parts[2], 10, 64)
		n, _ := strconv.Atoi(parts[4])
		t, ok := s.findTV(id)
		if !ok {
			notFound(w)
			return true
		}
		for _, sn := range t.Seasons {
			if sn.Number != n {
				continue
			}
			var list []map[string]any
			for _, e := range sn.Episodes {
				list = append(list, map[string]any{"id": e.ID, "episode_number": e.Number, "season_number": n, "name": e.Title,
					"overview": "Episode " + strconv.Itoa(e.Number) + " of " + t.Title + ".", "air_date": e.AirDate, "runtime": e.Runtime})
			}
			writeJSON(w, http.StatusOK, map[string]any{"season_number": n, "name": sn.Title, "episodes": orEmpty(list)})
			return true
		}
		notFound(w)
	default:
		return false
	}
	return true
}

func (s *Server) findTVResults(ext, id string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, t := range s.Series {
		if (ext == "imdb_id" && t.IMDb != "" && strings.EqualFold(t.IMDb, id)) || (ext == "tvdb_id" && strconv.FormatInt(t.TVDB, 10) == id) {
			out = append(out, t.result())
		}
	}
	return orEmpty(out)
}

// EditSeries changes the TV catalog under the server's lock (tests).
func (s *Server) EditSeries(f func(list []TVSeries) []TVSeries) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Series = f(s.Series)
}
