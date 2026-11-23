package series

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/decision"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/wanted"
)

func (s *Service) profiles(ctx context.Context) (map[int64]*quality.Profile, error) {
	list, err := s.Quality.Profiles(ctx)
	if err != nil {
		return nil, err
	}
	out := map[int64]*quality.Profile{}
	for i := range list {
		list[i].Normalize()
		out[list[i].ID] = &list[i]
	}
	return out, nil
}

// busyEpisodes returns the episodes a download under way is for.
func (s *Service) busyEpisodes(ctx context.Context) (map[int64]bool, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT r.item_ref FROM download_refs r JOIN downloads d ON d.id = r.download_id
		WHERE d.status IN ('active', 'completed') AND r.item_ref LIKE 'episode:%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		if r, err := library.ParseRef(ref); err == nil {
			out[r.ID] = true
		}
	}
	return out, rows.Err()
}

func (s *Service) packThreshold(ctx context.Context) int {
	n, _ := settings.Get(ctx, s.Settings, SeasonPackThreshold)
	return max(n, 2)
}

// PackWanted decides whether a season is searched as a whole (D-051):
// never for specials or daily shows; when at least threshold of its
// episodes are wanted, or it has finished airing with at least two wanted.
func PackWanted(seriesType string, season int, missing, unaired, threshold int) bool {
	if season == 0 || seriesType == TypeDaily || missing < 2 {
		return false
	}
	return missing >= max(threshold, 2) || unaired == 0
}

// episodeWanted reports what the episode needs: "missing", "upgrade" or "".
func episodeWanted(v *Series, sn *Season, e *Episode, p *quality.Profile, now time.Time) string {
	if !v.Monitored || !sn.Monitored || !e.Monitored || e.Removed || !e.Aired(now) {
		return ""
	}
	if e.File == nil {
		return "missing"
	}
	if !CutoffMet(p, e.File) {
		return "upgrade"
	}
	return ""
}

func (s *Service) wantedFor(v *Series, p *quality.Profile, eps []*Episode) decision.Wanted {
	alts := append([]string{}, v.AlternateTitles...)
	if v.OriginalTitle != "" && !strings.EqualFold(v.OriginalTitle, v.Title) {
		alts = append(alts, v.OriginalTitle)
	}
	w := decision.Wanted{Kind: quality.Series, Title: v.Title, AlternateTitles: alts, Year: v.Year,
		IDs: decision.IDs{TVDb: int(v.TVDbID), IMDb: v.IMDbID}, SeriesType: v.SeriesType, RuntimeMinutes: v.Runtime,
		OriginalLanguage: v.OriginalLanguage, Tags: v.Tags}
	for _, e := range eps {
		rs, rn := e.ReleaseNumbers()
		w.Episodes = append(w.Episodes, decision.Episode{Season: rs, Episode: rn, Absolute: e.ReleaseAbsolute(), AirDate: e.AirDate})
		if e.Runtime > 0 {
			w.RuntimeMinutes = e.Runtime
		}
	}
	_ = p
	return w
}

// baseQuery carries every id the series has (the indexer service sends only
// the strongest one each indexer supports: indexers combine several ids with
// "and", and few know them all) plus the title as a text fallback for
// indexers without id search.
func baseQuery(v *Series) indexers.Query {
	return indexers.Query{Type: indexers.SearchTV, Term: naming.CleanTitle(v.Title), TVDBID: v.TVDbID, TVMazeID: v.TVMazeID,
		IMDBID: v.IMDbID, TMDBID: v.TMDbID}
}

