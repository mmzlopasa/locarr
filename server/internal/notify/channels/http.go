package channels

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/notify"
)

// --- webhook ---

type webhookSender struct{}

// WebhookPayload is the JSON body a webhook receives.
type WebhookPayload struct {
	Event     notify.Type    `json:"event"`
	At        string         `json:"at"`
	Headline  string         `json:"headline"`
	Title     string         `json:"title"`
	Message   string         `json:"message"`
	URL       string         `json:"url"`
	Poster    string         `json:"poster"`
	MediaKind string         `json:"mediaKind"`
	RequestID int64          `json:"requestId,omitempty"`
	IssueID   int64          `json:"issueId,omitempty"`
	ItemRef   string         `json:"itemRef,omitempty"`
	Actor     *notify.User   `json:"actor,omitempty"`
	Params    map[string]any `json:"params"`
}

// SignatureHeader carries the HMAC-SHA256 of the raw body ("sha256=<hex>").
const SignatureHeader = "X-Locarr-Signature"

// Sign returns the signature header value for body.
func Sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func (webhookSender) Validate(cfg Config, _ Secrets) error {
	if err := checkURL("url", cfg.URL); err != nil {
		return err
	}
	switch strings.ToUpper(cfg.Method) {
	case "", http.MethodPost, http.MethodPut:
		return nil
	}
	return invalid("method must be POST or PUT")
}

func (webhookSender) Send(ctx context.Context, cfg Config, sec Secrets, env Env, msg Message) error {
	ev := msg.Event
	at := ev.At
	if at.IsZero() {
		at = env.now()
	}
	params := ev.Params
	if params == nil {
		params = map[string]any{}
	}
	body, err := json.Marshal(WebhookPayload{Event: ev.Type, At: at.UTC().Format(time.RFC3339), Headline: msg.Headline, Title: ev.Title,
		Message: ev.Message, URL: msg.URL, Poster: msg.PosterURL, MediaKind: ev.MediaKind, RequestID: ev.RequestID, IssueID: ev.IssueID,
		ItemRef: ev.ItemRef, Actor: ev.Actor, Params: params})
	if err != nil {
		return err
	}
	hdr := jsonHeader()
	hdr.Set("X-Locarr-Event", string(ev.Type))
	if sec.Secret != "" {
		hdr.Set(SignatureHeader, Sign(sec.Secret, body))
	}
	method := strings.ToUpper(cfg.Method)
	if method == "" {
		method = http.MethodPost
	}
	_, err = do(ctx, env, "webhook", method, cfg.URL, hdr, body, sec.List())
	return err
}

// --- ntfy (https://docs.ntfy.sh/publish/#publish-as-json) ---

type ntfySender struct{}

// DefaultNtfyServer is the public ntfy server.
const DefaultNtfyServer = "https://ntfy.sh"

func (ntfySender) Validate(cfg Config, _ Secrets) error {
	if cfg.URL != "" {
		if err := checkURL("url", cfg.URL); err != nil {
			return err
		}
	}
	if !ntfyTopic.MatchString(cfg.Topic) {
		return invalid("topic is required and may contain only letters, digits, - and _ (at most 64)")
	}
	if cfg.Priority != 0 {
		if err := checkPriority(cfg.Priority, 1, 5); err != nil {
			return err
		}
	}
	if len(cfg.Tags) > 20 {
		return invalid("at most 20 tags")
	}
	return nil
}

func (ntfySender) Send(ctx context.Context, cfg Config, sec Secrets, env Env, msg Message) error {
	server := strings.TrimRight(cfg.URL, "/")
	if server == "" {
		server = DefaultNtfyServer
	}
	p := map[string]any{"topic": cfg.Topic, "title": msg.Headline, "message": bodyOr(msg)}
	if cfg.Priority != 0 {
		p["priority"] = cfg.Priority
	}
	if len(cfg.Tags) > 0 {
		p["tags"] = cfg.Tags
	}
	if msg.URL != "" {
		p["click"] = msg.URL
	}
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	hdr := jsonHeader()
	switch {
	case sec.Token != "":
		hdr.Set("Authorization", "Bearer "+sec.Token)
	case cfg.Username != "":
		hdr.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(cfg.Username+":"+sec.Password)))
	}
	_, err = do(ctx, env, "ntfy", http.MethodPost, server+"/", hdr, body, sec.List())
	return err
}

// bodyOr returns the body, or the headline when there is no body (services
// that need a non-empty message).
func bodyOr(msg Message) string {
	if strings.TrimSpace(msg.Body) == "" {
		return msg.Headline
	}
	return msg.Body
}

