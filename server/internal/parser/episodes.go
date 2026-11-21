package parser

import (
	"regexp"
	"strconv"
	"strings"
)

// epMatch is one season/episode/date match over a span of the joined name.
type epMatch struct {
	start, end int // byte span in the joined lower-case name
	seasons    []int
	episodes   []int
	absolute   []int
	airDate    string
	fullSeason bool
	version    int
}

var (
	reSxxEyy   = regexp.MustCompile(`\bs(\d{1,4}) ?(?:e|ep|episode) ?(\d{1,4})`)
	reNxNN     = regexp.MustCompile(`\b(\d{1,2})x(\d{2,3})(?:$|[^a-z0-9]|x)`)
	reLongSE   = regexp.MustCompile(`\b(?:season|saison|staffel|temporada|sezona|stagione) ?(\d{1,2}) ?(?:episode|episodio|epizoda|folge|ep|e) ?(\d{1,4})\b`)
	reSeasonR  = regexp.MustCompile(`\bs(\d{1,2})(?:-s?| ?(?:to|&) ?s?| - s)(\d{1,2})\b`)
	reSeason   = regexp.MustCompile(`\bs(\d{1,2})\b`)
	reSeasonW  = regexp.MustCompile(`\b(?:the )?(?:complete )?(?:seasons?|saison|saisons|staffel|staffeln|temporada|temporadas|sezona|sezone|stagione|seizoen|sezon) ?(\d{1,2})(?: ?(?:-|to|&|and) ?(\d{1,2}))?\b`)
	reSeasonO  = regexp.MustCompile(`\b(\d{1,2})(?:st|nd|rd|th) season\b`)
	reSeasonN  = regexp.MustCompile(`\bseason (one|two|three|four|five|six|seven|eight|nine|ten)\b`)
	reDaily    = regexp.MustCompile(`\b((?:19|20)\d{2})[ -](0[1-9]|1[0-2])[ -](0[1-9]|[12]\d|3[01])\b`)
	reDailyDMY = regexp.MustCompile(`\b(0[1-9]|[12]\d|3[01])[ -](0[1-9]|1[0-2])[ -]((?:19|20)\d{2})\b`)
	reDashAbs  = regexp.MustCompile(`(?:^| )[-~] (\d{1,4})(?:v(\d))?(?: ?[-~] ?(\d{1,4})(?:v\d)?)?(?: |$)`)
	reEpAlone  = regexp.MustCompile(`\b(?:e|(?:ep|episode|episodio|epizoda|folge) ?)(\d{1,4})(?:v(\d))?(?: ?- ?(?:e|ep)?(\d{1,4}))?\b`)
	reLongTail = regexp.MustCompile(`^ ?- ?(?:e|ep|episode)? ?(\d{1,3})\b`)
)

var wordNumbers = map[string]int{"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func rangeInts(a, b int) []int {
	if b < a || b-a > 200 {
		return []int{a, b}
	}
	out := make([]int, 0, b-a+1)
	for i := a; i <= b; i++ {
		out = append(out, i)
	}
	return out
}

// scanTail reads what follows a first episode: "e02", "-e03", "-03", "e02e03",
// "-s01e04", "x02" (for 1x01 style) and "v2". It returns the episodes, the new
// end offset and a version.
func scanTail(s string, p int, nx bool) (eps []int, end, version int) {
	end = p
	last := -1
	for {
		q := end
		dash := false
		if q < len(s) && (s[q] == '-' || s[q] == ' ' || s[q] == '&') {
			dash = s[q] == '-'
			q++
			if q < len(s) && s[q] == ' ' && dash {
				q++
			}
		}
		// optional repeated season: -s01e03 / -1x03
		if !nx && q < len(s) && s[q] == 's' {
			r := q + 1
			for r < len(s) && isDigit(s[r]) {
				r++
			}
			if r > q+1 && r < len(s) && s[r] == 'e' {
				q = r
			}
		} else if nx {
			r := q
			for r < len(s) && isDigit(s[r]) {
				r++
			}
			if r > q && r < len(s) && s[r] == 'x' {
				q = r
			}
		}
		marker := false
		if q < len(s) && ((!nx && s[q] == 'e') || (nx && s[q] == 'x')) {
			marker = true
			q++
			if q < len(s) && s[q] == 'p' && !nx {
				q++
			}
		}
		if !marker && !dash {
			break
		}
		r := q
		for r < len(s) && isDigit(s[r]) {
			r++
		}
		if r == q || r-q > 4 || (r < len(s) && s[r] != ' ' && s[r] != '-' && s[r] != 'e' && s[r] != 'v' && s[r] != 'x') {
			break
		}
		n := atoi(s[q:r])
		if dash && !marker && last >= 0 && n > last {
			eps = append(eps, rangeInts(last+1, n)...)
		} else if dash && marker && last >= 0 && n > last+1 {
			eps = append(eps, rangeInts(last+1, n)...)
		} else if dash && !marker && last < 0 {
			eps = append(eps, n)
		} else {
			eps = append(eps, n)
		}
		last = n
		end = r
	}
	if end+1 < len(s) && s[end] == 'v' && isDigit(s[end+1]) {
		version = int(s[end+1] - '0')
		end += 2
	}
	return eps, end, version
}

