package adult

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/adult/meta"
	"github.com/mmzlopasa/locarr/server/internal/db"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// RootFolder is an adult root folder.
type RootFolder struct {
	ID   int64  `json:"id"`
	Path string `json:"path"`
}

// RootFolders lists the adult root folders.
func (s *Service) RootFolders(ctx context.Context) ([]RootFolder, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, path FROM adult_root_folders ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RootFolder{}
	for rows.Next() {
		var r RootFolder
		if err := rows.Scan(&r.ID, &r.Path); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddRootFolder creates (inside the data roots) and records a folder.
func (s *Service) AddRootFolder(ctx context.Context, path string) (RootFolder, error) {
	p, err := lfs.SafePath(s.DataRoots, path)
	if err != nil {
		return RootFolder{}, err
	}
	if err := os.MkdirAll(p, 0o775); err != nil { // #nosec G301 -- media folders are shared with the media server
		return RootFolder{}, err
	}
	r := RootFolder{Path: p}
	err = s.DB.W.QueryRowContext(ctx, `INSERT INTO adult_root_folders (path, created_at) VALUES (?, ?) RETURNING id`, p, db.Millis(s.now())).Scan(&r.ID)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return r, ErrExists
	}
	return r, err
}

// DeleteRootFolder forgets a root folder (files stay).
func (s *Service) DeleteRootFolder(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM adult_root_folders WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) rootPath(ctx context.Context, id int64) (string, error) {
	var p string
	err := s.DB.R.QueryRowContext(ctx, `SELECT path FROM adult_root_folders WHERE id = ?`, id).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoRoot
	}
	return p, err
}

// AddOptions are the choices made on Add.
type AddOptions struct {
	Source           string
	ID               string
	QualityProfileID int64
	RootFolderID     int64
	Monitored        bool
	Search           bool
}

func (s *Service) defaults(ctx context.Context, o *AddOptions) error {
	if o.QualityProfileID == 0 {
		var err error
		if o.QualityProfileID, err = getInt(ctx, s, DefaultProfile); err != nil {
			return err
		}
	}
	if o.RootFolderID == 0 {
		o.RootFolderID, _ = getInt(ctx, s, DefaultRoot)
	}
	if o.RootFolderID == 0 {
		roots, err := s.RootFolders(ctx)
		if err != nil {
			return err
		}
		if len(roots) == 0 {
			return ErrNoRoot
		}
		o.RootFolderID = roots[0].ID
	}
	if o.QualityProfileID == 0 {
		_ = s.DB.R.QueryRowContext(ctx, `SELECT id FROM quality_profiles WHERE kind = 'movies' ORDER BY id LIMIT 1`).Scan(&o.QualityProfileID)
	}
	var n int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM quality_profiles WHERE id = ? AND kind = 'movies'`, o.QualityProfileID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNoProfile
	}
	return nil
}

// checkEntry refuses removed/disputed entries and blocked tags (D-103).
func (s *Service) checkEntry(ctx context.Context, sc *meta.Scene) error {
	if sc.Removed {
		return ErrRemoved
	}
	texts := append([]string{sc.Title}, sc.Tags...)
	if term, ok := s.BlockedText(ctx, texts...); ok {
		return fmt.Errorf("%w (%q)", ErrBlocked, term)
	}
	return nil
}

func folderName(tmpl string, v naming.Values) string {
	name, err := naming.Render(tmpl, v, naming.Options{})
	if err != nil || strings.TrimSpace(name) == "" {
		return "Unknown"
	}
	return name
}

// AddStudio adds a studio from a metadata source.
func (s *Service) AddStudio(ctx context.Context, o AddOptions) (Studio, error) {
	p, err := s.Provider(o.Source)
	if err != nil {
		return Studio{}, err
	}
	if err := s.defaults(ctx, &o); err != nil {
		return Studio{}, err
	}
	ms, err := p.Studio(ctx, o.ID)
	if errors.Is(err, meta.ErrRemoved) {
		return Studio{}, ErrRemoved
	}
	if err != nil {
		return Studio{}, err
	}
	if term, ok := s.BlockedText(ctx, ms.Name); ok {
		return Studio{}, fmt.Errorf("%w (%q)", ErrBlocked, term)
	}
	root, err := s.rootPath(ctx, o.RootFolderID)
	if err != nil {
		return Studio{}, err
	}
	path, err := s.folder(root, naming.Sanitize(ms.Name, naming.Options{}))
	if err != nil {
		return Studio{}, err
	}
	stash, tpdb := ids(ms.Source, ms.ID)
	img := s.cacheImage(ctx, ms.Image)
	now := db.Millis(s.now())
	var id int64
	err = s.DB.W.QueryRowContext(ctx, `INSERT INTO adult_studios (name, sort_name, stashdb_id, tpdb_id, url, image, monitored, monitor_since,
		quality_profile_id, root_folder_id, path, added_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		ms.Name, sortName(ms.Name), stash, tpdb, ms.URL, img, o.Monitored, s.now().UTC().Format("2006-01-02"), o.QualityProfileID,
		o.RootFolderID, path, now, now).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Studio{}, ErrExists
		}
		return Studio{}, err
	}
	if _, err := s.RefreshStudio(ctx, id); err != nil && s.Log != nil {
		s.Log.Warn("refresh studio", "studio", id, "err", err)
	}
	return s.Studio(ctx, id)
}

