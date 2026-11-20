package auth

import (
	"strings"
	"testing"
	"time"
)

func TestSetupCode(t *testing.T) {
	c, err := NewSetupCode()
	if err != nil || len(c) != 9 || c[4] != '-' {
		t.Fatal(c, err)
	}
	if !setupCodeMatches(strings.ToLower(strings.ReplaceAll(c, "-", " ")), c) || setupCodeMatches("", "") || setupCodeMatches("AAAA-AAAA", c) && c != "AAAA-AAAA" {
		t.Fatal("matching")
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("pässwörd")
	if err != nil || !strings.HasPrefix(h, "$argon2id$") {
		t.Fatal(h, err)
	}
	if ok, _ := VerifyPassword("pässwörd", h); !ok {
		t.Fatal("verify")
	}
	if ok, _ := VerifyPassword("password", h); ok {
		t.Fatal("wrong accepted")
	}
	if _, err := VerifyPassword("x", "$bcrypt$"); err == nil {
		t.Fatal("bad hash accepted")
	}
}

func TestTOTPVectors(t *testing.T) {
	// RFC 6238 appendix B (SHA-1, 8 digits there; the last 6 digits here).
	secret := []byte("12345678901234567890")
	for ts, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924"} {
		if got := totpCode(secret, ts/30); got != want {
			t.Errorf("t=%d got %s want %s", ts, got, want)
		}
	}
	now := time.Unix(1234567890, 0)
	if _, ok := verifyTOTP(secret, "005924", now, 0); !ok {
		t.Fatal("current step")
	}
	if _, ok := verifyTOTP(secret, totpCode(secret, totpStep(now)-1), now, 0); !ok {
		t.Fatal("skew")
	}
	if _, ok := verifyTOTP(secret, totpCode(secret, totpStep(now)-3), now, 0); ok {
		t.Fatal("old code")
	}
	if _, ok := verifyTOTP(secret, "005924", now, totpStep(now)); ok {
		t.Fatal("replay")
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := newRecoveryCodes(10)
	if err != nil || len(codes) != 10 || len(codes[0]) != 14 {
		t.Fatal(codes, err)
	}
	if normalizeRecoveryCode(" AB-cd ") != "abcd" {
		t.Fatal("normalize")
	}
}
