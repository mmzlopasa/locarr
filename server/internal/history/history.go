// Package history is the library-wide activity log (D-082): what was
// grabbed, imported, upgraded, failed, deleted, renamed, which subtitles
// arrived and which media server notifications went out. It reads the
// per-item timeline events and the download events (nothing is stored twice)
// and adds statistics per indexer and download client.
package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// Categories.
const (
	Grabbed       = "grabbed"
	Imported      = "imported"
	Upgraded      = "upgraded"
	Failed        = "failed"
	Deleted       = "deleted"
	Renamed       = "renamed"
	Subtitles     = "subtitles"
	Notifications = "notifications"
)

// timelineTypes maps categories to timeline event types.
var timelineTypes = map[string][]string{
	Imported:      {timeline.TypeFileImported},
	Upgraded:      {timeline.TypeUpgraded},
	Failed:        {timeline.TypeImportFailed, timeline.TypeGrabFailed},
	Deleted:       {timeline.TypeRecycled, timeline.TypeDeleted},
	Renamed:       {timeline.TypeRenamed},
	Subtitles:     {"subtitle_downloaded", "subtitle_upgraded", "subtitle_translated"},
	Notifications: {timeline.TypeNotified},
}

// downloadTypes maps categories to download event types.
var downloadTypes = map[string][]string{
	Grabbed: {"grabbed"},
	Failed:  {"failed"},
}

// CategoryOf returns the category of an event type ("" = not in the history).
func CategoryOf(source, typ string) string {
	types := timelineTypes
	if source == "download" {
		types = downloadTypes
	}
	for c, list := range types {
		for _, t := range list {
			if t == typ {
				return c
			}
		}
	}
	return ""
}

// Entry is one line of the history.
type Entry struct {
	ID       string
	At       time.Time
	Category string
	Type     string
	Kind     string // movie, series, music
	ItemRef  string
	// ItemTitle is the item's title when the event happened (D-107); for
	// older entries of removed items it may be "".
	ItemTitle string
	// Title is what to show: ItemTitle, else the release.
	Title string
	// ItemGone: the item is no longer in the library (no link).
	ItemGone   bool
	Release    string
	Indexer    string
	Client     string
	Message    string
	Data       json.RawMessage
	DownloadID *int64
}

// Filter selects history entries.
type Filter struct {
	Category string // "" = all
	Kind     string // movies, series, music; "" = all
	Query    string
	Offset   int
	Limit    int
	// Before keeps only entries older than this (zero = no limit).
	Before time.Time
}

// Store reads the history.
type Store struct{ DB *db.DB }

// ErrNotFound is returned for an unknown history entry (or one the caller may not see).
var ErrNotFound = errors.New("history entry not found")