func ids(source, id string) (stash, tpdb string) {
	if source == meta.TPDB {
		return "", id
	}
	return id, ""
}

// RefreshStudio pulls the studio's newest scenes; scenes released on or
// after the studio's monitor date are monitored (when the studio is).
func (s *Service) RefreshStudio(ctx context.Context, id int64) (int, error) {
	st, err := s.Studio(ctx, id)
	if err != nil {
		return 0, err
	}
	source, sid := meta.StashDB, st.StashDBID
	if sid == "" {
		source, sid = meta.TPDB, st.TPDBID
	}
	p, err := s.Provider(source)
	if err != nil {
		return 0, err
	}
	list, err := p.StudioScenes(ctx, sid, 1)
	if err != nil {
		return 0, err
	}
	added := 0
	for i := range list {
		sc := &list[i]
		mon := st.Monitored && (st.MonitorSince == "" || sc.Date >= st.MonitorSince)
		root := int64(0)
		if st.RootFolderID != nil {
			root = *st.RootFolderID
		}
		_, err := s.addItem(ctx, sc, AddOptions{Source: source, QualityProfileID: st.QualityProfileID, RootFolderID: root, Monitored: mon}, &st)
		switch {
		case err == nil:
			added++
		case errors.Is(err, ErrExists), errors.Is(err, ErrRemoved), errors.Is(err, ErrBlocked):
		default:
			return added, err
		}
	}
	_, _ = s.DB.W.ExecContext(ctx, `UPDATE adult_studios SET refreshed_at = ? WHERE id = ?`, db.Millis(s.now()), id)
	return added, nil
}

// AddItem adds one scene or movie from a source.
func (s *Service) AddItem(ctx context.Context, movie bool, o AddOptions) (Item, error) {
	p, err := s.Provider(o.Source)
	if err != nil {
		return Item{}, err
	}
	if err := s.defaults(ctx, &o); err != nil {
		return Item{}, err
	}
	var sc meta.Scene
	if movie {
		sc, err = p.Movie(ctx, o.ID)
		sc.Movie = true
	} else {
		sc, err = p.Scene(ctx, o.ID)
	}
	if err != nil {
		return Item{}, err
	}
	it, err := s.addItem(ctx, &sc, o, nil)
	if err != nil {
		return Item{}, err
	}
	if o.Search && it.Monitored && s.Wanted != nil {
		_ = s.Wanted.Enqueue(ctx, it.Ref(), "added")
	}
	return it, nil
}

