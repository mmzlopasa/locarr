// Package compat answers the small part of the Seerr, Radarr and Sonarr HTTP
// APIs that media servers such as Streamero use for "discover and request"
// and "upcoming" (D-079). It is mounted outside the generated API, under
// /compat/seerr, /compat/radarr and /compat/sonarr, so it never collides with
// Locarr's own /api/v1. Shapes follow those applications' public API
// documentation; only the fields a client actually reads are filled in.
//
// Every call needs a Locarr API key in X-Api-Key and acts as the key's owner:
// requests go through the requests service with that user's permissions,
// quotas and auto-approval.
package compat

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/discover"
	"github.com/mmzlopasa/locarr/server/internal/requests"
	"github.com/mmzlopasa/locarr/server/internal/version"
)

// Prefix is where the router mounts the handler.
const Prefix = "/compat"

// Handler serves the compatible APIs.
type Handler struct {
	Auth     *auth.Service
	DB       *db.DB
	Discover *discover.Service
	Requests *requests.Service
	Log      *slog.Logger
}

func (h *Handler) log() *slog.Logger {
	if h.Log != nil {
		return h.Log
	}
	return slog.Default()
}

// Routes returns the handler to mount at Prefix.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})
	r.Route("/seerr/api/v1", func(r chi.Router) {
		r.Use(h.authenticate(0))
		r.Get("/status", h.seerrStatus)
		r.Get("/search", h.seerrSearch)
		r.Get("/discover/trending", h.seerrTrending)
		r.Post("/request", h.seerrRequest)
	})
	r.Route("/radarr/api/v3", func(r chi.Router) {
		r.Use(h.authenticate(auth.PermManageLibrary))
		r.Get("/system/status", h.arrStatus)
		r.Get("/calendar", h.radarrCalendar)
		r.Get("/movie", h.radarrMovies)
	})
	r.Route("/sonarr/api/v3", func(r chi.Router) {
		r.Use(h.authenticate(auth.PermManageLibrary))
		r.Get("/system/status", h.arrStatus)
		r.Get("/calendar", h.sonarrCalendar)
		r.Get("/series", h.sonarrSeries)
	})
	// Clients build links to a title's page from the base URL and titleSlug
	// (<base>/movie/<slug>, <base>/series/<slug>); they open Locarr's own page.
	r.Get("/radarr/movie/{slug}", pageRedirect("/movies/"))
	r.Get("/sonarr/series/{slug}", pageRedirect("/series/"))
	return r
}

func pageRedirect(prefix string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "slug"), 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusNotFound, "not_found", "no such title")
			return
		}
		http.Redirect(w, r, prefix+strconv.FormatInt(id, 10), http.StatusFound)
	}
}

type principalKey struct{}

func principal(ctx context.Context) auth.Principal {
	p, _ := ctx.Value(principalKey{}).(auth.Principal)
	return p
}

// authenticate accepts only API keys (never a session cookie, so a browser
// cannot be tricked into calling these routes). A read-only key may only
// read; need is a permission the key's owner must have (0 = none).
func (h *Handler) authenticate(need auth.Permissions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := strings.TrimSpace(r.Header.Get("X-Api-Key"))
			if !strings.HasPrefix(key, auth.APIKeyPrefix) {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "a Locarr API key is required in the X-Api-Key header")
				return
			}
			p, err := h.Auth.Authenticate(r.Context(), key)
			if err != nil {
				if errors.Is(err, auth.ErrUnauthenticated) {
					writeError(w, http.StatusUnauthorized, "unauthenticated", "the API key is not valid")
					return
				}
				h.serverError(w, err)
				return
			}
			if p.ReadOnly && r.Method != http.MethodGet && r.Method != http.MethodHead {
				writeError(w, http.StatusForbidden, "read_only", "this API key is read-only")
				return
			}
			if need != 0 && !p.Can(need) {
				writeError(w, http.StatusForbidden, "forbidden", "the owner of this API key may not read the library")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
		})
	}
}

// status answers the version check of all three APIs.
type status struct {
	AppName   string `json:"appName"`
	Version   string `json:"version"`
	CommitTag string `json:"commitTag,omitempty"`
}

func (h *Handler) seerrStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, status{AppName: "Locarr", Version: version.Version, CommitTag: version.Commit})
}

func (h *Handler) arrStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, status{AppName: "Locarr", Version: version.Version})
}

// errorBody carries both the message field these APIs use and Locarr's code.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, code int, c, msg string) {
	writeJSON(w, code, errorBody{Code: c, Message: msg})
}

func (h *Handler) serverError(w http.ResponseWriter, err error) {
	h.log().Error("compat API", "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "internal error")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
