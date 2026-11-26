package app

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/adult"
	afake "github.com/mmzlopasa/locarr/server/internal/adult/meta/fake"
	"github.com/mmzlopasa/locarr/server/internal/config"
	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	ifake "github.com/mmzlopasa/locarr/server/internal/indexers/fake"
	"github.com/mmzlopasa/locarr/server/internal/library"
)

// Synthetic, non-explicit release names only.
const (
	adultRelease  = "StudioA.24.01.15.Performer.One.Scene.Title.1080p.WEB-DL.x264-GRP"
	blockedRelase = "StudioA.24.01.15.Blockedtagexample.Scene.Title.2160p.WEB-DL.x264-GRP"
)

type bodyHook struct {
	mu     sync.Mutex
	bodies map[string][]string
}

func (h *bodyHook) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	h.mu.Lock()
	if h.bodies == nil {
		h.bodies = map[string][]string{}
	}
	h.bodies[r.URL.Path] = append(h.bodies[r.URL.Path], string(b))
	h.mu.Unlock()
}

func (h *bodyHook) all(path string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.Join(h.bodies[path], "\n")
}

type adultEnv struct {
	*libraryEnv
	adultRoot string
}

// newAdultEnv is a library environment with the StashDB/TPDB fakes, a fake
// indexer offering adult (6xxx) releases and a download client.
func newAdultEnv(t *testing.T) *adultEnv {
	t.Helper()
	meta := afake.New("")
	metaSrv := httptest.NewServer(meta)
	t.Cleanup(metaSrv.Close)
	meta.SetBase(metaSrv.URL)

	saved := ifake.Catalog
	ifake.Catalog = append(append([]ifake.Release{}, saved...),
		ifake.Release{Title: adultRelease, Cat: 6010, Size: 900 << 20, Seeders: 40, Peers: 41, Hash: "9ab2c3d4e5f60718293a4b5c6d7e8f9012345678", Age: 5 * time.Hour},
		ifake.Release{Title: blockedRelase, Cat: 6010, Size: 950 << 20, Seeders: 90, Peers: 91, Hash: "9bb2c3d4e5f60718293a4b5c6d7e8f9012345678", Age: 5 * time.Hour})
	t.Cleanup(func() { ifake.Catalog = saved })

	e := newEnvWith(t, func(c *config.Config) {
		c.StashDBURL, c.TPDBURL = metaSrv.URL+"/graphql", metaSrv.URL+"/tpdb"
	}, noLimits)
	e.setup()
	_, base := startFakes(t)
	le := &libraryEnv{env: e, movies: filepath.Join(e.data, "media", "movies"), downloads: filepath.Join(e.data, "downloads")}
	le.client = newFlowClient(le.downloads)
	e.deps.Downloads.NewClient = func(downloaders.Implementation, downloaders.Config) (downloaders.Client, error) {
		return le.client, nil
	}
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"allowedHosts": []string{"127.0.0.1", "localhost"}}), 200)
	e.expect(e.do("POST", "/api/v1/indexers", map[string]any{"name": "Fake Torznab", "kind": "torznab", "baseUrl": base + "/torznab",
		"apiKey": "fakekey"}), 201)
	e.expect(e.do("POST", "/api/v1/downloadclients", map[string]any{"name": "Flow", "implementation": "apifake", "host": "flow.lan",
		"password": "x", "categories": map[string]string{"movies": "movies", "series": "", "music": ""}}), 201)
	var profiles []struct {
		ID         int64
		Name, Kind string
	}
	e.expect(e.do("GET", "/api/v1/qualityprofiles", nil), 200).json(&profiles)
	for _, p := range profiles {
		if p.Kind == "movies" && p.Name == "HD-720p/1080p" {
			le.profileID = p.ID
		}
	}
	return &adultEnv{libraryEnv: le, adultRoot: filepath.Join(e.data, "media", "adult")}
}

type apiUser struct {
	ID          int64
	Permissions map[string]bool
}

func (ae *adultEnv) me() apiUser {
	var u apiUser
	ae.expect(ae.do("GET", "/api/v1/auth/me", nil), 200).json(&u)
	return u
}

