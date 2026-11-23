package requests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/notify"
)

const requestColumns = `r.id, r.kind, r.is_4k, r.status, r.tmdb_id, r.mbid, r.artist_mbid, r.title, r.subtitle, r.year, r.poster,
	r.all_seasons, r.future_seasons, r.requested_by, ru.username, r.decided_by, COALESCE(du.username, ''), r.auto_approved, r.decided_at,
	r.decline_reason, r.profile_id, r.root_folder_id, r.tags, r.item_ref, r.explain_code, r.explain_tone, r.explain_message,
	r.explain_params, r.created_at, r.updated_at, r.available_at,
	(SELECT COUNT(*) FROM request_comments c WHERE c.request_id = r.id)`

const requestFrom = ` FROM requests r JOIN users ru ON ru.id = r.requested_by LEFT JOIN users du ON du.id = r.decided_by`

func scanRequest(row interface{ Scan(...any) error }) (Request, error) {
	var (
		r                          Request
		kind, status, tags, params string
		decidedBy                  sql.NullInt64
		decidedName                string
		decidedAt, availableAt     sql.NullInt64
		profile, root              sql.NullInt64
		created, updated           int64
		code, tone, msg            string
	)
	err := row.Scan(&r.ID, &kind, &r.Is4K, &status, &r.TMDbID, &r.MBID, &r.ArtistMBID, &r.Title, &r.Subtitle, &r.Year, &r.Poster,
		&r.AllSeasons, &r.FutureSeasons, &r.RequestedBy.ID, &r.RequestedBy.Username, &decidedBy, &decidedName, &r.AutoApproved, &decidedAt,
		&r.DeclineReason, &profile, &root, &tags, &r.ItemRef, &code, &tone, &msg, &params, &created, &updated, &availableAt, &r.CommentCount)
	if err != nil {
		return r, err
	}
	r.Kind, r.Status = Kind(kind), Status(status)
	if decidedBy.Valid {
		r.DecidedBy = &notify.User{ID: decidedBy.Int64, Username: decidedName}
	}
	r.DecidedAt, r.AvailableAt = db.NullTime(decidedAt), db.NullTime(availableAt)
	if profile.Valid {
		r.ProfileID = &profile.Int64
	}
	if root.Valid {
		r.RootFolderID = &root.Int64
	}
	_ = json.Unmarshal([]byte(tags), &r.Tags)
	if r.Tags == nil {
		r.Tags = []string{}
	}
	if code != "" {
		r.Explanation = &Explanation{Code: code, Tone: tone, Message: msg}
		_ = json.Unmarshal([]byte(params), &r.Explanation.Params)
	}
	r.CreatedAt, r.UpdatedAt = db.Time(created), db.Time(updated)
	return r, nil
}

