package requests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/notify"
)

// Issue types and statuses.
const (
	IssueVideo     = "video"
	IssueAudio     = "audio"
	IssueSubtitles = "subtitles"
	IssueOther     = "other"

	IssueOpen     = "open"
	IssueResolved = "resolved"
)

// ErrIssueNotFound is returned for an unknown (or someone else's) issue.
var ErrIssueNotFound = errors.New("issue not found")

// Issue is a reported problem with an available title.
type Issue struct {
	ID                  int64
	Kind                Kind // movie, series, album
	Type                string
	Status              string
	ItemRef             string
	TMDbID              int64
	MBID                string
	Title               string
	Poster              string
	Season, Episode     *int
	Message             string
	ReportedBy          notify.User
	ResolvedBy          *notify.User
	ResolvedAt          *time.Time
	ReplacementSearched bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
	CommentCount        int
}

const issueColumns = `i.id, i.kind, i.type, i.status, i.item_ref, i.tmdb_id, i.mbid, i.title, i.poster, i.season_number, i.episode_number,
	i.message, i.reported_by, ru.username, i.resolved_by, COALESCE(vu.username, ''), i.resolved_at, i.replacement_searched, i.created_at,
	i.updated_at, (SELECT COUNT(*) FROM issue_comments c WHERE c.issue_id = i.id)
	FROM issues i JOIN users ru ON ru.id = i.reported_by LEFT JOIN users vu ON vu.id = i.resolved_by`

func scanIssue(row interface{ Scan(...any) error }) (Issue, error) {
	var (
		is                        Issue
		kind                      string
		season, episode, resolver sql.NullInt64
		resolverName              string
		resolvedAt                sql.NullInt64
		created, updated          int64
	)
	err := row.Scan(&is.ID, &kind, &is.Type, &is.Status, &is.ItemRef, &is.TMDbID, &is.MBID, &is.Title, &is.Poster, &season, &episode,
		&is.Message, &is.ReportedBy.ID, &is.ReportedBy.Username, &resolver, &resolverName, &resolvedAt, &is.ReplacementSearched, &created,
		&updated, &is.CommentCount)
	if err != nil {
		return is, err
	}
	is.Kind = Kind(kind)
	if season.Valid {
		n := int(season.Int64)
		is.Season = &n
	}
	if episode.Valid {
		n := int(episode.Int64)
		is.Episode = &n
	}
	if resolver.Valid {
		is.ResolvedBy = &notify.User{ID: resolver.Int64, Username: resolverName}
	}
	is.ResolvedAt, is.CreatedAt, is.UpdatedAt = db.NullTime(resolvedAt), db.Time(created), db.Time(updated)
	return is, nil
}

