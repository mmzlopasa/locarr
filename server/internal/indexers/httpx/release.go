package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Release-file errors.
var (
	ErrTooManyRedirects = errors.New("too many redirects")
	ErrBadRedirect      = errors.New("redirect to an unsupported address")
	ErrNotReleaseFile   = errors.New("the answer is not a .torrent or .nzb file")
)

// MaxReleaseHops is how many redirects a release-file fetch follows.
const MaxReleaseHops = 5

// ReleaseFile is the outcome of a release-file fetch: either a response (any
// status; the caller checks it) or a magnet link the indexer redirected to.
type ReleaseFile struct {
	*Response
	// Magnet is set when a redirect pointed at a magnet: URI (nothing fetched).
	Magnet string
	// CrossHost reports that the file came from another host than the link's.
	CrossHost bool
}

// credentialParams are query parameters that carry the indexer's own key and
// never go to another host.
var credentialParams = map[string]bool{"apikey": true, "api_key": true, "jackett_apikey": true}

// FollowOptions configure FollowRelease.
type FollowOptions struct {
	// Header is sent only to the link's own host (API key headers, cookies).
	Header http.Header
	// Prepare may add host-specific state (cookies) to a request on the link's own host.
	Prepare func(*http.Request)
	// MaxBytes caps the body after decompression.
	MaxBytes int64
	// Secrets are scrubbed from errors and removed from links to other hosts.
	Secrets []string
}

// FollowRelease fetches a release file (.torrent or .nzb) and, unlike every
// other request, follows redirects to other hosts: Prowlarr and Jackett answer
// a download with a redirect to the tracker's own file or to a magnet link.
// Up to MaxReleaseHops hops; each hop goes through hc's transport, so every
// target address is checked against the outbound policy at connect time. A
// redirect to a magnet: URI stops and returns it. On a hop to another host
// only a plain GET goes out: no extra headers, no cookies, no user info and
// no API key parameters (nor any query value equal to a secret).
func FollowRelease(ctx context.Context, hc *http.Client, rawURL string, o FollowOptions) (*ReleaseFile, error) {
	if o.MaxBytes <= 0 {
		o.MaxBytes = DefaultMaxBytes
	}
	nc := *hc
	nc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	first, err := url.Parse(rawURL)
	if err != nil || (first.Scheme != "http" && first.Scheme != "https") || first.Host == "" {
		return nil, fmt.Errorf("invalid URL %s", ShortURL(rawURL, o.Secrets...))
	}
	cur := first
	for hop := 0; ; hop++ {
		same := strings.EqualFold(cur.Hostname(), first.Hostname())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cur.String(), nil)
		if err != nil {
			return nil, requestErr(err, cur.String(), o.Secrets)
		}
		req.Header.Set("User-Agent", userAgent)
		if same {
			for k, v := range o.Header {
				req.Header[k] = v
			}
			if o.Prepare != nil {
				o.Prepare(req)
			}
		}
		res, err := nc.Do(req)
		if err != nil {
			return nil, requestErr(err, cur.String(), o.Secrets)
		}
		if isRedirect(res.StatusCode) {
			loc := res.Header.Get("Location")
			_ = res.Body.Close()
			if loc == "" {
				return nil, requestErr(fmt.Errorf("%w: redirect without a location", ErrBadRedirect), cur.String(), o.Secrets)
			}
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(loc)), "magnet:") {
				return &ReleaseFile{Magnet: strings.TrimSpace(loc), CrossHost: true}, nil
			}
			next, err := cur.Parse(loc)
			if err != nil || (next.Scheme != "http" && next.Scheme != "https") || next.Host == "" {
				return nil, requestErr(ErrBadRedirect, cur.String(), o.Secrets)
			}
			if hop+1 > MaxReleaseHops {
				return nil, requestErr(ErrTooManyRedirects, cur.String(), o.Secrets)
			}
			if !strings.EqualFold(next.Hostname(), first.Hostname()) {
				next = stripCredentials(next, o.Secrets)
			}
			cur = next
			continue
		}
		body, err := ReadCapped(res.Body, o.MaxBytes)
		_ = res.Body.Close()
		if err != nil {
			return nil, requestErr(err, cur.String(), o.Secrets)
		}
		return &ReleaseFile{Response: &Response{Status: res.StatusCode, Header: res.Header, Body: body},
			CrossHost: !same}, nil
	}
}

func isRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

// stripCredentials removes what belongs to the indexer from a link to another host.
func stripCredentials(u *url.URL, secrets []string) *url.URL {
	out := *u
	out.User = nil
	q := out.Query()
	changed := false
	for k, vs := range q {
		drop := credentialParams[strings.ToLower(k)]
		for _, v := range vs {
			for _, s := range secrets {
				drop = drop || (len(s) >= 4 && v == s)
			}
		}
		if drop {
			q.Del(k)
			changed = true
		}
	}
	if changed {
		out.RawQuery = q.Encode()
	}
	return &out
}

func requestErr(err error, rawURL string, secrets []string) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return &RequestError{URL: ShortURL(rawURL, secrets...), Err: scrubErr{err, secrets}}
}

// FetchRelease fetches a release file with this client's policy, proxy, size
// cap and timeout, following redirects as FollowRelease does. A Cloudflare
// challenge on the indexer's own host goes through FlareSolverr like Do.
func (c *Client) FetchRelease(ctx context.Context, rawURL string, hdr http.Header) (*ReleaseFile, error) {
	o := FollowOptions{Header: hdr, MaxBytes: c.o.MaxBytes, Secrets: c.o.Secrets, Prepare: c.prepare}
	rf, err := FollowRelease(ctx, c.hc, rawURL, o)
	if err != nil || rf.Response == nil || rf.CrossHost || c.fsHC == nil || !isChallenge(rf.Response) {
		return rf, err
	}
	if err := c.solve(ctx, rawURL); err != nil {
		return nil, err
	}
	return FollowRelease(ctx, c.hc, rawURL, o)
}

// prepare adds FlareSolverr clearance for the request's host.
func (c *Client) prepare(req *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cf := c.cf; cf != nil && cf.host == req.URL.Host && time.Now().Before(cf.expires) {
		for _, ck := range cf.cookies {
			req.AddCookie(ck)
		}
		if cf.userAgent != "" {
			req.Header.Set("User-Agent", cf.userAgent)
		}
	}
}

// ReleaseKind tells what a fetched body is: "torrent" (bencoded dictionary),
// "nzb", "magnet" (a magnet link as plain text) or "" (anything else, such as
// a web page).
func ReleaseKind(body []byte) string {
	t := bytes.TrimSpace(body)
	switch {
	case len(body) > 1 && body[0] == 'd' && (body[1] >= '0' && body[1] <= '9' || body[1] == 'e'):
		return "torrent"
	case bytes.HasPrefix(bytes.ToLower(t), []byte("magnet:")):
		return "magnet"
	case bytes.Contains(bytes.ToLower(t[:min(len(t), 4096)]), []byte("<nzb")):
		return "nzb"
	}
	return ""
}

// ShortURL is a URL fit for an error message: scheme, host and path only. The
// query is dropped entirely (download links carry keys, encoded links and file
// names) and secrets are scrubbed from what is left.
func ShortURL(rawURL string, secrets ...string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		if i := strings.IndexAny(rawURL, "?#"); i >= 0 {
			rawURL = rawURL[:i]
		}
		return Scrub(rawURL, secrets...)
	}
	return Scrub((&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String(), secrets...)
}
