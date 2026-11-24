package adult

import (
	"slices"
	"testing"
)

// TestSearchTerms checks the order and form of the search texts for a scene
// and a movie (synthetic, non-explicit names).
func TestSearchTerms(t *testing.T) {
	cases := []struct {
		scene      bool
		studio     string
		date       string
		title      string
		year       int
		performers []string
		want       []string
	}{
		{true, "Studio H", "2020-08-19", "Outdoor Picnic Scene with Performer Seven", 2020, []string{"Performer Seven", "Performer Eight"},
			[]string{"Studio H 20 08 19", "StudioH 20.08.19", "Performer Seven Outdoor Picnic Scene", "Outdoor Picnic Scene with Performer Seven"}},
		// A studio written together gives the same words twice; the caller drops repeats.
		{true, "StudioA", "2024-01-15", "Scene Title", 2024, nil,
			[]string{"StudioA 24 01 15", "StudioA 24.01.15", "Scene Title"}},
		// Long titles are cut for the performer search.
		{true, "Studio-B", "2023-12-31", "One Two Three Four Five Six Seven Eight", 2023, []string{"Performer Nine"},
			[]string{"Studio B 23 12 31", "StudioB 23.12.31", "Performer Nine One Two Three Four Five Six", "One Two Three Four Five Six Seven Eight"}},
		// No date: performer + title, then the title.
		{true, "Studio C", "", "Scene Title", 0, []string{"Performer One"}, []string{"Performer One Scene Title", "Scene Title"}},
		{false, "", "", "Movie Title", 2023, nil, []string{"Movie Title 2023", "Movie Title"}},
		{false, "", "", "Movie Title", 0, nil, []string{"Movie Title"}},
	}
	for _, c := range cases {
		got := searchTerms(c.scene, c.studio, c.date, c.title, c.year, c.performers)
		if !slices.Equal(got, c.want) {
			t.Errorf("%q: got %q, want %q", c.title, got, c.want)
		}
	}
}
