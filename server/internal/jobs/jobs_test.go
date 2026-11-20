package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newQueue(t *testing.T, d *db.DB, opts Options) *Queue {
	t.Helper()
	if opts.Poll == 0 {
		opts.Poll = 10 * time.Millisecond
	}
	if opts.Backoff == nil {
		opts.Backoff = func(int) time.Duration { return time.Millisecond }
	}
	return New(d, quietLog(), opts)
}

func waitState(t *testing.T, q *Queue, id int64, want State) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		j, err := q.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if j.State == want {
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %d: state %s, want %s (error %q)", id, j.State, want, j.Error)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRunsJob(t *testing.T) {
	q := newQueue(t, db.OpenTest(t), Options{})
	type args struct{ N int }
	got := make(chan int, 1)
	q.Register("echo", func(_ context.Context, j *Job) error {
		var a args
		if err := decode(j.Args, &a); err != nil {
			return err
		}
		got <- a.N
		return nil
	}, KindOptions{})
	ctx := context.Background()
	if err := q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer q.Stop()
	j, inserted, err := q.Enqueue(ctx, "echo", args{N: 42}, EnqueueOptions{})
	if err != nil || !inserted {
		t.Fatal(inserted, err)
	}
	if n := <-got; n != 42 {
		t.Fatal(n)
	}
	done := waitState(t, q, j.ID, Succeeded)
	if done.Attempt != 1 || done.StartedAt == nil || done.FinishedAt == nil {
		t.Fatalf("%+v", done)
	}
	if _, _, err := q.Enqueue(ctx, "nope", nil, EnqueueOptions{}); !errors.Is(err, ErrUnknownKind) {
		t.Fatal(err)
	}
}

func TestUniqueKey(t *testing.T) {
	q := newQueue(t, db.OpenTest(t), Options{})
	release := make(chan struct{})
	q.Register("block", func(ctx context.Context, _ *Job) error {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}, KindOptions{})
	ctx := context.Background()
	a, ins1, err := q.Enqueue(ctx, "block", nil, EnqueueOptions{UniqueKey: "k"})
	if err != nil || !ins1 {
		t.Fatal(err)
	}
	b, ins2, err := q.Enqueue(ctx, "block", nil, EnqueueOptions{UniqueKey: "k"})
	if err != nil || ins2 || b.ID != a.ID {
		t.Fatalf("duplicate inserted: %v %d %d %v", ins2, a.ID, b.ID, err)
	}
	other, ins3, _ := q.Enqueue(ctx, "block", nil, EnqueueOptions{UniqueKey: "other"})
	if !ins3 || other.ID == a.ID {
		t.Fatal("different key not inserted")
	}
	if err := q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer q.Stop()
	waitState(t, q, a.ID, Running)
	// Still unique while running.
	if _, ins, _ := q.Enqueue(ctx, "block", nil, EnqueueOptions{UniqueKey: "k"}); ins {
		t.Fatal("inserted while running")
	}
	close(release)
	waitState(t, q, a.ID, Succeeded)
	// Free again after it finished.
	if _, ins, _ := q.Enqueue(ctx, "block", nil, EnqueueOptions{UniqueKey: "k"}); !ins {
		t.Fatal("not inserted after finish")
	}
}

