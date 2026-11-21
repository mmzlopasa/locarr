package app

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/fake"
)

func startFakes(t *testing.T) (*fake.Server, string) {
	t.Helper()
	f := fake.New("fakekey", "sourcekey")
	f.SlowDelay = 1500 * time.Millisecond
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

func TestIndexersAPI(t *testing.T) {
	e := newEnv(t, noLimits)
	e.setup()
	_, base := startFakes(t)

	// Loopback is refused until the administrator allows it.
	body := map[string]any{"name": "Fake Torznab", "kind": "torznab", "baseUrl": base + "/torznab", "apiKey": "fakekey"}
	if r := e.expect(e.do("POST", "/api/v1/indexers", body), 400); r.code() != "validation" {
		t.Fatal(r.code())
	}
	var st struct{ AllowedHosts []string }
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"allowedHosts": []string{"127.0.0.1"}}), 200).json(&st)
	if len(st.AllowedHosts) != 1 {
		t.Fatal(st)
	}
	if r := e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"allowedHosts": []string{"no way!"}}), 400); r.code() != "validation" {
		t.Fatal(r.code())
	}
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"rssSyncIntervalMinutes": 30}), 200)

	// Test before saving.
	var tr struct {
		Ok                bool
		ProbeResults      int
		ErrorCode         string
		DefaultCategories struct{ Movies []int }
	}
	e.expect(e.do("POST", "/api/v1/indexers/test", map[string]any{"indexer": body}), 200).json(&tr)
	if !tr.Ok || tr.ProbeResults == 0 || len(tr.DefaultCategories.Movies) == 0 {
		t.Fatalf("%+v", tr)
	}
	wrong := map[string]any{"name": "x", "kind": "torznab", "baseUrl": base + "/torznab", "apiKey": "nope"}
	e.expect(e.do("POST", "/api/v1/indexers/test", map[string]any{"indexer": wrong}), 200).json(&tr)
	if tr.Ok || tr.ErrorCode != "auth" {
		t.Fatalf("%+v", tr)
	}

	var ind struct {
		ID     int64
		ApiKey struct{ Configured bool }
		Health struct{ Status string }
		Caps   *struct{ Searching []any }
	}
	r := e.expect(e.do("POST", "/api/v1/indexers", body), 201)
	if strings.Contains(string(r.body), "fakekey") {
		t.Fatal("API key returned")
	}
	r.json(&ind)
	if !ind.ApiKey.Configured || ind.Caps == nil || ind.Health.Status != "unknown" {
		t.Fatalf("%s", r.body)
	}
	if r := e.expect(e.do("POST", "/api/v1/indexers", body), 409); r.code() != "name_taken" {
		t.Fatal(r.code())
	}
	for _, x := range []map[string]any{
		{"name": "Fake Quirky", "kind": "torznab", "baseUrl": base + "/quirky", "apiKey": "fakekey", "priority": 10},
		{"name": "Fake Newznab", "kind": "newznab", "baseUrl": base + "/newznab", "apiKey": "fakekey"},
		{"name": "Fake Slow", "kind": "torznab", "baseUrl": base + "/slow", "apiKey": "fakekey"},
	} {
		e.expect(e.do("POST", "/api/v1/indexers", x), 201)
	}
	var tested struct{ Ok bool }
	e.expect(e.do("POST", "/api/v1/indexers/"+itoa(ind.ID)+"/test", nil), 200).json(&tested)
	if !tested.Ok {
		t.Fatal("test of saved indexer")
	}
	e.expect(e.do("GET", "/api/v1/indexers/"+itoa(ind.ID), nil), 200).json(&ind)
	if ind.Health.Status != "healthy" {
		t.Fatal(ind.Health)
	}
	// Update keeps the key when it is not sent.
	upd := map[string]any{"name": "Fake Torznab", "kind": "torznab", "baseUrl": base + "/torznab", "tags": []string{"Public", "public"}}
	var updated struct {
		Tags   []string
		ApiKey struct{ Configured bool }
	}
	e.expect(e.do("PUT", "/api/v1/indexers/"+itoa(ind.ID), upd), 200).json(&updated)
	if !updated.ApiKey.Configured || len(updated.Tags) != 1 || updated.Tags[0] != "public" {
		t.Fatalf("%+v", updated)
	}

	// JSON search: every indexer, de-duplicated, links for the administrator.
	var sr struct {
		Releases []struct {
			Title, IndexerName, DownloadLink, MagnetUrl string
			Seeders                                     *int
		}
		Indexers []struct{ IndexerName, Status string }
	}
	e.expect(e.do("GET", "/api/v1/search?q=big+buck+bunny&type=movie", nil), 200).json(&sr)
	if len(sr.Indexers) != 4 || len(sr.Releases) != 2 {
		t.Fatalf("%+v", sr)
	}
	var link string
	for _, rel := range sr.Releases {
		if rel.DownloadLink != "" {
			link = rel.DownloadLink
		}
		if strings.Contains(rel.DownloadLink, "fakekey") {
			t.Fatal("key in link")
		}
	}
	if link == "" {
		t.Fatalf("no torrent link: %+v", sr.Releases)
	}
	dl := e.expect(e.do("GET", link, nil), 200)
	if !strings.Contains(string(dl.body), "<nzb") || !strings.Contains(dl.Header.Get("Content-Disposition"), ".nzb") {
		t.Fatalf("%s %v", dl.body, dl.Header)
	}
	if r := e.expect(e.do("GET", "/api/v1/indexers/"+itoa(ind.ID)+"/download?token=garbage", nil), 400); r.code() != "bad_token" {
		t.Fatal(r.code())
	}

	// A requester can search but gets no links and cannot manage indexers.
	e.expect(e.do("POST", "/api/v1/users", map[string]any{"username": "req", "password": "requester-pw", "role": "requester"}), 201)
	admin := e.token
	e.token = e.login("req", "requester-pw")
	r = e.expect(e.do("GET", "/api/v1/search?q=sintel", nil), 200)
	if strings.Contains(string(r.body), "downloadLink") || strings.Contains(string(r.body), "magnetUrl") {
		t.Fatalf("links for a requester: %s", r.body)
	}
	e.expect(e.do("GET", "/api/v1/indexers", nil), 403)
	e.expect(e.do("GET", link, nil), 403)
	e.token = admin

	// Streaming: the fast indexers arrive before the slow one, then "done".
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/search/stream?q=elephants&type=tv", nil)
	req.Header.Set("Authorization", "Bearer "+e.token)
	start := time.Now()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal(res.Status, res.Header)
	}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var events []string
	var firstAt time.Duration
	total := 0
	event := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
			events = append(events, event)
			if event == "indexer" && firstAt == 0 {
				firstAt = time.Since(start)
			}
		case strings.HasPrefix(line, "data: ") && event == "indexer":
			var ev struct {
				Indexer  struct{ Status string }
				Releases []json.RawMessage
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
				t.Fatal(err, line)
			}
			total += len(ev.Releases)
		case strings.HasPrefix(line, "data: ") && event == "done":
			var d struct{ Total int }
			_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &d)
			if d.Total != total {
				t.Fatalf("done total %d, streamed %d", d.Total, total)
			}
		}
	}
	if len(events) != 5 || events[4] != "done" || total == 0 {
		t.Fatalf("%v total %d", events, total)
	}
	if firstAt > time.Second {
		t.Fatalf("first result after %v: not streamed", firstAt)
	}

	// Search log and indexer list with health.
	var log []struct {
		Source   string
		Indexers []any
	}
	e.expect(e.do("GET", "/api/v1/search/log?limit=10", nil), 200).json(&log)
	if len(log) < 3 || log[0].Source != "interactive" || len(log[0].Indexers) != 4 {
		t.Fatalf("%+v", log)
	}

	// RSS sync through the task runner.
	var job struct{ ID int64 }
	e.expect(e.do("POST", "/api/v1/system/tasks/rss-sync/run", nil), 202).json(&job)
	waitJob(t, e, job.ID)
	var n int
	if err := e.deps.DB.R.QueryRow(`SELECT count(*) FROM releases_seen`).Scan(&n); err != nil || n == 0 {
		t.Fatal(n, err)
	}
	var tasks struct {
		Scheduled []struct {
			Name            string
			IntervalSeconds int
		}
	}
	e.expect(e.do("GET", "/api/v1/system/tasks", nil), 200).json(&tasks)
	found := false
	for _, s := range tasks.Scheduled {
		if s.Name == "rss-sync" && s.IntervalSeconds == 1800 {
			found = true
		}
	}
	if !found {
		t.Fatalf("%+v", tasks)
	}

	e.expect(e.do("DELETE", "/api/v1/indexers/"+itoa(ind.ID), nil), 204)
	e.expect(e.do("GET", "/api/v1/indexers/"+itoa(ind.ID), nil), 404)
}

