// Package znab speaks the Newznab API and its Torznab extension, implemented
// from the public specifications: capabilities (t=caps), searches (t=search,
// tvsearch, movie, music, book), RSS (a search without a query) and the
// <error code=…> responses. It parses real-world feeds leniently: namespaced
// attributes, missing sizes, enclosures or plain links, magnet-only items,
// HTML entities, odd date formats and gzip bodies.
package znab

import (
	"bytes"
	"encoding/xml"
	"io"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

// SearchType is a Newznab search function.
type SearchType string

// Search functions.
const (
	SearchGeneric SearchType = "search"
	SearchTV      SearchType = "tvsearch"
	SearchMovie   SearchType = "movie"
	SearchMusic   SearchType = "music"
	SearchBook    SearchType = "book"
)

// SearchTypes lists every search function in a stable order.
var SearchTypes = []SearchType{SearchGeneric, SearchTV, SearchMovie, SearchMusic, SearchBook}

// SearchCap is what one search function supports.
type SearchCap struct {
	Type      SearchType `json:"type"`
	Available bool       `json:"available"`
	Params    []string   `json:"params"` // lower-case, e.g. q, imdbid, season
}

// Supports reports whether the function accepts param.
func (s SearchCap) Supports(param string) bool { return slices.Contains(s.Params, param) }

// Category is an indexer category with its subcategories.
type Category struct {
	ID     int        `json:"id"`
	Name   string     `json:"name"`
	Subcat []Category `json:"subcats,omitempty"`
}

// Caps are an indexer's capabilities.
type Caps struct {
	ServerTitle  string      `json:"serverTitle,omitempty"`
	LimitMax     int         `json:"limitMax,omitempty"`
	LimitDefault int         `json:"limitDefault,omitempty"`
	Searching    []SearchCap `json:"searching"`
	Categories   []Category  `json:"categories"`
	Tags         []string    `json:"tags,omitempty"`
}

// Search returns the capability of a search function (not available when the
// indexer does not list it).
func (c *Caps) Search(t SearchType) SearchCap {
	for _, s := range c.Searching {
		if s.Type == t {
			return s
		}
	}
	return SearchCap{Type: t}
}

// CategoryIDs returns every category and subcategory id.
func (c *Caps) CategoryIDs() []int {
	var out []int
	for _, cat := range c.Categories {
		out = append(out, cat.ID)
		for _, s := range cat.Subcat {
			out = append(out, s.ID)
		}
	}
	return out
}

type xmlCaps struct {
	XMLName xml.Name `xml:"caps"`
	Server  struct {
		Title string `xml:"title,attr"`
	} `xml:"server"`
	Limits struct {
		Max     string `xml:"max,attr"`
		Default string `xml:"default,attr"`
	} `xml:"limits"`
	Searching struct {
		Items []xmlSearch `xml:",any"`
	} `xml:"searching"`
	Categories struct {
		Items []xmlCat `xml:"category"`
	} `xml:"categories"`
	Tags struct {
		Items []struct {
			Name string `xml:"name,attr"`
		} `xml:"tag"`
	} `xml:"tags"`
}

type xmlSearch struct {
	XMLName         xml.Name
	Available       string `xml:"available,attr"`
	SupportedParams string `xml:"supportedParams,attr"`
}

type xmlCat struct {
	ID     string   `xml:"id,attr"`
	Name   string   `xml:"name,attr"`
	Subcat []xmlCat `xml:"subcat"`
}

// searchElements maps <searching> children to search functions. Indexers
// use both audio-search and music-search.
var searchElements = map[string]SearchType{
	"search": SearchGeneric, "tv-search": SearchTV, "movie-search": SearchMovie,
	"audio-search": SearchMusic, "music-search": SearchMusic, "book-search": SearchBook,
}

// ParseCaps parses a t=caps response. An <error> response becomes *Error.
func ParseCaps(body []byte) (*Caps, error) {
	if err := detectError(body); err != nil {
		return nil, err
	}
	var x xmlCaps
	if err := decode(body, &x); err != nil {
		return nil, &Error{Kind: KindBadResponse, Message: "capabilities are not valid XML: " + err.Error()}
	}
	c := &Caps{ServerTitle: strings.TrimSpace(x.Server.Title)}
	c.LimitMax, _ = strconv.Atoi(strings.TrimSpace(x.Limits.Max))
	c.LimitDefault, _ = strconv.Atoi(strings.TrimSpace(x.Limits.Default))
	seen := map[SearchType]bool{}
	for _, s := range x.Searching.Items {
		t, ok := searchElements[strings.ToLower(s.XMLName.Local)]
		if !ok || seen[t] {
			continue
		}
		seen[t] = true
		sc := SearchCap{Type: t, Available: yes(s.Available)}
		for _, p := range strings.Split(s.SupportedParams, ",") {
			p = strings.ToLower(strings.TrimSpace(p))
			if p != "" && !slices.Contains(sc.Params, p) {
				sc.Params = append(sc.Params, p)
			}
		}
		// A function without supportedParams accepts a query (spec default).
		if s.SupportedParams == "" && sc.Available {
			sc.Params = []string{"q"}
		}
		c.Searching = append(c.Searching, sc)
	}
	if !seen[SearchGeneric] {
		// Every indexer supports the basic search even when caps forget it.
		c.Searching = append([]SearchCap{{Type: SearchGeneric, Available: true, Params: []string{"q"}}}, c.Searching...)
	}
	for _, xc := range x.Categories.Items {
		cat, ok := toCategory(xc)
		if !ok {
			continue
		}
		for _, s := range xc.Subcat {
			if sc, ok := toCategory(s); ok {
				cat.Subcat = append(cat.Subcat, sc)
			}
		}
		c.Categories = append(c.Categories, cat)
	}
	for _, t := range x.Tags.Items {
		if n := strings.TrimSpace(t.Name); n != "" {
			c.Tags = append(c.Tags, n)
		}
	}
	return c, nil
}

func toCategory(x xmlCat) (Category, bool) {
	id, err := strconv.Atoi(strings.TrimSpace(x.ID))
	if err != nil || id <= 0 {
		return Category{}, false
	}
	return Category{ID: id, Name: strings.TrimSpace(x.Name)}, true
}

func yes(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "yes", "true", "1":
		return true
	}
	return false
}

// decode unmarshals XML leniently: HTML entities (&nbsp;, &eacute;) are
// understood, unknown ones are kept as text, common 8-bit charsets are
// converted. encoding/xml never expands DTD-declared entities, so entity
// expansion attacks do not apply.
func decode(body []byte, v any) error {
	d := xml.NewDecoder(bytes.NewReader(body))
	d.Strict = false
	d.Entity = xml.HTMLEntity
	d.CharsetReader = func(label string, in io.Reader) (io.Reader, error) {
		switch strings.ToLower(strings.TrimSpace(label)) {
		case "iso-8859-1", "latin1", "latin-1", "iso8859-1":
			return charmap.ISO8859_1.NewDecoder().Reader(in), nil
		case "windows-1252", "cp1252":
			return charmap.Windows1252.NewDecoder().Reader(in), nil
		case "iso-8859-2", "latin2":
			return charmap.ISO8859_2.NewDecoder().Reader(in), nil
		case "windows-1250", "cp1250":
			return charmap.Windows1250.NewDecoder().Reader(in), nil
		}
		// Anything else is read as UTF-8 (the overwhelmingly common case).
		return in, nil
	}
	return d.Decode(v)
}
