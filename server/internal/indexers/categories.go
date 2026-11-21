package indexers

import (
	"slices"

	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
)

// Standard Newznab category ranges.
const (
	catMovies    = 2000
	catAudio     = 3000
	catAudiobook = 3030
	catTV        = 5000
	catBooks     = 7000
)

func inRange(id, base int) bool { return id >= base && id < base+1000 }

// DefaultCategories picks the standard categories an indexer offers for each
// kind of media: movies 2xxx, TV 5xxx, music 3xxx (without audiobooks) and
// books 7xxx plus audiobooks. Custom ids (100000 and up) are left out; the
// administrator can add them.
func DefaultCategories(c *znab.Caps) Categories {
	out := Categories{Movies: []int{}, TV: []int{}, Music: []int{}, Books: []int{}}
	if c == nil {
		return out
	}
	for _, id := range c.CategoryIDs() {
		switch {
		case inRange(id, catMovies):
			out.Movies = append(out.Movies, id)
		case inRange(id, catTV):
			out.TV = append(out.TV, id)
		case id == catAudiobook:
			out.Books = append(out.Books, id)
		case inRange(id, catAudio):
			out.Music = append(out.Music, id)
		case inRange(id, catBooks):
			out.Books = append(out.Books, id)
		}
	}
	return normCats(out)
}

// searchCategories narrows a category list for a request: a subcategory is
// not sent when its parent is (indexers return subcategories for a parent).
func searchCategories(ids []int) []int {
	set := map[int]bool{}
	for _, id := range ids {
		set[id] = true
	}
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		parent := id - id%1000
		if id < 100000 && id != parent && set[parent] {
			continue
		}
		out = append(out, id)
	}
	slices.Sort(out)
	return slices.Compact(out)
}
