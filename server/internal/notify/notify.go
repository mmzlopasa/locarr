// Package notify carries what happened to requests and issues to the people
// who should know (D-057). Every event is stored as an in-app notification for
// each recipient (the bell in the web client, GET /notifications for the
// apps); further sinks (P9: webhooks, ntfy, Gotify, Discord, Telegram, e-mail,
// Streamero) receive the same Event.
package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// Type is an event type. The values are stored and part of the API: never rename them.
type Type string

// Event types.
const (
	// RequestCreated: someone asked for a title (to the users who manage requests).
	RequestCreated Type = "request.created"
	// RequestApproved, RequestDeclined (with Message = reason): to the requester.
	RequestApproved Type = "request.approved"
	RequestDeclined Type = "request.declined"
	// RequestAvailable: everything asked for is in the library; RequestPartial:
	// some of it (seasons, tracks). To the requester.
	RequestAvailable Type = "request.available"
	RequestPartial   Type = "request.partial"
	// RequestFailed: the library gave up for now (Message = the explainer's
	// reason, Params.code its code). To the requester and the managers.
	RequestFailed Type = "request.failed"
	// RequestComment: a new comment (Message = text). To the other side.
	RequestComment Type = "request.comment"
	// IssueCreated: a problem was reported (to the managers).
	IssueCreated Type = "issue.created"
	// IssueComment, IssueResolved, IssueReopened: to the reporter (and the
	// managers for comments and reopening).
	IssueComment  Type = "issue.comment"
	IssueResolved Type = "issue.resolved"
	IssueReopened Type = "issue.reopened"
)

// Library and system events (P9). They have no in-app recipients and only
// reach the shared notification channels.
const (
	// LibraryGrabbed: a release was sent to a download client (Message = release name).
	LibraryGrabbed Type = "library.grabbed"
	// LibraryImported, LibraryUpgraded: a file arrived in the library.
	LibraryImported Type = "library.imported"
	LibraryUpgraded Type = "library.upgraded"
	// LibraryDownloadFailed, LibraryImportFailed: Message = the reason.
	LibraryDownloadFailed Type = "library.download_failed"
	LibraryImportFailed   Type = "library.import_failed"
	// HealthWarning: an indexer, download client or Streamero stopped working.
	HealthWarning Type = "health.warning"
	// SubtitleDownloaded: a subtitle was found or upgraded (Message = what).
	SubtitleDownloaded Type = "subtitles.downloaded"
	// SubtitleMissing: a file still has no subtitle in a wanted language
	// some days after it arrived (sent once per file).
	SubtitleMissing Type = "subtitles.missing"
	// MigrateFinished, MigrateFailed: an import of an existing setup (P10)
	// ended (Message = the counts or the reason).
	MigrateFinished Type = "migrate.finished"
	MigrateFailed   Type = "migrate.failed"
	// Adult library events (D-100): only shared channels that chose them
	// explicitly receive them, and only while the adult library is enabled.
	AdultGrabbed        Type = "adult.grabbed"
	AdultImported       Type = "adult.imported"
	AdultUpgraded       Type = "adult.upgraded"
	AdultDownloadFailed Type = "adult.download_failed"
	AdultImportFailed   Type = "adult.import_failed"
	// Test is only sent by a channel's Test button.
	Test Type = "test"
)

// Types lists every request and issue event type.
var Types = []Type{RequestCreated, RequestApproved, RequestDeclined, RequestAvailable, RequestPartial, RequestFailed,
	RequestComment, IssueCreated, IssueComment, IssueResolved, IssueReopened}

// LibraryTypes lists the library and system event types.
var LibraryTypes = []Type{LibraryGrabbed, LibraryImported, LibraryUpgraded, LibraryDownloadFailed, LibraryImportFailed, HealthWarning,
	SubtitleDownloaded, SubtitleMissing, MigrateFinished, MigrateFailed}

