package naming

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Multi-episode styles: how a file holding S01E01–E03 is named (D-041).
const (
	MultiExtend        = "extend"         // S01E01-E02-E03
	MultiDuplicate     = "duplicate"      // S01E01.S01E02.S01E03
	MultiRepeat        = "repeat"         // S01E01E02E03
	MultiScene         = "scene"          // S01E01-02-03
	MultiRange         = "range"          // S01E01-03
	MultiPrefixedRange = "prefixed_range" // S01E01-E03
)

// MultiStyles lists the styles.
var MultiStyles = []string{MultiExtend, MultiDuplicate, MultiRepeat, MultiScene, MultiRange, MultiPrefixedRange}

// ValidMultiStyle reports a known style.
func ValidMultiStyle(s string) bool {
	for _, m := range MultiStyles {
		if m == s {
			return true
		}
	}
	return false
}

// SeriesFolderTokens are the tokens of the series folder template.
var SeriesFolderTokens = []Token{
	{"SeriesTitle", "The Expanse"}, {"SeriesCleanTitle", "The Expanse"}, {"SeriesTitleThe", "Expanse, The"},
	{"SeriesTitleYear", "The Expanse (2015)"}, {"SeriesTitleFirstChar", "E"}, {"Year", "2015"},
	{"TvdbId", "280619"}, {"TmdbId", "63639"}, {"ImdbId", "tt3230854"},
}

// SeasonFolderTokens are the tokens of the season folder template.
var SeasonFolderTokens = append(append([]Token{}, SeriesFolderTokens...), Token{"Season", "01"}, Token{"SeasonNumber", "1"})

// EpisodeTokens are the tokens of the episode file templates.
var EpisodeTokens = append(append([]Token{}, SeasonFolderTokens...),
	Token{"Episode", "01"}, Token{"EpisodeNumber", "1"}, Token{"EpisodeCode", "S01E01"}, Token{"Absolute", "001"},
	Token{"AirDate", "2015-12-14"}, Token{"EpisodeTitle", "Dulcinea"}, Token{"EpisodeCleanTitle", "Dulcinea"},
	Token{"Quality", "WEBDL-1080p"}, Token{"QualityFull", "WEBDL-1080p Proper"}, Token{"Resolution", "1080p"}, Token{"Source", "WEB-DL"},
	Token{"VideoCodec", "x265"}, Token{"VideoBitDepth", "10bit"}, Token{"HDR", "HDR10"}, Token{"AudioCodec", "DDP Atmos"},
	Token{"AudioChannels", "5.1"}, Token{"AudioLanguages", "EN"}, Token{"CustomFormats", "x265"}, Token{"ReleaseGroup", "GROUP"},
	Token{"StreamingService", "AMZN"})

// SeriesInfo is what a series contributes to its names.
type SeriesInfo struct {
	Title  string
	Year   int
	TVDbID int64
	TMDbID int64
	IMDbID string
}

// EpisodeInfo is one episode in a file.
type EpisodeInfo struct {
	Season   int
	Number   int
	Absolute int
	AirDate  string
	Title    string
}

// SeriesValues returns the series folder values.
func SeriesValues(s SeriesInfo) Values {
	v := Values{"SeriesTitle": s.Title, "SeriesCleanTitle": CleanTitle(s.Title), "SeriesTitleThe": TitleThe(s.Title),
		"SeriesTitleYear": s.Title, "ImdbId": s.IMDbID}
	if r := []rune(CleanTitle(TitleThe(s.Title))); len(r) > 0 {
		v["SeriesTitleFirstChar"] = strings.ToUpper(string(r[0]))
	}
	if s.Year > 0 {
		v["Year"] = strconv.Itoa(s.Year)
		// Avoid "Show (2015) (2015)" when the title already carries the year.
		if !strings.Contains(s.Title, strconv.Itoa(s.Year)) {
			v["SeriesTitleYear"] = fmt.Sprintf("%s (%d)", s.Title, s.Year)
		}
	}
	if s.TVDbID > 0 {
		v["TvdbId"] = strconv.FormatInt(s.TVDbID, 10)
	}
	if s.TMDbID > 0 {
		v["TmdbId"] = strconv.FormatInt(s.TMDbID, 10)
	}
	return v
}

// SeasonValues returns the season folder values.
func SeasonValues(s SeriesInfo, season int) Values {
	v := SeriesValues(s)
	v["Season"] = fmt.Sprintf("%02d", season)
	v["SeasonNumber"] = strconv.Itoa(season)
	return v
}

