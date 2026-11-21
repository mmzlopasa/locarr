package znab

import (
	"regexp"
	"strings"
	"time"
)

// dateLayouts are the formats seen in real feeds: RFC 1123/822 with and
// without the weekday, numeric or named zones, single-digit days, and ISO 8601
// variants.
var dateLayouts = []string{
	time.RFC1123Z,
	time.RFC1123,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	"Mon, 02 Jan 2006 15:04 -0700",
	"Mon, 2 Jan 2006 15:04 -0700",
	"02 Jan 2006 15:04:05 -0700",
	"2 Jan 2006 15:04:05 -0700",
	"02 Jan 2006 15:04:05 MST",
	"2 Jan 2006 15:04:05 MST",
	time.RFC822Z,
	time.RFC822,
	"Mon, 02 Jan 06 15:04:05 -0700",
	"Mon, 2 Jan 06 15:04:05 -0700",
	"Monday, 02-Jan-06 15:04:05 MST",
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05Z0700",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	time.ANSIC,
	time.UnixDate,
}

var spaces = regexp.MustCompile(`\s+`)

// zoneOffsets are zone names whose offsets Go would otherwise not know
// (time.Parse only knows the local zone and UTC by name).
var zoneOffsets = map[string]string{
	"UT": "+0000", "UTC": "+0000", "GMT": "+0000", "Z": "+0000",
	"EST": "-0500", "EDT": "-0400", "CST": "-0600", "CDT": "-0500",
	"MST": "-0700", "MDT": "-0600", "PST": "-0800", "PDT": "-0700",
	"CET": "+0100", "CEST": "+0200", "BST": "+0100", "EET": "+0200", "EEST": "+0300",
}

// ParseDate parses a feed date; the zero time means it could not be read.
func ParseDate(s string) time.Time {
	s = strings.TrimSpace(spaces.ReplaceAllString(s, " "))
	if s == "" {
		return time.Time{}
	}
	// Replace a trailing zone name with its numeric offset.
	if i := strings.LastIndexByte(s, ' '); i > 0 {
		if off, ok := zoneOffsets[strings.ToUpper(s[i+1:])]; ok {
			s = s[:i] + " " + off
		}
	}
	// "+00:00" at the end of an RFC 822 style date.
	if n := len(s); n > 6 && (s[n-6] == '+' || s[n-6] == '-') && s[n-3] == ':' && strings.Contains(s, ",") {
		s = s[:n-3] + s[n-2:]
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	// Some feeds put the wrong weekday in; retry without it.
	if i := strings.IndexByte(s, ','); i > 0 && i < 12 {
		rest := strings.TrimSpace(s[i+1:])
		for _, l := range dateLayouts {
			if t, err := time.Parse(l, rest); err == nil {
				return t.UTC()
			}
		}
	}
	return time.Time{}
}
