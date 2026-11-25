package decision

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/mmzlopasa/locarr/server/internal/parser"
)

// NormWords lower-cases s and keeps letters and digits, one space between
// words, so terms and names compare the same however they are separated.
func NormWords(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// BlockedTerm returns the first term of the blocklist that text contains as
// a whole word or phrase: case and separators (".", "_", "-", …) do not
// matter, and a phrase also matches written together ("Some.Term" and
// "SomeTerm" for "some term"), but never inside a longer word.
func BlockedTerm(text string, terms []string) (string, bool) {
	return BlockedTermExcept(text, terms, nil)
}

// BlockedTermExcept is BlockedTerm that ignores the words of the given names
// (the performers the metadata source lists for the scene), so a stage name
// that happens to contain a term does not block it (D-109).
func BlockedTermExcept(text string, terms, names []string) (string, bool) {
	if len(terms) == 0 {
		return "", false
	}
	t := " " + NormWords(text) + " "
	for _, n := range names {
		if w := NormWords(n); w != "" {
			for strings.Contains(t, " "+w+" ") {
				t = strings.Replace(t, " "+w+" ", " | ", 1)
			}
		}
	}
	words := strings.Fields(t)
	for _, term := range terms {
		n := NormWords(term)
		if n == "" {
			continue
		}
		if strings.Contains(t, " "+n+" ") {
			return term, true
		}
		if joined := strings.ReplaceAll(n, " ", ""); joinedMatch(words, joined) {
			return term, true
		}
	}
	return "", false
}

// joinedMatch reports whether consecutive words written together are
// exactly target ("under", "age" or "underage" for "underage").
func joinedMatch(words []string, target string) bool {
	for i := range words {
		acc := ""
		for j := i; j < len(words) && len(acc) < len(target); j++ {
			if words[j] == "|" {
				break
			}
			acc += words[j]
			if acc == target {
				return true
			}
		}
	}
	return false
}

// ProtectedAdultTerms are always blocked for adult content and cannot be
// removed (D-109): unambiguous markers of content involving minors.
// Everything else is the owner's own list, which starts empty.
var ProtectedAdultTerms = []string{
	"underage", "under age", "child", "children", "preteen", "pre teen", "jailbait", "lolita", "loli", "shota", "pthc",
}

// specBlockedTag refuses adult releases that name a protected term or one of
// the owner's blocked terms; the rejection says which term and which list.
// A protected term inside one of the scene's performer names does not count.
func specBlockedTag(e *eval) {
	if a := e.ctx.Wanted.Adult; a != nil {
		if term, ok := BlockedTermExcept(e.c.Title, ProtectedAdultTerms, a.Performers); ok {
			e.reject(CodeBlockedTag, fmt.Sprintf("the name contains %q, which is always blocked", term),
				map[string]any{"term": term, "protected": true})
			return
		}
	}
	if term, ok := BlockedTerm(e.c.Title, e.ctx.BlockedTerms); ok {
		e.reject(CodeBlockedTag, fmt.Sprintf("the name contains %q from the blocklist", term), map[string]any{"term": term, "protected": false})
	}
}

// compact compares names without spaces ("Studio A" = "StudioA").
func compact(s string) string { return strings.ReplaceAll(NormWords(s), " ", "") }

func specAdult(e *eval) {
	a := e.ctx.Wanted.Adult
	if a == nil {
		return
	}
	sc, dated := parser.ParseScene(e.c.Title)
	if a.Scene {
		if !dated {
			undatedScene(e, a)
			return
		}
		studioOK := false
		for _, s := range a.Studios {
			if compact(s) != "" && compact(s) == compact(sc.Studio) {
				studioOK = true
			}
		}
		if !studioOK {
			e.reject(CodeWrongScene, fmt.Sprintf("the release is from %q, not %s", sc.Studio, strings.Join(a.Studios, " / ")),
				map[string]any{"got": sc.Studio})
			return
		}
		if a.Date != "" && sc.Date != a.Date {
			e.reject(CodeWrongScene, fmt.Sprintf("the release is dated %s, not %s", sc.Date, a.Date), map[string]any{"got": sc.Date, "want": a.Date})
			return
		}
		// A studio may publish several scenes on one day: names that say who
		// and what must name a performer or most of the title (D-108).
		if !sceneRestMatches(sc.Rest, a) {
			e.reject(CodeWrongScene, fmt.Sprintf("the release names another scene of the day (%q)", sc.Rest), map[string]any{"got": sc.Rest})
		}
		return
	}
	// Movies: the title must be in the name, and the year if both have one.
	name := compact(e.c.Title)
	if a.Title == "" || !strings.Contains(name, compact(a.Title)) {
		e.reject(CodeWrongScene, fmt.Sprintf("the name does not contain the title %q", a.Title), nil)
		return
	}
	if a.Year > 0 && e.r.Year > 0 && (e.r.Year < a.Year-1 || e.r.Year > a.Year+1) {
		e.reject(CodeWrongYear, fmt.Sprintf("the release is from %d, not %d", e.r.Year, a.Year), map[string]any{"got": e.r.Year, "want": a.Year})
	}
}

// sceneLinkWords join a title to performer names ("… with Performer One").
var sceneLinkWords = map[string]bool{"with": true, "and": true, "feat": true, "ft": true, "featuring": true, "starring": true, "vs": true}

// sceneCore is the scene title without performer names and the words that
// join them to it: "Picnic Scene with Performer One" → "picnic scene".
func sceneCore(title string, performers []string) string {
	t := " " + NormWords(title) + " "
	for _, p := range performers {
		if n := NormWords(p); n != "" {
			t = strings.ReplaceAll(t, " "+n+" ", " ")
		}
	}
	words := strings.Fields(t)
	for len(words) > 0 && sceneLinkWords[words[0]] {
		words = words[1:]
	}
	for len(words) > 0 && sceneLinkWords[words[len(words)-1]] {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

// undatedScene judges a scene release whose name has no studio and date
// ("[Studio] Performer - Title (2020)", "Performer - Title"). The name must
// contain the whole title, or the title without its performer names together
// with a performer or the studio; a one-word title also needs a performer or
// the studio. A year in the name must be the scene's (±1).
func undatedScene(e *eval, a *Adult) {
	name := " " + NormWords(e.c.Title) + " "
	has := func(phrase string) bool { return phrase != "" && strings.Contains(name, " "+phrase+" ") }
	other := false // a performer or the studio is named too
	for _, p := range a.Performers {
		if has(NormWords(p)) {
			other = true
		}
	}
	for _, s := range a.Studios {
		if c := compact(s); c != "" && (strings.Contains(strings.ReplaceAll(name, " ", ""), c) || has(NormWords(s))) {
			other = true
		}
	}
	full := NormWords(a.Title)
	core := sceneCore(a.Title, a.Performers)
	ok := false
	switch {
	case full == "":
	case has(full) && (strings.Contains(full, " ") || other):
		ok = true
	case strings.Contains(core, " ") && has(core) && other:
		ok = true
	}
	if !ok {
		e.reject(CodeWrongScene, "the name has no date and does not contain the scene title", nil)
		return
	}
	if want := sceneYear(a.Date); want > 0 {
		if years := nameYears(name); len(years) > 0 && !slices.ContainsFunc(years, func(y int) bool { return y >= want-1 && y <= want+1 }) {
			e.reject(CodeWrongYear, fmt.Sprintf("the release is from %d, not %d", years[0], want), map[string]any{"got": years[0], "want": want})
		}
	}
}

func sceneYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return y
}

// nameYears lists the whole words of a name that look like years.
func nameYears(name string) []int {
	var out []int
	for _, w := range strings.Fields(name) {
		if len(w) != 4 || (!strings.HasPrefix(w, "19") && !strings.HasPrefix(w, "20")) {
			continue
		}
		if y, err := strconv.Atoi(w); err == nil {
			out = append(out, y)
		}
	}
	return out
}

// sceneStopWords are too common to tell two scene titles apart.
var sceneStopWords = map[string]bool{"the": true, "and": true, "with": true, "for": true, "her": true, "his": true, "she": true,
	"him": true, "you": true, "are": true, "was": true, "from": true, "into": true, "out": true, "gets": true, "xxx": true}

// sceneRestMatches reports whether the words after a release's date fit the
// wanted scene: empty (nothing to tell), a performer's name, or at least half
// of the title's distinctive words (names are often cut short).
func sceneRestMatches(rest string, a *Adult) bool {
	r := " " + NormWords(rest) + " "
	if strings.TrimSpace(r) == "" {
		return true
	}
	for _, p := range a.Performers {
		if n := NormWords(p); n != "" && strings.Contains(r, " "+n+" ") {
			return true
		}
	}
	var words []string
	for _, w := range strings.Fields(sceneCore(a.Title, a.Performers)) {
		if len(w) >= 3 && !sceneStopWords[w] {
			words = append(words, w)
		}
	}
	if len(words) == 0 {
		return len(a.Performers) == 0
	}
	found := 0
	for _, w := range words {
		if strings.Contains(r, " "+w+" ") {
			found++
		}
	}
	return found*2 >= len(words)
}
