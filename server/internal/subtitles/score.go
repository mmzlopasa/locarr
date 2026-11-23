package subtitles

import (
	"sort"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
)

// Points of the score (D-064). A hash match means the subtitle was made for
// this very file; everything else adds up evidence from the release name.
const (
	PointsHash       = 100
	PointsIDs        = 20
	PointsYear       = 10
	PointsEpisode    = 20
	PointsGroup      = 25
	PointsSource     = 15
	PointsResolution = 8
	PointsCodec      = 4
	PointsService    = 5
	PointsTrusted    = 5
	PointsPopular    = 3
	PointsHIPrefer   = 3
	MaxScore         = 100
)

// ScorePart is one reason a score went up or down.
type ScorePart struct {
	Code   string `json:"code"`
	Points int    `json:"points"`
}

// Scored is a result with its score for a target and a wanted language.
type Scored struct {
	providers.Result
	Score int         `json:"score"`
	Parts []ScorePart `json:"parts"`
	// Rejected is why the result cannot be used for the wanted entry ("" = usable).
	Rejected string `json:"rejected,omitempty"`
	// Want is the profile entry the result is for.
	Want Want `json:"want"`
}

// sourceGroup folds equivalent sources (a WEB-DL and a WEBRip of one
// stream usually share timing; Blu-ray and its remuxes too).
func sourceGroup(s parser.Source) string {
	switch v := strings.ToLower(string(s)); {
	case v == "":
		return ""
	case strings.Contains(v, "bluray") || strings.Contains(v, "remux") || strings.Contains(v, "bdrip") || strings.Contains(v, "brrip"):
		return "bluray"
	case strings.Contains(v, "web"):
		return "web"
	case strings.Contains(v, "dvd"):
		return "dvd"
	case strings.Contains(v, "hdtv") || strings.Contains(v, "tv"):
		return "tv"
	default:
		return v
	}
}

func codecGroup(r parser.Release) string {
	c := r.Video.Codec
	switch r.Video.Encoder {
	case "x264":
		c = "h264"
	case "x265":
		c = "h265"
	}
	return c
}

// Score rates a result for a target's file and a wanted entry.
func Score(t *Target, file parser.Release, w Want, r providers.Result) Scored {
	sc := Scored{Result: r, Want: w}
	add := func(code string, pts int) {
		sc.Parts = append(sc.Parts, ScorePart{Code: code, Points: pts})
		sc.Score += pts
	}
	// Hard requirements first.
	switch {
	case r.Language != w.Language:
		sc.Rejected = "language"
	case w.Forced && !r.Forced:
		sc.Rejected = "not_forced"
	case !w.Forced && r.Forced:
		sc.Rejected = "forced_only"
	case w.HearingImpaired == HIExclude && r.HearingImpaired:
		sc.Rejected = "hearing_impaired"
	case w.HearingImpaired == HIOnly && !r.HearingImpaired:
		sc.Rejected = "not_hearing_impaired"
	}
	if t.Kind == providers.KindEpisode && sc.Rejected == "" {
		if r.Season > 0 && r.Season != t.Season || r.Episode > 0 && r.Episode != t.Episode && r.Episode != t.Absolute {
			sc.Rejected = "other_episode"
		}
	}
	if t.Kind == providers.KindMovie && sc.Rejected == "" && r.IMDbID != "" && t.IMDbID != "" && !strings.EqualFold(r.IMDbID, t.IMDbID) {
		sc.Rejected = "other_movie"
	}
	if r.HashMatch {
		add("hash", PointsHash)
	} else {
		if r.IMDbID != "" && strings.EqualFold(r.IMDbID, t.IMDbID) || r.TMDbID != 0 && r.TMDbID == t.TMDbID {
			add("ids", PointsIDs)
		}
		if t.Kind == providers.KindMovie {
			if r.Year != 0 && t.Year != 0 && (r.Year == t.Year) {
				add("year", PointsYear)
			}
		} else if r.Season == t.Season && (r.Episode == t.Episode || r.Episode == t.Absolute && t.Absolute > 0) {
			add("episode", PointsEpisode)
		}
		best := 0
		var bestParts []ScorePart
		for _, name := range append(append([]string{}, r.Releases...), strings.TrimSuffix(r.FileName, ".srt")) {
			if strings.TrimSpace(name) == "" {
				continue
			}
			parts, pts := releaseMatch(file, parser.Parse(name))
			if t.Kind == providers.KindMovie && r.Year == 0 && t.Year != 0 && parser.Parse(name).Year == t.Year && !hasPart(sc.Parts, "year") {
				parts = append(parts, ScorePart{Code: "year", Points: PointsYear})
				pts += PointsYear
			}
			if pts > best {
				best, bestParts = pts, parts
			}
		}
		for _, p := range bestParts {
			add(p.Code, p.Points)
		}
	}
	if r.Trusted {
		add("trusted", PointsTrusted)
	}
	if r.Downloads >= 100 || r.Rating >= 7 && r.Votes >= 2 {
		add("popular", PointsPopular)
	}
	switch {
	case w.HearingImpaired == HIPrefer && r.HearingImpaired:
		add("hearing_impaired_preferred", PointsHIPrefer)
	case w.HearingImpaired == HIInclude && r.HearingImpaired:
		add("hearing_impaired", -2)
	}
	if r.MachineTranslated {
		add("machine_translated", -30)
	}
	if r.SeasonPack {
		add("season_pack", -3)
	}
	if sc.Score > MaxScore {
		sc.Score = MaxScore
	}
	if sc.Score < 0 {
		sc.Score = 0
	}
	return sc
}

func hasPart(ps []ScorePart, code string) bool {
	for _, p := range ps {
		if p.Code == code {
			return true
		}
	}
	return false
}

// releaseMatch compares the file's release with a subtitle's release name.
func releaseMatch(file, sub parser.Release) ([]ScorePart, int) {
	var parts []ScorePart
	pts := 0
	add := func(code string, p int) { parts = append(parts, ScorePart{Code: code, Points: p}); pts += p }
	if file.Group != "" && strings.EqualFold(file.Group, sub.Group) {
		add("release_group", PointsGroup)
	}
	if g := sourceGroup(file.Video.Source); g != "" && g == sourceGroup(sub.Video.Source) {
		add("source", PointsSource)
	}
	if file.Video.Resolution != 0 && file.Video.Resolution == sub.Video.Resolution {
		add("resolution", PointsResolution)
	}
	if c := codecGroup(file); c != "" && c == codecGroup(sub) {
		add("codec", PointsCodec)
	}
	if file.StreamingService != "" && strings.EqualFold(file.StreamingService, sub.StreamingService) {
		add("streaming_service", PointsService)
	}
	return parts, pts
}

// Rank sorts scored results: usable first, then by score, then by provider
// order (the order of the input), downloads.
func Rank(list []Scored) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if (a.Rejected == "") != (b.Rejected == "") {
			return a.Rejected == ""
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.Downloads > b.Downloads
	})
}
