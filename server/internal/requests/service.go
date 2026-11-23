package requests

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/notify"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

const permManage = auth.PermManageRequests

// Timeline event types written on the library item's timeline.
const (
	TypeRequested       = "requested"
	TypeRequestApproved = "request_approved"
	TypeIssueReported   = "issue_reported"
	TypeIssueResolved   = "issue_resolved"
)

// Service keeps requests and issues.
type Service struct {
	DB       *db.DB
	Store    *settings.Store
	Library  Library
	Hub      *notify.Hub
	Timeline *timeline.Store
	Log      *slog.Logger
	Now      func() time.Time

	syncMu   sync.Mutex
	kickOnce sync.Once
	kick     chan struct{}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Settings returns the request settings with every quota kind filled in.
func (s *Service) Settings(ctx context.Context) (Settings, error) {
	v, err := settings.Get(ctx, s.Store, SettingsKey)
	v.Normalise()
	return v, err
}

// SaveSettings stores the request settings.
func (s *Service) SaveSettings(ctx context.Context, v Settings) (Settings, error) {
	v.Normalise()
	for k, r := range v.Quotas {
		if r.Limit < 0 || r.Days < 1 || r.Days > 365 {
			return v, invalid("quota %s: limit must be 0 or more and days 1 to 365", k)
		}
	}
	for _, d := range []*KindDefaults{&v.Movie, &v.Series, &v.Music} {
		d.Tags = quality.NormTags(d.Tags)
		if d.Tags == nil {
			d.Tags = []string{}
		}
		if d.MinimumAvailability != "" && !library.ValidAvailability(d.MinimumAvailability) {
			return v, invalid("unknown minimum availability %q", d.MinimumAvailability)
		}
	}
	if err := settings.Set(ctx, s.Store, nil, SettingsKey, v); err != nil {
		return v, err
	}
	return v, nil
}

func (s *Service) publish(ctx context.Context, typ notify.Type, r *Request, actor *notify.User, to []int64, msg string, params map[string]any) {
	if s.Hub == nil {
		return
	}
	if params == nil {
		params = map[string]any{}
	}
	params["is4k"] = r.Is4K
	if r.Kind == KindSeries {
		params["seasons"] = r.SeasonNumbers()
	}
	s.Hub.Publish(ctx, notify.Event{Type: typ, Recipients: to, Actor: actor, Title: r.DisplayTitle(), Message: msg, Poster: r.Poster,
		MediaKind: string(r.Kind), RequestID: r.ID, Params: params})
}

func userOf(p auth.Principal) notify.User {
	return notify.User{ID: p.User.ID, Username: p.User.Username}
}

// CreateInput is a new request.
type CreateInput struct {
	Kind          Kind
	TMDbID        int64
	MBID          string
	Is4K          bool
	Seasons       []int
	AllSeasons    bool
	FutureSeasons bool
	Comment       string
	// Options are only taken from users who manage requests.
	ProfileID    *int64
	RootFolderID *int64
	Tags         *[]string
}

// Create stores a request of p and, when p may auto-approve it, approves it.
func (s *Service) Create(ctx context.Context, p auth.Principal, in CreateInput) (Request, error) {
	if !in.Kind.Valid() {
		return Request{}, invalid("unknown kind %q", in.Kind)
	}
	switch in.Kind {
	case KindMovie, KindSeries:
		if in.TMDbID <= 0 {
			return Request{}, invalid("tmdbId is required")
		}
		in.MBID = ""
	default:
		in.MBID = strings.ToLower(strings.TrimSpace(in.MBID))
		if in.MBID == "" {
			return Request{}, invalid("mbid is required")
		}
		in.TMDbID = 0
	}
	if !CanRequest(p, in.Kind, in.Is4K) {
		return Request{}, ErrForbidden
	}
	title, err := s.Library.Describe(ctx, in.Kind, in.TMDbID, in.MBID)
	if err != nil {
		return Request{}, err
	}
	r := Request{Kind: in.Kind, Is4K: in.Is4K, Status: StatusPending, TMDbID: in.TMDbID, MBID: in.MBID, ArtistMBID: title.ArtistMBID,
		Title: title.Title, Subtitle: title.Subtitle, Year: title.Year, Poster: title.Poster, RequestedBy: userOf(p), Tags: []string{},
		Seasons: []Season{}}
	if in.Kind == KindSeries {
		r.AllSeasons, r.FutureSeasons = in.AllSeasons, in.FutureSeasons
		seasons, err := s.pickSeasons(in, title)
		if err != nil {
			return Request{}, err
		}
		if seasons, err = s.freeSeasons(ctx, &r, seasons); err != nil {
			return Request{}, err
		}
		for _, n := range seasons {
			r.Seasons = append(r.Seasons, Season{Number: n, Status: StatusPending})
		}
	} else if err := s.checkDuplicate(ctx, &r); err != nil {
		return Request{}, err
	}
	if !QuotaExempt(p) {
		u, err := s.Usage(ctx, p.User.ID, in.Kind.QuotaKind())
		if err != nil {
			return Request{}, err
		}
		if !u.Unlimited && u.Remaining <= 0 {
			return Request{}, &QuotaError{Kind: in.Kind.QuotaKind(), Limit: u.Limit, Days: u.Days, NextSlot: u.NextSlot}
		}
	}
	if CanManage(p) {
		r.ProfileID, r.RootFolderID = positive(in.ProfileID), positive(in.RootFolderID)
		if in.Tags != nil {
			r.Tags = quality.NormTags(*in.Tags)
		}
	}
	if err := s.insert(ctx, &r); err != nil {
		return Request{}, err
	}
	actor := userOf(p)
	if c := strings.TrimSpace(in.Comment); c != "" {
		if _, err := s.addComment(ctx, "request_comments", "request_id", r.ID, actor, c); err != nil {
			return Request{}, err
		}
	}
	auto := AutoApproves(p, in.Kind, in.Is4K)
	s.publish(ctx, notify.RequestCreated, &r, &actor, s.managers(ctx), strings.TrimSpace(in.Comment), map[string]any{"autoApproved": auto})
	if auto {
		if err := s.approve(ctx, &r, nil, Options{}, nil); err != nil {
			// It stays pending for a manager (e.g. no defaults are set yet).
			s.log().Warn("auto-approve request", "request", r.ID, "err", err)
		}
	}
	return s.Get(ctx, r.ID)
}

func positive(p *int64) *int64 {
	if p == nil || *p <= 0 {
		return nil
	}
	return p
}

// pickSeasons returns the season numbers a series request asks for.
func (s *Service) pickSeasons(in CreateInput, t Title) ([]int, error) {
	known := map[int]bool{}
	for _, n := range t.Seasons {
		known[n] = true
	}
	var out []int
	if in.AllSeasons {
		out = append(out, t.Seasons...)
	} else {
		seen := map[int]bool{}
		for _, n := range in.Seasons {
			if seen[n] {
				continue
			}
			seen[n] = true
			if !known[n] && n != 0 {
				return nil, invalid("the series has no season %d", n)
			}
			out = append(out, n)
		}
	}
	sort.Ints(out)
	if len(out) == 0 && !in.FutureSeasons {
		return nil, ErrNothingToRequest
	}
	return out, nil
}

// freeSeasons drops seasons that are already requested (on the same track)
// or already in the library; an empty answer is a conflict.
func (s *Service) freeSeasons(ctx context.Context, r *Request, seasons []int) ([]int, error) {
	open, err := s.openRequests(ctx, r)
	if err != nil {
		return nil, err
	}
	taken := map[int]bool{}
	futureTaken := false
	for _, o := range open {
		for _, sn := range o.Seasons {
			taken[sn.Number] = true
		}
		futureTaken = futureTaken || o.FutureSeasons
	}
	avail := map[int]bool{}
	if !r.Is4K {
		if st, ok, err := s.Library.Lookup(ctx, KindSeries, r.TMDbID, ""); err != nil {
			return nil, err
		} else if ok {
			for n, ss := range st.Seasons {
				avail[n] = ss == StatusAvailable
			}
		}
	}
	var out []int
	requested, available := false, false
	for _, n := range seasons {
		switch {
		case taken[n]:
			requested = true
		case avail[n]:
			available = true
		default:
			out = append(out, n)
		}
	}
	if len(out) == 0 && (!r.FutureSeasons || futureTaken) {
		if requested || (len(seasons) == 0 && futureTaken) {
			return nil, ErrAlreadyRequested
		}
		if available {
			return nil, ErrAlreadyAvailable
		}
		return nil, ErrNothingToRequest
	}
	return out, nil
}

// openRequests returns the open requests for the same title and track whose
// library item (if any) still exists.
func (s *Service) openRequests(ctx context.Context, r *Request) ([]Request, error) {
	f := Filter{Kind: r.Kind, TMDbID: r.TMDbID, Limit: 200}
	if r.MBID != "" {
		f.TMDbID, f.MBID = 0, r.MBID
	}
	list, _, err := s.List(ctx, f)
	if err != nil {
		return nil, err
	}
	var out []Request
	for _, o := range list {
		if o.ID == r.ID || o.Is4K != r.Is4K || !o.Status.Open() || (r.MBID != "" && o.MBID != r.MBID) {
			continue
		}
		if o.ItemRef != "" && !s.Library.Exists(ctx, o.ItemRef) {
			continue // the title was removed from the library since
		}
		out = append(out, o)
	}
	return out, nil
}

func (s *Service) checkDuplicate(ctx context.Context, r *Request) error {
	open, err := s.openRequests(ctx, r)
	if err != nil {
		return err
	}
	for _, o := range open {
		if o.Status != StatusAvailable {
			return ErrAlreadyRequested
		}
	}
	if len(open) > 0 {
		return ErrAlreadyAvailable
	}
	if r.Is4K {
		return nil
	}
	st, ok, err := s.Library.Lookup(ctx, r.Kind, r.TMDbID, r.MBID)
	if err != nil {
		return err
	}
	if ok && st.Status == StatusAvailable {
		return ErrAlreadyAvailable
	}
	return nil
}

// options resolves the library options of a request: its own, else the
// defaults of its kind (the 4K ones for 4K requests).
func (s *Service) options(ctx context.Context, r *Request, override Options) (Options, error) {
	set, err := s.Settings(ctx)
	if err != nil {
		return Options{}, err
	}
	d := set.For(r.Kind)
	o := Options{ProfileID: d.ProfileID, RootFolderID: d.RootFolderID, MetadataProfileID: d.MetadataProfileID,
		MinimumAvailability: d.MinimumAvailability, Tags: d.Tags}
	if r.Is4K {
		if d.Profile4KID > 0 {
			o.ProfileID = d.Profile4KID
		}
		if d.RootFolder4KID > 0 {
			o.RootFolderID = d.RootFolder4KID
		}
	}
	if r.ProfileID != nil {
		o.ProfileID = *r.ProfileID
	}
	if r.RootFolderID != nil {
		o.RootFolderID = *r.RootFolderID
	}
	if len(r.Tags) > 0 {
		o.Tags = r.Tags
	}
	if override.ProfileID > 0 {
		o.ProfileID = override.ProfileID
	}
	if override.RootFolderID > 0 {
		o.RootFolderID = override.RootFolderID
	}
	if override.Tags != nil {
		o.Tags = override.Tags
	}
	if o.ProfileID == 0 || o.RootFolderID == 0 {
		return o, ErrDefaultsMissing
	}
	return o, nil
}

// approve approves r (by actor; nil = automatically), hands it to the library
// and records it on the item's timeline.
func (s *Service) approve(ctx context.Context, r *Request, actor *notify.User, override Options, seasons []int) error {
	if _, err := Next(r.Status, ActApprove); err != nil {
		return err
	}
	if seasons != nil {
		r.Seasons = r.Seasons[:0]
		for _, n := range seasons {
			r.Seasons = append(r.Seasons, Season{Number: n, Status: StatusPending})
		}
	}
	o, err := s.options(ctx, r, override)
	if err != nil {
		return err
	}
	ref, err := s.Library.Fulfil(ctx, r, o)
	if err != nil {
		return err
	}
	st, by, auto := StatusProcessing, int64(0), actor == nil
	if actor != nil {
		by = actor.ID
	}
	seasonStatus := map[int]Status{}
	for _, sn := range r.Seasons {
		seasonStatus[sn.Number] = StatusProcessing
	}
	c := change{status: &st, decidedBy: &by, auto: &auto, decided: true, itemRef: &ref, seasons: seasonStatus}
	if seasons != nil {
		c.setSeasons = seasons
	}
	if override.ProfileID > 0 {
		p := &override.ProfileID
		c.profile = &p
	}
	if override.RootFolderID > 0 {
		p := &override.RootFolderID
		c.root = &p
	}
	if err := s.update(ctx, r.ID, c); err != nil {
		return err
	}
	if s.Timeline != nil {
		data := map[string]any{"requestId": r.ID, "by": r.RequestedBy.Username, "is4k": r.Is4K}
		if r.Kind == KindSeries {
			data["seasons"] = r.SeasonNumbers()
		}
		_ = s.Timeline.Record(ctx, ref, TypeRequested, "requested by "+r.RequestedBy.Username, data, nil)
		msg := "request approved automatically"
		if actor != nil {
			msg, data["approvedBy"] = "request approved by "+actor.Username, actor.Username
		}
		_ = s.Timeline.Record(ctx, ref, TypeRequestApproved, msg, data, nil)
	}
	r.Status, r.ItemRef, r.AutoApproved = st, ref, auto
	if actor != nil {
		s.publish(ctx, notify.RequestApproved, r, actor, []int64{r.RequestedBy.ID}, "", nil)
	}
	// It may be in the library already (another track, an existing season).
	if fresh, err := s.Get(ctx, r.ID); err == nil {
		s.syncOne(ctx, &fresh)
	}
	return nil
}

func (s *Service) managed(ctx context.Context, p auth.Principal, id int64) (Request, error) {
	if !CanManage(p) {
		return Request{}, ErrForbidden
	}
	return s.Get(ctx, id)
}

// ApproveInput are a manager's changes made while approving.
type ApproveInput struct {
	ProfileID    int64
	RootFolderID int64
	Tags         []string
	Seasons      []int
}

// Approve approves a pending request (managers).
func (s *Service) Approve(ctx context.Context, p auth.Principal, id int64, in ApproveInput) (Request, error) {
	r, err := s.managed(ctx, p, id)
	if err != nil {
		return r, err
	}
	actor := userOf(p)
	o := Options{ProfileID: in.ProfileID, RootFolderID: in.RootFolderID}
	if in.Tags != nil {
		o.Tags = quality.NormTags(in.Tags)
	}
	if err := s.approve(ctx, &r, &actor, o, in.Seasons); err != nil {
		return r, err
	}
	return s.Get(ctx, id)
}

// BulkError is one failure of a bulk action.
type BulkError struct {
	ID  int64
	Err error
}

// ApproveMany approves several requests; failures do not stop the others.
func (s *Service) ApproveMany(ctx context.Context, p auth.Principal, ids []int64) (int, []BulkError) {
	if !CanManage(p) {
		return 0, []BulkError{{Err: ErrForbidden}}
	}
	done := 0
	var errs []BulkError
	for _, id := range ids {
		if _, err := s.Approve(ctx, p, id, ApproveInput{}); err != nil {
			errs = append(errs, BulkError{ID: id, Err: err})
			continue
		}
		done++
	}
	return done, errs
}

// Decline declines a request with a reason (managers).
func (s *Service) Decline(ctx context.Context, p auth.Principal, id int64, reason string) (Request, error) {
	r, err := s.managed(ctx, p, id)
	if err != nil {
		return r, err
	}
	st, err := Next(r.Status, ActDecline)
	if err != nil {
		return r, err
	}
	reason = strings.TrimSpace(reason)
	by := p.User.ID
	if err := s.update(ctx, id, change{status: &st, decidedBy: &by, decided: true, reason: &reason}); err != nil {
		return r, err
	}
	actor := userOf(p)
	s.publish(ctx, notify.RequestDeclined, &r, &actor, []int64{r.RequestedBy.ID}, reason, nil)
	return s.Get(ctx, id)
}

// Cancel withdraws an own pending request.
func (s *Service) Cancel(ctx context.Context, p auth.Principal, id int64) (Request, error) {
	r, err := s.Get(ctx, id)
	if err != nil {
		return r, err
	}
	if r.RequestedBy.ID != p.User.ID && !CanManage(p) {
		return r, ErrNotFound
	}
	st, err := Next(r.Status, ActCancel)
	if err != nil {
		return r, err
	}
	if err := s.update(ctx, id, change{status: &st}); err != nil {
		return r, err
	}
	return s.Get(ctx, id)
}

// Retry searches again for a failed request (managers); a request whose
// title was removed from the library is added again.
func (s *Service) Retry(ctx context.Context, p auth.Principal, id int64) (Request, error) {
	r, err := s.managed(ctx, p, id)
	if err != nil {
		return r, err
	}
	st, err := Next(r.Status, ActRetry)
	if err != nil {
		return r, err
	}
	if r.ItemRef == "" || !s.Library.Exists(ctx, r.ItemRef) {
		o, err := s.options(ctx, &r, Options{})
		if err != nil {
			return r, err
		}
		ref, err := s.Library.Fulfil(ctx, &r, o)
		if err != nil {
			return r, err
		}
		r.ItemRef = ref
	} else if err := s.Library.Search(ctx, &r); err != nil {
		return r, err
	}
	var none *Explanation
	if err := s.update(ctx, id, change{status: &st, itemRef: &r.ItemRef, explanation: &none}); err != nil {
		return r, err
	}
	return s.Get(ctx, id)
}

// EditInput changes a pending request's options (managers).
type EditInput struct {
	ProfileID    *int64
	RootFolderID *int64
	Tags         *[]string
	Seasons      []int
}

// Edit changes a pending request.
func (s *Service) Edit(ctx context.Context, p auth.Principal, id int64, in EditInput) (Request, error) {
	r, err := s.managed(ctx, p, id)
	if err != nil {
		return r, err
	}
	if r.Status != StatusPending {
		return r, fmt.Errorf("%w: only pending requests can be edited", ErrNotPending)
	}
	c := change{}
	if in.ProfileID != nil {
		v := positive(in.ProfileID)
		c.profile = &v
	}
	if in.RootFolderID != nil {
		v := positive(in.RootFolderID)
		c.root = &v
	}
	if in.Tags != nil {
		t := quality.NormTags(*in.Tags)
		if t == nil {
			t = []string{}
		}
		c.tags = &t
	}
	if in.Seasons != nil && r.Kind == KindSeries {
		seasons := append([]int{}, in.Seasons...)
		sort.Ints(seasons)
		if len(seasons) == 0 && !r.FutureSeasons {
			return r, ErrNothingToRequest
		}
		c.setSeasons = seasons
	}
	if err := s.update(ctx, id, c); err != nil {
		return r, err
	}
	return s.Get(ctx, id)
}

// Visible returns a request p may see (their own, or any for managers).
func (s *Service) Visible(ctx context.Context, p auth.Principal, id int64) (Request, error) {
	r, err := s.Get(ctx, id)
	if err != nil {
		return r, err
	}
	if r.RequestedBy.ID != p.User.ID && !CanManage(p) {
		return r, ErrNotFound
	}
	return r, nil
}

// Comments returns a request's comments.
func (s *Service) Comments(ctx context.Context, p auth.Principal, id int64) ([]Comment, error) {
	if _, err := s.Visible(ctx, p, id); err != nil {
		return nil, err
	}
	return s.comments(ctx, "request_comments", "request_id", id)
}

// Comment adds a comment and tells the other side.
func (s *Service) Comment(ctx context.Context, p auth.Principal, id int64, body string) (Comment, error) {
	r, err := s.Visible(ctx, p, id)
	if err != nil {
		return Comment{}, err
	}
	actor := userOf(p)
	c, err := s.addComment(ctx, "request_comments", "request_id", id, actor, body)
	if err != nil {
		return c, err
	}
	to := []int64{r.RequestedBy.ID}
	if p.User.ID == r.RequestedBy.ID {
		to = s.managers(ctx)
	}
	s.publish(ctx, notify.RequestComment, &r, &actor, to, c.Body, nil)
	return c, nil
}

// WithProgress fills in the download progress of unfinished requests.
func (s *Service) WithProgress(ctx context.Context, list []Request) {
	for i := range list {
		r := &list[i]
		if r.ItemRef == "" || (r.Status != StatusProcessing && r.Status != StatusPartial && r.Status != StatusApproved) {
			continue
		}
		if pr, err := s.Library.Progress(ctx, r); err == nil {
			r.Progress = pr
		}
	}
}

// Summary are the counts behind the badges.
type Summary struct {
	Pending, Processing, Failed, OpenIssues int
}

// Summary counts requests (everyone's for managers, own otherwise) and open issues.
func (s *Service) Summary(ctx context.Context, p auth.Principal) (Summary, error) {
	var out Summary
	where, args := "", []any{}
	if !CanManage(p) {
		where, args = " AND requested_by = ?", []any{p.User.ID}
	}
	err := s.DB.R.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status IN ('approved', 'processing', 'partial') THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0)
		FROM requests WHERE 1 = 1`+where, args...).Scan(&out.Pending, &out.Processing, &out.Failed)
	if err != nil {
		return out, err
	}
	iw := strings.ReplaceAll(where, "requested_by", "reported_by")
	err = s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE status = 'open'`+iw, args...).Scan(&out.OpenIssues)
	return out, err
}

