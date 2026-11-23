// Package backup makes and restores backups of Locarr's own state (D-083): a
// zip in /config/backups with a consistent copy of the SQLite database
// (VACUUM INTO, safe while Locarr keeps writing), the secret key that
// decrypts the stored credentials, the plain settings as JSON for reading and
// a manifest. Media files are never part of a backup and a restore never
// touches them.
//
// A restore is staged in /config/restore and applied while the database is
// closed (the app restarts in-process); afterwards every session except the
// restorer's is signed out.
package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
)

// Kinds of backups.
const (
	KindScheduled  = "scheduled"
	KindManual     = "manual"
	KindPreRestore = "pre_restore"
	KindUploaded   = "uploaded"
)

// File names inside a backup.
const (
	fileDB       = "locarr.db"
	fileKey      = "secret.key"
	fileSettings = "settings.json"
	fileManifest = "manifest.json"
)

// MaxUpload caps an uploaded backup.
const MaxUpload = 2 << 30

// Errors.
var (
	ErrNotFound      = errors.New("backup not found")
	ErrInvalid       = errors.New("not a Locarr backup")
	ErrNewerSchema   = errors.New("the backup was made by a newer Locarr; update Locarr first")
	ErrKeyMismatch   = errors.New("the secret key in the backup does not open the credentials in its database")
	ErrRestoreFailed = errors.New("restore failed")
)

// NamePattern matches backup file names.
var NamePattern = regexp.MustCompile(`^locarr_backup_[0-9a-z_-]{1,80}\.zip$`)

// Manifest describes a backup.
type Manifest struct {
	Product   string    `json:"product"`
	Version   string    `json:"version"`
	Commit    string    `json:"commit"`
	Schema    int64     `json:"schema"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"createdAt"`
}

// Info is a backup in the backup folder.
type Info struct {
	Name      string
	Kind      string
	Size      int64
	CreatedAt time.Time
	Version   string
}

// Service makes, lists and restores backups.
type Service struct {
	DB        *db.DB
	ConfigDir string
	Version   string
	Commit    string
	Now       func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Dir is the backup folder.
func (s *Service) Dir() string { return filepath.Join(s.ConfigDir, "backups") }

// Create makes a backup now.
func (s *Service) Create(ctx context.Context, kind string) (Info, error) {
	if err := os.MkdirAll(s.Dir(), 0o700); err != nil {
		return Info{}, err
	}
	tmp, err := os.MkdirTemp(s.Dir(), ".tmp-")
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	dbCopy := filepath.Join(tmp, fileDB)
	if err := s.snapshot(ctx, dbCopy); err != nil {
		return Info{}, fmt.Errorf("copy the database: %w", err)
	}
	schema, err := s.DB.Version(ctx)
	if err != nil {
		return Info{}, err
	}
	settingsJSON, err := s.settings(ctx)
	if err != nil {
		return Info{}, err
	}
	key, err := os.ReadFile(filepath.Join(s.ConfigDir, secrets.KeyFile)) // #nosec G304 -- the config folder
	if err != nil {
		return Info{}, fmt.Errorf("read the secret key: %w", err)
	}
	now := s.now().UTC()
	m := Manifest{Product: "Locarr", Version: s.Version, Commit: s.Commit, Schema: schema, Kind: kind, CreatedAt: now}
	name := fmt.Sprintf("locarr_backup_%s_%s.zip", now.Format("20060102_150405"), strings.ReplaceAll(kind, "_", "-"))
	for i := 2; fileExists(filepath.Join(s.Dir(), name)); i++ {
		name = fmt.Sprintf("locarr_backup_%s_%s-%d.zip", now.Format("20060102_150405"), strings.ReplaceAll(kind, "_", "-"), i)
	}
	part := filepath.Join(tmp, name)
	if err := writeZip(part, m, dbCopy, key, settingsJSON); err != nil {
		return Info{}, err
	}
	final := filepath.Join(s.Dir(), name)
	if err := os.Rename(part, final); err != nil {
		return Info{}, err
	}
	st, err := os.Stat(final)
	if err != nil {
		return Info{}, err
	}
	return Info{Name: name, Kind: kind, Size: st.Size(), CreatedAt: now, Version: s.Version}, nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// snapshot writes a consistent copy of the live database to dst through its
// own connection, so writers are not blocked (WAL readers see one snapshot).
func (s *Service) snapshot(ctx context.Context, dst string) error {
	conn, err := sql.Open("sqlite", "file:"+s.DB.Path+"?_pragma=busy_timeout(10000)")
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	conn.SetMaxOpenConns(1)
	if _, err := conn.ExecContext(ctx, `VACUUM INTO ?`, dst); err != nil {
		return err
	}
	return os.Chmod(dst, 0o600)
}

func (s *Service) settings(ctx context.Context) ([]byte, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT key, value FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		if json.Valid([]byte(v)) {
			out[k] = json.RawMessage(v)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(out, "", "  ")
}

func writeZip(path string, m Manifest, dbFile string, key, settingsJSON []byte) (err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- inside the backup folder
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	zw := zip.NewWriter(f)
	add := func(name string, r io.Reader) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: m.CreatedAt})
		if err != nil {
			return err
		}
		_, err = io.Copy(w, r)
		return err
	}
	mj, _ := json.MarshalIndent(m, "", "  ")
	if err := add(fileManifest, strings.NewReader(string(mj))); err != nil {
		return err
	}
	dbf, err := os.Open(dbFile) // #nosec G304 -- our temporary copy
	if err != nil {
		return err
	}
	err = add(fileDB, dbf)
	_ = dbf.Close()
	if err != nil {
		return err
	}
	if err := add(fileKey, strings.NewReader(string(key))); err != nil {
		return err
	}
	if err := add(fileSettings, strings.NewReader(string(settingsJSON))); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Sync()
}

