package requests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/explain"
	"github.com/mmzlopasa/locarr/server/internal/library/movies"
	"github.com/mmzlopasa/locarr/server/internal/library/music"
	"github.com/mmzlopasa/locarr/server/internal/library/recycle"
	"github.com/mmzlopasa/locarr/server/internal/library/series"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
	"github.com/mmzlopasa/locarr/server/internal/wanted"
)

// ErrGone is returned when a request's library item no longer exists.
var ErrGone = errors.New("the library item is gone")

// Title is what the metadata provider says about a requested title.
type Title struct {
	Title      string
	Subtitle   string
	Year       int
	Poster     string
	ArtistMBID string
	// Seasons are a series' regular season numbers (no specials).
	Seasons []int
}

// Options are the library options an approved request is added with.
type Options struct {
	ProfileID           int64
	RootFolderID        int64
	MetadataProfileID   int64
	MinimumAvailability library.Availability
	Tags                []string
}

// LibraryState is what the library holds of a title.
type LibraryState struct {
	Ref     string
	Status  Status         // processing = in the library without (all) files
	Seasons map[int]Status // series: by season number
}

// Library is the part of the media library requests use. Bridge implements
// it with the movie, series and music services (P4–P6).
type Library interface {
	Describe(ctx context.Context, kind Kind, tmdbID int64, mbid string) (Title, error)
	// Lookup finds the title in the library (ok false when it is not there).
	Lookup(ctx context.Context, kind Kind, tmdbID int64, mbid string) (LibraryState, bool, error)
	// Fulfil adds (or monitors) the request's title and starts searching; it
	// returns the item's ref.
	Fulfil(ctx context.Context, r *Request, o Options) (string, error)
	// State computes the request's status from the library (ErrGone when the
	// item was removed).
	State(ctx context.Context, r *Request) (Status, map[int]Status, error)
	// Explain is the library's explanation for the request's item (for a
	// series: its first requested season that is not complete yet).
	Explain(ctx context.Context, r *Request) (*Explanation, error)
	Progress(ctx context.Context, r *Request) (*Progress, error)
	Search(ctx context.Context, r *Request) error
	Exists(ctx context.Context, ref string) bool
	// Replace sends the item's current files (one episode, a season, or all)
	// to the recycle bin, blocklists their releases and searches again.
	Replace(ctx context.Context, ref string, season, episode *int, reason string) (int, error)
}

// Bridge implements Library with the library services.
type Bridge struct {
	DB        *db.DB
	Movies    *movies.Service
	Series    *series.Service
	Music     *music.Service
	Wanted    *wanted.Service
	Downloads *downloads.Service
	Recycle   *recycle.Bin
	Timeline  *timeline.Store
	TMDb      metadata.MovieProvider
	TV        metadata.SeriesProvider
	MB        *musicbrainz.Client
	Now       func() time.Time
}

var _ Library = (*Bridge)(nil)

func (b *Bridge) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// Describe implements Library.
func (b *Bridge) Describe(ctx context.Context, kind Kind, tmdbID int64, mbid string) (Title, error) {
	switch kind {
	case KindMovie:
		m, err := b.TMDb.Movie(ctx, tmdbID)
		if err != nil {
			return Title{}, err
		}
		return Title{Title: m.Title, Year: m.Year, Poster: m.Poster}, nil
	case KindSeries:
		v, err := b.TV.Series(ctx, tmdbID)
		if err != nil {
			return Title{}, err
		}
		t := Title{Title: v.Title, Year: v.Year, Poster: v.Poster}
		for _, sn := range v.Seasons {
			if sn.Number > 0 {
				t.Seasons = append(t.Seasons, sn.Number)
			}
		}
		return t, nil
	case KindAlbum:
		if b.MB == nil {
			return Title{}, metadata.ErrUnavailable
		}
		g, err := b.MB.ReleaseGroup(ctx, mbid)
		if err != nil {
			return Title{}, err
		}
		return Title{Title: g.Title, Subtitle: g.ArtistName, Year: yearOf(g.FirstReleaseDate), Poster: musicbrainz.CoverName(g.MBID),
			ArtistMBID: g.ArtistMBID}, nil
	case KindArtist:
		if b.MB == nil {
			return Title{}, metadata.ErrUnavailable
		}
		a, err := b.MB.Artist(ctx, mbid)
		if err != nil {
			return Title{}, err
		}
		return Title{Title: a.Name, Subtitle: a.Disambiguation, ArtistMBID: a.MBID}, nil
	}
	return Title{}, invalid("unknown kind %q", kind)
}

