package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/migrate"
)

// mapMigrateError maps import errors (nil when err is not one).
func mapMigrateError(err error) *StatusError {
	var se *migrate.SourceError
	if errors.As(err, &se) {
		return &StatusError{Status: http.StatusBadGateway, Code: "source_" + se.Code, Message: se.Message}
	}
	for _, t := range []struct {
		err    error
		status int
		code   string
	}{
		{migrate.ErrNotFound, http.StatusNotFound, "not_found"},
		{migrate.ErrInvalid, http.StatusBadRequest, "validation"},
		{migrate.ErrExpired, http.StatusConflict, "migrate_expired"},
		{migrate.ErrBusy, http.StatusConflict, "migrate_busy"},
		{migrate.ErrWrongState, http.StatusConflict, "conflict"},
	} {
		if errors.Is(err, t.err) {
			return &StatusError{Status: t.status, Code: t.code, Message: err.Error()}
		}
	}
	return nil
}

func (h *Handler) migrateReady(ctx context.Context) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	if h.Migrate == nil {
		return &StatusError{Status: http.StatusServiceUnavailable, Code: "unavailable", Message: "import is not available"}
	}
	return nil
}

func toMigrateSource(s migrate.Source) gen.MigrateSource {
	sup := s.Kind.Supported()
	out := gen.MigrateSource{ID: s.ID, Kind: gen.MigrateKind(s.Kind), Name: s.Name, URL: s.URL, Username: s.Username, SecretSet: s.SecretSet,
		AppName: s.AppName, Version: s.Version, LastError: s.LastError, CreatedAt: s.CreatedAt,
		Supports: gen.MigrateScopes{Settings: sup.Settings, Library: sup.Library, Users: sup.Users, History: sup.History}}
	if s.LastTestAt != nil {
		out.LastTestAt = gen.NewOptDateTime(*s.LastTestAt)
	}
	return out
}

func fromMigrateInput(req *gen.MigrateSourceInput) migrate.SourceInput {
	in := migrate.SourceInput{Kind: migrate.Kind(req.Kind), Name: req.Name.Or(""), URL: req.URL, Username: req.Username.Or("")}
	if v, ok := req.Secret.Get(); ok {
		in.Secret = &v
	}
	return in
}

// ListMigrateSources implements GET /migrate/sources.
func (h *Handler) ListMigrateSources(ctx context.Context) ([]gen.MigrateSource, error) {
	if err := h.migrateReady(ctx); err != nil {
		return nil, err
	}
	list, err := h.Migrate.Sources(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.MigrateSource, 0, len(list))
	for _, s := range list {
		out = append(out, toMigrateSource(s))
	}
	return out, nil
}

// CreateMigrateSource implements POST /migrate/sources.
func (h *Handler) CreateMigrateSource(ctx context.Context, req *gen.MigrateSourceInput) (*gen.MigrateSource, error) {
	if err := h.migrateReady(ctx); err != nil {
		return nil, err
	}
	s, err := h.Migrate.CreateSource(ctx, fromMigrateInput(req))
	if err != nil {
		return nil, err
	}
	out := toMigrateSource(s)
	return &out, nil
}

func testResult(d migrate.Detected, err error) (*gen.MigrateTestResult, error) {
	if err == nil {
		return &gen.MigrateTestResult{Ok: true, AppName: gen.NewOptString(d.AppName), Version: gen.NewOptString(d.Version)}, nil
	}
	var se *migrate.SourceError
	if errors.As(err, &se) {
		return &gen.MigrateTestResult{Ok: false, Code: gen.NewOptString(se.Code), Message: gen.NewOptString(se.Message)}, nil
	}
	return nil, err
}

// TestMigrateSource implements POST /migrate/sources/test.
func (h *Handler) TestMigrateSource(ctx context.Context, req *gen.MigrateSourceInput) (*gen.MigrateTestResult, error) {
	if err := h.migrateReady(ctx); err != nil {
		return nil, err
	}
	return testResult(h.Migrate.Test(ctx, 0, fromMigrateInput(req)))
}

// TestSavedMigrateSource implements POST /migrate/sources/{id}/test.
func (h *Handler) TestSavedMigrateSource(ctx context.Context, params gen.TestSavedMigrateSourceParams) (*gen.MigrateTestResult, error) {
	if err := h.migrateReady(ctx); err != nil {
		return nil, err
	}
	return testResult(h.Migrate.Test(ctx, params.ID, migrate.SourceInput{}))
}

