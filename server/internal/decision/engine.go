package decision

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/mmzlopasa/locarr/server/internal/customformats"
	"github.com/mmzlopasa/locarr/server/internal/languages"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
)

// Rejection codes.
const (
	CodeParseFailed       = "parse_failed"
	CodeWrongKind         = "wrong_kind"
	CodeWrongIDs          = "wrong_ids"
	CodeWrongTitle        = "wrong_title"
	CodeWrongYear         = "wrong_year"
	CodeWrongSeason       = "wrong_season"
	CodeWrongEpisode      = "wrong_episode"
	CodeQualityNotAllowed = "quality_not_allowed"
	CodeUpgradesDisabled  = "upgrades_disabled"
	CodeCutoffMet         = "cutoff_met"
	CodeNotAnUpgrade      = "not_an_upgrade"
	CodeScoreTooLow       = "custom_format_score_too_low"
	CodeSizeTooSmall      = "size_too_small"
	CodeSizeTooLarge      = "size_too_large"
	CodeLanguage          = "language_not_wanted"
	CodeMustContain       = "must_contain_missing"
	CodeMustNotContain    = "must_not_contain_found"
	CodeBlocklisted       = "blocklisted"
	CodeSeeders           = "not_enough_seeders"
	CodeTooOld            = "too_old"
	CodeTooNew            = "too_new"
	CodeSample            = "sample"
	CodeHardcodedSubs     = "hardcoded_subs"
	CodeThreeD            = "three_d"
	CodeIndexerDisabled   = "indexer_disabled"
	CodeIndexerLimited    = "indexer_limited"
	CodeProtocolDisabled  = "protocol_disabled"
	CodeDelayed           = "delayed"
)

// Codes lists every rejection code (for documentation and clients).
var Codes = []string{CodeParseFailed, CodeWrongKind, CodeWrongIDs, CodeWrongTitle, CodeWrongYear, CodeWrongSeason,
	CodeWrongEpisode, CodeQualityNotAllowed, CodeUpgradesDisabled, CodeCutoffMet, CodeNotAnUpgrade, CodeScoreTooLow,
	CodeSizeTooSmall, CodeSizeTooLarge, CodeLanguage, CodeMustContain, CodeMustNotContain, CodeBlocklisted, CodeSeeders,
	CodeTooOld, CodeTooNew, CodeSample, CodeHardcodedSubs, CodeThreeD, CodeIndexerDisabled, CodeIndexerLimited,
	CodeProtocolDisabled, CodeDelayed}

// Typical runtimes when the wanted item does not say.
var defaultRuntime = map[quality.Kind]int{quality.Movies: 110, quality.Series: 45, quality.Music: 45}

// eval is the work on one candidate.
type eval struct {
	ctx     *Context
	c       *Candidate
	r       parser.Release
	q       quality.Quality
	pos     int
	allowed bool
	score   int
	matches []customformats.Match
	out     []Rejection
}

func (e *eval) reject(code, msg string, params map[string]any) {
	e.out = append(e.out, Rejection{Code: code, Message: msg, Params: params})
}

func (e *eval) wait(code, msg string, params map[string]any) {
	e.out = append(e.out, Rejection{Code: code, Message: msg, Temporary: true, Params: params})
}

// spec is one check.
type spec func(e *eval)

// specs run in this order on every candidate; all of them run, so a
// rejected candidate lists every reason.
var specs = []spec{specParse, specKind, specIdentity, specYear, specEpisodes, specQuality, specUpgrade, specScore,
	specSize, specLanguage, specReleaseProfiles, specBlocklist, specSeeders, specAge, specContent, specIndexer, specDelay}

