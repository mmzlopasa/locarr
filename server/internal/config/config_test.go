package config

import "testing"

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaults(t *testing.T) {
	c, err := load(envOf(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != ":8484" || c.ConfigDir != "/config" || c.LogLevel != "info" {
		t.Fatalf("defaults: %+v", c)
	}
	if len(c.DataRoots) != 1 || c.DataRoots[0] != "/data" {
		t.Fatalf("roots: %v", c.DataRoots)
	}
	if c.AuthRatePerMin != 10 || c.AuthRateBurst != 10 {
		t.Fatalf("rate: %+v", c)
	}
	if c.DatabasePath() != "/config/locarr.db" {
		t.Fatal(c.DatabasePath())
	}
}

func TestOverrides(t *testing.T) {
	c, err := load(envOf(map[string]string{
		"LOCARR_HTTP_ADDR": "127.0.0.1:9000", "LOCARR_DATA_ROOTS": "/data, /mnt/media/ ,",
		"LOCARR_AUTH_RATE_LIMIT": "0", "LOCARR_LOG_LEVEL": "DEBUG",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != "127.0.0.1:9000" || c.LogLevel != "debug" || c.AuthRatePerMin != 0 {
		t.Fatalf("%+v", c)
	}
	if len(c.DataRoots) != 2 || c.DataRoots[1] != "/mnt/media" {
		t.Fatalf("roots: %v", c.DataRoots)
	}
}

func TestInvalid(t *testing.T) {
	for _, m := range []map[string]string{
		{"LOCARR_LOG_LEVEL": "loud"},
		{"LOCARR_DATA_ROOTS": "relative/path"},
		{"LOCARR_AUTH_RATE_LIMIT": "10"},
		{"LOCARR_AUTH_RATE_LIMIT": "-1,5"},
	} {
		if _, err := load(envOf(m)); err == nil {
			t.Errorf("%v: want error", m)
		}
	}
}
