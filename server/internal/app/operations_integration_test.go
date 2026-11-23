package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/health"
	"github.com/mmzlopasa/locarr/server/internal/logging"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
)

type apiBoard struct {
	Total  int
	Groups []struct {
		Code, Fix, Tone string
		Count           int
		Kinds           map[string]int
	}
}

type apiBoardItems struct {
	Total int
	Items []struct {
		Key, Kind, ItemRef, Code, Fix, Message string
		DownloadId                             *int64 `json:"downloadId"`
		Params                                 map[string]any
	}
}

func (le *libraryEnv) rebuildBoard() {
	le.t.Helper()
	if _, err := le.deps.Board.Rebuild(context.Background()); err != nil {
		le.t.Fatal(err)
	}
}

// A finished download whose files Locarr cannot see (a wrong remote path
// mapping) shows on the board with the fix; after fixing the mapping,
// "retry import" brings the movie in. History, statistics and health follow.
func TestBoardPathMappingFlow(t *testing.T) {
	le := newLibraryEnv(t)
	var mapping struct{ ID int64 }
	le.expect(le.do("POST", "/api/v1/remotepathmappings", map[string]any{"host": "flow.lan", "remotePath": le.downloads,
		"localPath": filepath.Join(le.data, "wrong-mount")}), 201).json(&mapping)
	m := le.addMovie(10378, true)
	waitFor(t, "the grab", func() bool { return le.downloadCount(m.ID) == 1 })
	le.client.finish(t)
	le.poll()

	le.rebuildBoard()
	var b apiBoard
	le.expect(le.do("GET", "/api/v1/board", nil), 200).json(&b)
	if b.Total != 1 || len(b.Groups) != 1 || b.Groups[0].Code != "path_not_visible" || b.Groups[0].Fix != "path_mapping" ||
		b.Groups[0].Kinds["movie"] != 1 {
		t.Fatalf("board: %+v", b)
	}
	var items apiBoardItems
	le.expect(le.do("GET", "/api/v1/board/items?code=path_not_visible&fix=path_mapping", nil), 200).json(&items)
	if items.Total != 1 || items.Items[0].Key != fmt.Sprintf("movie:%d", m.ID) || items.Items[0].DownloadId == nil ||
		!strings.Contains(items.Items[0].Message, "not visible") {
		t.Fatalf("items: %+v", items)
	}

	// Health sees the same problem and the broken mapping.
	var rep struct {
		Issues []struct{ Key, Code, Severity, Docs, FixRoute string }
	}
	le.expect(le.do("POST", "/api/v1/system/health/run", nil), 200).json(&rep)
	codes := map[string]bool{}
	for _, is := range rep.Issues {
		codes[is.Code] = true
		if is.Docs == "" {
			t.Errorf("issue without docs: %+v", is)
		}
	}
	if !codes["path_not_visible"] || !codes["mapping_local_missing"] {
		t.Fatalf("health: %+v", rep.Issues)
	}

	// Fix the mapping (the client and Locarr see the same path) and retry.
	le.expect(le.do("DELETE", fmt.Sprintf("/api/v1/remotepathmappings/%d", mapping.ID), nil), 204)
	var res struct {
		Done   int
		Failed []struct{ Key, Code, Message string }
	}
	le.expect(le.do("POST", "/api/v1/board/actions", map[string]any{"action": "retry_import", "keys": []string{items.Items[0].Key}}), 200).json(&res)
	if res.Done != 1 || len(res.Failed) != 0 {
		t.Fatalf("retry: %+v", res)
	}
	waitFor(t, "the import", func() bool { return le.movie(m.ID).File != nil })
	if err := le.deps.Board.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	le.expect(le.do("GET", "/api/v1/board", nil), 200).json(&b)
	if b.Total != 0 {
		t.Fatalf("board after import: %+v", b)
	}

	// History across the library.
	var hist struct {
		Items []struct {
			ID, Category, Type, Title, ItemRef, Release, Indexer string
		}
		HasMore bool
	}
	le.expect(le.do("GET", "/api/v1/history?category=imported", nil), 200).json(&hist)
	if len(hist.Items) != 1 || hist.Items[0].Title != "Big Buck Bunny (2008)" || hist.Items[0].ItemRef != fmt.Sprintf("movie:%d", m.ID) {
		t.Fatalf("imported history: %+v", hist)
	}
	le.expect(le.do("GET", "/api/v1/history?category=grabbed&q=bunny", nil), 200).json(&hist)
	if len(hist.Items) != 1 || hist.Items[0].Indexer != "Fake Torznab" || hist.Items[0].Release == "" {
		t.Fatalf("grabbed history: %+v", hist)
	}
	le.expect(le.do("GET", "/api/v1/history?kind=music", nil), 200).json(&hist)
	if len(hist.Items) != 0 {
		t.Fatalf("music history: %+v", hist)
	}
	le.expect(le.do("GET", "/api/v1/history?limit=1", nil), 200).json(&hist)
	if len(hist.Items) != 1 || !hist.HasMore {
		t.Fatalf("paging: %+v", hist)
	}
	var st struct {
		Indexers []struct {
			Name             string
			Grabs, Imported  int
			SuccessRate      *float64
			AvgImportSeconds *int64
		}
		Clients []struct {
			Name  string
			Grabs int
		}
	}
	le.expect(le.do("GET", "/api/v1/activity/stats?days=7", nil), 200).json(&st)
	if len(st.Indexers) != 1 || st.Indexers[0].Name != "Fake Torznab" || st.Indexers[0].Grabs != 1 || st.Indexers[0].Imported != 1 ||
		st.Indexers[0].SuccessRate == nil || *st.Indexers[0].SuccessRate != 1 || len(st.Clients) != 1 || st.Clients[0].Name != "Flow" {
		t.Fatalf("stats: %+v", st)
	}
}

