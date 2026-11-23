// Package wanted is the shared search orchestration (D-032): automatic search
// on add and on a schedule with backoff per item, RSS matching, decisions
// through the engine, grabs, pending (delayed) releases, retry with the next
// best release after a failed download, and upgrades until the cutoff. Each
// media kind plugs in as a Source; movies are the first.
package wanted

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/customformats"
	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/decision"
	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// Settings.
var (
	// SearchIntervalMinutes is how often the scheduled search looks for due items.
	SearchIntervalMinutes = settings.Key[int]{Name: "wanted.searchIntervalMinutes", Default: 15}
	// SearchBatch is how many items one scheduled run searches at most.
	SearchBatch = settings.Key[int]{Name: "wanted.searchBatch", Default: 10}
	// RetryFailed re-searches after a failed download (the failed release is blocklisted).
	RetryFailed = settings.Key[bool]{Name: "wanted.retryFailedDownloads", Default: true}
)

// Task kinds.
const (
	TaskSearch        = "wanted-search"         // one item
	TaskSearchMissing = "wanted-search-missing" // scheduled
	TaskPending       = "wanted-pending"        // re-evaluate held-back releases
)

// Backoff between automatic searches of one item: 30 min doubling, at most a day.
const (
	backoffBase = 30 * time.Minute
	backoffMax  = 24 * time.Hour
	// afterGrab: the next automatic search after a grab (upgrades).
	afterGrab = 24 * time.Hour
	// pendingRecheck is when a pending release without a known wait is looked at again.
	pendingRecheck = 15 * time.Minute
	pendingExpire  = 7 * 24 * time.Hour
)

// Backoff returns the wait after the n-th fruitless search (n >= 1).
func Backoff(n int) time.Duration {
	d := backoffBase
	for i := 1; i < n && d < backoffMax; i++ {
		d *= 2
	}
	return min(d, backoffMax)
}

// Item is a wanted item as the orchestration sees it.
type Item struct {
	Ref       library.Ref
	Title     string
	Monitored bool
	Available bool
	HasFile   bool
	CutoffMet bool
	ProfileID int64
	Wanted    decision.Wanted
	Query     indexers.Query
	// MediaKind is the download clients' category kind (movies, series, music).
	MediaKind string
	// Covers lists what the item wants when one item stands for several (a
	// season: its wanted episodes). Empty = the item itself.
	Covers []library.Ref
	// PreferPacks ranks releases that cover all of Covers first (season packs).
	PreferPacks bool
	// Busy: a download for (part of) the item is under way.
	Busy bool
}

// Multi is implemented by sources where one release may cover several items
// (episodes: season packs and multi-episode releases).
type Multi interface {
	// Covers names the item a release is grabbed for and every item it
	// covers; a zero ref means the release is of no use for it.
	Covers(ctx context.Context, it Item, d *decision.Decision) (library.Ref, []library.Ref)
	// Refine may reject decisions the engine cannot judge (for example a
	// season pack offered to a single-episode search).
	Refine(ctx context.Context, it Item, rep *decision.Report)
}

// Matcher maps an RSS release to the wanted items it may be.
type Matcher func(r *indexers.Release, p *parser.Release) []library.Ref

// Source is one media kind.
type Source interface {
	// Kind is the ref kind it owns ("movie").
	Kind() string
	Item(ctx context.Context, id int64) (Item, error)
	// Wanted lists monitored, available items that are missing or below the cutoff.
	Wanted(ctx context.Context) ([]Item, error)
	Matcher(ctx context.Context) (Matcher, error)
}

// Service orchestrates searches and grabs.
type Service struct {
	DB        *db.DB
	Indexers  *indexers.Service
	Downloads *downloads.Service
	Quality   *quality.Store
	Formats   *customformats.Store
	Timeline  *timeline.Store
	Settings  *settings.Store
	Jobs      *jobs.Queue
	Log       *slog.Logger
	Now       func() time.Time

	// GrabGap is the least time between two grabs from one indexer (default 2 s).
	GrabGap time.Duration

	mu       sync.Mutex
	sources  map[string]Source
	locks    map[string]*sync.Mutex
	lastGrab map[int64]time.Time
}

