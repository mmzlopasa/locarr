package znab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
)

// KindBlocked means the address is refused by the outbound policy.
const KindBlocked Kind = "blocked"

// Client calls one Newznab/Torznab endpoint.
type Client struct {
	HTTP    *httpx.Client
	BaseURL string // e.g. https://indexer.example or http://prowlarr:9696/3
	APIPath string // default "/api"
	APIKey  string
}

// Request is a search. Type decides the function; empty fields are not sent.
type Request struct {
	Type       SearchType
	Query      string
	Categories []int
	IMDBID     string // with or without "tt"
	TMDBID     int64
	TVDBID     int64
	TVMazeID   int64
	RageID     int64
	Season     string
	Episode    string
	Artist     string
	Album      string
	Author     string
	Title      string
	Year       int
	Offset     int
	Limit      int
}

// Endpoint is the API URL without parameters.
func (c *Client) Endpoint() string {
	p := c.APIPath
	if p == "" {
		p = "/api"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimRight(c.BaseURL, "/") + p
}

func (c *Client) url(v url.Values) string {
	if c.APIKey != "" {
		v.Set("apikey", c.APIKey)
	}
	ep := c.Endpoint()
	sep := "?"
	if strings.Contains(ep, "?") {
		sep = "&"
	}
	return ep + sep + v.Encode()
}

// Caps fetches t=caps.
func (c *Client) Caps(ctx context.Context) (*Caps, error) {
	body, err := c.get(ctx, url.Values{"t": {"caps"}})
	if err != nil {
		return nil, err
	}
	return ParseCaps(body)
}

// Values encodes r as API parameters (without the key).
func (r Request) Values() url.Values {
	v := url.Values{}
	t := r.Type
	if t == "" {
		t = SearchGeneric
	}
	v.Set("t", string(t))
	v.Set("extended", "1")
	set := func(k, s string) {
		if s = strings.TrimSpace(s); s != "" {
			v.Set(k, s)
		}
	}
	setN := func(k string, n int64) {
		if n > 0 {
			v.Set(k, strconv.FormatInt(n, 10))
		}
	}
	set("q", r.Query)
	if len(r.Categories) > 0 {
		cs := make([]string, len(r.Categories))
		for i, c := range r.Categories {
			cs[i] = strconv.Itoa(c)
		}
		v.Set("cat", strings.Join(cs, ","))
	}
	// The Newznab spec sends IMDb ids as digits without "tt".
	set("imdbid", strings.TrimPrefix(strings.ToLower(strings.TrimSpace(r.IMDBID)), "tt"))
	setN("tmdbid", r.TMDBID)
	setN("tvdbid", r.TVDBID)
	setN("tvmazeid", r.TVMazeID)
	setN("rid", r.RageID)
	set("season", r.Season)
	set("ep", r.Episode)
	set("artist", r.Artist)
	set("album", r.Album)
	set("author", r.Author)
	set("title", r.Title)
	setN("year", int64(r.Year))
	setN("offset", int64(r.Offset))
	setN("limit", int64(r.Limit))
	return v
}

// Search runs a search (an RSS fetch is a search without query or ids).
func (c *Client) Search(ctx context.Context, r Request) (*Page, error) {
	body, err := c.get(ctx, r.Values())
	if err != nil {
		return nil, err
	}
	return ParseFeed(body)
}

// Fetch downloads a release file (a .torrent or .nzb) from link. Redirects
// are followed to other hosts too (Prowlarr and Jackett hand downloads on to
// the tracker), without the indexer's credentials; a redirect to a magnet
// link returns that link in File.Magnet. The answer must be a .torrent, an
// NZB or a magnet link in plain text.
func (c *Client) Fetch(ctx context.Context, link string) (*httpx.ReleaseFile, error) {
	rf, err := c.HTTP.FetchRelease(ctx, link, nil)
	if err != nil {
		return nil, transportError(err)
	}
	if rf.Magnet != "" {
		return rf, nil
	}
	res := rf.Response
	if res.Status < 200 || res.Status > 299 {
		if e := detectError(res.Body); e != nil {
			var ze *Error
			if errors.As(e, &ze) && ze.RetryAfter == 0 {
				ze.RetryAfter, _ = strconv.Atoi(res.Header.Get("Retry-After"))
			}
			return nil, e
		}
		if res.Status == 404 || res.Status == 410 {
			return nil, &Error{Kind: KindNotFound, Code: res.Status, Message: fmt.Sprintf("HTTP %d: the release file is gone", res.Status)}
		}
		return nil, httpError(res.Status, res.Header.Get("Retry-After"))
	}
	if e := detectError(res.Body); e != nil {
		return nil, e
	}
	if httpx.ReleaseKind(res.Body) == "" {
		return nil, &Error{Kind: KindBadResponse, Message: httpx.ErrNotReleaseFile.Error() + " (" + describeBody(res) + ")"}
	}
	return rf, nil
}

// describeBody names what came back instead of a release file.
func describeBody(res *httpx.Response) string {
	ct := strings.ToLower(res.Header.Get("Content-Type"))
	head := strings.ToLower(string(res.Body[:min(len(res.Body), 512)]))
	switch {
	case len(res.Body) == 0:
		return "empty answer"
	case strings.Contains(ct, "html") || strings.Contains(head, "<html") || strings.Contains(head, "<!doctype html"):
		return "a web page"
	case ct != "":
		return ct
	}
	return "unknown content"
}

func (c *Client) get(ctx context.Context, v url.Values) ([]byte, error) {
	hdr := http.Header{"Accept": {"application/rss+xml, application/xml, text/xml;q=0.9, */*;q=0.5"}}
	res, err := c.HTTP.Get(ctx, c.url(v), hdr)
	if err != nil {
		return nil, transportError(err)
	}
	if res.Status < 200 || res.Status > 299 {
		if e := detectError(res.Body); e != nil {
			var ze *Error
			if errors.As(e, &ze) && ze.RetryAfter == 0 {
				ze.RetryAfter, _ = strconv.Atoi(res.Header.Get("Retry-After"))
			}
			return nil, e
		}
		return nil, httpError(res.Status, res.Header.Get("Retry-After"))
	}
	return res.Body, nil
}

func transportError(err error) error {
	kind := KindUnreachable
	switch {
	case errors.Is(err, httpx.ErrBlocked):
		kind = KindBlocked
	case errors.Is(err, httpx.ErrTooLarge), errors.Is(err, httpx.ErrCrossHostRedirect), errors.Is(err, httpx.ErrTooManyRedirects),
		errors.Is(err, httpx.ErrBadRedirect):
		kind = KindBadResponse
	case errors.Is(err, httpx.ErrChallenge):
		kind = KindAuth
	}
	return &Error{Kind: kind, Message: err.Error()}
}