// Evaluate decides on every candidate and ranks the accepted ones.
func Evaluate(ctx Context, cands []Candidate) Report {
	if ctx.Now.IsZero() {
		ctx.Now = time.Now()
	}
	if ctx.Mode == "" {
		ctx.Mode = ModeSearch
	}
	rep := Report{Kind: ctx.Wanted.Kind, ProfileID: ctx.Profile.ID, ProfileName: ctx.Profile.Name, Mode: ctx.Mode,
		EvaluatedAt: ctx.Now.UTC(), Decisions: make([]Decision, 0, len(cands))}
	ctx.Profile.Normalize()
	for i := range cands {
		rep.Decisions = append(rep.Decisions, EvaluateOne(&ctx, cands[i]))
	}
	order := map[string]int{StatusAccepted: 0, StatusPending: 1, StatusRejected: 2}
	sort.SliceStable(rep.Decisions, func(i, j int) bool {
		a, b := &rep.Decisions[i], &rep.Decisions[j]
		if order[a.Status] != order[b.Status] {
			return order[a.Status] < order[b.Status]
		}
		if a.Status == StatusRejected {
			return false
		}
		return better(a.RankKey, b.RankKey)
	})
	for i := range rep.Decisions {
		d := &rep.Decisions[i]
		switch d.Status {
		case StatusAccepted:
			rep.Accepted++
			d.Rank = rep.Accepted
		case StatusPending:
			rep.Pending++
		default:
			rep.Rejected++
		}
	}
	if b := rep.Best(); b != nil {
		rep.BestGUID = b.Candidate.GUID
	}
	return rep
}

// EvaluateOne decides on one candidate (no rank).
func EvaluateOne(ctx *Context, c Candidate) Decision {
	if ctx.Now.IsZero() {
		ctx.Now = time.Now()
	}
	e := &eval{ctx: ctx, c: &c, r: parser.Parse(c.Title)}
	e.q = quality.Identify(&e.r, ctx.Wanted.Kind)
	e.pos, e.allowed = ctx.Profile.Position(e.q.ID)
	in := customformats.Input{Title: c.Title, Release: &e.r, Quality: e.q, Size: c.Size, IndexerFlags: c.IndexerFlags,
		OriginalLanguage: ctx.Wanted.OriginalLanguage}
	e.matches, e.score = customformats.Score(ctx.Formats, &ctx.Profile, in)
	for _, s := range specs {
		s(e)
	}
	d := Decision{Candidate: c, Release: e.r, Quality: e.q, CustomFormats: e.matches, Score: e.score,
		Rejections: e.out, RankKey: e.rankKey()}
	if d.Rejections == nil {
		d.Rejections = []Rejection{}
	}
	d.Status = StatusAccepted
	for _, r := range e.out {
		if !r.Temporary {
			d.Status = StatusRejected
			break
		}
		d.Status = StatusPending
	}
	return d
}

// --- specs ---

func specParse(e *eval) {
	switch {
	case e.r.Obfuscated:
		e.reject(CodeParseFailed, "the name is obfuscated and says nothing about the release", nil)
	case strings.TrimSpace(e.c.Title) == "":
		e.reject(CodeParseFailed, "the release has no name", nil)
	case e.r.Title == "" && (e.r.Music == nil || e.r.Music.Artist == "") && e.c.IDs == (IDs{}):
		e.reject(CodeParseFailed, "no title could be read from the name", map[string]any{"confidence": e.r.Confidence})
	}
}

func kindOf(k parser.Kind) quality.Kind {
	switch k {
	case parser.KindMovie:
		return quality.Movies
	case parser.KindSeries:
		return quality.Series
	case parser.KindMusic:
		return quality.Music
	}
	return ""
}

func specKind(e *eval) {
	got := kindOf(e.r.Kind)
	want := e.ctx.Wanted.Kind
	if got == "" || got == want {
		return
	}
	// A film name with a year and no episode is fine for a movie; an episode
	// name is never a film, and video is never music.
	if want == quality.Movies && got == quality.Series && len(e.r.Episodes) == 0 && len(e.r.Seasons) == 0 && e.r.AirDate == "" {
		return
	}
	if want == quality.Series && got == quality.Movies {
		return // reported by the episode check
	}
	e.reject(CodeWrongKind, fmt.Sprintf("this looks like %s, not %s", kindWord(got), kindWord(want)),
		map[string]any{"got": string(got), "want": string(want)})
}

