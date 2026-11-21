package series

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// ListFilter selects and orders series.
type ListFilter struct {
	Monitored *bool
	// Status: continuing, ended or upcoming.
	Status string
	// Missing: only series with missing (monitored, aired) episodes.
	Missing   bool
	ProfileID int64
	Tag       string
	Search    string
	Sort      string // title, year, added, network, nextAiring, previousAiring, missing, size, rating
	Desc      bool
	Offset    int
	Limit     int
}

// Stats summarise a series' (or a season's) episodes.
type Stats struct {
	Seasons           int
	Episodes          int // outside specials, not removed
	EpisodeFiles      int
	MonitoredEpisodes int
	Missing           int // monitored, aired, no file
	SizeOnDisk        int64
	NextAiring        *time.Time
	PreviousAiring    *time.Time
}

// ListItem is a series with what the library list shows about it.
type ListItem struct {
	Series
	Stats
	Downloading int
}

var sortColumns = map[string]string{"title": "i.sort_title", "year": "i.year", "added": "i.added_at", "network": "lower(s.network)",
	"rating": "s.rating", "nextAiring": "COALESCE(agg.next_air, 9e15)", "previousAiring": "COALESCE(agg.prev_air, 0)",
	"missing": "COALESCE(agg.missing, 0)", "size": "COALESCE(sz.size, 0)"}

// Sorting by episode facts joins them aggregated once (not per row).
const episodeAgg = ` LEFT JOIN (SELECT e.series_id, MIN(CASE WHEN e.air_date_utc > :now THEN e.air_date_utc END) AS next_air,
	MAX(CASE WHEN e.air_date_utc <= :now THEN e.air_date_utc END) AS prev_air,
	SUM(CASE WHEN e.monitored = 1 AND e.removed = 0 AND e.file_id IS NULL AND e.air_date_utc <= :now THEN 1 ELSE 0 END) AS missing
	FROM episodes e GROUP BY e.series_id) agg ON agg.series_id = i.id`

const sizeAgg = ` LEFT JOIN (SELECT item_id, SUM(size) AS size FROM media_files GROUP BY item_id) sz ON sz.item_id = i.id`

// missingIn keeps series with a monitored, aired episode without a file (one pass).
// (A monitored episode always has a monitored season: toggling a season
// sets its episodes, monitoring an episode monitors its season.)
const missingIn = `i.id IN (SELECT e.series_id FROM episodes e WHERE e.monitored = 1 AND e.removed = 0 AND e.file_id IS NULL
	AND e.air_date_utc <= :now)`

