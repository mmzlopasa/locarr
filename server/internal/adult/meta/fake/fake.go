// Package fake serves StashDB- and ThePornDB-shaped answers (from their
// public API documentation) over a small synthetic, non-explicit data set,
// for tests and the dev fakes. Images are plain grey squares.
package fake

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Keys the fakes accept.
const (
	StashKey = "stash-test-key"
	TPDBKey  = "tpdb-test-key"
)

// Scene is one synthetic entry.
type Scene struct {
	ID, TPDBID, Title, Date, Studio string
	Performers                      []string
	Tags                            []string
	Deleted, Movie                  bool
	PHash                           string
}

// Server is both fakes; mount StashDB at /graphql and TPDB at /tpdb.
type Server struct {
	mu       sync.Mutex
	Scenes   []Scene
	Requests int
	base     string
}

// New returns the fakes with the default data set; base is where they are
// reachable (for image URLs).
func New(base string) *Server {
	return &Server{base: strings.TrimRight(base, "/"), Scenes: []Scene{
		{ID: "sc-1", TPDBID: "tp-1", Title: "Scene Title", Date: "2024-01-15", Studio: "Studio A", Performers: []string{"Performer One"},
			Tags: []string{"Outdoor"}, PHash: "a1b2c3d4e5f60718"},
		{ID: "sc-2", TPDBID: "tp-2", Title: "Second Scene", Date: "2024-02-01", Studio: "Studio A", Performers: []string{"Performer One", "Performer Two"}},
		{ID: "sc-3", TPDBID: "tp-3", Title: "Removed Scene", Date: "2024-02-10", Studio: "Studio A", Performers: []string{"Performer Two"}, Deleted: true},
		{ID: "sc-4", TPDBID: "tp-4", Title: "Tagged Scene", Date: "2024-03-01", Studio: "Studio A", Performers: []string{"Performer Two"},
			Tags: []string{"Blockedtagexample"}},
		{ID: "mv-1", TPDBID: "tm-1", Title: "Movie Title", Date: "2023-06-01", Studio: "Studio A", Performers: []string{"Performer One"}, Movie: true},
	}}
}

// SetBase sets the address image URLs point to.
func (s *Server) SetBase(b string) { s.mu.Lock(); s.base = strings.TrimRight(b, "/"); s.mu.Unlock() }

// ServeHTTP routes to the right fake.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.Requests++
	s.mu.Unlock()
	switch {
	case strings.HasSuffix(r.URL.Path, "/img/grey.png"):
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(Grey())
	case strings.HasSuffix(r.URL.Path, "/graphql"):
		s.stash(w, r)
	case strings.Contains(r.URL.Path, "/tpdb/"):
		s.tpdb(w, r)
	default:
		http.NotFound(w, r)
	}
}

// Grey is a small grey placeholder PNG.
func Grey() []byte {
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = 128
	}
	_ = color.Gray{}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func (s *Server) img() string { s.mu.Lock(); defer s.mu.Unlock(); return s.base + "/img/grey.png" }

func slug(name string) string { return strings.ToLower(strings.ReplaceAll(name, " ", "-")) }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) list() []Scene {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Scene{}, s.Scenes...)
}

// --- StashDB ---

var rootField = regexp.MustCompile(`\{\s*(\w+)`)

func (s *Server) stashScene(sc Scene) map[string]any {
	var perfs []map[string]any
	for _, p := range sc.Performers {
		perfs = append(perfs, map[string]any{"performer": map[string]any{"id": "pf-" + slug(p), "name": p, "disambiguation": "", "deleted": false}})
	}
	var tags []map[string]any
	for _, t := range sc.Tags {
		tags = append(tags, map[string]any{"name": t})
	}
	fp := []map[string]any{}
	if sc.PHash != "" {
		fp = append(fp, map[string]any{"hash": sc.PHash, "algorithm": "PHASH"})
	}
	return map[string]any{"id": sc.ID, "title": sc.Title, "details": "Synthetic test entry.", "release_date": sc.Date, "duration": 1800,
		"deleted": sc.Deleted, "images": []map[string]any{{"url": s.img(), "width": 8, "height": 8}},
		"studio": map[string]any{"id": "st-" + slug(sc.Studio), "name": sc.Studio}, "performers": perfs, "tags": tags, "fingerprints": fp}
}

