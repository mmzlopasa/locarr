package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/net/proxy"
)

// guardHeader marks the guard's own refusal answers to plain http:// requests.
const guardHeader = "X-Locarr-Guard"

// errRefused is returned when an address is not allowed.
var errRefused = errors.New("address not allowed")

var (
	linkLocal = []netip.Prefix{
		netip.MustParsePrefix("169.254.0.0/16"), // link-local and cloud metadata
		netip.MustParsePrefix("100.100.100.200/32"),
		netip.MustParsePrefix("fd00:ec2::254/128"),
		netip.MustParsePrefix("fe80::/10"),
	}
	privateNets = []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("fc00::/7"),
	}
)

// Policy decides which addresses the browser may reach during one solve.
// Everything the page loads (documents, scripts, images, websockets) goes
// through the guard proxy, which applies it when it connects.
type Policy struct {
	// Target is the host of the page Locarr asked for (lower-case).
	Target string
	// TargetPrivate lets the target host (only it) resolve to a private
	// address: Locarr checked it with its own policy first.
	TargetPrivate bool
	// Allowed are networks that are always allowed.
	Allowed []netip.Prefix
	// AllowedHosts skip the address check (lower-case host names).
	AllowedHosts []string
	// selfTest allows exactly this loopback address (the self-test page).
	selfTest netip.AddrPort
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

// check reports whether host (as asked for) may be reached at ap.
func (p Policy) check(host string, ap netip.AddrPort) error {
	ip := ap.Addr().Unmap()
	if p.selfTest.IsValid() && ap.Port() == p.selfTest.Port() && ip == p.selfTest.Addr() {
		return nil
	}
	for _, a := range p.Allowed {
		if a.Contains(ip) {
			return nil
		}
	}
	switch {
	case ip.IsLoopback(), ip.IsUnspecified(), ip.IsMulticast(), ip.IsInterfaceLocalMulticast(),
		ip == netip.AddrFrom4([4]byte{255, 255, 255, 255}), ip.Is4() && ip.As4()[0] == 0:
		return fmt.Errorf("%w: %s", errRefused, ip)
	}
	for _, m := range linkLocal {
		if m.Contains(ip) {
			return fmt.Errorf("%w: %s", errRefused, ip)
		}
	}
	for _, m := range privateNets {
		if m.Contains(ip) {
			if p.TargetPrivate && strings.EqualFold(strings.TrimSuffix(host, "."), p.Target) {
				return nil
			}
			return fmt.Errorf("%w: %s (private network)", errRefused, ip)
		}
	}
	return nil
}

// parsePolicy turns Locarr's allowed entries (addresses, networks, host names) into a policy.
func parsePolicy(target string, targetPrivate bool, entries []string) (Policy, error) {
	p := Policy{Target: strings.ToLower(target), TargetPrivate: targetPrivate}
	for _, e := range entries {
		e = strings.TrimSpace(strings.ToLower(e))
		if e == "" {
			continue
		}
		if pr, err := netip.ParsePrefix(e); err == nil {
			p.Allowed = append(p.Allowed, pr.Masked())
			continue
		}
		if a, err := netip.ParseAddr(e); err == nil {
			p.Allowed = append(p.Allowed, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
			continue
		}
		if strings.ContainsAny(e, "/:@ ") || len(e) > 253 {
			return Policy{}, fmt.Errorf("%q is not an address, network or host name", e)
		}
		p.AllowedHosts = append(p.AllowedHosts, e)
	}
	return p, nil
}

// guard is a forward proxy for one solve. Chromium sends every request of the
// solve's browser context to it; it checks each address when it connects
// (so DNS rebinding cannot slip past an earlier check), optionally chains to
// the indexer's own proxy, and stops once the solve moved too many bytes.
type guard struct {
	policy   Policy
	upstream *url.URL // optional http(s):// or socks5:// proxy
	maxBytes int64

	ln       net.Listener
	srv      *http.Server
	bytes    atomic.Int64
	refused  atomic.Int64
	mu       sync.Mutex
	lastDeny string
	conns    map[net.Conn]struct{}
}

func newGuard(p Policy, upstream string, maxBytes int64) (*guard, error) {
	g := &guard{policy: p, maxBytes: maxBytes, conns: map[net.Conn]struct{}{}}
	if upstream != "" {
		u, err := url.Parse(upstream)
		if err != nil || u.Host == "" {
			return nil, errors.New("unsupported proxy URL")
		}
		switch u.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return nil, errors.New("unsupported proxy URL")
		}
		g.upstream = u
	}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	g.ln = ln
	g.srv = &http.Server{Handler: g, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = g.srv.Serve(ln) }()
	return g, nil
}

// addr is the proxy address to give Chromium.
func (g *guard) addr() string { return "http://" + g.ln.Addr().String() }

func (g *guard) close() {
	_ = g.srv.Close()
	g.mu.Lock()
	for c := range g.conns {
		_ = c.Close()
	}
	g.mu.Unlock()
}

func (g *guard) overLimit() bool { return g.maxBytes > 0 && g.bytes.Load() > g.maxBytes }

func (g *guard) deny(err error) {
	g.refused.Add(1)
	g.mu.Lock()
	g.lastDeny = err.Error()
	g.mu.Unlock()
}

func (g *guard) track(c net.Conn, on bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if on {
		g.conns[c] = struct{}{}
	} else {
		delete(g.conns, c)
	}
}

// dial connects to addr (host:port) directly or through the upstream proxy,
// after checking the policy.
func (g *guard) dial(ctx context.Context, addr string) (net.Conn, error) {
	if g.overLimit() {
		return nil, errors.New("page size limit reached")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if g.upstream != nil {
		// The upstream proxy resolves names; literal addresses are still checked.
		if a, err := netip.ParseAddr(host); err == nil && !g.policy.hostAllowed(host) {
			if err := g.policy.check(host, netip.AddrPortFrom(a, 0)); err != nil {
				return nil, err
			}
		}
		if strings.EqualFold(host, "localhost") {
			return nil, fmt.Errorf("%w: localhost", errRefused)
		}
		return g.dialUpstream(ctx, addr)
	}
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if !g.policy.hostAllowed(host) {
		d.Control = func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("%w: %s", errRefused, address)
			}
			return g.policy.check(host, ap)
		}
	}
	return d.DialContext(ctx, "tcp", addr)
}

