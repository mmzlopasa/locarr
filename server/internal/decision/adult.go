package decision

import (
	"fmt"
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
// a whole word or phrase (terms and text are compared as NormWords).
func BlockedTerm(text string, terms []string) (string, bool) {
	if len(terms) == 0 {
		return "", false
	}
	t := " " + NormWords(text) + " "
	joined := " " + strings.ReplaceAll(strings.TrimSpace(t), " ", "") + " "
	for _, term := range terms {
		n := NormWords(term)
		if n == "" {
			continue
		}
		if strings.Contains(t, " "+n+" ") {
			return term, true
		}
		// "Some.Term" written together ("someterm") is caught too.
		if strings.Contains(n, " ") && strings.Contains(joined, strings.ReplaceAll(n, " ", "")) {
			return term, true
		}
	}
	return "", false
}

func specBlockedTag(e *eval) {
	if term, ok := BlockedTerm(e.c.Title, e.ctx.BlockedTerms); ok {
		e.reject(CodeBlockedTag, fmt.Sprintf("the name contains the blocked term %q", term), map[string]any{"term": term})
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
			// Without a date the title (and a performer) must be in the name.
			name := " " + NormWords(e.c.Title) + " "
			if a.Title == "" || !strings.Contains(name, " "+NormWords(a.Title)+" ") {
				e.reject(CodeWrongScene, "the name has no date and does not contain the scene title", nil)
			}
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