func kindWord(k quality.Kind) string {
	switch k {
	case quality.Movies:
		return "a film"
	case quality.Series:
		return "a series episode"
	}
	return "music"
}

// idsMatch: 1 = an id matches, -1 = an id contradicts, 0 = nothing to compare.
func idsMatch(a, b IDs) int {
	res := 0
	cmp := func(ok bool, same bool) {
		if !ok || res == -1 {
			return
		}
		if same {
			res = 1
		} else {
			res = -1
		}
	}
	cmp(a.TMDb != 0 && b.TMDb != 0, a.TMDb == b.TMDb)
	cmp(a.TVDb != 0 && b.TVDb != 0, a.TVDb == b.TVDb)
	cmp(a.IMDb != "" && b.IMDb != "", strings.EqualFold(a.IMDb, b.IMDb))
	cmp(a.MusicBrainz != "" && b.MusicBrainz != "", strings.EqualFold(a.MusicBrainz, b.MusicBrainz))
	return res
}

func specIdentity(e *eval) {
	switch idsMatch(e.c.IDs, e.ctx.Wanted.IDs) {
	case 1:
		return // the indexer matched it by id; the name does not need to
	case -1:
		e.reject(CodeWrongIDs, "the indexer's ids point to a different title", map[string]any{"got": e.c.IDs, "want": e.ctx.Wanted.IDs})
		return
	}
	w := e.ctx.Wanted
	if w.Kind == quality.Music {
		var artist, album string
		if e.r.Music != nil {
			artist, album = e.r.Music.Artist, e.r.Music.Album
		}
		if album == "" {
			album = e.r.Title
		}
		if w.Album != "" && !TitleMatches(album, []string{w.Album}) {
			e.reject(CodeWrongTitle, fmt.Sprintf("album %q is not %q", album, w.Album), map[string]any{"got": album, "want": w.Album})
		}
		if w.Artist != "" && artist != "" && !TitleMatches(artist, []string{w.Artist}) && !isVarious(artist) {
			e.reject(CodeWrongTitle, fmt.Sprintf("artist %q is not %q", artist, w.Artist), map[string]any{"got": artist, "want": w.Artist})
		}
		return
	}
	if w.Title == "" {
		return
	}
	names := append([]string{w.Title}, w.AlternateTitles...)
	got := append([]string{e.r.Title}, e.r.AlternateTitles...)
	for _, g := range got {
		if TitleMatches(g, names) {
			return
		}
		// "Show 2005" written into the title
		if w.Year > 0 && TitleMatches(g, []string{fmt.Sprintf("%s %d", w.Title, w.Year)}) {
			return
		}
	}
	e.reject(CodeWrongTitle, fmt.Sprintf("title %q does not match %q", e.r.Title, w.Title), map[string]any{"got": e.r.Title, "want": w.Title})
}

func isVarious(a string) bool {
	k := CleanTitle(a)
	return k == "va" || k == "variousartists"
}

func specYear(e *eval) {
	w := e.ctx.Wanted
	if w.Year == 0 || e.r.Year == 0 || idsMatch(e.c.IDs, w.IDs) == 1 {
		return
	}
	diff := e.r.Year - w.Year
	if diff < 0 {
		diff = -diff
	}
	// Release dates differ by a year between countries; allow one.
	if diff > 1 && (w.Kind == quality.Movies || w.Kind == quality.Music) {
		e.reject(CodeWrongYear, fmt.Sprintf("year %d is not %d", e.r.Year, w.Year), map[string]any{"got": e.r.Year, "want": w.Year})
	}
}