// --- following the library ---

// Sync recomputes every request the library is working on and tells the
// requesters what changed. It runs after imports, on download events and
// on a schedule; runs never overlap.
func (s *Service) Sync(ctx context.Context) (int, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id FROM requests WHERE item_ref <> '' AND status IN ('approved', 'processing', 'partial', 'failed', 'available')`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	changed := 0
	for _, id := range ids {
		r, err := s.Get(ctx, id)
		if err != nil {
			continue
		}
		if s.syncOne(ctx, &r) {
			changed++
		}
	}
	return changed, nil
}

// syncOne brings one request up to date; it reports a status change.
func (s *Service) syncOne(ctx context.Context, r *Request) bool {
	st, per, err := s.Library.State(ctx, r)
	var ex *Explanation
	switch {
	case errors.Is(err, ErrGone):
		if r.Status == StatusAvailable {
			return false // it arrived; removing it later does not undo that
		}
		st, ex = StatusFailed, &Explanation{Code: "removed", Tone: "warning", Message: "the title was removed from the library"}
	case err != nil:
		s.log().Warn("request state", "request", r.ID, "err", err)
		return false
	}
	if ex == nil && st != StatusAvailable {
		for i := range r.Seasons {
			if v, ok := per[r.Seasons[i].Number]; ok {
				r.Seasons[i].Status = v
			}
		}
		if e, err := s.Library.Explain(ctx, r); err == nil {
			ex = e
			if st == StatusProcessing && Stuck(e.Code) {
				st = StatusFailed
			}
		}
	}
	if ex == nil {
		ex = &Explanation{Code: "available", Tone: "success", Message: "in the library"}
	}
	next, err := Next(r.Status, actionFor(st))
	if err != nil {
		next = r.Status
	}
	c := change{explanation: &ex, seasons: per}
	moved := next != r.Status
	if moved {
		c.status = &next
		c.available = next == StatusAvailable
	}
	if err := s.update(ctx, r.ID, c); err != nil {
		s.log().Warn("update request", "request", r.ID, "err", err)
		return false
	}
	if !moved {
		return false
	}
	prev := r.Status
	r.Status = next
	switch next {
	case StatusAvailable:
		s.publish(ctx, notify.RequestAvailable, r, nil, []int64{r.RequestedBy.ID}, "", nil)
	case StatusPartial:
		if prev != StatusAvailable {
			avail := []int{}
			for n, ss := range per {
				if ss == StatusAvailable {
					avail = append(avail, n)
				}
			}
			sort.Ints(avail)
			s.publish(ctx, notify.RequestPartial, r, nil, []int64{r.RequestedBy.ID}, "", map[string]any{"availableSeasons": avail})
		}
	case StatusFailed:
		to := append([]int64{r.RequestedBy.ID}, s.managers(ctx)...)
		s.publish(ctx, notify.RequestFailed, r, nil, to, ex.Message, map[string]any{"code": ex.Code})
	}
	return true
}
