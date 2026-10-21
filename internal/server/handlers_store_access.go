package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"custom_erp/engines"
)

// Stage 85.A: store access per role and user, and the working store at the
// top of the screen. The engine side is engines/store_access.go.

// handleMyStores is the switcher's list: the stores this user may work at,
// the home store and the one they are working at now.
func handleMyStores(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	access, err := engines.UserStoreAccess(tenantID, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusServiceUnavailable, "Unable to read your stores - please retry shortly")
		return
	}
	stores, err := engines.StoresFor(tenantID, access)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to list stores")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"home":    access.Home,
		"working": r.Header.Get("Resolved-Location"),
		"all":     access.All,
		"stores":  stores,
	})
}

// handleSwitchWorkingStore re-issues the caller's session for another store
// they have been given. The new token is the only thing that changes; the
// server re-checks it on every request (LiveUserState.WorkingStore).
func handleSwitchWorkingStore(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Resolved-Purpose") != "" {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Only a signed-in session can switch store")
		return
	}
	var req struct {
		Store string `json:"store"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	username := r.Header.Get("Resolved-Username")
	store, err := engines.SwitchWorkingStore(tenantID, userID, username, r.Header.Get("Resolved-Location"), req.Store)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	live, err := engines.ResolveLiveUserState(tenantID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusServiceUnavailable, "Unable to verify session state - please retry shortly")
		return
	}
	token := engines.SignToken(userID, username, live.Role, tenantID, store, live.CredentialVersion)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "token": token, "store": store})
}

// handleUserStores reads (GET ?id=) or saves (POST) one user's store access.
func handleUserStores(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method == http.MethodGet {
		stores, err := engines.ListStores(tenantID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to list stores")
			return
		}
		// No id: just the store list (the Users screen's New page and table).
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"stores": stores})
			return
		}
		if forbidSupportUserTarget(w, r, id) {
			return
		}
		setting, err := engines.GetUserStoreAccess(tenantID, id)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusNotFound, "User not found")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"setting": setting, "stores": stores})
		return
	}
	var req struct {
		ID     string   `json:"id"`
		Access string   `json:"access"`
		Stores []string `json:"stores"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ID) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'id' and 'access' are required")
		return
	}
	if forbidSupportUserTarget(w, r, req.ID) {
		return
	}
	if err := engines.SetUserStoreAccess(tenantID, req.ID, req.Access, req.Stores, r.Header.Get("Resolved-Username")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleRoleStores reads (GET ?role=) or saves (POST) one role's store access.
func handleRoleStores(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method == http.MethodGet && strings.TrimSpace(r.URL.Query().Get("role")) == "" {
		// No role: every role's setting, for the Roles list.
		all, err := engines.ListRoleStoreAccess(tenantID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to read role stores")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"roles": all})
		return
	}
	if r.Method == http.MethodGet {
		setting, err := engines.GetRoleStoreAccess(tenantID, strings.TrimSpace(r.URL.Query().Get("role")))
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to read the role's stores")
			return
		}
		stores, err := engines.ListStores(tenantID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to list stores")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"setting": setting, "stores": stores})
		return
	}
	var req struct {
		Role   string   `json:"role"`
		Access string   `json:"access"`
		Stores []string `json:"stores"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if err := engines.SetRoleStoreAccess(tenantID, req.Role, req.Access, req.Stores, r.Header.Get("Resolved-Username")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// requireStoreAccess refuses a till action at a store the caller has not
// been given (STORE-0850). It sits beside ValidatePOSSellableLocation in each
// POS entry point: "may anyone sell here" and "may this person work here" are
// two different questions.
func requireStoreAccess(w http.ResponseWriter, r *http.Request, store string) bool {
	access, err := engines.UserStoreAccess(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusServiceUnavailable, "Unable to verify your stores - please retry shortly")
		return false
	}
	if err := engines.ValidateUserMayWorkAt(access, store); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return false
	}
	return true
}
