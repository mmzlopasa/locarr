package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/downloaders/testkit"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
)

// apiFake is a minimal in-memory torrent client for API tests.
type apiFake struct {
	mu    sync.Mutex
	items map[string]downloaders.Item
}

func (f *apiFake) Capabilities() downloaders.Capabilities {
	return downloaders.Capabilities{Protocol: downloaders.Torrent, Categories: true, CreateCategories: true, Priority: true, DeletesData: true}
}
func (f *apiFake) Test(context.Context) (downloaders.Status, error) {
	return downloaders.Status{Version: "9.9"}, nil
}
func (f *apiFake) Add(_ context.Context, r downloaders.AddRequest) (string, error) {
	m, err := downloaders.ParseMagnet(r.Magnet)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[m.InfoHash] = downloaders.Item{ID: m.InfoHash, Name: r.Name, Category: r.Category, State: downloaders.StateDownloading,
		RawState: "downloading", Progress: 0.25, Size: 4000, Downloaded: 1000, DownloadSpeed: 500, Seeds: 3}
	return m.InfoHash, nil
}
func (f *apiFake) List(_ context.Context, cat string) ([]downloaders.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []downloaders.Item
	for _, it := range f.items {
		if cat == "" || it.Category == cat {
			out = append(out, it)
		}
	}
	return out, nil
}
func (f *apiFake) Files(context.Context, string) ([]downloaders.File, error) { return nil, nil }
func (f *apiFake) Remove(_ context.Context, id string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.items, id)
	return nil
}
func (f *apiFake) Pause(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	it := f.items[id]
	it.State, it.RawState = downloaders.StatePaused, "pausedDL"
	f.items[id] = it
	return nil
}
func (f *apiFake) Resume(context.Context, string) error                            { return nil }
func (f *apiFake) SetPriority(context.Context, string, downloaders.Priority) error { return nil }
func (f *apiFake) Categories(context.Context) ([]downloaders.Category, error)      { return nil, nil }
func (f *apiFake) EnsureCategory(context.Context, string, string) error            { return nil }

func init() {
	downloaders.Register(downloaders.Implementation{Name: "apifake", DisplayName: "API fake", Protocol: downloaders.Torrent,
		DefaultPort: 9999, Fields: []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort, downloaders.FieldPassword},
		New: func(downloaders.Config) (downloaders.Client, error) { return nil, nil }})
}