func (ae *adultEnv) grant(id int64, role string, view bool) apiUser {
	var u apiUser
	ae.expect(ae.do("PATCH", "/api/v1/users/"+itoa(id), map[string]any{"role": role, "permissions": map[string]bool{
		"requestMovies": true, "requestSeries": true, "requestMusic": true, "autoApprove": false, "request4k": false,
		"manageLibrary": role != "requester", "manageRequests": false, "viewAdult": view}}), 200).json(&u)
	return u
}

func (ae *adultEnv) as(token string, method, path string, body any) resp {
	return ae.req(method, path, body, map[string]string{"Authorization": "Bearer " + token})
}

// adultGets are every adult read endpoint (ids filled in later).
func adultGets(itemID, studioID, perfID int64, image string) []string {
	return []string{"/api/v1/adult/preferences", "/api/v1/adult/rootfolders", "/api/v1/adult/studios", "/api/v1/adult/performers",
		"/api/v1/adult/items", "/api/v1/adult/items?missing=true", "/api/v1/adult/lookup?source=stashdb&type=scene&term=scene",
		fmt.Sprintf("/api/v1/adult/studios/%d", studioID), fmt.Sprintf("/api/v1/adult/performers/%d", perfID),
		fmt.Sprintf("/api/v1/adult/items/%d", itemID), fmt.Sprintf("/api/v1/adult/items/%d/timeline", itemID),
		"/api/v1/adult/calendar?start=2024-01-01&end=2024-12-31", "/api/v1/adult/images/" + image, "/api/v1/adult/exclusions",
		"/api/v1/adult/items?status=wanted&sort=added&monitored=true", "/api/v1/adult/items?kind=gallery",
		fmt.Sprintf("/api/v1/adult/items/%d/images", itemID), fmt.Sprintf("/api/v1/adult/items/%d/images/1", itemID),
		fmt.Sprintf("/api/v1/adult/items/%d/images/1?thumb=true", itemID)}
}

func adultWrites(itemID, studioID int64) [][2]string {
	return [][2]string{{"POST", "/api/v1/adult/studios"}, {"POST", "/api/v1/adult/items"}, {"POST", "/api/v1/adult/performers"},
		{"POST", "/api/v1/adult/rootfolders"}, {"PATCH", fmt.Sprintf("/api/v1/adult/items/%d", itemID)},
		{"POST", fmt.Sprintf("/api/v1/adult/items/%d/search", itemID)}, {"GET", fmt.Sprintf("/api/v1/adult/items/%d/releases", itemID)},
		{"POST", fmt.Sprintf("/api/v1/adult/items/%d/rename", itemID)}, {"DELETE", fmt.Sprintf("/api/v1/adult/items/%d", itemID)},
		{"POST", fmt.Sprintf("/api/v1/adult/studios/%d/refresh", studioID)}, {"DELETE", fmt.Sprintf("/api/v1/adult/studios/%d", studioID)},
		{"POST", "/api/v1/adult/items/bulk"}, {"DELETE", "/api/v1/adult/exclusions/1"}, {"POST", "/api/v1/adult/performers/1/refresh"},
		{"DELETE", "/api/v1/adult/performers/1?removeScenes=true"}, {"POST", "/api/v1/adult/galleries"},
		{"PATCH", "/api/v1/adult/performers/1"}}
}

type apiAdultItem struct {
	ID         int64
	Ref        string
	Title      string
	Studio     string
	Monitored  bool
	Image      string
	SourceTags []string
	File       *struct{ Path, Quality string }
}