func specEpisodes(e *eval) {
	w := e.ctx.Wanted
	if w.Kind != quality.Series || len(w.Episodes) == 0 {
		return
	}
	r := &e.r
	if w.SeriesType == SeriesDaily || (r.AirDate != "" && len(r.Seasons) == 0) {
		if r.AirDate == "" {
			e.reject(CodeWrongEpisode, "the name has no air date for a daily show", nil)
			return
		}
		for _, ep := range w.Episodes {
			if ep.AirDate == r.AirDate {
				return
			}
		}
		e.reject(CodeWrongEpisode, fmt.Sprintf("air date %s is not wanted", r.AirDate), map[string]any{"got": r.AirDate})
		return
	}
	if len(r.Seasons) == 0 && len(r.AbsoluteEpisodes) > 0 {
		hit := 0
		for _, a := range r.AbsoluteEpisodes {
			for _, ep := range w.Episodes {
				if ep.Absolute == a {
					hit++
					break
				}
			}
		}
		if hit == 0 {
			e.reject(CodeWrongEpisode, fmt.Sprintf("absolute episode %s is not wanted", joinInts(r.AbsoluteEpisodes)),
				map[string]any{"got": r.AbsoluteEpisodes})
		}
		return
	}
	if len(r.Seasons) == 0 && !r.CompleteSeries {
		e.reject(CodeWrongEpisode, "the name says no season or episode", nil)
		return
	}
	if r.CompleteSeries && len(r.Seasons) == 0 {
		return
	}
	wantedSeasons := map[int]bool{}
	for _, ep := range w.Episodes {
		wantedSeasons[ep.Season] = true
	}
	inSeason := false
	for _, s := range r.Seasons {
		if wantedSeasons[s] {
			inSeason = true
		}
	}
	if !inSeason {
		e.reject(CodeWrongSeason, fmt.Sprintf("season %s is not wanted", joinInts(r.Seasons)), map[string]any{"got": r.Seasons})
		return
	}
	if len(r.Episodes) == 0 {
		return // a season pack that holds wanted episodes
	}
	for _, ep := range w.Episodes {
		for _, n := range r.Episodes {
			if ep.Episode == n && containsInt(r.Seasons, ep.Season) {
				return
			}
		}
	}
	e.reject(CodeWrongEpisode, fmt.Sprintf("S%02dE%s is not wanted", r.Seasons[0], joinIntsPad(r.Episodes)),
		map[string]any{"season": r.Seasons[0], "episodes": r.Episodes})
}

func specQuality(e *eval) {
	if e.pos < 0 || !e.allowed {
		e.reject(CodeQualityNotAllowed, fmt.Sprintf("%s is not allowed in profile %s", e.q.Name, e.ctx.Profile.Name),
			map[string]any{"quality": e.q.ID, "profile": e.ctx.Profile.Name})
	}
}

func revisionValue(r parser.Revision) int { return r.Version*10 + r.Real }

func specUpgrade(e *eval) {
	cur := e.ctx.Wanted.Current
	if cur == nil {
		return
	}
	p := &e.ctx.Profile
	curQ := quality.ByIDMust(cur.Quality)
	if !p.UpgradesAllowed {
		e.reject(CodeUpgradesDisabled, fmt.Sprintf("%s is already in the library and profile %s does not upgrade", curQ.Name, p.Name),
			map[string]any{"current": cur.Quality})
		return
	}
	curPos, _ := p.Position(cur.Quality)
	cutoff := p.CutoffPosition()
	if curPos >= 0 && curPos <= cutoff && (p.CutoffFormatScore == 0 || cur.Score >= p.CutoffFormatScore) {
		// The cutoff is met; only a proper/repack of the same quality is still welcome.
		if !(e.pos == curPos && revisionValue(e.r.Revision) > revisionValue(cur.Revision)) {
			e.reject(CodeCutoffMet, fmt.Sprintf("%s (score %d) already meets the cutoff %s", curQ.Name, cur.Score, cutoffName(p)),
				map[string]any{"current": cur.Quality, "currentScore": cur.Score})
		}
		return
	}
	if e.pos < 0 {
		return
	}
	switch {
	case curPos >= 0 && e.pos > curPos:
		e.reject(CodeNotAnUpgrade, fmt.Sprintf("%s is worse than %s in the library", e.q.Name, curQ.Name),
			map[string]any{"quality": e.q.ID, "current": cur.Quality})
	case curPos < 0 || e.pos < curPos:
	case revisionValue(e.r.Revision) > revisionValue(cur.Revision):
	case e.score > cur.Score:
	default:
		e.reject(CodeNotAnUpgrade, fmt.Sprintf("same quality (%s) and no better score (%d, library %d)", e.q.Name, e.score, cur.Score),
			map[string]any{"quality": e.q.ID, "score": e.score, "currentScore": cur.Score})
	}
}

