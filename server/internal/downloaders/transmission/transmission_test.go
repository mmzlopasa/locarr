package transmission

import (
	"context"
	"encoding/json"
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

// fake is a Transmission RPC endpoint as the specification describes it.
type fake struct {
	mu        sync.Mutex
	handshake int
	calls     []map[string]any
	torrents  string
}

const listJSON = `[
 {"hashString":"ABCDEF0123456789ABCDEF0123456789ABCDEF01","name":"Movie.2024","status":6,"percentDone":1,"sizeWhenDone":1000,
  "leftUntilDone":0,"downloadedEver":1000,"uploadedEver":2000,"rateUpload":5,"eta":-1,"uploadRatio":2,"secondsSeeding":60,
  "downloadDir":"/downloads/complete","error":0,"labels":["movies"],"addedDate":1700000000,"peersSendingToUs":0},
 {"hashString":"1111111111111111111111111111111111111111","name":"Busy","status":4,"percentDone":0.5,"sizeWhenDone":10,
  "leftUntilDone":5,"rateDownload":7,"eta":42,"uploadRatio":-1,"downloadDir":"/d","error":2,"errorString":"tracker down",
  "labels":["movies"],"addedDate":1,"metadataPercentComplete":1},
 {"hashString":"2222222222222222222222222222222222222222","name":"Broken","status":0,"percentDone":0.1,"sizeWhenDone":10,
  "leftUntilDone":9,"downloadDir":"/d","error":3,"errorString":"No data found","labels":["tv"],"addedDate":1},
 {"hashString":"3333333333333333333333333333333333333333","name":"Finished","status":0,"percentDone":1,"sizeWhenDone":10,
  "leftUntilDone":0,"isFinished":true,"downloadDir":"/d","labels":["movies"],"addedDate":1,
  "files":[{"name":"Finished/a.mkv","length":10,"bytesCompleted":10}]}
]`

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path != "/transmission/rpc" {
		http.NotFound(w, r)
		return
	}
	if u, p, _ := r.BasicAuth(); u != "user" || p != "pw" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Header.Get(sessionHeader) != "sess-1" {
		f.handshake++
		w.Header().Set(sessionHeader, "sess-1")
		w.WriteHeader(http.StatusConflict)
		return
	}
	b, _ := io.ReadAll(r.Body)
	var req struct {
		Method    string         `json:"method"`
		Arguments map[string]any `json:"arguments"`
	}
	_ = json.Unmarshal(b, &req)
	f.calls = append(f.calls, map[string]any{"method": req.Method, "args": req.Arguments})
	reply := func(args string) { _, _ = io.WriteString(w, `{"result":"success","arguments":`+args+`}`) }
	switch req.Method {
	case "session-get":
		reply(`{"version":"4.0.6 (38c164933e)","rpc-version":17,"download-dir":"/downloads"}`)
	case "free-space":
		reply(`{"path":"/downloads","size-bytes":5000}`)
	case "torrent-get":
		list := f.torrents
		if ids, ok := req.Arguments["ids"].([]any); ok {
			var keep []json.RawMessage
			var all []json.RawMessage
			_ = json.Unmarshal([]byte(list), &all)
			for _, t := range all {
				for _, id := range ids {
					if strings.Contains(strings.ToLower(string(t)), id.(string)) {
						keep = append(keep, t)
					}
				}
			}
			out, _ := json.Marshal(keep)
			list = string(out)
			if keep == nil {
				list = "[]"
			}
		}
		reply(`{"torrents":` + list + `}`)
	case "torrent-add":
		if _, ok := req.Arguments["metainfo"]; ok {
			reply(`{"torrent-added":{"hashString":"ABCDEF0123456789ABCDEF0123456789ABCDEF01","id":1,"name":"x"}}`)
			return
		}
		reply(`{"torrent-duplicate":{"hashString":"1111111111111111111111111111111111111111","id":2,"name":"y"}}`)
	case "torrent-set", "torrent-remove", "torrent-stop", "torrent-start", "queue-move-top", "queue-move-bottom":
		reply(`{}`)
	default:
		_, _ = io.WriteString(w, `{"result":"method name not recognized","arguments":{}}`)
	}
}

