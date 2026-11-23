package native

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
)

// impl is a Go implementation for a site with a real API.
type impl struct {
	rss    bool // can list the latest releases
	search func(ctx context.Context, c *Client, q Query) (items []znab.Item, dropped int, err error)
}

// impls are the Go implementations a definition can name.
var impls = map[string]impl{}

func init() {
	impls["yts"] = impl{rss: true, search: searchYTS}
	impls["eztv"] = impl{rss: true, search: searchEZTV}
	impls["apibay"] = impl{rss: true, search: searchAPIBay}
	impls["subsplease"] = impl{rss: true, search: searchSubsPlease}
	impls["knaben"] = impl{rss: true, search: searchKnaben}
}

// getJSON fetches a path of the site and decodes the JSON answer.
func (c *Client) getJSON(ctx context.Context, method, path string, body []byte) (any, error) {
	hdr := http.Header{"Accept": {"application/json"}}
	if body != nil {
		hdr.Set("Content-Type", "application/json")
	}
	res, err := c.Do(ctx, method, c.base()+path, hdr, body)
	if err != nil {
		return nil, err
	}
	v, err := decodeJSON(res.Body)
	if err != nil {
		return nil, layoutError("the answer is not JSON")
	}
	return v, nil
}

func str(v any, path string) string {
	x, ok := jsonPath(v, path)
	if !ok {
		return ""
	}
	return strings.TrimSpace(jsonString(x))
}

func list(v any, path string) []any {
	x, _ := jsonPath(v, path)
	arr, _ := jsonArray(x)
	return arr
}

func wantEpisode(q Query, season, episode string) bool {
	if q.Season == "" {
		return true
	}
	if atoi(season) != atoi(q.Season) {
		return false
	}
	return q.Episode == "" || atoi(episode) == atoi(q.Episode)
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimLeft(strings.TrimSpace(s), "0"))
	if err != nil {
		return 0
	}
	return n
}

// ---- YTS: the official JSON API (list_movies.json), movies only. ----

func searchYTS(ctx context.Context, c *Client, q Query) ([]znab.Item, int, error) {
	v := url.Values{"limit": {"50"}, "sort_by": {"date_added"}}
	switch {
	case q.IMDBID != "":
		v.Set("query_term", q.IMDBID)
	case q.Query != "":
		v.Set("query_term", q.Query)
	case q.Mode != "rss":
		return nil, 0, &znab.Error{Kind: znab.KindUnsupported, Message: "YTS searches movies by IMDb id or title"}
	}
	if q.Mode == "tv" || q.Mode == "music" || q.Mode == "book" {
		return nil, 0, nil
	}
	doc, err := c.getJSON(ctx, http.MethodGet, "/api/v2/list_movies.json?"+v.Encode(), nil)
	if err != nil {
		return nil, 0, err
	}
	if s := str(doc, "status"); s != "" && s != "ok" {
		return nil, 0, &znab.Error{Kind: znab.KindUnknown, Message: "YTS: " + str(doc, "status_message")}
	}
	if _, ok := jsonPath(doc, "data"); !ok {
		return nil, 0, layoutError("no data in the answer")
	}
	var (
		out     []znab.Item
		dropped int
	)
	for _, m := range list(doc, "data.movies") {
		year := str(m, "year")
		for _, t := range list(m, "torrents") {
			quality := str(t, "quality")
			src := map[string]string{"bluray": "BluRay", "web": "WEBRip"}[strings.ToLower(str(t, "type"))]
			codec := str(t, "video_codec")
			if codec == "" {
				codec = "x264"
			}
			title := strings.Join(strings.Fields(strings.Join([]string{str(m, "title_english"), year, quality, src, codec}, " ")), " ") + "-YTS"
			if str(m, "title_english") == "" {
				title = strings.Join(strings.Fields(strings.Join([]string{str(m, "title"), year, quality, src, codec}, " ")), " ") + "-YTS"
			}
			vals := map[string]string{"title": title, "details": str(m, "url"), "infohash": str(t, "hash"),
				"size": str(t, "size_bytes"), "date": str(t, "date_uploaded_unix"), "seeders": str(t, "seeds"),
				"leechers": str(t, "peers"), "imdbid": str(m, "imdb_code"), "year": year, "category": "movies"}
			if vals["size"] == "" || vals["size"] == "0" {
				vals["size"] = str(t, "size")
			}
			it, ok, _ := c.build(vals, nil)
			if !ok {
				dropped++
				continue
			}
			it.GUID = "yts:" + it.InfoHash
			out = append(out, it)
		}
	}
	return out, dropped, nil
}

// ---- EZTV: the public JSON API (get-torrents), TV by IMDb id or latest. ----