// TestAdultLibraryEndToEnd: off by default → enable with confirmation →
// grant → add studio (removed and blocked entries skipped) → scene grabbed
// through the fake client (blocked release rejected) → imported → renamed →
// timeline; and every leak path is closed for everyone else.
func TestAdultLibraryEndToEnd(t *testing.T) {
	ae := newAdultEnv(t)
	hook := &bodyHook{}
	hookSrv := httptest.NewServer(hook)
	t.Cleanup(hookSrv.Close)
	ae.expect(ae.do("POST", "/api/v1/notificationchannels", map[string]any{"name": "Plain", "kind": "webhook",
		"events": []string{"library.grabbed", "library.imported", "library.import_failed"}, "settings": map[string]any{"url": hookSrv.URL + "/plain"}}), 201)
	ae.expect(ae.do("POST", "/api/v1/notificationchannels", map[string]any{"name": "Adult", "kind": "webhook",
		"events": []string{"adult.grabbed", "adult.imported"}, "settings": map[string]any{"url": hookSrv.URL + "/adult"}}), 201)

	// Off by default: even the administrator sees nothing but the settings.
	admin := ae.me()
	if admin.Permissions["viewAdult"] {
		t.Fatal("view_adult granted by default")
	}
	for _, p := range adultGets(1, 1, 1, strings.Repeat("a", 32)) {
		if r := ae.do("GET", p, nil); r.StatusCode != 404 {
			t.Fatalf("disabled %s: %d %s", p, r.StatusCode, r.body)
		}
	}
	ae.expect(ae.do("GET", "/api/v1/adult/settings", nil), 200)

	// Other accounts: a manager (default permissions) and a requester.
	var mgr, rq apiUser
	ae.expect(ae.do("POST", "/api/v1/users", map[string]any{"username": "mgr", "password": "manager-pw1", "role": "manager"}), 201).json(&mgr)
	ae.expect(ae.do("POST", "/api/v1/users", map[string]any{"username": "kid", "password": "requester-pw", "role": "requester"}), 201).json(&rq)
	if mgr.Permissions["viewAdult"] || rq.Permissions["viewAdult"] {
		t.Fatal("view_adult granted by default")
	}
	mgrTok, rqTok := ae.login("mgr", "manager-pw1"), ae.login("kid", "requester-pw")
	if r := ae.as(mgrTok, "GET", "/api/v1/adult/settings", nil); r.StatusCode != 404 {
		t.Fatalf("manager settings: %d", r.StatusCode)
	}

	// Enabling needs the confirmation; keys are checked with the source.
	if r := ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"enabled": true}), 400); r.code() != "adult_confirm_required" {
		t.Fatal(r.code())
	}
	if r := ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"stashdbKey": "wrong"}), 400); r.code() != "metadata_key_invalid" {
		t.Fatal(r.code())
	}
	r := ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"enabled": true, "confirmAdults": true,
		"stashdbKey": afake.StashKey, "tpdbKey": afake.TPDBKey, "defaultProfileId": ae.profileID}), 200)
	if strings.Contains(string(r.body), afake.StashKey) || !strings.Contains(string(r.body), `"stashdbKeySet":true`) {
		t.Fatalf("%s", r.body)
	}
	// Enabled, but nobody holds view_adult yet.
	if r := ae.do("GET", "/api/v1/adult/studios", nil); r.StatusCode != 404 {
		t.Fatalf("admin without view_adult: %d", r.StatusCode)
	}
	// A requester can never hold it.
	if u := ae.grant(rq.ID, "requester", true); u.Permissions["viewAdult"] {
		t.Fatal("requester got view_adult")
	}
	var adminFull struct {
		ID          int64
		Permissions map[string]bool
	}
	ae.expect(ae.do("PATCH", "/api/v1/users/"+itoa(admin.ID), map[string]any{"permissions": map[string]bool{"requestMovies": true,
		"requestSeries": true, "requestMusic": true, "autoApprove": true, "request4k": true, "manageLibrary": true, "manageRequests": true,
		"viewAdult": true}}), 200).json(&adminFull)
	if !adminFull.Permissions["viewAdult"] {
		t.Fatal("grant failed")
	}
	var me struct{ AdultVisible bool }
	ae.expect(ae.do("GET", "/api/v1/auth/me", nil), 200).json(&me)
	if !me.AdultVisible {
		t.Fatal("adultVisible")
	}

	// Blocklist: the owner's list starts empty (D-109); add a term the fake data uses.
	var terms []struct {
		ID      int64
		Term    string
		Builtin bool
	}
	ae.expect(ae.do("GET", "/api/v1/adult/blocklist", nil), 200).json(&terms)
	if len(terms) != 0 {
		t.Fatalf("seeded terms: %+v", terms)
	}
	ae.expect(ae.do("POST", "/api/v1/adult/blocklist", map[string]any{"term": "Blockedtagexample"}), 201)

	// Root folder, then a studio: its scenes come in, removed and blocked ones do not.
	ae.expect(ae.do("POST", "/api/v1/adult/rootfolders", map[string]any{"path": ae.adultRoot}), 201)
	var lookup struct {
		Results []struct {
			ID, Name, Image string
		}
	}
	ae.expect(ae.do("GET", "/api/v1/adult/lookup?source=stashdb&type=studio&term=studio", nil), 200).json(&lookup)
	if len(lookup.Results) != 1 || lookup.Results[0].Image == "" {
		t.Fatalf("lookup %+v", lookup)
	}
	ae.expect(ae.do("GET", "/api/v1/adult/lookup?source=tpdb&type=scene&term=scene", nil), 200).json(&lookup)
	for _, l := range lookup.Results {
		if l.Name == "Removed Scene" || l.Name == "Tagged Scene" {
			t.Fatalf("lookup leaked %s", l.Name)
		}
	}
	var studio struct {
		ID     int64
		Scenes int
	}
	ae.expect(ae.do("POST", "/api/v1/adult/studios", map[string]any{"source": "stashdb", "id": lookupID(t, ae, "studio"), "monitored": true,
		"search": false}), 201).json(&studio)
	if studio.Scenes != 2 {
		t.Fatalf("scenes %d", studio.Scenes)
	}
	var page struct {
		Items []apiAdultItem
		Total int
	}
	ae.expect(ae.do("GET", "/api/v1/adult/items?kind=scene", nil), 200).json(&page)
	var scene apiAdultItem
	for _, it := range page.Items {
		if it.Title == "Removed Scene" || it.Title == "Tagged Scene" {
			t.Fatalf("imported %s", it.Title)
		}
		if it.Title == "Scene Title" {
			scene = it
		}
	}
	if scene.ID == 0 || scene.Image == "" || scene.Monitored {
		t.Fatalf("scene %+v (older scenes start unmonitored)", scene)
	}
	// Adding a removed scene directly is refused.
	if r := ae.expect(ae.do("POST", "/api/v1/adult/items", map[string]any{"source": "stashdb", "id": "sc-3"}), 422); r.code() != "adult_entry_removed" {
		t.Fatal(r.code())
	}
	if r := ae.expect(ae.do("POST", "/api/v1/adult/items", map[string]any{"source": "tpdb", "id": "tp-4"}), 422); r.code() != "blocked_tag" {
		t.Fatal(r.code())
	}

	// Images: private, permitted users only.
	img := ae.expect(ae.do("GET", "/api/v1/adult/images/"+scene.Image, nil), 200)
	if cc := img.Header.Get("Cache-Control"); !strings.Contains(cc, "private") {
		t.Fatalf("cache-control %q", cc)
	}

	// No indexer is opted in to adult categories: a search finds nothing.
	ae.expect(ae.do("PATCH", fmt.Sprintf("/api/v1/adult/items/%d", scene.ID), map[string]any{"monitored": true}), 200)
	if _, err := ae.deps.Wanted.Search(t.Context(), refOf(scene.Ref), "manual"); err != nil {
		t.Fatal(err)
	}
	if ae.adultDownloads() != 0 {
		t.Fatal("grabbed without an opted-in indexer")
	}
	if _, err := ae.deps.DB.W.Exec(`UPDATE indexers SET categories = json_set(categories, '$.adult', json('[6000]'))`); err != nil {
		t.Fatal(err)
	}

	// Search → the blocked release is rejected, the other grabbed.
	ae.expect(ae.do("POST", fmt.Sprintf("/api/v1/adult/items/%d/search", scene.ID), nil), 202)
	waitFor(t, "the adult grab", func() bool { return ae.adultDownloads() == 1 })
	var title string
	_ = ae.deps.DB.R.QueryRow(`SELECT title FROM downloads WHERE json_extract(item_ref, '$.ref') = ?`, scene.Ref).Scan(&title)
	if title != adultRelease {
		t.Fatalf("grabbed %q", title)
	}
	// Leak checks while the download is in the queue.
	ae.checkLeaks(t, mgrTok, rqTok, scene, studio.ID)

	ae.poll()
	ae.client.finish(t)
	ae.poll()
	waitFor(t, "the adult import", func() bool {
		var it apiAdultItem
		ae.expect(ae.do("GET", fmt.Sprintf("/api/v1/adult/items/%d", scene.ID), nil), 200).json(&it)
		scene.File = it.File
		return it.File != nil
	})
	base := filepath.Base(scene.File.Path)
	if !strings.HasPrefix(scene.File.Path, filepath.Join(ae.adultRoot, "Studio A")+string(os.PathSeparator)) ||
		!strings.HasPrefix(base, "Studio A - 2024-01-15 - Scene Title [Performer One]") {
		t.Fatalf("path %s", scene.File.Path)
	}
	var tl apiTimeline
	ae.expect(ae.do("GET", fmt.Sprintf("/api/v1/adult/items/%d/timeline", scene.ID), nil), 200).json(&tl)
	types := tl.types()
	for _, want := range []string{"added", "decision", "file_imported", "renamed"} {
		if !strings.Contains(types, want) {
			t.Fatalf("timeline %s lacks %s", types, want)
		}
	}
	decisions := ""
	for _, ev := range tl.Events {
		if ev.Type == "decision" {
			decisions += string(ev.Data)
		}
	}
	if !strings.Contains(decisions, "blocked_tag") {
		t.Fatalf("decision without blocked_tag: %s", decisions)
	}
	if tl.Explanation.Code == "" {
		t.Fatal("no explanation")
	}

	// No subtitle work (search, translation) is ever queued for adult items.
	var subs int
	_ = ae.deps.DB.R.QueryRow(`SELECT COUNT(*) FROM subtitle_state WHERE item_ref LIKE 'scene:%' OR item_ref LIKE 'adult_movie:%'`).Scan(&subs)
	if subs != 0 {
		t.Fatalf("subtitle state for adult items: %d", subs)
	}
	if n, err := ae.deps.Subtitles.TranslateDue(t.Context()); err != nil || n != 0 {
		t.Fatalf("translation ran for adult items: %d %v", n, err)
	}

	// Channels: only the one that chose adult events hears about it.
	waitFor(t, "the adult webhook", func() bool { return strings.Contains(hook.all("/adult"), "adult.imported") })
	if p := hook.all("/plain"); strings.Contains(p, "Scene Title") || strings.Contains(p, "StudioA") || strings.Contains(p, "adult") {
		t.Fatalf("plain channel got adult events: %s", p)
	}
	ae.checkLeaks(t, mgrTok, rqTok, scene, studio.ID)

	// Import blocklist: a file named with a blocked term is not imported.
	blocked := filepath.Join(ae.data, "downloads", blockedRelase)
	writeMovieFolder(t, blocked, blockedRelase)
	if _, err := ae.deps.Adult.ImportFile(t.Context(), mustItem(t, ae, scene.ID), filepath.Join(blocked, blockedRelase+".mkv"), blockedRelase, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "blocked term") {
		t.Fatalf("blocked import: %v", err)
	}

	// Rename preview keeps the name; the stats aggregate is opt-in.
	ae.expect(ae.do("POST", fmt.Sprintf("/api/v1/adult/items/%d/rename", scene.ID), nil), 200)
	ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"statsAggregate": true}), 200)
	ae.deps.Stats.Invalidate()
	st := ae.expect(ae.do("GET", "/api/stats", nil), 200)
	if !strings.Contains(string(st.body), `"adultItems":2`) || strings.Contains(string(st.body), "Scene Title") {
		t.Fatalf("stats %s", st.body)
	}

	// Disabled again: everything is 404, even for the permitted administrator.
	ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"enabled": false}), 200)
	for _, p := range adultGets(scene.ID, studio.ID, 1, scene.Image) {
		if r := ae.do("GET", p, nil); r.StatusCode != 404 {
			t.Fatalf("disabled again %s: %d", p, r.StatusCode)
		}
	}
	ae.deps.Stats.Invalidate()
	if st := ae.expect(ae.do("GET", "/api/stats", nil), 200); strings.Contains(string(st.body), "adultItems") {
		t.Fatalf("stats while disabled %s", st.body)
	}
	if q := ae.expect(ae.do("GET", "/api/v1/queue", nil), 200); strings.Contains(string(q.body), "Scene.Title") {
		t.Fatal("queue shows adult while disabled")
	}
}

