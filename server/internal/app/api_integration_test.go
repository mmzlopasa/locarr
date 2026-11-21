package app

import (
	"compress/gzip"
	tmdbfake "github.com/mmzlopasa/locarr/server/internal/metadata/tmdb/fake"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/netx"
)

func TestSetupAndCookieSession(t *testing.T) {
	e := newEnv(t, nil)
	var info struct {
		SetupRequired bool
		Product       string
		InstanceName  string
	}
	e.expect(e.do("GET", "/api/v1/system/info", nil), 200).json(&info)
	if !info.SetupRequired || info.Product != "Locarr" || info.InstanceName != "Locarr" {
		t.Fatalf("%+v", info)
	}
	// Everything else needs a session.
	if r := e.expect(e.do("GET", "/api/v1/auth/me", nil), 401); r.code() != "unauthorized" {
		t.Fatal(r.code())
	}
	if r := e.expect(e.do("POST", "/api/v1/setup", map[string]any{"setupCode": "WRONG", "username": "admin", "password": "admin-password"}), 403); r.code() != "bad_setup_code" {
		t.Fatal(r.code())
	}
	r := e.expect(e.do("POST", "/api/v1/setup", map[string]any{"setupCode": strings.ToLower(testSetupCode), "username": "admin",
		"password": "admin-password", "instanceName": "Kino", "useCookie": true, "device": map[string]string{"name": "Firefox", "client": "Web"}}), 200)
	if strings.Contains(string(r.body), `"token"`) {
		t.Fatal("token in body when the cookie was requested")
	}
	cookie := r.Header.Get("Set-Cookie")
	for _, attr := range []string{"locarr_session=", "HttpOnly", "SameSite=Strict", "Path=/"} {
		if !strings.Contains(cookie, attr) {
			t.Fatalf("cookie %q lacks %s", cookie, attr)
		}
	}
	if strings.Contains(cookie, "Secure") {
		t.Fatal("Secure cookie over plain HTTP")
	}
	val := strings.TrimPrefix(strings.Split(cookie, ";")[0], "locarr_session=")
	c := map[string]string{"Cookie": "locarr_session=" + val}
	var me struct {
		Username, Role string
	}
	e.expect(e.req("GET", "/api/v1/auth/me", nil, c), 200).json(&me)
	if me.Username != "admin" || me.Role != "admin" {
		t.Fatalf("%+v", me)
	}
	e.expect(e.do("GET", "/api/v1/system/info", nil), 200).json(&info)
	if info.SetupRequired || info.InstanceName != "Kino" {
		t.Fatalf("%+v", info)
	}
	if r := e.expect(e.do("POST", "/api/v1/setup", map[string]any{"setupCode": testSetupCode, "username": "x", "password": "admin-password"}), 409); r.code() != "setup_done" {
		t.Fatal(r.code())
	}
	// Cross-site requests with the cookie are refused; same-site ones work.
	withOrigin := map[string]string{"Cookie": c["Cookie"], "Origin": "https://evil.example"}
	if r := e.expect(e.req("PATCH", "/api/v1/auth/me", map[string]string{"language": "hr"}, withOrigin), 403); r.code() != "forbidden" {
		t.Fatal(r.code())
	}
	e.expect(e.req("PATCH", "/api/v1/auth/me", map[string]string{"language": "hr"},
		map[string]string{"Cookie": c["Cookie"], "Sec-Fetch-Site": "cross-site"}), 403)
	e.expect(e.req("PATCH", "/api/v1/auth/me", map[string]string{"language": "hr"},
		map[string]string{"Cookie": c["Cookie"], "Sec-Fetch-Site": "same-origin"}), 200)
	e.expect(e.req("PATCH", "/api/v1/auth/me", map[string]string{"language": "hr"},
		map[string]string{"Cookie": c["Cookie"], "Origin": e.srv.URL}), 200)
	// Logout clears the cookie and ends the session.
	r = e.expect(e.req("POST", "/api/v1/auth/logout", nil, c), 204)
	if !strings.Contains(r.Header.Get("Set-Cookie"), "Max-Age=0") {
		t.Fatal(r.Header.Get("Set-Cookie"))
	}
	e.expect(e.req("GET", "/api/v1/auth/me", nil, c), 401)
}

