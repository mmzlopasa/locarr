package channels

import (
	"context"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
)

var senders = map[Kind]Sender{
	Webhook: webhookSender{}, Ntfy: ntfySender{}, Gotify: gotifySender{}, Discord: discordSender{},
	Telegram: telegramSender{}, Pushover: pushoverSender{}, Email: emailSender{}, Apprise: appriseSender{},
}

// Get returns the sender of a kind.
func Get(k Kind) (Sender, error) {
	s, ok := senders[k]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupported, string(k))
	}
	return s, nil
}

// Validate checks a channel's settings and secrets for its kind.
func Validate(k Kind, cfg Config, sec Secrets) error {
	s, err := Get(k)
	if err != nil {
		return err
	}
	if cfg.Language != "" && cfg.Language != "en" && cfg.Language != "hr" {
		return invalid("language must be en or hr")
	}
	return s.Validate(cfg, sec)
}

// Send validates the channel and delivers msg through it.
func Send(ctx context.Context, k Kind, cfg Config, sec Secrets, env Env, msg Message) error {
	s, err := Get(k)
	if err != nil {
		return err
	}
	if err := s.Validate(cfg, sec); err != nil {
		return err
	}
	if err := s.Send(ctx, cfg, sec, env, msg); err != nil {
		return scrubbed{err: err, secrets: sec.List()}
	}
	return nil
}

// scrubbed hides secret values in an error's text but keeps it unwrappable.
type scrubbed struct {
	err     error
	secrets []string
}

func (e scrubbed) Error() string { return httpx.Scrub(e.err.Error(), e.secrets...) }
func (e scrubbed) Unwrap() error { return e.err }

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func checkURL(field, raw string) error {
	if raw == "" {
		return invalid("%s is required", field)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return invalid("%s must be an http:// or https:// address", field)
	}
	return nil
}

func checkPriority(p, lo, hi int) error {
	if p < lo || p > hi {
		return invalid("priority must be between %d and %d", lo, hi)
	}
	return nil
}

var ntfyTopic = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func validAddresses(to []string) error {
	if len(to) == 0 {
		return invalid("at least one recipient is required")
	}
	if len(to) > 20 {
		return invalid("at most 20 recipients")
	}
	for _, a := range to {
		if strings.ContainsAny(a, "\r\n") {
			return invalid("recipient %q is not an e-mail address", a)
		}
		if _, err := mail.ParseAddress(a); err != nil {
			return invalid("recipient %q is not an e-mail address", a)
		}
	}
	return nil
}

// do sends one HTTP request through the guarded client and fails on a non-2xx answer.
func do(ctx context.Context, env Env, service, method, rawURL string, hdr http.Header, body []byte, secrets []string) (*httpx.Response, error) {
	if env.HTTP == nil {
		return nil, fmt.Errorf("%s: no HTTP client", service)
	}
	res, err := env.HTTP.Do(ctx, method, rawURL, hdr, body)
	if err != nil {
		return nil, fmt.Errorf("%s: %s", service, httpx.Scrub(err.Error(), secrets...))
	}
	if res.Status < 200 || res.Status > 299 {
		return res, fmt.Errorf("%s: HTTP %d: %s", service, res.Status, excerpt(res.Body, secrets))
	}
	return res, nil
}

func excerpt(b []byte, secrets []string) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	if s == "" {
		s = "(empty answer)"
	}
	return httpx.Scrub(s, secrets...)
}

func jsonHeader() http.Header {
	return http.Header{"Content-Type": {"application/json"}, "Accept": {"application/json"}}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		if n > 1 {
			return string(r[:n-1]) + "…"
		}
		return string(r[:n])
	}
	return s
}
