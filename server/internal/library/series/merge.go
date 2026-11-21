package series

import (
	"sort"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
)

// NewEpisode is a provider episode to add.
type NewEpisode struct {
	metadata.Episode
	Monitored bool
}

// EpisodeUpdate refreshes an existing episode from the provider.
type EpisodeUpdate struct {
	ID int64
	metadata.Episode
	// Renumbered: the provider moved it (same provider id, other numbers).
	Renumbered             bool
	FromSeason, FromNumber int
}

// MergePlan is what a refresh changes (D-050).
type MergePlan struct {
	Insert []NewEpisode
	Update []EpisodeUpdate
	// Delete: gone from the provider and without a file.
	Delete []int64
	// Keep: gone from the provider but has a file; kept, marked removed and unmonitored.
	Keep       []int64
	NewSeasons []int
}

func providerID(e metadata.Episode, source string) int64 {
	if source == "tvdb" {
		return e.TVDbID
	}
	return e.TMDbID
}

func existingID(e *Episode, source string) int64 {
	if source == "tvdb" {
		return e.TVDbID
	}
	return e.TMDbID
}

// FillAbsolute numbers the episodes outside specials 1, 2, 3… in season
// order when the provider gives no absolute numbers (TMDb, D-047).
func FillAbsolute(seasons []metadata.Season) {
	for _, sn := range seasons {
		for _, e := range sn.Episodes {
			if e.Absolute > 0 {
				return
			}
		}
	}
	sort.SliceStable(seasons, func(i, j int) bool { return seasons[i].Number < seasons[j].Number })
	n := 0
	for si := range seasons {
		if seasons[si].Number == 0 {
			continue
		}
		for ei := range seasons[si].Episodes {
			n++
			seasons[si].Episodes[ei].Absolute = n
		}
	}
}

// PlanMerge compares the library's episodes with the provider's. Episodes
// are matched by the provider's episode id first (so a renumbered episode
// keeps its file and monitoring), then by season and number. Monitoring of
// existing episodes never changes; new ones follow their season (monitored
// seasons monitor new episodes) and new seasons follow monitorNew. Specials
// are never monitored automatically.
func PlanMerge(existing []Episode, provider []metadata.Season, source string, seasonMonitored map[int]bool, monitorNew bool) MergePlan {
	var plan MergePlan
	claimed := map[int64]bool{}
	byPID := map[int64]*Episode{}
	byNum := map[[2]int]*Episode{}
	for i := range existing {
		e := &existing[i]
		if id := existingID(e, source); id > 0 {
			byPID[id] = e
		}
		byNum[[2]int{e.Season, e.Number}] = e
	}
	var rest []metadata.Episode
	for _, sn := range provider {
		if _, ok := seasonMonitored[sn.Number]; !ok {
			plan.NewSeasons = append(plan.NewSeasons, sn.Number)
		}
		for _, pe := range sn.Episodes {
			if id := providerID(pe, source); id > 0 {
				if ex, ok := byPID[id]; ok && !claimed[ex.ID] {
					claimed[ex.ID] = true
					u := EpisodeUpdate{ID: ex.ID, Episode: pe}
					if ex.Season != pe.Season || ex.Number != pe.Number {
						u.Renumbered, u.FromSeason, u.FromNumber = true, ex.Season, ex.Number
					}
					plan.Update = append(plan.Update, u)
					continue
				}
			}
			rest = append(rest, pe)
		}
	}
	for _, pe := range rest {
		if ex, ok := byNum[[2]int{pe.Season, pe.Number}]; ok && !claimed[ex.ID] {
			claimed[ex.ID] = true
			plan.Update = append(plan.Update, EpisodeUpdate{ID: ex.ID, Episode: pe})
			continue
		}
		mon, ok := seasonMonitored[pe.Season]
		if !ok {
			mon = monitorNew
		}
		plan.Insert = append(plan.Insert, NewEpisode{Episode: pe, Monitored: mon && pe.Season != 0})
	}
	for i := range existing {
		e := &existing[i]
		if claimed[e.ID] {
			continue
		}
		if e.FileID != nil {
			plan.Keep = append(plan.Keep, e.ID)
		} else {
			plan.Delete = append(plan.Delete, e.ID)
		}
	}
	return plan
}
