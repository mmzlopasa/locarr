package aria2

import (
	"context"
	"encoding/base64"
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

const secret = "s3cret"

type rpcCall struct {
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

type fake struct {
	mu     sync.Mutex
	calls  []rpcCall
	answer func(c rpcCall) (result any, errMsg string)
}

func (f *fake) start(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jsonrpc" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var c rpcCall
		if err := json.Unmarshal(body, &c); err != nil {
			t.Errorf("bad request %s", body)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var token string
		if len(c.Params) > 0 {
			_ = json.Unmarshal(c.Params[0], &token)
		}
		// The secret comes first, as "token:<secret>"; the manual's answer to a bad one is "Unauthorized".
		if token != "token:"+secret {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"id":"1","jsonrpc":"2.0","error":{"code":1,"message":"Unauthorized"}}`)
			return
		}
		c.Params = c.Params[1:]
		f.mu.Lock()
		f.calls = append(f.calls, c)
		f.mu.Unlock()
		res, msg := f.answer(c)
		if msg != "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "1", "jsonrpc": "2.0", "error": map[string]any{"code": 1, "message": msg}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "1", "jsonrpc": "2.0", "result": res})
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	c, err := New(downloaders.Config{Host: u.Hostname(), Port: port, APIKey: secret, HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f *fake) find(method string) []rpcCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []rpcCall
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

const (
	hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hashC = "cccccccccccccccccccccccccccccccccccccccc"
)

// Fixtures follow the tellStatus fields of the aria2 manual (numbers are strings).
var (
	seeding = map[string]any{"gid": "2089b05ecca3d829", "status": "active", "totalLength": "1000", "completedLength": "1000",
		"uploadLength": "500", "downloadSpeed": "0", "uploadSpeed": "20", "infoHash": strings.ToUpper(hashA), "dir": "/downloads/movies",
		"numSeeders": "0", "seeder": "true",
		"files":      []any{map[string]any{"path": "/downloads/movies/Movie.mkv", "length": "1000", "completedLength": "1000", "selected": "true"}},
		"bittorrent": map[string]any{"info": map[string]any{"name": "Movie.mkv"}, "mode": "single"}}
	// A magnet's metadata download that was followed by the real one.
	metaDone = map[string]any{"gid": "0000000000000001", "status": "complete", "totalLength": "0", "completedLength": "0",
		"infoHash": hashB, "dir": "/downloads/tv", "followedBy": []any{"0000000000000002"},
		"files": []any{map[string]any{"path": "[METADATA]" + hashB, "length": "0", "completedLength": "0"}}}
	downloading = map[string]any{"gid": "0000000000000002", "status": "waiting", "totalLength": "4000", "completedLength": "1000",
		"downloadSpeed": "0", "infoHash": hashB, "dir": "/downloads/tv",
		"files":      []any{map[string]any{"path": "/downloads/tv/Show.S01/e1.mkv", "length": "4000", "completedLength": "1000"}},
		"bittorrent": map[string]any{"info": map[string]any{"name": "Show.S01"}, "mode": "multi"}}
	paused = map[string]any{"gid": "0000000000000003", "status": "paused", "totalLength": "0", "completedLength": "0",
		"infoHash": hashC, "dir": "/elsewhere",
		"files": []any{map[string]any{"path": "[METADATA]" + hashC, "length": "0", "completedLength": "0"}}}
	failed = map[string]any{"gid": "0000000000000004", "status": "error", "totalLength": "10", "completedLength": "1",
		"errorCode": "3", "errorMessage": "Resource not found", "dir": "/downloads/movies",
		"files": []any{map[string]any{"path": "/downloads/movies/file.iso", "length": "10", "completedLength": "1"}}}
	removed = map[string]any{"gid": "0000000000000005", "status": "removed", "infoHash": hashA, "dir": "/downloads/movies"}
)

func answers(c rpcCall) (any, string) {
	switch c.Method {
	case "aria2.getVersion":
		return map[string]any{"version": "1.37.0", "enabledFeatures": []string{"BitTorrent"}}, ""
	case "aria2.getGlobalOption":
		return map[string]any{"dir": "/downloads"}, ""
	case "aria2.tellActive":
		return []any{seeding}, ""
	case "aria2.tellWaiting":
		return []any{downloading, paused}, ""
	case "aria2.tellStopped":
		return []any{metaDone, failed, removed}, ""
	case "aria2.tellStatus":
		return map[string]any{"status": "removed"}, ""
	case "aria2.addUri", "aria2.addTorrent":
		return "0000000000000009", ""
	case "aria2.changePosition":
		return 0, ""
	}
	return "OK", ""
}

func TestListAndState(t *testing.T) {
	f := &fake{answer: answers}
	c := f.start(t)
	ctx := context.Background()

	st, err := c.Test(ctx)
	if err != nil || st.Version != "1.37.0" {
		t.Fatalf("Test = %+v, %v", st, err)
	}
	items, err := c.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("want 4 items (removed and followed metadata dropped), got %+v", items)
	}
	byID := map[string]downloaders.Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	a := byID[hashA]
	if a.State != downloaders.StateSeeding || a.Progress != 1 || a.Category != "movies" || a.Name != "Movie.mkv" ||
		a.ContentPath != "/downloads/movies/Movie.mkv" || a.SavePath != "/downloads/movies" || a.Ratio != 0.5 || a.UploadSpeed != 20 {
		t.Errorf("seeding = %+v", a)
	}
	b := byID[hashB]
	if b.State != downloaders.StateQueued || b.Progress != 0.25 || b.Category != "tv" || b.ContentPath != "/downloads/tv/Show.S01" || b.Busy {
		t.Errorf("waiting = %+v", b)
	}
	cc := byID[hashC]
	if cc.State != downloaders.StatePaused || !cc.Busy || cc.Category != "" {
		t.Errorf("paused metadata = %+v", cc)
	}
	e := byID["0000000000000004"]
	if e.State != downloaders.StateFailed || e.Message != "Resource not found" {
		t.Errorf("error = %+v", e)
	}

	movies, err := c.List(ctx, "movies")
	if err != nil || len(movies) != 2 {
		t.Errorf("List(movies) = %+v, %v", movies, err)
	}
	cats, err := c.Categories(ctx)
	if err != nil || len(cats) != 2 {
		t.Errorf("Categories = %+v, %v", cats, err)
	}
	files, err := c.Files(ctx, hashB)
	if err != nil || len(files) != 1 || files[0].Path != "Show.S01/e1.mkv" || files[0].Progress != 0.25 {
		t.Errorf("Files = %+v, %v", files, err)
	}
}

func TestAddAndActions(t *testing.T) {
	f := &fake{answer: answers}
	c := f.start(t)
	ctx := context.Background()

	ratio, link := 2.0, ""
	link, hash := testkit.RandomMagnet("m")
	id, err := c.Add(ctx, downloaders.AddRequest{Magnet: link, Category: "tv", Paused: true, SeedRatio: &ratio,
		FirstLastPiece: true, Priority: downloaders.PriorityTop})
	if err != nil || id != hash {
		t.Fatalf("Add magnet = %q, %v", id, err)
	}
	add := f.find("aria2.addUri")[0]
	var uris []string
	var opts map[string]string
	_ = json.Unmarshal(add.Params[0], &uris)
	_ = json.Unmarshal(add.Params[1], &opts)
	if uris[0] != link || opts["dir"] != "/downloads/tv" || opts["pause"] != "true" || opts["seed-ratio"] != "2" ||
		opts["bt-prioritize-piece"] != "head,tail" {
		t.Errorf("addUri = %v %v", uris, opts)
	}
	if pos := f.find("aria2.changePosition"); len(pos) != 1 || string(pos[0].Params[2]) != `"POS_SET"` {
		t.Errorf("changePosition = %+v", pos)
	}

	tor := testkit.RandomTorrent("t", 1000)
	id, err = c.Add(ctx, downloaders.AddRequest{TorrentFile: tor.File, SavePath: "/data/seed"})
	if err != nil || id != tor.InfoHash {
		t.Fatalf("Add torrent = %q, %v", id, err)
	}
	at := f.find("aria2.addTorrent")[0]
	var b64 string
	_ = json.Unmarshal(at.Params[0], &b64)
	_ = json.Unmarshal(at.Params[2], &opts)
	raw, _ := base64.StdEncoding.DecodeString(b64)
	if string(raw) != string(tor.File) || opts["dir"] != "/data/seed" || opts["check-integrity"] != "true" {
		t.Errorf("addTorrent opts = %v", opts)
	}
	if _, err := c.Add(ctx, downloaders.AddRequest{Magnet: link, Category: "../x"}); !errors.Is(err, downloaders.ErrInvalidRequest) {
		t.Errorf("category with a slash: %v", err)
	}

	// Pause works on the active seeding item; resume only on paused ones.
	if err := c.Pause(ctx, hashA); err != nil {
		t.Fatal(err)
	}
	if p := f.find("aria2.forcePause"); len(p) != 1 || string(p[0].Params[0]) != `"2089b05ecca3d829"` {
		t.Errorf("forcePause = %+v", p)
	}
	if err := c.Resume(ctx, hashC); err != nil {
		t.Fatal(err)
	}
	if u := f.find("aria2.unpause"); len(u) != 1 || string(u[0].Params[0]) != `"0000000000000003"` {
		t.Errorf("unpause = %+v", u)
	}
	if err := c.SetPriority(ctx, hashB, downloaders.PriorityLast); err != nil {
		t.Fatal(err)
	}
	if err := c.SetPriority(ctx, hashA, downloaders.PriorityTop); err != nil { // active: no queue position, no-op
		t.Fatal(err)
	}
	if pos := f.find("aria2.changePosition"); len(pos) != 2 || string(pos[1].Params[2]) != `"POS_END"` {
		t.Errorf("changePosition after SetPriority = %+v", pos)
	}

	// Removing a magnet removes both of its gids (metadata and download).
	if err := c.Remove(ctx, hashB, true); err != nil {
		t.Fatal(err)
	}
	if fr := f.find("aria2.forceRemove"); len(fr) != 1 || string(fr[0].Params[0]) != `"0000000000000002"` {
		t.Errorf("forceRemove = %+v", fr)
	}
	if rr := f.find("aria2.removeDownloadResult"); len(rr) != 2 {
		t.Errorf("removeDownloadResult = %+v", rr)
	}
	if err := c.Remove(ctx, strings.Repeat("f", 40), false); !errors.Is(err, downloaders.ErrItemNotFound) {
		t.Errorf("remove unknown: %v", err)
	}
}

func TestErrors(t *testing.T) {
	f := &fake{answer: func(c rpcCall) (any, string) {
		if c.Method == "aria2.forcePause" {
			return nil, "GID 2089b05ecca3d829 is not found"
		}
		return answers(c)
	}}
	c := f.start(t)
	ctx := context.Background()

	bad, _ := New(downloaders.Config{Host: c.cfg.Host, Port: c.cfg.Port, APIKey: "wrong", HTTP: c.hc})
	_, err := bad.Test(ctx)
	if !errors.Is(err, downloaders.ErrAuth) {
		t.Errorf("wrong secret: %v", err)
	}
	if strings.Contains(err.Error(), "wrong") && strings.Contains(err.Error(), "token:") {
		t.Errorf("error leaks the secret: %v", err)
	}
	if err := c.Pause(ctx, hashA); !errors.Is(err, downloaders.ErrItemNotFound) {
		t.Errorf("not found: %v", err)
	}
	wrong, _ := New(downloaders.Config{Host: c.cfg.Host, Port: c.cfg.Port, URLBase: "/x", APIKey: secret, HTTP: c.hc})
	if _, err := wrong.Test(ctx); !errors.Is(err, downloaders.ErrWrongURL) {
		t.Errorf("wrong URL base: %v", err)
	}
	if _, err := c.Add(ctx, downloaders.AddRequest{NZB: []byte("<nzb/>")}); !errors.Is(err, downloaders.ErrInvalidRequest) {
		t.Errorf("NZB: %v", err)
	}
}

func TestCategoryOf(t *testing.T) {
	for _, tc := range []struct{ dir, base, want string }{
		{"/downloads/movies", "/downloads", "movies"},
		{"/downloads/movies/sub", "/downloads", "movies"},
		{"/downloads", "/downloads", ""},
		{"/downloadsx/y", "/downloads", ""},
		{"/other/movies", "/downloads", ""},
	} {
		if got := categoryOf(tc.dir, tc.base); got != tc.want {
			t.Errorf("categoryOf(%q, %q) = %q, want %q", tc.dir, tc.base, got, tc.want)
		}
	}
}
