package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/mediaserver/streamero/fake"
	"github.com/mmzlopasa/locarr/server/internal/netx"
)

type hookReceiver struct {
	mu     sync.Mutex
	events []map[string]any
	sigs   []string
}

func (h *hookReceiver) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	h.mu.Lock()
	h.events = append(h.events, m)
	h.sigs = append(h.sigs, r.Header.Get("X-Locarr-Signature"))
	h.mu.Unlock()
}

func (h *hookReceiver) has(typ string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.events {
		if e["event"] == typ {
			return true
		}
	}
	return false
}

func startStreamero(t *testing.T) (*fake.Server, string) {
	t.Helper()
	f := fake.New()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

func TestStreameroLibraryUpdatesEndToEnd(t *testing.T) {
	le := newLibraryEnv(t)
	st, stURL := startStreamero(t)
	le.deps.Streamero.Debounce = 300 * time.Millisecond

	// Test before saving: libraries with the matching root folder suggested.
	var test struct {
		Ok         bool
		ErrorCode  string
		ServerName string
		Libraries  []struct {
			ID                     string
			Name                   string
			SuggestedRootFolderIds []int64
		}
	}
	mappings := []map[string]string{{"locarrPath": le.movies, "streameroPath": "/media/movies"}}
	le.expect(le.do("POST", "/api/v1/mediaserver/streamero/test", map[string]any{"url": stURL, "apiKey": "sk_nope"}), 200).json(&test)
	if test.Ok || test.ErrorCode != "auth_failed" {
		t.Fatalf("%+v", test)
	}
	le.expect(le.do("PUT", "/api/v1/mediaserver/streamero", map[string]any{"enabled": false, "url": stURL, "pathMappings": mappings}), 200)
	le.expect(le.do("POST", "/api/v1/mediaserver/streamero/test", map[string]any{"url": stURL, "apiKey": fake.AdminKey}), 200).json(&test)
	if !test.Ok || test.ServerName != "Fake Streamero" || len(test.Libraries) != 3 || len(test.Libraries[0].SuggestedRootFolderIds) != 1 ||
		test.Libraries[0].SuggestedRootFolderIds[0] != le.rootID {
		t.Fatalf("%+v", test)
	}
	var cfg struct {
		Enabled bool
		ApiKey  struct{ Configured bool }
		Status  struct{ IndexedItems int }
	}
	r := le.expect(le.do("PUT", "/api/v1/mediaserver/streamero", map[string]any{"enabled": true, "url": stURL + "/", "apiKey": fake.AdminKey,
		"pathMappings": mappings, "libraryMap": []map[string]any{{"rootFolderId": le.rootID, "libraryIds": []string{fake.MoviesID}}}}), 200)
	r.json(&cfg)
	if !cfg.Enabled || !cfg.ApiKey.Configured || strings.Contains(string(r.body), fake.AdminKey) {
		t.Fatalf("%s", r.body)
	}

	// A shared webhook for library events.
	hook := &hookReceiver{}
	hookSrv := httptest.NewServer(hook)
	t.Cleanup(hookSrv.Close)
	le.expect(le.do("POST", "/api/v1/notificationchannels", map[string]any{"name": "Hook", "kind": "webhook",
		"events":   []string{"library.grabbed", "library.imported"},
		"settings": map[string]any{"url": hookSrv.URL}, "secrets": map[string]any{"secret": "s3cret"}}), 201)

	m := le.addMovie(10378, true)
	waitFor(t, "the grab", func() bool { return le.downloadCount(m.ID) == 1 })
	le.poll()
	le.client.finish(t)
	le.poll()
	waitFor(t, "the import", func() bool { return le.movie(m.ID).File != nil })
	waitFor(t, "the Streamero scan", func() bool {
		scans, _, _, _ := st.Snapshot()
		return len(scans) == 1
	})
	if scans, _, _, _ := st.Snapshot(); scans[0] != fake.MoviesID {
		t.Fatal(scans)
	}
	waitFor(t, "the timeline entry", func() bool {
		for _, ev := range le.timeline(m.ID).Events {
			if ev.Type == "notified" && strings.Contains(ev.Message, "notified Streamero: scanning Movies") {
				return true
			}
		}
		return false
	})
	waitFor(t, "the webhook", func() bool { return hook.has("library.grabbed") && hook.has("library.imported") })
	if !strings.HasPrefix(hook.sigs[0], "sha256=") {
		t.Fatal(hook.sigs)
	}

	// The index refresh after the scan finds what Streamero has.
	le.expect(le.do("POST", "/api/v1/mediaserver/streamero/sync", nil), 202)
	waitFor(t, "the index", func() bool {
		le.expect(le.do("GET", "/api/v1/mediaserver/streamero", nil), 200).json(&cfg)
		return cfg.Status.IndexedItems == 2
	})
	var look []struct {
		TmdbId       int64
		StreameroUrl string
	}
	le.expect(le.do("GET", "/api/v1/movies/lookup?term=sintel", nil), 200).json(&look)
	if len(look) == 0 || look[0].StreameroUrl != stURL+"/item/"+fake.SintelID {
		t.Fatalf("%+v", look)
	}
	var title struct {
		Item struct{ StreameroUrl string }
	}
	le.expect(le.do("GET", "/api/v1/discover/movies/45745", nil), 200).json(&title)
	if title.Item.StreameroUrl == "" {
		t.Fatal("discover title without the Streamero link")
	}
}

func TestStreameroSignInAndPermissions(t *testing.T) {
	e := newEnv(t, noLimits)
	e.setup()
	_, stURL := startStreamero(t)
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"allowedHosts": []string{"127.0.0.1"}}), 200)
	var providers []struct{ ID string }
	e.expect(e.do("GET", "/api/v1/auth/providers", nil), 200).json(&providers)
	if len(providers) != 1 {
		t.Fatal(providers)
	}
	e.expect(e.do("PUT", "/api/v1/mediaserver/streamero", map[string]any{"enabled": true, "url": stURL, "apiKey": fake.AdminKey,
		"signIn": map[string]any{"enabled": true, "autoCreate": false}}), 200)
	e.expect(e.do("GET", "/api/v1/auth/providers", nil), 200).json(&providers)
	if len(providers) != 2 || providers[1].ID != "streamero" {
		t.Fatal(providers)
	}
	anon := map[string]string{"Authorization": ""}
	login := func(user, pw, code string) resp {
		body := map[string]any{"username": user, "password": pw}
		if code != "" {
			body["totpCode"] = code
		}
		return e.req("POST", "/api/v1/auth/providers/streamero/login", body, anon)
	}
	if r := e.expect(login("ana", "wrong-pass", ""), 401); r.code() != "invalid_credentials" {
		t.Fatal(r.code())
	}
	if r := e.expect(login("ana", fake.Password, ""), 403); r.code() != "identity_not_linked" {
		t.Fatal(r.code())
	}
	e.expect(e.req("POST", "/api/v1/auth/providers/nope/login", map[string]any{"username": "a", "password": "b"}, anon), 404)

	// The admin links tom (Streamero, 2FA) to a local manager.
	e.user("tomislav", "manager", nil)
	var users []struct {
		ID           string
		Username     string
		IsAdmin      bool
		LinkedUserId int64
	}
	e.expect(e.do("GET", "/api/v1/mediaserver/streamero/users", nil), 200).json(&users)
	var tomID string
	for _, u := range users {
		if u.Username == "tom" {
			tomID = u.ID
		}
	}
	var list []struct {
		ID       int64
		Username string
	}
	e.expect(e.do("GET", "/api/v1/users", nil), 200).json(&list)
	var localID int64
	for _, u := range list {
		if u.Username == "tomislav" {
			localID = u.ID
		}
	}
	e.expect(e.do("PUT", fmt.Sprintf("/api/v1/users/%d/identities/streamero", localID), map[string]any{"subject": tomID, "username": "tom"}), 200)
	if r := e.expect(login("tom", fake.Password, ""), 401); r.code() != "totp_required" {
		t.Fatal(r.code())
	}
	var signed struct {
		Token string
		User  struct {
			Username string
			Role     string
		}
	}
	e.expect(login("tom", fake.Password, fake.TOTPCode), 200).json(&signed)
	if signed.User.Username != "tomislav" || signed.User.Role != "manager" || signed.Token == "" {
		t.Fatalf("%+v", signed)
	}

	// Auto-create on: the Streamero administrator becomes a requester.
	e.expect(e.do("PUT", "/api/v1/mediaserver/streamero", map[string]any{"enabled": true, "url": stURL,
		"signIn": map[string]any{"enabled": true, "autoCreate": true}}), 200)
	e.expect(login("admin", fake.Password, ""), 200).json(&signed)
	if signed.User.Role != "requester" || signed.User.Username != "admin-2" {
		t.Fatalf("%+v", signed.User)
	}
	req := signed.Token
	var ids []struct{ Provider, Subject string }
	e.as(req, func() {
		e.expect(e.do("GET", "/api/v1/auth/identities", nil), 200).json(&ids)
		if len(ids) != 1 || ids[0].Subject != fake.AdminID {
			t.Fatalf("%+v", ids)
		}
		// Requesters: no Streamero settings, no shared channels, no webhooks of their own.
		e.expect(e.do("GET", "/api/v1/mediaserver/streamero", nil), 403)
		e.expect(e.do("GET", "/api/v1/mediaserver/streamero/users", nil), 403)
		e.expect(e.do("POST", "/api/v1/mediaserver/streamero/sync", nil), 403)
		e.expect(e.do("GET", "/api/v1/notificationchannels", nil), 403)
		e.expect(e.do("GET", "/api/v1/settings/notifications", nil), 403)
		e.expect(e.do("POST", "/api/v1/notificationchannels", map[string]any{"name": "x", "kind": "ntfy", "events": []string{"request.available"},
			"settings": map[string]any{"topic": "t"}}), 403)
		e.expect(e.do("POST", "/api/v1/notificationchannels", map[string]any{"name": "x", "kind": "webhook", "personal": true,
			"events": []string{"request.available"}, "settings": map[string]any{"url": "https://example.com/h"}}), 403)
		e.expect(e.do("POST", "/api/v1/notificationchannels", map[string]any{"name": "x", "kind": "ntfy", "personal": true,
			"events": []string{"library.imported"}, "settings": map[string]any{"topic": "t"}}), 403)
		e.expect(e.do("POST", "/api/v1/notificationchannels", map[string]any{"name": "Phone", "kind": "ntfy", "personal": true,
			"events": []string{"request.available"}, "settings": map[string]any{"topic": "ana-phone"}, "secrets": map[string]any{"token": "tk_123"}}), 201)
		var mine []struct {
			ID         int64
			SecretsSet []string
		}
		r := e.expect(e.do("GET", "/api/v1/notificationchannels?personal=true", nil), 200)
		r.json(&mine)
		if len(mine) != 1 || len(mine[0].SecretsSet) != 1 || strings.Contains(string(r.body), "tk_123") {
			t.Fatalf("%s", r.body)
		}
	})
	// The admin sees no personal channels in the shared list, and cannot open another user's.
	var shared []any
	e.expect(e.do("GET", "/api/v1/notificationchannels", nil), 200).json(&shared)
	if len(shared) != 0 {
		t.Fatal(shared)
	}
	var mineID int64
	_ = e.deps.DB.R.QueryRow(`SELECT id FROM notification_channels`).Scan(&mineID)
	e.expect(e.do("GET", fmt.Sprintf("/api/v1/notificationchannels/%d", mineID), nil), 404)
	e.expect(e.do("DELETE", fmt.Sprintf("/api/v1/notificationchannels/%d", mineID), nil), 404)

	// Read-only API keys cannot change anything.
	var key struct{ Key string }
	e.expect(e.do("POST", "/api/v1/auth/apikeys", map[string]any{"name": "ro", "readOnly": true}), 201).json(&key)
	e.as(key.Key, func() {
		e.expect(e.do("GET", "/api/v1/mediaserver/streamero", nil), 200)
		e.expect(e.do("PUT", "/api/v1/mediaserver/streamero", map[string]any{"enabled": false, "url": stURL}), 403)
		e.expect(e.do("POST", "/api/v1/notificationchannels/test", map[string]any{"channel": map[string]any{"name": "x", "kind": "ntfy",
			"events": []string{}, "settings": map[string]any{"topic": "t"}}}), 403)
	})
	// Unlinking.
	e.expect(e.do("DELETE", fmt.Sprintf("/api/v1/users/%d/identities/streamero", localID), nil), 204)
	e.expect(e.do("DELETE", fmt.Sprintf("/api/v1/users/%d/identities/streamero", localID), nil), 404)
}

