package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
)

// Policy constants.
const (
	MaxFailedLogins  = 5
	LockoutDuration  = 15 * time.Minute
	SessionTTL       = 30 * 24 * time.Hour // sliding: extended while the session is used
	touchInterval    = time.Minute         // how often last-seen times are written
	maxSetupFailures = 10
	setupCooldown    = time.Minute
	minPasswordLen   = 8
	maxPasswordLen   = 256
	recoveryCodeN    = 10
)

// Errors returned by the service; the API layer maps them to codes and statuses.
var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrSetupDone          = errors.New("setup has already been completed")
	ErrBadSetupCode       = errors.New("invalid setup code")
	ErrSetupThrottled     = errors.New("too many wrong setup codes; try again in a minute")
	ErrUsernameTaken      = errors.New("username is already taken")
	ErrInvalidUsername    = errors.New("username may contain letters, digits, spaces and . _ - @")
	ErrWeakPassword       = errors.New("password must be 8 to 256 characters and differ from the username")
	ErrInvalidRole        = errors.New("unknown role")
	ErrNotFound           = errors.New("not found")
	ErrLastAdmin          = errors.New("the last enabled administrator cannot be removed, demoted or disabled")
	ErrSelf               = errors.New("you cannot delete or disable your own account")
	ErrUnauthenticated    = errors.New("not signed in")
	ErrSessionRequired    = errors.New("this action needs a signed-in session, not an API key")
	ErrTOTPRequired       = errors.New("two-factor code required")
	ErrInvalidTOTP        = errors.New("invalid two-factor code")
	ErrTOTPAlreadyOn      = errors.New("two-factor authentication is already enabled")
	ErrTOTPNotStarted     = errors.New("start two-factor setup first")
	ErrTOTPNotOn          = errors.New("two-factor authentication is not enabled")
	ErrInvalidName        = errors.New("a name is required")
	ErrNoAdmin            = errors.New("no administrator exists yet; finish first-run setup in the web interface")
	ErrWrongPassword      = errors.New("the current password is wrong")
)

// LockedError is returned while an account is locked after failed sign-ins.
type LockedError struct{ Until time.Time }

func (e *LockedError) Error() string {
	return fmt.Sprintf("account locked after %d failed sign-ins; try again later", MaxFailedLogins)
}

// User is an account.
type User struct {
	ID          int64
	Username    string
	Role        Role
	Permissions Permissions
	Language    string
	Disabled    bool
	TOTPEnabled bool
	LockedUntil *time.Time
	CreatedAt   time.Time
	LastLoginAt *time.Time
}

// Principal is the authenticated caller: a session or an API key.
type Principal struct {
	User      User
	SessionID int64 // zero for API keys
	APIKeyID  int64 // zero for sessions
	ReadOnly  bool  // a read-only API key
	// IncludeAdult: the API key was created with "include adult" (D-101).
	IncludeAdult bool
}

// Device describes the client creating a session.
type Device struct{ Name, Client, IP string }

// Service implements accounts, sessions and API keys.
type Service struct {
	db        *db.DB
	box       *secrets.Box
	setupCode string
	now       func() time.Time

	providers []Provider

	mu            sync.Mutex
	setupFailures int
	setupBlocked  time.Time
}

// NewService returns a Service. setupCode guards first-run setup; box encrypts
// two-factor secrets.
func NewService(d *db.DB, box *secrets.Box, setupCode string) *Service {
	return &Service{db: d, box: box, setupCode: setupCode, now: time.Now}
}

var usernameRe = regexp.MustCompile(`^[\p{L}\p{N}._@\- ]{1,64}$`)

// NormalizeUsername trims and validates a username.
func NormalizeUsername(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !usernameRe.MatchString(s) {
		return "", ErrInvalidUsername
	}
	return s, nil
}

func usernameKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func checkPassword(username, password string) error {
	n := len([]rune(password))
	if n < minPasswordLen || n > maxPasswordLen || strings.EqualFold(strings.TrimSpace(password), strings.TrimSpace(username)) {
		return ErrWeakPassword
	}
	return nil
}

func normLanguage(l string) string {
	if l == "hr" {
		return "hr"
	}
	return "en"
}

const userColumns = `u.id, u.username, u.role, u.permissions, u.language, u.disabled, u.totp_enabled, u.locked_until,
	u.created_at, u.last_login_at`

