package downloads

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/logging"
)

// Status is where a download is in Locarr's own life cycle.
type Status string

// Statuses.
const (
	StatusActive    Status = "active"    // in the client, not finished (or finished but not visible locally yet)
	StatusCompleted Status = "completed" // finished and visible locally: ready to import
	StatusImported  Status = "imported"  // the importer took it; may still be seeding
	StatusFailed    Status = "failed"
	StatusRemoved   Status = "removed"
)

// Download is one release sent to a client.
type Download struct {
	ID            int64
	ClientID      *int64
	ClientName    string
	ClientItemID  string
	Protocol      downloaders.Protocol
	Title         string
	IndexerID     *int64
	Indexer       string
	GUID          string
	InfoHash      string
	Category      string
	MediaKind     string
	ItemRef       json.RawMessage
	SeedRatio     *float64
	SeedTime      *time.Duration
	Status        Status
	State         downloaders.State
	RawState      string
	Progress      float64
	Size          int64
	Downloaded    int64
	DownloadSpeed int64
	UploadSpeed   int64
	ETA           *time.Duration
	Ratio         float64
	SeedingTime   time.Duration
	SavePath      string
	ContentPath   string
	LocalPath     string
	Message       string
	StalledSince  *time.Time
	MissingCount  int
	AddedAt       time.Time
	UpdatedAt     time.Time
	CompletedAt   *time.Time
	FinishedAt    *time.Time
}

// HistoryEvent is one row of download_events.
type HistoryEvent struct {
	ID         int64
	DownloadID int64
	At         time.Time
	Type       EventType
	FromState  string
	ToState    string
	Message    string
	Data       json.RawMessage
}

const downloadColumns = `id, client_id, client_name, client_item_id, protocol, title, indexer_id, indexer, guid, info_hash,
	category, media_kind, item_ref, seed_ratio, seed_time, status, state, raw_state, progress, size, downloaded,
	download_speed, upload_speed, eta, ratio, seeding_time, save_path, content_path, local_path, message,
	stalled_since, missing_count, added_at, updated_at, completed_at, finished_at`

func scanDownload(row interface{ Scan(...any) error }) (Download, error) {
	var (
		d                         Download
		clientID, indexerID       sql.NullInt64
		seedRatio                 sql.NullFloat64
		seedTime, eta             sql.NullInt64
		ref, proto, status, state string
		seeding                   int64
		stalled, completed, fin   sql.NullInt64
		added, updated            int64
	)
	err := row.Scan(&d.ID, &clientID, &d.ClientName, &d.ClientItemID, &proto, &d.Title, &indexerID, &d.Indexer, &d.GUID,
		&d.InfoHash, &d.Category, &d.MediaKind, &ref, &seedRatio, &seedTime, &status, &state, &d.RawState, &d.Progress,
		&d.Size, &d.Downloaded, &d.DownloadSpeed, &d.UploadSpeed, &eta, &d.Ratio, &seeding, &d.SavePath, &d.ContentPath,
		&d.LocalPath, &d.Message, &stalled, &d.MissingCount, &added, &updated, &completed, &fin)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	if clientID.Valid {
		d.ClientID = &clientID.Int64
	}
	if indexerID.Valid {
		d.IndexerID = &indexerID.Int64
	}
	if seedRatio.Valid {
		d.SeedRatio = &seedRatio.Float64
	}
	if seedTime.Valid {
		v := time.Duration(seedTime.Int64) * time.Second
		d.SeedTime = &v
	}
	if eta.Valid {
		v := time.Duration(eta.Int64) * time.Second
		d.ETA = &v
	}
	d.ItemRef = json.RawMessage(ref)
	d.Protocol, d.Status, d.State = downloaders.Protocol(proto), Status(status), downloaders.State(state)
	d.SeedingTime = time.Duration(seeding) * time.Second
	d.StalledSince, d.CompletedAt, d.FinishedAt = db.NullTime(stalled), db.NullTime(completed), db.NullTime(fin)
	d.AddedAt, d.UpdatedAt = db.Time(added), db.Time(updated)
	return d, nil
}

// Get returns one download.
func (s *Service) Get(ctx context.Context, id int64) (Download, error) {
	return scanDownload(s.DB.R.QueryRowContext(ctx, `SELECT `+downloadColumns+` FROM downloads WHERE id = ?`, id))
}

// Filter selects downloads.
type Filter struct {
	Statuses []Status
	ClientID int64
	Limit    int
}