func cutoffName(p *quality.Profile) string {
	if i := p.CutoffPosition(); i >= 0 {
		return quality.ItemName(p.Items[i])
	}
	return p.Cutoff
}

func specScore(e *eval) {
	if min := e.ctx.Profile.MinFormatScore; e.score < min {
		e.reject(CodeScoreTooLow, fmt.Sprintf("custom format score %d is below the minimum %d", e.score, min),
			map[string]any{"score": e.score, "minimum": min})
	}
}

// runtime returns the minutes the release should cover.
func (e *eval) runtime() (int, bool) {
	w := e.ctx.Wanted
	rt, assumed := w.RuntimeMinutes, false
	if rt <= 0 {
		rt, assumed = defaultRuntime[w.Kind], true
	}
	if w.Kind != quality.Series {
		return rt, assumed
	}
	n := len(e.r.Episodes)
	if n == 0 {
		n = len(e.r.AbsoluteEpisodes)
	}
	if n == 0 && (e.r.FullSeason || e.r.MultiSeason || e.r.CompleteSeries) {
		for _, ep := range w.Episodes {
			if e.r.CompleteSeries || containsInt(e.r.Seasons, ep.Season) {
				n++
			}
		}
	}
	return rt * max(n, 1), assumed
}

func (e *eval) definition() (quality.Definition, bool) {
	for _, d := range e.ctx.Definitions {
		if d.Kind == e.ctx.Wanted.Kind && d.Quality == e.q.ID {
			return d, true
		}
	}
	return quality.Definition{}, false
}

const mb = 1 << 20

func specSize(e *eval) {
	if e.c.Size <= 0 {
		return
	}
	def, ok := e.definition()
	if !ok {
		return
	}
	rt, assumed := e.runtime()
	note := fmt.Sprintf("%d min", rt)
	if assumed {
		note += ", assumed"
	}
	params := func(limit int64) map[string]any {
		return map[string]any{"size": e.c.Size, "limit": limit, "quality": e.q.Name, "runtime": rt, "assumed": assumed}
	}
	if def.Min > 0 {
		if limit := int64(def.Min * float64(rt) * mb); e.c.Size < limit {
			e.reject(CodeSizeTooSmall, fmt.Sprintf("%s < %s min for %s (%s)", formatSize(e.c.Size), formatSize(limit), e.q.Name, note), params(limit))
		}
	}
	if def.Max > 0 {
		if limit := int64(def.Max * float64(rt) * mb); e.c.Size > limit {
			e.reject(CodeSizeTooLarge, fmt.Sprintf("%s > %s max for %s (%s)", formatSize(e.c.Size), formatSize(limit), e.q.Name, note), params(limit))
		}
	}
}

