package music

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/decision"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/wanted"
)

// WantedSource plugs albums into the wanted orchestration: an album is
// wanted while it is monitored (with its artist), released, and missing
// tracks or below the quality profile's cutoff.
type WantedSource struct{ S *Service }

var _ wanted.Source = WantedSource{}

// Kind implements wanted.Source.
func (WantedSource) Kind() string { return library.RefAlbum }

func (w WantedSource) toItem(st *AlbumState, a *Artist) wanted.Item {
	it := wanted.Item{Ref: st.Ref(), Title: a.Name + " - " + st.Title, Monitored: st.Monitored && a.Monitored, Available: st.Available,
		HasFile: st.FileCount > 0, CutoffMet: st.CutoffMet, ProfileID: a.QualityProfileID, MediaKind: downloads.KindMusic}
	runtime := 0
	if st.Release != nil && st.Release.TracksLoaded {
		if tracks, err := w.S.Tracks(context.Background(), st.Release.ID); err == nil {
			ms := 0
			for _, t := range tracks {
				ms += t.LengthMs
			}
			runtime = ms / 60000
		}
	}
	alts := append([]string{}, a.Aliases...)
	it.Wanted = decision.Wanted{Kind: quality.Music, Title: st.Title, AlternateTitles: alts, Year: st.Year(), Artist: a.Name, Album: st.Title,
		IDs: decision.IDs{MusicBrainz: st.MBID}, RuntimeMinutes: runtime, Tags: a.Tags}
	// An incomplete album takes any acceptable release; a complete one only upgrades.
	if st.Complete() && st.Quality != "" {
		it.Wanted.Current = &decision.Current{Quality: st.Quality, Revision: parser.Revision{Version: 1}}
	}
	it.Query = indexers.Query{Type: indexers.SearchMusic, Artist: a.Name, Album: st.Title, Term: naming.CleanTitle(a.Name + " " + st.Title)}
	return it
}

// Item implements wanted.Source.
func (w WantedSource) Item(ctx context.Context, id int64) (wanted.Item, error) {
	al, err := w.S.Album(ctx, id)
	if err != nil {
		return wanted.Item{}, err
	}
	a, err := w.S.Artist(ctx, al.ArtistID)
	if err != nil {
		return wanted.Item{}, err
	}
	st, err := w.S.State(ctx, al, &a, nil)
	if err != nil {
		return wanted.Item{}, err
	}
	return w.toItem(&st, &a), nil
}

// Wanted implements wanted.Source.
func (w WantedSource) Wanted(ctx context.Context) ([]wanted.Item, error) {
	list, err := w.S.WantedAlbums(ctx, false)
	if err != nil {
		return nil, err
	}
	out := make([]wanted.Item, 0, len(list))
	cache := map[int64]*Artist{}
	for i := range list {
		a := cache[list[i].ArtistID]
		if a == nil {
			ar, err := w.S.Artist(ctx, list[i].ArtistID)
			if err != nil {
				continue
			}
			a = &ar
			cache[a.ID] = a
		}
		out = append(out, w.toItem(&list[i], a))
	}
	return out, nil
}

// WantedAlbums lists monitored, released albums of monitored artists that
// are missing tracks (cutoffUnmet=false) or complete but below the cutoff.
func (s *Service) WantedAlbums(ctx context.Context, cutoffUnmet bool) ([]AlbumState, error) {
	artists, err := s.artists(ctx, `AND i.monitored = 1 ORDER BY i.sort_title`)
	if err != nil {
		return nil, err
	}
	today := time.Now().UTC().Format("2006-01-02")
	out := []AlbumState{}
	for i := range artists {
		a := &artists[i]
		p, err := s.Quality.Profile(ctx, a.QualityProfileID)
		if err != nil {
			return nil, err
		}
		p.Normalize()
		albums, err := s.albums(ctx, `WHERE artist_id = ? AND monitored = 1 AND release_date <> '' AND release_date <= ? ORDER BY release_date DESC`, a.ID, today)
		if err != nil {
			return nil, err
		}
		for _, al := range albums {
			st, err := s.State(ctx, al, a, &p)
			if err != nil {
				return nil, err
			}
			if !st.Available {
				continue
			}
			switch {
			case cutoffUnmet && st.Complete() && !st.CutoffMet:
				out = append(out, st)
			case !cutoffUnmet && !st.Complete():
				out = append(out, st)
			}
		}
	}
	return out, nil
}

// normName compares names: letters and digits only, lower case, accents dropped.
func normName(s string) string { return strings.ReplaceAll(normText(s, true), " ", "") }

// Matcher implements wanted.Source: an RSS release matches a monitored
// album when its artist (or an alias) and album title match.
func (w WantedSource) Matcher(ctx context.Context) (wanted.Matcher, error) {
	rows, err := w.S.DB.R.QueryContext(ctx, `SELECT al.id, al.title, al.release_date, i.title, a.aliases FROM albums al
		JOIN media_items i ON i.id = al.artist_id JOIN artists a ON a.item_id = i.id
		WHERE al.monitored = 1 AND i.monitored = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type entry struct {
		id   int64
		year int
	}
	byKey := map[string][]entry{}
	for rows.Next() {
		var (
			id                  int64
			title, date, artist string
			aliases             string
		)
		if err := rows.Scan(&id, &title, &date, &artist, &aliases); err != nil {
			return nil, err
		}
		for _, name := range append([]string{artist}, jsonList(aliases)...) {
			k := normName(name) + "\x00" + normName(title)
			byKey[k] = append(byKey[k], entry{id, yearOf(date)})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return func(_ *indexers.Release, p *parser.Release) []library.Ref {
		artist, album := "", ""
		if p.Music != nil {
			artist, album = p.Music.Artist, p.Music.Album
		}
		if artist == "" || album == "" {
			// "Artist - Album" titles the parser did not see as music.
			if a, b, ok := strings.Cut(p.Title, " - "); ok {
				artist, album = a, b
			}
		}
		if artist == "" || album == "" {
			return nil
		}
		var out []library.Ref
		seen := map[int64]bool{}
		for _, e := range byKey[normName(artist)+"\x00"+normName(album)] {
			if seen[e.id] || (p.Year > 0 && e.year > 0 && (p.Year < e.year-1 || p.Year > e.year+1) && p.Year != e.year) {
				continue
			}
			seen[e.id] = true
			out = append(out, library.Ref{Kind: library.RefAlbum, ID: e.id})
		}
		return out
	}, nil
}

// SearchTerm is the free-text query for an album ("Artist Album").
func SearchTerm(artist, album string, year int) string {
	t := naming.CleanTitle(artist + " " + album)
	if year > 0 {
		t += " " + strconv.Itoa(year)
	}
	return t
}
