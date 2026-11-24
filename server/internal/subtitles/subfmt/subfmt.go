// Package subfmt reads, checks, re-times and writes subtitle files: SubRip
// (SRT), Advanced SubStation Alpha / SubStation Alpha (ASS/SSA) and WebVTT.
// Input is UTF-8 text (see package charset for decoding); output is UTF-8
// with LF line endings.
package subfmt

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Formats.
const (
	SRT = "srt"
	ASS = "ass"
	SSA = "ssa"
	VTT = "vtt"
)

// Errors.
var (
	ErrUnknownFormat = errors.New("not a subtitle file (SRT, ASS, SSA or VTT)")
	ErrNoCues        = errors.New("the subtitle file has no cues")
)

// Cue is one subtitle line on screen.
type Cue struct {
	Start, End time.Duration
	// Text is plain text; lines separated by "\n" (SRT/VTT tags kept).
	Text string
	// raw keeps the ASS Dialogue fields so styling survives re-timing.
	raw []string
}

// Doc is a parsed subtitle file.
type Doc struct {
	Format string
	Cues   []Cue
	// header is everything of an ASS/SSA file before [Events] plus the
	// Format line; vttHeader is the WEBVTT block of a VTT file.
	header    string
	format    []string // ASS Events Format fields (lower-case)
	vttHeader string
}

// Normalize turns CRLF and CR line endings into LF and drops a BOM.
func Normalize(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// Detect names the format of the text ("" when unknown).
func Detect(s string) string {
	t := strings.TrimSpace(Normalize(s))
	switch {
	case strings.HasPrefix(t, "WEBVTT"):
		return VTT
	case strings.HasPrefix(t, "[Script Info]") || strings.Contains(t, "\n[Events]") || strings.HasPrefix(t, "[Events]"):
		if strings.Contains(strings.ToLower(t), "scripttype: v4.00+") || strings.Contains(t, "[V4+ Styles]") {
			return ASS
		}
		return SSA
	case srtTime.MatchString(t):
		return SRT
	}
	return ""
}

// Parse reads a subtitle file of any supported format.
func Parse(s string) (*Doc, error) {
	s = Normalize(s)
	var (
		d   *Doc
		err error
	)
	switch Detect(s) {
	case SRT:
		d, err = parseSRT(s)
	case VTT:
		d, err = parseVTT(s)
	case ASS, SSA:
		d, err = parseASS(s)
	default:
		return nil, ErrUnknownFormat
	}
	if err != nil {
		return nil, err
	}
	if len(d.Cues) == 0 {
		return nil, ErrNoCues
	}
	return d, nil
}

var (
	srtTime = regexp.MustCompile(`(?m)^\s*(\d{1,3}):(\d{1,2}):(\d{1,2})[,.](\d{1,3})\s*-->\s*(\d{1,3}):(\d{1,2}):(\d{1,2})[,.](\d{1,3})`)
	vttTime = regexp.MustCompile(`^\s*(?:(\d{1,3}):)?(\d{1,2}):(\d{1,2})\.(\d{1,3})\s*-->\s*(?:(\d{1,3}):)?(\d{1,2}):(\d{1,2})\.(\d{1,3})(.*)$`)
)

func ms(frac string) time.Duration {
	for len(frac) < 3 {
		frac += "0"
	}
	n, _ := strconv.Atoi(frac[:3])
	return time.Duration(n) * time.Millisecond
}

func hms(h, m, s, frac string) time.Duration {
	hh, _ := strconv.Atoi(h)
	mm, _ := strconv.Atoi(m)
	ss, _ := strconv.Atoi(s)
	return time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute + time.Duration(ss)*time.Second + ms(frac)
}

func parseSRT(s string) (*Doc, error) {
	d := &Doc{Format: SRT}
	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines); i++ {
		m := srtTime.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		c := Cue{Start: hms(m[1], m[2], m[3], m[4]), End: hms(m[5], m[6], m[7], m[8])}
		var text []string
		for i++; i < len(lines); i++ {
			l := strings.TrimRight(lines[i], " \t")
			if l == "" {
				break
			}
			// A missing blank line before the next cue: its number is followed by a time line.
			if i+1 < len(lines) && isNumber(l) && srtTime.MatchString(lines[i+1]) {
				i--
				break
			}
			text = append(text, l)
		}
		c.Text = strings.Join(text, "\n")
		d.Cues = append(d.Cues, c)
	}
	return d, nil
}

