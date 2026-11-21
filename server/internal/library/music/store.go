package music

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

type execer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

const artistColumns = `i.id, a.mbid, i.title, a.sort_name, a.disambiguation, a.type, a.country, a.aliases, a.genres, i.monitored,
	a.monitor_new_albums, i.quality_profile_id, a.metadata_profile_id, i.root_folder_id, i.path, i.tags, i.added_at, i.updated_at,
	a.metadata_refreshed_at`

const artistFrom = ` FROM media_items i JOIN artists a ON a.item_id = i.id`

func scanArtist(row interface{ Scan(...any) error }) (Artist, error) {
	var (
		a                       Artist
		aliases, genres, tagsJS string
		added, updated          int64
		refreshed               sql.NullInt64
		root                    sql.NullInt64
	)
	err := row.Scan(&a.ID, &a.MBID, &a.Name, &a.SortName, &a.Disambiguation, &a.Type, &a.Country, &aliases, &genres, &a.Monitored,
		&a.MonitorNewAlbums, &a.QualityProfileID, &a.MetadataProfileID, &root, &a.Path, &tagsJS, &added, &updated, &refreshed)
	if err != nil {
		return a, err
	}
	a.Aliases, a.Genres, a.Tags = jsonList(aliases), jsonList(genres), jsonList(tagsJS)
	if root.Valid {
		v := root.Int64
		a.RootFolderID = &v
	}
	a.AddedAt, a.UpdatedAt, a.RefreshedAt = db.Time(added), db.Time(updated), db.NullTime(refreshed)
	return a, nil
}

