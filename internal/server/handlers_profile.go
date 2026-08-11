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

// allowedThemes (Stage 28.2) mirrors the frontend's theme selector. 'system'
// defers to the OS via the CSS prefers-color-scheme media query.
var allowedThemes = map[string]bool{"light": true, "dark": true, "system": true}

func handleGetProfile(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	var username, email, role, status, locationCode, themePref string
	var mfaEnabled bool
	var idleTimeout int
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT username, COALESCE(email, ''), role, status, mfa_enabled, idle_timeout_minutes, location_code, COALESCE(theme_preference, 'system')
		FROM %s.users WHERE id = $1`, schema), userID).
		Scan(&username, &email, &role, &status, &mfaEnabled, &idleTimeout, &locationCode, &themePref)
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
		"theme_preference":     themePref,
		"employee_id":          employeeID,
		"employee_name":        employeeName,
		// 24.1: read-only here, same as role - admin-managed via the Users
		// screen (handleSetUserLocation), not self-service editable.
		"location_code": locationCode,
	})
}

// handleMyPermissions (Stage 22.6) is the self-service read the sidebar
// needs to filter itself per-role: every authenticated user can call this
// for their own role (unlike /api/v1/admin/role-permissions, which lists
// every role's grants and stays HR/Admin-only). Deriving visibility from
// the same role_permissions table checkPermission() already enforces
// server-side (handlers_core_doc_engine.go:624) means an admin who creates
// a new role and grants it doctypes via the existing Roles screen sees the
// sidebar reflect that automatically - no code change needed per role.
// HR/Admin bypasses role_permissions entirely in checkPermission, so it's
// reported here as a flat is_admin flag rather than enumerating doctypes.
func handleMyPermissions(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	if engines.IsSuperAdmin(role) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"role":     role,
			"is_admin": true,
			"doctypes": []string{},
			"create":   []string{},
			"update":   []string{},
			"delete":   []string{},
		})
		return
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	// Stage 30.5.7: this used to return read grants only, so the frontend had
	// no way to know a role could see a record type but not create one - which
	// is why a Store Manager could fill in the whole New Item form and only
	// discover the refusal at Save. The other three verbs already exist on
	// role_permissions and are already enforced server-side; they were simply
	// never told to the client. Returned as separate lists rather than one
	// list of objects so the existing `doctypes` key keeps its exact shape and
	// any older client reading only that is unaffected.
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT doctype_name, allow_read, allow_create, allow_update, allow_delete
		   FROM %s.role_permissions WHERE role = $1`, schema), role)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	doctypes := []string{}
	createable := []string{}
	updatable := []string{}
	deletable := []string{}
	for rows.Next() {
		var d string
		var canRead, canCreate, canUpdate, canDelete bool
		if err := rows.Scan(&d, &canRead, &canCreate, &canUpdate, &canDelete); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if canRead {
			doctypes = append(doctypes, d)
		}
		if canCreate {
			createable = append(createable, d)
		}
		if canUpdate {
			updatable = append(updatable, d)
		}
		if canDelete {
			deletable = append(deletable, d)
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"role":     role,
		"is_admin": false,
		"doctypes": doctypes,
		"create":   createable,
		"update":   updatable,
		"delete":   deletable,
	})
}

// handleMyModules (Stage 27: Modular Product Packaging) is the self-service
// counterpart to GET /api/v1/admin/tenant/module-entitlements - that
// existing endpoint is HR/Admin-only, but every logged-in user's own
// frontend session needs to know which products/modules are enabled for
// their tenant so it can filter its own nav/URLs by entitlement (see
// public/app.js's applyModuleEntitlements()). Reuses
// engines.ListModuleEntitlements - no new engine code, same read the admin
// endpoint already uses, just exposed to any authenticated caller for their
// own tenant.
func handleMyModules(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	entitlements, err := engines.ListModuleEntitlements(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	enabled := []string{}
	var enabledOptional []string // excludes is_core - see ResolveSoleProductPackage
	for _, e := range entitlements {
		if e.Enabled {
			enabled = append(enabled, e.ModuleKey)
			if !e.IsCore {
				enabledOptional = append(enabledOptional, e.ModuleKey)
			}
		}
	}

	resp := map[string]interface{}{
		"enabled_modules": enabled,
	}
	// sole_package (Stage 27): a navigation hint only - if this tenant's
	// enabled optional modules resolve to exactly one sellable product, the
	// frontend uses this to redirect bare "/" straight to that product's own
	// URL. nil for a multi-product or full-suite tenant (today's full
	// sidebar experience, unchanged).
	if pkg := engines.ResolveSoleProductPackage(enabledOptional); pkg != nil {
		resp["sole_package"] = map[string]string{
			"package_key":  pkg.PackageKey,
			"display_name": pkg.DisplayName,
			"url_prefix":   pkg.URLPrefix,
		}
	}
	// owned_packages: every product this tenant licenses (not just the sole
	// one above) - drives the frontend's product switcher for a tenant with
	// 2+ products but not the full suite. Deliberately left empty for a
	// full-suite tenant (engines.IsFullSuite) - such a tenant technically
	// satisfies every individual package's requirements too, but should see
	// exactly today's one unified sidebar, not a "switch product" list.
	owned := []map[string]string{}
	if !engines.IsFullSuite(enabledOptional) {
		for _, pkg := range engines.ResolveOwnedPackages(enabledOptional) {
			owned = append(owned, map[string]string{
				"package_key":  pkg.PackageKey,
				"display_name": pkg.DisplayName,
				"url_prefix":   pkg.URLPrefix,
			})
		}
	}
	resp["owned_packages"] = owned
	_ = json.NewEncoder(w).Encode(resp)
}

func handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email              *string `json:"email"`
		IdleTimeoutMinutes *int    `json:"idle_timeout_minutes"`
		ThemePreference    *string `json:"theme_preference"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if req.IdleTimeoutMinutes != nil && !allowedIdleTimeouts[*req.IdleTimeoutMinutes] {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "idle_timeout_minutes must be one of 0 (never), 15, 30, 60, 120")
		return
	}
	if req.ThemePreference != nil && !allowedThemes[*req.ThemePreference] {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "theme_preference must be one of light, dark, system")
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
	if req.ThemePreference != nil {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET theme_preference = $1 WHERE id = $2`, schema), *req.ThemePreference, userID); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to update theme preference")
			return
		}
	}

	engines.LogAuditEvent(tenantID, username, "PROFILE", "UPDATED", "Self-service profile update (email, idle-timeout, and/or theme preference)")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if len(req.NewPassword) < 8 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "New password must be at least 8 characters")
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
