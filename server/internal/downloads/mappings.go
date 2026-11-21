package downloads

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"path/filepath"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/db"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
)

// Mapping translates a path a client reports (on host Host) to the path Locarr sees.
type Mapping struct {
	ID         int64
	Host       string
	RemotePath string
	LocalPath  string
}

// normaliseRemote keeps the client's separator style (Windows clients use \) but
// trims trailing separators.
func normaliseRemote(p string) string {
	p = strings.TrimSpace(p)
	if len(p) > 1 {
		p = strings.TrimRight(p, `/\`)
		if p == "" {
			p = "/"
		}
	}
	return p
}

func (s *Service) validMapping(m *Mapping) error {
	m.Host = strings.ToLower(strings.TrimSpace(m.Host))
	m.RemotePath = normaliseRemote(m.RemotePath)
	m.LocalPath = strings.TrimSpace(m.LocalPath)
	if m.Host == "" {
		return invalid("host is required")
	}
	if m.RemotePath == "" {
		return invalid("remote path is required")
	}
	if !filepath.IsAbs(m.LocalPath) {
		return invalid("local path must be absolute")
	}
	local, err := lfs.SafePath(s.DataRoots, filepath.Clean(m.LocalPath))
	if err != nil {
		return err
	}
	m.LocalPath = local
	return nil
}

// Mappings lists the remote path mappings.
func (s *Service) Mappings(ctx context.Context) ([]Mapping, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, host, remote_path, local_path FROM remote_path_mappings ORDER BY host, remote_path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Mapping{}
	for rows.Next() {
		var m Mapping
		if err := rows.Scan(&m.ID, &m.Host, &m.RemotePath, &m.LocalPath); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateMapping adds a mapping; the local path must lie inside the data roots.
func (s *Service) CreateMapping(ctx context.Context, m Mapping) (Mapping, error) {
	if err := s.validMapping(&m); err != nil {
		return m, err
	}
	err := s.DB.W.QueryRowContext(ctx, `INSERT INTO remote_path_mappings (host, remote_path, local_path, created_at)
		VALUES (?, ?, ?, ?) RETURNING id`, m.Host, m.RemotePath, m.LocalPath, db.Millis(s.now())).Scan(&m.ID)
	if isUnique(err) {
		return m, invalid("a mapping for this host and remote path already exists")
	}
	return m, err
}

// UpdateMapping replaces a mapping.
func (s *Service) UpdateMapping(ctx context.Context, m Mapping) (Mapping, error) {
	if err := s.validMapping(&m); err != nil {
		return m, err
	}
	res, err := s.DB.W.ExecContext(ctx, `UPDATE remote_path_mappings SET host = ?, remote_path = ?, local_path = ? WHERE id = ?`,
		m.Host, m.RemotePath, m.LocalPath, m.ID)
	if isUnique(err) {
		return m, invalid("a mapping for this host and remote path already exists")
	}
	if err != nil {
		return m, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m, ErrNotFound
	}
	return m, nil
}

// DeleteMapping removes a mapping.
func (s *Service) DeleteMapping(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM remote_path_mappings WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Translate maps a client path to a local path with the longest matching
// mapping for host (unchanged when none matches).
func Translate(mappings []Mapping, host, remote string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	var (
		m     Mapping
		found bool
	)
	for _, cand := range mappings {
		if cand.Host == host && hasPathPrefix(remote, cand.RemotePath) && (!found || len(cand.RemotePath) > len(m.RemotePath)) {
			m, found = cand, true
		}
	}
	if !found {
		return remote
	}
	rest := strings.TrimLeft(remote[len(m.RemotePath):], `/\`)
	rest = strings.ReplaceAll(rest, `\`, "/")
	if rest == "" {
		return m.LocalPath
	}
	return filepath.Join(m.LocalPath, filepath.FromSlash(path.Clean("/" + rest))[1:])
}

func hasPathPrefix(p, prefix string) bool {
	if prefix == "/" {
		return strings.HasPrefix(p, "/")
	}
	win := strings.Contains(prefix, `\`) || (len(prefix) >= 2 && prefix[1] == ':')
	if win {
		if len(p) < len(prefix) || !strings.EqualFold(p[:len(prefix)], prefix) {
			return false
		}
	} else if !strings.HasPrefix(p, prefix) {
		return false
	}
	return len(p) == len(prefix) || p[len(prefix)] == '/' || p[len(prefix)] == '\\'
}

// resolveLocal turns a client path into an existing local path inside the data roots.
func (s *Service) resolveLocal(mappings []Mapping, host, remote string) (string, error) {
	if remote == "" {
		return "", lfs.ErrNotFound
	}
	local := Translate(mappings, host, remote)
	if !filepath.IsAbs(local) {
		return "", lfs.ErrNotAllowed
	}
	return lfs.MustExist(s.DataRoots, local)
}

var errUnsafeDelete = errors.New("refusing to delete this path")

// deleteLocal deletes a download's content after the user asked for its data to
// be removed and the client cannot do it itself. Only paths inside the data
// roots that are neither a data root nor a library root folder (or a parent
// of one) are deleted.
func (s *Service) deleteLocal(ctx context.Context, p string) error {
	safe, err := lfs.MustExist(s.DataRoots, p)
	if err != nil {
		if errors.Is(err, lfs.ErrNotFound) {
			return nil
		}
		return err
	}
	for _, r := range s.DataRoots {
		if rr, err := filepath.EvalSymlinks(r); err == nil && within(safe, rr) {
			return errUnsafeDelete
		}
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT path FROM root_folders`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var rf string
		if err := rows.Scan(&rf); err != nil {
			return err
		}
		if within(safe, rf) {
			return errUnsafeDelete
		}
	}
	if err := rows.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return removeAll(safe)
}

// within reports whether child is parent itself or inside it.
func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)))
}