func client(t *testing.T, f http.Handler, user, pass, base string) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	c, err := New(downloaders.Config{Host: u.Hostname(), Port: port, URLBase: base, Username: user, Password: pass})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestHandshakeAndTest(t *testing.T) {
	f := &fake{torrents: listJSON}
	c := client(t, f, "user", "pw", "")
	st, err := c.Test(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(st.Version, "4.0.6") || st.FreeSpace == nil || *st.FreeSpace != 5000 {
		t.Fatalf("status %+v", st)
	}
	if f.handshake != 1 {
		t.Fatalf("handshakes = %d, want 1 (the ID is reused)", f.handshake)
	}
}

func TestErrors(t *testing.T) {
	f := &fake{torrents: listJSON}
	if _, err := client(t, f, "user", "bad", "").Test(context.Background()); !errors.Is(err, downloaders.ErrAuth) {
		t.Fatalf("bad password: %v", err)
	}
	if _, err := client(t, f, "user", "pw", "/nope").Test(context.Background()); !errors.Is(err, downloaders.ErrWrongURL) {
		t.Fatalf("wrong base: %v", err)
	}
	notRPC := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html>hello</html>") })
	if _, err := client(t, notRPC, "", "", "").Test(context.Background()); !errors.Is(err, downloaders.ErrWrongURL) {
		t.Fatalf("not RPC: %v", err)
	}
}

func TestListMapping(t *testing.T) {
	c := client(t, &fake{torrents: listJSON}, "user", "pw", "/transmission/")
	items, err := c.List(context.Background(), "movies")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want the 3 labelled movies", len(items))
	}
	a := items[0]
	if a.ID != "abcdef0123456789abcdef0123456789abcdef01" || a.State != downloaders.StateSeeding || a.Category != "movies" ||
		a.ContentPath != "/downloads/complete/Movie.2024" || a.SavePath != "/downloads/complete" || a.Ratio != 2 || a.ETA != nil {
		t.Fatalf("seeding: %+v", a)
	}
	b := items[1]
	if b.State != downloaders.StateWarning || b.Message != "tracker down" || b.Ratio != 0 || b.ETA != nil {
		t.Fatalf("tracker error: %+v", b)
	}
	d := items[2]
	if d.State != downloaders.StateCompleted || !d.SeedGoalReached {
		t.Fatalf("finished: %+v", d)
	}
	all, _ := c.List(context.Background(), "")
	if all[2].State != downloaders.StateFailed || all[2].Message != "No data found" {
		t.Fatalf("local error: %+v", all[2])
	}
	files, err := c.Files(context.Background(), "3333333333333333333333333333333333333333")
	if err != nil || len(files) != 1 || files[0].Progress != 1 {
		t.Fatalf("files %+v %v", files, err)
	}
	if _, err := c.Files(context.Background(), "9999999999999999999999999999999999999999"); !errors.Is(err, downloaders.ErrItemNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestAddAndActions(t *testing.T) {
	f := &fake{torrents: listJSON}
	c := client(t, f, "user", "pw", "")
	ctx := context.Background()
	tor := testkit.RandomTorrent("x", 100)
	ratio := 1.0
	id, err := c.Add(ctx, downloaders.AddRequest{TorrentFile: tor.File, Category: "movies", Paused: true, SeedRatio: &ratio, SavePath: "/dl"})
	if err != nil || id != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Fatalf("add file: %s %v", id, err)
	}
	add := f.calls[len(f.calls)-2]["args"].(map[string]any)
	if add["paused"] != true || add["download-dir"] != "/dl" || add["labels"].([]any)[0] != "movies" {
		t.Fatalf("torrent-add args %+v", add)
	}
	set := f.calls[len(f.calls)-1]
	if set["method"] != "torrent-set" || set["args"].(map[string]any)["seedRatioMode"].(float64) != 1 {
		t.Fatalf("torrent-set %+v", set)
	}
	link, _ := testkit.RandomMagnet("m")
	id, err = c.Add(ctx, downloaders.AddRequest{Magnet: link})
	if err != nil || id != "1111111111111111111111111111111111111111" {
		t.Fatalf("duplicate: %s %v", id, err)
	}
	h := "abcdef0123456789abcdef0123456789abcdef01"
	for _, step := range []struct {
		do   func() error
		want string
	}{
		{func() error { return c.Pause(ctx, h) }, "torrent-stop"},
		{func() error { return c.Resume(ctx, h) }, "torrent-start"},
		{func() error { return c.SetPriority(ctx, h, downloaders.PriorityTop) }, "queue-move-top"},
		{func() error { return c.SetPriority(ctx, h, downloaders.PriorityLast) }, "queue-move-bottom"},
		{func() error { return c.Remove(ctx, h, true) }, "torrent-remove"},
	} {
		if err := step.do(); err != nil {
			t.Fatal(err)
		}
		if got := f.calls[len(f.calls)-1]["method"]; got != step.want {
			t.Fatalf("called %v, want %s", got, step.want)
		}
	}
	if f.calls[len(f.calls)-1]["args"].(map[string]any)["delete-local-data"] != true {
		t.Fatal("delete-local-data not sent")
	}
	if err := c.Pause(ctx, "9999999999999999999999999999999999999999"); !errors.Is(err, downloaders.ErrItemNotFound) {
		t.Fatalf("pause unknown: %v", err)
	}
	cats, err := c.Categories(ctx)
	if err != nil || len(cats) != 2 || cats[0].Name != "movies" {
		t.Fatalf("categories %+v %v", cats, err)
	}
}
