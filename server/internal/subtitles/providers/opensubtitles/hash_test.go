package opensubtitles

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// The hash is size + Σ of the 64-bit little-endian words of the first and
// the last 64 KiB; with known word values the sum is easy to check.
func TestHash(t *testing.T) {
	const chunk = 64 << 10
	b := make([]byte, 3*chunk)
	binary.LittleEndian.PutUint64(b[0:], 1)
	binary.LittleEndian.PutUint64(b[8:], 2)
	binary.LittleEndian.PutUint64(b[len(b)-8:], 0xFFFFFFFFFFFFFFFF) // wraps around
	binary.LittleEndian.PutUint64(b[chunk+8:], 1000)                // in the middle: ignored
	p := filepath.Join(t.TempDir(), "f.mkv")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := Hash(p)
	if err != nil {
		t.Fatal(err)
	}
	want := uint64(len(b)) + 1 + 2 + 0xFFFFFFFFFFFFFFFF
	if h != fmtHash(want) {
		t.Fatalf("hash %s, want %s", h, fmtHash(want))
	}
	small := filepath.Join(t.TempDir(), "s.mkv")
	_ = os.WriteFile(small, []byte("tiny"), 0o600)
	if _, err := Hash(small); err == nil {
		t.Fatal("tiny file hashed")
	}
}

func fmtHash(v uint64) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		out[i] = hex[v&0xF]
		v >>= 4
	}
	return string(out)
}

func TestParamsSortedLowercase(t *testing.T) {
	got := params(map[string]string{"query": "Big Buck", "languages": "en,hr", "imdb_id": "", "year": "2008", "moviehash": "AB"})
	if got != "languages=en%2chr&moviehash=ab&query=big+buck&year=2008" {
		t.Fatal(got)
	}
}
