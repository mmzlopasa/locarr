package deluge

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

// fake is a Deluge Web UI JSON-RPC endpoint as its documentation describes it.
type fake struct {
	mu        sync.Mutex
	session   string
	logins    int
	connected bool
	plugins   []string
	labels    []string
	calls     []string
	params    map[string][]any
	expire    bool
}

const torrents = `{
 "abcdef0123456789abcdef0123456789abcdef01":{"name":"Movie.2024","state":"Seeding","progress":100,"total_wanted":1000,
  "total_done":1000,"total_uploaded":10,"upload_payload_rate":3,"eta":0,"ratio":0.01,"seeding_time":5,
  "save_path":"/downloads","download_location":"/downloads","label":"movies","time_added":1700000000.5,"message":"OK",
  "is_finished":true,"num_seeds":0},
 "1111111111111111111111111111111111111111":{"name":"Part","state":"Downloading","progress":50,"total_wanted":10,
  "total_done":5,"download_payload_rate":2.5,"eta":30,"ratio":-1,"save_path":"/d","label":"movies","time_added":1,"message":"OK"},
 "2222222222222222222222222222222222222222":{"name":"Err","state":"Error","progress":3,"save_path":"/d","label":"tv",
  "time_added":2,"message":"Missing or invalid torrent data!"},
 "3333333333333333333333333333333333333333":{"name":"Done","state":"Paused","progress":100,"save_path":"/d","label":"movies",
  "time_added":3,"is_finished":true,"stop_at_ratio":true,"stop_ratio":1,"ratio":1.2}
}`

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path != "/json" {
		http.NotFound(w, r)
		return
	}
	b, _ := io.ReadAll(r.Body)
	var req struct {
		Method string `json:"method"`
		Params []any  `json:"params"`
		ID     int    `json:"id"`
	}
	_ = json.Unmarshal(b, &req)
	reply := func(result string) {
		_, _ = io.WriteString(w, `{"id":`+strconv.Itoa(req.ID)+`,"result":`+result+`,"error":null}`)
	}
	f.calls = append(f.calls, req.Method)
	f.params[req.Method] = req.Params
	if req.Method == "auth.login" {
		f.logins++
		if req.Params[0] != "deluge" {
			reply("false")
			return
		}
		f.session = "s" + strconv.Itoa(f.logins)
		http.SetCookie(w, &http.Cookie{Name: "_session_id", Value: f.session, Path: "/"})
		reply("true")
		return
	}
	if c, err := r.Cookie("_session_id"); err != nil || c.Value != f.session || f.expire {
		f.expire = false
		_, _ = io.WriteString(w, `{"id":`+strconv.Itoa(req.ID)+`,"result":null,"error":{"message":"Not authenticated","code":1}}`)
		return
	}
	switch req.Method {
	case "web.connected":
		reply(strconv.FormatBool(f.connected))
	case "web.get_hosts":
		reply(`[["host1","127.0.0.1",58846,"Online"]]`)
	case "web.connect":
		f.connected = true
		reply(`[]`)
	case "daemon.info":
		reply(`"2.1.1"`)
	case "core.get_free_space":
		reply(`4096`)
	case "core.get_enabled_plugins":
		out, _ := json.Marshal(f.plugins)
		reply(string(out))
	case "label.get_labels":
		out, _ := json.Marshal(f.labels)
		reply(string(out))
	case "label.add":
		f.labels = append(f.labels, req.Params[0].(string))
		reply("null")
	case "core.get_torrents_status":
		filter := req.Params[0].(map[string]any)
		if ids, ok := filter["id"].([]any); ok {
			var all map[string]json.RawMessage
			_ = json.Unmarshal([]byte(torrents), &all)
			keep := map[string]json.RawMessage{}
			for _, id := range ids {
				if v, ok := all[id.(string)]; ok {
					keep[id.(string)] = v
				}
			}
			out, _ := json.Marshal(keep)
			reply(string(out))
			return
		}
		reply(torrents)
	case "core.add_torrent_magnet", "core.add_torrent_file":
		reply(`"ABCDEF0123456789ABCDEF0123456789ABCDEF01"`)
	case "label.set_torrent", "core.remove_torrent", "core.pause_torrent", "core.resume_torrent", "core.queue_top", "core.queue_bottom":
		reply("null")
	default:
		_, _ = io.WriteString(w, `{"id":1,"result":null,"error":{"message":"Unknown method","code":2}}`)
	}
}