// AdultTypes lists the adult library event types (never chosen by default).
var AdultTypes = []Type{AdultGrabbed, AdultImported, AdultUpgraded, AdultDownloadFailed, AdultImportFailed}

// IsAdult reports an adult event type.
func IsAdult(t Type) bool {
	for _, x := range AdultTypes {
		if x == t {
			return true
		}
	}
	return false
}

// Personal reports whether t is about requests or issues (the events a
// user's own channel can receive).
func Personal(t Type) bool {
	for _, x := range Types {
		if x == t {
			return true
		}
	}
	return false
}

// User names a person in an event.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// Event is one thing that happened. It carries everything a sink needs to
// write a message without asking the database again.
type Event struct {
	Type Type      `json:"type"`
	At   time.Time `json:"at"`
	// Recipients are the users told in-app (never the actor themselves).
	Recipients []int64 `json:"recipients"`
	Actor      *User   `json:"actor,omitempty"`
	// Title is the title the event is about ("Big Buck Bunny (2008)").
	Title string `json:"title"`
	// Message is the decline reason, comment text or failure reason.
	Message string `json:"message,omitempty"`
	// Poster is a TMDb image file or Cover Art file name; MediaKind is movie,
	// series, album or artist.
	Poster    string `json:"poster,omitempty"`
	MediaKind string `json:"mediaKind,omitempty"`
	RequestID int64  `json:"requestId,omitempty"`
	IssueID   int64  `json:"issueId,omitempty"`
	// ItemRef is the library item of a library event ("movie:42").
	ItemRef string `json:"itemRef,omitempty"`
	// Params are extra facts (seasons, is4k, code, autoApproved, …).
	Params map[string]any `json:"params,omitempty"`
}

// Sink receives every event after it is stored in-app. Deliver must not
// block for long; errors are logged and never undo anything.
type Sink interface {
	Name() string
	Deliver(ctx context.Context, ev Event) error
}

// Hub stores events as notifications and hands them to the sinks.
type Hub struct {
	DB    *db.DB
	Sinks []Sink
	Log   *slog.Logger
	Now   func() time.Time
}

func (h *Hub) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *Hub) log() *slog.Logger {
	if h.Log != nil {
		return h.Log
	}
	return slog.Default()
}