func searchEZTV(ctx context.Context, c *Client, q Query) ([]znab.Item, int, error) {
	v := url.Values{"limit": {"100"}, "page": {"1"}}
	switch {
	case q.IMDBNum != "":
		v.Set("imdb_id", strings.TrimLeft(q.IMDBNum, "0"))
	case q.Mode == "rss":
	default:
		return nil, 0, &znab.Error{Kind: znab.KindUnsupported, Message: "EZTV searches only by IMDb id"}
	}
	if q.Mode == "movie" || q.Mode == "music" || q.Mode == "book" {
		return nil, 0, nil
	}
	doc, err := c.getJSON(ctx, http.MethodGet, "/api/get-torrents?"+v.Encode(), nil)
	if err != nil {
		return nil, 0, err
	}
	if _, ok := jsonPath(doc, "torrents_count"); !ok {
		return nil, 0, layoutError("no torrents_count in the answer")
	}
	var (
		out     []znab.Item
		dropped int
	)
	for _, t := range list(doc, "torrents") {
		if !wantEpisode(q, str(t, "season"), str(t, "episode")) {
			continue
		}
		title := strings.TrimSpace(strings.TrimSuffix(str(t, "title"), " EZTV"))
		vals := map[string]string{"title": title, "magnet": str(t, "magnet_url"), "infohash": str(t, "hash"),
			"size": str(t, "size_bytes"), "date": str(t, "date_released_unix"), "seeders": str(t, "seeds"),
			"peers": str(t, "peers"), "imdbid": str(t, "imdb_id"), "season": str(t, "season"),
			"episode": str(t, "episode"), "category": "tv"}
		if id := str(t, "id"); id != "" {
			vals["details"] = "/ep/" + id + "/"
		}
		it, ok, _ := c.build(vals, nil)
		if !ok {
			dropped++
			continue
		}
		if s := it.Seeders; s != nil && it.Peers != nil && *it.Peers >= *s {
			l := *it.Peers - *s
			it.Leechers = &l
		}
		out = append(out, it)
	}
	return out, dropped, nil
}

// ---- The Pirate Bay: the JSON API its own site uses (apibay). ----

func searchAPIBay(ctx context.Context, c *Client, q Query) ([]znab.Item, int, error) {
	path := ""
	switch {
	case q.Mode == "rss":
		path = "/precompiled/data_top100_recent.json"
	case q.IMDBID != "" && q.Mode == "movie":
		path = "/q.php?" + url.Values{"q": {q.IMDBID}, "cat": {strings.Join(q.Categories, ",")}}.Encode()
	case q.Keywords != "":
		path = "/q.php?" + url.Values{"q": {q.Keywords}, "cat": {strings.Join(q.Categories, ",")}}.Encode()
	case q.IMDBID != "":
		path = "/q.php?" + url.Values{"q": {q.IMDBID}, "cat": {strings.Join(q.Categories, ",")}}.Encode()
	default:
		return nil, 0, &znab.Error{Kind: znab.KindUnsupported, Message: "The Pirate Bay needs search words or an IMDb id"}
	}
	doc, err := c.getJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, 0, err
	}
	rows, ok := doc.([]any)
	if !ok {
		return nil, 0, layoutError("the answer is not a list")
	}
	var (
		out     []znab.Item
		dropped int
	)
	for _, t := range rows {
		hash := str(t, "info_hash")
		if str(t, "id") == "0" || strings.Trim(hash, "0") == "" {
			continue // the "No results returned" placeholder
		}
		vals := map[string]string{"title": str(t, "name"), "infohash": hash, "size": str(t, "size"),
			"date": str(t, "added"), "seeders": str(t, "seeders"), "leechers": str(t, "leechers"),
			"files": str(t, "num_files"), "category": str(t, "category"), "imdbid": str(t, "imdb"),
			"uploader": str(t, "username")}
		if id := str(t, "id"); id != "" {
			vals["details"] = "https://thepiratebay.org/description.php?id=" + id
		}
		it, ok, _ := c.build(vals, nil)
		if !ok {
			dropped++
			continue
		}
		it.GUID = "tpb:" + str(t, "id")
		if st := str(t, "status"); st == "vip" || st == "trusted" {
			it.Tags = append(it.Tags, st)
		}
		out = append(out, it)
	}
	return out, dropped, nil
}

// ---- SubsPlease: the public JSON API its site uses (f=search, f=latest). ----

