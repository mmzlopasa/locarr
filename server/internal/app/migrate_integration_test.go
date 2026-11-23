package app

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	tmdbfake "github.com/mmzlopasa/locarr/server/internal/metadata/tmdb/fake"
	"github.com/mmzlopasa/locarr/server/internal/migrate/fake"
)

type migrateEnv struct {
	*env
	fakes   map[string]*fake.Server
	urls    map[string]string
	lib     *fake.Library
	sources map[string]int64
	wait    time.Duration
}

type apiMigrateRun struct {
	ID       int64
	Status   string
	Error    string
	Progress struct {
		Phase       string
		Done, Total int
	}
	Options struct {
		PathMappings []struct {
			SourceID int64 `json:"sourceId"`
			From, To string
		} `json:"pathMappings"`
	}
	Summary struct {
		Counts     map[string]map[string]int
		Statuses   map[string]int
		Attention  map[string]int
		Conflicts  int
		PathChecks []struct {
			SourcePath  string `json:"sourcePath"`
			LocalPath   string `json:"localPath"`
			Files       int
			Found       int
			Exists      bool
		} `json:"pathChecks"`
		Suggested []struct{ From, To string }
		Refresh   *struct{ Total, Done, Failed int }
	}
}

type apiMigrateItem struct {
	Key, Category, Name, Action, Reason, Message, Status, LocalRef, Error string
	Attention                                                              []string
	Conflict                                                               *struct {
		Existing string
		Options  []string
		Chosen   string
	}
}

// newMigrateEnv starts a fake of every application with the given library,
// writes the library's files where Locarr sees them (data/media/…) and adds
// every application as a source.
func newMigrateEnv(t *testing.T, lib *fake.Library, skip map[string]bool) *migrateEnv {
	t.Helper()
	e := newEnv(t, noLimits)
	e.setup()
	me := &migrateEnv{env: e, fakes: map[string]*fake.Server{}, urls: map[string]string{}, lib: lib, sources: map[string]int64{}}
	e.expect(e.do("PUT", "/api/v1/settings/metadata", map[string]any{"tmdbApiKey": tmdbfake.APIKey}), 200)
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"allowedHosts": []string{"127.0.0.1"}}), 200)
	media := filepath.Join(e.data, "media")
	if err := os.MkdirAll(filepath.Join(media, "music"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := lib.WriteFiles(filepath.Join(media, "movies"), filepath.Join(media, "tv"), filepath.Join(media, "music"), skip); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"prowlarr", "qbittorrent", "sabnzbd", "radarr", "sonarr", "lidarr", "bazarr", "overseerr"} {
		f := fake.New(k, "key-"+k, lib)
		if k == "qbittorrent" {
			f.Username, f.Password = "admin", "qbit-pass"
		}
		srv := httptest.NewServer(f)
		t.Cleanup(srv.Close)
		me.fakes[k], me.urls[k] = f, srv.URL
		body := map[string]any{"kind": k, "url": srv.URL, "secret": "key-" + k}
		if k == "qbittorrent" {
			body["username"], body["secret"] = "admin", "qbit-pass"
		}
		var src struct {
			ID        int64
			Version   string
			SecretSet bool `json:"secretSet"`
		}
		me.expect(me.do("POST", "/api/v1/migrate/sources", body), 201).json(&src)
		if src.Version == "" || !src.SecretSet {
			t.Fatalf("%s source: %+v", k, src)
		}
		me.sources[k] = src.ID
	}
	return me
}

func (me *migrateEnv) options() map[string]any {
	var list []map[string]any
	for _, k := range []string{"prowlarr", "qbittorrent", "sabnzbd", "radarr", "sonarr", "lidarr", "bazarr", "overseerr"} {
		list = append(list, map[string]any{"id": me.sources[k], "settings": true, "library": true, "users": true, "history": true})
	}
	return map[string]any{"sources": list}
}

