package qbittorrent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/downloaders/testkit"
)

// fake is a qBittorrent WebUI as its API documentation describes it.
type fake struct {
	mu         sync.Mutex
	apiVer     string
	sid        string
	logins     int
	calls      []string
	forms      map[string]url.Values
	queueing   bool
	categories map[string]string
	torrents   string // JSON for torrents/info
	expireNext bool
}

func newFake(apiVer string) *fake {
	return &fake{apiVer: apiVer, forms: map[string]url.Values{}, categories: map[string]string{"movies": "/data/movies"},
		torrents: `[{"hash":"ABCDEF0123456789ABCDEF0123456789ABCDEF01","name":"Some.Movie.2024.1080p","state":"stalledUP",
		"category":"movies","tags":"locarr, x","progress":1,"size":1000,"downloaded":1000,"uploaded":500,"dlspeed":0,"upspeed":10,
		"eta":8640000,"ratio":0.5,"seeding_time":3600,"save_path":"/data/movies","content_path":"/data/movies/Some.Movie.2024.1080p",
		"added_on":1700000000,"num_seeds":3,"max_ratio":-1,"max_seeding_time":-1},
		{"hash":"1111111111111111111111111111111111111111","name":"Downloading","state":"downloading","category":"movies","tags":"",
		"progress":0.25,"size":4000,"downloaded":1000,"dlspeed":100,"eta":30,"save_path":"/d","content_path":"/d/x","added_on":1700000100,"num_seeds":1},
		{"hash":"2222222222222222222222222222222222222222","name":"Done","state":"pausedUP","category":"movies","progress":1,"size":1,
		"ratio":2.1,"max_ratio":2,"max_seeding_time":-1,"seeding_time":10,"save_path":"/d","content_path":"/d/done","added_on":1}]`}
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/qb/api/v2/")
	if path == r.URL.Path {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Referer") == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		_ = r.ParseMultipartForm(1 << 20)
	} else {
		_ = r.ParseForm()
	}
	f.calls = append(f.calls, path)
	f.forms[path] = r.Form
	if path == "auth/login" {
		f.logins++
		if r.Form.Get("username") != "admin" || r.Form.Get("password") != "secret" {
			_, _ = io.WriteString(w, "Fails.")
			return
		}
		f.sid = "sid" + strconv.Itoa(f.logins)
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: f.sid, Path: "/"})
		_, _ = io.WriteString(w, "Ok.")
		return
	}
	if c, err := r.Cookie("SID"); err != nil || c.Value != f.sid || f.expireNext {
		f.expireNext = false
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "Forbidden")
		return
	}
	switch path {
	case "app/version":
		_, _ = io.WriteString(w, "v5.0.2")
	case "app/webapiVersion":
		_, _ = io.WriteString(w, f.apiVer)
	case "app/defaultSavePath":
		_, _ = io.WriteString(w, "/downloads")
	case "sync/maindata":
		_, _ = io.WriteString(w, `{"rid":1,"server_state":{"free_space_on_disk":123456789}}`)
	case "torrents/info":
		_, _ = io.WriteString(w, f.torrents)
		if h := r.Form.Get("hashes"); h != "" && !strings.Contains(strings.ToLower(f.torrents), h) {
			// A real client filters; the test list contains everything, so mimic "not found".
			w.Header().Set("X-Filtered", "1")
		}
	case "torrents/files":
		_, _ = io.WriteString(w, `[{"index":0,"name":"Some.Movie.2024.1080p/movie.mkv","size":1000,"progress":1,"priority":1}]`)
	case "torrents/add":
		if len(r.MultipartForm.File["torrents"]) == 0 && r.Form.Get("urls") == "" {
			_, _ = io.WriteString(w, "Fails.")
			return
		}
		_, _ = io.WriteString(w, "Ok.")
	case "torrents/categories":
		var parts []string
		for k, v := range f.categories {
			parts = append(parts, `"`+k+`":{"name":"`+k+`","savePath":"`+v+`"}`)
		}
		_, _ = io.WriteString(w, "{"+strings.Join(parts, ",")+"}")
	case "torrents/createCategory":
		if _, ok := f.categories[r.Form.Get("category")]; ok {
			w.WriteHeader(http.StatusConflict)
			return
		}
		f.categories[r.Form.Get("category")] = r.Form.Get("savePath")
	case "torrents/topPrio", "torrents/bottomPrio":
		if !f.queueing {
			w.WriteHeader(http.StatusConflict)
		}
	case "torrents/delete", "torrents/stop", "torrents/start", "torrents/pause", "torrents/resume":
	default:
		http.NotFound(w, r)
	}
}

