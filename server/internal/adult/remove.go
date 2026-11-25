package adult

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// RemoveOptions say what removing an item, studio or performer also does.
type RemoveOptions struct {
	// DeleteFiles sends the files to the recycle bin (never deleted outright).
	DeleteFiles bool
	// Exclude remembers each removed scene or movie, so refreshing a followed
	// studio or performer does not add it again (D-110).
	Exclude bool
}

// Exclusion is one "not interested" entry.
type Exclusion struct {
	ID          int64     `json:"id"`
	Kind        string    `json:"kind"`
	StashDBID   string    `json:"stashdbId,omitempty"`
	TPDBID      string    `json:"tpdbId,omitempty"`
	Title       string    `json:"title"`
	Studio      string    `json:"studio,omitempty"`
	ReleaseDate string    `json:"releaseDate,omitempty"`
	AddedAt     time.Time `json:"addedAt"`
}

// DeleteItem removes an item; with DeleteFiles its file goes to the recycle
// bin, with Exclude it is not added again by a refresh.
func (s *Service) DeleteItem(ctx context.Context, id int64, o RemoveOptions) error {
	it, err := s.Item(ctx, id)
	if err != nil {
		return err
	}
	if o.DeleteFiles && it.File != nil && s.Recycle != nil {
		if _, err := s.Recycle.Recycle(ctx, it.File.Path, it.Ref().String(), "deleted"); err != nil {
			return err
		}
	}
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if o.Exclude && (it.StashDBID != "" || it.TPDBID != "") {
			if err := excludeTx(ctx, tx, &it, db.Millis(s.now())); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM adult_items WHERE id = ?`, id)
		return err
	})
	if err != nil {
		return err
	}
	_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM search_state WHERE item_ref = ?`, it.Ref().String())
	_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM pending_releases WHERE item_ref = ?`, it.Ref().String())
	return nil
}

func excludeTx(ctx context.Context, tx *sql.Tx, it *Item, now int64) error {
	// Either ID may already be listed (the other source added later): merge.
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM adult_exclusions WHERE (stashdb_id <> '' AND stashdb_id = ?) OR (tpdb_id <> '' AND tpdb_id = ?)
		LIMIT 1`, it.StashDBID, it.TPDBID).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx, `INSERT INTO adult_exclusions (kind, stashdb_id, tpdb_id, title, studio_name, release_date, added_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, it.Kind, it.StashDBID, it.TPDBID, it.Title, it.Studio, it.ReleaseDate, now)
		return err
	case err != nil:
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE adult_exclusions SET stashdb_id = CASE WHEN stashdb_id = '' THEN ? ELSE stashdb_id END,
		tpdb_id = CASE WHEN tpdb_id = '' THEN ? ELSE tpdb_id END WHERE id = ?`, it.StashDBID, it.TPDBID, id)
	return err
}

// Excluded reports whether a source entry is on the "not interested" list.
func (s *Service) Excluded(ctx context.Context, source, id string) bool {
	stash, tpdb := ids(source, id)
	var n int
	_ = s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM adult_exclusions WHERE (stashdb_id <> '' AND stashdb_id = ?) OR (tpdb_id <> '' AND tpdb_id = ?)`,
		stash, tpdb).Scan(&n)
	return n > 0
}

// unexclude forgets an entry (adding it by hand is an explicit choice).
func (s *Service) unexclude(ctx context.Context, source, id string) {
	stash, tpdb := ids(source, id)
	_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM adult_exclusions WHERE (stashdb_id <> '' AND stashdb_id = ?) OR (tpdb_id <> '' AND tpdb_id = ?)`,
		stash, tpdb)
}

