// Package channels sends notification events to outside services: webhooks,
// ntfy, Gotify, Discord, Telegram, Pushover, e-mail (SMTP) and Apprise API.
// Every sender is written from the service's public API documentation. HTTP
// goes through the guarded outbound client (timeouts, size cap, no
// cross-host redirects, address policy); secrets never appear in errors.
package channels

import (
	"context"
	"crypto/tls"
	"errors"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/notify"
)

// Kind is a channel type. The values are stored and part of the API.
type Kind string

// Kinds.
const (
	Webhook  Kind = "webhook"
	Ntfy     Kind = "ntfy"
	Gotify   Kind = "gotify"
	Discord  Kind = "discord"
	Telegram Kind = "telegram"
	Pushover Kind = "pushover"
	Email    Kind = "email"
	Apprise  Kind = "apprise"
)

// Kinds lists every kind.
var Kinds = []Kind{Webhook, Ntfy, Gotify, Discord, Telegram, Pushover, Email, Apprise}

// Config are a channel's plain settings (stored as JSON); each kind uses only
// its own fields.
type Config struct {
	// URL: webhook target, ntfy/gotify server, Apprise notify endpoint.
	URL    string `json:"url,omitempty"`
	Method string `json:"method,omitempty"` // webhook: POST (default) or PUT
	Topic  string `json:"topic,omitempty"`  // ntfy
	// Priority: ntfy 1–5, Gotify 0–10, Pushover -2–2 (0 = the service default).
	Priority int      `json:"priority,omitempty"`
	Tags     []string `json:"tags,omitempty"` // ntfy
	Tag      string   `json:"tag,omitempty"`  // apprise
	ChatID   string   `json:"chatId,omitempty"`
	Sound    string   `json:"sound,omitempty"`  // pushover
	Device   string   `json:"device,omitempty"` // pushover
	To       []string `json:"to,omitempty"`     // e-mail
	// Username: Discord sender name; ntfy user for basic auth (with Secrets.Password).
	Username string `json:"username,omitempty"`
	Language string `json:"language,omitempty"` // en (default) or hr
}

// Secrets are a channel's write-only values (stored encrypted).
type Secrets struct {
	Token      string `json:"token,omitempty"`      // ntfy access token, Gotify app token, Pushover app token
	Secret     string `json:"secret,omitempty"`     // webhook HMAC key
	BotToken   string `json:"botToken,omitempty"`   // telegram
	WebhookURL string `json:"webhookUrl,omitempty"` // discord
	UserKey    string `json:"userKey,omitempty"`    // pushover
	Password   string `json:"password,omitempty"`   // ntfy basic auth
}

// List returns the secrets' values (for redaction).
func (s Secrets) List() []string {
	return []string{s.Token, s.Secret, s.BotToken, s.WebhookURL, s.UserKey, s.Password}
}

// Names returns the names of the secrets that are set (API field names).
func (s Secrets) Names() []string {
	var out []string
	for _, x := range []struct{ n, v string }{{"token", s.Token}, {"secret", s.Secret}, {"botToken", s.BotToken},
		{"webhookUrl", s.WebhookURL}, {"userKey", s.UserKey}, {"password", s.Password}} {
		if x.v != "" {
			out = append(out, x.n)
		}
	}
	return out
}

// SMTP is the mail server every e-mail channel uses.
type SMTP struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	// Security: none, starttls (default) or tls (implicit TLS, usually port 465).
	Security string `json:"security"`
	Username string `json:"username"`
	Password string `json:"-"`
	From     string `json:"from"`
}

// Env is what a send needs besides the channel.
type Env struct {
	// HTTP is the guarded outbound client for this channel.
	HTTP *httpx.Client
	SMTP SMTP
	// BaseURL is Locarr's address for links ("" = no links).
	BaseURL string
	// TelegramAPI and PushoverAPI replace the services' addresses (tests).
	TelegramAPI string
	PushoverAPI string
	// SMTPTimeout bounds a whole e-mail delivery (default 30 s).
	SMTPTimeout time.Duration
	// TLSConfig replaces the TLS settings for the mail server (tests; nil =
	// system roots with the server name set to SMTP.Host).
	TLSConfig *tls.Config
	Now       func() time.Time
}

func (e Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// Message is an event rendered for people.
type Message struct {
	Event notify.Event
	// Headline is one line ("Request approved: Big Buck Bunny (2008)").
	Headline string
	// Body is plain text (may be empty).
	Body string
	// URL links to the page in Locarr ("" without a base address).
	URL string
	// PosterURL is a public image URL ("" = none).
	PosterURL string
}

// Errors.
var (
	ErrInvalid     = errors.New("invalid channel settings")
	ErrUnsupported = errors.New("unknown channel kind")
)

// Sender delivers a message through one kind of service.
type Sender interface {
	Validate(cfg Config, sec Secrets) error
	Send(ctx context.Context, cfg Config, sec Secrets, env Env, msg Message) error
}