// addItem stores a scene or movie (with its studio and performers).
func (s *Service) addItem(ctx context.Context, sc *meta.Scene, o AddOptions, studio *Studio) (Item, error) {
	if err := s.checkEntry(ctx, sc); err != nil {
		return Item{}, err
	}
	stash, tpdb := ids(sc.Source, sc.ID)
	var exists int
	_ = s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM adult_items WHERE (stashdb_id <> '' AND stashdb_id = ?) OR (tpdb_id <> '' AND tpdb_id = ?)`,
		stash, tpdb).Scan(&exists)
	if exists > 0 {
		return Item{}, ErrExists
	}
	kind := KindScene
	if sc.Movie {
		kind = KindMovie
	}
	studioName := ""
	if sc.Studio != nil {
		studioName = sc.Studio.Name
	}
	var studioID any
	if studio != nil {
		studioID, studioName = studio.ID, studio.Name
	} else if sc.Studio != nil {
		var sid int64
		s1, s2 := ids(sc.Studio.Source, sc.Studio.ID)
		if s.DB.R.QueryRowContext(ctx, `SELECT id FROM adult_studios WHERE (stashdb_id <> '' AND stashdb_id = ?) OR (tpdb_id <> '' AND tpdb_id = ?)`,
			s1, s2).Scan(&sid) == nil {
			studioID = sid
		}
	}
	root, err := s.rootPath(ctx, o.RootFolderID)
	if err != nil {
		return Item{}, err
	}
	v := naming.Values{"Studio": studioName, "Title": sc.Title, "Year": yearStr(sc.Year()), "Date": sc.Date}
	tmpl, _ := getStr(ctx, s, SceneFolder)
	if sc.Movie {
		tmpl, _ = getStr(ctx, s, MovieFolder)
	}
	path, err := s.folder(root, folderName(tmpl, v))
	if err != nil {
		return Item{}, err
	}
	tags, _ := json.Marshal(nonNil(sc.Tags))
	img := s.cacheImage(ctx, sc.Image)
	now := db.Millis(s.now())
	var id int64
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO adult_items (kind, studio_id, studio_name, title, sort_title, release_date, year, duration,
			overview, stashdb_id, tpdb_id, phash, source_tags, image, monitored, quality_profile_id, root_folder_id, path, added_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`, kind, studioID, studioName, sc.Title, sortName(sc.Title),
			sc.Date, sc.Year(), sc.Duration, sc.Overview, stash, tpdb, sc.PHash, string(tags), img, o.Monitored, o.QualityProfileID,
			o.RootFolderID, path, now, now).Scan(&id); err != nil {
			return err
		}
		for _, p := range sc.Performers {
			pid, err := s.ensurePerformer(ctx, tx, &p)
			if err != nil {
				return err
			}
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
	s.record(ctx, it.Ref().String(), timeline.TypeAdded, "added "+it.Title, map[string]any{"source": sc.Source}, nil)
	return it, nil
}

func (s *Service) ensurePerformer(ctx context.Context, tx *sql.Tx, p *meta.Performer) (int64, error) {
	stash, tpdb := ids(p.Source, p.ID)
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM adult_performers WHERE (stashdb_id <> '' AND stashdb_id = ?) OR (tpdb_id <> '' AND tpdb_id = ?)`,
		stash, tpdb).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	now := db.Millis(s.now())
	err = tx.QueryRowContext(ctx, `INSERT INTO adult_performers (name, sort_name, disambiguation, stashdb_id, tpdb_id, image, monitored, added_at, updated_at)
		VALUES (?, ?, ?, ?, ?, '', 0, ?, ?) RETURNING id`, p.Name, sortName(p.Name), p.Disambiguation, stash, tpdb, now, now).Scan(&id)
	return id, err
}

// AddPerformer adds (or monitors) a performer from a source.
func (s *Service) AddPerformer(ctx context.Context, o AddOptions) (Performer, error) {
	p, err := s.Provider(o.Source)
	if err != nil {
		return Performer{}, err
	}
	if err := s.defaults(ctx, &o); err != nil {
		return Performer{}, err
	}
	mp, err := p.Performer(ctx, o.ID)
	if errors.Is(err, meta.ErrRemoved) {
		return Performer{}, ErrRemoved
	}
	if err != nil {
		return Performer{}, err
	}
	img := s.cacheImage(ctx, mp.Image)
	var id int64
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		var e error
		if id, e = s.ensurePerformer(ctx, tx, &mp); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE adult_performers SET monitored = ?, monitor_since = ?, quality_profile_id = ?, root_folder_id = ?,
			image = CASE WHEN ? <> '' THEN ? ELSE image END, updated_at = ? WHERE id = ?`, o.Monitored, s.now().UTC().Format("2006-01-02"),
			o.QualityProfileID, o.RootFolderID, img, img, db.Millis(s.now()), id)
		return e
	})
	if err != nil {
		return Performer{}, err
	}
	if o.Monitored {
		if _, err := s.RefreshPerformer(ctx, id); err != nil && s.Log != nil {
			s.Log.Warn("refresh performer", "performer", id, "err", err)
		}
	}
	return s.Performer(ctx, id)
}