// Exclusions lists the "not interested" entries, newest first.
func (s *Service) Exclusions(ctx context.Context) ([]Exclusion, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, kind, stashdb_id, tpdb_id, title, studio_name, release_date, added_at
		FROM adult_exclusions ORDER BY added_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Exclusion{}
	for rows.Next() {
		var (
			e     Exclusion
			added int64
		)
		if err := rows.Scan(&e.ID, &e.Kind, &e.StashDBID, &e.TPDBID, &e.Title, &e.Studio, &e.ReleaseDate, &added); err != nil {
			return nil, err
		}
		e.AddedAt = db.Time(added)
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteExclusion undoes one "not interested" entry; the next refresh may
// add the scene again.
func (s *Service) DeleteExclusion(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM adult_exclusions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteStudio removes a studio. Without keepScenes its scenes go too (files
// to the recycle bin only with DeleteFiles); with keepScenes they stay in
// the library without a studio page and are no longer refreshed.
func (s *Service) DeleteStudio(ctx context.Context, id int64, keepScenes bool, o RemoveOptions) error {
	if _, err := s.Studio(ctx, id); err != nil {
		return err
	}
	if keepScenes {
		return s.DB.Tx(ctx, func(tx *sql.Tx) error {
			// adult_items.studio_id cascades on delete: detach first.
			if _, err := tx.ExecContext(ctx, `UPDATE adult_items SET studio_id = NULL, updated_at = ? WHERE studio_id = ?`,
				db.Millis(s.now()), id); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `DELETE FROM adult_studios WHERE id = ?`, id)
			return err
		})
	}
	items, _, err := s.Items(ctx, ItemFilter{StudioID: id, Limit: -1})
	if err != nil {
		return err
	}
	for _, it := range items {
		if err := s.DeleteItem(ctx, it.ID, o); err != nil {
			return err
		}
	}
	_, err = s.DB.W.ExecContext(ctx, `DELETE FROM adult_studios WHERE id = ?`, id)
	return err
}

// DeletePerformer forgets a performer. Their scenes stay unless
// removeScenes; then each goes as with DeleteItem.
func (s *Service) DeletePerformer(ctx context.Context, id int64, removeScenes bool, o RemoveOptions) error {
	if _, err := s.Performer(ctx, id); err != nil {
		return err
	}
	if removeScenes {
		items, _, err := s.Items(ctx, ItemFilter{PerformerID: id, Limit: -1})
		if err != nil {
			return err
		}
		for _, it := range items {
			if err := s.DeleteItem(ctx, it.ID, o); err != nil {
				return err
			}
		}
	}
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM adult_performers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Bulk actions on items.
const (
	BulkRemove    = "remove"
	BulkMonitor   = "monitor"
	BulkUnmonitor = "unmonitor"
)

// MaxBulk is the most items one bulk call takes.
const MaxBulk = 500

// BulkItems applies one action to many items and returns how many changed.
// Unknown IDs are skipped.
func (s *Service) BulkItems(ctx context.Context, ids []int64, action string, o RemoveOptions) (int, error) {
	if len(ids) > MaxBulk {
		ids = ids[:MaxBulk]
	}
	n := 0
	switch action {
	case BulkRemove:
		for _, id := range ids {
			err := s.DeleteItem(ctx, id, o)
			switch {
			case err == nil:
				n++
			case errors.Is(err, ErrNotFound):
			default:
				return n, err
			}
		}
	case BulkMonitor, BulkUnmonitor:
		mon := action == BulkMonitor
		err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
			for _, id := range ids {
				res, err := tx.ExecContext(ctx, `UPDATE adult_items SET monitored = ?, updated_at = ? WHERE id = ? AND monitored <> ?`,
					mon, db.Millis(s.now()), id, mon)
				if err != nil {
					return err
				}
				c, _ := res.RowsAffected()
				n += int(c)
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
		if mon && s.Wanted != nil {
			for _, id := range ids {
				if it, err := s.Item(ctx, id); err == nil && it.File == nil {
					_ = s.Wanted.Enqueue(ctx, it.Ref(), "monitored")
				}
			}
		}
	default:
		return 0, ErrBadAction
	}
	return n, nil
}
