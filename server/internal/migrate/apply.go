package migrate

import (
	"context"
	"fmt"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// Progress reports how far an apply is.
type Progress func(done, total int, what string)

// Apply runs the plan in order: every item that is not skipped is created
// or merged through the Locarr services; items of one batch (movies) go
// together. A failing item does not stop the others. Links are written so a
// second run updates instead of duplicating.
func (p *planner) Apply(ctx context.Context, runID int64, progress Progress) error {
	items := p.plan.Items
	total := len(items)
	report := func(i int, what string) {
		if progress != nil {
			progress(i, total, what)
		}
	}
	for i := 0; i < len(items); {
		if err := ctx.Err(); err != nil {
			return err
		}
		it := items[i]
		if it.batch != "" && it.Action != ActionSkip {
			j := i
			var group []*Item
			for j < len(items) && (items[j].batch == it.batch || (items[j].Category == it.Category && items[j].Action == ActionSkip)) && len(group) < 2000 {
				if items[j].Action != ActionSkip {
					group = append(group, items[j])
				} else {
					p.finishSkip(items[j])
				}
				j++
			}
			if it.batch == "movies" {
				p.applyMovies(ctx, group)
			}
			for _, g := range group {
				p.writeLink(ctx, g, runID)
			}
			i = j
			report(i, it.Category)
			continue
		}
		if it.Action == ActionSkip {
			p.finishSkip(it)
			if it.run != nil {
				// A duplicate of another application's item: linked to it so
				// the next run knows it.
				if ref, err := it.run(ctx); err == nil && ref != "" {
					it.LocalRef = ref
					p.writeLink(ctx, it, runID)
				}
			}
		} else if it.run != nil {
			ref, err := it.run(ctx)
			if err != nil {
				it.Status, it.Error = "failed", err.Error()
			} else {
				it.Status = "done"
				if ref != "" {
					it.LocalRef = ref
				}
				p.writeLink(ctx, it, runID)
			}
		} else {
			it.Status = "done"
		}
		i++
		if i%20 == 0 || i == total {
			report(i, it.Category)
		}
	}
	report(total, "done")
	return nil
}

func (p *planner) finishSkip(it *Item) {
	if it.Status == "planned" {
		it.Status = "skipped"
	}
}

func (p *planner) writeLink(ctx context.Context, it *Item, runID int64) {
	if it.link == nil || (it.Status != "done" && it.Status != "skipped") || it.LocalRef == "" {
		return
	}
	l := it.link
	_, err := p.l.DB.W.ExecContext(ctx, `INSERT INTO migrate_links (source_key, entity, external_id, local_ref, hash, run_id, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT (source_key, entity, external_id) DO UPDATE SET local_ref = excluded.local_ref,
		hash = excluded.hash, run_id = excluded.run_id, updated_at = excluded.updated_at`,
		l.sourceKey, l.entity, l.external, it.LocalRef, l.hash, runID, db.Millis(time.Now()))
	if err != nil && it.Error == "" {
		it.Error = fmt.Sprintf("the import mark could not be saved: %v", err)
	}
}

// Refresh lists the library items the apply created (their metadata is
// refreshed later, in the background).
func (p *planner) Refresh() []string { return append([]string(nil), p.refresh...) }
