// Package timeline keeps one history per library item (D-030): what Locarr
// searched, what it decided and why, what it grabbed, how the download went,
// what it imported, renamed, upgraded or recycled. Searches come from
// search_log, download steps from download_events; everything else is
// recorded here. Explain derives the one answer to "why isn't it here?".
package timeline

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// Event types recorded in timeline_events.
const (
	TypeAdded        = "added"
	TypeEdited       = "edited"
	TypeRefreshed    = "refreshed"
	TypeDecision     = "decision"
	TypePending      = "pending"
	TypeGrabFailed   = "grab_failed"
	TypeFileImported = "file_imported"
	TypeImportFailed = "import_failed"
	TypeRenamed      = "renamed"
	TypeUpgraded     = "upgraded"
	TypeRecycled     = "recycled"
	TypeDeleted      = "deleted"
	TypeNotified     = "notified"
	TypeRetry        = "retry"
	TypeFileFound    = "file_found"
	// TypeImportSkipped: a file of a download that was not imported (no
	// matching episode, not an upgrade) while others were.
	TypeImportSkipped = "import_skipped"
)

// Types derived from other tables.
const (
	TypeSearch   = "search"    // search_log
	TypeDownload = "download_" // prefix for download_events types: download_grabbed, download_progress, …
)

// Event is one entry of an item's timeline.
type Event struct {
	ID         int64           `json:"id"`
	ItemRef    string          `json:"itemRef"`
	At         time.Time       `json:"at"`
	Type       string          `json:"type"`
	Message    string          `json:"message"`
	Data       json.RawMessage `json:"data"`
	DownloadID *int64          `json:"downloadId,omitempty"`
	// Source is timeline, search or download.
	Source string `json:"source"`
}

// Store reads and writes timelines.
type Store struct {
	DB  *db.DB
	Now func() time.Time
	// OnRecord is told about every recorded event's item (the board refreshes it).
	OnRecord func(ref string)
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Record adds an event (data is marshalled to JSON; nil = {}).
func (s *Store) Record(ctx context.Context, ref, typ, message string, data any, downloadID *int64) error {
	raw := []byte("{}")
	if data != nil {
		var err error
		if raw, err = json.Marshal(data); err != nil {
			return err
		}
	}
	_, err := s.DB.W.ExecContext(ctx, `INSERT INTO timeline_events (item_ref, at, type, message, data, download_id) VALUES (?, ?, ?, ?, ?, ?)`,
		ref, db.Millis(s.now()), typ, message, string(raw), downloadID)
	if err == nil && s.OnRecord != nil {
		s.OnRecord(ref)
	}
	return err
}

// Delete removes an item's own events (the item is gone; searches and downloads stay).
func (s *Store) Delete(ctx context.Context, ref string) error {
	_, err := s.DB.W.ExecContext(ctx, `DELETE FROM timeline_events WHERE item_ref = ?`, ref)
	return err
}

// placeholders returns "?, ?, …" for n values and the values as arguments.
func placeholders(refs []string) (string, []any) {
	args := make([]any, len(refs))
	marks := make([]string, len(refs))
	for i, r := range refs {
		args[i], marks[i] = r, "?"
	}
	return strings.Join(marks, ", "), args
}

// List returns an item's timeline, newest first (at most limit entries).
func (s *Store) List(ctx context.Context, ref string, limit int) ([]Event, error) {
	return s.ListRefs(ctx, []string{ref}, limit)
}

// ListRefs returns one timeline for several related refs (an episode with
// its season and series), newest first. Downloads are those any of the refs
// is part of (download_refs), so a season pack shows on each episode.
func (s *Store) ListRefs(ctx context.Context, refs []string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	if len(refs) == 0 {
		return []Event{}, nil
	}
	marks, args := placeholders(refs)
	out := []Event{}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, item_ref, at, type, message, data, download_id FROM timeline_events
		WHERE item_ref IN (`+marks+`) ORDER BY at DESC, id DESC LIMIT ?`, append(args, limit)...) // #nosec G202 -- placeholders only
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			e    Event
			at   int64
			data string
		)
		if err := rows.Scan(&e.ID, &e.ItemRef, &at, &e.Type, &e.Message, &data, &e.DownloadID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		e.At, e.Data, e.Source = db.Time(at), json.RawMessage(data), "timeline"
		out = append(out, e)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	searches, err := s.searches(ctx, refs, limit)
	if err != nil {
		return nil, err
	}
	downloads, err := s.downloadEvents(ctx, refs, limit)
	if err != nil {
		return nil, err
	}
	out = append(append(out, searches...), downloads...)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return out[i].ID > out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// SearchIndexer is one indexer's part of a search event.
type SearchIndexer struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Count    int    `json:"count"`
	Cached   bool   `json:"cached,omitempty"`
	Error    string `json:"error,omitempty"`
	Duration int64  `json:"durationMs"`
}

// SearchData is the data of a search event.
type SearchData struct {
	Source      string          `json:"source"`
	Query       string          `json:"query"`
	ResultCount int             `json:"resultCount"`
	ErrorCount  int             `json:"errorCount"`
	DurationMs  int64           `json:"durationMs"`
	FinishedAt  time.Time       `json:"finishedAt"`
	Indexers    []SearchIndexer `json:"indexers"`
}

func (s *Store) searches(ctx context.Context, refs []string, limit int) ([]Event, error) {
	marks, args := placeholders(refs)
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, item_ref, started_at, duration_ms, source, query, result_count, error_count
		FROM search_log WHERE item_ref IN (`+marks+`) ORDER BY started_at DESC LIMIT ?`, append(args, limit)...) // #nosec G202 -- placeholders only
	if err != nil {
		return nil, err
	}
	type row struct {
		id  int64
		ref string
		at  int64
		sd  SearchData
		dur int64
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.ref, &r.at, &r.dur, &r.sd.Source, &r.sd.Query, &r.sd.ResultCount, &r.sd.ErrorCount); err != nil {
			_ = rows.Close()
			return nil, err
		}
		list = append(list, r)
	}
	_ = rows.Close()
	out := make([]Event, 0, len(list))
	for _, r := range list {
		r.sd.DurationMs = r.dur
		r.sd.FinishedAt = db.Time(r.at + r.dur)
		r.sd.Indexers = []SearchIndexer{}
		irows, err := s.DB.R.QueryContext(ctx, `SELECT indexer_name, status, result_count, cached, duration_ms, COALESCE(error, '')
			FROM search_log_indexers WHERE search_id = ? ORDER BY indexer_name`, r.id)
		if err != nil {
			return nil, err
		}
		for irows.Next() {
			var si SearchIndexer
			if err := irows.Scan(&si.Name, &si.Status, &si.Count, &si.Cached, &si.Duration, &si.Error); err != nil {
				_ = irows.Close()
				return nil, err
			}
			r.sd.Indexers = append(r.sd.Indexers, si)
		}
		_ = irows.Close()
		data, _ := json.Marshal(r.sd)
		out = append(out, Event{ID: r.id, ItemRef: r.ref, At: db.Time(r.at), Type: TypeSearch, Message: r.sd.Query, Data: data, Source: "search"})
	}
	return out, nil
}

