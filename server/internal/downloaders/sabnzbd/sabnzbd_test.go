package sabnzbd

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
)

const testKey = "0123456789abcdef0123456789abcdef"

// Fixtures follow the answers documented for SABnzbd's API (output=json).
const queueJSON = `{"queue":{"status":"Downloading","paused":false,"kbpersec":"2048.00","diskspace1":"100.50",
"slots":[
 {"status":"Downloading","nzo_id":"SABnzbd_nzo_aaa","filename":"Movie.2024.1080p","cat":"movies","mb":"1000.00","mbleft":"250.00","percentage":"75","timeleft":"0:02:05","priority":"Normal"},
 {"status":"Paused","nzo_id":"SABnzbd_nzo_bbb","filename":"Show.S01E01","cat":"tv","mb":"500.00","mbleft":"500.00","percentage":"0","timeleft":"0:00:00","priority":"Normal"},
 {"status":"Queued","nzo_id":"SABnzbd_nzo_ccc","filename":"Album","cat":"*","size":"1000 B","sizeleft":"1000 B","mb":"0.00","mbleft":"0.00","percentage":"0","timeleft":"1:00:00:00","priority":"Low"}
]}}`

const historyJSON = `{"history":{"noofslots":3,"slots":[
 {"nzo_id":"SABnzbd_nzo_ddd","name":"Done.Movie","category":"movies","status":"Completed","fail_message":"","bytes":2097152,"storage":"/data/usenet/complete/movies/Done.Movie","completed":1700000500,"time_added":1700000000},
 {"nzo_id":"SABnzbd_nzo_eee","name":"Broken.Movie","category":"movies","status":"Failed","fail_message":"Repair failed, not enough repair blocks","bytes":1048576,"storage":"/data/usenet/incomplete/Broken.Movie"},
 {"nzo_id":"SABnzbd_nzo_fff","name":"Unpacking.Movie","category":"movies","status":"Extracting","fail_message":"","bytes":3145728,"storage":""}
]}}`

type fakeSAB struct {
	mu       sync.Mutex
	key      string
	requests []url.Values
	upload   struct {
		field, filename, body string
	}
}

func (f *fakeSAB) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	f.mu.Lock()
	f.requests = append(f.requests, q)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	mode := q.Get("mode")
	if mode == "version" {
		_, _ = io.WriteString(w, `{"version":"4.3.2"}`)
		return
	}
	switch q.Get("apikey") {
	case "":
		_, _ = io.WriteString(w, "API Key Required\n") // plain text, as SABnzbd answers
		return
	case f.key:
	default:
		_, _ = io.WriteString(w, "API Key Incorrect\n")
		return
	}
	switch mode {
	case "queue":
		switch q.Get("name") {
		case "":
			_, _ = io.WriteString(w, queueJSON)
		case "delete", "pause", "resume":
			_, _ = io.WriteString(w, `{"status":true,"nzo_ids":["`+q.Get("value")+`"]}`)
		case "priority":
			_, _ = io.WriteString(w, `{"position":0}`)
		}
	case "history":
		if q.Get("name") == "delete" {
			_, _ = io.WriteString(w, `{"status":true}`)
			return
		}
		_, _ = io.WriteString(w, historyJSON)
	case "addfile":
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		for field, fhs := range r.MultipartForm.File {
			file, _ := fhs[0].Open()
			b, _ := io.ReadAll(file)
			_ = file.Close()
			f.mu.Lock()
			f.upload.field, f.upload.filename, f.upload.body = field, fhs[0].Filename, string(b)
			f.mu.Unlock()
		}
		_, _ = io.WriteString(w, `{"status":true,"nzo_ids":["SABnzbd_nzo_new"]}`)
	case "addurl":
		_, _ = io.WriteString(w, `{"status":true,"nzo_ids":["SABnzbd_nzo_url"]}`)
	case "get_config":
		_, _ = io.WriteString(w, `{"config":{"categories":[{"name":"*","dir":""},{"name":"movies","dir":"movies"},{"name":"tv","dir":""}]}}`)
	case "set_config":
		_, _ = io.WriteString(w, `{"config":{"categories":[{"name":"`+q.Get("keyword")+`"}]}}`)
	case "get_files":
		_, _ = io.WriteString(w, `{"files":[{"filename":"movie.part01.rar","mb":"100.00","mbleft":"25.00","bytes":"104857600.00"}]}`)
	default:
		_, _ = io.WriteString(w, `{"status":false,"error":"not implemented"}`)
	}
}

func (f *fakeSAB) last(mode string) url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.requests) - 1; i >= 0; i-- {
		r := f.requests[i]
		listing := (mode == "queue" || mode == "history") && r.Get("name") == ""
		if r.Get("mode") == mode && !listing {
			return r
		}
	}
	return nil
}

