package engines

import (
	"crypto/rand"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// Stage 125.8 (user, 2026-10-21: "Yes. Bring that" - remember this device).
//
// After a correct authenticator code, the person may tick "Remember this
// device". The browser then holds a random device token; the server keeps
// only its SHA-256 in the client's mfa_trusted_devices. On a later sign-in
// from that browser the password is still checked as always, and the token
// stands in for the 6-digit code only while every one of these holds:
//
//   - the client's security.mfa_remember_days is above 0 (0 switches the
//     option off and stops every remembered device at once);
//   - the device was remembered fewer than that many days ago, and before
//     the expiry stamped when it was remembered - lowering the setting
//     shortens devices already remembered, raising it never lengthens them;
//   - the account's password has not changed since (credential_version);
//   - the authenticator was not reset or moved to a new phone since
//     (ForgetMFADevices runs on both).
//
// A token alone signs nobody in: it only ever replaces the code, never the
// password, and it belongs to one user of one client.

// SettingMFARememberDays is how long a remembered device skips the code.
const SettingMFARememberDays = "security.mfa_remember_days"

// maxRememberedDevices is how many remembered browsers one account keeps;
// remembering another forgets the oldest.
const maxRememberedDevices = 10

// MFARememberDays is the client's setting, clamped to 0-90.
func MFARememberDays(tenantID string) int {
	days := GetSettingInt(tenantID, SettingMFARememberDays)
	if days < 0 {
		return 0
	}
	if days > 90 {
		return 90
	}
	return days
}

func mfaDeviceHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// RememberMFADevice stores a new remembered device for the user and returns
// its token for the browser to keep, with the days it lasts. With the option
// switched off it returns "", 0 and stores nothing.
func RememberMFADevice(tenantID, userID, label string) (string, int, error) {
	days := MFARememberDays(tenantID)
	if days <= 0 {
		return "", 0, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", 0, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", 0, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if len(label) > 160 {
		label = label[:160]
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %[1]s.mfa_trusted_devices (user_id, token_hash, credential_version, label, expires_at)
		SELECT id, $2, credential_version, $3, NOW() + make_interval(days => $4)
		  FROM %[1]s.users WHERE id = $1`, schema),
		userID, mfaDeviceHash(token), label, days); err != nil {
		return "", 0, err
	}
	// Expired rows, and anything beyond the newest maxRememberedDevices.
	_, _ = db.DB.Exec(fmt.Sprintf(`
		DELETE FROM %[1]s.mfa_trusted_devices WHERE user_id = $1 AND (expires_at <= NOW() OR id NOT IN (
		  SELECT id FROM %[1]s.mfa_trusted_devices WHERE user_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2))`, schema),
		userID, maxRememberedDevices)
	return token, days, nil
}

// TrustedMFADevice reports whether token is a live remembered device of this
// user at this credential version, and marks it used. Any failure - the
// table missing on a schema not yet migrated included - answers false, so
// the person is simply asked for the code.
func TrustedMFADevice(tenantID, userID, token string, credentialVersion int) bool {
	days := MFARememberDays(tenantID)
	if days <= 0 || strings.TrimSpace(token) == "" {
		return false
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	var id int64
	err = db.DB.QueryRow(fmt.Sprintf(`
		UPDATE %s.mfa_trusted_devices SET last_used_at = NOW()
		 WHERE user_id = $1 AND token_hash = $2 AND credential_version = $3
		   AND expires_at > NOW() AND created_at > NOW() - make_interval(days => $4)
		RETURNING id`, schema), userID, mfaDeviceHash(token), credentialVersion, days).Scan(&id)
	return err == nil
}

// ForgetMFADevices drops every remembered device of the user: on a 2FA reset,
// a move to a new authenticator, or the person's own "Forget" in My Profile.
func ForgetMFADevices(tenantID, userID string) (int64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	if !mfaDeviceTableExists(schema) {
		return 0, nil
	}
	res, err := db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.mfa_trusted_devices WHERE user_id = $1`, schema), userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountMFADevices is how many live remembered devices the user has.
func CountMFADevices(tenantID, userID string) int {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || !mfaDeviceTableExists(schema) {
		return 0
	}
	var n int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.mfa_trusted_devices WHERE user_id = $1 AND expires_at > NOW()`, schema), userID).Scan(&n); err != nil {
		return 0
	}
	return n
}

func mfaDeviceTableExists(schema string) bool {
	var exists bool
	err := db.DB.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+".mfa_trusted_devices").Scan(&exists)
	return err == nil && exists
}
