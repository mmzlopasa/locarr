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
	catAdult     = 6000
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
	ids := c.CategoryIDs()
	// A site that carries nothing but adult content is opted in to adult
	// searches when it is added: there is nothing else it could answer.
	if len(ids) > 0 && !slices.ContainsFunc(ids, func(id int) bool { return !inRange(id, catAdult) }) {
		out.Adult = slices.Clone(ids)
		return normCats(out)
	}
	for _, id := range ids {
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

// IsAdultRelease reports a release in the adult (6xxx) categories.
func IsAdultRelease(r *Release) bool {
	for _, c := range r.Categories {
		if inRange(c, catAdult) {
			return true
		}
	}
	return false
}

// AdultCategories picks the indexer's 6xxx categories (used when an
// administrator opts an indexer in).
func AdultCategories(c *znab.Caps) []int {
	out := []int{}
	if c == nil {
		return []int{catAdult}
	}
	for _, id := range c.CategoryIDs() {
		if inRange(id, catAdult) {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		out = append(out, catAdult)
	}
	return out
}

// AdultCapable reports whether an indexer can answer adult searches. A
// built-in site can when its definition maps adult categories; built-ins
// that cannot are left out of adult searches even when opted in (D-108).
// Torznab/Newznab indexers are left to the administrator's choice: their
// capabilities do not always list every category they return.
func (s *Service) AdultCapable(ind Indexer) bool {
	if ind.Kind == Native {
		d, ok := s.Definition(ind.Definition)
		return ok && d.HasAdult()
	}
	return true
}

// adultOnly reports a built-in site that carries nothing but adult content:
// it answers adult searches and RSS only.
func (s *Service) adultOnly(ind Indexer) bool {
	if ind.Kind != Native {
		return false
	}
	d, ok := s.Definition(ind.Definition)
	return ok && d.Adult
}

// AnswersQuery reports whether a release belongs in the answer to a search
// of type t: adult releases only answer adult searches, and an adult search
// takes adult or uncategorised releases only.
func AnswersQuery(t SearchType, r *Release) bool {
	if t != SearchAdult {
		return !IsAdultRelease(r)
	}
	return IsAdultRelease(r) || len(r.Categories) == 0
}
