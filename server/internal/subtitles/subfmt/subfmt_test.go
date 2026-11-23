package subfmt

import (
	"strings"
	"testing"
	"time"
)

const srtSample = "1\r\n00:00:01,500 --> 00:00:03,000\r\nPrvi redak\r\ndrugi redak\r\n\r\n2\r\n00:00:04,000 --> 00:00:05,250\r\n<i>Kurziv</i>\r\n3\r\n00:00:06,000 --> 00:00:07,000\r\nBez praznog retka prije\r\n"

const assSample = `[Script Info]
Title: test
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize
Style: Default,Arial,20

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.50,0:00:03.00,Default,,0,0,0,,{\i1}Prvi{\i0} redak\Ndrugi, sa zarezom
Comment: 0,0:00:02.00,0:00:03.00,Default,,0,0,0,,komentar
Dialogue: 0,0:01:04.00,0:01:05.25,Default,,0,0,0,,Drugi
`

func TestParseSRT(t *testing.T) {
	d, err := Parse(srtSample)
	if err != nil {
		t.Fatal(err)
	}
	if d.Format != SRT || len(d.Cues) != 3 {
		t.Fatalf("got %s with %d cues", d.Format, len(d.Cues))
	}
	if c := d.Cues[0]; c.Start != 1500*time.Millisecond || c.End != 3*time.Second || c.Text != "Prvi redak\ndrugi redak" {
		t.Fatalf("cue 1: %+v", c)
	}
	if d.Cues[1].Text != "<i>Kurziv</i>" || d.Cues[2].Text != "Bez praznog retka prije" {
		t.Fatalf("cues: %+v", d.Cues)
	}
	if err := d.Valid(); err != nil {
		t.Fatal(err)
	}
	out, _ := d.Write(SRT)
	if strings.Contains(out, "\r") || !strings.HasPrefix(out, "1\n00:00:01,500 --> 00:00:03,000\nPrvi redak\ndrugi redak\n\n2\n") {
		t.Fatalf("written:\n%q", out)
	}
	again, err := Parse(out)
	if err != nil || len(again.Cues) != 3 {
		t.Fatalf("round trip: %v %d", err, len(again.Cues))
	}
}

func TestParseWriteASS(t *testing.T) {
	d, err := Parse(assSample)
	if err != nil {
		t.Fatal(err)
	}
	if d.Format != ASS || len(d.Cues) != 2 {
		t.Fatalf("got %s with %d cues", d.Format, len(d.Cues))
	}
	if d.Cues[0].Text != "Prvi redak\ndrugi, sa zarezom" {
		t.Fatalf("text %q", d.Cues[0].Text)
	}
	d.Shift(2 * time.Second)
	out, err := d.Write(ASS)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Dialogue: 0,0:00:03.50,0:00:05.00,Default,,0,0,0,,{\\i1}Prvi{\\i0} redak\\Ndrugi, sa zarezom") ||
		!strings.Contains(out, "[V4+ Styles]") || !strings.Contains(out, "0:01:06.00,0:01:07.25") {
		t.Fatalf("written:\n%s", out)
	}
	srt, _ := d.Write(SRT)
	if !strings.Contains(srt, "00:00:03,500 --> 00:00:05,000\nPrvi redak\ndrugi, sa zarezom") {
		t.Fatalf("ASS to SRT:\n%s", srt)
	}
}

func TestParseVTT(t *testing.T) {
	d, err := Parse("WEBVTT\n\nNOTE x\n\n00:01.000 --> 00:02.500 align:start\nHello\n\nid2\n01:00:00.000 --> 01:00:01.000\nHour\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Cues) != 2 || d.Cues[0].End != 2500*time.Millisecond || d.Cues[1].Start != time.Hour {
		t.Fatalf("%+v", d.Cues)
	}
	out, _ := d.Write(VTT)
	if !strings.HasPrefix(out, "WEBVTT\n\n00:00:01.000 --> 00:00:02.500\nHello") {
		t.Fatalf("%q", out)
	}
}

func TestRejectsGarbage(t *testing.T) {
	if _, err := Parse("<html>not a subtitle</html>"); err == nil {
		t.Fatal("html accepted")
	}
	d, err := Parse("1\n00:00:05,000 --> 00:00:01,000\n\n2\n00:00:06,000 --> 00:00:01,000\n\n")
	if err == nil && d.Valid() == nil {
		t.Fatal("damaged file is valid")
	}
}

func TestScaleAndShift(t *testing.T) {
	d, _ := Parse(srtSample)
	d.Scale(25 / 23.976)
	if got := d.Cues[1].Start; got < 4170*time.Millisecond || got > 4172*time.Millisecond {
		t.Fatalf("scaled start %v", got)
	}
	d.Shift(-10 * time.Second)
	if d.Cues[0].Start != 0 {
		t.Fatalf("negative times must clamp to zero: %v", d.Cues[0].Start)
	}
}