func yearOf(date string) int {
	if len(date) >= 4 {
		var y int
		_, _ = fmt.Sscanf(date[:4], "%d", &y)
		return y
	}
	return 0
}

// seasonFacts counts aired episodes and files per season of a series.
func (b *Bridge) seasonFacts(ctx context.Context, seriesID int64) (map[int]SeasonFacts, error) {
	rows, err := b.DB.R.QueryContext(ctx, `SELECT e.season_number,
		SUM(CASE WHEN e.air_date_utc IS NOT NULL AND e.air_date_utc <= ? THEN 1 ELSE 0 END),
		SUM(CASE WHEN e.file_id IS NOT NULL AND (e.air_date_utc IS NULL OR e.air_date_utc <= ?) THEN 1 ELSE 0 END)
		FROM episodes e WHERE e.series_id = ? AND e.removed = 0 GROUP BY e.season_number`,
		db.Millis(b.now()), db.Millis(b.now()), seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]SeasonFacts{}
	for rows.Next() {
		var f SeasonFacts
		if err := rows.Scan(&f.Number, &f.Aired, &f.Files); err != nil {
			return nil, err
		}
		out[f.Number] = f
	}
	return out, rows.Err()
}

func (b *Bridge) albumByMBID(ctx context.Context, mbid string) (music.Album, error) {
	var id int64
	err := b.DB.R.QueryRowContext(ctx, `SELECT id FROM albums WHERE mbid = ?`, strings.ToLower(mbid)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return music.Album{}, music.ErrNotFound
	}
	if err != nil {
		return music.Album{}, err
	}
	return b.Music.Album(ctx, id)
}

func (b *Bridge) albumStatus(ctx context.Context, al music.Album) (Status, error) {
	st, err := b.Music.State(ctx, al, nil, nil)
	if err != nil {
		return "", err
	}
	return TrackStatus(st.FileCount, st.TrackCount), nil
}

func (b *Bridge) artistStatus(ctx context.Context, a music.Artist) (Status, error) {
	list, err := b.Music.AlbumStates(ctx, &a)
	if err != nil {
		return "", err
	}
	wanted, complete, some := 0, 0, false
	for _, st := range list {
		if !st.Monitored || !st.Available {
			continue
		}
		wanted++
		if st.Complete() {
			complete++
		}
		if st.FileCount > 0 {
			some = true
		}
	}
	switch {
	case wanted > 0 && complete == wanted:
		return StatusAvailable, nil
	case some:
		return StatusPartial, nil
	}
	return StatusProcessing, nil
}

// Lookup implements Library.
func (b *Bridge) Lookup(ctx context.Context, kind Kind, tmdbID int64, mbid string) (LibraryState, bool, error) {
	switch kind {
	case KindMovie:
		m, err := b.Movies.ByTMDb(ctx, tmdbID)
		if errors.Is(err, movies.ErrNotFound) {
			return LibraryState{}, false, nil
		}
		if err != nil {
			return LibraryState{}, false, err
		}
		return LibraryState{Ref: m.Ref().String(), Status: MovieStatus(m.File != nil)}, true, nil
	case KindSeries:
		v, err := b.Series.ByProviderID(ctx, tmdbID, 0)
		if errors.Is(err, series.ErrNotFound) {
			return LibraryState{}, false, nil
		}
		if err != nil {
			return LibraryState{}, false, err
		}
		facts, err := b.seasonFacts(ctx, v.ID)
		if err != nil {
			return LibraryState{}, false, err
		}
		st := LibraryState{Ref: library.SeriesRef(v.ID).String(), Seasons: map[int]Status{}}
		var numbers []int
		for n, f := range facts {
			if n > 0 {
				st.Seasons[n] = SeasonStatus(f)
				numbers = append(numbers, n)
			}
		}
		st.Status, _ = SeriesStatus(numbers, facts)
		return st, true, nil
	case KindAlbum:
		al, err := b.albumByMBID(ctx, mbid)
		if errors.Is(err, music.ErrNotFound) {
			return LibraryState{}, false, nil
		}
		if err != nil {
			return LibraryState{}, false, err
		}
		s, err := b.albumStatus(ctx, al)
		return LibraryState{Ref: al.Ref().String(), Status: s}, err == nil, err
	case KindArtist:
		a, err := b.Music.ArtistByMBID(ctx, mbid)
		if errors.Is(err, music.ErrNotFound) {
			return LibraryState{}, false, nil
		}
		if err != nil {
			return LibraryState{}, false, err
		}
		s, err := b.artistStatus(ctx, a)
		return LibraryState{Ref: a.Ref().String(), Status: s}, err == nil, err
	}
	return LibraryState{}, false, nil
}

