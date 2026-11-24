package subtitles

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/subtitles/charset"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/subfmt"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/translate"
)

// prefixer is a Translator that marks every text, in small batches.
type prefixer struct {
	calls int
	fail  error
}

func (p *prefixer) Name() string             { return "test" }
func (p *prefixer) Limits() translate.Limits { return translate.Limits{MaxTexts: 2} }
func (p *prefixer) Translate(_ context.Context, r translate.Request) ([]string, translate.Usage, error) {
	p.calls++
	if p.fail != nil {
		return nil, translate.Usage{}, p.fail
	}
	out := make([]string, len(r.Texts))
	for i, t := range r.Texts {
		out[i] = strings.Replace(t, "you", "ti", 1) + " (" + r.To + ")"
	}
	return out, translate.Usage{Characters: translate.Chars(r.Texts...), Requests: 1}, nil
}

const assDoc = `[Script Info]
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize
Style: Default,Arial,20
Style: Sign,Arial,30

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.50,Default,,0,0,0,,{\an8}Where are you going?
Dialogue: 0,0:00:04.00,0:00:06.00,Sign,,0,0,0,,{\i1}I told you\Nalready.{\i0}
Dialogue: 0,0:00:07.00,0:00:08.00,Default,,0,0,0,,{\p1}m 0 0 l 100 0 100 100{\p0}
`

func TestTranslateDocKeepsASSStylesTimingAndMarkup(t *testing.T) {
	d, err := subfmt.Parse(assDoc)
	if err != nil {
		t.Fatal(err)
	}
	p := &prefixer{}
	steps := 0
	out, units, u, err := TranslateDoc(context.Background(), p, d, "en", "hr", "Sintel (2010)", nil, func(_, _ int) { steps++ })
	if err != nil {
		t.Fatal(err)
	}
	if units != 2 || p.calls != 1 || steps != 1 || u.Characters == 0 {
		t.Fatalf("units %d, calls %d, steps %d, usage %+v", units, p.calls, steps, u)
	}
	text, err := out.Write(subfmt.ASS)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Style: Sign,Arial,30",
		`Dialogue: 0,0:00:01.00,0:00:03.50,Default,,0,0,0,,{\an8}Where are ti going? (hr)`,
		`Dialogue: 0,0:00:04.00,0:00:06.00,Sign,,0,0,0,,{\i1}I told ti already. (hr){\i0}`,
		`{\p1}m 0 0 l 100 0 100 100{\p0}`, // drawings are not text
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
	// The source document is untouched.
	if d.Cues[0].Text != "Where are you going?" {
		t.Fatalf("source changed: %q", d.Cues[0].Text)
	}
}

func TestTranslateDocWrapsAndKeepsTiming(t *testing.T) {
	src := "1\n00:00:01,000 --> 00:00:04,000\nThis sentence is long enough that it must be wrapped\nonto two lines.\n\n" +
		"2\n00:00:05,000 --> 00:00:06,000\n- Who is it?\n- It's me.\n"
	// Windows-1250 bytes decode before translation (the source is read like any subtitle).
	text, _, err := charset.Decode([]byte(src), "en")
	if err != nil {
		t.Fatal(err)
	}
	d, _ := subfmt.Parse(text)
	out, _, _, err := TranslateDoc(context.Background(), &prefixer{}, d, "en", "hr", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range d.Cues {
		if out.Cues[i].Start != d.Cues[i].Start || out.Cues[i].End != d.Cues[i].End {
			t.Fatalf("cue %d moved", i)
		}
	}
	lines := strings.Split(out.Cues[0].Text, "\n")
	if len(lines) != 2 || len([]rune(lines[0])) > 42 || len([]rune(lines[1])) > 42 {
		t.Fatalf("wrap: %q", out.Cues[0].Text)
	}
	if out.Cues[1].Text != "- Who is it? (hr)\n- It's me. (hr)" {
		t.Fatalf("dialogue: %q", out.Cues[1].Text)
	}
}

func TestTranslateDocStopsAtTheCap(t *testing.T) {
	d, _ := subfmt.Parse(assDoc)
	p := &prefixer{}
	capErr := &CapError{Translator: "test", Cap: 1}
	_, _, _, err := TranslateDoc(context.Background(), p, d, "en", "hr", "", func(int64, translate.Usage) error { return capErr }, nil)
	var ce *CapError
	if !errors.As(err, &ce) || p.calls != 0 {
		t.Fatalf("cap: %v, %d calls", err, p.calls)
	}
}

func TestTranslatedNames(t *testing.T) {
	w := Want{Language: "hr", HearingImpaired: HIInclude}
	if got := translatedPath("/m/Movie (2008).mkv", w, "translated", "srt"); got != "/m/Movie (2008).hr.translated.srt" {
		t.Fatal(got)
	}
	if got := translatedPath("/m/Movie (2008).mkv", Want{Language: "hr", Forced: true}, "", "ass"); got != "/m/Movie (2008).hr.forced.ass" {
		t.Fatal(got)
	}
	if !translatedName(".hr.translated.srt", "translated") || !translatedName(".hr.mt.srt", "mt") || translatedName(".hr.srt", "translated") {
		t.Fatal("translated marker")
	}
	if lang, _, _, _, ok := sidecarInfo(".hr.translated.srt"); !ok || lang != "hr" {
		t.Fatalf("sidecar %s", lang)
	}
	if !ValidSuffix("") || !ValidSuffix("ai-hr") || ValidSuffix("../x") {
		t.Fatal("suffix validation")
	}
}
