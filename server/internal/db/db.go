// Package db opens the SQLite database: one writer connection (SQLite allows a
// single writer at a time; serialising writes in the pool avoids SQLITE_BUSY)
// and a pool of read-only connections that read concurrently thanks to WAL.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed migrations/*.sql
var migrations embed.FS

// DB is the database: W for every write (and reads inside write transactions),
// R for plain reads.
type DB struct {
	W    *sql.DB
	R    *sql.DB
	Path string
}

// busyTimeout is how long a connection waits for a lock before SQLITE_BUSY.
const busyTimeout = 10 * time.Second

func dsn(path string, readOnly bool) string {
	q := url.Values{}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_time_format", "sqlite")
	if readOnly {
		q.Add("_pragma", "query_only(1)")
	} else {
		q.Add("_pragma", "journal_mode(WAL)")
		// Write transactions take the lock at BEGIN, so they never fail half-way
		// when upgrading from a read lock.
		q.Set("_txlock", "immediate")
	}
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + q.Encode()
}

// Open opens (creating if needed) the database at path. The directory must exist.
func Open(ctx context.Context, path string) (*DB, error) {
	w, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)
	w.SetConnMaxLifetime(0)
	if err := w.PingContext(ctx); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	// The database holds password hashes and encrypted secrets: owner only.
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		_ = os.Chmod(f, 0o600)
	}
	var mode string
	if err := w.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		_ = w.Close()
		return nil, fmt.Errorf("database %s: WAL mode not available (got %q): %w", path, mode, err)
	}
	r, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	n := max(4, runtime.NumCPU())
	r.SetMaxOpenConns(n)
	r.SetMaxIdleConns(n)
	return &DB{W: w, R: r, Path: path}, nil
}

// Close checkpoints the write-ahead log and closes both pools.
func (d *DB) Close() error {
	_, _ = d.W.ExecContext(context.Background(), "PRAGMA wal_checkpoint(TRUNCATE)")
	return errors.Join(d.R.Close(), d.W.Close())
}

// Migrate applies every pending migration.
func (d *DB) Migrate(ctx context.Context) error {
	p, err := d.provider()
	if err != nil {
		return err
	}
	_, err = p.Up(ctx)
	return err
}

// Version returns the current schema version.
func (d *DB) Version(ctx context.Context) (int64, error) {
	p, err := d.provider()
	if err != nil {
		return 0, err
	}
	return p.GetDBVersion(ctx)
}

func (d *DB) provider() (*goose.Provider, error) {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectSQLite3, d.W, sub)
}

// Size returns the size of the database file plus its write-ahead log.
func (d *DB) Size() int64 {
	var total int64
	for _, p := range []string{d.Path, d.Path + "-wal"} {
		if st, err := os.Stat(p); err == nil {
			total += st.Size()
		}
	}
	return total
}

// Tx runs fn in a write transaction, committing when it returns nil.
func (d *DB) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// EnsureDir creates dir (0700) if it does not exist.
func EnsureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Clean(dir), err)
	}
	return nil
}

// Millis converts a time to the stored representation (Unix milliseconds).
func Millis(t time.Time) int64 { return t.UnixMilli() }

// Time converts stored Unix milliseconds back to a UTC time.
func Time(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// NullTime converts a nullable stored time.
func NullTime(ms sql.NullInt64) *time.Time {
	if !ms.Valid {
		return nil
	}
	t := Time(ms.Int64)
	return &t
}

// Querier is satisfied by *sql.DB and *sql.Tx.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// LatestVersion is the newest schema version this build knows.
func LatestVersion() int64 {
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return 0
	}
	var latest int64
	for _, e := range entries {
		var v int64
		if _, err := fmt.Sscanf(e.Name(), "%d_", &v); err == nil && v > latest {
			latest = v
		}
	}
	return latest
}
