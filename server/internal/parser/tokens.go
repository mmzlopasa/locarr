package parser

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// token is one word of the normalised name.
type token struct {
	text  string // as written
	key   string // lower case, apostrophes removed
	start int    // byte offsets in the joined lower-case name
	end   int
	// bracket is the id of the bracket group the token is in (0 = none).
	bracket int
	// lastInBracket marks the last token before a closing bracket.
	lastInBracket bool

	f       feat
	val     string
	val2    string
	n       int
	primary bool
	// weak tokens (ambiguous words not in capitals) never end the title.
	weak bool
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isSep(c byte) bool {
	switch c {
	case ' ', '.', '_', ',', '\t', ';', '|', '[', ']', '(', ')', '{', '}':
		return true
	}
	return false
}

// keepDot reports whether the dot at s[i] belongs to a token ("5.1", "H.264",
// "44.1kHz") rather than separating two.
func keepDot(s string, i int) bool {
	if i == 0 || i+1 >= len(s) {
		return false
	}
	l, r := s[i-1], s[i+1]
	if isDigit(l) && isDigit(r) {
		if i+2 < len(s) && (s[i+2] == 'k' || s[i+2] == 'K') && strings.HasPrefix(strings.ToLower(s[i+2:]), "khz") {
			return true
		}
		leftSingle := i < 2 || !isDigit(s[i-2])
		rightSingle := i+2 >= len(s) || !isDigit(s[i+2])
		return leftSingle && rightSingle
	}
	if (l == 'h' || l == 'H' || l == 'x' || l == 'X') && (i < 2 || isSep(s[i-2])) && i+4 <= len(s) {
		n := s[i+1 : i+4]
		if (n == "264" || n == "265") && (i+4 == len(s) || !isDigit(s[i+4])) {
			return true
		}
	}
	return false
}

// tokenize splits s into tokens. Separators are spaces, dots, underscores and
// brackets; a dash surrounded by spaces becomes its own token.
func tokenize(s string) []token {
	toks := make([]token, 0, 24)
	start := -1
	bracket, nextBracket := 0, 0
	add := func(end int) {
		if start < 0 {
			return
		}
		w := s[start:end]
		start = -1
		if w != "-" && w != "~" {
			w = strings.TrimLeft(strings.TrimRight(w, "-~:'\""), "-~:'\"!")
		}
		if w == "" {
			return
		}
		toks = append(toks, token{text: w, bracket: bracket})
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '[', '(', '{':
			add(i)
			nextBracket++
			bracket = nextBracket
		case ']', ')', '}':
			add(i)
			if n := len(toks); n > 0 && toks[n-1].bracket == bracket && bracket != 0 {
				toks[n-1].lastInBracket = true
			}
			bracket = 0
		case ' ', '_', ',', '\t', ';', '|':
			add(i)
		case '.':
			if keepDot(s, i) {
				if start < 0 {
					start = i
				}
				continue
			}
			add(i)
		default:
			if start < 0 {
				start = i
			}
		}
	}
	add(len(s))
	return splitDashed(toks)
}

// splitDashed splits tokens glued with dashes ("Show-S01E01-720p") when a part
// is a known feature and the whole is not.
func splitDashed(toks []token) []token {
	var out []token
	for _, t := range toks {
		if !strings.Contains(t.text, "-") || t.text == "-" || knownWhole(t.text) {
			out = append(out, t)
			continue
		}
		parts := strings.Split(t.text, "-")
		known := false
		for _, p := range parts {
			if p != "" && knownWhole(p) {
				known = true
				break
			}
		}
		if !known {
			out = append(out, t)
			continue
		}
		for j, p := range parts {
			if p == "" {
				continue
			}
			nt := t
			nt.text = p
			nt.lastInBracket = t.lastInBracket && j == len(parts)-1
			out = append(out, nt)
		}
	}
	return out
}

var reEpisodeToken = regexp.MustCompile(`(?i)^(?:s\d{1,4}(?:e\d{1,4})?|\d{1,2}x\d{2,3}|e\d{1,4})(?:[-e]|$)`)

