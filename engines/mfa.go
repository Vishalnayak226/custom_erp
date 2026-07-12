package engines

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"custom_erp/db"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
)

const (
	totpDigits    = 6
	totpPeriod    = 30 * time.Second
	totpSkewSteps = 1 // tolerate +/-1 step (+/-30s) of client/server clock drift
)

// mfaRequiredRoles lists the roles SEC-V2 Sec.12 marks MFA-mandatory for
// ("admin/finance/IT/super users/production support"). This codebase has no
// distinct Finance/IT role today - HR/Admin is the only privileged,
// admin-equivalent role, so it stands in for the whole group.
var mfaRequiredRoles = map[string]bool{
	"HR/Admin": true,
}

// RequiresMFA reports whether a role must complete TOTP enrollment/challenge
// before a full session token is issued.
func RequiresMFA(role string) bool {
	return mfaRequiredRoles[role]
}

var totpBase32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret returns a fresh, high-entropy base32 TOTP secret (160
// bits, the RFC 4226-recommended HMAC-SHA1 key size).
func GenerateTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return totpBase32.EncodeToString(raw), nil
}

// totpCodeAt computes the RFC 6238 TOTP code for secretBase32 at a given
// 30-second time-step counter.
func totpCodeAt(secretBase32 string, counter uint64) (string, error) {
	key, err := totpBase32.DecodeString(strings.ToUpper(secretBase32))
	if err != nil {
		return "", err
	}
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)

	h := hmac.New(sha1.New, key)
	h.Write(buf)
	sum := h.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	mod := uint32(math.Pow10(totpDigits))
	return fmt.Sprintf("%0*d", totpDigits, code%mod), nil
}

// GenerateTOTPCode returns the current valid TOTP code for secretBase32 -
// the same computation an authenticator app performs. Used by clients that
// need to prove possession of a secret programmatically (e.g. this
// project's own integration test completing real MFA enrollment).
func GenerateTOTPCode(secretBase32 string) (string, error) {
	now := uint64(time.Now().Unix()) / uint64(totpPeriod.Seconds())
	return totpCodeAt(secretBase32, now)
}

// VerifyTOTPCode checks code against secretBase32 at the current time step,
// tolerating up to totpSkewSteps of clock drift in either direction.
func VerifyTOTPCode(secretBase32, code string) bool {
	if len(code) != totpDigits {
		return false
	}
	now := uint64(time.Now().Unix()) / uint64(totpPeriod.Seconds())
	for skew := -totpSkewSteps; skew <= totpSkewSteps; skew++ {
		counter := now
		if skew < 0 {
			shift := uint64(-skew)
			if shift > counter {
				continue
			}
			counter -= shift
		} else {
			counter += uint64(skew)
		}
		expected, err := totpCodeAt(secretBase32, counter)
		if err == nil && expected == code {
			return true
		}
	}
	return false
}

// BuildOTPAuthURL returns an otpauth:// URI an authenticator app can accept
// (rendered as a QR code client-side, or entered manually).
func BuildOTPAuthURL(secretBase32, accountName, issuer string) string {
	label := url.PathEscape(fmt.Sprintf("%s:%s", issuer, accountName))
	return fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=%s&algorithm=SHA1&digits=%d&period=%d",
		label, secretBase32, url.QueryEscape(issuer), totpDigits, int(totpPeriod.Seconds()))
}

// GetUserMFAStatus returns whether MFA is active and, if a secret has ever
// been generated (pending or active), what it is.
func GetUserMFAStatus(tenantID, userID string) (enabled bool, secret string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, "", err
	}
	query := fmt.Sprintf("SELECT mfa_enabled, COALESCE(mfa_secret, '') FROM %s.users WHERE id = $1", schema)
	err = db.DB.QueryRow(query, userID).Scan(&enabled, &secret)
	return enabled, secret, err
}

// SetPendingMFASecret stores a newly generated TOTP secret without
// activating it - activation only happens once the user proves possession
// of it by submitting a valid code (see ActivateMFA).
func SetPendingMFASecret(tenantID, userID, secret string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf("UPDATE %s.users SET mfa_secret = $1 WHERE id = $2", schema), secret, userID)
	return err
}

// ActivateMFA marks the tenant's pending secret as confirmed and active.
func ActivateMFA(tenantID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf("UPDATE %s.users SET mfa_enabled = TRUE WHERE id = $1", schema), userID)
	return err
}

// LookupUserRoleAndUsername resolves the role/username needed to issue a
// full session token once MFA enrollment/challenge succeeds - the purpose
// token that carried the request only holds id/username, not role.
func LookupUserRoleAndUsername(tenantID, userID string) (role, username string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", err
	}
	query := fmt.Sprintf("SELECT role, username FROM %s.users WHERE id = $1", schema)
	err = db.DB.QueryRow(query, userID).Scan(&role, &username)
	return role, username, err
}