func newClient(t *testing.T, f *fake, pass string) *Client {
	t.Helper()
	f.params = map[string][]any{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	c, err := New(downloaders.Config{Host: u.Hostname(), Port: port, Password: pass})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLoginConnectsToDaemon(t *testing.T) {
	f := &fake{plugins: []string{"Label"}}
	c := newClient(t, f, "deluge")
	st, err := c.Test(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "2.1.1" || st.FreeSpace == nil || *st.FreeSpace != 4096 {
		t.Fatalf("status %+v", st)
	}
	if !f.connected || !strings.Contains(strings.Join(f.calls, " "), "web.connect ") {
		t.Fatalf("not connected: %v", f.calls)
	}
}

func TestWrongPassword(t *testing.T) {
	_, err := newClient(t, &fake{}, "nope").Test(context.Background())
	if !errors.Is(err, downloaders.ErrAuth) {
		t.Fatalf("err = %v", err)
	}
}

func TestReloginOnExpiry(t *testing.T) {
	f := &fake{connected: true}
	c := newClient(t, f, "deluge")
	ctx := context.Background()
	if _, err := c.List(ctx, ""); err != nil {
		t.Fatal(err)
	}
	f.expire = true
	if _, err := c.List(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if f.logins != 2 {
		t.Fatalf("logins = %d", f.logins)
	}
}

func TestListMapping(t *testing.T) {
	c := newClient(t, &fake{connected: true}, "deluge")
	items, err := c.List(context.Background(), "movies")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items %d", len(items))
	}
	byID := map[string]downloaders.Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	a := byID["abcdef0123456789abcdef0123456789abcdef01"]
	if a.State != downloaders.StateSeeding || a.Progress != 1 || a.ContentPath != "/downloads/Movie.2024" || a.Message != "" || a.ETA != nil {
		t.Fatalf("seeding %+v", a)
	}
	b := byID["1111111111111111111111111111111111111111"]
	if b.State != downloaders.StateDownloading || b.ETA == nil || b.Ratio != 0 || b.DownloadSpeed != 2 {
		t.Fatalf("downloading %+v", b)
	}
	d := byID["3333333333333333333333333333333333333333"]
	if d.State != downloaders.StateCompleted || !d.SeedGoalReached {
		t.Fatalf("done %+v", d)
	}
	all, _ := c.List(context.Background(), "")
	for _, it := range all {
		if it.ID == "2222222222222222222222222222222222222222" && (it.State != downloaders.StateFailed || it.Message == "") {
			t.Fatalf("error item %+v", it)
		}
	}
}

func TestAddWithLabel(t *testing.T) {
	f := &fake{connected: true, plugins: []string{"Label"}}
	c := newClient(t, f, "deluge")
	ctx := context.Background()
	tor := testkit.RandomTorrent("x", 100)
	ratio := 2.0
	id, err := c.Add(ctx, downloaders.AddRequest{TorrentFile: tor.File, Category: "Movies", Paused: true, SavePath: "/dl", SeedRatio: &ratio})
	if err != nil || id != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Fatalf("add: %s %v", id, err)
	}
	opts := f.params["core.add_torrent_file"][2].(map[string]any)
	if opts["add_paused"] != true || opts["download_location"] != "/dl" || opts["stop_ratio"].(float64) != 2 {
		t.Fatalf("options %+v", opts)
	}
	if got := f.params["label.set_torrent"]; got[1] != "movies" {
		t.Fatalf("label %v", got)
	}
	if len(f.labels) != 1 || f.labels[0] != "movies" {
		t.Fatalf("labels %v", f.labels)
	}
	link, _ := testkit.RandomMagnet("m")
	if _, err := c.Add(ctx, downloaders.AddRequest{Magnet: link}); err != nil {
		t.Fatal(err)
	}
}

func TestActionsAndCategories(t *testing.T) {
	f := &fake{connected: true}
	c := newClient(t, f, "deluge")
	ctx := context.Background()
	h := "abcdef0123456789abcdef0123456789abcdef01"
	if err := c.EnsureCategory(ctx, "movies", ""); !errors.Is(err, downloaders.ErrUnsupported) {
		t.Fatalf("no plugin: %v", err)
	}
	if err := c.Pause(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err := c.Resume(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err := c.SetPriority(ctx, h, downloaders.PriorityTop); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, h, true); err != nil {
		t.Fatal(err)
	}
	if p := f.params["core.remove_torrent"]; p[0] != h || p[1] != true {
		t.Fatalf("remove params %v", p)
	}
	if err := c.Pause(ctx, "9999999999999999999999999999999999999999"); !errors.Is(err, downloaders.ErrItemNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	f.plugins = []string{"Label"}
	if err := c.EnsureCategory(ctx, "TV", ""); err != nil {
		t.Fatal(err)
	}
	cats, err := c.Categories(ctx)
	if err != nil || len(cats) != 1 || cats[0].Name != "tv" {
		t.Fatalf("cats %+v %v", cats, err)
	}
}