func refOf(s string) library.Ref {
	r, _ := library.ParseRef(s)
	return r
}

func mustItem(t *testing.T, ae *adultEnv, id int64) *adult.Item {
	t.Helper()
	it, err := ae.deps.Adult.Item(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return &it
}

func lookupID(t *testing.T, ae *adultEnv, typ string) string {
	t.Helper()
	var l struct{ Results []struct{ ID string } }
	ae.expect(ae.do("GET", "/api/v1/adult/lookup?source=stashdb&type="+typ+"&term=studio", nil), 200).json(&l)
	if len(l.Results) == 0 {
		t.Fatal("no lookup result")
	}
	return l.Results[0].ID
}

func (ae *adultEnv) adultDownloads() int {
	var n int
	_ = ae.deps.DB.R.QueryRow(`SELECT COUNT(*) FROM downloads WHERE json_extract(item_ref, '$.ref') LIKE 'scene:%'`).Scan(&n)
	return n
}

// checkLeaks: nothing adult reaches the manager without view_adult, the
// requester, a read-only key without "include adult", the shared lists,
// the generic timeline, the board, history, calendar, compat routes or stats.
func (ae *adultEnv) checkLeaks(t *testing.T, mgrTok, rqTok string, scene apiAdultItem, studioID int64) {
	t.Helper()
	var noFlag, withFlag struct{ Key string }
	ae.expect(ae.do("POST", "/api/v1/auth/apikeys", map[string]any{"name": "dash", "readOnly": true}), 201).json(&noFlag)
	ae.expect(ae.do("POST", "/api/v1/auth/apikeys", map[string]any{"name": "dash-adult", "readOnly": true, "includeAdult": true}), 201).json(&withFlag)
	for _, tok := range []string{mgrTok, rqTok, noFlag.Key} {
		for _, p := range adultGets(scene.ID, studioID, 1, scene.Image) {
			if r := ae.as(tok, "GET", p, nil); r.StatusCode != 404 {
				t.Fatalf("%s leaked to %.8s: %d", p, tok, r.StatusCode)
			}
		}
		for _, w := range adultWrites(scene.ID, studioID) {
			if r := ae.as(tok, w[0], w[1], map[string]any{"source": "stashdb", "id": "sc-1", "path": "/data/x", "ids": []int64{scene.ID}, "action": "remove"}); r.StatusCode != 404 && (tok != noFlag.Key || r.StatusCode != 403) {
				t.Fatalf("%s %s for %.8s: %d", w[0], w[1], tok, r.StatusCode)
			}
		}
		for _, p := range []string{"/api/v1/adult/settings", "/api/v1/adult/blocklist"} {
			if r := ae.as(tok, "GET", p, nil); r.StatusCode != 404 {
				t.Fatalf("%s for %.8s: %d", p, tok, r.StatusCode)
			}
		}
	}
	// The key with the flag reads the adult library.
	ae.expect(ae.as(withFlag.Key, "GET", "/api/v1/adult/items", nil), 200)
	// The generic timeline never answers for adult refs, not even the owner.
	for _, p := range []string{"/api/v1/timeline?item=" + scene.Ref, "/api/v1/timeline/explain?item=" + scene.Ref} {
		ae.expect(ae.do("GET", p, nil), 404)
	}
	ae.expect(ae.do("POST", "/api/v1/timeline/retry?item="+scene.Ref, nil), 404)
	for _, tok := range []string{mgrTok, noFlag.Key, ae.token} {
		for _, p := range []string{"/api/v1/history", "/api/v1/history?kind=movies", "/api/v1/activity/stats", "/api/v1/board",
			"/api/v1/board/items", "/api/v1/calendar?start=2024-01-01&end=2024-12-31", "/api/v1/movies",
			"/api/v1/system/stats", "/api/stats"} {
			r := ae.as(tok, "GET", p, nil)
			if strings.Contains(string(r.body), "Scene Title") || strings.Contains(string(r.body), "Scene.Title") ||
				strings.Contains(string(r.body), scene.Ref) || strings.Contains(string(r.body), "Studio A") {
				t.Fatalf("%s leaked adult data: %s", p, r.body)
			}
		}
	}
	for _, tok := range []string{mgrTok, noFlag.Key} {
		for _, p := range []string{"/api/v1/queue", "/api/v1/queue/blocklist", "/api/v1/recyclebin"} {
			r := ae.as(tok, "GET", p, nil)
			if strings.Contains(string(r.body), "Scene.Title") || strings.Contains(string(r.body), scene.Ref) {
				t.Fatalf("%s leaked to a user without view_adult: %s", p, r.body)
			}
		}
		r := ae.req("GET", "/compat/radarr/api/v3/movie", nil, map[string]string{"X-Api-Key": noFlag.Key, "Authorization": ""})
		if strings.Contains(string(r.body), "Scene Title") {
			t.Fatalf("compat leaked: %s", r.body)
		}
	}
	// Stats counts leave adult downloads out.
	ae.deps.Stats.Invalidate()
	var st struct{ QueueSize int }
	ae.expect(ae.do("GET", "/api/stats", nil), 200).json(&st)
	var n int
	_ = ae.deps.DB.R.QueryRow(`SELECT COUNT(*) FROM downloads WHERE status IN ('active', 'completed') AND json_extract(item_ref, '$.ref') NOT LIKE 'scene:%'`).Scan(&n)
	if st.QueueSize != n {
		t.Fatalf("queue size %d counts adult downloads (want %d)", st.QueueSize, n)
	}
}

// TestAdultRSSBlocklist: RSS releases in the adult categories only reach
// adult items (never a movie), the blocklist rejects matching names, and
// nothing is grabbed while the feature is off.
func TestAdultRSSBlocklist(t *testing.T) {
	ae := newAdultEnv(t)
	me := ae.me()
	ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"enabled": true, "confirmAdults": true, "stashdbKey": afake.StashKey,
		"defaultProfileId": ae.profileID}), 200)
	ae.grant(me.ID, "admin", true)
	ae.expect(ae.do("POST", "/api/v1/adult/rootfolders", map[string]any{"path": ae.adultRoot}), 201)
	ae.expect(ae.do("POST", "/api/v1/adult/blocklist", map[string]any{"term": "blockedtagexample"}), 201)
	var it apiAdultItem
	ae.expect(ae.do("POST", "/api/v1/adult/items", map[string]any{"source": "stashdb", "id": "sc-2", "search": false}), 201).json(&it)
	if !it.Monitored {
		t.Fatal("not monitored")
	}
	var indexerID int64
	_ = ae.deps.DB.R.QueryRow(`SELECT id FROM indexers LIMIT 1`).Scan(&indexerID)
	seeds := 50
	mk := func(title, hash string, cat int) indexers.Release {
		return indexers.Release{GUID: hash, Title: title, IndexerID: indexerID, IndexerName: "Fake Torznab", Protocol: "torrent",
			Size: 900 << 20, Seeders: &seeds, PublishDate: time.Now().Add(-5 * time.Hour), Categories: []int{cat},
			MagnetURL: "magnet:?xt=urn:btih:" + hash + "&dn=" + strings.ReplaceAll(title, " ", "."), InfoHash: hash}
	}
	blockedRel := mk("StudioA.24.02.01.Blockedtagexample.Second.Scene.1080p.WEB-DL.x264-GRP", "1111111111111111111111111111111111111111", 6010)
	goodRel := mk("StudioA.24.02.01.Performer.Two.Second.Scene.1080p.WEB-DL.x264-GRP", "2222222222222222222222222222222222222222", 6010)

	// Off: nothing.
	ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"enabled": false}), 200)
	if n, err := ae.deps.Wanted.ProcessRSS(t.Context(), []indexers.Release{goodRel}); err != nil || n != 0 {
		t.Fatalf("grabbed while disabled: %d %v", n, err)
	}
	ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"enabled": true, "confirmAdults": true}), 200)

	// Blocked name: rejected.
	if n, err := ae.deps.Wanted.ProcessRSS(t.Context(), []indexers.Release{blockedRel}); err != nil || n != 0 {
		t.Fatalf("blocked release grabbed: %d %v", n, err)
	}
	// The same release in a movie category never reaches the adult item.
	movieCat := goodRel
	movieCat.Categories = []int{2040}
	if n, _ := ae.deps.Wanted.ProcessRSS(t.Context(), []indexers.Release{movieCat}); n != 0 {
		t.Fatal("a movie-category release reached an adult item")
	}
	// The good one is grabbed for the scene.
	if n, err := ae.deps.Wanted.ProcessRSS(t.Context(), []indexers.Release{goodRel}); err != nil || n != 1 {
		t.Fatalf("grab: %d %v", n, err)
	}
	var title string
	_ = ae.deps.DB.R.QueryRow(`SELECT title FROM downloads WHERE json_extract(item_ref, '$.ref') = ?`, it.Ref).Scan(&title)
	if title != goodRel.Title {
		t.Fatalf("grabbed %q", title)
	}
}