// knownWhole reports whether w on its own is a feature word.
func knownWhole(w string) bool {
	k := normKey(w)
	if _, ok := single[k]; ok {
		return true
	}
	if _, ok := streaming[k]; ok {
		return true
	}
	if isYear(w) || reYearRange.MatchString(k) || resolution(k) > 0 || reEpisodeToken.MatchString(w) {
		return true
	}
	if _, _, ok := audioWithChannels(k); ok {
		return true
	}
	return false
}

func normKey(w string) string {
	k := strings.ToLower(w)
	if strings.ContainsAny(k, "'’") {
		k = strings.NewReplacer("'", "", "’", "").Replace(k)
	}
	return k
}

func isYear(w string) bool {
	if len(w) != 4 || !isDigit(w[0]) || !isDigit(w[1]) || !isDigit(w[2]) || !isDigit(w[3]) {
		return false
	}
	n, _ := strconv.Atoi(w)
	return n >= 1900 && n <= 2099
}

var resolutions = map[int]bool{240: true, 360: true, 480: true, 540: true, 576: true, 720: true, 1080: true, 1440: true, 2160: true, 4320: true}

// resolution parses "1080p", "720i", "4k", "1920x1080" (0 when not one).
func resolution(k string) int {
	switch k {
	case "4k", "uhd", "2160":
		return 2160
	case "8k":
		return 4320
	}
	if n := len(k); n >= 4 && n <= 5 && (k[n-1] == 'p' || k[n-1] == 'i') {
		v, err := strconv.Atoi(k[:n-1])
		if err == nil && resolutions[v] {
			return v
		}
		return 0
	}
	if x := strings.IndexByte(k, 'x'); x >= 3 && x <= 4 && len(k)-x-1 >= 3 && len(k)-x-1 <= 4 {
		w, err1 := strconv.Atoi(k[:x])
		h, err2 := strconv.Atoi(k[x+1:])
		if err1 != nil || err2 != nil || w < 320 || h < 200 {
			return 0
		}
		switch {
		case w >= 3800 || h >= 2000:
			return 2160
		case w >= 1900 || h >= 1000:
			return 1080
		case w >= 1200 || h >= 700:
			return 720
		case h >= 560:
			return 576
		default:
			return 480
		}
	}
	return 0
}

// audioWithChannels splits glued audio tokens such as "ddp5.1", "aac2.0",
// "truehd7.1", "dd+5.1" into codec and channels.
func audioWithChannels(k string) (codec, channels string, ok bool) {
	n := len(k)
	if n < 4 || !isDigit(k[n-1]) || k[n-2] != '.' || !isDigit(k[n-3]) {
		return "", "", false
	}
	ch := k[n-3:]
	if !validChannels(ch) {
		return "", "", false
	}
	prefix := strings.TrimRight(k[:n-3], "-.")
	if prefix == "" {
		return "", ch, true
	}
	if prefix == "ma" {
		return "dts-hd ma", ch, true
	}
	if prefix == "atmos" {
		return "atmos", ch, true
	}
	if d, found := single[prefix]; found && (d.f == fAudio) {
		return d.val, ch, true
	}
	return "", "", false
}

func validChannels(ch string) bool {
	switch ch {
	case "1.0", "2.0", "2.1", "3.0", "4.0", "4.1", "5.0", "5.1", "6.1", "7.1", "9.1":
		return true
	}
	return false
}

// isCapitals reports a word written in capitals ("GERMAN"), or scene-style
// mixed case that starts lower and has capitals inside ("iTALiAN", "MULTi").
func isCapitals(w string) bool {
	hasUpper, hasLetter := false, false
	for _, r := range w {
		if unicode.IsLetter(r) {
			hasLetter = true
			if unicode.IsUpper(r) {
				hasUpper = true
			}
		}
	}
	if !hasLetter {
		return true
	}
	if w == strings.ToUpper(w) {
		return true
	}
	// "MULTi", "iNTERNAL", "GERMAN.DL" style: mostly capitals.
	r, _ := utf8.DecodeRuneInString(w)
	upper := 0
	letters := 0
	for _, c := range w {
		if unicode.IsLetter(c) {
			letters++
			if unicode.IsUpper(c) {
				upper++
			}
		}
	}
	return hasUpper && (unicode.IsLower(r) || upper*2 > letters) && upper >= 2
}
