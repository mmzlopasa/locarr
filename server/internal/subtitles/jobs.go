package subtitles

import (
	"context"
	"errors"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/library"
)

// RegisterTasks registers the subtitle jobs: a search per item (after
// imports and on request), the hourly search for missing subtitles, the
// daily scan of the library, and sync. They run on their own queue, one at a
// time, so provider limits and ffmpeg stay predictable.
func (s *Service) RegisterTasks() {
	s.Jobs.Register(TaskSearchItem, func(ctx context.Context, j *jobs.Job) error {
		var a searchArgs
		if err := j.Decode(&a); err != nil {
			return jobs.Permanent(err)
		}
		ref, err := library.ParseRef(a.Ref)
		if err != nil {
			return jobs.Permanent(err)
		}
		_, err = s.Search(ctx, ref, a.Reason, a.Probe)
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNoFile) {
			return jobs.Permanent(err)
		}
		return err
	}, jobs.KindOptions{Queue: Queue, MaxAttempts: 2, Timeout: 10 * time.Minute})
	s.Jobs.Register(TaskMissing, func(ctx context.Context, _ *jobs.Job) error {
		n, err := s.SearchDue(ctx)
		if n > 0 {
			s.log().Info("scheduled subtitle search", "items", n)
		}
		return err
	}, jobs.KindOptions{Queue: Queue, MaxAttempts: 1, Timeout: 55 * time.Minute})
	s.Jobs.Register(TaskScan, func(ctx context.Context, j *jobs.Job) error {
		var a scanArgs
		_ = j.Decode(&a)
		n, err := s.ScanAll(ctx, a.StateOnly)
		s.log().Info("subtitle scan", "items", n, "stateOnly", a.StateOnly)
		return err
	}, jobs.KindOptions{Queue: Queue, MaxAttempts: 1, Timeout: 4 * time.Hour})
	s.Jobs.Register(TaskSync, func(ctx context.Context, j *jobs.Job) error {
		var a syncArgs
		if err := j.Decode(&a); err != nil {
			return jobs.Permanent(err)
		}
		return s.RunSync(ctx, a)
	}, jobs.KindOptions{Queue: Queue + "-sync", MaxAttempts: 1, Timeout: 30 * time.Minute})
	s.Jobs.Schedule(TaskMissing, TaskMissing, time.Hour)
	s.Jobs.Schedule(TaskScan, TaskScan, 24*time.Hour)
}
