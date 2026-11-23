package logging

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedaction(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "debug")
	log.Info("hello", "password", "hunter2", "api_key", "abc", "X-Api-Key", "k", "user", "ana",
		"nested", map[string]string{"a": "b"})
	log.WithGroup("req").Info("g", "Authorization", "Bearer xyz", "session_token", "t1")
	out := buf.String()
	for _, leak := range []string{"hunter2", `"abc"`, "Bearer xyz", `"t1"`, `"k"`} {
		if strings.Contains(out, leak) {
			t.Errorf("leaked %s in %s", leak, out)
		}
	}
	if !strings.Contains(out, `"user":"ana"`) {
		t.Errorf("non-secret attribute missing: %s", out)
	}
}

func TestLevel(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, "warn").Info("quiet")
	if buf.Len() != 0 {
		t.Fatal("info logged at warn level")
	}
}

func TestIsSensitive(t *testing.T) {
	for k, want := range map[string]bool{"Password": true, "new-password": true, "TOTP_CODE": true, "user": false, "path": false, "ip": false} {
		if IsSensitive(k) != want {
			t.Errorf("%s: want %v", k, want)
		}
	}
}

func TestValueRedaction(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "debug")
	log.Info("fetch https://idx.example/api?t=search&apikey=SECRETKEY1&q=x failed", "url", "http://user:hunter3@host:9091/rpc",
		"err", errors.New("GET https://api.example/x?api_key=SECRETKEY2: timeout"), "hdr", "Bearer abcdefghijk", "key", "lk_abcdefghijklmnop")
	out := buf.String()
	for _, leak := range []string{"SECRETKEY1", "SECRETKEY2", "hunter3", "abcdefghijk", "lk_abcdefghijklmnop"} {
		if strings.Contains(out, leak) {
			t.Errorf("leaked %s in %s", leak, out)
		}
	}
	if !strings.Contains(out, "q=x") || !strings.Contains(out, "host:9091") {
		t.Errorf("too much removed: %s", out)
	}
}

func TestRotationAndRing(t *testing.T) {
	dir := t.TempDir()
	f, err := OpenRotating(dir, 200, 2)
	if err != nil {
		t.Fatal(err)
	}
	ring := NewRing(3)
	log := New(&fanout{ws: []io.Writer{f, ring}}, "debug")
	for i := 0; i < 20; i++ {
		log.Info("line", "n", i, "password", "p")
	}
	_ = f.Close()
	files, err := Files(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("want current + 2 rotated, got %+v", files)
	}
	for _, fl := range files {
		if fl.Size > 400 {
			t.Errorf("%s too big: %d", fl.Name, fl.Size)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, fl.Name))
		if strings.Contains(string(raw), `"password":"p"`) {
			t.Error("secret in file")
		}
	}
	got := ring.Entries(Filter{}, 10)
	if len(got) != 3 || got[2].Fields["n"].(float64) != 19 || got[0].Seq != 18 {
		t.Fatalf("ring: %+v", got)
	}
	ch, stop := ring.Subscribe()
	defer stop()
	log.Warn("disk almost full", "path", "/data")
	select {
	case e := <-ch:
		if e.Level != "warn" || e.Message != "disk almost full" {
			t.Fatalf("%+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("no live entry")
	}
	if n := len(ring.Entries(Filter{MinLevel: "warn"}, 10)); n != 1 {
		t.Fatalf("level filter: %d", n)
	}
	if n := len(ring.Entries(Filter{Query: "/DATA"}, 10)); n != 1 {
		t.Fatalf("search: %d", n)
	}
	if _, err := OpenFile(dir, "../secret.key"); err == nil {
		t.Fatal("path outside the log names opened")
	}
}
