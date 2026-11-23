// Package explain answers "why isn't it here?" for any library item: it
// gathers the item's state from its library and the facts from the timeline
// and runs timeline.Explain (D-030). The API's timeline endpoints and the
// requests (P8) use it.
package explain

import (
	"context"
	"fmt"

	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/movies"
	"github.com/mmzlopasa/locarr/server/internal/library/music"
	"github.com/mmzlopasa/locarr/server/internal/library/series"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// Explainer explains library items.
type Explainer struct {
	Movies   *movies.Service
	Series   *series.Service
	Music    *music.Service
	Timeline *timeline.Store
}

// Explain returns the explanation for ref, the item's title and the refs
// whose history belongs to it (an episode with its season).
func (x *Explainer) Explain(ctx context.Context, ref library.Ref) (timeline.Explanation, string, []string, error) {
	switch ref.Kind {
	case library.RefAlbum, library.RefArtist:
		ex, title, err := x.music(ctx, ref)
		return ex, title, []string{ref.String()}, err
	case library.RefMovie:
		if x.Movies == nil {
			break
		}
		it, err := x.Movies.Item(ctx, ref.ID)
		if err != nil {
			return timeline.Explanation{}, "", nil, err
		}
		facts, err := x.Timeline.Facts(ctx, ref.String(), "movies")
		if err != nil {
			return timeline.Explanation{}, "", nil, err
		}
		st := timeline.ItemState{Ref: ref.String(), Monitored: it.Monitored, Available: it.Available, AvailableFrom: it.AvailableFrom,
			HasFile: it.File != nil, CutoffMet: it.CutoffMet}
		return timeline.Explain(st, facts), it.Title, []string{ref.String()}, nil
	case library.RefEpisode, library.RefSeason, library.RefSeries:
		if x.Series == nil {
			break
		}
		es, err := x.Series.Explain(ctx, ref)
		if err != nil {
			return timeline.Explanation{}, "", nil, err
		}
		facts, err := x.Timeline.FactsRefs(ctx, es.Refs, "tv")
		if err != nil {
			return timeline.Explanation{}, "", nil, err
		}
		return timeline.Explain(es.State, facts), es.Title, es.Refs, nil
	}
	return timeline.Explanation{}, "", nil, fmt.Errorf("%w: this kind has no timeline yet", library.ErrBadRef)
}

// music answers for an album (and sums up an artist).
func (x *Explainer) music(ctx context.Context, ref library.Ref) (timeline.Explanation, string, error) {
	if x.Music == nil {
		return timeline.Explanation{}, "", fmt.Errorf("%w: the music library is not running", library.ErrBadRef)
	}
	if ref.Kind == library.RefArtist {
		a, err := x.Music.Artist(ctx, ref.ID)
		if err != nil {
			return timeline.Explanation{}, "", err
		}
		list, err := x.Music.AlbumStates(ctx, &a)
		if err != nil {
			return timeline.Explanation{}, "", err
		}
		counts := map[string]any{"albums": len(list)}
		for _, st := range list {
			k := st.Status()
			n, _ := counts[k].(int)
			counts[k] = n + 1
		}
		return timeline.Explanation{Code: "artist", Tone: "info", Message: fmt.Sprintf("%d albums", len(list)), Params: counts,
			Reasons: []timeline.ReasonCount{}}, a.Name, nil
	}
	al, err := x.Music.Album(ctx, ref.ID)
	if err != nil {
		return timeline.Explanation{}, "", err
	}
	st, err := x.Music.State(ctx, al, nil, nil)
	if err != nil {
		return timeline.Explanation{}, "", err
	}
	facts, err := x.Timeline.Facts(ctx, ref.String(), "music")
	if err != nil {
		return timeline.Explanation{}, "", err
	}
	is := timeline.ItemState{Ref: ref.String(), Monitored: st.Monitored && st.ArtistMonitored, Available: st.Available,
		HasFile: st.FileCount > 0, CutoffMet: st.CutoffMet}
	if t := music.ReleaseTime(al.ReleaseDate); !t.IsZero() {
		is.AvailableFrom = &t
	}
	ex := timeline.Explain(is, facts)
	if st.FileCount > 0 && !st.Complete() && ex.Code == timeline.ExplainUpgradeWanted {
		ex.Params["files"], ex.Params["tracks"] = st.FileCount, st.TrackCount
		ex.Message = fmt.Sprintf("%d of %d tracks are in the library; Locarr keeps looking for the whole album", st.FileCount, st.TrackCount)
	}
	return ex, st.ArtistName + " - " + al.Title, nil
}
