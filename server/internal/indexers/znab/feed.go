package znab

import (
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Item is one release from a feed, with the Newznab/Torznab attributes
// interpreted. Pointers and zero values mean "the indexer did not say".
type Item struct {
	Title       string
	GUID        string
	Link        string // download link (enclosure or <link>), may be a magnet
	MagnetURL   string
	InfoHash    string // lower-case hex
	InfoURL     string // details page (<comments> or a non-download <link>)
	Description string
	PublishDate time.Time
	Size        int64
	Files       *int
	Grabs       *int
	Seeders     *int
	Leechers    *int
	Peers       *int
	Categories  []int

	IMDBID    string // "tt1234567"
	TMDBID    int64
	TVDBID    int64
	TVMazeID  int64
	RageID    int64
	Season    string
	Episode   string
	Year      int
	Artist    string
	Album     string
	Author    string
	BookTitle string
	Genre     string
	Group     string // usenet group or release team
	Poster    string // usenet poster

	DownloadVolumeFactor *float64
	UploadVolumeFactor   *float64
	MinimumRatio         *float64
	MinimumSeedTime      *time.Duration
	Tags                 []string

	// Attrs holds every attribute as sent (lower-case names), for consumers
	// that need one this package does not interpret.
	Attrs map[string][]string
}

type xmlFeed struct {
	Channel struct {
		Response struct {
			Offset string `xml:"offset,attr"`
			Total  string `xml:"total,attr"`
		} `xml:"response"`
		Items []xmlItem `xml:"item"`
	} `xml:"channel"`
}

type xmlItem struct {
	Title       string   `xml:"title"`
	GUID        string   `xml:"guid"`
	Links       []string `xml:"link"`
	Comments    string   `xml:"comments"`
	PubDate     string   `xml:"pubDate"`
	Size        string   `xml:"size"`
	Description string   `xml:"description"`
	Categories  []string `xml:"category"`
	Enclosures  []struct {
		URL    string `xml:"url,attr"`
		Length string `xml:"length,attr"`
		Type   string `xml:"type,attr"`
	} `xml:"enclosure"`
	Attrs []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	} `xml:"attr"`
}

// Page is one parsed feed response.
type Page struct {
	Items  []Item
	Offset int
	Total  int // -1 when the indexer does not say
}

// ParseFeed parses a search or RSS response. An <error> document becomes *Error.
func ParseFeed(body []byte) (*Page, error) {
	if err := detectError(body); err != nil {
		return nil, err
	}
	var x xmlFeed
	if err := decode(body, &x); err != nil {
		return nil, &Error{Kind: KindBadResponse, Message: "the feed is not valid XML: " + err.Error()}
	}
	p := &Page{Total: -1}
	p.Offset, _ = strconv.Atoi(strings.TrimSpace(x.Channel.Response.Offset))
	if t, err := strconv.Atoi(strings.TrimSpace(x.Channel.Response.Total)); err == nil {
		p.Total = t
	}
	for _, xi := range x.Channel.Items {
		if it, ok := toItem(xi); ok {
			p.Items = append(p.Items, it)
		}
	}
	return p, nil
}

var (
	btih      = regexp.MustCompile(`(?i)xt=urn:btih:([a-z0-9]{32,40})`)
	hexHash   = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	imdbDigit = regexp.MustCompile(`^(?:tt)?0*(\d{1,9})$`)
)

