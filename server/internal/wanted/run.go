package wanted

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/decision"
	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

type searchArgs struct {
	Ref    string `json:"ref"`
	Reason string `json:"reason"`
}

// RegisterTasks registers the jobs and schedules.
func (s *Service) RegisterTasks(ctx context.Context) error {
	s.Jobs.Register(TaskSearch, func(ctx context.Context, j *jobs.Job) error {
		var a searchArgs
		if err := j.Decode(&a); err != nil {
			return jobs.Permanent(err)
		}
		ref, err := library.ParseRef(a.Ref)
		if err != nil {
			return jobs.Permanent(err)
		}
		out, err := s.Search(ctx, ref, a.Reason)
		if err != nil {
			return err
		}
		s.Log.Info("search done", "item", a.Ref, "reason", a.Reason, "found", out.Found, "accepted", out.Accepted,
			"grabbed", out.Grabbed != nil, "skipped", out.Skipped)
		return nil
	}, jobs.KindOptions{MaxAttempts: 2, Timeout: 5 * time.Minute})
	s.Jobs.Register(TaskSearchMissing, func(ctx context.Context, _ *jobs.Job) error {
		n, err := s.SearchDue(ctx)
		if err == nil && n > 0 {
			s.Log.Info("scheduled search", "items", n)
		}
		return err
	}, jobs.KindOptions{MaxAttempts: 1, Timeout: 30 * time.Minute})
	s.Jobs.Register(TaskPending, func(ctx context.Context, _ *jobs.Job) error {
		_, err := s.RetryPending(ctx)
		return err
	}, jobs.KindOptions{MaxAttempts: 1, Timeout: 10 * time.Minute})
	every, err := settings.Get(ctx, s.Settings, SearchIntervalMinutes)
	if err != nil {
		return err
	}
	s.Jobs.Schedule(TaskSearchMissing, TaskSearchMissing, time.Duration(max(every, 5))*time.Minute)
	s.Jobs.Schedule(TaskPending, TaskPending, 5*time.Minute)
	return nil
}

// Enqueue schedules a search for one item (deduplicated while queued).
func (s *Service) Enqueue(ctx context.Context, ref library.Ref, reason string) error {
	_, _, err := s.Jobs.Enqueue(ctx, TaskSearch, searchArgs{Ref: ref.String(), Reason: reason}, jobs.EnqueueOptions{UniqueKey: "search:" + ref.String()})
	return err
}

// SearchDue searches wanted items whose next search is due (never-searched
// first), at most SearchBatch per run.
func (s *Service) SearchDue(ctx context.Context) (int, error) {
	batch, err := settings.Get(ctx, s.Settings, SearchBatch)
	if err != nil {
		return 0, err
	}
	now := s.now()
	type due struct {
		it   Item
		next int64
	}
	var list []due
	s.mu.Lock()
	sources := make([]Source, 0, len(s.sources))
	for _, src := range s.sources {
		sources = append(sources, src)
	}
	s.mu.Unlock()
	for _, src := range sources {
		items, err := src.Wanted(ctx)
		if err != nil {
			return 0, err
		}
		for _, it := range items {
			var next *int64
			_ = s.DB.R.QueryRowContext(ctx, `SELECT next_search_at FROM search_state WHERE item_ref = ?`, it.Ref.String()).Scan(&next)
			n := int64(0)
			if next != nil {
				n = *next
			}
			if n <= db.Millis(now) {
				list = append(list, due{it, n})
			}
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].next < list[j].next })
	done := 0
	for _, d := range list {
		if done >= max(batch, 1) || ctx.Err() != nil {
			break
		}
		if _, err := s.Search(ctx, d.it.Ref, "scheduled"); err != nil {
			s.Log.Warn("scheduled search", "item", d.it.Ref.String(), "err", err)
			continue
		}
		done++
	}
	return done, nil
}

// RunRSS consumes new RSS releases until ctx ends.
func (s *Service) RunRSS(ctx context.Context) {
	ch, unsubscribe := s.Indexers.SubscribeRSS(16)
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case batch, ok := <-ch:
			if !ok {
				return
			}
			if n, err := s.ProcessRSS(ctx, batch); err != nil {
				s.Log.Warn("rss releases", "err", err)
			} else if n > 0 {
				s.Log.Info("rss grabs", "grabbed", n)
			}
		}
	}
}

// ProcessRSS matches new releases to wanted items, decides and grabs.
func (s *Service) ProcessRSS(ctx context.Context, batch []indexers.Release) (int, error) {
	s.mu.Lock()
	sources := make([]Source, 0, len(s.sources))
	for _, src := range s.sources {
		sources = append(sources, src)
	}
	s.mu.Unlock()
	byRef := map[library.Ref][]indexers.Release{}
	var order []library.Ref
	for _, src := range sources {
		match, err := src.Matcher(ctx)
		if err != nil {
			return 0, err
		}
		for i := range batch {
			p := parser.Parse(batch[i].Title)
			for _, ref := range match(&batch[i], &p) {
				if _, ok := byRef[ref]; !ok {
					order = append(order, ref)
				}
				byRef[ref] = append(byRef[ref], batch[i])
			}
		}
	}
	grabbed := 0
	for _, ref := range order {
		ok, err := s.rssItem(ctx, ref, byRef[ref])
		if err != nil {
			s.Log.Warn("rss item", "item", ref.String(), "err", err)
			continue
		}
		if ok {
			grabbed++
		}
	}
	return grabbed, nil
}

