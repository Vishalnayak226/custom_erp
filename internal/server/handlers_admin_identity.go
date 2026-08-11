package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"custom_erp/db"
	"custom_erp/engines"

	"golang.org/x/crypto/bcrypt"
)

// Admin-only user and role-permission management (Stage 21 QA fix): the
// "Users" and "Roles" sidebar items routed to view names the frontend
// router had no case for, always falling through to a "Module Setup
// Pending" placeholder despite ADMIN_GUIDE.md explicitly documenting a
// working Users screen ("New users are created as records in the system
// itself, via the Users screen"). None of this existed anywhere - no
// endpoint reads or writes tenant_default.users/role_permissions outside
// login/MFA/self-service-profile (a raw SQL table, not a generic doctype,
// so the existing /api/v1/doc/{doctype} engine can't reach it either).
// All handlers here are HR/Admin-only, matching every other admin screen's
// existing role check (e.g. handlers_integrations_admin.go).

// requireHRAdmin is the single shared admin gate every HR/Admin-only handler
// in this codebase calls through (17 call sites as of Stage 24 loophole
// review) - the one choke point to close loophole #1 (ERP_LOOPHOLES_ANALYSIS.md
// Critical #1, "Extension Token Scope Not Enforced in All Handlers") for all
// of them at once, rather than adding a Resolved-Purpose check to each
// handler individually. A SignExtensionToken carries no "role" claim, so
// role would already be "" here and fail the role check below - this
// explicit check is defense-in-depth against a future role_permissions-style
// misconfiguration ever granting "" a role, not a fix for a currently
// reachable bypass.
func requireHRAdmin(w http.ResponseWriter, r *http.Request, role string) bool {
	if r.Header.Get("Resolved-Purpose") == "extension" {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Extension tokens cannot access admin/configuration endpoints")
		return false
	}
	if !engines.IsSuperAdmin(role) {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Only HR/Admin can access this")
		return false
	}
	return true
}

// handleVerifyAuditLogChain (24.24) is the on-demand "checked periodically"
// half of the audit-log tamper-evidence checksum chain - an operator (or a
// future scheduled job) hits this to find out whether any row's checksum no
// longer matches its recomputed content, rather than only ever preventing
// tampering silently via the write-time chain in engines.LogAuditEvent.
func handleVerifyAuditLogChain(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	intact, brokenAt, err := engines.VerifyAuditLogChain(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"intact": intact, "broken_at": brokenAt})
}

func handleListUsers(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	// supplier_code (26.4.10) is COALESCEd because it is null for every
	// non-supplier account, which is almost all of them.
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, username, COALESCE(email, ''), role, status, location_code, COALESCE(supplier_code, '') FROM %s.users ORDER BY username`, schema))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	users := []map[string]interface{}{}
	for rows.Next() {
		var id, username, email, userRole, status, locationCode, supplierCode string
		if err := rows.Scan(&id, &username, &email, &userRole, &status, &locationCode, &supplierCode); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		users = append(users, map[string]interface{}{
			"id": id, "username": username, "email": email, "role": userRole, "status": status, "location_code": locationCode,
			"supplier_code": supplierCode,
		})
	}
	_ = json.NewEncoder(w).Encode(users)
}

func handleListRoles(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	rows, err := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT role FROM %s.users ORDER BY role`, schema))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	roles := []string{}
	for rows.Next() {
		var roleName string
		if err := rows.Scan(&roleName); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		roles = append(roles, roleName)
	}
	_ = json.NewEncoder(w).Encode(roles)
}