func (s *Service) loadSeasons(ctx context.Context, list []Request) error {
	if len(list) == 0 {
		return nil
	}
	idx := map[int64]int{}
	ids := make([]any, 0, len(list))
	for i := range list {
		list[i].Seasons = []Season{}
		if list[i].Kind == KindSeries {
			idx[list[i].ID] = i
			ids = append(ids, list[i].ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT request_id, number, status FROM request_seasons WHERE request_id IN (`+
		strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`) ORDER BY request_id, number`, ids...) // #nosec G202 -- placeholders only
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id int64
			sn Season
			st string
		)
		if err := rows.Scan(&id, &sn.Number, &st); err != nil {
			return err
		}
		sn.Status = Status(st)
		i := idx[id]
		list[i].Seasons = append(list[i].Seasons, sn)
	}
	return rows.Err()
}

// Get returns one request.
func (s *Service) Get(ctx context.Context, id int64) (Request, error) {
	r, err := scanRequest(s.DB.R.QueryRowContext(ctx, `SELECT `+requestColumns+requestFrom+` WHERE r.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	list := []Request{r}
	err = s.loadSeasons(ctx, list)
	return list[0], err
}

// Filter selects requests.
type Filter struct {
	// Status: "" or all = every status; active = not declined, cancelled or available.
	Status string
	Kind   Kind
	UserID int64
	TMDbID int64
	MBID   string
	Offset int
	Limit  int
}

func (f Filter) where() (string, []any) {
	var (
		conds []string
		args  []any
	)
	switch f.Status {
	case "", "all":
	case "active":
		conds = append(conds, `r.status IN ('pending', 'approved', 'processing', 'partial', 'failed')`)
	default:
		conds = append(conds, `r.status = ?`)
		args = append(args, f.Status)
	}
	if f.Kind != "" {
		conds = append(conds, `r.kind = ?`)
		args = append(args, string(f.Kind))
	}
	if f.UserID > 0 {
		conds = append(conds, `r.requested_by = ?`)
		args = append(args, f.UserID)
	}
	if f.TMDbID > 0 {
		conds = append(conds, `r.tmdb_id = ?`)
		args = append(args, f.TMDbID)
	}
	if f.MBID != "" {
		conds = append(conds, `(r.mbid = ? OR r.artist_mbid = ?)`)
		args = append(args, strings.ToLower(f.MBID), strings.ToLower(f.MBID))
	}
	if len(conds) == 0 {
		return "", nil
	}
	return ` WHERE ` + strings.Join(conds, " AND "), args
}

// List returns requests, newest first, and the total.
func (s *Service) List(ctx context.Context, f Filter) ([]Request, int, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	where, args := f.where()
	var total int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM requests r`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+requestColumns+requestFrom+where+` ORDER BY r.created_at DESC, r.id DESC LIMIT ? OFFSET ?`, // #nosec G202 -- fixed columns, clauses and placeholders only
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Request{}
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, s.loadSeasons(ctx, out)
}

func (s *Service) insert(ctx context.Context, r *Request) error {
	tags, _ := json.Marshal(r.Tags)
	now := db.Millis(s.now())
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO requests (kind, is_4k, status, tmdb_id, mbid, artist_mbid, title, subtitle, year, poster,
			all_seasons, future_seasons, requested_by, profile_id, root_folder_id, tags, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`, string(r.Kind), r.Is4K, string(r.Status), r.TMDbID,
			r.MBID, r.ArtistMBID, r.Title, r.Subtitle, r.Year, r.Poster, r.AllSeasons, r.FutureSeasons, r.RequestedBy.ID, r.ProfileID,
			r.RootFolderID, string(tags), now, now).Scan(&r.ID); err != nil {
			return err
		}
		for _, sn := range r.Seasons {
			if _, err := tx.ExecContext(ctx, `INSERT INTO request_seasons (request_id, number, status) VALUES (?, ?, ?)`, r.ID, sn.Number,
				string(sn.Status)); err != nil {
				return err
			}
		}
		return nil
	})
}

// change is a partial update of a stored request.
type change struct {
	status      *Status
	decidedBy   *int64
	auto        *bool
	decided     bool
	reason      *string
	itemRef     *string
	explanation **Explanation
	available   bool
	profile     **int64
	root        **int64
	tags        *[]string
	seasons     map[int]Status
	setSeasons  []int // replaces the season list (pending requests)
}

func (s *Service) update(ctx context.Context, id int64, c change) error {
	sets := []string{"updated_at = ?"}
	now := db.Millis(s.now())
	args := []any{now}
	add := func(col string, v any) {
		sets = append(sets, col+" = ?")
		args = append(args, v)
	}
	if c.status != nil {
		add("status", string(*c.status))
	}
	if c.decidedBy != nil {
		if *c.decidedBy > 0 {
			add("decided_by", *c.decidedBy)
		} else {
			add("decided_by", nil)
		}
	}
	if c.auto != nil {
		add("auto_approved", *c.auto)
	}
	if c.decided {
		add("decided_at", now)
	}
	if c.reason != nil {
		add("decline_reason", *c.reason)
	}
	if c.itemRef != nil {
		add("item_ref", *c.itemRef)
	}
	if c.explanation != nil {
		ex := *c.explanation
		if ex == nil {
			ex = &Explanation{}
		}
		params := "{}"
		if len(ex.Params) > 0 {
			if b, err := json.Marshal(ex.Params); err == nil {
				params = string(b)
			}
		}
		add("explain_code", ex.Code)
		add("explain_tone", ex.Tone)
		add("explain_message", ex.Message)
		add("explain_params", params)
	}
	if c.available {
		sets = append(sets, "available_at = COALESCE(available_at, ?)")
		args = append(args, now)
	}
	if c.profile != nil {
		add("profile_id", *c.profile)
	}
	if c.root != nil {
		add("root_folder_id", *c.root)
	}
	if c.tags != nil {
		b, _ := json.Marshal(*c.tags)
		add("tags", string(b))
	}
	args = append(args, id)
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE requests SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil { // #nosec G202 -- fixed columns, clauses and placeholders only
			return err
		}
		if c.setSeasons != nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM request_seasons WHERE request_id = ?`, id); err != nil {
				return err
			}
			for _, n := range c.setSeasons {
				if _, err := tx.ExecContext(ctx, `INSERT INTO request_seasons (request_id, number, status) VALUES (?, ?, ?)`, id, n,
					string(StatusPending)); err != nil {
					return err
				}
			}
		}
		for n, st := range c.seasons {
			if _, err := tx.ExecContext(ctx, `UPDATE request_seasons SET status = ? WHERE request_id = ? AND number = ?`, string(st), id, n); err != nil {
				return err
			}
		}
		return nil
	})
}

// --- comments ---

func (s *Service) comments(ctx context.Context, table, col string, id int64) ([]Comment, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT c.id, c.user_id, u.username, c.body, c.created_at FROM `+table+` c
		JOIN users u ON u.id = c.user_id WHERE c.`+col+` = ? ORDER BY c.id`, id) // #nosec G202 -- table and column are constants
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var (
			c       Comment
			created int64
		)
		if err := rows.Scan(&c.ID, &c.User.ID, &c.User.Username, &c.Body, &created); err != nil {
			return nil, err
		}
		c.CreatedAt = db.Time(created)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) addComment(ctx context.Context, table, col string, id int64, user notify.User, body string) (Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Comment{}, invalid("the comment is empty")
	}
	if r := []rune(body); len(r) > 2000 {
		body = string(r[:2000])
	}
	c := Comment{User: user, Body: body, CreatedAt: s.now().UTC().Truncate(time.Millisecond)}
	err := s.DB.W.QueryRowContext(ctx, `INSERT INTO `+table+` (`+col+`, user_id, body, created_at) VALUES (?, ?, ?, ?) RETURNING id`, // #nosec G202
		id, user.ID, body, db.Millis(c.CreatedAt)).Scan(&c.ID)
	return c, err
}

