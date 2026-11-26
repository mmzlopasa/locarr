package adult

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// GalleryQuality is the quality recorded for a gallery's file row: images
// have no video quality (D-112).
const GalleryQuality = "images"

// Gallery errors.
var (
	ErrGalleryTitle = errors.New("a gallery needs a title (or a scene or performer to take it from)")
	ErrGalleryDate  = errors.New("the date must be YYYY-MM-DD")
	ErrNotScene     = errors.New("only a scene has a photo set")
)

// GalleryOptions create a gallery by hand or from a scene or performer.
// Neither StashDB nor ThePornDB lists image sets (D-112), so a gallery is
// either the photo set of a scene in the library (SceneID: title, studio,
// date and performers come from it), a performer's set (PerformerID with no
// title: the performer's name) or typed in.
type GalleryOptions struct {
	Title            string
	StudioID         int64
	Studio           string
	ReleaseDate      string
	PerformerIDs     []int64
	SceneID          int64
	PerformerID      int64
	QualityProfileID int64
	RootFolderID     int64
	Monitored        bool
	Search           bool
	// lift: added by hand (not by AutoGallery).
	lift bool
}

// AddGalleryByHand is AddGallery for a person's explicit choice.
func (s *Service) AddGalleryByHand(ctx context.Context, o GalleryOptions) (Item, error) {
	o.lift = true
	return s.AddGallery(ctx, o)
}

var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// AddGallery adds a gallery.
func (s *Service) AddGallery(ctx context.Context, o GalleryOptions) (Item, error) {
	ao := AddOptions{QualityProfileID: o.QualityProfileID, RootFolderID: o.RootFolderID, Monitored: o.Monitored}
	var (
		title, studio, date string
		studioID            *int64
		perfs               []int64
		sceneID             *int64
		image, overview     string
	)
	switch {
	case o.SceneID > 0:
		sc, err := s.Item(ctx, o.SceneID)
		if err != nil {
			return Item{}, err
		}
		if sc.Kind != KindScene {
			return Item{}, ErrNotScene
		}
		if o.lift {
			// Adding by hand is an explicit choice: it lifts "not interested".
			s.unexcludeGallery(ctx, sc.StashDBID, sc.TPDBID)
		}
		var n int
		_ = s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM adult_items WHERE kind = 'gallery' AND scene_id = ?`, sc.ID).Scan(&n)
		if n > 0 {
			return Item{}, ErrExists
		}
		title, studio, date, studioID, sceneID, image, overview = sc.Title, sc.Studio, sc.ReleaseDate, sc.StudioID, &sc.ID, sc.Image, sc.Overview
		for _, p := range sc.Performers {
			perfs = append(perfs, p.ID)
		}
		if ao.QualityProfileID == 0 {
			ao.QualityProfileID = sc.QualityProfileID
		}
		if ao.RootFolderID == 0 && sc.RootFolderID != nil {
			ao.RootFolderID = *sc.RootFolderID
		}
	default:
		title, studio, date, perfs = strings.TrimSpace(o.Title), strings.TrimSpace(o.Studio), strings.TrimSpace(o.ReleaseDate), o.PerformerIDs
		if o.StudioID > 0 {
			st, err := s.Studio(ctx, o.StudioID)
			if err != nil {
				return Item{}, err
			}
			studio, studioID, image = st.Name, &st.ID, st.Image
		}
		if o.PerformerID > 0 {
			p, err := s.Performer(ctx, o.PerformerID)
			if err != nil {
				return Item{}, err
			}
			if title == "" {
				title = p.Name
			}
			if image == "" {
				image = p.Image
			}
			perfs = append(perfs, p.ID)
		}
	}
	if title == "" {
		return Item{}, ErrGalleryTitle
	}
	if date != "" && !isoDate.MatchString(date) {
		return Item{}, ErrGalleryDate
	}
	names := []string{}
	for _, id := range perfs {
		if p, err := s.Performer(ctx, id); err == nil {
			names = append(names, p.Name)
		} else {
			return Item{}, err
		}
	}
	if term, _, ok := s.BlockedMatch(ctx, names, title, studio); ok {
		return Item{}, fmt.Errorf("%w (%q)", ErrBlocked, term)
	}
	if err := s.defaults(ctx, &ao); err != nil {
		return Item{}, err
	}
	root, err := s.rootPath(ctx, ao.RootFolderID)
	if err != nil {
		return Item{}, err
	}
	year := 0
	if len(date) == 10 {
		_, _ = fmt.Sscanf(date[:4], "%d", &year)
	}
	tmpl, _ := getStr(ctx, s, GalleryFolder)
	name := folderName(tmpl, naming.Values{"Studio": studio, "Date": date, "Title": title, "Year": yearStr(year),
		"Performers": strings.Join(names, ", "), "Quality": "", "ReleaseGroup": ""})
	path, err := s.uniqueGalleryFolder(ctx, root, naming.Truncate(name, 200))
	if err != nil {
		return Item{}, err
	}
	now := db.Millis(s.now())
	var id int64
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO adult_items (kind, studio_id, studio_name, title, sort_title, release_date, year, overview,
			image, monitored, quality_profile_id, root_folder_id, path, scene_id, added_at, updated_at)
			VALUES ('gallery', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`, studioID, studio, title, sortName(title), date, year,
			overview, image, ao.Monitored, ao.QualityProfileID, ao.RootFolderID, path, sceneID, now, now).Scan(&id); err != nil {
			return err
		}
		for _, pid := range perfs {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO adult_item_performers (item_id, performer_id) VALUES (?, ?)`, id, pid); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Item{}, err
	}
	it, err := s.Item(ctx, id)
	if err != nil {
		return it, err
	}
	src := "manual"
	if sceneID != nil {
		src = "scene"
	}
	s.record(ctx, it.Ref().String(), timeline.TypeAdded, "added "+it.Title, map[string]any{"source": src}, nil)
	if o.Search && it.Monitored && s.Wanted != nil {
		_ = s.Wanted.Enqueue(ctx, it.Ref(), "added")
	}
	return it, nil
}

// uniqueGalleryFolder is a folder in root no other item uses and that does
// not exist yet with other content ("Name (2)" …).
func (s *Service) uniqueGalleryFolder(ctx context.Context, root, name string) (string, error) {
	for i := 1; i < 1000; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%s (%d)", name, i)
		}
		p, err := s.folder(root, n)
		if err != nil {
			return "", err
		}
		var used int
		_ = s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM adult_items WHERE path = ?`, p).Scan(&used)
		if used == 0 && !nonEmptyDir(p) {
			return p, nil
		}
	}
	return "", ErrExists
}

