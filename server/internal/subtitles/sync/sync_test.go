package sync

import (
	"context"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/subtitles/subfmt"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/tracks/trackstest"
)

// dialogue makes n cues with irregular lengths and gaps, like real speech.
func dialogue(n int, seed int64) []subfmt.Cue {
	r := rand.New(rand.NewSource(seed)) // #nosec G404 -- test data
	var out []subfmt.Cue
	t := 3 * time.Second
	for range n {
		d := time.Duration(800+r.Intn(3200)) * time.Millisecond
		out = append(out, subfmt.Cue{Start: t, End: t + d, Text: "x"})
		t += d + time.Duration(300+r.Intn(5000))*time.Millisecond
	}
	return out
}

func shifted(cues []subfmt.Cue, scale float64, off time.Duration) []subfmt.Cue {
	out := make([]subfmt.Cue, len(cues))
	for i, c := range cues {
		out[i] = subfmt.Cue{Start: time.Duration(float64(c.Start)*scale) + off, End: time.Duration(float64(c.End)*scale) + off, Text: c.Text}
	}
	return out
}

func near(a, b, tol time.Duration) bool { return a-b <= tol && b-a <= tol }

func TestAlignConstantOffset(t *testing.T) {
	truth := dialogue(300, 1)
	ref := CueSignal(truth, 1, 0)
	for _, off := range []time.Duration{2500 * time.Millisecond, -4 * time.Second, 47 * time.Second, 0} {
		sub := shifted(truth, 1, -off) // the subtitle is off by -off; aligning must find +off
		r, err := Align(ref, sub)
		if err != nil {
			t.Fatal(err)
		}
		if r.Scale != 1 || !near(r.Offset, off, Step) || !r.Confident {
			t.Errorf("offset %v: got %+v", off, r)
		}
	}
}

func TestAlignFrameRate(t *testing.T) {
	truth := dialogue(400, 2)
	ref := CueSignal(truth, 1, 0)
	// Made for 25 fps, played at 23.976: its times are 23.976/25 of the truth, plus 1.2 s.
	sub := shifted(truth, 23.976/25, 1200*time.Millisecond)
	r, err := Align(ref, sub)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(r.Scale-25/23.976) > 1e-9 || !r.Confident || r.Matched < 0.9 {
		t.Fatalf("got %+v", r)
	}
	d := &subfmt.Doc{Cues: sub}
	Apply(d, r)
	for i := range truth {
		if !near(d.Cues[i].Start, truth[i].Start, 60*time.Millisecond) {
			t.Fatalf("cue %d: %v want %v", i, d.Cues[i].Start, truth[i].Start)
		}
	}
}

func TestAlignNoise(t *testing.T) {
	ref := CueSignal(dialogue(200, 3), 1, 0)
	r, err := Align(ref, dialogue(200, 99))
	if err != nil {
		t.Fatal(err)
	}
	if r.Confident {
		t.Fatalf("unrelated subtitle aligned confidently: %+v", r)
	}
	if _, err := Align(nil, dialogue(3, 1)); err == nil {
		t.Fatal("empty reference accepted")
	}
}

func TestSpeechDetection(t *testing.T) {
	spans := []trackstest.Cue{{Start: 2 * time.Second, End: 4 * time.Second}, {Start: 6 * time.Second, End: 9 * time.Second}}
	pcm := trackstest.Speech(spans, 12*time.Second, Rate)
	sp := Speech(Energies(pcm, Rate))
	on := func(t time.Duration) bool { return sp[int(t/Step)] > 0 }
	if !on(3*time.Second) || !on(7*time.Second) || on(5*time.Second) || on(time.Second) || on(11*time.Second) {
		t.Fatal("speech spans not detected")
	}
}

// TestFFmpegAudioSync decodes a generated Matroska file with ffmpeg (skipped
// without one; CI and the image have it) and re-times a late subtitle.
func TestFFmpegAudioSync(t *testing.T) {
	ff, ok := Find(os.Getenv("LOCARR_FFMPEG"))
	if !ok {
		t.Skip("no ffmpeg")
	}
	truth := dialogue(40, 5)
	var spans []trackstest.Cue
	for _, c := range truth {
		spans = append(spans, trackstest.Cue{Start: c.Start, End: c.End})
	}
	total := truth[len(truth)-1].End + 3*time.Second
	p := filepath.Join(t.TempDir(), "v.mkv")
	mkv := trackstest.MKV{Tracks: []trackstest.Track{{Kind: "audio", Codec: "A_PCM/INT/LIT", Language: "eng", Default: true},
		{Kind: "subtitle", Codec: "S_TEXT/UTF8", Language: "eng"}}, PCM: trackstest.Speech(spans, total, Rate), Rate: Rate}
	for _, c := range truth {
		mkv.Cues = append(mkv.Cues, trackstest.Cue{Start: c.Start, End: c.End, Text: "line"})
	}
	if err := mkv.Write(p); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	speech, err := ff.SpeechOf(ctx, p, 0)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Align(speech, shifted(truth, 1, 3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Confident || !near(r.Offset, -3*time.Second, 2*Step) {
		t.Fatalf("audio sync: %+v", r)
	}
	cues, err := ff.TextTrack(ctx, p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != len(truth) || !near(cues[3].Start, truth[3].Start, 10*time.Millisecond) {
		t.Fatalf("embedded track: %d cues", len(cues))
	}
}
