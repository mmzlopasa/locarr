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

	mu      sync.Mutex
	sources map[string]Source
	locks   map[string]*sync.Mutex
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
	return ev, nil
}

// Outcome is what a search did.
type Outcome struct {
	Searched bool
	Found    int
	Accepted int
	Pending  int
	Grabbed  *downloads.Download
	Skipped  string // why nothing was searched
}

func (s *Service) activeDownload(ctx context.Context, ref string) bool {
	var n int
	_ = s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE json_extract(item_ref, '$.ref') = ? AND status IN ('active', 'completed')`, ref).Scan(&n)
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
	case s.activeDownload(ctx, ref.String()):
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
	d, err := s.act(ctx, it, &ev)
	if err != nil {
		return out, err
	}
	out.Grabbed = d
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

// act grabs the best accepted release (trying the next on failure) and keeps
// pending ones for later.
func (s *Service) act(ctx context.Context, it Item, ev *Evaluation) (*downloads.Download, error) {
	ref := it.Ref.String()
	for i := range ev.Report.Decisions {
		d := &ev.Report.Decisions[i]
		if !d.Accepted() {
			break
		}
		r, ok := ev.Release(d)
		if !ok {
			continue
		}
		dl, err := s.grab(ctx, it, &r, d)
		if err != nil {
			if errors.Is(err, downloads.ErrNoClient) {
				s.record(ctx, ref, timeline.TypeGrabFailed, "no download client takes this release: "+err.Error(),
					map[string]any{"title": r.Title, "indexer": r.IndexerName, "error": err.Error()})
				return nil, nil
			}
			s.record(ctx, ref, timeline.TypeGrabFailed, fmt.Sprintf("could not grab %s: %v; trying the next release", r.Title, err),
				map[string]any{"title": r.Title, "indexer": r.IndexerName, "error": err.Error()})
			continue
		}
		// A grab supersedes anything held back.
		_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM pending_releases WHERE item_ref = ?`, ref)
		return &dl, nil
	}
	s.keepPending(ctx, it, ev)
	return nil, nil
}

func (s *Service) grab(ctx context.Context, it Item, r *indexers.Release, d *decision.Decision) (downloads.Download, error) {
	link := r.DownloadURL
	if link == "" {
		link = r.MagnetURL
	}
	id := r.IndexerID
	req := downloads.GrabRequest{DownloadURL: link, Protocol: downloaders.Protocol(r.Protocol), Title: r.Title, IndexerID: &id,
		Indexer: r.IndexerName, GUID: r.GUID, ItemRef: it.Ref.JSON(), MediaKind: it.MediaKind}
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
	_ = d
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
	if !it.Monitored || (it.HasFile && it.CutoffMet) || s.activeDownload(ctx, ref.String()) {
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
	d, err := s.act(ctx, it, &ev)
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