// List returns one page of series and the number that match.
func (s *Service) List(ctx context.Context, f ListFilter) ([]ListItem, int, error) {
	now := db.Millis(s.now())
	where := []string{"i.kind = 'series'"}
	var args []any
	if f.Monitored != nil {
		where, args = append(where, "i.monitored = ?"), append(args, *f.Monitored)
	}
	if f.Status != "" {
		where, args = append(where, "s.status = ?"), append(args, f.Status)
	}
	if f.ProfileID > 0 {
		where, args = append(where, "i.quality_profile_id = ?"), append(args, f.ProfileID)
	}
	if t := strings.ToLower(strings.TrimSpace(f.Tag)); t != "" {
		where, args = append(where, "EXISTS (SELECT 1 FROM json_each(i.tags) WHERE value = ?)"), append(args, t)
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(q)) + "%"
		where = append(where, `(lower(i.title) LIKE ? ESCAPE '\' OR lower(s.original_title) LIKE ? ESCAPE '\' OR lower(s.network) LIKE ? ESCAPE '\'
			OR s.imdb_id = ? OR CAST(s.tvdb_id AS TEXT) = ? OR CAST(s.tmdb_id AS TEXT) = ?)`)
		args = append(args, like, like, like, q, q, q)
	}
	if f.Missing {
		where = append(where, "i.monitored = 1", missingIn)
	}
	col, ok := sortColumns[f.Sort]
	if !ok {
		col = "i.sort_title"
	}
	dir := "ASC"
	if f.Desc {
		dir = "DESC"
	}
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	fill := func(q string) string { return strings.ReplaceAll(q, ":now", fmt.Sprint(now)) }
	clause := fill(" WHERE " + strings.Join(where, " AND "))
	order := fill(" ORDER BY " + col + " " + dir + ", i.sort_title, i.id")
	joins := ""
	switch f.Sort {
	case "nextAiring", "previousAiring", "missing":
		joins = fill(episodeAgg)
	case "size":
		joins = sizeAgg
	}
	total := 0
	if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*)`+seriesFrom+clause, args...).Scan(&total); err != nil { // #nosec G202 -- clauses are constants
		return nil, 0, err
	}
	list, err := s.querySeries(ctx, `SELECT `+seriesColumns+seriesFrom+joins+clause+order+` LIMIT ? OFFSET ?`, append(args, f.Limit, max(f.Offset, 0))...) // #nosec G202
	if err != nil {
		return nil, 0, err
	}
	items, err := s.decorate(ctx, list)
	return items, total, err
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

// decorate adds episode statistics and downloads for a page of series.
func (s *Service) decorate(ctx context.Context, list []Series) ([]ListItem, error) {
	items := make([]ListItem, len(list))
	ids := make([]any, len(list))
	byID := map[int64]int{}
	for i := range list {
		items[i] = ListItem{Series: list[i]}
		ids[i] = list[i].ID
		byID[list[i].ID] = i
	}
	if len(ids) == 0 {
		return items, nil
	}
	now := db.Millis(s.now())
	in := placeholders(len(ids))
	rows, err := s.DB.R.QueryContext(ctx, `SELECT e.series_id,
		SUM(CASE WHEN e.season_number > 0 AND e.removed = 0 THEN 1 ELSE 0 END),
		SUM(CASE WHEN e.file_id IS NOT NULL THEN 1 ELSE 0 END),
		SUM(CASE WHEN e.monitored = 1 AND e.removed = 0 THEN 1 ELSE 0 END),
		SUM(CASE WHEN e.monitored = 1 AND sn.monitored = 1 AND e.removed = 0 AND e.file_id IS NULL AND e.air_date_utc <= ? THEN 1 ELSE 0 END),
		MIN(CASE WHEN e.air_date_utc > ? THEN e.air_date_utc END),
		MAX(CASE WHEN e.air_date_utc <= ? THEN e.air_date_utc END),
		COUNT(DISTINCT CASE WHEN e.season_number > 0 THEN e.season_number END)
		FROM episodes e JOIN seasons sn ON sn.id = e.season_id WHERE e.series_id IN (`+in+`) GROUP BY e.series_id`, // #nosec G202 -- placeholders only
		append([]any{now, now, now}, ids...)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			id         int64
			st         Stats
			next, prev sql.NullInt64
		)
		if err := rows.Scan(&id, &st.Episodes, &st.EpisodeFiles, &st.MonitoredEpisodes, &st.Missing, &next, &prev, &st.Seasons); err != nil {
			_ = rows.Close()
			return nil, err
		}
		st.NextAiring, st.PreviousAiring = db.NullTime(next), db.NullTime(prev)
		it := &items[byID[id]]
		if !it.Monitored {
			st.Missing = 0
		}
		it.Stats = st
	}
	_ = rows.Close()
	rows, err = s.DB.R.QueryContext(ctx, `SELECT item_id, SUM(size) FROM media_files WHERE item_id IN (`+in+`) GROUP BY item_id`, ids...) // #nosec G202
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, size int64
		if err := rows.Scan(&id, &size); err != nil {
			_ = rows.Close()
			return nil, err
		}
		items[byID[id]].SizeOnDisk = size
	}
	_ = rows.Close()
	rows, err = s.DB.R.QueryContext(ctx, `SELECT e.series_id, COUNT(DISTINCT d.id) FROM downloads d
		JOIN download_refs r ON r.download_id = d.id JOIN episodes e ON r.item_ref = 'episode:' || e.id
		WHERE d.status = 'active' AND e.series_id IN (`+in+`) GROUP BY e.series_id`, ids...) // #nosec G202
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		items[byID[id]].Downloading = n
	}
	return items, rows.Err()
}

// Item returns one series with its list facts.
func (s *Service) Item(ctx context.Context, id int64) (ListItem, error) {
	v, err := s.Get(ctx, id)
	if err != nil {
		return ListItem{}, err
	}
	items, err := s.decorate(ctx, []Series{v})
	if err != nil {
		return ListItem{}, err
	}
	return items[0], nil
}

// EpisodeDownload is an episode's download under way.
type EpisodeDownload struct {
	ID       int64
	Title    string
	Progress float64
	State    string
	Status   string
}

// EpisodeView is an episode with what the series page shows.
type EpisodeView struct {
	Episode
	Aired      bool
	CutoffMet  bool
	Wanted     string // missing, upgrade or ""
	Download   *EpisodeDownload
	SeasonMon  bool
	SeriesType string
}

// EpisodeViews returns every episode of a series with its state.
func (s *Service) EpisodeViews(ctx context.Context, seriesID int64) ([]EpisodeView, error) {
	vw, err := s.loadView(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	dl, err := s.activeDownloads(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	out := make([]EpisodeView, 0, len(vw.eps))
	for i := range vw.eps {
		e := &vw.eps[i]
		sn := vw.seasons[e.SeasonID]
		ev := EpisodeView{Episode: *e, Aired: e.Aired(now), CutoffMet: CutoffMet(vw.profile, e.File), SeriesType: vw.series.SeriesType}
		if sn != nil {
			ev.SeasonMon = sn.Monitored
			ev.Wanted = episodeWanted(&vw.series, sn, e, vw.profile, now)
		}
		ev.Download = dl[e.ID]
		out = append(out, ev)
	}
	return out, nil
}

// activeDownloads maps a series' episodes to their downloads under way.
func (s *Service) activeDownloads(ctx context.Context, seriesID int64) (map[int64]*EpisodeDownload, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT e.id, d.id, d.title, d.progress, d.state, d.status FROM downloads d
		JOIN download_refs r ON r.download_id = d.id JOIN episodes e ON r.item_ref = 'episode:' || e.id
		WHERE d.status IN ('active', 'completed') AND e.series_id = ? ORDER BY d.added_at`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]*EpisodeDownload{}
	for rows.Next() {
		var id int64
		var d EpisodeDownload
		if err := rows.Scan(&id, &d.ID, &d.Title, &d.Progress, &d.State, &d.Status); err != nil {
			return nil, err
		}
		dd := d
		out[id] = &dd
	}
	return out, rows.Err()
}

