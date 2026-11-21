package movies

import (
	"context"
	"strconv"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/decision"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/wanted"
)

// WantedSource plugs movies into the wanted orchestration.
type WantedSource struct{ S *Service }

var _ wanted.Source = WantedSource{}

// Kind implements wanted.Source.
func (WantedSource) Kind() string { return library.RefMovie }

func (w WantedSource) toItem(m *Movie, p *quality.Profile, avail bool) wanted.Item {
	it := wanted.Item{Ref: m.Ref(), Title: m.Title, Monitored: m.Monitored, Available: avail, HasFile: m.File != nil,
		CutoffMet: CutoffMet(p, m.File), ProfileID: m.QualityProfileID, MediaKind: downloads.KindMovies}
	alts := append([]string{}, m.AlternateTitles...)
	if m.OriginalTitle != "" && !strings.EqualFold(m.OriginalTitle, m.Title) {
		alts = append(alts, m.OriginalTitle)
	}
	it.Wanted = decision.Wanted{Kind: quality.Movies, Title: m.Title, AlternateTitles: alts, Year: m.Year,
		IDs: decision.IDs{TMDb: int(m.TMDbID), IMDb: m.IMDbID}, RuntimeMinutes: m.Runtime, OriginalLanguage: m.OriginalLanguage, Tags: m.Tags}
	if m.File != nil {
		it.Wanted.Current = &decision.Current{Quality: m.File.Quality, Revision: m.File.Revision, Score: m.File.Score}
	}
	term := naming.CleanTitle(m.Title)
	if m.Year > 0 {
		term += " " + strconv.Itoa(m.Year)
	}
	it.Query = indexers.Query{Type: indexers.SearchMovie, Term: term, IMDBID: m.IMDbID, TMDBID: m.TMDbID}
	return it
}

// Item implements wanted.Source.
func (w WantedSource) Item(ctx context.Context, id int64) (wanted.Item, error) {
	m, err := w.S.Get(ctx, id)
	if err != nil {
		return wanted.Item{}, err
	}
	p, err := w.S.Quality.Profile(ctx, m.QualityProfileID)
	if err != nil {
		return wanted.Item{}, err
	}
	p.Normalize()
	avail, _ := w.S.Available(ctx, &m)
	return w.toItem(&m, &p, avail), nil
}

// Wanted implements wanted.Source.
func (w WantedSource) Wanted(ctx context.Context) ([]wanted.Item, error) {
	all, err := w.S.query(ctx, `SELECT `+movieColumns+movieFrom+` WHERE i.kind = 'movie' AND i.monitored = 1 ORDER BY i.added_at`)
	if err != nil {
		return nil, err
	}
	profiles, err := w.S.profiles(ctx)
	if err != nil {
		return nil, err
	}
	delay := w.S.availabilityDelay(ctx)
	out := []wanted.Item{}
	for i := range all {
		m := &all[i]
		avail, _ := w.S.availableWith(m, delay)
		if !avail {
			continue
		}
		p := profiles[m.QualityProfileID]
		if m.File != nil && CutoffMet(p, m.File) {
			continue
		}
		out = append(out, w.toItem(m, p, avail))
	}
	return out, nil
}

// normTitle compares titles: lower case, letters and digits only.
func normTitle(s string) string {
	return strings.ReplaceAll(strings.ToLower(naming.CleanTitle(strings.ReplaceAll(s, "'", ""))), " ", "")
}

// Matcher implements wanted.Source: a release matches a monitored movie by
// TMDb or IMDb id, or by title (or alternate title) and year (±1).
func (w WantedSource) Matcher(ctx context.Context) (wanted.Matcher, error) {
	rows, err := w.S.DB.R.QueryContext(ctx, `SELECT i.id, i.title, i.year, m.tmdb_id, m.imdb_id, m.original_title, m.alternate_titles
		FROM media_items i JOIN movies m ON m.item_id = i.id WHERE i.kind = 'movie' AND i.monitored = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type entry struct {
		id   int64
		year int
	}
	byTMDb := map[int64]int64{}
	byIMDb := map[string]int64{}
	byTitle := map[string][]entry{}
	for rows.Next() {
		var (
			id, tmdb          int64
			year              int
			title, imdb, orig string
			alts              string
		)
		if err := rows.Scan(&id, &title, &year, &tmdb, &imdb, &orig, &alts); err != nil {
			return nil, err
		}
		byTMDb[tmdb] = id
		if imdb != "" {
			byIMDb[strings.ToLower(imdb)] = id
		}
		for _, t := range append([]string{title, orig}, jsonList(alts)...) {
			if k := normTitle(t); k != "" {
				byTitle[k] = append(byTitle[k], entry{id, year})
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return func(r *indexers.Release, p *parser.Release) []library.Ref {
		if p.Kind == parser.KindSeries || p.Kind == parser.KindMusic {
			return nil
		}
		if id, ok := byTMDb[r.TMDBID]; ok && r.TMDBID > 0 {
			return []library.Ref{library.MovieRef(id)}
		}
		if id, ok := byIMDb[strings.ToLower(r.IMDBID)]; ok && r.IMDBID != "" {
			return []library.Ref{library.MovieRef(id)}
		}
		seen := map[int64]bool{}
		var out []library.Ref
		for _, t := range append([]string{p.Title}, p.AlternateTitles...) {
			for _, e := range byTitle[normTitle(t)] {
				if seen[e.id] || (p.Year > 0 && e.year > 0 && (p.Year < e.year-1 || p.Year > e.year+1)) {
					continue
				}
				seen[e.id] = true
				out = append(out, library.MovieRef(e.id))
			}
		}
		return out
	}, nil
}
