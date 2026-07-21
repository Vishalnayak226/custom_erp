package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"custom_erp/db"
	"custom_erp/engines"

	"golang.org/x/crypto/bcrypt"
)

// Self-service "My Profile" endpoints (Stage 21): view own account info
// (including a best-effort linked Employee lookup), change own password,
// and set a personal client-side idle-timeout preference. All three read
// the caller's identity from apiMiddleware's Resolved-* headers - there is
// no admin-on-behalf-of-another-user path here, this is self-service only.

// allowedIdleTimeouts mirrors the options offered on the frontend's Profile
// screen. 0 means "never" (rely solely on the server-side JWT session TTL
// in engines/auth.go's tokenTTL(), not a client-side inactivity timer).
var allowedIdleTimeouts = map[int]bool{0: true, 15: true, 30: true, 60: true, 120: true}

func handleGetProfile(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	var username, email, role, status, locationCode string
	var mfaEnabled bool
	var idleTimeout int
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT username, COALESCE(email, ''), role, status, mfa_enabled, idle_timeout_minutes, location_code
		FROM %s.users WHERE id = $1`, schema), userID).
		Scan(&username, &email, &role, &status, &mfaEnabled, &idleTimeout, &locationCode)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "User not found")
		return
	}

	// Best-effort linked Employee lookup (Employee.user_id -> this user's
	// id). This direction has no existing query anywhere else in the
	// codebase - engines.SyncEmployeeAccessLink only writes the other way
	// (Employee save -> users.status). No match just means "not linked",
	// not an error.
	var employeeID, employeeName string
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'name', '')
		FROM %s.documents
		WHERE doctype = 'Employee' AND data->>'user_id' = $1 AND deleted_at IS NULL
		LIMIT 1`, schema), userID).Scan(&employeeID, &employeeName)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":                   userID,
		"username":             username,
		"email":                email,
		"role":                 role,
		"status":               status,
		"mfa_enabled":          mfaEnabled,
		"idle_timeout_minutes": idleTimeout,
		"employee_id":          employeeID,
		"employee_name":        employeeName,
		// 24.1: read-only here, same as role - admin-managed via the Users
		// screen (handleSetUserLocation), not self-service editable.
		"location_code": locationCode,
	})
}

func handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email              *string `json:"email"`
		IdleTimeoutMinutes *int    `json:"idle_timeout_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if req.IdleTimeoutMinutes != nil && !allowedIdleTimeouts[*req.IdleTimeoutMinutes] {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "idle_timeout_minutes must be one of 0 (never), 15, 30, 60, 120")
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	username := r.Header.Get("Resolved-Username")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if req.Email != nil {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET email = $1 WHERE id = $2`, schema), *req.Email, userID); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to update email")
			return
		}
	}
	if req.IdleTimeoutMinutes != nil {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET idle_timeout_minutes = $1 WHERE id = $2`, schema), *req.IdleTimeoutMinutes, userID); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to update session preference")
			return
		}
	}

	engines.LogAuditEvent(tenantID, username, "PROFILE", "UPDATED", "Self-service profile update (email and/or idle-timeout preference)")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if len(req.NewPassword) < 8 {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "New password must be at least 8 characters")
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	username := r.Header.Get("Resolved-Username")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	var currentHash string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT password_hash FROM %s.users WHERE id = $1`, schema), userID).Scan(&currentHash); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "User not found")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(req.CurrentPassword)) != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "Current password is incorrect")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to set new password")
		return
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET password_hash = $1 WHERE id = $2`, schema), string(newHash), userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to set new password")
		return
	}

	engines.LogAuditEvent(tenantID, username, "PROFILE", "PASSWORD_CHANGED", "User changed their own password")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