// --- Gotify (https://gotify.net/docs/pushmsg, https://gotify.net/docs/msgextras) ---

type gotifySender struct{}

func (gotifySender) Validate(cfg Config, sec Secrets) error {
	if err := checkURL("url", cfg.URL); err != nil {
		return err
	}
	if sec.Token == "" {
		return invalid("application token is required")
	}
	return checkPriority(cfg.Priority, 0, 10)
}

func (gotifySender) Send(ctx context.Context, cfg Config, sec Secrets, env Env, msg Message) error {
	p := map[string]any{"title": msg.Headline, "message": bodyOr(msg), "priority": cfg.Priority}
	if msg.URL != "" {
		p["extras"] = map[string]any{"client::notification": map[string]any{"click": map[string]any{"url": msg.URL}}}
	}
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	hdr := jsonHeader()
	// The token goes in a header only: URLs end up in proxy and server logs.
	hdr.Set("X-Gotify-Key", sec.Token)
	_, err = do(ctx, env, "gotify", http.MethodPost, strings.TrimRight(cfg.URL, "/")+"/message", hdr, body, sec.List())
	return err
}

// --- Discord (https://discord.com/developers/docs/resources/webhook#execute-webhook) ---

type discordSender struct{}

func (discordSender) Validate(_ Config, sec Secrets) error {
	return checkURL("webhook URL", sec.WebhookURL)
}

func (discordSender) Send(ctx context.Context, cfg Config, sec Secrets, env Env, msg Message) error {
	name := cfg.Username
	if name == "" {
		name = "Locarr"
	}
	at := msg.Event.At
	if at.IsZero() {
		at = env.now()
	}
	embed := map[string]any{"title": truncate(msg.Headline, 256), "description": truncate(msg.Body, 4096),
		"color": color(msg.Event.Type), "timestamp": at.UTC().Format(time.RFC3339)}
	if msg.URL != "" {
		embed["url"] = msg.URL
	}
	if msg.PosterURL != "" {
		embed["thumbnail"] = map[string]any{"url": msg.PosterURL}
	}
	body, err := json.Marshal(map[string]any{"username": truncate(name, 80), "embeds": []any{embed}})
	if err != nil {
		return err
	}
	_, err = do(ctx, env, "discord", http.MethodPost, sec.WebhookURL, jsonHeader(), body, sec.List())
	return err
}

// --- Telegram (https://core.telegram.org/bots/api#sendmessage) ---

type telegramSender struct{}

// DefaultTelegramAPI is the Bot API address.
const DefaultTelegramAPI = "https://api.telegram.org"

func (telegramSender) Validate(cfg Config, sec Secrets) error {
	if sec.BotToken == "" || strings.ContainsAny(sec.BotToken, "/?# ") {
		return invalid("bot token is required")
	}
	if strings.TrimSpace(cfg.ChatID) == "" {
		return invalid("chat id is required")
	}
	return nil
}

// markdownV2Special are the characters MarkdownV2 needs escaped in text.
const markdownV2Special = "_*[]()~`>#+-=|{}.!\\"