// Errors.
var (
	ErrUnknownKind = errors.New("no wanted source for this kind")
	ErrBusy        = errors.New("this item already has an active download")
)

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Register adds a media kind.
func (s *Service) Register(src Source) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sources == nil {
		s.sources = map[string]Source{}
	}
	s.sources[src.Kind()] = src
}

func (s *Service) source(kind string) (Source, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, ok := s.sources[kind]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownKind, kind)
	}
	return src, nil
}

func (s *Service) lock(ref string) func() {
	s.mu.Lock()
	if s.locks == nil {
		s.locks = map[string]*sync.Mutex{}
	}
	l, ok := s.locks[ref]
	if !ok {
		l = &sync.Mutex{}
		s.locks[ref] = l
	}
	s.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (s *Service) item(ctx context.Context, ref library.Ref) (Source, Item, error) {
	src, err := s.source(ref.Kind)
	if err != nil {
		return nil, Item{}, err
	}
	it, err := src.Item(ctx, ref.ID)
	return src, it, err
}

func (s *Service) record(ctx context.Context, ref, typ, msg string, data any) {
	if err := s.Timeline.Record(ctx, ref, typ, msg, data, nil); err != nil {
		s.Log.Warn("timeline", "err", err)
	}
}

// decisionContext loads profiles and rules for an item.
func (s *Service) decisionContext(ctx context.Context, it Item, mode decision.Mode) (decision.Context, error) {
	p, err := s.Quality.Profile(ctx, it.ProfileID)
	if err != nil {
		return decision.Context{}, err
	}
	dc := decision.Context{Wanted: it.Wanted, Profile: p, Mode: mode, Now: s.now(), Blocklist: Blocklist{Checker: s.Downloads}}
	dc.Wanted.Kind = p.Kind
	formats, err := s.Formats.List(ctx)
	if err != nil {
		return dc, err
	}
	dc.Formats, _ = customformats.CompileAll(formats)
	if dc.Definitions, err = s.Quality.Definitions(ctx); err != nil {
		return dc, err
	}
	if dc.ReleaseProfiles, err = s.Quality.ReleaseProfiles(ctx); err != nil {
		return dc, err
	}
	if dc.DelayProfiles, err = s.Quality.DelayProfiles(ctx); err != nil {
		return dc, err
	}
	return dc, nil
}

func (s *Service) indexerMap(ctx context.Context) map[int64]*indexers.Indexer {
	list, err := s.Indexers.List(ctx)
	out := map[int64]*indexers.Indexer{}
	if err != nil {
		return out
	}
	for i := range list {
		out[list[i].ID] = &list[i]
	}
	return out
}

// releaseKey identifies a release within one evaluation.
func releaseKey(indexerID int64, guid string) string { return fmt.Sprintf("%d\x00%s", indexerID, guid) }

// Evaluation is a decision report with the releases behind it.
type Evaluation struct {
	Report   decision.Report
	Releases map[string]indexers.Release
	Search   indexers.Result
}

// Release returns the release behind a decision.
func (e *Evaluation) Release(d *decision.Decision) (indexers.Release, bool) {
	r, ok := e.Releases[releaseKey(d.Candidate.IndexerID, d.Candidate.GUID)]
	return r, ok
}

func (s *Service) evaluate(ctx context.Context, it Item, mode decision.Mode, releases []indexers.Release) (Evaluation, error) {
	dc, err := s.decisionContext(ctx, it, mode)
	if err != nil {
		return Evaluation{}, err
	}
	inds := s.indexerMap(ctx)
	ev := Evaluation{Releases: map[string]indexers.Release{}}
	cands := make([]decision.Candidate, 0, len(releases))
	for i := range releases {
		r := releases[i]
		ev.Releases[releaseKey(r.IndexerID, r.GUID)] = r
		cands = append(cands, Candidate(&r, inds[r.IndexerID]))
	}
	ev.Report = decision.Evaluate(ctx, dc, cands)
	if src, err := s.source(it.Ref.Kind); err == nil {
		if m, ok := src.(Multi); ok {
			m.Refine(ctx, it, &ev.Report)
			ev.Report.Finalize()
		}
	}
	return ev, nil
}

// Outcome is what a search did.
type Outcome struct {
	Searched bool
	Found    int
	Accepted int
	Pending  int
	Grabbed  *downloads.Download
	// Grabs counts every release sent (a season search may grab several).
	Grabs   int
	Skipped string // why nothing was searched
}

// activeDownload reports a download under way that is for ref or covers it.
func (s *Service) activeDownload(ctx context.Context, ref string) bool {
	var n int
	_ = s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE status IN ('active', 'completed')
		AND id IN (SELECT download_id FROM download_refs WHERE item_ref = ?)`, ref).Scan(&n)
	return n > 0
}

// Search runs an automatic search for one item and grabs the best release.
// reason is recorded ("added", "scheduled", "manual", "retry", …); manual
// searches also run for unmonitored or unavailable items.
func (s *Service) Search(ctx context.Context, ref library.Ref, reason string) (Outcome, error) {
	unlock := s.lock(ref.String())
	defer unlock()
	_, it, err := s.item(ctx, ref)
	if err != nil {
		return Outcome{}, err
	}
	manual := reason == "manual"
	switch {
	case !manual && !it.Monitored:
		return Outcome{Skipped: "not_monitored"}, nil
	case !manual && !it.Available:
		return Outcome{Skipped: "not_available"}, nil
	case it.HasFile && it.CutoffMet && !manual:
		return Outcome{Skipped: "cutoff_met"}, nil
	case it.Busy || s.activeDownload(ctx, ref.String()):
		return Outcome{Skipped: "downloading"}, nil
	}
	q := it.Query
	q.Source, q.ItemRef = indexers.SourceAutomatic, ref.String()
	res, err := s.Indexers.Search(ctx, q)
	if err != nil {
		return Outcome{}, err
	}
	ev, err := s.evaluate(ctx, it, decision.ModeSearch, res.Releases)
	if err != nil {
		return Outcome{}, err
	}
	ev.Search = res
	sum := timeline.Summarize(ev.Report)
	s.record(ctx, ref.String(), timeline.TypeDecision, decisionMessage(sum, reason), map[string]any{"summary": sum, "reason": reason})
	out := Outcome{Searched: true, Found: len(res.Releases), Accepted: ev.Report.Accepted, Pending: ev.Report.Pending}
	d, n, err := s.act(ctx, it, &ev)
	if err != nil {
		return out, err
	}
	out.Grabbed, out.Grabs = d, n
	s.updateState(ctx, ref.String(), d != nil, sum)
	return out, nil
}

func decisionMessage(s timeline.DecisionSummary, reason string) string {
	msg := fmt.Sprintf("%s search: %d releases, %d accepted, %d pending, %d rejected", reason, s.Candidates, s.Accepted, s.Pending, s.Rejected)
	if s.Best != "" {
		msg += "; best: " + s.Best
	}
	return msg
}

// updateState records the search and schedules the next one.
func (s *Service) updateState(ctx context.Context, ref string, grabbed bool, sum timeline.DecisionSummary) {
	now := s.now()
	var searches int
	_ = s.DB.R.QueryRowContext(ctx, `SELECT searches FROM search_state WHERE item_ref = ?`, ref).Scan(&searches)
	searches++
	next := now.Add(Backoff(searches))
	result := "nothing_accepted"
	if grabbed {
		next, result = now.Add(afterGrab), "grabbed"
	} else if sum.Pending > 0 {
		result = "pending"
	}
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO search_state (item_ref, searches, last_search_at, next_search_at, last_result)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT (item_ref) DO UPDATE SET searches = excluded.searches, last_search_at = excluded.last_search_at,
		next_search_at = excluded.next_search_at, last_result = excluded.last_result`,
		ref, searches, db.Millis(now), db.Millis(next), result); err != nil {
		s.Log.Warn("search state", "err", err)
	}
}