func TestRetryWithBackoff(t *testing.T) {
	var calls atomic.Int32
	var backoffs []int
	var mu sync.Mutex
	q := newQueue(t, db.OpenTest(t), Options{Backoff: func(a int) time.Duration {
		mu.Lock()
		backoffs = append(backoffs, a)
		mu.Unlock()
		return 5 * time.Millisecond
	}})
	q.Register("flaky", func(context.Context, *Job) error {
		if calls.Add(1) < 3 {
			return errors.New("temporary")
		}
		return nil
	}, KindOptions{MaxAttempts: 5})
	q.Register("broken", func(context.Context, *Job) error { return errors.New("always") }, KindOptions{MaxAttempts: 2})
	q.Register("fatal", func(context.Context, *Job) error { return Permanent(errors.New("bad input")) }, KindOptions{})
	q.Register("panics", func(context.Context, *Job) error { panic("boom") }, KindOptions{})
	ctx := context.Background()
	if err := q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer q.Stop()
	f, _, _ := q.Enqueue(ctx, "flaky", nil, EnqueueOptions{})
	b, _, _ := q.Enqueue(ctx, "broken", nil, EnqueueOptions{})
	p, _, _ := q.Enqueue(ctx, "fatal", nil, EnqueueOptions{})
	x, _, _ := q.Enqueue(ctx, "panics", nil, EnqueueOptions{})
	if j := waitState(t, q, f.ID, Succeeded); j.Attempt != 3 || j.Error != "temporary" {
		t.Fatalf("flaky: %+v", j)
	}
	if j := waitState(t, q, b.ID, Failed); j.Attempt != 2 || j.Error != "always" {
		t.Fatalf("broken: %+v", j)
	}
	if j := waitState(t, q, p.ID, Failed); j.Attempt != 1 {
		t.Fatalf("permanent retried: %+v", j)
	}
	if j := waitState(t, q, x.ID, Failed); j.Attempt != 1 {
		t.Fatalf("panic retried: %+v", j)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(backoffs) != 3 { // flaky after 1 and 2, broken after 1
		t.Fatalf("backoff calls %v", backoffs)
	}
}

func TestDefaultBackoff(t *testing.T) {
	if d := DefaultBackoff(1); d < 8*time.Second || d > 12*time.Second {
		t.Fatal(d)
	}
	if d := DefaultBackoff(3); d < 32*time.Second || d > 48*time.Second {
		t.Fatal(d)
	}
	if d := DefaultBackoff(50); d > 72*time.Minute {
		t.Fatal(d)
	}
}

func TestPerQueueConcurrency(t *testing.T) {
	q := newQueue(t, db.OpenTest(t), Options{Concurrency: map[string]int{"io": 2}})
	var cur, peak, otherRan atomic.Int32
	release := make(chan struct{})
	q.Register("slow", func(ctx context.Context, _ *Job) error {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		select {
		case <-release:
		case <-ctx.Done():
		}
		cur.Add(-1)
		return nil
	}, KindOptions{Queue: "io"})
	q.Register("other", func(context.Context, *Job) error { otherRan.Add(1); return nil }, KindOptions{})
	ctx := context.Background()
	var ids []int64
	for range 6 {
		j, _, err := q.Enqueue(ctx, "slow", nil, EnqueueOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, j.ID)
	}
	if err := q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer q.Stop()
	waitState(t, q, ids[0], Running)
	waitState(t, q, ids[1], Running)
	// A full queue does not block other queues.
	o, _, _ := q.Enqueue(ctx, "other", nil, EnqueueOptions{})
	waitState(t, q, o.ID, Succeeded)
	time.Sleep(50 * time.Millisecond)
	if p := peak.Load(); p != 2 {
		t.Fatalf("peak concurrency %d, want 2", p)
	}
	close(release)
	for _, id := range ids {
		waitState(t, q, id, Succeeded)
	}
	if peak.Load() != 2 {
		t.Fatalf("peak %d", peak.Load())
	}
}

func TestCancel(t *testing.T) {
	q := newQueue(t, db.OpenTest(t), Options{})
	q.Register("wait", func(ctx context.Context, _ *Job) error {
		<-ctx.Done()
		return ctx.Err()
	}, KindOptions{})
	ctx := context.Background()
	later, _, _ := q.Enqueue(ctx, "wait", nil, EnqueueOptions{RunAt: time.Now().Add(time.Hour)})
	if err := q.Cancel(ctx, later.ID); err != nil {
		t.Fatal(err)
	}
	waitState(t, q, later.ID, Cancelled)
	if err := q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer q.Stop()
	run, _, _ := q.Enqueue(ctx, "wait", nil, EnqueueOptions{})
	waitState(t, q, run.ID, Running)
	if err := q.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if j := waitState(t, q, run.ID, Cancelled); j.Attempt != 1 {
		t.Fatalf("cancelled job retried: %+v", j)
	}
	if err := q.Cancel(ctx, run.ID); !errors.Is(err, ErrNotCancelable) {
		t.Fatal(err)
	}
	if err := q.Cancel(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestRestartRecovery(t *testing.T) {
	d := db.OpenTest(t)
	ctx := context.Background()
	q1 := newQueue(t, d, Options{})
	started := make(chan struct{}, 1)
	q1.Register("work", func(ctx context.Context, _ *Job) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}, KindOptions{MaxAttempts: 3})
	if err := q1.Start(ctx); err != nil {
		t.Fatal(err)
	}
	j, _, _ := q1.Enqueue(ctx, "work", nil, EnqueueOptions{})
	<-started
	// A clean shutdown puts the interrupted job back without using up an attempt.
	q1.Stop()
	if got, _ := q1.Get(ctx, j.ID); got.State != Queued || got.Attempt != 0 {
		t.Fatalf("after stop: %+v", got)
	}
	// A crash leaves rows in "running": simulate one, plus one on its last attempt.
	if _, err := d.W.Exec(`UPDATE jobs SET state = 'running', attempt = 1 WHERE id = ?`, j.ID); err != nil {
		t.Fatal(err)
	}
	last, _, _ := q1.Enqueue(ctx, "work", nil, EnqueueOptions{MaxAttempts: 1})
	if _, err := d.W.Exec(`UPDATE jobs SET state = 'running', attempt = 1 WHERE id = ?`, last.ID); err != nil {
		t.Fatal(err)
	}
	q2 := newQueue(t, d, Options{})
	var ran atomic.Int32
	q2.Register("work", func(context.Context, *Job) error { ran.Add(1); return nil }, KindOptions{MaxAttempts: 3})
	if err := q2.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer q2.Stop()
	if got := waitState(t, q2, j.ID, Succeeded); got.Attempt != 2 {
		t.Fatalf("recovered: %+v", got)
	}
	if got := waitState(t, q2, last.ID, Failed); got.Error == "" {
		t.Fatalf("last attempt: %+v", got)
	}
	if ran.Load() != 1 {
		t.Fatalf("ran %d", ran.Load())
	}
}

func TestSchedules(t *testing.T) {
	d := db.OpenTest(t)
	ctx := context.Background()
	var runs atomic.Int32
	q := newQueue(t, d, Options{})
	q.Register("tick", func(context.Context, *Job) error { runs.Add(1); return nil }, KindOptions{})
	q.Schedule("ticker", "tick", time.Hour)
	if err := q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	// A new schedule runs right after start, then waits for its interval.
	deadline := time.Now().Add(5 * time.Second)
	for runs.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if runs.Load() != 1 {
		t.Fatalf("runs %d", runs.Load())
	}
	tasks, err := q.Scheduled(ctx)
	if err != nil || len(tasks) != 1 {
		t.Fatal(tasks, err)
	}
	if tasks[0].Every != time.Hour || tasks[0].LastRunAt == nil || time.Until(tasks[0].NextRunAt) < 59*time.Minute {
		t.Fatalf("%+v", tasks[0])
	}
	j, err := q.RunNow(ctx, "ticker")
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, q, j.ID, Succeeded)
	if runs.Load() != 2 {
		t.Fatalf("runs %d", runs.Load())
	}
	if _, err := q.RunNow(ctx, "nope"); !errors.Is(err, ErrUnknownTask) {
		t.Fatal(err)
	}
	tasks, _ = q.Scheduled(ctx)
	if tasks[0].LastState != Succeeded {
		t.Fatalf("%+v", tasks[0])
	}
	q.Stop()
	// After a restart the schedule keeps its next run instead of firing again.
	q2 := newQueue(t, d, Options{})
	q2.Register("tick", func(context.Context, *Job) error { runs.Add(1); return nil }, KindOptions{})
	q2.Schedule("ticker", "tick", time.Hour)
	if err := q2.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	q2.Stop()
	if runs.Load() != 2 {
		t.Fatalf("ran again after restart: %d", runs.Load())
	}
}

func TestListAndPrune(t *testing.T) {
	q := newQueue(t, db.OpenTest(t), Options{})
	q.Register("noop", func(context.Context, *Job) error { return nil }, KindOptions{})
	ctx := context.Background()
	if err := q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var last Job
	for range 3 {
		last, _, _ = q.Enqueue(ctx, "noop", nil, EnqueueOptions{})
	}
	waitState(t, q, last.ID, Succeeded)
	q.Stop()
	all, err := q.List(ctx, "", 10)
	if err != nil || len(all) != 3 || all[0].ID != last.ID {
		t.Fatalf("%v %v", all, err)
	}
	ok, _ := q.List(ctx, Succeeded, 10)
	queued, _ := q.List(ctx, Queued, 10)
	if len(ok) != 3 || len(queued) != 0 {
		t.Fatal(len(ok), len(queued))
	}
	n, err := q.Prune(ctx, time.Now().Add(time.Minute))
	if err != nil || n != 3 {
		t.Fatal(n, err)
	}
}