func TestLoginTwoFactorAndSessions(t *testing.T) {
	e := newEnv(t, noLimits)
	e.setup()
	if r := e.expect(e.req("POST", "/api/v1/auth/login", map[string]any{"username": "admin", "password": "nope-nope"}, map[string]string{"Authorization": ""}), 401); r.code() != "invalid_credentials" {
		t.Fatal(r.code())
	}
	other := e.login("ADMIN", "admin-password")
	var setup struct{ Secret, URI string }
	e.expect(e.do("POST", "/api/v1/auth/totp/setup", nil), 200).json(&setup)
	if !strings.HasPrefix(setup.URI, "otpauth://totp/") {
		t.Fatal(setup.URI)
	}
	if r := e.expect(e.do("POST", "/api/v1/auth/totp/enable", map[string]string{"code": "000000"}), 400); r.code() != "invalid_totp" {
		t.Fatal(r.code())
	}
	var codes struct{ Codes []string }
	e.expect(e.do("POST", "/api/v1/auth/totp/enable", map[string]string{"code": currentCode(t, setup.Secret)}), 200).json(&codes)
	if len(codes.Codes) != 10 {
		t.Fatal(codes)
	}
	if r := e.expect(e.req("POST", "/api/v1/auth/login", map[string]any{"username": "admin", "password": "admin-password"}, map[string]string{"Authorization": ""}), 401); r.code() != "totp_required" {
		t.Fatal(r.code())
	}
	e.expect(e.req("POST", "/api/v1/auth/login", map[string]any{"username": "admin", "password": "admin-password",
		"recoveryCode": codes.Codes[0]}, map[string]string{"Authorization": ""}), 200)
	// Sessions: three (setup, login, recovery login); the current one is marked.
	var sessions []struct {
		ID      int64
		Current bool
	}
	e.expect(e.do("GET", "/api/v1/auth/sessions", nil), 200).json(&sessions)
	if len(sessions) != 3 {
		t.Fatal(sessions)
	}
	cur := 0
	for _, s := range sessions {
		if s.Current {
			cur++
		}
	}
	if cur != 1 {
		t.Fatal("current not marked once")
	}
	// Wrong current password: 403, so web clients do not mistake it for a lost session.
	if r := e.expect(e.do("POST", "/api/v1/auth/password", map[string]string{"currentPassword": "bad", "newPassword": "brand-new-pass"}), 403); r.code() != "invalid_credentials" {
		t.Fatal(r.code())
	}
	e.expect(e.do("POST", "/api/v1/auth/password", map[string]string{"currentPassword": "admin-password", "newPassword": "brand-new-pass"}), 204)
	old := e.token
	e.token = other
	e.expect(e.do("GET", "/api/v1/auth/me", nil), 401)
	e.token = old
	e.expect(e.do("GET", "/api/v1/auth/me", nil), 200)
	e.expect(e.do("POST", "/api/v1/auth/totp/disable", map[string]string{"password": "brand-new-pass"}), 204)
}

func TestAPIKeys(t *testing.T) {
	e := newEnv(t, nil)
	e.setup()
	var created struct {
		Key    string
		ApiKey struct{ ID int64 }
	}
	e.expect(e.do("POST", "/api/v1/auth/apikeys", map[string]any{"name": "Dashboard", "readOnly": true}), 201).json(&created)
	if !strings.HasPrefix(created.Key, auth.APIKeyPrefix) {
		t.Fatal(created.Key)
	}
	var keys []map[string]any
	r := e.expect(e.do("GET", "/api/v1/auth/apikeys", nil), 200)
	r.json(&keys)
	if len(keys) != 1 || strings.Contains(string(r.body), created.Key) {
		t.Fatal("key listed in full")
	}
	session := e.token
	e.token = ""
	hdr := map[string]string{"X-Api-Key": created.Key}
	e.expect(e.req("GET", "/api/v1/system/status", nil, hdr), 200)
	if r := e.expect(e.req("PATCH", "/api/v1/settings", map[string]string{"instanceName": "x"}, hdr), 403); r.code() != "forbidden" {
		t.Fatal(r.code())
	}
	// A session token is never taken from X-Api-Key.
	e.expect(e.req("GET", "/api/v1/auth/me", nil, map[string]string{"X-Api-Key": session}), 401)
	e.token = created.Key
	if r := e.expect(e.do("POST", "/api/v1/auth/apikeys", map[string]any{"name": "more"}), 403); r.code() != "forbidden" {
		t.Fatal(r.code()) // read-only refused before session_required
	}
	e.token = session
	var full struct{ Key string }
	e.expect(e.do("POST", "/api/v1/auth/apikeys", map[string]any{"name": "Scripts"}), 201).json(&full)
	e.token = full.Key
	e.expect(e.do("PATCH", "/api/v1/settings", map[string]string{"instanceName": "Via key"}), 200)
	if r := e.expect(e.do("POST", "/api/v1/auth/apikeys", map[string]any{"name": "more"}), 403); r.code() != "session_required" {
		t.Fatal(r.code())
	}
	e.token = session
	e.expect(e.do("DELETE", "/api/v1/auth/apikeys/"+itoa(created.ApiKey.ID), nil), 204)
	e.token = ""
	e.expect(e.req("GET", "/api/v1/system/status", nil, hdr), 401)
}

