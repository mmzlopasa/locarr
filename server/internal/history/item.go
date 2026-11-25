package history

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// ItemFilter selects the rows of one item's timeline (D-109): its own
// timeline events, its searches and the steps of downloads it is part of.
// Unlike the library-wide history, every event type counts. The caller has
// already checked that it may see the item (adult refs need view_adult).
type ItemFilter struct {
	Ref string
	// IDs limits the rows ("t:12" timeline, "s:7" search, "d:3" download
	// step); empty = every row of the item.
	IDs []string
	// Before keeps only rows older than this (zero = no limit).
	Before time.Time
}

type itemPart struct {
	table, where string
	args         []any
}

// itemParts builds one statement fragment per source (ids that are not
// valid match nothing).
func itemParts(f ItemFilter) []itemPart {
	ids := map[string][]any{}
	for _, id := range f.IDs {
		src, n, ok := parseItemID(id)
		if ok {
			ids[src] = append(ids[src], n)
		}
	}
	parts := []itemPart{
		{table: "timeline_events", where: "item_ref = ?", args: []any{f.Ref}},
		{table: "search_log", where: "item_ref = ?", args: []any{f.Ref}},
		{table: "download_events", where: `(download_id IN (SELECT download_id FROM download_refs WHERE item_ref = ?)
			OR download_id IN (SELECT id FROM downloads WHERE json_extract(item_ref, '$.ref') = ?))`, args: []any{f.Ref, f.Ref}},
	}
	srcs := []string{"t", "s", "d"}
	timeCol := []string{"at", "started_at", "at"}
	out := make([]itemPart, 0, len(parts))
	for i, p := range parts {
		if len(f.IDs) > 0 {
			list := ids[srcs[i]]
			if len(list) == 0 {
				continue
			}
			p.where += " AND id IN (" + marks(len(list)) + ")"
			p.args = append(p.args, list...)
		}
		if !f.Before.IsZero() {
			p.where += " AND " + timeCol[i] + " < ?"
			p.args = append(p.args, db.Millis(f.Before))
		}
		out = append(out, p)
	}
	return out
}

// parseItemID splits "t:12" / "s:7" / "d:3".
func parseItemID(id string) (string, int64, bool) {
	src, raw, found := strings.Cut(id, ":")
	if !found || (src != "t" && src != "s" && src != "d") {
		return "", 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	return src, n, err == nil && n > 0
}

// CountItem is how many rows ClearItem would delete.
func (s *Store) CountItem(ctx context.Context, f ItemFilter) (int64, error) {
	if f.Ref == "" {
		return 0, ErrNotFound
	}
	var total int64
	for _, p := range itemParts(f) {
		var n int64
		if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+p.table+` WHERE `+p.where, p.args...).Scan(&n); err != nil { // #nosec G202 -- fixed fragments
			return 0, err
		}
		total += n
	}
	return total, nil
}

// ClearItem deletes the selected rows of an item's timeline and returns how
// many went. Files, the blocklist, release decisions already acted on, the
// search schedule and the downloads themselves stay.
func (s *Store) ClearItem(ctx context.Context, f ItemFilter) (int64, error) {
	if f.Ref == "" {
		return 0, ErrNotFound
	}
	var total int64
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		for _, p := range itemParts(f) {
			res, err := tx.ExecContext(ctx, `DELETE FROM `+p.table+` WHERE `+p.where, p.args...) // #nosec G202 -- fixed fragments
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

// DeleteItemEntry deletes one row of an item's timeline.
func (s *Store) DeleteItemEntry(ctx context.Context, ref, id string) error {
	if _, _, ok := parseItemID(id); !ok {
		return ErrNotFound
	}
	n, err := s.ClearItem(ctx, ItemFilter{Ref: ref, IDs: []string{id}})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
