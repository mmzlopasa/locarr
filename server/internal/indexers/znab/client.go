package znab

import (
	"context"
	"errors"
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

// Fetch downloads a release file (a .torrent or .nzb) from link, which must be
// on the indexer's host or a host the indexer redirected to on the same host.
func (c *Client) Fetch(ctx context.Context, link string) (*httpx.Response, error) {
	res, err := c.HTTP.Get(ctx, link, nil)
	if err != nil {
		return nil, transportError(err)
	}
	if res.Status < 200 || res.Status > 299 {
		if e := detectError(res.Body); e != nil {
			return nil, e
		}
		return nil, httpError(res.Status, res.Header.Get("Retry-After"))
	}
	if e := detectError(res.Body); e != nil {
		return nil, e
	}
	return res, nil
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
	case errors.Is(err, httpx.ErrTooLarge), errors.Is(err, httpx.ErrCrossHostRedirect):
		kind = KindBadResponse
	case errors.Is(err, httpx.ErrChallenge):
		kind = KindAuth
	}
	return &Error{Kind: kind, Message: err.Error()}
}
