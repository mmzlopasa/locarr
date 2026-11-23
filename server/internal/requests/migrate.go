package requests

import (
	"context"
	"database/sql"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// Imported is a request read from another request application (P10). Its
// state, dates and people are kept; the library sync then follows it like
// any other request.
type Imported struct {
	Kind        Kind
	Is4K        bool
	Status      Status
	TMDbID      int64
	Title       string
	Year        int
	Poster      string
	AllSeasons  bool
	Seasons     []Season
	RequestedBy int64
	DecidedBy   *int64
	DecidedAt   *time.Time
	ItemRef     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	AvailableAt *time.Time
	Comments    []ImportedComment
}

// ImportedComment is a comment with its author and time.
type ImportedComment struct {
	UserID    int64
	Body      string
	CreatedAt time.Time
}

func millisOr(t time.Time, def int64) int64 {
	if t.IsZero() {
		return def
	}
	return db.Millis(t)
}

func nullMs(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return db.Millis(*t)
}

// ImportRequest stores an imported request and returns its id.
func (s *Service) ImportRequest(ctx context.Context, in Imported) (int64, error) {
	if !in.Kind.Valid() {
		return 0, invalid("unknown kind %q", in.Kind)
	}
	switch in.Status {
	case StatusPending, StatusApproved, StatusDeclined, StatusCancelled, StatusProcessing, StatusPartial, StatusAvailable, StatusFailed:
	default:
		return 0, invalid("unknown status %q", in.Status)
	}
	if in.Title == "" {
		return 0, invalid("a title is required")
	}
	now := db.Millis(s.now())
	created := millisOr(in.CreatedAt, now)
	updated := millisOr(in.UpdatedAt, created)
	var id int64
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO requests (kind, is_4k, status, tmdb_id, title, year, poster, all_seasons,
			requested_by, decided_by, decided_at, item_ref, created_at, updated_at, available_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`, string(in.Kind), in.Is4K, string(in.Status), in.TMDbID,
			in.Title, in.Year, in.Poster, in.AllSeasons, in.RequestedBy, in.DecidedBy, nullMs(in.DecidedAt), in.ItemRef, created, updated,
			nullMs(in.AvailableAt)).Scan(&id); err != nil {
			return err
		}
		for _, sn := range in.Seasons {
			st := sn.Status
			if st == "" {
				st = in.Status
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO request_seasons (request_id, number, status) VALUES (?, ?, ?)
				ON CONFLICT DO NOTHING`, id, sn.Number, string(st)); err != nil {
				return err
			}
		}
		for _, c := range in.Comments {
			if _, err := tx.ExecContext(ctx, `INSERT INTO request_comments (request_id, user_id, body, created_at) VALUES (?, ?, ?, ?)`,
				id, c.UserID, truncateRunes(c.Body, 2000), millisOr(c.CreatedAt, now)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	s.Kick()
	return id, nil
}

// ImportedIssue is an issue read from another request application.
type ImportedIssue struct {
	Kind       Kind
	Type       string // video, audio, subtitles, other
	Resolved   bool
	ItemRef    string
	TMDbID     int64
	Title      string
	Season     *int
	Episode    *int
	Message    string
	ReportedBy int64
	ResolvedBy *int64
	ResolvedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Comments   []ImportedComment
}

// ImportIssue stores an imported issue with its comments.
func (s *Service) ImportIssue(ctx context.Context, in ImportedIssue) (int64, error) {
	if in.Kind != KindMovie && in.Kind != KindSeries && in.Kind != KindAlbum {
		return 0, invalid("unknown issue kind %q", in.Kind)
	}
	switch in.Type {
	case "video", "audio", "subtitles", "other":
	default:
		in.Type = "other"
	}
	status := "open"
	if in.Resolved {
		status = "resolved"
	}
	if in.Message == "" {
		in.Message = "-"
	}
	now := db.Millis(s.now())
	created := millisOr(in.CreatedAt, now)
	var id int64
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO issues (kind, type, status, item_ref, tmdb_id, title, season_number, episode_number,
			message, reported_by, resolved_by, resolved_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			string(in.Kind), in.Type, status, in.ItemRef, in.TMDbID, in.Title, in.Season, in.Episode, truncateRunes(in.Message, 2000),
			in.ReportedBy, in.ResolvedBy, nullMs(in.ResolvedAt), created, millisOr(in.UpdatedAt, created)).Scan(&id); err != nil {
			return err
		}
		for _, c := range in.Comments {
			if _, err := tx.ExecContext(ctx, `INSERT INTO issue_comments (issue_id, user_id, body, created_at) VALUES (?, ?, ?, ?)`,
				id, c.UserID, truncateRunes(c.Body, 2000), millisOr(c.CreatedAt, now)); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

// SetDefaultQuotas changes only the default quota rules of the settings.
func (s *Service) SetDefaultQuotas(ctx context.Context, rules map[string]Rule) error {
	cur, err := s.Settings(ctx)
	if err != nil {
		return err
	}
	if cur.Quotas == nil {
		cur.Quotas = map[string]Rule{}
	}
	for k, r := range rules {
		cur.Quotas[k] = r
	}
	_, err = s.SaveSettings(ctx, cur)
	return err
}
