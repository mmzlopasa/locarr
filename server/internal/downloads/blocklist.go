package downloads

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// Blocklist reasons.
const (
	ReasonManual   = "manual"
	ReasonFailed   = "download_failed"
	ReasonStalled  = "stalled"
	ReasonImport   = "import_failed"
	maxBlockReason = 64
)

// BlockEntry is a release that must not be grabbed again.
type BlockEntry struct {
	ID         int64
	Title      string
	Protocol   string
	IndexerID  *int64
	Indexer    string
	InfoHash   string
	GUID       string
	Reason     string
	Message    string
	ItemRef    json.RawMessage
	DownloadID *int64
	CreatedAt  time.Time
}

// Candidate is a release the decision engine is considering.
type Candidate struct {
	Title     string
	Protocol  string
	IndexerID *int64
	InfoHash  string
	GUID      string
}

// BlocklistChecker is the hook the decision engine (P2) calls for every candidate.
type BlocklistChecker interface {
	IsBlocklisted(ctx context.Context, c Candidate) (bool, string)
}

var _ BlocklistChecker = (*Service)(nil)

type blockIndex struct {
	hashes map[string]string
	guids  map[string]string
	titles map[string][]blockTitle
}

type blockTitle struct {
	indexerID *int64
	protocol  string
	reason    string
}

func normTitle(t string) string { return strings.ToLower(strings.Join(strings.Fields(t), " ")) }

func (s *Service) resetBlockIndex() {
	s.mu.Lock()
	s.block = nil
	s.mu.Unlock()
}

func (s *Service) loadBlockIndex(ctx context.Context) (*blockIndex, error) {
	s.mu.Lock()
	if s.block != nil {
		b := s.block
		s.mu.Unlock()
		return b, nil
	}
	s.mu.Unlock()
	rows, err := s.DB.R.QueryContext(ctx, `SELECT title, protocol, indexer_id, info_hash, guid, reason FROM blocklist`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	b := &blockIndex{hashes: map[string]string{}, guids: map[string]string{}, titles: map[string][]blockTitle{}}
	for rows.Next() {
		var (
			title, proto, hash, guid, reason string
			idx                              sql.NullInt64
		)
		if err := rows.Scan(&title, &proto, &idx, &hash, &guid, &reason); err != nil {
			return nil, err
		}
		if hash != "" {
			b.hashes[strings.ToLower(hash)] = reason
		}
		if guid != "" {
			b.guids[guid] = reason
		}
		bt := blockTitle{protocol: proto, reason: reason}
		if idx.Valid {
			v := idx.Int64
			bt.indexerID = &v
		}
		b.titles[normTitle(title)] = append(b.titles[normTitle(title)], bt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.block = b
	s.mu.Unlock()
	return b, nil
}

// IsBlocklisted reports whether a candidate matches the blocklist: same info
// hash, same GUID, or the same title from the same indexer (or protocol when
// the indexer is unknown). Lookups are in memory; errors count as "not blocked"
// and are logged.
func (s *Service) IsBlocklisted(ctx context.Context, c Candidate) (bool, string) {
	b, err := s.loadBlockIndex(ctx)
	if err != nil {
		s.Log.Warn("blocklist", "err", err)
		return false, ""
	}
	if h := strings.ToLower(strings.TrimSpace(c.InfoHash)); h != "" {
		if r, ok := b.hashes[h]; ok {
			return true, r
		}
	}
	if c.GUID != "" {
		if r, ok := b.guids[c.GUID]; ok {
			return true, r
		}
	}
	for _, bt := range b.titles[normTitle(c.Title)] {
		switch {
		case bt.indexerID != nil && c.IndexerID != nil:
			if *bt.indexerID == *c.IndexerID {
				return true, bt.reason
			}
		case bt.protocol == "" || c.Protocol == "" || bt.protocol == c.Protocol:
			return true, bt.reason
		}
	}
	return false, ""
}

func (s *Service) addBlock(ctx context.Context, q db.Querier, e BlockEntry) (BlockEntry, error) {
	e.Title = strings.TrimSpace(e.Title)
	if e.Title == "" {
		return e, invalid("title is required")
	}
	e.Reason = strings.TrimSpace(e.Reason)
	if e.Reason == "" {
		e.Reason = ReasonManual
	}
	if len(e.Reason) > maxBlockReason {
		return e, invalid("reason is too long")
	}
	if len(e.ItemRef) == 0 {
		e.ItemRef = json.RawMessage("{}")
	}
	if !json.Valid(e.ItemRef) {
		return e, invalid("itemRef must be JSON")
	}
	e.InfoHash = strings.ToLower(strings.TrimSpace(e.InfoHash))
	e.CreatedAt = s.now()
	err := q.QueryRowContext(ctx, `INSERT INTO blocklist (title, protocol, indexer_id, indexer, info_hash, guid, reason, message,
		item_ref, download_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		e.Title, e.Protocol, e.IndexerID, e.Indexer, e.InfoHash, e.GUID, e.Reason, e.Message, string(e.ItemRef),
		e.DownloadID, db.Millis(e.CreatedAt)).Scan(&e.ID)
	return e, err
}

// AddBlock blocks a release (used by importers and the decision engine too).
func (s *Service) AddBlock(ctx context.Context, e BlockEntry) (BlockEntry, error) {
	out, err := s.addBlock(ctx, s.DB.W, e)
	if err == nil {
		s.resetBlockIndex()
	}
	return out, err
}

// Blocklist lists entries, newest first.
func (s *Service) Blocklist(ctx context.Context, limit int) ([]BlockEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, title, protocol, indexer_id, indexer, info_hash, guid, reason, message,
		item_ref, download_id, created_at FROM blocklist ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlockEntry{}
	for rows.Next() {
		var (
			e       BlockEntry
			idx, dl sql.NullInt64
			ref     string
			created int64
		)
		if err := rows.Scan(&e.ID, &e.Title, &e.Protocol, &idx, &e.Indexer, &e.InfoHash, &e.GUID, &e.Reason, &e.Message,
			&ref, &dl, &created); err != nil {
			return nil, err
		}
		if idx.Valid {
			e.IndexerID = &idx.Int64
		}
		if dl.Valid {
			e.DownloadID = &dl.Int64
		}
		e.ItemRef, e.CreatedAt = json.RawMessage(ref), db.Time(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteBlock removes an entry (the release may be grabbed again).
func (s *Service) DeleteBlock(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM blocklist WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.resetBlockIndex()
	return nil
}
