package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The shapes below are what Streamero reads from Seerr, Radarr and Sonarr
// (field names and types as its client decodes them). Locarr's compatible
// API must decode into exactly these; nothing else is relied on.

type stSeerrResult struct {
	ID           int     `json:"id"`
	MediaType    string  `json:"mediaType"`
	Title        string  `json:"title"`
	Name         string  `json:"name"`
	ReleaseDate  string  `json:"releaseDate"`
	FirstAirDate string  `json:"firstAirDate"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"posterPath"`
	BackdropPath string  `json:"backdropPath"`
	VoteAverage  float64 `json:"voteAverage"`
	MediaInfo    *struct {
		Status int `json:"status"`
	} `json:"mediaInfo"`
}

type stSeerrPage struct {
	Page         int             `json:"page"`
	TotalPages   int             `json:"totalPages"`
	TotalResults int             `json:"totalResults"`
	Results      []stSeerrResult `json:"results"`
}

type stRequestAnswer struct {
	Status int `json:"status"`
	Media  struct {
		Status int `json:"status"`
	} `json:"media"`
}

type stVersion struct {
	Version string `json:"version"`
}

type stImage struct {
	CoverType string `json:"coverType"`
	RemoteURL string `json:"remoteUrl"`
}

type stRadarrCalendar []struct {
	Title           string     `json:"title"`
	Overview        string     `json:"overview"`
	DigitalRelease  *time.Time `json:"digitalRelease"`
	PhysicalRelease *time.Time `json:"physicalRelease"`
	InCinemas       *time.Time `json:"inCinemas"`
	HasFile         bool       `json:"hasFile"`
	Images          []stImage  `json:"images"`
}

type stSonarrCalendar []struct {
	Title         string     `json:"title"`
	Overview      string     `json:"overview"`
	SeasonNumber  int        `json:"seasonNumber"`
	EpisodeNumber int        `json:"episodeNumber"`
	AirDateUTC    *time.Time `json:"airDateUtc"`
	HasFile       bool       `json:"hasFile"`
	Series        struct {
		Title  string    `json:"title"`
		Images []stImage `json:"images"`
	} `json:"series"`
}

type stRadarrMovies []struct {
	ID        int    `json:"id"`
	TitleSlug string `json:"titleSlug"`
	TMDbID    int    `json:"tmdbId"`
}

type stSonarrSeries []struct {
	ID        int    `json:"id"`
	TitleSlug string `json:"titleSlug"`
	TVDbID    int    `json:"tvdbId"`
	TMDbID    int    `json:"tmdbId"`
}

// stStatus is how a media server reads a media status code (Seerr's public
// codes: 2 pending, 3 processing, 4 partially available, 5 available).
func stStatus(code int) string {
	switch code {
	case 2:
		return "pending"
	case 3:
		return "processing"
	case 4:
		return "partial"
	case 5:
		return "available"
	}
	return "none"
}

// stRequested is how a media server reads the answer to a request
// (request status 2 = approved, then the media state).
func stRequested(a stRequestAnswer) string {
	if a.Status == 2 {
		if st := stStatus(a.Media.Status); st != "none" && st != "pending" {
			return st
		}
		return "processing"
	}
	return "pending"
}

// decodeStrict decodes into the client's shape and fails on a type mismatch.
func decodeStrict(t *testing.T, r resp, v any) {
	t.Helper()
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("%s: content type %q", r.Request.URL.Path, ct)
	}
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("%s: %v: %s", r.Request.URL.Path, err, r.body)
	}
}

// apiKey creates an API key for the signed-in token.
func (e *env) apiKey(token string, readOnly bool) string {
	e.t.Helper()
	var out struct{ Key string }
	e.as(token, func() {
		e.expect(e.do("POST", "/api/v1/auth/apikeys", map[string]any{"name": "Streamero", "readOnly": readOnly}), 201).json(&out)
	})
	if out.Key == "" {
		e.t.Fatal("no key")
	}
	return out.Key
}

// compat calls the compatible API with an API key the way a media server does.
func (e *env) compat(method, path, key string, body any) resp {
	e.t.Helper()
	hdr := map[string]string{"Authorization": "", "Accept": "application/json"}
	if key != "" {
		hdr["X-Api-Key"] = key
	}
	old := e.token
	e.token = ""
	defer func() { e.token = old }()
	return e.req(method, path, body, hdr)
}

