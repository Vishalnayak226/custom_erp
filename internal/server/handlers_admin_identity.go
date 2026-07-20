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

func requireHRAdmin(w http.ResponseWriter, role string) bool {
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can access this"})
		return false
	}
	return true
}

func handleListUsers(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, role) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, username, COALESCE(email, ''), role, status FROM %s.users ORDER BY username`, schema))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	users := []map[string]interface{}{}
	for rows.Next() {
		var id, username, email, userRole, status string
		if err := rows.Scan(&id, &username, &email, &userRole, &status); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		users = append(users, map[string]interface{}{
			"id": id, "username": username, "email": email, "role": userRole, "status": status,
		})
	}
	_ = json.NewEncoder(w).Encode(users)
}

func handleListRoles(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, role) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	rows, err := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT role FROM %s.users ORDER BY role`, schema))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	roles := []string{}
	for rows.Next() {
		var roleName string
		if err := rows.Scan(&roleName); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		roles = append(roles, roleName)
	}
	_ = json.NewEncoder(w).Encode(roles)
}

func handleCreateUser(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, role) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}
	if req.Username == "" || req.Role == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "username and role are required"})
		return
	}
	if len(req.Password) < 8 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Password must be at least 8 characters"})
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actorUsername := r.Header.Get("Resolved-Username")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to hash password"})
		return
	}

	_, err = db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.users (id, username, password_hash, email, role, status) VALUES ($1, $1, $2, $3, $4, 'Active')`, schema),
		req.Username, string(hash), req.Email, req.Role)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		msg := "Failed to create user."
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			msg = fmt.Sprintf("Username %q is already taken.", req.Username)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
		return
	}

	engines.LogAuditEvent(tenantID, actorUsername, "USER_MANAGEMENT", "USER_CREATED", fmt.Sprintf("Created user %s with role %s", req.Username, req.Role))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleSetUserStatus(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, role) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || (req.Status != "Active" && req.Status != "Inactive") {
		http.Error(w, "Fields 'id' and 'status' (Active or Inactive) are required", http.StatusBadRequest)
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actorUsername := r.Header.Get("Resolved-Username")
	actorUserID := r.Header.Get("Resolved-User-ID")
	if req.ID == actorUserID && req.Status == "Inactive" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "You cannot deactivate your own account"})
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET status = $1 WHERE id = $2`, schema), req.Status, req.ID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to update user status"})
		return
	}

	engines.LogAuditEvent(tenantID, actorUsername, "USER_MANAGEMENT", "USER_STATUS_CHANGED", fmt.Sprintf("Set user %s status to %s", req.ID, req.Status))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleRolePermissions(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, role) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := db.DB.Query(fmt.Sprintf(
			`SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete FROM %s.role_permissions ORDER BY role, doctype_name`, schema))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()

		grants := []map[string]interface{}{}
		for rows.Next() {
			var roleName, doctypeName string
			var allowRead, allowCreate, allowUpdate, allowDelete bool
			if err := rows.Scan(&roleName, &doctypeName, &allowRead, &allowCreate, &allowUpdate, &allowDelete); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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
			http.Error(w, "Fields 'role' and 'doctype_name' are required", http.StatusBadRequest)
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
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		actorUsername := r.Header.Get("Resolved-Username")
		engines.LogAuditEvent(tenantID, actorUsername, "ROLE_PERMISSIONS", "GRANT_UPDATED",
			fmt.Sprintf("Set %s permissions on %s: read=%v create=%v update=%v delete=%v", req.Role, req.DoctypeName, req.AllowRead, req.AllowCreate, req.AllowUpdate, req.AllowDelete))
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
