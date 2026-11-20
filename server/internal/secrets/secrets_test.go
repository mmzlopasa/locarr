package secrets

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreate(t *testing.T) {
	dir := t.TempDir()
	b1, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, KeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	ct, err := b1.Seal([]byte("indexer-key"), "indexer.1")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, []byte("indexer-key")) {
		t.Fatal("plaintext visible")
	}
	b2, err := LoadOrCreate(dir) // the same key is read back
	if err != nil {
		t.Fatal(err)
	}
	pt, err := b2.Open(ct, "indexer.1")
	if err != nil || string(pt) != "indexer-key" {
		t.Fatalf("%q %v", pt, err)
	}
}

func TestTamperWrongAADWrongKey(t *testing.T) {
	b1, _ := LoadOrCreate(t.TempDir())
	b2, _ := LoadOrCreate(t.TempDir())
	ct, _ := b1.Seal([]byte("s3cret"), "a")
	if _, err := b1.Open(ct, "b"); !errors.Is(err, ErrDecrypt) {
		t.Fatal("other aad opened")
	}
	if _, err := b2.Open(ct, "a"); !errors.Is(err, ErrDecrypt) {
		t.Fatal("other key opened")
	}
	bad := bytes.Clone(ct)
	bad[len(bad)-1] ^= 1
	if _, err := b1.Open(bad, "a"); !errors.Is(err, ErrDecrypt) {
		t.Fatal("tampered opened")
	}
	if _, err := b1.Open([]byte{1, 2}, "a"); !errors.Is(err, ErrDecrypt) {
		t.Fatal("short opened")
	}
	ct2, _ := b1.Seal([]byte("s3cret"), "a")
	if bytes.Equal(ct, ct2) {
		t.Fatal("nonce reused")
	}
}

func TestBadKeyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, KeyFile), []byte("not base64!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(dir); err == nil {
		t.Fatal("want error")
	}
	if _, err := New(make([]byte, 16)); err == nil {
		t.Fatal("short key accepted")
	}
}