func (s *Service) episodeItem(v *Series, sn *Season, e *Episode, p *quality.Profile, busy bool, now time.Time) wanted.Item {
	it := wanted.Item{Ref: e.Ref(), Title: fmt.Sprintf("%s S%02dE%02d", v.Title, e.Season, e.Number),
		Monitored: v.Monitored && sn.Monitored && e.Monitored && !e.Removed, Available: e.Aired(now), HasFile: e.File != nil,
		CutoffMet: CutoffMet(p, e.File), ProfileID: v.QualityProfileID, MediaKind: downloads.KindSeries, Covers: []library.Ref{e.Ref()},
		Busy: busy}
	it.Wanted = s.wantedFor(v, p, []*Episode{e})
	if e.File != nil {
		it.Wanted.Current = &decision.Current{Quality: e.File.Quality, Revision: e.File.Revision, Score: e.File.Score}
	}
	q := baseQuery(v)
	switch {
	case v.SeriesType == TypeDaily && len(e.AirDate) == 10:
		q.Season, q.Episode = e.AirDate[:4], e.AirDate[5:7]+"/"+e.AirDate[8:10]
	case v.SeriesType == TypeAnime && e.ReleaseAbsolute() > 0:
		q.Term += fmt.Sprintf(" %02d", e.ReleaseAbsolute())
	default:
		rs, rn := e.ReleaseNumbers()
		q.Season, q.Episode = strconv.Itoa(rs), strconv.Itoa(rn)
	}
	it.Query = q
	return it
}

// seasonItem is a season searched as a whole for its missing episodes.
func (s *Service) seasonItem(v *Series, sn *Season, missing []*Episode, unaired int, busy bool, p *quality.Profile, threshold int) wanted.Item {
	it := wanted.Item{Ref: sn.Ref(), Title: fmt.Sprintf("%s season %d", v.Title, sn.Number), Monitored: v.Monitored && sn.Monitored,
		Available: PackWanted(v.SeriesType, sn.Number, len(missing), unaired, threshold), ProfileID: v.QualityProfileID,
		MediaKind: downloads.KindSeries, PreferPacks: true, Busy: busy && len(missing) == 0}
	for _, e := range missing {
		it.Covers = append(it.Covers, e.Ref())
	}
	it.Wanted = s.wantedFor(v, p, missing)
	q := baseQuery(v)
	seasonNo := sn.Number
	if len(missing) > 0 {
		seasonNo, _ = missing[0].ReleaseNumbers()
	}
	if v.SeriesType != TypeAnime {
		// Anime releases rarely carry seasons: the title alone finds them.
		q.Season = strconv.Itoa(seasonNo)
	}
	it.Query = q
	return it
}

// seriesOf returns the series an episode or season ref belongs to.
func (s *Service) seriesOf(ctx context.Context, ref library.Ref) (int64, error) {
	var id int64
	var err error
	switch ref.Kind {
	case library.RefEpisode:
		err = s.DB.R.QueryRowContext(ctx, `SELECT series_id FROM episodes WHERE id = ?`, ref.ID).Scan(&id)
	case library.RefSeason:
		err = s.DB.R.QueryRowContext(ctx, `SELECT series_id FROM seasons WHERE id = ?`, ref.ID).Scan(&id)
	case library.RefSeries:
		id = ref.ID
	default:
		return 0, library.ErrBadRef
	}
	if err != nil {
		return 0, ErrEpisodeNotFound
	}
	return id, nil
}

// view is a series with everything the wanted sources look at.
type view struct {
	series  Series
	profile *quality.Profile
	seasons map[int64]*Season
	eps     []Episode
}