func (b *Bridge) enqueue(ctx context.Context, ref library.Ref, reason string) error {
	b.Wanted.ResetBackoff(ctx, ref)
	return b.Wanted.Enqueue(ctx, ref, reason)
}

// Fulfil implements Library.
func (b *Bridge) Fulfil(ctx context.Context, r *Request, o Options) (string, error) {
	yes := true
	switch r.Kind {
	case KindMovie:
		m, err := b.Movies.ByTMDb(ctx, r.TMDbID)
		if errors.Is(err, movies.ErrNotFound) {
			m, err = b.Movies.Add(ctx, movies.AddInput{TMDbID: r.TMDbID, RootFolderID: o.RootFolderID, QualityProfileID: o.ProfileID,
				MinimumAvailability: o.MinimumAvailability, Monitored: true, Tags: o.Tags, SearchOnAdd: true})
			if err != nil {
				return "", err
			}
			return m.Ref().String(), nil
		}
		if err != nil {
			return "", err
		}
		u := movies.Update{Monitored: &yes}
		if r.Is4K && o.ProfileID > 0 && o.ProfileID != m.QualityProfileID {
			u.QualityProfileID = &o.ProfileID
		}
		if _, err := b.Movies.Edit(ctx, m.ID, u); err != nil {
			return "", err
		}
		return m.Ref().String(), b.enqueue(ctx, m.Ref(), "requested")
	case KindSeries:
		v, err := b.Series.ByProviderID(ctx, r.TMDbID, 0)
		newSeasons := series.MonitorNone
		if r.FutureSeasons {
			newSeasons = series.MonitorAll
		}
		if errors.Is(err, series.ErrNotFound) {
			v, err = b.Series.Add(ctx, series.AddInput{TMDbID: r.TMDbID, RootFolderID: o.RootFolderID, QualityProfileID: o.ProfileID,
				SeriesType: "standard", SeasonFolder: true, Monitor: series.MonitorNone, MonitorNewSeasons: newSeasons, Monitored: true,
				Tags: o.Tags})
		} else if err == nil {
			u := series.Update{Monitored: &yes}
			if r.FutureSeasons {
				u.MonitorNewSeasons = &newSeasons
			}
			if r.Is4K && o.ProfileID > 0 && o.ProfileID != v.QualityProfileID {
				u.QualityProfileID = &o.ProfileID
			}
			v, err = b.Series.Edit(ctx, v.ID, u)
		}
		if err != nil {
			return "", err
		}
		for _, n := range r.SeasonNumbers() {
			if _, err := b.Series.SetSeasonMonitored(ctx, v.ID, n, true); err != nil && !errors.Is(err, series.ErrNotFound) {
				return "", err
			}
		}
		ref := library.SeriesRef(v.ID)
		return ref.String(), b.searchSeries(ctx, v.ID)
	case KindAlbum, KindArtist:
		return b.fulfilMusic(ctx, r, o)
	}
	return "", invalid("unknown kind %q", r.Kind)
}

func (b *Bridge) searchSeries(ctx context.Context, id int64) error {
	refs, err := b.Series.SearchTargets(ctx, id)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if err := b.enqueue(ctx, ref, "requested"); err != nil {
			return err
		}
	}
	return nil
}

