package downloaders

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

// HTTPOptions configure NewHTTPClient.
type HTTPOptions struct {
	// Timeout bounds a whole request (default 30 s).
	Timeout time.Duration
	// AllowSelfSigned turns TLS certificate verification off (explicit per-client choice).
	AllowSelfSigned bool
	// AllowLinkLocal permits link-local and cloud metadata addresses (off by default).
	AllowLinkLocal bool
}

// metadataAddrs are cloud metadata endpoints that are never reachable through
// the guard, even with AllowLinkLocal.
var metadataAddrs = []netip.Addr{
	netip.MustParseAddr("169.254.169.254"),
	netip.MustParseAddr("169.254.170.2"),
	netip.MustParseAddr("100.100.100.200"),
	netip.MustParseAddr("fd00:ec2::254"),
}

// AllowedAddr reports whether an outgoing connection to ip is allowed. Private,
// LAN, Docker and loopback addresses are allowed (download clients live there);
// link-local, multicast, unspecified and cloud metadata addresses are not.
func AllowedAddr(ip netip.Addr, allowLinkLocal bool) bool {
	ip = ip.Unmap()
	for _, m := range metadataAddrs {
		if ip == m {
			return false
		}
	}
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if ip.IsLinkLocalUnicast() && !allowLinkLocal {
		return false
	}
	return true
}

// guardControl runs after DNS resolution, on the address actually dialled, so
// a name that resolves (or re-resolves) to a forbidden address is refused.
func guardControl(allowLinkLocal bool) func(network, address string, c syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		ap, err := netip.ParseAddrPort(address)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrBlocked, address)
		}
		if !AllowedAddr(ap.Addr(), allowLinkLocal) {
			return fmt.Errorf("%w: %s", ErrBlocked, ap.Addr())
		}
		return nil
	}
}

// NewHTTPClient returns an HTTP client for talking to download clients and
// fetching release files: guarded dialler, timeouts, no proxies from the
// environment, and redirects only within the same host.
func NewHTTPClient(o HTTPOptions) *http.Client {
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Control: guardControl(o.AllowLinkLocal)}
	tr := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: o.Timeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   4,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: o.AllowSelfSigned}, // #nosec G402 -- explicit per-client opt-in
	}
	return &http.Client{
		Timeout:   o.Timeout,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
				return &CrossHostRedirect{Location: req.URL}
			}
			return nil
		},
	}
}

// CrossHostRedirect is returned when a response redirects to another host. A
// redirect to a magnet link is reported this way too (its "host" differs).
type CrossHostRedirect struct{ Location *url.URL }

func (e *CrossHostRedirect) Error() string {
	return "redirect to another host refused: " + e.Location.Redacted()
}

// ReadLimited reads at most limit bytes of r; more is ErrResponseTooLarge.
func ReadLimited(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%w (over %d bytes)", ErrResponseTooLarge, limit)
	}
	return b, nil
}

// MaxResponse is the default cap on a client API response (a big qBittorrent list is a few MB).
const MaxResponse = 32 << 20

// ClassifyTransportError maps a failed request to ErrUnreachable, ErrTLS or ErrBlocked.
// Messages never contain credentials (URLs are redacted by net/http).
func ClassifyTransportError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrBlocked) {
		return err
	}
	var (
		uerr  *x509.UnknownAuthorityError
		herr  x509.HostnameError
		cerr  *x509.CertificateInvalidError
		verr  *tls.CertificateVerificationError
		redir *CrossHostRedirect
	)
	if errors.As(err, &uerr) || errors.As(err, &herr) || errors.As(err, &cerr) || errors.As(err, &verr) {
		return fmt.Errorf("%w: %s", ErrTLS, stripURL(err))
	}
	if errors.As(err, &redir) {
		return fmt.Errorf("%w: %s", ErrWrongURL, redir.Error())
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: timed out", ErrUnreachable)
	}
	return fmt.Errorf("%w: %s", ErrUnreachable, stripURL(err))
}

// stripURL drops the URL part of a *url.Error (which may carry query
// parameters such as an API key) and keeps the cause.
func stripURL(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Op + ": " + ue.Err.Error()
	}
	return err.Error()
}

// StatusError maps an unexpected HTTP status to an error.
func StatusError(status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w (HTTP %d)", ErrAuth, status)
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return fmt.Errorf("%w (HTTP %d)", ErrWrongURL, status)
	}
	return ClientError("HTTP %d", status)
}