// findEpisodes runs the season/episode/date patterns over the lower-case
// joined name. used reports byte ranges already taken.
func findEpisodes(ln string, anime bool) []epMatch {
	var out []epMatch
	hasDigit := strings.IndexAny(ln, "0123456789") >= 0
	seasonWord := strings.Contains(ln, "season") || strings.Contains(ln, "saison") || strings.Contains(ln, "staffel") ||
		strings.Contains(ln, "temporad") || strings.Contains(ln, "sezon") || strings.Contains(ln, "stagione") || strings.Contains(ln, "seizoen")
	if !hasDigit && !seasonWord {
		return nil
	}
	sDigit, eDigit, xDigit, dashDigit, date := scanHints(ln)
	if sDigit {
		out = sxxEyy(ln, out)
	}
	if xDigit {
		out = nxNN(ln, out)
	}
	if seasonWord {
		out = longSE(ln, out)
	}
	if date {
		out = dates(ln, out)
	}
	out = seasons(ln, out, sDigit, seasonWord)
	if dashDigit {
		out = dashAbs(ln, out, anime)
	}
	if eDigit {
		out = epAlone(ln, out)
	}
	return out
}

// scanHints looks for the characters each pattern needs, so most patterns
// can be skipped without running a regular expression.
func scanHints(ln string) (sDigit, eDigit, xDigit, dashDigit, date bool) {
	run := 0
	for i := 0; i < len(ln); i++ {
		c := ln[i]
		if isDigit(c) {
			run++
			if run == 4 && i+3 < len(ln) && (ln[i+1] == ' ' || ln[i+1] == '-') && isDigit(ln[i+2]) && isDigit(ln[i+3]) {
				date = true
			}
			if run == 2 && i+6 < len(ln) && (ln[i+1] == ' ' || ln[i+1] == '-') && isDigit(ln[i+2]) && isDigit(ln[i+3]) {
				date = true
			}
			continue
		}
		run = 0
		if i+1 >= len(ln) {
			break
		}
		n := ln[i+1]
		switch c {
		case 's':
			if isDigit(n) {
				sDigit = true
			}
		case 'e':
			if isDigit(n) || n == 'p' || n == ' ' {
				eDigit = true
			}
		case 'x':
			if isDigit(n) && i > 0 && isDigit(ln[i-1]) {
				xDigit = true
			}
		case '-', '~':
			if n == ' ' && i+2 < len(ln) && isDigit(ln[i+2]) {
				dashDigit = true
			}
		}
	}
	if strings.Contains(ln, "episod") || strings.Contains(ln, "epizod") || strings.Contains(ln, "folge") {
		eDigit = true
	}
	return
}

func sxxEyy(ln string, out []epMatch) []epMatch {
	for _, m := range reSxxEyy.FindAllStringSubmatchIndex(ln, -1) {
		first := atoi(ln[m[4]:m[5]])
		tail, end, v := scanTail(ln, m[1], false)
		// "S01E01-E03": scanTail returns 2, 3 for a range from the first.
		eps := []int{first}
		if len(tail) > 0 && ln[m[1]] == '-' && tail[0] > first+1 && !strings.HasPrefix(ln[m[1]:], "-s") {
			eps = append(eps, rangeInts(first+1, tail[len(tail)-1])...)
		} else {
			for _, e := range tail {
				eps = addInt(eps, e)
			}
		}
		out = append(out, epMatch{start: m[0], end: end, seasons: []int{atoi(ln[m[2]:m[3]])}, episodes: eps, version: v})
	}
	return out
}

func nxNN(ln string, out []epMatch) []epMatch {
	for _, m := range reNxNN.FindAllStringSubmatchIndex(ln, -1) {
		if overlaps(out, m[0], m[1]) {
			continue
		}
		first := atoi(ln[m[4]:m[5]])
		tail, end, v := scanTail(ln, m[5], true)
		eps := []int{first}
		if len(tail) > 0 && ln[m[1]] == '-' && tail[0] > first+1 && !strings.Contains(ln[m[1]:end], "x") {
			eps = append(eps, rangeInts(first+1, tail[len(tail)-1])...)
		} else {
			for _, e := range tail {
				eps = addInt(eps, e)
			}
		}
		out = append(out, epMatch{start: m[0], end: end, seasons: []int{atoi(ln[m[2]:m[3]])}, episodes: eps, version: v})
	}
	return out
}

func longSE(ln string, out []epMatch) []epMatch {
	for _, m := range reLongSE.FindAllStringSubmatchIndex(ln, -1) {
		if overlaps(out, m[0], m[1]) {
			continue
		}
		first := atoi(ln[m[4]:m[5]])
		eps := []int{first}
		end := m[1]
		if rest := ln[m[1]:]; strings.HasPrefix(rest, "-") || strings.HasPrefix(rest, " - ") {
			if mm := reLongTail.FindStringSubmatchIndex(rest); mm != nil {
				eps = append(eps, rangeInts(first+1, atoi(rest[mm[2]:mm[3]]))...)
				end += mm[1]
			}
		}
		out = append(out, epMatch{start: m[0], end: end, seasons: []int{atoi(ln[m[2]:m[3]])}, episodes: eps})
	}
	return out
}