func scanUser(row interface{ Scan(...any) error }, extra ...any) (User, error) {
	var (
		u              User
		perms          int64
		locked, last   sql.NullInt64
		created        int64
		disabled, totp bool
	)
	dest := append([]any{&u.ID, &u.Username, &u.Role, &perms, &u.Language, &disabled, &totp, &locked, &created, &last}, extra...)
	if err := row.Scan(dest...); err != nil {
		return User{}, err
	}
	u.Permissions = Permissions(perms).Clean() // #nosec G115 -- stored from a uint64 bitset
	u.Disabled, u.TOTPEnabled = disabled, totp
	u.LockedUntil, u.CreatedAt, u.LastLoginAt = db.NullTime(locked), db.Time(created), db.NullTime(last)
	if u.LockedUntil != nil && !u.LockedUntil.After(time.Now()) {
		u.LockedUntil = nil
	}
	return u, nil
}

// SetupRequired reports whether no administrator exists yet.
func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	var n int
	err := s.db.R.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role = 'admin'`).Scan(&n)
	return n == 0, err
}

func (s *Service) checkSetupCode(code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if now.Before(s.setupBlocked) {
		return ErrSetupThrottled
	}
	if setupCodeMatches(code, s.setupCode) {
		s.setupFailures = 0
		return nil
	}
	s.setupFailures++
	if s.setupFailures >= maxSetupFailures {
		s.setupFailures = 0
		s.setupBlocked = now.Add(setupCooldown)
	}
	return ErrBadSetupCode
}

// Setup creates the first administrator and signs it in. It only works while no
// administrator exists; the write transaction makes concurrent setups race-free.
func (s *Service) Setup(ctx context.Context, code, username, password, language string, dev Device) (Principal, string, error) {
	if required, err := s.SetupRequired(ctx); err != nil {
		return Principal{}, "", err
	} else if !required {
		return Principal{}, "", ErrSetupDone
	}
	if err := s.checkSetupCode(code); err != nil {
		return Principal{}, "", err
	}
	username, err := NormalizeUsername(username)
	if err != nil {
		return Principal{}, "", err
	}
	if err := checkPassword(username, password); err != nil {
		return Principal{}, "", err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return Principal{}, "", err
	}
	var (
		p     Principal
		token string
	)
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role = 'admin'`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrSetupDone
		}
		u, err := s.insertUser(ctx, tx, username, hash, RoleAdmin, DefaultPermissions(RoleAdmin), normLanguage(language))
		if err != nil {
			return err
		}
		p, token, err = s.createSession(ctx, tx, u, dev)
		return err
	})
	return p, token, err
}

func (s *Service) insertUser(ctx context.Context, q db.Querier, username, hash string, role Role, perms Permissions, lang string) (User, error) {
	now := db.Millis(s.now())
	var id int64
	err := q.QueryRowContext(ctx, `INSERT INTO users (username, username_key, password_hash, role, permissions, language,
			created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (username_key) DO NOTHING RETURNING id`,
		username, usernameKey(username), hash, string(role), int64(perms.ForRole(role)), lang, now, now).Scan(&id) // #nosec G115
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUsernameTaken
	}
	if err != nil {
		return User{}, err
	}
	return getUser(ctx, q, id)
}

