package adult

import (
	"context"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// Explain answers "why isn't it here?" for a scene or movie with the shared
// rules (D-030) and returns its timeline. Only the adult endpoints call it;
// the generic timeline endpoints refuse adult refs.
func (s *Service) Explain(ctx context.Context, id int64, limit int) (Item, timeline.Explanation, []timeline.Event, error) {
	it, err := s.Item(ctx, id)
	if err != nil {
		return it, timeline.Explanation{}, nil, err
	}
	ref := it.Ref().String()
	facts, err := s.Timeline.Facts(ctx, ref, "movies")
	if err != nil {
		return it, timeline.Explanation{}, nil, err
	}
	p, err := s.Quality.Profile(ctx, it.QualityProfileID)
	if err != nil {
		return it, timeline.Explanation{}, nil, err
	}
	p.Normalize()
	today := s.now().UTC().Format("2006-01-02")
	st := timeline.ItemState{Ref: ref, Monitored: it.Monitored, Available: it.ReleaseDate == "" || it.ReleaseDate <= today,
		HasFile: it.File != nil, CutoffMet: cutoffMet(&p, it.File)}
	if t, err := time.Parse("2006-01-02", it.ReleaseDate); err == nil {
		st.AvailableFrom = &t
	}
	events, err := s.Timeline.List(ctx, ref, limit)
	if err != nil {
		return it, timeline.Explanation{}, nil, err
	}
	return it, timeline.Explain(st, facts), events, nil
}