func (s *Server) stash(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("ApiKey") != StashKey {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	q := req.Query
	if i := strings.Index(q, "{"); i >= 0 {
		q = q[i:]
	}
	m := rootField.FindStringSubmatch(q)
	if m == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	term, _ := req.Variables["term"].(string)
	id, _ := req.Variables["id"].(string)
	data := map[string]any{}
	switch m[1] {
	case "me":
		data["me"] = map[string]any{"id": "u-1"}
	case "searchScene":
		out := []map[string]any{}
		for _, sc := range s.list() {
			if !sc.Movie && strings.Contains(strings.ToLower(sc.Title+" "+sc.Studio), strings.ToLower(term)) {
				out = append(out, s.stashScene(sc))
			}
		}
		data["searchScene"] = out
	case "findScene":
		data["findScene"] = nil
		for _, sc := range s.list() {
			if sc.ID == id {
				data["findScene"] = s.stashScene(sc)
			}
		}
	case "searchPerformer", "findPerformer":
		var out []map[string]any
		for _, p := range s.performers() {
			if (m[1] == "findPerformer" && "pf-"+slug(p) == id) || (m[1] == "searchPerformer" && strings.Contains(strings.ToLower(p), strings.ToLower(term))) {
				out = append(out, map[string]any{"id": "pf-" + slug(p), "name": p, "disambiguation": "", "aliases": []string{}, "deleted": false,
					"images": []map[string]any{{"url": s.img()}}})
			}
		}
		if m[1] == "findPerformer" {
			if len(out) == 0 {
				data[m[1]] = nil
			} else {
				data[m[1]] = out[0]
			}
		} else {
			data[m[1]] = append([]map[string]any{}, out...)
		}
	case "searchStudio", "findStudio":
		studio := map[string]any{"id": "st-studio-a", "name": "Studio A", "deleted": false, "urls": []map[string]any{{"url": "https://studio-a.invalid"}},
			"images": []map[string]any{{"url": s.img()}}, "parent": nil}
		if m[1] == "findStudio" {
			data[m[1]] = nil
			if id == "st-studio-a" {
				data[m[1]] = studio
			}
		} else {
			out := []map[string]any{}
			if strings.Contains("studio a", strings.ToLower(term)) {
				out = append(out, studio)
			}
			data[m[1]] = out
		}
	case "queryScenes":
		in, _ := req.Variables["input"].(map[string]any)
		out := []map[string]any{}
		list := s.list()
		sortBy, _ := in["sort"].(string)
		dir, _ := in["direction"].(string)
		sortScenes(list, sortBy == "TITLE", dir == "ASC")
		text, _ := in["text"].(string)
		for _, sc := range list {
			if sc.Movie {
				continue
			}
			if text != "" && !strings.Contains(strings.ToLower(sc.Title+" "+sc.Studio+" "+strings.Join(sc.Performers, " ")), strings.ToLower(text)) {
				continue
			}
			if f, ok := in["studios"].(map[string]any); ok && !hasValue(f, "st-"+slug(sc.Studio)) {
				continue
			}
			if f, ok := in["performers"].(map[string]any); ok {
				hit := false
				for _, p := range sc.Performers {
					hit = hit || hasValue(f, "pf-"+slug(p))
				}
				if !hit {
					continue
				}
			}
			out = append(out, s.stashScene(sc))
		}
		page, per := num(in["page"], 1), num(in["per_page"], 25)
		data["queryScenes"] = map[string]any{"count": len(out), "scenes": pageOf(out, page, per)}
	default:
		writeJSON(w, map[string]any{"errors": []map[string]any{{"message": "unknown field " + m[1]}}})
		return
	}
	writeJSON(w, map[string]any{"data": data})
}

// sortScenes orders by date (newest first unless asc) or title.
func sortScenes(list []Scene, byTitle, asc bool) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i].Date, list[j].Date
		if byTitle {
			a, b = list[i].Title, list[j].Title
		}
		if asc {
			return a < b
		}
		return a > b
	})
}

func num(v any, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	}
	return def
}

func pageOf(list []map[string]any, page, per int) []map[string]any {
	page, per = max(page, 1), max(per, 1)
	from := min((page-1)*per, len(list))
	return list[from:min(from+per, len(list))]
}

