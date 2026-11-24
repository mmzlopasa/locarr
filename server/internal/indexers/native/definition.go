// Package native searches public torrent sites without a Torznab proxy. Each
// site is described by a definition: a small YAML document of Locarr's own
// design (search URL templates, how to read the answer, category mapping,
// rate limits) or, for sites with a real API, a Go implementation named by
// the definition. A Client built from a definition speaks the same
// capabilities/search/fetch contract as a Torznab client, so the indexer
// service treats both alike.
package native

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// Definition describes one site.
type Definition struct {
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	// Version increases whenever the definition changes behaviour.
	Version  int    `yaml:"version" json:"version"`
	Protocol string `yaml:"protocol" json:"protocol"` // torrent (default)
	Language string `yaml:"language" json:"language"`
	// Adult marks a site that only carries adult content: every category is
	// adult (6xxx), every result is labelled adult, and the site answers only
	// adult searches and RSS (D-108).
	Adult bool `yaml:"adult" json:"adult"`
	// Links are the site's addresses: the first is the default, the others
	// are known mirrors an administrator may pick.
	Links []string `yaml:"links" json:"links"`
	// Verified says how the definition was written and checked.
	Verified string `yaml:"verified" json:"verified"`
	// Challenge says whether the site shows a browser check (Cloudflare,
	// DDoS-Guard …) that the built-in browser passes: never, sometimes or
	// always.
	Challenge string `yaml:"challenge" json:"challenge"`
	// FlareSolverr is the old name of Challenge, still read from custom
	// definitions; after loading it always equals Challenge.
	FlareSolverr string `yaml:"flaresolverr" json:"flaresolverr"`
	// RateLimit is the default request limit of a new indexer for this site.
	RateLimit RateLimit `yaml:"rateLimit" json:"rateLimit"`
	// DelayMillis is the smallest gap between two requests to the site.
	DelayMillis int `yaml:"delayMillis" json:"delayMillis"`
	// TimeoutSeconds is the default request timeout for slow sites.
	TimeoutSeconds int `yaml:"timeoutSeconds" json:"timeoutSeconds"`
	// Aliases are other names of the site (as other tools call it), used to
	// recognise it when importing.
	Aliases []string `yaml:"aliases" json:"aliases"`
	// Modes lists the supported search functions and their parameters:
	// search, tv, movie, music, book → q, imdbid, tmdbid, tvdbid, season, ep.
	Modes map[string][]string `yaml:"modes" json:"modes"`
	// Categories maps the site's categories onto Newznab ids.
	Categories []CategoryMap `yaml:"categories" json:"categories"`
	// Settings are per-indexer options the templates can read (.Config.name).
	Settings []Setting `yaml:"settings" json:"settings"`
	// Impl names a Go implementation (sites with a real API); the
	// declarative blocks are then unused.
	Impl     string         `yaml:"impl" json:"impl,omitempty"`
	Search   *Block         `yaml:"search" json:"search,omitempty"`
	RSS      *Block         `yaml:"rss" json:"rss,omitempty"`
	Download *DownloadBlock `yaml:"download" json:"download,omitempty"`
	Test     TestQuery      `yaml:"test" json:"test"`

	// Set by the loader.
	Builtin   bool   `yaml:"-" json:"builtin"`
	File      string `yaml:"-" json:"file,omitempty"`
	Overrides bool   `yaml:"-" json:"overrides,omitempty"` // a custom file replacing a built-in

	compiled *compiled
}

// RateLimit is "Requests per PeriodSeconds".
type RateLimit struct {
	Requests      int `yaml:"requests" json:"requests"`
	PeriodSeconds int `yaml:"periodSeconds" json:"periodSeconds"`
}

// CategoryMap maps one site category onto a Newznab category.
type CategoryMap struct {
	ID   string `yaml:"id" json:"id"`     // the site's id (or name)
	Cat  int    `yaml:"cat" json:"cat"`   // Newznab id, e.g. 2000, 5070
	Desc string `yaml:"desc" json:"desc"` // the site's label
}