// kindFromName reads the kind from a backup's file name.
func kindFromName(name string) string {
	base := strings.TrimSuffix(strings.TrimPrefix(name, "locarr_backup_"), ".zip")
	for _, k := range []string{KindScheduled, KindManual, KindPreRestore, KindUploaded} {
		tag := strings.ReplaceAll(k, "_", "-")
		if strings.HasSuffix(base, "_"+tag) || strings.Contains(base, "_"+tag+"-") {
			return k
		}
	}
	return KindManual
}

// List returns the backups, newest first.
func (s *Service) List() ([]Info, error) {
	entries, err := os.ReadDir(s.Dir())
	if errors.Is(err, os.ErrNotExist) {
		return []Info{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Info{}
	for _, e := range entries {
		if e.IsDir() || !NamePattern.MatchString(e.Name()) {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		in := Info{Name: e.Name(), Kind: kindFromName(e.Name()), Size: st.Size(), CreatedAt: st.ModTime().UTC()}
		if m, err := readManifest(filepath.Join(s.Dir(), e.Name())); err == nil {
			in.CreatedAt, in.Version = m.CreatedAt.UTC(), m.Version
			if m.Kind != "" {
				in.Kind = m.Kind
			}
		}
		out = append(out, in)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Path returns the file of a backup by name.
func (s *Service) Path(name string) (string, error) {
	if !NamePattern.MatchString(name) {
		return "", ErrNotFound
	}
	p := filepath.Join(s.Dir(), filepath.Base(name))
	if !fileExists(p) {
		return "", ErrNotFound
	}
	return p, nil
}

// Delete removes a backup.
func (s *Service) Delete(name string) error {
	p, err := s.Path(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// Prune keeps the newest keep scheduled backups and deletes the older ones;
// other kinds are kept until the administrator deletes them.
func (s *Service) Prune(keep int) (int, error) {
	list, err := s.List()
	if err != nil {
		return 0, err
	}
	n, removed := 0, 0
	for _, b := range list {
		if b.Kind != KindScheduled {
			continue
		}
		n++
		if n <= keep {
			continue
		}
		if err := s.Delete(b.Name); err == nil {
			removed++
		}
	}
	return removed, nil
}

func readManifest(path string) (Manifest, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return Manifest{}, err
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.Name != fileManifest {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return Manifest{}, err
		}
		defer rc.Close()
		var m Manifest
		err = json.NewDecoder(io.LimitReader(rc, 1<<20)).Decode(&m)
		return m, err
	}
	return Manifest{}, ErrInvalid
}

// SaveUpload stores an uploaded backup in the backup folder (at most
// MaxUpload bytes) and returns its name.
func (s *Service) SaveUpload(r io.Reader) (string, error) {
	if err := os.MkdirAll(s.Dir(), 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("locarr_backup_%s_uploaded.zip", s.now().UTC().Format("20060102_150405"))
	for i := 2; fileExists(filepath.Join(s.Dir(), name)); i++ {
		name = fmt.Sprintf("locarr_backup_%s_uploaded-%d.zip", s.now().UTC().Format("20060102_150405"), i)
	}
	tmp, err := os.CreateTemp(s.Dir(), ".upload-")
	if err != nil {
		return "", err
	}
	n, err := io.Copy(tmp, io.LimitReader(r, MaxUpload+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > MaxUpload {
		err = fmt.Errorf("%w: larger than 2 GiB", ErrInvalid)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	if _, err := readManifest(tmp.Name()); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(s.Dir(), name)); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return name, nil
}

// Session is the restorer's session, kept across a restore.
type Session struct {
	UserID     int64  `json:"userId"`
	Username   string `json:"username"`
	TokenHash  []byte `json:"tokenHash"`
	DeviceName string `json:"deviceName"`
	Client     string `json:"client"`
	IP         string `json:"ip"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
	ExpiresAt  int64  `json:"expiresAt"`
}

// RestorerSession loads a session to keep across a restore (sessionID 0 = none, e.g. an API key).
func RestorerSession(ctx context.Context, d *db.DB, sessionID int64) (*Session, error) {
	if sessionID == 0 {
		return nil, nil
	}
	var s Session
	err := d.R.QueryRowContext(ctx, `SELECT s.user_id, u.username, s.token_hash, s.device_name, s.client, s.ip, s.created_at, s.last_seen_at,
		s.expires_at FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.id = ?`, sessionID).
		Scan(&s.UserID, &s.Username, &s.TokenHash, &s.DeviceName, &s.Client, &s.IP, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

// stagingDir holds a restore until it is applied; appliedDir until the
// sessions are revoked.
func stagingDir(configDir string) string { return filepath.Join(configDir, "restore") }
func appliedDir(configDir string) string { return filepath.Join(configDir, "restore-applied") }

const readyMarker = "READY"

// Stage checks a backup and prepares it to be applied at the next start.
// maxSchema is the newest schema this Locarr knows.
func (s *Service) Stage(ctx context.Context, name string, maxSchema int64, keep *Session) (Manifest, error) {
	p, err := s.Path(name)
	if err != nil {
		return Manifest{}, err
	}
	dir := stagingDir(s.ConfigDir)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Manifest{}, err
	}
	m, err := extract(p, dir)
	if err == nil {
		err = verify(ctx, dir, maxSchema)
	}
	if err == nil && keep != nil {
		raw, _ := json.Marshal(keep)
		err = os.WriteFile(filepath.Join(dir, "restorer.json"), raw, 0o600)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, readyMarker), []byte(name+"\n"), 0o600)
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		return Manifest{}, err
	}
	return m, nil
}

// extract writes the database and the key of a backup into dir.
func extract(zipPath, dir string) (Manifest, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	defer func() { _ = zr.Close() }()
	var m Manifest
	found := map[string]bool{}
	for _, f := range zr.File {
		switch f.Name {
		case fileManifest:
			rc, err := f.Open()
			if err != nil {
				return m, err
			}
			err = json.NewDecoder(io.LimitReader(rc, 1<<20)).Decode(&m)
			_ = rc.Close()
			if err != nil {
				return m, fmt.Errorf("%w: bad manifest", ErrInvalid)
			}
		case fileDB, fileKey:
			if f.UncompressedSize64 > 64<<30 {
				return m, fmt.Errorf("%w: %s is too large", ErrInvalid, f.Name)
			}
			rc, err := f.Open()
			if err != nil {
				return m, err
			}
			out, err := os.OpenFile(filepath.Join(dir, f.Name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) // #nosec G304 G305 -- f.Name is one of two fixed names
			if err != nil {
				_ = rc.Close()
				return m, err
			}
			_, err = io.Copy(out, io.LimitReader(rc, 64<<30)) // #nosec G110 -- bounded above
			_ = rc.Close()
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return m, err
			}
		default:
			continue
		}
		found[f.Name] = true
	}
	if !found[fileManifest] || !found[fileDB] || !found[fileKey] || m.Product != "Locarr" {
		return m, fmt.Errorf("%w: it must hold %s, %s and %s", ErrInvalid, fileManifest, fileDB, fileKey)
	}
	return m, nil
}

// verify opens the staged database read-only: it must pass an integrity
// check, not be newer than this Locarr, and its credentials must open with
// the staged key.
func verify(ctx context.Context, dir string, maxSchema int64) error {
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(dir, fileDB)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	var ok string
	if err := conn.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&ok); err != nil || ok != "ok" {
		return fmt.Errorf("%w: the database is damaged (%s)", ErrInvalid, ok)
	}
	var schema int64
	if err := conn.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied = 1`).Scan(&schema); err != nil {
		return fmt.Errorf("%w: no schema version", ErrInvalid)
	}
	if schema > maxSchema {
		return ErrNewerSchema
	}
	raw, err := os.ReadFile(filepath.Join(dir, fileKey)) // #nosec G304 -- staged file
	if err != nil {
		return err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return fmt.Errorf("%w: bad secret key", ErrInvalid)
	}
	box, err := secrets.New(key)
	if err != nil {
		return fmt.Errorf("%w: bad secret key", ErrInvalid)
	}
	var (
		name string
		ct   []byte
	)
	err = conn.QueryRowContext(ctx, `SELECT key, ciphertext FROM secrets LIMIT 1`).Scan(&name, &ct)
	if err == nil {
		if _, err := box.Open(ct, "setting:"+name); err != nil {
			return ErrKeyMismatch
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}

// CancelPending drops a staged restore.
func CancelPending(configDir string) error { return os.RemoveAll(stagingDir(configDir)) }

// Pending reports whether a staged restore waits to be applied.
func Pending(configDir string) bool {
	return fileExists(filepath.Join(stagingDir(configDir), readyMarker))
}

// ApplyPending applies a staged restore. The database must be closed. The
// replaced database and key stay next to them as *.pre-restore.
func ApplyPending(configDir, dbPath string) (bool, error) {
	dir := stagingDir(configDir)
	if !Pending(configDir) {
		return false, nil
	}
	keyPath := filepath.Join(configDir, secrets.KeyFile)
	if fileExists(dbPath) {
		if err := os.Rename(dbPath, dbPath+".pre-restore"); err != nil {
			return false, fmt.Errorf("%w: %w", ErrRestoreFailed, err)
		}
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(dbPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("%w: %w", ErrRestoreFailed, err)
		}
	}
	if err := os.Rename(filepath.Join(dir, fileDB), dbPath); err != nil {
		_ = os.Rename(dbPath+".pre-restore", dbPath)
		return false, fmt.Errorf("%w: %w", ErrRestoreFailed, err)
	}
	if fileExists(keyPath) {
		_ = os.Remove(keyPath + ".pre-restore")
		if err := os.Rename(keyPath, keyPath+".pre-restore"); err != nil {
			return false, fmt.Errorf("%w: %w", ErrRestoreFailed, err)
		}
	}
	if err := os.Rename(filepath.Join(dir, fileKey), keyPath); err != nil {
		return false, fmt.Errorf("%w: %w", ErrRestoreFailed, err)
	}
	_ = os.Remove(filepath.Join(dir, readyMarker))
	_ = os.RemoveAll(appliedDir(configDir))
	if err := os.Rename(dir, appliedDir(configDir)); err != nil {
		return true, err
	}
	return true, nil
}

// FinishRestore runs on the restored (and migrated) database: it signs out
// every session except the restorer's, which is carried over when the same
// administrator exists in the restored data.
func FinishRestore(ctx context.Context, d *db.DB, configDir string) (bool, error) {
	dir := appliedDir(configDir)
	if !fileExists(dir) {
		return false, nil
	}
	var keep *Session
	if raw, err := os.ReadFile(filepath.Join(dir, "restorer.json")); err == nil { // #nosec G304 -- our own file
		var s Session
		if json.Unmarshal(raw, &s) == nil {
			keep = &s
		}
	}
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions`); err != nil {
			return err
		}
		if keep == nil {
			return nil
		}
		var (
			username string
			role     string
			disabled bool
		)
		err := tx.QueryRowContext(ctx, `SELECT username, role, disabled FROM users WHERE id = ?`, keep.UserID).Scan(&username, &role, &disabled)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (!strings.EqualFold(username, keep.Username) || role != "admin" || disabled)) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO sessions (user_id, token_hash, device_name, client, ip, created_at, last_seen_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, keep.UserID, keep.TokenHash, keep.DeviceName, keep.Client, keep.IP, keep.CreatedAt,
			keep.LastSeenAt, keep.ExpiresAt)
		return err
	})
	if err != nil {
		return false, err
	}
	return true, os.RemoveAll(dir)
}
