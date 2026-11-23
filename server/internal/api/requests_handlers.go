package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-faster/jx"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/notify"
	"github.com/mmzlopasa/locarr/server/internal/requests"
)

// mapRequestsError maps errors of requests, issues, invites and notifications.
func mapRequestsError(err error) *StatusError {
	var qe *requests.QuotaError
	if errors.As(err, &qe) {
		return &StatusError{Status: http.StatusTooManyRequests, Code: "quota_exceeded", Message: err.Error()}
	}
	table := []struct {
		err    error
		status int
		code   string
	}{
		{requests.ErrNotFound, http.StatusNotFound, "not_found"},
		{requests.ErrIssueNotFound, http.StatusNotFound, "not_found"},
		{requests.ErrForbidden, http.StatusForbidden, "forbidden"},
		{requests.ErrInvalid, http.StatusBadRequest, "validation"},
		{requests.ErrAlreadyRequested, http.StatusConflict, "already_requested"},
		{requests.ErrAlreadyAvailable, http.StatusConflict, "already_available"},
		{requests.ErrNothingToRequest, http.StatusBadRequest, "nothing_to_request"},
		{requests.ErrNotPending, http.StatusConflict, "not_pending"},
		{requests.ErrDefaultsMissing, http.StatusConflict, "defaults_missing"},
		{requests.ErrNotAvailable, http.StatusConflict, "not_available"},
		{auth.ErrInviteInvalid, http.StatusForbidden, "invite_invalid"},
		{notify.ErrNothingToMark, http.StatusBadRequest, "validation"},
	}
	for _, t := range table {
		if errors.Is(err, t.err) {
			return &StatusError{Status: t.status, Code: t.code, Message: err.Error()}
		}
	}
	return nil
}

func userRef(u notify.User) gen.UserRef { return gen.UserRef{ID: u.ID, Username: u.Username} }

func optUserRef(u *notify.User) gen.OptUserRef {
	if u == nil {
		return gen.OptUserRef{}
	}
	return gen.NewOptUserRef(userRef(*u))
}

func rawMap(m map[string]any) map[string]jx.Raw {
	out := map[string]jx.Raw{}
	for k, v := range m {
		if b, err := json.Marshal(v); err == nil {
			out[k] = jx.Raw(b)
		}
	}
	return out
}

func optPos(n int64) gen.OptInt64 {
	if n <= 0 {
		return gen.OptInt64{}
	}
	return gen.NewOptInt64(n)
}

func optPtr(p *int64) gen.OptInt64 {
	if p == nil {
		return gen.OptInt64{}
	}
	return gen.NewOptInt64(*p)
}

// toRequest converts a request for p: library internals only for managers.
func toRequest(p auth.Principal, r requests.Request) gen.MediaRequest {
	out := gen.MediaRequest{ID: r.ID, Kind: gen.RequestKind(r.Kind), Is4k: r.Is4K, Status: gen.RequestStatus(r.Status), TmdbId: optPos(r.TMDbID),
		Mbid: optStr(r.MBID), ArtistMbid: optStr(r.ArtistMBID), Title: r.Title, Subtitle: optStr(r.Subtitle), Year: r.Year,
		Poster: optStr(r.Poster), Seasons: make([]gen.RequestSeason, 0, len(r.Seasons)), AllSeasons: r.AllSeasons,
		FutureSeasons: r.FutureSeasons, RequestedBy: userRef(r.RequestedBy), DecidedBy: optUserRef(r.DecidedBy), AutoApproved: r.AutoApproved,
		DecidedAt: optTime(r.DecidedAt), DeclineReason: optStr(r.DeclineReason), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		AvailableAt: optTime(r.AvailableAt), Tags: []string{}, CommentCount: r.CommentCount,
		CanCancel: r.Status == requests.StatusPending && r.RequestedBy.ID == p.User.ID}
	for _, sn := range r.Seasons {
		out.Seasons = append(out.Seasons, gen.RequestSeason{Number: sn.Number, Status: gen.RequestStatus(sn.Status)})
	}
	if ex := r.Explanation; ex != nil && ex.Code != "" {
		tone := gen.RequestExplanationTone(ex.Tone)
		if tone == "" {
			tone = gen.RequestExplanationToneInfo
		}
		e := gen.RequestExplanation{Code: ex.Code, Tone: tone, Message: ex.Message}
		if len(ex.Params) > 0 {
			e.Params = gen.NewOptRequestExplanationParams(rawMap(ex.Params))
		}
		out.Explanation = gen.NewOptRequestExplanation(e)
	}
	if pr := r.Progress; pr != nil {
		g := gen.RequestProgress{Percent: pr.Percent, Downloads: pr.Downloads, State: optStr(pr.State)}
		if pr.ETA != nil {
			g.EtaSeconds = gen.NewOptInt(int(*pr.ETA))
		}
		out.Progress = gen.NewOptRequestProgress(g)
	}
	if requests.CanManage(p) {
		out.QualityProfileId, out.RootFolderId, out.Tags = optPtr(r.ProfileID), optPtr(r.RootFolderID), r.Tags
	}
	if p.Can(auth.PermManageLibrary) {
		out.LibraryRef = optStr(r.ItemRef)
	}
	return out
}

