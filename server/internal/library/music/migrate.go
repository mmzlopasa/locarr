package music

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// ImportedArtist is an artist read from another application (P10) with its
// albums (monitoring kept) and track files. It is added without asking
// MusicBrainz; editions and tracks load later (a background refresh).
type ImportedArtist struct {
	MBID              string
	Name              string
	SortName          string
	Disambiguation    string
	Type              string
	Genres            []string
	Monitored         bool
	MonitorNewAlbums  string
	QualityProfileID  int64
	MetadataProfileID int64
	RootFolderID      int64
	Tags              []string
	Path              string
	Albums            []ImportedAlbum
	Note              string
	Data              map[string]any
}

// ImportedAlbum is one release group of the artist.
type ImportedAlbum struct {
	MBID           string
	Title          string
	Disambiguation string
	PrimaryType    string
	SecondaryTypes []string
	ReleaseDate    string
	Monitored      bool
	Files          []ImportedTrackFile
}

// ImportedTrackFile is a track file the other application had.
type ImportedTrackFile struct {
	Path          string
	Quality       string // Locarr quality id
	Disc          int
	Position      int
	RecordingMBID string
	Title         string
	DurationMS    int
	ReleaseGroup  string
	SceneName     string
}

// ArtistOutcome is what happened to one imported artist.
type ArtistOutcome struct {
	ID      int64
	Created bool
	Albums  int
	Files   int
	Missing []string
	Err     error
}

