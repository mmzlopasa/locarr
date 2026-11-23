package znab

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Kind classifies an indexer error.
type Kind string

// Error kinds.
const (
	KindAuth        Kind = "auth"         // wrong or missing API key, no permission
	KindRateLimit   Kind = "rate_limit"   // request or download limit reached
	KindUnsupported Kind = "unsupported"  // function or parameter not supported
	KindDisabled    Kind = "disabled"     // API or account disabled
	KindNotFound    Kind = "not_found"    // no such item
	KindBadResponse Kind = "bad_response" // not a Newznab/Torznab answer
	KindUnreachable Kind = "unreachable"  // network error, timeout, 5xx
	KindUnknown     Kind = "unknown"
)

// Error is an error reported by (or about) an indexer.
type Error struct {
	Kind    Kind
	Code    int // the Newznab error code, or the HTTP status, when known
	Message string
	// RetryAfter is a server-suggested wait in seconds (0 = none).
	RetryAfter int
}

func (e *Error) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("indexer error %d (%s): %s", e.Code, e.Kind, e.Message)
	}
	return fmt.Sprintf("indexer error (%s): %s", e.Kind, e.Message)
}

// KindOf returns the kind of err (KindUnknown when it is not an *Error).
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindUnknown
}

// kindForCode maps Newznab error codes (spec: 1xx account, 2xx request,
// 3xx content, 5xx limits, 9xx other) to kinds.
func kindForCode(code int) Kind {
	switch {
	case code == 101 || code == 910:
		return KindDisabled
	case code >= 100 && code < 200:
		return KindAuth
	case code >= 200 && code < 300:
		return KindUnsupported
	case code >= 300 && code < 400:
		return KindNotFound
	case code == 429 || (code >= 500 && code < 600):
		return KindRateLimit
	}
	return KindUnknown
}

// detectError returns an *Error when body is an <error code=… description=…/> document.
func detectError(body []byte) error {
	head := body[:min(len(body), 2048)]
	i := bytes.Index(head, []byte("<error"))
	if i < 0 {
		return nil
	}
	// Only when <error> is the root element (not an item title mentioning it).
	before := bytes.TrimSpace(head[:i])
	if len(before) > 0 && !bytes.HasSuffix(before, []byte("?>")) && !bytes.HasPrefix(before, []byte("<?xml")) {
		return nil
	}
	var x struct {
		Code        string `xml:"code,attr"`
		Description string `xml:"description,attr"`
		Text        string `xml:",chardata"`
	}
	if err := decode(body, &x); err != nil {
		return nil
	}
	code, _ := strconv.Atoi(strings.TrimSpace(x.Code))
	msg := strings.TrimSpace(x.Description)
	if msg == "" {
		msg = strings.TrimSpace(x.Text)
	}
	if msg == "" {
		msg = "unspecified error"
	}
	return &Error{Kind: kindForCode(code), Code: code, Message: msg}
}

// httpError classifies a non-2xx HTTP status (when the body is not an <error>).
func httpError(status int, retryAfter string) *Error {
	e := &Error{Code: status, Message: fmt.Sprintf("HTTP %d", status)}
	switch {
	case status == 401 || status == 403:
		e.Kind = KindAuth
	case status == 404:
		e.Kind = KindBadResponse
		e.Message = "HTTP 404: check the URL and API path"
	case status == 429:
		e.Kind = KindRateLimit
		e.RetryAfter, _ = strconv.Atoi(strings.TrimSpace(retryAfter))
	case status >= 500:
		e.Kind = KindUnreachable
	default:
		e.Kind = KindUnknown
	}
	return e
}

var disabledTill = regexp.MustCompile(`(?i)disabled\s+(?:till|until)\s+(.+?)(?:\s+due\s+to|\s*\.?\s*$)`)

// disabledLayouts are the date forms a "disabled till <time>" message has been
// seen with (the time is the indexer manager's local time).
var disabledLayouts = []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02 15:04",
	"1/2/2006 3:04:05 PM", "1/2/2006 15:04:05", "01/02/2006 15:04:05", "2.1.2006. 15:04:05", "2.1.2006 15:04:05",
	"02.01.2006 15:04:05", "2/1/2006 15:04:05"}

// DisabledUntil reads the time out of a message such as Prowlarr's "Indexer is
// disabled till 11/23/2026 12:53:01 due to recent failures". Times without a
// zone are read in loc. ok is false when there is no such time.
func DisabledUntil(msg string, loc *time.Location) (time.Time, bool) {
	m := disabledTill.FindStringSubmatch(msg)
	if m == nil {
		return time.Time{}, false
	}
	v := strings.TrimSpace(m[1])
	for _, l := range disabledLayouts {
		if t, err := time.ParseInLocation(l, v, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