// Failed downloads: blocklist & retry removes, blocks and searches again;
// other bulk actions change the library.
func TestBoardBulkActions(t *testing.T) {
	le := newLibraryEnv(t)
	m := le.addMovie(10378, true)
	waitFor(t, "the grab", func() bool { return le.downloadCount(m.ID) == 1 })
	le.poll()
	le.rebuildBoard()
	var items apiBoardItems
	le.expect(le.do("GET", "/api/v1/board/items?code=downloading", nil), 200).json(&items)
	if items.Total != 1 || items.Items[0].Params["progress"] == nil {
		t.Fatalf("downloading: %+v", items)
	}
	key := items.Items[0].Key
	var res struct {
		Done   int
		Failed []struct{ Key, Code string }
	}
	le.expect(le.do("POST", "/api/v1/board/actions", map[string]any{"action": "blocklist_retry", "keys": []string{key, "movie:999999"}}), 200).json(&res)
	if res.Done != 1 || len(res.Failed) != 1 || res.Failed[0].Code != "not_found" {
		t.Fatalf("blocklist & retry: %+v", res)
	}
	var blocked []struct{ Title string }
	le.expect(le.do("GET", "/api/v1/queue/blocklist", nil), 200).json(&blocked)
	if len(blocked) != 1 {
		t.Fatalf("blocklist: %+v", blocked)
	}
	var cleared struct{ Count int }
	le.expect(le.do("DELETE", "/api/v1/queue/blocklist", nil), 200).json(&cleared)
	if cleared.Count != 1 {
		t.Fatal(cleared)
	}
	// change_profile needs a profile; unmonitor takes the movie off the board.
	le.expect(le.do("POST", "/api/v1/board/actions", map[string]any{"action": "change_profile", "keys": []string{key}}), 400)
	le.expect(le.do("POST", "/api/v1/board/actions", map[string]any{"action": "change_profile", "keys": []string{key},
		"qualityProfileId": le.profileID}), 200).json(&res)
	if res.Done != 1 {
		t.Fatalf("change profile: %+v", res)
	}
	le.rebuildBoard()
	le.expect(le.do("POST", "/api/v1/board/actions", map[string]any{"action": "unmonitor", "keys": []string{key}}), 200).json(&res)
	if res.Done != 1 || le.movie(m.ID).Monitored {
		t.Fatalf("unmonitor: %+v", res)
	}
	if err := le.deps.Board.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	le.expect(le.do("GET", "/api/v1/board/items", nil), 200).json(&items)
	if items.Total != 0 {
		t.Fatalf("unmonitored movie still on the board: %+v", items)
	}
	// Requesters may not see the board.
	fam := le.user("fam", "requester", requesterPerms())
	le.expect(le.req("GET", "/api/v1/board", nil, map[string]string{"Authorization": "Bearer " + fam}), 403)
	le.expect(le.req("GET", "/api/v1/history", nil, map[string]string{"Authorization": "Bearer " + fam}), 403)
}

