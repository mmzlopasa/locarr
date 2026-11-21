package indexers

import (
	"context"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// limiter enforces each indexer's "N requests per period" in memory (one
// process runs Locarr, D-001).
type limiter struct {
	mu   sync.Mutex
	hits map[int64][]time.Time
}

func newLimiter() *limiter { return &limiter{hits: map[int64][]time.Time{}} }

// allow records a request for id at now when fewer than limit happened in the
// last period; otherwise it reports how long until the next one may go.
func (l *limiter) allow(id int64, limit int, period time.Duration, now time.Time) (bool, time.Duration) {
	if limit <= 0 || period <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-period)
	h := l.hits[id]
	i := 0
	for i < len(h) && !h[i].After(cut) {
		i++
	}
	h = h[i:]
	if len(h) >= limit {
		l.hits[id] = h
		return false, h[0].Add(period).Sub(now)
	}
	l.hits[id] = append(h, now)
	return true, 0
}

// allowRequest applies an indexer's request limit.
func (s *Service) allowRequest(ind Indexer) (bool, time.Duration) {
	if ind.RequestLimit == nil || ind.RequestPeriodSec == nil {
		return true, 0
	}
	return s.limiter.allow(ind.ID, *ind.RequestLimit, time.Duration(*ind.RequestPeriodSec)*time.Second, s.now())
}

// GrabsToday counts the indexer's grabs in the last 24 hours.
func (s *Service) GrabsToday(ctx context.Context, id int64) (int, error) {
	var n int
	err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM indexer_grabs WHERE indexer_id = ? AND at > ?`,
		id, db.Millis(s.now().Add(-24*time.Hour))).Scan(&n)
	return n, err
}

// ReserveGrab checks the indexer's daily grab limit and records a grab.
// Download clients call it (through Fetch) before sending a release.
func (s *Service) ReserveGrab(ctx context.Context, ind Indexer) error {
	if ind.GrabLimitPerDay != nil {
		n, err := s.GrabsToday(ctx, ind.ID)
		if err != nil {
			return err
		}
		if n >= *ind.GrabLimitPerDay {
			return ErrGrabLimit
		}
	}
	now := s.now()
	_, err := s.DB.W.ExecContext(ctx, `INSERT INTO indexer_grabs (indexer_id, at) VALUES (?, ?)`, ind.ID, db.Millis(now))
	if err != nil {
		return err
	}
	_, err = s.DB.W.ExecContext(ctx, `DELETE FROM indexer_grabs WHERE at < ?`, db.Millis(now.Add(-48*time.Hour)))
	return err
}