// ImportArtist adds an artist from another application or merges it into
// the one already in the library (same MusicBrainz id): new albums are
// added, files are linked to albums that have none. Files stay in place.
func (s *Service) ImportArtist(ctx context.Context, in ImportedArtist) ArtistOutcome {
	var o ArtistOutcome
	in.MBID = strings.ToLower(strings.TrimSpace(in.MBID))
	if in.MBID == "" {
		o.Err = invalid("mbid is required")
		return o
	}
	now := s.now()
	a, err := s.ArtistByMBID(ctx, in.MBID)
	switch {
	case err == nil:
		o.ID = a.ID
	case errors.Is(err, ErrNotFound):
		root, err := s.rootFolder(ctx, in.RootFolderID)
		if err != nil {
			o.Err = err
			return o
		}
		if err := s.checkQualityProfile(ctx, in.QualityProfileID); err != nil {
			o.Err = err
			return o
		}
		if _, err := s.Profile(ctx, in.MetadataProfileID); err != nil {
			o.Err = err
			return o
		}
		if !validMonitor(in.MonitorNewAlbums, false) {
			in.MonitorNewAlbums = MonitorAll
		}
		a = Artist{MBID: in.MBID, Name: strings.TrimSpace(in.Name), SortName: in.SortName, Disambiguation: in.Disambiguation,
			Type: in.Type, Aliases: []string{}, Genres: in.Genres, Monitored: in.Monitored, MonitorNewAlbums: in.MonitorNewAlbums,
			QualityProfileID: in.QualityProfileID, MetadataProfileID: in.MetadataProfileID, RootFolderID: &root.ID, Tags: quality.NormTags(in.Tags)}
		if a.Name == "" {
			a.Name = in.MBID
		}
		if a.Genres == nil {
			a.Genres = []string{}
		}
		if a.Path, err = s.insidePath(root.Path, in.Path); err != nil {
			o.Err = err
			return o
		}
		note := in.Note
		if note == "" {
			note = "imported from another application"
		}
		data := in.Data
		if data == nil {
			data = map[string]any{}
		}
		data["path"] = a.Path
		err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
			if err := insertArtist(ctx, tx, &a, now); err != nil {
				return err
			}
			return timeline.RecordAt(ctx, tx, a.Ref().String(), timeline.TypeMigrated, note, data, now)
		})
		if err != nil {
			o.Err = err
			return o
		}
		o.ID, o.Created = a.ID, true
	default:
		o.Err = err
		return o
	}
	existing, err := s.Albums(ctx, a.ID)
	if err != nil {
		o.Err = err
		return o
	}
	byMBID := map[string]*Album{}
	for i := range existing {
		byMBID[existing[i].MBID] = &existing[i]
	}
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		for _, ia := range in.Albums {
			mbid := strings.ToLower(strings.TrimSpace(ia.MBID))
			if mbid == "" {
				continue
			}
			al := byMBID[mbid]
			if al == nil {
				res, err := tx.ExecContext(ctx, `INSERT INTO albums (artist_id, mbid, title, sort_title, disambiguation, primary_type,
					secondary_types, release_date, monitored, added_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
					ON CONFLICT (mbid) DO NOTHING`, a.ID, mbid, ia.Title, naming.SortTitle(ia.Title), ia.Disambiguation,
					strings.ToLower(ia.PrimaryType), jsonText(lowerAll(ia.SecondaryTypes)), ia.ReleaseDate, ia.Monitored, db.Millis(now), db.Millis(now))
				if err != nil {
					return err
				}
				id, _ := res.LastInsertId()
				if n, _ := res.RowsAffected(); n == 0 {
					continue // belongs to another artist in the library
				}
				al = &Album{ID: id, ArtistID: a.ID, MBID: mbid, Title: ia.Title}
				byMBID[mbid] = al
				o.Albums++
			}
			if len(ia.Files) == 0 {
				continue
			}
			var has int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM album_files WHERE album_id = ?`, al.ID).Scan(&has); err != nil {
				return err
			}
			if has > 0 {
				continue
			}
			n, missing, dir, err := s.linkImportedTracks(ctx, tx, &a, al, ia.Files, now)
			if err != nil {
				return err
			}
			o.Missing = append(o.Missing, missing...)
			if n > 0 {
				o.Files += n
				if _, err := tx.ExecContext(ctx, `UPDATE albums SET path = ? WHERE id = ? AND path = ''`, dir, al.ID); err != nil {
					return err
				}
				if err := timeline.RecordAt(ctx, tx, al.Ref().String(), timeline.TypeFileFound,
					fmt.Sprintf("linked %d files in %s in place (imported)", n, dir), map[string]any{"path": dir, "files": n}, now); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		o.Err = err
		return o
	}
	if o.Created {
		_, _ = s.DB.W.ExecContext(ctx, `UPDATE artists SET metadata_refreshed_at = NULL WHERE item_id = ?`, a.ID)
	}
	return o
}

func lowerAll(list []string) []string {
	out := make([]string, 0, len(list))
	for _, x := range list {
		if x = strings.ToLower(strings.TrimSpace(x)); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// linkImportedTracks records the files on disk; it returns the album folder.
func (s *Service) linkImportedTracks(ctx context.Context, tx *sql.Tx, a *Artist, al *Album, files []ImportedTrackFile, now time.Time) (int, []string, string, error) {
	n, dir := 0, ""
	var missing []string
	for _, f := range files {
		p, err := lfs.SafePath(s.DataRoots, f.Path)
		if err != nil {
			missing = append(missing, f.Path)
			continue
		}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			missing = append(missing, f.Path)
			continue
		}
		q := f.Quality
		if x, ok := quality.ByID(q); !ok || !x.Music {
			q = quality.UnknownMusic
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM media_files WHERE path = ?`, p); err != nil {
			return n, missing, dir, err
		}
		format := strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")
		res, err := tx.ExecContext(ctx, `INSERT INTO media_files (item_id, path, size, quality, release_group, scene_name, media_info, added_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, a.ID, p, st.Size(), q, f.ReleaseGroup, f.SceneName,
			fmt.Sprintf(`{"source":"import","format":%q}`, format), db.Millis(now))
		if err != nil {
			return n, missing, dir, err
		}
		id, _ := res.LastInsertId()
		disc := max(f.Disc, 1)
		if _, err := tx.ExecContext(ctx, `INSERT INTO album_files (file_id, album_id, disc, position, recording_mbid, title, duration_ms, format)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, al.ID, disc, f.Position, strings.ToLower(f.RecordingMBID), f.Title, f.DurationMS, format); err != nil {
			return n, missing, dir, err
		}
		if dir == "" {
			dir = filepath.Dir(p)
		}
		n++
	}
	return n, missing, dir, nil
}

// NeedsRefresh lists the artists (of ids, or all) never refreshed.
func (s *Service) NeedsRefresh(ctx context.Context, ids []int64) ([]int64, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT item_id FROM artists WHERE metadata_refreshed_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	want := map[int64]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if len(want) == 0 || want[id] {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

