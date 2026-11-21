// Package recycle is the recycle bin: files Locarr replaces or the user
// deletes are moved here, kept for the retention period and can be restored.
// Nothing outside the data roots is ever touched, and only files inside the
// bin folder are ever deleted (by emptying the bin or by retention).
package recycle

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/fsops"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Errors.
var (
	ErrNotFound     = errors.New("recycle bin entry not found")
	ErrRestoreTaken = errors.New("the original location is in use again")
	ErrInsideBin    = errors.New("the recycle bin cannot be inside a library folder it protects")
)

// DefaultFolder is the bin under the first data root when none is set.
const DefaultFolder = "recycle-bin"

// Entry is one recycled file or folder.
type Entry struct {
	ID           int64
	OriginalPath string
	RecycledPath string
	Size         int64
	IsDir        bool
	ItemRef      string
	Reason       string
	RecycledAt   time.Time
}

// Bin is the recycle bin.
type Bin struct {
	DB       *db.DB
	Settings *settings.Store
	Roots    []string
	Now      func() time.Time
}

func (b *Bin) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// Folder returns the bin folder (the setting, or <first data root>/recycle-bin),
// checked to be inside the data roots.
func (b *Bin) Folder(ctx context.Context) (string, error) {
	p, err := settings.Get(ctx, b.Settings, library.RecycleBinPath)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(p) == "" {
		if len(b.Roots) == 0 {
			return "", lfs.ErrNotAllowed
		}
		p = filepath.Join(b.Roots[0], DefaultFolder)
	}
	return lfs.SafePath(b.Roots, p)
}

func token() string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// Recycle moves path (a file or folder inside the data roots) into the bin.
func (b *Bin) Recycle(ctx context.Context, path, itemRef, reason string) (Entry, error) {
	src, err := lfs.MustExist(b.Roots, path)
	if err != nil {
		return Entry{}, err
	}
	bin, err := b.Folder(ctx)
	if err != nil {
		return Entry{}, err
	}
	if src == bin || strings.HasPrefix(bin, src+string(filepath.Separator)) {
		return Entry{}, ErrInsideBin
	}
	st, err := os.Stat(src)
	if err != nil {
		return Entry{}, err
	}
	size := st.Size()
	if st.IsDir() {
		if size, err = fsops.Size(src); err != nil {
			return Entry{}, err
		}
	}
	now := b.now()
	dst := filepath.Join(bin, now.UTC().Format("20060102-150405")+"-"+token(), filepath.Base(src))
	if err := fsops.MoveTree(src, dst); err != nil {
		return Entry{}, err
	}
	e := Entry{OriginalPath: src, RecycledPath: dst, Size: size, IsDir: st.IsDir(), ItemRef: itemRef, Reason: reason, RecycledAt: now.UTC()}
	err = b.DB.W.QueryRowContext(ctx, `INSERT INTO recycle_bin (original_path, recycled_path, size, is_dir, item_ref, reason, recycled_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`, e.OriginalPath, e.RecycledPath, e.Size, e.IsDir, e.ItemRef, e.Reason, db.Millis(now)).Scan(&e.ID)
	return e, err
}

const columns = `id, original_path, recycled_path, size, is_dir, item_ref, reason, recycled_at`

func scan(row interface{ Scan(...any) error }) (Entry, error) {
	var (
		e  Entry
		at int64
	)
	err := row.Scan(&e.ID, &e.OriginalPath, &e.RecycledPath, &e.Size, &e.IsDir, &e.ItemRef, &e.Reason, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	e.RecycledAt = db.Time(at)
	return e, err
}

// List returns the bin's entries, newest first.
func (b *Bin) List(ctx context.Context) ([]Entry, error) {
	rows, err := b.DB.R.QueryContext(ctx, `SELECT `+columns+` FROM recycle_bin ORDER BY recycled_at DESC, id DESC LIMIT 5000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Get returns one entry.
func (b *Bin) Get(ctx context.Context, id int64) (Entry, error) {
	return scan(b.DB.R.QueryRowContext(ctx, `SELECT `+columns+` FROM recycle_bin WHERE id = ?`, id))
}

// Restore moves an entry back to where it was.
func (b *Bin) Restore(ctx context.Context, id int64) (Entry, error) {
	e, err := b.Get(ctx, id)
	if err != nil {
		return e, err
	}
	dst, err := lfs.SafePath(b.Roots, e.OriginalPath)
	if err != nil {
		return e, err
	}
	if _, err := os.Lstat(dst); err == nil {
		return e, ErrRestoreTaken
	}
	src, err := b.inside(ctx, e.RecycledPath)
	if err != nil {
		return e, err
	}
	if err := fsops.MoveTree(src, dst); err != nil {
		return e, err
	}
	b.removeEmptyParent(ctx, src)
	_, err = b.DB.W.ExecContext(ctx, `DELETE FROM recycle_bin WHERE id = ?`, id)
	return e, err
}

// inside resolves p and requires it to lie inside the bin folder.
func (b *Bin) inside(ctx context.Context, p string) (string, error) {
	bin, err := b.Folder(ctx)
	if err != nil {
		return "", err
	}
	s, err := lfs.SafePath([]string{bin}, p)
	if err != nil {
		return "", err
	}
	if s == bin {
		return "", lfs.ErrNotAllowed
	}
	return s, nil
}

func (b *Bin) removeEmptyParent(ctx context.Context, p string) {
	parent := filepath.Dir(p)
	if s, err := b.inside(ctx, parent); err == nil {
		_ = os.Remove(s) // only succeeds when empty
	}
}

// Delete permanently removes one entry (an explicit user action).
func (b *Bin) Delete(ctx context.Context, id int64) error {
	e, err := b.Get(ctx, id)
	if err != nil {
		return err
	}
	return b.purge(ctx, e)
}

func (b *Bin) purge(ctx context.Context, e Entry) error {
	p, err := b.inside(ctx, e.RecycledPath)
	if err != nil && !errors.Is(err, lfs.ErrNotFound) {
		return fmt.Errorf("refusing to delete %s: %w", e.RecycledPath, err)
	}
	if err == nil {
		if err := os.RemoveAll(p); err != nil {
			return err
		}
		b.removeEmptyParent(ctx, p)
	}
	_, err = b.DB.W.ExecContext(ctx, `DELETE FROM recycle_bin WHERE id = ?`, e.ID)
	return err
}

// Empty permanently removes every entry (an explicit user action).
func (b *Bin) Empty(ctx context.Context) (int, error) {
	return b.purgeBefore(ctx, time.Time{})
}

// Cleanup removes entries older than the retention setting (0 = keep forever).
func (b *Bin) Cleanup(ctx context.Context) (int, error) {
	days, err := settings.Get(ctx, b.Settings, library.RecycleBinDays)
	if err != nil || days <= 0 {
		return 0, err
	}
	return b.purgeBefore(ctx, b.now().Add(-time.Duration(days)*24*time.Hour))
}

func (b *Bin) purgeBefore(ctx context.Context, before time.Time) (int, error) {
	list, err := b.List(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range list {
		if !before.IsZero() && !e.RecycledAt.Before(before) {
			continue
		}
		if err := b.purge(ctx, e); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
