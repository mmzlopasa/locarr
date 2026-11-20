package app

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/config"
)

// TestServeLifecycle starts the real server, checks it is ready within the
// 3-second target and that it shuts down cleanly.
func TestServeLifecycle(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	cfg := config.Config{HTTPAddr: addr, ConfigDir: filepath.Join(t.TempDir(), "config"), DataRoots: []string{t.TempDir()}, LogLevel: "info"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- Serve(ctx, cfg, quietLog()) }()
	for {
		res, err := http.Get("http://" + addr + "/readyz")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		if time.Since(start) > 3*time.Second {
			t.Fatal("not ready within 3 s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("ready after %s", time.Since(start))
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("no clean shutdown")
	}
}