// SeasonView is a season with its statistics.
type SeasonView struct {
	Season
	Stats
}

// SeasonViews returns a series' seasons with their statistics.
func (s *Service) SeasonViews(ctx context.Context, seriesID int64) ([]SeasonView, error) {
	vw, err := s.loadView(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	stats := map[int64]*Stats{}
	sizes := map[int64]bool{}
	for i := range vw.eps {
		e := &vw.eps[i]
		st := stats[e.SeasonID]
		if st == nil {
			st = &Stats{}
			stats[e.SeasonID] = st
		}
		if !e.Removed {
			st.Episodes++
		}
		if e.File != nil {
			st.EpisodeFiles++
			if !sizes[e.File.ID] {
				sizes[e.File.ID] = true
				st.SizeOnDisk += e.File.Size
			}
		}
		if e.Monitored && !e.Removed {
			st.MonitoredEpisodes++
		}
		if sn := vw.seasons[e.SeasonID]; sn != nil && episodeWanted(&vw.series, sn, e, vw.profile, now) == "missing" {
			st.Missing++
		}
		if e.AirDateUTC != nil {
			if e.AirDateUTC.After(now) && (st.NextAiring == nil || e.AirDateUTC.Before(*st.NextAiring)) {
				st.NextAiring = e.AirDateUTC
			}
			if !e.AirDateUTC.After(now) && (st.PreviousAiring == nil || e.AirDateUTC.After(*st.PreviousAiring)) {
				st.PreviousAiring = e.AirDateUTC
			}
		}
	}
	out := make([]SeasonView, 0, len(vw.seasons))
	for id, sn := range vw.seasons {
		sv := SeasonView{Season: *sn}
		if st := stats[id]; st != nil {
			sv.Stats = *st
		}
		out = append(out, sv)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

// CalendarEntry is an episode on the calendar.
type CalendarEntry struct {
	Date         string
	AirDateUTC   *time.Time
	SeriesID     int64
	EpisodeID    int64
	SeriesTitle  string
	Season       int
	Number       int
	EpisodeTitle string
	HasFile      bool
	Monitored    bool
	Poster       string
	Network      string
}

// Calendar lists episodes airing between from and to (YYYY-MM-DD, inclusive).
func (s *Service) Calendar(ctx context.Context, from, to string) ([]CalendarEntry, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT e.air_date, e.air_date_utc, i.id, e.id, i.title, e.season_number, e.episode_number, e.title,
		e.file_id IS NOT NULL, (e.monitored = 1 AND i.monitored = 1), s.poster, s.network
		FROM episodes e JOIN media_items i ON i.id = e.series_id JOIN series s ON s.item_id = i.id
		WHERE e.air_date <> '' AND e.air_date BETWEEN ? AND ? AND e.removed = 0
		ORDER BY e.air_date, e.air_date_utc, i.sort_title, e.season_number, e.episode_number`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarEntry{}
	for rows.Next() {
		var (
			c   CalendarEntry
			air sql.NullInt64
		)
		if err := rows.Scan(&c.Date, &air, &c.SeriesID, &c.EpisodeID, &c.SeriesTitle, &c.Season, &c.Number, &c.EpisodeTitle, &c.HasFile,
			&c.Monitored, &c.Poster, &c.Network); err != nil {
			return nil, err
		}
		c.AirDateUTC = db.NullTime(air)
		out = append(out, c)
	}
	return out, rows.Err()
}

// WantedEpisode is a missing episode or one below the cutoff.
type WantedEpisode struct {
	EpisodeView
	SeriesTitle string
	Poster      string
}

// WantedEpisodes lists missing (kind "missing") or below-cutoff
// ("cutoff_unmet") episodes of monitored series, oldest air date first.
func (s *Service) WantedEpisodes(ctx context.Context, kind string, offset, limit int) ([]WantedEpisode, int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	now := s.now()
	cond := "e.file_id IS NULL"
	if kind == "cutoff_unmet" {
		cond = "e.file_id IS NOT NULL"
	}
	q := `SELECT ` + episodeColumns + `, i.title, s.poster, i.quality_profile_id, s.series_type` + episodeFrom + `
		JOIN media_items i ON i.id = e.series_id JOIN series s ON s.item_id = i.id JOIN seasons sn ON sn.id = e.season_id
		WHERE i.monitored = 1 AND sn.monitored = 1 AND e.monitored = 1 AND e.removed = 0 AND e.air_date_utc <= ? AND ` + cond + `
		ORDER BY e.air_date_utc, i.sort_title, e.season_number, e.episode_number`
	if kind != "cutoff_unmet" {
		var total int
		if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodes e JOIN media_items i ON i.id = e.series_id JOIN seasons sn ON sn.id = e.season_id
			WHERE i.monitored = 1 AND sn.monitored = 1 AND e.monitored = 1 AND e.removed = 0 AND e.air_date_utc <= ? AND e.file_id IS NULL`,
			db.Millis(now)).Scan(&total); err != nil {
			return nil, 0, err
		}
		list, err := s.wantedRows(ctx, q+` LIMIT ? OFFSET ?`, nil, db.Millis(now), limit, max(offset, 0))
		return list, total, err
	}
	profiles, err := s.profiles(ctx)
	if err != nil {
		return nil, 0, err
	}
	all, err := s.wantedRows(ctx, q, profiles, db.Millis(now))
	if err != nil {
		return nil, 0, err
	}
	lo := min(max(offset, 0), len(all))
	hi := min(lo+limit, len(all))
	return all[lo:hi], len(all), nil
}

func (s *Service) wantedRows(ctx context.Context, q string, cutoffOnly map[int64]*quality.Profile, args ...any) ([]WantedEpisode, error) {
	rows, err := s.DB.R.QueryContext(ctx, q, args...) // #nosec G202 -- constant query
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WantedEpisode{}
	for rows.Next() {
		var (
			w         WantedEpisode
			profileID int64
		)
		e, err := scanEpisode(scanTail{rows, []any{&w.SeriesTitle, &w.Poster, &profileID, &w.SeriesType}})
		if err != nil {
			return nil, err
		}
		w.Episode, w.Aired, w.SeasonMon = e, true, true
		if cutoffOnly != nil {
			w.CutoffMet = CutoffMet(cutoffOnly[profileID], e.File)
			if w.CutoffMet {
				continue
			}
			w.Wanted = "upgrade"
		} else {
			w.Wanted = "missing"
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// scanTail scans an episode row followed by extra columns.
type scanTail struct {
	rows  *sql.Rows
	extra []any
}

func (t scanTail) Scan(dest ...any) error { return t.rows.Scan(append(dest, t.extra...)...) }

// ExplainState is what "why isn't it here?" needs for a series ref.
type ExplainState struct {
	State timeline.ItemState
	// Refs are the refs whose history concerns the item (the item first).
	Refs  []string
	Title string
}

// Explain returns the state of an episode, season or series ref.
func (s *Service) Explain(ctx context.Context, ref library.Ref) (ExplainState, error) {
	sid, err := s.seriesOf(ctx, ref)
	if err != nil {
		return ExplainState{}, err
	}
	vw, err := s.loadView(ctx, sid)
	if err != nil {
		return ExplainState{}, err
	}
	now := s.now()
	v := &vw.series
	out := ExplainState{Refs: []string{ref.String()}}
	switch ref.Kind {
	case library.RefEpisode:
		for i := range vw.eps {
			e := &vw.eps[i]
			if e.ID != ref.ID {
				continue
			}
			sn := vw.seasons[e.SeasonID]
			out.Title = fmt.Sprintf("%s S%02dE%02d", v.Title, e.Season, e.Number)
			out.State = timeline.ItemState{Ref: ref.String(), Monitored: v.Monitored && sn.Monitored && e.Monitored && !e.Removed,
				Available: e.Aired(now), AvailableFrom: e.AirDateUTC, HasFile: e.File != nil, CutoffMet: CutoffMet(vw.profile, e.File)}
			out.Refs = append(out.Refs, sn.Ref().String())
			return out, nil
		}
		return out, ErrEpisodeNotFound
	case library.RefSeason:
		sn := vw.seasons[ref.ID]
		if sn == nil {
			return out, ErrSeasonNotFound
		}
		out.Title = fmt.Sprintf("%s season %d", v.Title, sn.Number)
		missing, upgrade, aired := 0, 0, false
		for i := range vw.eps {
			e := &vw.eps[i]
			if e.SeasonID != sn.ID {
				continue
			}
			aired = aired || e.Aired(now)
			switch episodeWanted(v, sn, e, vw.profile, now) {
			case "missing":
				missing++
			case "upgrade":
				upgrade++
			}
		}
		out.State = timeline.ItemState{Ref: ref.String(), Monitored: v.Monitored && sn.Monitored, Available: aired, HasFile: missing == 0,
			CutoffMet: missing == 0 && upgrade == 0}
		return out, nil
	default:
		out.Title = v.Title
		missing, upgrade, aired := 0, 0, false
		for i := range vw.eps {
			e := &vw.eps[i]
			aired = aired || e.Aired(now)
			if sn := vw.seasons[e.SeasonID]; sn != nil {
				switch episodeWanted(v, sn, e, vw.profile, now) {
				case "missing":
					missing++
				case "upgrade":
					upgrade++
				}
			}
		}
		for _, sn := range vw.seasons {
			out.Refs = append(out.Refs, sn.Ref().String())
		}
		out.State = timeline.ItemState{Ref: ref.String(), Monitored: v.Monitored, Available: aired, HasFile: missing == 0,
			CutoffMet: missing == 0 && upgrade == 0}
		return out, nil
	}
}
