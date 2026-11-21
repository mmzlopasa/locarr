package nzbget

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
)

// Fixtures follow the result shapes documented for NZBGet's JSON-RPC API.
const groupsJSON = `[
 {"NZBID":11,"NZBName":"Movie.2024.1080p","Category":"movies","Status":"DOWNLOADING","DestDir":"/data/usenet/intermediate/Movie.2024.1080p.#11",
  "FileSizeLo":1048576000,"FileSizeHi":0,"RemainingSizeLo":262144000,"RemainingSizeHi":0,"PausedSizeLo":0,"PausedSizeHi":0,"ActiveDownloads":4,"MaxPriority":0},
 {"NZBID":12,"NZBName":"Show.S01E01","Category":"tv","Status":"PAUSED","DestDir":"/data/usenet/intermediate/Show.S01E01.#12",
  "FileSizeLo":0,"FileSizeHi":1,"RemainingSizeLo":0,"RemainingSizeHi":1,"PausedSizeLo":0,"PausedSizeHi":1},
 {"NZBID":13,"NZBName":"Paused.Files","Category":"movies","Status":"QUEUED","DestDir":"/x",
  "FileSizeLo":1000,"RemainingSizeLo":1000,"PausedSizeLo":1000},
 {"NZBID":14,"NZBName":"Unpacking.Movie","Category":"movies","Status":"UNPACKING","DestDir":"/x","FileSizeLo":1000,"RemainingSizeLo":0,
  "PostInfoText":"Unpacking Unpacking.Movie"}
]`

const historyJSON = `[
 {"NZBID":21,"Kind":"NZB","Name":"Done.Movie","Category":"movies","Status":"SUCCESS/ALL","ParStatus":"SUCCESS","UnpackStatus":"SUCCESS",
  "MoveStatus":"SUCCESS","ScriptStatus":"NONE","DeleteStatus":"NONE","MarkStatus":"NONE","UrlStatus":"NONE",
  "DestDir":"/data/usenet/intermediate/Done.Movie.#21","FinalDir":"/data/usenet/completed/movies/Done.Movie","FileSizeLo":2097152,"FileSizeHi":0,"HistoryTime":1700000000},
 {"NZBID":22,"Kind":"NZB","Name":"Broken.Movie","Category":"movies","Status":"FAILURE/UNPACK","ParStatus":"SUCCESS","UnpackStatus":"FAILURE",
  "MoveStatus":"NONE","ScriptStatus":"NONE","DeleteStatus":"NONE","MarkStatus":"NONE","UrlStatus":"NONE","DestDir":"/data/usenet/completed/movies/Broken.Movie","FinalDir":""},
 {"NZBID":23,"Kind":"NZB","Name":"Unhealthy","Category":"tv","Status":"DELETED/HEALTH","DeleteStatus":"HEALTH","DestDir":"/x"},
 {"NZBID":24,"Kind":"NZB","Name":"Script.Warn","Category":"tv","Status":"WARNING/SCRIPT","ScriptStatus":"FAILURE","DestDir":"/y"},
 {"NZBID":25,"Kind":"DUP","Name":"Dupe.Placeholder","Category":"tv","Status":"DELETED/DUPE"}
]`

type rpcCall struct {
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

type fakeNZBGet struct {
	mu    sync.Mutex
	calls []rpcCall
}

func (f *fakeNZBGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/jsonrpc" {
		http.NotFound(w, r)
		return
	}
	if u, p, ok := r.BasicAuth(); !ok || u != "nzbget" || p != "secret-pass" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var c rpcCall
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	var result string
	switch c.Method {
	case "version":
		result = `"24.3"`
	case "status":
		result = `{"DownloadRate":1048576,"FreeDiskSpaceLo":0,"FreeDiskSpaceHi":2,"FreeDiskSpaceMB":8192}`
	case "listgroups":
		result = groupsJSON
	case "history":
		result = historyJSON
	case "append":
		result = "42"
	case "editqueue":
		result = "true"
	case "config":
		result = `[{"Name":"MainDir","Value":"/data/usenet"},{"Name":"Category1.Name","Value":"movies"},{"Name":"Category1.DestDir","Value":"/data/usenet/completed/movies"},
		 {"Name":"Category1.Unpack","Value":"yes"},{"Name":"Category2.Name","Value":"tv"},{"Name":"Category2.DestDir","Value":""}]`
	case "listfiles":
		result = `[{"ID":1,"NZBID":11,"Filename":"movie.part01.rar","FileSizeLo":1000,"FileSizeHi":0,"RemainingSizeLo":250,"RemainingSizeHi":0}]`
	default:
		_, _ = io.WriteString(w, `{"version":"1.1","error":{"name":"JSONRPCError","code":1,"message":"Invalid procedure"}}`)
		return
	}
	_, _ = io.WriteString(w, `{"version":"1.1","result":`+result+`}`)
}

