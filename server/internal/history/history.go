// Package history is the library-wide activity log (D-082): what was
// grabbed, imported, upgraded, failed, deleted, renamed, which subtitles
// arrived and which media server notifications went out. It reads the
// per-item timeline events and the download events (nothing is stored twice)
// and adds statistics per indexer and download client.
package history

import (
	"context"
	"database/sql"
	"fmt"
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
	Subtitles:     {"subtitle_downloaded", "subtitle_upgraded"},
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
	ID         string
	At         time.Time
	Category   string
	Type       string
	Kind       string // movie, series, music
	ItemRef    string
	Title      string
	Release    string
	Indexer    string
	Client     string
	Message    string
	DownloadID *int64
}

// Filter selects history entries.
type Filter struct {
	Category string // "" = all
	Kind     string // movies, series, music; "" = all
	Query    string
	Offset   int
	Limit    int
}

// Store reads the history.
type Store struct{ DB *db.DB }

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

// List returns one page of the history, newest first, and whether there is more.
func (s *Store) List(ctx context.Context, f Filter) ([]Entry, bool, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 50
	}
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
	kindFilter := func(col string) (string, []any) {
		var parts []string
		var args []any
		for _, p := range refPrefixes(f.Kind) {
			parts, args = append(parts, col+" LIKE ?"), append(args, p+"%")
		}
		if len(parts) == 0 {
			return "", nil
		}
		return " AND (" + strings.Join(parts, " OR ") + ")", args
	}
	// Adult items never appear in the shared history (D-101); they have
	// their own timelines in the adult section.
	refFilter := func(col string) (string, []any) {
		f, a := kindFilter(col)
		return " AND " + col + " NOT LIKE 'scene:%' AND " + col + " NOT LIKE 'adult_movie:%'" + f, a
	}
	q := strings.TrimSpace(f.Query)
	like := ""
	if q != "" {
		like = "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(q)) + "%"
	}
	// Items whose title matches the search.
	matching := `SELECT 'movie:' || id FROM media_items WHERE kind = 'movie' AND lower(title) LIKE ? ESCAPE '\'
		UNION ALL SELECT 'series:' || id FROM media_items WHERE kind = 'series' AND lower(title) LIKE ? ESCAPE '\'
		UNION ALL SELECT 'episode:' || e.id FROM episodes e JOIN media_items i ON i.id = e.series_id WHERE lower(i.title) LIKE ? ESCAPE '\'
		UNION ALL SELECT 'season:' || sn.id FROM seasons sn JOIN media_items i ON i.id = sn.series_id WHERE lower(i.title) LIKE ? ESCAPE '\'
		UNION ALL SELECT 'artist:' || id FROM media_items WHERE kind = 'artist' AND lower(title) LIKE ? ESCAPE '\'
		UNION ALL SELECT 'album:' || a.id FROM albums a JOIN media_items i ON i.id = a.artist_id
			WHERE lower(i.title) LIKE ? ESCAPE '\' OR lower(a.title) LIKE ? ESCAPE '\'`
	matchArgs := func() []any { return []any{like, like, like, like, like, like, like} }

	var parts []string
	var args []any
	if len(tTypes) > 0 {
		part := `SELECT 't:' || id AS hid, 'timeline' AS src, at, type, item_ref, message, download_id, '' AS rel, '' AS idx, '' AS cl
			FROM timeline_events WHERE type IN (` + marks(len(tTypes)) + `)`
		for _, t := range tTypes {
			args = append(args, t)
		}
		rf, ra := refFilter("item_ref")
		part += rf
		args = append(args, ra...)
		if like != "" {
			part += ` AND (lower(message) LIKE ? ESCAPE '\' OR item_ref IN (` + matching + `))`
			args = append(append(args, like), matchArgs()...)
		}
		parts = append(parts, part)
	}
	if len(dTypes) > 0 {
		part := `SELECT 'd:' || e.id AS hid, 'download' AS src, e.at AS at, e.type AS type, COALESCE(json_extract(d.item_ref, '$.ref'), '') AS item_ref,
			e.message AS message, d.id AS download_id, d.title AS rel, d.indexer AS idx, d.client_name AS cl FROM download_events e JOIN downloads d ON d.id = e.download_id WHERE e.type IN (` + marks(len(dTypes)) + `)`
		for _, t := range dTypes {
			args = append(args, t)
		}
		rf, ra := refFilter("COALESCE(json_extract(d.item_ref, '$.ref'), '')")
		part += rf
		args = append(args, ra...)
		if like != "" {
			part += ` AND (lower(d.title) LIKE ? ESCAPE '\' OR lower(e.message) LIKE ? ESCAPE '\' OR json_extract(d.item_ref, '$.ref') IN (` + matching + `))`
			args = append(append(args, like, like), matchArgs()...)
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return []Entry{}, false, nil
	}
	query := `SELECT hid, src, at, type, item_ref, message, download_id, rel, idx, cl FROM (` + strings.Join(parts, " UNION ALL ") + // #nosec G202 -- fixed fragments
		`) ORDER BY at DESC, hid DESC LIMIT ? OFFSET ?`
	rows, err := s.DB.R.QueryContext(ctx, query, append(args, f.Limit+1, max(f.Offset, 0))...) // #nosec G202 -- placeholders only
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var (
			e   Entry
			src string
			at  int64
			dl  sql.NullInt64
		)
		if err := rows.Scan(&e.ID, &src, &at, &e.Type, &e.ItemRef, &e.Message, &dl, &e.Release, &e.Indexer, &e.Client); err != nil {
			return nil, false, err
		}
		e.At = db.Time(at)
		e.Category = CategoryOf(src, e.Type)
		if src == "download" {
			e.Type = timeline.TypeDownload + e.Type
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

// describe fills the title and kind of each entry's item.
func (s *Store) describe(ctx context.Context, list []Entry) {
	cache := map[string][2]string{}
	for i := range list {
		e := &list[i]
		if v, ok := cache[e.ItemRef]; ok {
			e.Title, e.Kind = v[0], v[1]
			continue
		}
		title, kind := s.title(ctx, e.ItemRef)
		cache[e.ItemRef] = [2]string{title, kind}
		e.Title, e.Kind = title, kind
		if e.Title == "" {
			e.Title = e.Release
		}
	}
}

func (s *Store) title(ctx context.Context, raw string) (string, string) {
	r, err := library.ParseRef(raw)
	if err != nil {
		return "", ""
	}
	var (
		title string
		year  int
		sn    int
		en    int
	)
	switch r.Kind {
	case library.RefMovie:
		if s.DB.R.QueryRowContext(ctx, `SELECT title, year FROM media_items WHERE id = ?`, r.ID).Scan(&title, &year) != nil {
			return "", "movie"
		}
		if year > 0 {
			title = fmt.Sprintf("%s (%d)", title, year)
		}
		return title, "movie"
	case library.RefSeries:
		_ = s.DB.R.QueryRowContext(ctx, `SELECT title FROM media_items WHERE id = ?`, r.ID).Scan(&title)
		return title, "series"
	case library.RefSeason:
		if s.DB.R.QueryRowContext(ctx, `SELECT i.title, sn.number FROM seasons sn JOIN media_items i ON i.id = sn.series_id WHERE sn.id = ?`,
			r.ID).Scan(&title, &sn) == nil {
			title = fmt.Sprintf("%s season %d", title, sn)
		}
		return title, "series"
	case library.RefEpisode:
		if s.DB.R.QueryRowContext(ctx, `SELECT i.title, e.season_number, e.episode_number FROM episodes e JOIN media_items i ON i.id = e.series_id
			WHERE e.id = ?`, r.ID).Scan(&title, &sn, &en) == nil {
			title = fmt.Sprintf("%s S%02dE%02d", title, sn, en)
		}
		return title, "series"
	case library.RefArtist:
		_ = s.DB.R.QueryRowContext(ctx, `SELECT title FROM media_items WHERE id = ?`, r.ID).Scan(&title)
		return title, "music"
	case library.RefAlbum:
		var artist string
		if s.DB.R.QueryRowContext(ctx, `SELECT i.title, a.title FROM albums a JOIN media_items i ON i.id = a.artist_id WHERE a.id = ?`,
			r.ID).Scan(&artist, &title) == nil {
			title = artist + " - " + title
		}
		return title, "music"
	}
	return "", ""
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
