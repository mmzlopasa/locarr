package quality

import (
	"strings"
	"time"

	"github.com/dlclark/regexp2"
)

// ReleaseProfile requires or forbids words in release names.
type ReleaseProfile struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Required: the name must contain at least one of these terms.
	Required []string `json:"required"`
	// Ignored: the name must contain none of these terms.
	Ignored []string `json:"ignored"`
	// Tags limit the profile to titles with one of these tags (empty = all).
	Tags []string `json:"tags"`
	// IndexerID limits the profile to one indexer (0 = all).
	IndexerID int64 `json:"indexerId"`
}

// Protocols.
const (
	Usenet  = "usenet"
	Torrent = "torrent"
)

// DelayProfile waits before grabbing so a better release can turn up, and
// says which protocol is preferred.
type DelayProfile struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Order   int    `json:"order"`
	Enabled bool   `json:"enabled"`
	// PreferredProtocol is usenet or torrent.
	PreferredProtocol string `json:"preferredProtocol"`
	UsenetEnabled     bool   `json:"usenetEnabled"`
	TorrentEnabled    bool   `json:"torrentEnabled"`
	UsenetDelay       int    `json:"usenetDelay"`  // minutes
	TorrentDelay      int    `json:"torrentDelay"` // minutes
	// BypassIfHighestQuality skips the delay for the profile's best allowed quality.
	BypassIfHighestQuality bool `json:"bypassIfHighestQuality"`
	// BypassIfAboveScore skips the delay when the custom format score reaches MinimumScore.
	BypassIfAboveScore bool     `json:"bypassIfAboveScore"`
	MinimumScore       int      `json:"minimumScore"`
	Tags               []string `json:"tags"`
	// Default marks the catch-all profile (no tags, always last).
	Default bool `json:"default"`
}

// Delay returns the wait for a protocol.
func (d *DelayProfile) Delay(protocol string) time.Duration {
	if protocol == Usenet {
		return time.Duration(d.UsenetDelay) * time.Minute
	}
	return time.Duration(d.TorrentDelay) * time.Minute
}

// ProtocolEnabled reports whether a protocol may be grabbed.
func (d *DelayProfile) ProtocolEnabled(protocol string) bool {
	if protocol == Usenet {
		return d.UsenetEnabled
	}
	return d.TorrentEnabled
}

// NormTags lower-cases and trims tags, dropping empty and repeated ones.
func NormTags(tags []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// TagsMatch reports whether a profile with tags applies to a title with tags
// (a profile without tags applies to every title).
func TagsMatch(profile, title []string) bool {
	if len(profile) == 0 {
		return true
	}
	for _, p := range profile {
		for _, t := range title {
			if strings.EqualFold(p, t) {
				return true
			}
		}
	}
	return false
}

// Term is a compiled required/ignored term: plain text (case-insensitive
// substring) or /regex/ (with optional i flag; case-insensitive by default).
type Term struct {
	Raw string
	re  *regexp2.Regexp
	low string
}

// regexTimeout bounds one regular expression match.
const regexTimeout = 50 * time.Millisecond

// CompileTerm compiles one term.
func CompileTerm(raw string) (Term, error) {
	t := Term{Raw: raw}
	s := strings.TrimSpace(raw)
	if len(s) >= 2 && strings.HasPrefix(s, "/") && (strings.HasSuffix(s, "/") || strings.HasSuffix(s, "/i")) {
		body := strings.TrimSuffix(strings.TrimSuffix(s[1:], "i"), "/")
		re, err := regexp2.Compile(body, regexp2.IgnoreCase)
		if err != nil {
			return t, invalid("bad regular expression %s: %v", raw, err)
		}
		re.MatchTimeout = regexTimeout
		t.re = re
		return t, nil
	}
	t.low = strings.ToLower(s)
	return t, nil
}

// Match reports whether the term occurs in the release name.
func (t Term) Match(name string) bool {
	if t.re != nil {
		ok, err := t.re.MatchString(name)
		return err == nil && ok
	}
	return t.low != "" && strings.Contains(strings.ToLower(name), t.low)
}

// Validate checks a release profile.
func (r *ReleaseProfile) Validate() error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" || len(r.Name) > 100 {
		return invalid("the name must be 1 to 100 characters")
	}
	r.Required, r.Ignored, r.Tags = cleanTerms(r.Required), cleanTerms(r.Ignored), NormTags(r.Tags)
	if len(r.Required) == 0 && len(r.Ignored) == 0 {
		return invalid("add at least one required or ignored term")
	}
	for _, t := range append(append([]string{}, r.Required...), r.Ignored...) {
		if len(t) > 500 {
			return invalid("a term is longer than 500 characters")
		}
		if _, err := CompileTerm(t); err != nil {
			return err
		}
	}
	return nil
}

func cleanTerms(in []string) []string {
	out := []string{}
	for _, t := range in {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Validate checks a delay profile.
func (d *DelayProfile) Validate() error {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || len(d.Name) > 100 {
		return invalid("the name must be 1 to 100 characters")
	}
	if d.PreferredProtocol != Usenet && d.PreferredProtocol != Torrent {
		return invalid("the preferred protocol must be usenet or torrent")
	}
	if !d.UsenetEnabled && !d.TorrentEnabled {
		return invalid("enable at least one protocol")
	}
	if d.UsenetDelay < 0 || d.TorrentDelay < 0 || d.UsenetDelay > 525600 || d.TorrentDelay > 525600 {
		return invalid("delays must be between 0 and 525600 minutes")
	}
	d.Tags = NormTags(d.Tags)
	if !d.Default && len(d.Tags) == 0 {
		return invalid("a delay profile other than the default needs at least one tag")
	}
	if d.Default {
		d.Tags = []string{}
	}
	return nil
}

// DefaultDelayProfile is the catch-all seeded on first start.
func DefaultDelayProfile() DelayProfile {
	return DelayProfile{Name: "Default", Enabled: true, PreferredProtocol: Usenet, UsenetEnabled: true, TorrentEnabled: true,
		Tags: []string{}, Default: true, Order: 1 << 30}
}

// PickDelayProfile returns the first enabled delay profile (by order) whose
// tags match, falling back to the default.
func PickDelayProfile(list []DelayProfile, tags []string) *DelayProfile {
	var def *DelayProfile
	for i := range list {
		d := &list[i]
		if d.Default {
			def = d
			continue
		}
		if d.Enabled && TagsMatch(d.Tags, tags) {
			return d
		}
	}
	return def
}
