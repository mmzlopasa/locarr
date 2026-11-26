package parser

import (
	"encoding/json"
	"os"
	"testing"
)

type galleryCase struct {
	sceneCase
	Images bool `json:"images"`
	Video  bool `json:"video"`
	Count  int  `json:"count"`
	Pixels int  `json:"pixels"`
}

// TestGalleryGolden checks image set names (synthetic, non-explicit fixture
// names only): the scene part (studio, date, rest without the image words)
// and the image set hints.
func TestGalleryGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/galleries.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []galleryCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		s, ok := ParseScene(c.Name)
		if ok != c.OK {
			t.Errorf("%q: ok = %v, want %v (%+v)", c.Name, ok, c.OK, s)
		} else if ok && (s.Studio != c.Studio || s.Date != c.Date || s.Rest != c.Rest) {
			t.Errorf("%q: got %+v, want studio %q date %q rest %q", c.Name, s, c.Studio, c.Date, c.Rest)
		}
		g := ParseGallery(c.Name)
		if g.Images != c.Images || g.Video != c.Video || g.Count != c.Count || g.Pixels != c.Pixels {
			t.Errorf("%q: gallery %+v, want images %v video %v count %d pixels %d", c.Name, g, c.Images, c.Video, c.Count, c.Pixels)
		}
	}
}

func FuzzParseGallery(f *testing.F) {
	f.Add("StudioA.24.01.15.Performer.One.Set.XXX.IMAGESET-GRP")
	f.Add("x 1 x99999 999999px")
	f.Fuzz(func(_ *testing.T, s string) { _ = ParseGallery(s) })
}
