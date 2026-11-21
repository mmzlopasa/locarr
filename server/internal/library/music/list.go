package music

import (
	"context"
	"database/sql"
		"strings"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/quality"
)

// Album statuses.
const (
	StatusDownloaded  = "downloaded"
	StatusPartial     = "partial"
	StatusMissing     = "missing"
	StatusDownloading = "downloading"
	StatusUnreleased  = "unreleased"
	StatusUnmonitored = "unmonitored"
	StatusCutoffUnmet = "cutoff_unmet"
)

// Download is an album's active download.
type Download struct {
	ID       int64
	Title    string
	Progress float64
	State    string
	Status   string
}

// AlbumState is an album with what the library has of it.
type AlbumState struct {
	Album
	Release    *Release
	TrackCount int
	FileCount  int
	Size       int64
	// Quality is the worst file quality (the album is as good as its worst track).
	Quality   string
	CutoffMet bool
	Available bool
	Download  *Download
	ArtistName string
	ArtistMonitored bool
}

// Complete reports a file for every track.
func (st *AlbumState) Complete() bool { return st.TrackCount > 0 && st.FileCount >= st.TrackCount }

// Status is the album's library status.
func (st *AlbumState) Status() string {
	switch {
	case st.Complete():
		return StatusDownloaded
	case st.Download != nil && st.Download.Status == "active":
		return StatusDownloading
	case st.FileCount > 0:
		return StatusPartial
	case !st.Monitored:
		return StatusUnmonitored
	case !st.Available:
		return StatusUnreleased
	}
	return StatusMissing
}

// worstQuality returns the lowest-ranked quality of the files under p.
func worstQuality(p *quality.Profile, files []TrackFile) string {
	worst, worstPos := "", -2
	for _, f := range files {
		pos, _ := p.Position(f.Quality)
		if pos < 0 {
			pos = len(p.Items) + 1
		}
		if pos > worstPos {
			worst, worstPos = f.Quality, pos
		}
	}
	return worst
}

// cutoffMet: every file at or above the profile's cutoff.
func cutoffMet(p *quality.Profile, q string) bool {
	if q == "" {
		return false
	}
	if !p.UpgradesAllowed {
		return true
	}
	pos, _ := p.Position(q)
	if pos < 0 {
		return false
	}
	cut := p.CutoffPosition()
	return cut < 0 || pos <= cut
}

func (s *Service) download(ctx context.Context, ref string) *Download {
	var d Download
	err := s.DB.R.QueryRowContext(ctx, `SELECT id, title, progress, state, status FROM downloads WHERE json_extract(item_ref, '$.ref') = ?
		AND status IN ('active', 'completed') ORDER BY added_at DESC LIMIT 1`, ref).Scan(&d.ID, &d.Title, &d.Progress, &d.State, &d.Status)
	if err != nil {
		return nil
	}
	return &d
}

// State computes an album's state (profile may be nil: it is loaded).
func (s *Service) State(ctx context.Context, al Album, a *Artist, p *quality.Profile) (AlbumState, error) {
	st := AlbumState{Album: al, Available: s.released(&al)}
	if a == nil {
		ar, err := s.Artist(ctx, al.ArtistID)
		if err != nil {
			return st, err
		}
		a = &ar
	}
	st.ArtistName, st.ArtistMonitored = a.Name, a.Monitored
	if p == nil {
		prof, err := s.Quality.Profile(ctx, a.QualityProfileID)
		if err != nil {
			return st, err
		}
		prof.Normalize()
		p = &prof
	}
	if al.ReleaseID != nil {
		if r, err := s.ReleaseByID(ctx, *al.ReleaseID); err == nil {
			st.Release = &r
			st.TrackCount = r.TrackCount
		}
	}
	files, err := s.Files(ctx, al.ID)
	if err != nil {
		return st, err
	}
	for _, f := range files {
		st.Size += f.Size
	}
	st.FileCount = len(files)
	if st.Release != nil && st.Release.TracksLoaded {
		tracks, err := s.Tracks(ctx, st.Release.ID)
		if err != nil {
			return st, err
		}
		LinkFiles(tracks, files)
		n := 0
		for _, t := range tracks {
			if t.File != nil {
				n++
			}
		}
		st.FileCount, st.TrackCount = n, len(tracks)
	}
	st.Quality = worstQuality(p, files)
	st.CutoffMet = st.Complete() && cutoffMet(p, st.Quality)
	st.Download = s.download(ctx, al.Ref().String())
	return st, nil
}