func TestProviderSignInIsRateLimitedAndBanned(t *testing.T) {
	if !rateLimited(httptest.NewRequest("POST", "/api/v1/auth/providers/streamero/login", nil)) ||
		!rateLimited(httptest.NewRequest("POST", "/api/v1/auth/identities/streamero", nil)) ||
		rateLimited(httptest.NewRequest("GET", "/api/v1/auth/providers", nil)) {
		t.Fatal("provider sign-in is not treated like a password sign-in")
	}
	e := newEnv(t, func(o *RouterOptions) {
		o.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}
		o.AuthBans = netx.NewBanlist(3, time.Minute, time.Hour)
	})
	e.setup()
	_, stURL := startStreamero(t)
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"allowedHosts": []string{"127.0.0.1"}}), 200)
	e.expect(e.do("PUT", "/api/v1/mediaserver/streamero", map[string]any{"enabled": true, "url": stURL, "apiKey": fake.AdminKey,
		"signIn": map[string]any{"enabled": true, "autoCreate": true}}), 200)
	hdr := map[string]string{"Authorization": "", "X-Forwarded-For": "203.0.113.7"}
	bad := map[string]any{"username": "ana", "password": "wrong-pass"}
	for range 3 {
		e.expect(e.req("POST", "/api/v1/auth/providers/streamero/login", bad, hdr), 401)
	}
	// Banned now, even with the right password.
	e.expect(e.req("POST", "/api/v1/auth/providers/streamero/login", map[string]any{"username": "ana", "password": fake.Password}, hdr), 429)
	// Local sign-in shares the ban.
	e.expect(e.req("POST", "/api/v1/auth/login", map[string]any{"username": "admin", "password": "admin-password"}, hdr), 429)
}