func toRequests(p auth.Principal, list []requests.Request) []gen.MediaRequest {
	out := make([]gen.MediaRequest, 0, len(list))
	for _, r := range list {
		out = append(out, toRequest(p, r))
	}
	return out
}

// ListRequests implements GET /requests.
func (h *Handler) ListRequests(ctx context.Context, params gen.ListRequestsParams) (*gen.RequestList, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	f := requests.Filter{Status: string(params.Status.Or("")), Kind: requests.Kind(params.Kind.Or("")), Offset: params.Offset.Or(0),
		Limit: params.Limit.Or(50)}
	switch {
	case !requests.CanManage(p) || params.Mine.Or(false):
		f.UserID = p.User.ID
	default:
		f.UserID = params.UserId.Or(0)
	}
	list, total, err := h.Requests.List(ctx, f)
	if err != nil {
		return nil, err
	}
	h.Requests.WithProgress(ctx, list)
	return &gen.RequestList{Items: toRequests(p, list), Total: total}, nil
}

// CreateRequest implements POST /requests.
func (h *Handler) CreateRequest(ctx context.Context, req *gen.CreateRequest) (*gen.MediaRequest, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	in := requests.CreateInput{Kind: requests.Kind(req.Kind), TMDbID: req.TmdbId.Or(0), MBID: req.Mbid.Or(""), Is4K: req.Is4k.Or(false),
		Seasons: req.Seasons, AllSeasons: req.AllSeasons.Or(false), FutureSeasons: req.FutureSeasons.Or(false), Comment: req.Comment.Or("")}
	if o, ok := req.Options.Get(); ok {
		if v, ok := o.QualityProfileId.Get(); ok {
			in.ProfileID = &v
		}
		if v, ok := o.RootFolderId.Get(); ok {
			in.RootFolderID = &v
		}
		if o.Tags != nil {
			in.Tags = &o.Tags
		}
	}
	r, err := h.Requests.Create(ctx, p, in)
	if err != nil {
		return nil, err
	}
	out := toRequest(p, r)
	return &out, nil
}

func (h *Handler) quotaStatus(ctx context.Context, userID int64, kind string) (gen.QuotaStatus, error) {
	u, err := h.Requests.Usage(ctx, userID, kind)
	if err != nil {
		return gen.QuotaStatus{}, err
	}
	out := gen.QuotaStatus{Limit: u.Limit, Days: u.Days, Used: u.Used, Unlimited: u.Unlimited, Custom: u.Custom, NextSlotAt: optTime(u.NextSlot)}
	if !u.Unlimited {
		out.Remaining = gen.NewOptInt(u.Remaining)
	}
	return out, nil
}

