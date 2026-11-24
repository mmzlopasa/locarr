package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultBrowserURL is where the companion browser listens on the compose
// network (the "browser" profile of deploy/docker-compose.yml).
const DefaultBrowserURL = "http://locarr-browser:8490"

// BrowserCommand starts the companion browser next to a compose install.
const BrowserCommand = "docker compose --profile browser up -d"

// Reasons a browser check could not be passed.
const (
	ReasonMissing = "browser_missing" // the companion browser is not running or not reachable
	ReasonFailed  = "browser_failed"  // it ran but the check did not finish, or it refused the token
	ReasonHuman   = "human"           // the site wants a person to solve a CAPTCHA
	ReasonBlocked = "blocked"         // the site refuses the browser outright
)

// ChallengeError is a browser check that could not be passed. It matches
// ErrChallenge with errors.Is.
type ChallengeError struct {
	Reason  string
	Message string
}

func (e *ChallengeError) Error() string { return e.Message }

// Is makes errors.Is(err, ErrChallenge) true.
func (e *ChallengeError) Is(t error) bool { return t == ErrChallenge }

// BrowserConfig says where the companion browser is and what Locarr may ask
// it to reach.
type BrowserConfig struct {
	URL   string
	Token string
	// Policy, when set, replaces ControlPolicy for reaching the browser.
	Policy *Policy
}

// Browser talks to the companion browser (the locarr-browser image). It is
// shared by every indexer client: it keeps the cookies a solve earned per
// site (and proxy) so Locarr's own client can reuse them with the browser's
// User-Agent until they expire or the check comes back, and it backs off
// from sites whose check could not be passed, so neither the site nor the
// browser is asked again and again.
type Browser struct {
	// Config returns the current address and token (from the settings).
	Config func(ctx context.Context) (BrowserConfig, error)
	Now    func() time.Time
	// ControlPolicy is the address policy for reaching the browser itself
	// (default: everything but loopback, link-local and metadata addresses).
	ControlPolicy Policy

	once   sync.Once
	hc     *http.Client
	hcs    map[string]*http.Client // by control policy
	mu     sync.Mutex
	jar    map[string]*browserClearance
	hosts  map[string]*hostState
	flight map[string]*sync.Mutex
	last   *SolveEvent
	reach  *reachState
}

type browserClearance struct {
	cookies   []*http.Cookie
	userAgent string
	headers   map[string]string
	expires   time.Time
	at        time.Time
}

type hostState struct {
	until   time.Time // no new solve before this
	reason  string
	message string
	// direct: the cookies do not carry over to Locarr's client (the site
	// ties them to the browser), so requests go through the browser.
	direct time.Time
}

type reachState struct {
	ok  bool
	at  time.Time
	err string
}

// SolveEvent is the last solve Locarr asked for.
type SolveEvent struct {
	At      time.Time
	Host    string
	Outcome string
	Message string
	Elapsed time.Duration
}

// Backoffs after a failed solve.
const (
	backoffHuman   = 30 * time.Minute
	backoffFailed  = 5 * time.Minute
	backoffMissing = time.Minute
	directFor      = 30 * time.Minute
)