func getUser(ctx context.Context, q db.Querier, id int64) (User, error) {
	u, err := scanUser(q.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users u WHERE u.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *Service) createSession(ctx context.Context, q db.Querier, u User, dev Device) (Principal, string, error) {
	token, hash, err := newToken("")
	if err != nil {
		return Principal{}, "", err
	}
	now := s.now()
	name := strings.TrimSpace(dev.Name)
	if name == "" {
		name = "Unknown device"
	}
	client := strings.TrimSpace(dev.Client)
	if client == "" {
		client = "API"
	}
	var id int64
	if err := q.QueryRowContext(ctx, `INSERT INTO sessions (user_id, token_hash, device_name, client, ip, created_at,
			last_seen_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		u.ID, hash, truncate(name, 100), truncate(client, 100), dev.IP, db.Millis(now), db.Millis(now),
		db.Millis(now.Add(SessionTTL))).Scan(&id); err != nil {
		return Principal{}, "", err
	}
	if _, err := q.ExecContext(ctx, `UPDATE users SET last_login_at = ?, failed_logins = 0, locked_until = NULL WHERE id = ?`,
		db.Millis(now), u.ID); err != nil {
		return Principal{}, "", err
	}
	u.LastLoginAt = &now
	return Principal{User: u, SessionID: id}, token, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// LoginInput is a sign-in attempt.
type LoginInput struct {
	Username, Password     string
	TOTPCode, RecoveryCode string
	Device                 Device
}

// Login verifies credentials (and the second factor) and creates a session.
func (s *Service) Login(ctx context.Context, in LoginInput) (Principal, string, error) {
	var (
		hash     string
		secret   []byte
		lastStep int64
	)
	u, err := scanUser(s.db.W.QueryRowContext(ctx, `SELECT `+userColumns+`, u.password_hash, u.totp_secret, u.totp_last_step
		FROM users u WHERE u.username_key = ?`, usernameKey(in.Username)), &hash, &secret, &lastStep)
	if errors.Is(err, sql.ErrNoRows) {
		_, _ = VerifyPassword(in.Password, dummyHash) // equalise timing
		return Principal{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return Principal{}, "", err
	}
	now := s.now()
	var lockedUntil sql.NullInt64
	if err := s.db.W.QueryRowContext(ctx, `SELECT locked_until FROM users WHERE id = ?`, u.ID).Scan(&lockedUntil); err != nil {
		return Principal{}, "", err
	}
	if lockedUntil.Valid && now.Before(db.Time(lockedUntil.Int64)) {
		return Principal{}, "", &LockedError{Until: db.Time(lockedUntil.Int64)}
	}
	ok, err := VerifyPassword(in.Password, hash)
	if err != nil {
		return Principal{}, "", err
	}
	if !ok {
		return Principal{}, "", s.failLogin(ctx, u.ID, ErrInvalidCredentials)
	}
	if u.Disabled {
		return Principal{}, "", ErrInvalidCredentials
	}
	if u.TOTPEnabled {
		if err := s.checkSecondFactor(ctx, u, secret, lastStep, in); err != nil {
			return Principal{}, "", err
		}
	}
	var (
		p     Principal
		token string
	)
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		p, token, err = s.createSession(ctx, tx, u, in.Device)
		return err
	})
	return p, token, err
}

func (s *Service) checkSecondFactor(ctx context.Context, u User, sealed []byte, lastStep int64, in LoginInput) error {
	code, recovery := strings.TrimSpace(in.TOTPCode), strings.TrimSpace(in.RecoveryCode)
	switch {
	case code == "" && recovery == "":
		return ErrTOTPRequired
	case code != "":
		secret, err := s.box.Open(sealed, totpAAD(u.ID))
		if err != nil {
			return err
		}
		step, ok := verifyTOTP(secret, code, s.now(), lastStep)
		if !ok {
			return s.failLogin(ctx, u.ID, ErrInvalidTOTP)
		}
		// Each code works once: remember the step (only if it is newer).
		res, err := s.db.W.ExecContext(ctx, `UPDATE users SET totp_last_step = ? WHERE id = ? AND totp_last_step < ?`, step, u.ID, step)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return s.failLogin(ctx, u.ID, ErrInvalidTOTP)
		}
		return nil
	default:
		res, err := s.db.W.ExecContext(ctx, `UPDATE recovery_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`,
			db.Millis(s.now()), u.ID, hashToken(normalizeRecoveryCode(recovery)))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return s.failLogin(ctx, u.ID, ErrInvalidTOTP)
		}
		return nil
	}
}

// failLogin counts a failed attempt and locks the account after too many.
func (s *Service) failLogin(ctx context.Context, userID int64, cause error) error {
	until := db.Millis(s.now().Add(LockoutDuration))
	var locked sql.NullInt64
	err := s.db.W.QueryRowContext(ctx, `UPDATE users SET
			locked_until = CASE WHEN failed_logins + 1 >= ? THEN ? ELSE locked_until END,
			failed_logins = CASE WHEN failed_logins + 1 >= ? THEN 0 ELSE failed_logins + 1 END
		WHERE id = ? RETURNING locked_until`, MaxFailedLogins, until, MaxFailedLogins, userID).Scan(&locked)
	if err != nil {
		return err
	}
	if locked.Valid && locked.Int64 == until {
		return &LockedError{Until: db.Time(until)}
	}
	return cause
}

// Authenticate resolves a session token or API key.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	if token == "" || len(token) > 200 {
		return Principal{}, ErrUnauthenticated
	}
	now := s.now()
	h := hashToken(token)
	if strings.HasPrefix(token, APIKeyPrefix) {
		var (
			keyID        int64
			readOnly     bool
			lastUsed     sql.NullInt64
			includeAdult bool
		)
		u, err := scanUser(s.db.R.QueryRowContext(ctx, `SELECT `+userColumns+`, k.id, k.read_only, k.last_used_at, k.include_adult
			FROM api_keys k JOIN users u ON u.id = k.user_id WHERE k.key_hash = ?`, h), &keyID, &readOnly, &lastUsed, &includeAdult)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && u.Disabled) {
			return Principal{}, ErrUnauthenticated
		}
		if err != nil {
			return Principal{}, err
		}
		if !lastUsed.Valid || now.Sub(db.Time(lastUsed.Int64)) > touchInterval {
			_, _ = s.db.W.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, db.Millis(now), keyID)
		}
		return Principal{User: u, APIKeyID: keyID, ReadOnly: readOnly, IncludeAdult: includeAdult}, nil
	}
	var (
		sessionID int64
		lastSeen  int64
	)
	u, err := scanUser(s.db.R.QueryRowContext(ctx, `SELECT `+userColumns+`, s.id, s.last_seen_at
		FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ? AND s.expires_at > ?`, h, db.Millis(now)),
		&sessionID, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && u.Disabled) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, err
	}
	if now.Sub(db.Time(lastSeen)) > touchInterval {
		_, _ = s.db.W.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?`,
			db.Millis(now), db.Millis(now.Add(SessionTTL)), sessionID)
	}
	return Principal{User: u, SessionID: sessionID}, nil
}

