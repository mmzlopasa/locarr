package quality

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/languages"
)

// Errors.
var (
	ErrNotFound  = errors.New("not found")
	ErrNameTaken = errors.New("name already in use")
	ErrDefault   = errors.New("the default delay profile cannot be removed")
)

// InvalidError is a validation failure with an English explanation.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return e.Reason }

func invalid(format string, a ...any) error { return &InvalidError{Reason: fmt.Sprintf(format, a...)} }

// Definition is the size limit for one quality, in MB per minute of runtime.
// Max 0 means no upper limit; Preferred 0 means no preference.
type Definition struct {
	Kind      Kind    `json:"kind"`
	Quality   string  `json:"quality"`
	Min       float64 `json:"min"`
	Preferred float64 `json:"preferred"`
	Max       float64 `json:"max"`
}

type limits struct{ min, pref, max float64 }

var videoDefaults = map[string]limits{
	UnknownVideo: {0, 0, 0}, "workprint": {0, 10, 40}, "cam": {0, 10, 40}, "telesync": {0, 10, 40},
	"telecine": {0, 10, 40}, "regional": {0, 10, 40}, "screener": {0, 10, 40}, "sdtv": {2, 6, 30},
	"dvd": {2, 10, 40}, "dvdr": {10, 50, 100}, "webdl-480p": {2, 6, 40}, "webrip-480p": {2, 6, 40},
	"bluray-480p": {2, 10, 60}, "bluray-576p": {2, 12, 60}, "hdtv-720p": {4, 12, 70}, "webdl-720p": {4, 12, 70},
	"webrip-720p": {4, 12, 70}, "bluray-720p": {6, 20, 100}, "hdtv-1080p": {6, 15, 120}, "webdl-1080p": {6, 20, 130},
	"webrip-1080p": {6, 20, 130}, "bluray-1080p": {8, 35, 155}, "remux-1080p": {20, 120, 400}, "rawhd": {10, 40, 200},
	"hdtv-2160p": {20, 60, 350}, "webdl-2160p": {20, 60, 400}, "webrip-2160p": {20, 60, 400},
	"bluray-2160p": {30, 100, 400}, "remux-2160p": {50, 200, 0}, "brdisk": {50, 0, 0}, "webdl-4320p": {60, 150, 0},
}

var musicDefaults = map[string]limits{
	UnknownMusic: {0, 0, 0}, "mp3-128": {0.5, 0.95, 1.5}, "mp3-192": {0.8, 1.4, 2}, "mp3": {0.5, 2, 3.5},
	"mp3-v2": {0.8, 1.6, 2.5}, "mp3-256": {1.2, 1.9, 2.5}, "mp3-v0": {1.2, 2, 3.5}, "mp3-320": {1.6, 2.4, 3.5},
	"aac": {0.5, 2, 3.5}, "vorbis": {0.3, 1.5, 3}, "opus": {0.3, 1.5, 3}, "alac": {2, 6, 16}, "flac": {2, 6, 16},
	"flac-24": {4, 15, 60}, "wav": {8, 10, 120},
}

// DefaultDefinitions returns the shipped size limits for every quality of every kind.
func DefaultDefinitions() []Definition {
	var out []Definition
	for _, k := range []Kind{Movies, Series, Music} {
		for _, x := range ForKind(k) {
			l := videoDefaults[x.ID]
			if k == Music {
				l = musicDefaults[x.ID]
			}
			out = append(out, Definition{Kind: k, Quality: x.ID, Min: l.min, Preferred: l.pref, Max: l.max})
		}
	}
	return out
}

// Validate checks a definition.
func (d Definition) Validate() error {
	x, ok := ByID(d.Quality)
	if !ok || x.Music != (d.Kind == Music) || !ValidKind(d.Kind) {
		return invalid("unknown quality %q for %s", d.Quality, d.Kind)
	}
	if d.Min < 0 || d.Preferred < 0 || d.Max < 0 || d.Max > 2000 {
		return invalid("%s: sizes must be between 0 and 2000 MB per minute", x.Name)
	}
	if d.Max > 0 && d.Min > d.Max {
		return invalid("%s: minimum %.1f is above maximum %.1f", x.Name, d.Min, d.Max)
	}
	if d.Preferred > 0 && (d.Preferred < d.Min || (d.Max > 0 && d.Preferred > d.Max)) {
		return invalid("%s: preferred %.1f must be between minimum and maximum", x.Name, d.Preferred)
	}
	return nil
}