// everythingProfile is the metadata profile used when none is chosen, so a
// requested album of any type is in the artist's list.
func (b *Bridge) everythingProfile(ctx context.Context) int64 {
	list, err := b.Music.Profiles(ctx)
	if err != nil {
		return 0
	}
	for _, p := range list {
		if p.Name == "Everything" {
			return p.ID
		}
	}
	if len(list) > 0 {
		return list[0].ID
	}
	return 0
}

func (b *Bridge) fulfilMusic(ctx context.Context, r *Request, o Options) (string, error) {
	if b.Music == nil {
		return "", metadata.ErrUnavailable
	}
	yes := true
	artistMBID := r.ArtistMBID
	if r.Kind == KindArtist {
		artistMBID = r.MBID
	}
	monitor, monitorNew := music.MonitorNone, music.MonitorNone
	if r.Kind == KindArtist {
		monitor, monitorNew = music.MonitorAll, music.MonitorAll
	}
	a, err := b.Music.ArtistByMBID(ctx, artistMBID)
	switch {
	case errors.Is(err, music.ErrNotFound):
		mp := o.MetadataProfileID
		if mp == 0 {
			mp = b.everythingProfile(ctx)
		}
		a, err = b.Music.Add(ctx, music.AddInput{MBID: artistMBID, RootFolderID: o.RootFolderID, QualityProfileID: o.ProfileID,
			MetadataProfileID: mp, Monitored: true, Monitor: monitor, MonitorNewAlbums: monitorNew, Tags: o.Tags,
			SearchOnAdd: r.Kind == KindArtist})
		if err != nil {
			return "", err
		}
		if r.Kind == KindArtist {
			return a.Ref().String(), nil
		}
	case err != nil:
		return "", err
	default:
		u := music.ArtistUpdate{Monitored: &yes}
		if r.Kind == KindArtist {
			u.MonitorNewAlbums = &monitorNew
		}
		if a, err = b.Music.EditArtist(ctx, a.ID, u); err != nil {
			return "", err
		}
	}
	albums, err := b.Music.Albums(ctx, a.ID)
	if err != nil {
		return "", err
	}
	if r.Kind == KindArtist {
		var ids []int64
		for _, al := range albums {
			ids = append(ids, al.ID)
		}
		if err := b.Music.SetMonitored(ctx, ids, true); err != nil {
			return "", err
		}
		for _, al := range albums {
			if err := b.enqueue(ctx, al.Ref(), "requested"); err != nil {
				return "", err
			}
		}
		return a.Ref().String(), nil
	}
	for _, al := range albums {
		if strings.EqualFold(al.MBID, r.MBID) {
			if err := b.Music.SetMonitored(ctx, []int64{al.ID}, true); err != nil {
				return "", err
			}
			return al.Ref().String(), b.enqueue(ctx, al.Ref(), "requested")
		}
	}
	return "", invalid("the album is not in %s's list (its metadata profile leaves it out)", a.Name)
}

// State implements Library.
func (b *Bridge) State(ctx context.Context, r *Request) (Status, map[int]Status, error) {
	ref, err := library.ParseRef(r.ItemRef)
	if err != nil {
		return "", nil, ErrGone
	}
	switch ref.Kind {
	case library.RefMovie:
		it, err := b.Movies.Item(ctx, ref.ID)
		if errors.Is(err, movies.ErrNotFound) {
			return "", nil, ErrGone
		}
		if err != nil {
			return "", nil, err
		}
		return MovieStatus(it.File != nil), nil, nil
	case library.RefSeries:
		if _, err := b.Series.Get(ctx, ref.ID); errors.Is(err, series.ErrNotFound) {
			return "", nil, ErrGone
		} else if err != nil {
			return "", nil, err
		}
		facts, err := b.seasonFacts(ctx, ref.ID)
		if err != nil {
			return "", nil, err
		}
		st, per := SeriesStatus(r.SeasonNumbers(), facts)
		return st, per, nil
	case library.RefAlbum:
		al, err := b.Music.Album(ctx, ref.ID)
		if errors.Is(err, music.ErrNotFound) {
			return "", nil, ErrGone
		}
		if err != nil {
			return "", nil, err
		}
		st, err := b.albumStatus(ctx, al)
		return st, nil, err
	case library.RefArtist:
		a, err := b.Music.Artist(ctx, ref.ID)
		if errors.Is(err, music.ErrNotFound) {
			return "", nil, ErrGone
		}
		if err != nil {
			return "", nil, err
		}
		st, err := b.artistStatus(ctx, a)
		return st, nil, err
	}
	return "", nil, ErrGone
}