// GetRequestSummary implements GET /requests/summary.
func (h *Handler) GetRequestSummary(ctx context.Context) (*gen.RequestSummary, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	s, err := h.Requests.Summary(ctx, p)
	if err != nil {
		return nil, err
	}
	out := &gen.RequestSummary{Pending: s.Pending, Processing: s.Processing, Failed: s.Failed, OpenIssues: s.OpenIssues}
	for _, k := range []struct {
		kind string
		dst  *gen.QuotaStatus
	}{{requests.QuotaMovie, &out.Quotas.Movie}, {requests.QuotaSeries, &out.Quotas.Series}, {requests.QuotaMusic, &out.Quotas.Music}} {
		q, err := h.quotaStatus(ctx, p.User.ID, k.kind)
		if err != nil {
			return nil, err
		}
		if requests.QuotaExempt(p) {
			q = gen.QuotaStatus{Limit: 0, Days: q.Days, Used: q.Used, Unlimited: true}
		}
		*k.dst = q
	}
	return out, nil
}

// ApproveRequests implements POST /requests/approve.
func (h *Handler) ApproveRequests(ctx context.Context, req *gen.RequestIds) (*gen.BulkResult, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if !requests.CanManage(p) {
		return nil, errForbidden
	}
	done, errs := h.Requests.ApproveMany(ctx, p, req.Ids)
	out := &gen.BulkResult{Done: done, Errors: []gen.BulkError{}}
	for _, e := range errs {
		code, msg := "internal", e.Err.Error()
		if se := mapError(e.Err); se != nil {
			code = se.Code
		}
		out.Errors = append(out.Errors, gen.BulkError{ID: e.ID, Code: code, Message: msg})
	}
	return out, nil
}

// GetRequest implements GET /requests/{id}.
func (h *Handler) GetRequest(ctx context.Context, params gen.GetRequestParams) (*gen.MediaRequest, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	r, err := h.Requests.Visible(ctx, p, params.ID)
	if err != nil {
		return nil, err
	}
	list := []requests.Request{r}
	h.Requests.WithProgress(ctx, list)
	out := toRequest(p, list[0])
	return &out, nil
}

// UpdateRequest implements PATCH /requests/{id}.
func (h *Handler) UpdateRequest(ctx context.Context, req *gen.RequestOptions, params gen.UpdateRequestParams) (*gen.MediaRequest, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	in := requests.EditInput{Seasons: req.Seasons}
	if v, ok := req.QualityProfileId.Get(); ok {
		in.ProfileID = &v
	}
	if v, ok := req.RootFolderId.Get(); ok {
		in.RootFolderID = &v
	}
	if req.Tags != nil {
		in.Tags = &req.Tags
	}
	r, err := h.Requests.Edit(ctx, p, params.ID, in)
	if err != nil {
		return nil, err
	}
	out := toRequest(p, r)
	return &out, nil
}

// ApproveRequest implements POST /requests/{id}/approve.
func (h *Handler) ApproveRequest(ctx context.Context, req gen.OptRequestOptions, params gen.ApproveRequestParams) (*gen.MediaRequest, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	in := requests.ApproveInput{}
	if o, ok := req.Get(); ok {
		in.ProfileID, in.RootFolderID, in.Tags, in.Seasons = o.QualityProfileId.Or(0), o.RootFolderId.Or(0), o.Tags, o.Seasons
	}
	r, err := h.Requests.Approve(ctx, p, params.ID, in)
	if err != nil {
		return nil, err
	}
	out := toRequest(p, r)
	return &out, nil
}

// DeclineRequest implements POST /requests/{id}/decline.
func (h *Handler) DeclineRequest(ctx context.Context, req *gen.DeclineRequest, params gen.DeclineRequestParams) (*gen.MediaRequest, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	r, err := h.Requests.Decline(ctx, p, params.ID, req.Reason.Or(""))
	if err != nil {
		return nil, err
	}
	out := toRequest(p, r)
	return &out, nil
}

// CancelRequest implements POST /requests/{id}/cancel.
func (h *Handler) CancelRequest(ctx context.Context, params gen.CancelRequestParams) (*gen.MediaRequest, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	r, err := h.Requests.Cancel(ctx, p, params.ID)
	if err != nil {
		return nil, err
	}
	out := toRequest(p, r)
	return &out, nil
}

// RetryRequest implements POST /requests/{id}/retry.
func (h *Handler) RetryRequest(ctx context.Context, params gen.RetryRequestParams) (*gen.MediaRequest, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	r, err := h.Requests.Retry(ctx, p, params.ID)
	if err != nil {
		return nil, err
	}
	out := toRequest(p, r)
	return &out, nil
}

