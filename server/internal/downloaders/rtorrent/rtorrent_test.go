package rtorrent

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/downloaders/testkit"
)

func respond(v string) string {
	return `<?xml version="1.0"?><methodResponse><params><param>` + v + `</param></params></methodResponse>`
}

func fault(code int, msg string) string {
	return `<?xml version="1.0"?><methodResponse><fault><value><struct>` +
		`<member><name>faultCode</name><value><i4>` + strconv.Itoa(code) + `</i4></value></member>` +
		`<member><name>faultString</name><value><string>` + msg + `</string></value></member>` +
		`</struct></value></fault></methodResponse>`
}

func TestCodecRoundTrip(t *testing.T) {
	in := []any{"a<b&c", int64(42), int64(1) << 40, true, false, 1.5, []byte{0, 1, 2, 255}, nil,
		[]any{"x", int64(-1)}, map[string]any{"k": "v", "n": int64(3)}}
	for _, v := range in {
		var b bytes.Buffer
		if err := encodeValue(&b, v, 0); err != nil {
			t.Fatalf("encode %v: %v", v, err)
		}
		got, err := decodeResponse([]byte(respond(b.String())))
		if err != nil {
			t.Fatalf("decode %v: %v", v, err)
		}
		if !reflect.DeepEqual(got, v) {
			t.Errorf("round trip %#v -> %#v", v, got)
		}
	}
}

func TestDecodeForms(t *testing.T) {
	// A value without a type is a string; <int> is an integer; <data/> is an empty array.
	v, err := decodeResponse([]byte(respond(`<value>plain</value>`)))
	if err != nil || v != "plain" {
		t.Fatalf("untyped = %v, %v", v, err)
	}
	v, err = decodeResponse([]byte(respond(`<value><int> 7 </int></value>`)))
	if err != nil || v != int64(7) {
		t.Fatalf("int = %v, %v", v, err)
	}
	v, err = decodeResponse([]byte(respond(`<value><array><data/></array></value>`)))
	if err != nil || !reflect.DeepEqual(v, []any{}) {
		t.Fatalf("empty array = %#v, %v", v, err)
	}
}

func TestDecodeRefusesBadInput(t *testing.T) {
	bad := []string{
		`<?xml version="1.0"?><!DOCTYPE x [<!ENTITY a "aaaa">]><methodResponse><params><param><value>&a;</value></param></params></methodResponse>`,
		`<html><body>Not here</body></html>`,
		respond(`<value><i4>x</i4></value>`),
		respond(`<value><boolean>2</boolean></value>`),
		respond(strings.Repeat(`<value><array><data>`, 40) + strings.Repeat(`</data></array></value>`, 40)),
		`not xml`,
	}
	for _, b := range bad {
		if _, err := decodeResponse([]byte(b)); !errors.Is(err, errBadXML) {
			t.Errorf("%.60q: err = %v, want errBadXML", b, err)
		}
	}
}

func TestFault(t *testing.T) {
	_, err := decodeResponse([]byte(fault(-501, "Could not find info-hash.")))
	var f *Fault
	if !errors.As(err, &f) || f.Code != -501 {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(mapFault(f), downloaders.ErrItemNotFound) {
		t.Error("unknown hash should map to ErrItemNotFound")
	}
	if !errors.Is(mapFault(&Fault{Code: -506, Message: "Method 'x' not defined"}), downloaders.ErrClient) {
		t.Error("other faults should map to ErrClient")
	}
}

// decodeCall parses a methodCall (the test server's side of the codec).
func decodeCall(t *testing.T, b []byte) (string, []any) {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(b))
	must := func(err error) {
		if err != nil {
			t.Fatalf("bad call %s: %v", b, err)
		}
	}
	_, err := nextStart(d) // methodCall
	must(err)
	_, err = nextStart(d) // methodName
	must(err)
	name, err := readText(d)
	must(err)
	_, err = nextStart(d) // params
	must(err)
	var params []any
	for {
		_, more, err := nextStartOrEnd(d)
		must(err)
		if !more {
			break
		}
		v, err := expectValue(d, 0)
		must(err)
		must(expectEnd(d))
		params = append(params, v)
	}
	return name, params
}

type call struct {
	method string
	params []any
}

type fakeServer struct {
	mu     sync.Mutex
	calls  []call
	answer func(method string, params []any) string
}

