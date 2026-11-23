// Package logging builds the JSON logger and keeps secrets out of log records:
// attributes whose key names a secret are replaced, and secret-looking parts of
// every text value (API keys in URLs, bearer tokens, passwords in URLs) are
// masked. Records go to stdout, to rotating files in the config folder and to
// an in-memory ring that the log viewer reads and tails (D-083).
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"regexp"
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

var (
	// key=value pairs in query strings and form-like text.
	secretParam = regexp.MustCompile(`(?i)\b((?:api[_-]?key|apikey|access[_-]?token|token|password|passwd|pass|secret|sig|signature|pin)=)[^&\s"',;]+`)
	// Authorization headers written out as text.
	bearer = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{6,}`)
	// Locarr API keys.
	locarrKey = regexp.MustCompile(`\blk_[A-Za-z0-9_-]{8,}`)
	// Passwords in URLs (scheme://user:password@host).
	urlPassword = regexp.MustCompile(`(://[^/\s:@]+:)[^@\s/]+@`)
)

// Scrub masks secret-looking parts of a text.
func Scrub(s string) string {
	if s == "" {
		return s
	}
	s = secretParam.ReplaceAllString(s, "${1}"+Redacted)
	s = bearer.ReplaceAllString(s, "${1} "+Redacted)
	s = locarrKey.ReplaceAllString(s, "lk_"+Redacted)
	s = urlPassword.ReplaceAllString(s, "${1}"+Redacted+"@")
	return s
}

// New returns a JSON logger at level that redacts secrets, also inside groups.
func New(w io.Writer, level string) *slog.Logger {
	var l slog.Level
	_ = l.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: l, ReplaceAttr: redact}))
}

func redact(_ []string, a slog.Attr) slog.Attr {
	if a.Key != slog.TimeKey && a.Key != slog.LevelKey && IsSensitive(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		if s := v.String(); s != Scrub(s) {
			return slog.String(a.Key, Scrub(s))
		}
	case slog.KindAny:
		switch x := v.Any().(type) {
		case error:
			return slog.String(a.Key, Scrub(x.Error()))
		case fmt.Stringer:
			return slog.String(a.Key, Scrub(x.String()))
		}
	}
	return a
}