// Logout ends a session.
func (s *Service) Logout(ctx context.Context, p Principal) error {
	if p.SessionID == 0 {
		return ErrSessionRequired
	}
	_, err := s.db.W.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, p.SessionID)
	return err
}

// PruneSessions deletes expired sessions and returns how many.
func (s *Service) PruneSessions(ctx context.Context) (int64, error) {
	res, err := s.db.W.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, db.Millis(s.now()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpdateLanguage sets the user's interface language.
func (s *Service) UpdateLanguage(ctx context.Context, userID int64, lang string) (User, error) {
	if _, err := s.db.W.ExecContext(ctx, `UPDATE users SET language = ?, updated_at = ? WHERE id = ?`,
		normLanguage(lang), db.Millis(s.now()), userID); err != nil {
		return User{}, err
	}
	return getUser(ctx, s.db.W, userID)
}

// ChangePassword changes the caller's password and signs out its other sessions.
func (s *Service) ChangePassword(ctx context.Context, p Principal, current, next string) error {
	if p.SessionID == 0 {
		return ErrSessionRequired
	}
	if err := s.verifyOwnPassword(ctx, p.User.ID, current); err != nil {
		return err
	}
	if err := checkPassword(p.User.Username, next); err != nil {
		return err
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
			hash, db.Millis(s.now()), p.User.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id <> ?`, p.User.ID, p.SessionID)
		return err
	})
}

// verifyOwnPassword re-checks the password before a sensitive change.
func (s *Service) verifyOwnPassword(ctx context.Context, userID int64, password string) error {
	var hash string
	if err := s.db.W.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash); err != nil {
		return err
	}
	ok, err := VerifyPassword(password, hash)
	if err != nil {
		return err
	}
	if !ok {
		return ErrWrongPassword
	}
	return nil
}

// Session is a signed-in device.
type Session struct {
	ID         int64
	DeviceName string
	Client     string
	IP         string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// Sessions lists a user's active sessions, most recently used first.
func (s *Service) Sessions(ctx context.Context, userID int64) ([]Session, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, device_name, client, ip, created_at, last_seen_at FROM sessions
		WHERE user_id = ? AND expires_at > ? ORDER BY last_seen_at DESC, id DESC`, userID, db.Millis(s.now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var (
			x             Session
			created, seen int64
		)
		if err := rows.Scan(&x.ID, &x.DeviceName, &x.Client, &x.IP, &created, &seen); err != nil {
			return nil, err
		}
		x.CreatedAt, x.LastSeenAt = db.Time(created), db.Time(seen)
		out = append(out, x)
	}
	return out, rows.Err()
}

// RevokeSession signs out one of the user's sessions.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID int64) error {
	res, err := s.db.W.ExecContext(ctx, `DELETE FROM sessions WHERE id = ? AND user_id = ?`, sessionID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
