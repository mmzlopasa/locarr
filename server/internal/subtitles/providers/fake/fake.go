// Package fake serves fake subtitle providers for tests and the dev fakes
// container, each written from the provider's public API documentation:
//
//	/opensubtitles/api/v1   OpenSubtitles.com REST (Api-Key, User-Agent, /login, /subtitles, /download, /infos/user)
//	/subdl/api/v1           SubDL search (api_key) and /subdl/dl/subtitle/… ZIP downloads
//	/jimaku/api             Jimaku entries and files (Authorization header)
//
// Keys: OpenSubtitles "fakeoskey" (user "locarr" / "fakepass"), SubDL
// "fakesubdlkey", Jimaku "fakejimakukey".
package fake

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/encoding/charmap"
)

// Keys accepted by the fakes.
const (
	OSKey      = "fakeoskey"
	OSUser     = "locarr"
	OSPassword = "fakepass"
	SubDLKey   = "fakesubdlkey"
	JimakuKey  = "fakejimakukey"
)

// Sub is one subtitle in the fake catalogue.
type Sub struct {
	Provider string // opensubtitles, subdl, jimaku
	ID       string
	Language string
	IMDb     string // movie, or the series for episodes
	TMDb     int64  // movie TMDb id, or the series TMDb id
	Episode  bool
	Season   int
	Number   int
	Year     int
	Release  string
	HI       bool
	Forced   bool
	Hash     string // OpenSubtitles moviehash that matches
	Trusted  bool
	Body     []byte // file content as served
	Ext      string // srt (default), ass
}

// Server is the fake.
type Server struct {
	mu      sync.Mutex
	Catalog []Sub
	// OSQuota is the number of OpenSubtitles downloads allowed per 24 h
	// (anonymous and per user).
	OSQuota int
	// SubDLDown makes SubDL answer 503; JimakuRateLimit makes every Jimaku request 429.
	SubDLDown       bool
	JimakuRateLimit bool
	// OSRateLimit makes every OpenSubtitles request 429.
	OSRateLimit bool
	// Requests counts requests per provider; Downloads counts downloads.
	Requests  map[string]int
	Downloads map[string]int
	osUsed    map[string]int
	osWindow  map[string][]time.Time
	tokens    map[string]string
	host      string
}

// New returns a fake with the default catalogue.
func New() *Server {
	return &Server{Catalog: DefaultCatalog(), OSQuota: 20, Requests: map[string]int{}, Downloads: map[string]int{},
		osUsed: map[string]int{}, osWindow: map[string][]time.Time{}, tokens: map[string]string{}}
}

// Configure changes the fake's settings safely while it serves.
func (s *Server) Configure(fn func(*Server)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(s)
}

func (s *Server) flag(f func() bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return f()
}

// SRT renders cues "start-end text" as an SRT file, one every 4 seconds.
func SRT(lines ...string) string {
	var b strings.Builder
	for i, l := range lines {
		s := time.Duration(2+i*4) * time.Second
		e := s + 3*time.Second
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, ts(s), ts(e), l)
	}
	return b.String()
}

