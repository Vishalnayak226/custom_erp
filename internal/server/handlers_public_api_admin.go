package server

import (
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type issueAPICredentialRequest struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at"`
}

func parseAPICredentialExpiry(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("expires_at must be an RFC3339 timestamp, for example 2027-08-11T00:00:00Z")
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

// API credential administration deliberately stays behind the existing
// human-session Super Admin gate. The credentials created here are for the
// future curated /api/public/v1 surface and are never accepted as user
// sessions or extension tokens.
func handleIssueAPICredential(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	var req issueAPICredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid API credential payload")
		return
	}
	expiresAt, err := parseAPICredentialExpiry(req.ExpiresAt)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actor := r.Header.Get("Resolved-Username")
	issued, err := engines.IssueAPICredential(tenantID, req.Name, req.Scopes, expiresAt, actor)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEvent(tenantID, actor, "API_CREDENTIAL_ISSUED", "SUCCESS",
		fmt.Sprintf("credential=%s prefix=%s scopes=%s", issued.Credential.ID, issued.Credential.KeyPrefix, strings.Join(issued.Credential.Scopes, ",")))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(issued)
}

func handleListAPICredentials(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	credentials, err := engines.ListAPICredentials(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"credentials":   credentials,
		"scope_catalog": engines.PublicAPIScopes(),
	})
}

func handleRotateAPICredential(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actor := r.Header.Get("Resolved-Username")
	issued, err := engines.RotateAPICredential(tenantID, r.PathValue("id"), actor)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEvent(tenantID, actor, "API_CREDENTIAL_ROTATED", "SUCCESS",
		fmt.Sprintf("old_credential=%s new_credential=%s new_prefix=%s", r.PathValue("id"), issued.Credential.ID, issued.Credential.KeyPrefix))
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(issued)
}

func handleRevokeAPICredential(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actor := r.Header.Get("Resolved-Username")
	credentialID := r.PathValue("id")
	if err := engines.RevokeAPICredential(tenantID, credentialID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEvent(tenantID, actor, "API_CREDENTIAL_REVOKED", "SUCCESS", "credential="+credentialID)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "revoked", "id": credentialID})
}