// Setting is a per-indexer option.
type Setting struct {
	Name    string            `yaml:"name" json:"name"`
	Type    string            `yaml:"type" json:"type"` // text, bool or select
	Label   string            `yaml:"label" json:"label"`
	Default string            `yaml:"default" json:"default"`
	Options map[string]string `yaml:"options" json:"options,omitempty"` // value → label (select)
}

// Block describes one way of asking the site and reading the answer.
type Block struct {
	Requests []Request `yaml:"requests"`
	// Response is json, xml (RSS/Atom) or html.
	Response string `yaml:"response"`
	// Rows selects the result rows: a CSS selector (xml, html) or a dotted
	// path (json; empty = the document itself is the array).
	Rows string `yaml:"rows"`
	// SubRows (json) selects an array inside each row whose elements are the
	// results; fields read the outer row with the "parent." prefix.
	SubRows string `yaml:"subRows"`
	// Empty is a selector (or JSON path) whose presence means "no results"
	// rather than "the layout changed" when no rows are found.
	Empty string `yaml:"empty"`
	// Skip drops rows whose field matches: field name → regular expression.
	Skip map[string]string `yaml:"skip"`
	// Pages is how many pages to read at most (default 1); requests get .Page
	// (1-based) and .Offset.
	Pages    int              `yaml:"pages"`
	PageSize int              `yaml:"pageSize"`
	Fields   map[string]Field `yaml:"fields"`
}

// Request is one templated HTTP request. If lists template values that must
// be set ("imdbid", "keywords", …; "!keywords" = must be empty); the first
// request whose conditions hold is used.
type Request struct {
	If      []string          `yaml:"if"`
	Method  string            `yaml:"method"` // GET (default) or POST
	Path    string            `yaml:"path"`   // template, relative to the site's address
	Body    string            `yaml:"body"`   // template (POST)
	Headers map[string]string `yaml:"headers"`
}

// Field reads one value from a row.
type Field struct {
	// Select is a CSS selector relative to the row (xml, html) or a dotted
	// path (json). Empty reads the row itself.
	Select string `yaml:"select"`
	// Attr reads an attribute instead of the text (xml, html).
	Attr string `yaml:"attr"`
	// Regex keeps the first capture group (or the whole match).
	Regex string `yaml:"regex"`
	// Replace applies old → new replacements in order.
	Replace [][2]string `yaml:"replace"`
	// Default is used when nothing was found.
	Default string `yaml:"default"`
	// Layouts are Go time layouts tried before the built-in date formats.
	Layouts []string `yaml:"layouts"`
}

// DownloadBlock fetches the details page at grab time to find the magnet or
// .torrent link (sites whose result lists carry neither).
type DownloadBlock struct {
	Select string `yaml:"select" json:"select"`
	Attr   string `yaml:"attr" json:"attr"`
	Regex  string `yaml:"regex" json:"regex,omitempty"`
}

// TestQuery is the known search a test runs.
type TestQuery struct {
	Query  string `yaml:"query" json:"query"`
	Mode   string `yaml:"mode" json:"mode"`     // search (default), tv, movie, …
	IMDBID string `yaml:"imdbid" json:"imdbid"` // for id-only sites
}

// Field names a definition can fill.
var fieldNames = []string{"title", "details", "download", "magnet", "infohash", "size", "date", "seeders", "leechers",
	"peers", "grabs", "files", "category", "imdbid", "tvdbid", "tmdbid", "season", "episode", "year", "uploader"}