func hasValue(f map[string]any, id string) bool {
	vals, _ := f["value"].([]any)
	for _, v := range vals {
		if v == id {
			return true
		}
	}
	return false
}

func (s *Server) performers() []string {
	seen := map[string]bool{}
	var out []string
	for _, sc := range s.list() {
		for _, p := range sc.Performers {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// --- ThePornDB ---

func (s *Server) tpdbScene(sc Scene) map[string]any {
	var perfs []map[string]any
	for _, p := range sc.Performers {
		perfs = append(perfs, map[string]any{"id": "tpf-" + slug(p), "name": p, "image": s.img()})
	}
	var tags []map[string]any
	for _, t := range sc.Tags {
		tags = append(tags, map[string]any{"name": t})
	}
	var deleted any
	if sc.Deleted {
		deleted = "2024-03-01T00:00:00Z"
	}
	typ := "Scene"
	if sc.Movie {
		typ = "Movie"
	}
	return map[string]any{"id": sc.TPDBID, "title": sc.Title, "type": typ, "date": sc.Date, "description": "Synthetic test entry.", "duration": 1800,
		"image": s.img(), "site": map[string]any{"uuid": "site-" + slug(sc.Studio), "id": 7, "name": sc.Studio, "url": "https://studio-a.invalid"},
		"performers": perfs, "tags": tags, "hashes": []map[string]any{{"hash": sc.PHash, "type": "PHASH"}}, "deleted_at": deleted}
}

func (s *Server) tpdb(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+TPDBKey {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	path := r.URL.Path[strings.Index(r.URL.Path, "/tpdb/")+len("/tpdb"):]
	q := r.URL.Query()
	parts := strings.Split(strings.Trim(path, "/"), "/")
	term := strings.ToLower(q.Get("q"))
	switch {
	case len(parts) == 1 && (parts[0] == "scenes" || parts[0] == "movies"):
		movie := parts[0] == "movies"
		out := []map[string]any{}
		list := s.list()
		if q.Get("orderBy") == "recently_released" {
			sortScenes(list, false, false)
		}
		for _, sc := range list {
			if sc.Movie != movie {
				continue
			}
			if term != "" && term != "a" && !strings.Contains(strings.ToLower(sc.Title+" "+sc.Studio), term) {
				continue
			}
			if id := q.Get("site_id"); id != "" && id != "site-"+slug(sc.Studio) {
				continue
			}
			if id := q.Get("performer_id"); id != "" {
				hit := false
				for _, p := range sc.Performers {
					hit = hit || id == "tpf-"+slug(p)
				}
				if !hit {
					continue
				}
			}
			out = append(out, s.tpdbScene(sc))
		}
		page, per := num(q.Get("page"), 1), num(q.Get("per_page"), 25)
		last := max((len(out)+max(per, 1)-1)/max(per, 1), 1)
		writeJSON(w, map[string]any{"data": pageOf(out, page, per), "meta": map[string]any{"total": len(out), "current_page": page, "last_page": last}})
	case len(parts) == 2 && (parts[0] == "scenes" || parts[0] == "movies"):
		for _, sc := range s.list() {
			if sc.TPDBID == parts[1] {
				writeJSON(w, map[string]any{"data": s.tpdbScene(sc)})
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	case parts[0] == "performers":
		out := []map[string]any{}
		for _, p := range s.performers() {
			e := map[string]any{"id": "tpf-" + slug(p), "name": p, "image": s.img()}
			if len(parts) == 2 && parts[1] == "tpf-"+slug(p) {
				writeJSON(w, map[string]any{"data": e})
				return
			}
			if strings.Contains(strings.ToLower(p), term) {
				out = append(out, e)
			}
		}
		if len(parts) == 2 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"data": out})
	case parts[0] == "sites":
		site := map[string]any{"uuid": "site-studio-a", "id": 7, "name": "Studio A", "url": "https://studio-a.invalid", "logo": s.img()}
		if len(parts) == 2 {
			if parts[1] == "site-studio-a" {
				writeJSON(w, map[string]any{"data": site})
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		out := []map[string]any{}
		if strings.Contains("studio a", term) {
			out = append(out, site)
		}
		writeJSON(w, map[string]any{"data": out})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}