// ResetBackoff makes an item due for the next scheduled search (after a
// change that may make releases acceptable).
func (s *Service) ResetBackoff(ctx context.Context, ref library.Ref) {
	_, _ = s.DB.W.ExecContext(ctx, `UPDATE search_state SET next_search_at = ? WHERE item_ref = ?`, db.Millis(s.now()), ref.String())
}

// RetryNow clears an item's search backoff and queues an automatic search at
// once ("retry now" on the item's page). Indexer limits and grab pacing still
// apply to the search and its grabs.
func (s *Service) RetryNow(ctx context.Context, ref library.Ref) error {
	if _, _, err := s.item(ctx, ref); err != nil {
		return err
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE search_state SET searches = 0, next_search_at = ? WHERE item_ref = ?`,
		db.Millis(s.now()), ref.String()); err != nil {
		return err
	}
	return s.Enqueue(ctx, ref, "manual")
}

// act grabs the best accepted release (trying the next on failure) and keeps
// pending ones for later. For a Multi source it keeps going until everything
// the item wants is covered: a season pack, or the season's episodes one by
// one, never two releases for the same episode. It returns the first grab and
// how many releases were sent.
func (s *Service) act(ctx context.Context, it Item, ev *Evaluation) (*downloads.Download, int, error) {
	ref := it.Ref.String()
	var multi Multi
	if src, err := s.source(it.Ref.Kind); err == nil {
		multi, _ = src.(Multi)
	}
	type option struct {
		d      *decision.Decision
		ref    library.Ref
		covers []library.Ref
	}
	wanted := map[library.Ref]bool{}
	for _, c := range it.Covers {
		wanted[c] = true
	}
	var opts []option
	for i := range ev.Report.Decisions {
		d := &ev.Report.Decisions[i]
		if !d.Accepted() {
			break
		}
		o := option{d: d, ref: it.Ref}
		if multi != nil {
			o.ref, o.covers = multi.Covers(ctx, it, d)
			if o.ref.IsZero() {
				continue
			}
		}
		if len(o.covers) == 0 {
			o.covers = []library.Ref{o.ref}
		}
		opts = append(opts, o)
	}
	full := func(o option) bool {
		if len(wanted) == 0 {
			return false
		}
		have := map[library.Ref]bool{}
		for _, c := range o.covers {
			have[c] = true
		}
		for w := range wanted {
			if !have[w] {
				return false
			}
		}
		return true
	}
	if it.PreferPacks {
		sort.SliceStable(opts, func(i, j int) bool { return full(opts[i]) && !full(opts[j]) })
	}
	covered := map[library.Ref]bool{}
	var first *downloads.Download
	grabs := 0
	attempts, _ := settings.Get(ctx, s.Settings, GrabAttempts)
	attempts = max(attempts, 1)
	failures := 0
	// skip holds indexers whose releases are left alone for the rest of this
	// cycle (limited, disabled or failing).
	skip := map[int64]bool{}
	for _, o := range opts {
		if failures >= attempts {
			s.record(ctx, ref, timeline.TypeGrabFailed, fmt.Sprintf("stopped after %d failed grabs; the next search tries again", failures),
				map[string]any{"code": "attempts_exhausted", "attempts": failures})
			break
		}
		overlap := false
		for _, c := range o.covers {
			overlap = overlap || covered[c]
		}
		if overlap {
			continue
		}
		r, ok := ev.Release(o.d)
		if !ok {
			continue
		}
		if skip[r.IndexerID] {
			continue
		}
		if resting, until := s.indexerResting(ctx, r.IndexerID); resting {
			skip[r.IndexerID] = true
			s.record(ctx, ref, timeline.TypeGrabFailed, fmt.Sprintf("skipped releases from %s: the indexer is limited until %s", r.IndexerName, until.UTC().Format(time.RFC3339)),
				map[string]any{"title": r.Title, "indexer": r.IndexerName, "code": GrabIndexerLimited, "until": until.UTC(), "skipped": true})
			continue
		}
		if err := s.pace(ctx, r.IndexerID); err != nil {
			return first, grabs, err
		}
		dl, err := s.grab(ctx, it, o.ref, o.covers, &r)
		if err != nil {
			f := classifyGrab(err, s.now())
			data := map[string]any{"title": r.Title, "indexer": r.IndexerName, "error": shortError(err), "code": f.Code}
			if f.Until != nil {
				data["until"] = f.Until.UTC()
			}
			if f.Code == GrabNoClient {
				s.record(ctx, ref, timeline.TypeGrabFailed, "no download client takes this release: "+err.Error(), data)
				return first, grabs, nil
			}
			if f.IndexerProblem {
				skip[r.IndexerID] = true
			}
			if f.Content {
				s.blockBadRelease(ctx, &r, o.ref, o.covers, err)
				data["blocklisted"] = true
			}
			if f.Code != GrabAlreadyQueued {
				failures++
			}
			s.record(ctx, ref, timeline.TypeGrabFailed, fmt.Sprintf("could not grab %s: %s", r.Title, shortError(err)), data)
			continue
		}
		grabs++
		if first == nil {
			first = &dl
			// A grab supersedes anything held back.
			_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM pending_releases WHERE item_ref = ?`, ref)
		}
		for _, c := range o.covers {
			covered[c] = true
		}
		if multi == nil {
			return first, grabs, nil
		}
		done := len(wanted) > 0
		for w := range wanted {
			done = done && covered[w]
		}
		if done || len(wanted) == 0 {
			break
		}
	}
	if first == nil {
		s.keepPending(ctx, it, ev)
	}
	return first, grabs, nil
}