// Search modes and the parameters each may declare.
var (
	modeNames  = []string{"search", "tv", "movie", "music", "book"}
	modeParams = []string{"q", "imdbid", "tmdbid", "tvdbid", "season", "ep"}
	conditions = []string{"keywords", "query", "imdbid", "tmdbid", "tvdbid", "season", "episode", "categories"}
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,39}$`)

// compiled holds the parsed templates and expressions of a definition.
type compiled struct {
	search, rss *compiledBlock
	download    *regexp.Regexp
}

type compiledBlock struct {
	b        *Block
	requests []compiledRequest
	fields   map[string]compiledField
	skip     map[string]*regexp.Regexp
}

type compiledRequest struct {
	r       Request
	path    *template.Template
	body    *template.Template
	headers map[string]*template.Template
}

type compiledField struct {
	f     Field
	regex *regexp.Regexp
}

// Parse reads and validates a definition.
func Parse(data []byte) (*Definition, error) {
	var d Definition
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("not a valid definition: %w", err)
	}
	if err := d.validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

func (d *Definition) validate() error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	if !idPattern.MatchString(d.ID) {
		bad("id must be 2 to 40 lower-case letters, digits or dashes")
	}
	if strings.TrimSpace(d.Name) == "" {
		bad("name is required")
	}
	if d.Version < 1 {
		bad("version must be 1 or more")
	}
	if d.Protocol == "" {
		d.Protocol = "torrent"
	}
	if d.Protocol != "torrent" {
		bad("protocol must be torrent")
	}
	if len(d.Links) == 0 {
		bad("links needs at least one address")
	}
	for i, l := range d.Links {
		u, err := url.Parse(l)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" {
			bad("links[%d] must be an http(s) address without query", i)
		}
		d.Links[i] = strings.TrimRight(l, "/")
	}
	switch {
	case d.Challenge == "":
		d.Challenge = d.FlareSolverr
	case d.FlareSolverr != "" && d.FlareSolverr != d.Challenge:
		bad("challenge and its old name flaresolverr disagree; keep only challenge")
	}
	switch d.Challenge {
	case "":
		d.Challenge = "never"
	case "never", "sometimes", "always":
	default:
		bad("challenge must be never, sometimes or always")
	}
	d.FlareSolverr = d.Challenge
	if (d.RateLimit.Requests > 0) != (d.RateLimit.PeriodSeconds > 0) {
		bad("rateLimit needs both requests and periodSeconds")
	}
	if d.DelayMillis < 0 || d.DelayMillis > 60000 {
		bad("delayMillis must be between 0 and 60000")
	}
	if d.TimeoutSeconds != 0 && (d.TimeoutSeconds < 2 || d.TimeoutSeconds > 120) {
		bad("timeoutSeconds must be between 2 and 120")
	}
	if len(d.Modes) == 0 {
		bad("modes must list at least one search mode")
	}
	if _, ok := d.Modes["search"]; !ok && len(d.Modes) > 0 {
		bad("modes must include search")
	}
	for m, ps := range d.Modes {
		if !slices.Contains(modeNames, m) {
			bad("unknown mode %q (use %s)", m, strings.Join(modeNames, ", "))
		}
		for _, p := range ps {
			if !slices.Contains(modeParams, p) {
				bad("mode %s: unknown parameter %q (use %s)", m, p, strings.Join(modeParams, ", "))
			}
		}
	}
	seen := map[string]bool{}
	for i, c := range d.Categories {
		if c.ID == "" || c.Cat <= 0 {
			bad("categories[%d] needs id and a positive cat", i)
		}
		if seen[c.ID] {
			bad("category %q is listed twice", c.ID)
		}
		seen[c.ID] = true
		if d.Adult && !IsAdultCategory(c.Cat) {
			bad("category %q: an adult site maps every category to 6xxx", c.ID)
		}
	}
	if d.Adult && len(d.Categories) == 0 {
		bad("an adult site needs its categories mapped to 6xxx")
	}
	for i, s := range d.Settings {
		if !idPattern.MatchString(s.Name) && !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,39}$`).MatchString(s.Name) {
			bad("settings[%d]: invalid name", i)
		}
		switch s.Type {
		case "text", "bool":
		case "select":
			if _, ok := s.Options[s.Default]; !ok {
				bad("setting %s: the default must be one of the options", s.Name)
			}
		default:
			bad("setting %s: type must be text, bool or select", s.Name)
		}
	}
	c := &compiled{}
	if d.Impl != "" {
		if _, ok := impls[d.Impl]; !ok {
			bad("impl %q is not a built-in implementation", d.Impl)
		}
		if d.Search != nil || d.RSS != nil {
			bad("a definition with impl has no search or rss block")
		}
	} else {
		if d.Search == nil {
			bad("search block is required")
		} else if cb, err := compileBlock("search", d.Search); err != nil {
			errs = append(errs, err)
		} else {
			c.search = cb
		}
		if d.RSS != nil {
			if cb, err := compileBlock("rss", d.RSS); err != nil {
				errs = append(errs, err)
			} else {
				c.rss = cb
			}
		}
	}
	if d.Download != nil {
		if d.Download.Select == "" && d.Download.Regex == "" {
			bad("download needs select or regex")
		}
		if d.Download.Select != "" {
			if _, err := compileSelector(d.Download.Select); err != nil {
				bad("download.select: %v", err)
			}
		}
		if d.Download.Regex != "" {
			re, err := regexp.Compile(d.Download.Regex)
			if err != nil {
				bad("download.regex: %v", err)
			}
			c.download = re
		}
	}
	if d.Test.Mode == "" {
		d.Test.Mode = "search"
	}
	if _, ok := d.Modes[d.Test.Mode]; !ok && len(d.Modes) > 0 {
		bad("test.mode %q is not one of the modes", d.Test.Mode)
	}
	if d.Test.Query == "" && d.Test.IMDBID == "" {
		bad("test needs a query or an imdbid")
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	d.compiled = c
	return nil
}