func isNumber(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseVTT(s string) (*Doc, error) {
	d := &Doc{Format: VTT}
	blocks := strings.Split(s, "\n\n")
	if len(blocks) > 0 {
		d.vttHeader = strings.TrimSpace(blocks[0])
	}
	for _, b := range blocks[1:] {
		lines := strings.Split(strings.Trim(b, "\n"), "\n")
		for i, l := range lines {
			m := vttTime.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			c := Cue{Start: hms(m[1], m[2], m[3], m[4]), End: hms(m[5], m[6], m[7], m[8]), Text: strings.Join(lines[i+1:], "\n")}
			d.Cues = append(d.Cues, c)
			break
		}
	}
	return d, nil
}

// ASS times are h:mm:ss.cc (centiseconds).
func parseASSTime(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	h, rest, ok := strings.Cut(s, ":")
	if !ok {
		return 0, false
	}
	m, rest, ok := strings.Cut(rest, ":")
	if !ok {
		return 0, false
	}
	sec, frac, _ := strings.Cut(rest, ".")
	if !isNumber(h) || !isNumber(m) || !isNumber(sec) {
		return 0, false
	}
	return hms(h, m, sec, frac), true
}

func formatASSTime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	cs := (d + 5*time.Millisecond) / (10 * time.Millisecond)
	h := cs / 360000
	m := cs / 6000 % 60
	sec := cs / 100 % 60
	return fmt.Sprintf("%d:%02d:%02d.%02d", h, m, sec, cs%100)
}

var assTags = regexp.MustCompile(`\{[^}]*\}`)

// assText turns ASS event text into plain text.
func assText(s string) string {
	s = assTags.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, `\N`, "\n")
	s = strings.ReplaceAll(s, `\n`, "\n")
	return strings.ReplaceAll(s, `\h`, " ")
}

func parseASS(s string) (*Doc, error) {
	d := &Doc{Format: ASS}
	if !strings.Contains(s, "[V4+ Styles]") && !strings.Contains(strings.ToLower(s), "scripttype: v4.00+") {
		d.Format = SSA
	}
	head, events, ok := strings.Cut(s, "[Events]")
	if !ok {
		return nil, fmt.Errorf("%w: ASS/SSA file without [Events]", ErrUnknownFormat)
	}
	d.header = strings.TrimRight(head, "\n")
	d.format = []string{"layer", "start", "end", "style", "name", "marginl", "marginr", "marginv", "effect", "text"}
	for _, l := range strings.Split(events, "\n") {
		key, val, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(key)) {
		case "format":
			d.format = nil
			for _, f := range strings.Split(val, ",") {
				d.format = append(d.format, strings.ToLower(strings.TrimSpace(f)))
			}
		case "dialogue":
			fields := strings.SplitN(strings.TrimPrefix(val, " "), ",", len(d.format))
			if len(fields) != len(d.format) {
				continue
			}
			c := Cue{raw: fields}
			var okS, okE bool
			for i, f := range d.format {
				switch f {
				case "start":
					c.Start, okS = parseASSTime(fields[i])
				case "end":
					c.End, okE = parseASSTime(fields[i])
				case "text":
					c.Text = assText(fields[i])
				}
			}
			if okS && okE {
				d.Cues = append(d.Cues, c)
			}
		}
	}
	return d, nil
}

// Problems lists what is wrong with a parsed file (empty = valid): cues
// that end before they start, overlap heavily or are empty. A few problems
// are normal in real files; Valid decides when there are too many.
func (d *Doc) Problems() []string {
	var out []string
	empty, backwards := 0, 0
	for _, c := range d.Cues {
		if strings.TrimSpace(c.Text) == "" {
			empty++
		}
		if c.End < c.Start {
			backwards++
		}
	}
	if empty > 0 {
		out = append(out, fmt.Sprintf("%d empty cues", empty))
	}
	if backwards > 0 {
		out = append(out, fmt.Sprintf("%d cues end before they start", backwards))
	}
	return out
}

// Valid reports whether the file is usable: it has cues and most of them
// have text and a sane duration.
func (d *Doc) Valid() error {
	if len(d.Cues) == 0 {
		return ErrNoCues
	}
	bad := 0
	for _, c := range d.Cues {
		if strings.TrimSpace(c.Text) == "" || c.End < c.Start || c.End-c.Start > 5*time.Minute {
			bad++
		}
	}
	if bad*2 > len(d.Cues) {
		return fmt.Errorf("the subtitle file is damaged: %d of %d cues are empty or have impossible times", bad, len(d.Cues))
	}
	return nil
}

// Shift moves every cue by off (negative = earlier). Cues pushed before zero
// start at zero.
func (d *Doc) Shift(off time.Duration) {
	d.Transform(func(t time.Duration) time.Duration { return t + off })
}

// Scale multiplies every time by factor (frame rate conversion: a subtitle
// made for 25 fps played on a 23.976 fps file needs 25/23.976).
func (d *Doc) Scale(factor float64) {
	d.Transform(func(t time.Duration) time.Duration { return time.Duration(math.Round(float64(t) * factor)) })
}

