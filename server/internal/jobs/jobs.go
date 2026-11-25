// Package jobs is a small durable job queue stored in SQLite: jobs with unique
// keys, recurring schedules, retries with exponential backoff, per-queue
// concurrency, cancellation and a history that survives restarts (D-004).
//
// One server process owns the queue. A job is claimed by switching it from
// queued to running in a single UPDATE on the writer connection, so two workers
// can never take the same job. Jobs left running by a crash or kill are queued
// again on the next start.
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"runtime/debug"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// State is a job's lifecycle state.
type State string

// Job states.
const (
	Queued    State = "queued"
	Running   State = "running"
	Succeeded State = "succeeded"
	Failed    State = "failed"
	Cancelled State = "cancelled"
)

// Valid reports whether s is a known state.
func (s State) Valid() bool {
	switch s {
	case Queued, Running, Succeeded, Failed, Cancelled:
		return true
	}
	return false
}

// DefaultQueue is used when a kind names no queue.
const DefaultQueue = "default"

// Errors.
var (
	ErrNotFound      = errors.New("job not found")
	ErrUnknownKind   = errors.New("unknown job kind")
	ErrNotCancelable = errors.New("job has already finished")
	ErrUnknownTask   = errors.New("unknown scheduled task")
)

// Job is one unit of work.
type Job struct {
	ID          int64
	Kind        string
	Queue       string
	Args        json.RawMessage
	UniqueKey   string
	State       State
	Attempt     int
	MaxAttempts int
	RunAt       time.Time
	CreatedAt   time.Time
	StartedAt   *time.Time
	FinishedAt  *time.Time
	Error       string
}

// Handler runs a job. It must return when ctx is cancelled (job cancelled,
// timed out or the server stopping). Returning Permanent(err) skips retries.
type Handler func(ctx context.Context, job *Job) error

type permanent struct{ error }

func (p permanent) Unwrap() error { return p.error }

// Permanent marks an error that retrying will not fix.
func Permanent(err error) error { return permanent{err} }

// KindOptions configure a registered kind.
type KindOptions struct {
	Queue       string        // default DefaultQueue
	MaxAttempts int           // default 5
	Timeout     time.Duration // per attempt; default 30 minutes
}

// EnqueueOptions configure one job.
type EnqueueOptions struct {
	// UniqueKey: while a job with this key is queued or running, enqueueing
	// another one returns the existing job instead.
	UniqueKey   string
	RunAt       time.Time // default now
	MaxAttempts int       // default: the kind's
}

// Options configure the queue.
type Options struct {
	// Concurrency per queue name; a queue not listed runs one job at a time.
	Concurrency map[string]int
	// Poll is how often queues look for due jobs (default 1 s). New jobs wake
	// their queue at once.
	Poll time.Duration
	// Backoff returns the wait before attempt n+1 after attempt n failed
	// (default: 10 s doubling per attempt with jitter, at most 1 h).
	Backoff func(attempt int) time.Duration
	Now     func() time.Time
}

type kind struct {
	handler Handler
	opts    KindOptions
}

type schedule struct {
	name, kind string
	every      time.Duration
}

// Queue runs registered job kinds.
type Queue struct {
	db   *db.DB
	log  *slog.Logger
	opts Options

	mu        sync.Mutex
	kinds     map[string]kind
	schedules []schedule
	wake      map[string]chan struct{}
	running   map[int64]context.CancelFunc
	cancelled map[int64]bool
	started   bool
	stop      context.CancelFunc
	wg        sync.WaitGroup
}

