package auth

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// totpIssuer is the name authenticator apps show next to the account.
const totpIssuer = "Locarr"

func totpAAD(userID int64) string { return "totp:" + strconv.FormatInt(userID, 10) }

// StartTOTP creates a pending secret for the caller; two-factor turns on only
// after EnableTOTP proves the authenticator app has it.
func (s *Service) StartTOTP(ctx context.Context, p Principal) (secret, uri string, err error) {
	if p.SessionID == 0 {
		return "", "", ErrSessionRequired
	}
	if p.User.TOTPEnabled {
		return "", "", ErrTOTPAlreadyOn
	}
	raw, err := newTOTPSecret()
	if err != nil {
		return "", "", err
	}
	sealed, err := s.box.Seal(raw, totpAAD(p.User.ID))
	if err != nil {
		return "", "", err
	}
	if _, err := s.db.W.ExecContext(ctx, `UPDATE users SET totp_pending = ? WHERE id = ?`, sealed, p.User.ID); err != nil {
		return "", "", err
	}
	return b32.EncodeToString(raw), totpURI(totpIssuer, p.User.Username, raw), nil
}

// EnableTOTP turns two-factor on when code matches the pending secret, and
// returns new recovery codes (shown once).
func (s *Service) EnableTOTP(ctx context.Context, p Principal, code string) ([]string, error) {
	if p.SessionID == 0 {
		return nil, ErrSessionRequired
	}
	var (
		pending []byte
		enabled bool
	)
	if err := s.db.W.QueryRowContext(ctx, `SELECT totp_pending, totp_enabled FROM users WHERE id = ?`, p.User.ID).
		Scan(&pending, &enabled); err != nil {
		return nil, err
	}
	if enabled {
		return nil, ErrTOTPAlreadyOn
	}
	if len(pending) == 0 {
		return nil, ErrTOTPNotStarted
	}
	secret, err := s.box.Open(pending, totpAAD(p.User.ID))
	if err != nil {
		return nil, err
	}
	step, ok := verifyTOTP(secret, code, s.now(), 0)
	if !ok {
		return nil, ErrInvalidTOTP
	}
	codes, err := newRecoveryCodes(recoveryCodeN)
	if err != nil {
		return nil, err
	}
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET totp_secret = totp_pending, totp_pending = NULL, totp_enabled = 1,
			totp_last_step = ?, updated_at = ? WHERE id = ?`, step, db.Millis(s.now()), p.User.ID); err != nil {
			return err
		}
		return replaceRecoveryCodes(ctx, tx, p.User.ID, codes)
	})
	return codes, err
}

func replaceRecoveryCodes(ctx context.Context, tx *sql.Tx, userID int64, codes []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, c := range codes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes (user_id, code_hash) VALUES (?, ?)`,
			userID, hashToken(normalizeRecoveryCode(c))); err != nil {
			return err
		}
	}
	return nil
}

// DisableTOTP turns two-factor off after re-checking the password.
func (s *Service) DisableTOTP(ctx context.Context, p Principal, password string) error {
	if p.SessionID == 0 {
		return ErrSessionRequired
	}
	if err := s.verifyOwnPassword(ctx, p.User.ID, password); err != nil {
		return err
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error { return clearTOTP(ctx, tx, p.User.ID) })
}

func clearTOTP(ctx context.Context, tx *sql.Tx, userID int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE users SET totp_secret = NULL, totp_pending = NULL, totp_enabled = 0,
		totp_last_step = 0 WHERE id = ?`, userID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID)
	return err
}

// RegenerateRecoveryCodes replaces the recovery codes after re-checking the password.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, p Principal, password string) ([]string, error) {
	if p.SessionID == 0 {
		return nil, ErrSessionRequired
	}
	if err := s.verifyOwnPassword(ctx, p.User.ID, password); err != nil {
		return nil, err
	}
	u, err := getUser(ctx, s.db.W, p.User.ID)
	if err != nil {
		return nil, err
	}
	if !u.TOTPEnabled {
		return nil, ErrTOTPNotOn
	}
	codes, err := newRecoveryCodes(recoveryCodeN)
	if err != nil {
		return nil, err
	}
	return codes, s.db.Tx(ctx, func(tx *sql.Tx) error { return replaceRecoveryCodes(ctx, tx, p.User.ID, codes) })
}