func pad(n, width int) string { return fmt.Sprintf("%0*d", width, n) }

// EpisodeCode renders the season/episode part for one file's episodes in a
// multi-episode style: "S01E01", "S01E01-E03", …
func EpisodeCode(eps []EpisodeInfo, style string) string {
	if len(eps) == 0 {
		return ""
	}
	sorted := append([]EpisodeInfo{}, eps...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Season != sorted[j].Season {
			return sorted[i].Season < sorted[j].Season
		}
		return sorted[i].Number < sorted[j].Number
	})
	first := sorted[0]
	code := func(e EpisodeInfo) string { return fmt.Sprintf("S%02dE%s", e.Season, pad(e.Number, 2)) }
	if len(sorted) == 1 {
		return code(first)
	}
	last := sorted[len(sorted)-1]
	var b strings.Builder
	b.WriteString(code(first))
	switch style {
	case MultiDuplicate:
		for _, e := range sorted[1:] {
			b.WriteString("." + code(e))
		}
	case MultiRepeat:
		for _, e := range sorted[1:] {
			b.WriteString("E" + pad(e.Number, 2))
		}
	case MultiScene:
		for _, e := range sorted[1:] {
			b.WriteString("-" + pad(e.Number, 2))
		}
	case MultiRange:
		b.WriteString("-" + pad(last.Number, 2))
	case MultiExtend:
		for _, e := range sorted[1:] {
			b.WriteString("-E" + pad(e.Number, 2))
		}
	default: // prefixed range
		b.WriteString("-E" + pad(last.Number, 2))
	}
	return b.String()
}

func joinNumbers(nums []int, width int, style string) string {
	if len(nums) == 0 {
		return ""
	}
	if len(nums) > 1 && (style == MultiRange || style == MultiPrefixedRange) {
		return pad(nums[0], width) + "-" + pad(nums[len(nums)-1], width)
	}
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = pad(n, width)
	}
	return strings.Join(parts, "-")
}

// episodeTitles joins the titles of a multi-episode file; "Pilot (1)" and
// "Pilot (2)" become "Pilot".
func episodeTitles(eps []EpisodeInfo) string {
	var titles []string
	seen := map[string]bool{}
	for _, e := range eps {
		t := strings.TrimSpace(e.Title)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		titles = append(titles, t)
	}
	if len(titles) > 1 {
		base := strings.TrimRightFunc(partSuffix.ReplaceAllString(titles[0], ""), unicode.IsSpace)
		same := base != ""
		for _, t := range titles[1:] {
			same = same && strings.TrimRightFunc(partSuffix.ReplaceAllString(t, ""), unicode.IsSpace) == base
		}
		if same {
			return base
		}
	}
	return strings.Join(titles, " + ")
}

// EpisodeValues returns the values for an episode file holding eps.
func EpisodeValues(s SeriesInfo, eps []EpisodeInfo, f *FileInfo, style string) Values {
	season := 0
	if len(eps) > 0 {
		season = eps[0].Season
	}
	v := SeasonValues(s, season)
	if f != nil {
		for k, val := range MovieValues(MovieInfo{}, f) {
			switch k {
			case "Title", "CleanTitle", "TitleThe", "TitleFirstChar", "OriginalTitle", "Year", "Collection", "TmdbId", "ImdbId", "Edition":
				continue
			}
			v[k] = val
		}
		if f.Release != nil {
			v["StreamingService"] = strings.ToUpper(f.Release.StreamingService)
		}
	}
	var nums, abs []int
	for _, e := range eps {
		nums = append(nums, e.Number)
		if e.Absolute > 0 {
			abs = append(abs, e.Absolute)
		}
	}
	sort.Ints(nums)
	sort.Ints(abs)
	v["Episode"] = joinNumbers(nums, 2, style)
	if len(nums) > 0 {
		v["EpisodeNumber"] = strconv.Itoa(nums[0])
	}
	v["EpisodeCode"] = EpisodeCode(eps, style)
	v["Absolute"] = joinNumbers(abs, 3, style)
	if len(eps) > 0 {
		v["AirDate"] = eps[0].AirDate
	}
	v["EpisodeTitle"] = episodeTitles(eps)
	v["EpisodeCleanTitle"] = CleanTitle(v["EpisodeTitle"])
	return v
}

var partSuffix = regexp.MustCompile(`(?i)\s*(\((part\s*)?\d+\)|,?\s*part\s*\d+|\s+\d+)$`)