func clean(s string) string {
	// encoding/xml has decoded entities once; feeds that double-escape
	// (&amp;amp;) or put HTML entities inside CDATA need a second pass.
	s = strings.TrimSpace(s)
	if strings.ContainsRune(s, '&') {
		s = html.UnescapeString(s)
	}
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

func toItem(x xmlItem) (Item, bool) {
	it := Item{Title: clean(x.Title), GUID: strings.TrimSpace(x.GUID), Description: strings.TrimSpace(x.Description),
		Attrs: map[string][]string{}}
	if it.Title == "" {
		return it, false
	}
	for _, a := range x.Attrs {
		n := strings.ToLower(strings.TrimSpace(a.Name))
		if n == "" {
			continue
		}
		it.Attrs[n] = append(it.Attrs[n], strings.TrimSpace(a.Value))
	}
	attr := func(names ...string) string {
		for _, n := range names {
			if v := it.Attrs[n]; len(v) > 0 && v[0] != "" {
				return v[0]
			}
		}
		return ""
	}

	// Download link: enclosure first (the spec's place for it), then <link>.
	for _, e := range x.Enclosures {
		if u := strings.TrimSpace(e.URL); u != "" && it.Link == "" {
			it.Link = u
			if n, err := strconv.ParseInt(strings.TrimSpace(e.Length), 10, 64); err == nil && n > 0 {
				it.Size = n
			}
		}
	}
	for _, l := range x.Links {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if it.Link == "" {
			it.Link = l
		} else if it.InfoURL == "" && l != it.Link {
			it.InfoURL = l
		}
	}
	if c := strings.TrimSpace(x.Comments); c != "" {
		it.InfoURL = c
	}
	if m := attr("magneturl"); m != "" {
		it.MagnetURL = m
	}
	if strings.HasPrefix(strings.ToLower(it.Link), "magnet:") {
		if it.MagnetURL == "" {
			it.MagnetURL = it.Link
		}
		it.Link = ""
	}
	if h := attr("infohash"); hexHash.MatchString(h) {
		it.InfoHash = strings.ToLower(h)
	} else if m := btih.FindStringSubmatch(it.MagnetURL); m != nil && len(m[1]) == 40 {
		it.InfoHash = strings.ToLower(m[1])
	}
	if it.Link == "" && it.MagnetURL == "" {
		return it, false
	}
	if it.GUID == "" {
		switch {
		case it.InfoHash != "":
			it.GUID = it.InfoHash
		case it.InfoURL != "":
			it.GUID = it.InfoURL
		case it.Link != "":
			it.GUID = it.Link
		default:
			it.GUID = it.MagnetURL
		}
	}
	if u, err := url.Parse(it.InfoURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		it.InfoURL = ""
	}

	// Size: attribute, then <size>, then the enclosure length.
	if n := parseInt64(attr("size")); n > 0 {
		it.Size = n
	} else if n := parseInt64(x.Size); n > 0 {
		it.Size = n
	}
	it.PublishDate = ParseDate(x.PubDate)
	if it.PublishDate.IsZero() {
		it.PublishDate = ParseDate(attr("usenetdate", "publishdate"))
	}
	it.Files = optInt(attr("files"))
	it.Grabs = optInt(attr("grabs"))
	it.Seeders = optInt(attr("seeders"))
	it.Leechers = optInt(attr("leechers"))
	it.Peers = optInt(attr("peers"))
	// Torznab defines peers as seeders + leechers.
	if it.Leechers == nil && it.Peers != nil && it.Seeders != nil && *it.Peers >= *it.Seeders {
		l := *it.Peers - *it.Seeders
		it.Leechers = &l
	}
	if it.Peers == nil && it.Seeders != nil && it.Leechers != nil {
		p := *it.Seeders + *it.Leechers
		it.Peers = &p
	}

	seen := map[int]bool{}
	addCat := func(s string) {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 && !seen[n] {
			seen[n] = true
			it.Categories = append(it.Categories, n)
		}
	}
	for _, c := range it.Attrs["category"] {
		addCat(c)
	}
	for _, c := range x.Categories {
		addCat(c) // numeric <category> elements only; names are ignored
	}

	if m := imdbDigit.FindStringSubmatch(attr("imdbid", "imdb")); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n > 0 {
			it.IMDBID = "tt" + leftPad(strconv.Itoa(n), 7)
		}
	}
	it.TMDBID = parseInt64(attr("tmdbid", "tmdb"))
	it.TVDBID = parseInt64(attr("tvdbid", "tvdb"))
	it.TVMazeID = parseInt64(attr("tvmazeid", "tvmaze"))
	it.RageID = parseInt64(attr("rageid"))
	it.Season = attr("season")
	it.Episode = attr("episode", "ep")
	it.Year, _ = strconv.Atoi(attr("year"))
	it.Artist = attr("artist")
	it.Album = attr("album")
	it.Author = attr("author")
	it.BookTitle = attr("booktitle")
	it.Genre = attr("genre")
	it.Group = attr("group", "team")
	it.Poster = attr("poster")
	it.DownloadVolumeFactor = optFloat(attr("downloadvolumefactor"))
	it.UploadVolumeFactor = optFloat(attr("uploadvolumefactor"))
	it.MinimumRatio = optFloat(attr("minimumratio"))
	if n := parseInt64(attr("minimumseedtime")); n > 0 {
		d := time.Duration(n) * time.Second
		it.MinimumSeedTime = &d
	}
	for _, tg := range it.Attrs["tag"] {
		if tg != "" {
			it.Tags = append(it.Tags, strings.ToLower(tg))
		}
	}
	return it, true
}

func leftPad(s string, n int) string {
	for len(s) < n {
		s = "0" + s
	}
	return s
}

func parseInt64(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
		return n
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
		return int64(f)
	}
	return 0
}

func optInt(s string) *int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return nil
	}
	return &n
}

func optFloat(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return nil
	}
	return &f
}