func (g *guard) dialUpstream(ctx context.Context, addr string) (net.Conn, error) {
	u := g.upstream
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	switch u.Scheme {
	case "socks5", "socks5h":
		var auth *proxy.Auth
		if u.User != nil {
			pw, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: pw}
		}
		sd, err := proxy.SOCKS5("tcp", u.Host, auth, d)
		if err != nil {
			return nil, err
		}
		if cd, ok := sd.(proxy.ContextDialer); ok {
			return cd.DialContext(ctx, "tcp", addr)
		}
		return sd.Dial("tcp", addr)
	}
	// An HTTP proxy: open a tunnel with CONNECT.
	c, err := d.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, err
	}
	req := "CONNECT " + addr + " HTTP/1.1\r\nHost: " + addr + "\r\n"
	if u.User != nil {
		pw, _ := u.User.Password()
		req += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+pw)) + "\r\n"
	}
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := io.WriteString(c, req+"\r\n"); err != nil {
		_ = c.Close()
		return nil, err
	}
	br := bufio.NewReader(c)
	res, err := http.ReadResponse(br, nil)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_ = c.Close()
		return nil, fmt.Errorf("proxy answered %s", res.Status)
	}
	_ = c.SetDeadline(time.Time{})
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: c, r: br}, nil
	}
	return c, nil
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.r.Read(p) }

// countingConn adds every byte read or written to the solve's total.
type countingConn struct {
	net.Conn
	g *guard
}

func (c countingConn) Read(p []byte) (int, error) {
	if c.g.overLimit() {
		return 0, errors.New("page size limit reached")
	}
	n, err := c.Conn.Read(p)
	c.g.bytes.Add(int64(n))
	return n, err
}

func (c countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.g.bytes.Add(int64(n))
	return n, err
}

func (g *guard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		g.tunnel(w, r)
		return
	}
	g.forward(w, r)
}

func (g *guard) tunnel(w http.ResponseWriter, r *http.Request) {
	addr := r.Host
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "443")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	up, err := g.dial(ctx, addr)
	cancel()
	if err != nil {
		if errors.Is(err, errRefused) {
			g.deny(err)
			http.Error(w, "refused", http.StatusForbidden)
			return
		}
		http.Error(w, "unreachable", http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = up.Close()
		http.Error(w, "no tunnel", http.StatusInternalServerError)
		return
	}
	down, buf, err := hj.Hijack()
	if err != nil {
		_ = up.Close()
		return
	}
	g.track(up, true)
	g.track(down, true)
	defer func() {
		g.track(up, false)
		g.track(down, false)
		_ = up.Close()
		_ = down.Close()
	}()
	if _, err := io.WriteString(down, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		return
	}
	cu := countingConn{Conn: up, g: g}
	done := make(chan struct{}, 2)
	go func() {
		if buf.Reader.Buffered() > 0 {
			_, _ = io.CopyN(cu, buf, int64(buf.Reader.Buffered()))
		}
		_, _ = io.Copy(cu, down)
		done <- struct{}{}
	}()
	go func() { _, _ = io.Copy(down, cu); done <- struct{}{} }()
	<-done
}

// hopHeaders are not passed on by a proxy.
var hopHeaders = []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"}

func (g *guard) forward(w http.ResponseWriter, r *http.Request) {
	if r.URL.Scheme != "http" || r.URL.Host == "" {
		http.Error(w, "only http:// through this proxy", http.StatusBadRequest)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	for _, h := range hopHeaders {
		out.Header.Del(h)
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			c, err := g.dial(ctx, addr)
			if err != nil {
				return nil, err
			}
			return countingConn{Conn: c, g: g}, nil
		},
		ResponseHeaderTimeout: 30 * time.Second,
		DisableCompression:    true,
	}
	defer tr.CloseIdleConnections()
	res, err := tr.RoundTrip(out)
	if err != nil {
		if errors.Is(err, errRefused) {
			g.deny(err)
			w.Header().Set(guardHeader, err.Error())
			http.Error(w, "refused", http.StatusForbidden)
			return
		}
		http.Error(w, "unreachable", http.StatusBadGateway)
		return
	}
	defer func() { _ = res.Body.Close() }()
	res.Header.Del(guardHeader)
	for _, h := range hopHeaders {
		res.Header.Del(h)
	}
	for k, v := range res.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}