// Item is one row of a profile: a single quality or a named group of
// qualities that count as equal.
type Item struct {
	Quality   string   `json:"quality,omitempty"`
	Name      string   `json:"name,omitempty"` // group name
	Qualities []string `json:"qualities,omitempty"`
	Allowed   bool     `json:"allowed"`
}

// Key identifies the item for the cutoff: the quality id, or "group:Name".
func (it Item) Key() string {
	if it.Quality != "" {
		return it.Quality
	}
	return "group:" + it.Name
}

// Members are the qualities in the item.
func (it Item) Members() []string {
	if it.Quality != "" {
		return []string{it.Quality}
	}
	return it.Qualities
}

// FormatScore is the score a profile gives one custom format.
type FormatScore struct {
	FormatID int64 `json:"formatId"`
	Score    int   `json:"score"`
}

// Profile is a quality profile.
type Profile struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
	// Items are ordered best first.
	Items           []Item `json:"items"`
	Cutoff          string `json:"cutoff"` // an item key
	UpgradesAllowed bool   `json:"upgradesAllowed"`
	// MinFormatScore rejects releases whose custom format score is lower.
	MinFormatScore int `json:"minFormatScore"`
	// CutoffFormatScore: upgrades stop once a file reaches the cutoff
	// quality and this score.
	CutoffFormatScore int           `json:"cutoffFormatScore"`
	FormatScores      []FormatScore `json:"formatScores"`
	// Languages accepted (ISO codes, "original", "any"); empty = any.
	Languages []string `json:"languages"`
}

// Position returns the quality's place in the profile (0 = best item) and
// whether it is allowed; -1 when the profile does not list it.
func (p *Profile) Position(qualityID string) (int, bool) {
	for i, it := range p.Items {
		for _, m := range it.Members() {
			if m == qualityID {
				return i, it.Allowed
			}
		}
	}
	return -1, false
}

// CutoffPosition is the cutoff item's position (-1 when unset).
func (p *Profile) CutoffPosition() int {
	for i, it := range p.Items {
		if it.Key() == p.Cutoff {
			return i
		}
	}
	return -1
}

// Score returns the profile's score for a custom format.
func (p *Profile) Score(formatID int64) int {
	for _, fs := range p.FormatScores {
		if fs.FormatID == formatID {
			return fs.Score
		}
	}
	return 0
}

// BestAllowed is the position of the best allowed item (-1 when none).
func (p *Profile) BestAllowed() int {
	for i, it := range p.Items {
		if it.Allowed {
			return i
		}
	}
	return -1
}

// ItemName is how an item is shown.
func ItemName(it Item) string {
	if it.Quality != "" {
		return ByIDMust(it.Quality).Name
	}
	return it.Name
}

// Normalize fills in qualities missing from the item list (as not allowed,
// in ladder order at the end) so every quality of the kind appears once.
func (p *Profile) Normalize() {
	seen := map[string]bool{}
	for _, it := range p.Items {
		for _, m := range it.Members() {
			seen[m] = true
		}
	}
	all := ForKind(p.Kind)
	for i := len(all) - 1; i >= 0; i-- {
		if !seen[all[i].ID] {
			p.Items = append(p.Items, Item{Quality: all[i].ID})
		}
	}
	if p.FormatScores == nil {
		p.FormatScores = []FormatScore{}
	}
	if p.Languages == nil {
		p.Languages = []string{}
	}
}

