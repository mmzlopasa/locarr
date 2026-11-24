package native

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
)

// Client searches one site through its definition. It implements the same
// Caps/Search/Fetch contract as znab.Client.
type Client struct {
	Def  *Definition
	HTTP *httpx.Client
	// BaseURL is the site address in use (a mirror or a local copy); empty
	// uses the definition's default.
	BaseURL string
	// Config holds the per-indexer settings (missing ones take defaults).
	Config map[string]string
	// Trackers are added to magnets built from bare info hashes.
	Trackers []string
	Now      func() time.Time

	mu   sync.Mutex
	last time.Time
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return c.Def.DefaultURL()
}

func (c *Client) config() map[string]string {
	out := c.Def.SettingDefaults()
	for k, v := range c.Config {
		if _, ok := out[k]; ok {
			out[k] = v
		}
	}
	return out
}

var modeTypes = map[string]znab.SearchType{"search": znab.SearchGeneric, "tv": znab.SearchTV, "movie": znab.SearchMovie,
	"music": znab.SearchMusic, "book": znab.SearchBook}

// Standard Newznab category names, for the capabilities.
var newznabNames = map[int]string{
	2000: "Movies", 2010: "Movies/Foreign", 2020: "Movies/Other", 2030: "Movies/SD", 2040: "Movies/HD", 2045: "Movies/UHD",
	2050: "Movies/BluRay", 2060: "Movies/3D", 2070: "Movies/DVD", 2080: "Movies/WEB-DL",
	3000: "Audio", 3010: "Audio/MP3", 3020: "Audio/Video", 3030: "Audio/Audiobook", 3040: "Audio/Lossless", 3050: "Audio/Other",
	4000: "PC", 5000: "TV", 5010: "TV/WEB-DL", 5020: "TV/Foreign", 5030: "TV/SD", 5040: "TV/HD", 5045: "TV/UHD",
	5060: "TV/Sport", 5070: "TV/Anime", 5080: "TV/Documentary", 6000: "XXX", 7000: "Books", 7010: "Books/Mags",
	7020: "Books/EBook", 7030: "Books/Comics", 7040: "Books/Technical", 7050: "Books/Other", 8000: "Other",
}

// Caps describes the definition as Newznab capabilities (no request).
func (c *Client) Caps(context.Context) (*znab.Caps, error) {
	caps := &znab.Caps{ServerTitle: c.Def.Name, LimitMax: 100, LimitDefault: 100}
	for _, m := range modeNames {
		ps, ok := c.Def.Modes[m]
		if !ok {
			continue
		}
		caps.Searching = append(caps.Searching, znab.SearchCap{Type: modeTypes[m], Available: true, Params: slices.Clone(ps)})
	}
	byParent := map[int]*znab.Category{}
	var order []int
	add := func(id int) *znab.Category {
		if p, ok := byParent[id]; ok {
			return p
		}
		name := newznabNames[id]
		if name == "" {
			name = strconv.Itoa(id)
		}
		byParent[id] = &znab.Category{ID: id, Name: name}
		order = append(order, id)
		return byParent[id]
	}
	for _, cm := range c.Def.Categories {
		parent := cm.Cat - cm.Cat%1000
		p := add(parent)
		if cm.Cat != parent && !slices.ContainsFunc(p.Subcat, func(s znab.Category) bool { return s.ID == cm.Cat }) {
			name := newznabNames[cm.Cat]
			if name == "" {
				name = cm.Desc
			}
			p.Subcat = append(p.Subcat, znab.Category{ID: cm.Cat, Name: name})
		}
	}
	slices.Sort(order)
	for _, id := range order {
		caps.Categories = append(caps.Categories, *byParent[id])
	}
	return caps, nil
}

// Query is a search translated for a site.
type Query struct {
	Mode     string // search, tv, movie, music, book or rss
	Keywords string // the text to send: query plus season/episode tokens
	Query    string // the text as asked
	IMDBID   string // tt0123456
	IMDBNum  string // 0123456
	TMDBID   int64
	TVDBID   int64
	Season   string
	Episode  string
	Year     int
	// Categories are the site's category ids for the asked Newznab ids
	// (empty: all).
	Categories []string
	Newznab    []int
	Offset     int
	Limit      int
	Page       int
	Config     map[string]string
}

