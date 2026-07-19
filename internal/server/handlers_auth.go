package server

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
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
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var u struct {
		ID               string
		Username         string
		PasswordHash     string
		Role             string
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
		SELECT id, username, password_hash, role, failed_login_count, (locked_until IS NOT NULL AND locked_until > NOW())
		FROM %s.users
		WHERE username = $1 AND status = 'Active'`, schema), req.Username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.FailedLoginCount, &u.IsLocked)
	if err != nil {
		// Generic security error message
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid username or password"})
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
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid username or password"})
		return
	}

	// Check password with bcrypt (supports fallback check for local seed configs)
	err = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password))
	if err != nil && u.PasswordHash != req.Password {
		newCount := u.FailedLoginCount + 1
		if newCount >= accountLockoutThreshold {
			// NOW() + make_interval(...) is also computed in Postgres for the
			// same reason as the is_locked check above - the lockout window's
			// end time must be reckoned against the same clock it's later
			// compared to.
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET failed_login_count = $1, locked_until = NOW() + make_interval(mins => $2) WHERE id = $3`, schema),
				newCount, int(accountLockoutDuration.Minutes()), u.ID)
		} else {
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET failed_login_count = $1 WHERE id = $2`, schema), newCount, u.ID)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid username or password"})
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
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to resolve MFA status"})
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

	// Hardcoded default location for simplicity, can be mapped in DB users table later
	locationCode := "HO"
	token := engines.SignToken(u.ID, u.Username, u.Role, tenantID, locationCode)

	engines.LogAuditEvent(tenantID, u.Username, "LOGIN", "SUCCESS", fmt.Sprintf("User logged in successfully with role %s", u.Role))

	_ = json.NewEncoder(w).Encode(map[string]string{
		"token": token,
		"role":  u.Role,
		"user":  u.Username,
	})
}

// handleMFAEnroll issues a fresh (pending, not-yet-active) TOTP secret for
// the account named in a mfa_enroll purpose token. Safe to call more than
// once before activation - each call simply replaces the pending secret.
func handleMFAEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Resolved-Purpose") != "mfa_enroll" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "This endpoint requires a pending MFA enrollment token from /api/v1/login"})
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	username := r.Header.Get("Resolved-Username")

	secret, err := engines.GenerateTOTPSecret()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to generate MFA secret"})
		return
	}
	if err := engines.SetPendingMFASecret(tenantID, userID, secret); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to store MFA secret"})
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
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "This endpoint requires a pending MFA enrollment token from /api/v1/login"})
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")

	_, secret, err := engines.GetUserMFAStatus(tenantID, userID)
	if err != nil || secret == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "No pending MFA enrollment found - call /api/v1/auth/mfa/enroll first"})
		return
	}
	if !engines.VerifyTOTPCode(secret, req.Code) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid MFA code"})
		return
	}
	if err := engines.ActivateMFA(tenantID, userID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to activate MFA"})
		return
	}

	role, username, err := engines.LookupUserRoleAndUsername(tenantID, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "MFA activated but failed to issue session"})
		return
	}
	token := engines.SignToken(userID, username, role, tenantID, "HO")
	engines.LogAuditEvent(tenantID, username, "LOGIN", "MFA_ENROLLED_AND_VERIFIED", "TOTP enrollment completed and verified")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": token, "role": role, "user": username})
}

// handleMFAVerify completes login for an already-enrolled MFA account by
// checking a TOTP code against the stored active secret.
func handleMFAVerify(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Resolved-Purpose") != "mfa_challenge" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "This endpoint requires an MFA challenge token from /api/v1/login"})
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")

	enabled, secret, err := engines.GetUserMFAStatus(tenantID, userID)
	if err != nil || !enabled || secret == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "MFA is not enrolled for this account"})
		return
	}
	if !engines.VerifyTOTPCode(secret, req.Code) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid MFA code"})
		return
	}

	role, username, err := engines.LookupUserRoleAndUsername(tenantID, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "MFA verified but failed to issue session"})
		return
	}
	token := engines.SignToken(userID, username, role, tenantID, "HO")
	engines.LogAuditEvent(tenantID, username, "LOGIN", "MFA_VERIFIED", "TOTP code verified, session issued")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": token, "role": role, "user": username})
}

// Generic CRUD handler wrapping security RBAC authorization and validation rules