// RefreshPerformer pulls a monitored performer's newest scenes; new ones
// released since monitoring started become wanted.
func (s *Service) RefreshPerformer(ctx context.Context, id int64) (int, error) {
	pf, err := s.Performer(ctx, id)
	if err != nil {
		return 0, err
	}
	if !pf.Monitored {
		return 0, nil
	}
	source, pid := meta.StashDB, pf.StashDBID
	if pid == "" {
		source, pid = meta.TPDB, pf.TPDBID
	}
	p, err := s.Provider(source)
	if err != nil {
		return 0, err
	}
	list, err := p.PerformerScenes(ctx, pid, 1)
	if err != nil {
		return 0, err
	}
	root := int64(0)
	if pf.RootFolderID != nil {
		root = *pf.RootFolderID
	}
	o := AddOptions{Source: source, QualityProfileID: pf.QualityProfileID, RootFolderID: root}
	if err := s.defaults(ctx, &o); err != nil {
		return 0, err
	}
	added := 0
	for i := range list {
		sc := &list[i]
		o.Monitored = pf.MonitorSince == "" || sc.Date >= pf.MonitorSince
		it, err := s.addItem(ctx, sc, o, nil)
		switch {
		case err == nil:
			added++
			if it.Monitored && s.Wanted != nil {
				_ = s.Wanted.Enqueue(ctx, it.Ref(), "added")
			}
		case errors.Is(err, ErrExists), errors.Is(err, ErrRemoved), errors.Is(err, ErrBlocked):
		default:
			return added, err
		}
	}
	_, _ = s.DB.W.ExecContext(ctx, `UPDATE adult_performers SET refreshed_at = ? WHERE id = ?`, db.Millis(s.now()), id)
	return added, nil
}

// RefreshAll refreshes monitored studios and performers (scheduled).
func (s *Service) RefreshAll(ctx context.Context) error {
	if !s.On(ctx) {
		return nil
	}
	studios, err := s.Studios(ctx)
	if err != nil {
		return err
	}
	for _, st := range studios {
		if st.Monitored {
			if _, err := s.RefreshStudio(ctx, st.ID); err != nil && s.Log != nil {
				s.Log.Warn("refresh studio", "studio", st.ID, "err", err)
			}
		}
	}
	perfs, err := s.Performers(ctx)
	if err != nil {
		return err
	}
	for _, p := range perfs {
		if p.Monitored {
			if _, err := s.RefreshPerformer(ctx, p.ID); err != nil && s.Log != nil {
				s.Log.Warn("refresh performer", "performer", p.ID, "err", err)
			}
		}
	}
	return nil
}

// Edit changes what can be changed on an item, studio or performer.
type Edit struct {
	Monitored        *bool
	QualityProfileID *int64
}