func ts(d time.Duration) string {
	ms := d / time.Millisecond
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

// CP1250 encodes text as Windows-1250 (how many Croatian subtitles are stored).
func CP1250(s string) []byte {
	b, err := charmap.Windows1250.NewEncoder().Bytes([]byte(s))
	if err != nil {
		panic(err)
	}
	return b
}

// CroatianLines is the text of the fake Croatian subtitles.
var CroatianLines = []string{"Dobro jutro, šumo!", "Čuješ li ptice? Žute su i veselé.", "Đuro, ćao! Idemo na izlet.", "Zec se probudio i protegnuo.", "Leptir mu je sletio na nos.", "Tri nevaljalca vrebaju iz grmlja."}

// DefaultCatalog has Croatian and English subtitles for the fake TMDb's
// movies and series, in several encodings and with different releases.
func DefaultCatalog() []Sub {
	hr := CP1250(SRT(CroatianLines...))
	hrUTF8 := []byte("\ufeff" + SRT(CroatianLines...))
	en := []byte(SRT("Good morning, forest!", "Do you hear the birds?", "Let's go on a trip.", "The rabbit woke up.", "A butterfly landed on his nose.", "Three rascals lurk in the bushes."))
	out := []Sub{
		{Provider: "opensubtitles", ID: "9100001", Language: "hr", IMDb: "tt1254207", TMDb: 10378, Year: 2008, Release: "Big.Buck.Bunny.2008.1080p.BluRay.x264-LOCARR", Trusted: true, Body: hr},
		{Provider: "opensubtitles", ID: "9100002", Language: "hr", IMDb: "tt1254207", TMDb: 10378, Year: 2008, Release: "Big.Buck.Bunny.2008.720p.WEB-DL.H264-OTHER", Body: hrUTF8},
		{Provider: "opensubtitles", ID: "9100003", Language: "hr", IMDb: "tt1254207", TMDb: 10378, Year: 2008, Release: "Big.Buck.Bunny.2008.1080p.BluRay.x264-LOCARR", HI: true, Body: hrUTF8},
		{Provider: "opensubtitles", ID: "9100004", Language: "en", IMDb: "tt1254207", TMDb: 10378, Year: 2008, Release: "Big.Buck.Bunny.2008.1080p.BluRay.x264-LOCARR", Body: en},
		{Provider: "subdl", ID: "3197651-3213944", Language: "hr", IMDb: "tt1254207", TMDb: 10378, Year: 2008, Release: "Big.Buck.Bunny.2008.BDRip.XviD-OLD", Body: hr},
		{Provider: "opensubtitles", ID: "9200001", Language: "en", IMDb: "tt1727587", TMDb: 45745, Year: 2010, Release: "Sintel.2010.1080p.BluRay.x264-LOCARR", Body: en},
		{Provider: "opensubtitles", ID: "9300001", Language: "hr", IMDb: "tt2285752", TMDb: 133701, Year: 2012, Release: "Tears.of.Steel.2012.720p.WEBRip.x264-LOCARR", Trusted: true, Body: hr},
		{Provider: "subdl", ID: "3300001-3300002", Language: "hr", IMDb: "tt2285752", TMDb: 133701, Year: 2012, Release: "Tears.of.Steel.2012.1080p.BluRay.x264-OTHER", Body: hrUTF8},
	}
	for i := 1; i <= 3; i++ {
		rel := fmt.Sprintf("Cosmos.Laundromat.S01E%02d.720p.WEB-DL.AAC2.0.x264-LOCARR", i)
		out = append(out, Sub{Provider: "opensubtitles", ID: fmt.Sprintf("94343%02d", i), Language: "hr", IMDb: "tt9100434", TMDb: 800434,
			Episode: true, Season: 1, Number: i, Release: rel, Body: hr})
	}
	for i := 1; i <= 2; i++ {
		out = append(out,
			Sub{Provider: "opensubtitles", ID: fmt.Sprintf("94242%02d", i), Language: "hr", IMDb: "tt9100424", TMDb: 800424, Episode: true,
				Season: 1, Number: i, Release: fmt.Sprintf("Elephants.Dream.S01E%02d.1080p.WEB.h264-LOCARR", i), Body: hr},
			Sub{Provider: "subdl", ID: fmt.Sprintf("42424-%d", i), Language: "hr", IMDb: "tt9100424", TMDb: 800424, Episode: true,
				Season: 1, Number: i, Release: fmt.Sprintf("Elephants.Dream.S01E%02d.720p.HDTV.x264-OTHER", i), Body: hr})
	}
	out = append(out, Sub{Provider: "jimaku", ID: "1", Language: "ja", TMDb: 800454, Episode: true, Season: 1, Number: 1,
		Release: "[Kurenai] Spring Kurenai - 01 [1080p]", Body: []byte(SRT("おはよう", "春が来た")), Ext: "srt"})
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ServeHTTP routes to the providers.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.host = r.Host
	s.mu.Unlock()
	switch p := r.URL.Path; {
	case strings.HasPrefix(p, "/opensubtitles/"):
		s.count("opensubtitles")
		s.opensubtitles(w, r, strings.TrimPrefix(p, "/opensubtitles/api/v1"))
	case strings.HasPrefix(p, "/subdl/dl/"):
		s.count("subdl")
		s.subdlDownload(w, r, strings.TrimPrefix(p, "/subdl/dl"))
	case strings.HasPrefix(p, "/subdl/"):
		s.count("subdl")
		s.subdl(w, r)
	case strings.HasPrefix(p, "/jimaku/"):
		s.count("jimaku")
		s.jimaku(w, r, strings.TrimPrefix(p, "/jimaku/api"))
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) count(p string) {
	s.mu.Lock()
	s.Requests[p]++
	s.mu.Unlock()
}

// Count returns a counter safely.
func (s *Server) Count(m map[string]int, k string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return m[k]
}

func (s *Server) find(provider string, match func(Sub) bool) []Sub {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Sub
	for _, c := range s.Catalog {
		if c.Provider == provider && match(c) {
			out = append(out, c)
		}
	}
	return out
}

// --- OpenSubtitles ------------------------------------------------------------

func (s *Server) opensubtitles(w http.ResponseWriter, r *http.Request, p string) {
	// The temporary download links need no key (like the real file host).
	if strings.HasPrefix(p, "/file/") {
		id := strings.TrimPrefix(p, "/file/")
		for _, c := range s.find("opensubtitles", func(c Sub) bool { return c.ID == id }) {
			w.Header().Set("Content-Type", "application/x-subrip")
			_, _ = w.Write(c.Body)
			return
		}
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Api-Key") != OSKey {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "You cannot consume this service", "status": 401})
		return
	}
	if ua := r.UserAgent(); ua == "" || !strings.Contains(ua, " v") {
		writeJSON(w, http.StatusForbidden, map[string]any{"message": "User-Agent header is wrong; set it to App name with version eg: MyApp v1.2.3"})
		return
	}
	s.mu.Lock()
	limited := s.OSRateLimit
	// 5 requests per second per client.
	now := time.Now()
	win := s.osWindow["all"]
	for len(win) > 0 && now.Sub(win[0]) > time.Second {
		win = win[1:]
	}
	win = append(win, now)
	s.osWindow["all"] = win
	if len(win) > 5 {
		limited = true
	}
	s.mu.Unlock()
	if limited {
		w.Header().Set("Ratelimit-Reset", "1")
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"message": "API rate limit exceeded"})
		return
	}
	user := ""
	if auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); auth != "" {
		s.mu.Lock()
		user = s.tokens[auth]
		s.mu.Unlock()
		if user == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "invalid token"})
			return
		}
	}
	switch {
	case p == "/login" && r.Method == http.MethodPost:
		var in struct{ Username, Password string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Username != OSUser || in.Password != OSPassword {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "Error, invalid username/password", "status": 401})
			return
		}
		tok := fmt.Sprintf("tok-%d", time.Now().UnixNano())
		s.mu.Lock()
		s.tokens[tok] = in.Username
		quota := s.OSQuota
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"user": map[string]any{"allowed_downloads": quota, "level": "Sub leecher", "user_id": 66, "vip": false},
			"base_url": "api.opensubtitles.com", "token": tok, "status": 200})
	case p == "/infos/formats":
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"output_formats": []string{"srt", "ass", "vtt"}}})
	case p == "/infos/user":
		s.mu.Lock()
		used, quota := s.osUsed[user], s.OSQuota
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"allowed_downloads": quota, "level": "Sub leecher", "user_id": 66,
			"vip": false, "downloads_count": used, "remaining_downloads": max(quota-used, 0)}})
	case p == "/subtitles":
		s.osSearch(w, r)
	case p == "/download" && r.Method == http.MethodPost:
		s.osDownload(w, r, user)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "not found"})
	}
}

