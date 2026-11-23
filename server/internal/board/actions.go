package board

import (
	"context"
	"errors"
	"fmt"

	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/movies"
	"github.com/mmzlopasa/locarr/server/internal/library/music"
	"github.com/mmzlopasa/locarr/server/internal/library/series"
)

// Bulk actions.
const (
	ActionSearch         = "search"
	ActionUnmonitor      = "unmonitor"
	ActionChangeProfile  = "change_profile"
	ActionBlocklistRetry = "blocklist_retry"
	ActionRetryImport    = "retry_import"
)

// Actions are what the board does through other services (set by the app).
type Actions struct {
	// Search searches a movie, episode or album now.
	Search func(ctx context.Context, ref library.Ref) error
	// SearchSubtitles searches the subtitles of a movie or episode now.
	SearchSubtitles func(ctx context.Context, ref library.Ref) error
	// RemoveAndBlock removes a download from its client (keeping its data)
	// and blocks the release.
	RemoveAndBlock func(ctx context.Context, downloadID int64, message string) error
	// RetryImport imports a finished download again, or polls the clients
	// when its files were not visible yet.
	RetryImport func(ctx context.Context, downloadID int64) error
}

// Errors of single items in a bulk action.
var (
	ErrNotSupported = errors.New("this action does not apply to this item")
	ErrNoDownload   = errors.New("the item has no download to act on")
)

// Failure is one item an action could not be applied to.
type Failure struct {
	Key  string
	Err  error
	Code string
}

// Apply runs action on the items behind keys. profileID is the new quality
// profile of change_profile.
func (s *Service) Apply(ctx context.Context, a Actions, action string, keys []string, profileID int64) (int, []Failure) {
	items, err := s.Get(ctx, keys)
	if err != nil {
		return 0, []Failure{{Key: "", Err: err, Code: "internal"}}
	}
	byKey := map[string]Item{}
	for _, it := range items {
		byKey[it.Key] = it
	}
	done := 0
	failed := []Failure{}
	seenSeries, seenArtists := map[int64]bool{}, map[int64]bool{}
	var touched []string
	for _, k := range keys {
		it, ok := byKey[k]
		if !ok {
			failed = append(failed, Failure{Key: k, Err: ErrNotOnBoard, Code: "not_found"})
			continue
		}
		ref, err := RefOf(it.Key)
		if err == nil {
			err = s.apply(ctx, a, action, it, ref, profileID, seenSeries, seenArtists)
		}
		if err != nil {
			code := "failed"
			switch {
			case errors.Is(err, ErrNotSupported):
				code = "not_supported"
			case errors.Is(err, ErrNoDownload):
				code = "no_download"
			}
			failed = append(failed, Failure{Key: k, Err: err, Code: code})
			continue
		}
		done++
		touched = append(touched, it.ItemRef)
		if it.ParentRef != "" && action == ActionChangeProfile {
			touched = append(touched, it.ParentRef)
		}
	}
	s.Mark(touched...)
	return done, failed
}

func (s *Service) apply(ctx context.Context, a Actions, action string, it Item, ref library.Ref, profileID int64,
	seenSeries, seenArtists map[int64]bool) error {
	sub := it.Kind == KindSubtitle
	switch action {
	case ActionSearch:
		if sub {
			if a.SearchSubtitles == nil {
				return ErrNotSupported
			}
			return a.SearchSubtitles(ctx, ref)
		}
		if a.Search == nil {
			return ErrNotSupported
		}
		return a.Search(ctx, ref)
	case ActionUnmonitor:
		switch it.Kind {
		case KindMovie:
			off := false
			_, err := s.Movies.Edit(ctx, ref.ID, movies.Update{Monitored: &off})
			return err
		case KindEpisode:
			_, err := s.Series.SetEpisodesMonitored(ctx, []int64{ref.ID}, false)
			return err
		case KindAlbum:
			off := false
			_, err := s.Music.EditAlbum(ctx, ref.ID, music.AlbumUpdate{Monitored: &off})
			return err
		}
		return ErrNotSupported
	case ActionChangeProfile:
		if profileID <= 0 {
			return fmt.Errorf("%w: pick a quality profile", ErrNotSupported)
		}
		switch it.Kind {
		case KindMovie:
			_, err := s.Movies.Edit(ctx, ref.ID, movies.Update{QualityProfileID: &profileID})
			return err
		case KindEpisode:
			p, err := library.ParseRef(it.ParentRef)
			if err != nil {
				return err
			}
			if seenSeries[p.ID] {
				return nil
			}
			seenSeries[p.ID] = true
			_, err = s.Series.Edit(ctx, p.ID, series.Update{QualityProfileID: &profileID})
			return err
		case KindAlbum:
			p, err := library.ParseRef(it.ParentRef)
			if err != nil {
				return err
			}
			if seenArtists[p.ID] {
				return nil
			}
			seenArtists[p.ID] = true
			_, err = s.Music.EditArtist(ctx, p.ID, music.ArtistUpdate{QualityProfileID: &profileID})
			return err
		}
		return ErrNotSupported
	case ActionBlocklistRetry:
		if sub || a.RemoveAndBlock == nil {
			return ErrNotSupported
		}
		if it.DownloadID == nil {
			return ErrNoDownload
		}
		if err := a.RemoveAndBlock(ctx, *it.DownloadID, "blocked from the board"); err != nil {
			return err
		}
		if a.Search != nil {
			return a.Search(ctx, ref)
		}
		return nil
	case ActionRetryImport:
		if sub || a.RetryImport == nil {
			return ErrNotSupported
		}
		if it.DownloadID == nil {
			return ErrNoDownload
		}
		return a.RetryImport(ctx, *it.DownloadID)
	}
	return ErrNotSupported
}
