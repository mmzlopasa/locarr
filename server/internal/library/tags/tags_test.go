package tags_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/library/tags"
	"github.com/mmzlopasa/locarr/server/internal/library/tags/tagtest"
)

var fields = tagtest.Fields{
	"TITLE": "Žuta Kuća", "ARTIST": "Test Artist", "ALBUMARTIST": "Test Artist", "ALBUM": "Album One", "DATE": "2019-05-01",
	"TRACKNUMBER": "3/12", "DISCNUMBER": "2/2",
	"MUSICBRAINZ_TRACKID":        "1D6C0E1A-0000-4000-8000-000000000003",
	"MUSICBRAINZ_RELEASETRACKID": "2d6c0e1a-0000-4000-8000-000000000003",
	"MUSICBRAINZ_ALBUMID":        "3d6c0e1a-0000-4000-8000-000000000000",
	"MUSICBRAINZ_RELEASEGROUPID": "4d6c0e1a-0000-4000-8000-000000000000",
	"MUSICBRAINZ_ARTISTID":       "5d6c0e1a-0000-4000-8000-000000000000",
	"MUSICBRAINZ_ALBUMARTISTID":  "5d6c0e1a-0000-4000-8000-000000000000",
}

func write(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func near(a, b time.Duration) bool {
	d := a - b
	return d > -time.Second && d < time.Second
}

func checkTags(t *testing.T, got tags.Tags) {
	t.Helper()
	if got.Title != "Žuta Kuća" || got.Artist != "Test Artist" || got.AlbumArtist != "Test Artist" || got.Album != "Album One" {
		t.Errorf("text tags: %+v", got)
	}
	if got.Track != 3 || got.TrackTotal != 12 || got.Disc != 2 || got.DiscTotal != 2 {
		t.Errorf("numbers: track %d/%d disc %d/%d", got.Track, got.TrackTotal, got.Disc, got.DiscTotal)
	}
	if got.Year() != 2019 {
		t.Errorf("year %d from %q", got.Year(), got.Date)
	}
	if got.RecordingID != "1d6c0e1a-0000-4000-8000-000000000003" || got.ReleaseID != "3d6c0e1a-0000-4000-8000-000000000000" ||
		got.ReleaseGroupID != "4d6c0e1a-0000-4000-8000-000000000000" || got.ArtistID != "5d6c0e1a-0000-4000-8000-000000000000" ||
		got.AlbumArtistID != "5d6c0e1a-0000-4000-8000-000000000000" || got.TrackID != "2d6c0e1a-0000-4000-8000-000000000003" {
		t.Errorf("musicbrainz ids: %+v", got)
	}
}

func TestFormats(t *testing.T) {
	dur := 3*time.Minute + 25*time.Second
	cases := []struct {
		name     string
		data     []byte
		format   string
		depth    int
		rate     int
		bitrate  int
		vbr      bool
		lossless bool
	}{
		{"a.flac", tagtest.FLAC(fields, tagtest.Stream{Duration: dur}), tags.FormatFLAC, 16, 44100, 0, false, true},
		{"b.flac", tagtest.FLAC(fields, tagtest.Stream{Duration: dur, BitDepth: 24, SampleRate: 96000}), tags.FormatFLAC, 24, 96000, 0, false, true},
		{"c.mp3", tagtest.MP3(3, fields, tagtest.Stream{Duration: dur, Bitrate: 320}), tags.FormatMP3, 0, 44100, 320, false, false},
		{"d.mp3", tagtest.MP3(4, fields, tagtest.Stream{Duration: dur, Bitrate: 245, VBR: true}), tags.FormatMP3, 0, 44100, 245, true, false},
		{"e.m4a", tagtest.M4A(false, fields, tagtest.Stream{Duration: dur}), tags.FormatAAC, 0, 44100, 0, false, false},
		{"f.m4a", tagtest.M4A(true, fields, tagtest.Stream{Duration: dur, BitDepth: 24}), tags.FormatALAC, 24, 44100, 0, false, true},
		{"g.ogg", tagtest.Ogg(false, fields, tagtest.Stream{Duration: dur, Bitrate: 192}), tags.FormatVorbis, 0, 44100, 192, false, false},
		{"h.opus", tagtest.Ogg(true, fields, tagtest.Stream{Duration: dur}), tags.FormatOpus, 0, 44100, 0, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := tags.Read(write(t, c.name, c.data))
			if err != nil {
				t.Fatal(err)
			}
			checkTags(t, got)
			if got.Format != c.format || got.Lossless() != c.lossless {
				t.Errorf("format %s lossless %v", got.Format, got.Lossless())
			}
			if !near(got.Duration, dur) {
				t.Errorf("duration %v", got.Duration)
			}
			if c.depth > 0 && got.BitDepth != c.depth {
				t.Errorf("bit depth %d", got.BitDepth)
			}
			if got.SampleRate != c.rate {
				t.Errorf("sample rate %d", got.SampleRate)
			}
			if got.Channels != 2 {
				t.Errorf("channels %d", got.Channels)
			}
			if c.bitrate > 0 && (got.Bitrate < c.bitrate-2 || got.Bitrate > c.bitrate+2) {
				t.Errorf("bitrate %d", got.Bitrate)
			}
			if got.VBR != c.vbr {
				t.Errorf("vbr %v", got.VBR)
			}
		})
	}
}

func TestUnsupportedAndDamaged(t *testing.T) {
	if _, err := tags.Read(write(t, "x.flac", []byte("not audio at all"))); err == nil {
		t.Fatal("expected an error")
	}
	b := tagtest.FLAC(fields, tagtest.Stream{Duration: time.Minute})
	if _, err := tags.Read(write(t, "y.flac", b[:20])); err == nil {
		t.Fatal("expected an error for a truncated file")
	}
	m := tagtest.M4A(false, fields, tagtest.Stream{Duration: time.Minute})
	if _, err := tags.Read(write(t, "z.m4a", m[:60])); err == nil {
		t.Fatal("expected an error for a truncated mp4")
	}
}

func TestWriteFLAC(t *testing.T) {
	p := write(t, "w.flac", tagtest.FLAC(fields, tagtest.Stream{Duration: time.Minute, BitDepth: 24}))
	before, _ := os.ReadFile(p)
	if err := tags.WriteFLAC(p, map[string]string{"TITLE": "New Title", "ALBUM": "Album One", "TRACKNUMBER": "4/12"}); err != nil {
		t.Fatal(err)
	}
	got, err := tags.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "New Title" || got.Track != 4 || got.Artist != "" || got.BitDepth != 24 || !near(got.Duration, time.Minute) {
		t.Fatalf("after write: %+v", got)
	}
	after, _ := os.ReadFile(p)
	// The audio after the metadata is unchanged.
	if string(before[len(before)-4096:]) != string(after[len(after)-4096:]) {
		t.Fatal("audio changed")
	}
	if _, err := os.Stat(p + ".locarr-tags"); !os.IsNotExist(err) {
		t.Fatal("temporary file left behind")
	}
}

func FuzzRead(f *testing.F) {
	f.Add(tagtest.FLAC(fields, tagtest.Stream{Duration: time.Second}))
	f.Add(tagtest.MP3(4, fields, tagtest.Stream{Duration: time.Second}))
	f.Add(tagtest.M4A(true, fields, tagtest.Stream{Duration: time.Second}))
	f.Add(tagtest.Ogg(true, fields, tagtest.Stream{Duration: time.Second}))
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, b []byte) {
		p := filepath.Join(dir, "fuzz")
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _ = tags.Read(p) // must not panic
	})
}