func specLanguage(e *eval) {
	want := e.ctx.Profile.Languages
	if len(want) == 0 {
		return
	}
	original := e.ctx.Wanted.OriginalLanguage
	accepted := map[string]bool{}
	for _, l := range want {
		switch l {
		case languages.Any:
			return
		case languages.Original:
			if original != "" {
				accepted[original] = true
			}
			if e.r.MultiAudio || e.r.DualAudio {
				return // multi-audio releases carry the original track
			}
		default:
			accepted[l] = true
		}
	}
	got := customformats.ReleaseLanguages(&e.r)
	for _, l := range got {
		if accepted[l] || accepted[strings.SplitN(l, "-", 2)[0]] {
			return
		}
	}
	names := make([]string, 0, len(got))
	for _, l := range got {
		names = append(names, languages.Name(l))
	}
	wantNames := make([]string, 0, len(want))
	for _, l := range want {
		if l == languages.Original && original != "" {
			wantNames = append(wantNames, "original ("+languages.Name(original)+")")
			continue
		}
		wantNames = append(wantNames, languages.Name(l))
	}
	e.reject(CodeLanguage, fmt.Sprintf("%s is not one of %s", strings.Join(names, ", "), strings.Join(wantNames, ", ")),
		map[string]any{"languages": got, "wanted": want})
}

func specReleaseProfiles(e *eval) {
	for _, rp := range e.ctx.ReleaseProfiles {
		if !rp.Enabled || !quality.TagsMatch(rp.Tags, e.ctx.Wanted.Tags) || (rp.IndexerID != 0 && rp.IndexerID != e.c.IndexerID) {
			continue
		}
		if len(rp.Required) > 0 {
			found := false
			for _, t := range rp.Required {
				if term, err := quality.CompileTerm(t); err == nil && term.Match(e.c.Title) {
					found = true
					break
				}
			}
			if !found {
				e.reject(CodeMustContain, fmt.Sprintf("the name contains none of %s (release profile %s)", strings.Join(rp.Required, ", "), rp.Name),
					map[string]any{"terms": rp.Required, "profile": rp.Name})
			}
		}
		for _, t := range rp.Ignored {
			if term, err := quality.CompileTerm(t); err == nil && term.Match(e.c.Title) {
				e.reject(CodeMustNotContain, fmt.Sprintf("the name contains %s (release profile %s)", t, rp.Name),
					map[string]any{"term": t, "profile": rp.Name})
			}
		}
	}
}

func specBlocklist(e *eval) {
	if e.ctx.Blocklist == nil {
		return
	}
	if reason, ok := e.ctx.Blocklist.Blocked(e.c, &e.r); ok {
		e.reject(CodeBlocklisted, "the release is blocklisted: "+reason, map[string]any{"reason": reason})
	}
}

func specSeeders(e *eval) {
	if e.c.Protocol != Torrent || e.c.Seeders == nil {
		return
	}
	min := e.c.MinimumSeeders
	if min == 0 {
		min = e.ctx.MinimumSeeders
	}
	if *e.c.Seeders < min {
		e.reject(CodeSeeders, fmt.Sprintf("%d seeders, at least %d needed", *e.c.Seeders, min), map[string]any{"seeders": *e.c.Seeders, "minimum": min})
	}
}

func (e *eval) ageMinutes() int64 {
	if e.c.PublishDate.IsZero() {
		return -1
	}
	// A publish date in the future (clock skew) counts as "just now".
	return max(int64(e.ctx.Now.Sub(e.c.PublishDate)/time.Minute), 0)
}

func specAge(e *eval) {
	if e.c.Protocol != Usenet {
		return
	}
	age := e.ageMinutes()
	if age < 0 {
		return
	}
	if r := e.ctx.RetentionDays; r > 0 && age > int64(r)*24*60 {
		e.reject(CodeTooOld, fmt.Sprintf("posted %d days ago, retention is %d days", age/(24*60), r), map[string]any{"ageDays": age / (24 * 60), "retentionDays": r})
	}
	if m := e.ctx.MinimumAgeMinutes; m > 0 && age < int64(m) {
		e.wait(CodeTooNew, fmt.Sprintf("posted %s ago, waiting until it is %s old", formatMinutes(age), formatMinutes(int64(m))),
			map[string]any{"ageMinutes": age, "minimumMinutes": m})
	}
}

