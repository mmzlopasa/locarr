// Package httpx is the outbound HTTP client for indexers and the services that
// front them (Prowlarr, Jackett, FlareSolverr). Every request has a timeout, a
// response size cap, never follows a redirect to another host, refuses
// addresses that point back into the host or at cloud metadata services
// (unless the administrator allows them) and never puts an API key into an
// error message.
package httpx

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/proxy"
)

// Errors returned by the client.
var (
	ErrBlocked           = errors.New("address not allowed")
	ErrCrossHostRedirect = errors.New("redirect to another host refused")
	ErrTooLarge          = errors.New("response too large")
	ErrBadProxy          = errors.New("unsupported proxy URL")
)

// Defaults.
const (
	DefaultTimeout  = 30 * time.Second
	DefaultMaxBytes = 16 << 20
	maxRedirects    = 5
	userAgent       = "Locarr"
)

// Policy decides which addresses outbound requests may reach.
type Policy struct {
	// BlockPrivate refuses RFC 1918 / ULA / CGNAT addresses too. Off by default:
	// Prowlarr, Jackett and FlareSolverr usually live on the same Docker network.
	BlockPrivate bool
	// Allowed are addresses (prefixes) and host names that are allowed even when
	// the rules above would block them (e.g. 127.0.0.1 or "localhost").
	Allowed []netip.Prefix
	// AllowedHosts are host names (lower-case) that skip the address check.
	AllowedHosts []string
}

// ParseAllowed splits administrator entries into prefixes and host names.
func ParseAllowed(entries []string) (prefixes []netip.Prefix, hosts []string, err error) {
	for _, e := range entries {
		e = strings.TrimSpace(strings.ToLower(e))
		if e == "" {
			continue
		}
		if p, perr := netip.ParsePrefix(e); perr == nil {
			prefixes = append(prefixes, p.Masked())
			continue
		}
		if a, aerr := netip.ParseAddr(e); aerr == nil {
			prefixes = append(prefixes, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
			continue
		}
		if !validHost(e) {
			return nil, nil, fmt.Errorf("%q is not an address, network or host name", e)
		}
		hosts = append(hosts, e)
	}
	return prefixes, hosts, nil
}

func validHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, r := range h {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.' || r == '_') {
			return false
		}
	}
	return true
}

var (
	metadata = []netip.Prefix{
		netip.MustParsePrefix("169.254.0.0/16"), // link-local, AWS/GCP/Azure metadata
		netip.MustParsePrefix("100.100.100.200/32"),
		netip.MustParsePrefix("fd00:ec2::254/128"),
		netip.MustParsePrefix("fe80::/10"),
	}
	private = []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("fc00::/7"),
	}
)

// Check reports whether ip may be connected to.
func (p Policy) Check(ip netip.Addr) error {
	ip = ip.Unmap()
	for _, a := range p.Allowed {
		if a.Contains(ip) {
			return nil
		}
	}
	switch {
	case ip.IsLoopback(), ip.IsUnspecified(), ip.IsMulticast(), ip.IsInterfaceLocalMulticast(),
		ip == netip.AddrFrom4([4]byte{255, 255, 255, 255}):
		return fmt.Errorf("%w: %s", ErrBlocked, ip)
	}
	if ip.Is4() && ip.As4()[0] == 0 {
		return fmt.Errorf("%w: %s", ErrBlocked, ip)
	}
	for _, m := range metadata {
		if m.Contains(ip) {
			return fmt.Errorf("%w: %s", ErrBlocked, ip)
		}
	}
	if p.BlockPrivate {
		for _, m := range private {
			if m.Contains(ip) {
				return fmt.Errorf("%w: %s", ErrBlocked, ip)
			}
		}
	}
	return nil
}

func (p Policy) hostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, h := range p.AllowedHosts {
		if h == host {
			return true
		}
	}
	return false
}

// dialer returns a DialContext that checks every resolved address at connect
// time (so DNS rebinding cannot slip past a check made earlier).
func (p Policy) dialer(timeout time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		d := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
		if !p.hostAllowed(host) {
			d.Control = func(_, address string, _ syscall.RawConn) error {
				ap, err := netip.ParseAddrPort(address)
				if err != nil {
					return fmt.Errorf("%w: %s", ErrBlocked, address)
				}
				return p.Check(ap.Addr())
			}
		}
		return d.DialContext(ctx, network, addr)
	}
}