func newTest(t *testing.T, key string) (*Client, *fakeSAB) {
	t.Helper()
	fake := &fakeSAB{key: testKey}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	c, err := New(downloaders.Config{Host: host, Port: p, APIKey: key, HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return c, fake
}

func TestTest(t *testing.T) {
	c, _ := newTest(t, testKey)
	st, err := c.Test(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "4.3.2" || st.FreeSpace == nil || *st.FreeSpace != int64(100.5*(1<<30)) {
		t.Fatalf("status = %+v", st)
	}
}

func TestWrongKeyIsAuthAndNeverLeaksKey(t *testing.T) {
	const secret = "supersecretkey-should-not-leak"
	c, _ := newTest(t, secret)
	_, err := c.Test(context.Background())
	if !errors.Is(err, downloaders.ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks the key: %v", err)
	}
}

func TestUnreachableNeverLeaksKey(t *testing.T) {
	const secret = "supersecretkey-should-not-leak"
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	c, err := New(downloaders.Config{Host: "127.0.0.1", Port: port, APIKey: secret,
		HTTP: downloaders.NewHTTPClient(downloaders.HTTPOptions{Timeout: 2 * time.Second})})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Test(context.Background())
	if !errors.Is(err, downloaders.ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks the key: %v", err)
	}
}

func TestWrongURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html>some other app</html>")
	}))
	defer srv.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	c, _ := New(downloaders.Config{Host: host, Port: p, APIKey: "k", HTTP: srv.Client()})
	if _, err := c.Test(context.Background()); !errors.Is(err, downloaders.ErrWrongURL) {
		t.Fatalf("err = %v, want ErrWrongURL", err)
	}
}

func TestList(t *testing.T) {
	c, _ := newTest(t, testKey)
	items, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]downloaders.Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if len(byID) != 6 {
		t.Fatalf("got %d items", len(byID))
	}
	dl := byID["SABnzbd_nzo_aaa"]
	if dl.State != downloaders.StateDownloading || dl.RawState != "Downloading" || dl.Progress != 0.75 ||
		dl.Size != 1000<<20 || dl.Downloaded != 750<<20 || dl.DownloadSpeed != 2048*1024 || dl.ETA == nil || *dl.ETA != 125*time.Second {
		t.Errorf("downloading = %+v", dl)
	}
	if byID["SABnzbd_nzo_bbb"].State != downloaders.StatePaused {
		t.Errorf("paused = %+v", byID["SABnzbd_nzo_bbb"])
	}
	q := byID["SABnzbd_nzo_ccc"]
	if q.State != downloaders.StateQueued || q.Category != "" || q.DownloadSpeed != 0 || q.Size != 1000 || q.Progress != 0 {
		t.Errorf("queued = %+v", q)
	}
	done := byID["SABnzbd_nzo_ddd"]
	if done.State != downloaders.StateCompleted || done.Busy || done.Progress != 1 ||
		done.ContentPath != "/data/usenet/complete/movies/Done.Movie" || done.SavePath != "/data/usenet/complete/movies" {
		t.Errorf("completed = %+v", done)
	}
	failed := byID["SABnzbd_nzo_eee"]
	if failed.State != downloaders.StateFailed || failed.Message != "Repair failed, not enough repair blocks" {
		t.Errorf("failed = %+v", failed)
	}
	pp := byID["SABnzbd_nzo_fff"]
	if !pp.Busy || pp.State == downloaders.StateCompleted {
		t.Errorf("post-processing = %+v", pp)
	}

	movies, err := c.List(context.Background(), "movies")
	if err != nil {
		t.Fatal(err)
	}
	if len(movies) != 4 {
		t.Errorf("movies = %d items, want 4", len(movies))
	}
}

func TestParseTimeLeft(t *testing.T) {
	for in, want := range map[string]time.Duration{"0:02:05": 125 * time.Second, "1:00:00:00": 24 * time.Hour, "12:00": 12 * time.Minute} {
		if got, ok := parseTimeLeft(in); !ok || got != want {
			t.Errorf("%s = %v %v", in, got, ok)
		}
	}
	if _, ok := parseTimeLeft("soon"); ok {
		t.Error("garbage parsed")
	}
}