func TestDownloadClientsQueueAndBlocklistAPI(t *testing.T) {
	e := newEnv(t, nil)
	e.setup()
	fake := &apiFake{items: map[string]downloaders.Item{}}
	e.deps.Downloads.NewClient = func(downloaders.Implementation, downloaders.Config) (downloaders.Client, error) { return fake, nil }

	var impls []struct{ Name, DisplayName string }
	e.expect(e.do("GET", "/api/v1/downloadclients/implementations", nil), 200).json(&impls)
	names := ""
	for _, i := range impls {
		names += i.Name + " "
	}
	for _, want := range []string{"qbittorrent", "transmission", "deluge", "rtorrent", "aria2", "sabnzbd", "nzbget"} {
		if !strings.Contains(names, want+" ") {
			t.Errorf("implementation %s missing from %s", want, names)
		}
	}

	const secret = "client-password-xyz"
	body := map[string]any{"name": "Fake", "implementation": "apifake", "host": "fake.lan", "password": secret,
		"categories": map[string]string{"movies": "movies", "series": "", "music": ""}}
	r := e.expect(e.do("POST", "/api/v1/downloadclients/test", map[string]any{"client": body}), 200)
	if !strings.Contains(string(r.body), `"ok":true`) || !strings.Contains(string(r.body), `"version":"9.9"`) {
		t.Fatalf("test: %s", r.body)
	}
	var c struct {
		ID       int64
		Port     int
		Password struct{ Configured bool }
	}
	r = e.expect(e.do("POST", "/api/v1/downloadclients", body), 201)
	if strings.Contains(string(r.body), secret) {
		t.Fatal("password echoed")
	}
	r.json(&c)
	if !c.Password.Configured || c.Port != 9999 {
		t.Fatalf("client: %s", r.body)
	}
	e.expect(e.do("POST", "/api/v1/downloadclients", body), 409)

	link, hash := testkit.RandomMagnet("Some.Movie")
	var item struct {
		ID           int64
		ClientItemID string `json:"clientItemId"`
		Category     string
		ItemRef      map[string]int `json:"itemRef"`
	}
	e.expect(e.do("POST", "/api/v1/queue/grab", map[string]any{"downloadUrl": link, "protocol": "torrent", "title": "Some.Movie",
		"mediaKind": "movies", "itemRef": map[string]int{"movieId": 7}, "seedRatio": 2}), 201).json(&item)
	if item.ClientItemID != hash || item.Category != "movies" || item.ItemRef["movieId"] != 7 {
		t.Fatalf("grab: %+v", item)
	}
	e.expect(e.do("POST", "/api/v1/queue/grab", map[string]any{"downloadUrl": link, "protocol": "torrent", "title": "Some.Movie"}), 409)
	if r := e.do("POST", "/api/v1/queue/grab", map[string]any{"downloadUrl": "magnet:?xt=x", "protocol": "usenet", "title": "x"}); r.code() != "no_download_client" {
		t.Fatalf("usenet grab: %s", r.body)
	}

	// Someone else's item in our category shows as unknown.
	fake.items["ffff"] = downloaders.Item{ID: "ffff", Name: "Manual.Add", Category: "movies", State: downloaders.StateSeeding, RawState: "uploading", Progress: 1}
	if err := e.deps.Downloads.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	var q struct {
		Items []struct {
			ID       int64
			Unknown  bool
			State    string
			Progress float64
			Title    string
		}
		Clients []struct {
			Name   string
			Status struct{ Ok bool }
		}
	}
	e.expect(e.do("GET", "/api/v1/queue", nil), 200).json(&q)
	if len(q.Items) != 2 || len(q.Clients) != 1 || !q.Clients[0].Status.Ok {
		t.Fatalf("queue: %+v", q)
	}
	for _, it := range q.Items {
		if it.Unknown != (it.Title == "Manual.Add") {
			t.Fatalf("unknown flag wrong: %+v", it)
		}
		if !it.Unknown && (it.State != "downloading" || it.Progress != 0.25) {
			t.Fatalf("tracked item: %+v", it)
		}
	}

	e.expect(e.do("POST", "/api/v1/queue/"+strconv.FormatInt(item.ID, 10)+"/pause", nil), 204)
	e.expect(e.do("POST", "/api/v1/queue/"+strconv.FormatInt(item.ID, 10)+"/priority", map[string]string{"priority": "top"}), 204)
	e.expect(e.do("DELETE", "/api/v1/queue/"+strconv.FormatInt(item.ID, 10)+"?blocklist=true&reason=fake", nil), 204)
	if _, ok := fake.items[hash]; ok {
		t.Fatal("not removed from the client")
	}
	var events []struct{ Type string }
	e.expect(e.do("GET", "/api/v1/queue/"+strconv.FormatInt(item.ID, 10)+"/events", nil), 200).json(&events)
	if len(events) != 4 || events[0].Type != "grabbed" || events[1].Type != "state" || events[2].Type != "removed" || events[3].Type != "blocklisted" {
		t.Fatalf("events: %+v", events)
	}
	var bl []struct {
		ID       int64
		InfoHash string `json:"infoHash"`
	}
	e.expect(e.do("GET", "/api/v1/queue/blocklist", nil), 200).json(&bl)
	if len(bl) != 1 || bl[0].InfoHash != hash {
		t.Fatalf("blocklist: %+v", bl)
	}
	if blocked, _ := e.deps.Downloads.IsBlocklisted(context.Background(), downloads.Candidate{InfoHash: hash}); !blocked {
		t.Fatal("hook does not see the entry")
	}
	e.expect(e.do("DELETE", "/api/v1/queue/blocklist/"+strconv.FormatInt(bl[0].ID, 10), nil), 204)

	// Remote path mappings: local paths must stay inside the data roots.
	local := filepath.Join(e.data, "downloads")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	e.expect(e.do("POST", "/api/v1/remotepathmappings", map[string]string{"host": "fake.lan", "remotePath": "/downloads", "localPath": local}), 201)
	if r := e.do("POST", "/api/v1/remotepathmappings", map[string]string{"host": "fake.lan", "remotePath": "/x", "localPath": "/etc"}); r.code() != "path_not_allowed" {
		t.Fatalf("mapping outside roots: %s", r.body)
	}
	e.expect(e.do("PATCH", "/api/v1/downloadclients/settings", map[string]int{"pollSeconds": 60, "stalledHours": 0}), 200)
	if r := e.do("PATCH", "/api/v1/downloadclients/settings", map[string]int{"pollSeconds": 1}); r.StatusCode != 400 {
		t.Fatalf("poll 1s accepted: %s", r.body)
	}
	e.expect(e.do("DELETE", "/api/v1/downloadclients/"+strconv.FormatInt(c.ID, 10), nil), 204)
}
