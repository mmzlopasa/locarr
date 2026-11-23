package timeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/decision"
)

// ReasonCount is how many releases one rejection code stopped.
type ReasonCount struct {
	Code    string `json:"code"`
	Count   int    `json:"count"`
	Example string `json:"example"`
}

// RejectedRelease is one of the best rejected releases with its reasons.
type RejectedRelease struct {
	Title   string               `json:"title"`
	Indexer string               `json:"indexer"`
	Quality string               `json:"quality"`
	Status  string               `json:"status"`
	Reasons []decision.Rejection `json:"reasons"`
}

// DecisionSummary is the data of a decision event.
type DecisionSummary struct {
	Mode       string            `json:"mode"`
	Profile    string            `json:"profile"`
	Candidates int               `json:"candidates"`
	Accepted   int               `json:"accepted"`
	Pending    int               `json:"pending"`
	Rejected   int               `json:"rejected"`
	Best       string            `json:"best,omitempty"`
	BestScore  int               `json:"bestScore,omitempty"`
	Reasons    []ReasonCount     `json:"reasons"`
	TopRejects []RejectedRelease `json:"topRejected"`
}

// TopN is how many rejected releases a decision event keeps.
const TopN = 10

// Summarize turns a decision report into a decision event's data.
func Summarize(rep decision.Report) DecisionSummary {
	s := DecisionSummary{Mode: string(rep.Mode), Profile: rep.ProfileName, Candidates: len(rep.Decisions), Accepted: rep.Accepted,
		Pending: rep.Pending, Rejected: rep.Rejected, Reasons: []ReasonCount{}, TopRejects: []RejectedRelease{}}
	if b := rep.Best(); b != nil {
		s.Best, s.BestScore = b.Candidate.Title, b.Score
	}
	counts := map[string]*ReasonCount{}
	for _, d := range rep.Decisions {
		if d.Status == decision.StatusAccepted {
			continue
		}
		for _, r := range d.Rejections {
			rc := counts[r.Code]
			if rc == nil {
				rc = &ReasonCount{Code: r.Code, Example: r.Message}
				counts[r.Code] = rc
			}
			rc.Count++
		}
		if len(s.TopRejects) < TopN {
			s.TopRejects = append(s.TopRejects, RejectedRelease{Title: d.Candidate.Title, Indexer: d.Candidate.IndexerName,
				Quality: d.Quality.Name, Status: d.Status, Reasons: d.Rejections})
		}
	}
	for _, rc := range counts {
		s.Reasons = append(s.Reasons, *rc)
	}
	sort.Slice(s.Reasons, func(i, j int) bool {
		if s.Reasons[i].Count != s.Reasons[j].Count {
			return s.Reasons[i].Count > s.Reasons[j].Count
		}
		return s.Reasons[i].Code < s.Reasons[j].Code
	})
	return s
}

// ItemState is what the media kind knows about an item.
type ItemState struct {
	Ref       string
	Monitored bool
	// Available: the item reached its minimum availability.
	Available     bool
	AvailableFrom *time.Time
	HasFile       bool
	CutoffMet     bool
}

// DownloadFact is the item's latest download.
type DownloadFact struct {
	ID        int64
	Title     string
	Status    string // active, completed, imported, failed, removed
	State     string
	Progress  float64
	Message   string
	UpdatedAt time.Time
}

// PendingFact is a release held back.
type PendingFact struct {
	Title   string
	Reason  string
	RetryAt time.Time
}

// GrabFailureFact is a failed grab (timeline data of a grab_failed event).
type GrabFailureFact struct {
	Title   string     `json:"title"`
	Indexer string     `json:"indexer"`
	Code    string     `json:"code"`
	Error   string     `json:"error"`
	Until   *time.Time `json:"until,omitempty"`
}

// Facts are what the database says about an item's way to the library.
type Facts struct {
	Download     *DownloadFact
	ImportFailed string // last import failure message after the latest download completed
	Pending      *PendingFact
	Searches     int
	LastSearch   *time.Time
	NextSearch   *time.Time
	LastDecision *DecisionSummary
	// GrabFailure is the latest failed grab since the last decision.
	GrabFailure *GrabFailureFact
	// GrabbedSince: a release was sent to a client after the last decision.
	GrabbedSince bool
	// Indexers usable for automatic search; Clients enabled download clients.
	Indexers int
	Clients  int
	// IndexerErrors are the indexers that failed in the item's last search
	// (site down, Cloudflare challenge, layout changed, rate limited, …).
	IndexerErrors []IndexerError
}

// IndexerError is one indexer that failed in a search.
type IndexerError struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

