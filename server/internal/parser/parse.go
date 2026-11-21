package parser

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mmzlopasa/locarr/server/internal/languages"
)

// maxInput is the longest name the parser reads.
const maxInput = 512

const tlds = `com|org|net|to|tv|me|cc|ws|xyz|nl|info|in|io|club|lol|re|mx|se|li|ch|cz|ms|pw|vip|uno|bz|ag|sx|ru|eu|co|biz`

var (
	reHash          = regexp.MustCompile(`[\[(]([0-9A-Fa-f]{8})[\])]`)
	reSiteBracket   = regexp.MustCompile(`(?i)^\s*[\[(]\s*(?:www\.)?[a-z0-9][a-z0-9\-]*\.(?:` + tlds + `)\s*[\])]\s*(?:-\s*)?`)
	reSiteWWW       = regexp.MustCompile(`(?i)^\s*www\.[a-z0-9][a-z0-9\-]*\.(?:` + tlds + `)\s*(?:-\s*)?`)
	reSiteDash      = regexp.MustCompile(`(?i)^\s*[a-z0-9][a-z0-9\-]*\.(?:` + tlds + `)\s+-\s+`)
	reSiteSuffix    = regexp.MustCompile(`(?i)\s*[\[(]\s*(?:rarbg|eztv(?:\.re|x\.to|\.io)?|ettv|tgx|rartv|ethd|publichd|vtv|1337x|torrentgalaxy|ion10|prime|eztvx\.to|rartv)\s*[\])]\s*$`)
	reRepost        = regexp.MustCompile(`(?i)[-.](?:obfuscated|postbot|xpost|scrambled|asrequested|buymore|chamele0n|whiterev|rakuv\d*|alternativetorequested|gerov|z0ids3n|4planet|4p)$`)
	reSampleSuffix  = regexp.MustCompile(`(?i)(?:^|[ ._\-])sample$`)
	reLeadGroup     = regexp.MustCompile(`^\s*[\[【]([^\]】]{1,40})[\]】]\s*`)
	reBracketAbs    = regexp.MustCompile(`[\[(](\d{2,4})(?:v(\d))?[\])]`)
	reBracketRange  = regexp.MustCompile(`[\[(](\d{1,4}) ?[-~] ?(\d{1,4})[\])]`)
	reGroupName     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_&@$!+]{0,30}$`)
	reYTS           = regexp.MustCompile(`(?i)^(?:yts|yify)(?:\.[a-z]{2,3})?$`)
	reFeatureHint   = regexp.MustCompile(`(?i)(?:(?:19|20)\d{2}|\d{3,4}[pi]\b|[xh]\.?26[45]|hevc|xvid|web|blu-?ray|bdrip|brrip|hdtv|dvd|s\d{1,2}e\d|\d{1,2}x\d{2}|flac|mp3|remux|aac|ac3|dts|ddp|season|complete|4k|hdrip|cam|\bts\b)`)
	reUUID          = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	reHex           = regexp.MustCompile(`^(?:[0-9a-f]{20,}|[0-9A-F]{20,})$`)
	reBitDepth      = regexp.MustCompile(`^(8|10|12|16|24|32)-?bits?$`)
	reDepthRate     = regexp.MustCompile(`^(16|24|32)(?:bits?|b)?[-/](44|48|88|96|176|192)(?:\.\d)?(?:khz)?$`)
	reYearRange     = regexp.MustCompile(`^(?:19|20)\d{2}-(?:19|20)\d{2}$`)
	reYTSBracket    = regexp.MustCompile(`(?i)\s*[\[(]((?:yts|yify)(?:\.[a-z]{2,3})?)[\])]\s*$`)
	reKHz           = regexp.MustCompile(`^(\d{2,3}(?:\.\d)?)khz$`)
	reKbps          = regexp.MustCompile(`^(\d{2,3})kbps$`)
	reRevN          = regexp.MustCompile(`^(proper|repack|rerip)(\d)$`)
	reVersion       = regexp.MustCompile(`^v(\d)$`)
	reDisc          = regexp.MustCompile(`^(?:(\d{1,2})(?:x)?(cd|lp|dvd)s?|(?:cd|disc|disk|dvd)(\d{1,2}))$`)
	reFPS           = regexp.MustCompile(`^(\d{2,3})fps$`)
	reSubsWord      = regexp.MustCompile(`^([a-z]{2,10})-?(?:subs?|subtitles?|titlovi)$`)
	reChannelsCh    = regexp.MustCompile(`^(\d)ch$`)
	reDVProfile     = regexp.MustCompile(`^(?:dv)?p([5789])$`)
	reMusicScene    = regexp.MustCompile(`(?i)^(?:web|cd|\d{1,2}cd|cds|cdm|cdr|cdep|vinyl|\d?lp|ep|flac|mp3|sat|dvb|cable|fm|bootleg|promo|ost|sacd|dab|bonus|remastered|deluxe|ltd|retail|reissue|\(.*\))$`)
	reMultiDashGrps = regexp.MustCompile(`(?i)-(D-Z0N3|E-Sub|HD-Spain|Ma-Ra)$`)
)

// Parse parses a release name (a torrent or NZB title, or a folder name).
func Parse(name string) Release {
	return parse(name, false)
}

// ParsePath parses a file path. The file name is used first; when it is
// obfuscated or says little, the folder name fills in what is missing.
func ParsePath(p string) Release {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimRight(p, "/")
	base := path.Base(p)
	dir := path.Dir(p)
	parent := ""
	if dir != "." && dir != "/" {
		parent = path.Base(dir)
		// files inside "Sample", "Subs" or "CD1" folders take the folder above
		switch strings.ToLower(parent) {
		case "sample", "samples", "subs", "subtitles", "extras", "featurettes":
			if d := path.Dir(dir); d != "." && d != "/" {
				parent = path.Base(d)
			}
		}
	}
	r := parse(base, true)
	r.Input = p
	if strings.Contains(strings.ToLower("/"+p+"/"), "/sample/") || strings.Contains(strings.ToLower("/"+p+"/"), "/samples/") {
		r.Sample = true
	}
	if parent == "" {
		return r
	}
	pr := parse(parent, false)
	if r.Obfuscated && !pr.Obfuscated {
		ext, sample := r.Extension, r.Sample
		pr.Input = p
		pr.Extension = ext
		pr.Sample = pr.Sample || sample
		return pr
	}
	mergeMissing(&r, &pr)
	return r
}

// mergeMissing fills what the file name did not say from the folder name.
func mergeMissing(r, pr *Release) {
	if r.Title == "" {
		r.Title, r.AlternateTitles = pr.Title, pr.AlternateTitles
	}
	if r.Year == 0 {
		r.Year = pr.Year
	}
	if len(r.Seasons) == 0 && len(pr.Seasons) > 0 && len(r.Episodes) > 0 {
		r.Seasons = pr.Seasons[:1]
	}
	if r.Video.Source == "" {
		r.Video.Source = pr.Video.Source
	}
	if r.Video.Resolution == 0 {
		r.Video.Resolution = pr.Video.Resolution
	}
	if r.Video.Codec == "" {
		r.Video.Codec, r.Video.Encoder = pr.Video.Codec, pr.Video.Encoder
	}
	if len(r.Video.HDR) == 0 {
		r.Video.HDR = pr.Video.HDR
	}
	if len(r.Audio.Codecs) == 0 {
		r.Audio = pr.Audio
	}
	if r.Group == "" {
		r.Group = pr.Group
	}
	if len(r.Languages) == 0 {
		r.Languages = pr.Languages
	}
	if r.StreamingService == "" {
		r.StreamingService = pr.StreamingService
	}
	if r.Kind == KindUnknown {
		r.Kind = pr.Kind
	}
	if r.Music == nil && pr.Music != nil {
		r.Music = pr.Music
	}
	r.Confidence = maxf(r.Confidence, pr.Confidence*0.9)
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

var inputReplacer = strings.NewReplacer("–", " - ", "—", " - ", "【", "[", "】", "]", "「", "[", "」", "]",
	"（", "(", "）", ")", "\u200b", "", " ", " ", "：", ":", "＋", "+")

func stripExt(s string, isPath bool) (string, string) {
	i := strings.LastIndexByte(s, '.')
	if i <= 0 || i < len(s)-8 {
		return s, ""
	}
	ext := strings.ToLower(s[i+1:])
	if _, ok := extensions[ext]; ok {
		// "mp3", "flac" and "aac" are also tags; only an extension when the
		// name has no spaces before it (a file name) or it is a path.
		if !isPath && (ext == "flac" || ext == "mp3" || ext == "aac" || ext == "iso" || ext == "ogg" || ext == "opus" || ext == "wav" || ext == "ape" || ext == "alac" || ext == "divx" || ext == "sub" || ext == "aiff") {
			return s, ""
		}
		return s[:i], ext
	}
	if isPath && (ext == "ts") {
		return s[:i], ext
	}
	return s, ""
}

// isObfuscated reports names that are random strings or hashes.
func isObfuscated(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if reUUID.MatchString(s) || reHex.MatchString(s) {
		return true
	}
	if strings.ContainsAny(s, " ._[]()") || len(s) < 16 {
		return false
	}
	digits, upper, lower := 0, 0, 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r >= 'A' && r <= 'Z':
			upper++
		case r >= 'a' && r <= 'z':
			lower++
		case r == '-':
		default:
			return false
		}
	}
	if reEpisodeToken.MatchString(s) || reFeatureHint.MatchString(s) && strings.Contains(s, "-") {
		return false
	}
	return digits >= 3 && upper+lower >= 3 && (upper >= 2 && lower >= 2 || digits*4 >= len(s))
}

// splitGroup removes a trailing "-GROUP" and returns it.
func splitGroup(s string) (string, string) {
	if m := reMultiDashGrps.FindStringSubmatchIndex(s); m != nil {
		return s[:m[0]], s[m[2]:m[3]]
	}
	i := strings.LastIndexByte(s, '-')
	if i <= 0 || i == len(s)-1 {
		return s, ""
	}
	cand := strings.TrimSpace(s[i+1:])
	before := s[:i]
	bracketed := false
	if strings.HasPrefix(cand, "[") && strings.HasSuffix(cand, "]") || strings.HasPrefix(cand, "(") && strings.HasSuffix(cand, ")") {
		cand = cand[1 : len(cand)-1]
		bracketed = true
	}
	if cand == "" {
		return s, ""
	}
	if strings.HasSuffix(before, " ") && !bracketed {
		// "Artist - Album": only a group when the part before clearly is a release.
		return s, ""
	}
	if !reGroupName.MatchString(cand) && !reYTS.MatchString(cand) {
		return s, ""
	}
	if isAllDigits(cand) && len(cand) <= 4 {
		return s, ""
	}
	if knownWhole(cand) || reEpisodeToken.MatchString(cand) || isLanguageWord(cand) {
		return s, ""
	}
	switch strings.ToLower(cand) {
	case "dl", "hd", "rip", "ray", "es", "x", "ma", "hra", "sub", "subs", "man", "ac", "3", "ii", "iii", "jr", "in", "the", "up", "on", "off":
		return s, ""
	}
	if !hasFeatureToken(before) {
		return s, ""
	}
	return strings.TrimRight(before, " ._"), cand
}

// hasFeatureToken reports whether s contains a word that marks a release
// (year, resolution, source, codec, episode…).
func hasFeatureToken(s string) bool {
	found := false
	start := -1
	check := func(w string) {
		if found || w == "" {
			return
		}
		k := strings.ToLower(w)
		if isYear(w) || resolution(k) > 0 {
			found = true
			return
		}
		if d, ok := single[k]; ok && (d.primary || d.f == fComplete) {
			found = true
			return
		}
		if (k[0] == 's' || k[0] == 'e' || isDigit(k[0])) && reEpisodeToken.MatchString(k) {
			found = true
		}
	}
	for i := 0; i <= len(s); i++ {
		if i == len(s) || isSep(s[i]) || s[i] == '-' {
			if start >= 0 {
				check(s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	return found
}

func endsBracket(s string) bool {
	s = strings.TrimRight(s, " ")
	return s != "" && (s[len(s)-1] == ']' || s[len(s)-1] == ')')
}

func bracketMatches(has bool, s string) [][]int {
	if !has {
		return nil
	}
	return reBracketAbs.FindAllStringSubmatchIndex(s, -1)
}

func isLanguageWord(w string) bool {
	_, ok := languages.FromWord(w, isCapitals(w))
	return ok
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return true
}

func parse(name string, isPath bool) Release {
	r := Release{Input: name, Kind: KindUnknown, Revision: Revision{Version: 1}}
	if len(name) > maxInput {
		// Release names are short; longer input is cut so the work stays bounded.
		cut := maxInput
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = name[:cut]
	}
	s := strings.TrimSpace(inputReplacer.Replace(name))
	s, r.Extension = stripExt(s, isPath)
	if len(s) >= 6 && strings.EqualFold(s[len(s)-6:], "sample") && reSampleSuffix.MatchString(s) {
		r.Sample = true
		s = strings.TrimRight(reSampleSuffix.ReplaceAllString(s, ""), " ._-")
	}
	if isObfuscated(s) {
		r.Obfuscated = true
		r.Unparsed = []string{s}
		return r
	}
	if s != "" {
		var re *regexp.Regexp
		switch {
		case s[0] == '[' || s[0] == '(':
			re = reSiteBracket
		case len(s) > 4 && strings.EqualFold(s[:4], "www."):
			re = reSiteWWW
		case strings.Contains(s, " - "):
			re = reSiteDash
		}
		if re != nil {
			if loc := re.FindStringIndex(s); loc != nil && loc[1] < len(s) {
				s = s[loc[1]:]
			}
		}
	}
	for changed := true; changed; {
		changed = false
		if endsBracket(s) {
			if loc := reSiteSuffix.FindStringIndex(s); loc != nil && loc[0] > 0 {
				s, changed = strings.TrimRight(s[:loc[0]], " ._"), true
			}
		}
		if i := strings.LastIndexAny(s, "-."); i > 0 && len(s)-i < 26 {
			if loc := reRepost.FindStringIndex(s[i:]); loc != nil {
				s, changed = s[:i], true
			}
		}
	}
	ytsGroup := ""
	if m := ytsMatch(s); m != nil && m[0] > 0 {
		ytsGroup = s[m[2]:m[3]]
		s = strings.TrimRight(s[:m[0]], " ._-")
	}
	hasBracket := strings.ContainsAny(s, "[(")
	if m := reHash.FindAllStringSubmatchIndex(s, -1); hasBracket && len(m) > 0 {
		for j := len(m) - 1; j >= 0; j-- {
			h := s[m[j][2]:m[j][3]]
			if isAllDigits(h) {
				continue
			}
			r.Hash = strings.ToUpper(h)
			s = s[:m[j][0]] + " " + s[m[j][1]:]
			break
		}
	}
	anime := false
	if m := reLeadGroup.FindStringSubmatchIndex(s); m != nil {
		g := strings.TrimSpace(s[m[2]:m[3]])
		if !knownWhole(g) && !isAllDigits(g) && !strings.ContainsAny(g, " ") || (strings.Contains(g, " ") && !reFeatureHint.MatchString(g)) {
			r.Group = g
			anime = true
			s = s[m[1]:]
		}
	}
	var bracketAbs []int
	if anime {
		if m := reBracketRange.FindStringSubmatchIndex(s); m != nil {
			a, b := atoi(s[m[2]:m[3]]), atoi(s[m[4]:m[5]])
			if b > a && b < 1900 {
				bracketAbs = rangeInts(a, b)
				s = s[:m[0]] + " " + s[m[1]:]
			}
		}
	}
	for _, m := range bracketMatches(hasBracket, s) {
		n := atoi(s[m[2]:m[3]])
		if n >= 1900 && n <= 2099 {
			continue
		}
		if !anime && m[3]-m[2] < 2 {
			continue
		}
		bracketAbs = append(bracketAbs, n)
		if m[4] >= 0 {
			r.Revision.Version = atoi(s[m[4]:m[5]])
		}
		s = s[:m[0]] + strings.Repeat(" ", m[1]-m[0]) + s[m[1]:]
		break
	}
	if parseSceneMusic(s, &r) {
		return r
	}
	if r.Group == "" || anime {
		if rest, g := splitGroup(strings.TrimRight(s, " ._")); g != "" {
			if r.Group == "" {
				r.Group = g
			}
			s = rest
		}
	}
	if r.Group == "" && ytsGroup != "" {
		r.Group = ytsGroup
	}
	parseTokens(s, &r, anime, bracketAbs)
	return r
}

// parseSceneMusic handles scene music names: Artist-Album-(CAT)-WEB-2020-GROUP
// (underscores for spaces, dashes between fields).
func parseSceneMusic(s string, r *Release) bool {
	if strings.ContainsAny(s, " .") || strings.Count(s, "-") < 3 {
		return false
	}
	segs := strings.Split(s, "-")
	year, src, signal := 0, "", false
	for _, sg := range segs[2:] {
		if isYear(sg) {
			year = atoi(sg)
		} else if reMusicScene.MatchString(sg) {
			signal = true
			if src == "" {
				switch k := strings.ToLower(sg); {
				case k == "web":
					src = "web"
				case strings.HasSuffix(k, "cd") || k == "cds" || k == "cdm" || k == "cdr" || k == "cdep":
					src = "cd"
				case k == "vinyl" || strings.HasSuffix(k, "lp"):
					src = "vinyl"
				case k == "sacd":
					src = "sacd"
				}
			}
		} else if resolution(strings.ToLower(sg)) > 0 || reEpisodeToken.MatchString(sg) {
			return false
		}
	}
	if year == 0 || !signal {
		return false
	}
	r.Kind = KindMusic
	r.Year = year
	r.Music = &Music{Artist: strings.ReplaceAll(segs[0], "_", " "), Album: strings.ReplaceAll(segs[1], "_", " "), Source: src}
	r.Title = r.Music.Album
	last := segs[len(segs)-1]
	if !isYear(last) && !reMusicScene.MatchString(last) {
		r.Group = last
	}
	for _, sg := range segs[2 : len(segs)-1] {
		k := strings.ToLower(sg)
		switch {
		case k == "flac":
			r.Audio.Codecs = addUnique(r.Audio.Codecs, "flac")
		case k == "mp3":
			r.Audio.Codecs = addUnique(r.Audio.Codecs, "mp3")
		case reDisc.MatchString(k):
			if m := reDisc.FindStringSubmatch(k); m[1] != "" {
				r.Music.Discs = atoi(m[1])
			}
		case k == "bootleg" || k == "promo" || k == "retail" || k == "reissue" || k == "ost":
			r.addFlag(k)
		case k == "remastered":
			r.Editions = addUnique(r.Editions, "Remastered")
		case k == "deluxe":
			r.Editions = addUnique(r.Editions, "Deluxe Edition")
		}
	}
	if len(r.Audio.Codecs) == 0 {
		// Scene music without a format tag is MP3 by convention.
		r.Audio.Codecs = []string{"mp3"}
		r.addFlag("assumed-format")
	}
	r.Confidence = 0.85
	return true
}

// parseTokens is the general path: tokenise, classify, find the title, apply.
func parseTokens(s string, r *Release, anime bool, bracketAbs []int) {
	toks := tokenize(s)
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := range toks {
		if i > 0 {
			b.WriteByte(' ')
		}
		toks[i].key = normKey(toks[i].text)
		toks[i].start = b.Len()
		b.WriteString(toks[i].key)
		toks[i].end = b.Len()
	}
	ln := b.String()
	eps := findEpisodes(ln, anime)
	for ei, m := range eps {
		head := -1
		for i := range toks {
			if toks[i].end <= m.start || toks[i].start >= m.end || toks[i].f != fNone {
				continue
			}
			if head < 0 {
				head = i
				toks[i].f, toks[i].n, toks[i].primary = fEpisode, ei, true
			} else {
				toks[i].f = fCont
			}
		}
	}
	classify(toks)
	// "Artist - 25 (2015) [FLAC]": a dash and a number is an album, not an episode.
	if !anime && musicSignals(withoutAbsolute(toks, eps), r.Extension) {
		for i := range toks {
			if toks[i].f == fEpisode && len(eps[toks[i].n].seasons) == 0 && eps[toks[i].n].airDate == "" {
				toks[i].f, toks[i].primary = fNone, false
				if toks[i].text == "-" {
					toks[i].f = fSep
				}
				for j := i + 1; j < len(toks) && toks[j].f == fCont; j++ {
					toks[j].f = fNone
				}
			}
		}
	}

	// The title ends at the first strong feature (or the year before it).
	p := len(toks)
	for i := range toks {
		t := &toks[i]
		if t.f == fEpisode || (t.primary && !t.weak && t.f != fYear) {
			p = i
			break
		}
	}
	yearIdx := -1
	for i := 1; i < p; i++ {
		if toks[i].f == fYear {
			yearIdx = i
		}
	}
	if yearIdx < 0 {
		for i := p; i < len(toks); i++ {
			if toks[i].f == fYear && i > 0 {
				yearIdx = i
				break
			}
		}
	}
	titleEnd := p
	if yearIdx >= 0 && yearIdx < p {
		titleEnd = yearIdx
	}
	hasEpisode := false
	for i := range toks {
		if toks[i].f == fEpisode {
			hasEpisode = true
		}
	}
	// "[Group] Title 05 [1080p]": a bare number right before the features.
	if anime && !hasEpisode && len(bracketAbs) == 0 && titleEnd > 1 {
		t := &toks[titleEnd-1]
		if isAllDigits(t.text) && len(t.text) >= 2 && len(t.text) <= 4 && !isYear(t.text) {
			eps = append(eps, epMatch{absolute: []int{atoi(t.text)}})
			t.f, t.n, t.primary = fEpisode, len(eps)-1, true
			titleEnd--
			hasEpisode = true
		}
	}
	// Trailing tags that are not part of the title ("Movie GERMAN DL", "Show REPACK").
	allowLang := false
	for titleEnd > 1 {
		h := titleEnd - 1
		for h > 0 && toks[h].f == fCont {
			h--
		}
		t := &toks[h]
		if !trimmable(t, allowLang) {
			break
		}
		if t.f == fDual || t.f == fMulti || t.f == fSubs {
			allowLang = true
		}
		if h == 0 {
			break
		}
		titleEnd = h
	}
	for titleEnd > 1 && (toks[titleEnd-1].f == fSep) {
		titleEnd--
	}
	// The title.
	title := toks[:titleEnd]
	for i := range title {
		if title[i].f != fSep {
			title[i].f = fNone
		}
	}
	isMusic := musicSignals(toks[titleEnd:], r.Extension)
	assembleTitle(r, title, isMusic)
	for _, t := range title {
		if t.key == "discography" {
			r.addFlag("discography")
		}
	}

	// Everything after the title.
	for i := titleEnd; i < len(toks); i++ {
		t := &toks[i]
		if t.f == fYear {
			if i == yearIdx {
				r.Year = t.n
			} else if r.Year == 0 {
				r.Year = t.n
			} else {
				t.f = fNone
			}
		}
	}
	for i := titleEnd; i < len(toks); i++ {
		apply(r, toks, i, eps, isMusic)
	}
	if len(bracketAbs) > 0 {
		for _, a := range bracketAbs {
			r.AbsoluteEpisodes = addInt(r.AbsoluteEpisodes, a)
		}
		hasEpisode = true
	}
	// Words right after the episode (and before the next tag) are its title.
	for i := titleEnd; i < len(toks); i++ {
		if toks[i].f != fEpisode {
			continue
		}
		j := i + 1
		for j < len(toks) && toks[j].f == fCont {
			j++
		}
		if j < len(toks) && toks[j].f == fSep {
			j++
		}
		k := j
		for k < len(toks) && (toks[k].f == fNone || toks[k].f == fSep || toks[k].f == fYear && k > j) && toks[k].bracket == 0 {
			k++
		}
		for k > j && toks[k-1].f == fSep {
			k--
		}
		if k > j && (k < len(toks) || r.Group != "") {
			r.EpisodeTitle = joinTitle(toks[j:k])
			for x := j; x < k; x++ {
				toks[x].f = fCont
			}
		}
		break
	}
	// Unparsed words and a group in trailing brackets ("[QxR]", "(… Tigole)").
	var unparsed []int
	for i := titleEnd; i < len(toks); i++ {
		if toks[i].f == fNone {
			unparsed = append(unparsed, i)
		}
	}
	if r.Group == "" && len(unparsed) > 0 {
		last := unparsed[len(unparsed)-1]
		t := toks[last]
		if last == len(toks)-1 && t.bracket != 0 && reGroupName.MatchString(t.text) && !isAllDigits(t.text) {
			r.Group = t.text
			unparsed = unparsed[:len(unparsed)-1]
		}
	}
	for _, i := range unparsed {
		r.Unparsed = append(r.Unparsed, toks[i].text)
	}
	finish(r, anime, hasEpisode, isMusic)
}

func trimmable(t *token, allowLang bool) bool {
	switch t.f {
	case fSep:
		return true
	case fEdition, fComplete:
		return true
	case fFlag:
		return t.val != "discography" && isCapitals(t.text)
	case fLanguage:
		return isCapitals(t.text) || allowLang
	case fRevision, fStreaming, fMulti, fDual, fSubs, fHardsub:
		return isCapitals(t.text) || len(t.text) > 6 && t.f == fSubs
	}
	return false
}

// classify runs the dictionary over tokens not taken by episode patterns.
func classify(toks []token) {
	for i := 0; i < len(toks); i++ {
		t := &toks[i]
		if t.f != fNone {
			continue
		}
		switch t.text {
		case "-", "~", "/", "&", "+", "|", ":":
			t.f = fSep
			continue
		}
		if list, ok := phrases[t.key]; ok {
			matched := false
			for _, p := range list {
				if i+len(p.words) > len(toks) {
					continue
				}
				ok := true
				for j, w := range p.words {
					if toks[i+j].key != w || (j > 0 && toks[i+j].f != fNone) {
						ok = false
						break
					}
				}
				if !ok {
					continue
				}
				setDef(t, p.d)
				for j := 1; j < len(p.words); j++ {
					toks[i+j].f = fCont
				}
				i += len(p.words) - 1
				matched = true
				break
			}
			if matched {
				continue
			}
		}
		classifySingle(toks, i)
	}
}

func setDef(t *token, d def) {
	t.f, t.val, t.n, t.primary = d.f, d.val, d.n, d.primary
	if d.ambiguous && !isCapitals(t.text) {
		t.weak = true
	}
}

func classifySingle(toks []token, i int) {
	t := &toks[i]
	k := t.key
	if isYear(t.text) {
		t.f, t.n = fYear, atoi(t.text)
		return
	}
	if reYearRange.MatchString(k) {
		t.f, t.val, t.primary = fFlag, "year-range", true
		return
	}
	if res := resolution(k); res > 0 {
		t.f, t.n, t.primary = fResolution, res, true
		if strings.HasSuffix(k, "i") {
			t.val = "i"
		}
		if k == "uhd" && !isCapitals(t.text) {
			t.weak = true
		}
		return
	}
	if d, ok := single[k]; ok && (!d.upper || isCapitals(t.text)) {
		setDef(t, d)
		return
	}
	if name, ok := streamingUpper[t.text]; ok {
		t.f, t.val = fStreaming, name
		return
	}
	if name, ok := streaming[k]; ok && len(k) >= 3 {
		t.f, t.val = fStreaming, name
		return
	}
	if c, ch, ok := audioWithChannels(k); ok {
		t.primary = true
		switch c {
		case "":
			t.f, t.val = fChannels, ch
		case "atmos":
			t.f, t.val2 = fAtmos, ch
		default:
			t.f, t.val, t.val2 = fAudio, c, ch
		}
		return
	}
	if validChannels(k) {
		t.f, t.val = fChannels, k
		return
	}
	if m := reChannelsCh.FindStringSubmatch(k); m != nil {
		switch m[1] {
		case "2":
			t.f, t.val = fChannels, "2.0"
		case "6":
			t.f, t.val = fChannels, "5.1"
		case "8":
			t.f, t.val = fChannels, "7.1"
		}
		return
	}
	if m := reBitDepth.FindStringSubmatch(k); m != nil {
		t.f, t.n, t.primary = fBitDepth, atoi(m[1]), true
		return
	}
	if m := reDepthRate.FindStringSubmatch(k); m != nil {
		t.f, t.n, t.val = fBitDepth, atoi(m[1]), m[2]
		return
	}
	if m := reKHz.FindStringSubmatch(k); m != nil {
		t.f, t.val = fSampleRate, m[1]
		return
	}
	if m := reKbps.FindStringSubmatch(k); m != nil {
		t.f, t.val = fMusicBitrate, m[1]
		return
	}
	switch k {
	case "320", "256", "192", "160", "128", "96":
		t.f, t.val, t.weak = fMusicBitrate, k, true
		return
	}
	if m := reRevN.FindStringSubmatch(k); m != nil {
		t.f, t.val, t.n = fRevision, m[1], atoi(m[2])
		if t.val == "rerip" {
			t.val = "repack"
		}
		return
	}
	if m := reVersion.FindStringSubmatch(k); m != nil {
		t.f, t.val, t.n = fRevision, "version", atoi(m[1])
		return
	}
	if m := reDisc.FindStringSubmatch(k); m != nil {
		t.f = fDisc
		if m[1] != "" {
			t.n, t.val = atoi(m[1]), "discs"
		} else {
			t.n, t.val = atoi(m[3]), "disc"
		}
		return
	}
	if reFPS.MatchString(k) {
		t.f, t.val = fFlag, k
		return
	}
	if m := reDVProfile.FindStringSubmatch(k); m != nil && i > 0 && toks[i-1].f == fHDR {
		t.f, t.val = fHDR, "P"+m[1]
		return
	}
	if code, ok := languages.FromWord(t.text, isCapitals(t.text)); ok {
		t.f, t.val = fLanguage, code
		return
	}
	if m := reSubsWord.FindStringSubmatch(k); m != nil {
		if code, ok := languages.FromWord(m[1], true); ok {
			t.f, t.val = fSubs, code
			return
		}
		if m[1] == "multi" {
			t.f, t.val = fSubs, "multi"
			return
		}
		if m[1] == "hc" || m[1] == "hard" {
			t.f = fHardsub
			return
		}
	}
	if strings.HasPrefix(k, "dts-hd") || strings.HasPrefix(k, "truehd") || strings.HasPrefix(k, "ddp") {
		// "TrueHD.Atmos" style leftovers glued with unknown suffixes
		if strings.HasSuffix(k, "atmos") {
			t.f, t.primary = fAtmos, true
			switch {
			case strings.HasPrefix(k, "truehd"):
				t.val = "truehd"
			case strings.HasPrefix(k, "ddp"):
				t.val = "eac3"
			}
		}
	}
}

func withoutAbsolute(toks []token, eps []epMatch) []token {
	out := make([]token, 0, len(toks))
	for _, t := range toks {
		if t.f == fEpisode && len(eps[t.n].seasons) == 0 && eps[t.n].airDate == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

// musicSignals reports a music release: audio-only formats and no video facts.
func musicSignals(toks []token, ext string) bool {
	music, video := extensions[ext] == "audio", false
	for i := range toks {
		t := &toks[i]
		switch t.f {
		case fEpisode, fResolution, fCodec, fHDR, f3D:
			video = true
		case fSource:
			if t.val != string(SourceWEB) {
				video = true
			}
		case fAudio:
			switch t.val {
			case "flac", "mp3", "alac", "ape", "wav", "dsd", "mp2", "opus", "vorbis":
				if t.val2 == "" {
					music = true
				}
			}
		case fMusicBitrate:
			if !t.weak || t.val == "V0" || t.val == "V2" {
				music = true
			}
		case fMusicSource, fSampleRate:
			music = true
		case fBitDepth:
			if t.n >= 16 {
				music = true
			}
		case fFlag:
			if t.val == "discography" || t.val == "lossless" {
				music = true
			}
		}
	}
	if extensions[ext] == "video" {
		video = true
	}
	return music && !video
}

// assembleTitle builds the title (and alternates, or artist and album).
func assembleTitle(r *Release, title []token, isMusic bool) {
	// split on "aka", "a.k.a" and "/"; a bracketed part is an alternate title
	var parts [][]token
	cur := []token{}
	for i := 0; i < len(title); i++ {
		t := title[i]
		if t.key == "aka" || t.text == "/" {
			parts = append(parts, cur)
			cur = []token{}
			continue
		}
		if i+2 < len(title) && strings.EqualFold(t.text, "a") && strings.EqualFold(title[i+1].text, "k") && strings.EqualFold(title[i+2].text, "a") {
			parts = append(parts, cur)
			cur = []token{}
			i += 2
			continue
		}
		cur = append(cur, t)
	}
	parts = append(parts, cur)
	main := parts[0]
	var alts []string
	// "Title (Alternate Title)" inside the title part.
	if len(main) > 1 {
		lastB := main[len(main)-1].bracket
		if lastB != 0 && main[0].bracket != lastB {
			j := len(main) - 1
			for j > 0 && main[j-1].bracket == lastB {
				j--
			}
			if alt := joinTitle(main[j:]); alt != "" && !isAllDigits(strings.ReplaceAll(alt, " ", "")) {
				alts = append(alts, alt)
				main = main[:j]
			}
		}
	}
	for _, p := range parts[1:] {
		if a := joinTitle(p); a != "" {
			alts = append(alts, a)
		}
	}
	if isMusic {
		for i, t := range main {
			if t.text == "-" {
				artist := joinTitle(main[:i])
				album := joinTitle(main[i+1:])
				r.Music = &Music{Artist: artist, Album: album}
				r.Title = album
				r.AlternateTitles = alts
				return
			}
		}
		r.Music = &Music{Album: joinTitle(main)}
	}
	r.Title = joinTitle(main)
	r.AlternateTitles = alts
}

// joinTitle joins title words; single letters in a row become an acronym ("S.W.A.T.").
func joinTitle(ts []token) string {
	var b strings.Builder
	for i := 0; i < len(ts); i++ {
		t := ts[i]
		if t.text == "-" && (i == 0 || i == len(ts)-1) {
			continue
		}
		if isLetter1(t.text) && i+1 < len(ts) && isLetter1(ts[i+1].text) {
			j := i
			var ac strings.Builder
			for j < len(ts) && isLetter1(ts[j].text) {
				ac.WriteString(ts[j].text)
				ac.WriteByte('.')
				j++
			}
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(ac.String())
			i = j - 1
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(t.text)
	}
	return strings.TrimSpace(strings.Trim(b.String(), "-:,"))
}

func isLetter1(s string) bool {
	return len(s) == 1 && unicode.IsLetter(rune(s[0]))
}

// apply records the meaning of token i on the release.
func apply(r *Release, toks []token, i int, eps []epMatch, isMusic bool) {
	t := &toks[i]
	switch t.f {
	case fEpisode:
		m := eps[t.n]
		for _, s := range m.seasons {
			r.Seasons = addInt(r.Seasons, s)
		}
		for _, e := range m.episodes {
			r.Episodes = addInt(r.Episodes, e)
		}
		for _, a := range m.absolute {
			r.AbsoluteEpisodes = addInt(r.AbsoluteEpisodes, a)
		}
		if m.airDate != "" && r.AirDate == "" {
			r.AirDate = m.airDate
		}
		if m.fullSeason {
			r.FullSeason = true
		}
		if m.version > 1 {
			r.Revision.Version = maxInt(r.Revision.Version, m.version)
		}
	case fResolution:
		if r.Video.Resolution == 0 || t.n > r.Video.Resolution && t.key != "uhd" {
			r.Video.Resolution = t.n
		}
		if t.val == "i" {
			r.Video.Interlaced = true
		}
		if t.key == "uhd" {
			r.Video.UHD = true
		}
	case fSource:
		applySource(r, Source(t.val))
	case fCodec:
		switch t.val {
		case "x264":
			r.Video.Codec, r.Video.Encoder = "h264", "x264"
		case "x265":
			r.Video.Codec, r.Video.Encoder = "h265", "x265"
		case "hi10p":
			r.Video.Codec, r.Video.BitDepth = "h264", 10
		default:
			if r.Video.Codec == "" || r.Video.Encoder == "" {
				r.Video.Codec = t.val
			}
		}
	case fBitDepth:
		if t.n >= 16 {
			r.Audio.BitDepth = t.n
			if t.val != "" {
				r.Audio.SampleRateKHz = sampleRate(t.val)
			}
		} else {
			r.Video.BitDepth = t.n
		}
	case fHDR:
		if strings.HasPrefix(t.val, "P") {
			r.Video.DolbyVisionProfile = t.val[1:]
			return
		}
		r.Video.HDR = addUnique(r.Video.HDR, t.val)
	case fAudio:
		v := t.val
		if v == "eac3+atmos" {
			v = "eac3"
			r.Audio.Atmos = true
		}
		if v == "dts-hd" {
			// "DTS-HD" without MA/HRA: treat as master audio when lossless context is unknown.
			v = "dts-hd ma"
			if i+1 < len(toks) && (toks[i+1].key == "hra" || toks[i+1].key == "hr") {
				v = "dts-hd hra"
				toks[i+1].f = fCont
			} else if i+1 < len(toks) && toks[i+1].key == "ma" {
				toks[i+1].f = fCont
			}
		}
		r.Audio.Codecs = addUnique(r.Audio.Codecs, v)
		if t.val2 != "" {
			r.Audio.Channels = t.val2
		}
	case fChannels:
		if r.Audio.Channels == "" || channelRank(t.val) > channelRank(r.Audio.Channels) {
			r.Audio.Channels = t.val
		}
	case fAtmos:
		r.Audio.Atmos = true
		if t.val != "" {
			r.Audio.Codecs = addUnique(r.Audio.Codecs, t.val)
		}
		if t.val2 != "" {
			r.Audio.Channels = t.val2
		}
	case fStreaming:
		if r.StreamingService == "" {
			r.StreamingService = t.val
		}
	case fLanguage:
		r.Languages = addUnique(r.Languages, t.val)
	case fMulti:
		r.MultiAudio = true
	case fDual:
		r.DualAudio = true
	case fSubs:
		switch t.val {
		case "":
			r.addFlag("subbed")
		case "multi":
			r.MultiSubs = true
		default:
			r.SubtitleLanguages = addUnique(r.SubtitleLanguages, t.val)
		}
	case fHardsub:
		r.HardcodedSubs = true
		if t.val != "" {
			r.SubtitleLanguages = addUnique(r.SubtitleLanguages, t.val)
		}
	case fEdition:
		r.Editions = addUnique(r.Editions, t.val)
	case f3D:
		r.ThreeD = true
	case fRevision:
		switch t.val {
		case "proper":
			r.Revision.Proper = true
			r.Revision.Version = maxInt(r.Revision.Version+1, t.n+1)
		case "repack":
			r.Revision.Repack = true
			r.Revision.Version = maxInt(r.Revision.Version+1, t.n+1)
		case "real":
			r.Revision.Real++
		case "version":
			r.Revision.Version = maxInt(r.Revision.Version, t.n)
		}
	case fFlag:
		r.addFlag(t.val)
	case fComplete:
		switch t.val {
		case "series":
			r.CompleteSeries = true
		case "batch":
			r.addFlag("batch")
			r.FullSeason = true
		default:
			r.addFlag("complete")
		}
	case fMusicBitrate:
		if !isMusic && (t.val == "V2" || t.val == "V0") {
			if t.val == "V2" {
				r.Revision.Version = maxInt(r.Revision.Version, 2)
			}
			return
		}
		if isMusic || !t.weak {
			r.Audio.Bitrate = t.val
		} else {
			t.f = fNone
		}
	case fMusicSource:
		if r.Music == nil {
			r.Music = &Music{}
		}
		r.Music.Source = t.val
	case fSampleRate:
		r.Audio.SampleRateKHz = sampleRate(t.val)
	case fDisc:
		if r.Music == nil && !isMusic {
			r.addFlag("disc")
			if t.val == "disc" {
				r.addFlag("disc-" + strconv.Itoa(t.n))
			}
			return
		}
		if r.Music == nil {
			r.Music = &Music{}
		}
		if t.val == "discs" {
			r.Music.Discs = t.n
		} else {
			r.Music.Disc = t.n
		}
	}
}

func applySource(r *Release, s Source) {
	switch s {
	case "uhd-bluray":
		r.Video.UHD = true
		s = SourceBluRay
	case "uhd-remux":
		r.Video.UHD = true
		s = SourceRemux
	}
	cur := r.Video.Source
	// More specific sources win: Remux over BluRay, WEB-DL over WEB, BR-DISK over BluRay.
	if cur == "" || sourceRank(s) > sourceRank(cur) {
		r.Video.Source = s
	}
}

func sourceRank(s Source) int {
	switch s {
	case SourceRemux, SourceBRDisk:
		return 5
	case SourceBDRip, SourceBRRip, SourceWEBDL, SourceWEBRip:
		return 4
	case SourceBluRay:
		return 3
	case SourceWEB, SourceHDTV:
		return 2
	case "":
		return 0
	}
	return 1
}

func channelRank(c string) int {
	f, _ := strconv.ParseFloat(c, 64)
	return int(f * 10)
}

func sampleRate(v string) float64 {
	f, _ := strconv.ParseFloat(v, 64)
	switch f {
	case 44:
		return 44.1
	case 88:
		return 88.2
	case 176:
		return 176.4
	}
	return f
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// finish settles the kind, derived facts and the confidence.
func finish(r *Release, anime, hasEpisode, isMusic bool) {
	if len(r.AbsoluteEpisodes) > 0 && len(r.Episodes) == 0 && len(r.Seasons) == 1 && r.FullSeason {
		// "Title S2 - 05": the number counts within the season.
		r.Episodes, r.AbsoluteEpisodes = r.AbsoluteEpisodes, nil
	}
	if len(r.Episodes) > 0 || r.AirDate != "" || len(r.AbsoluteEpisodes) > 0 {
		r.FullSeason = false
	}
	if len(r.Seasons) > 1 {
		r.MultiSeason = true
	}
	if r.CompleteSeries && len(r.Seasons) == 0 {
		r.FullSeason = false
	}
	if r.HasFlag("complete") && len(r.Seasons) > 0 && len(r.Episodes) == 0 {
		r.FullSeason = true
	}
	for _, s := range r.Seasons {
		if s == 0 {
			r.Special = true
		}
	}
	for _, e := range r.Episodes {
		if e == 0 {
			r.Special = true
		}
	}
	r.Anime = anime || (len(r.AbsoluteEpisodes) > 0 && len(r.Episodes) == 0)
	// BR-DISK: an untouched Blu-ray (codec named, no encoder, not a remux).
	if r.Video.Source == SourceBluRay && r.Video.Encoder == "" && (r.Video.Codec == "h264" || r.Video.Codec == "h265" || r.Video.Codec == "vc1" || r.Video.Codec == "mpeg2") &&
		r.Extension == "" && firstCodecWord(r.Input) != "" {
		r.Video.Source = SourceBRDisk
	}
	if r.Video.Codec == "h265" && r.Video.BitDepth == 0 && len(r.Video.HDR) > 0 {
		r.Video.BitDepth = 10
	}
	if hasHDR(r, "DV") && r.Video.DolbyVisionProfile == "" {
		switch {
		case r.HasFlag("dv-fel") || r.HasFlag("dv-mel"):
			r.Video.DolbyVisionProfile = "7"
		case r.Video.Source == SourceWEBDL || r.Video.Source == SourceWEB || r.Video.Source == SourceWEBRip:
			if hasHDR(r, "HDR10") || hasHDR(r, "HDR10+") {
				r.Video.DolbyVisionProfile = "8"
			} else {
				r.Video.DolbyVisionProfile = "5"
			}
		case r.Video.Source == SourceRemux || r.Video.Source == SourceBRDisk:
			r.Video.DolbyVisionProfile = "7"
		case hasHDR(r, "HDR10") || hasHDR(r, "HDR10+"):
			r.Video.DolbyVisionProfile = "8"
		}
	}
	switch {
	case isMusic:
		r.Kind = KindMusic
		if r.Music == nil {
			r.Music = &Music{Album: r.Title}
		}
	case hasEpisode || len(r.Seasons) > 0 || r.CompleteSeries:
		r.Kind = KindSeries
	case r.Year > 0 || len(r.Editions) > 0 || r.ThreeD:
		r.Kind = KindMovie
	case r.Video.Resolution > 0 || r.Video.Source != "":
		r.Kind = KindMovie
	}
	if r.Kind != KindMusic && r.Music != nil && r.Music.Artist == "" && r.Music.Album == "" && r.Music.Source == "" {
		r.Music = nil
	}
	if r.Kind != KindMusic && r.Music != nil {
		r.Music = nil
	}
	r.Confidence = confidence(r)
}

func firstCodecWord(input string) string {
	l := strings.ToLower(input)
	for _, w := range []string{"avc", "hevc", "vc-1", "mpeg-2"} {
		if strings.Contains(l, "."+w+".") || strings.Contains(l, " "+w+" ") || strings.Contains(l, "."+w+"-") || strings.Contains(l, " "+w+"-") {
			return w
		}
	}
	return ""
}

func hasHDR(r *Release, v string) bool {
	for _, h := range r.Video.HDR {
		if h == v {
			return true
		}
	}
	return false
}

func confidence(r *Release) float64 {
	if r.Obfuscated {
		return 0
	}
	c := 1.0
	if r.Title == "" && (r.Music == nil || r.Music.Artist == "") {
		c -= 0.5
	}
	switch r.Kind {
	case KindUnknown:
		c -= 0.3
	case KindMovie:
		if r.Year == 0 {
			c -= 0.15
		}
	}
	if r.Kind != KindMusic {
		if r.Video.Source == "" {
			c -= 0.1
		}
		if r.Video.Resolution == 0 {
			c -= 0.05
		}
	} else if len(r.Audio.Codecs) == 0 {
		c -= 0.15
	}
	if r.Group == "" {
		c -= 0.05
	}
	c -= 0.05 * float64(len(r.Unparsed))
	if c < 0 {
		c = 0
	}
	return float64(int(c*100+0.5)) / 100
}

func ytsMatch(s string) []int {
	if !endsBracket(s) {
		return nil
	}
	return reYTSBracket.FindStringSubmatchIndex(s)
}