func (b *Browser) init() {
	b.once.Do(func() {
		if b.Now == nil {
			b.Now = time.Now
		}
		b.jar = map[string]*browserClearance{}
		b.hosts = map[string]*hostState{}
		b.flight = map[string]*sync.Mutex{}
		// The browser lives on the compose network: a private address. Loopback
		// and metadata addresses stay blocked.
		b.hc = &http.Client{
			Transport:     &http.Transport{DialContext: b.ControlPolicy.dialer(5 * time.Second), MaxIdleConnsPerHost: 2},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	})
}

func jarKey(host, proxy string) string { return strings.ToLower(host) + "|" + proxy }

// clearance returns the cookies earned for host through proxy, if still fresh.
func (b *Browser) clearance(host, proxy string) *browserClearance {
	b.init()
	b.mu.Lock()
	defer b.mu.Unlock()
	cl := b.jar[jarKey(host, proxy)]
	if cl == nil || !b.Now().Before(cl.expires) {
		return nil
	}
	return cl
}

// cookiesRejected notes that the site showed its check again although the
// request carried fresh cookies: the site ties them to the browser itself, so
// requests go through the browser for a while.
func (b *Browser) cookiesRejected(host, proxy string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.hosts[jarKey(host, proxy)]
	if st == nil {
		st = &hostState{}
		b.hosts[jarKey(host, proxy)] = st
	}
	st.direct = b.Now().Add(directFor)
	delete(b.jar, jarKey(host, proxy))
}

// direct reports whether requests to host go straight through the browser.
func (b *Browser) direct(host, proxy string) bool {
	b.init()
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.hosts[jarKey(host, proxy)]
	return st != nil && b.Now().Before(st.direct)
}

// HostIssue reports why the last solve for host failed, while its backoff runs.
func (b *Browser) HostIssue(host string) (reason, message string, ok bool) {
	b.init()
	b.mu.Lock()
	defer b.mu.Unlock()
	host = strings.ToLower(host)
	for k, st := range b.hosts {
		if strings.HasPrefix(k, host+"|") && st.reason != "" && b.Now().Before(st.until) {
			return st.reason, st.message, true
		}
	}
	return "", "", false
}

// Reachable reports the outcome of the last contact with the browser
// (ok=false and known=false before the first contact).
func (b *Browser) Reachable() (ok, known bool) {
	b.init()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.reach == nil {
		return false, false
	}
	return b.reach.ok, true
}

// LastSolve is the last solve Locarr asked for (nil before the first).
func (b *Browser) LastSolve() *SolveEvent {
	b.init()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.last == nil {
		return nil
	}
	e := *b.last
	return &e
}

func (b *Browser) noteReach(ok bool, err string) {
	b.mu.Lock()
	b.reach = &reachState{ok: ok, at: b.Now(), err: err}
	b.mu.Unlock()
}

// SolveRequest is the control protocol's request (see browser/solve.go).
type SolveRequest struct {
	URL           string        `json:"url"`
	UserAgent     string        `json:"userAgent,omitempty"`
	Cookies       []SolveCookie `json:"cookies,omitempty"`
	Proxy         string        `json:"proxy,omitempty"`
	TimeoutMs     int           `json:"timeoutMs,omitempty"`
	Allowed       []string      `json:"allowed,omitempty"`
	TargetPrivate bool          `json:"targetPrivate,omitempty"`
}

// SolveCookie is a cookie in the control protocol.
type SolveCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain,omitempty"`
	Path     string  `json:"path,omitempty"`
	Expires  float64 `json:"expires,omitempty"`
	HTTPOnly bool    `json:"httpOnly,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
}

// SolveResult is the control protocol's answer.
type SolveResult struct {
	Outcome     string            `json:"outcome"`
	Message     string            `json:"message"`
	URL         string            `json:"url"`
	Status      int               `json:"status"`
	ContentType string            `json:"contentType"`
	Title       string            `json:"title"`
	Body        string            `json:"body"`
	Cookies     []SolveCookie     `json:"cookies"`
	UserAgent   string            `json:"userAgent"`
	Headers     map[string]string `json:"headers"`
	Challenged  bool              `json:"challenged"`
	ElapsedMs   int64             `json:"elapsedMs"`
}

// call sends one control request.
func (b *Browser) call(ctx context.Context, method, path string, in, out any) error {
	b.init()
	cfg, err := b.Config(ctx)
	if err != nil {
		return err
	}
	if cfg.URL == "" {
		cfg.URL = DefaultBrowserURL
	}
	var body *bytes.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	} else {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(cfg.URL, "/")+path, body)
	if err != nil {
		return &ChallengeError{Reason: ReasonMissing, Message: "the browser address is not valid"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	res, err := b.client(cfg.Policy).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return &ChallengeError{Reason: ReasonFailed, Message: "the browser did not answer in time"}
		}
		msg := "the built-in browser is not running or not reachable at " + ShortURL(cfg.URL) +
			"; start it with: " + BrowserCommand
		b.noteReach(false, msg)
		return &ChallengeError{Reason: ReasonMissing, Message: msg}
	}
	defer res.Body.Close()
	raw, err := ReadCapped(res.Body, 32<<20)
	if err != nil {
		return &ChallengeError{Reason: ReasonFailed, Message: "the browser's answer could not be read: " + err.Error()}
	}
	if res.StatusCode == http.StatusUnauthorized {
		msg := "the browser refused Locarr's token: set the same LOCARR_BROWSER_TOKEN for both"
		b.noteReach(false, msg)
		return &ChallengeError{Reason: ReasonFailed, Message: msg}
	}
	if res.StatusCode != http.StatusOK {
		b.noteReach(false, res.Status)
		return &ChallengeError{Reason: ReasonFailed, Message: "the browser answered " + res.Status}
	}
	b.noteReach(true, "")
	if err := json.Unmarshal(raw, out); err != nil {
		return &ChallengeError{Reason: ReasonFailed, Message: "the browser's answer could not be read"}
	}
	return nil
}

// client returns the HTTP client for the control connection.
func (b *Browser) client(p *Policy) *http.Client {
	if p == nil {
		return b.hc
	}
	key := policyKey(*p)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hcs == nil {
		b.hcs = map[string]*http.Client{}
	}
	if hc := b.hcs[key]; hc != nil {
		return hc
	}
	hc := &http.Client{
		Transport:     &http.Transport{DialContext: p.dialer(5 * time.Second), MaxIdleConnsPerHost: 2},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	b.hcs[key] = hc
	return hc
}

func policyKey(p Policy) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%v;", p.BlockPrivate)
	for _, a := range p.Allowed {
		sb.WriteString(a.String() + ";")
	}
	sb.WriteString(strings.Join(p.AllowedHosts, ";"))
	return sb.String()
}

// policyEntries turns an address policy into the browser's allowed list.
func policyEntries(p Policy) []string {
	out := make([]string, 0, len(p.Allowed)+len(p.AllowedHosts))
	for _, a := range p.Allowed {
		out = append(out, a.String())
	}
	return append(out, p.AllowedHosts...)
}

// checkTarget applies Locarr's own address policy to the page before the
// browser is asked to open it (the browser applies its own, stricter one too).
func checkTarget(ctx context.Context, p Policy, raw string, proxied bool) error {
	if err := p.CheckURL(raw); err != nil {
		return err
	}
	u, _ := url.Parse(raw)
	host := u.Hostname()
	if proxied || p.hostAllowed(host) {
		return nil
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return nil // CheckURL checked the literal address
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("%s: %w", host, err)
	}
	for _, a := range addrs {
		if err := p.Check(a); err != nil {
			return err
		}
	}
	return nil
}

// solveInput is one page a client wants opened.
type solveInput struct {
	url     string
	proxy   string
	policy  Policy
	secrets []string
}

// solve opens a page in the browser and keeps the cookies it earned. The
// answer is the page itself, ready to use as the response.
func (b *Browser) solve(ctx context.Context, in solveInput) (*Response, error) {
	b.init()
	u, err := url.Parse(in.url)
	if err != nil {
		return nil, err
	}
	key := jarKey(u.Host, in.proxy)
	b.mu.Lock()
	if st := b.hosts[key]; st != nil && st.reason != "" && b.Now().Before(st.until) {
		b.mu.Unlock()
		return nil, &ChallengeError{Reason: st.reason, Message: st.message}
	}
	fl := b.flight[key]
	if fl == nil {
		fl = &sync.Mutex{}
		b.flight[key] = fl
	}
	b.mu.Unlock()
	// One solve per site at a time; the others wait and reuse its cookies.
	fl.Lock()
	defer fl.Unlock()

	if err := checkTarget(ctx, in.policy, in.url, in.proxy != ""); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBlocked, Scrub(err.Error(), in.secrets...))
	}
	timeout := 60 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(dl)-time.Second)
	}
	if timeout < 5*time.Second {
		return nil, &ChallengeError{Reason: ReasonFailed, Message: "too little time left for the browser check"}
	}
	req := SolveRequest{URL: in.url, Proxy: in.proxy, TimeoutMs: int(timeout.Milliseconds()),
		Allowed: policyEntries(in.policy), TargetPrivate: !in.policy.BlockPrivate}
	if cl := b.clearance(u.Host, in.proxy); cl != nil {
		for _, ck := range cl.cookies {
			req.Cookies = append(req.Cookies, SolveCookie{Name: ck.Name, Value: ck.Value})
		}
	}
	var res SolveResult
	start := b.Now()
	cctx, cancel := context.WithTimeout(ctx, timeout+10*time.Second)
	defer cancel()
	if err := b.call(cctx, http.MethodPost, "/v1/solve", req, &res); err != nil {
		var ce *ChallengeError
		if errors.As(err, &ce) {
			b.fail(key, u.Host, ce, start)
		}
		return nil, err
	}
	b.mu.Lock()
	b.last = &SolveEvent{At: b.Now(), Host: u.Host, Outcome: res.Outcome, Message: Scrub(res.Message, in.secrets...),
		Elapsed: time.Duration(res.ElapsedMs) * time.Millisecond}
	b.mu.Unlock()
	switch res.Outcome {
	case "solved":
	case "human":
		ce := &ChallengeError{Reason: ReasonHuman,
			Message: "the site " + u.Host + " needs a human check (a CAPTCHA); open it in your browser. Locarr does not solve these and waits before trying again"}
		b.fail(key, u.Host, ce, start)
		return nil, ce
	case "blocked":
		ce := &ChallengeError{Reason: ReasonBlocked, Message: "the site " + u.Host + " refuses the built-in browser (blocked, not a check)"}
		b.fail(key, u.Host, ce, start)
		return nil, ce
	case "refused":
		return nil, fmt.Errorf("%w: the browser refused the address: %s", ErrBlocked, Scrub(res.Message, in.secrets...))
	default:
		msg := res.Message
		if msg == "" {
			msg = res.Outcome
		}
		ce := &ChallengeError{Reason: ReasonFailed, Message: "the built-in browser could not pass the check of " + u.Host + ": " + Scrub(msg, in.secrets...)}
		b.fail(key, u.Host, ce, start)
		return nil, ce
	}
	b.keep(key, res)
	hdr := http.Header{}
	if res.ContentType != "" {
		hdr.Set("Content-Type", res.ContentType)
	}
	status := res.Status
	if status == 0 {
		status = http.StatusOK
	}
	return &Response{Status: status, Header: hdr, Body: []byte(res.Body)}, nil
}

func (b *Browser) fail(key, host string, ce *ChallengeError, _ time.Time) {
	wait := backoffFailed
	switch ce.Reason {
	case ReasonHuman, ReasonBlocked:
		wait = backoffHuman
	case ReasonMissing:
		wait = backoffMissing
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.hosts[key]
	if st == nil {
		st = &hostState{}
		b.hosts[key] = st
	}
	st.until, st.reason, st.message = b.Now().Add(wait), ce.Reason, ce.Message
	if b.last == nil || ce.Reason == ReasonMissing {
		b.last = &SolveEvent{At: b.Now(), Host: host, Outcome: ce.Reason, Message: ce.Message}
	}
}

// keep stores the cookies of a solved page.
func (b *Browser) keep(key string, res SolveResult) {
	now := b.Now()
	cl := &browserClearance{userAgent: res.UserAgent, headers: res.Headers, at: now, expires: now.Add(30 * time.Minute)}
	var earliest time.Time
	for _, ck := range res.Cookies {
		cl.cookies = append(cl.cookies, &http.Cookie{Name: ck.Name, Value: ck.Value}) // #nosec G124 -- sent to a server, not set in a browser
		if ck.Expires > 0 {
			t := time.Unix(int64(ck.Expires), 0)
			if earliest.IsZero() || t.Before(earliest) {
				earliest = t
			}
		}
	}
	if !earliest.IsZero() {
		switch {
		case earliest.Before(now.Add(5 * time.Minute)):
			cl.expires = now.Add(5 * time.Minute)
		case earliest.After(now.Add(12 * time.Hour)):
			cl.expires = now.Add(12 * time.Hour)
		default:
			cl.expires = earliest
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.jar[key] = cl
	if st := b.hosts[key]; st != nil {
		st.reason, st.message, st.until = "", "", time.Time{}
	}
}

// BrowserStatus is the companion browser's state.
type BrowserStatus struct {
	State     string // connected, missing, unauthorized or error
	URL       string
	Message   string
	Version   string
	Chromium  string
	Running   bool
	Sandbox   bool
	Tabs      int
	TabsInUse int
	Solves    int
	Restarts  int
	LastSolve *SolveEvent
}

// Status asks the browser how it is.
func (b *Browser) Status(ctx context.Context) BrowserStatus {
	b.init()
	cfg, err := b.Config(ctx)
	st := BrowserStatus{URL: cfg.URL}
	if st.URL == "" {
		st.URL = DefaultBrowserURL
	}
	if err != nil {
		return BrowserStatus{State: "error", URL: st.URL, Message: err.Error()}
	}
	var out struct {
		Version   string `json:"version"`
		Browser   string `json:"browser"`
		Running   bool   `json:"running"`
		Sandbox   bool   `json:"sandbox"`
		Tabs      int    `json:"tabs"`
		TabsInUse int    `json:"tabsInUse"`
		Restarts  int    `json:"restarts"`
		Solves    int    `json:"solves"`
		LastSolve *struct {
			At        time.Time `json:"at"`
			Host      string    `json:"host"`
			Outcome   string    `json:"outcome"`
			ElapsedMs int64     `json:"elapsedMs"`
			Message   string    `json:"message"`
		} `json:"lastSolve"`
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := b.call(cctx, http.MethodGet, "/v1/status", nil, &out); err != nil {
		st.State, st.Message = "error", err.Error()
		var ce *ChallengeError
		if errors.As(err, &ce) {
			switch {
			case ce.Reason == ReasonMissing:
				st.State = "missing"
			case strings.Contains(ce.Message, "token"):
				st.State = "unauthorized"
			}
		}
		st.LastSolve = b.LastSolve()
		return st
	}
	st.State, st.Version, st.Chromium, st.Running, st.Sandbox = "connected", out.Version, out.Browser, out.Running, out.Sandbox
	st.Tabs, st.TabsInUse, st.Solves, st.Restarts = out.Tabs, out.TabsInUse, out.Solves, out.Restarts
	if ls := out.LastSolve; ls != nil {
		st.LastSolve = &SolveEvent{At: ls.At, Host: ls.Host, Outcome: ls.Outcome, Message: ls.Message,
			Elapsed: time.Duration(ls.ElapsedMs) * time.Millisecond}
	}
	return st
}

// TestResult is the outcome of a browser test.
type TestResult struct {
	Passed     bool
	Outcome    string
	Message    string
	Title      string
	Challenged bool
	Cookies    int
	Elapsed    time.Duration
}

// SelfTest has the browser pass its own built-in check page (variant ""), or
// recognise its CAPTCHA page ("human").
func (b *Browser) SelfTest(ctx context.Context, variant string) (TestResult, error) {
	var out struct {
		Passed bool        `json:"passed"`
		Result SolveResult `json:"result"`
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := b.call(cctx, http.MethodPost, "/v1/selftest", map[string]string{"variant": variant}, &out); err != nil {
		return TestResult{}, err
	}
	r := out.Result
	return TestResult{Passed: out.Passed, Outcome: r.Outcome, Message: r.Message, Title: r.Title, Challenged: r.Challenged,
		Cookies: len(r.Cookies), Elapsed: time.Duration(r.ElapsedMs) * time.Millisecond}, nil
}

// TestURL opens a page in the browser (with the indexers' address policy)
// and reports what happened, without keeping cookies or backing off.
func (b *Browser) TestURL(ctx context.Context, p Policy, raw string) (TestResult, error) {
	if err := checkTarget(ctx, p, raw, false); err != nil {
		return TestResult{}, fmt.Errorf("%w: %s", ErrBlocked, err.Error())
	}
	var r SolveResult
	cctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	req := SolveRequest{URL: raw, TimeoutMs: 60000, Allowed: policyEntries(p), TargetPrivate: !p.BlockPrivate}
	if err := b.call(cctx, http.MethodPost, "/v1/solve", req, &r); err != nil {
		return TestResult{}, err
	}
	return TestResult{Passed: r.Outcome == "solved", Outcome: r.Outcome, Message: r.Message, Title: r.Title,
		Challenged: r.Challenged, Cookies: len(r.Cookies), Elapsed: time.Duration(r.ElapsedMs) * time.Millisecond}, nil
}
