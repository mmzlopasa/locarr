package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/metadata/tmdb/fake"
)

// flowClient is a torrent client that "downloads" a release by writing a
// small movie folder (main file, sample, subtitle) into its download folder.
type flowClient struct {
	mu    sync.Mutex
	dir   string
	items map[string]downloaders.Item
	// fail makes the next added release fail in the client.
	fail  map[string]bool
	added []string
	// writeFolder, when set, writes a finished release instead of writeMovieFolder.
	writeFolder func(t *testing.T, folder, name string)
}

func newFlowClient(dir string) *flowClient {
	return &flowClient{dir: dir, items: map[string]downloaders.Item{}, fail: map[string]bool{}}
}

func (f *flowClient) Capabilities() downloaders.Capabilities {
	return downloaders.Capabilities{Protocol: downloaders.Torrent, Categories: true, CreateCategories: true, DeletesData: true}
}
func (f *flowClient) Test(context.Context) (downloaders.Status, error) {
	return downloaders.Status{Version: "flow"}, nil
}

func (f *flowClient) Add(_ context.Context, r downloaders.AddRequest) (string, error) {
	var hash, name string
	switch {
	case len(r.TorrentFile) > 0:
		ti, err := downloaders.ParseTorrent(r.TorrentFile)
		if err != nil {
			return "", err
		}
		hash, name = ti.InfoHash, ti.Name
	case r.Magnet != "":
		m, err := downloaders.ParseMagnet(r.Magnet)
		if err != nil {
			return "", err
		}
		hash, name = m.InfoHash, r.Name
	default:
		return "", fmt.Errorf("unsupported add")
	}
	if name == "" {
		name = r.Name
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, name)
	f.items[hash] = downloaders.Item{ID: hash, Name: name, Category: r.Category, State: downloaders.StateDownloading, RawState: "downloading",
		Progress: 0.5, Size: 3 << 20, Downloaded: 1 << 20, DownloadSpeed: 1000, Seeds: 5, SavePath: f.dir}
	return hash, nil
}

// finish completes (or fails) every downloading item.
func (f *flowClient) finish(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, it := range f.items {
		if it.State != downloaders.StateDownloading {
			continue
		}
		if f.fail[it.Name] {
			it.State, it.RawState, it.Message = downloaders.StateFailed, "error", "tracker said no"
			f.items[id] = it
			continue
		}
		folder := filepath.Join(f.dir, it.Name)
		if f.writeFolder != nil {
			f.writeFolder(t, folder, it.Name)
		} else {
			writeMovieFolder(t, folder, it.Name)
		}
		it.State, it.RawState, it.Progress, it.Downloaded = downloaders.StateSeeding, "seeding", 1, it.Size
		it.ContentPath = folder
		f.items[id] = it
	}
}

func writeMovieFolder(t *testing.T, folder, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(folder, "Sample"), 0o755); err != nil {
		t.Fatal(err)
	}
	main := make([]byte, 3<<20)
	copy(main, name)
	for path, data := range map[string][]byte{
		filepath.Join(folder, name+".mkv"):            []byte(string(main)),
		filepath.Join(folder, "Sample", "sample.mkv"): make([]byte, 64<<10),
		filepath.Join(folder, name+".en.srt"):         []byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"),
		filepath.Join(folder, "release.nfo"):          []byte("nfo"),
	} {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *flowClient) List(_ context.Context, cat string) ([]downloaders.Item, error) {
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
func (f *flowClient) Files(context.Context, string) ([]downloaders.File, error) { return nil, nil }
func (f *flowClient) Remove(_ context.Context, id string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.items, id)
	return nil
}
func (f *flowClient) Pause(context.Context, string) error                             { return nil }
func (f *flowClient) Resume(context.Context, string) error                            { return nil }
func (f *flowClient) SetPriority(context.Context, string, downloaders.Priority) error { return nil }
func (f *flowClient) Categories(context.Context) ([]downloaders.Category, error)      { return nil, nil }
func (f *flowClient) EnsureCategory(context.Context, string, string) error            { return nil }

// libraryEnv is an environment with TMDb, a fake indexer, a download client
// and a movie root folder.
type libraryEnv struct {
	*env
	client    *flowClient
	rootID    int64
	profileID int64
	movies    string
	downloads string
}