// AlbumStates lists an artist's albums with their state.
func (s *Service) AlbumStates(ctx context.Context, a *Artist) ([]AlbumState, error) {
	albums, err := s.Albums(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	p, err := s.Quality.Profile(ctx, a.QualityProfileID)
	if err != nil {
		return nil, err
	}
	p.Normalize()
	out := make([]AlbumState, 0, len(albums))
	for _, al := range albums {
		st, err := s.State(ctx, al, a, &p)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// ArtistStats are an artist's counts for lists.
type ArtistStats struct {
	Albums, MonitoredAlbums, AlbumsWithFiles int
	Files                                    int
	Size                                     int64
	// Cover is the release group of the newest album (its cover stands for the artist).
	CoverGroup string
	LatestDate string
}

// ListItem is an artist in a list.
type ListItem struct {
	Artist
	ArtistStats
}

// ListFilter filters the artist list.
type ListFilter struct {
	ID        int64 // one artist
	Search    string
	Monitored *bool
	Tag       string
	Sort      string // name, added, albums, size
	Desc      bool
	Offset    int
	Limit     int
}

var artistSorts = map[string]string{"name": "i.sort_title", "added": "i.added_at", "albums": "albums", "size": "size"}

// List returns a page of artists with their counts.
func (s *Service) List(ctx context.Context, f ListFilter) ([]ListItem, int, error) {
	where := []string{"i.kind = 'artist'"}
	var args []any
	if q := strings.TrimSpace(f.Search); q != "" {
		where = append(where, "(i.title LIKE ? ESCAPE '\\' OR a.aliases LIKE ? ESCAPE '\\')")
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
		args = append(args, like, like)
	}
	if f.ID > 0 {
		where = append(where, "i.id = ?")
		args = append(args, f.ID)
	}
	if f.Monitored != nil {
		where = append(where, "i.monitored = ?")
		args = append(args, *f.Monitored)
	}
	if t := strings.ToLower(strings.TrimSpace(f.Tag)); t != "" {
		where = append(where, "EXISTS (SELECT 1 FROM json_each(i.tags) WHERE value = ?)")
		args = append(args, t)
	}
	w := strings.Join(where, " AND ")
	var total int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*)`+artistFrom+` WHERE `+w, args...).Scan(&total); err != nil { // #nosec G202 -- constant fragments
		return nil, 0, err
	}
	col, ok := artistSorts[f.Sort]
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
	q := `SELECT ` + artistColumns + `,
		(SELECT COUNT(*) FROM albums x WHERE x.artist_id = i.id) AS albums,
		(SELECT COUNT(*) FROM albums x WHERE x.artist_id = i.id AND x.monitored = 1) AS monitored_albums,
		(SELECT COUNT(DISTINCT af.album_id) FROM albums x JOIN album_files af ON af.album_id = x.id WHERE x.artist_id = i.id) AS with_files,
		(SELECT COUNT(*) FROM media_files mf WHERE mf.item_id = i.id) AS files,
		(SELECT COALESCE(SUM(mf.size), 0) FROM media_files mf WHERE mf.item_id = i.id) AS size,
		(SELECT x.mbid FROM albums x WHERE x.artist_id = i.id AND x.release_date <> '' AND x.release_date <= date('now') ORDER BY x.release_date DESC LIMIT 1),
		(SELECT MAX(x.release_date) FROM albums x WHERE x.artist_id = i.id AND x.release_date <= date('now'))` +
		artistFrom + ` WHERE ` + w + ` ORDER BY ` + col + ` ` + dir + `, i.id LIMIT ? OFFSET ?` // #nosec G202 -- whitelisted sort column
	rows, err := s.DB.R.QueryContext(ctx, q, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ListItem{}
	for rows.Next() {
		var (
			it          ListItem
			cover, last sql.NullString
		)
		dest := []any{}
		var a Artist
		var aliases, genres, tagsJS string
		var added, updated int64
		var refreshed, root sql.NullInt64
		dest = append(dest, &a.ID, &a.MBID, &a.Name, &a.SortName, &a.Disambiguation, &a.Type, &a.Country, &aliases, &genres, &a.Monitored,
			&a.MonitorNewAlbums, &a.QualityProfileID, &a.MetadataProfileID, &root, &a.Path, &tagsJS, &added, &updated, &refreshed,
			&it.Albums, &it.MonitoredAlbums, &it.AlbumsWithFiles, &it.Files, &it.Size, &cover, &last)
		if err := rows.Scan(dest...); err != nil {
			return nil, 0, err
		}
		a.Aliases, a.Genres, a.Tags = jsonList(aliases), jsonList(genres), jsonList(tagsJS)
		if root.Valid {
			v := root.Int64
			a.RootFolderID = &v
		}
		a.AddedAt, a.UpdatedAt, a.RefreshedAt = db.Time(added), db.Time(updated), db.NullTime(refreshed)
		it.Artist, it.CoverGroup, it.LatestDate = a, cover.String, last.String
		out = append(out, it)
	}
	return out, total, rows.Err()
}

// Stats returns one artist's counts.
func (s *Service) Stats(ctx context.Context, id int64) (ArtistStats, error) {
	list, _, err := s.List(ctx, ListFilter{ID: id, Limit: 1})
	if err != nil {
		return ArtistStats{}, err
	}
	if len(list) == 0 {
		return ArtistStats{}, ErrNotFound
	}
	return list[0].ArtistStats, nil
}
