// Package app wires the server together: storage, services, jobs and the HTTP router.
package app

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/mmzlopasa/locarr/server/internal/api"
	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/compat"
	"github.com/mmzlopasa/locarr/server/internal/netx"
	"github.com/mmzlopasa/locarr/server/internal/webui"
)

// RouterOptions configure the HTTP router.
type RouterOptions struct {
	TrustedProxies []netip.Prefix
	// AuthLimiter limits sign-in style requests per client address (nil = none).
	AuthLimiter *netx.Limiter
	// AuthBans blocks a client that keeps failing them (nil = never).
	AuthBans *netx.Banlist
	// Web serves the web client (nil = the embedded build).
	Web http.Handler
	Log *slog.Logger
}

// maxJSONBody caps API request bodies (the generated decoders read them into memory).
const maxJSONBody = 1 << 20

// requestTimeout bounds every request.
const requestTimeout = 60 * time.Second

// rateLimited lists requests that carry a password, a setup code or a
// two-factor code: they are rate-limited and count towards a ban when refused.
func rateLimited(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v1/auth/login", "/api/v1/setup", "/api/v1/auth/password",
		"/api/v1/auth/totp/enable", "/api/v1/auth/totp/disable", "/api/v1/auth/totp/recovery-codes",
		"/api/v1/auth/invite", "/api/v1/auth/signup":
		return true
	}
	// Sign-in through a provider and linking one's own account carry the
	// provider's password (D-063).
	if strings.HasPrefix(r.URL.Path, "/api/v1/auth/providers/") && strings.HasSuffix(r.URL.Path, "/login") {
		return true
	}
	return strings.HasPrefix(r.URL.Path, "/api/v1/auth/identities/")
}

// NewRouter builds the HTTP handler for a ready server.
func NewRouter(h *api.Handler, o RouterOptions) (http.Handler, error) {
	apiServer, err := gen.NewServer(h, api.Security{Auth: h.Auth},
		gen.WithPathPrefix("/api/v1"), gen.WithErrorHandler(api.ErrorHandler),
		gen.WithNotFound(api.NotFound), gen.WithMethodNotAllowed(api.MethodNotAllowed))
	if err != nil {
		return nil, err
	}
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	r := chi.NewRouter()
	// chi's RealIP trusts spoofable headers; the client address comes from netx
	// with an explicit list of trusted proxies instead.
	r.Use(middleware.Recoverer, accessLog(log), middleware.Timeout(requestTimeout), securityHeaders,
		netx.Middleware(o.TrustedProxies), requestContext, sameOrigin)
	if o.AuthLimiter != nil {
		r.Use(netx.Limit(o.AuthLimiter, rateLimited))
	}
	if o.AuthBans != nil {
		r.Use(netx.Guard(o.AuthBans, rateLimited))
	}
	r.Get("/healthz", health)
	r.Get("/readyz", ready)
	compress := compressor()
	r.Mount("/api/v1", compress(limitBody(flushStream(apiServer))))
	r.Handle("/api/*", http.HandlerFunc(api.NotFound))
	// The Seerr/Radarr/Sonarr compatible routes for media servers (D-079).
	compatAPI := &compat.Handler{Auth: h.Auth, DB: h.DB, Discover: h.Discover, Requests: h.Requests, Log: log}
	r.Mount(compat.Prefix, compress(limitBody(compatAPI.Routes())))
	web := o.Web
	if web == nil {
		web = webui.Handler()
	}
	r.Handle("/*", webClientHeaders(compress(web)))
	return r, nil
}

func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func ready(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// startingHandler answers while the server is starting: health is fine,
// readiness and the API say "starting".
func startingHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			health(w, r)
			return
		}
		w.Header().Set("Retry-After", "2")
		if r.URL.Path == "/readyz" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "starting"})
			return
		}
		api.WriteError(w, http.StatusServiceUnavailable, "starting", "server is starting")
	})
}

// requestContext turns the web client's session cookie or an X-Api-Key header
// into a bearer token (unless the request already has one) and records what
// handlers need to know about the request.
func requestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("Authorization") == "" {
			if key := strings.TrimSpace(r.Header.Get("X-Api-Key")); strings.HasPrefix(key, auth.APIKeyPrefix) {
				r.Header.Set("Authorization", "Bearer "+key)
			} else if c, err := r.Cookie(api.SessionCookie); err == nil && c.Value != "" {
				r.Header.Set("Authorization", "Bearer "+c.Value)
				r = r.WithContext(withCookieAuth(r.Context()))
			}
		}
		ctx := api.WithRequest(r.Context(), api.RequestInfo{Method: r.Method, Secure: isHTTPS(r), ClientIP: netx.FromContext(r.Context())})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isHTTPS reports whether the client connection is HTTPS. X-Forwarded-Proto
// only decides the cookie's Secure flag and HSTS, which cannot weaken sign-in,
// so it is believed without a trusted-proxy list (browsers ignore HSTS sent
// over plain HTTP).
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// sameOrigin refuses cross-site state-changing API requests from browsers. With
// SameSite=Strict cookies, JSON-only bodies and no CORS this is a second line of
// defence (D-005): requests that carry their own Authorization header (apps,
// scripts) are not affected, since a browser cannot add one cross-site.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && !safeMethod(r.Method) && (r.Header.Get("Authorization") == "" || cookieAuth(r.Context())) {
			if !sameOriginRequest(r) {
				api.WriteError(w, http.StatusForbidden, "forbidden", "cross-site request refused")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sameOriginRequest(r *http.Request) bool {
	// Every current browser sends Sec-Fetch-Site; it is reliable even behind a
	// reverse proxy that rewrites Host.
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
	default:
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // not a cross-site browser request
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	return strings.EqualFold(u.Host, host)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		if isHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// webClientCSP allows only the server's own scripts, styles, images and API.
// Inline styles are allowed for style attributes set by components.
const webClientCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'"

func webClientHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", webClientCSP)
		next.ServeHTTP(w, r)
	})
}

// limitBody caps request bodies; a larger body gets 413.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxJSONBody {
			api.WriteError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body too large")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
		next.ServeHTTP(w, r)
	})
}

// compressor gzips text responses for clients that accept it.
func compressor() func(http.Handler) http.Handler {
	return middleware.Compress(5, "text/html", "text/css", "text/plain", "text/javascript", "application/javascript",
		"application/json", "application/manifest+json", "image/svg+xml")
}

// accessLog logs each request at debug level, and server errors as warnings.
func accessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			level := slog.LevelDebug
			if status >= 500 {
				level = slog.LevelWarn
			}
			log.Log(r.Context(), level, "http request", "method", r.Method, "path", r.URL.Path, "status", status,
				"bytes", ww.BytesWritten(), "took_ms", time.Since(start).Milliseconds(),
				"ip", netx.FromContext(r.Context()).String())
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