func (s *Service) grab(ctx context.Context, it Item, ref library.Ref, covers []library.Ref, r *indexers.Release) (downloads.Download, error) {
	link := r.DownloadURL
	if link == "" {
		link = r.MagnetURL
	}
	id := r.IndexerID
	req := downloads.GrabRequest{DownloadURL: link, Protocol: downloaders.Protocol(r.Protocol), Title: r.Title, IndexerID: &id,
		Indexer: r.IndexerName, GUID: r.GUID, ItemRef: ref.JSONWith(covers), MediaKind: it.MediaKind}
	if len(it.Wanted.Tags) > 0 {
		// Item tags select clients too (with the indexer's own tags).
		tags := append([]string{}, it.Wanted.Tags...)
		if ind, err := s.Indexers.Get(ctx, id); err == nil {
			tags = append(tags, ind.Tags...)
		}
		req.Tags = tags
	}
	if !r.PublishDate.IsZero() {
		p := r.PublishDate
		req.PublishedAt = &p
	}
	return s.Downloads.Grab(ctx, req)
}

// keepPending stores releases held back by temporary rejections.
func (s *Service) keepPending(ctx context.Context, it Item, ev *Evaluation) {
	now := s.now()
	ref := it.Ref.String()
	for i := range ev.Report.Decisions {
		d := &ev.Report.Decisions[i]
		if d.Status != decision.StatusPending {
			continue
		}
		retry := now.Add(pendingRecheck)
		var reasons []string
		for _, rj := range d.Rejections {
			reasons = append(reasons, rj.Message)
			if rj.Code == decision.CodeDelayed {
				if m, ok := rj.Params["remainingMinutes"].(int64); ok && m > 0 {
					retry = now.Add(time.Duration(m)*time.Minute + time.Minute)
				}
			}
		}
		cand, _ := json.Marshal(d.Candidate)
		_, err := s.DB.W.ExecContext(ctx, `INSERT INTO pending_releases (item_ref, guid, indexer_id, title, candidate, reason, first_seen, retry_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (item_ref, guid, indexer_id) DO UPDATE SET candidate = excluded.candidate,
			reason = excluded.reason, retry_at = excluded.retry_at`,
			ref, d.Candidate.GUID, d.Candidate.IndexerID, d.Candidate.Title, string(cand), strings.Join(reasons, "; "), db.Millis(now), db.Millis(retry))
		if err != nil {
			s.Log.Warn("pending release", "err", err)
			continue
		}
		s.record(ctx, ref, timeline.TypePending, fmt.Sprintf("holding %s: %s", d.Candidate.Title, strings.Join(reasons, "; ")),
			map[string]any{"title": d.Candidate.Title, "retryAt": retry.UTC(), "reasons": d.Rejections})
	}
}