// TestRequest is the known search a test runs.
func (c *Client) TestRequest() znab.Request {
	r := znab.Request{Type: modeTypes[c.Def.Test.Mode], Query: c.Def.Test.Query, IMDBID: c.Def.Test.IMDBID, Limit: 10}
	return r
}

// translate turns a Newznab request into a site query.
func (c *Client) translate(r znab.Request) Query {
	q := Query{Query: strings.TrimSpace(r.Query), TMDBID: r.TMDBID, TVDBID: r.TVDBID, Season: r.Season, Episode: r.Episode,
		Year: r.Year, Offset: r.Offset, Limit: r.Limit, Config: c.config(), Newznab: r.Categories}
	q.IMDBID = ParseIMDB(r.IMDBID)
	q.IMDBNum = strings.TrimPrefix(q.IMDBID, "tt")
	for m, t := range modeTypes {
		if t == r.Type {
			q.Mode = m
		}
	}
	if q.Mode == "" {
		q.Mode = "search"
	}
	q.Keywords = q.Query
	switch {
	case len(r.Season) == 4 && strings.Contains(r.Episode, "/"): // daily shows
		q.Keywords += " " + r.Season + " " + strings.ReplaceAll(r.Episode, "/", " ")
	case r.Season != "" && r.Episode != "":
		s, _ := strconv.Atoi(r.Season)
		e, _ := strconv.Atoi(r.Episode)
		q.Keywords += fmt.Sprintf(" S%02dE%02d", s, e)
	case r.Season != "":
		s, _ := strconv.Atoi(r.Season)
		q.Keywords += fmt.Sprintf(" S%02d", s)
	}
	q.Keywords = strings.TrimSpace(q.Keywords)
	if q.Keywords == "" && q.IMDBID == "" && q.TMDBID == 0 && q.TVDBID == 0 && q.Mode == "search" {
		q.Mode = "rss"
	}
	q.Categories = c.siteCategories(r.Categories)
	return q
}

// siteCategories maps Newznab ids onto the site's ids: a parent id (5000)
// takes every site category under it.
func (c *Client) siteCategories(cats []int) []string {
	if len(cats) == 0 {
		return nil
	}
	var out []string
	for _, cm := range c.Def.Categories {
		for _, want := range cats {
			if cm.Cat == want || (want%1000 == 0 && cm.Cat-cm.Cat%1000 == want) {
				if !slices.Contains(out, cm.ID) {
					out = append(out, cm.ID)
				}
				break
			}
		}
	}
	return out
}

// newznabFor maps a site category (id or label) onto Newznab ids (the
// category and its parent).
func (c *Client) newznabFor(site string) []int {
	site = strings.TrimSpace(site)
	if site == "" {
		return nil
	}
	for _, part := range strings.Split(site, ",") {
		part = strings.TrimSpace(part)
		for _, cm := range c.Def.Categories {
			if cm.ID == part || strings.EqualFold(cm.Desc, part) {
				out := []int{cm.Cat}
				if p := cm.Cat - cm.Cat%1000; p != cm.Cat {
					out = append(out, p)
				}
				return out
			}
		}
	}
	return nil
}

// Search runs a search. An empty request is an RSS fetch of the latest
// releases (empty when the site has no such list).
func (c *Client) Search(ctx context.Context, r znab.Request) (*znab.Page, error) {
	q := c.translate(r)
	var (
		items   []znab.Item
		dropped int
		err     error
	)
	if c.Def.Impl != "" {
		im := impls[c.Def.Impl]
		if q.Mode == "rss" && !im.rss {
			return &znab.Page{Total: 0}, nil
		}
		items, dropped, err = im.search(ctx, c, q)
	} else {
		items, dropped, err = c.declarative(ctx, q)
	}
	if err != nil {
		return nil, err
	}
	items = c.filter(items, q)
	if q.Limit > 0 && len(items) > q.Limit {
		items = items[:q.Limit]
	}
	return &znab.Page{Items: items, Total: -1, Dropped: dropped}, nil
}