// GetIssue returns an issue p may see.
func (s *Service) GetIssue(ctx context.Context, p auth.Principal, id int64) (Issue, error) {
	is, err := scanIssue(s.DB.R.QueryRowContext(ctx, `SELECT `+issueColumns+` WHERE i.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && is.ReportedBy.ID != p.User.ID && !CanManage(p)) {
		return Issue{}, ErrIssueNotFound
	}
	return is, err
}

// Issues lists issues (own ones for non-managers), newest first.
func (s *Service) Issues(ctx context.Context, p auth.Principal, status string, offset, limit int) ([]Issue, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var (
		conds []string
		args  []any
	)
	if status != "" {
		conds, args = append(conds, "i.status = ?"), append(args, status)
	}
	if !CanManage(p) {
		conds, args = append(conds, "i.reported_by = ?"), append(args, p.User.ID)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues i`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+issueColumns+where+` ORDER BY i.created_at DESC, i.id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Issue{}
	for rows.Next() {
		is, err := scanIssue(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, is)
	}
	return out, total, rows.Err()
}

// IssueInput is a new issue.
type IssueInput struct {
	Kind            Kind
	TMDbID          int64
	MBID            string
	Type            string
	Season, Episode *int
	Message         string
}

func (is *Issue) display() string {
	t := is.Title
	switch {
	case is.Season != nil && is.Episode != nil:
		t += fmt.Sprintf(" S%02dE%02d", *is.Season, *is.Episode)
	case is.Season != nil:
		t += fmt.Sprintf(" S%02d", *is.Season)
	}
	return t
}

func (s *Service) publishIssue(ctx context.Context, typ notify.Type, is *Issue, actor *notify.User, to []int64, msg string) {
	if s.Hub == nil {
		return
	}
	s.Hub.Publish(ctx, notify.Event{Type: typ, Recipients: to, Actor: actor, Title: is.display(), Message: msg, Poster: is.Poster,
		MediaKind: string(is.Kind), IssueID: is.ID, Params: map[string]any{"issueType": is.Type}})
}

// CreateIssue reports a problem with a title that is in the library.
func (s *Service) CreateIssue(ctx context.Context, p auth.Principal, in IssueInput) (Issue, error) {
	switch in.Kind {
	case KindMovie, KindSeries, KindAlbum:
	default:
		return Issue{}, invalid("issues are for movies, series and albums")
	}
	switch in.Type {
	case IssueVideo, IssueAudio, IssueSubtitles, IssueOther:
	default:
		return Issue{}, invalid("unknown issue type %q", in.Type)
	}
	in.Message = strings.TrimSpace(in.Message)
	if in.Message == "" {
		return Issue{}, invalid("describe the problem")
	}
	if in.Kind != KindSeries {
		in.Season, in.Episode = nil, nil
	} else if in.Season == nil {
		in.Episode = nil
	}
	in.MBID = strings.ToLower(strings.TrimSpace(in.MBID))
	st, ok, err := s.Library.Lookup(ctx, in.Kind, in.TMDbID, in.MBID)
	if err != nil {
		return Issue{}, err
	}
	if !ok || (st.Status != StatusAvailable && st.Status != StatusPartial) {
		return Issue{}, ErrNotAvailable
	}
	t, err := s.Library.Describe(ctx, in.Kind, in.TMDbID, in.MBID)
	if err != nil {
		return Issue{}, err
	}
	title := t.Title
	if t.Subtitle != "" && in.Kind == KindAlbum {
		title = t.Subtitle + " - " + title
	}
	now := db.Millis(s.now())
	var id int64
	err = s.DB.W.QueryRowContext(ctx, `INSERT INTO issues (kind, type, status, item_ref, tmdb_id, mbid, title, poster, season_number,
		episode_number, message, reported_by, created_at, updated_at) VALUES (?, ?, 'open', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		string(in.Kind), in.Type, st.Ref, in.TMDbID, in.MBID, title, t.Poster, in.Season, in.Episode, truncateRunes(in.Message, 2000),
		p.User.ID, now, now).Scan(&id)
	if err != nil {
		return Issue{}, err
	}
	is, err := s.GetIssue(ctx, p, id)
	if err != nil {
		return is, err
	}
	actor := userOf(p)
	if s.Timeline != nil {
		_ = s.Timeline.Record(ctx, st.Ref, TypeIssueReported, fmt.Sprintf("%s reported a %s problem: %s", actor.Username, in.Type, is.Message),
			map[string]any{"issueId": id, "type": in.Type, "season": in.Season, "episode": in.Episode}, nil)
	}
	s.publishIssue(ctx, notify.IssueCreated, &is, &actor, s.managers(ctx), is.Message)
	return is, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// ResolveIssue closes an issue (managers); with replace the current files go
// to the recycle bin, their releases are blocklisted and Locarr searches again.
func (s *Service) ResolveIssue(ctx context.Context, p auth.Principal, id int64, replace bool, comment string) (Issue, error) {
	if !CanManage(p) {
		return Issue{}, ErrForbidden
	}
	is, err := s.GetIssue(ctx, p, id)
	if err != nil {
		return is, err
	}
	if is.Status != IssueOpen {
		return is, fmt.Errorf("%w: the issue is already resolved", ErrNotPending)
	}
	actor := userOf(p)
	if c := strings.TrimSpace(comment); c != "" {
		if _, err := s.addComment(ctx, "issue_comments", "issue_id", id, actor, c); err != nil {
			return is, err
		}
	}
	replaced := 0
	if replace {
		reason := fmt.Sprintf("issue #%d (%s): %s", is.ID, is.Type, is.Message)
		if replaced, err = s.Library.Replace(ctx, is.ItemRef, is.Season, is.Episode, reason); err != nil {
			return is, err
		}
	}
	now := db.Millis(s.now())
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE issues SET status = 'resolved', resolved_by = ?, resolved_at = ?, updated_at = ?,
		replacement_searched = replacement_searched OR ? WHERE id = ?`, p.User.ID, now, now, replace, id); err != nil {
		return is, err
	}
	if s.Timeline != nil {
		_ = s.Timeline.Record(ctx, is.ItemRef, TypeIssueResolved, "issue resolved by "+actor.Username,
			map[string]any{"issueId": id, "replacement": replace, "files": replaced}, nil)
	}
	is, err = s.GetIssue(ctx, p, id)
	if err == nil {
		s.publishIssue(ctx, notify.IssueResolved, &is, &actor, []int64{is.ReportedBy.ID}, strings.TrimSpace(comment))
	}
	return is, err
}

// ReopenIssue opens a resolved issue again (managers and the reporter).
func (s *Service) ReopenIssue(ctx context.Context, p auth.Principal, id int64) (Issue, error) {
	is, err := s.GetIssue(ctx, p, id)
	if err != nil {
		return is, err
	}
	if is.Status != IssueResolved {
		return is, fmt.Errorf("%w: the issue is open", ErrNotPending)
	}
	now := db.Millis(s.now())
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE issues SET status = 'open', resolved_by = NULL, resolved_at = NULL, updated_at = ? WHERE id = ?`,
		now, id); err != nil {
		return is, err
	}
	actor := userOf(p)
	to := []int64{is.ReportedBy.ID}
	if p.User.ID == is.ReportedBy.ID {
		to = s.managers(ctx)
	}
	is, err = s.GetIssue(ctx, p, id)
	if err == nil {
		s.publishIssue(ctx, notify.IssueReopened, &is, &actor, to, "")
	}
	return is, err
}

// IssueComments returns an issue's comments.
func (s *Service) IssueComments(ctx context.Context, p auth.Principal, id int64) ([]Comment, error) {
	if _, err := s.GetIssue(ctx, p, id); err != nil {
		return nil, err
	}
	return s.comments(ctx, "issue_comments", "issue_id", id)
}

// IssueComment adds a comment and tells the other side.
func (s *Service) IssueComment(ctx context.Context, p auth.Principal, id int64, body string) (Comment, error) {
	is, err := s.GetIssue(ctx, p, id)
	if err != nil {
		return Comment{}, err
	}
	actor := userOf(p)
	c, err := s.addComment(ctx, "issue_comments", "issue_id", id, actor, body)
	if err != nil {
		return c, err
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE issues SET updated_at = ? WHERE id = ?`, db.Millis(s.now()), id); err != nil {
		return c, err
	}
	to := []int64{is.ReportedBy.ID}
	if p.User.ID == is.ReportedBy.ID {
		to = s.managers(ctx)
	}
	s.publishIssue(ctx, notify.IssueComment, &is, &actor, to, c.Body)
	return c, nil
}
