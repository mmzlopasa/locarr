package wanted

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// GrabAttempts is how many releases one search cycle tries to grab for an
// item before it gives up until the next cycle (failures only; a release
// skipped because its indexer is limited does not count).
var GrabAttempts = settings.Key[int]{Name: "wanted.grabAttempts", Default: 3}

// defaultGrabGap is the least time between two grabs from one indexer.
const defaultGrabGap = 2 * time.Second

// Grab failure codes (timeline data; the web client translates them).
const (
	GrabIndexerLimited     = "indexer_limited"     // rate limited (429 or a limit error)
	GrabIndexerDisabled    = "indexer_disabled"    // disabled by the indexer manager or the account
	GrabIndexerError       = "indexer_error"       // the indexer failed (5xx, "download failed")
	GrabIndexerUnreachable = "indexer_unreachable" // network error or timeout
	GrabIndexerAuth        = "indexer_auth"        // key refused
	GrabLimitReached       = "grab_limit"          // the indexer's daily grab limit
	GrabReleaseGone        = "release_gone"        // 404 for this release
	GrabBadFile            = "bad_file"            // not a torrent / NZB
	GrabBlockedAddress     = "blocked_address"     // a redirect pointed at a refused address
	GrabBadRedirect        = "bad_redirect"        // redirect loop or unusable redirect
	GrabNoClient           = "no_client"
	GrabAlreadyQueued      = "already_queued"
	GrabOther              = "other"
)

// grabFailure is what a failed grab means for the rest of the cycle.
type grabFailure struct {
	Code string
	// IndexerProblem: skip the indexer's other releases for the rest of the cycle.
	IndexerProblem bool
	// Content: the release itself is bad; blocklist it.
	Content bool
	// Until is when the indexer takes requests again, when known.
	Until *time.Time
}

// classifyGrab sorts a grab error into indexer trouble, a bad release or other.
func classifyGrab(err error, now time.Time) grabFailure {
	f := grabFailure{Code: GrabOther}
	if t, ok := indexers.LimitedUntil(err, now); ok {
		f.Until = &t
	}
	switch {
	case errors.Is(err, downloads.ErrNoClient):
		f.Code = GrabNoClient
		return f
	case errors.Is(err, downloads.ErrAlreadyQueued):
		f.Code = GrabAlreadyQueued
		return f
	case errors.Is(err, indexers.ErrGrabLimit):
		f.Code, f.IndexerProblem = GrabLimitReached, true
		return f
	case errors.Is(err, downloads.ErrBadTorrent), errors.Is(err, downloads.ErrNotRelease):
		f.Code, f.Content = GrabBadFile, true
		return f
	case errors.Is(err, downloads.ErrReleaseGone):
		f.Code, f.Content = GrabReleaseGone, true
		return f
	case errors.Is(err, httpx.ErrBlocked):
		f.Code = GrabBlockedAddress
		return f
	}
	var ze *znab.Error
	if !errors.As(err, &ze) {
		return f
	}
	msg := strings.ToLower(ze.Message)
	switch ze.Kind {
	case znab.KindRateLimit:
		f.IndexerProblem = true
		switch {
		case strings.Contains(msg, "disabled"):
			f.Code = GrabIndexerDisabled
		case ze.Code >= 500 && ze.Code < 600 && ze.Code != 429:
			f.Code = GrabIndexerError
		default:
			f.Code = GrabIndexerLimited
		}
	case znab.KindDisabled:
		f.Code, f.IndexerProblem = GrabIndexerDisabled, true
	case znab.KindUnreachable:
		f.Code, f.IndexerProblem = GrabIndexerUnreachable, true
		if ze.Code >= 500 {
			f.Code = GrabIndexerError
		}
	case znab.KindAuth:
		f.Code, f.IndexerProblem = GrabIndexerAuth, true
	case znab.KindNotFound:
		f.Code, f.Content = GrabReleaseGone, true
	case znab.KindBlocked:
		f.Code = GrabBlockedAddress
	case znab.KindBadResponse:
		switch {
		case strings.Contains(msg, httpx.ErrNotReleaseFile.Error()):
			f.Code, f.Content = GrabBadFile, true
		case strings.Contains(msg, "redirect"):
			f.Code = GrabBadRedirect
		}
	}
	return f
}

// indexerResting reports an indexer that is backing off after a limit or a
// "disabled" answer; its releases are not grabbed until then.
func (s *Service) indexerResting(ctx context.Context, id int64) (bool, *time.Time) {
	ind, err := s.Indexers.Get(ctx, id)
	if err != nil || ind.Health.RetryAt == nil || !ind.Health.RetryAt.After(s.now()) {
		return false, nil
	}
	switch ind.Health.LastErrorCode {
	case string(znab.KindRateLimit), string(znab.KindDisabled):
		return true, ind.Health.RetryAt
	}
	return false, nil
}

// pace waits until the indexer's previous grab is at least the grab gap ago,
// so one cycle never fires grabs at an indexer back to back.
func (s *Service) pace(ctx context.Context, indexerID int64) error {
	gap := s.GrabGap
	if gap == 0 {
		gap = defaultGrabGap
	}
	s.mu.Lock()
	if s.lastGrab == nil {
		s.lastGrab = map[int64]time.Time{}
	}
	now := time.Now()
	next := s.lastGrab[indexerID].Add(gap)
	wait := next.Sub(now)
	if wait < 0 {
		wait = 0
	}
	s.lastGrab[indexerID] = now.Add(wait)
	s.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// blockBadRelease blocklists a release whose file is bad or gone (never one
// that failed because its indexer was limited or down).
func (s *Service) blockBadRelease(ctx context.Context, r *indexers.Release, ref library.Ref, covers []library.Ref, cause error) {
	id := r.IndexerID
	if _, err := s.Downloads.AddBlock(ctx, downloads.BlockEntry{Title: r.Title, Protocol: string(r.Protocol), IndexerID: &id,
		Indexer: r.IndexerName, InfoHash: strings.ToLower(r.InfoHash), GUID: r.GUID, Reason: downloads.ReasonBadRelease,
		Message: shortError(cause), ItemRef: ref.JSONWith(covers)}); err != nil {
		s.Log.Warn("blocklist bad release", "err", err)
	}
}

// shortError is a grab error for the timeline: secrets and query strings are
// already gone (httpx), long messages are cut.
func shortError(err error) string {
	msg := err.Error()
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return msg
}
