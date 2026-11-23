// Package fake is a small Streamero server for tests, written from Streamero's
// public OpenAPI description (the parts Locarr uses): system info, password
// sign-in with the two-factor step, sign-out, the current user, users,
// libraries, library scans, library items and item details.
//
// Accounts (password "streamero-pass"): admin (administrator, API key
// AdminKey), ana (viewer), tom (viewer with two-factor code TOTPCode),
// off (disabled). Libraries: Movies (/media/movies) with Sintel (TMDb 45745),
// Shows (/media/shows) with Cosmos Laundromat (TMDb 800434, TVDB 434343),
// Music (/media/music).
package fake

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Fixed credentials.
const (
	AdminKey   = "sk_fakeadminkey"
	ViewerKey  = "sk_fakeviewerkey"
	Password   = "streamero-pass"
	TOTPCode   = "246810"
	MoviesID   = "0190a8b0-0000-7000-8000-000000000101"
	ShowsID    = "0190a8b0-0000-7000-8000-000000000102"
	MusicID    = "0190a8b0-0000-7000-8000-000000000103"
	SintelID   = "0190a8b0-0000-7000-8000-000000000201"
	CosmosID   = "0190a8b0-0000-7000-8000-000000000202"
	AdminID    = "0190a8b0-0000-7000-8000-000000000001"
	AnaID      = "0190a8b0-0000-7000-8000-000000000002"
	TomID      = "0190a8b0-0000-7000-8000-000000000003"
	DisabledID = "0190a8b0-0000-7000-8000-000000000004"
)

// User is an account.
type User struct {
	ID, Username string
	Admin, TOTP  bool
	Disabled     bool
}

// Library is a library.
type Library struct {
	ID, Name, Kind string
	Paths          []string
}

// Item is a title with provider ids.
type Item struct {
	ID, LibraryID, Kind, Title string
	Year                       int
	IDs                        map[string]string
}

// Server is the fake.
type Server struct {
	mu       sync.Mutex
	Users    []User
	Keys     map[string]string // API key → user id
	Libs     []Library
	Items    []Item
	sessions map[string]string // token → user id
	// Scans are the library ids asked to scan, in order.
	Scans []string
	// Logins counts successful sign-ins; Logouts the ended sessions.
	Logins, Logouts int
	// Fail makes every scan answer 500 while true.
	FailScans bool
	// DetailCalls counts item detail requests.
	DetailCalls int
}

// New returns a seeded fake.
func New() *Server {
	s := &Server{}
	s.Reset()
	return s
}

// Reset restores the seed data.
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Users = []User{{ID: AdminID, Username: "admin", Admin: true}, {ID: AnaID, Username: "ana"},
		{ID: TomID, Username: "tom", TOTP: true}, {ID: DisabledID, Username: "off", Disabled: true}}
	s.Keys = map[string]string{AdminKey: AdminID, ViewerKey: AnaID}
	s.Libs = []Library{{ID: MoviesID, Name: "Movies", Kind: "movies", Paths: []string{"/media/movies"}},
		{ID: ShowsID, Name: "Shows", Kind: "shows", Paths: []string{"/media/shows"}},
		{ID: MusicID, Name: "Music", Kind: "music", Paths: []string{"/media/music"}}}
	s.Items = []Item{{ID: SintelID, LibraryID: MoviesID, Kind: "movie", Title: "Sintel", Year: 2010, IDs: map[string]string{"tmdb": "45745", "imdb": "tt1727587"}},
		{ID: CosmosID, LibraryID: ShowsID, Kind: "series", Title: "Cosmos Laundromat", Year: 2015, IDs: map[string]string{"tmdb": "800434", "tvdb": "434343"}}}
	s.sessions = map[string]string{}
	s.Scans, s.Logins, s.Logouts, s.FailScans, s.DetailCalls = nil, 0, 0, false, 0
}

// Snapshot returns the scans and session counters.
func (s *Server) Snapshot() (scans []string, logins, logouts, sessions int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.Scans...), s.Logins, s.Logouts, len(s.sessions)
}

// AddItem adds a title (as if a scan found it).
func (s *Server) AddItem(it Item) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Items = append(s.Items, it)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"code": code, "message": msg})
}

func (s *Server) userJSON(u User) map[string]any {
	return map[string]any{"id": u.ID, "username": u.Username, "isAdmin": u.Admin, "disabled": u.Disabled, "totpEnabled": u.TOTP,
		"permissions": map[string]any{}, "createdAt": "2026-01-01T00:00:00Z"}
}

func (s *Server) caller(r *http.Request) (User, bool) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" {
		tok = r.Header.Get("X-Api-Key")
	}
	id, ok := s.Keys[tok]
	if !ok {
		id, ok = s.sessions[tok]
	}
	if !ok {
		return User{}, false
	}
	for _, u := range s.Users {
		if u.ID == id && !u.Disabled {
			return u, true
		}
	}
	return User{}, false
}

