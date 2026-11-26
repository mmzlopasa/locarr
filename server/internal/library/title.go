package library

import (
	"context"
	"fmt"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// Item kinds as the history groups them.
const (
	TitleKindMovie  = "movie"
	TitleKindSeries = "series"
	TitleKindMusic  = "music"
	TitleKindAdult  = "adult"
)

// Title returns the display title of the item a ref names as it is now
// ("Iron Man (2008)", "Show S01E02", "Artist - Album") and the kind it belongs
// to. The title is "" when the ref is malformed or the item no longer exists.
// Events store this as a snapshot (D-107), so they keep the title the item
// had when they happened.
func Title(ctx context.Context, q db.Querier, raw string) (title, kind string) {
	r, err := ParseRef(raw)
	if err != nil {
		return "", ""
	}
	var (
		year   int
		sn, en int
		artist string
	)
	switch r.Kind {
	case RefMovie:
		if q.QueryRowContext(ctx, `SELECT title, year FROM media_items WHERE id = ? AND kind = 'movie'`, r.ID).Scan(&title, &year) != nil {
			return "", TitleKindMovie
		}
		if year > 0 {
			title = fmt.Sprintf("%s (%d)", title, year)
		}
		return title, TitleKindMovie
	case RefSeries:
		_ = q.QueryRowContext(ctx, `SELECT title FROM media_items WHERE id = ? AND kind = 'series'`, r.ID).Scan(&title)
		return title, TitleKindSeries
	case RefSeason:
		if q.QueryRowContext(ctx, `SELECT i.title, sn.number FROM seasons sn JOIN media_items i ON i.id = sn.series_id WHERE sn.id = ?`,
			r.ID).Scan(&title, &sn) == nil {
			title = fmt.Sprintf("%s season %d", title, sn)
		}
		return title, TitleKindSeries
	case RefEpisode:
		if q.QueryRowContext(ctx, `SELECT i.title, e.season_number, e.episode_number FROM episodes e JOIN media_items i ON i.id = e.series_id
			WHERE e.id = ?`, r.ID).Scan(&title, &sn, &en) == nil {
			title = fmt.Sprintf("%s S%02dE%02d", title, sn, en)
		}
		return title, TitleKindSeries
	case RefArtist:
		_ = q.QueryRowContext(ctx, `SELECT title FROM media_items WHERE id = ? AND kind = 'artist'`, r.ID).Scan(&title)
		return title, TitleKindMusic
	case RefAlbum:
		if q.QueryRowContext(ctx, `SELECT i.title, a.title FROM albums a JOIN media_items i ON i.id = a.artist_id WHERE a.id = ?`,
			r.ID).Scan(&artist, &title) == nil {
			title = artist + " - " + title
		}
		return title, TitleKindMusic
	case RefScene, RefAdultMovie, RefAdultGallery:
		kind := "scene"
		switch r.Kind {
		case RefAdultMovie:
			kind = "movie"
		case RefAdultGallery:
			kind = "gallery"
		}
		if q.QueryRowContext(ctx, `SELECT title, year FROM adult_items WHERE id = ? AND kind = ?`, r.ID, kind).Scan(&title, &year) == nil && year > 0 {
			title = fmt.Sprintf("%s (%d)", title, year)
		}
		return title, TitleKindAdult
	}
	return "", ""
}

// DetachedSuffix marks a ref of an item that was removed before ids stopped
// being reused (D-107): migration 00017 appended it to events that happened
// before the item now holding that id was added, so they no longer show on
// that item's timeline.
const DetachedSuffix = "#removed"

// KindOfRef is the history kind of a ref string, whether or not the item
// still exists (detached refs included).
func KindOfRef(raw string) string {
	raw, _, _ = strings.Cut(raw, "#")
	r, err := ParseRef(raw)
	if err != nil {
		return ""
	}
	switch r.Kind {
	case RefMovie:
		return TitleKindMovie
	case RefSeries, RefSeason, RefEpisode:
		return TitleKindSeries
	case RefArtist, RefAlbum:
		return TitleKindMusic
	case RefScene, RefAdultMovie, RefAdultGallery:
		return TitleKindAdult
	}
	return ""
}