// UpdateMigrateSource implements PUT /migrate/sources/{id}.
func (h *Handler) UpdateMigrateSource(ctx context.Context, req *gen.MigrateSourceInput, params gen.UpdateMigrateSourceParams) (*gen.MigrateSource, error) {
	if err := h.migrateReady(ctx); err != nil {
		return nil, err
	}
	s, err := h.Migrate.UpdateSource(ctx, params.ID, fromMigrateInput(req))
	if err != nil {
		return nil, err
	}
	out := toMigrateSource(s)
	return &out, nil
}

// DeleteMigrateSource implements DELETE /migrate/sources/{id}.
func (h *Handler) DeleteMigrateSource(ctx context.Context, params gen.DeleteMigrateSourceParams) error {
	if err := h.migrateReady(ctx); err != nil {
		return err
	}
	return h.Migrate.DeleteSource(ctx, params.ID)
}

func toPathMappings(list []migrate.PathMapping) []gen.MigratePathMapping {
	out := make([]gen.MigratePathMapping, 0, len(list))
	for _, m := range list {
		out = append(out, gen.MigratePathMapping{SourceId: m.SourceID, From: m.From, To: m.To})
	}
	return out
}

func fromPathMappings(list []gen.MigratePathMapping) []migrate.PathMapping {
	out := make([]migrate.PathMapping, 0, len(list))
	for _, m := range list {
		out = append(out, migrate.PathMapping{SourceID: m.SourceId, From: m.From, To: m.To})
	}
	return out
}

func toPathChecks(list []migrate.PathCheck) []gen.MigratePathCheck {
	out := make([]gen.MigratePathCheck, 0, len(list))
	for _, c := range list {
		missing := c.Missing
		if missing == nil {
			missing = []string{}
		}
		out = append(out, gen.MigratePathCheck{SourceId: c.SourceID, SourcePath: c.SourcePath, LocalPath: c.LocalPath, Inside: c.Inside,
			Exists: c.Exists, Files: c.Files, Found: c.Found, Missing: missing})
	}
	return out
}

func toMigrateRun(r migrate.Run) gen.MigrateRun {
	opts := gen.MigrateRunOptions{Sources: []gen.MigrateSourceOptions{}, PathMappings: toPathMappings(r.Options.PathMappings),
		Resolutions: gen.MigrateRunOptionsResolutions{}, HistoryDays: r.Options.HistoryDays}
	for _, s := range r.Options.Sources {
		so := gen.MigrateSourceOptions{ID: s.ID, Settings: gen.NewOptBool(s.Settings), Library: gen.NewOptBool(s.Library),
			Users: gen.NewOptBool(s.Users), History: gen.NewOptBool(s.History)}
		if s.BuiltinReplace != nil {
			so.BuiltinReplace = gen.NewOptBool(*s.BuiltinReplace)
		}
		if s.ProwlarrMode != "" {
			so.ProwlarrMode = gen.NewOptMigrateSourceOptionsProwlarrMode(gen.MigrateSourceOptionsProwlarrMode(s.ProwlarrMode))
		}
		opts.Sources = append(opts.Sources, so)
	}
	for k, v := range r.Options.Resolutions {
		opts.Resolutions[k] = v
	}
	sum := r.Summary
	gs := gen.MigrateSummary{Sources: []gen.MigrateSourceSummary{}, PathChecks: toPathChecks(sum.PathChecks), Suggested: toPathMappings(sum.Suggested),
		Counts: gen.MigrateSummaryCounts{}, Statuses: gen.MigrateSummaryStatuses{}, Attention: gen.MigrateSummaryAttention{},
		Conflicts: sum.Conflicts, Warnings: sum.Warnings}
	if gs.Warnings == nil {
		gs.Warnings = []string{}
	}
	for _, s := range sum.Sources {
		read := gen.MigrateSourceSummaryRead{}
		for k, v := range s.Read {
			read[k] = v
		}
		roots := s.Roots
		if roots == nil {
			roots = []string{}
		}
		gs.Sources = append(gs.Sources, gen.MigrateSourceSummary{ID: s.ID, Kind: gen.MigrateKind(s.Kind), Name: s.Name, Version: s.Version,
			Read: read, Roots: roots})
	}
	for cat, m := range sum.Counts {
		item := gen.MigrateSummaryCountsItem{}
		for k, v := range m {
			item[k] = v
		}
		gs.Counts[cat] = item
	}
	for k, v := range sum.Statuses {
		gs.Statuses[k] = v
	}
	for k, v := range sum.Attention {
		gs.Attention[k] = v
	}
	if sum.Refresh != nil {
		rf := gen.MigrateRefresh{Total: sum.Refresh.Total, Done: sum.Refresh.Done, Failed: sum.Refresh.Failed}
		if sum.Refresh.Error != "" {
			rf.Error = gen.NewOptString(sum.Refresh.Error)
		}
		gs.Refresh = gen.NewOptMigrateRefresh(rf)
	}
	if sum.DurationMS > 0 {
		gs.DurationMs = gen.NewOptInt64(sum.DurationMS)
	}
	out := gen.MigrateRun{ID: r.ID, Status: gen.MigrateRunStatus(r.Status), Options: opts, Summary: gs, Error: r.Error,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		Progress: gen.MigrateProgress{Phase: r.Progress.Phase, Done: r.Progress.Done, Total: r.Progress.Total, Message: r.Progress.Message}}
	if r.FinishedAt != nil {
		out.FinishedAt = gen.NewOptDateTime(*r.FinishedAt)
	}
	return out
}

