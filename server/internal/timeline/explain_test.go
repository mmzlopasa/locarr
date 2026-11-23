package timeline

import (
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/decision"
)

func TestExplainRules(t *testing.T) {
	soon := time.Now().Add(48 * time.Hour)
	ok := Facts{Indexers: 1, Clients: 1}
	rejected := ok
	rejected.Searches = 3
	rejected.LastDecision = &DecisionSummary{Candidates: 4, Rejected: 4, Reasons: []ReasonCount{{Code: "size_too_large", Count: 3, Example: "too big"}}}
	cases := []struct {
		name string
		st   ItemState
		f    Facts
		want string
	}{
		{"in the library", ItemState{Monitored: true, Available: true, HasFile: true, CutoffMet: true}, ok, ExplainAvailable},
		{"downloading wins over monitoring", ItemState{}, Facts{Download: &DownloadFact{Status: "active", Progress: 0.4}}, ExplainDownloading},
		{"import failed", ItemState{Monitored: true}, Facts{Download: &DownloadFact{Status: "completed"}, ImportFailed: "no video file"}, ExplainImportFailed},
		{"importing", ItemState{Monitored: true}, Facts{Download: &DownloadFact{Status: "completed"}}, ExplainImporting},
		{"not monitored", ItemState{Available: true}, ok, ExplainNotMonitored},
		{"not available", ItemState{Monitored: true, AvailableFrom: &soon}, ok, ExplainNotAvailable},
		{"no indexers", ItemState{Monitored: true, Available: true}, Facts{Clients: 1}, ExplainNoIndexers},
		{"no clients", ItemState{Monitored: true, Available: true}, Facts{Indexers: 2}, ExplainNoClients},
		{"delay", ItemState{Monitored: true, Available: true}, Facts{Indexers: 1, Clients: 1, Pending: &PendingFact{Title: "x", RetryAt: soon}}, ExplainDelayed},
		{"never searched", ItemState{Monitored: true, Available: true}, ok, ExplainNotSearched},
		{"all rejected", ItemState{Monitored: true, Available: true}, rejected, ExplainAllRejected},
		{"upgrade wanted", ItemState{Monitored: true, Available: true, HasFile: true}, rejected, ExplainUpgradeWanted},
		{"nothing found", ItemState{Monitored: true, Available: true}, Facts{Indexers: 1, Clients: 1, Searches: 2,
			LastDecision: &DecisionSummary{}}, ExplainNothingFound},
		{"accepted but grab failed", ItemState{Monitored: true, Available: true}, Facts{Indexers: 1, Clients: 1, Searches: 1,
			LastDecision: &DecisionSummary{Candidates: 116, Accepted: 31}}, ExplainGrabFailed},
	}
	for _, c := range cases {
		if got := Explain(c.st, c.f); got.Code != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, got.Code, got.Message, c.want)
		}
	}
	e := Explain(ItemState{Monitored: true, Available: true}, rejected)
	if len(e.Reasons) != 1 || e.Reasons[0].Code != "size_too_large" || e.Params["searches"] != 3 {
		t.Fatalf("%+v", e)
	}
}

func TestExplainGrabFailed(t *testing.T) {
	until := time.Date(2026, 11, 23, 12, 53, 0, 0, time.UTC)
	f := Facts{Indexers: 1, Clients: 1, Searches: 1, LastDecision: &DecisionSummary{Candidates: 116, Accepted: 31},
		GrabFailure: &GrabFailureFact{Title: "Film.2024.1080p", Indexer: "1337x", Code: "indexer_disabled",
			Error: "indexer error 429 (rate_limit): Indexer is disabled till 11/23/2026 12:53:00 due to recent failures", Until: &until}}
	e := Explain(ItemState{Monitored: true, Available: true}, f)
	if e.Code != ExplainGrabFailed || e.Params["accepted"] != 31 || e.Params["candidates"] != 116 || e.Params["failCode"] != "indexer_disabled" ||
		e.Params["until"] != until || !strings.Contains(e.Message, "31 releases accepted, but grabbing failed") {
		t.Fatalf("%+v", e)
	}
	if strings.Contains(e.Message, "no indexer had a release") {
		t.Fatal(e.Message)
	}
}

func TestSummarize(t *testing.T) {
	rep := decision.Report{Mode: decision.ModeSearch, ProfileName: "HD", Accepted: 1, Rejected: 2, Decisions: []decision.Decision{
		{Candidate: decision.Candidate{Title: "Good"}, Status: decision.StatusAccepted, Score: 5},
		{Candidate: decision.Candidate{Title: "Big"}, Status: decision.StatusRejected, Rejections: []decision.Rejection{{Code: "size_too_large"}, {Code: "blocklisted"}}},
		{Candidate: decision.Candidate{Title: "Big2"}, Status: decision.StatusRejected, Rejections: []decision.Rejection{{Code: "size_too_large"}}},
	}}
	s := Summarize(rep)
	if s.Best != "Good" || s.Candidates != 3 || len(s.TopRejects) != 2 || s.Reasons[0].Code != "size_too_large" || s.Reasons[0].Count != 2 {
		t.Fatalf("%+v", s)
	}
}