// EditItem changes an item.
func (s *Service) EditItem(ctx context.Context, id int64, e Edit) (Item, error) {
	it, err := s.Item(ctx, id)
	if err != nil {
		return it, err
	}
	if e.Monitored != nil {
		_, _ = s.DB.W.ExecContext(ctx, `UPDATE adult_items SET monitored = ?, updated_at = ? WHERE id = ?`, *e.Monitored, db.Millis(s.now()), id)
	}
	if e.QualityProfileID != nil {
		o := AddOptions{QualityProfileID: *e.QualityProfileID, RootFolderID: -1}
		var n int
		_ = s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM quality_profiles WHERE id = ?`, o.QualityProfileID).Scan(&n)
		if n == 0 {
			return it, ErrNoProfile
		}
		_, _ = s.DB.W.ExecContext(ctx, `UPDATE adult_items SET quality_profile_id = ?, updated_at = ? WHERE id = ?`, o.QualityProfileID, db.Millis(s.now()), id)
	}
	s.record(ctx, it.Ref().String(), timeline.TypeEdited, "edited", nil, nil)
	return s.Item(ctx, id)
}

// EditStudio changes a studio (monitoring a studio again starts today).
func (s *Service) EditStudio(ctx context.Context, id int64, e Edit) (Studio, error) {
	if _, err := s.Studio(ctx, id); err != nil {
		return Studio{}, err
	}
	if e.Monitored != nil {
		_, _ = s.DB.W.ExecContext(ctx, `UPDATE adult_studios SET monitored = ?, monitor_since = CASE WHEN ? THEN ? ELSE monitor_since END,
			updated_at = ? WHERE id = ?`, *e.Monitored, *e.Monitored, s.now().UTC().Format("2006-01-02"), db.Millis(s.now()), id)
	}
	if e.QualityProfileID != nil {
		_, _ = s.DB.W.ExecContext(ctx, `UPDATE adult_studios SET quality_profile_id = ? WHERE id = ?`, *e.QualityProfileID, id)
	}
	return s.Studio(ctx, id)
}

// EditPerformer changes a performer.
func (s *Service) EditPerformer(ctx context.Context, id int64, e Edit) (Performer, error) {
	if _, err := s.Performer(ctx, id); err != nil {
		return Performer{}, err
	}
	if e.Monitored != nil {
		_, _ = s.DB.W.ExecContext(ctx, `UPDATE adult_performers SET monitored = ?, monitor_since = CASE WHEN ? THEN ? ELSE monitor_since END,
			updated_at = ? WHERE id = ?`, *e.Monitored, *e.Monitored, s.now().UTC().Format("2006-01-02"), db.Millis(s.now()), id)
	}
	if e.QualityProfileID != nil {
		_, _ = s.DB.W.ExecContext(ctx, `UPDATE adult_performers SET quality_profile_id = ? WHERE id = ?`, *e.QualityProfileID, id)
	}
	return s.Performer(ctx, id)
}

// DeleteItem removes an item; with deleteFiles its file goes to the recycle bin.
func (s *Service) DeleteItem(ctx context.Context, id int64, deleteFiles bool) error {
	it, err := s.Item(ctx, id)
	if err != nil {
		return err
	}
	if deleteFiles && it.File != nil && s.Recycle != nil {
		if _, err := s.Recycle.Recycle(ctx, it.File.Path, it.Ref().String(), "deleted"); err != nil {
			return err
		}
	}
	if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM adult_items WHERE id = ?`, id); err != nil {
		return err
	}
	_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM search_state WHERE item_ref = ?`, it.Ref().String())
	_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM pending_releases WHERE item_ref = ?`, it.Ref().String())
	return nil
}

// DeleteStudio removes a studio and its scenes (files go to the recycle bin
// only with deleteFiles).
func (s *Service) DeleteStudio(ctx context.Context, id int64, deleteFiles bool) error {
	if _, err := s.Studio(ctx, id); err != nil {
		return err
	}
	items, _, err := s.Items(ctx, ItemFilter{StudioID: id, Limit: -1})
	if err != nil {
		return err
	}
	for _, it := range items {
		if err := s.DeleteItem(ctx, it.ID, deleteFiles); err != nil {
			return err
		}
	}
	_, err = s.DB.W.ExecContext(ctx, `DELETE FROM adult_studios WHERE id = ?`, id)
	return err
}

// DeletePerformer forgets a performer (their items stay).
func (s *Service) DeletePerformer(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM adult_performers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func yearStr(y int) string {
	if y <= 0 {
		return ""
	}
	return fmt.Sprint(y)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func getStr(ctx context.Context, s *Service, k settings.Key[string]) (string, error) {
	return settings.Get(ctx, s.Settings, k)
}

func getInt(ctx context.Context, s *Service, k settings.Key[int64]) (int64, error) {
	return settings.Get(ctx, s.Settings, k)
}

// folder is a sanitised folder inside a root (inside the data roots).
func (s *Service) folder(root, name string) (string, error) {
	name = naming.Sanitize(name, naming.Options{})
	if name == "" || name == "." || name == ".." {
		name = "Unknown"
	}
	return lfs.SafePath(s.DataRoots, filepath.Join(root, name))
}