// Options configure a Client.
type Options struct {
	Policy   Policy
	Timeout  time.Duration // whole request; default 30 s
	MaxBytes int64         // response cap after decompression; default 16 MiB
	// Proxy is an optional http://, https:// or socks5:// proxy URL (with credentials).
	Proxy string
	// FlareSolverr is an optional FlareSolverr base URL used to get past a
	// Cloudflare challenge.
	FlareSolverr string
	// Secrets are values that must never appear in errors (API keys, passwords).
	Secrets []string
}

// Client is a safe outbound HTTP client. It is cheap; build one per indexer.
type Client struct {
	hc   *http.Client
	o    Options
	mu   sync.Mutex
	cf   *clearance // FlareSolverr cookies for the current host
	fsHC *http.Client
}

type clearance struct {
	host      string
	cookies   []*http.Cookie
	userAgent string
	expires   time.Time
}

// New builds a Client.
func New(o Options) (*Client, error) {
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = DefaultMaxBytes
	}
	tr := &http.Transport{
		DialContext:           o.Policy.dialer(10 * time.Second),
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: o.Timeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   4,
		ForceAttemptHTTP2:     true,
	}
	if o.Proxy != "" {
		pu, err := url.Parse(o.Proxy)
		if err != nil || pu.Host == "" {
			return nil, ErrBadProxy
		}
		switch pu.Scheme {
		case "http", "https":
			tr.Proxy = http.ProxyURL(pu)
		case "socks5", "socks5h":
			var auth *proxy.Auth
			if pu.User != nil {
				pw, _ := pu.User.Password()
				auth = &proxy.Auth{User: pu.User.Username(), Password: pw}
			}
			d, err := proxy.SOCKS5("tcp", pu.Host, auth, guardedDialer{o.Policy.dialer(10 * time.Second)})
			if err != nil {
				return nil, ErrBadProxy
			}
			cd, ok := d.(proxy.ContextDialer)
			if !ok {
				return nil, ErrBadProxy
			}
			tr.DialContext = cd.DialContext
		default:
			return nil, ErrBadProxy
		}
		if pw, ok := pu.User.Password(); ok && pw != "" {
			o.Secrets = append(o.Secrets, pw)
		}
	}
	c := &Client{o: o}
	c.hc = &http.Client{Transport: tr, Timeout: o.Timeout, CheckRedirect: sameHostRedirects}
	if o.FlareSolverr != "" {
		c.fsHC = &http.Client{
			Transport:     &http.Transport{DialContext: o.Policy.dialer(10 * time.Second)},
			Timeout:       90 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	return c, nil
}

type guardedDialer struct {
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

func (g guardedDialer) Dial(network, addr string) (net.Conn, error) {
	return g.dial(context.Background(), network, addr)
}

func (g guardedDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return g.dial(ctx, network, addr)
}

func sameHostRedirects(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errors.New("too many redirects")
	}
	if !strings.EqualFold(req.URL.Hostname(), via[0].URL.Hostname()) {
		return ErrCrossHostRedirect
	}
	return nil
}

// Response is a fully read response body.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Get fetches rawURL with optional extra headers.
func (c *Client) Get(ctx context.Context, rawURL string, hdr http.Header) (*Response, error) {
	return c.Do(ctx, http.MethodGet, rawURL, hdr, nil)
}

// Do sends a request and reads the whole (size-capped) response.
func (c *Client) Do(ctx context.Context, method, rawURL string, hdr http.Header, body []byte) (*Response, error) {
	res, err := c.do(ctx, method, rawURL, hdr, body)
	if err != nil {
		return nil, err
	}
	if c.fsHC != nil && method == http.MethodGet && isChallenge(res) {
		if err := c.solve(ctx, rawURL); err != nil {
			return nil, err
		}
		return c.do(ctx, method, rawURL, hdr, body)
	}
	return res, nil
}

func (c *Client) do(ctx context.Context, method, rawURL string, hdr http.Header, body []byte) (*Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid URL %s", Redact(rawURL, c.o.Secrets...))
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
	if err != nil {
		return nil, c.wrap(err, rawURL)
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range hdr {
		req.Header[k] = v
	}
	c.mu.Lock()
	if cf := c.cf; cf != nil && cf.host == u.Host && time.Now().Before(cf.expires) {
		for _, ck := range cf.cookies {
			req.AddCookie(ck)
		}
		if cf.userAgent != "" {
			req.Header.Set("User-Agent", cf.userAgent)
		}
	}
	c.mu.Unlock()
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, c.wrap(err, rawURL)
	}
	defer res.Body.Close()
	b, err := ReadCapped(res.Body, c.o.MaxBytes)
	if err != nil {
		return nil, c.wrap(err, rawURL)
	}
	return &Response{Status: res.StatusCode, Header: res.Header, Body: b}, nil
}

// ReadCapped reads r (gunzipping it when it is gzip even without a
// Content-Encoding header) and fails when it is larger than limit.
func ReadCapped(r io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, ErrTooLarge
	}
	if len(raw) > 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		out, err := io.ReadAll(io.LimitReader(zr, limit+1))
		if err != nil {
			return nil, err
		}
		if int64(len(out)) > limit {
			return nil, ErrTooLarge
		}
		return out, nil
	}
	return raw, nil
}

