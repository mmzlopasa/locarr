package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestVersionAndUnknown(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), []string{"version"}, &out); err != nil || !strings.HasPrefix(out.String(), "locarr ") {
		t.Fatal(out.String(), err)
	}
	t.Setenv("LOCARR_CONFIG_DIR", t.TempDir())
	if err := run(context.Background(), []string{"bogus"}, &out); err == nil {
		t.Fatal("unknown command accepted")
	}
}

func TestMigrateAndResetAdmin(t *testing.T) {
	t.Setenv("LOCARR_CONFIG_DIR", t.TempDir())
	var out bytes.Buffer
	if err := run(context.Background(), []string{"migrate"}, &out); err != nil || !strings.Contains(out.String(), "schema version") {
		t.Fatal(out.String(), err)
	}
	// No administrator yet: reset-admin explains that setup is needed.
	if err := run(context.Background(), []string{"reset-admin"}, &out); err == nil || !strings.Contains(err.Error(), "setup") {
		t.Fatal(err)
	}
}

func TestHealthcheckFailsWithoutServer(t *testing.T) {
	t.Setenv("LOCARR_HTTP_ADDR", "127.0.0.1:1")
	if err := run(context.Background(), []string{"healthcheck"}, &bytes.Buffer{}); err == nil {
		t.Fatal("healthy without a server")
	}
}