// Publish records ev for its recipients and passes it to the sinks.
func (h *Hub) Publish(ctx context.Context, ev Event) {
	if ev.At.IsZero() {
		ev.At = h.now()
	}
	seen := map[int64]bool{}
	var to []int64
	for _, id := range ev.Recipients {
		if id <= 0 || seen[id] || (ev.Actor != nil && ev.Actor.ID == id) {
			continue
		}
		seen[id] = true
		to = append(to, id)
	}
	ev.Recipients = to
	params := "{}"
	if len(ev.Params) > 0 {
		if b, err := json.Marshal(ev.Params); err == nil {
			params = string(b)
		}
	}
	var actor any
	if ev.Actor != nil {
		actor = ev.Actor.ID
	}
	err := h.DB.Tx(ctx, func(tx *sql.Tx) error {
		for _, uid := range to {
			if _, err := tx.ExecContext(ctx, `INSERT INTO notifications (user_id, type, title, message, poster, media_kind, actor_id,
				request_id, issue_id, params, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, uid, string(ev.Type), ev.Title,
				truncate(ev.Message, 2000), ev.Poster, ev.MediaKind, actor, nullID(ev.RequestID), nullID(ev.IssueID), params,
				db.Millis(ev.At)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		h.log().Warn("store notification", "type", ev.Type, "err", err)
	}
	for _, s := range h.Sinks {
		if err := s.Deliver(ctx, ev); err != nil {
			h.log().Warn("notification sink", "sink", s.Name(), "type", ev.Type, "err", err)
		}
	}
}

func nullID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// Notification is a stored in-app notification.
type Notification struct {
	ID        int64
	Type      Type
	Title     string
	Message   string
	Poster    string
	MediaKind string
	Actor     *User
	RequestID int64
	IssueID   int64
	Params    map[string]any
	CreatedAt time.Time
	Read      bool
}

// ListOptions filter a user's notifications.
type ListOptions struct {
	// Since returns only notifications with a larger id, oldest first (polling).
	Since      int64
	UnreadOnly bool
	Limit      int
}

// Page is a list answer.
type Page struct {
	Items    []Notification
	Unread   int
	LatestID int64
}

// List returns a user's notifications: newest first, or with Since the
// newer ones oldest first.
func (h *Hub) List(ctx context.Context, userID int64, o ListOptions) (Page, error) {
	if o.Limit <= 0 || o.Limit > 200 {
		o.Limit = 50
	}
	q := `SELECT n.id, n.type, n.title, n.message, n.poster, n.media_kind, n.actor_id, COALESCE(u.username, ''), COALESCE(n.request_id, 0),
		COALESCE(n.issue_id, 0), n.params, n.created_at, n.read_at IS NOT NULL
		FROM notifications n LEFT JOIN users u ON u.id = n.actor_id WHERE n.user_id = ?`
	args := []any{userID}
	if o.UnreadOnly {
		q += ` AND n.read_at IS NULL`
	}
	if o.Since > 0 {
		q += ` AND n.id > ? ORDER BY n.id ASC LIMIT ?`
		args = append(args, o.Since, o.Limit)
	} else {
		q += ` ORDER BY n.id DESC LIMIT ?`
		args = append(args, o.Limit)
	}
	rows, err := h.DB.R.QueryContext(ctx, q, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	p := Page{Items: []Notification{}}
	for rows.Next() {
		var (
			n       Notification
			typ     string
			actor   sql.NullInt64
			name    string
			params  string
			created int64
		)
		if err := rows.Scan(&n.ID, &typ, &n.Title, &n.Message, &n.Poster, &n.MediaKind, &actor, &name, &n.RequestID, &n.IssueID, &params,
			&created, &n.Read); err != nil {
			return p, err
		}
		n.Type, n.CreatedAt = Type(typ), db.Time(created)
		if actor.Valid {
			n.Actor = &User{ID: actor.Int64, Username: name}
		}
		_ = json.Unmarshal([]byte(params), &n.Params)
		if n.Params == nil {
			n.Params = map[string]any{}
		}
		p.Items = append(p.Items, n)
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	if err := h.DB.R.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE((SELECT MAX(id) FROM notifications WHERE user_id = ?), 0)
		FROM notifications WHERE user_id = ? AND read_at IS NULL`, userID, userID).Scan(&p.Unread, &p.LatestID); err != nil {
		return p, err
	}
	return p, nil
}

// ErrNothingToMark is returned when neither ids nor all were given.
var ErrNothingToMark = errors.New("give ids or all")

// MarkRead marks some (or all) of a user's notifications read and returns
// how many stay unread.
func (h *Hub) MarkRead(ctx context.Context, userID int64, ids []int64, all bool) (int, error) {
	now := db.Millis(h.now())
	switch {
	case all:
		if _, err := h.DB.W.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL`, now, userID); err != nil {
			return 0, err
		}
	case len(ids) > 0:
		ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		args := []any{now, userID}
		for _, id := range ids {
			args = append(args, id)
		}
		if _, err := h.DB.W.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL AND id IN (`+ph+`)`, // #nosec G202 -- placeholders only
			args...); err != nil {
			return 0, err
		}
	default:
		return 0, ErrNothingToMark
	}
	var n int
	err := h.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

// Prune removes read notifications older than before (housekeeping).
func (h *Hub) Prune(ctx context.Context, before time.Time) (int64, error) {
	res, err := h.DB.W.ExecContext(ctx, `DELETE FROM notifications WHERE read_at IS NOT NULL AND created_at < ?`, db.Millis(before))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
