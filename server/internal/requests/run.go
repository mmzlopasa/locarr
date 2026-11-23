package requests

import (
	"context"
	"time"
)

// SyncEvery is how often requests are compared with the library without a
// reason (imports and download events trigger a sync at once).
const SyncEvery = 5 * time.Minute

func (s *Service) kicks() chan struct{} {
	s.kickOnce.Do(func() { s.kick = make(chan struct{}, 1) })
	return s.kick
}

// Kick asks for a sync soon (it never blocks; kicks during a sync make one
// more run).
func (s *Service) Kick() {
	select {
	case s.kicks() <- struct{}{}:
	default:
	}
}

// Run syncs after every kick and every SyncEvery until ctx ends.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(SyncEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.kicks():
		case <-t.C:
		}
		if _, err := s.Sync(ctx); err != nil && ctx.Err() == nil {
			s.log().Warn("sync requests", "err", err)
		}
	}
}
