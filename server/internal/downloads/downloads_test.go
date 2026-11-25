package downloads

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/downloaders/testkit"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// fakeClient is an in-memory download client.
type fakeClient struct {
	mu       sync.Mutex
	caps     downloaders.Capabilities
	items    map[string]downloaders.Item
	added    []downloaders.AddRequest
	removed  map[string]bool // id -> deleteData
	testErr  error
	cats     []downloaders.Category
	ensured  []string
	listCats []string
	savePath string
}

func newFake(p downloaders.Protocol) *fakeClient {
	return &fakeClient{caps: downloaders.Capabilities{Protocol: p, Categories: true, CreateCategories: true, Priority: true,
		DeletesData: true}, items: map[string]downloaders.Item{}, removed: map[string]bool{}}
}

func (f *fakeClient) Capabilities() downloaders.Capabilities { return f.caps }
func (f *fakeClient) Test(context.Context) (downloaders.Status, error) {
	if f.testErr != nil {
		return downloaders.Status{}, f.testErr
	}
	free := int64(1 << 40)
	return downloaders.Status{Version: "1.0", FreeSpace: &free, SavePath: f.savePath}, nil
}

func (f *fakeClient) Add(_ context.Context, r downloaders.AddRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, r)
	var id string
	switch {
	case r.Magnet != "":
		m, _ := downloaders.ParseMagnet(r.Magnet)
		id = m.InfoHash
	case len(r.TorrentFile) > 0:
		ti, _ := downloaders.ParseTorrent(r.TorrentFile)
		id = ti.InfoHash
	default:
		id = "nzo_" + r.Name
	}
	f.items[id] = downloaders.Item{ID: id, Name: r.Name, Category: r.Category, State: downloaders.StateQueued, RawState: "queued", Seeds: -1}
	return id, nil
}

func (f *fakeClient) set(id string, fn func(*downloaders.Item)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it := f.items[id]
	fn(&it)
	f.items[id] = it
}

func (f *fakeClient) List(_ context.Context, cat string) ([]downloaders.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCats = append(f.listCats, cat)
	var out []downloaders.Item
	for _, it := range f.items {
		if cat == "" || strings.EqualFold(it.Category, cat) {
			out = append(out, it)
		}
	}
	return out, nil
}

func (f *fakeClient) Files(context.Context, string) ([]downloaders.File, error) { return nil, nil }

func (f *fakeClient) Remove(_ context.Context, id string, del bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[id]; !ok {
		return downloaders.ErrItemNotFound
	}
	delete(f.items, id)
	f.removed[id] = del
	return nil
}
func (f *fakeClient) Pause(_ context.Context, id string) error {
	f.set(id, func(it *downloaders.Item) { it.State = downloaders.StatePaused })
	return nil
}
func (f *fakeClient) Resume(_ context.Context, id string) error {
	f.set(id, func(it *downloaders.Item) { it.State = downloaders.StateDownloading })
	return nil
}
func (f *fakeClient) SetPriority(context.Context, string, downloaders.Priority) error { return nil }
func (f *fakeClient) Categories(context.Context) ([]downloaders.Category, error) {
	return f.cats, nil
}
func (f *fakeClient) EnsureCategory(_ context.Context, name, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensured = append(f.ensured, name)
	f.cats = append(f.cats, downloaders.Category{Name: name})
	return nil
}

func init() {
	for _, name := range []string{"faketorrent", "fakeusenet"} {
		p := downloaders.Torrent
		if name == "fakeusenet" {
			p = downloaders.Usenet
		}
		downloaders.Register(downloaders.Implementation{Name: name, DisplayName: name, Protocol: p, DefaultPort: 1234,
			Fields:   []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort, downloaders.FieldPassword},
			Required: []downloaders.Field{downloaders.FieldPassword},
			New:      func(downloaders.Config) (downloaders.Client, error) { return nil, errors.New("use the fake") }})
	}
}