// filter drops results outside the asked categories (sites that ignore the
// category parameter) and, for id searches, other episodes.
func (c *Client) filter(items []znab.Item, q Query) []znab.Item {
	out := items[:0]
	for _, it := range items {
		if len(q.Newznab) > 0 {
			if len(it.Categories) > 0 && !catMatch(it.Categories, q.Newznab) {
				continue
			}
			if len(it.Categories) == 0 && it.Attrs["sitecategory"] != nil {
				continue // a site category Locarr does not search (games, software, …)
			}
		}
		out = append(out, it)
	}
	return out
}

func catMatch(have, want []int) bool {
	for _, h := range have {
		for _, w := range want {
			if h == w || (w%1000 == 0 && h-h%1000 == w) {
				return true
			}
		}
	}
	return false
}

func (c *Client) declarative(ctx context.Context, q Query) ([]znab.Item, int, error) {
	cb := c.Def.compiled.search
	if q.Mode == "rss" {
		if c.Def.compiled.rss != nil {
			cb = c.Def.compiled.rss
		} else if !c.Def.SupportsRSS() {
			return nil, 0, nil
		}
	}
	req, ok := pickRequest(cb, q)
	if !ok {
		if q.Mode == "rss" {
			return nil, 0, nil
		}
		return nil, 0, &znab.Error{Kind: znab.KindUnsupported, Message: "the site cannot search by these terms"}
	}
	pages := max(cb.b.Pages, 1)
	if q.Mode == "rss" {
		pages = 1
	}
	var (
		items   []znab.Item
		dropped int
	)
	for p := 1; p <= pages; p++ {
		pq := q
		pq.Page = p
		pq.Offset = q.Offset + (p-1)*max(cb.b.PageSize, 1)
		res, err := c.send(ctx, req, pq)
		if err != nil {
			if p > 1 {
				break // keep the pages already read
			}
			return nil, 0, err
		}
		rows, err := c.rows(cb, res.Body)
		if err != nil {
			return nil, 0, err
		}
		for _, rw := range rows {
			it, ok, skip := c.item(cb, rw)
			if skip {
				continue
			}
			if !ok {
				dropped++
				continue
			}
			items = append(items, it)
		}
		if len(rows) > 0 && dropped == len(rows) && len(items) == 0 {
			return nil, 0, layoutError("no result row could be read (title or link missing)")
		}
		if cb.b.PageSize <= 0 || len(rows) < cb.b.PageSize {
			break
		}
	}
	return items, dropped, nil
}

func layoutError(msg string) error {
	return &znab.Error{Kind: znab.KindLayout, Message: "the site's page layout seems to have changed: " + msg}
}

// pickRequest returns the first request whose conditions hold.
func pickRequest(cb *compiledBlock, q Query) (compiledRequest, bool) {
	for _, r := range cb.requests {
		if conditionsHold(r.r.If, q) {
			return r, true
		}
	}
	return compiledRequest{}, false
}

func conditionsHold(conds []string, q Query) bool {
	for _, cond := range conds {
		neg := strings.HasPrefix(cond, "!")
		var set bool
		switch strings.TrimPrefix(cond, "!") {
		case "keywords":
			set = q.Keywords != ""
		case "query":
			set = q.Query != ""
		case "imdbid":
			set = q.IMDBID != ""
		case "tmdbid":
			set = q.TMDBID > 0
		case "tvdbid":
			set = q.TVDBID > 0
		case "season":
			set = q.Season != ""
		case "episode":
			set = q.Episode != ""
		case "categories":
			set = len(q.Categories) > 0
		}
		if set == neg {
			return false
		}
	}
	return true
}

// send performs one templated request.
func (c *Client) send(ctx context.Context, cr compiledRequest, q Query) (*httpx.Response, error) {
	var path bytes.Buffer
	if err := cr.path.Execute(&path, q); err != nil {
		return nil, &znab.Error{Kind: znab.KindBadResponse, Message: "definition template: " + err.Error()}
	}
	var body []byte
	hdr := http.Header{}
	if cr.body != nil {
		var b bytes.Buffer
		if err := cr.body.Execute(&b, q); err != nil {
			return nil, &znab.Error{Kind: znab.KindBadResponse, Message: "definition template: " + err.Error()}
		}
		body = b.Bytes()
		hdr.Set("Content-Type", "application/json")
	}
	for k, t := range cr.headers {
		var b bytes.Buffer
		if err := t.Execute(&b, q); err != nil {
			return nil, &znab.Error{Kind: znab.KindBadResponse, Message: "definition template: " + err.Error()}
		}
		hdr.Set(k, b.String())
	}
	return c.Do(ctx, cr.r.Method, c.base()+path.String(), hdr, body)
}

