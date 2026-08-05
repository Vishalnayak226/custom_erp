package engines

import (
	"crypto/rand"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"fmt"
	"strings"
)

// MFA recovery codes and authenticator re-enrollment (Stage 32.5).
//
// Before this, MFA had no recovery path at all: a lost or replaced phone
// locked the tenant out of every HR/Admin screen, recoverable only by SSH-ing
// to the box and running cmd/reset_mfa against the database. Two flows fix
// that, both built on the machinery here:
//
//   - single-use recovery codes, issued at enrollment, accepted in place of a
//     TOTP code at /api/v1/auth/mfa/verify;
//   - re-enrollment, which parks a new authenticator's secret in
//     users.mfa_pending_secret until a code proves the new device works.

// RecoveryCodeCount is how many single-use codes are issued per enrollment.
// Ten is the common bar (GitHub, Google) - enough that a user can burn a few
// over the years without being pushed back into a lockout, few enough to fit
// on one printable card.
const RecoveryCodeCount = 10

// recoveryCodeAlphabet is exactly 32 characters, so each character consumes 5
// bits of a random byte with no modulo bias. 0/O/1/I are omitted because
// these codes get written down and typed back in by hand, which is the whole
// point of them - a code that is unreadable on paper is not a recovery path.
const recoveryCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// recoveryCodeLength is the number of alphabet characters per code (excluding
// the display-only dash), giving 10 * 5 = 50 bits of entropy each.
const recoveryCodeLength = 10

// GenerateRecoveryCodes returns RecoveryCodeCount fresh codes in the display
// form "XXXXX-XXXXX". The raw codes are returned to the caller exactly once -
// only their hashes are ever persisted (see ReplaceRecoveryCodes), so this
// return value is the only chance to show them to the user.
func GenerateRecoveryCodes() ([]string, error) {
	codes := make([]string, 0, RecoveryCodeCount)
	for i := 0; i < RecoveryCodeCount; i++ {
		raw := make([]byte, recoveryCodeLength)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		var sb strings.Builder
		for j, b := range raw {
			if j == recoveryCodeLength/2 {
				sb.WriteByte('-')
			}
			sb.WriteByte(recoveryCodeAlphabet[int(b)%len(recoveryCodeAlphabet)])
		}
		codes = append(codes, sb.String())
	}
	return codes, nil
}

// normalizeRecoveryCode makes typed-in codes forgiving: case is folded and
// anything outside the alphabet (dashes, spaces, stray punctuation from a
// copy/paste) is dropped, so "abcde-fghjk", "ABCDE FGHJK" and "ABCDEFGHJK"
// are all the same code.
func normalizeRecoveryCode(code string) string {
	var sb strings.Builder
	for _, r := range strings.ToUpper(code) {
		if strings.ContainsRune(recoveryCodeAlphabet, r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// hashRecoveryCode stores only the SHA-256 hash, never the code - the same
// reasoning as engines/password_reset.go's hashResetToken: a database leak on
// its own must not be replayable into a login. SHA-256 rather than bcrypt is
// deliberate here; these are 50-bit random codes rather than user-chosen
// passwords, so there is no dictionary for a slow hash to defend against, and
// hashing cheaply lets ConsumeRecoveryCode be one indexed lookup instead of a
// bcrypt comparison against every outstanding code.
func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeRecoveryCode(code)))
	return hex.EncodeToString(sum[:])
}

// ReplaceRecoveryCodes discards every code the user currently holds and
// stores the hashes of a new set. Used codes are cleared along with unused
// ones: a regenerated set is meant to invalidate anything previously printed
// or screenshotted, which is the point of offering regeneration at all.
func ReplaceRecoveryCodes(tenantID, userID string, codes []string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s.mfa_recovery_codes WHERE user_id = $1", schema), userID); err != nil {
		return err
	}
	for _, code := range codes {
		if _, err := tx.Exec(fmt.Sprintf(
			"INSERT INTO %s.mfa_recovery_codes (user_id, code_hash) VALUES ($1, $2)", schema),
			userID, hashRecoveryCode(code)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ConsumeRecoveryCode redeems one unused code, returning false if the code
// does not match or has already been spent. The check and the burn are a
// single conditional UPDATE rather than a SELECT followed by an UPDATE, so
// two simultaneous logins cannot both redeem the same code.
func ConsumeRecoveryCode(tenantID, userID, code string) (bool, error) {
	if normalizeRecoveryCode(code) == "" {
		return false, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	res, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.mfa_recovery_codes SET used_at = NOW()
		 WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL`, schema),
		userID, hashRecoveryCode(code))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CountUnusedRecoveryCodes reports how many codes the user has left, so the
// profile screen can warn before the last one is spent.
func CountUnusedRecoveryCodes(tenantID, userID string) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	var n int
	err = db.DB.QueryRow(fmt.Sprintf(
		"SELECT COUNT(*) FROM %s.mfa_recovery_codes WHERE user_id = $1 AND used_at IS NULL", schema),
		userID).Scan(&n)
	return n, err
}

// SetPendingReenrollSecret parks a new authenticator's secret without
// touching the active one. Until ConfirmReenrollSecret promotes it, the
// user's existing device keeps working - so abandoning a half-finished
// device switch cannot itself cause a lockout.
func SetPendingReenrollSecret(tenantID, userID, secret string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf("UPDATE %s.users SET mfa_pending_secret = $1 WHERE id = $2", schema), secret, userID)
	return err
}

// GetPendingReenrollSecret returns the parked secret, or "" if no device
// switch is in progress.
func GetPendingReenrollSecret(tenantID, userID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var secret string
	err = db.DB.QueryRow(fmt.Sprintf(
		"SELECT COALESCE(mfa_pending_secret, '') FROM %s.users WHERE id = $1", schema), userID).Scan(&secret)
	return secret, err
}

// ConfirmReenrollSecret promotes the parked secret to the active one and
// clears the parking slot. Callers must have verified a code against the
// pending secret first - this function does not re-check it.
func ConfirmReenrollSecret(tenantID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.users SET mfa_secret = mfa_pending_secret, mfa_pending_secret = NULL, mfa_enabled = TRUE
		 WHERE id = $1 AND mfa_pending_secret IS NOT NULL`, schema), userID)
	return err
}

// CancelReenroll drops a half-finished device switch.
func CancelReenroll(tenantID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf("UPDATE %s.users SET mfa_pending_secret = NULL WHERE id = $1", schema), userID)
	return err
}