func TestUserAdministration(t *testing.T) {
	e := newEnv(t, nil)
	e.setup()
	var u struct {
		ID          int64
		Role        string
		Permissions map[string]bool
	}
	e.expect(e.do("POST", "/api/v1/users", map[string]any{"username": "ana", "password": "ana-password", "role": "requester"}), 201).json(&u)
	if u.Role != "requester" || !u.Permissions["requestMovies"] || u.Permissions["manageLibrary"] {
		t.Fatalf("%+v", u)
	}
	if r := e.expect(e.do("POST", "/api/v1/users", map[string]any{"username": "Ana", "password": "ana-password", "role": "requester"}), 409); r.code() != "username_taken" {
		t.Fatal(r.code())
	}
	if r := e.expect(e.do("POST", "/api/v1/users", map[string]any{"username": "x", "password": "short", "role": "requester"}), 400); r.code() != "validation" {
		t.Fatal(r.code())
	}
	if r := e.expect(e.do("POST", "/api/v1/users", map[string]any{"username": "y", "password": "long-enough", "role": "king"}), 400); r.code() != "validation" {
		t.Fatal(r.code())
	}
	admin := e.token
	e.token = e.login("ana", "ana-password")
	for _, p := range []string{"/api/v1/users", "/api/v1/settings", "/api/v1/system/tasks", "/api/v1/system/status", "/api/v1/rootfolders", "/api/v1/filesystem"} {
		if r := e.expect(e.do("GET", p, nil), 403); r.code() != "forbidden" {
			t.Fatal(p, r.code())
		}
	}
	e.expect(e.do("GET", "/api/v1/auth/me", nil), 200)
	anaToken := e.token
	e.token = admin
	var me struct{ ID int64 }
	e.expect(e.do("GET", "/api/v1/auth/me", nil), 200).json(&me)
	if r := e.expect(e.do("PATCH", "/api/v1/users/"+itoa(me.ID), map[string]any{"role": "manager"}), 409); r.code() != "last_admin" {
		t.Fatal(r.code())
	}
	if r := e.expect(e.do("DELETE", "/api/v1/users/"+itoa(me.ID), nil), 409); r.code() != "self_action" {
		t.Fatal(r.code())
	}
	e.expect(e.do("PATCH", "/api/v1/users/"+itoa(u.ID), map[string]any{"role": "manager", "permissions": map[string]bool{
		"requestMovies": true, "requestSeries": false, "requestMusic": false, "autoApprove": true, "request4k": true,
		"manageLibrary": true, "manageRequests": false}}), 200).json(&u)
	if u.Role != "manager" || !u.Permissions["request4k"] || u.Permissions["requestSeries"] {
		t.Fatalf("%+v", u)
	}
	e.expect(e.do("PATCH", "/api/v1/users/"+itoa(u.ID), map[string]any{"disabled": true}), 200)
	e.token = anaToken
	e.expect(e.do("GET", "/api/v1/auth/me", nil), 401)
	e.token = admin
	e.expect(e.do("PATCH", "/api/v1/users/"+itoa(u.ID), map[string]any{"disabled": false}), 200)
	e.expect(e.do("POST", "/api/v1/users/"+itoa(u.ID)+"/password", map[string]any{"password": "ana-new-password"}), 204)
	e.login("ana", "ana-new-password")
	e.expect(e.do("DELETE", "/api/v1/users/"+itoa(u.ID), nil), 204)
	e.expect(e.do("GET", "/api/v1/users/"+itoa(u.ID), nil), 404)
	var users []any
	e.expect(e.do("GET", "/api/v1/users", nil), 200).json(&users)
	if len(users) != 1 {
		t.Fatal(users)
	}
}