// RetryPending re-evaluates held-back releases that are due and grabs the
// best one that passes now.
func (s *Service) RetryPending(ctx context.Context) (int, error) {
	now := s.now()
	_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM pending_releases WHERE first_seen < ?`, db.Millis(now.Add(-pendingExpire)))
	rows, err := s.DB.R.QueryContext(ctx, `SELECT DISTINCT item_ref FROM pending_releases WHERE retry_at <= ?`, db.Millis(now))
	if err != nil {
		return 0, err
	}
	var refs []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			_ = rows.Close()
			return 0, err
		}
		refs = append(refs, r)
	}
	_ = rows.Close()
	grabbed := 0
	for _, rs := range refs {
		ref, err := library.ParseRef(rs)
		if err != nil {
			continue
		}
		ok, err := s.retryItem(ctx, ref)
		if err != nil {
			s.Log.Warn("pending releases", "item", rs, "err", err)
			continue
		}
		if ok {
			grabbed++
		}
	}
	return grabbed, nil
}

func (s *Service) retryItem(ctx context.Context, ref library.Ref) (bool, error) {
	unlock := s.lock(ref.String())
	defer unlock()
	_, it, err := s.item(ctx, ref)
	if err != nil {
		_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM pending_releases WHERE item_ref = ?`, ref.String())
		return false, err
	}
	if !it.Monitored || (it.HasFile && it.CutoffMet) || it.Busy || s.activeDownload(ctx, ref.String()) {
		_, err := s.DB.W.ExecContext(ctx, `DELETE FROM pending_releases WHERE item_ref = ?`, ref.String())
		return false, err
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT candidate FROM pending_releases WHERE item_ref = ?`, ref.String())
	if err != nil {
		return false, err
	}
	var releases []indexers.Release
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			_ = rows.Close()
			return false, err
		}
		var c decision.Candidate
		if json.Unmarshal([]byte(raw), &c) != nil {
			continue
		}
		releases = append(releases, fromCandidate(c))
	}
	_ = rows.Close()
	ev, err := s.evaluate(ctx, it, decision.ModeSearch, releases)
	if err != nil {
		return false, err
	}
	// Rejected ones go; pending ones are stored again with a new retry time.
	_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM pending_releases WHERE item_ref = ?`, ref.String())
	if ev.Report.Accepted > 0 {
		s.record(ctx, ref.String(), timeline.TypeDecision, decisionMessage(timeline.Summarize(ev.Report), "pending"),
			map[string]any{"summary": timeline.Summarize(ev.Report), "reason": "pending"})
	}
	d, _, err := s.act(ctx, it, &ev)
	return d != nil, err
}

// fromCandidate rebuilds enough of a release to grab it again.
func fromCandidate(c decision.Candidate) indexers.Release {
	r := indexers.Release{GUID: c.GUID, Title: c.Title, IndexerID: c.IndexerID, IndexerName: c.IndexerName, IndexerPriority: c.IndexerPriority,
		Protocol: indexers.Protocol(c.Protocol), Size: c.Size, Seeders: c.Seeders, Peers: c.Peers, PublishDate: c.PublishDate,
		InfoURL: c.InfoURL, InfoHash: c.InfoHash, IMDBID: c.IDs.IMDb, TMDBID: int64(c.IDs.TMDb), TVDBID: int64(c.IDs.TVDb)}
	if strings.HasPrefix(strings.ToLower(c.DownloadURL), "magnet:") {
		r.MagnetURL = c.DownloadURL
	} else {
		r.DownloadURL = c.DownloadURL
	}
	for _, f := range c.IndexerFlags {
		r.Flags = append(r.Flags, strings.ReplaceAll(f, "_", ""))
	}
	return r
}
