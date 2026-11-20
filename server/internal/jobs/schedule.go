package jobs

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// ScheduledTask is a recurring task as shown in the task list.
type ScheduledTask struct {
	Name      string
	Kind      string
	Every     time.Duration
	NextRunAt time.Time
	LastRunAt *time.Time
	LastState State // empty when it never ran
}

func uniqueKeyFor(name string) string { return "schedule:" + name }

// saveSchedules records registered schedules; an existing schedule keeps its
// next run (pulled in when its interval got shorter).
func (q *Queue) saveSchedules(ctx context.Context) error {
	q.mu.Lock()
	scheds := append([]schedule(nil), q.schedules...)
	q.mu.Unlock()
	now := q.opts.Now()
	return q.db.Tx(ctx, func(tx *sql.Tx) error {
		for _, s := range scheds {
			secs := int64(s.every / time.Second)
			if _, err := tx.ExecContext(ctx, `INSERT INTO job_schedules (name, kind, interval_seconds, next_run_at)
				VALUES (?, ?, ?, ?)
				ON CONFLICT (name) DO UPDATE SET kind = excluded.kind, interval_seconds = excluded.interval_seconds,
					next_run_at = min(job_schedules.next_run_at, ?)`,
				s.name, s.kind, secs, db.Millis(now), db.Millis(now.Add(s.every))); err != nil {
				return err
			}
		}
		return nil
	})
}

func (q *Queue) scheduler(ctx context.Context) {
	defer q.wg.Done()
	ticker := time.NewTicker(q.opts.Poll)
	defer ticker.Stop()
	for {
		if err := q.runDue(ctx); err != nil && ctx.Err() == nil {
			q.log.Error("run scheduled tasks", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (q *Queue) runDue(ctx context.Context) error {
	q.mu.Lock()
	scheds := append([]schedule(nil), q.schedules...)
	q.mu.Unlock()
	now := q.opts.Now()
	for _, s := range scheds {
		var next int64
		if err := q.db.W.QueryRowContext(ctx, `SELECT next_run_at FROM job_schedules WHERE name = ?`, s.name).Scan(&next); err != nil {
			return err
		}
		if next > db.Millis(now) {
			continue
		}
		if _, err := q.trigger(ctx, s, now); err != nil {
			return err
		}
	}
	return nil
}

// trigger queues a run of s (unless one is queued or running) and moves its next run.
func (q *Queue) trigger(ctx context.Context, s schedule, now time.Time) (Job, error) {
	j, _, err := q.Enqueue(ctx, s.kind, nil, EnqueueOptions{UniqueKey: uniqueKeyFor(s.name)})
	if err != nil {
		return Job{}, err
	}
	_, err = q.db.W.ExecContext(ctx, `UPDATE job_schedules SET next_run_at = ?, last_run_at = ?, last_job_id = ? WHERE name = ?`,
		db.Millis(now.Add(s.every)), db.Millis(now), j.ID, s.name)
	return j, err
}

// RunNow queues the scheduled task name at once (or returns its queued or running job).
func (q *Queue) RunNow(ctx context.Context, name string) (Job, error) {
	q.mu.Lock()
	var found *schedule
	for i := range q.schedules {
		if q.schedules[i].name == name {
			s := q.schedules[i]
			found = &s
		}
	}
	q.mu.Unlock()
	if found == nil {
		return Job{}, ErrUnknownTask
	}
	return q.trigger(ctx, *found, q.opts.Now())
}

// Scheduled lists the recurring tasks with their last outcome.
func (q *Queue) Scheduled(ctx context.Context) ([]ScheduledTask, error) {
	rows, err := q.db.R.QueryContext(ctx, `SELECT s.name, s.kind, s.interval_seconds, s.next_run_at, s.last_run_at, j.state
		FROM job_schedules s LEFT JOIN jobs j ON j.id = s.last_job_id ORDER BY s.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	q.mu.Lock()
	known := map[string]bool{}
	for _, s := range q.schedules {
		known[s.name] = true
	}
	q.mu.Unlock()
	out := []ScheduledTask{}
	for rows.Next() {
		var (
			t     ScheduledTask
			secs  int64
			next  int64
			last  sql.NullInt64
			state sql.NullString
		)
		if err := rows.Scan(&t.Name, &t.Kind, &secs, &next, &last, &state); err != nil {
			return nil, err
		}
		if !known[t.Name] { // a schedule an older version registered
			continue
		}
		t.Every = time.Duration(secs) * time.Second
		t.NextRunAt, t.LastRunAt, t.LastState = db.Time(next), db.NullTime(last), State(state.String)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return out, nil
}