// New returns a queue; register kinds and schedules, then Start it.
func New(d *db.DB, log *slog.Logger, opts Options) *Queue {
	if opts.Poll <= 0 {
		opts.Poll = time.Second
	}
	if opts.Backoff == nil {
		opts.Backoff = DefaultBackoff
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Queue{db: d, log: log, opts: opts, kinds: map[string]kind{}, wake: map[string]chan struct{}{},
		running: map[int64]context.CancelFunc{}, cancelled: map[int64]bool{}}
}

// DefaultBackoff is 10 s × 2^(attempt-1) ± 20 %, capped at one hour.
func DefaultBackoff(attempt int) time.Duration {
	d := 10 * time.Second << min(max(attempt-1, 0), 9)
	d = min(d, time.Hour)
	jitter := 0.8 + rand.Float64()*0.4 // #nosec G404 -- jitter needs no cryptographic randomness
	return time.Duration(float64(d) * jitter)
}

// Register adds a job kind. Call before Start.
func (q *Queue) Register(name string, h Handler, o KindOptions) {
	if o.Queue == "" {
		o.Queue = DefaultQueue
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 5
	}
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Minute
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.kinds[name] = kind{handler: h, opts: o}
	if _, ok := q.wake[o.Queue]; !ok {
		q.wake[o.Queue] = make(chan struct{}, 1)
	}
}

// Schedule runs kind every interval under name (a task shown in the task list).
// A new schedule runs for the first time right after start. Call before Start.
func (q *Queue) Schedule(name, kind string, every time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.schedules = append(q.schedules, schedule{name: name, kind: kind, every: every})
}

// Enqueue adds a job. With a UniqueKey that is already queued or running, it
// returns that job and inserted=false.
func (q *Queue) Enqueue(ctx context.Context, kindName string, args any, o EnqueueOptions) (Job, bool, error) {
	q.mu.Lock()
	k, ok := q.kinds[kindName]
	q.mu.Unlock()
	if !ok {
		return Job{}, false, fmt.Errorf("%w %q", ErrUnknownKind, kindName)
	}
	raw := []byte("{}")
	if args != nil {
		var err error
		if raw, err = json.Marshal(args); err != nil {
			return Job{}, false, err
		}
	}
	now := q.opts.Now()
	runAt := o.RunAt
	if runAt.IsZero() {
		runAt = now
	}
	maxAttempts := o.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = k.opts.MaxAttempts
	}
	var unique sql.NullString
	if o.UniqueKey != "" {
		unique = sql.NullString{String: o.UniqueKey, Valid: true}
	}
	var id int64
	err := q.db.W.QueryRowContext(ctx, `INSERT INTO jobs (kind, queue, args, unique_key, state, max_attempts, run_at, created_at)
		VALUES (?, ?, ?, ?, 'queued', ?, ?, ?) ON CONFLICT DO NOTHING RETURNING id`,
		kindName, k.opts.Queue, string(raw), unique, maxAttempts, db.Millis(runAt), db.Millis(now)).Scan(&id)
	inserted := true
	if errors.Is(err, sql.ErrNoRows) {
		inserted = false
		err = q.db.W.QueryRowContext(ctx, `SELECT id FROM jobs WHERE unique_key = ? AND state IN ('queued', 'running')`,
			o.UniqueKey).Scan(&id)
	}
	if err != nil {
		return Job{}, false, err
	}
	if inserted {
		q.notify(k.opts.Queue)
	}
	j, err := q.Get(ctx, id)
	return j, inserted, err
}

func (q *Queue) notify(queue string) {
	q.mu.Lock()
	ch := q.wake[queue]
	q.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

const jobColumns = `id, kind, queue, args, unique_key, state, attempt, max_attempts, run_at, created_at, started_at, finished_at, last_error`

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var (
		j                   Job
		args                string
		unique, lastErr     sql.NullString
		runAt, createdAt    int64
		started, finishedAt sql.NullInt64
	)
	if err := row.Scan(&j.ID, &j.Kind, &j.Queue, &args, &unique, &j.State, &j.Attempt, &j.MaxAttempts,
		&runAt, &createdAt, &started, &finishedAt, &lastErr); err != nil {
		return Job{}, err
	}
	j.Args = json.RawMessage(args)
	j.UniqueKey, j.Error = unique.String, lastErr.String
	j.RunAt, j.CreatedAt = db.Time(runAt), db.Time(createdAt)
	j.StartedAt, j.FinishedAt = db.NullTime(started), db.NullTime(finishedAt)
	return j, nil
}