func (me *migrateEnv) run(id int64) apiMigrateRun {
	me.t.Helper()
	var r apiMigrateRun
	me.expect(me.do("GET", fmt.Sprintf("/api/v1/migrate/runs/%d", id), nil), 200).json(&r)
	return r
}

func (me *migrateEnv) waitRun(id int64, status string) apiMigrateRun {
	me.t.Helper()
	wait := me.wait
	if wait == 0 {
		wait = 60 * time.Second
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		r := me.run(id)
		if r.Status == status {
			return r
		}
		if r.Status == "failed" || r.Status == "cancelled" {
			me.t.Fatalf("run %d %s: %s", id, r.Status, r.Error)
		}
		time.Sleep(50 * time.Millisecond)
	}
	me.t.Fatalf("run %d never reached %s: %+v", id, status, me.run(id))
	return apiMigrateRun{}
}

func (me *migrateEnv) items(id int64, query string) []apiMigrateItem {
	me.t.Helper()
	var out struct {
		Items []apiMigrateItem
		Total int
	}
	me.expect(me.do("GET", fmt.Sprintf("/api/v1/migrate/runs/%d/items?limit=1000%s", id, query), nil), 200).json(&out)
	return out.Items
}

func (me *migrateEnv) analyse() apiMigrateRun {
	me.t.Helper()
	var r apiMigrateRun
	me.expect(me.do("POST", "/api/v1/migrate/runs", me.options()), 202).json(&r)
	return me.waitRun(r.ID, "planned")
}

func (me *migrateEnv) importRun(id int64) apiMigrateRun {
	me.t.Helper()
	me.expect(me.do("POST", fmt.Sprintf("/api/v1/migrate/runs/%d/import", id), nil), 202)
	return me.waitRun(id, "done")
}

func (me *migrateEnv) mutations() int64 {
	var n int64
	for _, f := range me.fakes {
		n += f.Mutations.Load()
	}
	return n
}

func byKey(items []apiMigrateItem) map[string]apiMigrateItem {
	out := map[string]apiMigrateItem{}
	for _, it := range items {
		out[it.Key] = it
	}
	return out
}

func plannedActions(items []apiMigrateItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Key+"="+it.Action)
	}
	sort.Strings(out)
	return out
}

