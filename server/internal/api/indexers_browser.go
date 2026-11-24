package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
)

// GetIndexerBrowser implements GET /indexers/browser.
func (h *Handler) GetIndexerBrowser(ctx context.Context) (*gen.IndexerBrowserStatus, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	st := h.Indexers.BrowserStatus(ctx)
	out := &gen.IndexerBrowserStatus{State: gen.IndexerBrowserStatusState(st.State), URL: httpx.ShortURL(st.URL),
		Command: httpx.BrowserCommand, Message: optStr(st.Message), Version: optStr(st.Version), Chromium: optStr(st.Chromium)}
	if st.State == "connected" {
		out.Running = gen.NewOptBool(st.Running)
		out.Sandbox = gen.NewOptBool(st.Sandbox)
		out.Tabs = gen.NewOptInt(st.Tabs)
		out.TabsInUse = gen.NewOptInt(st.TabsInUse)
		out.Solves = gen.NewOptInt(st.Solves)
		out.Restarts = gen.NewOptInt(st.Restarts)
	}
	if ls := st.LastSolve; ls != nil {
		out.LastSolve = gen.NewOptIndexerBrowserSolve(gen.IndexerBrowserSolve{At: ls.At.UTC(), Host: ls.Host, Outcome: ls.Outcome,
			Message: optStr(ls.Message), ElapsedMs: ls.Elapsed.Milliseconds()})
	}
	return out, nil
}

// TestIndexerBrowser implements POST /indexers/browser/test.
func (h *Handler) TestIndexerBrowser(ctx context.Context, req *gen.IndexerBrowserTestRequest) (*gen.IndexerBrowserTestResult, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	variant := ""
	if v, ok := req.Variant.Get(); ok && v == gen.IndexerBrowserTestRequestVariantHuman {
		variant = "human"
	}
	r, err := h.Indexers.BrowserTest(ctx, req.URL.Or(""), variant)
	if err != nil {
		var ce *httpx.ChallengeError
		switch {
		case errors.As(err, &ce):
			return &gen.IndexerBrowserTestResult{Passed: false, Outcome: ce.Reason, Message: optStr(ce.Message), Cookies: 0}, nil
		case errors.Is(err, httpx.ErrBlocked):
			return nil, &StatusError{Status: http.StatusBadRequest, Code: "blocked", Message: err.Error()}
		}
		return nil, err
	}
	return &gen.IndexerBrowserTestResult{Passed: r.Passed, Outcome: r.Outcome, Message: optStr(r.Message), Title: optStr(r.Title),
		Challenged: r.Challenged, Cookies: r.Cookies, ElapsedMs: r.Elapsed.Milliseconds()}, nil
}
