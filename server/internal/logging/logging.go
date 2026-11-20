// Package logging builds the JSON logger and keeps secrets out of log records.
package logging

import (
	"io"
	"log/slog"
	"strings"
)

// Redacted replaces the value of every attribute whose key names a secret.
const Redacted = "[redacted]"

// sensitive are key fragments that mark an attribute as secret (matched
// case-insensitively, ignoring _ and -).
var sensitive = []string{"password", "passwd", "secret", "token", "apikey", "authorization", "cookie", "totp", "recovery", "credential", "privatekey"}

// IsSensitive reports whether an attribute key names a secret.
func IsSensitive(key string) bool {
	k := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	for _, s := range sensitive {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// New returns a JSON logger at level that redacts secret attributes, also
// inside groups.
func New(w io.Writer, level string) *slog.Logger {
	var l slog.Level
	_ = l.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: l, ReplaceAttr: redact}))
}

func redact(_ []string, a slog.Attr) slog.Attr {
	if IsSensitive(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	return a
}
