package app

import (
	"encoding/base32"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"crypto/hmac"
	"crypto/sha1" // #nosec G505 -- RFC 6238
	"encoding/binary"
	"fmt"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }

func mustReq(path string) *http.Request { return httptest.NewRequest("GET", path, nil) }

// currentCode computes the RFC 6238 code an authenticator app would show.
func currentCode(t *testing.T, secretB32 string) string {
	t.Helper()
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secretB32))
	if err != nil {
		t.Fatal(err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(time.Now().Unix()/30))
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7fffffff)%1_000_000)
}
