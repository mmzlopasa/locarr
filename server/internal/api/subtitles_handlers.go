package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-faster/jx"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/subtitles"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
)

func mapSubtitlesError(err error) *StatusError {
	var (
		ve *subtitles.ValidationError
		qe *providers.QuotaError
		rl *providers.RateLimitError
	)
	switch {
	case errors.As(err, &ve):
		return &StatusError{Status: http.StatusBadRequest, Code: "validation", Message: ve.Msg}
	case errors.As(err, &qe):
		return &StatusError{Status: http.StatusTooManyRequests, Code: "subtitle_quota", Message: err.Error()}
	case errors.As(err, &rl):
		return &StatusError{Status: http.StatusTooManyRequests, Code: "subtitle_rate_limited", Message: err.Error()}
	}
	table := []struct {
		err    error
		status int
		code   string
	}{
		{subtitles.ErrNotFound, http.StatusNotFound, "not_found"},
		{subtitles.ErrNoFile, http.StatusConflict, "no_file"},
		{subtitles.ErrProfileInUse, http.StatusConflict, "subtitle_profile_in_use"},
		{subtitles.ErrDuplicateName, http.StatusConflict, "already_exists"},
		{subtitles.ErrResultExpired, http.StatusGone, "subtitle_result_expired"},
		{subtitles.ErrSyncUnavailable, http.StatusConflict, "subtitle_sync_unavailable"},
		{subtitles.ErrBadLanguage, http.StatusBadRequest, "validation"},
		{subtitles.ErrNotBetter, http.StatusConflict, "subtitle_not_better"},
		{providers.ErrAuth, http.StatusBadGateway, "subtitle_provider_auth"},
		{providers.ErrMissingKey, http.StatusBadRequest, "subtitle_provider_key_missing"},
		{providers.ErrNotFound, http.StatusBadGateway, "subtitle_provider_not_found"},
		{providers.ErrBadResponse, http.StatusBadGateway, "subtitle_provider_error"},
	}
	for _, e := range table {
		if errors.Is(err, e.err) {
			return &StatusError{Status: e.status, Code: e.code, Message: err.Error()}
		}
	}
	var he *providers.HTTPError
	if errors.As(err, &he) {
		return &StatusError{Status: http.StatusBadGateway, Code: "subtitle_provider_error", Message: err.Error()}
	}
	return nil
}

func subRef(s string) (library.Ref, error) {
	ref, err := library.ParseRef(s)
	if err != nil {
		return ref, err
	}
	if ref.Kind != library.RefMovie && ref.Kind != library.RefEpisode {
		return ref, library.ErrBadRef
	}
	return ref, nil
}

func toSubtitleProfile(p subtitles.Profile) (gen.SubtitleProfile, error) {
	return convert[gen.SubtitleProfile](p)
}

func fromSubtitleProfileInput(req *gen.SubtitleProfileInput) subtitles.ProfileInput {
	in := subtitles.ProfileInput{Name: req.Name, SkipEmbedded: req.SkipEmbedded.Or(true), Cutoff: req.Cutoff.Or(false)}
	for _, w := range req.Languages {
		in.Languages = append(in.Languages, subtitles.Want{Language: w.Language, Forced: w.Forced, HearingImpaired: string(w.HearingImpaired)})
	}
	return in
}

