package adult

import (
	"context"

	"github.com/mmzlopasa/locarr/server/internal/adult/meta"
)

// LookupResult is one search hit (no remote image is ever passed on).
type LookupResult struct {
	Source     string
	ID         string
	Type       string
	Name       string
	Date       string
	Studio     string
	Performers []string
	InLibrary  bool
}

// Lookup searches a source. Removed or disputed entries and entries with a
// blocked tag are left out (D-102).
func (s *Service) Lookup(ctx context.Context, source, typ, term string) ([]LookupResult, error) {
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
		var list []meta.Scene
		if typ == "movie" {
			list, err = p.SearchMovies(ctx, term)
		} else {
			list, err = p.SearchScenes(ctx, term)
		}
		if err != nil {
			return nil, err
		}
		for i := range list {
			sc := &list[i]
			if s.checkEntry(ctx, sc) != nil {
				continue
			}
			r := LookupResult{Source: source, ID: sc.ID, Type: typ, Name: sc.Title, Date: sc.Date, InLibrary: have("adult_items", col, sc.ID)}
			if sc.Studio != nil {
				r.Studio = sc.Studio.Name
			}
			for _, pf := range sc.Performers {
				r.Performers = append(r.Performers, pf.Name)
			}
			out = append(out, r)
		}
	case "performer":
		list, err := p.SearchPerformers(ctx, term)
		if err != nil {
			return nil, err
		}
		for _, pf := range list {
			if _, blocked := s.BlockedText(ctx, pf.Name); blocked {
				continue
			}
			var n int
			_ = s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM adult_performers WHERE `+col+` = ? AND monitored = 1`, pf.ID).Scan(&n) // #nosec G202
			out = append(out, LookupResult{Source: source, ID: pf.ID, Type: typ, Name: pf.Name, InLibrary: n > 0})
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
			out = append(out, LookupResult{Source: source, ID: st.ID, Type: typ, Name: st.Name, InLibrary: have("adult_studios", col, st.ID)})
		}
	default:
		return nil, ErrUnknownSource
	}
	return out, nil
}