// Validate checks a profile.
func (p *Profile) Validate() error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len(p.Name) > 100 {
		return invalid("the name must be 1 to 100 characters")
	}
	if !ValidKind(p.Kind) {
		return invalid("unknown media kind %q", p.Kind)
	}
	seen := map[string]bool{}
	groups := map[string]bool{}
	allowed := 0
	for _, it := range p.Items {
		if it.Quality != "" && len(it.Qualities) > 0 {
			return invalid("an item is either a quality or a group")
		}
		if it.Quality == "" {
			it.Name = strings.TrimSpace(it.Name)
			if it.Name == "" || len(it.Qualities) == 0 {
				return invalid("a group needs a name and at least one quality")
			}
			if groups[it.Name] {
				return invalid("group %q appears twice", it.Name)
			}
			groups[it.Name] = true
		}
		for _, m := range it.Members() {
			x, ok := ByID(m)
			if !ok || x.Music != (p.Kind == Music) {
				return invalid("quality %q does not apply to %s", m, p.Kind)
			}
			if seen[m] {
				return invalid("quality %s appears twice", x.Name)
			}
			seen[m] = true
		}
		if it.Allowed {
			allowed++
		}
	}
	if allowed == 0 {
		return invalid("allow at least one quality")
	}
	c := p.CutoffPosition()
	if c < 0 {
		return invalid("the cutoff must be one of the profile's items")
	}
	if !p.Items[c].Allowed {
		return invalid("the cutoff must be an allowed quality")
	}
	if p.CutoffFormatScore < p.MinFormatScore && p.CutoffFormatScore != 0 {
		return invalid("the upgrade-until score cannot be below the minimum score")
	}
	ids := map[int64]bool{}
	for _, fs := range p.FormatScores {
		if ids[fs.FormatID] {
			return invalid("a custom format is scored twice")
		}
		ids[fs.FormatID] = true
	}
	for _, l := range p.Languages {
		if !languages.Valid(l) {
			return invalid("unknown language %q", l)
		}
	}
	return nil
}

func group(name string, allowed bool, ids ...string) Item {
	return Item{Name: name, Qualities: ids, Allowed: allowed}
}

func single(id string, allowed bool) Item { return Item{Quality: id, Allowed: allowed} }

// DefaultProfiles are seeded on first start.
func DefaultProfiles() []Profile {
	web := func(res string) Item { return group("WEB "+res, true, "webdl-"+res, "webrip-"+res) }
	hd1080 := func(k Kind) Profile {
		return Profile{Name: "HD-1080p", Kind: k, Items: []Item{single("bluray-1080p", true), web("1080p"), single("hdtv-1080p", true)},
			Cutoff: "group:WEB 1080p", UpgradesAllowed: true}
	}
	hd := func(k Kind) Profile {
		return Profile{Name: "HD-720p/1080p", Kind: k, Items: []Item{single("bluray-1080p", true), web("1080p"),
			single("hdtv-1080p", true), single("bluray-720p", true), web("720p"), single("hdtv-720p", true)},
			Cutoff: "group:WEB 1080p", UpgradesAllowed: true}
	}
	uhd := func(k Kind) Profile {
		return Profile{Name: "UHD", Kind: k, Items: []Item{single("remux-2160p", false), single("bluray-2160p", true), web("2160p"),
			single("hdtv-2160p", true)}, Cutoff: "group:WEB 2160p", UpgradesAllowed: true}
	}
	anyP := func(k Kind) Profile {
		return Profile{Name: "Any", Kind: k, Items: []Item{
			single("bluray-2160p", true), web("2160p"), single("bluray-1080p", true), web("1080p"), single("hdtv-1080p", true),
			single("bluray-720p", true), web("720p"), single("hdtv-720p", true), single("bluray-576p", true),
			single("bluray-480p", true), web("480p"), single("dvd", true), single("sdtv", true)},
			Cutoff: "group:WEB 1080p", UpgradesAllowed: true}
	}
	out := []Profile{anyP(Movies), hd(Movies), hd1080(Movies), uhd(Movies), anyP(Series), hd(Series), hd1080(Series), uhd(Series),
		{Name: "Lossless", Kind: Music, Items: []Item{single("flac-24", true), single("flac", true), single("alac", true)},
			Cutoff: "flac", UpgradesAllowed: true},
		{Name: "Lossy", Kind: Music, Items: []Item{single("mp3-320", true), single("mp3-v0", true), single("aac", true),
			single("opus", true), single("mp3-256", true), single("mp3-v2", true), single("mp3", true)},
			Cutoff: "mp3-320", UpgradesAllowed: true},
		{Name: "Any", Kind: Music, Items: []Item{single("flac-24", true), single("flac", true), single("alac", true),
			single("mp3-320", true), single("mp3-v0", true), single("aac", true), single("opus", true), single("vorbis", true),
			single("mp3-256", true), single("mp3-v2", true), single("mp3", true), single("mp3-192", true)},
			Cutoff: "flac", UpgradesAllowed: true},
	}
	for i := range out {
		out[i].Normalize()
	}
	return out
}