// ListMigrateRuns implements GET /migrate/runs.
func (h *Handler) ListMigrateRuns(ctx context.Context) ([]gen.MigrateRun, error) {
	if err := h.migrateReady(ctx); err != nil {
		return nil, err
	}
	list, err := h.Migrate.Runs(ctx, 20)
	if err != nil {
		return nil, err
	}
	out := make([]gen.MigrateRun, 0, len(list))
	for _, r := range list {
		out = append(out, toMigrateRun(r))
	}
	return out, nil
}

// StartMigrateRun implements POST /migrate/runs.
func (h *Handler) StartMigrateRun(ctx context.Context, req *gen.MigrateOptions) (*gen.MigrateRun, error) {
	if err := h.migrateReady(ctx); err != nil {
		return nil, err
	}
	p, _ := principal(ctx)
	o := migrate.Options{PathMappings: fromPathMappings(req.PathMappings), HistoryDays: req.HistoryDays.Or(90)}
	for _, s := range req.Sources {
		so := migrate.SourceOptions{ID: s.ID, Settings: s.Settings.Or(false), Library: s.Library.Or(false), Users: s.Users.Or(false),
			History: s.History.Or(false)}
		if v, ok := s.BuiltinReplace.Get(); ok {
			so.BuiltinReplace = &v
		}
		if m, ok := s.ProwlarrMode.Get(); ok {
			so.ProwlarrMode = string(m)
		}
		o.Sources = append(o.Sources, so)
	}
	r, err := h.Migrate.Analyse(ctx, p.User.ID, o)
	if err != nil {
		return nil, err
	}
	out := toMigrateRun(r)
	return &out, nil
}