func handleCreateUser(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		Username     string `json:"username"`
		Password     string `json:"password"`
		Email        string `json:"email"`
		Role         string `json:"role"`
		LocationCode string `json:"location_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if req.Username == "" || req.Role == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "username and role are required")
		return
	}
	if len(req.Password) < 8 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Password must be at least 8 characters")
		return
	}
	// 24.1: defaults to "HO" (the column's own DEFAULT) when omitted,
	// matching every existing user's behavior before this field existed.
	if req.LocationCode == "" {
		req.LocationCode = "HO"
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actorUsername := r.Header.Get("Resolved-Username")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	// SAAS-0193 (Stage 25.8): checked before creating, not after - a tenant
	// with a configured max_users limit gets rejected instead of silently
	// exceeding it. No tenant_limits row for this tenant = no cap (open),
	// same as every other optional-config convention in this codebase.
	var activeUserCount int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.users WHERE status = 'Active'`, schema)).Scan(&activeUserCount); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := engines.CheckTenantLimit(tenantID, "max_users", activeUserCount+1); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	// idle_timeout_minutes seeds from the tenant-configured default (Stage 28,
	// security.default_idle_timeout_minutes); the user can change their own on
	// the Profile screen afterwards. Falls back to the column DEFAULT behavior
	// (30) via the setting's own registered default.
	_, err = db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.users (id, username, password_hash, email, role, status, location_code, idle_timeout_minutes) VALUES ($1, $1, $2, $3, $4, 'Active', $5, $6)`, schema),
		req.Username, string(hash), req.Email, req.Role, req.LocationCode, engines.GetSettingInt(tenantID, "security.default_idle_timeout_minutes"))
	if err != nil {
		msg := "Failed to create user."
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			msg = fmt.Sprintf("Username %q is already taken.", req.Username)
		}
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, msg)
		return
	}

	engines.LogAuditEvent(tenantID, actorUsername, "USER_MANAGEMENT", "USER_CREATED", fmt.Sprintf("Created user %s with role %s", req.Username, req.Role))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleSetUserStatus(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || (req.Status != "Active" && req.Status != "Inactive") {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'id' and 'status' (Active or Inactive) are required")
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actorUsername := r.Header.Get("Resolved-Username")
	actorUserID := r.Header.Get("Resolved-User-ID")
	if req.ID == actorUserID && req.Status == "Inactive" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "You cannot deactivate your own account")
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET status = $1 WHERE id = $2`, schema), req.Status, req.ID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to update user status")
		return
	}

	// Stage 29.8: deactivating a user must end their live session now, not at
	// the end of the auth-state cache window.
	engines.InvalidateLiveUserState(tenantID, req.ID)

	engines.LogAuditEvent(tenantID, actorUsername, "USER_MANAGEMENT", "USER_STATUS_CHANGED", fmt.Sprintf("Set user %s status to %s", req.ID, req.Status))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleSetUserLocation (24.1) is the update path for an existing user's
// location_code - e.g. a Store Manager transferred to a different store.
// Mirrors handleSetUserStatus's shape exactly.
func handleSetUserLocation(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		ID           string `json:"id"`
		LocationCode string `json:"location_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.LocationCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'id' and 'location_code' are required")
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actorUsername := r.Header.Get("Resolved-Username")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET location_code = $1 WHERE id = $2`, schema), req.LocationCode, req.ID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to update user location")
		return
	}

	// Stage 29.8: location is now resolved live per request (it drives
	// location-scoped object filtering), so a transfer must invalidate the
	// cached state too or the user keeps seeing their old store's data.
	engines.InvalidateLiveUserState(tenantID, req.ID)

	engines.LogAuditEvent(tenantID, actorUsername, "USER_MANAGEMENT", "USER_LOCATION_CHANGED", fmt.Sprintf("Set user %s location to %s", req.ID, req.LocationCode))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleRolePermissions(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := db.DB.Query(fmt.Sprintf(
			`SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete FROM %s.role_permissions ORDER BY role, doctype_name`, schema))
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		defer rows.Close()

		grants := []map[string]interface{}{}
		for rows.Next() {
			var roleName, doctypeName string
			var allowRead, allowCreate, allowUpdate, allowDelete bool
			if err := rows.Scan(&roleName, &doctypeName, &allowRead, &allowCreate, &allowUpdate, &allowDelete); err != nil {
				writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
				return
			}
			grants = append(grants, map[string]interface{}{
				"role": roleName, "doctype_name": doctypeName,
				"allow_read": allowRead, "allow_create": allowCreate, "allow_update": allowUpdate, "allow_delete": allowDelete,
			})
		}
		_ = json.NewEncoder(w).Encode(grants)

	case http.MethodPost:
		var req struct {
			Role        string `json:"role"`
			DoctypeName string `json:"doctype_name"`
			AllowRead   bool   `json:"allow_read"`
			AllowCreate bool   `json:"allow_create"`
			AllowUpdate bool   `json:"allow_update"`
			AllowDelete bool   `json:"allow_delete"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Role == "" || req.DoctypeName == "" {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'role' and 'doctype_name' are required")
			return
		}
		_, err := db.DB.Exec(fmt.Sprintf(`
			INSERT INTO %s.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (role, doctype_name) DO UPDATE SET
				allow_read = EXCLUDED.allow_read, allow_create = EXCLUDED.allow_create,
				allow_update = EXCLUDED.allow_update, allow_delete = EXCLUDED.allow_delete`, schema),
			req.Role, req.DoctypeName, req.AllowRead, req.AllowCreate, req.AllowUpdate, req.AllowDelete)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		actorUsername := r.Header.Get("Resolved-Username")
		engines.LogAuditEvent(tenantID, actorUsername, "ROLE_PERMISSIONS", "GRANT_UPDATED",
			fmt.Sprintf("Set %s permissions on %s: read=%v create=%v update=%v delete=%v", req.Role, req.DoctypeName, req.AllowRead, req.AllowCreate, req.AllowUpdate, req.AllowDelete))
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})

	default:
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
	}
}
