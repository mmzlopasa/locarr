package logging

import (
	"bytes"
	"strings"
	"testing"
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
