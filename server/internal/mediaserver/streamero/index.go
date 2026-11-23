package streamero

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
)

// IndexInterval is how often the index is refreshed besides after scans.
const IndexInterval = 6 * time.Hour

// QueueIndex schedules an index refresh after delay.
func (s *Service) QueueIndex(ctx context.Context, delay time.Duration) {
	if s.Jobs == nil {
		return
	}
	if _, _, err := s.Jobs.Enqueue(ctx, TaskIndex, nil, jobs.EnqueueOptions{UniqueKey: TaskIndex, RunAt: s.now().Add(delay)}); err != nil {
		s.log().Warn("queue Streamero index", "err", err)
	}
}

// Refresh brings the index of titles Streamero has up to date (D-064).
// Streamero's listing has no provider ids, so each new item's details are
// read once; known items are only re-read while they still have no ids (its
// metadata may arrive later). Items no longer listed are removed.
func (s *Service) Refresh(ctx context.Context) (int, error) {
	cl, _, err := s.Client(ctx)
	if errors.Is(err, ErrNotConfigured) {
		_, err := s.DB.W.ExecContext(ctx, `DELETE FROM streamero_items`)
		return 0, err
	}
	if err != nil {
		return 0, err
	}
	n, err := s.refresh(ctx, cl)
	now := s.now()
	if err != nil {
		msg := err.Error()
		s.setStatus(func(st *Status) { st.IndexError = msg })
		return n, err
	}
	s.setStatus(func(st *Status) { st.IndexedAt, st.IndexError = &now, "" })
	return n, nil
}

type known struct {
	hasIDs bool
}

func (s *Service) refresh(ctx context.Context, cl *Client) (int, error) {
	libs, err := cl.Libraries(ctx)
	if err != nil {
		return 0, err
	}
	have := map[string]known{}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, tmdb_id <> '' OR imdb_id <> '' OR tvdb_id <> '' FROM streamero_items`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var (
			id  string
			ids bool
		)
		if err := rows.Scan(&id, &ids); err != nil {
			_ = rows.Close()
			return 0, err
		}
		have[id] = known{hasIDs: ids}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	var todo []struct {
		item Item
		lib  string
	}
	for _, l := range libs {
		switch l.Kind {
		case "movies", "shows", "mixed":
		default:
			continue
		}
		for off := 0; ; off += 200 {
			items, total, err := cl.Items(ctx, l.ID, off, 200)
			if err != nil {
				return 0, err
			}
			for _, it := range items {
				if it.Kind != "movie" && it.Kind != "series" {
					continue
				}
				seen[it.ID] = true
				if k, ok := have[it.ID]; !ok || !k.hasIDs {
					todo = append(todo, struct {
						item Item
						lib  string
					}{it, l.ID})
				}
			}
			if len(items) == 0 || off+len(items) >= total {
				break
			}
		}
	}
	// Details of new items, a few at a time.
	type res struct {
		item Item
		lib  string
		ids  map[string]string
		err  error
	}
	out := make([]res, len(todo))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, t := range todo {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			ids, err := cl.ProviderIDs(ctx, t.item.ID)
			out[i] = res{item: t.item, lib: t.lib, ids: ids, err: err}
		}()
	}
	wg.Wait()
	now := db.Millis(s.now())
	for _, r := range out {
		if r.err != nil {
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			s.log().Debug("Streamero item details", "item", r.item.ID, "err", r.err)
			continue
		}
		if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO streamero_items (id, library_id, kind, title, year, tmdb_id, imdb_id, tvdb_id, checked_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (id) DO UPDATE SET library_id = excluded.library_id, kind = excluded.kind,
			title = excluded.title, year = excluded.year, tmdb_id = excluded.tmdb_id, imdb_id = excluded.imdb_id, tvdb_id = excluded.tvdb_id,
			checked_at = excluded.checked_at`, r.item.ID, r.lib, r.item.Kind, r.item.Title, r.item.Year,
			providerID(r.ids, "tmdb"), strings.ToLower(providerID(r.ids, "imdb")), providerID(r.ids, "tvdb"), now); err != nil {
			return 0, err
		}
	}
	for id := range have {
		if !seen[id] {
			if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM streamero_items WHERE id = ?`, id); err != nil {
				return 0, err
			}
		}
	}
	var n int
	err = s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM streamero_items`).Scan(&n)
	return n, err
}

func providerID(ids map[string]string, name string) string {
	for k, v := range ids {
		if strings.EqualFold(k, name) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// Title identifies a title to look up.
type Title struct {
	Kind   string // movie or series
	TMDbID int64
	IMDbID string
	TVDbID int64
}

// Links returns "Watch in Streamero" links for titles Streamero has, keyed
// by the titles' positions. Nothing (and no error) while Streamero is off.
func (s *Service) Links(ctx context.Context, titles []Title) (map[int]string, error) {
	out := map[int]string{}
	if len(titles) == 0 {
		return out, nil
	}
	c, err := s.Load(ctx)
	if err != nil || !c.Enabled || c.URL == "" {
		return out, err
	}
	base := c.LinkURL()
	var keys []any
	for _, t := range titles {
		if t.TMDbID > 0 {
			keys = append(keys, strconv.FormatInt(t.TMDbID, 10))
		}
	}
	found := map[string]string{} // "movie:603" / "imdb:tt..." / "tvdb:1" → streamero id
	if len(keys) > 0 {
		q := `SELECT id, kind, tmdb_id FROM streamero_items WHERE tmdb_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",") + `)` // #nosec G202 -- placeholders only
		rows, err := s.DB.R.QueryContext(ctx, q, keys...)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var id, kind, tm string
			if err := rows.Scan(&id, &kind, &tm); err != nil {
				_ = rows.Close()
				return out, err
			}
			found[kind+":"+tm] = id
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return out, err
		}
	}
	for i, t := range titles {
		id := found[t.Kind+":"+strconv.FormatInt(t.TMDbID, 10)]
		if id == "" && (t.IMDbID != "" || t.TVDbID > 0) {
			_ = s.DB.R.QueryRowContext(ctx, `SELECT id FROM streamero_items WHERE kind = ? AND ((imdb_id <> '' AND imdb_id = ?) OR (tvdb_id <> '' AND tvdb_id = ?)) LIMIT 1`,
				t.Kind, strings.ToLower(t.IMDbID), strconv.FormatInt(t.TVDbID, 10)).Scan(&id)
		}
		if id != "" {
			out[i] = base + "/item/" + url.PathEscape(id)
		}
	}
	return out, nil
}
