package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/config"
)

type liveServer struct {
	t    *testing.T
	base string
}

func (s liveServer) call(method, path, token string, body any) (int, []byte) {
	s.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, s.base+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func (s liveServer) must(method, path, token string, body any, want int, out any) {
	s.t.Helper()
	code, b := s.call(method, path, token, body)
	if code != want {
		s.t.Fatalf("%s %s: %d, want %d: %s", method, path, code, want, b)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			s.t.Fatal(err)
		}
	}
}

// A backup restored over the running server brings back the data, keeps the
// restorer signed in, signs everyone else out and never touches media.
func TestBackupRestoreRoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	data := t.TempDir()
	media := filepath.Join(data, "media", "movies", "Film (2001)", "Film (2001).mkv")
	if err := os.MkdirAll(filepath.Dir(media), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(media, []byte("the movie"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(t.TempDir(), "config")
	cfg := config.Config{HTTPAddr: addr, ConfigDir: cfgDir, DataRoots: []string{data}, LogLevel: "info", SetupCode: testSetupCode,
		ReleasesURL: "http://127.0.0.1:1/releases", TMDbURL: "http://127.0.0.1:1"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ServeWith(ctx, cfg, quietLog(), nil) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Error("no clean shutdown")
		}
	}()
	s := liveServer{t: t, base: "http://" + addr}
	waitReady := func() {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if code, _ := s.call("GET", "/readyz", "", nil); code == 200 {
				if code, _ := s.call("GET", "/api/v1/system/info", "", nil); code == 200 {
					return
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("not ready")
	}
	waitReady()
	var setup struct{ Token string }
	s.must("POST", "/api/v1/setup", "", map[string]any{"setupCode": testSetupCode, "username": "admin", "password": "admin-password",
		"language": "en"}, 200, &setup)
	admin := setup.Token
	var other struct{ Token string }
	s.must("POST", "/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin-password"}, 200, &other)
	s.must("PATCH", "/api/v1/settings", admin, map[string]any{"instanceName": "Before"}, 200, nil)
	var b struct{ Name, Kind string }
	s.must("POST", "/api/v1/system/backups", admin, nil, 201, &b)
	if b.Kind != "manual" || !strings.HasPrefix(b.Name, "locarr_backup_") {
		t.Fatalf("backup: %+v", b)
	}
	// Later changes that the restore must undo.
	s.must("PATCH", "/api/v1/settings", admin, map[string]any{"instanceName": "After"}, 200, nil)
	s.must("POST", "/api/v1/users", admin, map[string]any{"username": "later", "password": "later-password", "role": "manager"}, 201, nil)

	if code, body := s.call("POST", "/api/v1/system/backups/"+b.Name+"/restore?confirm=yes", admin, nil); code != 400 ||
		!strings.Contains(string(body), "confirmation_required") {
		t.Fatalf("restore without confirmation: %d %s", code, body)
	}
	var started struct {
		Restarting       bool
		PreRestoreBackup string `json:"preRestoreBackup"`
	}
	s.must("POST", "/api/v1/system/backups/"+b.Name+"/restore?confirm=RESTORE", admin, nil, 202, &started)
	if !started.Restarting || started.PreRestoreBackup == "" {
		t.Fatalf("restore: %+v", started)
	}
	// The server restarts in-process; wait until it runs on the restored data.
	deadline := time.Now().Add(20 * time.Second)
	for {
		var info struct{ InstanceName string }
		if code, body := s.call("GET", "/api/v1/system/info", "", nil); code == 200 {
			_ = json.Unmarshal(body, &info)
			if info.InstanceName == "Before" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("restore did not take effect")
		}
		time.Sleep(50 * time.Millisecond)
	}
	waitReady()
	s.must("GET", "/api/v1/auth/me", admin, nil, 200, nil)
	if code, _ := s.call("GET", "/api/v1/auth/me", other.Token, nil); code != 401 {
		t.Fatalf("other session still signed in: %d", code)
	}
	var users []struct{ Username string }
	s.must("GET", "/api/v1/users", admin, nil, 200, &users)
	if len(users) != 1 || users[0].Username != "admin" {
		t.Fatalf("users after restore: %+v", users)
	}
	raw, err := os.ReadFile(media)
	if err != nil || string(raw) != "the movie" {
		t.Fatalf("media touched: %q %v", raw, err)
	}
	var list []struct{ Name, Kind string }
	s.must("GET", "/api/v1/system/backups", admin, nil, 200, &list)
	kinds := map[string]bool{}
	for _, x := range list {
		kinds[x.Kind] = true
	}
	if !kinds["manual"] || !kinds["pre_restore"] {
		t.Fatalf("backups: %+v", list)
	}
	// The pre-restore backup can be downloaded and restored by upload.
	req, _ := http.NewRequest("GET", s.base+"/api/v1/system/backups/"+started.PreRestoreBackup, nil)
	req.Header.Set("Authorization", "Bearer "+admin)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	zipBody, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || len(zipBody) < 100 || !strings.Contains(res.Header.Get("Content-Disposition"), started.PreRestoreBackup) {
		t.Fatalf("download: %d %d", res.StatusCode, len(zipBody))
	}
	up, _ := http.NewRequest("POST", s.base+"/api/v1/system/backups/restore?confirm=RESTORE", bytes.NewReader([]byte("not a zip")))
	up.Header.Set("Authorization", "Bearer "+admin)
	up.Header.Set("Content-Type", "application/zip")
	res, err = http.DefaultClient.Do(up)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("bad upload: %d", res.StatusCode)
	}
	up, _ = http.NewRequest("POST", s.base+"/api/v1/system/backups/restore?confirm=RESTORE", bytes.NewReader(zipBody))
	up.Header.Set("Authorization", "Bearer "+admin)
	up.Header.Set("Content-Type", "application/zip")
	res, err = http.DefaultClient.Do(up)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 202 {
		t.Fatalf("upload restore: %d", res.StatusCode)
	}
	deadline = time.Now().Add(20 * time.Second)
	for {
		var info struct{ InstanceName string }
		if code, body := s.call("GET", "/api/v1/system/info", "", nil); code == 200 {
			_ = json.Unmarshal(body, &info)
			if info.InstanceName == "After" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("uploaded restore did not take effect")
		}
		time.Sleep(50 * time.Millisecond)
	}
	waitReady()
	s.must("GET", "/api/v1/users", admin, nil, 200, &users)
	if len(users) != 2 {
		t.Fatalf("users after the second restore: %+v", users)
	}
}
