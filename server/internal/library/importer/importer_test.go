package importer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/library/fsops"
)

func mk(t *testing.T, p string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverSkipsSamplesAndExtras(t *testing.T) {
	d := t.TempDir()
	mk(t, filepath.Join(d, "Movie.2020.1080p.mkv"), 10000)
	mk(t, filepath.Join(d, "movie.2020.1080p-sample.mkv"), 500)
	mk(t, filepath.Join(d, "Sample", "x.mkv"), 400)
	mk(t, filepath.Join(d, "Featurettes", "Making Of.mkv"), 3000)
	mk(t, filepath.Join(d, "trailer.mp4"), 1000)
	mk(t, filepath.Join(d, "Movie.2020.1080p.en.srt"), 10)
	mk(t, filepath.Join(d, "Subs", "2_Croatian.hr.srt"), 10)
	mk(t, filepath.Join(d, "movie.nfo"), 10)
	mk(t, filepath.Join(d, ".hidden.mkv"), 99999)
	disc, err := Discover(d, []string{"srt"})
	if err != nil {
		t.Fatal(err)
	}
	main, ok := disc.MainVideo()
	if !ok || filepath.Base(main.Path) != "Movie.2020.1080p.mkv" || len(disc.Videos) != 1 {
		t.Fatalf("main: %+v videos %+v", main, disc.Videos)
	}
	reasons := map[string]string{}
	for _, s := range disc.Skipped {
		reasons[filepath.Base(s.Path)] = s.Skip
	}
	want := map[string]string{"movie.2020.1080p-sample.mkv": SkipSample, "x.mkv": SkipSample, "Making Of.mkv": SkipExtra, "trailer.mp4": SkipSmall}
	for f, r := range want {
		if reasons[f] != r {
			t.Errorf("%s skipped as %q, want %q", f, reasons[f], r)
		}
	}
	if len(disc.Sidecars) != 2 {
		t.Fatalf("sidecars: %+v", disc.Sidecars)
	}
}

func TestDiscoverDiscAndSingleFile(t *testing.T) {
	d := t.TempDir()
	mk(t, filepath.Join(d, "BDMV", "STREAM", "00001.m2ts"), 1000)
	disc, _ := Discover(d, nil)
	if _, ok := disc.MainVideo(); ok || !disc.Disc {
		t.Fatalf("disc: %+v", disc)
	}
	f := filepath.Join(d, "one.MKV")
	mk(t, f, 10)
	disc, _ = Discover(f, nil)
	if m, ok := disc.MainVideo(); !ok || m.Path != f {
		t.Fatal("single file")
	}
	// Two parts of similar size both stay (multi-part films).
	p := t.TempDir()
	mk(t, filepath.Join(p, "Film.CD1.avi"), 1000)
	mk(t, filepath.Join(p, "Film.CD2.avi"), 900)
	disc, _ = Discover(p, nil)
	disc.MainVideo()
	if len(disc.Videos) != 2 {
		t.Fatalf("parts: %+v", disc.Videos)
	}
}

func TestSidecarSuffix(t *testing.T) {
	cases := map[string]string{
		"/d/Movie.2020.en.srt":        ".en.srt",
		"/d/Movie.2020.en.forced.srt": ".en.forced.srt",
		"/d/Subs/English.srt":         ".srt",
		"/d/Subs/2_Croatian.hr.srt":   ".hr.srt",
		"/d/Subs/x.eng.sdh.SRT":       ".eng.sdh.srt",
	}
	for side, want := range cases {
		if got := SidecarSuffix("/d/Movie.2020.mkv", side); got != want {
			t.Errorf("%s -> %q, want %q", side, got, want)
		}
	}
}

func TestPlaceReplacesThroughTheHook(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "dl", "New.mkv")
	mk(t, src, 200)
	mk(t, filepath.Join(d, "dl", "New.en.srt"), 5)
	lib := filepath.Join(d, "lib", "Movie (2020)")
	mk(t, filepath.Join(lib, "Movie (2020).mkv"), 100)
	p := Placement{Roots: []string{d}, Source: src, Sidecars: []string{filepath.Join(d, "dl", "New.en.srt")}, Folder: lib,
		Name: "Movie (2020)", Mode: fsops.Hardlink}
	if _, _, err := Place(p); !errors.Is(err, fsops.ErrExists) {
		t.Fatalf("without a hook: %v", err)
	}
	var replaced []string
	p.Replace = func(path string) error {
		replaced = append(replaced, path)
		return os.Rename(path, filepath.Join(d, "bin.mkv"))
	}
	main, sides, err := Place(p)
	if err != nil || main.Size != 200 || len(sides) != 1 || filepath.Base(sides[0].Path) != "Movie (2020).en.srt" {
		t.Fatal(main, sides, err)
	}
	if len(replaced) != 1 {
		t.Fatal(replaced)
	}
	if _, err := os.Stat(filepath.Join(lib, "Movie (2020).mkv.locarr-new")); !os.IsNotExist(err) {
		t.Fatal("temporary file left")
	}
	// Outside the roots is refused.
	p.Folder = "/etc"
	if _, _, err := Place(p); err == nil {
		t.Fatal("placed outside the roots")
	}
	if ParseMode("0644") != 0o644 || ParseMode("999") != 0 {
		t.Fatal("ParseMode")
	}
}
