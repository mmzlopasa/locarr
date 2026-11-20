package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"math/big"
	"strings"
)

// APIKeyPrefix starts every API key, so keys are recognisable (in the
// X-Api-Key header, in secret scanners) and never mistaken for session tokens.
const APIKeyPrefix = "lk_"

// apiKeyDisplayLen is how much of a key is kept in clear to recognise it.
const apiKeyDisplayLen = len(APIKeyPrefix) + 6

// newToken returns a random 256-bit token and the SHA-256 hash that is stored.
// The token has full entropy, so a fast hash is enough (no password hashing).
func newToken(prefix string) (token string, hash []byte, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	token = prefix + base64.RawURLEncoding.EncodeToString(buf)
	return token, hashToken(token), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// Setup codes avoid look-alike characters (0/O, 1/I/L) so they can be read from a log.
const setupAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// NewSetupCode returns a random code formatted as XXXX-XXXX (~39 bits).
func NewSetupCode() (string, error) { return randomString(setupAlphabet, 8, 4) }

// NewPassword returns a random password for reset-admin (~99 bits).
func NewPassword() (string, error) {
	return randomString("abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789", 20, 5)
}

// randomString draws n characters uniformly from alphabet, with a dash every group.
func randomString(alphabet string, n, group int) (string, error) {
	var b strings.Builder
	size := big.NewInt(int64(len(alphabet)))
	for i := range n {
		if i > 0 && i%group == 0 {
			b.WriteByte('-')
		}
		k, err := rand.Int(rand.Reader, size) // uniform, no modulo bias
		if err != nil {
			return "", err
		}
		b.WriteByte(alphabet[k.Int64()])
	}
	return b.String(), nil
}

// setupCodeMatches compares codes case-insensitively, ignoring spaces and dashes.
func setupCodeMatches(got, want string) bool {
	norm := func(s string) []byte {
		return []byte(strings.NewReplacer("-", "", " ", "").Replace(strings.ToUpper(s)))
	}
	g, w := norm(got), norm(want)
	return len(w) > 0 && subtle.ConstantTimeCompare(g, w) == 1
}