func newLibraryEnv(t *testing.T) *libraryEnv {
	t.Helper()
	e := newEnv(t, noLimits)
	e.setup()
	_, base := startFakes(t)
	le := &libraryEnv{env: e, movies: filepath.Join(e.data, "media", "movies"), downloads: filepath.Join(e.data, "downloads")}
	le.client = newFlowClient(le.downloads)
	e.deps.Downloads.NewClient = func(downloaders.Implementation, downloaders.Config) (downloaders.Client, error) {
		return le.client, nil
	}
	e.expect(e.do("PUT", "/api/v1/settings/metadata", map[string]any{"tmdbApiKey": fake.APIKey}), 200)
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"allowedHosts": []string{"127.0.0.1"}}), 200)
	e.expect(e.do("POST", "/api/v1/indexers", map[string]any{"name": "Fake Torznab", "kind": "torznab", "baseUrl": base + "/torznab",
		"apiKey": "fakekey"}), 201)
	e.expect(e.do("POST", "/api/v1/downloadclients", map[string]any{"name": "Flow", "implementation": "apifake", "host": "flow.lan",
		"password": "x", "categories": map[string]string{"movies": "movies", "series": "", "music": ""}}), 201)
	var rf struct{ ID int64 }
	e.expect(e.do("POST", "/api/v1/rootfolders", map[string]any{"path": le.movies, "kind": "movies"}), 201).json(&rf)
	le.rootID = rf.ID
	var profiles []struct {
		ID   int64
		Name string
		Kind string
	}
	e.expect(e.do("GET", "/api/v1/qualityprofiles", nil), 200).json(&profiles)
	for _, p := range profiles {
		if p.Kind == "movies" && p.Name == "HD-720p/1080p" {
			le.profileID = p.ID
		}
	}
	if le.profileID == 0 {
		t.Fatal("no movie profile")
	}
	return le
}

type apiMovie struct {
	ID            int64
	Title         string
	Year          int
	Path          string
	Monitored     bool
	Available     bool
	LibraryStatus string `json:"libraryStatus"`
	File          *struct {
		Path    string
		Quality struct{ ID string }
	}
}

func (le *libraryEnv) addMovie(tmdbID int64, search bool) apiMovie {
	le.t.Helper()
	var m apiMovie
	le.expect(le.do("POST", "/api/v1/movies", map[string]any{"tmdbId": tmdbID, "rootFolderId": le.rootID, "qualityProfileId": le.profileID,
		"minimumAvailability": "released", "searchOnAdd": search}), 201).json(&m)
	return m
}

func (le *libraryEnv) movie(id int64) apiMovie {
	le.t.Helper()
	var m apiMovie
	le.expect(le.do("GET", fmt.Sprintf("/api/v1/movies/%d", id), nil), 200).json(&m)
	return m
}

type apiTimeline struct {
	Explanation struct {
		Code    string
		Message string
	}
	Events []struct {
		Type    string
		Message string
		Data    json.RawMessage
	}
}

func (le *libraryEnv) timeline(id int64) apiTimeline {
	le.t.Helper()
	var tl apiTimeline
	le.expect(le.do("GET", fmt.Sprintf("/api/v1/timeline?item=movie:%d", id), nil), 200).json(&tl)
	return tl
}

func (tl apiTimeline) types() string {
	var out []string
	for i := len(tl.Events) - 1; i >= 0; i-- {
		out = append(out, tl.Events[i].Type)
	}
	return strings.Join(out, " ")
}

// waitFor polls cond until it holds or the time is up.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (le *libraryEnv) downloadCount(id int64) int {
	var n int
	_ = le.deps.DB.R.QueryRow(`SELECT COUNT(*) FROM downloads WHERE json_extract(item_ref, '$.ref') = ?`, fmt.Sprintf("movie:%d", id)).Scan(&n)
	return n
}

// poll lets the tracker look at the client once.
func (le *libraryEnv) poll() {
	le.t.Helper()
	if err := le.deps.Downloads.Poll(context.Background()); err != nil {
		le.t.Fatal(err)
	}
}
