package jobs

import (
	"context"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// Reschedule changes the interval of a registered task while the queue runs.
// The next run moves closer when the new interval is shorter.
func (q *Queue) Reschedule(ctx context.Context, name string, every time.Duration) error {
	q.mu.Lock()
	found := false
	for i := range q.schedules {
		if q.schedules[i].name == name {
			q.schedules[i].every = every
			found = true
		}
	}
	q.mu.Unlock()
	if !found {
		return ErrUnknownTask
	}
	now := q.opts.Now()
	_, err := q.db.W.ExecContext(ctx, `UPDATE job_schedules SET interval_seconds = ?,
		next_run_at = min(next_run_at, ?) WHERE name = ?`, int64(every/time.Second), db.Millis(now.Add(every)), name)
	return err
}
