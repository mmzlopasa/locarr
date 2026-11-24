// Package delivery keeps the outward notification channels (shared ones
// chosen by administrators and users' own ones), picks the channels for each
// event and sends through them with retries (D-070). It is the notify.Sink
// that the hub calls; sending happens in jobs, never on the caller's path.
package delivery

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/notify"
	"github.com/mmzlopasa/locarr/server/internal/notify/channels"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Settings keys.
var (
	BaseURLKey   = settings.Key[string]{Name: "notifications.baseUrl", Default: ""}
	SMTPKey      = settings.Key[channels.SMTP]{Name: "notifications.smtp", Default: channels.SMTP{Security: "starttls", Port: 587}}
	SMTPPassword = settings.SecretKey{Name: "notifications.smtpPassword"}
	// SubtitleMissingDays: when subtitles.missing is sent for a file.
	SubtitleMissingDays = settings.Key[int]{Name: "notifications.subtitleMissingDays", Default: 7}
)

// TaskDeliver sends one event through one channel.
const TaskDeliver = "notify-deliver"

// deliverAttempts: the first try and retries after ~10 s, 20 s, 40 s, 80 s.
const deliverAttempts = 5

// Errors.
var (
	ErrNotFound      = errors.New("notification channel not found")
	ErrInvalid       = errors.New("invalid notification channel")
	ErrKindForbidden = errors.New("personal channels cannot be webhooks or Apprise")
	ErrEventNotAllow = errors.New("personal channels only receive request and issue events")
)

