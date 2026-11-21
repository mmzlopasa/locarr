// Package fake is a small Torznab/Newznab server for tests and manual
// testing (scripts/dev-fakes.sh runs it as a container). It serves:
//
//	/{indexer}/api                                  Torznab or Newznab (caps, search, tvsearch, movie, music, book)
//	/{indexer}/dl/{n}                               release file (.torrent or .nzb), needs the API key
//	/prowlarr/api/v1/indexer                        Prowlarr-style indexer list (X-Api-Key)
//	/prowlarr/{id}/api                              Prowlarr-style proxied feed
//	/jackett/api/v2.0/indexers/all/results/torznab/api?t=indexers   Jackett-style list
//	/jackett/api/v2.0/indexers/{id}/results/torznab/api             Jackett-style proxied feed
//
// Indexers: "torznab" (well-formed), "quirky" (namespaced attributes, CDATA,
// HTML entities, missing sizes, magnet-only items, odd dates, gzip without a
// header, limited caps), "newznab" (usenet), "broken" (always an error) and
// "slow" (answers after a delay).
package fake

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders/testkit"
)

// Release is one release in the fake catalog.
type Release struct {
	Title    string
	Cat      int
	Size     int64
	Seeders  int
	Peers    int
	Grabs    int
	IMDB     string
	TMDB     int
	TVDB     int
	Season   string
	Episode  string
	Artist   string
	Album    string
	Author   string
	Hash     string
	Age      time.Duration
	Freelech bool
}

