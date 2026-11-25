package quality

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/parser"
)

// TestSceneQualityGolden checks the quality read from scene names as general
// sites write them (synthetic, non-explicit names only; D-109).
func TestSceneQualityGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/scene_quality.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name    string `json:"name"`
		Quality string `json:"quality"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		r := parser.Parse(c.Name)
		if got := Identify(&r, Movies).ID; got != c.Quality {
			t.Errorf("%q: %s, want %s (resolution %d, source %q)", c.Name, got, c.Quality, r.Video.Resolution, r.Video.Source)
		}
	}
}

// TestOpenProfile: every video quality is allowed, Unknown included, and the
// profile is valid.
func TestOpenProfile(t *testing.T) {
	p := OpenProfile()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, q := range ForKind(Movies) {
		if pos, ok := p.Position(q.ID); pos < 0 || !ok {
			t.Errorf("%s not allowed", q.ID)
		}
	}
}
