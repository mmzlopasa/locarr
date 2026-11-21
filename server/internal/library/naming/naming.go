// Package naming renders folder and file names from templates such as
// "{Title} ({Year}) {[QualityFull]}{-ReleaseGroup}" and makes the result safe
// on every common filesystem. It is shared by movies, series and music: each
// kind supplies its token values.
//
// Template syntax: "{" prefix? Token suffix? "}". Prefix and suffix are any
// characters other than letters, digits and braces; they are written only
// when the token has a value, so "{[Edition]}" disappears for a film without
// an edition. Text outside braces is copied as is.
package naming

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxSegment is the longest file or folder name (bytes) most filesystems accept.
const MaxSegment = 255

// ErrTemplate is returned for an invalid template.
var ErrTemplate = errors.New("invalid naming template")

// Values are token values by token name (case-sensitive).
type Values map[string]string

// Options control sanitising.
type Options struct {
	// Colon is "delete", "dash" (default) or "space_dash".
	Colon string
}

// Token documents one token.
type Token struct {
	Name    string `json:"name"`
	Example string `json:"example"`
}

type part struct {
	literal        string
	token          string
	prefix, suffix string
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func parse(tmpl string) ([]part, error) {
	var out []part
	for len(tmpl) > 0 {
		open := strings.IndexByte(tmpl, '{')
		closeAt := strings.IndexByte(tmpl, '}')
		if open < 0 {
			if closeAt >= 0 {
				return nil, fmt.Errorf("%w: unmatched }", ErrTemplate)
			}
			out = append(out, part{literal: tmpl})
			break
		}
		if closeAt >= 0 && closeAt < open {
			return nil, fmt.Errorf("%w: unmatched }", ErrTemplate)
		}
		if open > 0 {
			out = append(out, part{literal: tmpl[:open]})
		}
		end := strings.IndexByte(tmpl[open:], '}')
		if end < 0 {
			return nil, fmt.Errorf("%w: unmatched {", ErrTemplate)
		}
		inner := tmpl[open+1 : open+end]
		if strings.ContainsRune(inner, '{') {
			return nil, fmt.Errorf("%w: nested {", ErrTemplate)
		}
		start := strings.IndexFunc(inner, isWord)
		if start < 0 {
			return nil, fmt.Errorf("%w: empty token {%s}", ErrTemplate, inner)
		}
		stop := start
		for stop < len(inner) {
			r, n := utf8.DecodeRuneInString(inner[stop:])
			if !isWord(r) {
				break
			}
			stop += n
		}
		if strings.IndexFunc(inner[stop:], isWord) >= 0 {
			return nil, fmt.Errorf("%w: {%s} holds more than one token", ErrTemplate, inner)
		}
		out = append(out, part{token: inner[start:stop], prefix: inner[:start], suffix: inner[stop:]})
		tmpl = tmpl[open+end+1:]
	}
	return out, nil
}

// Validate checks the syntax and that every token is known.
func Validate(tmpl string, known []Token) error {
	if strings.TrimSpace(tmpl) == "" {
		return fmt.Errorf("%w: empty", ErrTemplate)
	}
	parts, err := parse(tmpl)
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for _, t := range known {
		names[t.Name] = true
	}
	hasToken := false
	for _, p := range parts {
		if p.token == "" {
			continue
		}
		hasToken = true
		if !names[p.token] {
			return fmt.Errorf("%w: unknown token {%s}", ErrTemplate, p.token)
		}
	}
	if !hasToken {
		return fmt.Errorf("%w: no tokens", ErrTemplate)
	}
	if strings.ContainsAny(tmpl, `/\`) {
		return fmt.Errorf("%w: a name cannot contain / or \\", ErrTemplate)
	}
	return nil
}

// Render fills the template and sanitises the result into one safe file or
// folder name (without extension).
func Render(tmpl string, v Values, o Options) (string, error) {
	parts, err := parse(tmpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, p := range parts {
		if p.token == "" {
			b.WriteString(p.literal)
			continue
		}
		val := strings.TrimSpace(v[p.token])
		if val == "" {
			continue
		}
		b.WriteString(p.prefix)
		b.WriteString(val)
		b.WriteString(p.suffix)
	}
	return Sanitize(tidy(b.String()), o), nil
}

// tidy removes what empty tokens leave behind: doubled spaces, empty
// brackets and dangling separators.
func tidy(s string) string {
	for _, e := range []string{"()", "[]", "{}"} {
		s = strings.ReplaceAll(s, e, "")
	}
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimRight(s, " -_.")
	return strings.TrimLeft(s, " -_.")
}

var reserved = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true}

// Sanitize makes name safe on Linux, macOS and Windows (shares are often
// read from Windows): no path separators, no characters Windows refuses, no
// control characters, no trailing dots or spaces, no reserved device names,
// at most MaxSegment bytes.
func Sanitize(name string, o Options) string {
	switch o.Colon {
	case ColonDelete:
		name = strings.ReplaceAll(name, ":", "")
	case ColonSpaceDash:
		name = strings.ReplaceAll(name, ":", " - ")
	default:
		name = strings.ReplaceAll(name, ": ", " - ")
		name = strings.ReplaceAll(name, ":", "-")
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 32 || r == 127:
		case r == '/' || r == '\\' || r == '|':
			b.WriteRune(' ')
		case r == '?' || r == '*' || r == '"' || r == '<' || r == '>':
			if r == '"' {
				b.WriteRune('\'')
			}
		default:
			b.WriteRune(r)
		}
	}
	name = strings.Join(strings.Fields(b.String()), " ")
	name = strings.TrimRight(name, ". ")
	name = strings.TrimLeft(name, " ")
	if name == "" || name == "." || name == ".." {
		name = "_"
	}
	base := strings.ToUpper(name)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	if reserved[base] {
		name = "_" + name
	}
	return Truncate(name, MaxSegment)
}

// Truncate shortens s to at most limit bytes on a character boundary, keeping
// the end readable (no trailing space or dot).
func Truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimRight(s[:cut], ". ")
}

// WithExtension joins a rendered name and an extension (".mkv") so that the
// whole stays within MaxSegment.
func WithExtension(name, ext string) string {
	ext = strings.ToLower(ext)
	return Truncate(name, MaxSegment-len(ext)) + ext
}

// Constants re-exported so callers need not import library for options.
const (
	ColonDelete    = "delete"
	ColonDash      = "dash"
	ColonSpaceDash = "space_dash"
)

// Join builds root/folder/file, refusing anything that would leave root.
func Join(root, folder, file string) (string, error) {
	for _, seg := range []string{folder, file} {
		if seg == "" || seg == "." || seg == ".." || strings.ContainsAny(seg, `/\`) {
			return "", fmt.Errorf("%w: unsafe name %q", ErrTemplate, seg)
		}
	}
	return filepath.Join(root, folder, file), nil
}

// TokenNames returns the names sorted (for error messages and docs).
func TokenNames(ts []Token) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Name)
	}
	sort.Strings(out)
	return out
}