// Catalog is the shared set of releases every indexer offers (each in its own way).
var Catalog = []Release{
	{Title: "Big.Buck.Bunny.2008.1080p.BluRay.x264-LOCARR", Cat: 2040, Size: 1_288_490_189, Seeders: 120, Peers: 130, Grabs: 900,
		IMDB: "tt1254207", TMDB: 10378, Hash: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", Age: 3 * time.Hour, Freelech: true},
	{Title: "Sintel.2010.2160p.WEB-DL.DDP5.1.HEVC-LOCARR", Cat: 2045, Size: 8_589_934_592, Seeders: 45, Peers: 50, Grabs: 300,
		IMDB: "tt1727587", TMDB: 45745, Hash: "b1b2c3d4e5f60718293a4b5c6d7e8f9012345678", Age: 26 * time.Hour},
	{Title: "Tears.of.Steel.2012.720p.WEBRip.x264-LOCARR", Cat: 2040, Size: 734_003_200, Seeders: 8, Peers: 9, Grabs: 40,
		IMDB: "tt2285752", TMDB: 133701, Hash: "c1b2c3d4e5f60718293a4b5c6d7e8f9012345678", Age: 72 * time.Hour},
	{Title: "Elephants.Dream.S01E01.1080p.WEB.h264-LOCARR", Cat: 5040, Size: 1_073_741_824, Seeders: 30, Peers: 33, Grabs: 120,
		TVDB: 424242, Season: "1", Episode: "1", Hash: "d1b2c3d4e5f60718293a4b5c6d7e8f9012345678", Age: 5 * time.Hour},
	{Title: "Elephants.Dream.S01E02.1080p.WEB.h264-LOCARR", Cat: 5040, Size: 1_073_741_824, Seeders: 25, Peers: 27, Grabs: 100,
		TVDB: 424242, Season: "1", Episode: "2", Hash: "e1b2c3d4e5f60718293a4b5c6d7e8f9012345678", Age: 4 * time.Hour},
	{Title: "Cosmos.Laundromat.S01.720p.WEB-DL.AAC2.0.x264-LOCARR", Cat: 5030, Size: 3_221_225_472, Seeders: 12, Peers: 14, Grabs: 60,
		TVDB: 434343, Season: "1", Hash: "f1b2c3d4e5f60718293a4b5c6d7e8f9012345678", Age: 240 * time.Hour},
	{Title: "Open Source Artist - Free Album (2020) [FLAC]", Cat: 3040, Size: 419_430_400, Seeders: 6, Peers: 6, Grabs: 15,
		Artist: "Open Source Artist", Album: "Free Album", Hash: "a2b2c3d4e5f60718293a4b5c6d7e8f9012345678", Age: 48 * time.Hour},
	{Title: "Public Domain Author - A Free Book (EPUB)", Cat: 7020, Size: 2_097_152, Seeders: 3, Peers: 3, Grabs: 4,
		Author: "Public Domain Author", Hash: "a3b2c3d4e5f60718293a4b5c6d7e8f9012345678", Age: 30 * time.Hour},
}

// Server is the fake. Change its fields only before serving or under Lock.
type Server struct {
	sync.Mutex
	APIKey    string        // key for the direct indexers
	SourceKey string        // key for the Prowlarr/Jackett endpoints
	SlowDelay time.Duration // delay of the "slow" indexer
	// SeedDir, when set, makes every .torrent a real tiny torrent of a
	// deterministic file "<title>.mkv" that the fake also writes into SeedDir,
	// so a real client given that folder completes it without any network.
	SeedDir string
	Now     func() time.Time
	// ProwlarrIDs and JackettIDs are what the source lists return (sync tests change them).
	ProwlarrIDs []int
	JackettIDs  []string
	// Requests counts API calls per indexer name (for cache and limit tests).
	Requests map[string]int
}

// New returns a fake with the given API keys.
func New(apiKey, sourceKey string) *Server {
	return &Server{APIKey: apiKey, SourceKey: sourceKey, SlowDelay: 3 * time.Second, Now: time.Now,
		ProwlarrIDs: []int{1, 2}, JackettIDs: []string{"faketorznab", "fakequirky"}, Requests: map[string]int{}}
}

// Indexers lists the indexer names and their kinds.
var Indexers = map[string]string{"torznab": "torznab", "quirky": "torznab", "newznab": "newznab", "broken": "torznab", "slow": "torznab"}

var prowlarrMap = map[int]struct{ name, indexer, protocol string }{
	1: {"Fake Torznab", "torznab", "torrent"},
	2: {"Fake Newznab", "newznab", "usenet"},
	3: {"Fake Quirky", "quirky", "torrent"},
}

var jackettMap = map[string]struct{ name, indexer string }{
	"faketorznab": {"Fake Torznab", "torznab"},
	"fakequirky":  {"Fake Quirky", "quirky"},
	"fakeslow":    {"Fake Slow", "slow"},
}

func (s *Server) count(name string) {
	s.Lock()
	s.Requests[name]++
	s.Unlock()
}

// Count returns how many API calls an indexer received.
func (s *Server) Count(name string) int {
	s.Lock()
	defer s.Unlock()
	return s.Requests[name]
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.URL.Path == "/healthz":
		_, _ = w.Write([]byte("ok"))
	case len(parts) == 4 && parts[0] == "prowlarr" && parts[1] == "api" && parts[2] == "v1" && parts[3] == "indexer":
		s.prowlarrList(w, r)
	case len(parts) == 3 && parts[0] == "prowlarr" && parts[2] == "api":
		id, _ := strconv.Atoi(parts[1])
		p, ok := prowlarrMap[id]
		s.Lock()
		listed := slices.Contains(s.ProwlarrIDs, id)
		s.Unlock()
		if !ok || !listed {
			http.NotFound(w, r)
			return
		}
		s.api(w, r, p.indexer, s.SourceKey, "/prowlarr/"+parts[1])
	case len(parts) == 4 && parts[0] == "prowlarr" && parts[2] == "dl":
		id, _ := strconv.Atoi(parts[1])
		p, ok := prowlarrMap[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		s.download(w, r, p.indexer, s.SourceKey, parts[3])
	case len(parts) >= 7 && parts[0] == "jackett" && parts[1] == "api" && parts[2] == "v2.0" && parts[3] == "indexers" &&
		parts[5] == "results" && parts[6] == "torznab":
		s.jackett(w, r, parts)
	case len(parts) == 2 && parts[1] == "api" && Indexers[parts[0]] != "":
		s.api(w, r, parts[0], s.APIKey, "/"+parts[0])
	case len(parts) == 3 && parts[1] == "dl" && Indexers[parts[0]] != "":
		s.download(w, r, parts[0], s.APIKey, parts[2])
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) prowlarrList(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Api-Key") != s.SourceKey {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	s.Lock()
	ids := slices.Clone(s.ProwlarrIDs)
	s.Unlock()
	type item struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
		Enable   bool   `json:"enable"`
	}
	out := []item{}
	for _, id := range ids {
		p := prowlarrMap[id]
		out = append(out, item{ID: id, Name: p.name, Protocol: p.protocol, Enable: true})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) jackett(w http.ResponseWriter, r *http.Request, parts []string) {
	id := parts[4]
	if id == "all" && r.URL.Query().Get("t") == "indexers" {
		if r.URL.Query().Get("apikey") != s.SourceKey {
			writeXML(w, `<error code="100" description="Invalid API Key"/>`)
			return
		}
		s.Lock()
		ids := slices.Clone(s.JackettIDs)
		s.Unlock()
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><indexers>`)
		for _, i := range ids {
			fmt.Fprintf(&b, `<indexer id="%s" configured="true"><title>%s</title><description>fake</description><type>public</type></indexer>`,
				i, jackettMap[i].name)
		}
		b.WriteString(`</indexers>`)
		writeXML(w, b.String())
		return
	}
	j, ok := jackettMap[id]
	if !ok {
		http.NotFound(w, r)
		return
	}
	base := "/jackett/api/v2.0/indexers/" + id + "/results/torznab"
	switch {
	case len(parts) == 8 && parts[7] == "api":
		s.api(w, r, j.indexer, s.SourceKey, base)
	case len(parts) == 9 && parts[7] == "dl":
		s.download(w, r, j.indexer, s.SourceKey, parts[8])
	default:
		http.NotFound(w, r)
	}
}

func writeXML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func baseURL(r *http.Request, path string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + path
}

func (s *Server) api(w http.ResponseWriter, r *http.Request, name, key, path string) {
	s.count(name)
	q := r.URL.Query()
	if name == "slow" {
		select {
		case <-time.After(s.SlowDelay):
		case <-r.Context().Done():
			return
		}
	}
	if name == "broken" {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("<html><body>Bad gateway</body></html>"))
		return
	}
	if q.Get("apikey") != key {
		writeXML(w, `<?xml version="1.0" encoding="UTF-8"?><error code="100" description="Incorrect user credentials"/>`)
		return
	}
	t := q.Get("t")
	if t == "caps" {
		writeXML(w, caps(name))
		return
	}
	fns := map[string]bool{"search": true, "tvsearch": true, "movie": true, "music": true, "book": true}
	if !fns[t] {
		writeXML(w, `<error code="202" description="No such function"/>`)
		return
	}
	if q.Get("q") == "ratelimit" {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		writeXML(w, `<error code="500" description="Request limit reached"/>`)
		return
	}
	items := s.filter(name, q)
	body := s.feed(name, items, key, baseURL(r, path), q)
	if name == "quirky" {
		// gzip without a Content-Encoding header, as some servers send it.
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write([]byte(body))
		_ = zw.Close()
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write(buf.Bytes())
		return
	}
	writeXML(w, body)
}

func (s *Server) download(w http.ResponseWriter, r *http.Request, name, key, n string) {
	if r.URL.Query().Get("apikey") != key {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	i, err := strconv.Atoi(n)
	if err != nil || i < 0 || i >= len(Catalog) {
		http.NotFound(w, r)
		return
	}
	rel := Catalog[i]
	if Indexers[name] == "newznab" {
		w.Header().Set("Content-Type", "application/x-nzb")
		w.Header().Set("Content-Disposition", `attachment; filename="`+rel.Title+`.nzb"`)
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"><head><meta type="title">%s</meta></head></nzb>`,
			html.EscapeString(rel.Title))
		return
	}
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", `attachment; filename="`+rel.Title+`.torrent"`)
	if s.SeedDir != "" {
		t, err := SeedTorrent(s.SeedDir, rel.Title)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(t)
		return
	}
	info := fmt.Sprintf("d6:lengthi%de4:name%d:%s12:piece lengthi262144e6:pieces0:e", rel.Size, len(rel.Title), rel.Title)
	_, _ = fmt.Fprintf(w, "d8:announce%d:%s4:info%se", len("http://tracker.invalid/announce"), "http://tracker.invalid/announce", info)
}

func caps(name string) string {
	cats := `<categories>
  <category id="2000" name="Movies"><subcat id="2040" name="Movies/HD"/><subcat id="2045" name="Movies/UHD"/></category>
  <category id="5000" name="TV"><subcat id="5030" name="TV/SD"/><subcat id="5040" name="TV/HD"/></category>
  <category id="3000" name="Audio"><subcat id="3040" name="Audio/Lossless"/><subcat id="3030" name="Audio/Audiobook"/></category>
  <category id="7000" name="Books"><subcat id="7020" name="Books/EBook"/></category>
  <category id="100001" name="Custom tracker category"/>
</categories>`
	switch name {
	case "quirky":
		return `<?xml version="1.0" encoding="UTF-8"?>
<caps><server title="Quirky &amp; Co"/><limits max="50" default="25"/>
<searching><search available="yes"/><movie-search available="yes" supportedParams="q"/>
<tv-search available="no" supportedParams="q,season,ep"/><audio-search available="yes" supportedParams="q"/></searching>
` + cats + `</caps>`
	case "newznab":
		return `<?xml version="1.0" encoding="UTF-8"?>
<caps><server version="0.8" title="Fake Newznab"/><limits max="100" default="100"/>
<searching><search available="yes" supportedParams="q"/><tv-search available="yes" supportedParams="q,rid,tvdbid,season,ep"/>
<movie-search available="yes" supportedParams="q,imdbid"/><audio-search available="no"/><book-search available="no"/></searching>
` + cats + `</caps>`
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<caps><server title="Fake Torznab"/><limits max="100" default="50"/>
<searching><search available="yes" supportedParams="q"/>
<tv-search available="yes" supportedParams="q,season,ep,tvdbid,imdbid"/>
<movie-search available="yes" supportedParams="q,imdbid,tmdbid"/>
<music-search available="yes" supportedParams="q,artist,album"/>
<book-search available="yes" supportedParams="q,author,title"/></searching>
` + cats + `<tags><tag name="freeleech" description="Free"/></tags></caps>`
}

func (s *Server) filter(name string, q url.Values) []int {
	var cats []int
	for _, c := range strings.Split(q.Get("cat"), ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(c)); err == nil {
			cats = append(cats, n)
		}
	}
	term := strings.ToLower(q.Get("q"))
	var out []int
	for i, r := range Catalog {
		if name == "newznab" && (r.Cat == 3040 || r.Cat == 7020) {
			continue // the usenet indexer has no music or books
		}
		if len(cats) > 0 && !slices.Contains(cats, r.Cat) && !slices.Contains(cats, r.Cat-r.Cat%1000) {
			continue
		}
		title := strings.ToLower(strings.NewReplacer(".", " ", "-", " ").Replace(r.Title))
		ok := true
		for _, w := range strings.Fields(term) {
			if !strings.Contains(title, w) {
				ok = false
			}
		}
		if v := q.Get("imdbid"); v != "" && strings.TrimPrefix(r.IMDB, "tt") != strings.TrimLeft(strings.TrimPrefix(v, "tt"), "0") &&
			strings.TrimPrefix(r.IMDB, "tt") != strings.TrimPrefix(v, "tt") {
			ok = false
		}
		if v := q.Get("tmdbid"); v != "" && strconv.Itoa(r.TMDB) != v {
			ok = false
		}
		if v := q.Get("tvdbid"); v != "" && strconv.Itoa(r.TVDB) != v {
			ok = false
		}
		if v := q.Get("season"); v != "" && strings.TrimLeft(r.Season, "0") != strings.TrimLeft(v, "0") {
			ok = false
		}
		if v := q.Get("ep"); v != "" && strings.TrimLeft(r.Episode, "0") != strings.TrimLeft(v, "0") {
			ok = false
		}
		if v := strings.ToLower(q.Get("artist")); v != "" && strings.ToLower(r.Artist) != v {
			ok = false
		}
		if v := strings.ToLower(q.Get("author")); v != "" && strings.ToLower(r.Author) != v {
			ok = false
		}
		if ok {
			out = append(out, i)
		}
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if offset >= len(out) {
		return nil
	}
	out = out[offset:]
	if limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	return out
}

func (s *Server) feed(name string, idx []int, key, base string, q url.Values) string {
	now := s.Now().UTC()
	var b strings.Builder
	switch name {
	case "newznab":
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">
<channel><title>Fake Newznab</title>`)
		fmt.Fprintf(&b, `<newznab:response offset="%s" total="%d"/>`, q.Get("offset"), len(idx))
		for _, i := range idx {
			r := Catalog[i]
			link := fmt.Sprintf("%s/dl/%d?apikey=%s", base, i, key)
			fmt.Fprintf(&b, `<item><title>%s</title><guid isPermaLink="true">%s/details/nz%d</guid><link>%s</link>
<comments>%s/details/nz%d#comments</comments><pubDate>%s</pubDate><category>%d</category>
<enclosure url="%s" length="%d" type="application/x-nzb"/>
<newznab:attr name="category" value="%d"/><newznab:attr name="size" value="%d"/><newznab:attr name="grabs" value="%d"/>`,
				html.EscapeString(r.Title), base, i, html.EscapeString(link), base, i, now.Add(-r.Age).Format(time.RFC1123Z), r.Cat,
				html.EscapeString(link), r.Size, r.Cat, r.Size, r.Grabs)
			if r.TVDB > 0 {
				fmt.Fprintf(&b, `<newznab:attr name="tvdbid" value="%d"/>`, r.TVDB)
			}
			if r.IMDB != "" {
				fmt.Fprintf(&b, `<newznab:attr name="imdb" value="%s"/>`, strings.TrimPrefix(r.IMDB, "tt"))
			}
			b.WriteString("</item>\n")
		}
		b.WriteString("</channel></rss>")
	case "quirky":
		// Prefix "tz" bound to the Torznab namespace, CDATA titles with entities,
		// magnet-only items, missing sizes and a variety of date formats.
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:tz="http://torznab.com/schemas/2015/feed"><channel><title>Quirky</title>`)
		dates := []string{"Mon, 2 Jan 2006 15:04:05 GMT", "2006-01-02T15:04:05Z", "2006-01-02 15:04:05", "02 Jan 2006 15:04:05 +0000", "not a date"}
		for n, i := range idx {
			r := Catalog[i]
			date := now.Add(-r.Age).Format(dates[n%len(dates)])
			title := strings.ReplaceAll(r.Title, ".", " ")
			if n%2 == 0 {
				title = "<![CDATA[" + strings.ReplaceAll(title, "&", "&amp;") + " &ndash; quirky]]>"
			} else {
				title = html.EscapeString(title) + " &eacute;dition"
			}
			fmt.Fprintf(&b, "<item><title>%s</title><pubDate>%s</pubDate>", title, date)
			magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s&amp;dn=%s", strings.ToUpper(r.Hash), url.QueryEscape(r.Title))
			if n%3 == 0 {
				// Magnet only: no enclosure, no infohash attribute, no size.
				fmt.Fprintf(&b, "<link>%s</link>", magnet)
			} else {
				link := fmt.Sprintf("%s/dl/%d?apikey=%s", base, i, key)
				fmt.Fprintf(&b, `<guid>quirky-%d</guid><link>%s</link><enclosure url="%s" type="application/x-bittorrent"/>`,
					i, html.EscapeString(link), html.EscapeString(link))
				fmt.Fprintf(&b, `<tz:attr name="size" value="%d"/>`, r.Size)
				fmt.Fprintf(&b, `<torznab:attr name="magneturl" value="%s"/>`, magnet)
			}
			fmt.Fprintf(&b, `<tz:attr name="category" value="%d"/><tz:attr name="seeders" value="%d"/><tz:attr name="peers" value="%d"/>`,
				r.Cat, r.Seeders, r.Peers)
			b.WriteString("</item>\n")
		}
		b.WriteString("</channel></rss>")
	default:
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:torznab="http://torznab.com/schemas/2015/feed">
<channel><title>Fake Torznab</title>`)
		for _, i := range idx {
			r := Catalog[i]
			link := fmt.Sprintf("%s/dl/%d?apikey=%s", base, i, key)
			fmt.Fprintf(&b, `<item><title>%s</title><guid>%s/details/%d</guid><comments>%s/details/%d</comments>
<pubDate>%s</pubDate><size>%d</size><link>%s</link><category>%d</category>
<enclosure url="%s" length="%d" type="application/x-bittorrent"/>
<torznab:attr name="category" value="%d"/><torznab:attr name="category" value="%d"/>
<torznab:attr name="seeders" value="%d"/><torznab:attr name="peers" value="%d"/><torznab:attr name="grabs" value="%d"/>
<torznab:attr name="infohash" value="%s"/><torznab:attr name="downloadvolumefactor" value="%s"/><torznab:attr name="uploadvolumefactor" value="1"/>
<torznab:attr name="minimumratio" value="1"/><torznab:attr name="minimumseedtime" value="172800"/>`,
				html.EscapeString(r.Title), base, i, base, i, now.Add(-r.Age).Format(time.RFC1123), r.Size, html.EscapeString(link), r.Cat,
				html.EscapeString(link), r.Size, r.Cat-r.Cat%1000, r.Cat, r.Seeders, r.Peers, r.Grabs, r.Hash, map[bool]string{true: "0", false: "1"}[r.Freelech])
			if r.IMDB != "" {
				fmt.Fprintf(&b, `<torznab:attr name="imdbid" value="%s"/>`, r.IMDB)
			}
			if r.TMDB > 0 {
				fmt.Fprintf(&b, `<torznab:attr name="tmdbid" value="%d"/>`, r.TMDB)
			}
			if r.TVDB > 0 {
				fmt.Fprintf(&b, `<torznab:attr name="tvdbid" value="%d"/><torznab:attr name="season" value="%s"/>`, r.TVDB, r.Season)
				if r.Episode != "" {
					fmt.Fprintf(&b, `<torznab:attr name="episode" value="%s"/>`, r.Episode)
				}
			}
			if r.Freelech {
				b.WriteString(`<torznab:attr name="tag" value="freeleech"/>`)
			}
			b.WriteString("</item>\n")
		}
		b.WriteString("</channel></rss>")
	}
	return b.String()
}

// SeedPayload is the deterministic content of a seeded release (1 MiB).
func SeedPayload(title string) []byte {
	out := make([]byte, 0, 1<<20)
	block := sha256.Sum256([]byte(title))
	for len(out) < 1<<20 {
		out = append(out, block[:]...)
		block = sha256.Sum256(block[:])
	}
	return out[:1<<20]
}

// SeedTorrent writes "<title>.mkv" into dir (unless it is there) and returns
// a torrent for it.
func SeedTorrent(dir, title string) ([]byte, error) {
	name := title + ".mkv"
	data := SeedPayload(title)
	p := filepath.Join(dir, name)
	if st, err := os.Stat(p); err != nil || st.Size() != int64(len(data)) {
		if err := os.WriteFile(p, data, 0o666); err != nil {
			return nil, err
		}
		// World-writable so the client and Locarr (other users) may hardlink it.
		_ = os.Chmod(p, 0o666)
	}
	return testkit.MakeTorrent(name, data).File, nil
}
