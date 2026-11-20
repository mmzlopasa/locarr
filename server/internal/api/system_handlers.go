package api

import (
	"context"
	"runtime"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/version"
)

// GetSystemInfo implements GET /system/info (no sign-in).
func (h *Handler) GetSystemInfo(ctx context.Context) (*gen.SystemInfo, error) {
	required, err := h.Auth.SetupRequired(ctx)
	if err != nil {
		return nil, err
	}
	name, err := settings.Get(ctx, h.Settings, settings.InstanceName)
	if err != nil {
		return nil, err
	}
	return &gen.SystemInfo{Product: "Locarr", InstanceName: name, Version: version.Version, Commit: version.Commit,
		StartedAt: h.StartedAt, SetupRequired: required}, nil
}

// GetSystemStatus implements GET /system/status.
func (h *Handler) GetSystemStatus(ctx context.Context) (*gen.SystemStatus, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return &gen.SystemStatus{
		Version: version.Version, Commit: version.Commit, GoVersion: runtime.Version(), Os: runtime.GOOS, Arch: runtime.GOARCH,
		StartedAt: h.StartedAt, UptimeSeconds: int64(time.Since(h.StartedAt).Seconds()), ConfigDir: h.ConfigDir,
		DatabaseSizeBytes: h.DB.Size(), DataRoots: append([]string{}, h.DataRoots...),
	}, nil
}

func toJob(j jobs.Job) gen.Job {
	return gen.Job{ID: j.ID, Kind: j.Kind, Queue: j.Queue, State: gen.JobState(j.State), Attempt: j.Attempt,
		MaxAttempts: j.MaxAttempts, RunAt: j.RunAt, CreatedAt: j.CreatedAt, StartedAt: optTime(j.StartedAt),
		FinishedAt: optTime(j.FinishedAt), Error: optStr(j.Error), UniqueKey: optStr(j.UniqueKey)}
}

// ListTasks implements GET /system/tasks.
func (h *Handler) ListTasks(ctx context.Context, params gen.ListTasksParams) (*gen.TaskList, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	sched, err := h.Jobs.Scheduled(ctx)
	if err != nil {
		return nil, err
	}
	list, err := h.Jobs.List(ctx, jobs.State(params.State.Or("")), params.Limit.Or(100))
	if err != nil {
		return nil, err
	}
	out := &gen.TaskList{Scheduled: make([]gen.ScheduledTask, 0, len(sched)), Jobs: make([]gen.Job, 0, len(list))}
	for _, s := range sched {
		t := gen.ScheduledTask{Name: s.Name, Kind: s.Kind, IntervalSeconds: int64(s.Every / time.Second),
			NextRunAt: s.NextRunAt, LastRunAt: optTime(s.LastRunAt)}
		if s.LastState != "" {
			t.LastState = gen.NewOptJobState(gen.JobState(s.LastState))
		}
		out.Scheduled = append(out.Scheduled, t)
	}
	for _, j := range list {
		out.Jobs = append(out.Jobs, toJob(j))
	}
	return out, nil
}

// RunTask implements POST /system/tasks/{name}/run.
func (h *Handler) RunTask(ctx context.Context, params gen.RunTaskParams) (*gen.Job, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	j, err := h.Jobs.RunNow(ctx, params.Name)
	if err != nil {
		return nil, err
	}
	out := toJob(j)
	return &out, nil
}

// CancelJob implements POST /system/jobs/{id}/cancel.
func (h *Handler) CancelJob(ctx context.Context, params gen.CancelJobParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Jobs.Cancel(ctx, params.ID)
}