func (f *fakeServer) start(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/RPC2" {
			http.NotFound(w, r)
			return
		}
		if u, p, ok := r.BasicAuth(); !ok || u != "user" || p != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		m, params := decodeCall(t, body)
		f.mu.Lock()
		f.calls = append(f.calls, call{m, params})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, f.answer(m, params))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	c, err := New(downloaders.Config{Host: u.Hostname(), Port: port, Username: "user", Password: "secret",
		HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f *fakeServer) last() call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func i(n int64) string  { return "<value><i8>" + strconv.FormatInt(n, 10) + "</i8></value>" }
func s(v string) string { return "<value><string>" + v + "</string></value>" }

// row renders one d.multicall2 row in listFields order.
func row(vals ...string) string {
	return "<value><array><data>" + strings.Join(vals, "") + "</data></array></value>"
}

func listAnswer() string {
	seeding := row(s("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), s("Movie.2024.1080p.mkv"), s("movies"), i(1000), i(1000), i(0),
		i(0), i(250), i(1500), i(1), i(1), i(1), i(1), i(0), s("Tracker: [Timeout was reached]"), s("/downloads/complete"), i(0),
		i(1500), i(3), s("1700000000"), i(1700000100), i(1700000200), i(2), s("/downloads/complete/Movie.2024.1080p.mkv"))
	paused := row(s("BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"), s("Show.S01"), s("tv"), i(2000), i(500), i(1500),
		i(0), i(0), i(0), i(1), i(0), i(1), i(0), i(0), s(""), s("/downloads/temp/Show.S01"), i(1),
		i(0), i(0), s(""), i(1700000300), i(0), i(3), s(""))
	downloading := row(s("CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"), s("Album"), s("music"), i(4000), i(1000), i(3000),
		i(100), i(0), i(0), i(1), i(1), i(1), i(0), i(0), s(""), s("/downloads/temp/Album"), i(1),
		i(0), i(0), s(""), i(0), i(0), i(2), s("/downloads/temp/Album"))
	hashing := row(s("DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"), s("Check"), s("movies"), i(10), i(0), i(10),
		i(0), i(0), i(0), i(1), i(1), i(1), i(0), i(1), s(""), s("/downloads/x"), i(0),
		i(0), i(0), s(""), i(0), i(0), i(2), s(""))
	broken := row(s("EEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE"), s("Broken"), s("movies"), i(10), i(5), i(5),
		i(0), i(0), i(0), i(0), i(0), i(1), i(0), i(0), s("Storage error: [File not found]"), s("/downloads/y"), i(0),
		i(0), i(0), s(""), i(0), i(0), i(2), s(""))
	return respond("<value><array><data>" + seeding + paused + downloading + hashing + broken + "</data></array></value>")
}

func TestClient(t *testing.T) {
	f := &fakeServer{answer: func(m string, p []any) string {
		switch m {
		case "system.client_version":
			return respond(s("0.9.8"))
		case "d.multicall2":
			if len(p) == 3 { // categories
				return respond("<value><array><data>" + row(s("movies")) + row(s("tv")) + row(s("movies")) + row(s("")) + "</data></array></value>")
			}
			return listAnswer()
		case "f.multicall":
			return respond("<value><array><data>" + row(s("a/b.mkv"), i(100), i(5), i(10)) + "</data></array></value>")
		case "d.erase":
			if p[0] == "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF" {
				return fault(-501, "Could not find info-hash.")
			}
		}
		return respond(i(0))
	}}
	c := f.start(t)
	ctx := context.Background()

	st, err := c.Test(ctx)
	if err != nil || st.Version != "0.9.8" {
		t.Fatalf("Test = %+v, %v", st, err)
	}

	items, err := c.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("got %d items", len(items))
	}
	seed := items[0]
	if seed.ID != strings.Repeat("a", 40) || seed.State != downloaders.StateSeeding || seed.Progress != 1 || seed.Ratio != 1.5 ||
		seed.SavePath != "/downloads/complete" || seed.ContentPath != "/downloads/complete/Movie.2024.1080p.mkv" ||
		seed.Category != "movies" || seed.UploadSpeed != 250 || seed.Seeds != 3 || seed.AddedAt.Unix() != 1700000000 ||
		seed.SeedingTime <= 0 || seed.Message == "" {
		t.Errorf("seeding item = %+v", seed)
	}
	paused := items[1]
	if paused.State != downloaders.StatePaused || paused.Progress != 0.25 || paused.SavePath != "/downloads/temp" ||
		paused.ContentPath != "/downloads/temp/Show.S01" || paused.ETA != nil {
		t.Errorf("paused item = %+v", paused)
	}
	dl := items[2]
	if dl.State != downloaders.StateDownloading || dl.ETA == nil || dl.ETA.Seconds() != 30 {
		t.Errorf("downloading item = %+v", dl)
	}
	if h := items[3]; h.State != downloaders.StateQueued || !h.Busy || h.RawState != "hashing" {
		t.Errorf("hashing item = %+v", h)
	}
	if b := items[4]; b.State != downloaders.StateWarning || !strings.Contains(b.Message, "Storage error") {
		t.Errorf("broken item = %+v", b)
	}

	movies, err := c.List(ctx, "Movies")
	if err != nil || len(movies) != 3 {
		t.Errorf("List(movies) = %d items, %v", len(movies), err)
	}

	cats, err := c.Categories(ctx)
	if err != nil || len(cats) != 2 {
		t.Errorf("Categories = %+v, %v", cats, err)
	}

	tor := testkit.RandomTorrent("x", 1000)
	id, err := c.Add(ctx, downloaders.AddRequest{TorrentFile: tor.File, Category: "movies", SavePath: "/data/my dir", Priority: downloaders.PriorityTop})
	if err != nil || id != tor.InfoHash {
		t.Fatalf("Add = %q, %v", id, err)
	}
	got := f.last()
	if got.method != "load.raw_start" || got.params[0] != "" || !bytes.Equal(got.params[1].([]byte), tor.File) {
		t.Errorf("add call = %s %v", got.method, got.params[:1])
	}
	want := []any{"d.custom1.set=movies", `d.directory.set="/data/my dir"`, "d.priority.set=3"}
	if !reflect.DeepEqual(got.params[2:5], want) {
		t.Errorf("add commands = %v, want %v", got.params[2:5], want)
	}

	link, hash := testkit.RandomMagnet("m")
	id, err = c.Add(ctx, downloaders.AddRequest{Magnet: link, Paused: true})
	if err != nil || id != hash {
		t.Fatalf("Add magnet = %q, %v", id, err)
	}
	if got := f.last(); got.method != "load.normal" || got.params[1] != link {
		t.Errorf("magnet call = %s %v", got.method, got.params)
	}

	if err := c.SetPriority(ctx, strings.Repeat("a", 40), downloaders.PriorityLast); err != nil {
		t.Fatal(err)
	}
	if got := f.last(); got.method != "d.priority.set" || got.params[0] != strings.Repeat("A", 40) || got.params[1] != int64(1) {
		t.Errorf("priority call = %+v", got)
	}
	if err := c.Pause(ctx, strings.Repeat("a", 40)); err != nil || f.last().method != "d.stop" {
		t.Errorf("pause: %v %+v", err, f.last())
	}
	if err := c.Resume(ctx, strings.Repeat("a", 40)); err != nil || f.last().method != "d.start" {
		t.Errorf("resume: %v %+v", err, f.last())
	}
	if err := c.Remove(ctx, strings.Repeat("a", 40), true); err != nil || f.last().method != "d.erase" {
		t.Errorf("remove: %v %+v", err, f.last())
	}
	if err := c.Remove(ctx, strings.Repeat("f", 40), false); !errors.Is(err, downloaders.ErrItemNotFound) {
		t.Errorf("remove unknown: %v", err)
	}

	files, err := c.Files(ctx, strings.Repeat("a", 40))
	if err != nil || len(files) != 1 || files[0].Path != "a/b.mkv" || files[0].Progress != 0.5 {
		t.Errorf("Files = %+v, %v", files, err)
	}
}

func TestErrors(t *testing.T) {
	f := &fakeServer{answer: func(string, []any) string { return respond(s("x")) }}
	c := f.start(t)
	ctx := context.Background()

	bad, err := New(downloaders.Config{Host: c.cfg.Host, Port: c.cfg.Port, Username: "user", Password: "wrong", HTTP: c.hc})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Test(ctx); !errors.Is(err, downloaders.ErrAuth) {
		t.Errorf("wrong password: %v", err)
	}
	wrongBase, _ := New(downloaders.Config{Host: c.cfg.Host, Port: c.cfg.Port, URLBase: "/nope", Username: "user", Password: "secret", HTTP: c.hc})
	if _, err := wrongBase.Test(ctx); !errors.Is(err, downloaders.ErrWrongURL) {
		t.Errorf("wrong URL base: %v", err)
	}
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html>hi</html>") }))
	defer html.Close()
	u, _ := url.Parse(html.URL)
	port, _ := strconv.Atoi(u.Port())
	h, _ := New(downloaders.Config{Host: u.Hostname(), Port: port, HTTP: html.Client()})
	if _, err := h.Test(ctx); !errors.Is(err, downloaders.ErrWrongURL) {
		t.Errorf("HTML page: %v", err)
	}
	if _, err := c.Add(ctx, downloaders.AddRequest{NZB: []byte("x")}); !errors.Is(err, downloaders.ErrInvalidRequest) {
		t.Errorf("NZB on a torrent client: %v", err)
	}
	if _, err := c.Add(ctx, downloaders.AddRequest{TorrentFile: []byte("garbage")}); !errors.Is(err, downloaders.ErrBadTorrent) {
		t.Errorf("bad torrent: %v", err)
	}
}

func TestEscapeCmd(t *testing.T) {
	for in, want := range map[string]string{
		"movies":      "movies",
		"a b":         `"a b"`,
		`x,y;"z"`:     `"x,y;\"z\""`,
		"line\nbreak": `"line break"`,
	} {
		if got := escapeCmd(in); got != want {
			t.Errorf("escapeCmd(%q) = %q, want %q", in, got, want)
		}
	}
}