func TestAddFile(t *testing.T) {
	c, fake := newTest(t, testKey)
	id, err := c.Add(context.Background(), downloaders.AddRequest{NZB: []byte("<nzb/>"), Name: "Some/Release", Category: "movies", Paused: true})
	if err != nil {
		t.Fatal(err)
	}
	if id != "SABnzbd_nzo_new" {
		t.Fatalf("id = %q", id)
	}
	q := fake.last("addfile")
	if q.Get("cat") != "movies" || q.Get("priority") != "-2" || q.Get("nzbname") != "Some/Release" {
		t.Errorf("query = %v", q)
	}
	if fake.upload.field != "name" || fake.upload.filename != "Some_Release.nzb" || fake.upload.body != "<nzb/>" {
		t.Errorf("upload = %+v", fake.upload)
	}
}

func TestAddPriorities(t *testing.T) {
	c, fake := newTest(t, testKey)
	for prio, want := range map[downloaders.Priority]string{downloaders.PriorityTop: "1", downloaders.PriorityLast: "-1", downloaders.PriorityNormal: "-100"} {
		if _, err := c.Add(context.Background(), downloaders.AddRequest{NZBURL: "http://indexer/get/1", Priority: prio}); err != nil {
			t.Fatal(err)
		}
		q := fake.last("addurl")
		if q.Get("priority") != want || q.Get("name") != "http://indexer/get/1" {
			t.Errorf("%s: query = %v", prio, q)
		}
	}
	if _, err := c.Add(context.Background(), downloaders.AddRequest{Magnet: "magnet:?xt=urn:btih:abc"}); !errors.Is(err, downloaders.ErrInvalidRequest) {
		t.Errorf("magnet accepted: %v", err)
	}
}

func TestActions(t *testing.T) {
	c, fake := newTest(t, testKey)
	ctx := context.Background()
	if err := c.Pause(ctx, "SABnzbd_nzo_aaa"); err != nil {
		t.Fatal(err)
	}
	if q := fake.last("queue"); q.Get("name") != "pause" || q.Get("value") != "SABnzbd_nzo_aaa" {
		t.Errorf("pause = %v", q)
	}
	if err := c.Resume(ctx, "SABnzbd_nzo_bbb"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetPriority(ctx, "SABnzbd_nzo_aaa", downloaders.PriorityTop); err != nil {
		t.Fatal(err)
	}
	if q := fake.last("queue"); q.Get("name") != "priority" || q.Get("value2") != "1" {
		t.Errorf("priority = %v", q)
	}
	if err := c.Remove(ctx, "SABnzbd_nzo_aaa", true); err != nil {
		t.Fatal(err)
	}
	if q := fake.last("queue"); q.Get("name") != "delete" || q.Get("del_files") != "1" {
		t.Errorf("queue delete = %v", q)
	}
	if err := c.Remove(ctx, "SABnzbd_nzo_ddd", false); err != nil {
		t.Fatal(err)
	}
	if q := fake.last("history"); q.Get("name") != "delete" || q.Get("value") != "SABnzbd_nzo_ddd" || q.Has("del_files") {
		t.Errorf("history delete = %v", q)
	}
	if err := c.Remove(ctx, "SABnzbd_nzo_nothere", true); !errors.Is(err, downloaders.ErrItemNotFound) {
		t.Errorf("unknown remove: %v", err)
	}
	if err := c.Pause(ctx, "SABnzbd_nzo_ddd"); !errors.Is(err, downloaders.ErrItemNotFound) {
		t.Errorf("pausing a finished job: %v", err)
	}
	files, err := c.Files(ctx, "SABnzbd_nzo_aaa")
	if err != nil || len(files) != 1 || files[0].Size != 104857600 || files[0].Progress != 0.75 {
		t.Errorf("files = %+v, %v", files, err)
	}
}

func TestCategories(t *testing.T) {
	c, fake := newTest(t, testKey)
	cats, err := c.Categories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 2 || cats[0].Name != "movies" || cats[0].SavePath != "movies" {
		t.Fatalf("cats = %+v", cats)
	}
	if err := c.EnsureCategory(context.Background(), "Movies", ""); err != nil {
		t.Fatal(err)
	}
	if fake.last("set_config") != nil {
		t.Error("existing category created again")
	}
	if err := c.EnsureCategory(context.Background(), "music", "/data/usenet/music"); err != nil {
		t.Fatal(err)
	}
	q := fake.last("set_config")
	if q.Get("section") != "categories" || q.Get("keyword") != "music" || q.Get("dir") != "/data/usenet/music" {
		t.Errorf("set_config = %v", q)
	}
}

func TestHostWhitelist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "Access denied - Hostname verification failed: https://sabnzbd.org/hostname-check")
	}))
	defer srv.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	c, _ := New(downloaders.Config{Host: host, Port: p, APIKey: "k", HTTP: srv.Client()})
	_, err := c.Test(context.Background())
	if !errors.Is(err, downloaders.ErrClient) || !strings.Contains(err.Error(), "host_whitelist") {
		t.Fatalf("err = %v", err)
	}
}
