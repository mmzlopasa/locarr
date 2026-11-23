package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
)

func newService(t *testing.T) (*Service, *db.DB, *secrets.Box) {
	t.Helper()
	dir := t.TempDir()
	box, err := secrets.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(context.Background(), filepath.Join(dir, "locarr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &Service{DB: d, ConfigDir: dir, Version: "1.2.3"}, d, box
}

// A backup taken while another goroutine keeps writing is a consistent,
// complete database.
func TestBackupWhileWriting(t *testing.T) {
	s, d, box := newService(t)
	ct, _ := box.Seal([]byte("indexer-key"), "setting:metadata.tmdbApiKey")
	if _, err := d.W.Exec(`INSERT INTO secrets (key, ciphertext, updated_at) VALUES ('metadata.tmdbApiKey', ?, 0)`, ct); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	var written atomic.Int64
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ctx.Err() == nil; i++ {
			// Two rows per transaction: a consistent copy always has an even count.
			err := d.Tx(ctx, func(tx *sql.Tx) error {
				for j := 0; j < 2; j++ {
					if _, err := tx.Exec(`INSERT INTO timeline_events (item_ref, at, type) VALUES ('movie:1', ?, 'added')`, i); err != nil {
						return err
					}
				}
				return nil
			})
			if err == nil {
				written.Add(2)
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	var infos []Info
	for i := 0; i < 3; i++ {
		in, err := s.Create(context.Background(), KindManual)
		if err != nil {
			t.Fatal(err)
		}
		infos = append(infos, in)
	}
	cancel()
	wg.Wait()
	if written.Load() == 0 {
		t.Fatal("nothing written")
	}
	for _, in := range infos {
		dir := t.TempDir()
		p, err := s.Path(in.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := extract(p, dir); err != nil {
			t.Fatal(err)
		}
		if err := verify(context.Background(), dir, db.LatestVersion()); err != nil {
			t.Fatal(err)
		}
		c, err := sql.Open("sqlite", "file:"+filepath.Join(dir, fileDB)+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err := c.QueryRow(`SELECT COUNT(*) FROM timeline_events`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		_ = c.Close()
		if n%2 != 0 {
			t.Fatalf("half a transaction in the backup: %d rows", n)
		}
		zr, err := zip.OpenReader(p)
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, f := range zr.File {
			names[f.Name] = true
		}
		_ = zr.Close()
		for _, want := range []string{fileDB, fileKey, fileSettings, fileManifest} {
			if !names[want] {
				t.Errorf("%s missing", want)
			}
		}
		st, _ := os.Stat(p)
		if st.Mode().Perm() != 0o600 {
			t.Errorf("backup readable by others: %v", st.Mode())
		}
	}
}

func TestPruneKeepsNewestScheduled(t *testing.T) {
	s, _, _ := newService(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		s.Now = func() time.Time { return base.Add(time.Duration(i) * time.Hour) }
		if _, err := s.Create(context.Background(), KindScheduled); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Create(context.Background(), KindManual); err != nil {
		t.Fatal(err)
	}
	n, err := s.Prune(2)
	if err != nil || n != 3 {
		t.Fatal(n, err)
	}
	list, _ := s.List()
	if len(list) != 3 || list[0].Kind != KindManual || list[1].CreatedAt != base.Add(4*time.Hour) {
		t.Fatalf("%+v", list)
	}
}

func TestStageRefusesBadBackups(t *testing.T) {
	s, d, _ := newService(t)
	in, err := s.Create(context.Background(), KindManual)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stage(context.Background(), in.Name, 1, nil); !errors.Is(err, ErrNewerSchema) {
		t.Fatalf("newer schema: %v", err)
	}
	if _, err := s.Stage(context.Background(), "../secret.key", db.LatestVersion(), nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("path: %v", err)
	}
	// A key that does not open the stored credentials.
	other, _ := secrets.New(make([]byte, 32))
	ct, _ := other.Seal([]byte("x"), "setting:metadata.tmdbApiKey")
	if _, err := d.W.Exec(`INSERT INTO secrets (key, ciphertext, updated_at) VALUES ('metadata.tmdbApiKey', ?, 0)`, ct); err != nil {
		t.Fatal(err)
	}
	in, err = s.Create(context.Background(), KindManual)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stage(context.Background(), in.Name, db.LatestVersion(), nil); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("key: %v", err)
	}
	if Pending(s.ConfigDir) {
		t.Fatal("a refused restore stays staged")
	}
	if _, err := s.SaveUpload(stringsReader("PK not really")); err == nil {
		t.Fatal("bad upload accepted")
	}
}

type sr struct {
	s string
	i int
}

func (r *sr) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}

func stringsReader(s string) io.Reader { return &sr{s: s} }