// ServeHTTP implements http.Handler (mount it at the server root or strip a prefix).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := r.URL.Path
	switch {
	case p == "/_fake/state" && r.Method == http.MethodGet:
		writeJSON(w, 200, map[string]any{"scans": s.Scans, "logins": s.Logins, "logouts": s.Logouts, "openSessions": len(s.sessions)})
		return
	case p == "/_fake/reset" && r.Method == http.MethodPost:
		s.mu.Unlock()
		s.Reset()
		s.mu.Lock()
		w.WriteHeader(204)
		return
	case !strings.HasPrefix(p, "/api/v1/"):
		fail(w, 404, "not_found", "no such endpoint")
		return
	}
	p = strings.TrimPrefix(p, "/api/v1")
	switch {
	case p == "/system/info" && r.Method == http.MethodGet:
		writeJSON(w, 200, map[string]any{"id": "0190a8b0-0000-7000-8000-00000000f00d", "name": "Fake Streamero", "product": "Streamero",
			"version": "0.11.4", "commit": "fake", "apiVersion": "1", "setupCompleted": true, "startedAt": "2026-01-01T00:00:00Z"})
		return
	case p == "/auth/login" && r.Method == http.MethodPost:
		s.login(w, r)
		return
	}
	u, ok := s.caller(r)
	if !ok {
		fail(w, 401, "unauthenticated", "not signed in")
		return
	}
	switch {
	case p == "/auth/logout" && r.Method == http.MethodPost:
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if _, ok := s.sessions[tok]; ok {
			delete(s.sessions, tok)
			s.Logouts++
		}
		w.WriteHeader(204)
	case p == "/auth/me" && r.Method == http.MethodGet:
		writeJSON(w, 200, s.userJSON(u))
	case p == "/users" && r.Method == http.MethodGet:
		if !u.Admin {
			fail(w, 403, "forbidden", "admin only")
			return
		}
		out := []any{}
		for _, x := range s.Users {
			out = append(out, s.userJSON(x))
		}
		writeJSON(w, 200, out)
	case p == "/libraries" && r.Method == http.MethodGet:
		out := []any{}
		for _, l := range s.Libs {
			n := 0
			for _, it := range s.Items {
				if it.LibraryID == l.ID {
					n++
				}
			}
			out = append(out, map[string]any{"id": l.ID, "name": l.Name, "kind": l.Kind, "paths": l.Paths, "itemCount": n,
				"scan": map[string]any{"state": "idle"}})
		}
		writeJSON(w, 200, out)
	case strings.HasPrefix(p, "/libraries/") && strings.HasSuffix(p, "/scan") && r.Method == http.MethodPost:
		if !u.Admin {
			fail(w, 403, "forbidden", "admin only")
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/libraries/"), "/scan")
		if !s.hasLib(id) {
			fail(w, 404, "not_found", "no such library")
			return
		}
		if s.FailScans {
			fail(w, 500, "internal", "scan failed")
			return
		}
		s.Scans = append(s.Scans, id)
		w.WriteHeader(202)
	case strings.HasPrefix(p, "/libraries/") && strings.HasSuffix(p, "/items") && r.Method == http.MethodGet:
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/libraries/"), "/items")
		if !s.hasLib(id) {
			fail(w, 404, "not_found", "no such library")
			return
		}
		off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 200 {
			limit = 100
		}
		var all []Item
		for _, it := range s.Items {
			if it.LibraryID == id {
				all = append(all, it)
			}
		}
		items := []any{}
		for i := off; i < len(all) && i < off+limit; i++ {
			it := all[i]
			items = append(items, map[string]any{"id": it.ID, "kind": it.Kind, "title": it.Title, "year": it.Year,
				"addedAt": "2026-01-01T00:00:00Z", "hasPoster": false})
		}
		writeJSON(w, 200, map[string]any{"items": items, "total": len(all)})
	case strings.HasPrefix(p, "/items/") && r.Method == http.MethodGet:
		id := strings.TrimPrefix(p, "/items/")
		s.DetailCalls++
		for _, it := range s.Items {
			if it.ID == id {
				writeJSON(w, 200, map[string]any{"item": map[string]any{"id": it.ID, "kind": it.Kind, "title": it.Title, "year": it.Year,
					"addedAt": "2026-01-01T00:00:00Z", "hasPoster": false}, "libraryId": it.LibraryID, "files": []any{}, "children": []any{},
					"providerIds": it.IDs, "genres": []any{}, "cast": []any{}, "crew": []any{}, "hasBackdrop": false})
				return
			}
		}
		fail(w, 404, "not_found", "no such item")
	default:
		fail(w, 404, "not_found", "no such endpoint")
	}
}

func (s *Server) hasLib(id string) bool {
	for _, l := range s.Libs {
		if l.ID == id {
			return true
		}
	}
	return false
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username, Password, TotpCode string
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, "invalid_request", "bad body")
		return
	}
	for _, u := range s.Users {
		if !strings.EqualFold(u.Username, req.Username) {
			continue
		}
		if req.Password != Password || u.Disabled {
			break
		}
		if u.TOTP {
			if req.TotpCode == "" {
				fail(w, 401, "totp_required", "two-factor code required")
				return
			}
			if req.TotpCode != TOTPCode {
				fail(w, 401, "invalid_totp", "invalid two-factor code")
				return
			}
		}
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		tok := hex.EncodeToString(b)
		s.sessions[tok] = u.ID
		s.Logins++
		writeJSON(w, 200, map[string]any{"token": tok, "sessionId": "0190a8b0-0000-7000-8000-00000000aaaa", "user": s.userJSON(u)})
		return
	}
	fail(w, 401, "invalid_credentials", "invalid username or password")
}