// DownloadData is the data of a download event.
type DownloadData struct {
	DownloadID int64   `json:"downloadId"`
	Title      string  `json:"title"`
	Indexer    string  `json:"indexer,omitempty"`
	Client     string  `json:"client,omitempty"`
	Protocol   string  `json:"protocol,omitempty"`
	FromState  string  `json:"fromState,omitempty"`
	ToState    string  `json:"toState,omitempty"`
	Progress   float64 `json:"progress"`
}

func (s *Store) downloadEvents(ctx context.Context, refs []string, limit int) ([]Event, error) {
	marks, args := placeholders(refs)
	rows, err := s.DB.R.QueryContext(ctx, `SELECT e.id, e.at, e.type, e.from_state, e.to_state, e.message, d.id, d.title, d.indexer,
		d.client_name, d.protocol, d.progress, COALESCE(json_extract(d.item_ref, '$.ref'), '')
		FROM downloads d JOIN download_events e ON e.download_id = d.id
		WHERE d.id IN (SELECT download_id FROM download_refs WHERE item_ref IN (`+marks+`))
		ORDER BY e.at DESC, e.id DESC LIMIT ?`, append(args, limit)...) // #nosec G202 -- placeholders only
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var (
			e   Event
			at  int64
			typ string
			dd  DownloadData
		)
		if err := rows.Scan(&e.ID, &at, &typ, &dd.FromState, &dd.ToState, &e.Message, &dd.DownloadID, &dd.Title, &dd.Indexer,
			&dd.Client, &dd.Protocol, &dd.Progress, &e.ItemRef); err != nil {
			return nil, err
		}
		id := dd.DownloadID
		data, _ := json.Marshal(dd)
		e.At, e.Type, e.Data, e.DownloadID, e.Source = db.Time(at), TypeDownload+typ, data, &id, "download"
		out = append(out, e)
	}
	return out, rows.Err()
}
