package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newSvc(t *testing.T) (*Service, *clock) {
	t.Helper()
	box, err := secrets.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(db.OpenTest(t), box, "ABCD-EFGH")
	c := &clock{t: time.Now()}
	s.now = c.now
	return s, c
}

var ctx = context.Background()

func setupAdmin(t *testing.T, s *Service) (Principal, string) {
	t.Helper()
	p, tok, err := s.Setup(ctx, "abcd efgh", "Admin", "correct horse", "hr", Device{Name: "Firefox", Client: "Web", IP: "10.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	return p, tok
}

func TestSetup(t *testing.T) {
	s, _ := newSvc(t)
	if req, _ := s.SetupRequired(ctx); !req {
		t.Fatal("setup not required on empty db")
	}
	if _, _, err := s.Setup(ctx, "WRONG-CODE", "admin", "correct horse", "en", Device{}); !errors.Is(err, ErrBadSetupCode) {
		t.Fatal(err)
	}
	if _, _, err := s.Setup(ctx, "ABCD-EFGH", "admin", "short", "en", Device{}); !errors.Is(err, ErrWeakPassword) {
		t.Fatal(err)
	}
	if _, _, err := s.Setup(ctx, "ABCD-EFGH", "bad/name", "correct horse", "en", Device{}); !errors.Is(err, ErrInvalidUsername) {
		t.Fatal(err)
	}
	p, tok := setupAdmin(t, s)
	if p.User.Role != RoleAdmin || p.User.Language != "hr" || p.SessionID == 0 || tok == "" {
		t.Fatalf("%+v", p)
	}
	if req, _ := s.SetupRequired(ctx); req {
		t.Fatal("still required")
	}
	if _, _, err := s.Setup(ctx, "ABCD-EFGH", "other", "correct horse", "en", Device{}); !errors.Is(err, ErrSetupDone) {
		t.Fatal(err)
	}
	got, err := s.Authenticate(ctx, tok)
	if err != nil || got.User.ID != p.User.ID || got.SessionID != p.SessionID {
		t.Fatal(got, err)
	}
}

func TestSetupCodeThrottle(t *testing.T) {
	s, c := newSvc(t)
	for range maxSetupFailures {
		_, _, _ = s.Setup(ctx, "nope", "admin", "correct horse", "en", Device{})
	}
	if _, _, err := s.Setup(ctx, "ABCD-EFGH", "admin", "correct horse", "en", Device{}); !errors.Is(err, ErrSetupThrottled) {
		t.Fatal(err)
	}
	c.t = c.t.Add(setupCooldown + time.Second)
	setupAdmin(t, s)
}

func TestTokensHashedAtRest(t *testing.T) {
	s, _ := newSvc(t)
	_, tok := setupAdmin(t, s)
	var n int
	if err := s.db.R.QueryRow(`SELECT count(*) FROM sessions WHERE token_hash = ?`, []byte(tok)).Scan(&n); err != nil || n != 0 {
		t.Fatal("token stored in clear")
	}
	if err := s.db.R.QueryRow(`SELECT count(*) FROM sessions WHERE token_hash = ?`, hashToken(tok)).Scan(&n); err != nil || n != 1 {
		t.Fatal("hash missing")
	}
}

func TestLoginAndLockout(t *testing.T) {
	s, c := newSvc(t)
	setupAdmin(t, s)
	in := LoginInput{Username: "ADMIN", Password: "correct horse", Device: Device{Name: "Phone"}}
	p, tok, err := s.Login(ctx, in)
	if err != nil || tok == "" || p.User.Username != "Admin" {
		t.Fatal(p, err)
	}
	if _, _, err := s.Login(ctx, LoginInput{Username: "nobody", Password: "x"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
	bad := LoginInput{Username: "admin", Password: "wrong password"}
	for i := 1; i < MaxFailedLogins; i++ {
		if _, _, err := s.Login(ctx, bad); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	var le *LockedError
	if _, _, err := s.Login(ctx, bad); !errors.As(err, &le) {
		t.Fatalf("not locked: %v", err)
	}
	// Even the right password is refused while locked.
	if _, _, err := s.Login(ctx, in); !errors.As(err, &le) {
		t.Fatalf("locked account signed in: %v", err)
	}
	u, _ := s.User(ctx, p.User.ID)
	if u.LockedUntil == nil {
		t.Fatal("lockedUntil not reported")
	}
	c.t = c.t.Add(LockoutDuration + time.Second)
	if _, _, err := s.Login(ctx, in); err != nil {
		t.Fatal(err)
	}
}

func TestDisabledUser(t *testing.T) {
	s, _ := newSvc(t)
	admin, _ := setupAdmin(t, s)
	u, err := s.CreateUser(ctx, NewUser{Username: "ana", Password: "ana-password", Role: RoleRequester})
	if err != nil {
		t.Fatal(err)
	}
	_, tok, err := s.Login(ctx, LoginInput{Username: "ana", Password: "ana-password"})
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	if _, err := s.UpdateUser(ctx, admin, u.ID, UserChange{Disabled: &yes}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, tok); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("disabled user's session still works")
	}
	if _, _, err := s.Login(ctx, LoginInput{Username: "ana", Password: "ana-password"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
}

func TestSessionsAndPasswordChange(t *testing.T) {
	s, c := newSvc(t)
	p1, tok1 := setupAdmin(t, s)
	p2, tok2, _ := s.Login(ctx, LoginInput{Username: "admin", Password: "correct horse", Device: Device{Name: "Phone", IP: "192.168.1.5"}})
	list, err := s.Sessions(ctx, p1.User.ID)
	if err != nil || len(list) != 2 {
		t.Fatal(list, err)
	}
	if err := s.ChangePassword(ctx, p1, "wrong", "new password 1"); !errors.Is(err, ErrWrongPassword) {
		t.Fatal(err)
	}
	if err := s.ChangePassword(ctx, p1, "correct horse", "admin"); !errors.Is(err, ErrWeakPassword) {
		t.Fatal(err)
	}
	if err := s.ChangePassword(ctx, p1, "correct horse", "new password 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, tok1); err != nil {
		t.Fatal("current session revoked")
	}
	if _, err := s.Authenticate(ctx, tok2); err == nil {
		t.Fatal("other session survived password change")
	}
	if _, _, err := s.Login(ctx, LoginInput{Username: "admin", Password: "new password 1"}); err != nil {
		t.Fatal(err)
	}
	// Revoke a device.
	list, _ = s.Sessions(ctx, p1.User.ID)
	var other int64
	for _, x := range list {
		if x.ID != p1.SessionID {
			other = x.ID
		}
	}
	if err := s.RevokeSession(ctx, p1.User.ID, other); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeSession(ctx, p1.User.ID, p2.SessionID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// Expiry: sessions slide while used and expire after the TTL.
	c.t = c.t.Add(SessionTTL + time.Hour)
	if _, err := s.Authenticate(ctx, tok1); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("expired session accepted")
	}
	if n, err := s.PruneSessions(ctx); err != nil || n == 0 {
		t.Fatal(n, err)
	}
	if err := s.Logout(ctx, Principal{}); !errors.Is(err, ErrSessionRequired) {
		t.Fatal(err)
	}
}

func TestAPIKeys(t *testing.T) {
	s, _ := newSvc(t)
	p, _ := setupAdmin(t, s)
	k, key, err := s.CreateAPIKey(ctx, p, "  Grafana ", true)
	if err != nil || !strings.HasPrefix(key, APIKeyPrefix) || k.Name != "Grafana" || !strings.HasPrefix(key, k.Prefix) {
		t.Fatal(k, key, err)
	}
	kp, err := s.Authenticate(ctx, key)
	if err != nil || !kp.ReadOnly || kp.APIKeyID != k.ID || kp.SessionID != 0 {
		t.Fatal(kp, err)
	}
	if _, _, err := s.CreateAPIKey(ctx, kp, "more", false); !errors.Is(err, ErrSessionRequired) {
		t.Fatal("an API key minted a key")
	}
	if _, _, err := s.CreateAPIKey(ctx, p, " ", false); !errors.Is(err, ErrInvalidName) {
		t.Fatal(err)
	}
	keys, _ := s.APIKeys(ctx, p.User.ID)
	if len(keys) != 1 || keys[0].LastUsedAt == nil {
		t.Fatal(keys)
	}
	if err := s.DeleteAPIKey(ctx, p.User.ID, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, key); err == nil {
		t.Fatal("deleted key works")
	}
	if _, err := s.Authenticate(ctx, APIKeyPrefix+"garbage"); err == nil {
		t.Fatal("garbage key works")
	}
}

func TestTOTPFlow(t *testing.T) {
	s, c := newSvc(t)
	p, _ := setupAdmin(t, s)
	if _, err := s.EnableTOTP(ctx, p, "123456"); !errors.Is(err, ErrTOTPNotStarted) {
		t.Fatal(err)
	}
	secretB32, uri, err := s.StartTOTP(ctx, p)
	if err != nil || !strings.HasPrefix(uri, "otpauth://totp/Locarr:Admin?") {
		t.Fatal(uri, err)
	}
	secret, _ := b32.DecodeString(secretB32)
	if _, err := s.EnableTOTP(ctx, p, "000000"); !errors.Is(err, ErrInvalidTOTP) && totpCode(secret, totpStep(c.t)) != "000000" {
		t.Fatal(err)
	}
	codes, err := s.EnableTOTP(ctx, p, totpCode(secret, totpStep(c.t)))
	if err != nil || len(codes) != recoveryCodeN {
		t.Fatal(codes, err)
	}
	// The secret is stored encrypted.
	var stored []byte
	_ = s.db.R.QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, p.User.ID).Scan(&stored)
	if strings.Contains(string(stored), string(secret)) {
		t.Fatal("totp secret in clear")
	}
	in := LoginInput{Username: "admin", Password: "correct horse"}
	if _, _, err := s.Login(ctx, in); !errors.Is(err, ErrTOTPRequired) {
		t.Fatal(err)
	}
	// The code used to enable cannot be replayed.
	in.TOTPCode = totpCode(secret, totpStep(c.t))
	if _, _, err := s.Login(ctx, in); !errors.Is(err, ErrInvalidTOTP) {
		t.Fatalf("replayed code accepted: %v", err)
	}
	c.t = c.t.Add(30 * time.Second)
	in.TOTPCode = totpCode(secret, totpStep(c.t))
	if _, _, err := s.Login(ctx, in); err != nil {
		t.Fatal(err)
	}
	// Recovery codes work once each.
	in.TOTPCode, in.RecoveryCode = "", strings.ToUpper(codes[0])
	if _, _, err := s.Login(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Login(ctx, in); !errors.Is(err, ErrInvalidTOTP) {
		t.Fatal("recovery code reused")
	}
	newCodes, err := s.RegenerateRecoveryCodes(ctx, p, "correct horse")
	if err != nil || newCodes[0] == codes[1] {
		t.Fatal(err)
	}
	in.RecoveryCode = codes[1]
	if _, _, err := s.Login(ctx, in); !errors.Is(err, ErrInvalidTOTP) {
		t.Fatal("old recovery code still works")
	}
	if err := s.DisableTOTP(ctx, p, "wrong"); !errors.Is(err, ErrWrongPassword) {
		t.Fatal(err)
	}
	if err := s.DisableTOTP(ctx, p, "correct horse"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Login(ctx, LoginInput{Username: "admin", Password: "correct horse"}); err != nil {
		t.Fatal(err)
	}
}

func TestUserAdministration(t *testing.T) {
	s, _ := newSvc(t)
	admin, _ := setupAdmin(t, s)
	if _, err := s.CreateUser(ctx, NewUser{Username: "admin", Password: "whatever-pass", Role: RoleRequester}); !errors.Is(err, ErrUsernameTaken) {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, NewUser{Username: "x", Password: "whatever-pass", Role: "boss"}); !errors.Is(err, ErrInvalidRole) {
		t.Fatal(err)
	}
	m, err := s.CreateUser(ctx, NewUser{Username: "Marko", Password: "marko-password", Role: RoleManager})
	if err != nil || !m.Permissions.Has(PermManageLibrary) || m.Permissions.Has(PermRequest4K) {
		t.Fatal(m, err)
	}
	custom := PermRequestMovies | PermRequest4K
	r, err := s.CreateUser(ctx, NewUser{Username: "Iva", Password: "iva-password", Role: RoleRequester, Permissions: &custom})
	if err != nil || r.Permissions != custom {
		t.Fatal(r, err)
	}
	if (Principal{User: r}).Can(PermManageLibrary) || !(Principal{User: r}).Can(PermRequest4K) || !admin.Can(PermManageRequests) {
		t.Fatal("Can")
	}
	// The last admin cannot be demoted, disabled or deleted.
	requester, yes := RoleRequester, true
	if _, err := s.UpdateUser(ctx, Principal{User: m}, admin.User.ID, UserChange{Role: &requester}); !errors.Is(err, ErrLastAdmin) {
		t.Fatal(err)
	}
	if _, err := s.UpdateUser(ctx, Principal{User: m}, admin.User.ID, UserChange{Disabled: &yes}); !errors.Is(err, ErrLastAdmin) {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, Principal{User: m}, admin.User.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, admin, admin.User.ID); !errors.Is(err, ErrSelf) {
		t.Fatal(err)
	}
	if _, err := s.UpdateUser(ctx, admin, admin.User.ID, UserChange{Disabled: &yes}); !errors.Is(err, ErrSelf) {
		t.Fatal(err)
	}
	// With a second admin, the first may be demoted.
	adminRole := RoleAdmin
	if _, err := s.UpdateUser(ctx, admin, m.ID, UserChange{Role: &adminRole}); err != nil {
		t.Fatal(err)
	}
	if u, err := s.UpdateUser(ctx, Principal{User: m}, admin.User.ID, UserChange{Role: &requester}); err != nil || u.Role != RoleRequester {
		t.Fatal(u, err)
	}
	// Password reset signs the user out and clears a lockout.
	_, tok, _ := s.Login(ctx, LoginInput{Username: "iva", Password: "iva-password"})
	if err := s.ResetPassword(ctx, r.ID, "iva-new-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, tok); err == nil {
		t.Fatal("session survived reset")
	}
	if err := s.DeleteUser(ctx, admin, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.User(ctx, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	users, _ := s.Users(ctx)
	if len(users) != 2 {
		t.Fatal(users)
	}
}

func TestResetAdmin(t *testing.T) {
	s, _ := newSvc(t)
	if _, _, err := s.ResetAdmin(ctx, ""); !errors.Is(err, ErrNoAdmin) {
		t.Fatal(err)
	}
	p, tok := setupAdmin(t, s)
	_, _, _ = s.StartTOTP(ctx, p)
	for range MaxFailedLogins {
		_, _, _ = s.Login(ctx, LoginInput{Username: "admin", Password: "bad password"})
	}
	name, pw, err := s.ResetAdmin(ctx, "")
	if err != nil || name != "Admin" || len(pw) < 20 {
		t.Fatal(name, err)
	}
	if _, err := s.Authenticate(ctx, tok); err == nil {
		t.Fatal("old session survived")
	}
	if _, _, err := s.Login(ctx, LoginInput{Username: "admin", Password: pw}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResetAdmin(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	_, _ = s.CreateUser(ctx, NewUser{Username: "req", Password: "req-password", Role: RoleRequester})
	if _, _, err := s.ResetAdmin(ctx, "req"); err == nil {
		t.Fatal("non-admin reset")
	}
}

func TestUpdateLanguage(t *testing.T) {
	s, _ := newSvc(t)
	p, _ := setupAdmin(t, s)
	u, err := s.UpdateLanguage(ctx, p.User.ID, "en")
	if err != nil || u.Language != "en" {
		t.Fatal(u, err)
	}
	if u, _ = s.UpdateLanguage(ctx, p.User.ID, "xx"); u.Language != "en" {
		t.Fatal(u.Language)
	}
}

func TestPermissionBits(t *testing.T) {
	// Stored values must never change.
	if PermRequestMovies != 1 || PermManageRequests != 64 {
		t.Fatal("permission bits renumbered")
	}
	if (Permissions(1<<40) | PermRequestMusic).Clean() != PermRequestMusic {
		t.Fatal("unknown bits kept")
	}
}