func dates(ln string, out []epMatch) []epMatch {
	for _, m := range reDaily.FindAllStringSubmatchIndex(ln, -1) {
		if overlaps(out, m[0], m[1]) {
			continue
		}
		out = append(out, epMatch{start: m[0], end: m[1], airDate: ln[m[2]:m[3]] + "-" + ln[m[4]:m[5]] + "-" + ln[m[6]:m[7]]})
	}
	for _, m := range reDailyDMY.FindAllStringSubmatchIndex(ln, -1) {
		if overlaps(out, m[0], m[1]) {
			continue
		}
		out = append(out, epMatch{start: m[0], end: m[1], airDate: ln[m[6]:m[7]] + "-" + ln[m[4]:m[5]] + "-" + ln[m[2]:m[3]]})
	}
	return out
}

func seasons(ln string, out []epMatch, sDigit, seasonWord bool) []epMatch {
	if sDigit {
		out = seasonRanges(ln, out)
	}
	if seasonWord {
		out = seasonWords(ln, out)
	}
	if sDigit {
		for _, m := range reSeason.FindAllStringSubmatchIndex(ln, -1) {
			if overlaps(out, m[0], m[1]) {
				continue
			}
			out = append(out, epMatch{start: m[0], end: m[1], seasons: []int{atoi(ln[m[2]:m[3]])}, fullSeason: true})
		}
	}
	return out
}

func seasonRanges(ln string, out []epMatch) []epMatch {
	for _, m := range reSeasonR.FindAllStringSubmatchIndex(ln, -1) {
		if overlaps(out, m[0], m[1]) {
			continue
		}
		a, b := atoi(ln[m[2]:m[3]]), atoi(ln[m[4]:m[5]])
		if b <= a {
			continue
		}
		out = append(out, epMatch{start: m[0], end: m[1], seasons: rangeInts(a, b), fullSeason: true})
	}
	return out
}

func seasonWords(ln string, out []epMatch) []epMatch {
	for _, re := range []*regexp.Regexp{reSeasonW, reSeasonO} {
		for _, m := range re.FindAllStringSubmatchIndex(ln, -1) {
			if overlaps(out, m[0], m[1]) {
				continue
			}
			a := atoi(ln[m[2]:m[3]])
			ss := []int{a}
			if len(m) > 4 && m[4] >= 0 {
				if b := atoi(ln[m[4]:m[5]]); b > a {
					ss = rangeInts(a, b)
				}
			}
			out = append(out, epMatch{start: m[0], end: m[1], seasons: ss, fullSeason: true})
		}
	}
	for _, m := range reSeasonN.FindAllStringSubmatchIndex(ln, -1) {
		if overlaps(out, m[0], m[1]) {
			continue
		}
		out = append(out, epMatch{start: m[0], end: m[1], seasons: []int{wordNumbers[ln[m[2]:m[3]]]}, fullSeason: true})
	}
	return out
}

func dashAbs(ln string, out []epMatch, anime bool) []epMatch {
	// Anime style "Title - 012", "Title - 01-12", and lone "E1012" / "Episode 5".
	for _, m := range reDashAbs.FindAllStringSubmatchIndex(ln, -1) {
		if overlaps(out, m[0], m[1]) {
			continue
		}
		a := atoi(ln[m[2]:m[3]])
		numStr := ln[m[2]:m[3]]
		if !anime && len(numStr) == 4 && a >= 1900 && a <= 2099 {
			continue
		}
		if len(numStr) == 1 && !anime {
			continue
		}
		em := epMatch{start: m[0], end: m[1], absolute: []int{a}}
		if m[4] >= 0 {
			em.version = atoi(ln[m[4]:m[5]])
		}
		if m[6] >= 0 {
			if b := atoi(ln[m[6]:m[7]]); b > a {
				em.absolute = rangeInts(a, b)
			}
		}
		// keep the trailing space for the next token
		if strings.HasSuffix(ln[m[0]:m[1]], " ") {
			em.end--
		}
		if ln[em.start] == ' ' {
			em.start++
		}
		out = append(out, em)
	}
	return out
}

func epAlone(ln string, out []epMatch) []epMatch {
	for _, m := range reEpAlone.FindAllStringSubmatchIndex(ln, -1) {
		if overlaps(out, m[0], m[1]) {
			continue
		}
		a := atoi(ln[m[2]:m[3]])
		em := epMatch{start: m[0], end: m[1], absolute: []int{a}}
		if m[4] >= 0 {
			em.version = atoi(ln[m[4]:m[5]])
		}
		if m[6] >= 0 {
			if b := atoi(ln[m[6]:m[7]]); b > a {
				em.absolute = rangeInts(a, b)
			}
		}
		out = append(out, em)
	}
	return out
}

func overlaps(out []epMatch, a, b int) bool {
	for _, m := range out {
		if a < m.end && b > m.start {
			return true
		}
	}
	return false
}