func trimTT(s string) string {
	return strings.TrimLeft(strings.TrimPrefix(strings.ToLower(s), "tt"), "0")
}

func (s *Server) osSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	// Parameters must come sorted and in lower case, like the docs ask (otherwise a redirect).
	if raw := r.URL.RawQuery; raw != "" {
		keys := strings.Split(raw, "&")
		sorted := append([]string(nil), keys...)
		sort.Strings(sorted)
		if strings.Join(sorted, "&") != raw || strings.ToLower(raw) != raw {
			w.Header().Set("Location", "/opensubtitles/api/v1/subtitles?"+strings.ToLower(q.Encode()))
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
	}
	langs := strings.Split(q.Get("languages"), ",")
	match := func(c Sub) bool {
		if q.Get("languages") != "" && !contains(langs, c.Language) {
			return false
		}
		switch {
		case q.Get("moviehash") != "" && c.Hash == q.Get("moviehash"):
			return true
		case q.Get("imdb_id") != "":
			return !c.Episode && trimTT(c.IMDb) == q.Get("imdb_id")
		case q.Get("tmdb_id") != "":
			return !c.Episode && strconv.FormatInt(c.TMDb, 10) == q.Get("tmdb_id")
		case q.Get("parent_imdb_id") != "" || q.Get("parent_tmdb_id") != "":
			if !c.Episode || trimTT(c.IMDb) != q.Get("parent_imdb_id") && strconv.FormatInt(c.TMDb, 10) != q.Get("parent_tmdb_id") {
				return false
			}
			return q.Get("season_number") == strconv.Itoa(c.Season) && q.Get("episode_number") == strconv.Itoa(c.Number)
		case q.Get("query") != "":
			words := strings.Fields(strings.ToLower(q.Get("query")))
			rel := strings.ToLower(strings.ReplaceAll(c.Release, ".", " "))
			for _, wd := range words {
				if !strings.Contains(rel, wd) {
					return false
				}
			}
			if c.Episode && q.Get("season_number") != "" {
				return q.Get("season_number") == strconv.Itoa(c.Season) && q.Get("episode_number") == strconv.Itoa(c.Number)
			}
			return true
		}
		return false
	}
	list := s.find("opensubtitles", match)
	data := make([]map[string]any, 0, len(list))
	for _, c := range list {
		fid, _ := strconv.ParseInt(c.ID, 10, 64)
		imdb, _ := strconv.ParseInt(trimTT(c.IMDb), 10, 64)
		fd := map[string]any{"feature_id": 1, "year": c.Year, "title": c.Release, "imdb_id": imdb, "tmdb_id": c.TMDb}
		if c.Episode {
			fd = map[string]any{"feature_type": "Episode", "parent_imdb_id": imdb, "parent_tmdb_id": c.TMDb, "season_number": c.Season, "episode_number": c.Number}
		}
		attr := map[string]any{"subtitle_id": c.ID, "language": c.Language, "download_count": 100 + fid%50, "hearing_impaired": c.HI,
			"foreign_parts_only": c.Forced, "fps": 23.976, "votes": 2, "ratings": 8.0, "from_trusted": c.Trusted, "release": c.Release,
			"machine_translated": false, "ai_translated": false, "url": "https://www.opensubtitles.com/en/subtitles/" + c.ID,
			"uploader": map[string]any{"name": "fake-uploader", "rank": "user"}, "feature_details": fd,
			"files": []map[string]any{{"file_id": fid, "cd_number": 1, "file_name": c.Release + "." + c.Language + ".srt"}}}
		if q.Get("moviehash") != "" {
			attr["moviehash_match"] = c.Hash != "" && c.Hash == q.Get("moviehash")
		}
		data = append(data, map[string]any{"id": c.ID, "type": "subtitle", "attributes": attr})
	}
	writeJSON(w, http.StatusOK, map[string]any{"total_pages": 1, "total_count": len(data), "per_page": 60, "page": 1, "data": data})
}