// GetMigrateRun implements GET /migrate/runs/{id}.
func (h *Handler) GetMigrateRun(ctx context.Context, params gen.GetMigrateRunParams) (*gen.MigrateRun, error) {
	if err := h.migrateReady(ctx); err != nil {
		return nil, err
	}
	r, err := h.Migrate.Run(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	out := toMigrateRun(r)
	return &out, nil
}

// PlanMigrateRun implements POST /migrate/runs/{id}/plan.
func (h *Handler) PlanMigrateRun(ctx context.Context, req *gen.MigratePlanRequest, params gen.PlanMigrateRunParams) (*gen.MigrateRun, error) {
	if err := h.migrateReady(ctx); err != nil {
		return nil, err
	}
	var mappings []migrate.PathMapping
	if req.PathMappings != nil {
		mappings = fromPathMappings(req.PathMappings)
	}
	var res map[string]string
	if v, ok := req.Resolutions.Get(); ok {
		res = map[string]string{}
		for k, x := range v {
			res[k] = string(x)
		}
	}
	modes := map[int64]string{}
	for _, m := range req.ProwlarrModes {
		modes[m.SourceId] = string(m.Mode)
	}
	r, err := h.Migrate.Replan(ctx, params.ID, mappings, res, modes)
	if err != nil {
		return nil, err
	}
	out := toMigrateRun(r)
	return &out, nil
}

// CheckMigratePaths implements POST /migrate/runs/{id}/paths.
func (h *Handler) CheckMigratePaths(ctx context.Context, req *gen.CheckMigratePathsReq, params gen.CheckMigratePathsParams) ([]gen.MigratePathCheck, error) {
	if err := h.migrateReady(ctx); err != nil {
		return nil, err
	}
	list, err := h.Migrate.CheckPaths(ctx, params.ID, fromPathMappings(req.PathMappings))
	if err != nil {
		return nil, err
	}
	return toPathChecks(list), nil
}

// ImportMigrateRun implements POST /migrate/runs/{id}/import.
func (h *Handler) ImportMigrateRun(ctx context.Context, params gen.ImportMigrateRunParams) (*gen.MigrateRun, error) {
	if err := h.migrateReady(ctx); err != nil {
		return nil, err
	}
	r, err := h.Migrate.Import(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	out := toMigrateRun(r)
	return &out, nil
}

// CancelMigrateRun implements POST /migrate/runs/{id}/cancel.
func (h *Handler) CancelMigrateRun(ctx context.Context, params gen.CancelMigrateRunParams) (*gen.MigrateRun, error) {
	if err := h.migrateReady(ctx); err != nil {
		return nil, err
	}
	r, err := h.Migrate.Cancel(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	out := toMigrateRun(r)
	return &out, nil
}

func toMigrateItem(it migrate.Item) gen.MigrateItem {
	att := it.Attention
	if att == nil {
		att = []string{}
	}
	out := gen.MigrateItem{Seq: it.Seq, Key: it.Key, SourceId: it.SourceID, Category: it.Category, Name: it.Name,
		Action: gen.MigrateAction(it.Action), Reason: it.Reason, Message: it.Message, Attention: att, Status: gen.MigrateItemStatus(it.Status),
		LocalRef: it.LocalRef, Error: it.Error}
	if it.Conflict != nil {
		c := gen.MigrateConflict{Existing: it.Conflict.Existing, Chosen: gen.MigrateConflictChosen(it.Conflict.Chosen),
			Options: []gen.MigrateConflictOptionsItem{}}
		for _, o := range it.Conflict.Options {
			c.Options = append(c.Options, gen.MigrateConflictOptionsItem(o))
		}
		out.Conflict = gen.NewOptMigrateConflict(c)
	}
	return out
}

// ListMigrateItems implements GET /migrate/runs/{id}/items.
func (h *Handler) ListMigrateItems(ctx context.Context, params gen.ListMigrateItemsParams) (*gen.MigrateItemList, error) {
	if err := h.migrateReady(ctx); err != nil {
		return nil, err
	}
	f := migrate.ItemFilter{Category: params.Category.Or(""), Attention: params.Attention.Or(false), Conflicts: params.Conflicts.Or(false),
		SourceID: params.SourceId.Or(0), Query: params.Q.Or(""), Offset: params.Offset.Or(0), Limit: params.Limit.Or(100)}
	if v, ok := params.Action.Get(); ok {
		f.Action = string(v)
	}
	if v, ok := params.Status.Get(); ok {
		f.Status = string(v)
	}
	items, total, err := h.Migrate.Items(ctx, params.ID, f)
	if err != nil {
		return nil, err
	}
	out := &gen.MigrateItemList{Items: make([]gen.MigrateItem, 0, len(items)), Total: total}
	for _, it := range items {
		out.Items = append(out.Items, toMigrateItem(it))
	}
	return out, nil
}

// GetMigrateReport implements GET /migrate/runs/{id}/report.
func (h *Handler) GetMigrateReport(ctx context.Context, params gen.GetMigrateReportParams) (*gen.MigrateReport, error) {
	if err := h.migrateReady(ctx); err != nil {
		return nil, err
	}
	rep, err := h.Migrate.Report(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	out := &gen.MigrateReport{Run: toMigrateRun(rep.Run), Sources: []gen.MigrateSource{}, Items: make([]gen.MigrateItem, 0, len(rep.Items))}
	for _, s := range rep.Sources {
		out.Sources = append(out.Sources, toMigrateSource(s))
	}
	for _, it := range rep.Items {
		out.Items = append(out.Items, toMigrateItem(it))
	}
	out.Generated = rep.Run.UpdatedAt
	return out, nil
}

// GetMigrateChecklist implements GET /migrate/checklist.
func (h *Handler) GetMigrateChecklist(ctx context.Context) ([]gen.MigrateCheckItem, error) {
	if err := h.migrateReady(ctx); err != nil {
		return nil, err
	}
	list, err := h.Migrate.Checklist(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.MigrateCheckItem, 0, len(list))
	for _, c := range list {
		params := gen.MigrateCheckItemParams{}
		for k, v := range c.Params {
			params[k] = fmtParam(v)
		}
		out = append(out, gen.MigrateCheckItem{Key: c.Key, Done: c.Done, Manual: c.Manual, Params: params})
	}
	return out, nil
}

func fmtParam(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	}
	return ""
}