func TestSettingsSecretsAreWriteOnly(t *testing.T) {
	e := newEnv(t, nil)
	e.setup()
	secret := tmdbfake.APIKey
	r := e.expect(e.do("PATCH", "/api/v1/settings", map[string]any{"instanceName": "Home", "tmdbApiKey": secret}), 200)
	if strings.Contains(string(r.body), secret) {
		t.Fatal("secret echoed")
	}
	var s struct {
		InstanceName string
		TmdbApiKey   struct{ Configured bool }
	}
	r = e.expect(e.do("GET", "/api/v1/settings", nil), 200)
	r.json(&s)
	if s.InstanceName != "Home" || !s.TmdbApiKey.Configured || strings.Contains(string(r.body), secret) {
		t.Fatalf("%s", r.body)
	}
	// Not in the database file in clear either.
	_, _ = e.deps.DB.W.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	raw, err := os.ReadFile(e.deps.DB.Path)
	if err != nil || strings.Contains(string(raw), secret) {
		t.Fatal("secret stored in clear", err)
	}
	e.expect(e.do("PATCH", "/api/v1/settings", map[string]any{"tmdbApiKey": ""}), 200).json(&s)
	if s.TmdbApiKey.Configured {
		t.Fatal("not cleared")
	}
}

func TestRootFoldersAndBrowse(t *testing.T) {
	e := newEnv(t, nil)
	e.setup()
	var f struct {
		ID             int64
		Path           string
		Accessible     bool
		Writable       bool
		FreeSpaceBytes int64
	}
	e.expect(e.do("POST", "/api/v1/rootfolders", map[string]any{"path": filepath.Join(e.data, "media", "movies"), "kind": "movies"}), 201).json(&f)
	if !f.Accessible || !f.Writable || f.FreeSpaceBytes <= 0 {
		t.Fatalf("%+v", f)
	}
	for _, p := range []string{e.data + "/../", "/etc", filepath.Join(e.data, "media", "..", "..")} {
		if r := e.expect(e.do("POST", "/api/v1/rootfolders", map[string]any{"path": p, "kind": "movies"}), 400); r.code() != "path_not_allowed" {
			t.Fatal(p, r.code())
		}
	}
	if r := e.expect(e.do("POST", "/api/v1/rootfolders", map[string]any{"path": filepath.Join(e.data, "media", "movies"), "kind": "movies"}), 409); r.code() != "already_exists" {
		t.Fatal(r.code())
	}
	if r := e.expect(e.do("POST", "/api/v1/rootfolders", map[string]any{"path": filepath.Join(e.data, "nope"), "kind": "music"}), 404); r.code() != "path_not_found" {
		t.Fatal(r.code())
	}
	var listing struct {
		Path, Parent string
		Entries      []struct{ Name, Path string }
	}
	e.expect(e.do("GET", "/api/v1/filesystem", nil), 200).json(&listing)
	if len(listing.Entries) != 1 || listing.Entries[0].Path != e.data {
		t.Fatalf("%+v", listing)
	}
	e.expect(e.do("GET", "/api/v1/filesystem?path="+filepath.Join(e.data, "media"), nil), 200).json(&listing)
	if len(listing.Entries) != 2 || listing.Parent != e.data {
		t.Fatalf("%+v", listing)
	}
	e.expect(e.do("GET", "/api/v1/filesystem?path=/etc", nil), 400)
	var all []any
	e.expect(e.do("GET", "/api/v1/rootfolders", nil), 200).json(&all)
	if len(all) != 1 {
		t.Fatal(all)
	}
	e.expect(e.do("DELETE", "/api/v1/rootfolders/"+itoa(f.ID), nil), 204)
	if _, err := os.Stat(filepath.Join(e.data, "media", "movies")); err != nil {
		t.Fatal("removing a root folder touched the disk")
	}
}