// EscapeMarkdownV2 escapes text for Telegram's MarkdownV2.
func EscapeMarkdownV2(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(markdownV2Special, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeMarkdownV2URL escapes the URL part of an inline link.
func escapeMarkdownV2URL(s string) string {
	return strings.NewReplacer(`\`, `\\`, `)`, `\)`).Replace(s)
}

// TelegramText is the MarkdownV2 text of a message.
func TelegramText(msg Message, lang string) string {
	var b strings.Builder
	b.WriteString("*" + EscapeMarkdownV2(msg.Headline) + "*")
	if strings.TrimSpace(msg.Body) != "" {
		b.WriteString("\n\n" + EscapeMarkdownV2(msg.Body))
	}
	if msg.URL != "" {
		b.WriteString("\n\n[" + EscapeMarkdownV2(text(lang, "open")) + "](" + escapeMarkdownV2URL(msg.URL) + ")")
	}
	return b.String()
}

func (telegramSender) Send(ctx context.Context, cfg Config, sec Secrets, env Env, msg Message) error {
	base := strings.TrimRight(env.TelegramAPI, "/")
	if base == "" {
		base = DefaultTelegramAPI
	}
	body, err := json.Marshal(map[string]any{"chat_id": strings.TrimSpace(cfg.ChatID), "text": truncate(TelegramText(msg, cfg.Language), 4096),
		"parse_mode": "MarkdownV2", "disable_web_page_preview": true})
	if err != nil {
		return err
	}
	res, err := do(ctx, env, "telegram", http.MethodPost, base+"/bot"+sec.BotToken+"/sendMessage", jsonHeader(), body, sec.List())
	var ans struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if res != nil {
		_ = json.Unmarshal(res.Body, &ans)
	}
	if err != nil {
		if ans.Description != "" {
			return fmt.Errorf("telegram: HTTP %d: %s", res.Status, excerpt([]byte(ans.Description), sec.List()))
		}
		return err
	}
	if !ans.OK {
		d := ans.Description
		if d == "" {
			d = "the answer was not ok"
		}
		return fmt.Errorf("telegram: %s", excerpt([]byte(d), sec.List()))
	}
	return nil
}

// --- Pushover (https://pushover.net/api) ---

type pushoverSender struct{}

// DefaultPushoverAPI is the Pushover API address.
const DefaultPushoverAPI = "https://api.pushover.net"

func (pushoverSender) Validate(cfg Config, sec Secrets) error {
	if sec.Token == "" {
		return invalid("application token is required")
	}
	if sec.UserKey == "" {
		return invalid("user key is required")
	}
	return checkPriority(cfg.Priority, -2, 2)
}

func (pushoverSender) Send(ctx context.Context, cfg Config, sec Secrets, env Env, msg Message) error {
	base := strings.TrimRight(env.PushoverAPI, "/")
	if base == "" {
		base = DefaultPushoverAPI
	}
	f := url.Values{"token": {sec.Token}, "user": {sec.UserKey}, "title": {truncate(msg.Headline, 250)},
		"message": {truncate(bodyOr(msg), 1024)}}
	if msg.URL != "" {
		f.Set("url", msg.URL)
		f.Set("url_title", "Locarr")
	}
	if cfg.Priority != 0 {
		f.Set("priority", strconv.Itoa(cfg.Priority))
	}
	if cfg.Sound != "" {
		f.Set("sound", cfg.Sound)
	}
	if cfg.Device != "" {
		f.Set("device", cfg.Device)
	}
	hdr := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}, "Accept": {"application/json"}}
	res, err := do(ctx, env, "pushover", http.MethodPost, base+"/1/messages.json", hdr, []byte(f.Encode()), sec.List())
	var ans struct {
		Status int      `json:"status"`
		Errors []string `json:"errors"`
	}
	if res != nil {
		_ = json.Unmarshal(res.Body, &ans)
	}
	if err != nil {
		if len(ans.Errors) > 0 {
			return fmt.Errorf("pushover: HTTP %d: %s", res.Status, excerpt([]byte(strings.Join(ans.Errors, "; ")), sec.List()))
		}
		return err
	}
	if ans.Status != 1 {
		reason := strings.Join(ans.Errors, "; ")
		if reason == "" {
			reason = "the message was not accepted"
		}
		return fmt.Errorf("pushover: %s", excerpt([]byte(reason), sec.List()))
	}
	return nil
}

// --- Apprise API (https://github.com/caronc/apprise-api#api-details) ---

type appriseSender struct{}

func (appriseSender) Validate(cfg Config, _ Secrets) error {
	return checkURL("url", cfg.URL)
}

func (appriseSender) Send(ctx context.Context, cfg Config, sec Secrets, env Env, msg Message) error {
	p := map[string]any{"title": msg.Headline, "body": bodyOr(msg), "type": severity(msg.Event.Type), "format": "text"}
	if cfg.Tag != "" {
		p["tag"] = cfg.Tag
	}
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = do(ctx, env, "apprise", http.MethodPost, cfg.URL, jsonHeader(), body, sec.List())
	return err
}

// severity is the Apprise notification type of an event.
func severity(t notify.Type) string {
	switch t {
	case notify.RequestAvailable, notify.LibraryImported, notify.LibraryUpgraded, notify.IssueResolved, notify.RequestApproved:
		return "success"
	case notify.RequestFailed, notify.LibraryDownloadFailed, notify.LibraryImportFailed:
		return "failure"
	case notify.RequestDeclined, notify.HealthWarning, notify.IssueCreated, notify.IssueReopened:
		return "warning"
	}
	return "info"
}

// color is the Discord embed colour of an event.
func color(t notify.Type) int {
	switch severity(t) {
	case "success":
		return 0x2ea043
	case "failure":
		return 0xd1242f
	case "warning":
		return 0xd29922
	}
	return 0x3b82f6
}
