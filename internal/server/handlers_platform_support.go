package server

import (
	"custom_erp/engines"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Stage 125.2: the platform operator opens any client from the Control Panel
// with their own sign-in - no password or authenticator per client. See
// engines/platform_support.go for the support user and the hand-off code.

// POST /api/v1/platform/tenants/{tenant}/open -> {"url": "https://<client>/#operator_handoff=<code>"}.
// Platform operators only. The browser opens the URL in a new tab on the
// client's own hostname, where the page redeems the code below.
func handleControlPanelOpenTenant(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	target, ok := platformTargetTenant(w, r)
	if !ok {
		return
	}
	if target == operatorTenant(r) {
		writeAPIErrorGeneric(w, r, http.StatusConflict, "That is the client you are signed in to.")
		return
	}
	if operational, err := engines.TenantIsOperational(target); err != nil || !operational {
		writeAPIErrorGeneric(w, r, http.StatusConflict, "This client is paused or closed - resume it on the Control Panel first.")
		return
	}
	url, err := engines.TenantURL(target)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	if url == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "This client has no address yet - give it a hostname first.")
		return
	}
	code, err := engines.IssueSupportHandoff(target, controlPanelActor(r))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusConflict, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"url": url + "#operator_handoff=" + code})
}

// POST /api/v1/auth/operator-handoff {"code": "..."} -> the same body a login
// returns. Public, like /login: the single-use code is the credential, and the
// client is the one the request's hostname names.
func handleOperatorHandoff(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Code) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "code is required")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if operational, err := engines.TenantIsOperational(tenantID); err != nil || !operational {
		writeAPIError(w, r, "USERAC-0021", "")
		return
	}
	userID, username, role, locationCode, cv, err := engines.RedeemSupportHandoff(tenantID, req.Code)
	if errors.Is(err, engines.ErrSupportHandoffInvalid) {
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Could not open this client - try again from the Control Panel")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"token": engines.SignToken(userID, username, role, tenantID, locationCode, cv),
		"role":  engines.CanonicalRole(role),
		"user":  username,
	})
}