// wrap replaces the URL in transport errors with a redacted one.
func (c *Client) wrap(err error, rawURL string) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return &RequestError{URL: Redact(rawURL, c.o.Secrets...), Err: scrubErr{err, c.o.Secrets}}
}

// RequestError is a failed request; URL is already redacted.
type RequestError struct {
	URL string
	Err error
}

func (e *RequestError) Error() string { return "request " + e.URL + ": " + e.Err.Error() }
func (e *RequestError) Unwrap() error { return e.Err }

// Timeout reports whether the request timed out.
func (e *RequestError) Timeout() bool {
	var ne net.Error
	return errors.Is(e.Err, context.DeadlineExceeded) || (errors.As(e.Err, &ne) && ne.Timeout())
}

type scrubErr struct {
	err     error
	secrets []string
}

func (s scrubErr) Error() string { return Scrub(s.err.Error(), s.secrets...) }
func (s scrubErr) Unwrap() error { return s.err }

// sensitiveParams are query parameters that carry credentials.
var sensitiveParams = map[string]bool{"apikey": true, "api_key": true, "jackett_apikey": true, "passkey": true,
	"token": true, "key": true, "password": true, "pass": true, "auth": true}

// Redact returns rawURL with credentials removed: user info, sensitive query
// parameters and any of the given secrets.
func Redact(rawURL string, secrets ...string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Scrub(rawURL, secrets...)
	}
	if u.User != nil {
		u.User = url.User("redacted")
	}
	q := u.Query()
	changed := false
	for k := range q {
		if sensitiveParams[strings.ToLower(k)] {
			q.Set(k, "redacted")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	return Scrub(u.String(), secrets...)
}

// Scrub replaces every secret (and its URL-escaped form) in s.
func Scrub(s string, secrets ...string) string {
	for _, sec := range secrets {
		if len(sec) < 4 {
			continue
		}
		s = strings.ReplaceAll(s, sec, "redacted")
		if esc := url.QueryEscape(sec); esc != sec {
			s = strings.ReplaceAll(s, esc, "redacted")
		}
	}
	return s
}

// CheckURL validates an administrator-entered service URL: http(s), a host,
// no user info and, when the host is a literal address, one the policy allows.
func (p Policy) CheckURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return errors.New("must be an http:// or https:// URL")
	}
	if u.User != nil {
		return errors.New("must not contain a user name or password")
	}
	if p.hostAllowed(u.Hostname()) {
		return nil
	}
	if a, err := netip.ParseAddr(u.Hostname()); err == nil {
		return p.Check(a)
	}
	if strings.EqualFold(u.Hostname(), "localhost") || strings.HasSuffix(strings.ToLower(u.Hostname()), ".localhost") {
		return fmt.Errorf("%w: localhost", ErrBlocked)
	}
	return nil
}
