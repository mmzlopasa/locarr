// Package secrets encrypts stored third-party credentials (indexer API keys,
// download client passwords, tokens, two-factor secrets) with AES-256-GCM. The key
// is generated on first run into the config folder (secret.key, mode 0600) and
// never stored in the database, so a copy of the database alone reveals nothing.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// KeyFile is the key's file name inside the config folder.
const KeyFile = "secret.key"

const (
	keyLen  = 32
	version = 1 // first byte of every ciphertext, so the format can change later
)

// ErrDecrypt is returned for ciphertext that does not open with this key.
var ErrDecrypt = errors.New("secrets: cannot decrypt (wrong key or damaged data)")

// Box seals and opens values with one key.
type Box struct{ aead cipher.AEAD }

// New returns a Box for a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != keyLen {
		return nil, fmt.Errorf("secrets: key must be %d bytes, got %d", keyLen, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// LoadOrCreate reads the key from dir/secret.key, creating it (0600) on first run.
func LoadOrCreate(dir string) (*Box, error) {
	path := filepath.Join(dir, KeyFile)
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the configured config folder
	if errors.Is(err, fs.ErrNotExist) {
		key := make([]byte, keyLen)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		enc := base64.StdEncoding.EncodeToString(key) + "\n"
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", path, err)
		}
		if _, err := f.WriteString(enc); err != nil {
			_ = f.Close()
			return nil, err
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		return New(key)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if st, err := os.Stat(path); err == nil && st.Mode().Perm()&0o077 != 0 {
		// Tighten permissions copied in by hand; not fatal if the filesystem refuses.
		_ = os.Chmod(path, 0o600)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("%s is not a valid key: %w", path, err)
	}
	return New(key)
}

// Seal encrypts plaintext. aad binds the ciphertext to its purpose (for example
// the setting name), so a value copied to another slot does not open.
func (b *Box) Seal(plaintext []byte, aad string) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+b.aead.Overhead())
	out = append(out, version)
	out = append(out, nonce...)
	return b.aead.Seal(out, nonce, plaintext, []byte(aad)), nil
}

// Open decrypts what Seal produced with the same aad.
func (b *Box) Open(ciphertext []byte, aad string) ([]byte, error) {
	ns := b.aead.NonceSize()
	if len(ciphertext) < 1+ns+b.aead.Overhead() || ciphertext[0] != version {
		return nil, ErrDecrypt
	}
	pt, err := b.aead.Open(nil, ciphertext[1:1+ns], ciphertext[1+ns:], []byte(aad))
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}