func toComments(list []requests.Comment) []gen.Comment {
	out := make([]gen.Comment, 0, len(list))
	for _, c := range list {
		out = append(out, gen.Comment{ID: c.ID, User: userRef(c.User), Body: c.Body, CreatedAt: c.CreatedAt})
	}
	return out
}

// ListRequestComments implements GET /requests/{id}/comments.
func (h *Handler) ListRequestComments(ctx context.Context, params gen.ListRequestCommentsParams) ([]gen.Comment, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	list, err := h.Requests.Comments(ctx, p, params.ID)
	if err != nil {
		return nil, err
	}
	return toComments(list), nil
}

// AddRequestComment implements POST /requests/{id}/comments.
func (h *Handler) AddRequestComment(ctx context.Context, req *gen.CommentInput, params gen.AddRequestCommentParams) (*gen.Comment, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	c, err := h.Requests.Comment(ctx, p, params.ID, req.Body)
	if err != nil {
		return nil, err
	}
	out := toComments([]requests.Comment{c})[0]
	return &out, nil
}

// --- issues ---

func toIssue(p auth.Principal, is requests.Issue) gen.Issue {
	out := gen.Issue{ID: is.ID, Kind: gen.IssueKind(is.Kind), Type: gen.IssueType(is.Type), Status: gen.IssueStatus(is.Status), Title: is.Title,
		TmdbId: optPos(is.TMDbID), Mbid: optStr(is.MBID), Poster: optStr(is.Poster), SeasonNumber: optIntPtr(is.Season),
		EpisodeNumber: optIntPtr(is.Episode), Message: is.Message, ReportedBy: userRef(is.ReportedBy), ResolvedBy: optUserRef(is.ResolvedBy),
		ResolvedAt: optTime(is.ResolvedAt), ReplacementSearched: gen.NewOptBool(is.ReplacementSearched), CreatedAt: is.CreatedAt,
		UpdatedAt: is.UpdatedAt, CommentCount: is.CommentCount}
	if p.Can(auth.PermManageLibrary) {
		out.LibraryRef = optStr(is.ItemRef)
	}
	return out
}

// ListIssues implements GET /issues.
func (h *Handler) ListIssues(ctx context.Context, params gen.ListIssuesParams) (*gen.IssueList, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	list, total, err := h.Requests.Issues(ctx, p, string(params.Status.Or("")), params.Offset.Or(0), params.Limit.Or(50))
	if err != nil {
		return nil, err
	}
	out := &gen.IssueList{Items: make([]gen.Issue, 0, len(list)), Total: total}
	for _, is := range list {
		out.Items = append(out.Items, toIssue(p, is))
	}
	return out, nil
}

// CreateIssue implements POST /issues.
func (h *Handler) CreateIssue(ctx context.Context, req *gen.IssueInput) (*gen.Issue, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	in := requests.IssueInput{Kind: requests.Kind(req.Kind), TMDbID: req.TmdbId.Or(0), MBID: req.Mbid.Or(""), Type: string(req.Type),
		Message: req.Message}
	if v, ok := req.SeasonNumber.Get(); ok {
		in.Season = &v
	}
	if v, ok := req.EpisodeNumber.Get(); ok {
		in.Episode = &v
	}
	is, err := h.Requests.CreateIssue(ctx, p, in)
	if err != nil {
		return nil, err
	}
	out := toIssue(p, is)
	return &out, nil
}

// GetIssue implements GET /issues/{id}.
func (h *Handler) GetIssue(ctx context.Context, params gen.GetIssueParams) (*gen.Issue, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	is, err := h.Requests.GetIssue(ctx, p, params.ID)
	if err != nil {
		return nil, err
	}
	out := toIssue(p, is)
	return &out, nil
}

// ResolveIssue implements POST /issues/{id}/resolve.
func (h *Handler) ResolveIssue(ctx context.Context, req *gen.ResolveIssueRequest, params gen.ResolveIssueParams) (*gen.Issue, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	is, err := h.Requests.ResolveIssue(ctx, p, params.ID, req.SearchReplacement.Or(false), req.Comment.Or(""))
	if err != nil {
		return nil, err
	}
	out := toIssue(p, is)
	return &out, nil
}

