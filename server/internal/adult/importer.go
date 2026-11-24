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

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/decision"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/importer"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// ImportError is an import refused with a code the explainer and the web
// translate (blocked_tag, not_an_upgrade, no_video, disabled).
type ImportError struct {
	Code string
	Msg  string
}

func (e *ImportError) Error() string { return e.Msg }

func refuse(code, format string, args ...any) error {
	return &ImportError{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// Tokens of the adult naming templates (D-103).
var Tokens = []naming.Token{{Name: "Studio", Example: "Studio A"}, {Name: "Date", Example: "2024-01-15"}, {Name: "Title", Example: "Scene Title"},
	{Name: "Year", Example: "2024"}, {Name: "Performers", Example: "Performer One, Performer Two"}, {Name: "Quality", Example: "WEBDL-1080p"},
	{Name: "ReleaseGroup", Example: "GRP"}}

// FileValues are the naming values of an item with a file of quality q.
func FileValues(it *Item, q, group string) naming.Values {
	var perfs []string
	for _, p := range it.Performers {
		perfs = append(perfs, p.Name)
	}
	return naming.Values{"Studio": it.Studio, "Date": it.ReleaseDate, "Title": it.Title, "Year": yearStr(it.Year),
		"Performers": strings.Join(perfs, ", "), "Quality": q, "ReleaseGroup": group}
}

func (s *Service) fileName(ctx context.Context, it *Item, q, group string) (string, error) {
	tmpl, err := settings.Get(ctx, s.Settings, SceneFile)
	if it.Kind == KindMovie {
		tmpl, err = settings.Get(ctx, s.Settings, MovieFile)
	}
	if err != nil {
		return "", err
	}
	colon, _ := settings.Get(ctx, s.Settings, library.ColonReplacement)
	name, err := naming.Render(tmpl, FileValues(it, q, group), naming.Options{Colon: colon})
	if err != nil {
		return "", err
	}
	return naming.Truncate(name, 200), nil
}

// ImportDownload imports a completed download grabbed for a scene or movie.
// It always ends with MarkImported or MarkImportFailed.
func (s *Service) ImportDownload(ctx context.Context, downloadID int64) error {
	d, err := s.Downloads.Get(ctx, downloadID)
	if err != nil {
		return err
	}
	if d.Status != downloads.StatusCompleted {
		return nil
	}
	ref := library.RefFromJSON(d.ItemRef)
	var it *Item
	fail := func(err error) error {
		data := map[string]any{"download": d.Title, "path": d.LocalPath}
		var ie *ImportError
		if errors.As(err, &ie) {
			data["code"] = ie.Code
		}
		if it != nil {
			s.record(ctx, it.Ref().String(), timeline.TypeImportFailed, err.Error(), data, &d.ID)
		}
		return s.Downloads.MarkImportFailed(ctx, d.ID, err.Error())
	}
	if !s.On(ctx) {
		return fail(refuse("disabled", "the adult library is disabled"))
	}
	id, ok := ItemID(ref)
	if !ok {
		return fail(refuse("no_item", "the download is not linked to an adult item"))
	}
	item, err := s.Item(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return fail(refuse("no_item", "the item was removed from the library"))
	}
	if err != nil {
		return err
	}
	it = &item
	extras, _ := settings.Get(ctx, s.Settings, library.ExtraExtensions)
	disc, err := importer.Discover(d.LocalPath, extras)
	if err != nil {
		return fail(fmt.Errorf("cannot read %s: %w", d.LocalPath, err))
	}
	main, ok := disc.MainVideo()
	if !ok {
		return fail(refuse("no_video", "no video file in %s", d.LocalPath))
	}
	var sidecars []string
	for _, sc := range disc.Sidecars {
		sidecars = append(sidecars, sc.Path)
	}
	res, err := s.ImportFile(ctx, it, main.Path, d.Title, sidecars, &d.ID)
	if err != nil {
		return fail(err)
	}
	return s.Downloads.MarkImported(ctx, d.ID, "imported to "+res)
}

// ImportFile places one file for an item: blocklist, upgrade check,
// transfer, rename, recycle the old file, timeline.
func (s *Service) ImportFile(ctx context.Context, it *Item, path, release string, sidecars []string, dl *int64) (string, error) {
	src, err := lfs.MustExist(s.DataRoots, path)
	if err != nil {
		return "", err
	}
	if term, ok := s.BlockedText(ctx, release, filepath.Base(src)); ok {
		return "", refuse(decision.CodeBlockedTag, "not imported: the name contains the blocked term %q", term)
	}
	name := release
	if name == "" {
		name = filepath.Base(src)
	}
	rel := parser.Parse(name)
	if rel.Video.Resolution == 0 {
		rel = parser.Parse(filepath.Base(src))
	}
	q := quality.Identify(&rel, quality.Movies)
	p, err := s.Quality.Profile(ctx, it.QualityProfileID)
	if err != nil {
		return "", err
	}
	p.Normalize()
	old := it.File
	if old != nil {
		newPos, _ := p.Position(q.ID)
		oldPos, _ := p.Position(old.Quality)
		if newPos < 0 || (oldPos >= 0 && newPos >= oldPos && !(newPos == oldPos && rel.Revision.Version > old.Revision.Version)) {
			return "", refuse(decision.CodeNotAnUpgrade, "not imported: %s is not better than the current %s", q.Name, old.Quality)
		}
	}
	fname, err := s.fileName(ctx, it, q.Name, rel.Group)
	if err != nil {
		return "", err
	}
	mode, _ := settings.Get(ctx, s.Settings, library.TransferMode)
	ref := it.Ref().String()
	replace := func(p string) error {
		_, err := s.Recycle.Recycle(ctx, p, ref, "replaced by an upgrade")
		return err
	}
	placed, _, err := importer.Place(importer.Placement{Roots: s.DataRoots, Source: src, Sidecars: sidecars, Folder: it.Path, Name: fname,
		Mode: mode, Replace: replace})
	if err != nil {
		return "", err
	}
	if old != nil && old.Path != placed.Path {
		if _, err := os.Stat(old.Path); err == nil {
			_ = replace(old.Path)
		}
	}
	rev, _ := json.Marshal(rel.Revision)
	if rel.Revision.Version == 0 {
		rev = []byte(`{"version":1}`)
	}
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO adult_files (item_id, path, size, quality, revision, score, release_group, scene_name, download_id, added_at)
			VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?, ?) ON CONFLICT (item_id) DO UPDATE SET path = excluded.path, size = excluded.size,
			quality = excluded.quality, revision = excluded.revision, release_group = excluded.release_group, scene_name = excluded.scene_name,
			download_id = excluded.download_id, added_at = excluded.added_at`,
			it.ID, placed.Path, placed.Size, q.ID, string(rev), rel.Group, release, dl, db.Millis(s.now()))
		return err
	})
	if err != nil {
		return "", err
	}
	s.record(ctx, ref, timeline.TypeFileImported, fmt.Sprintf("imported %s (%s, %s)", filepath.Base(placed.Path), q.Name, placed.Mode),
		map[string]any{"source": src, "path": placed.Path, "mode": placed.Mode, "quality": q.Name, "size": placed.Size}, dl)
	if filepath.Base(src) != filepath.Base(placed.Path) {
		s.record(ctx, ref, timeline.TypeRenamed, fmt.Sprintf("renamed %s to %s", filepath.Base(src), filepath.Base(placed.Path)),
			map[string]any{"from": filepath.Base(src), "to": filepath.Base(placed.Path)}, dl)
	}
	action := "imported"
	if old != nil {
		action = "upgraded"
		s.record(ctx, ref, timeline.TypeUpgraded, fmt.Sprintf("upgraded from %s to %s", old.Quality, q.Name), map[string]any{"from": old.Quality, "to": q.Name}, dl)
	}
	s.afterImport(ctx, it, action, placed.Path)
	return placed.Path, nil
}

// afterImport tells channels that opted in and, only when the administrator
// mapped an adult Streamero library, the media server.
func (s *Service) afterImport(ctx context.Context, it *Item, action, path string) {
	if s.Events != nil {
		s.Events(ctx, "adult."+action, it.Title, "", it.Ref().String())
	}
	if on, _ := settings.Get(ctx, s.Settings, NotifyStreamero); on && s.Notifier != nil && s.Notifier.Name() != "" {
		if err := s.Notifier.Notify(ctx, mediaserver.Change{ItemRef: it.Ref().String(), Kind: "adult", Action: action, Title: it.Title,
			Year: it.Year, Paths: []string{path}}); err != nil && s.Log != nil {
			s.Log.Warn("adult media server", "err", err)
		}
	}
}

// RenamePreview is one planned rename.
type RenamePreview struct {
	ItemID int64  `json:"itemId"`
	From   string `json:"from"`
	To     string `json:"to"`
}

// Rename renames an item's file to the current template (inside its folder,
// never over an existing file). With apply false it only previews.
func (s *Service) Rename(ctx context.Context, id int64, apply bool) (RenamePreview, error) {
	it, err := s.Item(ctx, id)
	if err != nil {
		return RenamePreview{}, err
	}
	if it.File == nil {
		return RenamePreview{}, refuse("no_file", "the item has no file")
	}
	qn := it.File.Quality
	if q, ok := quality.ByID(qn); ok {
		qn = q.Name
	}
	name, err := s.fileName(ctx, &it, qn, it.File.ReleaseGroup)
	if err != nil {
		return RenamePreview{}, err
	}
	to := naming.WithExtension(name, strings.TrimPrefix(filepath.Ext(it.File.Path), "."))
	dst, err := lfs.SafePath(s.DataRoots, filepath.Join(it.Path, to))
	if err != nil {
		return RenamePreview{}, err
	}
	pr := RenamePreview{ItemID: it.ID, From: it.File.Path, To: dst}
	if !apply || dst == it.File.Path {
		return pr, nil
	}
	if _, err := lfs.SafePath(s.DataRoots, dst); err != nil {
		return pr, err
	}
	if _, err := os.Stat(dst); err == nil {
		return pr, refuse("exists", "%s already exists", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o775); err != nil { // #nosec G301 -- media folders are shared
		return pr, err
	}
	if err := os.Rename(it.File.Path, dst); err != nil {
		return pr, err
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE adult_files SET path = ? WHERE id = ?`, dst, it.File.ID); err != nil {
		return pr, err
	}
	s.record(ctx, it.Ref().String(), timeline.TypeRenamed, fmt.Sprintf("renamed %s to %s", filepath.Base(pr.From), filepath.Base(dst)),
		map[string]any{"from": filepath.Base(pr.From), "to": filepath.Base(dst)}, nil)
	return pr, nil
}