func (f *fakeNZBGet) last(method string) []json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].Method == method {
			return f.calls[i].Params
		}
	}
	return nil
}

func (f *fakeNZBGet) edits() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if c.Method == "editqueue" {
			var cmd, param string
			var ids []int64
			_ = json.Unmarshal(c.Params[0], &cmd)
			_ = json.Unmarshal(c.Params[1], &param)
			_ = json.Unmarshal(c.Params[2], &ids)
			out = append(out, cmd+" "+param+" "+strconv.FormatInt(ids[0], 10))
		}
	}
	return out
}

func newTest(t *testing.T, pass string) (*Client, *fakeNZBGet) {
	t.Helper()
	fake := &fakeNZBGet{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	c, err := New(downloaders.Config{Host: host, Port: p, Username: "nzbget", Password: pass, HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return c, fake
}

func TestTest(t *testing.T) {
	c, _ := newTest(t, "secret-pass")
	st, err := c.Test(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "24.3" || st.FreeSpace == nil || *st.FreeSpace != 2<<32 {
		t.Fatalf("status = %+v", st)
	}
}

func TestAuthFailedNeverLeaksPassword(t *testing.T) {
	const pass = "wrong-password-should-not-leak"
	c, _ := newTest(t, pass)
	_, err := c.Test(context.Background())
	if !errors.Is(err, downloaders.ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	if strings.Contains(err.Error(), pass) {
		t.Fatalf("error leaks the password: %v", err)
	}
}

func TestWrongURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html>not here</html>")
	}))
	defer srv.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	c, _ := New(downloaders.Config{Host: host, Port: p, HTTP: srv.Client()})
	if _, err := c.Test(context.Background()); !errors.Is(err, downloaders.ErrWrongURL) {
		t.Fatalf("err = %v", err)
	}
}

func TestList(t *testing.T) {
	c, _ := newTest(t, "secret-pass")
	items, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]downloaders.Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if len(byID) != 8 {
		t.Fatalf("got %d items (the DUP placeholder must be skipped)", len(byID))
	}
	dl := byID["11"]
	if dl.State != downloaders.StateDownloading || dl.Progress != 0.75 || dl.Size != 1048576000 || dl.DownloadSpeed != 1048576 ||
		dl.ETA == nil || *dl.ETA != 250*time.Second {
		t.Errorf("downloading = %+v", dl)
	}
	if it := byID["12"]; it.State != downloaders.StatePaused || it.Size != 1<<32 {
		t.Errorf("paused = %+v", it)
	}
	if it := byID["13"]; it.State != downloaders.StatePaused {
		t.Errorf("all files paused = %+v", it)
	}
	if it := byID["14"]; !it.Busy || it.State != downloaders.StateDownloading || it.Message == "" {
		t.Errorf("unpacking = %+v", it)
	}
	if it := byID["21"]; it.State != downloaders.StateCompleted || it.ContentPath != "/data/usenet/completed/movies/Done.Movie" ||
		it.SavePath != "/data/usenet/completed/movies" || it.Busy {
		t.Errorf("completed = %+v", it)
	}
	if it := byID["22"]; it.State != downloaders.StateFailed || it.Message != "FAILURE/UNPACK (unpack: failure)" ||
		it.ContentPath != "/data/usenet/completed/movies/Broken.Movie" {
		t.Errorf("failed = %+v", it)
	}
	if it := byID["23"]; it.State != downloaders.StateFailed || !strings.Contains(it.Message, "health") {
		t.Errorf("health deleted = %+v", it)
	}
	if it := byID["24"]; it.State != downloaders.StateWarning || !strings.Contains(it.Message, "script") {
		t.Errorf("warning = %+v", it)
	}
	movies, _ := c.List(context.Background(), "MOVIES")
	if len(movies) != 5 {
		t.Errorf("movies = %d items, want 5", len(movies))
	}
}