func findResult(p stSeerrPage, id int) *stSeerrResult {
	for i := range p.Results {
		if p.Results[i].ID == id {
			return &p.Results[i]
		}
	}
	return nil
}

// Search, trending and requests through the Seerr-compatible API, decoded
// into the shapes a media server reads, with the key owner's permissions,
// auto-approval and quotas.
func TestCompatSeerrContract(t *testing.T) {
	se := newSeriesEnv(t)
	runRequests(t, se.env)
	se.requestSettings(func(s map[string]any) {
		s["movie"] = kindDefaults(se.profileID, se.rootID)
		s["series"] = kindDefaults(se.tvProfile, se.tvRoot)
		s["quotas"] = map[string]any{"movie": map[string]any{"limit": 1, "days": 7}, "series": map[string]any{"limit": 0, "days": 7},
			"music": map[string]any{"limit": 0, "days": 7}}
	})
	admin := se.apiKey(se.token, false)
	const base = "/compat/seerr/api/v1"

	var v stVersion
	decodeStrict(t, se.expect(se.compat("GET", base+"/status", admin, nil), 200), &v)
	if v.Version == "" {
		t.Fatal("no version")
	}

	var trending stSeerrPage
	decodeStrict(t, se.expect(se.compat("GET", base+"/discover/trending?page=1&language=hr", admin, nil), 200), &trending)
	if trending.Page != 1 || trending.TotalPages < 1 || len(trending.Results) == 0 {
		t.Fatalf("trending: %+v", trending)
	}
	for _, r := range trending.Results {
		if r.MediaType != "movie" && r.MediaType != "tv" && r.MediaType != "person" {
			t.Fatalf("media type %q", r.MediaType)
		}
		if r.PosterPath != "" && !strings.HasPrefix(r.PosterPath, "/") {
			t.Fatalf("poster path %q", r.PosterPath)
		}
		if r.MediaType == "tv" && r.Name == "" || r.MediaType == "movie" && r.Title == "" {
			t.Fatalf("no title: %+v", r)
		}
	}
	if n := se.tmdb.Count("/3/trending/all/week"); n == 0 {
		t.Fatal("trending did not reach TMDb")
	}

	var search stSeerrPage
	q := url.Values{"query": {"Sintel"}, "page": {"1"}}
	decodeStrict(t, se.expect(se.compat("GET", base+"/search?"+strings.ReplaceAll(q.Encode(), "+", "%20"), admin, nil), 200), &search)
	sintel := findResult(search, 45745)
	if sintel == nil || sintel.MediaType != "movie" || sintel.Title != "Sintel" || !strings.HasPrefix(sintel.ReleaseDate, "2010") ||
		sintel.MediaInfo != nil {
		t.Fatalf("search: %+v", search)
	}
	decodeStrict(t, se.expect(se.compat("GET", base+"/search?query=Cosmos%20Laundromat&page=1", admin, nil), 200), &search)
	if c := findResult(search, 800434); c == nil || c.MediaType != "tv" || c.Name != "Cosmos Laundromat" || c.FirstAirDate == "" {
		t.Fatalf("series search: %+v", search)
	}
	// A second page of a search is empty, not an error.
	decodeStrict(t, se.expect(se.compat("GET", base+"/search?query=Sintel&page=2", admin, nil), 200), &search)
	if len(search.Results) != 0 {
		t.Fatalf("page 2: %+v", search)
	}

	// An administrator's key auto-approves: the movie is added and searched.
	var ans stRequestAnswer
	decodeStrict(t, se.expect(se.compat("POST", base+"/request", admin, map[string]any{"mediaType": "movie", "mediaId": 45745}), 201), &ans)
	if got := stRequested(ans); got != "processing" {
		t.Fatalf("admin request: %s (%+v)", got, ans)
	}
	var reqs struct {
		Items []apiRequest
	}
	se.expect(se.do("GET", "/api/v1/requests?status=all", nil), 200).json(&reqs)
	if len(reqs.Items) != 1 || reqs.Items[0].Kind != "movie" || reqs.Items[0].RequestedBy.Username != "admin" || reqs.Items[0].LibraryRef == "" {
		t.Fatalf("requests: %+v", reqs.Items)
	}
	decodeStrict(t, se.expect(se.compat("GET", base+"/search?query=Sintel", admin, nil), 200), &search)
	if s := findResult(search, 45745); s == nil || s.MediaInfo == nil || stStatus(s.MediaInfo.Status) != "processing" {
		t.Fatalf("badge after request: %+v", s)
	}
	// Asking again answers with the state instead of an error.
	r := se.expect(se.compat("POST", base+"/request", admin, map[string]any{"mediaType": "movie", "mediaId": 45745}), 200)
	decodeStrict(t, r, &ans)
	if got := stRequested(ans); got != "processing" {
		t.Fatalf("again: %s", got)
	}

	// A whole series: every season and the ones announced later.
	decodeStrict(t, se.expect(se.compat("POST", base+"/request", admin, map[string]any{"mediaType": "tv", "mediaId": 800424, "seasons": "all"}), 201), &ans)
	if got := stRequested(ans); got != "processing" {
		t.Fatalf("series: %s", got)
	}
	var tv struct {
		Items []struct {
			Kind          string
			AllSeasons    bool `json:"allSeasons"`
			FutureSeasons bool `json:"futureSeasons"`
			Seasons       []struct{ Number int }
		}
	}
	se.expect(se.do("GET", "/api/v1/requests?status=all&kind=series", nil), 200).json(&tv)
	found := false
	for _, it := range tv.Items {
		if it.Kind == "series" {
			found = true
			if !it.AllSeasons || !it.FutureSeasons || len(it.Seasons) != 1 || it.Seasons[0].Number != 1 {
				t.Fatalf("series request: %+v", it)
			}
		}
	}
	if !found {
		t.Fatalf("no series request: %+v", tv.Items)
	}

	// A requester's key: pending approval, then the quota.
	fam := se.user("fam", "requester", nil)
	famKey := se.apiKey(fam, false)
	decodeStrict(t, se.expect(se.compat("POST", base+"/request", famKey, map[string]any{"mediaType": "movie", "mediaId": 10378}), 201), &ans)
	if got := stRequested(ans); got != "pending" || ans.Status != 1 || ans.Media.Status != 2 {
		t.Fatalf("requester: %s %+v", got, ans)
	}
	decodeStrict(t, se.expect(se.compat("GET", base+"/discover/trending", famKey, nil), 200), &trending)
	if b := findResult(trending, 10378); b == nil || b.MediaInfo == nil || stStatus(b.MediaInfo.Status) != "pending" {
		t.Fatalf("pending badge: %+v", b)
	}
	r = se.expect(se.compat("POST", base+"/request", famKey, map[string]any{"mediaType": "movie", "mediaId": 133701}), 429)
	var e struct{ Code, Message string }
	decodeStrict(t, r, &e)
	if e.Code != "quota_exceeded" || !strings.Contains(e.Message, "quota") {
		t.Fatalf("quota: %+v", e)
	}
	// A kind the user may not request.
	se.expect(se.do("PUT", "/api/v1/users/"+itoa(userID(se.env, "fam"))+"/quotas", map[string]any{"movie": map[string]any{"limit": 0, "days": 7}}), 200)
	kidPerms := requesterPerms()
	kidPerms["requestSeries"] = false
	kid := se.user("kid", "requester", kidPerms)
	kidKey := se.apiKey(kid, false)
	r = se.expect(se.compat("POST", base+"/request", kidKey, map[string]any{"mediaType": "tv", "mediaId": 800434, "seasons": "all"}), 403)
	if r.code() != "forbidden" {
		t.Fatalf("forbidden: %s", r.body)
	}
	for _, bad := range []map[string]any{{"mediaType": "person", "mediaId": 1}, {"mediaType": "movie"}, {"mediaType": "tv", "mediaId": 800434, "seasons": "some"}} {
		if r := se.expect(se.compat("POST", base+"/request", admin, bad), 400); r.code() != "validation" {
			t.Fatalf("%v: %s", bad, r.body)
		}
	}
}