// Explanation is the one answer to "why isn't it here?".
type Explanation struct {
	Code    string         `json:"code"`
	Tone    string         `json:"tone"` // success, info, warning, danger
	Message string         `json:"message"`
	Params  map[string]any `json:"params"`
	// Reasons are the most common rejection reasons of the last search.
	Reasons []ReasonCount `json:"reasons"`
}

// Explanation codes (the web client translates them).
const (
	ExplainAvailable      = "available"
	ExplainDownloading    = "downloading"
	ExplainImporting      = "importing"
	ExplainImportFailed   = "import_failed"
	ExplainNotMonitored   = "not_monitored"
	ExplainNotAvailable   = "not_available_yet"
	ExplainNoIndexers     = "no_indexers"
	ExplainNoClients      = "no_download_client"
	ExplainDelayed        = "waiting_delay"
	ExplainNothingFound   = "nothing_found"
	ExplainAllRejected    = "all_rejected"
	ExplainNotSearched    = "not_searched_yet"
	ExplainUpgradeWanted  = "upgrade_wanted"
	ExplainDownloadFailed = "download_failed"
	ExplainGrabFailed     = "grab_failed"
)

func fmtTime(t *time.Time) string {
	if t == nil {
		return "unknown"
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// Explain derives the current blocking reason. Rules run in order; the first
// that applies wins.
func Explain(st ItemState, f Facts) Explanation {
	e := Explanation{Params: map[string]any{"searches": f.Searches}, Reasons: []ReasonCount{}}
	if f.LastSearch != nil {
		e.Params["lastSearch"] = f.LastSearch.UTC()
	}
	if f.NextSearch != nil {
		e.Params["nextSearch"] = f.NextSearch.UTC()
	}
	if f.LastDecision != nil && f.LastDecision.Reasons != nil {
		e.Reasons = f.LastDecision.Reasons
		if len(e.Reasons) > 3 {
			e.Reasons = e.Reasons[:3]
		}
	}
	set := func(code, tone, msg string) Explanation {
		e.Code, e.Tone, e.Message = code, tone, msg
		return e
	}
	d := f.Download
	switch {
	case st.HasFile && st.CutoffMet:
		return set(ExplainAvailable, "success", "in the library and the quality profile's cutoff is met")
	case d != nil && d.Status == "active":
		e.Params["progress"], e.Params["title"], e.Params["state"] = d.Progress, d.Title, d.State
		msg := fmt.Sprintf("downloading %s: %.0f%% (%s)", d.Title, d.Progress*100, d.State)
		if d.Message != "" {
			e.Params["detail"] = d.Message
			msg += ": " + d.Message
		}
		return set(ExplainDownloading, "info", msg)
	case d != nil && d.Status == "completed" && f.ImportFailed != "":
		e.Params["title"], e.Params["detail"] = d.Title, f.ImportFailed
		return set(ExplainImportFailed, "danger", "the download finished but the import failed: "+f.ImportFailed)
	case d != nil && d.Status == "completed":
		e.Params["title"] = d.Title
		return set(ExplainImporting, "info", "the download finished and is waiting to be imported")
	case !st.Monitored:
		return set(ExplainNotMonitored, "warning", "not monitored: Locarr does not search for it")
	case !st.Available:
		if st.AvailableFrom != nil {
			e.Params["availableFrom"] = st.AvailableFrom.UTC()
		}
		return set(ExplainNotAvailable, "info", "not available yet (minimum availability): searching starts "+fmtTime(st.AvailableFrom))
	case f.Indexers == 0:
		return set(ExplainNoIndexers, "danger", "no enabled indexer is used for automatic searches of this kind")
	case f.Clients == 0:
		return set(ExplainNoClients, "danger", "no download client is enabled")
	case f.Pending != nil:
		e.Params["title"], e.Params["retryAt"], e.Params["detail"] = f.Pending.Title, f.Pending.RetryAt.UTC(), f.Pending.Reason
		return set(ExplainDelayed, "info", fmt.Sprintf("found %s, held back until %s: %s", f.Pending.Title, fmtTime(&f.Pending.RetryAt), f.Pending.Reason))
	case d != nil && d.Status == "failed" && f.Searches == 0:
		e.Params["title"], e.Params["detail"] = d.Title, d.Message
		return set(ExplainDownloadFailed, "danger", "the last download failed: "+d.Message)
	case f.Searches == 0:
		return set(ExplainNotSearched, "info", "not searched yet; the next automatic search is at "+fmtTime(f.NextSearch))
	case f.LastDecision != nil && f.LastDecision.Candidates > 0 && f.LastDecision.Accepted == 0:
		e.Params["candidates"] = f.LastDecision.Candidates
		msg := fmt.Sprintf("searched %d times; the last search found %d releases and every one was rejected", f.Searches, f.LastDecision.Candidates)
		if len(e.Reasons) > 0 {
			msg += " (mostly: " + e.Reasons[0].Example + ")"
		}
		if st.HasFile {
			return set(ExplainUpgradeWanted, "info", "in the library below the cutoff; "+msg)
		}
		return set(ExplainAllRejected, "warning", msg)
	case f.LastDecision != nil && f.LastDecision.Accepted > 0 && !f.GrabbedSince && (f.GrabFailure != nil || !st.HasFile):
		e.Params["candidates"], e.Params["accepted"] = f.LastDecision.Candidates, f.LastDecision.Accepted
		msg := fmt.Sprintf("the last search found %d releases and accepted %d, but nothing was grabbed", f.LastDecision.Candidates, f.LastDecision.Accepted)
		if g := f.GrabFailure; g != nil {
			e.Params["failCode"], e.Params["detail"], e.Params["indexer"], e.Params["title"] = g.Code, g.Error, g.Indexer, g.Title
			if g.Until != nil {
				e.Params["until"] = g.Until.UTC()
			}
			if g.Error != "" {
				msg = fmt.Sprintf("%d releases accepted, but grabbing failed: %s", f.LastDecision.Accepted, g.Error)
			}
		}
		return set(ExplainGrabFailed, "danger", msg)
	case st.HasFile:
		return set(ExplainUpgradeWanted, "info", "in the library below the quality profile's cutoff; Locarr keeps looking for an upgrade")
	default:
		msg := fmt.Sprintf("searched %d times and no indexer had a release; next search %s", f.Searches, fmtTime(f.NextSearch))
		if len(f.IndexerErrors) > 0 {
			e.Params["indexerErrors"] = f.IndexerErrors
			names := make([]string, len(f.IndexerErrors))
			for i, x := range f.IndexerErrors {
				names[i] = x.Name + " (" + x.Code + ")"
			}
			msg += "; failing in the last search: " + strings.Join(names, ", ")
		}
		return set(ExplainNothingFound, "warning", msg)
	}
}

// Facts loads what the database knows about ref's way to the library.
// kindCategory is the indexers.categories key ("movies", "tv", "music").
func (s *Store) Facts(ctx context.Context, ref, kindCategory string) (Facts, error) {
	return s.FactsRefs(ctx, []string{ref}, kindCategory)
}

// FactsRefs is Facts for an item seen through several refs: the first is the
// item (its downloads, including season packs that cover it), the others are
// related items whose searches and decisions also concern it (an episode's
// season). Searches add up; the latest decision of any ref counts.
func (s *Store) FactsRefs(ctx context.Context, refs []string, kindCategory string) (Facts, error) {
	return s.FactsCounted(ctx, refs, kindCategory, nil)
}

// Counts are the indexers usable for automatic search of one kind and the
// enabled download clients.
type Counts struct{ Indexers, Clients int }

// Counts loads Counts for kindCategory ("movies", "tv", "music").
func (s *Store) Counts(ctx context.Context, kindCategory string) (Counts, error) {
	var c Counts
	// An indexer counts when it is enabled for automatic search and offers
	// categories of this kind (an empty list means "the standard ones").
	if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM indexers WHERE enabled = 1 AND enable_automatic = 1
		AND (json_extract(categories, '$.`+kindCategory+`') IS NULL OR json_array_length(json_extract(categories, '$.`+kindCategory+`')) > 0)`).
		Scan(&c.Indexers); err != nil { // #nosec G202 -- kindCategory is a constant from the caller
		return c, err
	}
	err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_clients WHERE enabled = 1`).Scan(&c.Clients)
	return c, err
}

// FactsCounted is FactsRefs with the indexer and client counts already
// loaded (nil = load them), for callers that explain many items at once.
func (s *Store) FactsCounted(ctx context.Context, refs []string, kindCategory string, counts *Counts) (Facts, error) {
	var f Facts
	if len(refs) == 0 {
		return f, nil
	}
	ref := refs[0]
	var (
		d       DownloadFact
		updated int64
	)
	err := s.DB.R.QueryRowContext(ctx, `SELECT id, title, status, state, progress, message, updated_at FROM downloads
		WHERE id IN (SELECT download_id FROM download_refs WHERE item_ref = ?) ORDER BY added_at DESC, id DESC LIMIT 1`, ref).
		Scan(&d.ID, &d.Title, &d.Status, &d.State, &d.Progress, &d.Message, &updated)
	switch {
	case err == nil:
		d.UpdatedAt = db.Time(updated)
		f.Download = &d
		if d.Status == "completed" {
			var msg string
			if err := s.DB.R.QueryRowContext(ctx, `SELECT message FROM download_events WHERE download_id = ? AND type = 'import_failed'
				ORDER BY at DESC, id DESC LIMIT 1`, d.ID).Scan(&msg); err == nil {
				f.ImportFailed = msg
			}
		}
	case !errors.Is(err, sql.ErrNoRows):
		return f, err
	}
	marks, args := placeholders(refs)
	var p PendingFact
	var retry int64
	err = s.DB.R.QueryRowContext(ctx, `SELECT title, reason, retry_at FROM pending_releases WHERE item_ref IN (`+marks+`) ORDER BY retry_at LIMIT 1`, args...). // #nosec G202 -- placeholders only
																					Scan(&p.Title, &p.Reason, &retry)
	if err == nil {
		p.RetryAt = db.Time(retry)
		f.Pending = &p
	} else if !errors.Is(err, sql.ErrNoRows) {
		return f, err
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT searches, last_search_at, next_search_at FROM search_state WHERE item_ref IN (`+marks+`)`, args...) // #nosec G202 -- placeholders only
	if err != nil {
		return f, err
	}
	for rows.Next() {
		var (
			n          int
			last, next sql.NullInt64
		)
		if err := rows.Scan(&n, &last, &next); err != nil {
			_ = rows.Close()
			return f, err
		}
		f.Searches += n
		if t := db.NullTime(last); t != nil && (f.LastSearch == nil || t.After(*f.LastSearch)) {
			f.LastSearch = t
		}
		if t := db.NullTime(next); t != nil && (f.NextSearch == nil || t.Before(*f.NextSearch)) {
			f.NextSearch = t
		}
	}
	_ = rows.Close()
	var (
		data       string
		decisionAt int64
	)
	err = s.DB.R.QueryRowContext(ctx, `SELECT data, at FROM timeline_events WHERE item_ref IN (`+marks+`) AND type = ? ORDER BY at DESC, id DESC LIMIT 1`, // #nosec G202 -- placeholders only
		append(args, TypeDecision)...).Scan(&data, &decisionAt)
	if err == nil {
		var wrap struct {
			Summary DecisionSummary `json:"summary"`
		}
		if json.Unmarshal([]byte(data), &wrap) == nil {
			f.LastDecision = &wrap.Summary
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return f, err
	}
	if f.LastDecision != nil && f.LastDecision.Accepted > 0 {
		var n int
		if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE added_at >= ?
			AND id IN (SELECT download_id FROM download_refs WHERE item_ref IN (`+marks+`))`, append([]any{decisionAt}, args...)...).Scan(&n); err != nil { // #nosec G202 -- placeholders only
			return f, err
		}
		f.GrabbedSince = n > 0
	}
	if f.LastDecision != nil && f.LastDecision.Accepted > 0 && !f.GrabbedSince {
		// The failure that matters is a real attempt; "skipped" notes and the
		// "stopped after N" line only say what happened around it.
		rows, err := s.DB.R.QueryContext(ctx, `SELECT data FROM timeline_events WHERE item_ref IN (`+marks+`) AND type = ? AND at >= ?
			ORDER BY at DESC, id DESC LIMIT 20`, append(args, TypeGrabFailed, decisionAt)...) // #nosec G202 -- placeholders only
		if err != nil {
			return f, err
		}
		var skipped *GrabFailureFact
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				_ = rows.Close()
				return f, err
			}
			var g struct {
				GrabFailureFact
				Skipped bool `json:"skipped"`
			}
			if json.Unmarshal([]byte(raw), &g) != nil || g.Code == "attempts_exhausted" {
				continue
			}
			if g.Skipped {
				if skipped == nil {
					skipped = &g.GrabFailureFact
				}
				continue
			}
			f.GrabFailure = &g.GrabFailureFact
			break
		}
		_ = rows.Close()
		if f.GrabFailure == nil {
			f.GrabFailure = skipped
		}
	}
	if counts == nil {
		c, err := s.Counts(ctx, kindCategory)
		if err != nil {
			return f, err
		}
		counts = &c
	}
	f.Indexers, f.Clients = counts.Indexers, counts.Clients
	if f.Searches > 0 {
		errs, err := s.indexerErrors(ctx, marks, args)
		if err != nil {
			return f, err
		}
		f.IndexerErrors = errs
	}
	return f, nil
}

// indexerErrors lists the indexers that failed in the latest search of the refs.
func (s *Store) indexerErrors(ctx context.Context, marks string, args []any) ([]IndexerError, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT indexer_name, CASE WHEN status = 'backoff' THEN 'backoff' ELSE COALESCE(error_code, status) END FROM search_log_indexers
		WHERE search_id = (SELECT id FROM search_log WHERE item_ref IN (`+marks+`) ORDER BY started_at DESC, id DESC LIMIT 1)
		AND status IN ('error', 'rate_limited', 'backoff') ORDER BY indexer_name`, args...) // #nosec G202 -- placeholders only
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IndexerError
	for rows.Next() {
		var e IndexerError
		if err := rows.Scan(&e.Name, &e.Code); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