func TestAdd(t *testing.T) {
	c, fake := newTest(t, "secret-pass")
	id, err := c.Add(context.Background(), downloaders.AddRequest{NZB: []byte("<nzb/>"), Name: "Some/Release", Category: "movies",
		Paused: true, Priority: downloaders.PriorityTop})
	if err != nil {
		t.Fatal(err)
	}
	if id != "42" {
		t.Fatalf("id = %q", id)
	}
	p := fake.last("append")
	var name, content, cat, dupeMode string
	var prio int
	var top, paused bool
	_ = json.Unmarshal(p[0], &name)
	_ = json.Unmarshal(p[1], &content)
	_ = json.Unmarshal(p[2], &cat)
	_ = json.Unmarshal(p[3], &prio)
	_ = json.Unmarshal(p[4], &top)
	_ = json.Unmarshal(p[5], &paused)
	_ = json.Unmarshal(p[8], &dupeMode)
	raw, _ := base64.StdEncoding.DecodeString(content)
	if len(p) != 10 || name != "Some_Release.nzb" || string(raw) != "<nzb/>" || cat != "movies" || prio != 100 || !top || !paused || dupeMode != "SCORE" {
		t.Errorf("append params = %s", p)
	}
	if _, err := c.Add(context.Background(), downloaders.AddRequest{NZBURL: "http://indexer/get/1", Priority: downloaders.PriorityLast}); err != nil {
		t.Fatal(err)
	}
	p = fake.last("append")
	_ = json.Unmarshal(p[1], &content)
	_ = json.Unmarshal(p[3], &prio)
	if content != "http://indexer/get/1" || prio != -100 {
		t.Errorf("url append = %s", p)
	}
	if _, err := c.Add(context.Background(), downloaders.AddRequest{NZBURL: "file:///etc/passwd"}); !errors.Is(err, downloaders.ErrInvalidRequest) {
		t.Errorf("file URL accepted: %v", err)
	}
}

func TestActions(t *testing.T) {
	c, fake := newTest(t, "secret-pass")
	ctx := context.Background()
	for _, step := range []func() error{
		func() error { return c.Pause(ctx, "11") },
		func() error { return c.Resume(ctx, "12") },
		func() error { return c.SetPriority(ctx, "11", downloaders.PriorityTop) },
		func() error { return c.SetPriority(ctx, "11", downloaders.PriorityLast) },
		func() error { return c.SetPriority(ctx, "11", downloaders.PriorityNormal) },
		func() error { return c.Remove(ctx, "11", true) },
		func() error { return c.Remove(ctx, "21", true) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"GroupPause  11", "GroupResume  12", "GroupSetPriority 100 11", "GroupMoveTop  11",
		"GroupSetPriority -100 11", "GroupMoveBottom  11", "GroupSetPriority 0 11", "GroupFinalDelete  11", "HistoryFinalDelete  21"}
	if got := fake.edits(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("edits =\n%v\nwant\n%v", got, want)
	}
	if err := c.Pause(ctx, "21"); !errors.Is(err, downloaders.ErrItemNotFound) {
		t.Errorf("pausing history: %v", err)
	}
	if err := c.Remove(ctx, "999", false); !errors.Is(err, downloaders.ErrItemNotFound) {
		t.Errorf("unknown remove: %v", err)
	}
	if err := c.Remove(ctx, "abc", false); !errors.Is(err, downloaders.ErrItemNotFound) {
		t.Errorf("bad id: %v", err)
	}
	files, err := c.Files(ctx, "11")
	if err != nil || len(files) != 1 || files[0].Progress != 0.75 {
		t.Errorf("files = %+v, %v", files, err)
	}
}

func TestCategories(t *testing.T) {
	c, _ := newTest(t, "secret-pass")
	cats, err := c.Categories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 2 || cats[0] != (downloaders.Category{Name: "movies", SavePath: "/data/usenet/completed/movies"}) || cats[1].Name != "tv" {
		t.Fatalf("cats = %+v", cats)
	}
	if err := c.EnsureCategory(context.Background(), "TV", ""); err != nil {
		t.Errorf("existing category: %v", err)
	}
	if err := c.EnsureCategory(context.Background(), "music", ""); !errors.Is(err, downloaders.ErrUnsupported) {
		t.Errorf("missing category: %v", err)
	}
}

func TestRPCError(t *testing.T) {
	c, _ := newTest(t, "secret-pass")
	err := c.call(context.Background(), "nosuchmethod", nil, nil)
	if !errors.Is(err, downloaders.ErrClient) || !strings.Contains(err.Error(), "Invalid procedure") {
		t.Fatalf("err = %v", err)
	}
}
