// Package rootfolders keeps the folders where the library lives (one per media
// kind or several), with their free space and whether they can be written.
package rootfolders

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
)

// Kinds of media a root folder holds.
const (
	KindMovies = "movies"
	KindSeries = "series"
	KindMusic  = "music"
)

// Errors.
var (
	ErrExists      = errors.New("this folder is already a root folder")
	ErrNotFound    = errors.New("root folder not found")
	ErrNotDir      = errors.New("path is not a folder")
	ErrInvalidKind = errors.New("unknown media kind")
)

// Folder is a root folder with its current state on disk.
type Folder struct {
	ID         int64
	Path       string
	Kind       string
	Accessible bool
	Writable   bool
	// WriteProblem says why it is not writable (nil when it is).
	WriteProblem *lfs.WriteProblem
	Space        *lfs.Space
	CreatedAt    time.Time
}

// Service manages root folders inside the allowed data roots.
type Service struct {
	DB    *db.DB
	Roots []string // LOCARR_DATA_ROOTS
	Now   func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func inspect(f *Folder) {
	f.Accessible = lfs.IsDir(f.Path)
	if !f.Accessible {
		return
	}
	f.WriteProblem = lfs.CheckWritable(f.Path)
	f.Writable = f.WriteProblem == nil
	if sp, err := lfs.DiskSpace(f.Path); err == nil {
		f.Space = &sp
	}
}

// List returns every root folder with its state on disk.
func (s *Service) List(ctx context.Context) ([]Folder, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, path, kind, created_at FROM root_folders ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Folder{}
	for rows.Next() {
		var (
			f       Folder
			created int64
		)
		if err := rows.Scan(&f.ID, &f.Path, &f.Kind, &created); err != nil {
			return nil, err
		}
		f.CreatedAt = db.Time(created)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		inspect(&out[i])
	}
	return out, nil
}

// Create makes path (inside the data roots) when it does not exist yet and
// adds it as a root folder.
func (s *Service) Create(ctx context.Context, path, kind string) (Folder, error) {
	switch kind {
	case KindMovies, KindSeries, KindMusic:
	default:
		return Folder{}, ErrInvalidKind
	}
	p, err := lfs.SafePath(s.Roots, path)
	if err != nil {
		return Folder{}, err
	}
	if err := os.MkdirAll(p, 0o775); err != nil { // #nosec G301 -- media folders are shared with the media server
		return Folder{}, err
	}
	return s.Add(ctx, p, kind)
}

// Add records an existing folder inside the data roots as a root folder.
func (s *Service) Add(ctx context.Context, path, kind string) (Folder, error) {
	switch kind {
	case KindMovies, KindSeries, KindMusic:
	default:
		return Folder{}, ErrInvalidKind
	}
	p, err := lfs.MustExist(s.Roots, path)
	if err != nil {
		return Folder{}, err
	}
	if st, err := os.Stat(p); err != nil || !st.IsDir() {
		return Folder{}, ErrNotDir
	}
	now := s.now()
	f := Folder{Path: p, Kind: kind, CreatedAt: db.Time(db.Millis(now))}
	err = s.DB.W.QueryRowContext(ctx, `INSERT INTO root_folders (path, kind, created_at) VALUES (?, ?, ?)
		ON CONFLICT (path) DO NOTHING RETURNING id`, p, kind, db.Millis(now)).Scan(&f.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return Folder{}, ErrExists
	}
	if err != nil {
		return Folder{}, err
	}
	inspect(&f)
	return f, nil
}

// Remove forgets a root folder; nothing on disk changes.
func (s *Service) Remove(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM root_folders WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Entry is a folder in a listing.
type Entry struct{ Name, Path string }

// Listing is the content of one folder (or the data roots).
type Listing struct {
	Path    string // empty for the data roots
	Parent  string // empty when the parent is outside the data roots
	Entries []Entry
}

// maxEntries bounds a listing (a folder with a huge number of subfolders).
const maxEntries = 2000

// Browse lists the subfolders of path, or the data roots when path is empty.
// Hidden folders are left out.
func (s *Service) Browse(path string) (Listing, error) {
	if strings.TrimSpace(path) == "" {
		l := Listing{Entries: []Entry{}}
		for _, r := range s.Roots {
			if lfs.IsDir(r) {
				l.Entries = append(l.Entries, Entry{Name: r, Path: r})
			}
		}
		return l, nil
	}
	p, err := lfs.MustExist(s.Roots, path)
	if err != nil {
		return Listing{}, err
	}
	des, err := os.ReadDir(p)
	if err != nil {
		return Listing{}, ErrNotDir
	}
	l := Listing{Path: p, Entries: []Entry{}}
	if parent := filepath.Dir(p); parent != p {
		if sp, err := lfs.SafePath(s.Roots, parent); err == nil {
			l.Parent = sp
		}
	}
	for _, de := range des {
		if strings.HasPrefix(de.Name(), ".") {
			continue
		}
		full := filepath.Join(p, de.Name())
		if !de.IsDir() {
			// A link to a folder inside the roots counts as a folder.
			if de.Type()&os.ModeSymlink == 0 {
				continue
			}
			if st, err := os.Stat(full); err != nil || !st.IsDir() {
				continue
			}
			if _, err := lfs.SafePath(s.Roots, full); err != nil {
				continue
			}
		}
		l.Entries = append(l.Entries, Entry{Name: de.Name(), Path: full})
		if len(l.Entries) >= maxEntries {
			break
		}
	}
	sort.Slice(l.Entries, func(i, j int) bool { return strings.ToLower(l.Entries[i].Name) < strings.ToLower(l.Entries[j].Name) })
	return l, nil
}