func (s *Service) rssItem(ctx context.Context, ref library.Ref, releases []indexers.Release) (bool, error) {
	unlock := s.lock(ref.String())
	defer unlock()
	_, it, err := s.item(ctx, ref)
	if err != nil {
		return false, err
	}
	if !it.Monitored || !it.Available || (it.HasFile && it.CutoffMet) || s.activeDownload(ctx, ref.String()) {
		return false, nil
	}
	ev, err := s.evaluate(ctx, it, decision.ModeRSS, releases)
	if err != nil {
		return false, err
	}
	if ev.Report.Accepted == 0 && ev.Report.Pending == 0 {
		return false, nil // RSS noise stays out of the timeline
	}
	sum := timeline.Summarize(ev.Report)
	s.record(ctx, ref.String(), timeline.TypeDecision, decisionMessage(sum, "rss"), map[string]any{"summary": sum, "reason": "rss"})
	d, err := s.act(ctx, it, &ev)
	return d != nil, err
}

// HandleDownloadEvent reacts to the downloads bus: a failed download is
// blocklisted and the item searched again for the next best release. It runs
// on the tracker's goroutine, so the search itself goes to the job queue.
func (s *Service) HandleDownloadEvent(ctx context.Context, ev downloads.Event) {
	if ev.Type != downloads.EventFailed {
		return
	}
	ref := library.RefFromJSON(ev.Download.ItemRef)
	if ref.IsZero() {
		return
	}
	d := ev.Download
	reason := downloads.ReasonFailed
	if ev.Reason == "stalled" {
		reason = downloads.ReasonStalled
	}
	id := d.ID
	if _, err := s.Downloads.AddBlock(ctx, downloads.BlockEntry{Title: d.Title, Protocol: string(d.Protocol), IndexerID: d.IndexerID,
		Indexer: d.Indexer, InfoHash: d.InfoHash, GUID: d.GUID, Reason: reason, Message: ev.Message, ItemRef: d.ItemRef, DownloadID: &id}); err != nil {
		s.Log.Warn("blocklist failed download", "err", err)
	}
	retry, _ := settings.Get(ctx, s.Settings, RetryFailed)
	msg := fmt.Sprintf("download %s failed (%s) and was blocklisted", d.Title, ev.Message)
	if retry {
		msg += "; searching for the next best release"
	}
	if err := s.Timeline.Record(ctx, ref.String(), timeline.TypeRetry, msg, map[string]any{"title": d.Title, "reason": ev.Reason,
		"message": ev.Message, "retry": retry}, &id); err != nil {
		s.Log.Warn("timeline", "err", err)
	}
	if retry {
		if err := s.Enqueue(ctx, ref, "retry"); err != nil {
			s.Log.Warn("retry search", "err", err)
		}
	}
}

// Interactive searches every indexer for an item and evaluates the results
// for a person to choose (nothing is grabbed). The decision is recorded.
func (s *Service) Interactive(ctx context.Context, ref library.Ref, userID int64) (Evaluation, error) {
	_, it, err := s.item(ctx, ref)
	if err != nil {
		return Evaluation{}, err
	}
	q := it.Query
	q.Source, q.ItemRef, q.UserID = indexers.SourceInteractive, ref.String(), userID
	res, err := s.Indexers.Search(ctx, q)
	if err != nil {
		return Evaluation{}, err
	}
	ev, err := s.evaluate(ctx, it, decision.ModeInteractive, res.Releases)
	if err != nil {
		return ev, err
	}
	ev.Search = res
	sum := timeline.Summarize(ev.Report)
	s.record(ctx, ref.String(), timeline.TypeDecision, decisionMessage(sum, "interactive"), map[string]any{"summary": sum, "reason": "interactive"})
	return ev, nil
}

// ManualGrab is a release a person chose in an interactive search.
type ManualGrab struct {
	IndexerID     int64
	DownloadToken string
	MagnetURL     string
	Title         string
	Protocol      string
	GUID          string
	PublishedAt   *time.Time
}

// Grab sends a chosen release for an item to a client.
func (s *Service) Grab(ctx context.Context, ref library.Ref, g ManualGrab) (downloads.Download, error) {
	_, it, err := s.item(ctx, ref)
	if err != nil {
		return downloads.Download{}, err
	}
	id := g.IndexerID
	req := downloads.GrabRequest{DownloadToken: g.DownloadToken, DownloadURL: g.MagnetURL, Protocol: downloaders.Protocol(g.Protocol),
		Title: g.Title, GUID: g.GUID, ItemRef: ref.JSON(), MediaKind: it.MediaKind, PublishedAt: g.PublishedAt}
	if id > 0 {
		req.IndexerID = &id
	}
	d, err := s.Downloads.Grab(ctx, req)
	if err != nil {
		s.record(ctx, ref.String(), timeline.TypeGrabFailed, fmt.Sprintf("could not grab %s: %v", g.Title, err), map[string]any{"title": g.Title, "error": err.Error(), "manual": true})
		return d, err
	}
	_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM pending_releases WHERE item_ref = ?`, ref.String())
	return d, nil
}

// IsUnknownKind reports ErrUnknownKind.
func IsUnknownKind(err error) bool { return errors.Is(err, ErrUnknownKind) }
