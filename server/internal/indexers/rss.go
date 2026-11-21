package indexers

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// rssParallel is how many indexers the RSS sync fetches at once.
const rssParallel = 4

// RSSResult summarises one RSS sync.
type RSSResult struct {
	Indexers int
	Fetched  int
	New      int
	Pruned   int64
	Errors   int
}

// SyncRSS fetches the latest releases of every RSS-enabled indexer (respecting
// backoff and request limits), stores the ones not seen before in
// releases_seen, publishes them to RSS subscribers and prunes old ones.
func (s *Service) SyncRSS(ctx context.Context) (RSSResult, error) {
	start := time.Now()
	q := Query{Source: SourceRSS, Type: SearchAny}
	inds, err := s.eligible(ctx, q)
	if err != nil {
		return RSSResult{}, err
	}
	var (
		mu    sync.Mutex
		res   = RSSResult{Indexers: len(inds)}
		fresh []Release
		parts = Result{Releases: []Release{}, Indexers: []IndexerResult{}}
		wg    sync.WaitGroup
		sem   = make(chan struct{}, rssParallel)
	)
	for _, ind := range inds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// RSS asks for every configured category (or all when none are set).
			rq := q
			rq.Categories = ind.Categories.All()
			ir, releases := s.searchOne(ctx, ind, rq)
			added, err := s.storeSeen(ctx, ind.ID, releases)
			mu.Lock()
			defer mu.Unlock()
			parts.Indexers = append(parts.Indexers, ir)
			if ir.Status == StatusError {
				res.Errors++
			}
			res.Fetched += len(releases)
			if err != nil {
				s.Log.Warn("store RSS releases", "indexer", ind.Name, "err", err)
				res.Errors++
				return
			}
			res.New += len(added)
			fresh = append(fresh, added...)
		}()
	}
	wg.Wait()
	parts.Releases = fresh
	parts.Duration = time.Since(start)
	if len(inds) > 0 {
		s.writeLog(ctx, q, start, parts)
	}
	if dropped := s.bus.publish(fresh); dropped > 0 {
		s.Log.Warn("RSS subscribers did not keep up; they can catch up from releases_seen", "dropped", dropped)
	}
	days, err := settings.Get(ctx, s.Settings, ReleaseRetentionDays)
	if err != nil {
		return res, err
	}
	pr, err := s.DB.W.ExecContext(ctx, `DELETE FROM releases_seen WHERE first_seen_at < ?`,
		db.Millis(s.now().Add(-time.Duration(days)*24*time.Hour)))
	if err != nil {
		return res, err
	}
	res.Pruned, _ = pr.RowsAffected()
	return res, nil
}

// storeSeen inserts releases not seen before and returns those.
func (s *Service) storeSeen(ctx context.Context, indexerID int64, releases []Release) ([]Release, error) {
	if len(releases) == 0 {
		return nil, nil
	}
	var added []Release
	now := db.Millis(s.now())
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO releases_seen (indexer_id, guid, info_hash, title, size, publish_date,
			first_seen_at, data) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (indexer_id, guid) DO NOTHING`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, r := range releases {
			data, err := json.Marshal(r)
			if err != nil {
				return err
			}
			var pub, size any
			if !r.PublishDate.IsZero() {
				pub = db.Millis(r.PublishDate)
			}
			if r.Size > 0 {
				size = r.Size
			}
			res, err := stmt.ExecContext(ctx, indexerID, r.GUID, strArg(r.InfoHash), r.Title, size, pub, now, string(data))
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 1 {
				added = append(added, r)
			}
		}
		return nil
	})
	return added, err
}

// SeenSince returns releases the RSS sync first saw after t (oldest first, at
// most limit), for consumers that start late or missed published batches.
func (s *Service) SeenSince(ctx context.Context, t time.Time, limit int) ([]Release, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT data FROM releases_seen WHERE first_seen_at > ?
		ORDER BY first_seen_at, id LIMIT ?`, db.Millis(t), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Release{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var r Release
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