func compileBlock(name string, b *Block) (*compiledBlock, error) {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(name+": "+format, a...)) }
	switch b.Response {
	case "json", "xml", "html":
	default:
		bad("response must be json, xml or html")
	}
	if b.SubRows != "" && b.Response != "json" {
		bad("subRows only applies to json")
	}
	if b.Response != "json" {
		if b.Rows == "" {
			bad("rows selector is required")
		} else if _, err := compileSelector(b.Rows); err != nil {
			bad("rows: %v", err)
		}
		if b.Empty != "" {
			if _, err := compileSelector(b.Empty); err != nil {
				bad("empty: %v", err)
			}
		}
	}
	if b.Pages < 0 || b.Pages > 5 {
		bad("pages must be between 1 and 5")
	}
	if len(b.Requests) == 0 {
		bad("at least one request is required")
	}
	cb := &compiledBlock{b: b, fields: map[string]compiledField{}, skip: map[string]*regexp.Regexp{}}
	for i, r := range b.Requests {
		cr := compiledRequest{r: r, headers: map[string]*template.Template{}}
		switch strings.ToUpper(r.Method) {
		case "", "GET":
			cr.r.Method = "GET"
		case "POST":
			cr.r.Method = "POST"
		default:
			bad("requests[%d]: method must be GET or POST", i)
		}
		for _, c := range r.If {
			if !slices.Contains(conditions, strings.TrimPrefix(c, "!")) {
				bad("requests[%d]: unknown condition %q (use %s)", i, c, strings.Join(conditions, ", "))
			}
		}
		if !strings.HasPrefix(r.Path, "/") {
			bad("requests[%d]: path must start with /", i)
		}
		t, err := newTemplate(r.Path)
		if err != nil {
			bad("requests[%d].path: %v", i, err)
		}
		cr.path = t
		if r.Body != "" {
			if cr.body, err = newTemplate(r.Body); err != nil {
				bad("requests[%d].body: %v", i, err)
			}
		}
		for k, v := range r.Headers {
			if cr.headers[k], err = newTemplate(v); err != nil {
				bad("requests[%d].headers.%s: %v", i, k, err)
			}
		}
		cb.requests = append(cb.requests, cr)
	}
	if _, ok := b.Fields["title"]; !ok {
		bad("fields.title is required")
	}
	_, hasDownload := b.Fields["download"]
	_, hasMagnet := b.Fields["magnet"]
	_, hasHash := b.Fields["infohash"]
	_, hasDetails := b.Fields["details"]
	if !hasDownload && !hasMagnet && !hasHash && !hasDetails {
		bad("fields need download, magnet, infohash or details")
	}
	for k, f := range b.Fields {
		if !slices.Contains(fieldNames, k) {
			bad("unknown field %q (use %s)", k, strings.Join(fieldNames, ", "))
			continue
		}
		cf := compiledField{f: f}
		if f.Select != "" && b.Response != "json" {
			if _, err := compileSelector(f.Select); err != nil {
				bad("fields.%s.select: %v", k, err)
			}
		}
		if f.Regex != "" {
			re, err := regexp.Compile(f.Regex)
			if err != nil {
				bad("fields.%s.regex: %v", k, err)
			}
			cf.regex = re
		}
		cb.fields[k] = cf
	}
	for k, expr := range b.Skip {
		re, err := regexp.Compile(expr)
		if err != nil {
			bad("skip.%s: %v", k, err)
			continue
		}
		if _, ok := b.Fields[k]; !ok {
			bad("skip.%s: no such field", k)
		}
		cb.skip[k] = re
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return cb, nil
}