// ReopenIssue implements POST /issues/{id}/reopen.
func (h *Handler) ReopenIssue(ctx context.Context, params gen.ReopenIssueParams) (*gen.Issue, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	is, err := h.Requests.ReopenIssue(ctx, p, params.ID)
	if err != nil {
		return nil, err
	}
	out := toIssue(p, is)
	return &out, nil
}

// ListIssueComments implements GET /issues/{id}/comments.
func (h *Handler) ListIssueComments(ctx context.Context, params gen.ListIssueCommentsParams) ([]gen.Comment, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	list, err := h.Requests.IssueComments(ctx, p, params.ID)
	if err != nil {
		return nil, err
	}
	return toComments(list), nil
}

// AddIssueComment implements POST /issues/{id}/comments.
func (h *Handler) AddIssueComment(ctx context.Context, req *gen.CommentInput, params gen.AddIssueCommentParams) (*gen.Comment, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	c, err := h.Requests.IssueComment(ctx, p, params.ID, req.Body)
	if err != nil {
		return nil, err
	}
	out := toComments([]requests.Comment{c})[0]
	return &out, nil
}

// --- notifications ---

// ListNotifications implements GET /notifications.
func (h *Handler) ListNotifications(ctx context.Context, params gen.ListNotificationsParams) (*gen.NotificationList, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	pg, err := h.Hub.List(ctx, p.User.ID, notify.ListOptions{Since: params.Since.Or(0), UnreadOnly: params.Unread.Or(false),
		Limit: params.Limit.Or(50)})
	if err != nil {
		return nil, err
	}
	out := &gen.NotificationList{Items: make([]gen.Notification, 0, len(pg.Items)), Unread: pg.Unread, LatestId: pg.LatestID}
	for _, n := range pg.Items {
		g := gen.Notification{ID: n.ID, Type: gen.NotificationType(n.Type), Title: n.Title, Message: optStr(n.Message), Poster: optStr(n.Poster),
			MediaKind: optStr(n.MediaKind), Actor: optUserRef(n.Actor), RequestId: optPos(n.RequestID), IssueId: optPos(n.IssueID),
			CreatedAt: n.CreatedAt, Read: n.Read}
		if len(n.Params) > 0 {
			g.Params = gen.NewOptNotificationParams(rawMap(n.Params))
		}
		out.Items = append(out.Items, g)
	}
	return out, nil
}

// MarkNotificationsRead implements POST /notifications/read.
func (h *Handler) MarkNotificationsRead(ctx context.Context, req *gen.MarkReadRequest) (*gen.UnreadCount, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	n, err := h.Hub.MarkRead(ctx, p.User.ID, req.Ids, req.All.Or(false))
	if err != nil {
		return nil, err
	}
	return &gen.UnreadCount{Unread: n}, nil
}

// --- settings and quotas ---

