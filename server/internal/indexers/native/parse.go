package native

import (
	"encoding/base32"
	"encoding/hex"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// templateFuncs are the only functions a definition's templates may call.
var templateFuncs = template.FuncMap{
	"urlquery":   url.QueryEscape,
	"pathescape": url.PathEscape,
	"join":       func(sep string, l []string) string { return strings.Join(l, sep) },
	"first": func(l []string, def string) string {
		if len(l) == 0 {
			return def
		}
		return l[0]
	},
	"default": func(def, v string) string {
		if strings.TrimSpace(v) == "" {
			return def
		}
		return v
	},
	"lower":   strings.ToLower,
	"replace": func(old, new, s string) string { return strings.ReplaceAll(s, old, new) },
	// dashed turns "Big Buck Bunny" into "Big-Buck-Bunny" (path-style searches).
	"dashed": func(s string) string { return strings.Join(strings.Fields(s), "-") },
	"pad2": func(s string) string {
		if n, err := strconv.Atoi(s); err == nil {
			return twoDigits(n)
		}
		return s
	},
	"json": func(s string) string {
		q := strconv.Quote(s)
		return q
	},
}

func newTemplate(s string) (*template.Template, error) {
	return template.New("").Option("missingkey=zero").Funcs(templateFuncs).Parse(s)
}

func twoDigits(n int) string {
	if n < 10 && n >= 0 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

var (
	hexHash    = regexp.MustCompile(`(?i)\b([0-9a-f]{40})\b`)
	base32Hash = regexp.MustCompile(`(?i)\b([a-z2-7]{32})\b`)
	btih       = regexp.MustCompile(`(?i)xt=urn:btih:([a-z0-9]{32,40})`)
	imdbRe     = regexp.MustCompile(`(?i)(?:tt)?0*(\d{4,9})`)
)

// NormalizeHash returns a lower-case hex info hash from a hex or base32
// hash, a magnet link or a URL that contains one ("" when there is none).
func NormalizeHash(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if m := btih.FindStringSubmatch(s); m != nil {
		s = m[1]
	}
	if m := hexHash.FindStringSubmatch(s); m != nil {
		return strings.ToLower(m[1])
	}
	if m := base32Hash.FindStringSubmatch(s); m != nil {
		b, err := base32.StdEncoding.DecodeString(strings.ToUpper(m[1]))
		if err == nil && len(b) == 20 {
			return hex.EncodeToString(b)
		}
	}
	return ""
}

// Magnet builds a magnet link from an info hash, a display name and trackers.
func Magnet(hash, name string, trackers []string) string {
	v := "magnet:?xt=urn:btih:" + hash
	if name != "" {
		v += "&dn=" + url.QueryEscape(name)
	}
	for _, t := range trackers {
		if t = strings.TrimSpace(t); t != "" {
			v += "&tr=" + url.QueryEscape(t)
		}
	}
	return v
}

// DefaultTrackers is the standard public tracker list added to magnets that
// Locarr builds from a bare info hash (administrators can change it).
var DefaultTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://open.demonii.com:1337/announce",
	"udp://tracker.dler.org:6969/announce",
}

var sizeRe = regexp.MustCompile(`(?i)([\d.,]+)\s*([kmgtp]?i?b|bytes?|[kmgtp])?\b`)

// ParseSize reads "6.6 GiB", "848.96 MB", "1,234 KB" or a plain byte count.
// Torrent sites mean binary units whatever they write.
func ParseSize(s string) int64 {
	s = strings.TrimSpace(strings.ReplaceAll(s, " ", " "))
	if s == "" {
		return 0
	}
	m := sizeRe.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	num := m[1]
	// "1,234.5" and "1.234,5": the last separator is the decimal one when it
	// has one or two digits after it.
	if i := strings.LastIndexAny(num, ".,"); i >= 0 && len(num)-i-1 <= 2 && num[i] == ',' {
		num = strings.ReplaceAll(num[:i], ".", "") + "." + num[i+1:]
	} else {
		num = strings.ReplaceAll(num, ",", "")
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil || f < 0 {
		return 0
	}
	mult := 1.0
	switch u := strings.ToLower(m[2]); {
	case strings.HasPrefix(u, "k"):
		mult = 1 << 10
	case strings.HasPrefix(u, "m"):
		mult = 1 << 20
	case strings.HasPrefix(u, "g"):
		mult = 1 << 30
	case strings.HasPrefix(u, "t"):
		mult = 1 << 40
	case strings.HasPrefix(u, "p"):
		mult = 1 << 50
	}
	v := f * mult
	if v > math.MaxInt64/2 {
		return 0
	}
	return int64(math.Round(v))
}

// ParseInt reads "1,234", "1 234" or "12k" ("" or junk → -1).
func ParseInt(s string) int {
	s = strings.TrimSpace(strings.NewReplacer(",", "", " ", "", " ", "").Replace(s))
	if s == "" {
		return -1
	}
	mult := 1.0
	switch {
	case strings.HasSuffix(strings.ToLower(s), "k"):
		mult, s = 1000, s[:len(s)-1]
	case strings.HasSuffix(strings.ToLower(s), "m"):
		mult, s = 1e6, s[:len(s)-1]
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return -1
	}
	return int(f * mult)
}

// ParseIMDB returns "tt0123456" from any spelling of an IMDb id.
func ParseIMDB(s string) string {
	m := imdbRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return ""
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n == 0 {
		return ""
	}
	id := strconv.Itoa(n)
	for len(id) < 7 {
		id = "0" + id
	}
	return "tt" + id
}

var dateLayouts = []string{
	time.RFC1123Z, time.RFC1123, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST", "2 Jan 2006 15:04:05 -0700",
	"02 Jan 2006 15:04:05 -0700", time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05",
	"2006-01-02 15:04", "2006-01-02", "01/02/2006", "Jan 2, 2006",
}

var (
	relRe     = regexp.MustCompile(`(?i)(\d+|an?|one)\s*(sec|second|min|minute|hour|hr|day|week|month|mo|year|yr)s?\+?(?:\s+ago)?`)
	ordinalRe = regexp.MustCompile(`(?i)(\d+)(st|nd|rd|th)\b`)
	clockRe   = regexp.MustCompile(`(?i)^(\d{1,2})(?::(\d{2}))?\s*(am|pm)\s*`)
	yearTail  = regexp.MustCompile(`'(\d{2})$`)
)

// ParseDate reads the many date styles torrent sites use: absolute dates
// (RFC 1123, ISO, Unix seconds), relative ones ("9 hours ago", "1 Year+",
// "yesterday") and short ones ("Nov. 27th '25", "9am Nov. 14th"). layouts
// are tried first. The zero time means "unknown".
func ParseDate(s string, layouts []string, now time.Time) time.Time {
	s = strings.TrimSpace(strings.ReplaceAll(s, " ", " "))
	if s == "" {
		return time.Time{}
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		switch {
		case n > 1e11: // milliseconds
			return time.UnixMilli(n).UTC()
		case n > 1e8:
			return time.Unix(n, 0).UTC()
		}
		return time.Time{}
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "today") || strings.HasPrefix(low, "just now"):
		return now.UTC()
	case strings.HasPrefix(low, "yesterday"):
		return now.UTC().Add(-24 * time.Hour)
	}
	if m := relRe.FindStringSubmatch(s); m != nil && (strings.Contains(low, "ago") || strings.Contains(low, "+") || len(strings.Fields(low)) <= 3) {
		n := 1
		if v, err := strconv.Atoi(m[1]); err == nil {
			n = v
		}
		var unit time.Duration
		switch u := strings.ToLower(m[2]); {
		case strings.HasPrefix(u, "sec"):
			unit = time.Second
		case strings.HasPrefix(u, "min"):
			unit = time.Minute
		case strings.HasPrefix(u, "h"):
			unit = time.Hour
		case strings.HasPrefix(u, "d"):
			unit = 24 * time.Hour
		case strings.HasPrefix(u, "w"):
			unit = 7 * 24 * time.Hour
		case strings.HasPrefix(u, "mo"):
			unit = 30 * 24 * time.Hour
		default:
			unit = 365 * 24 * time.Hour
		}
		return now.UTC().Add(-time.Duration(n) * unit)
	}
	return parseShort(s, now)
}

// parseShort reads "Nov. 27th '25", "9am Nov. 14th", "Nov. 14th" (this year,
// or last year when that would be in the future) and "10:12am" (today).
func parseShort(s string, now time.Time) time.Time {
	now = now.UTC()
	hour, minute := 0, 0
	if m := clockRe.FindStringSubmatch(s); m != nil {
		hour, _ = strconv.Atoi(m[1])
		minute, _ = strconv.Atoi(m[2])
		if strings.EqualFold(m[3], "pm") && hour < 12 {
			hour += 12
		}
		if strings.EqualFold(m[3], "am") && hour == 12 {
			hour = 0
		}
		s = strings.TrimSpace(s[len(m[0]):])
		if s == "" {
			return time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
		}
	}
	year := 0
	if m := yearTail.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[1])
		year = 2000 + y
		s = strings.TrimSpace(yearTail.ReplaceAllString(s, ""))
	}
	s = ordinalRe.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, ".", "")
	for _, l := range []string{"Jan 2", "January 2", "2 Jan", "2 January"} {
		t, err := time.Parse(l, s)
		if err != nil {
			continue
		}
		y := year
		if y == 0 {
			y = now.Year()
		}
		d := time.Date(y, t.Month(), t.Day(), hour, minute, 0, 0, time.UTC)
		if year == 0 && d.After(now.Add(48*time.Hour)) {
			d = d.AddDate(-1, 0, 0)
		}
		return d
	}
	return time.Time{}
}
