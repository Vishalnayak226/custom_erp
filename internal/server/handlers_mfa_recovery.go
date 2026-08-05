package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"custom_erp/db"
	"custom_erp/engines"

	"golang.org/x/crypto/bcrypt"
)

// Self-service MFA recovery and device migration (Stage 32.5).
//
// These all sit behind a normal full session token rather than the
// `mfa_enroll`/`mfa_challenge` purpose tokens the login-time handlers in
// handlers_auth.go use, because they are things a signed-in user does about
// their own account - not steps in an unfinished login.
//
// Re-authentication uses the account password (the same bcrypt check
// handleChangePassword performs) rather than a TOTP code. That is deliberate:
// the user most likely to need these screens is one whose authenticator is
// already gone, so gating them on a TOTP code would make them useless in
// exactly the situation they exist for.

// verifyOwnPassword re-checks the signed-in user's password. Returns false
// and writes the error response if it does not match.
func verifyOwnPassword(w http.ResponseWriter, r *http.Request, tenantID, userID, password string) bool {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return false
	}
	var hash string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT password_hash FROM %s.users WHERE id = $1`, schema), userID).Scan(&hash); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "User not found")
		return false
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "Password is incorrect")
		return false
	}
	return true
}

// handleMFARecoveryStatus reports how many unused recovery codes the
// signed-in user holds, so the profile screen can warn before the last one is
// spent rather than after.
func handleMFARecoveryStatus(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")

	enabled, _, err := engines.GetUserMFAStatus(tenantID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to resolve MFA status")
		return
	}
	remaining, err := engines.CountUnusedRecoveryCodes(tenantID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to count recovery codes")
		return
	}
	pending, _ := engines.GetPendingReenrollSecret(tenantID, userID)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"mfa_enabled":          enabled,
		"remaining":            remaining,
		"issued_per_set":       engines.RecoveryCodeCount,
		"reenroll_in_progress": pending != "",
	})
}

// handleMFARegenerateRecoveryCodes mints a fresh set, invalidating every code
// the user previously held - which is the point: a set that may have been
// photographed, emailed to oneself, or left in a drawer stops working the
// moment a new set is printed.
func handleMFARegenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	username := r.Header.Get("Resolved-Username")

	if !verifyOwnPassword(w, r, tenantID, userID, req.Password) {
		return
	}
	enabled, _, err := engines.GetUserMFAStatus(tenantID, userID)
	if err != nil || !enabled {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "MFA is not enrolled for this account")
		return
	}

	codes, err := engines.GenerateRecoveryCodes()
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to generate recovery codes")
		return
	}
	if err := engines.ReplaceRecoveryCodes(tenantID, userID, codes); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to store recovery codes")
		return
	}

	engines.LogAuditEvent(tenantID, username, "PROFILE", "MFA_RECOVERY_CODES_REGENERATED",
		fmt.Sprintf("%d new single-use recovery codes issued; all previous codes invalidated", len(codes)))
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"recovery_codes": codes})
}

// handleMFAReenrollStart begins moving the account's authenticator to a new
// device. The new secret is parked in users.mfa_pending_secret and the
// existing one keeps working until handleMFAReenrollConfirm promotes it, so
// an abandoned attempt cannot lock anyone out.
func handleMFAReenrollStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	username := r.Header.Get("Resolved-Username")

	if !verifyOwnPassword(w, r, tenantID, userID, req.Password) {
		return
	}

	secret, err := engines.GenerateTOTPSecret()
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to generate MFA secret")
		return
	}
	if err := engines.SetPendingReenrollSecret(tenantID, userID, secret); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to store MFA secret")
		return
	}

	engines.LogAuditEvent(tenantID, username, "PROFILE", "MFA_REENROLL_STARTED", "New authenticator secret issued, awaiting confirmation")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"secret":      secret,
		"otpauth_url": engines.BuildOTPAuthURL(secret, username, "CustomERP"),
	})
}

// handleMFAReenrollConfirm proves the new device works, promotes its secret
// to the active one, and issues a fresh set of recovery codes - the old set
// belonged to the old device's enrollment and is retired with it.
func handleMFAReenrollConfirm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	username := r.Header.Get("Resolved-Username")

	pending, err := engines.GetPendingReenrollSecret(tenantID, userID)
	if err != nil || pending == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "No device change is in progress - start one first")
		return
	}
	if !engines.VerifyTOTPCode(tenantID, pending, req.Code) {
		writeAPIError(w, r, "USERAC-0025", "")
		return
	}
	if err := engines.ConfirmReenrollSecret(tenantID, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to activate the new authenticator")
		return
	}

	// Same non-fatal posture as handleMFAActivate: the device switch itself
	// has already succeeded and must not be reported as a failure just
	// because the new code set could not be written.
	codes, recErr := engines.GenerateRecoveryCodes()
	if recErr == nil {
		recErr = engines.ReplaceRecoveryCodes(tenantID, userID, codes)
	}
	if recErr != nil {
		codes = nil
		engines.LogSystemError(tenantID, username, "Medium", "User Access & Security",
			fmt.Sprintf("authenticator moved but recovery codes could not be reissued for %s: %v", username, recErr), "")
	}

	engines.LogAuditEvent(tenantID, username, "PROFILE", "MFA_REENROLL_COMPLETED", "Authenticator moved to a new device")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "success",
		"recovery_codes": codes,
	})
}

// handleMFAReenrollCancel drops a half-finished device switch so the profile
// screen does not keep showing a stale "setup in progress" state.
func handleMFAReenrollCancel(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	username := r.Header.Get("Resolved-Username")

	if err := engines.CancelReenroll(tenantID, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to cancel the device change")
		return
	}
	engines.LogAuditEvent(tenantID, username, "PROFILE", "MFA_REENROLL_CANCELLED", "Pending authenticator change discarded")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleSetUserSupplier links a Supplier login to the Vendor it speaks for
// (Stage 26.4.10). Without this there is no way to finish creating a supplier
// account from inside the app at all - the column would have to be set by
// hand in SQL, which is exactly the kind of gap that makes a feature "built
// but not usable".
//
// The vendor is verified to exist before it is stored. A typo here would
// otherwise produce an account that logs in successfully and then finds
// nothing, with no indication of why.
func handleSetUserSupplier(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	var req struct {
		ID           string `json:"id"`
		SupplierCode string `json:"supplier_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'id' and 'supplier_code' are required")
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actor := r.Header.Get("Resolved-Username")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	// A blank code unlinks the account, which is how a supplier login is
	// retired without deleting it.
	if req.SupplierCode != "" {
		var exists int
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT 1 FROM %s.documents WHERE id = $1 AND doctype = 'Vendor' AND deleted_at IS NULL`, schema),
			req.SupplierCode).Scan(&exists); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "No Vendor with that code exists")
			return
		}
	}

	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET supplier_code = NULLIF($1, '') WHERE id = $2`, schema),
		req.SupplierCode, req.ID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to update the user's supplier link")
		return
	}

	detail := "Supplier link cleared"
	if req.SupplierCode != "" {
		detail = "Linked to vendor " + req.SupplierCode
	}
	engines.LogAuditEvent(tenantID, actor, "USER_MANAGEMENT", "USER_SUPPLIER_LINK_SET", detail+" for user "+req.ID)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleAdminResetUserMFA lets one HR/Admin clear another user's MFA