func toKindDefaults(d requests.KindDefaults) gen.KindDefaults {
	out := gen.KindDefaults{QualityProfileId: d.ProfileID, RootFolderId: d.RootFolderID, QualityProfile4kId: d.Profile4KID,
		RootFolder4kId: d.RootFolder4KID, MetadataProfileId: gen.NewOptInt64(d.MetadataProfileID), Tags: d.Tags}
	if d.MinimumAvailability != "" {
		out.MinimumAvailability = gen.NewOptMinimumAvailability(gen.MinimumAvailability(d.MinimumAvailability))
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	return out
}

func fromKindDefaults(g gen.KindDefaults) requests.KindDefaults {
	return requests.KindDefaults{ProfileID: g.QualityProfileId, RootFolderID: g.RootFolderId, Profile4KID: g.QualityProfile4kId,
		RootFolder4KID: g.RootFolder4kId, MetadataProfileID: g.MetadataProfileId.Or(0),
		MinimumAvailability: library.Availability(g.MinimumAvailability.Or("")), Tags: g.Tags}
}

func toRequestSettings(s requests.Settings) *gen.RequestSettings {
	rule := func(k string) gen.QuotaRule { r := s.Quota(k); return gen.QuotaRule{Limit: r.Limit, Days: r.Days} }
	return &gen.RequestSettings{Movie: toKindDefaults(s.Movie), Series: toKindDefaults(s.Series), Music: toKindDefaults(s.Music),
		Quotas: gen.QuotaRules{Movie: rule(requests.QuotaMovie), Series: rule(requests.QuotaSeries), Music: rule(requests.QuotaMusic)}}
}

// GetRequestSettings implements GET /settings/requests.
func (h *Handler) GetRequestSettings(ctx context.Context) (*gen.RequestSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	s, err := h.Requests.Settings(ctx)
	if err != nil {
		return nil, err
	}
	return toRequestSettings(s), nil
}

// PutRequestSettings implements PUT /settings/requests.
func (h *Handler) PutRequestSettings(ctx context.Context, req *gen.RequestSettings) (*gen.RequestSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	s := requests.Settings{Movie: fromKindDefaults(req.Movie), Series: fromKindDefaults(req.Series), Music: fromKindDefaults(req.Music),
		Quotas: map[string]requests.Rule{
			requests.QuotaMovie:  {Limit: req.Quotas.Movie.Limit, Days: req.Quotas.Movie.Days},
			requests.QuotaSeries: {Limit: req.Quotas.Series.Limit, Days: req.Quotas.Series.Days},
			requests.QuotaMusic:  {Limit: req.Quotas.Music.Limit, Days: req.Quotas.Music.Days},
		}}
	saved, err := h.Requests.SaveSettings(ctx, s)
	if err != nil {
		return nil, err
	}
	return toRequestSettings(saved), nil
}

func toOverrides(o requests.Overrides) gen.QuotaOverrides {
	var out gen.QuotaOverrides
	set := func(k string, dst *gen.OptQuotaRule) {
		if r, ok := o[k]; ok {
			*dst = gen.NewOptQuotaRule(gen.QuotaRule{Limit: r.Limit, Days: r.Days})
		}
	}
	set(requests.QuotaMovie, &out.Movie)
	set(requests.QuotaSeries, &out.Series)
	set(requests.QuotaMusic, &out.Music)
	return out
}

func fromOverrides(g gen.QuotaOverrides) requests.Overrides {
	out := requests.Overrides{}
	get := func(k string, v gen.OptQuotaRule) {
		if r, ok := v.Get(); ok {
			out[k] = requests.Rule{Limit: r.Limit, Days: r.Days}
		}
	}
	get(requests.QuotaMovie, g.Movie)
	get(requests.QuotaSeries, g.Series)
	get(requests.QuotaMusic, g.Music)
	return out
}

func (h *Handler) userQuotas(ctx context.Context, id int64) (*gen.UserQuotas, error) {
	if _, err := h.Auth.User(ctx, id); err != nil {
		return nil, err
	}
	o, err := h.Requests.UserOverrides(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &gen.UserQuotas{Overrides: toOverrides(o)}
	for _, k := range []struct {
		kind string
		dst  *gen.QuotaStatus
	}{{requests.QuotaMovie, &out.Movie}, {requests.QuotaSeries, &out.Series}, {requests.QuotaMusic, &out.Music}} {
		q, err := h.quotaStatus(ctx, id, k.kind)
		if err != nil {
			return nil, err
		}
		*k.dst = q
	}
	return out, nil
}

// GetUserQuotas implements GET /users/{id}/quotas.
func (h *Handler) GetUserQuotas(ctx context.Context, params gen.GetUserQuotasParams) (*gen.UserQuotas, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return h.userQuotas(ctx, params.ID)
}

// SetUserQuotas implements PUT /users/{id}/quotas.
func (h *Handler) SetUserQuotas(ctx context.Context, req *gen.QuotaOverrides, params gen.SetUserQuotasParams) (*gen.UserQuotas, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	if _, err := h.Auth.User(ctx, params.ID); err != nil {
		return nil, err
	}
	if err := h.Requests.SetOverrides(ctx, params.ID, fromOverrides(*req)); err != nil {
		return nil, err
	}
	return h.userQuotas(ctx, params.ID)
}

// --- invites and sign-in providers ---

func toInvite(in auth.Invite, now time.Time) gen.Invite {
	out := gen.Invite{ID: in.ID, Label: in.Label, Role: gen.Role(in.Role), Permissions: toPermissions(in.Permissions), MaxUses: in.MaxUses,
		Uses: in.Uses, ExpiresAt: optTime(in.ExpiresAt), CreatedAt: in.CreatedAt, CreatedBy: optStr(in.CreatedBy), Revoked: in.Revoked,
		Expired: !in.Revoked && !in.Usable(now)}
	var o gen.QuotaOverrides
	if len(in.Quotas) > 2 && json.Unmarshal(in.Quotas, &o) == nil {
		out.Quotas = gen.NewOptQuotaOverrides(o)
	}
	return out
}

// ListInvites implements GET /invites.
func (h *Handler) ListInvites(ctx context.Context) ([]gen.Invite, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	list, err := h.Auth.Invites(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]gen.Invite, 0, len(list))
	for _, in := range list {
		out = append(out, toInvite(in, now))
	}
	return out, nil
}

// CreateInvite implements POST /invites.
func (h *Handler) CreateInvite(ctx context.Context, req *gen.InviteInput) (*gen.CreatedInvite, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	in := auth.NewInvite{Label: req.Label.Or(""), Role: auth.Role(req.Role), MaxUses: req.MaxUses.Or(1),
		ExpiresIn: time.Duration(req.ExpiresInDays.Or(auth.DefaultInviteDays)) * 24 * time.Hour}
	if v, ok := req.Permissions.Get(); ok {
		perms := fromPermissions(v)
		in.Permissions = &perms
	}
	if q, ok := req.Quotas.Get(); ok {
		raw, err := json.Marshal(&q)
		if err != nil {
			return nil, err
		}
		in.Quotas = raw
	}
	inv, code, err := h.Auth.CreateInvite(ctx, p.User.ID, in)
	if err != nil {
		return nil, err
	}
	return &gen.CreatedInvite{Invite: toInvite(inv, time.Now()), Code: code}, nil
}

// RevokeInvite implements DELETE /invites/{id}.
func (h *Handler) RevokeInvite(ctx context.Context, params gen.RevokeInviteParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Auth.RevokeInvite(ctx, params.ID)
}

// CheckInvite implements POST /auth/invite.
func (h *Handler) CheckInvite(ctx context.Context, req *gen.InviteCodeRequest) (*gen.InviteCheck, error) {
	inv, err := h.Auth.CheckInvite(ctx, req.Code)
	if err != nil {
		return nil, err
	}
	name, _ := h.instanceName(ctx)
	return &gen.InviteCheck{Role: gen.Role(inv.Role), Label: optStr(inv.Label), ExpiresAt: optTime(inv.ExpiresAt), InstanceName: name}, nil
}

// SignUp implements POST /auth/signup.
func (h *Handler) SignUp(ctx context.Context, req *gen.SignUpRequest) (*gen.SignedInHeaders, error) {
	p, token, inv, err := h.Auth.SignUp(ctx, req.Code, string(req.Username), string(req.Password), string(req.Language.Or("")),
		device(ctx, req.Device))
	if err != nil {
		if errors.Is(err, auth.ErrInviteInvalid) {
			h.Log.Warn("sign-up with an invalid invite", "ip", requestInfo(ctx).ClientIP.String())
		}
		return nil, err
	}
	var o gen.QuotaOverrides
	if len(inv.Quotas) > 2 && json.Unmarshal(inv.Quotas, &o) == nil {
		if err := h.Requests.SetOverrides(ctx, p.User.ID, fromOverrides(o)); err != nil {
			h.Log.Warn("invite quotas", "user", p.User.ID, "err", err)
		}
	}
	h.Log.Info("user signed up with an invite", "user", p.User.Username, "invite", inv.ID)
	return signedIn(ctx, p, token, req.UseCookie.Or(false)), nil
}

// ListAuthProviders implements GET /auth/providers.
func (h *Handler) ListAuthProviders(ctx context.Context) ([]gen.AuthProvider, error) {
	list := h.Auth.Providers()
	out := make([]gen.AuthProvider, 0, len(list))
	for _, p := range list {
		out = append(out, gen.AuthProvider{ID: p.ID, Kind: gen.AuthProviderKind(p.Kind), Name: p.Name})
	}
	return out, nil
}