// Channel is a stored channel.
type Channel struct {
	ID          int64
	UserID      int64 // 0 = shared
	Name        string
	Kind        channels.Kind
	Enabled     bool
	Events      []notify.Type
	Config      channels.Config
	Secrets     channels.Secrets
	Failures    int
	LastError   string
	LastErrorAt *time.Time
	LastSentAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Personal reports whether the channel belongs to a user.
func (c Channel) Personal() bool { return c.UserID > 0 }

// Wants reports whether the channel takes events of type t.
func (c Channel) Wants(t notify.Type) bool {
	for _, e := range c.Events {
		if e == t {
			return true
		}
	}
	return false
}

// Service keeps and uses the channels.
type Service struct {
	DB       *db.DB
	Box      *secrets.Box
	Settings *settings.Store
	Jobs     *jobs.Queue
	Log      *slog.Logger
	Now      func() time.Time
	// Policy is the outbound address policy for shared channels and
	// administrators' own (the indexers' policy: LAN allowed).
	Policy func(ctx context.Context) (httpx.Policy, error)
	// PersonalPolicy is used for other users' own channels (default: no
	// private, loopback or link-local addresses).
	PersonalPolicy func(ctx context.Context) (httpx.Policy, error)
	// TelegramAPI, PushoverAPI and TLSConfig replace the services (tests).
	TelegramAPI, PushoverAPI string
	TLSConfig                *tls.Config
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Register adds the delivery job.
func (s *Service) Register() {
	s.Jobs.Register(TaskDeliver, s.runDeliver, jobs.KindOptions{MaxAttempts: deliverAttempts, Timeout: 2 * time.Minute})
}

// --- storage ---

const channelColumns = `id, COALESCE(user_id, 0), name, kind, enabled, events, settings, secrets, failures, last_error, last_error_at,
	last_sent_at, created_at, updated_at`

func (s *Service) scan(row interface{ Scan(...any) error }) (Channel, error) {
	var (
		c                 Channel
		kind, ev, cfg     string
		sealed            []byte
		lastErr, lastSent sql.NullInt64
		created, updated  int64
	)
	if err := row.Scan(&c.ID, &c.UserID, &c.Name, &kind, &c.Enabled, &ev, &cfg, &sealed, &c.Failures, &c.LastError, &lastErr, &lastSent,
		&created, &updated); err != nil {
		return c, err
	}
	c.Kind = channels.Kind(kind)
	_ = json.Unmarshal([]byte(ev), &c.Events)
	_ = json.Unmarshal([]byte(cfg), &c.Config)
	if c.Events == nil {
		c.Events = []notify.Type{}
	}
	if len(sealed) > 0 {
		raw, err := s.Box.Open(sealed, aad(c.ID))
		if err != nil {
			return c, err
		}
		_ = json.Unmarshal(raw, &c.Secrets)
	}
	c.LastErrorAt, c.LastSentAt = db.NullTime(lastErr), db.NullTime(lastSent)
	c.CreatedAt, c.UpdatedAt = db.Time(created), db.Time(updated)
	return c, nil
}

func aad(id int64) string { return "notification-channel:" + strconv.FormatInt(id, 10) }

// List returns the shared channels (userID 0) or a user's own.
func (s *Service) List(ctx context.Context, userID int64) ([]Channel, error) {
	q, args := `SELECT `+channelColumns+` FROM notification_channels WHERE user_id IS NULL ORDER BY name, id`, []any{}
	if userID > 0 {
		q, args = `SELECT `+channelColumns+` FROM notification_channels WHERE user_id = ? ORDER BY name, id`, []any{userID}
	}
	rows, err := s.DB.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		c, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Get returns one channel.
func (s *Service) Get(ctx context.Context, id int64) (Channel, error) {
	c, err := s.scan(s.DB.R.QueryRowContext(ctx, `SELECT `+channelColumns+` FROM notification_channels WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// SecretsPatch changes secrets: nil keeps a value, "" removes it.
type SecretsPatch struct {
	Token, Secret, BotToken, WebhookURL, UserKey, Password *string
}

// Apply returns sec with the patch applied.
func (p SecretsPatch) Apply(sec channels.Secrets) channels.Secrets {
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	set(&sec.Token, p.Token)
	set(&sec.Secret, p.Secret)
	set(&sec.BotToken, p.BotToken)
	set(&sec.WebhookURL, p.WebhookURL)
	set(&sec.UserKey, p.UserKey)
	set(&sec.Password, p.Password)
	return sec
}

// Input is a channel to create or change.
type Input struct {
	Name    string
	Kind    channels.Kind
	Enabled bool
	Events  []notify.Type
	Config  channels.Config
	Secrets SecretsPatch
}

// Owner is who a channel is for: a user (personal) or nobody (shared).
type Owner struct {
	UserID int64 // 0 = shared
	Admin  bool  // the owner is an administrator (personal channels only)
}

func validEvent(t notify.Type) bool {
	return notify.Personal(t) || t == notify.Test || notify.IsAdult(t) || func() bool {
		for _, x := range notify.LibraryTypes {
			if x == t {
				return true
			}
		}
		return false
	}()
}

// check validates an input for its owner and returns the clean events.
func check(o Owner, in Input, sec channels.Secrets) ([]notify.Type, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, errors.Join(ErrInvalid, errors.New("a name is required"))
	}
	if _, err := channels.Get(in.Kind); err != nil {
		return nil, errors.Join(ErrInvalid, err)
	}
	if o.UserID > 0 && !o.Admin && (in.Kind == channels.Webhook || in.Kind == channels.Apprise) {
		return nil, ErrKindForbidden
	}
	seen := map[notify.Type]bool{}
	events := []notify.Type{}
	for _, e := range in.Events {
		if !validEvent(e) || e == notify.Test {
			return nil, errors.Join(ErrInvalid, errors.New("unknown event "+string(e)))
		}
		if o.UserID > 0 && !notify.Personal(e) {
			return nil, ErrEventNotAllow
		}
		if !seen[e] {
			seen[e] = true
			events = append(events, e)
		}
	}
	if err := channels.Validate(in.Kind, in.Config, sec); err != nil {
		return nil, err
	}
	return events, nil
}

// Create stores a new channel.
func (s *Service) Create(ctx context.Context, o Owner, in Input) (Channel, error) {
	sec := in.Secrets.Apply(channels.Secrets{})
	events, err := check(o, in, sec)
	if err != nil {
		return Channel{}, err
	}
	ev, _ := json.Marshal(events)
	cfg, _ := json.Marshal(in.Config)
	now := db.Millis(s.now())
	var owner any
	if o.UserID > 0 {
		owner = o.UserID
	}
	var id int64
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO notification_channels (user_id, name, kind, enabled, events, settings, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`, owner, strings.TrimSpace(in.Name), string(in.Kind), in.Enabled, string(ev), string(cfg),
			now, now).Scan(&id); err != nil {
			return err
		}
		return s.storeSecrets(ctx, tx, id, sec)
	})
	if err != nil {
		return Channel{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) storeSecrets(ctx context.Context, q db.Querier, id int64, sec channels.Secrets) error {
	var sealed []byte
	if len(sec.Names()) > 0 {
		raw, _ := json.Marshal(sec) // #nosec G117 -- sealed with the secret box right below
		var err error
		if sealed, err = s.Box.Seal(raw, aad(id)); err != nil {
			return err
		}
	}
	_, err := q.ExecContext(ctx, `UPDATE notification_channels SET secrets = ? WHERE id = ?`, sealed, id)
	return err
}

// Update changes a channel (its owner stays).
func (s *Service) Update(ctx context.Context, id int64, ownerAdmin bool, in Input) (Channel, error) {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return cur, err
	}
	sec := in.Secrets.Apply(cur.Secrets)
	events, err := check(Owner{UserID: cur.UserID, Admin: ownerAdmin}, in, sec)
	if err != nil {
		return cur, err
	}
	ev, _ := json.Marshal(events)
	cfg, _ := json.Marshal(in.Config)
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE notification_channels SET name = ?, kind = ?, enabled = ?, events = ?, settings = ?, updated_at = ?,
			failures = CASE WHEN kind = ? THEN failures ELSE 0 END WHERE id = ?`, strings.TrimSpace(in.Name), string(in.Kind), in.Enabled,
			string(ev), string(cfg), db.Millis(s.now()), string(in.Kind), id); err != nil {
			return err
		}
		return s.storeSecrets(ctx, tx, id, sec)
	})
	if err != nil {
		return cur, err
	}
	return s.Get(ctx, id)
}

// Delete removes a channel.
func (s *Service) Delete(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM notification_channels WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- sending ---

// Name implements notify.Sink.
func (s *Service) Name() string { return "channels" }

type deliverArgs struct {
	Channel int64        `json:"channel"`
	Event   notify.Event `json:"event"`
}

// Deliver implements notify.Sink: it queues a send for each channel that
// wants the event. Shared channels get every chosen event; a user's own
// channel only events addressed to that user.
func (s *Service) Deliver(ctx context.Context, ev notify.Event) error {
	if ev.Type == notify.Test {
		return nil
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, COALESCE(user_id, 0), events FROM notification_channels WHERE enabled = 1`)
	if err != nil {
		return err
	}
	type target struct{ id int64 }
	var to []target
	rec := map[int64]bool{}
	for _, r := range ev.Recipients {
		rec[r] = true
	}
	for rows.Next() {
		var (
			id, uid int64
			raw     string
			events  []notify.Type
		)
		if err := rows.Scan(&id, &uid, &raw); err != nil {
			_ = rows.Close()
			return err
		}
		_ = json.Unmarshal([]byte(raw), &events)
		c := Channel{Events: events}
		if !c.Wants(ev.Type) {
			continue
		}
		if uid > 0 && (!rec[uid] || !notify.Personal(ev.Type)) {
			continue
		}
		to = append(to, target{id})
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var errs []error
	for _, t := range to {
		if _, _, err := s.Jobs.Enqueue(ctx, TaskDeliver, deliverArgs{Channel: t.id, Event: ev}, jobs.EnqueueOptions{}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) runDeliver(ctx context.Context, j *jobs.Job) error {
	var a deliverArgs
	if err := j.Decode(&a); err != nil {
		return jobs.Permanent(err)
	}
	c, err := s.Get(ctx, a.Channel)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !c.Enabled {
		return nil
	}
	err = s.send(ctx, c.Kind, c.Config, c.Secrets, c.UserID, a.Event)
	now := db.Millis(s.now())
	if err == nil {
		_, uerr := s.DB.W.ExecContext(ctx, `UPDATE notification_channels SET failures = 0, last_sent_at = ? WHERE id = ?`, now, c.ID)
		return uerr
	}
	msg := err.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	if _, uerr := s.DB.W.ExecContext(ctx, `UPDATE notification_channels SET failures = failures + 1, last_error = ?, last_error_at = ? WHERE id = ?`,
		msg, now, c.ID); uerr != nil {
		s.log().Warn("record channel failure", "channel", c.ID, "err", uerr)
	}
	if errors.Is(err, channels.ErrInvalid) {
		return jobs.Permanent(err)
	}
	return err
}

// Env builds the sending environment for a channel owned by userID (0 = shared).
func (s *Service) env(ctx context.Context, userID int64, sec channels.Secrets) (channels.Env, string, error) {
	base, err := settings.Get(ctx, s.Settings, BaseURLKey)
	if err != nil {
		return channels.Env{}, "", err
	}
	smtp, err := settings.Get(ctx, s.Settings, SMTPKey)
	if err != nil {
		return channels.Env{}, "", err
	}
	if smtp.Password, _, err = s.Settings.Secret(ctx, SMTPPassword); err != nil {
		return channels.Env{}, "", err
	}
	lang := "en"
	admin := userID == 0
	if userID > 0 {
		var role string
		if err := s.DB.R.QueryRowContext(ctx, `SELECT role, language FROM users WHERE id = ?`, userID).Scan(&role, &lang); err != nil {
			return channels.Env{}, "", err
		}
		admin = role == "admin"
	}
	var pol httpx.Policy
	switch {
	case admin && s.Policy != nil:
		pol, err = s.Policy(ctx)
	case !admin && s.PersonalPolicy != nil:
		pol, err = s.PersonalPolicy(ctx)
	case !admin:
		pol = httpx.Policy{BlockPrivate: true}
	}
	if err != nil {
		return channels.Env{}, "", err
	}
	hc, err := httpx.New(httpx.Options{Policy: pol, Timeout: 20 * time.Second, MaxBytes: 1 << 20, Secrets: sec.List()})
	if err != nil {
		return channels.Env{}, "", err
	}
	return channels.Env{HTTP: hc, SMTP: smtp, BaseURL: base, TelegramAPI: s.TelegramAPI, PushoverAPI: s.PushoverAPI,
		TLSConfig: s.TLSConfig, Now: s.Now}, lang, nil
}

func (s *Service) send(ctx context.Context, kind channels.Kind, cfg channels.Config, sec channels.Secrets, userID int64, ev notify.Event) error {
	env, lang, err := s.env(ctx, userID, sec)
	if err != nil {
		return err
	}
	if cfg.Language != "" {
		lang = cfg.Language
	}
	return channels.Send(ctx, kind, cfg, sec, env, channels.Compose(ev, lang, env.BaseURL))
}

// Test sends a test message through a configuration (secrets left out are
// taken from channel id when it belongs to the same owner).
func (s *Service) Test(ctx context.Context, o Owner, id int64, in Input) error {
	var base channels.Secrets
	if id > 0 {
		c, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		if c.UserID != o.UserID {
			return ErrNotFound
		}
		base = c.Secrets
	}
	sec := in.Secrets.Apply(base)
	if _, err := check(o, Input{Name: "test", Kind: in.Kind, Config: in.Config}, sec); err != nil {
		return err
	}
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.send(tctx, in.Kind, in.Config, sec, o.UserID, notify.Event{Type: notify.Test, At: s.now(), Title: "Locarr"})
}

// --- settings ---

// MailInput changes the mail server; Password nil keeps it.
type MailInput struct {
	SMTP     channels.SMTP
	Password *string
}

// SaveSettings stores the address for links and the mail server.
func (s *Service) SaveSettings(ctx context.Context, baseURL *string, mail *MailInput) error {
	if baseURL != nil {
		b := strings.TrimRight(strings.TrimSpace(*baseURL), "/")
		if b != "" && !strings.HasPrefix(b, "http://") && !strings.HasPrefix(b, "https://") {
			return errors.Join(ErrInvalid, errors.New("the address for links must start with http:// or https://"))
		}
		if err := settings.Set(ctx, s.Settings, nil, BaseURLKey, b); err != nil {
			return err
		}
	}
	if mail != nil {
		m := mail.SMTP
		m.Host, m.From, m.Username = strings.TrimSpace(m.Host), strings.TrimSpace(m.From), strings.TrimSpace(m.Username)
		switch m.Security {
		case "none", "starttls", "tls":
		case "":
			m.Security = "starttls"
		default:
			return errors.Join(ErrInvalid, errors.New("security must be none, starttls or tls"))
		}
		if strings.ContainsAny(m.Host+m.From+m.Username, "\r\n") {
			return errors.Join(ErrInvalid, errors.New("line breaks are not allowed"))
		}
		m.Password = ""
		if err := settings.Set(ctx, s.Settings, nil, SMTPKey, m); err != nil {
			return err
		}
		if mail.Password != nil {
			if err := s.Settings.SetSecret(ctx, SMTPPassword, *mail.Password); err != nil {
				return err
			}
		}
	}
	return nil
}

// LoadSettings returns the address for links, the mail server and whether its password is set.
func (s *Service) LoadSettings(ctx context.Context) (string, channels.SMTP, bool, error) {
	base, err := settings.Get(ctx, s.Settings, BaseURLKey)
	if err != nil {
		return "", channels.SMTP{}, false, err
	}
	smtp, err := settings.Get(ctx, s.Settings, SMTPKey)
	if err != nil {
		return "", channels.SMTP{}, false, err
	}
	set, err := s.Settings.Configured(ctx, SMTPPassword)
	return base, smtp, set, err
}
