package settings

import (
	"bytes"
	"context"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	box, err := secrets.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(db.OpenTest(t), box)
}

func TestPlainSettings(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	v, err := Get(ctx, s, InstanceName)
	if err != nil || v != "Locarr" {
		t.Fatalf("default %q %v", v, err)
	}
	if err := Set(ctx, s, nil, InstanceName, "Home"); err != nil {
		t.Fatal(err)
	}
	if err := Set(ctx, s, nil, InstanceName, "Kuća"); err != nil {
		t.Fatal(err)
	}
	v, err = Get(ctx, s, InstanceName)
	if err != nil || v != "Kuća" {
		t.Fatalf("%q %v", v, err)
	}
	type nested struct {
		A int  `json:"a"`
		B bool `json:"b"`
	}
	k := Key[nested]{Name: "test.nested", Default: nested{A: 1}}
	if err := Set(ctx, s, nil, k, nested{A: 7, B: true}); err != nil {
		t.Fatal(err)
	}
	got, err := Get(ctx, s, k)
	if err != nil || got.A != 7 || !got.B {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSecretsEncryptedAtRest(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if ok, _ := s.Configured(ctx, TMDbAPIKey); ok {
		t.Fatal("configured before set")
	}
	if err := s.SetSecret(ctx, TMDbAPIKey, "tmdb-plain-value"); err != nil {
		t.Fatal(err)
	}
	var ct []byte
	if err := s.DB.R.QueryRow(`SELECT ciphertext FROM secrets WHERE key = ?`, TMDbAPIKey.Name).Scan(&ct); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, []byte("tmdb-plain-value")) {
		t.Fatal("stored in plain text")
	}
	v, ok, err := s.Secret(ctx, TMDbAPIKey)
	if err != nil || !ok || v != "tmdb-plain-value" {
		t.Fatalf("%q %v %v", v, ok, err)
	}
	if ok, _ := s.Configured(ctx, TMDbAPIKey); !ok {
		t.Fatal("not configured")
	}
	// A ciphertext moved to another key does not open (bound by aad).
	if _, err := s.DB.W.Exec(`INSERT INTO secrets (key, ciphertext, updated_at) VALUES ('other', ?, 0)`, ct); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Secret(ctx, SecretKey{Name: "other"}); err == nil {
		t.Fatal("moved ciphertext opened")
	}
	if err := s.SetSecret(ctx, TMDbAPIKey, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Secret(ctx, TMDbAPIKey); ok {
		t.Fatal("not cleared")
	}
}