// AutoGallery adds the photo set of a scene that was just added (the
// scene's "also look for the photo set", or a performer who asks for the
// sets of new scenes) unless it was marked "not interested".
func (s *Service) AutoGallery(ctx context.Context, scene *Item, search bool) {
	if scene.Kind != KindScene || s.excludedGallery(ctx, scene.StashDBID, scene.TPDBID) {
		return
	}
	_, err := s.AddGallery(ctx, GalleryOptions{SceneID: scene.ID, Monitored: scene.Monitored, Search: search})
	if err != nil && !errors.Is(err, ErrExists) && s.Log != nil {
		s.Log.Warn("adult gallery for scene", "scene", scene.ID, "err", err)
	}
}

// SetPerformerGalleries turns "also look for the photo sets" of a performer
// on or off.
func (s *Service) SetPerformerGalleries(ctx context.Context, id int64, on bool) error {
	res, err := s.DB.W.ExecContext(ctx, `UPDATE adult_performers SET galleries = ?, updated_at = ? WHERE id = ?`, on, db.Millis(s.now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// setGalleryCover gives a gallery its own cover (instead of the scene's image).
func (s *Service) setGalleryCover(ctx context.Context, id int64, key string) {
	if key == "" {
		return
	}
	_, _ = s.DB.W.ExecContext(ctx, `UPDATE adult_items SET image = ?, updated_at = ? WHERE id = ?`, key, db.Millis(s.now()), id)
}

func (s *Service) unexcludeGallery(ctx context.Context, stash, tpdb string) {
	if stash == "" && tpdb == "" {
		return
	}
	_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM adult_exclusions WHERE kind = 'gallery' AND
		((stashdb_id <> '' AND stashdb_id = ?) OR (tpdb_id <> '' AND tpdb_id = ?))`, stash, tpdb)
}