func (s *Service) loadView(ctx context.Context, seriesID int64) (*view, error) {
	v, err := s.Get(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	p, err := s.Quality.Profile(ctx, v.QualityProfileID)
	if err != nil {
		return nil, err
	}
	p.Normalize()
	seasons, err := s.Seasons(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	eps, err := s.Episodes(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	out := &view{series: v, profile: &p, seasons: map[int64]*Season{}, eps: eps}
	for i := range seasons {
		out.seasons[seasons[i].ID] = &seasons[i]
	}
	return out, nil
}

// seasonState splits a season's episodes for the pack rule.
func (vw *view) seasonState(seasonID int64, busy map[int64]bool, now time.Time) (missing []*Episode, unaired int, anyBusy bool) {
	sn := vw.seasons[seasonID]
	for i := range vw.eps {
		e := &vw.eps[i]
		if e.SeasonID != seasonID || e.Removed {
			continue
		}
		if !e.Aired(now) {
			unaired++
		}
		if busy[e.ID] {
			anyBusy = true
			continue
		}
		if episodeWanted(&vw.series, sn, e, vw.profile, now) == "missing" {
			missing = append(missing, e)
		}
	}
	return missing, unaired, anyBusy
}

// EpisodeSource plugs episodes into the wanted orchestration.
type EpisodeSource struct{ S *Service }

// SeasonSource plugs whole seasons (season packs) into it.
type SeasonSource struct{ S *Service }

var (
	_ wanted.Source = EpisodeSource{}
	_ wanted.Multi  = EpisodeSource{}
	_ wanted.Source = SeasonSource{}
	_ wanted.Multi  = SeasonSource{}
)

// Kind implements wanted.Source.
func (EpisodeSource) Kind() string { return library.RefEpisode }

// Kind implements wanted.Source.
func (SeasonSource) Kind() string { return library.RefSeason }

// Item implements wanted.Source.
func (w EpisodeSource) Item(ctx context.Context, id int64) (wanted.Item, error) {
	e, err := w.S.Episode(ctx, id)
	if err != nil {
		return wanted.Item{}, err
	}
	vw, err := w.S.loadView(ctx, e.SeriesID)
	if err != nil {
		return wanted.Item{}, err
	}
	sn := vw.seasons[e.SeasonID]
	if sn == nil {
		return wanted.Item{}, ErrSeasonNotFound
	}
	return w.S.episodeItem(&vw.series, sn, &e, vw.profile, false, w.S.now()), nil
}

// Item implements wanted.Source.
func (w SeasonSource) Item(ctx context.Context, id int64) (wanted.Item, error) {
	sn, err := w.S.SeasonByID(ctx, id)
	if err != nil {
		return wanted.Item{}, err
	}
	vw, err := w.S.loadView(ctx, sn.SeriesID)
	if err != nil {
		return wanted.Item{}, err
	}
	busy, err := w.S.busyEpisodes(ctx)
	if err != nil {
		return wanted.Item{}, err
	}
	now := w.S.now()
	missing, unaired, anyBusy := vw.seasonState(sn.ID, busy, now)
	return w.S.seasonItem(&vw.series, vw.seasons[sn.ID], missing, unaired, anyBusy, vw.profile, w.S.packThreshold(ctx)), nil
}

// wantedAll lists what the scheduled search should look at: whole seasons
// that meet the pack rule, and every other wanted episode (missing or below
// the cutoff).
func (s *Service) wantedAll(ctx context.Context) ([]wanted.Item, []wanted.Item, error) {
	now := s.now()
	rows, err := s.DB.R.QueryContext(ctx, `SELECT DISTINCT e.series_id FROM episodes e JOIN media_items i ON i.id = e.series_id
		JOIN seasons sn ON sn.id = e.season_id
		WHERE i.monitored = 1 AND sn.monitored = 1 AND e.monitored = 1 AND e.removed = 0 AND e.air_date_utc <= ?`, db.Millis(now))
	if err != nil {
		return nil, nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	busy, err := s.busyEpisodes(ctx)
	if err != nil {
		return nil, nil, err
	}
	threshold := s.packThreshold(ctx)
	var seasonsOut, episodesOut []wanted.Item
	for _, id := range ids {
		vw, err := s.loadView(ctx, id)
		if err != nil {
			continue
		}
		packs := map[int64]bool{}
		for sid, sn := range vw.seasons {
			missing, unaired, anyBusy := vw.seasonState(sid, busy, now)
			if PackWanted(vw.series.SeriesType, sn.Number, len(missing), unaired, threshold) {
				packs[sid] = true
				seasonsOut = append(seasonsOut, s.seasonItem(&vw.series, sn, missing, unaired, anyBusy, vw.profile, threshold))
			}
		}
		for i := range vw.eps {
			e := &vw.eps[i]
			sn := vw.seasons[e.SeasonID]
			if sn == nil || busy[e.ID] {
				continue
			}
			kind := episodeWanted(&vw.series, sn, e, vw.profile, now)
			if kind == "" || (kind == "missing" && packs[e.SeasonID]) {
				continue
			}
			episodesOut = append(episodesOut, s.episodeItem(&vw.series, sn, e, vw.profile, false, now))
		}
	}
	return seasonsOut, episodesOut, nil
}

// Wanted implements wanted.Source.
func (w EpisodeSource) Wanted(ctx context.Context) ([]wanted.Item, error) {
	_, eps, err := w.S.wantedAll(ctx)
	return eps, err
}

// Wanted implements wanted.Source.
func (w SeasonSource) Wanted(ctx context.Context) ([]wanted.Item, error) {
	seasons, _, err := w.S.wantedAll(ctx)
	return seasons, err
}

// SearchTargets are the refs to search for a series now: its pack-wanted
// seasons and every other wanted episode.
func (s *Service) SearchTargets(ctx context.Context, seriesID int64) ([]library.Ref, error) {
	vw, err := s.loadView(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	busy, err := s.busyEpisodes(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	threshold := s.packThreshold(ctx)
	var out []library.Ref
	packs := map[int64]bool{}
	for sid, sn := range vw.seasons {
		missing, unaired, _ := vw.seasonState(sid, busy, now)
		if PackWanted(vw.series.SeriesType, sn.Number, len(missing), unaired, threshold) {
			packs[sid] = true
			out = append(out, sn.Ref())
		}
	}
	for i := range vw.eps {
		e := &vw.eps[i]
		sn := vw.seasons[e.SeasonID]
		if sn == nil || busy[e.ID] {
			continue
		}
		kind := episodeWanted(&vw.series, sn, e, vw.profile, now)
		if kind == "" || (kind == "missing" && packs[e.SeasonID]) {
			continue
		}
		out = append(out, e.Ref())
	}
	return out, nil
}

func normTitle(t string) string {
	return strings.ReplaceAll(strings.ToLower(naming.CleanTitle(strings.ReplaceAll(t, "'", ""))), " ", "")
}

func looksLikeEpisode(p *parser.Release) bool {
	return p.Kind == parser.KindSeries || len(p.Seasons) > 0 || len(p.Episodes) > 0 || len(p.AbsoluteEpisodes) > 0 || p.AirDate != "" ||
		p.CompleteSeries
}

// Matcher implements wanted.Source: a release matches a monitored series by
// TVDB, TMDb or IMDb id, else by title or alternate title (and the year when
// the name has one); then its episodes by numbering. Season packs map to the
// season, everything else to the episodes.
func (w EpisodeSource) Matcher(ctx context.Context) (wanted.Matcher, error) {
	rows, err := w.S.DB.R.QueryContext(ctx, `SELECT i.id, i.title, i.year, s.tvdb_id, s.tmdb_id, s.imdb_id, s.original_title, s.alternate_titles
		FROM media_items i JOIN series s ON s.item_id = i.id WHERE i.kind = 'series' AND i.monitored = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type entry struct {
		id   int64
		year int
	}
	byTVDB, byTMDb := map[int64]int64{}, map[int64]int64{}
	byIMDb := map[string]int64{}
	byTitle := map[string][]entry{}
	for rows.Next() {
		var (
			id, tvdb, tmdb    int64
			year              int
			title, imdb, orig string
			alts              string
		)
		if err := rows.Scan(&id, &title, &year, &tvdb, &tmdb, &imdb, &orig, &alts); err != nil {
			return nil, err
		}
		if tvdb > 0 {
			byTVDB[tvdb] = id
		}
		if tmdb > 0 {
			byTMDb[tmdb] = id
		}
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
	views := map[int64]*view{}
	return func(r *indexers.Release, p *parser.Release) []library.Ref {
		if !looksLikeEpisode(p) || p.Kind == parser.KindMusic {
			return nil
		}
		var sid int64
		switch {
		case r.TVDBID > 0 && byTVDB[r.TVDBID] > 0:
			sid = byTVDB[r.TVDBID]
		case r.IMDBID != "" && byIMDb[strings.ToLower(r.IMDBID)] > 0:
			sid = byIMDb[strings.ToLower(r.IMDBID)]
		default:
			for _, t := range append([]string{p.Title}, p.AlternateTitles...) {
				for _, e := range byTitle[normTitle(t)] {
					if p.Year > 0 && e.year > 0 && (p.Year < e.year-1 || p.Year > e.year+1) {
						continue
					}
					sid = e.id
					break
				}
				if sid > 0 {
					break
				}
			}
		}
		if sid == 0 {
			return nil
		}
		vw, ok := views[sid]
		if !ok {
			vw, _ = w.S.loadView(ctx, sid)
			views[sid] = vw
		}
		if vw == nil {
			return nil
		}
		var out []library.Ref
		if IsPack(p) {
			want := map[int]bool{}
			for _, n := range p.Seasons {
				want[n] = true
			}
			for _, sn := range vw.seasons {
				if want[sn.Number] {
					out = append(out, sn.Ref())
				}
			}
			return out
		}
		for _, e := range MatchRelease(vw.series.SeriesType, vw.eps, p) {
			out = append(out, e.Ref())
		}
		return out
	}, nil
}

// Matcher implements wanted.Source (the episode source matches for both).
func (SeasonSource) Matcher(context.Context) (wanted.Matcher, error) {
	return func(*indexers.Release, *parser.Release) []library.Ref { return nil }, nil
}

func refsOf(eps []*Episode) []library.Ref {
	out := make([]library.Ref, 0, len(eps))
	for _, e := range eps {
		out = append(out, e.Ref())
	}
	return out
}

func (s *Service) itemEpisodes(ctx context.Context, it wanted.Item) (*view, error) {
	sid, err := s.seriesOf(ctx, it.Ref)
	if err != nil {
		return nil, err
	}
	return s.loadView(ctx, sid)
}

// Covers implements wanted.Multi for an episode search: the release must
// hold this episode and must not be a season pack.
func (w EpisodeSource) Covers(ctx context.Context, it wanted.Item, d *decision.Decision) (library.Ref, []library.Ref) {
	if IsPack(&d.Release) {
		return library.Ref{}, nil
	}
	vw, err := w.S.itemEpisodes(ctx, it)
	if err != nil {
		return library.Ref{}, nil
	}
	m := MatchRelease(vw.series.SeriesType, vw.eps, &d.Release)
	for _, e := range m {
		if e.Ref() == it.Ref {
			return it.Ref, refsOf(m)
		}
	}
	return library.Ref{}, nil
}

// Refine implements wanted.Multi: a season pack is not taken for one episode.
func (EpisodeSource) Refine(_ context.Context, _ wanted.Item, rep *decision.Report) {
	for i := range rep.Decisions {
		d := &rep.Decisions[i]
		if d.Status != decision.StatusRejected && IsPack(&d.Release) {
			d.Reject(decision.Rejection{Code: decision.CodeSeasonPack, Message: "a season pack; this search is for one episode"})
		}
	}
}

// Covers implements wanted.Multi for a season search: a pack covers the
// season, an episode release the episodes it holds.
func (w SeasonSource) Covers(ctx context.Context, it wanted.Item, d *decision.Decision) (library.Ref, []library.Ref) {
	vw, err := w.S.itemEpisodes(ctx, it)
	if err != nil {
		return library.Ref{}, nil
	}
	m := MatchRelease(vw.series.SeriesType, vw.eps, &d.Release)
	wantedSet := map[library.Ref]bool{}
	for _, c := range it.Covers {
		wantedSet[c] = true
	}
	var first *Episode
	for _, e := range m {
		if wantedSet[e.Ref()] && first == nil {
			first = e
		}
	}
	if first == nil && len(wantedSet) > 0 {
		return library.Ref{}, nil
	}
	if IsPack(&d.Release) {
		return it.Ref, refsOf(m)
	}
	if first == nil {
		if len(m) == 0 {
			return library.Ref{}, nil
		}
		first = m[0]
	}
	return first.Ref(), refsOf(m)
}

// Refine implements wanted.Multi: a release none of whose episodes is
// wanted is rejected with a reason.
func (w SeasonSource) Refine(ctx context.Context, it wanted.Item, rep *decision.Report) {
	if len(it.Covers) == 0 {
		return
	}
	vw, err := w.S.itemEpisodes(ctx, it)
	if err != nil {
		return
	}
	wantedSet := map[library.Ref]bool{}
	for _, c := range it.Covers {
		wantedSet[c] = true
	}
	for i := range rep.Decisions {
		d := &rep.Decisions[i]
		if d.Status == decision.StatusRejected {
			continue
		}
		hit := false
		for _, e := range MatchRelease(vw.series.SeriesType, vw.eps, &d.Release) {
			hit = hit || wantedSet[e.Ref()]
		}
		if !hit {
			d.Reject(decision.Rejection{Code: decision.CodeEpisodeNotWanted, Message: "none of the release's episodes is wanted"})
		}
	}
}