// Transform maps every cue time through f.
func (d *Doc) Transform(f func(time.Duration) time.Duration) {
	for i := range d.Cues {
		c := &d.Cues[i]
		c.Start, c.End = max(f(c.Start), 0), max(f(c.End), 0)
	}
}

// Sorted returns the cues ordered by start time.
func (d *Doc) Sorted() []Cue {
	out := append([]Cue(nil), d.Cues...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

func formatSRTTime(d time.Duration, sep string) string {
	if d < 0 {
		d = 0
	}
	msec := d / time.Millisecond
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", msec/3600000, msec/60000%60, msec/1000%60, sep, msec%1000)
}

// Write renders the document in format (SRT, ASS/SSA only from ASS/SSA
// sources, VTT).
func (d *Doc) Write(format string) (string, error) {
	switch format {
	case SRT:
		return d.writeSRT(), nil
	case VTT:
		return d.writeVTT(), nil
	case ASS, SSA:
		if d.Format != ASS && d.Format != SSA {
			return "", fmt.Errorf("cannot convert %s to %s", d.Format, format)
		}
		return d.writeASS(), nil
	}
	return "", fmt.Errorf("unknown subtitle format %q", format)
}

func (d *Doc) writeSRT() string {
	var b strings.Builder
	n := 0
	for _, c := range d.Sorted() {
		text := strings.TrimSpace(c.Text)
		if text == "" {
			continue
		}
		n++
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", n, formatSRTTime(c.Start, ","), formatSRTTime(c.End, ","), text)
	}
	return b.String()
}

func (d *Doc) writeVTT() string {
	var b strings.Builder
	head := d.vttHeader
	if head == "" {
		head = "WEBVTT"
	}
	b.WriteString(head + "\n\n")
	for _, c := range d.Sorted() {
		if strings.TrimSpace(c.Text) == "" {
			continue
		}
		fmt.Fprintf(&b, "%s --> %s\n%s\n\n", formatSRTTime(c.Start, "."), formatSRTTime(c.End, "."), strings.TrimSpace(c.Text))
	}
	return b.String()
}

func (d *Doc) writeASS() string {
	var b strings.Builder
	b.WriteString(d.header)
	b.WriteString("\n\n[Events]\nFormat: ")
	names := make([]string, len(d.format))
	for i, f := range d.format {
		names[i] = assFieldName(f)
	}
	b.WriteString(strings.Join(names, ", ") + "\n")
	for _, c := range d.Cues {
		fields := append([]string(nil), c.raw...)
		if len(fields) != len(d.format) {
			continue
		}
		for i, f := range d.format {
			switch f {
			case "start":
				fields[i] = formatASSTime(c.Start)
			case "end":
				fields[i] = formatASSTime(c.End)
			}
		}
		b.WriteString("Dialogue: " + strings.Join(fields, ",") + "\n")
	}
	return b.String()
}

func assFieldName(f string) string {
	switch f {
	case "marginl":
		return "MarginL"
	case "marginr":
		return "MarginR"
	case "marginv":
		return "MarginV"
	case "":
		return ""
	}
	return strings.ToUpper(f[:1]) + f[1:]
}

// Ext is the file extension (without dot) for a format.
func Ext(format string) string { return format }

// textField is the index of the Text field of ASS events (-1 = none).
func (d *Doc) textField() int {
	for i, f := range d.format {
		if f == "text" {
			return i
		}
	}
	return -1
}

// EditableText returns the text of cue i for editing: for ASS/SSA the
// event text with its override blocks ({\an8}) and "\n" for line breaks;
// for SRT/VTT the text as it is.
func (d *Doc) EditableText(i int) string {
	c := d.Cues[i]
	if f := d.textField(); (d.Format == ASS || d.Format == SSA) && f >= 0 && f < len(c.raw) {
		s := strings.ReplaceAll(c.raw[f], `\N`, "\n")
		return strings.ReplaceAll(s, `\n`, "\n")
	}
	return c.Text
}

// SetText replaces the text of cue i (same convention as EditableText);
// times and, for ASS/SSA, the style and every other field stay.
func (d *Doc) SetText(i int, text string) {
	c := &d.Cues[i]
	if f := d.textField(); (d.Format == ASS || d.Format == SSA) && f >= 0 && f < len(c.raw) {
		raw := append([]string(nil), c.raw...)
		raw[f] = strings.ReplaceAll(text, "\n", `\N`)
		c.raw = raw
		c.Text = assText(raw[f])
		return
	}
	c.Text = text
}

// Clone returns a copy that can be changed without touching d.
func (d *Doc) Clone() *Doc {
	n := *d
	n.Cues = append([]Cue(nil), d.Cues...)
	return &n
}
