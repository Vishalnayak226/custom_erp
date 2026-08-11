package server

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"custom_erp/db"
	"custom_erp/engines"

	"golang.org/x/crypto/bcrypt"
)

// Login and TOTP MFA enrollment/activation/verification handlers.

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	var u struct {
		ID               string
		Username         string
		PasswordHash     string
		Role             string
		LocationCode     string
		FailedLoginCount int
		IsLocked         bool
	}

	// Query user details. is_locked is computed in SQL (locked_until > NOW())
	// rather than scanned and compared in Go - a real bug caught live while
	// verifying this: lib/pq returns a tz-naive `timestamp` column's value
	// tagged as UTC, but this app server's local clock is IST (UTC+5:30), so
	// comparing time.Now() (local) against the scanned value directly made a
	// genuinely-expired lock look like it was still ~5.5 hours in the
	// future. Computing the comparison in Postgres, against Postgres's own
	// NOW(), sidesteps any app-server-vs-database clock/timezone
	// reconciliation entirely rather than trying to get it right in Go.
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id, username, password_hash, role, location_code, failed_login_count, (locked_until IS NOT NULL AND locked_until > NOW())
		FROM %s.users
		WHERE username = $1 AND status = 'Active'`, schema), req.Username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.LocationCode, &u.FailedLoginCount, &u.IsLocked)
	if err != nil {
		// Generic security error message
		writeAPIError(w, r, "USERAC-0021", "")
		return
	}

	// Account-level brute-force lockout (Stage 14.21-14.24), independent of
	// and in addition to the existing IP-scoped rate limiter (Stage 13.14) -
	// that one alone doesn't slow down an attempt distributed across many
	// IPs against a single account. Deliberately the same generic error
	// message as every other login failure here - a distinguishable "your
	// account is locked" response would let an attacker confirm the
	// username is valid, the exact leak this endpoint's error messages
	// have consistently avoided elsewhere (e.g. a deactivated Employee).
	if u.IsLocked {
		writeAPIError(w, r, "USERAC-0021", "")
		return
	}

	// Check password with bcrypt (supports fallback check for local seed configs)
	err = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password))
	if err != nil && u.PasswordHash != req.Password {
		newCount := u.FailedLoginCount + 1
		if newCount >= accountLockoutThresholdFor(tenantID) {
			// NOW() + make_interval(...) is also computed in Postgres for the
			// same reason as the is_locked check above - the lockout window's
			// end time must be reckoned against the same clock it's later
			// compared to.
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET failed_login_count = $1, locked_until = NOW() + make_interval(mins => $2) WHERE id = $3`, schema),
				newCount, accountLockoutDurationMinutesFor(tenantID), u.ID)
		} else {
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET failed_login_count = $1 WHERE id = $2`, schema), newCount, u.ID)
		}
		writeAPIError(w, r, "USERAC-0021", "")
		return
	}

	// Correct password: clear any accumulated failure count/lock.
	if u.FailedLoginCount > 0 {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET failed_login_count = 0, locked_until = NULL WHERE id = $1`, schema), u.ID)
	}

	// MFA-mandatory roles (SEC-V2 Sec.12) never get a full session token
	// straight out of /login - they're routed into enrollment (first time)
	// or a TOTP challenge (subsequently) instead.
	if engines.RequiresMFA(u.Role) {
		enabled, _, mfaErr := engines.GetUserMFAStatus(tenantID, u.ID)
		if mfaErr != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to resolve MFA status")
			return
		}
		if !enabled {
			enrollToken := engines.SignPurposeToken(u.ID, u.Username, tenantID, "mfa_enroll", 10*time.Minute)
			engines.LogAuditEvent(tenantID, u.Username, "LOGIN", "MFA_ENROLLMENT_REQUIRED", "Password correct; TOTP enrollment required before a session can be issued")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"mfa_enrollment_required": true,
				"enrollment_token":        enrollToken,
			})
			return
		}
		challengeToken := engines.SignPurposeToken(u.ID, u.Username, tenantID, "mfa_challenge", 5*time.Minute)
		engines.LogAuditEvent(tenantID, u.Username, "LOGIN", "MFA_CHALLENGE_ISSUED", "Password correct; awaiting TOTP code")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"mfa_required":    true,
			"challenge_token": challengeToken,
		})
		return
	}

	// 24.1: the user's own location_code, not a hardcoded "HO" - this used to
	// silently defeat every location-scoped authorization check in
	// handleGenericDoc (a Store Manager's token claimed "HO" access same as
	// everyone else's). Falls back to "HO" for legacy rows via the column's
	// own DEFAULT (db/migrations_stage24_security.sql), so an unassigned
	// user behaves exactly as before rather than losing access.
	token := engines.SignToken(u.ID, u.Username, u.Role, tenantID, u.LocationCode)

	engines.LogAuditEvent(tenantID, u.Username, "LOGIN", "SUCCESS", fmt.Sprintf("User logged in successfully with role %s", u.Role))

	_ = json.NewEncoder(w).Encode(map[string]string{
		"token": token,
		// Stage 40.3: the canonical name, so the profile chip never shows the
		// pre-rename spelling on a database that has not been migrated yet.
		"role":  engines.CanonicalRole(u.Role),
		"user":  u.Username,
	})
}