func waitJob(t *testing.T, e *env, id int64) {
	t.Helper()
	for i := 0; i < 100; i++ {
		var list struct {
			Jobs []struct {
				ID    int64
				State string
				Error string
			}
		}
		e.expect(e.do("GET", "/api/v1/system/tasks", nil), 200).json(&list)
		for _, j := range list.Jobs {
			if j.ID == id && (j.State == "succeeded" || j.State == "failed") {
				if j.State == "failed" {
					t.Fatal(j.Error)
				}
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("job did not finish")
}

func TestIndexerSourcesAPI(t *testing.T) {
	e := newEnv(t, noLimits)
	e.setup()
	f, base := startFakes(t)
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"allowedHosts": []string{"127.0.0.0/8"}}), 200)

	src := map[string]any{"kind": "prowlarr", "url": base + "/prowlarr", "apiKey": "sourcekey"}
	var prev struct {
		Items []struct{ Ref, Action, FeedUrl string }
	}
	e.expect(e.do("POST", "/api/v1/indexers/sources/preview", src), 200).json(&prev)
	if len(prev.Items) != 2 || prev.Items[0].Action != "add" || !strings.HasSuffix(prev.Items[0].FeedUrl, "/prowlarr/1/api") {
		t.Fatalf("%+v", prev)
	}
	bad := map[string]any{"kind": "prowlarr", "url": base + "/prowlarr", "apiKey": "wrong"}
	if r := e.expect(e.do("POST", "/api/v1/indexers/sources/preview", bad), 502); r.code() != "source_error" {
		t.Fatal(r.code())
	}
	var sync struct {
		Added  int
		Source struct {
			ID     int64
			ApiKey struct{ Configured bool }
		}
	}
	r := e.expect(e.do("POST", "/api/v1/indexers/sources", src), 201)
	if strings.Contains(string(r.body), "sourcekey") {
		t.Fatal("key returned")
	}
	r.json(&sync)
	if sync.Added != 2 || !sync.Source.ApiKey.Configured {
		t.Fatalf("%s", r.body)
	}
	f.Lock()
	f.ProwlarrIDs = []int{1}
	f.Unlock()
	e.expect(e.do("POST", "/api/v1/indexers/sources/preview", map[string]any{"sourceId": sync.Source.ID}), 200).json(&prev)
	if len(prev.Items) != 2 || prev.Items[1].Action != "disable" {
		t.Fatalf("%+v", prev)
	}
	var res struct{ Disabled, Unchanged int }
	e.expect(e.do("POST", "/api/v1/indexers/sources/"+itoa(sync.Source.ID)+"/sync", nil), 200).json(&res)
	if res.Disabled != 1 || res.Unchanged != 1 {
		t.Fatalf("%+v", res)
	}
	var list []struct {
		Enabled bool
		Source  *struct{ Name string }
	}
	e.expect(e.do("GET", "/api/v1/indexers", nil), 200).json(&list)
	if len(list) != 2 || list[0].Source == nil {
		t.Fatalf("%+v", list)
	}
	var sources []any
	e.expect(e.do("GET", "/api/v1/indexers/sources", nil), 200).json(&sources)
	if len(sources) != 1 {
		t.Fatal(sources)
	}
	e.expect(e.do("DELETE", "/api/v1/indexers/sources/"+itoa(sync.Source.ID), nil), 204)
	list = nil
	e.expect(e.do("GET", "/api/v1/indexers", nil), 200).json(&list)
	if len(list) != 2 || list[0].Source != nil {
		t.Fatalf("%+v", list)
	}
}