func userID(e *env, name string) int64 {
	e.t.Helper()
	var users []struct {
		ID       int64
		Username string
	}
	e.expect(e.do("GET", "/api/v1/users", nil), 200).json(&users)
	for _, u := range users {
		if u.Username == name {
			return u.ID
		}
	}
	e.t.Fatalf("no user %s", name)
	return 0
}

// Only API keys open the compatible API; read-only keys only read; the
// Radarr/Sonarr side needs the library permission (D-060).
func TestCompatPermissions(t *testing.T) {
	e := newEnv(t, noLimits)
	e.setup()
	admin := e.apiKey(e.token, false)
	adminRO := e.apiKey(e.token, true)
	fam := e.user("fam", "requester", nil)
	famRO := e.apiKey(fam, true)

	for _, path := range []string{"/compat/seerr/api/v1/status", "/compat/radarr/api/v3/calendar", "/compat/sonarr/api/v3/series"} {
		for _, key := range []string{"", "lk_notakey", e.token} {
			r := e.expect(e.compat("GET", path, key, nil), 401)
			if r.code() != "unauthenticated" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				t.Fatalf("%s %q: %s", path, key, r.body)
			}
		}
	}
	// A session cookie is not enough either.
	r := e.req("GET", "/compat/seerr/api/v1/status", nil, map[string]string{"Authorization": "", "Cookie": "locarr_session=" + e.token})
	if r.StatusCode != 401 {
		t.Fatalf("cookie: %d", r.StatusCode)
	}
	e.expect(e.compat("GET", "/compat/seerr/api/v1/status", famRO, nil), 200)
	e.expect(e.compat("GET", "/compat/radarr/api/v3/system/status", adminRO, nil), 200)
	if r := e.expect(e.compat("POST", "/compat/seerr/api/v1/request", adminRO, map[string]any{"mediaType": "movie", "mediaId": 45745}), 403); r.code() != "read_only" {
		t.Fatalf("read-only: %s", r.body)
	}
	for _, path := range []string{"/compat/radarr/api/v3/calendar", "/compat/radarr/api/v3/movie", "/compat/sonarr/api/v3/calendar", "/compat/sonarr/api/v3/series"} {
		if r := e.expect(e.compat("GET", path, famRO, nil), 403); r.code() != "forbidden" {
			t.Fatalf("%s: %s", path, r.body)
		}
		e.expect(e.compat("GET", path, adminRO, nil), 200)
	}
	if r := e.expect(e.compat("GET", "/compat/seerr/api/v1/nothing", admin, nil), 404); r.code() != "not_found" {
		t.Fatalf("unknown: %s", r.body)
	}
	e.expect(e.compat("DELETE", "/compat/seerr/api/v1/request", admin, nil), 405)
	if r := e.expect(e.compat("GET", "/compat/radarr/api/v3/calendar?start=yesterday", admin, nil), 400); r.code() != "validation" {
		t.Fatalf("bad date: %s", r.body)
	}
	// Without a TMDb key the discover side says so.
	if r := e.expect(e.compat("GET", "/compat/seerr/api/v1/discover/trending", admin, nil), 503); r.code() != "metadata_key_missing" {
		t.Fatalf("no TMDb key: %s", r.body)
	}
	// Title links open Locarr's own pages, without a key (they are browser links).
	r = e.compat("GET", "/compat/radarr/movie/12", "", nil)
	if r.StatusCode != http.StatusFound || r.Header.Get("Location") != "/movies/12" {
		t.Fatalf("movie link: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	r = e.compat("GET", "/compat/sonarr/series/7", "", nil)
	if r.StatusCode != http.StatusFound || r.Header.Get("Location") != "/series/7" {
		t.Fatalf("series link: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	e.expect(e.compat("GET", "/compat/sonarr/series/../x", "", nil), 404)
}

// Upcoming releases and library lookups through the Radarr/Sonarr-compatible API.
func TestCompatCalendarAndLibrary(t *testing.T) {
	se := newSeriesEnv(t)
	runRequests(t, se.env)
	now := time.Now().UTC()
	soon := now.AddDate(0, 0, 20).Format("2006-01-02")
	for i := range se.tmdb.Movies {
		if se.tmdb.Movies[i].ID == 900002 {
			se.tmdb.Movies[i].Digital = soon
		}
	}
	se.requestSettings(func(s map[string]any) {
		s["movie"] = kindDefaults(se.profileID, se.rootID)
		s["series"] = kindDefaults(se.tvProfile, se.tvRoot)
	})
	admin := se.apiKey(se.token, true)
	full := se.apiKey(se.token, false)
	for _, body := range []map[string]any{{"mediaType": "movie", "mediaId": 900002}, {"mediaType": "tv", "mediaId": 800424, "seasons": "all"}} {
		se.expect(se.compat("POST", "/compat/seerr/api/v1/request", full, body), 201)
	}
	from := now.Truncate(24 * time.Hour)
	to := from.AddDate(0, 0, 30)
	q := url.Values{"start": {from.Format(time.RFC3339)}, "end": {to.Format(time.RFC3339)}}

	var movies stRadarrCalendar
	decodeStrict(t, se.expect(se.compat("GET", "/compat/radarr/api/v3/calendar?"+q.Encode(), admin, nil), 200), &movies)
	if len(movies) != 1 || movies[0].Title != "Kino Platno" || movies[0].DigitalRelease == nil ||
		movies[0].DigitalRelease.Format("2006-01-02") != soon || movies[0].HasFile {
		t.Fatalf("radarr calendar: %+v", movies)
	}
	if len(movies[0].Images) != 1 || movies[0].Images[0].CoverType != "poster" || !strings.HasPrefix(movies[0].Images[0].RemoteURL, "https://") {
		t.Fatalf("poster: %+v", movies[0].Images)
	}
	// The cinema date (10 days ago) is outside the range, the digital one inside.
	if movies[0].InCinemas == nil || !movies[0].InCinemas.Before(from) {
		t.Fatalf("in cinemas: %v", movies[0].InCinemas)
	}

	q2 := url.Values{"start": q["start"], "end": q["end"], "includeSeries": {"true"}}
	var eps stSonarrCalendar
	decodeStrict(t, se.expect(se.compat("GET", "/compat/sonarr/api/v3/calendar?"+q2.Encode(), admin, nil), 200), &eps)
	if len(eps) != 2 {
		t.Fatalf("sonarr calendar: %+v", eps)
	}
	for _, ep := range eps {
		if ep.Series.Title != "Elephants Dream" || ep.SeasonNumber != 1 || ep.AirDateUTC == nil || ep.AirDateUTC.Before(from) ||
			ep.AirDateUTC.After(to) || len(ep.Series.Images) != 1 {
			t.Fatalf("episode: %+v", ep)
		}
	}
	if eps[0].EpisodeNumber != 3 || eps[1].EpisodeNumber != 4 {
		t.Fatalf("order: %+v", eps)
	}
	// Without includeSeries there is no series object to read.
	var raw []map[string]any
	se.expect(se.compat("GET", "/compat/sonarr/api/v3/calendar?"+q.Encode(), admin, nil), 200).json(&raw)
	if len(raw) != 2 || raw[0]["series"] != nil {
		t.Fatalf("without series: %+v", raw)
	}

	var ml stRadarrMovies
	decodeStrict(t, se.expect(se.compat("GET", "/compat/radarr/api/v3/movie?tmdbId=900002", admin, nil), 200), &ml)
	if len(ml) != 1 || ml[0].TMDbID != 900002 || ml[0].TitleSlug != fmt.Sprint(ml[0].ID) {
		t.Fatalf("movie lookup: %+v", ml)
	}
	decodeStrict(t, se.expect(se.compat("GET", "/compat/radarr/api/v3/movie?tmdbId=1", admin, nil), 200), &ml)
	if len(ml) != 0 {
		t.Fatalf("unknown movie: %+v", ml)
	}
	var sl stSonarrSeries
	decodeStrict(t, se.expect(se.compat("GET", "/compat/sonarr/api/v3/series", admin, nil), 200), &sl)
	if len(sl) != 1 || sl[0].TVDbID != 424242 || sl[0].TMDbID != 800424 || sl[0].TitleSlug != fmt.Sprint(sl[0].ID) {
		t.Fatalf("series list: %+v", sl)
	}
	// The link a media server builds from the slug opens the series page.
	r := se.compat("GET", "/compat/sonarr/series/"+sl[0].TitleSlug, "", nil)
	if r.StatusCode != http.StatusFound || r.Header.Get("Location") != "/series/"+sl[0].TitleSlug {
		t.Fatalf("link: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
}