func newClient(t *testing.T, f *fake, user, pass string) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	c, err := New(downloaders.Config{Host: u.Hostname(), Port: port, URLBase: "/qb/", Username: user, Password: pass})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLoginAndTest(t *testing.T) {
	f := newFake("2.11.2")
	c := newClient(t, f, "admin", "secret")
	st, err := c.Test(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "v5.0.2" || st.FreeSpace == nil || *st.FreeSpace != 123456789 || st.SavePath != "/downloads" {
		t.Fatalf("status = %+v", st)
	}
	if f.logins != 1 {
		t.Fatalf("logins = %d", f.logins)
	}
}

func TestWrongPassword(t *testing.T) {
	c := newClient(t, newFake("2.11.2"), "admin", "nope")
	_, err := c.Test(context.Background())
	if !errors.Is(err, downloaders.ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	if strings.Contains(err.Error(), "nope") {
		t.Fatal("error leaks the password")
	}
}

func TestWrongURLBase(t *testing.T) {
	f := newFake("2.11.2")
	srv := httptest.NewServer(f)
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	c, _ := New(downloaders.Config{Host: u.Hostname(), Port: port, URLBase: "/elsewhere", Username: "admin", Password: "secret"})
	if _, err := c.Test(context.Background()); !errors.Is(err, downloaders.ErrWrongURL) {
		t.Fatalf("err = %v, want ErrWrongURL", err)
	}
}

func TestSessionExpiryRelogs(t *testing.T) {
	f := newFake("2.11.2")
	c := newClient(t, f, "admin", "secret")
	ctx := context.Background()
	if _, err := c.List(ctx, ""); err != nil {
		t.Fatal(err)
	}
	f.expireNext = true
	if _, err := c.List(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if f.logins != 2 {
		t.Fatalf("logins = %d, want 2", f.logins)
	}
}

func TestListMapping(t *testing.T) {
	c := newClient(t, newFake("2.11.2"), "admin", "secret")
	items, err := c.List(context.Background(), "movies")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d", len(items))
	}
	a := items[0]
	if a.ID != "abcdef0123456789abcdef0123456789abcdef01" || a.State != downloaders.StateSeeding || a.RawState != "stalledUP" ||
		a.Progress != 1 || a.ETA != nil || a.ContentPath != "/data/movies/Some.Movie.2024.1080p" || a.Seeds != 3 ||
		len(a.Tags) != 2 || a.Tags[0] != "locarr" || a.SeedingTime.Seconds() != 3600 || a.Category != "movies" {
		t.Fatalf("seeding item = %+v", a)
	}
	b := items[1]
	if b.State != downloaders.StateDownloading || b.ETA == nil || b.ETA.Seconds() != 30 || b.DownloadSpeed != 100 {
		t.Fatalf("downloading item = %+v", b)
	}
	d := items[2]
	if d.State != downloaders.StateCompleted || !d.SeedGoalReached {
		t.Fatalf("finished item = %+v", d)
	}
}

func TestStateMap(t *testing.T) {
	cases := map[string]downloaders.State{
		"error": downloaders.StateFailed, "missingFiles": downloaders.StateFailed, "uploading": downloaders.StateSeeding,
		"pausedUP": downloaders.StateCompleted, "stoppedUP": downloaders.StateCompleted, "queuedUP": downloaders.StateSeeding,
		"stalledUP": downloaders.StateSeeding, "checkingUP": downloaders.StateSeeding, "forcedUP": downloaders.StateSeeding,
		"allocating": downloaders.StateDownloading, "downloading": downloaders.StateDownloading, "metaDL": downloaders.StateDownloading,
		"pausedDL": downloaders.StatePaused, "stoppedDL": downloaders.StatePaused, "queuedDL": downloaders.StateQueued,
		"stalledDL": downloaders.StateDownloading, "checkingDL": downloaders.StateQueued, "forcedDL": downloaders.StateDownloading,
		"checkingResumeData": downloaders.StateQueued, "moving": downloaders.StateQueued, "unknown": downloaders.StateWarning,
	}
	for raw, want := range cases {
		if got, _ := mapState(raw); got != want {
			t.Errorf("%s: %s, want %s", raw, got, want)
		}
	}
	if _, busy := mapState("checkingUP"); !busy {
		t.Error("checkingUP should be busy")
	}
}

func TestAddTorrentFileAndMagnet(t *testing.T) {
	f := newFake("2.11.2")
	c := newClient(t, f, "admin", "secret")
	ctx := context.Background()
	tor := testkit.RandomTorrent("x", 1000)
	ratio := 1.5
	id, err := c.Add(ctx, downloaders.AddRequest{TorrentFile: tor.File, Category: "movies", Paused: true, SavePath: "/data/x",
		SeedRatio: &ratio, Tags: []string{"a", "b"}, Sequential: true, FirstLastPiece: true})
	if err != nil {
		t.Fatal(err)
	}
	if id != tor.InfoHash {
		t.Fatalf("id = %s, want %s", id, tor.InfoHash)
	}
	form := f.forms["torrents/add"]
	for k, want := range map[string]string{"category": "movies", "paused": "true", "stopped": "true", "savepath": "/data/x",
		"ratioLimit": "1.50", "tags": "a,b", "sequentialDownload": "true", "firstLastPiecePrio": "true"} {
		if got := form.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	link, hash := testkit.RandomMagnet("m")
	id, err = c.Add(ctx, downloaders.AddRequest{Magnet: link, Priority: downloaders.PriorityTop})
	if err != nil || id != hash {
		t.Fatalf("magnet: %s %v", id, err)
	}
	if got := f.forms["torrents/add"].Get("urls"); got != link {
		t.Errorf("urls = %q", got)
	}
	// Queueing off: 409 on topPrio is not an error.
	if err := c.SetPriority(ctx, hash, downloaders.PriorityTop); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Add(ctx, downloaders.AddRequest{NZB: []byte("x")}); !errors.Is(err, downloaders.ErrInvalidRequest) {
		t.Fatalf("NZB on a torrent client: %v", err)
	}
}

func TestPauseResumeByAPIVersion(t *testing.T) {
	for _, tc := range []struct{ ver, pause, resume string }{
		{"2.11.2", "torrents/stop", "torrents/start"},
		{"2.9.3", "torrents/pause", "torrents/resume"},
	} {
		f := newFake(tc.ver)
		c := newClient(t, f, "admin", "secret")
		ctx := context.Background()
		id := "abcdef0123456789abcdef0123456789abcdef01"
		if err := c.Pause(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := c.Resume(ctx, id); err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(f.calls, " ")
		if !strings.Contains(joined, tc.pause) || !strings.Contains(joined, tc.resume) {
			t.Errorf("%s: calls %s", tc.ver, joined)
		}
		if err := c.Remove(ctx, id, true); err != nil {
			t.Fatal(err)
		}
		if got := f.forms["torrents/delete"].Get("deleteFiles"); got != "true" {
			t.Errorf("deleteFiles = %q", got)
		}
	}
}

func TestCategoriesAndFiles(t *testing.T) {
	f := newFake("2.11.2")
	c := newClient(t, f, "admin", "secret")
	ctx := context.Background()
	if err := c.EnsureCategory(ctx, "movies", ""); err != nil {
		t.Fatal(err)
	}
	if err := c.EnsureCategory(ctx, "tv", "/data/tv"); err != nil {
		t.Fatal(err)
	}
	cats, err := c.Categories(ctx)
	if err != nil || len(cats) != 2 {
		t.Fatalf("cats = %+v %v", cats, err)
	}
	files, err := c.Files(ctx, "abcdef0123456789abcdef0123456789abcdef01")
	if err != nil || len(files) != 1 || files[0].Size != 1000 {
		t.Fatalf("files = %+v %v", files, err)
	}
}

func TestAtLeast(t *testing.T) {
	if !atLeast("2.11.0", 2, 11) || atLeast("2.9.9", 2, 11) || !atLeast("3.0", 2, 11) || atLeast("2.10", 2, 11) {
		t.Fatal("atLeast wrong")
	}
}