type env struct {
	s     *Service
	fakes map[string]*fakeClient // by host
	cfgs  []downloaders.Config
	data  string
	now   time.Time
	mu    sync.Mutex
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d := db.OpenTest(t)
	box, err := secrets.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	e := &env{fakes: map[string]*fakeClient{}, data: data, now: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	s := New(d, box, settings.New(d, box), []string{data}, nil)
	s.Now = func() time.Time {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.now
	}
	s.NewClient = func(impl downloaders.Implementation, cfg downloaders.Config) (downloaders.Client, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.cfgs = append(e.cfgs, cfg)
		f, ok := e.fakes[cfg.Host]
		if !ok {
			f = newFake(impl.Protocol)
			e.fakes[cfg.Host] = f
		}
		return f, nil
	}
	e.s = s
	return e
}

func (e *env) advance(d time.Duration) {
	e.mu.Lock()
	e.now = e.now.Add(d)
	e.mu.Unlock()
}

func pw(s string) *string { return &s }

func (e *env) addClient(t *testing.T, name, impl, host string, mod func(*ClientInput)) Client {
	t.Helper()
	in := ClientInput{Name: name, Implementation: impl, Enabled: true, Host: host, Port: 1234, Password: pw("secret"),
		CategoryMovies: "movies-locarr", CategorySeries: "tv-locarr"}
	if mod != nil {
		mod(&in)
	}
	c, err := e.s.CreateClient(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) handler(_ context.Context, ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recorder) types() []EventType {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []EventType
	for _, e := range r.events {
		out = append(out, e.Type)
	}
	return out
}

func TestClientCRUDKeepsSecretsWriteOnly(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.addClient(t, "Main", "faketorrent", "qbit", nil)
	if !c.PasswordSet || c.APIKeySet {
		t.Fatalf("secret flags = %v/%v", c.PasswordSet, c.APIKeySet)
	}
	var raw []byte
	if err := e.s.DB.R.QueryRow(`SELECT password FROM download_clients WHERE id = ?`, c.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") {
		t.Fatal("password stored in plain text")
	}
	// Update without a password keeps it.
	in := ClientInput{Name: "Main 2", Implementation: "faketorrent", Enabled: true, Host: "qbit", Port: 1234}
	c2, err := e.s.UpdateClient(ctx, c.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if !c2.PasswordSet || c2.Name != "Main 2" {
		t.Fatalf("after update: %+v", c2)
	}
	gotPW, _, err := e.s.secrets(ctx, c.ID)
	if err != nil || gotPW != "secret" {
		t.Fatalf("secret = %q, %v", gotPW, err)
	}
	// Clearing a required password is refused.
	in.Password = pw("")
	if _, err := e.s.UpdateClient(ctx, c.ID, in); !errors.Is(err, ErrInvalid) {
		t.Fatalf("clearing required password: %v", err)
	}
	if _, err := e.s.CreateClient(ctx, ClientInput{Name: "Main 2", Implementation: "faketorrent", Host: "x", Port: 1, Password: pw("p")}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("duplicate name: %v", err)
	}
	if _, err := e.s.CreateClient(ctx, ClientInput{Name: "Bad", Implementation: "faketorrent", Host: "http://x/", Port: 1, Password: pw("p")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("URL as host: %v", err)
	}
	if err := e.s.DeleteClient(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.GetClient(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestTrackingCompletesThroughRemotePathMapping(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	rec := &recorder{}
	e.s.Bus.Subscribe(rec.handler)
	c := e.addClient(t, "Seedbox", "faketorrent", "seedbox", nil)
	local := filepath.Join(e.data, "downloads", "torrents")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.CreateMapping(ctx, Mapping{Host: "Seedbox", RemotePath: "/remote/dl/", LocalPath: local}); err != nil {
		t.Fatal(err)
	}
	link, hash := testkit.RandomMagnet("Some.Movie.2024.1080p")
	ref := json.RawMessage(`{"movieId":42}`)
	d, err := e.s.Grab(ctx, GrabRequest{DownloadURL: link, Protocol: downloaders.Torrent, Title: "Some.Movie.2024.1080p",
		Indexer: "Test", MediaKind: KindMovies, ItemRef: ref})
	if err != nil {
		t.Fatal(err)
	}
	if d.ClientItemID != hash || d.Category != "movies-locarr" || *d.ClientID != c.ID || string(d.ItemRef) != `{"movieId":42}` {
		t.Fatalf("download = %+v", d)
	}
	fake := e.fakes["seedbox"]
	if len(fake.ensured) != 1 || fake.ensured[0] != "movies-locarr" {
		t.Fatalf("category not ensured: %v", fake.ensured)
	}
	// Grabbing the same release again is refused.
	if _, err := e.s.Grab(ctx, GrabRequest{DownloadURL: link, Protocol: downloaders.Torrent, Title: "x"}); !errors.Is(err, ErrAlreadyQueued) {
		t.Fatalf("second grab: %v", err)
	}

	fake.set(hash, func(it *downloaders.Item) {
		it.State, it.RawState, it.Progress, it.DownloadSpeed, it.Size = downloaders.StateDownloading, "downloading", 0.5, 1000, 2000
	})
	if err := e.s.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.s.Get(ctx, d.ID)
	if got.State != downloaders.StateDownloading || got.Progress != 0.5 || got.Status != StatusActive {
		t.Fatalf("after first poll: %+v", got)
	}

	// Finished, but the files are not there yet: not ready, with a hint.
	fake.set(hash, func(it *downloaders.Item) {
		it.State, it.RawState, it.Progress, it.SavePath, it.ContentPath = downloaders.StateSeeding, "uploading", 1, "/remote/dl", "/remote/dl/Some.Movie.2024.1080p"
	})
	_ = e.s.Poll(ctx)
	got, _ = e.s.Get(ctx, d.ID)
	if got.Status != StatusActive || !strings.Contains(got.Message, "remote path mapping") {
		t.Fatalf("missing locally: %+v", got)
	}
	if err := os.MkdirAll(filepath.Join(local, "Some.Movie.2024.1080p"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Still moving: not ready either.
	fake.set(hash, func(it *downloaders.Item) { it.Busy = true })
	_ = e.s.Poll(ctx)
	if got, _ = e.s.Get(ctx, d.ID); got.Status != StatusActive {
		t.Fatalf("busy item completed: %+v", got)
	}
	fake.set(hash, func(it *downloaders.Item) { it.Busy = false })
	_ = e.s.Poll(ctx)
	got, _ = e.s.Get(ctx, d.ID)
	if got.Status != StatusCompleted || got.LocalPath != filepath.Join(local, "Some.Movie.2024.1080p") || got.CompletedAt == nil {
		t.Fatalf("completed: %+v", got)
	}
	types := rec.types()
	if len(types) != 2 || types[0] != EventGrabbed || types[1] != EventCompleted {
		t.Fatalf("bus events = %v", types)
	}
	evs, err := e.s.Events(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, ev := range evs {
		kinds = append(kinds, string(ev.Type)+":"+ev.ToState)
	}
	want := "grabbed:queued state:downloading progress:downloading state:seeding completed:seeding"
	if strings.Join(kinds, " ") != want {
		t.Fatalf("history = %v, want %s", kinds, want)
	}

	// Imported, seeding goal (ratio 1) reached → removed with its data.
	if err := e.s.MarkImported(ctx, d.ID, "imported to /data/media/movies"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.UpdateClient(ctx, c.ID, ClientInput{Name: c.Name, Implementation: c.Implementation, Enabled: true,
		Host: c.Host, Port: c.Port, CategoryMovies: c.CategoryMovies, RemoveCompleted: true}); err != nil {
		t.Fatal(err)
	}
	fake = e.fakes["seedbox"]
	_, _ = e.s.DB.W.Exec(`UPDATE downloads SET seed_ratio = 1 WHERE id = ?`, d.ID)
	fake.set(hash, func(it *downloaders.Item) { it.Ratio = 0.4 })
	_ = e.s.Poll(ctx)
	if _, gone := fake.removed[hash]; gone {
		t.Fatal("removed before the seed goal")
	}
	fake.set(hash, func(it *downloaders.Item) { it.Ratio = 1.2 })
	_ = e.s.Poll(ctx)
	if del, gone := fake.removed[hash]; !gone || !del {
		t.Fatalf("not removed after the seed goal (removed=%v)", fake.removed)
	}
}

func TestUnknownItemsAreShownButNeverTouched(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addClient(t, "Main", "faketorrent", "qbit", nil)
	if err := e.s.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	fake := e.fakes["qbit"]
	fake.items["abc"] = downloaders.Item{ID: "abc", Name: "Added.By.Hand", Category: "movies-locarr", State: downloaders.StateSeeding, RawState: "uploading", Progress: 1}
	fake.items["def"] = downloaders.Item{ID: "def", Name: "Other.Category", Category: "linux-isos", State: downloaders.StateSeeding, RawState: "uploading"}
	_ = e.s.Poll(ctx)
	q, err := e.s.Queue(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(q) != 1 || !q[0].Unknown || q[0].Download.Title != "Added.By.Hand" || q[0].Download.ID != 0 {
		t.Fatalf("queue = %+v", q)
	}
	for _, c := range fake.listCats {
		if c == "" {
			t.Fatal("listed outside Locarr's categories")
		}
	}
	if len(fake.removed) != 0 || len(fake.items) != 2 {
		t.Fatal("unknown items were touched")
	}
}

func TestFailuresStallsAndRemovals(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	rec := &recorder{}
	e.s.Bus.Subscribe(rec.handler)
	e.addClient(t, "Main", "faketorrent", "qbit", nil)
	grab := func(name string) Download {
		link, _ := testkit.RandomMagnet(name)
		d, err := e.s.Grab(ctx, GrabRequest{DownloadURL: link, Protocol: downloaders.Torrent, Title: name, MediaKind: KindSeries})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	failed, stalled, gone := grab("A.S01E01"), grab("B.S01E01"), grab("C.S01E01")
	fake := e.fakes["qbit"]
	fake.set(failed.ClientItemID, func(it *downloaders.Item) {
		it.State, it.RawState, it.Message = downloaders.StateFailed, "missingFiles", ""
	})
	fake.set(stalled.ClientItemID, func(it *downloaders.Item) {
		it.State, it.RawState, it.Progress, it.Seeds = downloaders.StateDownloading, "stalledDL", 0.1, 0
	})
	_ = e.s.Poll(ctx)
	if d, _ := e.s.Get(ctx, failed.ID); d.Status != StatusFailed || d.Message == "" {
		t.Fatalf("failed: %+v", d)
	}
	e.advance(25 * time.Hour)
	delete(fake.items, gone.ClientItemID)
	_ = e.s.Poll(ctx)
	if d, _ := e.s.Get(ctx, stalled.ID); d.Status != StatusFailed || !strings.Contains(d.Message, "stalled") {
		t.Fatalf("stalled: %+v", d)
	}
	if d, _ := e.s.Get(ctx, gone.ID); d.Status != StatusActive || d.MissingCount != 1 {
		t.Fatalf("one miss must not remove: %+v", d)
	}
	_ = e.s.Poll(ctx)
	if d, _ := e.s.Get(ctx, gone.ID); d.Status != StatusRemoved {
		t.Fatalf("gone: %+v", d)
	}
	var reasons []string
	for _, ev := range rec.events {
		if ev.Type != EventGrabbed {
			reasons = append(reasons, string(ev.Type)+":"+ev.Reason)
		}
	}
	if strings.Join(reasons, " ") != "failed:missing_files failed:stalled removed:removed_in_client" {
		t.Fatalf("events = %v", reasons)
	}
}

func TestRemoveAndBlocklist(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addClient(t, "Main", "faketorrent", "qbit", nil)
	link, hash := testkit.RandomMagnet("Bad.Release")
	idx := int64(3)
	d, err := e.s.Grab(ctx, GrabRequest{DownloadURL: link, Protocol: downloaders.Torrent, Title: "Bad.Release", IndexerID: &idx,
		GUID: "guid-1", MediaKind: KindMovies})
	if err != nil {
		t.Fatal(err)
	}
	if blocked, _ := e.s.IsBlocklisted(ctx, Candidate{Title: "Bad.Release", InfoHash: hash}); blocked {
		t.Fatal("blocked before blocklisting")
	}
	if err := e.s.Remove(ctx, d.ID, RemoveOptions{DeleteData: true, Blocklist: true, Message: "fake"}); err != nil {
		t.Fatal(err)
	}
	if del, ok := e.fakes["qbit"].removed[hash]; !ok || !del {
		t.Fatal("not removed with data")
	}
	for _, c := range []Candidate{{InfoHash: strings.ToUpper(hash)}, {GUID: "guid-1"}, {Title: "bad.release", IndexerID: &idx}} {
		if blocked, reason := e.s.IsBlocklisted(ctx, c); !blocked || reason != ReasonManual {
			t.Fatalf("%+v not blocked", c)
		}
	}
	other := int64(4)
	if blocked, _ := e.s.IsBlocklisted(ctx, Candidate{Title: "Bad.Release", IndexerID: &other}); blocked {
		t.Fatal("same title from another indexer must not be blocked")
	}
	list, _ := e.s.Blocklist(ctx, 0)
	if len(list) != 1 || *list[0].DownloadID != d.ID {
		t.Fatalf("blocklist = %+v", list)
	}
	if err := e.s.DeleteBlock(ctx, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if blocked, _ := e.s.IsBlocklisted(ctx, Candidate{InfoHash: hash}); blocked {
		t.Fatal("still blocked after delete")
	}
}

func TestLocalDeleteWhenTheClientCannot(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addClient(t, "Aria", "faketorrent", "aria", nil)
	tor := testkit.RandomTorrent("x", 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(tor.File) }))
	defer srv.Close()
	d, err := e.s.Grab(ctx, GrabRequest{DownloadURL: srv.URL + "/dl", Protocol: downloaders.Torrent, Title: "x", MediaKind: KindMovies})
	if err != nil {
		t.Fatal(err)
	}
	fake := e.fakes["aria"]
	fake.caps.DeletesData = false
	content := filepath.Join(e.data, "downloads", tor.Name)
	if err := os.MkdirAll(filepath.Dir(content), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(content, tor.Data, 0o600); err != nil {
		t.Fatal(err)
	}
	fake.set(tor.InfoHash, func(it *downloaders.Item) { it.ContentPath = content })
	_ = e.s.Poll(ctx)
	if err := e.s.Remove(ctx, d.ID, RemoveOptions{DeleteData: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(content); !os.IsNotExist(err) {
		t.Fatalf("content not deleted: %v", err)
	}
	// The data root itself is never deleted.
	if err := e.s.deleteLocal(ctx, e.data); !errors.Is(err, errUnsafeDelete) {
		t.Fatalf("deleting a data root: %v", err)
	}
	if err := e.s.deleteLocal(ctx, "/etc"); err == nil {
		t.Fatal("deleting outside the data roots must fail")
	}
}

func TestGrabFetchesThroughTheServer(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addClient(t, "Q", "faketorrent", "qbit", nil)
	e.addClient(t, "S", "fakeusenet", "sab", nil)
	tor := testkit.RandomTorrent("fetch", 1000)
	link, hash := testkit.RandomMagnet("redirected")
	crossTor := testkit.RandomTorrent("cross", 1000)
	var crossQuery string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		crossQuery = r.URL.RawQuery
		_, _ = w.Write(crossTor.File)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/t":
			_, _ = w.Write(tor.File)
		case "/same":
			http.Redirect(w, r, "/t", http.StatusFound)
		case "/magnet":
			http.Redirect(w, r, link, http.StatusFound)
		case "/cross":
			http.Redirect(w, r, strings.Replace(other.URL, "127.0.0.1", "localhost", 1)+"/t?apikey=leak&id=7", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/meta":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
		case "/big":
			_, _ = w.Write(make([]byte, MaxTorrentFile+10))
		case "/html":
			_, _ = w.Write([]byte("<html>login required</html>"))
		case "/nzb":
			_, _ = w.Write(testkit.NZB("Some.Show.S01E01"))
		case "/404":
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	d, err := e.s.Grab(ctx, GrabRequest{DownloadURL: srv.URL + "/same", Protocol: downloaders.Torrent, Title: "fetch", MediaKind: KindMovies})
	if err != nil || d.ClientItemID != tor.InfoHash || d.InfoHash != tor.InfoHash {
		t.Fatalf("torrent via redirect: %+v %v", d, err)
	}
	if got := e.fakes["qbit"].added[0]; len(got.TorrentFile) == 0 || got.Category != "movies-locarr" {
		t.Fatalf("add request = %+v", got)
	}
	d, err = e.s.Grab(ctx, GrabRequest{DownloadURL: srv.URL + "/magnet", Protocol: downloaders.Torrent, Title: "redirected"})
	if err != nil || d.ClientItemID != hash {
		t.Fatalf("magnet redirect: %+v %v", d, err)
	}
	// A redirect to another host is followed for a release file, without the key.
	d, err = e.s.Grab(ctx, GrabRequest{DownloadURL: srv.URL + "/cross", Protocol: downloaders.Torrent, Title: "cross"})
	if err != nil || d.InfoHash != crossTor.InfoHash || crossQuery != "id=7" {
		t.Fatalf("cross-host redirect: %+v %v (query %q)", d, err, crossQuery)
	}
	for path, want := range map[string]string{"/big": "too large", "/html": "not a valid torrent", "/404": "HTTP 404",
		"/loop": "too many redirects", "/meta": "not allowed"} {
		_, err := e.s.Grab(ctx, GrabRequest{DownloadURL: srv.URL + path, Protocol: downloaders.Torrent, Title: path})
		if !errors.Is(err, ErrFetch) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", path, err, want)
		}
	}
	if _, err := e.s.Grab(ctx, GrabRequest{DownloadURL: srv.URL + "/html", Protocol: downloaders.Torrent, Title: "h"}); !errors.Is(err, ErrBadTorrent) {
		t.Fatalf("html is a content error: %v", err)
	}
	if _, err := e.s.Grab(ctx, GrabRequest{DownloadURL: srv.URL + "/404", Protocol: downloaders.Torrent, Title: "g"}); !errors.Is(err, ErrReleaseGone) {
		t.Fatalf("404 is a content error: %v", err)
	}
	d, err = e.s.Grab(ctx, GrabRequest{DownloadURL: srv.URL + "/nzb", Protocol: downloaders.Usenet, Title: "Some.Show.S01E01", MediaKind: KindSeries})
	if err != nil || d.Category != "tv-locarr" || d.Protocol != downloaders.Usenet {
		t.Fatalf("nzb: %+v %v", d, err)
	}
	if _, err := e.s.Grab(ctx, GrabRequest{DownloadURL: "file:///etc/passwd", Protocol: downloaders.Torrent, Title: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("file URL: %v", err)
	}
	if _, err := e.s.Grab(ctx, GrabRequest{DownloadURL: "http://169.254.169.254/latest/meta-data", Protocol: downloaders.Torrent, Title: "x"}); !errors.Is(err, ErrFetch) || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("metadata address: %v", err)
	}
}

func TestClientSelection(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.addClient(t, "A", "faketorrent", "a", func(in *ClientInput) { in.Priority = 1 })
	b := e.addClient(t, "B", "faketorrent", "b", func(in *ClientInput) { in.Priority = 1 })
	e.addClient(t, "C", "faketorrent", "c", func(in *ClientInput) { in.Priority = 5 })
	tagged := e.addClient(t, "Anime", "faketorrent", "anime", func(in *ClientInput) { in.Tags = []string{"Anime"}; in.Priority = 9 })
	e.addClient(t, "Off", "faketorrent", "off", func(in *ClientInput) { in.Enabled = false })
	seen := map[int64]int{}
	for range 4 {
		cs, err := e.s.pickClients(ctx, GrabRequest{Protocol: downloaders.Torrent})
		if err != nil {
			t.Fatal(err)
		}
		if len(cs) != 3 {
			t.Fatalf("candidates = %d", len(cs))
		}
		seen[cs[0].ID]++
	}
	if seen[a.ID] != 2 || seen[b.ID] != 2 {
		t.Fatalf("round robin = %v", seen)
	}
	cs, _ := e.s.pickClients(ctx, GrabRequest{Protocol: downloaders.Torrent, Tags: []string{"anime"}})
	if len(cs) != 4 || cs[3].ID != tagged.ID {
		t.Fatalf("tagged release candidates = %+v", cs)
	}
	if _, err := e.s.pickClients(ctx, GrabRequest{Protocol: downloaders.Usenet}); !errors.Is(err, ErrNoClient) {
		t.Fatalf("no usenet client: %v", err)
	}
}

func TestTranslate(t *testing.T) {
	ms := []Mapping{
		{Host: "box", RemotePath: "/downloads", LocalPath: "/data/downloads"},
		{Host: "box", RemotePath: "/downloads/tv", LocalPath: "/data/tv-dl"},
		{Host: "win", RemotePath: `D:\Torrents`, LocalPath: "/data/win"},
	}
	cases := map[[2]string]string{
		{"box", "/downloads/movie/a.mkv"}: "/data/downloads/movie/a.mkv",
		{"BOX", "/downloads/tv/show"}:     "/data/tv-dl/show",
		{"box", "/downloadsX/a"}:          "/downloadsX/a",
		{"other", "/downloads/a"}:         "/downloads/a",
		{"win", `d:\torrents\Film\a.mkv`}: "/data/win/Film/a.mkv",
		{"box", "/downloads/../etc"}:      "/data/downloads/etc",
	}
	for in, want := range cases {
		if got := Translate(ms, in[0], in[1]); got != want {
			t.Errorf("Translate(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestTestReportsMissingCategories(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.addClient(t, "Q", "faketorrent", "qbit", nil)
	res, err := e.s.Test(ctx, c.ID, ClientInput{Name: "Q", Implementation: "faketorrent", Host: "qbit", Port: 1234,
		CategoryMovies: "movies-locarr"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.ErrorCode != "category_missing" || len(res.MissingCategories) != 1 || !res.CanCreate {
		t.Fatalf("test = %+v", res)
	}
	if e.cfgs[len(e.cfgs)-1].Password != "secret" {
		t.Fatal("stored password not used for the test")
	}
	if _, err := e.s.CreateCategories(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	e.fakes["qbit"].testErr = downloaders.ErrAuth
	res, _ = e.s.Test(ctx, c.ID, ClientInput{Name: "Q", Implementation: "faketorrent", Host: "qbit", Port: 1234})
	if res.OK || res.ErrorCode != "auth_failed" {
		t.Fatalf("auth failure = %+v", res)
	}
}

type fakeReleases struct {
	file    []byte
	fetched []string
	err     error
}

func (f *fakeReleases) Indexer(context.Context, int64) (IndexerInfo, error) {
	r, d := 1.5, 90*time.Minute
	return IndexerInfo{Name: "Tracker", Tags: []string{"private"}, SeedRatio: &r, SeedTime: &d}, nil
}
func (f *fakeReleases) Fetch(_ context.Context, _ int64, _ string, link string) ([]byte, error) {
	f.fetched = append(f.fetched, "link:"+link)
	return f.file, f.err
}
func (f *fakeReleases) FetchToken(_ context.Context, _ int64, token string) ([]byte, error) {
	f.fetched = append(f.fetched, "token:"+token)
	return f.file, f.err
}

func TestGrabThroughTheIndexer(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addClient(t, "Private", "faketorrent", "private", func(in *ClientInput) { in.Tags = []string{"private"} })
	tor := testkit.RandomTorrent("ix", 500)
	src := &fakeReleases{file: tor.File}
	e.s.Releases = src
	idx := int64(5)
	d, err := e.s.Grab(ctx, GrabRequest{DownloadToken: "tok", IndexerID: &idx, Protocol: downloaders.Torrent, Title: "ix"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Indexer != "Tracker" || d.SeedRatio == nil || *d.SeedRatio != 1.5 || d.SeedTime == nil || *d.SeedTime != 90*time.Minute {
		t.Fatalf("indexer goals not applied: %+v", d)
	}
	add := e.fakes["private"].added[0]
	if add.SeedRatio == nil || *add.SeedRatio != 1.5 {
		t.Fatalf("seed ratio not sent to the client: %+v", add)
	}
	tor2 := testkit.RandomTorrent("ix2", 500)
	src.file = tor2.File
	if _, err := e.s.Grab(ctx, GrabRequest{DownloadURL: "https://tracker.example/dl?apikey={key}", IndexerID: &idx,
		Protocol: downloaders.Torrent, Title: "ix2"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(src.fetched, " ") != "token:tok link:https://tracker.example/dl?apikey={key}" {
		t.Fatalf("fetched = %v", src.fetched)
	}
	src.err = errors.New("grab limit")
	if _, err := e.s.Grab(ctx, GrabRequest{DownloadToken: "x", IndexerID: &idx, Protocol: downloaders.Torrent, Title: "x"}); err == nil {
		t.Fatal("indexer error swallowed")
	}
	if _, err := e.s.Grab(ctx, GrabRequest{DownloadToken: "x", Protocol: downloaders.Torrent, Title: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("token without indexer: %v", err)
	}
}
