package indexers

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// Summary is a short human-readable description of the query.
func (q Query) Summary() string {
	var parts []string
	add := func(k, v string) {
		if v != "" {
			parts = append(parts, k+v)
		}
	}
	num := func(n int64) string {
		if n <= 0 {
			return ""
		}
		return itoa(n)
	}
	add("", strings.TrimSpace(q.Term))
	add("imdb:", q.IMDBID)
	add("tmdb:", num(q.TMDBID))
	add("tvdb:", num(q.TVDBID))
	add("tvmaze:", num(q.TVMazeID))
	add("S", q.Season)
	add("E", q.Episode)
	add("artist:", q.Artist)
	add("album:", q.Album)
	add("author:", q.Author)
	add("title:", q.Title)
	if len(parts) == 0 {
		return "(latest)"
	}
	return truncate(strings.Join(parts, " "), 300)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// writeLog records a finished search. It survives the request ending.
func (s *Service) writeLog(ctx context.Context, q Query, start time.Time, r Result) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	params, _ := json.Marshal(struct {
		Type       SearchType `json:"type"`
		Term       string     `json:"term,omitempty"`
		Categories []int      `json:"categories,omitempty"`
		IndexerIDs []int64    `json:"indexerIds,omitempty"`
		IMDBID     string     `json:"imdbId,omitempty"`
		TMDBID     int64      `json:"tmdbId,omitempty"`
		TVDBID     int64      `json:"tvdbId,omitempty"`
		TVMazeID   int64      `json:"tvmazeId,omitempty"`
		Season     string     `json:"season,omitempty"`
		Episode    string     `json:"episode,omitempty"`
		Artist     string     `json:"artist,omitempty"`
		Album      string     `json:"album,omitempty"`
		Author     string     `json:"author,omitempty"`
		Title      string     `json:"title,omitempty"`
	}{q.Type, q.Term, q.Categories, q.IndexerIDs, q.IMDBID, q.TMDBID, q.TVDBID, q.TVMazeID, q.Season, q.Episode,
		q.Artist, q.Album, q.Author, q.Title})
	errs := 0
	for _, ir := range r.Indexers {
		if ir.Status == StatusError {
			errs++
		}
	}
	var itemRef, user any
	if q.ItemRef != "" {
		itemRef = q.ItemRef
	}
	if q.UserID > 0 {
		user = q.UserID
	}
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		var id int64
		if err := tx.QueryRowContext(ctx, `INSERT INTO search_log (started_at, duration_ms, source, search_type, query, params,
			item_ref, user_id, result_count, error_count) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			db.Millis(start), r.Duration.Milliseconds(), string(q.Source), string(q.Type), q.Summary(), string(params),
			itemRef, user, len(r.Releases), errs).Scan(&id); err != nil {
			return err
		}
		for _, ir := range r.Indexers {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO search_log_indexers (search_id, indexer_id, indexer_name,
				status, result_count, cached, duration_ms, error_code, error) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				id, ir.IndexerID, ir.IndexerName, string(ir.Status), ir.Count, ir.Cached, ir.Duration.Milliseconds(),
				strArg(ir.ErrorCode), strArg(ir.Error)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.Log.Warn("write search log", "err", err)
	}
}

// LogEntry is one recorded search.
type LogEntry struct {
	ID          int64
	StartedAt   time.Time
	Duration    time.Duration
	Source      Source
	Type        SearchType
	Query       string
	ItemRef     string
	UserID      int64
	ResultCount int
	ErrorCount  int
	Indexers    []IndexerResult
}

// SearchLog returns recent searches, newest first, optionally for one item.
func (s *Service) SearchLog(ctx context.Context, itemRef string, limit int) ([]LogEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	where, args := "", []any{}
	if itemRef != "" {
		where, args = "WHERE item_ref = ?", append(args, itemRef)
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, started_at, duration_ms, source, search_type, query, item_ref, user_id,
		result_count, error_count FROM search_log `+where+` ORDER BY started_at DESC, id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LogEntry{}
	byID := map[int64]int{}
	for rows.Next() {
		var (
			e            LogEntry
			started, dur int64
			src, typ     string
			ref          sql.NullString
			user         sql.NullInt64
		)
		if err := rows.Scan(&e.ID, &started, &dur, &src, &typ, &e.Query, &ref, &user, &e.ResultCount, &e.ErrorCount); err != nil {
			return nil, err
		}
		e.StartedAt, e.Duration, e.Source, e.Type = db.Time(started), time.Duration(dur)*time.Millisecond, Source(src), SearchType(typ)
		e.ItemRef, e.UserID = ref.String, user.Int64
		e.Indexers = []IndexerResult{}
		byID[e.ID] = len(out)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	ids := make([]any, 0, len(out))
	marks := make([]string, 0, len(out))
	for _, e := range out {
		ids = append(ids, e.ID)
		marks = append(marks, "?")
	}
	irows, err := s.DB.R.QueryContext(ctx, `SELECT search_id, indexer_id, indexer_name, status, result_count, cached, duration_ms,
		error_code, error FROM search_log_indexers WHERE search_id IN (`+strings.Join(marks, ",")+`) ORDER BY indexer_name`, ids...)
	if err != nil {
		return nil, err
	}
	defer irows.Close()
	for irows.Next() {
		var (
			sid      int64
			ir       IndexerResult
			iid      sql.NullInt64
			st       string
			dur      int64
			code, em sql.NullString
		)
		if err := irows.Scan(&sid, &iid, &ir.IndexerName, &st, &ir.Count, &ir.Cached, &dur, &code, &em); err != nil {
			return nil, err
		}
		ir.IndexerID, ir.Status, ir.Duration = iid.Int64, Status(st), time.Duration(dur)*time.Millisecond
		ir.ErrorCode, ir.Error = code.String, em.String
		i := byID[sid]
		out[i].Indexers = append(out[i].Indexers, ir)
	}
	return out, irows.Err()
}