// The board's full rebuild handles a large library in the background.
func TestBoardRebuildPerformance(t *testing.T) {
	le := newLibraryEnv(t)
	seedMovies(t, le, 10_000)
	start := time.Now()
	n, err := le.deps.Board.Rebuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rebuilt %d items in %s", n, time.Since(start))
	if n != 5000 {
		t.Fatalf("items: %d", n)
	}
	start = time.Now()
	var b apiBoard
	le.expect(le.do("GET", "/api/v1/board", nil), 200).json(&b)
	var items apiBoardItems
	le.expect(le.do("GET", "/api/v1/board/items?code=not_searched_yet&offset=4000&limit=100", nil), 200).json(&items)
	took := time.Since(start)
	t.Logf("board and a page in %s", took)
	if b.Total != 5000 || len(items.Items) != 100 {
		t.Fatalf("board: %+v / %d", b, len(items.Items))
	}
	limit := 400 * time.Millisecond
	if raceEnabled {
		limit *= 5
	}
	if took > limit {
		t.Errorf("board read took %s", took)
	}
}

// Every health check with fakes: broken things show up once, with docs.
func TestHealthChecks(t *testing.T) {
	le := newLibraryEnv(t)
	ctx := context.Background()
	// A root folder that disappears, a large recycle bin, a stuck import,
	// a refused metadata key, a subtitle provider without quota.
	gone := filepath.Join(le.data, "media", "gone")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	le.expect(le.do("POST", "/api/v1/rootfolders", map[string]any{"path": gone, "kind": "music"}), 201)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	exec := func(q string, args ...any) {
		if _, err := le.deps.DB.W.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO recycle_bin (original_path, recycled_path, size, recycled_at) VALUES ('/a', '/b', ?, ?)`, int64(60)<<30, db.Millis(now))
	exec(`INSERT INTO downloads (client_name, client_item_id, protocol, title, status, state, progress, added_at, updated_at, completed_at)
		VALUES ('Flow', 'x1', 'torrent', 'Stuck.Release', 'completed', 'seeding', 1, ?, ?, ?)`, db.Millis(now.Add(-3*time.Hour)),
		db.Millis(now), db.Millis(now.Add(-2*time.Hour)))
	exec(`UPDATE subtitle_providers SET enabled = 1, quota_remaining = 0, quota_reset_at = ? WHERE name = 'opensubtitles'`, db.Millis(now.Add(time.Hour)))
	le.deps.MetaHealth.Record("tmdb", metadata.ErrInvalidKey)
	le.deps.MetaHealth.Record("musicbrainz", fmt.Errorf("%w: %w", metadata.ErrUnavailable, metadata.ErrRateLimited))
	// GitHub says there is a newer version, and its clock is 10 minutes ahead.
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Date", time.Now().Add(10*time.Minute).UTC().Format(http.TimeFormat))
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9","html_url":"https://example.invalid/r"}`))
	}))
	defer gh.Close()
	le.deps.Version.URL, le.deps.Version.Current = gh.URL, "1.0.0"
	if err := le.deps.Version.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	// A download client that cannot be reached.
	le.deps.Downloads.NewClient = func(downloaders.Implementation, downloaders.Config) (downloaders.Client, error) {
		return nil, fmt.Errorf("dial tcp: connection refused")
	}
	_ = le.deps.Downloads.Poll(ctx)
	var published []health.Issue
	le.deps.Health.OnNew = func(_ context.Context, is health.Issue) { published = append(published, is) }

	var rep struct {
		Issues []struct {
			Key, Code, Severity, Docs, FixRoute string
			Params                              map[string]any
		}
	}
	le.expect(le.do("POST", "/api/v1/system/health/run", nil), 200).json(&rep)
	codes := map[string]string{}
	for _, is := range rep.Issues {
		codes[is.Code] = is.Severity
	}
	for code, sev := range map[string]string{
		"rootfolder_missing": "error", "recycle_bin_large": "warning", "import_stuck": "warning", "metadata_invalid_key": "error",
		"metadata_rate_limited": "warning", "subtitle_provider_quota": "warning", "update_available": "info", "time_skew": "warning",
		"client_unreachable": "error",
	} {
		if codes[code] != sev {
			t.Errorf("%s: got %q, want %q (all: %v)", code, codes[code], sev, codes)
		}
	}
	if len(published) == 0 {
		t.Fatal("no health.warning published")
	}
	n := len(published)
	le.expect(le.do("POST", "/api/v1/system/health/run", nil), 200)
	if len(published) != n {
		t.Fatalf("published again: %d → %d", n, len(published))
	}
	// Turning the version check off removes its issues.
	le.expect(le.do("PUT", "/api/v1/settings/system", map[string]any{"updateCheck": false}), 200)
	le.expect(le.do("POST", "/api/v1/system/health/run", nil), 200).json(&rep)
	for _, is := range rep.Issues {
		if is.Code == "update_available" || is.Code == "time_skew" {
			t.Fatalf("version check still on: %+v", is)
		}
	}
	// Streamero failures come in from its scans.
	le.deps.Streamero.Health(ctx, false, "connection refused")
	le.expect(le.do("POST", "/api/v1/system/health/run", nil), 200).json(&rep)
	found := false
	for _, is := range rep.Issues {
		found = found || is.Code == "streamero_unreachable"
	}
	if !found {
		t.Fatal("no streamero issue")
	}
}

