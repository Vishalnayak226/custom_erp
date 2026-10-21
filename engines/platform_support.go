package engines

import (
	"crypto/rand"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Stage 125.2 (user, 2026-10-20: "I can't add hundred of diff autheticator
// for diff client"). The platform operator signs in once, on their own client,
// with their own password and authenticator, and opens any other client from
// the Control Panel. Each client holds one hidden Super Admin, the support
// user, that nobody can sign in as:
//
//   - its password_hash is not a bcrypt hash, so the login's bcrypt-only check
//     (handlers_auth.go, 49.2.2) can never accept any password for it;
//   - the only way in is a single-use code the operator's signed-in session
//     asks for (IssueSupportHandoff), redeemed within supportHandoffTTL on the
//     client's own hostname (RedeemSupportHandoff) for an ordinary session;
//   - it is left out of the client's user lists, pickers, counts and
//     forgot-password lookup (is_platform_support).
//
// Every opening is written to the client's audit log and its evidence trail
// with the operator's name. Why not one authenticator key copied into every
// client: each client stores its key as plain text (mfa.go), so one leaked
// client would have exposed all of them.

// PlatformSupportUsername is the support user's id and username in every client.
const PlatformSupportUsername = "wholeops-support"

// TenantEventOperatorOpened is the evidence-trail event for an opening.
const TenantEventOperatorOpened = "operator_opened"

// supportHandoffTTL is how long a hand-off code waits to be redeemed - long
// enough for a new tab to load, short enough that a copied link is worthless.
const supportHandoffTTL = 60 * time.Second

// supportNoPassword is stored as the support user's password_hash. It is not
// a bcrypt hash, so no password ever matches it.
const supportNoPassword = "!no-password:platform-support"

// ErrSupportHandoffInvalid is a code that is unknown, used or expired.
var ErrSupportHandoffInvalid = errors.New("this link has expired or was already used - open the client again from the Control Panel")

// TenantURL is a client's own address, https://<slug>.<TENANT_BASE_DOMAIN>/,
// or "" when it has no hostname or the platform has no base domain.
func TenantURL(tenantID string) (string, error) {
	base := strings.ToLower(strings.TrimSpace(os.Getenv("TENANT_BASE_DOMAIN")))
	var slug string
	err := db.DB.QueryRow(`SELECT COALESCE(host_slug, '') FROM public.tenants WHERE tenant_id = $1`, tenantID).Scan(&slug)
	if err == sql.ErrNoRows {
		return "", ErrTenantNotRegistered
	}
	if err != nil || slug == "" || base == "" {
		return "", err
	}
	return "https://" + slug + "." + base + "/", nil
}

// NotSupportUserSQL is a WHERE predicate that leaves the support user out of
// a users query whose row is named alias (the table name when unaliased). It
// reads the flag through to_jsonb so it also answers on a schema the Stage
// 125.2 migration has not reached yet.
func NotSupportUserSQL(alias string) string {
	return "COALESCE((to_jsonb(" + alias + ")->>'is_platform_support')::boolean, FALSE) = FALSE"
}

// IsPlatformSupportUser reports whether userID is the client's support user.
// Fails closed: a lookup error answers true, so an admin action that cannot
// tell is refused rather than allowed onto the operator's account.
func IsPlatformSupportUser(tenantID, userID string) bool {
	userID = strings.TrimSpace(userID)
	if strings.EqualFold(userID, PlatformSupportUsername) {
		return true
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return true
	}
	var support bool
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT NOT (%s) FROM %s.users WHERE id = $1`, NotSupportUserSQL("users"), schema), userID).Scan(&support)
	if err == sql.ErrNoRows {
		return false
	}
	return err != nil || support
}

func supportHandoffHash(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}

// IssueSupportHandoff creates the client's support user, or puts it back to
// active Super Admin, and returns a fresh single-use hand-off code. A newer
// code replaces an unredeemed older one. operator names who asked, for the
// client's audit log and evidence trail.
func IssueSupportHandoff(tenantID, operator string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)

	// The WHERE on the conflict arm is the guard: an ordinary user who
	// somehow holds this id is never turned into the support user.
	res, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %[1]s.users (id, username, password_hash, role, status, must_change_password,
		                         is_platform_support, handoff_token_hash, handoff_expires_at)
		VALUES ($1, $1, $2, $3, 'Active', FALSE, TRUE, $4, NOW() + make_interval(secs => $5))
		ON CONFLICT (id) DO UPDATE SET
		    password_hash = EXCLUDED.password_hash, role = EXCLUDED.role, status = 'Active',
		    must_change_password = FALSE, failed_login_count = 0, locked_until = NULL,
		    handoff_token_hash = EXCLUDED.handoff_token_hash, handoff_expires_at = EXCLUDED.handoff_expires_at
		WHERE %[1]s.users.is_platform_support`, schema),
		PlatformSupportUsername, supportNoPassword, RoleSuperAdmin, supportHandoffHash(code), supportHandoffTTL.Seconds())
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", fmt.Errorf("this client already has an ordinary user named %q - rename it before opening the client", PlatformSupportUsername)
	}
	detail := "Opened from the Control Panel by " + operator
	LogAuditEvent(tenantID, PlatformSupportUsername, "LOGIN", "OPERATOR_OPEN_ISSUED", detail)
	_ = RecordTenantLifecycleEvent(tenantID, TenantEventOperatorOpened, operator, detail)
	return code, nil
}

// RedeemSupportHandoff spends a hand-off code - at most once, and only before
// it expires - and returns what a session token for the support user carries.
func RedeemSupportHandoff(tenantID, code string) (userID, username, role, locationCode string, credentialVersion int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", "", "", 0, err
	}
	if strings.TrimSpace(code) == "" {
		return "", "", "", "", 0, ErrSupportHandoffInvalid
	}
	// One UPDATE both checks and spends the code, so two tabs racing with the
	// same code cannot both win. Expiry is NOW() against NOW(): the database
	// clock set it and the database clock judges it.
	err = db.DB.QueryRow(fmt.Sprintf(`
		UPDATE %s.users SET handoff_token_hash = NULL, handoff_expires_at = NULL
		 WHERE is_platform_support AND status = 'Active'
		   AND handoff_token_hash = $1 AND handoff_expires_at > NOW()
		RETURNING id, username, role, COALESCE(location_code, ''), credential_version`, schema),
		supportHandoffHash(code)).Scan(&userID, &username, &role, &locationCode, &credentialVersion)
	if err == sql.ErrNoRows {
		return "", "", "", "", 0, ErrSupportHandoffInvalid
	}
	if err != nil {
		return "", "", "", "", 0, err
	}
	LogAuditEvent(tenantID, username, "LOGIN", "OPERATOR_SESSION", "Signed in through a Control Panel hand-off")
	return userID, username, role, locationCode, credentialVersion, nil
}