func specContent(e *eval) {
	if e.r.Sample {
		e.reject(CodeSample, "this is a sample", nil)
	}
	if e.r.HardcodedSubs && !e.ctx.AllowHardcodedSubs {
		e.reject(CodeHardcodedSubs, "the subtitles are burned into the picture", nil)
	}
	if e.r.ThreeD && !e.ctx.Allow3D {
		e.reject(CodeThreeD, "this is a 3D release", nil)
	}
}

func specIndexer(e *eval) {
	c := e.c
	name := c.IndexerName
	if name == "" {
		name = fmt.Sprintf("#%d", c.IndexerID)
	}
	switch {
	case c.IndexerDisabled:
		e.reject(CodeIndexerDisabled, fmt.Sprintf("indexer %s is disabled", name), map[string]any{"indexer": name})
	case e.ctx.Mode == ModeRSS && c.IndexerRSSDisabled:
		e.reject(CodeIndexerDisabled, fmt.Sprintf("indexer %s is not used for RSS", name), map[string]any{"indexer": name, "mode": "rss"})
	case e.ctx.Mode == ModeSearch && c.IndexerAutomaticSearchDisabled:
		e.reject(CodeIndexerDisabled, fmt.Sprintf("indexer %s is not used for automatic search", name), map[string]any{"indexer": name, "mode": "search"})
	case e.ctx.Mode == ModeInteractive && c.IndexerInteractiveSearchDisabled:
		e.reject(CodeIndexerDisabled, fmt.Sprintf("indexer %s is not used for interactive search", name), map[string]any{"indexer": name, "mode": "interactive"})
	}
	if !c.IndexerLimitedUntil.IsZero() && c.IndexerLimitedUntil.After(e.ctx.Now) {
		e.wait(CodeIndexerLimited, fmt.Sprintf("indexer %s has reached its limit until %s", name, c.IndexerLimitedUntil.UTC().Format("2006-01-02 15:04 UTC")),
			map[string]any{"indexer": name, "until": c.IndexerLimitedUntil.UTC()})
	}
}

func (e *eval) delayProfile() *quality.DelayProfile {
	return quality.PickDelayProfile(e.ctx.DelayProfiles, e.ctx.Wanted.Tags)
}

func specDelay(e *eval) {
	d := e.delayProfile()
	if d == nil {
		return
	}
	if !d.ProtocolEnabled(e.c.Protocol) {
		e.reject(CodeProtocolDisabled, fmt.Sprintf("%s is turned off in delay profile %s", e.c.Protocol, d.Name),
			map[string]any{"protocol": e.c.Protocol, "profile": d.Name})
		return
	}
	if e.ctx.Mode == ModeInteractive {
		return
	}
	delay := int64(d.Delay(e.c.Protocol) / time.Minute)
	age := e.ageMinutes()
	if delay <= 0 || age < 0 || age >= delay {
		return
	}
	if d.BypassIfHighestQuality && e.pos >= 0 && e.pos == e.ctx.Profile.BestAllowed() {
		return
	}
	if d.BypassIfAboveScore && e.score >= d.MinimumScore {
		return
	}
	e.wait(CodeDelayed, fmt.Sprintf("waiting %s more (%s delay %s in %s)", formatMinutes(delay-age), e.c.Protocol, formatMinutes(delay), d.Name),
		map[string]any{"remainingMinutes": delay - age, "delayMinutes": delay, "protocol": e.c.Protocol, "profile": d.Name})
}

// --- ranking ---

func (e *eval) rankKey() RankKey {
	k := RankKey{QualityPosition: e.pos, Score: e.score, Revision: revisionValue(e.r.Revision), IndexerPriority: e.c.IndexerPriority,
		SizeDistance: -1, AgeMinutes: e.ageMinutes(), Tiebreak: e.c.Title + "\x00" + e.c.GUID}
	if k.QualityPosition < 0 {
		k.QualityPosition = 1 << 20
	}
	if k.IndexerPriority == 0 {
		k.IndexerPriority = 25
	}
	if d := e.delayProfile(); d != nil {
		k.ProtocolPreferred = d.PreferredProtocol == e.c.Protocol
	}
	if e.c.Protocol == Torrent && e.c.Seeders != nil {
		k.Seeders = *e.c.Seeders
	}
	if def, ok := e.definition(); ok && def.Preferred > 0 && e.c.Size > 0 {
		rt, _ := e.runtime()
		pref := int64(def.Preferred * float64(rt) * mb)
		dist := e.c.Size - pref
		if dist < 0 {
			dist = -dist
		}
		k.SizeDistance = dist
	}
	return k
}