// --- quotas ---

// Overrides are a user's own quota rules by quota kind.
type Overrides map[string]Rule

// UserOverrides returns a user's own quota rules.
func (s *Service) UserOverrides(ctx context.Context, userID int64) (Overrides, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT kind, limit_count, days FROM user_quotas WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := Overrides{}
	for rows.Next() {
		var (
			k string
			r Rule
		)
		if err := rows.Scan(&k, &r.Limit, &r.Days); err != nil {
			return nil, err
		}
		out[k] = r
	}
	return out, rows.Err()
}

// SetOverrides replaces a user's own quota rules (a missing kind = the default).
func (s *Service) SetOverrides(ctx context.Context, userID int64, o Overrides) error {
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_quotas WHERE user_id = ?`, userID); err != nil {
			return err
		}
		for _, k := range QuotaKinds {
			r, ok := o[k]
			if !ok {
				continue
			}
			if r.Limit < 0 || r.Days < 1 || r.Days > 365 {
				return invalid("quota %s: limit must be 0 or more and days 1 to 365", k)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO user_quotas (user_id, kind, limit_count, days) VALUES (?, ?, ?, ?)`, userID, k,
				r.Limit, r.Days); err != nil {
				return err
			}
		}
		return nil
	})
}

func kindsFor(quotaKind string) []string {
	switch quotaKind {
	case QuotaMovie:
		return []string{string(KindMovie)}
	case QuotaSeries:
		return []string{string(KindSeries)}
	}
	return []string{string(KindAlbum), string(KindArtist)}
}

// Usage returns a user's quota usage for a quota kind.
func (s *Service) Usage(ctx context.Context, userID int64, quotaKind string) (Usage, error) {
	set, err := s.Settings(ctx)
	if err != nil {
		return Usage{}, err
	}
	rule, custom := set.Quota(quotaKind), false
	if o, err := s.UserOverrides(ctx, userID); err == nil {
		if r, ok := o[quotaKind]; ok {
			rule, custom = r, true
		}
	}
	days := rule.Days
	if days <= 0 {
		days = DefaultQuota.Days
	}
	kinds := kindsFor(quotaKind)
	from := s.now().Add(-time.Duration(days) * 24 * time.Hour)
	args := []any{userID, db.Millis(from)}
	for _, k := range kinds {
		args = append(args, k)
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",")
	q := "SELECT created_at FROM requests WHERE requested_by = ? AND created_at > ? AND status NOT IN ('declined', 'cancelled') AND kind IN (" + ph + ")" // #nosec G202 -- placeholders only
	rows, err := s.DB.R.QueryContext(ctx, q, args...)
	if err != nil {
		return Usage{}, err
	}
	defer rows.Close()
	var made []time.Time
	for rows.Next() {
		var ms int64
		if err := rows.Scan(&ms); err != nil {
			return Usage{}, err
		}
		made = append(made, db.Time(ms))
	}
	if err := rows.Err(); err != nil {
		return Usage{}, err
	}
	u := Compute(rule, made, s.now())
	u.Custom = custom
	return u, nil
}

// managers returns the users who manage requests (administrators included).
func (s *Service) managers(ctx context.Context) []int64 {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id FROM users WHERE disabled = 0 AND (role = 'admin' OR (permissions & ?) <> 0)`,
		int64(permManage)) // #nosec G115
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}