// Explain implements Library.
func (b *Bridge) Explain(ctx context.Context, req *Request) (*Explanation, error) {
	r, err := library.ParseRef(req.ItemRef)
	if err != nil {
		return nil, err
	}
	if r.Kind == library.RefSeries {
		for _, sn := range req.Seasons {
			if sn.Status == StatusAvailable {
				continue
			}
			var id int64
			if b.DB.R.QueryRowContext(ctx, `SELECT id FROM seasons WHERE series_id = ? AND number = ?`, r.ID, sn.Number).Scan(&id) == nil {
				r = library.SeasonRef(id)
			}
			break
		}
	}
	x := explain.Explainer{Movies: b.Movies, Series: b.Series, Music: b.Music, Timeline: b.Timeline}
	ex, _, _, err := x.Explain(ctx, r)
	if err != nil {
		return nil, err
	}
	return &Explanation{Code: ex.Code, Tone: ex.Tone, Message: ex.Message, Params: ex.Params}, nil
}

// downloadCond selects the download_refs of a request's item.
func (b *Bridge) downloadCond(r *Request) (string, []any, bool) {
	ref, err := library.ParseRef(r.ItemRef)
	if err != nil {
		return "", nil, false
	}
	switch ref.Kind {
	case library.RefSeries:
		nums := r.SeasonNumbers()
		if len(nums) == 0 {
			return "", nil, false
		}
		ph := strings.TrimSuffix(strings.Repeat("?,", len(nums)), ",")
		args := []any{ref.ID}
		for _, n := range nums {
			args = append(args, n)
		}
		args = append(args, ref.ID)
		for _, n := range nums {
			args = append(args, n)
		}
		return `item_ref IN (SELECT 'episode:' || id FROM episodes WHERE series_id = ? AND season_number IN (` + ph + `))
			OR item_ref IN (SELECT 'season:' || id FROM seasons WHERE series_id = ? AND number IN (` + ph + `))`, args, true
	case library.RefArtist:
		return `item_ref IN (SELECT 'album:' || id FROM albums WHERE artist_id = ?)`, []any{ref.ID}, true
	}
	return `item_ref = ?`, []any{ref.String()}, true
}

// Progress implements Library.
func (b *Bridge) Progress(ctx context.Context, r *Request) (*Progress, error) {
	cond, args, ok := b.downloadCond(r)
	if !ok {
		return nil, nil
	}
	var (
		p     Progress
		avg   sql.NullFloat64
		state sql.NullString
		eta   sql.NullInt64
	)
	err := b.DB.R.QueryRowContext(ctx, `SELECT COUNT(*), AVG(progress), MAX(state), MAX(eta) FROM downloads
		WHERE status = 'active' AND id IN (SELECT download_id FROM download_refs WHERE `+cond+`)`, args...).Scan(&p.Downloads, &avg, &state, &eta)
	if err != nil || p.Downloads == 0 {
		return nil, err
	}
	p.Percent, p.State = avg.Float64, state.String
	if eta.Valid {
		p.ETA = &eta.Int64
	}
	return &p, nil
}