// Artist returns one artist.
func (s *Service) Artist(ctx context.Context, id int64) (Artist, error) {
	a, err := scanArtist(s.DB.R.QueryRowContext(ctx, `SELECT `+artistColumns+artistFrom+` WHERE i.id = ? AND i.kind = 'artist'`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// ArtistByMBID returns an artist by MusicBrainz id.
func (s *Service) ArtistByMBID(ctx context.Context, mbid string) (Artist, error) {
	a, err := scanArtist(s.DB.R.QueryRowContext(ctx, `SELECT `+artistColumns+artistFrom+` WHERE a.mbid = ?`, strings.ToLower(mbid)))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Service) artists(ctx context.Context, where string, args ...any) ([]Artist, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+artistColumns+artistFrom+` WHERE i.kind = 'artist' `+where, args...) // #nosec G202 -- constant fragments
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Artist{}
	for rows.Next() {
		a, err := scanArtist(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func insertArtist(ctx context.Context, tx *sql.Tx, a *Artist, now time.Time) error {
	res, err := tx.ExecContext(ctx, `INSERT INTO media_items (kind, title, sort_title, year, monitored, quality_profile_id, root_folder_id, path,
		tags, added_at, updated_at) VALUES ('artist', ?, ?, 0, ?, ?, ?, ?, ?, ?, ?)`,
		a.Name, sortName(a), a.Monitored, a.QualityProfileID, a.RootFolderID, a.Path, jsonText(a.Tags), db.Millis(now), db.Millis(now))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrPathTaken
		}
		return err
	}
	a.ID, _ = res.LastInsertId()
	_, err = tx.ExecContext(ctx, `INSERT INTO artists (item_id, mbid, sort_name, disambiguation, type, country, aliases, genres,
		metadata_profile_id, monitor_new_albums) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.MBID, a.SortName, a.Disambiguation, a.Type, a.Country, jsonText(a.Aliases), jsonText(a.Genres), a.MetadataProfileID, a.MonitorNewAlbums)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrExists
	}
	a.AddedAt, a.UpdatedAt = now, now
	return err
}

func sortName(a *Artist) string {
	if a.SortName != "" {
		return strings.ToLower(a.SortName)
	}
	return strings.ToLower(a.Name)
}

func saveArtist(ctx context.Context, e execer, a *Artist, now time.Time) error {
	if _, err := e.ExecContext(ctx, `UPDATE media_items SET title = ?, sort_title = ?, monitored = ?, quality_profile_id = ?, root_folder_id = ?,
		path = ?, tags = ?, updated_at = ? WHERE id = ?`, a.Name, sortName(a), a.Monitored, a.QualityProfileID, a.RootFolderID, a.Path,
		jsonText(a.Tags), db.Millis(now), a.ID); err != nil {
		return err
	}
	_, err := e.ExecContext(ctx, `UPDATE artists SET sort_name = ?, disambiguation = ?, type = ?, country = ?, aliases = ?, genres = ?,
		metadata_profile_id = ?, monitor_new_albums = ?, metadata_refreshed_at = ? WHERE item_id = ?`, a.SortName, a.Disambiguation, a.Type,
		a.Country, jsonText(a.Aliases), jsonText(a.Genres), a.MetadataProfileID, a.MonitorNewAlbums, nullMillis(a.RefreshedAt), a.ID)
	return err
}

func nullMillis(t *time.Time) any {
	if t == nil {
		return nil
	}
	return db.Millis(*t)
}

const albumColumns = `id, artist_id, mbid, title, sort_title, disambiguation, primary_type, secondary_types, release_date, monitored,
	release_id, release_locked, filtered, path, added_at, updated_at`

func scanAlbum(row interface{ Scan(...any) error }) (Album, error) {
	var (
		a              Album
		sec            string
		rel            sql.NullInt64
		added, updated int64
	)
	err := row.Scan(&a.ID, &a.ArtistID, &a.MBID, &a.Title, &a.SortTitle, &a.Disambiguation, &a.PrimaryType, &sec, &a.ReleaseDate, &a.Monitored,
		&rel, &a.ReleaseLocked, &a.Filtered, &a.Path, &added, &updated)
	if err != nil {
		return a, err
	}
	a.SecondaryTypes = jsonList(sec)
	if rel.Valid {
		v := rel.Int64
		a.ReleaseID = &v
	}
	a.AddedAt, a.UpdatedAt = db.Time(added), db.Time(updated)
	return a, nil
}

// Album returns one album.
func (s *Service) Album(ctx context.Context, id int64) (Album, error) {
	a, err := scanAlbum(s.DB.R.QueryRowContext(ctx, `SELECT `+albumColumns+` FROM albums WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Service) albums(ctx context.Context, where string, args ...any) ([]Album, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+albumColumns+` FROM albums `+where, args...) // #nosec G202 -- constant fragments
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Album{}
	for rows.Next() {
		a, err := scanAlbum(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Albums lists an artist's albums (newest first).
func (s *Service) Albums(ctx context.Context, artistID int64) ([]Album, error) {
	return s.albums(ctx, `WHERE artist_id = ? ORDER BY release_date DESC, sort_title`, artistID)
}

const releaseColumns = `id, album_id, mbid, title, status, release_date, country, disambiguation, label, catalog_number, barcode, formats,
	track_count, media_count, tracks_loaded`

func scanRelease(row interface{ Scan(...any) error }) (Release, error) {
	var (
		r       Release
		formats string
	)
	err := row.Scan(&r.ID, &r.AlbumID, &r.MBID, &r.Title, &r.Status, &r.Date, &r.Country, &r.Disambiguation, &r.Label, &r.CatalogNumber,
		&r.Barcode, &formats, &r.TrackCount, &r.MediaCount, &r.TracksLoaded)
	r.Formats = jsonList(formats)
	return r, err
}

// Releases lists an album's editions.
func (s *Service) Releases(ctx context.Context, albumID int64) ([]Release, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+releaseColumns+` FROM album_releases WHERE album_id = ? ORDER BY release_date, id`, albumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Release{}
	for rows.Next() {
		r, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReleaseByID returns one edition.
func (s *Service) ReleaseByID(ctx context.Context, id int64) (Release, error) {
	r, err := scanRelease(s.DB.R.QueryRowContext(ctx, `SELECT `+releaseColumns+` FROM album_releases WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// Tracks lists a release's tracks (without files).
func (s *Service) Tracks(ctx context.Context, releaseID int64) ([]Track, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, release_id, mbid, recording_mbid, disc, position, number, title, length_ms, artist
		FROM tracks WHERE release_id = ? ORDER BY disc, position`, releaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Track{}
	for rows.Next() {
		var t Track
		if err := rows.Scan(&t.ID, &t.ReleaseID, &t.MBID, &t.RecordingMBID, &t.Disc, &t.Position, &t.Number, &t.Title, &t.LengthMs, &t.Artist); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

const fileColumns = `f.id, af.album_id, f.path, f.size, f.quality, f.score, f.release_group, f.scene_name, f.download_id, f.added_at,
	af.disc, af.position, af.recording_mbid, af.title, af.duration_ms, af.format, af.bit_depth, af.sample_rate, af.bitrate`

func scanFile(row interface{ Scan(...any) error }) (TrackFile, error) {
	var (
		f     TrackFile
		dl    sql.NullInt64
		added int64
	)
	err := row.Scan(&f.ID, &f.AlbumID, &f.Path, &f.Size, &f.Quality, &f.Score, &f.ReleaseGroup, &f.SceneName, &dl, &added, &f.Disc, &f.Position,
		&f.RecordingMBID, &f.Title, &f.DurationMs, &f.Format, &f.BitDepth, &f.SampleRate, &f.Bitrate)
	if dl.Valid {
		v := dl.Int64
		f.DownloadID = &v
	}
	f.AddedAt = db.Time(added)
	return f, err
}

// Files lists an album's files.
func (s *Service) Files(ctx context.Context, albumID int64) ([]TrackFile, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+fileColumns+` FROM album_files af JOIN media_files f ON f.id = af.file_id
		WHERE af.album_id = ? ORDER BY af.disc, af.position`, albumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrackFile{}
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// LinkFiles puts each file on its track: by recording id first, then by
// disc and position. Files without a track stay in the returned extras.
func LinkFiles(tracks []Track, files []TrackFile) []TrackFile {
	used := make([]bool, len(files))
	for i := range tracks {
		tracks[i].File = nil
	}
	for i := range tracks {
		if tracks[i].RecordingMBID == "" {
			continue
		}
		for j := range files {
			if !used[j] && files[j].RecordingMBID == tracks[i].RecordingMBID {
				f := files[j]
				tracks[i].File, used[j] = &f, true
				break
			}
		}
	}
	for i := range tracks {
		if tracks[i].File != nil {
			continue
		}
		for j := range files {
			if !used[j] && files[j].Disc == tracks[i].Disc && files[j].Position == tracks[i].Position &&
				(files[j].RecordingMBID == "" || tracks[i].RecordingMBID == "" || files[j].RecordingMBID == tracks[i].RecordingMBID) {
				f := files[j]
				tracks[i].File, used[j] = &f, true
				break
			}
		}
	}
	var extra []TrackFile
	for j := range files {
		if !used[j] {
			extra = append(extra, files[j])
		}
	}
	return extra
}