// better reports whether a ranks above b. Order: quality (profile position),
// custom format score, revision, preferred protocol, indexer priority,
// seeders, closeness to the preferred size, newer, then name.
func better(a, b RankKey) bool {
	switch {
	case a.QualityPosition != b.QualityPosition:
		return a.QualityPosition < b.QualityPosition
	case a.Score != b.Score:
		return a.Score > b.Score
	case a.Revision != b.Revision:
		return a.Revision > b.Revision
	case a.ProtocolPreferred != b.ProtocolPreferred:
		return a.ProtocolPreferred
	case a.IndexerPriority != b.IndexerPriority:
		return a.IndexerPriority < b.IndexerPriority
	case a.Seeders != b.Seeders:
		return a.Seeders > b.Seeders
	case a.SizeDistance != b.SizeDistance:
		if a.SizeDistance < 0 || b.SizeDistance < 0 {
			return a.SizeDistance >= 0
		}
		return a.SizeDistance < b.SizeDistance
	case a.AgeMinutes != b.AgeMinutes:
		if a.AgeMinutes < 0 || b.AgeMinutes < 0 {
			return a.AgeMinutes >= 0
		}
		return a.AgeMinutes < b.AgeMinutes
	}
	return a.Tiebreak < b.Tiebreak
}

// --- titles ---

var fold = map[rune]string{'á': "a", 'à': "a", 'â': "a", 'ä': "a", 'ã': "a", 'å': "a", 'æ': "ae", 'ç': "c", 'č': "c", 'ć': "c",
	'đ': "d", 'é': "e", 'è': "e", 'ê': "e", 'ë': "e", 'í': "i", 'ì': "i", 'î': "i", 'ï': "i", 'ñ': "n", 'ó': "o", 'ò': "o",
	'ô': "o", 'ö': "o", 'õ': "o", 'ø': "o", 'œ': "oe", 'š': "s", 'ß': "ss", 'ú': "u", 'ù': "u", 'û': "u", 'ü': "u", 'ý': "y",
	'ÿ': "y", 'ž': "z", 'ł': "l", 'ř': "r", 'ě': "e", 'ů': "u", 'ő': "o", 'ű': "u"}

// CleanTitle reduces a title for comparison: lower case, accents folded,
// "&" as "and", only letters and digits.
func CleanTitle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '&':
			b.WriteString("and")
		case r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
		case fold[r] != "":
			b.WriteString(fold[r])
		case r >= 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func trimArticle(s string) string {
	for _, a := range []string{"the", "a", "an"} {
		if strings.HasPrefix(s, a) && len(s) > len(a)+2 {
			return s[len(a):]
		}
	}
	return s
}

// TitleMatches compares a parsed title with the wanted names.
func TitleMatches(got string, names []string) bool {
	g := CleanTitle(got)
	if g == "" {
		return false
	}
	for _, n := range names {
		w := CleanTitle(n)
		if w == "" {
			continue
		}
		if g == w || trimArticle(g) == trimArticle(w) {
			return true
		}
	}
	return false
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func joinInts(list []int) string {
	s := make([]string, len(list))
	for i, v := range list {
		s[i] = fmt.Sprint(v)
	}
	return strings.Join(s, ", ")
}

func joinIntsPad(list []int) string {
	s := make([]string, len(list))
	for i, v := range list {
		s[i] = fmt.Sprintf("%02d", v)
	}
	return strings.Join(s, "-E")
}
