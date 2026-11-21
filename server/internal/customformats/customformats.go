// Package customformats implements custom formats: named sets of conditions
// on a release (title regex, source, resolution, codec, HDR, audio, group,
// language, edition, indexer flag, size, streaming service, …) that quality
// profiles score. It also reads and writes the commonly shared custom-format
// JSON (name, specifications[] with implementation, negate, required and
// fields.value) so formats from public guides can be pasted in.
package customformats

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/dlclark/regexp2"

	"github.com/mmzlopasa/locarr/server/internal/languages"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
)

// Implementations.
const (
	ReleaseTitle     = "ReleaseTitleSpecification"
	ReleaseGroup     = "ReleaseGroupSpecification"
	Edition          = "EditionSpecification"
	Source           = "SourceSpecification"
	Resolution       = "ResolutionSpecification"
	QualityModifier  = "QualityModifierSpecification"
	Language         = "LanguageSpecification"
	IndexerFlag      = "IndexerFlagSpecification"
	Size             = "SizeSpecification"
	ReleaseType      = "ReleaseTypeSpecification"
	Year             = "YearSpecification"
	VideoCodec       = "VideoCodecSpecification"
	HDR              = "HdrSpecification"
	AudioCodec       = "AudioCodecSpecification"
	StreamingService = "StreamingServiceSpecification"
)

// Implementations lists the supported implementations in display order.
var Implementations = []string{ReleaseTitle, Source, Resolution, VideoCodec, HDR, AudioCodec, ReleaseGroup, Language,
	Edition, IndexerFlag, Size, StreamingService, QualityModifier, ReleaseType, Year}

func supported(impl string) bool {
	for _, i := range Implementations {
		if i == impl {
			return true
		}
	}
	return false
}

// Values accepted by the enumerated conditions.
var (
	Sources       = []string{"cam", "telesync", "telecine", "workprint", "dvd", "tv", "tvraw", "webdl", "webrip", "bluray", "blurayraw"}
	Modifiers     = []string{"none", "regional", "screener", "rawhd", "brdisk", "remux"}
	Resolutions   = []int{360, 480, 540, 576, 720, 1080, 2160, 4320}
	VideoCodecs   = []string{"h264", "h265", "av1", "vc1", "mpeg2", "xvid", "divx", "vp9"}
	HDRFormats    = []string{"HDR10", "HDR10+", "DV", "HLG", "SDR"}
	AudioCodecs   = []string{"aac", "ac3", "eac3", "truehd", "atmos", "dts", "dts-es", "dts-hd hra", "dts-hd ma", "dts:x", "flac", "opus", "pcm", "mp3"}
	ReleaseTypes  = []string{"single", "multi", "season"}
	IndexerFlags  = []string{"freeleech", "halfleech", "double_upload", "golden", "approved", "internal", "scene", "freeleech75", "freeleech25", "nuked"}
	regexpTimeout = 50 * time.Millisecond
)

// Spec is one condition.
type Spec struct {
	Name           string         `json:"name"`
	Implementation string         `json:"implementation"`
	Negate         bool           `json:"negate"`
	Required       bool           `json:"required"`
	Fields         map[string]any `json:"fields"`
}

// Value returns the "value" field.
func (s Spec) Value() any { return s.Fields["value"] }

// Format is a custom format.
type Format struct {
	ID                  int64  `json:"id"`
	Name                string `json:"name"`
	IncludeWhenRenaming bool   `json:"includeWhenRenaming"`
	Specs               []Spec `json:"specifications"`
}

// Input is what a format is matched against.
type Input struct {
	Title        string
	Release      *parser.Release
	Quality      quality.Quality
	Size         int64 // bytes, 0 = unknown
	IndexerFlags []string
	// OriginalLanguage is the title's original language, when known.
	OriginalLanguage string
}

// InvalidError is a validation failure.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return e.Reason }

func invalid(format string, a ...any) error { return &InvalidError{Reason: fmt.Sprintf(format, a...)} }

type compiledSpec struct {
	Spec
	re          *regexp2.Regexp
	str         string
	num         float64
	min, max    float64
	unsupported bool
}

// Compiled is a format ready to match.
type Compiled struct {
	Format
	specs []compiledSpec
	// Unsupported names conditions with an implementation Locarr does not
	// know: optional ones are ignored, required ones stop the format applying.
	Unsupported []string
}

func asString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return fmt.Sprint(x), true
	}
	return "", false
}

func asNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case string:
		var f float64
		if _, err := fmt.Sscan(strings.TrimSpace(x), &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

func oneOf(v string, list []string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

// Compile checks a format and prepares it for matching.
func Compile(f Format) (*Compiled, error) {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" || len(f.Name) > 100 {
		return nil, invalid("the name must be 1 to 100 characters")
	}
	if len(f.Specs) == 0 {
		return nil, invalid("%s: add at least one condition", f.Name)
	}
	if len(f.Specs) > 100 {
		return nil, invalid("%s: at most 100 conditions", f.Name)
	}
	c := &Compiled{Format: f}
	for _, s := range f.Specs {
		if s.Fields == nil {
			s.Fields = map[string]any{}
		}
		cs := compiledSpec{Spec: s}
		label := s.Name
		if label == "" {
			label = s.Implementation
		}
		if !supported(s.Implementation) {
			cs.unsupported = true
			c.Unsupported = append(c.Unsupported, label)
			c.specs = append(c.specs, cs)
			continue
		}
		v := s.Value()
		switch s.Implementation {
		case ReleaseTitle, ReleaseGroup, Edition:
			str, ok := asString(v)
			if !ok || strings.TrimSpace(str) == "" || len(str) > 2000 {
				return nil, invalid("%s: condition %q needs a regular expression", f.Name, label)
			}
			re, err := regexp2.Compile(str, regexp2.IgnoreCase)
			if err != nil {
				return nil, invalid("%s: condition %q has a bad regular expression: %v", f.Name, label, err)
			}
			re.MatchTimeout = regexpTimeout
			cs.re = re
		case Source, QualityModifier, VideoCodec, HDR, AudioCodec, ReleaseType, IndexerFlag, Language, StreamingService:
			str, ok := asString(v)
			str = strings.TrimSpace(str)
			if !ok || str == "" {
				return nil, invalid("%s: condition %q needs a value", f.Name, label)
			}
			lists := map[string][]string{Source: Sources, QualityModifier: Modifiers, VideoCodec: VideoCodecs, HDR: HDRFormats,
				AudioCodec: AudioCodecs, ReleaseType: ReleaseTypes, IndexerFlag: IndexerFlags}
			if list, ok := lists[s.Implementation]; ok && !oneOf(str, list) {
				return nil, invalid("%s: condition %q: %q is not one of %s", f.Name, label, str, strings.Join(list, ", "))
			}
			if s.Implementation == Language && !languages.Valid(strings.ToLower(str)) {
				return nil, invalid("%s: condition %q: unknown language %q", f.Name, label, str)
			}
			cs.str = str
		case Resolution:
			n, ok := asNumber(v)
			if !ok || n <= 0 {
				return nil, invalid("%s: condition %q needs a resolution", f.Name, label)
			}
			cs.num = n
		case Size, Year:
			mn, _ := asNumber(s.Fields["min"])
			mx, ok := asNumber(s.Fields["max"])
			if !ok {
				mx = 0
			}
			if mn < 0 || mx < 0 || (mx > 0 && mn > mx) {
				return nil, invalid("%s: condition %q: minimum must not be above maximum", f.Name, label)
			}
			cs.min, cs.max = mn, mx
		}
		c.specs = append(c.specs, cs)
	}
	return c, nil
}

// Result tells how one condition did.
type Result struct {
	Name           string `json:"name"`
	Implementation string `json:"implementation"`
	Required       bool   `json:"required"`
	Negate         bool   `json:"negate"`
	// Passed is the outcome after negation.
	Passed      bool `json:"passed"`
	Unsupported bool `json:"unsupported,omitempty"`
}

// Evaluate matches the format and explains each condition. A format applies
// when, for every condition type it uses, all required conditions pass and
// (if the type has non-required conditions) at least one of those passes.
func (c *Compiled) Evaluate(in Input) (bool, []Result) {
	results := make([]Result, 0, len(c.specs))
	type groupState struct{ anyOptional, optionalPassed, requiredFailed bool }
	groups := map[string]*groupState{}
	for i := range c.specs {
		s := &c.specs[i]
		passed := false
		if !s.unsupported {
			passed = s.match(in) != s.Negate
		}
		results = append(results, Result{Name: s.Name, Implementation: s.Implementation, Required: s.Required, Negate: s.Negate,
			Passed: passed, Unsupported: s.unsupported})
		if s.unsupported && !s.Required {
			// An unknown optional condition is left out; an unknown required
			// one means the format can never apply.
			continue
		}
		g := groups[s.Implementation]
		if g == nil {
			g = &groupState{}
			groups[s.Implementation] = g
		}
		if s.Required {
			if !passed {
				g.requiredFailed = true
			}
		} else {
			g.anyOptional = true
			if passed {
				g.optionalPassed = true
			}
		}
	}
	ok := len(c.specs) > 0
	for _, g := range groups {
		if g.requiredFailed || (g.anyOptional && !g.optionalPassed) {
			ok = false
		}
	}
	return ok, results
}

// Matches reports whether the format applies.
func (c *Compiled) Matches(in Input) bool {
	ok, _ := c.Evaluate(in)
	return ok
}

func (s *compiledSpec) match(in Input) bool {
	r := in.Release
	if r == nil {
		r = &parser.Release{}
	}
	switch s.Implementation {
	case ReleaseTitle:
		return reMatch(s.re, in.Title)
	case ReleaseGroup:
		return r.Group != "" && reMatch(s.re, r.Group)
	case Edition:
		for _, e := range r.Editions {
			if reMatch(s.re, e) {
				return true
			}
		}
		return false
	case Source:
		return sourceOf(r, in.Quality) == strings.ToLower(s.str)
	case QualityModifier:
		mod := in.Quality.Modifier
		if mod == "" || mod == "dvdr" {
			mod = "none"
		}
		return mod == strings.ToLower(s.str)
	case Resolution:
		res := r.Video.Resolution
		if res == 0 {
			res = in.Quality.Resolution
		}
		return res == int(s.num)
	case VideoCodec:
		return strings.EqualFold(r.Video.Codec, s.str) || (s.str == "h264" && r.Video.Codec == "hi10p")
	case HDR:
		if strings.EqualFold(s.str, "SDR") {
			return len(r.Video.HDR) == 0 && r.Video.Resolution >= 2160 || r.HasFlag("sdr")
		}
		for _, h := range r.Video.HDR {
			if strings.EqualFold(h, s.str) {
				return true
			}
		}
		return false
	case AudioCodec:
		if strings.EqualFold(s.str, "atmos") {
			return r.Audio.Atmos
		}
		for _, a := range r.Audio.Codecs {
			if strings.EqualFold(a, s.str) {
				return true
			}
		}
		return false
	case StreamingService:
		return r.StreamingService != "" && strings.EqualFold(r.StreamingService, s.str)
	case IndexerFlag:
		for _, f := range in.IndexerFlags {
			if strings.EqualFold(f, s.str) {
				return true
			}
		}
		return false
	case Language:
		return languageMatch(r, strings.ToLower(s.str), in.OriginalLanguage, s.Fields["exceptLanguage"] == true)
	case ReleaseType:
		switch s.str {
		case "season":
			return r.IsSeasonPack()
		case "multi":
			return len(r.Episodes) > 1 || len(r.AbsoluteEpisodes) > 1
		case "single":
			return len(r.Episodes) == 1 || (len(r.Episodes) == 0 && len(r.AbsoluteEpisodes) == 1)
		}
	case Size:
		if in.Size <= 0 {
			return false
		}
		gb := float64(in.Size) / (1 << 30)
		return gb >= s.min && (s.max == 0 || gb <= s.max)
	case Year:
		y := float64(r.Year)
		return y > 0 && y >= s.min && (s.max == 0 || y <= s.max)
	}
	return false
}

func reMatch(re *regexp2.Regexp, s string) bool {
	if re == nil {
		return false
	}
	ok, err := re.MatchString(s)
	return err == nil && ok
}

// sourceOf maps a release to the custom-format source names.
func sourceOf(r *parser.Release, q quality.Quality) string {
	switch r.Video.Source {
	case parser.SourceCam:
		return "cam"
	case parser.SourceTelesync:
		return "telesync"
	case parser.SourceTelecine:
		return "telecine"
	case parser.SourceWorkprint:
		return "workprint"
	case parser.SourceDVD, parser.SourceDVDR, parser.SourceScreener, parser.SourceRegional:
		return "dvd"
	case parser.SourceSDTV, parser.SourcePDTV, parser.SourceHDTV, parser.SourceVHS:
		return "tv"
	case parser.SourceRawHD:
		return "tvraw"
	case parser.SourceWEB, parser.SourceWEBDL:
		return "webdl"
	case parser.SourceWEBRip, parser.SourceHDRip:
		return "webrip"
	case parser.SourceBluRay, parser.SourceBDRip, parser.SourceBRRip, parser.SourceRemux:
		return "bluray"
	case parser.SourceBRDisk:
		return "blurayraw"
	}
	switch q.Source {
	case "tv":
		return "tv"
	case "webdl", "webrip", "bluray", "dvd":
		return q.Source
	}
	return "unknown"
}

// ReleaseLanguages are the release's spoken languages; a name that names
// none is taken to be English (the convention for untagged releases).
func ReleaseLanguages(r *parser.Release) []string {
	if len(r.Languages) == 0 {
		return []string{"en"}
	}
	return r.Languages
}

func languageMatch(r *parser.Release, code, original string, except bool) bool {
	langs := ReleaseLanguages(r)
	if code == languages.Original {
		code = original
		if code == "" {
			return false
		}
	}
	if code == languages.Any {
		return true
	}
	for _, l := range langs {
		if except && l != code {
			return true
		}
		if !except && (l == code || strings.HasPrefix(l, code+"-")) {
			return true
		}
	}
	return false
}

// Match is a format that applies, with the profile's score for it.
type Match struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Score int    `json:"score"`
}

// Score returns the formats that apply (by score, then name) and the total.
func Score(formats []*Compiled, p *quality.Profile, in Input) ([]Match, int) {
	out := []Match{}
	total := 0
	for _, f := range formats {
		if !f.Matches(in) {
			continue
		}
		s := 0
		if p != nil {
			s = p.Score(f.ID)
		}
		total += s
		out = append(out, Match{ID: f.ID, Name: f.Name, Score: s})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Name < out[j].Name
	})
	if total > math.MaxInt32 {
		total = math.MaxInt32
	}
	return out, total
}

// CompileAll compiles stored formats, skipping (and reporting) broken ones.
func CompileAll(list []Format) ([]*Compiled, []error) {
	var out []*Compiled
	var errs []error
	for _, f := range list {
		c, err := Compile(f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, c)
	}
	return out, errs
}