// Search implements Library.
func (b *Bridge) Search(ctx context.Context, r *Request) error {
	ref, err := library.ParseRef(r.ItemRef)
	if err != nil {
		return err
	}
	switch ref.Kind {
	case library.RefSeries:
		return b.searchSeries(ctx, ref.ID)
	case library.RefArtist:
		albums, err := b.Music.Albums(ctx, ref.ID)
		if err != nil {
			return err
		}
		for _, al := range albums {
			if al.Monitored {
				if err := b.enqueue(ctx, al.Ref(), "request retry"); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return b.enqueue(ctx, ref, "request retry")
}

// Exists implements Library.
func (b *Bridge) Exists(ctx context.Context, ref string) bool {
	r, err := library.ParseRef(ref)
	if err != nil {
		return false
	}
	switch r.Kind {
	case library.RefMovie:
		_, err = b.Movies.Get(ctx, r.ID)
	case library.RefSeries:
		_, err = b.Series.Get(ctx, r.ID)
	case library.RefAlbum:
		_, err = b.Music.Album(ctx, r.ID)
	case library.RefArtist:
		_, err = b.Music.Artist(ctx, r.ID)
	}
	return err == nil
}

type fileRow struct {
	id         int64
	path       string
	scene      string
	downloadID sql.NullInt64
}

func (b *Bridge) files(ctx context.Context, ref library.Ref, season, episode *int) ([]fileRow, error) {
	var (
		q    string
		args []any
	)
	switch ref.Kind {
	case library.RefMovie:
		q, args = `SELECT id, path, scene_name, download_id FROM media_files WHERE item_id = ?`, []any{ref.ID}
	case library.RefSeries:
		q = `SELECT f.id, f.path, f.scene_name, f.download_id FROM media_files f WHERE f.id IN
			(SELECT e.file_id FROM episodes e WHERE e.series_id = ? AND e.file_id IS NOT NULL`
		args = []any{ref.ID}
		if season != nil {
			q += ` AND e.season_number = ?`
			args = append(args, *season)
		}
		if episode != nil {
			q += ` AND e.episode_number = ?`
			args = append(args, *episode)
		}
		q += `)`
	case library.RefAlbum:
		q = `SELECT f.id, f.path, f.scene_name, f.download_id FROM media_files f JOIN album_files af ON af.file_id = f.id WHERE af.album_id = ?`
		args = []any{ref.ID}
	default:
		return nil, invalid("cannot replace files of %s", ref)
	}
	rows, err := b.DB.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []fileRow
	for rows.Next() {
		var f fileRow
		if err := rows.Scan(&f.id, &f.path, &f.scene, &f.downloadID); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Replace implements Library. Files never get deleted: they go to the
// recycle bin; their release is blocklisted so the next search picks another.
func (b *Bridge) Replace(ctx context.Context, ref string, season, episode *int, reason string) (int, error) {
	r, err := library.ParseRef(ref)
	if err != nil {
		return 0, err
	}
	list, err := b.files(ctx, r, season, episode)
	if err != nil {
		return 0, err
	}
	blocked := map[string]bool{}
	for _, f := range list {
		if f.scene != "" && !blocked[f.scene] {
			blocked[f.scene] = true
			e := downloads.BlockEntry{Title: f.scene, Reason: "issue", Message: reason, ItemRef: r.JSON()}
			if f.downloadID.Valid {
				id := f.downloadID.Int64
				e.DownloadID = &id
				if d, err := b.Downloads.Get(ctx, id); err == nil {
					e.Protocol, e.IndexerID, e.Indexer, e.InfoHash, e.GUID = string(d.Protocol), d.IndexerID, d.Indexer, d.InfoHash, d.GUID
				}
			}
			if _, err := b.Downloads.AddBlock(ctx, e); err != nil {
				return 0, err
			}
		}
		if _, err := os.Stat(f.path); err == nil {
			if _, err := b.Recycle.Recycle(ctx, f.path, r.String(), "replacement requested: "+reason); err != nil {
				return 0, err
			}
		}
		if _, err := b.DB.W.ExecContext(ctx, `DELETE FROM media_files WHERE id = ?`, f.id); err != nil {
			return 0, err
		}
	}
	if len(list) > 0 {
		_ = b.Timeline.Record(ctx, r.String(), timeline.TypeRecycled, fmt.Sprintf("%d file(s) moved to the recycle bin for a replacement: %s",
			len(list), reason), map[string]any{"files": len(list), "blocklisted": len(blocked)}, nil)
	}
	switch r.Kind {
	case library.RefSeries:
		return len(list), b.searchSeries(ctx, r.ID)
	default:
		return len(list), b.enqueue(ctx, r, "replacement")
	}
}
