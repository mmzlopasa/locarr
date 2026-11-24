package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// Limits of one solve.
const (
	defaultTimeout = 60 * time.Second
	maxTimeout     = 120 * time.Second
	maxPageBytes   = 64 << 20 // everything one solve may load
	maxBodyBytes   = 8 << 20  // the document handed back
	pollEvery      = 500 * time.Millisecond
	// humanGrace is how long a check that shows a CAPTCHA widget may still
	// finish on its own before the solve reports that a person is needed.
	humanGrace = 12 * time.Second
)

// Cookie is a browser cookie.
type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain,omitempty"`
	Path     string  `json:"path,omitempty"`
	Expires  float64 `json:"expires,omitempty"` // Unix seconds; 0 = session cookie
	HTTPOnly bool    `json:"httpOnly,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
}

// SolveRequest asks the browser to open a page.
type SolveRequest struct {
	URL string `json:"url"`
	// UserAgent replaces the browser's own one (empty: the browser's own).
	UserAgent string   `json:"userAgent,omitempty"`
	Cookies   []Cookie `json:"cookies,omitempty"`
	// Proxy is an optional http://, https:// or socks5:// proxy (with credentials).
	Proxy     string `json:"proxy,omitempty"`
	TimeoutMs int    `json:"timeoutMs,omitempty"`
	// Allowed are addresses, networks or host names the page may reach even
	// though they are blocked by default (Locarr's allowed hosts).
	Allowed []string `json:"allowed,omitempty"`
	// TargetPrivate lets the page's own host be on a private network.
	TargetPrivate bool `json:"targetPrivate,omitempty"`
}

// Solve outcomes.
const (
	OutcomeSolved  = "solved"  // the page is the site's content
	OutcomeHuman   = "human"   // the site wants a person to solve a CAPTCHA
	OutcomeTimeout = "timeout" // the check did not finish in time
	OutcomeBlocked = "blocked" // the site refuses this client
	OutcomeRefused = "refused" // the address is not allowed
	OutcomeError   = "error"
)

// SolveResult is the answer to a SolveRequest.
type SolveResult struct {
	Outcome     string   `json:"outcome"`
	Message     string   `json:"message,omitempty"`
	URL         string   `json:"url,omitempty"` // final address
	Status      int      `json:"status,omitempty"`
	ContentType string   `json:"contentType,omitempty"`
	Title       string   `json:"title,omitempty"`
	Body        string   `json:"body,omitempty"`
	Cookies     []Cookie `json:"cookies"`
	UserAgent   string   `json:"userAgent,omitempty"`
	// Headers are the client hint headers the browser sent with that
	// User-Agent; a client reusing the cookies sends them too.
	Headers map[string]string `json:"headers,omitempty"`
	// Challenged says whether a browser check was seen on the way.
	Challenged bool  `json:"challenged"`
	ElapsedMs  int64 `json:"elapsedMs"`
}

// Solver opens pages in the pool's browser.
type Solver struct {
	Pool *Pool
}

// CheckTarget validates a page address: http(s) with a host and no user info.
func CheckTarget(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("the address must be an http:// or https:// URL")
	}
	if u.User != nil {
		return nil, errors.New("the address must not contain a user name or password")
	}
	return u, nil
}

// snapshotJS reads what the waiting loop needs from the page.
const snapshotJS = `(() => {
  const d = document, h = d.documentElement ? d.documentElement.outerHTML : '';
  const w = !!d.querySelector('iframe[src*="hcaptcha.com"],iframe[src*="recaptcha"],iframe[title*="hCaptcha"],iframe[title*="reCAPTCHA"],.h-captcha,.g-recaptcha');
  const t = d.body ? (d.body.innerText || '').slice(0, 4000) : '';
  return {text: t, title: d.title || '', html: h.length > 524288 ? h.slice(0, 524288) : h, ready: d.readyState, url: location.href, widget: w};
})()`

const fullHTMLJS = `(() => { const h = document.documentElement ? document.documentElement.outerHTML : ''; return h.length > 8388608 ? h.slice(0, 8388608) : h; })()`

type snapshot struct {
	Text   string `json:"text"`
	Title  string `json:"title"`
	HTML   string `json:"html"`
	Ready  string `json:"ready"`
	URL    string `json:"url"`
	Widget bool   `json:"widget"`
}

type mainDoc struct {
	mu        sync.Mutex
	requestID network.RequestID
	status    int
	server    string
	mitigated string
	mime      string
	url       string
	refused   string // the guard refused the document's address
}

// Solve opens req.URL and waits until the page is the site's content, a
// CAPTCHA that needs a person, a refusal, or the time is up.
func (s *Solver) Solve(ctx context.Context, req SolveRequest, pol *Policy) SolveResult {
	start := time.Now()
	res := s.solve(ctx, req, pol)
	res.ElapsedMs = time.Since(start).Milliseconds()
	if res.Cookies == nil {
		res.Cookies = []Cookie{}
	}
	return res
}

func (s *Solver) solve(ctx context.Context, req SolveRequest, pol *Policy) SolveResult {
	u, err := CheckTarget(req.URL)
	if err != nil {
		return SolveResult{Outcome: OutcomeRefused, Message: err.Error()}
	}
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	timeout = min(timeout, maxTimeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if pol == nil {
		p, err := parsePolicy(u.Hostname(), req.TargetPrivate, req.Allowed)
		if err != nil {
			return SolveResult{Outcome: OutcomeRefused, Message: err.Error()}
		}
		pol = &p
	}
	g, err := newGuard(*pol, req.Proxy, maxPageBytes)
	if err != nil {
		return SolveResult{Outcome: OutcomeError, Message: err.Error()}
	}
	defer g.close()

	waitCtx, waitCancel := context.WithTimeout(ctx, min(timeout/2, 30*time.Second))
	bctx, ua, full, release, err := s.Pool.Acquire(waitCtx)
	waitCancel()
	if err != nil {
		return SolveResult{Outcome: OutcomeError, Message: "the browser could not start: " + err.Error()}
	}
	defer release()
	own := true
	if req.UserAgent != "" && req.UserAgent != ua {
		ua, own = req.UserAgent, false
	}

	// No Runtime domain on the page: a page can notice it, and a browser a
	// person runs does not have it.
	tctx, tcancel := chromedp.NewContext(bctx, chromedp.WithRuntimeDisabled(), chromedp.WithNewBrowserContext(func(p *target.CreateBrowserContextParams) {
		p.ProxyServer = g.addr()
		p.ProxyBypassList = "<-loopback>"
	}))
	defer tcancel()
	// Stop the tab when the solve's time is up.
	go func() {
		select {
		case <-ctx.Done():
			tcancel()
		case <-tctx.Done():
		}
	}()
	if err := chromedp.Do(tctx); err != nil { // creates the browser context and the tab
		return SolveResult{Outcome: OutcomeError, Message: "could not open a tab: " + err.Error()}
	}
	c := chromedp.FromContext(tctx)
	frame := cdp.FrameID(c.Target.TargetID)

	doc := &mainDoc{}
	if err := chromedp.Listen(tctx, func(ev any) {
		if e, ok := ev.(*network.EventResponseReceived); ok && e.Type == network.ResourceTypeDocument && e.FrameID == frame {
			doc.mu.Lock()
			doc.requestID, doc.status, doc.mime, doc.url = e.RequestID, int(e.Response.Status), e.Response.MimeType, e.Response.URL
			doc.server, doc.mitigated, doc.refused = "", "", ""
			for k, v := range e.Response.Headers {
				switch strings.ToLower(k) {
				case "server":
					doc.server = strings.ToLower(fmt.Sprint(v))
				case "cf-mitigated":
					doc.mitigated = strings.ToLower(fmt.Sprint(v))
				case strings.ToLower(guardHeader):
					doc.refused = fmt.Sprint(v)
				}
			}
			doc.mu.Unlock()
		}
	}); err != nil {
		return SolveResult{Outcome: OutcomeError, Message: "could not watch the tab: " + err.Error()}
	}

	setup := func() error {
		if _, err := cdp.Call(tctx, c.Browser, browser.SetDownloadBehavior, browser.SetDownloadBehaviorParams{
			Behavior: browser.SetDownloadBehaviorBehaviorDeny, BrowserContextID: c.BrowserContextID}); err != nil {
			return err
		}
		if _, err := cdp.Call(tctx, c.Target, network.Enable, network.EnableParams{}); err != nil {
			return err
		}
		ov := emulation.SetUserAgentOverrideParams{UserAgent: ua, AcceptLanguage: "en-US,en"}
		if own {
			ov.UserAgentMetadata = uaMetadata(full)
		}
		if _, err := cdp.Call(tctx, c.Target, emulation.SetUserAgentOverride, ov); err != nil {
			return err
		}
		var cks []*network.CookieParam
		for _, ck := range req.Cookies {
			p := &network.CookieParam{Name: ck.Name, Value: ck.Value, Domain: ck.Domain, Path: ck.Path,
				HTTPOnly: ck.HTTPOnly, Secure: ck.Secure}
			if p.Domain == "" {
				p.URL = u.String()
			}
			if ck.Expires > 0 {
				p.Expires = cdp.TimeSinceEpoch(ck.Expires)
			}
			cks = append(cks, p)
		}
		if len(cks) > 0 {
			if _, err := cdp.Call(tctx, c.Target, network.SetCookies, network.SetCookiesParams{Cookies: cks}); err != nil {
				return err
			}
		}
		nav, err := cdp.Call(tctx, c.Target, page.Navigate, page.NavigateParams{URL: u.String()})
		if err != nil {
			return err
		}
		if nav.ErrorText != "" && nav.ErrorText != "net::ERR_ABORTED" {
			return errors.New(nav.ErrorText)
		}
		return nil
	}
	if err := setup(); err != nil {
		if g.refused.Load() > 0 {
			g.mu.Lock()
			msg := g.lastDeny
			g.mu.Unlock()
			return SolveResult{Outcome: OutcomeRefused, Message: msg}
		}
		if ctx.Err() != nil {
			return SolveResult{Outcome: OutcomeTimeout, Message: "the page did not load in time"}
		}
		return SolveResult{Outcome: OutcomeError, Message: "could not load the page: " + err.Error()}
	}

	var (
		challenged bool
		humanSince time.Time
		contentRun int
		blockedRun int
		snap       snapshot
	)
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			r := SolveResult{Outcome: OutcomeTimeout, Challenged: challenged, Title: snap.Title, UserAgent: ua,
				Message: "the browser check did not finish in time", Body: snap.Text}
			if !humanSince.IsZero() {
				r.Outcome, r.Message = OutcomeHuman, "the site asks for a human check (a CAPTCHA)"
			}
			return r
		case <-tick.C:
		}
		if g.overLimit() {
			return SolveResult{Outcome: OutcomeError, Message: "the page loads more than 64 MiB", Challenged: challenged}
		}
		cur, err := chromedp.Run(tctx, chromedp.Evaluate[snapshot](snapshotJS))
		if err != nil {
			continue // navigating; the next tick sees the new document
		}
		snap = cur
		if !cur.Widget {
			cur.Widget = s.checkFrame(tctx, c)
		}
		doc.mu.Lock()
		pg := Page{Status: doc.status, Server: doc.server, Mitigated: doc.mitigated, Title: cur.Title, Text: cur.Text, HTML: cur.HTML, Widget: cur.Widget}
		doc.mu.Unlock()
		switch Classify(pg) {
		case StateChallenge:
			challenged, contentRun, blockedRun = true, 0, 0
			humanSince = time.Time{}
		case StateHuman:
			challenged, contentRun, blockedRun = true, 0, 0
			if humanSince.IsZero() {
				humanSince = time.Now()
			} else if time.Since(humanSince) >= humanGrace {
				return SolveResult{Outcome: OutcomeHuman, Challenged: true, Title: cur.Title, UserAgent: ua,
					Message: "the site asks for a human check (a CAPTCHA); open it in your browser"}
			}
		case StateBlocked:
			contentRun = 0
			if blockedRun++; blockedRun >= 2 {
				return SolveResult{Outcome: OutcomeBlocked, Challenged: challenged, Status: pg.Status, Title: cur.Title, UserAgent: ua,
					Message: "the site refuses this browser (blocked, not a check)"}
			}
		case StateContent:
			blockedRun = 0
			humanSince = time.Time{}
			if cur.Ready == "loading" {
				continue
			}
			// Twice in a row, so a check that is about to reload is not mistaken for content.
			if contentRun++; contentRun < 2 {
				continue
			}
			r := s.finish(tctx, u, doc, cur, ua, challenged)
			if own {
				r.Headers = hintHeaders(full)
			}
			return r
		}
	}
}

// finish collects the document and the cookies of a solved page.
func (s *Solver) finish(ctx context.Context, u *url.URL, doc *mainDoc, snap snapshot, ua string, challenged bool) SolveResult {
	t := chromedp.FromContext(ctx).Target
	doc.mu.Lock()
	rid, status, mime, docURL, refused := doc.requestID, doc.status, doc.mime, doc.url, doc.refused
	doc.mu.Unlock()
	if refused != "" {
		return SolveResult{Outcome: OutcomeRefused, Message: refused, UserAgent: ua}
	}
	res := SolveResult{Outcome: OutcomeSolved, URL: snap.URL, Status: status, ContentType: mime, Title: snap.Title,
		UserAgent: ua, Challenged: challenged}
	if res.URL == "" {
		res.URL = docURL
	}
	var body []byte
	if rid != "" {
		if b, err := cdp.Call(ctx, t, network.GetResponseBody, network.GetResponseBodyParams{RequestID: rid}); err == nil {
			body = b.Body
		}
	}
	if len(body) == 0 || len(body) > maxBodyBytes {
		if html, err := chromedp.Run(ctx, chromedp.Evaluate[string](fullHTMLJS)); err == nil {
			body = []byte(html)
		}
		if res.ContentType == "" {
			res.ContentType = "text/html"
		}
	}
	res.Body = string(body)
	urls := []string{u.String()}
	if res.URL != "" && res.URL != u.String() {
		urls = append(urls, res.URL)
	}
	if cks, err := cdp.Call(ctx, t, network.GetCookies, network.GetCookiesParams{URLs: urls}); err == nil {
		for _, ck := range cks.Cookies {
			out := Cookie{Name: ck.Name, Value: ck.Value, Domain: ck.Domain, Path: ck.Path, HTTPOnly: ck.HTTPOnly, Secure: ck.Secure}
			if !ck.Session && ck.Expires > 0 {
				out.Expires = ck.Expires
			}
			res.Cookies = append(res.Cookies, out)
		}
	}
	return res
}

// uaMetadata describes the browser in the client hints the way a desktop
// Chromium on Linux does (the headless build would otherwise name itself).
func uaMetadata(full string) *emulation.UserAgentMetadata {
	major, _, _ := strings.Cut(full, ".")
	arch := "x86"
	if runtime.GOARCH == "arm64" {
		arch = "arm"
	}
	return &emulation.UserAgentMetadata{
		Brands: []*emulation.UserAgentBrandVersion{{Brand: "Chromium", Version: major}, {Brand: "Not.A/Brand", Version: "99"}},
		FullVersionList: []*emulation.UserAgentBrandVersion{{Brand: "Chromium", Version: full},
			{Brand: "Not.A/Brand", Version: "99.0.0.0"}},
		Platform: "Linux", PlatformVersion: "", Architecture: arch, Bitness: "64", Model: "", Mobile: false,
	}
}

// hintHeaders are the low-entropy client hints every request carries.
func hintHeaders(full string) map[string]string {
	major, _, _ := strings.Cut(full, ".")
	return map[string]string{
		"Sec-Ch-Ua":          `"Chromium";v="` + major + `", "Not.A/Brand";v="99"`,
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua-Platform": `"Linux"`,
	}
}

// checkFrame reports whether the tab holds a check widget in a frame of its
// own (such as Cloudflare's Turnstile box): it lives in another process and
// does not show in the page's markup.
func (s *Solver) checkFrame(ctx context.Context, c *chromedp.Context) bool {
	res, err := cdp.Call(ctx, c.Browser, target.GetTargets, target.GetTargetsParams{
		Filter: target.Filter{{Type: "iframe"}}})
	if err != nil {
		return false
	}
	for _, t := range res.TargetInfos {
		if t.BrowserContextID != c.BrowserContextID {
			continue
		}
		u := strings.ToLower(t.URL)
		if strings.Contains(u, "challenges.cloudflare.com") || strings.Contains(u, "hcaptcha.com") || strings.Contains(u, "/recaptcha/") {
			return true
		}
	}
	return false
}