func marks(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func refPrefixes(kind string) []string {
	switch kind {
	case "movies":
		return []string{"movie:"}
	case "series":
		return []string{"series:", "season:", "episode:"}
	case "music":
		return []string{"artist:", "album:"}
	}
	return nil
}

// TimelineTypes are the timeline event types the history shows.
func TimelineTypes() []string {
	var out []string
	for _, list := range timelineTypes {
		out = append(out, list...)
	}
	sort.Strings(out)
	return out
}

// DownloadTypes are the download event types the history shows.
func DownloadTypes() []string {
	var out []string
	for _, list := range downloadTypes {
		out = append(out, list...)
	}
	sort.Strings(out)
	return out
}

// part is one source of the history: "FROM … WHERE …" and its arguments.
type part struct {
	from string
	args []any
}

// parts builds the timeline and download sources for a filter (nil when the
// filter excludes the source). Adult items never appear in the shared history
// (D-103); they have their own timelines in the adult section, so nothing
// here ever reads, counts or deletes them.
func parts(f Filter) (tl, dl *part) {
	var tTypes, dTypes []string
	for c, list := range timelineTypes {
		if f.Category == "" || f.Category == c {
			tTypes = append(tTypes, list...)
		}
	}
	for c, list := range downloadTypes {
		if f.Category == "" || f.Category == c {
			dTypes = append(dTypes, list...)
		}
	}
	refFilter := func(col string) (string, []any) {
		out := " AND " + col + " NOT LIKE 'scene:%' AND " + col + " NOT LIKE 'adult_movie:%'"
		var ors []string
		var args []any
		for _, p := range refPrefixes(f.Kind) {
			ors, args = append(ors, col+" LIKE ?"), append(args, p+"%")
		}
		if len(ors) > 0 {
			out += " AND (" + strings.Join(ors, " OR ") + ")"
		}
		return out, args
	}
	q := strings.TrimSpace(f.Query)
	like := ""
	if q != "" {
		like = "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(q)) + "%"
	}
	// Items whose current title matches the search (entries also match on
	// the title they were stored with).
	matching := `SELECT 'movie:' || id FROM media_items WHERE kind = 'movie' AND lower(title) LIKE ? ESCAPE '\'
		UNION ALL SELECT 'series:' || id FROM media_items WHERE kind = 'series' AND lower(title) LIKE ? ESCAPE '\'
		UNION ALL SELECT 'episode:' || e.id FROM episodes e JOIN media_items i ON i.id = e.series_id WHERE lower(i.title) LIKE ? ESCAPE '\'
		UNION ALL SELECT 'season:' || sn.id FROM seasons sn JOIN media_items i ON i.id = sn.series_id WHERE lower(i.title) LIKE ? ESCAPE '\'
		UNION ALL SELECT 'artist:' || id FROM media_items WHERE kind = 'artist' AND lower(title) LIKE ? ESCAPE '\'
		UNION ALL SELECT 'album:' || a.id FROM albums a JOIN media_items i ON i.id = a.artist_id
			WHERE lower(i.title) LIKE ? ESCAPE '\' OR lower(a.title) LIKE ? ESCAPE '\'`
	matchArgs := func() []any { return []any{like, like, like, like, like, like, like} }
	if len(tTypes) > 0 {
		p := &part{from: `FROM timeline_events t WHERE t.type IN (` + marks(len(tTypes)) + `)`}
		for _, t := range tTypes {
			p.args = append(p.args, t)
		}
		rf, ra := refFilter("t.item_ref")
		p.from += rf
		p.args = append(p.args, ra...)
		if !f.Before.IsZero() {
			p.from += " AND t.at < ?"
			p.args = append(p.args, db.Millis(f.Before))
		}
		if like != "" {
			p.from += ` AND (lower(t.message) LIKE ? ESCAPE '\' OR lower(t.item_title) LIKE ? ESCAPE '\' OR t.item_ref IN (` + matching + `))`
			p.args = append(append(p.args, like, like), matchArgs()...)
		}
		tl = p
	}
	if len(dTypes) > 0 {
		ref := "COALESCE(json_extract(d.item_ref, '$.ref'), '')"
		p := &part{from: `FROM download_events e JOIN downloads d ON d.id = e.download_id WHERE e.type IN (` + marks(len(dTypes)) + `)`}
		for _, t := range dTypes {
			p.args = append(p.args, t)
		}
		rf, ra := refFilter(ref)
		p.from += rf
		p.args = append(p.args, ra...)
		if !f.Before.IsZero() {
			p.from += " AND e.at < ?"
			p.args = append(p.args, db.Millis(f.Before))
		}
		if like != "" {
			p.from += ` AND (lower(d.title) LIKE ? ESCAPE '\' OR lower(e.message) LIKE ? ESCAPE '\' OR lower(d.item_title) LIKE ? ESCAPE '\'
				OR json_extract(d.item_ref, '$.ref') IN (` + matching + `))`
			p.args = append(append(p.args, like, like, like), matchArgs()...)
		}
		dl = p
	}
	return tl, dl
}

// List returns one page of the history, newest first, and whether there is more.
func (s *Store) List(ctx context.Context, f Filter) ([]Entry, bool, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 50
	}
	tl, dl := parts(f)
	var sel []string
	var args []any
	if tl != nil {
		sel = append(sel, `SELECT 't:' || t.id AS hid, 'timeline' AS src, t.at AS at, t.type AS type, t.item_ref AS item_ref, t.item_title AS item_title,
			t.message AS message, t.data AS data, t.download_id AS download_id, '' AS rel, '' AS idx, '' AS cl `+tl.from)
		args = append(args, tl.args...)
	}
	if dl != nil {
		sel = append(sel, `SELECT 'd:' || e.id AS hid, 'download' AS src, e.at AS at, e.type AS type, COALESCE(json_extract(d.item_ref, '$.ref'), '') AS item_ref,
			d.item_title AS item_title, e.message AS message, e.data AS data, d.id AS download_id, d.title AS rel, d.indexer AS idx, d.client_name AS cl `+dl.from)
		args = append(args, dl.args...)
	}
	if len(sel) == 0 {
		return []Entry{}, false, nil
	}
	query := `SELECT hid, src, at, type, item_ref, item_title, message, data, download_id, rel, idx, cl FROM (` + strings.Join(sel, " UNION ALL ") + // #nosec G202 -- fixed fragments
		`) ORDER BY at DESC, hid DESC LIMIT ? OFFSET ?`
	rows, err := s.DB.R.QueryContext(ctx, query, append(args, f.Limit+1, max(f.Offset, 0))...) // #nosec G202 -- placeholders only
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var (
			e    Entry
			src  string
			at   int64
			data string
			dl   sql.NullInt64
		)
		if err := rows.Scan(&e.ID, &src, &at, &e.Type, &e.ItemRef, &e.ItemTitle, &e.Message, &data, &dl, &e.Release, &e.Indexer, &e.Client); err != nil {
			return nil, false, err
		}
		e.At = db.Time(at)
		e.Category = CategoryOf(src, e.Type)
		if src == "download" {
			e.Type = timeline.TypeDownload + e.Type
		}
		if json.Valid([]byte(data)) {
			e.Data = json.RawMessage(data)
		} else {
			e.Data = json.RawMessage("{}")
		}
		if dl.Valid {
			id := dl.Int64
			e.DownloadID = &id
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(out) > f.Limit
	if more {
		out = out[:f.Limit]
	}
	_ = rows.Close()
	s.describe(ctx, out)
	return out, more, nil
}

// describe fills each entry's kind, whether its item still exists, and the
// release and indexer an event's data names; the title falls back to the
// current one (entries recorded before titles were stored) and then to the
// release.
func (s *Store) describe(ctx context.Context, list []Entry) {
	cache := map[string]string{}
	for i := range list {
		e := &list[i]
		e.Kind = library.KindOfRef(e.ItemRef)
		current, ok := cache[e.ItemRef]
		if !ok {
			current, _ = library.Title(ctx, s.DB.R, e.ItemRef)
			cache[e.ItemRef] = current
		}
		e.ItemGone = current == ""
		if e.ItemTitle == "" {
			e.ItemTitle = current
		}
		var d struct {
			Title   string `json:"title"`
			Indexer string `json:"indexer"`
		}
		_ = json.Unmarshal(e.Data, &d)
		if e.Release == "" {
			e.Release = d.Title
		}
		if e.Indexer == "" {
			e.Indexer = d.Indexer
		}
		e.Title = e.ItemTitle
		if e.Title == "" {
			e.Title = e.Release
		}
	}
}

// parseID splits "t:12" / "d:7".
func parseID(id string) (src string, n int64, ok bool) {
	src, raw, found := strings.Cut(id, ":")
	if !found || (src != "t" && src != "d") {
		return "", 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	return src, n, err == nil && n > 0
}

// Delete removes one history entry. It is the same row the item's timeline
// shows, so it leaves that timeline too; files, the blocklist, searches and
// decisions are not touched.
func (s *Store) Delete(ctx context.Context, id string) error {
	src, n, ok := parseID(id)
	if !ok {
		return ErrNotFound
	}
	tl, dl := parts(Filter{})
	var (
		res sql.Result
		err error
	)
	if src == "t" {
		res, err = s.DB.W.ExecContext(ctx, `DELETE FROM timeline_events WHERE id = ? AND id IN (SELECT t.id `+tl.from+`)`, // #nosec G202 -- fixed fragments
			append([]any{n}, tl.args...)...)
	} else {
		res, err = s.DB.W.ExecContext(ctx, `DELETE FROM download_events WHERE id = ? AND id IN (SELECT e.id `+dl.from+`)`, // #nosec G202 -- fixed fragments
			append([]any{n}, dl.args...)...)
	}
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	return nil
}

// Count is how many entries match a filter (Offset and Limit are ignored).
func (s *Store) Count(ctx context.Context, f Filter) (int64, error) {
	tl, dl := parts(f)
	var total int64
	for _, p := range []struct {
		p   *part
		col string
	}{{tl, "t.id"}, {dl, "e.id"}} {
		if p.p == nil {
			continue
		}
		var n int64
		if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(`+p.col+`) `+p.p.from, p.p.args...).Scan(&n); err != nil { // #nosec G202 -- fixed fragments
			return 0, err
		}
		total += n
	}
	return total, nil
}

// Clear removes every entry matching a filter and returns how many went.
func (s *Store) Clear(ctx context.Context, f Filter) (int64, error) {
	tl, dl := parts(f)
	var total int64
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if tl != nil {
			res, err := tx.ExecContext(ctx, `DELETE FROM timeline_events WHERE id IN (SELECT t.id `+tl.from+`)`, tl.args...) // #nosec G202 -- fixed fragments
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			total += n
		}
		if dl != nil {
			res, err := tx.ExecContext(ctx, `DELETE FROM download_events WHERE id IN (SELECT e.id `+dl.from+`)`, dl.args...) // #nosec G202 -- fixed fragments
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			total += n
		}
		return nil
	})
	return total, err
}

// SourceStats are the numbers of one indexer or download client.
type SourceStats struct {
	ID          *int64
	Name        string
	Grabs       int
	Imported    int
	Failed      int
	InProgress  int
	SuccessRate *float64
	AvgImport   *time.Duration
}

// Stats are grabs per indexer and per download client since a time.
func (s *Store) Stats(ctx context.Context, since time.Time) (indexers, clients []SourceStats, err error) {
	q := func(idCol, nameCol string) ([]SourceStats, error) {
		rows, err := s.DB.R.QueryContext(ctx, `SELECT `+idCol+`, `+nameCol+`, COUNT(*), SUM(status = 'imported'), SUM(status = 'failed'),
			SUM(status IN ('active', 'completed')), AVG(CASE WHEN status = 'imported' AND finished_at IS NOT NULL THEN finished_at - added_at END)
			FROM downloads WHERE added_at >= ?
			AND COALESCE(json_extract(item_ref, '$.ref'), '') NOT LIKE 'scene:%' AND COALESCE(json_extract(item_ref, '$.ref'), '') NOT LIKE 'adult_movie:%'
			GROUP BY `+idCol+`, `+nameCol+` ORDER BY COUNT(*) DESC, `+nameCol, db.Millis(since)) // #nosec G202 -- constant columns
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []SourceStats{}
		for rows.Next() {
			var (
				st  SourceStats
				id  sql.NullInt64
				avg sql.NullFloat64
			)
			if err := rows.Scan(&id, &st.Name, &st.Grabs, &st.Imported, &st.Failed, &st.InProgress, &avg); err != nil {
				return nil, err
			}
			if id.Valid {
				v := id.Int64
				st.ID = &v
			}
			if st.Name == "" {
				st.Name = "-"
			}
			if n := st.Imported + st.Failed; n > 0 {
				r := float64(st.Imported) / float64(n)
				st.SuccessRate = &r
			}
			if avg.Valid {
				d := time.Duration(avg.Float64) * time.Millisecond
				st.AvgImport = &d
			}
			out = append(out, st)
		}
		return out, rows.Err()
	}
	if indexers, err = q("indexer_id", "indexer"); err != nil {
		return nil, nil, err
	}
	clients, err = q("client_id", "client_name")
	return indexers, clients, err
}