func searchSubsPlease(ctx context.Context, c *Client, q Query) ([]znab.Item, int, error) {
	if q.Mode == "movie" || q.Mode == "music" || q.Mode == "book" {
		return nil, 0, nil
	}
	v := url.Values{"tz": {"UTC"}}
	if q.Mode == "rss" {
		v.Set("f", "latest")
	} else {
		if q.Query == "" {
			return nil, 0, &znab.Error{Kind: znab.KindUnsupported, Message: "SubsPlease searches by title"}
		}
		v.Set("f", "search")
		v.Set("s", subsPleaseTerms(q))
	}
	doc, err := c.getJSON(ctx, http.MethodGet, "/api/?"+v.Encode(), nil)
	if err != nil {
		return nil, 0, err
	}
	shows, ok := jsonArray(doc)
	if !ok {
		if arr, isArr := doc.([]any); isArr && len(arr) == 0 {
			return nil, 0, nil // no results is an empty array
		}
		return nil, 0, layoutError("unexpected answer")
	}
	res := c.config()["resolution"]
	var (
		out     []znab.Item
		dropped int
	)
	for _, s := range shows {
		for _, d := range list(s, "downloads") {
			if res != "" && res != "all" && str(d, "res") != res {
				continue
			}
			magnet := str(d, "magnet")
			title := ""
			if u, err := url.Parse(magnet); err == nil {
				title = u.Query().Get("dn")
			}
			if title == "" {
				title = fmt.Sprintf("[SubsPlease] %s - %s (%sp)", str(s, "show"), str(s, "episode"), str(d, "res"))
			}
			size := ""
			if u, err := url.Parse(magnet); err == nil {
				size = u.Query().Get("xl")
			}
			vals := map[string]string{"title": title, "magnet": magnet, "size": size, "date": str(s, "release_date"),
				"category": "anime"}
			if page := str(s, "page"); page != "" {
				vals["details"] = "/shows/" + page + "/"
			}
			it, ok, _ := c.build(vals, nil)
			if !ok {
				dropped++
				continue
			}
			it.GUID = "subsplease:" + it.InfoHash
			out = append(out, it)
		}
	}
	return out, dropped, nil
}

// subsPleaseTerms is the title plus an episode number: SubsPlease names
// episodes "Show - 05" (absolute numbering is already in the query text).
func subsPleaseTerms(q Query) string {
	t := q.Query
	if q.Episode != "" {
		t += " " + twoDigits(atoi(q.Episode))
	}
	return t
}

// ---- Knaben: the documented public search API (POST /v1). ----

func searchKnaben(ctx context.Context, c *Client, q Query) ([]znab.Item, int, error) {
	body := map[string]any{"size": 100, "hide_unsafe": true, "hide_xxx": true}
	if q.Mode == "rss" {
		body["order_by"] = "date"
		body["order_direction"] = "desc"
	} else {
		if q.Keywords == "" {
			return nil, 0, &znab.Error{Kind: znab.KindUnsupported, Message: "Knaben searches by title"}
		}
		body["search_type"] = "100%"
		body["search_field"] = "title"
		body["query"] = q.Keywords
		body["order_by"] = "seeders"
		body["order_direction"] = "desc"
	}
	if len(q.Categories) > 0 {
		ids := make([]int, 0, len(q.Categories))
		for _, s := range q.Categories {
			if n, err := strconv.Atoi(s); err == nil {
				ids = append(ids, n)
			}
		}
		body["categories"] = ids
	}
	raw, _ := json.Marshal(body)
	doc, err := c.getJSON(ctx, http.MethodPost, "/v1", raw)
	if err != nil {
		return nil, 0, err
	}
	if _, ok := jsonPath(doc, "hits"); !ok {
		return nil, 0, layoutError("no hits in the answer")
	}
	var (
		out     []znab.Item
		dropped int
	)
	for _, h := range list(doc, "hits") {
		cats := list(h, "categoryId")
		cat := ""
		for _, x := range cats {
			// The most specific id first (the API lists it before its parent).
			if s := jsonString(x); s != "" {
				cat = s
				break
			}
		}
		if str(h, "magnetUrl") == "" && str(h, "hash") == "" {
			continue // only a link through another site: not grabbable from here
		}
		date := str(h, "date")
		if date == "" {
			date = str(h, "lastSeen")
		}
		vals := map[string]string{"title": str(h, "title"), "magnet": str(h, "magnetUrl"), "infohash": str(h, "hash"),
			"details": str(h, "details"), "size": str(h, "bytes"), "date": date, "seeders": str(h, "seeders"),
			"peers": str(h, "peers"), "category": knabenCategory(c, cat, cats), "uploader": str(h, "tracker")}
		it, ok, _ := c.build(vals, []string{time.RFC3339, "2006-01-02T15:04:05"})
		if !ok {
			dropped++
			continue
		}
		if it.InfoHash != "" {
			it.GUID = "knaben:" + it.InfoHash
		}
		out = append(out, it)
	}
	return out, dropped, nil
}

// knabenCategory picks the first listed category the definition maps.
func knabenCategory(c *Client, first string, all []any) string {
	if c.newznabFor(first) != nil {
		return first
	}
	for _, x := range all {
		if s := jsonString(x); c.newznabFor(s) != nil {
			return s
		}
	}
	return first
}