// handleMFAEnroll issues a fresh (pending, not-yet-active) TOTP secret for
// the account named in a mfa_enroll purpose token. Safe to call more than
// once before activation - each call simply replaces the pending secret.
func handleMFAEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Resolved-Purpose") != "mfa_enroll" {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "This endpoint requires a pending MFA enrollment token from /api/v1/login")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	username := r.Header.Get("Resolved-Username")

	secret, err := engines.GenerateTOTPSecret()
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to generate MFA secret")
		return
	}
	if err := engines.SetPendingMFASecret(tenantID, userID, secret); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to store MFA secret")
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"secret":      secret,
		"otpauth_url": engines.BuildOTPAuthURL(secret, username, "CustomERP"),
	})
}

// handleMFAActivate confirms a pending TOTP secret by verifying a code
// against it, activates MFA for the account, and - since this is also the
// completion of the original login attempt - issues the real session token.
func handleMFAActivate(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Resolved-Purpose") != "mfa_enroll" {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "This endpoint requires a pending MFA enrollment token from /api/v1/login")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")

	_, secret, err := engines.GetUserMFAStatus(tenantID, userID)
	if err != nil || secret == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "No pending MFA enrollment found - call /api/v1/auth/mfa/enroll first")
		return
	}
	if !engines.VerifyTOTPCode(tenantID, secret, req.Code) {
		writeAPIError(w, r, "USERAC-0025", "")
		return
	}
	if err := engines.ActivateMFA(tenantID, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to activate MFA")
		return
	}

	role, username, locationCode, err := engines.LookupUserRoleAndUsername(tenantID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "MFA activated but failed to issue session")
		return
	}

	// 32.5: recovery codes are minted here, at the one moment the user is
	// guaranteed to be looking at a setup screen. A failure to generate or
	// store them is NOT fatal to the login - MFA is already active and the
	// session is already earned, so refusing the token here would lock out an
	// account that just successfully enrolled. The user is told instead, and
	// can mint a set from their profile.
	recoveryCodes, recErr := engines.GenerateRecoveryCodes()
	if recErr == nil {
		recErr = engines.ReplaceRecoveryCodes(tenantID, userID, recoveryCodes)
	}
	if recErr != nil {
		recoveryCodes = nil
		engines.LogSystemError(tenantID, username, "Medium", "User Access & Security",
			fmt.Sprintf("failed to issue MFA recovery codes for %s: %v", username, recErr), "")
	}

	token := engines.SignToken(userID, username, role, tenantID, locationCode)
	engines.LogAuditEvent(tenantID, username, "LOGIN", "MFA_ENROLLED_AND_VERIFIED", "TOTP enrollment completed and verified")
	if len(recoveryCodes) > 0 {
		engines.LogAuditEvent(tenantID, username, "LOGIN", "MFA_RECOVERY_CODES_ISSUED",
			fmt.Sprintf("%d single-use recovery codes issued at enrollment", len(recoveryCodes)))
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":          token,
		"role":           engines.CanonicalRole(role),
		"user":           username,
		"recovery_codes": recoveryCodes,
	})
}