// enrollment, so a colleague with a lost phone and no recovery codes can be
// recovered from inside the app. Before this the only route was cmd/reset_mfa
// over SSH against the database - which on the production droplet means the
// deploy key is the sole path back in.
//
// This does not weaken the MFA requirement: the account is put back into the
// enrollment state, so its very next login is forced through
// /auth/mfa/enroll + /activate on a new device.
func handleAdminResetUserMFA(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'id' is required")
		return
	}
	targetID := req.ID
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actor := r.Header.Get("Resolved-Username")

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	var targetUsername string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT username FROM %s.users WHERE id = $1`, schema), targetID).Scan(&targetUsername); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "User not found")
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to reset MFA")
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.users SET mfa_enabled = FALSE, mfa_secret = NULL, mfa_pending_secret = NULL WHERE id = $1`, schema), targetID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to reset MFA")
		return
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`DELETE FROM %s.mfa_recovery_codes WHERE user_id = $1`, schema), targetID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to reset MFA")
		return
	}
	if err := tx.Commit(); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to reset MFA")
		return
	}

	engines.LogAuditEvent(tenantID, actor, "USER_MANAGEMENT", "MFA_RESET_BY_ADMIN",
		fmt.Sprintf("MFA enrollment and recovery codes cleared for %s; next login will re-enroll", targetUsername))
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
		"detail": targetUsername + " will be asked to set up an authenticator at their next login.",
	})
}