// /api/stats answers administrators and their read-only keys with JSON;
// /metrics only exists with a token.
func TestStatsAndMetrics(t *testing.T) {
	le := newLibraryEnv(t)
	seedMovies(t, le, 10)
	var created struct{ Key string }
	le.expect(le.do("POST", "/api/v1/auth/apikeys", map[string]any{"name": "Dashboard", "readOnly": true}), 201).json(&created)
	admin := le.token
	fam := le.user("fam", "requester", requesterPerms())
	le.token = ""
	r := le.expect(le.req("GET", "/api/stats", nil, map[string]string{"X-Api-Key": created.Key}), 200)
	var st map[string]any
	r.json(&st)
	for _, k := range []string{"version", "commit", "generatedAt", "movies", "moviesHave", "moviesMissing", "series", "episodes", "episodesHave",
		"episodesMissing", "artists", "albums", "albumsHave", "albumsMissing", "queueSize", "activeDownloads", "downloadSpeedBytes",
		"wantedMovies", "wantedEpisodes", "wantedAlbums", "failedImports", "healthErrors", "healthWarnings", "healthInfo", "indexers",
		"indexersHealthy", "indexersFailing", "indexersDisabled", "requestsPending", "subtitlesMissing", "rootFolders"} {
		if _, ok := st[k]; !ok {
			t.Errorf("missing field %s", k)
		}
	}
	if st["movies"].(float64) != 10 || st["moviesHave"].(float64) != 5 || st["moviesMissing"].(float64) != 5 || st["indexers"].(float64) != 1 {
		t.Fatalf("numbers: %v", st)
	}
	if rf := st["rootFolders"].([]any); len(rf) != 1 {
		t.Fatalf("root folders: %v", rf)
	}
	le.expect(le.req("GET", "/api/v1/system/stats", nil, map[string]string{"Authorization": "Bearer " + created.Key}), 200)
	if r := le.expect(le.req("GET", "/api/stats", nil, nil), 401); r.code() != "unauthorized" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		t.Fatal(string(r.body))
	}
	if r := le.expect(le.req("GET", "/api/stats", nil, map[string]string{"Authorization": "Bearer " + fam}), 403); r.code() != "forbidden" {
		t.Fatal(string(r.body))
	}
	// A read-only key cannot change anything.
	le.expect(le.req("POST", "/api/v1/system/backups", nil, map[string]string{"X-Api-Key": created.Key}), 403)

	le.token = admin
	le.expect(le.do("GET", "/metrics", nil), 404)
	le.expect(le.do("PUT", "/api/v1/settings/system", map[string]any{"metricsToken": "short"}), 400)
	token := "metrics-token-0123456789"
	var ss struct{ MetricsTokenSet bool }
	le.expect(le.do("PUT", "/api/v1/settings/system", map[string]any{"metricsToken": token}), 200).json(&ss)
	if !ss.MetricsTokenSet {
		t.Fatal("token not set")
	}
	if raw := le.expect(le.do("GET", "/api/v1/settings/system", nil), 200).body; strings.Contains(string(raw), token) {
		t.Fatal("token returned")
	}
	le.expect(le.req("GET", "/metrics", nil, map[string]string{"Authorization": "Bearer wrong"}), 401)
	m := le.expect(le.req("GET", "/metrics", nil, map[string]string{"Authorization": "Bearer " + token}), 200)
	body := string(m.body)
	for _, want := range []string{`locarr_library_items{kind="movie",state="total"} 10`, `locarr_indexers{health="healthy"} 1`, "# TYPE locarr_queue_size gauge"} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %q:\n%s", want, body)
		}
	}
	le.expect(le.do("PUT", "/api/v1/settings/system", map[string]any{"metricsToken": ""}), 200)
	le.expect(le.req("GET", "/metrics", nil, map[string]string{"Authorization": "Bearer " + token}), 404)
}

