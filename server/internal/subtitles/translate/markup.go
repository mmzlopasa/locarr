package translate

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Piece is one part of a cue sent for translation: the markup around it
// (styling tags, ASS override blocks, a dialogue dash) stays outside and is
// put back unchanged.
type Piece struct {
	Prefix, Text, Suffix string
	// Keep: nothing to translate (music notes, punctuation, ASS drawings).
	Keep bool
}

var (
	reOpen      = regexp.MustCompile(`^(?:\{[^}]*\}|<(?:i|b|u|font[^>]*)>|\s+)`)
	reClose     = regexp.MustCompile(`(?:\{[^}]*\}|</(?:i|b|u|font)>|\s+)$`)
	reDash      = regexp.MustCompile(`^[-–—]\s*`)
	reReopen    = regexp.MustCompile(`</(i|b|u)>(\s+)<(i|b|u)>`)
	reTag       = regexp.MustCompile(`<[^>]+>|\{[^}]*\}`)
	reDrawing   = regexp.MustCompile(`\\p[1-9]`)
	reLeadTags  = regexp.MustCompile(`^(?:\{[^}]*\}|<[^>]+>|\s)*`)
	reASSInline = regexp.MustCompile(`\{[^}]*\}`)
)

// peel splits s into leading markup (tags, override blocks, one dialogue
// dash), the text and trailing markup. A closing tag only moves to the
// suffix when its opening tag is in the prefix, so the text never carries
// half a pair.
func peel(s string) (prefix, core, suffix string) {
	core = s
	dash := false
	for {
		if m := reOpen.FindString(core); m != "" {
			prefix += m
			core = core[len(m):]
			continue
		}
		if d := reDash.FindString(core); d != "" && !dash {
			prefix += d
			core = core[len(d):]
			dash = true
			continue
		}
		break
	}
	for {
		loc := reClose.FindStringIndex(core)
		if loc == nil || loc[0] == loc[1] {
			break
		}
		m := core[loc[0]:]
		if strings.HasPrefix(m, "</") {
			name := strings.TrimSuffix(strings.TrimPrefix(m, "</"), ">")
			if !strings.Contains(prefix, "<"+name) {
				break
			}
		}
		suffix = m + suffix
		core = core[:loc[0]]
	}
	return prefix, core, suffix
}

func hasLetters(s string) bool {
	s = reTag.ReplaceAllString(s, "")
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// isDialogue: two or more lines that each start with a dash (one speaker per line).
func isDialogue(lines []string) bool {
	if len(lines) < 2 {
		return false
	}
	n := 0
	for _, l := range lines {
		if reDash.MatchString(strings.TrimSpace(reLeadTags.ReplaceAllString(l, ""))) {
			n++
		}
	}
	return n >= 2
}

// SplitCue cuts a cue's text ("\n" between lines) into pieces: one per
// speaker in a dialogue cue, otherwise the whole cue joined into one line
// (so a sentence broken over two lines is translated as one sentence).
func SplitCue(text string) []Piece {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	var groups []string
	if isDialogue(lines) {
		groups = lines
	} else {
		groups = []string{strings.Join(lines, " ")}
	}
	out := make([]Piece, 0, len(groups))
	for _, g := range groups {
		g = reReopen.ReplaceAllStringFunc(g, func(m string) string {
			sm := reReopen.FindStringSubmatch(m)
			if sm[1] == sm[3] {
				return sm[2]
			}
			return m
		})
		p := Piece{}
		p.Prefix, p.Text, p.Suffix = peel(g)
		p.Keep = !hasLetters(p.Text) || reDrawing.MatchString(p.Prefix+p.Text)
		out = append(out, p)
	}
	return out
}

// JoinCue puts translated texts (one per piece, in order; a kept piece's
// entry is ignored) back together, wrapped to at most two lines of max
// characters per piece (a dialogue cue keeps one line per speaker).
func JoinCue(pieces []Piece, texts []string, maxLen int) string {
	var lines []string
	dialogue := len(pieces) > 1
	for i, p := range pieces {
		t := p.Text
		if !p.Keep && i < len(texts) {
			t = strings.TrimSpace(texts[i])
		}
		var ls []string
		if dialogue {
			ls = []string{t}
		} else {
			ls = Rewrap(t, maxLen)
		}
		ls[0] = p.Prefix + ls[0]
		ls[len(ls)-1] += p.Suffix
		lines = append(lines, ls...)
	}
	return strings.Join(lines, "\n")
}

// visibleLen is the length of s without markup.
func visibleLen(s string) int { return utf8.RuneCountInString(reTag.ReplaceAllString(s, "")) }

// MaxLine is the usual subtitle line length for a target language.
func MaxLine(lang string) int {
	switch lang {
	case "ja":
		return 16
	case "zh", "ko":
		return 18
	}
	return 42
}

// Rewrap breaks s into one line, or two balanced lines when it is longer
// than max: at the space nearest the middle, preferring a break after
// punctuation, never inside markup. Text without spaces (Japanese,
// Chinese) breaks between characters.
func Rewrap(s string, width int) []string {
	s = strings.Join(strings.Fields(s), " ")
	total := visibleLen(s)
	if total <= width || total < 2 {
		return []string{s}
	}
	// Candidate break offsets (byte index where line 2 starts) outside markup.
	type cand struct {
		at, left int
		space    bool
		punct    bool
	}
	var cands []cand
	depth := rune(0)
	left := 0
	var prev rune
	for i, r := range s {
		switch {
		case depth != 0:
			if r == depth {
				depth = 0
			}
			continue
		case r == '<':
			depth = '>'
			continue
		case r == '{':
			depth = '}'
			continue
		}
		if r == ' ' {
			cands = append(cands, cand{at: i + 1, left: left, space: true, punct: strings.ContainsRune(",.;:!?…", prev)})
		} else if i > 0 && !unicode.IsSpace(prev) && prev != 0 && (isWide(r) || isWide(prev)) {
			cands = append(cands, cand{at: i, left: left, punct: strings.ContainsRune("、。，！？…", prev)})
		}
		left++
		prev = r
	}
	if len(cands) == 0 {
		return []string{s}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		cost := func(c cand) int {
			l1, l2 := c.left, total-c.left
			if c.space {
				l2--
			}
			d := max(l1, l2) * 10
			if c.punct {
				d -= 40 // a break after a comma or a full stop reads better
			}
			if l2 < l1 {
				d += 5 // bottom-heavy is easier to read
			}
			return d
		}
		return cost(cands[i]) < cost(cands[j])
	})
	c := cands[0]
	return []string{strings.TrimSpace(s[:c.at]), strings.TrimSpace(s[c.at:])}
}

func isWide(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r)
}

// Tags returns the markup inside s, sorted (for "kept every tag" checks).
func Tags(s string) []string {
	t := reTag.FindAllString(s, -1)
	sort.Strings(t)
	return t
}

// SameTags reports whether b has exactly the markup of a.
func SameTags(a, b string) bool {
	x, y := Tags(a), Tags(b)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// StripTags removes all markup.
func StripTags(s string) string { return strings.TrimSpace(reTag.ReplaceAllString(s, "")) }

// StripASS removes ASS override blocks ({\i1}) inside a text, for services
// that only understand HTML-like tags.
func StripASS(s string) string { return reASSInline.ReplaceAllString(s, "") }