// handleForgotPassword (24.28) is deliberately identical-response
// regardless of whether usernameOrEmail matched a real account - matching
// handleLogin's own USERAC-0021 generic-error convention, so this endpoint
// can't be used to enumerate valid usernames/emails.
func handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UsernameOrEmail string `json:"username_or_email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	// resetLinkBase points at this app's own frontend reset screen; the
	// token is appended as a query param by RequestPasswordReset. No
	// separate PUBLIC_APP_URL setting exists in this codebase yet, so this
	// reuses the request's own Origin when present (same-origin frontend,
	// the only deployment shape this app has today) and falls back to the
	// relative path otherwise.
	resetLinkBase := "/reset-password.html"
	if origin := r.Header.Get("Origin"); origin != "" {
		resetLinkBase = origin + resetLinkBase
	}
	if err := engines.RequestPasswordReset(tenantID, strings.TrimSpace(req.UsernameOrEmail), resetLinkBase); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to process request")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "If an account matches, a reset link has been sent."})
}

// handleResetPassword (24.28) completes a reset using the token minted by
// handleForgotPassword. Unlike that endpoint, this one's error IS specific
// (invalid/expired token) - there's no enumeration risk in confirming a
// token itself is bad, only in confirming which usernames/emails exist.
func handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if err := engines.CompletePasswordReset(tenantID, strings.TrimSpace(req.Token), req.NewPassword); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleMFAVerify completes login for an already-enrolled MFA account by
// checking a TOTP code against the stored active secret.
func handleMFAVerify(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Resolved-Purpose") != "mfa_challenge" {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "This endpoint requires an MFA challenge token from /api/v1/login")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")

	enabled, secret, err := engines.GetUserMFAStatus(tenantID, userID)
	if err != nil || !enabled || secret == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "MFA is not enrolled for this account")
		return
	}

	// 32.5: a recovery code is accepted in place of a TOTP code, which is the
	// entire point of issuing them - the authenticator is assumed gone. TOTP
	// is tried first so the normal path costs nothing extra; only a 6-digit
	// code can ever match VerifyTOTPCode, so a recovery code never burns a
	// TOTP comparison and vice versa.
	usedRecoveryCode := false
	if !engines.VerifyTOTPCode(tenantID, secret, req.Code) {
		ok, recErr := engines.ConsumeRecoveryCode(tenantID, userID, req.Code)
		if recErr != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to verify recovery code")
			return
		}
		if !ok {
			writeAPIError(w, r, "USERAC-0025", "")
			return
		}
		usedRecoveryCode = true
	}

	role, username, locationCode, err := engines.LookupUserRoleAndUsername(tenantID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "MFA verified but failed to issue session")
		return
	}
	token := engines.SignToken(userID, username, role, tenantID, locationCode)

	remaining := 0
	if usedRecoveryCode {
		remaining, _ = engines.CountUnusedRecoveryCodes(tenantID, userID)
		engines.LogAuditEvent(tenantID, username, "LOGIN", "MFA_RECOVERY_CODE_USED",
			fmt.Sprintf("Signed in with a single-use recovery code; %d remaining", remaining))
	} else {
		engines.LogAuditEvent(tenantID, username, "LOGIN", "MFA_VERIFIED", "TOTP code verified, session issued")
	}

	// used_recovery_code drives the frontend's "your authenticator is
	// probably gone - set up a new device" prompt. Without it the user gets a
	// session but is still one lost code away from the lockout this stage
	// exists to prevent.
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":                    token,
		"role":                     engines.CanonicalRole(role),
		"user":                     username,
		"used_recovery_code":       usedRecoveryCode,
		"recovery_codes_remaining": remaining,
	})
}

// Generic CRUD handler wrapping security RBAC authorization and validation rules
