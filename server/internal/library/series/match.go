package series

import (
	"sort"

	"github.com/mmzlopasa/locarr/server/internal/parser"
)

// IsPack reports a release that names whole seasons, not episodes.
func IsPack(r *parser.Release) bool {
	return len(r.Episodes) == 0 && len(r.AbsoluteEpisodes) == 0 && r.AirDate == "" &&
		(r.FullSeason || r.MultiSeason || r.CompleteSeries || len(r.Seasons) > 0)
}

// MatchRelease returns the episodes (of eps, one series) a parsed release or
// file name holds, in order. Daily shows match by air date, anime by absolute
// number (unless the name has season and episode), everything else by season
// and episode; the manual scene mapping (D-040) wins over the provider's
// numbers. Season packs return every episode of their seasons, complete
// series every episode outside specials.
func MatchRelease(seriesType string, eps []Episode, r *parser.Release) []*Episode {
	var out []*Episode
	seen := map[int64]bool{}
	add := func(e *Episode) {
		if !seen[e.ID] {
			seen[e.ID] = true
			out = append(out, e)
		}
	}
	switch {
	case r.AirDate != "" && (seriesType == TypeDaily || len(r.Seasons) == 0):
		for i := range eps {
			if eps[i].AirDate == r.AirDate {
				add(&eps[i])
			}
		}
	case len(r.AbsoluteEpisodes) > 0 && (len(r.Episodes) == 0 || seriesType == TypeAnime):
		for _, a := range r.AbsoluteEpisodes {
			found := false
			for i := range eps {
				if eps[i].SceneAbsolute != nil && *eps[i].SceneAbsolute == a {
					add(&eps[i])
					found = true
				}
			}
			if found {
				continue
			}
			for i := range eps {
				if eps[i].SceneAbsolute == nil && eps[i].Absolute == a && eps[i].Season != 0 {
					add(&eps[i])
				}
			}
		}
	case len(r.Seasons) > 0 && len(r.Episodes) > 0:
		for _, s := range r.Seasons {
			for _, n := range r.Episodes {
				found := false
				for i := range eps {
					rs, rn := eps[i].ReleaseNumbers()
					if (eps[i].SceneSeason != nil || eps[i].SceneEpisode != nil) && rs == s && rn == n {
						add(&eps[i])
						found = true
					}
				}
				if found {
					continue
				}
				for i := range eps {
					if eps[i].SceneSeason == nil && eps[i].SceneEpisode == nil && eps[i].Season == s && eps[i].Number == n {
						add(&eps[i])
					}
				}
			}
		}
	case r.CompleteSeries && len(r.Seasons) == 0:
		for i := range eps {
			if eps[i].Season != 0 {
				add(&eps[i])
			}
		}
	case len(r.Seasons) > 0:
		want := map[int]bool{}
		for _, s := range r.Seasons {
			want[s] = true
		}
		for i := range eps {
			rs, _ := eps[i].ReleaseNumbers()
			if want[rs] {
				add(&eps[i])
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Season != out[j].Season {
			return out[i].Season < out[j].Season
		}
		return out[i].Number < out[j].Number
	})
	return out
}