func (s *Server) osDownload(w http.ResponseWriter, r *http.Request, user string) {
	var in struct {
		FileID int64 `json:"file_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.FileID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "file_id required"})
		return
	}
	reset := time.Now().UTC().Add(7 * time.Hour).Truncate(time.Second)
	who := user
	if who == "" {
		who = "ip"
	}
	s.mu.Lock()
	used := s.osUsed[who]
	quota := s.OSQuota
	s.mu.Unlock()
	if used >= quota {
		writeJSON(w, http.StatusNotAcceptable, map[string]any{"requests": used, "remaining": 0,
			"message":    fmt.Sprintf("You have downloaded your allowed %d subtitles for 24h.Your quota will be renewed in 07 hours", quota),
			"reset_time": "07 hours", "reset_time_utc": reset.Format("2006-01-02T15:04:05.000Z")})
		return
	}
	id := strconv.FormatInt(in.FileID, 10)
	subs := s.find("opensubtitles", func(c Sub) bool { return c.ID == id })
	if len(subs) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "file not found"})
		return
	}
	s.mu.Lock()
	s.osUsed[who]++
	s.Downloads["opensubtitles"]++
	used = s.osUsed[who]
	host := s.host
	s.mu.Unlock()
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	writeJSON(w, http.StatusOK, map[string]any{"link": scheme + "://" + host + "/opensubtitles/api/v1/file/" + id,
		"file_name": subs[0].Release + ".srt", "requests": used, "remaining": quota - used,
		"message": "Your quota will be renewed in 07 hours", "reset_time": "07 hours", "reset_time_utc": reset.Format("2006-01-02T15:04:05.000Z")})
}

// --- SubDL --------------------------------------------------------------------

var subdlLangs = map[string]string{"hr": "HR", "en": "EN", "sr": "SR", "bs": "BS", "ja": "JA"}

func (s *Server) subdl(w http.ResponseWriter, r *http.Request) {
	if s.flag(func() bool { return s.SubDLDown }) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	if q.Get("api_key") != SubDLKey {
		writeJSON(w, http.StatusOK, map[string]any{"status": false, "error": "Invalid API key"})
		return
	}
	langs := strings.Split(strings.ToUpper(q.Get("languages")), ",")
	match := func(c Sub) bool {
		if q.Get("languages") != "" && !contains(langs, subdlLangs[c.Language]) {
			return false
		}
		switch {
		case q.Get("imdb_id") != "" && !strings.EqualFold(q.Get("imdb_id"), c.IMDb):
			return false
		case q.Get("tmdb_id") != "" && q.Get("tmdb_id") != strconv.FormatInt(c.TMDb, 10):
			return false
		case q.Get("imdb_id") == "" && q.Get("tmdb_id") == "" && q.Get("film_name") != "":
			if !strings.Contains(strings.ToLower(strings.ReplaceAll(c.Release, ".", " ")), strings.ToLower(q.Get("film_name"))) {
				return false
			}
		}
		if q.Get("type") == "tv" {
			return c.Episode && q.Get("season_number") == strconv.Itoa(c.Season) && q.Get("episode_number") == strconv.Itoa(c.Number)
		}
		return !c.Episode
	}
	list := s.find("subdl", match)
	subs := make([]map[string]any, 0, len(list))
	var results []map[string]any
	for _, c := range list {
		if results == nil {
			results = []map[string]any{{"imdb_id": c.IMDb, "tmdb_id": c.TMDb, "type": map[bool]string{true: "tv", false: "movie"}[c.Episode], "name": c.Release, "year": c.Year}}
		}
		sub := map[string]any{"release_name": c.Release, "name": "SUBDL::" + c.Release + ".zip", "lang": "croatian", "language": subdlLangs[c.Language],
			"author": "fake", "url": "/subtitle/" + c.ID + ".zip", "hi": c.HI, "full_season": false, "season": nil, "episode": nil}
		if c.Episode {
			sub["season"], sub["episode"] = c.Season, c.Number
		}
		if q.Get("releases") == "1" {
			sub["releases"] = []string{c.Release}
		}
		subs = append(subs, sub)
	}
	if results == nil {
		results = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": true, "results": results, "subtitles": subs})
}

func (s *Server) subdlDownload(w http.ResponseWriter, r *http.Request, p string) {
	id := strings.TrimSuffix(strings.TrimPrefix(p, "/subtitle/"), ".zip")
	subs := s.find("subdl", func(c Sub) bool { return c.ID == id })
	if len(subs) == 0 {
		http.NotFound(w, r)
		return
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create(subs[0].Release + ".srt")
	_, _ = f.Write(subs[0].Body)
	f2, _ := zw.Create("readme.txt")
	_, _ = f2.Write([]byte("downloaded from the fake SubDL"))
	_ = zw.Close()
	s.mu.Lock()
	s.Downloads["subdl"]++
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/zip")
	_, _ = w.Write(buf.Bytes())
}

// --- Jimaku -------------------------------------------------------------------

func (s *Server) jimaku(w http.ResponseWriter, r *http.Request, p string) {
	if s.flag(func() bool { return s.JimakuRateLimit }) {
		w.Header().Set("X-Ratelimit-Limit", "25")
		w.Header().Set("X-Ratelimit-Remaining", "0")
		w.Header().Set("X-Ratelimit-Reset-After", "2.5")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"code": 429, "error": "rate limited"})
		return
	}
	if r.Header.Get("Authorization") != JimakuKey {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"code": 401, "error": "unauthorized"})
		return
	}
	q := r.URL.Query()
	switch {
	case p == "/entries/search":
		var out []map[string]any
		seen := map[int64]bool{}
		for _, c := range s.find("jimaku", func(c Sub) bool {
			if t := q.Get("tmdb_id"); t != "" {
				return t == "tv:"+strconv.FormatInt(c.TMDb, 10) || t == "movie:"+strconv.FormatInt(c.TMDb, 10)
			}
			return strings.Contains(strings.ToLower(c.Release), strings.ToLower(q.Get("query")))
		}) {
			if !seen[c.TMDb] {
				seen[c.TMDb] = true
				out = append(out, map[string]any{"id": c.TMDb, "name": c.Release, "tmdb_id": "tv:" + strconv.FormatInt(c.TMDb, 10),
					"flags": map[string]any{"anime": true}})
			}
		}
		if out == nil {
			out = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, out)
	case strings.HasPrefix(p, "/entries/") && strings.HasSuffix(p, "/files"):
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/entries/"), "/files")
		s.mu.Lock()
		host := s.host
		s.mu.Unlock()
		out := []map[string]any{}
		for _, c := range s.find("jimaku", func(c Sub) bool {
			return strconv.FormatInt(c.TMDb, 10) == id && (q.Get("episode") == "" || q.Get("episode") == strconv.Itoa(c.Number))
		}) {
			name := c.Release + ".srt"
			out = append(out, map[string]any{"url": "http://" + host + "/jimaku/api/file/" + c.ID, "name": name, "size": len(c.Body),
				"last_modified": "2026-01-01T00:00:00Z"})
		}
		writeJSON(w, http.StatusOK, out)
	case strings.HasPrefix(p, "/file/"):
		id := strings.TrimPrefix(p, "/file/")
		for _, c := range s.find("jimaku", func(c Sub) bool { return c.ID == id }) {
			s.mu.Lock()
			s.Downloads["jimaku"]++
			s.mu.Unlock()
			_, _ = w.Write(c.Body)
			return
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
