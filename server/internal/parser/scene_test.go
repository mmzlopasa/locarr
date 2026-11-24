package parser

import (
	"encoding/json"
	"os"
	"testing"
)

type sceneCase struct {
	Name       string `json:"name"`
	OK         bool   `json:"ok"`
	Studio     string `json:"studio"`
	Date       string `json:"date"`
	Rest       string `json:"rest"`
	Resolution int    `json:"resolution"`
}

// TestSceneGolden checks date-based scene names (synthetic, non-explicit
// fixture names only).
func TestSceneGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/scenes.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []sceneCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		s, ok := ParseScene(c.Name)
		if ok != c.OK {
			t.Errorf("%q: ok = %v, want %v (%+v)", c.Name, ok, c.OK, s)
			continue
		}
		if !ok {
			continue
		}
		if s.Studio != c.Studio || s.Date != c.Date || s.Rest != c.Rest {
			t.Errorf("%q: got %+v, want studio %q date %q rest %q", c.Name, s, c.Studio, c.Date, c.Rest)
		}
		if c.Resolution > 0 {
			if r := Parse(c.Name); r.Video.Resolution != c.Resolution {
				t.Errorf("%q: resolution %d, want %d", c.Name, r.Video.Resolution, c.Resolution)
			}
		}
	}
}

func FuzzParseScene(f *testing.F) {
	f.Add("StudioA.24.01.15.Performer.One.Scene.Title.1080p.MP4-GRP")
	f.Add("[S] x (2024-01-01)")
	f.Fuzz(func(_ *testing.T, s string) { _, _ = ParseScene(s) })
}
