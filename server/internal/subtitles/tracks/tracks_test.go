package tracks_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/subtitles/tracks"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/tracks/trackstest"
)

func TestMatroskaTracks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "movie.mkv")
	m := trackstest.MKV{Tracks: []trackstest.Track{
		{Kind: "video", Codec: "V_MPEG4/ISO/AVC", Default: true},
		{Kind: "audio", Codec: "A_AC3", Language: "eng", Default: true},
		{Kind: "audio", Codec: "A_PCM/INT/LIT", Language: "hrv"},
		{Kind: "subtitle", Codec: "S_TEXT/UTF8", Language: "hrv", Name: "Hrvatski"},
		{Kind: "subtitle", Codec: "S_HDMV/PGS", Language: "eng", Forced: true},
		{Kind: "subtitle", Codec: "S_TEXT/ASS", Language: "eng", Name: "English SDH"},
		{Kind: "subtitle", Codec: "S_TEXT/UTF8", Language: "scc"},
	}, PCM: make([]int16, 16000), Rate: 16000, Cues: []trackstest.Cue{{Start: 0, End: 1e9, Text: "Bok"}}}
	if err := m.Write(p); err != nil {
		t.Fatal(err)
	}
	got, err := tracks.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 7 {
		t.Fatalf("%d tracks: %+v", len(got), got)
	}
	want := []tracks.Track{
		{Index: 0, Kind: "video", Codec: "h264", Language: "en", Default: true}, // Matroska's default language is English
		{Index: 0, Kind: "audio", Codec: "ac3", Language: "en", Default: true},
		{Index: 1, Kind: "audio", Codec: "pcm", Language: "hr"},
		{Index: 0, Kind: "subtitle", Codec: "srt", Language: "hr", Name: "Hrvatski", Text: true},
		{Index: 1, Kind: "subtitle", Codec: "pgs", Language: "en", Forced: true},
		{Index: 2, Kind: "subtitle", Codec: "ass", Language: "en", Name: "English SDH", HearingImpaired: true, Text: true},
		{Index: 3, Kind: "subtitle", Codec: "srt", Language: "sr", Text: true},
	}
	for i, w := range want {
		g := got[i]
		g.RawLanguage = ""
		if g != w {
			t.Errorf("track %d: got %+v, want %+v", i, g, w)
		}
	}
}

func TestMP4Tracks(t *testing.T) {
	b := trackstest.MP4([]trackstest.Track{
		{Kind: "video", Codec: "avc1", Language: "und", Default: true},
		{Kind: "audio", Codec: "mp4a", Language: "hrv", Default: true},
		{Kind: "subtitle", Codec: "tx3g", Language: "eng", Name: "English (Forced)"},
	})
	got, err := tracks.ReadFrom(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Kind != "video" || got[0].Language != "" || got[1].Codec != "aac" || got[1].Language != "hr" ||
		got[2].Kind != "subtitle" || !got[2].Text || !got[2].Forced || got[2].Language != "en" || got[2].Codec != "mov_text" {
		t.Fatalf("%+v", got)
	}
}

func TestUnsupportedAndDamaged(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.avi")
	_ = os.WriteFile(p, []byte("RIFF....AVI LIST"), 0o600)
	if _, err := tracks.Read(p); !errors.Is(err, tracks.ErrUnsupported) {
		t.Fatalf("avi: %v", err)
	}
	m := trackstest.MKV{Tracks: []trackstest.Track{{Kind: "audio", Codec: "A_AAC", Language: "eng"}}}.Bytes()
	for _, n := range []int{10, 30, len(m) / 2} {
		if _, err := tracks.ReadFrom(bytes.NewReader(m[:n])); err == nil && n < 40 {
			t.Errorf("truncated at %d accepted", n)
		}
	}
}

func FuzzRead(f *testing.F) {
	f.Add(trackstest.MKV{Tracks: []trackstest.Track{{Kind: "audio", Codec: "A_AAC", Language: "eng"}}}.Bytes())
	f.Add(trackstest.MP4([]trackstest.Track{{Kind: "audio", Codec: "mp4a", Language: "eng"}}))
	f.Fuzz(func(_ *testing.T, b []byte) {
		_, _ = tracks.ReadFrom(bytes.NewReader(b))
	})
}
