package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// ReleasesURL is GitHub's "latest release" API for Locarr.
const ReleasesURL = "https://api.github.com/repos/mmzlopasa/locarr/releases/latest"

// versionEvery is how often GitHub is asked.
const versionEvery = 24 * time.Hour

// Fetched is an HTTP answer as the checker needs it.
type Fetched struct {
	Status int
	Header http.Header
	Body   []byte
}

// VersionChecker asks GitHub's releases API for the newest Locarr (unless the
// administrator turned it off) and measures the clock against the answer's
// Date header.
type VersionChecker struct {
	Settings *settings.Store
	Current  string
	URL      string
	// Fetch does the GET (timeout, size cap and no redirects to another host
	// are the caller's job).
	Fetch func(ctx context.Context, url string) (*Fetched, error)
	Now   func() time.Time

	mu        sync.Mutex
	checkedAt time.Time
	latest    string
	pageURL   string
	skew      *time.Duration
	err       string
}

func (v *VersionChecker) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

// Refresh asks GitHub now.
func (v *VersionChecker) Refresh(ctx context.Context) error {
	url := v.URL
	if url == "" {
		url = ReleasesURL
	}
	sent := v.now()
	res, err := v.Fetch(ctx, url)
	v.mu.Lock()
	defer v.mu.Unlock()
	v.checkedAt = v.now()
	if err != nil {
		v.err = err.Error()
		return err
	}
	if d, perr := http.ParseTime(res.Header.Get("Date")); perr == nil {
		// The server's clock against the middle of the request.
		mid := sent.Add(v.now().Sub(sent) / 2)
		skew := mid.Sub(d)
		v.skew = &skew
	}
	if res.Status != http.StatusOK {
		v.err = fmt.Sprintf("HTTP %d", res.Status)
		return fmt.Errorf("release check: %s", v.err)
	}
	var body struct {
		Tag  string `json:"tag_name"`
		Page string `json:"html_url"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil {
		v.err = "bad answer"
		return err
	}
	v.latest, v.pageURL, v.err = trimVersion(body.Tag), body.Page, ""
	return nil
}

// Latest returns the newest released version ("" = unknown).
func (v *VersionChecker) Latest() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.latest
}

// Issues is the check: a newer version and the clock skew.
func (v *VersionChecker) Issues(ctx context.Context) ([]Issue, error) {
	on, _ := settings.Get(ctx, v.Settings, settings.UpdateCheck)
	if !on {
		return nil, nil
	}
	v.mu.Lock()
	due := v.checkedAt.IsZero() || v.now().Sub(v.checkedAt) > versionEvery
	v.mu.Unlock()
	if due {
		_ = v.Refresh(ctx) // a failed check is not a problem of the user's setup
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []Issue
	if v.latest != "" && NewerVersion(v.latest, v.Current) {
		out = append(out, Issue{Key: "version", Severity: SeverityInfo, Code: "update_available",
			Params:  map[string]any{"current": v.Current, "latest": v.latest, "url": v.pageURL},
			Message: fmt.Sprintf("Locarr %s is available (running %s)", v.latest, v.Current), Docs: "updates"})
	}
	if v.skew != nil && (*v.skew > SkewWarn || *v.skew < -SkewWarn) {
		secs := int64(v.skew.Seconds())
		out = append(out, Issue{Key: "time:skew", Severity: SeverityWarning, Code: "time_skew", Params: map[string]any{"seconds": secs},
			Message: fmt.Sprintf("the system clock is off by %d seconds", secs), Docs: "time-skew"})
	}
	return out, nil
}

// parseVersion reads "1.2.3" (pre-release and build parts ignored); ok is
// false for anything else (such as "dev").
func parseVersion(s string) ([3]int, bool) {
	var out [3]int
	s = trimVersion(s)
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// NewerVersion reports whether latest is newer than current. Builds that are
// not a release ("dev") are never told to update.
func NewerVersion(latest, current string) bool {
	l, ok1 := parseVersion(latest)
	c, ok2 := parseVersion(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range 3 {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}