// Get returns one job.
func (q *Queue) Get(ctx context.Context, id int64) (Job, error) {
	j, err := scanJob(q.db.W.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	return j, err
}

// List returns the most recent jobs, optionally only those in state.
func (q *Queue) List(ctx context.Context, state State, limit int) ([]Job, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT ` + jobColumns + ` FROM jobs ORDER BY id DESC LIMIT ?`
	args := []any{limit}
	if state != "" {
		query = `SELECT ` + jobColumns + ` FROM jobs WHERE state = ? ORDER BY id DESC LIMIT ?`
		args = []any{string(state), limit}
	}
	rows, err := q.db.R.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Cancel cancels a queued job at once, or asks a running one to stop.
func (q *Queue) Cancel(ctx context.Context, id int64) error {
	now := db.Millis(q.opts.Now())
	res, err := q.db.W.ExecContext(ctx, `UPDATE jobs SET state = 'cancelled', finished_at = ?, last_error = 'cancelled'
		WHERE id = ? AND state = 'queued'`, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	res, err = q.db.W.ExecContext(ctx, `UPDATE jobs SET cancel_requested = 1 WHERE id = ? AND state = 'running'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		q.mu.Lock()
		q.cancelled[id] = true
		cancel := q.running[id]
		q.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return nil
	}
	if _, err := q.Get(ctx, id); err != nil {
		return err
	}
	return ErrNotCancelable
}