// Retention removes old rows and keeps recent ones.
func TestHousekeepingRetention(t *testing.T) {
	e := newEnv(t, nil)
	e.setup()
	d := e.deps.DB
	old, recent := time.Now().AddDate(-2, 0, 0), time.Now()
	exec := func(q string, args ...any) {
		if _, err := d.W.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, at := range []time.Time{old, recent} {
		exec(`INSERT INTO timeline_events (item_ref, at, type, message) VALUES ('movie:1', ?, 'added', '')`, db.Millis(at))
		exec(`INSERT INTO search_log (started_at, duration_ms, source, search_type, query, params, result_count, error_count)
			VALUES (?, 1, 'automatic', 'movie', 'q', '{}', 0, 0)`, db.Millis(at))
		exec(`INSERT INTO downloads (client_name, client_item_id, protocol, title, status, state, added_at, updated_at, finished_at)
			VALUES ('c', ?, 'torrent', 't', 'imported', 'seeding', ?, ?, ?)`, fmt.Sprint(at.Unix()), db.Millis(at), db.Millis(at), db.Millis(at))
		exec(`INSERT INTO download_events (download_id, at, type) VALUES (last_insert_rowid(), ?, 'grabbed')`, db.Millis(at))
	}
	res, err := e.deps.Housekeeping.Prune(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Timeline != 1 || res.SearchLog != 1 || res.DownloadEvents != 1 {
		t.Fatalf("pruned: %+v", res)
	}
	for table, want := range map[string]int{"timeline_events": 1, "search_log": 1, "download_events": 1, "downloads": 2} {
		var n int
		_ = d.R.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n)
		if n != want {
			t.Errorf("%s: %d, want %d", table, n, want)
		}
	}
	// The image cache is capped (oldest first).
	dir := filepath.Join(e.deps.Housekeeping.ImageDir, "tmdb", "w342")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		p := filepath.Join(dir, fmt.Sprintf("%d.jpg", i))
		if err := os.WriteFile(p, make([]byte, 30<<20), 0o600); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(p, time.Now().Add(time.Duration(i)*time.Hour), time.Now().Add(time.Duration(i)*time.Hour))
	}
	e.expect(e.do("PUT", "/api/v1/settings/system", map[string]any{"imageCacheMaxMb": 64}), 200)
	n, freed, err := e.deps.Housekeeping.CapImages(context.Background())
	if err != nil || n != 3 || freed != 90<<20 {
		t.Fatalf("cap: %d %d %v", n, freed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "3.jpg")); err != nil {
		t.Fatal("newest image removed")
	}
	e.expect(e.do("PUT", "/api/v1/settings/system", map[string]any{"vacuumIntervalDays": 7}), 200)
	if _, err := d.W.Exec(`DELETE FROM settings WHERE key = 'housekeeping.lastVacuum'`); err != nil {
		t.Fatal(err)
	}
	vac, err := e.deps.Housekeeping.VacuumIfDue(context.Background())
	if err != nil || !vac {
		t.Fatal(vac, err)
	}
	if vac, _ := e.deps.Housekeeping.VacuumIfDue(context.Background()); vac {
		t.Fatal("vacuumed twice")
	}
}

// The log viewer reads the in-memory ring and lists the log files.
func TestLogViewerAPI(t *testing.T) {
	e := newEnv(t, nil)
	e.setup()
	dir := t.TempDir()
	log, sinks := logging.Setup(io.Discard, "info", dir)
	t.Cleanup(func() { _ = sinks.Close() })
	log.Warn("disk almost full", "path", "/data", "apikey", "SECRET")
	log.Info("fetch https://idx/api?apikey=SECRET2")
	e.handler.Logs = sinks
	var entries []struct {
		Level, Message string
		Fields         map[string]any
	}
	e.expect(e.do("GET", "/api/v1/system/logs?level=warn", nil), 200).json(&entries)
	if len(entries) != 1 || entries[0].Message != "disk almost full" || entries[0].Fields["apikey"] != "[redacted]" {
		t.Fatalf("entries: %+v", entries)
	}
	raw := e.expect(e.do("GET", "/api/v1/system/logs?q=fetch", nil), 200).body
	if strings.Contains(string(raw), "SECRET2") {
		t.Fatal("secret in the log viewer")
	}
	var files []struct{ Name string }
	e.expect(e.do("GET", "/api/v1/system/logs/files", nil), 200).json(&files)
	if len(files) != 1 || files[0].Name != "locarr.log" {
		t.Fatalf("files: %+v", files)
	}
	f := e.expect(e.do("GET", "/api/v1/system/logs/files/locarr.log", nil), 200)
	if !strings.Contains(string(f.body), "disk almost full") || strings.Contains(string(f.body), "SECRET") {
		t.Fatal(string(f.body))
	}
	e.expect(e.do("GET", "/api/v1/system/logs/files/..%2Fsecret.key", nil), 400)
}