// List returns downloads, newest first.
func (s *Service) List(ctx context.Context, f Filter) ([]Download, error) {
	q := `SELECT ` + downloadColumns + ` FROM downloads WHERE 1 = 1`
	var args []any
	if len(f.Statuses) > 0 {
		// Only placeholders are added to the query; the values are arguments.
		q += ` AND status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(f.Statuses)), ",") + `)` // #nosec G202
		for _, st := range f.Statuses {
			args = append(args, string(st))
		}
	}
	if f.ClientID > 0 {
		q += ` AND client_id = ?`
		args = append(args, f.ClientID)
	}
	q += ` ORDER BY added_at DESC, id DESC`
	if f.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	rows, err := s.DB.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Download{}
	for rows.Next() {
		d, err := scanDownload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Events returns a download's history, oldest first.
func (s *Service) Events(ctx context.Context, downloadID int64) ([]HistoryEvent, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, download_id, at, type, from_state, to_state, message, data
		FROM download_events WHERE download_id = ? ORDER BY at, id`, downloadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HistoryEvent{}
	for rows.Next() {
		var (
			e    HistoryEvent
			at   int64
			typ  string
			data string
		)
		if err := rows.Scan(&e.ID, &e.DownloadID, &at, &typ, &e.FromState, &e.ToState, &e.Message, &data); err != nil {
			return nil, err
		}
		e.At, e.Type, e.Data = db.Time(at), EventType(typ), json.RawMessage(data)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Service) addEvent(ctx context.Context, q db.Querier, id int64, typ EventType, from, to, msg string, data any) error {
	raw := []byte("{}")
	if data != nil {
		if b, err := json.Marshal(data); err == nil {
			raw = b
		}
	}
	_, err := q.ExecContext(ctx, `INSERT INTO download_events (download_id, at, type, from_state, to_state, message, data)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, db.Millis(s.now()), string(typ), from, to, logging.ScrubStored(msg), string(logging.ScrubStoredJSON(raw)))
	return err
}

func nullDur(d *time.Duration) any {
	if d == nil {
		return nil
	}
	return int64(*d / time.Second)
}

func nullMillis(t *time.Time) any {
	if t == nil {
		return nil
	}
	return db.Millis(*t)
}

// save writes the tracked fields of d.
func save(ctx context.Context, q db.Querier, d *Download) error {
	_, err := q.ExecContext(ctx, `UPDATE downloads SET status = ?, state = ?, raw_state = ?, progress = ?, size = ?,
		downloaded = ?, download_speed = ?, upload_speed = ?, eta = ?, ratio = ?, seeding_time = ?, save_path = ?,
		content_path = ?, local_path = ?, message = ?, stalled_since = ?, missing_count = ?, updated_at = ?,
		completed_at = ?, finished_at = ? WHERE id = ?`,
		string(d.Status), string(d.State), d.RawState, d.Progress, d.Size, d.Downloaded, d.DownloadSpeed, d.UploadSpeed,
		nullDur(d.ETA), d.Ratio, int64(d.SeedingTime/time.Second), d.SavePath, d.ContentPath, d.LocalPath, d.Message,
		nullMillis(d.StalledSince), d.MissingCount, db.Millis(d.UpdatedAt), nullMillis(d.CompletedAt),
		nullMillis(d.FinishedAt), d.ID)
	return err
}

// MarkImported records that the importer took a completed download (P4-P6
// call it after the destination is verified). With "remove completed downloads"
// on, the tracker removes it from the client once its seeding goals are met.
func (s *Service) MarkImported(ctx context.Context, id int64, message string) error {
	return s.finishImport(ctx, id, StatusImported, EventImported, message)
}

// MarkImportFailed records an import failure; the download stays completed so it can be retried.
func (s *Service) MarkImportFailed(ctx context.Context, id int64, message string) error {
	return s.finishImport(ctx, id, StatusCompleted, EventImportFailed, message)
}

func (s *Service) finishImport(ctx context.Context, id int64, to Status, typ EventType, message string) error {
	var d Download
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		d, err = scanDownload(tx.QueryRowContext(ctx, `SELECT `+downloadColumns+` FROM downloads WHERE id = ?`, id))
		if err != nil {
			return err
		}
		if d.Status != StatusCompleted && d.Status != StatusImported {
			return invalid("download %d is %s, not completed", id, d.Status)
		}
		from := d.Status
		d.Status, d.UpdatedAt = to, s.now()
		if to == StatusImported {
			now := s.now()
			d.FinishedAt = &now
			d.Message = ""
		} else {
			d.Message = message
		}
		if err := save(ctx, tx, &d); err != nil {
			return err
		}
		return s.addEvent(ctx, tx, id, typ, string(from), string(to), message, nil)
	})
	if err == nil && to == StatusImported {
		s.Bus.Publish(ctx, Event{Type: EventImported, Download: d, Message: message})
	}
	return err
}

func removeAll(p string) error { return os.RemoveAll(p) }