// SupportsRSS reports whether the site has a feed of its latest releases.
func (d *Definition) SupportsRSS() bool {
	if d.Impl != "" {
		return impls[d.Impl].rss
	}
	if d.RSS != nil {
		return true
	}
	// A search request that needs no search words lists the latest releases.
	if d.compiled != nil && d.compiled.search != nil {
		_, ok := pickRequest(d.compiled.search, Query{Mode: "rss"})
		return ok
	}
	return false
}

// IsAdultCategory reports a Newznab adult (6xxx) category.
func IsAdultCategory(cat int) bool { return cat >= 6000 && cat < 7000 }

// HasAdult reports whether the site carries adult content in categories of
// its own, so adult searches can ask for them.
func (d *Definition) HasAdult() bool {
	return slices.ContainsFunc(d.Categories, func(c CategoryMap) bool { return IsAdultCategory(c.Cat) })
}

// DefaultURL is the site's main address.
func (d *Definition) DefaultURL() string { return d.Links[0] }

// Hosts are the host names of every known address of the site.
func (d *Definition) Hosts() []string {
	var out []string
	for _, l := range d.Links {
		if u, err := url.Parse(l); err == nil {
			out = append(out, strings.ToLower(strings.TrimPrefix(u.Hostname(), "www.")))
		}
	}
	return out
}

// SettingDefaults returns every setting with its default value.
func (d *Definition) SettingDefaults() map[string]string {
	out := map[string]string{}
	for _, s := range d.Settings {
		out[s.Name] = s.Default
	}
	return out
}

// CheckOptions validates per-indexer options against the settings.
func (d *Definition) CheckOptions(opts map[string]string) error {
	for k, v := range opts {
		i := slices.IndexFunc(d.Settings, func(s Setting) bool { return s.Name == k })
		if i < 0 {
			return fmt.Errorf("unknown option %q", k)
		}
		s := d.Settings[i]
		switch s.Type {
		case "bool":
			if v != "true" && v != "false" {
				return fmt.Errorf("option %s must be true or false", k)
			}
		case "select":
			if _, ok := s.Options[v]; !ok {
				return fmt.Errorf("option %s: %q is not one of the choices", k, v)
			}
		case "text":
			if len(v) > 200 {
				return fmt.Errorf("option %s is too long", k)
			}
		}
	}
	return nil
}
