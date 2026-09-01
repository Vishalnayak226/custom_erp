package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 38.7 - self-service sandbox tenant provisioning for integrators.
// Super-Admin gated, the same as handleProvisionTenant (which
// ProvisionSandboxTenant wraps) - provisioning any tenant, sandbox or real,
// is an admin action; "self-service" describes the integrator's experience
// once the sandbox exists (their own login, their own API credentials),
// not an unauthenticated public signup flow.

func handleProvisionSandboxTenant(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if !engines.IsSuperAdmin(role) {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Only a Super Admin can provision a sandbox tenant")
		return
	}
	var req struct {
		TTLDays int `json:"ttl_days"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // an empty/absent body is fine - falls back to the default TTL

	tenantID, schemaName, adminPassword, err := engines.ProvisionSandboxTenant(currentAppVersion(), req.TTLDays)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"tenant_id": tenantID, "schema_name": schemaName, "admin_username": "admin", "admin_password": adminPassword,
	})
}

func handleListSandboxTenants(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if !engines.IsSuperAdmin(role) {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Only a Super Admin can list sandbox tenants")
		return
	}
	tenants, err := engines.ListSandboxTenants()
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"tenants": tenants})
}

func handleResetSandboxTenant(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if !engines.IsSuperAdmin(role) {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Only a Super Admin can reset a sandbox tenant")
		return
	}
	var req struct {
		TTLDays int `json:"ttl_days"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := engines.ResetSandboxTenant(r.PathValue("id"), req.TTLDays); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "reset"})
}

func handleDeleteSandboxTenant(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodDelete {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if !engines.IsSuperAdmin(role) {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Only a Super Admin can delete a sandbox tenant")
		return
	}
	if err := engines.DeleteSandboxTenant(r.PathValue("id")); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}
