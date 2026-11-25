package adult

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/mmzlopasa/locarr/server/internal/adult/meta"
)

// LookupResult is one search hit (no remote image is ever passed on; Image
// is a private cache key).
type LookupResult struct {
	Source     string
	ID         string
	Type       string
	Name       string
	Date       string
	Studio     string
	Performers []string
	InLibrary  bool
	Image      string
	remote     string
}

// LookupPage is one page of search hits.
type LookupPage struct {
	Results []LookupResult
	Page    int
	More    bool
}

// Lookup searches a source. Removed or disputed entries and entries with a
// blocked tag are left out (D-103). Scenes and movies come in the asked
// order (newest release first by default), asked of the source itself
// where it has that order (D-110).
func (s *Service) Lookup(ctx context.Context, source, typ, term string, o meta.Find) (LookupPage, error) {
	o.Page = max(o.Page, 1)
	if o.Sort == "" {
		o.Sort = meta.SortNewest
	}
	page := LookupPage{Page: o.Page}
	list, err := s.lookup(ctx, source, typ, term, o, &page)
	if err != nil {
		return page, err
	}
	s.lookupImages(ctx, list)
	page.Results = list
	return page, nil
}

// lookupImages caches the hits' images (a few at a time) so the results can
// show a private thumbnail.
func (s *Service) lookupImages(ctx context.Context, list []LookupResult) {
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for i := range list {
		if list[i].remote == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(r *LookupResult) {
			defer func() { <-sem; wg.Done() }()
			r.Image = s.cacheImage(ctx, r.remote)
		}(&list[i])
	}
	wg.Wait()
}

// orderResults orders a page the source could not order itself.
func orderResults(list []LookupResult, order string) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		switch order {
		case meta.SortOldest:
			if a.Date == "" || b.Date == "" {
				return a.Date != ""
			}
			return a.Date < b.Date
		case meta.SortTitle:
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		return a.Date > b.Date
	})
}

func (s *Service) lookup(ctx context.Context, source, typ, term string, o meta.Find, page *LookupPage) ([]LookupResult, error) {
	p, err := s.Provider(source)
	if err != nil {
		return nil, err
	}
	out := []LookupResult{}
	have := func(table, col, id string) bool {
		var n int
		_ = s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE `+col+` = ?`, id).Scan(&n) // #nosec G202 -- fixed names
		return n > 0
	}
	col := "stashdb_id"
	if source == meta.TPDB {
		col = "tpdb_id"
	}
	switch typ {
	case "scene", "movie":
		res, err := p.FindScenes(ctx, term, typ == "movie", o)
		if err != nil {
			return nil, err
		}
		page.More = res.More
		list := res.Scenes
		for i := range list {
			sc := &list[i]
			if s.checkEntry(ctx, sc) != nil {
				continue
			}
			r := LookupResult{Source: source, ID: sc.ID, Type: typ, Name: sc.Title, Date: sc.Date, InLibrary: have("adult_items", col, sc.ID),
				remote: sc.Image}
			if sc.Studio != nil {
				r.Studio = sc.Studio.Name
			}
			for _, pf := range sc.Performers {
				r.Performers = append(r.Performers, pf.Name)
			}
			out = append(out, r)
		}
		if !res.Ordered && o.Sort != meta.SortRelevance {
			orderResults(out, o.Sort)
		}
	case "performer":
		list, err := p.SearchPerformers(ctx, term)
		if err != nil {
			return nil, err
		}
		for _, pf := range list {
			// A stage name is the performer's own name: only the owner's terms apply to it.
			if _, _, blocked := s.BlockedMatch(ctx, []string{pf.Name}, pf.Name); blocked {
				continue
			}
			var n int
			_ = s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM adult_performers WHERE `+col+` = ? AND monitored = 1`, pf.ID).Scan(&n) // #nosec G202
			out = append(out, LookupResult{Source: source, ID: pf.ID, Type: typ, Name: pf.Name, InLibrary: n > 0, remote: pf.Image})
		}
	case "studio":
		list, err := p.SearchStudios(ctx, term)
		if err != nil {
			return nil, err
		}
		for _, st := range list {
			if _, blocked := s.BlockedText(ctx, st.Name); blocked {
				continue
			}
			out = append(out, LookupResult{Source: source, ID: st.ID, Type: typ, Name: st.Name, InLibrary: have("adult_studios", col, st.ID),
				remote: st.Image})
		}
	default:
		return nil, ErrUnknownSource
	}
	return out, nil
}