// Do sends one request to the site, pacing requests by the definition's
// delay, and classifies failures.
func (c *Client) Do(ctx context.Context, method, rawURL string, hdr http.Header, body []byte) (*httpx.Response, error) {
	if err := c.pace(ctx); err != nil {
		return nil, err
	}
	if hdr == nil {
		hdr = http.Header{}
	}
	if hdr.Get("Accept") == "" {
		hdr.Set("Accept", "text/html,application/xhtml+xml,application/xml,application/json;q=0.9,*/*;q=0.8")
	}
	res, err := c.HTTP.Do(ctx, method, rawURL, hdr, body)
	if err != nil {
		return nil, transportError(err)
	}
	if httpx.IsChallenge(res) {
		return nil, &znab.Error{Kind: znab.KindChallenge, Code: res.Status,
			Message: "the site answers with a browser check; start the built-in browser (" + httpx.BrowserCommand + ") or set a FlareSolverr URL for this indexer"}
	}
	if res.Status < 200 || res.Status > 299 {
		return nil, statusError(res)
	}
	return res, nil
}

func (c *Client) pace(ctx context.Context) error {
	gap := time.Duration(c.Def.DelayMillis) * time.Millisecond
	if gap <= 0 {
		return nil
	}
	c.mu.Lock()
	wait := c.last.Add(gap).Sub(c.now())
	next := c.now()
	if wait > 0 {
		next = next.Add(wait)
	}
	c.last = next
	c.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func transportError(err error) error {
	kind := znab.KindUnreachable
	switch {
	case errors.Is(err, httpx.ErrBlocked):
		kind = znab.KindBlocked
	case errors.Is(err, httpx.ErrTooLarge), errors.Is(err, httpx.ErrCrossHostRedirect):
		kind = znab.KindBadResponse
	case errors.Is(err, httpx.ErrChallenge):
		kind = znab.KindChallenge
	}
	return &znab.Error{Kind: kind, Message: err.Error()}
}

func statusError(res *httpx.Response) error {
	e := &znab.Error{Code: res.Status, Message: fmt.Sprintf("the site answered HTTP %d", res.Status)}
	switch {
	case res.Status == 429:
		e.Kind = znab.KindRateLimit
		e.RetryAfter, _ = strconv.Atoi(strings.TrimSpace(res.Header.Get("Retry-After")))
		e.Message = "the site limits requests (HTTP 429)"
	case res.Status == 401 || res.Status == 403:
		e.Kind = znab.KindAuth
		e.Message = fmt.Sprintf("the site refused the request (HTTP %d); it may block this server or need a proxy", res.Status)
	case res.Status == 404:
		e.Kind = znab.KindBadResponse
		e.Message = "HTTP 404: the site address or its search page changed"
	case res.Status >= 500:
		e.Kind = znab.KindUnreachable
		e.Message = fmt.Sprintf("the site is down (HTTP %d)", res.Status)
	default:
		e.Kind = znab.KindUnknown
	}
	return e
}

// rows parses a response into result rows.
func (c *Client) rows(cb *compiledBlock, body []byte) ([]row, error) {
	b := cb.b
	switch b.Response {
	case "json":
		doc, err := decodeJSON(body)
		if err != nil {
			return nil, layoutError("the answer is not JSON")
		}
		v, ok := jsonPath(doc, b.Rows)
		if !ok || v == nil {
			if b.Empty != "" {
				if _, found := jsonPath(doc, b.Empty); found {
					return nil, nil
				}
			}
			if v == nil && ok {
				return nil, nil // "results": null
			}
			return nil, layoutError(fmt.Sprintf("no %q in the answer", b.Rows))
		}
		arr, ok := jsonArray(v)
		if !ok {
			return nil, layoutError(fmt.Sprintf("%q is not a list", b.Rows))
		}
		var out []row
		for _, el := range arr {
			if b.SubRows == "" {
				out = append(out, row{json: el})
				continue
			}
			sv, _ := jsonPath(el, b.SubRows)
			subs, _ := jsonArray(sv)
			for _, s := range subs {
				out = append(out, row{json: s, parent: el})
			}
		}
		return out, nil
	default:
		var (
			root *html.Node
			err  error
		)
		if b.Response == "xml" {
			root, err = parseXML(body)
		} else {
			root, err = parseHTML(body)
		}
		if err != nil {
			return nil, layoutError("the answer cannot be parsed")
		}
		nodes := selectAll(root, b.Rows)
		if len(nodes) == 0 {
			if b.Empty != "" && selectOne(root, b.Empty) != nil {
				return nil, nil
			}
			if b.Response == "xml" && (selectOne(root, "channel") != nil || selectOne(root, "feed") != nil) {
				return nil, nil // an RSS/Atom feed without items
			}
			return nil, layoutError(fmt.Sprintf("no rows match %q", b.Rows))
		}
		out := make([]row, len(nodes))
		for i, n := range nodes {
			out[i] = row{node: n}
		}
		return out, nil
	}
}

// field evaluates one field of a row.
func (cb *compiledBlock) field(name string, r row) string {
	cf, ok := cb.fields[name]
	if !ok {
		return ""
	}
	v, _ := r.raw(cf.f)
	if cf.regex != nil && v != "" {
		m := cf.regex.FindStringSubmatch(v)
		switch {
		case m == nil:
			v = ""
		case len(m) > 1:
			v = m[1]
		default:
			v = m[0]
		}
	}
	for _, rp := range cf.f.Replace {
		v = strings.ReplaceAll(v, rp[0], rp[1])
	}
	v = strings.TrimSpace(v)
	if v == "" {
		v = cf.f.Default
	}
	return v
}

// item builds a feed item from a row. ok is false when the row lacks a
// title or any way to download it (a layout problem); skip is true when the
// definition says to leave the row out.
func (c *Client) item(cb *compiledBlock, r row) (it znab.Item, ok, skip bool) {
	vals := map[string]string{}
	for name := range cb.fields {
		vals[name] = cb.field(name, r)
	}
	for name, re := range cb.skip {
		if re.MatchString(vals[name]) {
			return it, false, true
		}
	}
	var layouts []string
	if f, ok := cb.fields["date"]; ok {
		layouts = f.f.Layouts
	}
	return c.build(vals, layouts)
}

// build turns field values into an item (shared with Go implementations).
func (c *Client) build(vals map[string]string, dateLayouts []string) (it znab.Item, ok, skip bool) {
	it.Title = strings.Join(strings.Fields(vals["title"]), " ")
	if it.Title == "" {
		return it, false, false
	}
	it.InfoURL = c.resolve(vals["details"])
	download := c.resolve(vals["download"])
	magnet := ""
	if m := vals["magnet"]; strings.HasPrefix(strings.ToLower(m), "magnet:") {
		magnet = m
	}
	if strings.HasPrefix(strings.ToLower(download), "magnet:") {
		magnet, download = download, ""
	}
	it.InfoHash = NormalizeHash(vals["infohash"])
	if it.InfoHash == "" && magnet != "" {
		it.InfoHash = NormalizeHash(magnet)
	}
	if magnet == "" && it.InfoHash != "" {
		magnet = Magnet(it.InfoHash, it.Title, c.Trackers)
	}
	// Only links on the site itself are fetched (the indexer service refuses
	// others); a magnet makes a foreign .torrent link unnecessary.
	if download != "" && !c.onSite(download) {
		download = ""
	}
	if download == "" && magnet == "" && it.InfoURL != "" && c.Def.Download != nil {
		download = it.InfoURL
	}
	if download == "" && magnet == "" {
		return it, false, false
	}
	it.Link, it.MagnetURL = download, magnet
	switch {
	case it.InfoURL != "":
		it.GUID = it.InfoURL
	case it.InfoHash != "":
		it.GUID = c.Def.ID + ":" + it.InfoHash
	default:
		it.GUID = download
	}
	it.Size = ParseSize(vals["size"])
	it.PublishDate = ParseDate(vals["date"], dateLayouts, c.now())
	setInt := func(dst **int, s string) {
		if n := ParseInt(s); n >= 0 {
			*dst = &n
		}
	}
	setInt(&it.Seeders, vals["seeders"])
	setInt(&it.Leechers, vals["leechers"])
	setInt(&it.Peers, vals["peers"])
	setInt(&it.Grabs, vals["grabs"])
	setInt(&it.Files, vals["files"])
	if it.Peers == nil && it.Seeders != nil && it.Leechers != nil {
		p := *it.Seeders + *it.Leechers
		it.Peers = &p
	}
	it.Categories = c.newznabFor(vals["category"])
	it.Attrs = map[string][]string{}
	if it.Categories == nil && vals["category"] != "" {
		it.Attrs["sitecategory"] = []string{vals["category"]}
	}
	it.IMDBID = ParseIMDB(vals["imdbid"])
	it.TVDBID, _ = strconv.ParseInt(vals["tvdbid"], 10, 64)
	it.TMDBID, _ = strconv.ParseInt(vals["tmdbid"], 10, 64)
	it.Season, it.Episode = vals["season"], vals["episode"]
	it.Year, _ = strconv.Atoi(vals["year"])
	if u := vals["uploader"]; u != "" {
		it.Attrs["uploader"] = []string{u}
	}
	return it, true, false
}

// resolve makes a link absolute against the site address.
func (c *Client) resolve(link string) string {
	link = strings.TrimSpace(link)
	if link == "" || strings.HasPrefix(strings.ToLower(link), "magnet:") {
		return link
	}
	if strings.HasPrefix(link, "//") {
		return "https:" + link
	}
	base, err := url.Parse(c.base() + "/")
	if err != nil {
		return link
	}
	ref, err := url.Parse(link)
	if err != nil {
		return ""
	}
	if !ref.IsAbs() && strings.HasPrefix(link, "/") && base.Path != "/" {
		// A site served under a path prefix (a mirror or a local copy).
		ref.Path = strings.TrimRight(base.Path, "/") + ref.Path
		ref.RawPath = ""
	}
	return base.ResolveReference(ref).String()
}

func (c *Client) onSite(link string) bool {
	u, err := url.Parse(link)
	b, berr := url.Parse(c.base())
	return err == nil && berr == nil && strings.EqualFold(u.Hostname(), b.Hostname())
}

// Fetch gets a release at grab time: with a download block, the details
// page is read for its magnet (returned as the body) or .torrent link;
// otherwise link is fetched as a .torrent.
func (c *Client) Fetch(ctx context.Context, link string) (*httpx.ReleaseFile, error) {
	if c.Def.Download == nil {
		return c.torrent(ctx, link)
	}
	res, err := c.Do(ctx, http.MethodGet, link, nil, nil)
	if err != nil {
		return nil, err
	}
	target := ""
	d := c.Def.Download
	if d.Select != "" {
		doc, err := parseHTML(res.Body)
		if err != nil {
			return nil, layoutError("the details page cannot be parsed")
		}
		if n := selectOne(doc, d.Select); n != nil {
			if d.Attr != "" {
				target, _ = attr(n, d.Attr)
			} else {
				target = nodeText(n)
			}
		}
	} else {
		target = string(res.Body)
	}
	if re := c.Def.compiled.download; re != nil && target != "" {
		if m := re.FindStringSubmatch(target); len(m) > 1 {
			target = m[1]
		} else if m != nil {
			target = m[0]
		} else {
			target = ""
		}
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, layoutError("no download link on the details page")
	}
	if strings.HasPrefix(strings.ToLower(target), "magnet:") {
		return &httpx.ReleaseFile{Magnet: target}, nil
	}
	abs := c.resolve(target)
	if !c.onSite(abs) {
		return nil, &znab.Error{Kind: znab.KindBadResponse, Message: "the download link points to another site"}
	}
	return c.torrent(ctx, abs)
}

// torrent fetches a .torrent from the site.
func (c *Client) torrent(ctx context.Context, link string) (*httpx.ReleaseFile, error) {
	res, err := c.Do(ctx, http.MethodGet, link, http.Header{"Accept": {"application/x-bittorrent, */*;q=0.5"}}, nil)
	if err != nil {
		return nil, err
	}
	if httpx.ReleaseKind(res.Body) == "" {
		return nil, &znab.Error{Kind: znab.KindBadResponse, Message: httpx.ErrNotReleaseFile.Error()}
	}
	return &httpx.ReleaseFile{Response: res}, nil
}