func TestMigrateFullSetup(t *testing.T) {
	lib := fake.Default()
	// One episode file is missing on disk.
	missing := "/tv/Elephants Dream/Season 01/Elephants.Dream.S01E02.720p.HDTV.x264-OLD.mkv"
	me := newMigrateEnv(t, lib, map[string]bool{missing: true})
	media := filepath.Join(me.data, "media")

	// Read and plan: path mappings are suggested for the applications' own folders.
	r := me.analyse()
	if len(r.Summary.Suggested) != 3 {
		t.Fatalf("suggested mappings %+v", r.Summary.Suggested)
	}
	found := map[string]string{}
	for _, pc := range r.Summary.PathChecks {
		found[pc.SourcePath] = fmt.Sprintf("%d/%d", pc.Found, pc.Files)
	}
	if found["/movies"] != "2/2" || found["/tv"] != "3/4" || found["/music"] != "2/2" {
		t.Fatalf("path checks %v", found)
	}
	// Live path check with no mappings finds nothing.
	var checks []struct{ Found, Files int }
	me.expect(me.do("POST", fmt.Sprintf("/api/v1/migrate/runs/%d/paths", r.ID), map[string]any{"pathMappings": []any{}}), 200).json(&checks)
	if len(checks) == 0 || checks[0].Found != 0 {
		t.Fatalf("checks without mappings %+v", checks)
	}
	preview := me.items(r.ID, "")
	pv := byKey(preview)
	k := func(src, cat string, ext any) string { return fmt.Sprintf("%d:%s:%v", me.sources[src], cat, ext) }

	// A few rules worth knowing, as the preview shows them.
	if it := pv[k("radarr", "movie", 10378)]; it.Action != "create" {
		t.Fatalf("movie %+v", it)
	}
	if it := pv[k("radarr", "movie", 133701)]; it.Action != "create" || len(it.Attention) != 0 {
		t.Fatalf("movie without a file %+v", it)
	}
	if it := pv[k("sonarr", "series", 424242)]; it.Action != "create" || !contains(it.Attention, "file_not_found") {
		t.Fatalf("series with a missing file %+v", it)
	}
	if it := pv[k("radarr", "indexer", 3)]; it.Action != "skip" || it.Reason != "provided_by_prowlarr" {
		// The fake Prowlarr is not at prowlarr:9696, so its feed is not recognised here.
		if it.Action == "skip" {
			t.Fatalf("indexer %+v", it)
		}
	}
	if it := pv[k("radarr", "indexer", 4)]; it.Reason != "unsupported_implementation" {
		t.Fatalf("rss indexer %+v", it)
	}
	if it := pv[k("radarr", "indexer", 1)]; !contains(it.Attention, "needs_secret") {
		t.Fatalf("masked indexer key %+v", it)
	}
	if it := pv[k("sonarr", "indexer", 1)]; it.Action != "update" || it.Reason != "merged" {
		t.Fatalf("same feed from Sonarr %+v", it)
	}
	if it := pv[k("radarr", "qualityprofile", 1)]; it.Conflict == nil || it.Conflict.Chosen != "merge" {
		t.Fatalf("profile name clash %+v", it)
	}
	if it := pv[k("radarr", "customformat", 3)]; !contains(it.Attention, "unknown_condition") {
		t.Fatalf("unknown condition %+v", it)
	}
	if it := pv[k("sonarr", "customformat", 1)]; it.Action != "skip" || it.Reason != "duplicate" {
		t.Fatalf("custom format already from Radarr %+v", it)
	}
	if it := pv[k("radarr", "downloadclient", 1)]; it.Action == "skip" {
		t.Fatalf("qBittorrent client %+v", it)
	}
	if it := pv[k("radarr", "downloadclient", 2)]; it.Action != "skip" || it.Reason != "needs_secret" {
		t.Fatalf("SABnzbd without key %+v", it)
	}
	if it := pv[k("overseerr", "user", 3)]; it.Action != "create" || !contains(it.Attention, "needs_password") {
		t.Fatalf("user %+v", it)
	}
	if it := pv[k("bazarr", "subtitleprovider", "addic7ed")]; it.Reason != "unsupported_provider" {
		t.Fatalf("provider %+v", it)
	}
	if it := pv[k("bazarr", "subtitleprovider", "opensubtitlescom")]; !contains(it.Attention, "needs_secret") {
		t.Fatalf("opensubtitles %+v", it)
	}
	if it := pv[k("radarr", "importlist", 2)]; it.Reason != "unsupported_implementation" {
		t.Fatalf("trakt list %+v", it)
	}

	// Choose: keep Locarr's own HD-1080p profile for movies (skip the clash).
	key := k("radarr", "qualityprofile", 1)
	me.expect(me.do("POST", fmt.Sprintf("/api/v1/migrate/runs/%d/plan", r.ID), map[string]any{"resolutions": map[string]string{key: "rename"}}), 200)
	preview = me.items(r.ID, "")
	if it := byKey(preview)[key]; it.Action != "create" || it.Name != "HD-1080p (Radarr)" {
		t.Fatalf("renamed %+v", it)
	}

	// Import: the same plan is carried out (dry run and run agree).
	done := me.importRun(r.ID)
	result := me.items(r.ID, "")
	if a, b := plannedActions(preview), plannedActions(result); strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Fatalf("the import differs from the preview:\n%v\n%v", a, b)
	}
	for _, it := range result {
		if it.Status == "failed" {
			t.Errorf("failed: %s %s: %s", it.Key, it.Name, it.Error)
		}
	}
	if done.Summary.Statuses["failed"] > 0 {
		t.FailNow()
	}
	if n := me.mutations(); n != 0 {
		t.Fatalf("%d changing requests reached the applications", n)
	}

	// The library: files linked in place, monitoring kept.
	var movies struct {
		Items []struct {
			ID        int64
			Title     string
			Path      string
			Monitored bool
			File      *struct{ Path string }
		}
	}
	me.expect(me.do("GET", "/api/v1/movies?limit=100", nil), 200).json(&movies)
	if len(movies.Items) != 3 {
		t.Fatalf("movies %+v", movies.Items)
	}
	files := 0
	for _, m := range movies.Items {
		if m.File != nil {
			files++
			if !strings.HasPrefix(m.File.Path, filepath.Join(media, "movies")) {
				t.Fatalf("file moved? %s", m.File.Path)
			}
			if _, err := os.Stat(m.File.Path); err != nil {
				t.Fatal(err)
			}
		}
		if m.Title == "Tears of Steel" && m.Monitored {
			t.Fatal("monitoring not kept")
		}
	}
	if files != 2 {
		t.Fatalf("%d movie files linked", files)
	}
	var tl struct {
		Events []struct{ Type, Message string }
	}
	me.expect(me.do("GET", fmt.Sprintf("/api/v1/timeline?item=movie:%d", movies.Items[0].ID), nil), 200).json(&tl)
	types := ""
	for _, ev := range tl.Events {
		types += ev.Type + ","
	}
	if !strings.Contains(types, "migrated") {
		t.Fatalf("no import mark on the timeline: %s", types)
	}

	var series struct {
		Items []struct {
			ID    int64
			Title string
		}
	}
	me.expect(me.do("GET", "/api/v1/series?limit=100", nil), 200).json(&series)
	if len(series.Items) != 2 {
		t.Fatalf("series %+v", series.Items)
	}
	var withFiles int
	_ = me.deps.DB.R.QueryRow(`SELECT count(*) FROM episodes WHERE file_id IS NOT NULL`).Scan(&withFiles)
	if withFiles != 3 {
		t.Fatalf("%d episodes with files", withFiles)
	}
	var monitored int
	// The episodes as Sonarr had them (a metadata refresh may add more afterwards).
	_ = me.deps.DB.R.QueryRow(`SELECT count(*) FROM episodes WHERE monitored = 1 AND tvdb_id IN (900011, 900012, 900013, 900014, 900021, 900022)`).Scan(&monitored)
	if monitored != 4 {
		t.Fatalf("%d monitored episodes", monitored)
	}
	var trackFiles int
	_ = me.deps.DB.R.QueryRow(`SELECT count(*) FROM album_files`).Scan(&trackFiles)
	if trackFiles != 2 {
		t.Fatalf("%d track files", trackFiles)
	}
	// Settings, connections and people.
	var count = func(q string) int {
		var n int
		if err := me.deps.DB.R.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM root_folders`); n != 3 {
		t.Fatalf("%d root folders", n)
	}
	// qBittorrent and SABnzbd read as sources (with the credentials entered
	// here) and the applications' qBittorrent at qbittorrent:8080: one
	// client with every application's category, off until its password is entered.
	if n := count(`SELECT count(*) FROM download_clients`); n != 3 {
		t.Fatalf("%d download clients", n)
	}
	if n := count(`SELECT count(*) FROM download_clients WHERE implementation = 'qbittorrent' AND host = 'qbittorrent' AND category_movies = 'radarr'
		AND category_series = 'tv-sonarr' AND category_music = 'music' AND password IS NULL AND enabled = 0`); n != 1 {
		t.Fatal("the applications' qBittorrent should hold every category and wait for its password")
	}
	if n := count(`SELECT count(*) FROM download_clients WHERE implementation = 'qbittorrent' AND host = '127.0.0.1' AND password IS NOT NULL
		AND category_movies = 'radarr' AND enabled = 1`); n != 1 {
		t.Fatal("qBittorrent read as a source keeps its password")
	}
	if n := count(`SELECT count(*) FROM indexer_sources WHERE kind = 'prowlarr'`); n != 1 {
		t.Fatal("Prowlarr should be an indexer source")
	}
	if n := count(`SELECT count(*) FROM users`); n != 3 {
		t.Fatalf("%d users (the source admin merges into the Locarr admin)", n)
	}
	if n := count(`SELECT count(*) FROM requests`); n != 4 {
		t.Fatalf("%d requests", n)
	}
	if n := count(`SELECT count(*) FROM issue_comments`); n != 1 {
		t.Fatalf("%d issue comments", n)
	}
	if n := count(`SELECT count(*) FROM subtitle_profiles`); n < 2 {
		t.Fatalf("%d subtitle profiles", n)
	}
	if n := count(`SELECT count(*) FROM subtitle_item_profiles`); n == 0 {
		t.Fatal("no subtitle assignments")
	}
	if n := count(`SELECT count(*) FROM timeline_events WHERE type = 'history'`); n == 0 {
		t.Fatal("no history")
	}
	if n := count(`SELECT count(*) FROM blocklist`); n == 0 {
		t.Fatal("no blocklist")
	}
	if n := count(`SELECT count(*) FROM import_exclusions`); n != 1 {
		t.Fatalf("%d exclusions", n)
	}
	// The report has every item and no secrets.
	rep := me.expect(me.do("GET", fmt.Sprintf("/api/v1/migrate/runs/%d/report", r.ID), nil), 200)
	for _, secret := range []string{"key-radarr", "qbit-pass", "plain-key-1", "subdl-key-123"} {
		if strings.Contains(string(rep.body), secret) {
			t.Fatalf("the report contains %q", secret)
		}
	}
	// The source list never shows keys.
	srcs := me.expect(me.do("GET", "/api/v1/migrate/sources", nil), 200)
	if strings.Contains(string(srcs.body), "key-") || strings.Contains(string(srcs.body), "qbit-pass") {
		t.Fatal("a source key reached the client")
	}
	// The metadata refresh runs in the background.
	waitFor(t, "metadata refresh", func() bool {
		x := me.run(r.ID)
		return x.Summary.Refresh != nil && x.Summary.Refresh.Done == x.Summary.Refresh.Total
	})
	var checklist []struct {
		Key  string
		Done bool
	}
	me.expect(me.do("GET", "/api/v1/migrate/checklist", nil), 200).json(&checklist)
	if len(checklist) < 6 || checklist[0].Key != "import_done" || !checklist[0].Done {
		t.Fatalf("checklist %+v", checklist)
	}

	// Running again changes nothing and duplicates nothing.
	before := []int{count(`SELECT count(*) FROM media_items`), count(`SELECT count(*) FROM media_files`), count(`SELECT count(*) FROM users`),
		count(`SELECT count(*) FROM requests`), count(`SELECT count(*) FROM indexers`), count(`SELECT count(*) FROM quality_profiles`),
		count(`SELECT count(*) FROM custom_formats`), count(`SELECT count(*) FROM download_clients`), count(`SELECT count(*) FROM blocklist`),
		count(`SELECT count(*) FROM timeline_events WHERE type = 'history'`), count(`SELECT count(*) FROM issues`)}
	r2 := me.analyse()
	for _, it := range me.items(r2.ID, "") {
		if it.Action != "skip" {
			t.Errorf("second run would %s %s %s (%s)", it.Action, it.Category, it.Name, it.Reason)
		}
	}
	me.importRun(r2.ID)
	after := []int{count(`SELECT count(*) FROM media_items`), count(`SELECT count(*) FROM media_files`), count(`SELECT count(*) FROM users`),
		count(`SELECT count(*) FROM requests`), count(`SELECT count(*) FROM indexers`), count(`SELECT count(*) FROM quality_profiles`),
		count(`SELECT count(*) FROM custom_formats`), count(`SELECT count(*) FROM download_clients`), count(`SELECT count(*) FROM blocklist`),
		count(`SELECT count(*) FROM timeline_events WHERE type = 'history'`), count(`SELECT count(*) FROM issues`)}
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("second run changed counts: %v → %v", before, after)
	}
	if n := me.mutations(); n != 0 {
		t.Fatalf("%d changing requests", n)
	}
	// The missing file turns up and a third run links it.
	if err := os.WriteFile(filepath.Join(media, "tv", "Elephants Dream", "Season 01", filepath.Base(missing)), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r3 := me.analyse()
	if it := byKey(me.items(r3.ID, ""))[k("sonarr", "series", 424242)]; it.Action != "update" {
		t.Fatalf("found file not offered: %+v", it)
	}
	me.importRun(r3.ID)
	_ = me.deps.DB.R.QueryRow(`SELECT count(*) FROM episodes WHERE file_id IS NOT NULL`).Scan(&withFiles)
	if withFiles != 4 {
		t.Fatalf("%d episodes with files after the third run", withFiles)
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func TestMigratePermissions(t *testing.T) {
	e := newEnv(t, noLimits)
	e.setup()
	for _, role := range []string{"manager", "requester"} {
		tok := e.user(role+"1", role, nil)
		e.as(tok, func() {
			for _, c := range []struct{ method, path string }{{"GET", "/api/v1/migrate/sources"}, {"POST", "/api/v1/migrate/sources"},
				{"POST", "/api/v1/migrate/sources/test"}, {"GET", "/api/v1/migrate/runs"}, {"POST", "/api/v1/migrate/runs"},
				{"GET", "/api/v1/migrate/runs/1"}, {"POST", "/api/v1/migrate/runs/1/import"}, {"GET", "/api/v1/migrate/runs/1/items"},
				{"GET", "/api/v1/migrate/runs/1/report"}, {"GET", "/api/v1/migrate/checklist"}, {"DELETE", "/api/v1/migrate/sources/1"}} {
				body := map[string]any{"kind": "radarr", "url": "http://x:7878", "sources": []any{map[string]any{"id": 1}}}
				if c.method == "GET" || c.method == "DELETE" {
					body = nil
				}
				if r := e.do(c.method, c.path, body); r.StatusCode != 403 {
					t.Errorf("%s %s %s: %d %s", role, c.method, c.path, r.StatusCode, r.body)
				}
			}
		})
	}
	// A wrong key is reported by the test, not saved.
	f := fake.New("radarr", "right", fake.Default())
	srv := httptest.NewServer(f)
	defer srv.Close()
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"allowedHosts": []string{"127.0.0.1"}}), 200)
	var res struct {
		Ok   bool
		Code string
	}
	e.expect(e.do("POST", "/api/v1/migrate/sources/test", map[string]any{"kind": "radarr", "url": srv.URL, "secret": "wrong"}), 200).json(&res)
	if res.Ok || res.Code != "unauthorized" {
		t.Fatalf("%+v", res)
	}
	if r := e.do("POST", "/api/v1/migrate/sources", map[string]any{"kind": "radarr", "url": srv.URL, "secret": "wrong"}); r.StatusCode != 502 || r.code() != "source_unauthorized" {
		t.Fatalf("%d %s", r.StatusCode, r.body)
	}
	if r := e.do("POST", "/api/v1/migrate/sources", map[string]any{"kind": "radarr", "url": "ftp://x", "secret": "k"}); r.StatusCode != 400 {
		t.Fatalf("%d", r.StatusCode)
	}
	// A read-only API key cannot start anything.
	var key struct{ Key string }
	e.expect(e.do("POST", "/api/v1/auth/apikeys", map[string]any{"name": "ro", "readOnly": true}), 201).json(&key)
	e.as(key.Key, func() {
		if r := e.do("POST", "/api/v1/migrate/runs", map[string]any{"sources": []any{map[string]any{"id": 1}}}); r.StatusCode != 403 {
			t.Fatalf("read-only key: %d", r.StatusCode)
		}
	})
	if f.Mutations.Load() != 0 {
		t.Fatal("mutation")
	}
}