// Prune deletes finished jobs older than before, and returns how many.
func (q *Queue) Prune(ctx context.Context, before time.Time) (int64, error) {
	res, err := q.db.W.ExecContext(ctx, `DELETE FROM jobs WHERE state IN ('succeeded', 'failed', 'cancelled')
		AND finished_at < ?`, db.Millis(before))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Start recovers interrupted jobs, records the schedules and starts the workers.
func (q *Queue) Start(ctx context.Context) error {
	q.mu.Lock()
	if q.started {
		q.mu.Unlock()
		return errors.New("jobs: already started")
	}
	q.started = true
	q.mu.Unlock()
	if err := q.recover(ctx); err != nil {
		return err
	}
	if err := q.saveSchedules(ctx); err != nil {
		return err
	}
	runCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	q.stop = stop
	q.mu.Lock()
	queues := make([]string, 0, len(q.wake))
	for name := range q.wake {
		queues = append(queues, name)
	}
	q.mu.Unlock()
	for _, name := range queues {
		n := q.opts.Concurrency[name]
		if n <= 0 {
			n = 1
		}
		q.wg.Add(1)
		go q.work(runCtx, name, n)
	}
	q.wg.Add(1)
	go q.scheduler(runCtx)
	return nil
}

// Stop stops claiming jobs, cancels running ones (they are queued again for
// the next start) and waits for them to return.
func (q *Queue) Stop() {
	if q.stop != nil {
		q.stop()
	}
	q.wg.Wait()
}

// recover requeues jobs a previous process left running.
func (q *Queue) recover(ctx context.Context) error {
	now := db.Millis(q.opts.Now())
	return q.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET state = 'cancelled', finished_at = ?, last_error = 'cancelled'
			WHERE state = 'running' AND cancel_requested = 1`, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET state = 'failed', finished_at = ?,
			last_error = 'interrupted by a restart on its last attempt' WHERE state = 'running' AND attempt >= max_attempts`, now); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE jobs SET state = 'queued', run_at = ?, started_at = NULL WHERE state = 'running'`, now)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			q.log.Info("requeued jobs interrupted by a restart", "count", n)
		}
		return nil
	})
}

func (q *Queue) work(ctx context.Context, queue string, concurrency int) {
	defer q.wg.Done()
	slots := make(chan struct{}, concurrency)
	q.mu.Lock()
	wake := q.wake[queue]
	q.mu.Unlock()
	ticker := time.NewTicker(q.opts.Poll)
	defer ticker.Stop()
	var running sync.WaitGroup
	defer running.Wait()
	for {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		job, ok, err := q.claim(ctx, queue)
		if err != nil && ctx.Err() == nil {
			q.log.Error("claim job", "queue", queue, "err", err)
		}
		if !ok {
			<-slots
			select {
			case <-ctx.Done():
				return
			case <-wake:
			case <-ticker.C:
			}
			continue
		}
		running.Add(1)
		go func() {
			defer running.Done()
			defer func() { <-slots }()
			q.run(ctx, job)
		}()
	}
}

func (q *Queue) claim(ctx context.Context, queue string) (Job, bool, error) {
	now := db.Millis(q.opts.Now())
	j, err := scanJob(q.db.W.QueryRowContext(ctx, `UPDATE jobs SET state = 'running', attempt = attempt + 1,
			started_at = ?, finished_at = NULL
		WHERE id = (SELECT id FROM jobs WHERE state = 'queued' AND queue = ? AND run_at <= ? ORDER BY run_at, id LIMIT 1)
		RETURNING `+jobColumns, now, queue, now))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	return j, true, nil
}

func (q *Queue) run(ctx context.Context, j Job) {
	q.mu.Lock()
	k, ok := q.kinds[j.Kind]
	q.mu.Unlock()
	if !ok {
		q.finish(j, Failed, fmt.Sprintf("no handler for job kind %q", j.Kind), time.Time{})
		return
	}
	jctx, cancel := context.WithTimeout(ctx, k.opts.Timeout)
	defer cancel()
	q.mu.Lock()
	q.running[j.ID] = cancel
	// Cancel can land between the claim (state running) and this point, when
	// there was no cancel func to call yet: honour it now.
	early := q.cancelled[j.ID]
	q.mu.Unlock()
	if early {
		cancel()
	}
	defer func() {
		q.mu.Lock()
		delete(q.running, j.ID)
		delete(q.cancelled, j.ID)
		q.mu.Unlock()
	}()
	log := q.log.With("job_id", j.ID, "kind", j.Kind, "attempt", j.Attempt)
	start := time.Now()
	err := safeCall(jctx, k.handler, &j)
	q.mu.Lock()
	wasCancelled := q.cancelled[j.ID]
	q.mu.Unlock()
	switch {
	case err == nil:
		q.finish(j, Succeeded, "", time.Time{})
		log.Debug("job succeeded", "took", time.Since(start).String())
	case wasCancelled:
		q.finish(j, Cancelled, "cancelled", time.Time{})
		log.Info("job cancelled")
	case ctx.Err() != nil:
		// The server is stopping: the attempt did not fail, run it again next start.
		q.requeue(j)
		log.Info("job interrupted by shutdown; queued again")
	default:
		var p permanent
		if errors.As(err, &p) || j.Attempt >= j.MaxAttempts {
			q.finish(j, Failed, err.Error(), time.Time{})
			log.Warn("job failed", "err", err)
			return
		}
		next := q.opts.Now().Add(q.opts.Backoff(j.Attempt))
		q.finish(j, Queued, err.Error(), next)
		log.Info("job failed; will retry", "err", err, "retry_at", next.UTC().Format(time.RFC3339))
	}
}

func safeCall(ctx context.Context, h Handler, j *Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = Permanent(fmt.Errorf("panic: %v\n%s", r, debug.Stack()))
		}
	}()
	return h(ctx, j)
}

// finish records an attempt's outcome. Writes use a context of their own so a
// stopping server still records them.
func (q *Queue) finish(j Job, state State, msg string, retryAt time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := db.Millis(q.opts.Now())
	var lastErr sql.NullString
	if msg != "" {
		lastErr = sql.NullString{String: msg, Valid: true}
	}
	var err error
	if state == Queued {
		_, err = q.db.W.ExecContext(ctx, `UPDATE jobs SET state = 'queued', run_at = ?, last_error = ? WHERE id = ?`,
			db.Millis(retryAt), lastErr, j.ID)
	} else {
		_, err = q.db.W.ExecContext(ctx, `UPDATE jobs SET state = ?, finished_at = ?,
			last_error = COALESCE(?, last_error) WHERE id = ?`, string(state), now, lastErr, j.ID)
	}
	if err != nil {
		q.log.Error("record job result", "job_id", j.ID, "err", err)
	}
}

func (q *Queue) requeue(j Job) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := q.db.W.ExecContext(ctx, `UPDATE jobs SET state = 'queued', attempt = attempt - 1, started_at = NULL
		WHERE id = ? AND state = 'running'`, j.ID); err != nil {
		q.log.Error("requeue job", "job_id", j.ID, "err", err)
	}
}