func TestTasksAPI(t *testing.T) {
	e := newEnv(t, nil)
	e.setup()
	var list struct {
		Scheduled []struct {
			Name            string
			IntervalSeconds int64
		}
		Jobs []struct {
			ID    int64
			Kind  string
			State string
		}
	}
	e.expect(e.do("GET", "/api/v1/system/tasks", nil), 200).json(&list)
	names := map[string]bool{}
	for _, s := range list.Scheduled {
		names[s.Name] = true
	}
	if !names[TaskPing] || !names[TaskHousekeeping] {
		t.Fatalf("%+v", list.Scheduled)
	}
	var job struct {
		ID    int64
		State string
	}
	e.expect(e.do("POST", "/api/v1/system/tasks/ping/run", nil), 202).json(&job)
	deadline := time.Now().Add(10 * time.Second)
	for {
		e.expect(e.do("GET", "/api/v1/system/tasks?state=succeeded", nil), 200).json(&list)
		found := false
		for _, j := range list.Jobs {
			if j.ID == job.ID {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ping did not succeed")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if r := e.expect(e.do("POST", "/api/v1/system/tasks/nope/run", nil), 404); r.code() != "unknown_task" {
		t.Fatal(r.code())
	}
	if r := e.expect(e.do("POST", "/api/v1/system/jobs/"+itoa(job.ID)+"/cancel", nil), 409); r.code() != "conflict" {
		t.Fatal(r.code())
	}
	e.expect(e.do("GET", "/api/v1/system/tasks?state=bogus", nil), 400)
	var st struct {
		Version           string
		DatabaseSizeBytes int64
	}
	e.expect(e.do("GET", "/api/v1/system/status", nil), 200).json(&st)
	if st.DatabaseSizeBytes <= 0 || st.Version == "" {
		t.Fatalf("%+v", st)
	}
}

func TestHTTPHardening(t *testing.T) {
	e := newEnv(t, nil)
	r := e.expect(e.do("GET", "/", nil), 200)
	if !strings.Contains(r.Header.Get("Content-Security-Policy"), "script-src 'self'") ||
		r.Header.Get("X-Frame-Options") != "DENY" || r.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers %v", r.Header)
	}
	if r.Header.Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS over plain HTTP")
	}
	r = e.expect(e.req("GET", "/", nil, map[string]string{"X-Forwarded-Proto": "https"}), 200)
	if r.Header.Get("Strict-Transport-Security") == "" {
		t.Fatal("no HSTS over HTTPS")
	}
	// Client routes fall back to the app; scanner paths and robots.txt do not.
	e.expect(e.do("GET", "/settings/users", nil), 200)
	for _, p := range []string{"/.env", "/.git/config", "/wp-login.php", "/backup.sql"} {
		e.expect(e.do("GET", p, nil), 404)
	}
	if r := e.expect(e.do("GET", "/robots.txt", nil), 200); !strings.Contains(string(r.body), "Disallow: /") {
		t.Fatal(string(r.body))
	}
	// JSON errors everywhere under /api.
	for _, p := range []string{"/api/v1/nope", "/api/v2/x", "/api/"} {
		r := e.expect(e.do("GET", p, nil), 404)
		if r.code() != "not_found" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			t.Fatal(p, string(r.body))
		}
	}
	if r := e.expect(e.do("DELETE", "/api/v1/system/info", nil), 405); r.code() != "method_not_allowed" {
		t.Fatal(r.code())
	}
	if r := e.expect(e.do("POST", "/api/v1/auth/login", "{not json"), 400); r.code() != "validation" {
		t.Fatal(r.code())
	}
	big := `{"username":"` + strings.Repeat("a", 2<<20) + `","password":"x"}`
	if r := e.expect(e.do("POST", "/api/v1/auth/login", big), 413); r.code() != "request_too_large" {
		t.Fatal(r.code())
	}
	if r := e.do("GET", "/api/v1/system/info", nil); r.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("api cacheable")
	}
	// No CORS.
	if r := e.req("OPTIONS", "/api/v1/auth/login", nil, map[string]string{"Origin": "https://evil.example",
		"Access-Control-Request-Method": "POST"}); r.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS allowed")
	}
	// Gzip for JSON and HTML.
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/system/info", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	res, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("Content-Encoding") != "gzip" {
		t.Fatal("not compressed")
	}
	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); !strings.Contains(string(b), "Locarr") {
		t.Fatal(string(b))
	}
	e.expect(e.do("GET", "/healthz", nil), 200)
	e.expect(e.do("GET", "/readyz", nil), 200)
}

func TestRateLimitAndBan(t *testing.T) {
	e := newEnv(t, func(o *RouterOptions) {
		o.AuthLimiter = netx.NewLimiter(1, 3)
		o.AuthBans = netx.NewBanlist(15, time.Minute, time.Hour)
	})
	bad := map[string]any{"username": "x", "password": "wrong-pass"}
	for range 3 {
		e.expect(e.do("POST", "/api/v1/auth/login", bad), 401)
	}
	r := e.expect(e.do("POST", "/api/v1/auth/login", bad), 429)
	if r.code() != "rate_limited" || r.Header.Get("Retry-After") == "" {
		t.Fatal(r.code(), r.Header)
	}
	// Other endpoints are not limited.
	for range 5 {
		e.expect(e.do("GET", "/api/v1/system/info", nil), 200)
	}
}

func TestStartingHandler(t *testing.T) {
	h := startingHandler()
	for path, want := range map[string]int{"/healthz": 200, "/readyz": 503, "/api/v1/system/info": 503} {
		rec := newRecorder()
		h.ServeHTTP(rec, mustReq(path))
		if rec.Code != want {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
}