// ListSubtitleProfiles implements GET /subtitles/profiles.
func (h *Handler) ListSubtitleProfiles(ctx context.Context) ([]gen.SubtitleProfile, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, err := h.Subtitles.Profiles(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.SubtitleProfile, 0, len(list))
	for _, p := range list {
		g, err := toSubtitleProfile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

// CreateSubtitleProfile implements POST /subtitles/profiles.
func (h *Handler) CreateSubtitleProfile(ctx context.Context, req *gen.SubtitleProfileInput) (*gen.SubtitleProfile, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	p, err := h.Subtitles.CreateProfile(ctx, fromSubtitleProfileInput(req))
	if err != nil {
		return nil, err
	}
	g, err := toSubtitleProfile(p)
	return &g, err
}

// UpdateSubtitleProfile implements PUT /subtitles/profiles/{id}.
func (h *Handler) UpdateSubtitleProfile(ctx context.Context, req *gen.SubtitleProfileInput, p gen.UpdateSubtitleProfileParams) (*gen.SubtitleProfile, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	pr, err := h.Subtitles.UpdateProfile(ctx, p.ID, fromSubtitleProfileInput(req))
	if err != nil {
		return nil, err
	}
	g, err := toSubtitleProfile(pr)
	return &g, err
}

// DeleteSubtitleProfile implements DELETE /subtitles/profiles/{id}.
func (h *Handler) DeleteSubtitleProfile(ctx context.Context, p gen.DeleteSubtitleProfileParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Subtitles.DeleteProfile(ctx, p.ID)
}

// AssignSubtitleProfile implements POST /subtitles/profiles/assign.
func (h *Handler) AssignSubtitleProfile(ctx context.Context, req *gen.SubtitleProfileAssign) (*gen.SubtitleCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	ids := req.ItemIds
	if all, ok := req.All.Get(); ok {
		var err error
		if ids, err = h.Subtitles.ItemIDs(ctx, map[gen.SubtitleProfileAssignAll]string{gen.SubtitleProfileAssignAllMovies: "movie",
			gen.SubtitleProfileAssignAllSeries: "series"}[all]); err != nil {
			return nil, err
		}
	}
	n, err := h.Subtitles.AssignProfile(ctx, ids, req.ProfileId)
	if err != nil {
		return nil, err
	}
	return &gen.SubtitleCount{Count: n}, nil
}

func (h *Handler) subtitleSettings(ctx context.Context) (*gen.SubtitleSettings, error) {
	out := &gen.SubtitleSettings{SyncAvailable: h.Subtitles.SyncAvailable()}
	var err error
	get64 := func(k settings.Key[int64], dst *int64) {
		if err == nil {
			*dst, err = settings.Get(ctx, h.Settings, k)
		}
	}
	getInt := func(k settings.Key[int], dst *int) {
		if err == nil {
			*dst, err = settings.Get(ctx, h.Settings, k)
		}
	}
	get64(subtitles.DefaultProfileMovies, &out.DefaultProfileMovies)
	get64(subtitles.DefaultProfileSeries, &out.DefaultProfileSeries)
	getInt(subtitles.MinScoreMovies, &out.MinScoreMovies)
	getInt(subtitles.MinScoreEpisodes, &out.MinScoreEpisodes)
	getInt(subtitles.UpgradeDays, &out.UpgradeDays)
	getInt(subtitles.SearchesPerHour, &out.SearchesPerHour)
	if err != nil {
		return nil, err
	}
	f, err := settings.Get(ctx, h.Settings, subtitles.PreferredFormat)
	if err != nil {
		return nil, err
	}
	out.PreferredFormat = gen.SubtitleSettingsPreferredFormat(f)
	if out.AutoSync, err = settings.Get(ctx, h.Settings, subtitles.AutoSync); err != nil {
		return nil, err
	}
	return out, nil
}

// GetSubtitleSettings implements GET /subtitles/settings.
func (h *Handler) GetSubtitleSettings(ctx context.Context) (*gen.SubtitleSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return h.subtitleSettings(ctx)
}

// UpdateSubtitleSettings implements PUT /subtitles/settings.
func (h *Handler) UpdateSubtitleSettings(ctx context.Context, req *gen.SubtitleSettingsInput) (*gen.SubtitleSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	for _, v := range []gen.OptInt64{req.DefaultProfileMovies, req.DefaultProfileSeries} {
		if id, ok := v.Get(); ok && id > 0 {
			if _, err := h.Subtitles.Profile(ctx, id); err != nil {
				return nil, err
			}
		}
	}
	err := h.DB.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		set64 := func(k settings.Key[int64], v gen.OptInt64) {
			if x, ok := v.Get(); ok && err == nil {
				err = settings.Set(ctx, h.Settings, tx, k, x)
			}
		}
		setInt := func(k settings.Key[int], v gen.OptInt) {
			if x, ok := v.Get(); ok && err == nil {
				err = settings.Set(ctx, h.Settings, tx, k, x)
			}
		}
		set64(subtitles.DefaultProfileMovies, req.DefaultProfileMovies)
		set64(subtitles.DefaultProfileSeries, req.DefaultProfileSeries)
		setInt(subtitles.MinScoreMovies, req.MinScoreMovies)
		setInt(subtitles.MinScoreEpisodes, req.MinScoreEpisodes)
		setInt(subtitles.UpgradeDays, req.UpgradeDays)
		setInt(subtitles.SearchesPerHour, req.SearchesPerHour)
		if v, ok := req.PreferredFormat.Get(); ok && err == nil {
			err = settings.Set(ctx, h.Settings, tx, subtitles.PreferredFormat, string(v))
		}
		if v, ok := req.AutoSync.Get(); ok && err == nil {
			err = settings.Set(ctx, h.Settings, tx, subtitles.AutoSync, v)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if req.DefaultProfileMovies.Set || req.DefaultProfileSeries.Set || req.UpgradeDays.Set {
		h.Subtitles.QueueStateRefresh(ctx)
	}
	return h.subtitleSettings(ctx)
}

func toSubtitleProvider(p subtitles.ProviderState) gen.SubtitleProvider {
	info, _ := subtitles.ImplementationOf(p.Name)
	out := gen.SubtitleProvider{Name: p.Name, Enabled: p.Enabled, Priority: p.Priority, Username: p.Username,
		ApiKey: gen.SecretState{Configured: p.APIKeySet}, Password: gen.SecretState{Configured: p.PasswordSet},
		Languages: append([]string{}, info.Languages...), NeedsKey: info.NeedsKey, Website: info.Website, Failures: p.Failures,
		RetryAt: optTime(p.RetryAt), LastError: p.LastError, LastErrorCode: p.LastErrorCode, LastErrorAt: optTime(p.LastErrorAt),
		LastSuccessAt: optTime(p.LastSuccessAt), QuotaRemaining: optInt(p.QuotaRemaining), QuotaLimit: optInt(p.QuotaLimit),
		QuotaResetAt: optTime(p.QuotaResetAt), Account: p.Account, DownloadsToday: p.DownloadsToday, Fields: []gen.SubtitleProviderFieldsItem{}}
	for _, f := range info.Fields {
		out.Fields = append(out.Fields, gen.SubtitleProviderFieldsItem(f))
	}
	return out
}

// ListSubtitleProviders implements GET /subtitles/providers.
func (h *Handler) ListSubtitleProviders(ctx context.Context) ([]gen.SubtitleProvider, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, err := h.Subtitles.Providers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.SubtitleProvider, 0, len(list))
	for _, p := range list {
		out = append(out, toSubtitleProvider(p))
	}
	return out, nil
}

func optStrPtr(o gen.OptString) *string {
	if v, ok := o.Get(); ok {
		return &v
	}
	return nil
}

// UpdateSubtitleProvider implements PUT /subtitles/providers/{name}.
func (h *Handler) UpdateSubtitleProvider(ctx context.Context, req *gen.SubtitleProviderInput, p gen.UpdateSubtitleProviderParams) (*gen.SubtitleProvider, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	in := subtitles.ProviderInput{Username: optStrPtr(req.Username), APIKey: optStrPtr(req.ApiKey), Password: optStrPtr(req.Password)}
	if v, ok := req.Enabled.Get(); ok {
		in.Enabled = &v
	}
	if v, ok := req.Priority.Get(); ok {
		in.Priority = &v
	}
	st, err := h.Subtitles.UpdateProvider(ctx, string(p.Name), in)
	if err != nil {
		return nil, err
	}
	out := toSubtitleProvider(st)
	return &out, nil
}

// TestSubtitleProvider implements POST /subtitles/providers/{name}/test.
func (h *Handler) TestSubtitleProvider(ctx context.Context, p gen.TestSubtitleProviderParams) (*gen.SubtitleProvider, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	st, err := h.Subtitles.TestProvider(ctx, string(p.Name))
	if err != nil {
		return nil, err
	}
	out := toSubtitleProvider(st)
	return &out, nil
}

// ResetSubtitleProviderHealth implements POST /subtitles/providers/{name}/health/reset.
func (h *Handler) ResetSubtitleProviderHealth(ctx context.Context, p gen.ResetSubtitleProviderHealthParams) (*gen.SubtitleProvider, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	st, err := h.Subtitles.ResetHealth(ctx, string(p.Name))
	if err != nil {
		return nil, err
	}
	out := toSubtitleProvider(st)
	return &out, nil
}

func toSubtitle(s subtitles.Subtitle) (gen.Subtitle, error) { return convert[gen.Subtitle](s) }

func toSubtitles(list []subtitles.Subtitle) ([]gen.Subtitle, error) {
	out := make([]gen.Subtitle, 0, len(list))
	for _, s := range list {
		g, err := toSubtitle(s)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

func toSubtitleExplanation(e subtitles.Explanation) gen.SubtitleExplanation {
	out := gen.SubtitleExplanation{Code: e.Code, Tone: gen.SubtitleExplanationTone(e.Tone), Message: e.Message, Params: gen.SubtitleExplanationParams{}}
	for k, v := range e.Params {
		if b, err := json.Marshal(v); err == nil {
			out.Params[k] = jx.Raw(b)
		}
	}
	return out
}

// GetItemSubtitles implements GET /subtitles/items/{ref}.
func (h *Handler) GetItemSubtitles(ctx context.Context, p gen.GetItemSubtitlesParams) (*gen.SubtitleItem, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	ref, err := subRef(p.Ref)
	if err != nil {
		return nil, err
	}
	v, err := h.Subtitles.Item(ctx, ref)
	if err != nil {
		return nil, err
	}
	out := &gen.SubtitleItem{Ref: v.Ref, Title: v.Title, FilePath: v.FilePath, HasFile: v.HasFile, ProfileSource: gen.SubtitleItemProfileSource(v.Profile.Source),
		ProfileOverride: v.Profile.Override, ProbeError: v.ProbeError, Searches: v.State.Searches, LastSearchAt: optTime(v.State.LastSearchAt),
		NextSearchAt: optTime(v.State.NextSearchAt), LastResult: v.State.LastResult, ProvidersAsked: v.State.ProvidersAsked,
		Explanation: toSubtitleExplanation(v.Explanation), SyncAvailable: v.SyncAvailable, Wants: []gen.SubtitleWantStatus{}, Tracks: []gen.MediaTrack{}}
	if out.ProfileSource == "" {
		out.ProfileSource = gen.SubtitleItemProfileSourceNone
	}
	if v.Profile.Profile != nil {
		g, err := toSubtitleProfile(*v.Profile.Profile)
		if err != nil {
			return nil, err
		}
		out.Profile = gen.NewOptSubtitleProfile(g)
	}
	for _, w := range v.Wants {
		gw := gen.SubtitleWantStatus{Key: w.Key, Language: w.Language, Forced: w.Forced, HearingImpaired: w.HearingImpaired,
			Status: gen.SubtitleWantStatusStatus(w.Status), TrackIndex: optInt(w.TrackIndex)}
		if w.SubtitleID > 0 {
			gw.SubtitleId = gen.NewOptInt64(w.SubtitleID)
		}
		out.Wants = append(out.Wants, gw)
	}
	if out.Subtitles, err = toSubtitles(v.Subtitles); err != nil {
		return nil, err
	}
	for _, t := range v.Tracks {
		out.Tracks = append(out.Tracks, gen.MediaTrack{Index: t.Index, Kind: gen.MediaTrackKind(t.Kind), Codec: t.Codec, Language: t.Language, Name: t.Name,
			Default: t.Default, Forced: t.Forced, HearingImpaired: t.HearingImpaired, Text: t.Text})
	}
	return out, nil
}

func toReports(list []subtitles.ProviderReport) []gen.SubtitleProviderReport {
	out := make([]gen.SubtitleProviderReport, 0, len(list))
	for _, r := range list {
		out = append(out, gen.SubtitleProviderReport{Provider: r.Provider, Results: r.Results, Error: optStr(r.Error), Code: optStr(r.Code), Skipped: optStr(r.Skipped)})
	}
	return out
}

// SearchItemSubtitles implements POST /subtitles/items/{ref}/search.
func (h *Handler) SearchItemSubtitles(ctx context.Context, p gen.SearchItemSubtitlesParams) (*gen.SubtitleSearchOutcome, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	ref, err := subRef(p.Ref)
	if err != nil {
		return nil, err
	}
	o, err := h.Subtitles.Search(ctx, ref, "manual", true)
	if err != nil {
		return nil, err
	}
	out := &gen.SubtitleSearchOutcome{Ref: o.Ref, Providers: toReports(o.Searched), Missing: o.Missing, Message: o.Message}
	if out.Downloaded, err = toSubtitles(o.Downloaded); err != nil {
		return nil, err
	}
	return out, nil
}

// ManualSubtitleSearch implements GET /subtitles/items/{ref}/results.
func (h *Handler) ManualSubtitleSearch(ctx context.Context, p gen.ManualSubtitleSearchParams) (*gen.SubtitleManualSearch, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	ref, err := subRef(p.Ref)
	if err != nil {
		return nil, err
	}
	list, reports, err := h.Subtitles.Interactive(ctx, ref, p.Language.Or(""))
	if err != nil {
		return nil, err
	}
	out := &gen.SubtitleManualSearch{Providers: toReports(reports), Results: make([]gen.SubtitleResult, 0, len(list))}
	for _, r := range list {
		g := gen.SubtitleResult{Key: r.Key, Provider: r.Provider, ID: r.ID, Language: r.Language, Releases: append([]string{}, r.Releases...),
			FileName: r.FileName, HearingImpaired: r.HearingImpaired, Forced: r.Forced, HashMatch: r.HashMatch, Downloads: r.Downloads,
			Trusted: r.Trusted, Uploader: r.Uploader, MachineTranslated: r.MachineTranslated, Score: r.Score, Want: r.Want.Key(),
			Downloaded: r.Downloaded, Blacklisted: r.Blacklisted, MinScore: r.MinScore, Rejected: optStr(r.Rejected), PageUrl: optStr(r.PageURL),
			Parts: make([]gen.SubtitleScorePart, 0, len(r.Parts))}
		if r.FPS > 0 {
			g.Fps = gen.NewOptFloat64(r.FPS)
		}
		if r.Rating > 0 {
			g.Rating = gen.NewOptFloat64(r.Rating)
		}
		if r.SeasonPack {
			g.SeasonPack = gen.NewOptBool(true)
		}
		for _, pt := range r.Parts {
			g.Parts = append(g.Parts, gen.SubtitleScorePart{Code: pt.Code, Points: pt.Points})
		}
		out.Results = append(out.Results, g)
	}
	return out, nil
}

// DownloadItemSubtitle implements POST /subtitles/items/{ref}/download.
func (h *Handler) DownloadItemSubtitle(ctx context.Context, req *gen.DownloadItemSubtitleReq, p gen.DownloadItemSubtitleParams) (*gen.Subtitle, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	ref, err := subRef(p.Ref)
	if err != nil {
		return nil, err
	}
	s, err := h.Subtitles.DownloadManual(ctx, ref, req.Key)
	if err != nil {
		return nil, err
	}
	g, err := toSubtitle(s)
	return &g, err
}

// ListSubtitleBlacklist implements GET /subtitles/items/{ref}/blacklist.
func (h *Handler) ListSubtitleBlacklist(ctx context.Context, p gen.ListSubtitleBlacklistParams) ([]gen.SubtitleBlacklistEntry, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	ref, err := subRef(p.Ref)
	if err != nil {
		return nil, err
	}
	list, err := h.Subtitles.Blacklisted(ctx, ref.String())
	if err != nil {
		return nil, err
	}
	out := make([]gen.SubtitleBlacklistEntry, 0, len(list))
	for _, e := range list {
		out = append(out, gen.SubtitleBlacklistEntry{ID: e.ID, ItemRef: e.ItemRef, Provider: e.Provider, ProviderId: e.ProviderID,
			Language: e.Language, Release: e.Release, At: e.At})
	}
	return out, nil
}

// DeleteSubtitleBlacklistEntry implements DELETE /subtitles/blacklist/{id}.
func (h *Handler) DeleteSubtitleBlacklistEntry(ctx context.Context, p gen.DeleteSubtitleBlacklistEntryParams) error {
	if _, err := manager(ctx); err != nil {
		return err
	}
	return h.Subtitles.Unblacklist(ctx, p.ID)
}

// ListWantedSubtitles implements GET /subtitles/wanted.
func (h *Handler) ListWantedSubtitles(ctx context.Context, p gen.ListWantedSubtitlesParams) (*gen.WantedSubtitles, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, total, err := h.Subtitles.Wanted(ctx, string(p.Kind.Or("")), p.Offset.Or(0), p.Limit.Or(100))
	if err != nil {
		return nil, err
	}
	out := &gen.WantedSubtitles{Total: total, Items: make([]gen.WantedSubtitle, 0, len(list))}
	for _, w := range list {
		g := gen.WantedSubtitle{Ref: w.Ref, Kind: gen.WantedSubtitleKind(w.Kind), ItemId: w.ItemID, Title: w.Title, Missing: w.Missing,
			Searches: w.Searches, LastSearchAt: optTime(w.LastSearchAt), NextSearchAt: optTime(w.NextSearchAt), LastResult: w.LastResult}
		if w.Kind == "episode" {
			g.Season, g.Episode, g.EpisodeTitle = gen.NewOptInt(w.Season), gen.NewOptInt(w.Episode), gen.NewOptString(w.EpisodeTitle)
		}
		if g.Missing == nil {
			g.Missing = []string{}
		}
		out.Items = append(out.Items, g)
	}
	return out, nil
}

// SearchWantedSubtitles implements POST /subtitles/wanted/search.
func (h *Handler) SearchWantedSubtitles(ctx context.Context, req *gen.SearchWantedSubtitlesReq) (*gen.SubtitleCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	var refs []library.Ref
	if req.All.Or(false) {
		var err error
		if refs, err = h.Subtitles.WantedRefs(ctx, string(req.Kind.Or(""))); err != nil {
			return nil, err
		}
	}
	for _, r := range req.Refs {
		ref, err := subRef(r)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	n, err := h.Subtitles.QueueSearch(ctx, refs, "manual")
	if err != nil {
		return nil, err
	}
	return &gen.SubtitleCount{Count: n}, nil
}

// ScanSubtitles implements POST /subtitles/scan.
func (h *Handler) ScanSubtitles(ctx context.Context) (*gen.SubtitleCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	if _, err := h.Jobs.RunNow(ctx, subtitles.TaskScan); err != nil && !errors.Is(err, jobs.ErrUnknownTask) {
		return nil, err
	}
	return &gen.SubtitleCount{Count: 1}, nil
}

// DeleteSubtitle implements DELETE /subtitles/{id}.
func (h *Handler) DeleteSubtitle(ctx context.Context, p gen.DeleteSubtitleParams) error {
	if _, err := manager(ctx); err != nil {
		return err
	}
	return h.Subtitles.Delete(ctx, p.ID, p.Blacklist.Or(false))
}

// PreviewSubtitle implements GET /subtitles/{id}/preview.
func (h *Handler) PreviewSubtitle(ctx context.Context, p gen.PreviewSubtitleParams) (*gen.SubtitlePreview, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	cues, total, err := h.Subtitles.Preview(ctx, p.ID, p.Lines.Or(12))
	if err != nil {
		return nil, err
	}
	out := &gen.SubtitlePreview{Total: total, Lines: make([]gen.SubtitlePreviewLinesItem, 0, len(cues))}
	for _, c := range cues {
		out.Lines = append(out.Lines, gen.SubtitlePreviewLinesItem{StartMs: c.Start.Milliseconds(), EndMs: c.End.Milliseconds(), Text: c.Text})
	}
	return out, nil
}

// ShiftSubtitle implements POST /subtitles/{id}/shift.
func (h *Handler) ShiftSubtitle(ctx context.Context, req *gen.ShiftSubtitleReq, p gen.ShiftSubtitleParams) (*gen.Subtitle, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	s, err := h.Subtitles.Shift(ctx, p.ID, time.Duration(req.OffsetMs)*time.Millisecond)
	if err != nil {
		return nil, err
	}
	g, err := toSubtitle(s)
	return &g, err
}

// ConvertSubtitleFps implements POST /subtitles/{id}/fps.
func (h *Handler) ConvertSubtitleFps(ctx context.Context, req *gen.ConvertSubtitleFpsReq, p gen.ConvertSubtitleFpsParams) (*gen.Subtitle, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	from, err := subtitles.ParseFPS(req.From)
	if err != nil {
		return nil, err
	}
	to, err := subtitles.ParseFPS(req.To)
	if err != nil {
		return nil, err
	}
	s, err := h.Subtitles.ConvertFPS(ctx, p.ID, from, to)
	if err != nil {
		return nil, err
	}
	g, err := toSubtitle(s)
	return &g, err
}

// SyncSubtitle implements POST /subtitles/{id}/sync.
func (h *Handler) SyncSubtitle(ctx context.Context, req *gen.SyncSubtitleReq, p gen.SyncSubtitleParams) (*gen.Subtitle, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	s, err := h.Subtitles.QueueSync(ctx, p.ID, req.Reference.Or(""), false)
	if err != nil {
		return nil, err
	}
	g, err := toSubtitle(s)
	return &g, err
}
