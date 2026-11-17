package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"custom_erp/engines"
)

// 205.3-205.5: the API access screen's routes. Every route is capability
// "api.share" (route_capabilities.go): Admin always, a holder for their own keys
// and only the modules they hold themselves. The engine does the holding; these
// handlers only read the request, name the actor and write the answer.

func apiAccessActor(r *http.Request) (tenantID string, actor engines.APIKeyActor, isAdmin bool) {
	tenantID = r.Header.Get("Resolved-Tenant-ID")
	actor = engines.APIKeyActor{UserID: r.Header.Get("Resolved-User-ID"), Role: r.Header.Get("Resolved-Role")}
	return tenantID, actor, engines.HasFullAccess(actor.Role)
}

func apiAccessJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// apiAccessOwnKey answers 404 unless the key exists and the actor may manage it.
func apiAccessOwnKey(w http.ResponseWriter, r *http.Request) (*engines.APIKeyRow, bool) {
	tenantID, actor, isAdmin := apiAccessActor(r)
	row, err := engines.GetAPIKey(tenantID, actor.UserID, isAdmin, r.PathValue("id"))
	if err != nil || row == nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "API key not found")
		return nil, false
	}
	return row, true
}

// GET /api/v1/api-access/modules
func handleAPIAccessModules(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, _ := apiAccessActor(r)
	modules, err := engines.APIAccessModules(tenantID, actor.Role)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	apiAccessJSON(w, http.StatusOK, modules)
}

// GET /api/v1/api-access/keys?scope=mine|all&status=
func handleAPIAccessListKeys(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, isAdmin := apiAccessActor(r)
	all := r.URL.Query().Get("scope") == "all"
	if all && !isAdmin {
		writeAPIErrorDetail(w, r, engines.APIKeyActionNotAllowed, "", "Only an Admin can list every key.")
		return
	}
	rows, err := engines.ListAPIKeys(tenantID, actor.UserID, all, strings.TrimSpace(r.URL.Query().Get("status")))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	if rows == nil {
		rows = []engines.APIKeyRow{}
	}
	apiAccessJSON(w, http.StatusOK, rows)
}

type apiAccessCreateRequest struct {
	Name       string              `json:"name"`
	QuickStart string              `json:"quick_start"`
	Modules    map[string]string   `json:"modules"`
	Doctypes   map[string][]string `json:"doctypes"`
	CanApprove bool                `json:"can_approve"`
	AuthMode   string              `json:"auth_mode"`
	Expires    string              `json:"expires"`
	Stores     []string            `json:"stores"`
	IPAllow    []string            `json:"ip_allow"`
	Deliver    string              `json:"deliver"`
}

// POST /api/v1/api-access/keys
func handleAPIAccessCreateKey(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, _ := apiAccessActor(r)
	var req apiAccessCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorDetail(w, r, engines.APIKeyInvalidRequest, "", "The request body is not valid JSON.")
		return
	}
	out, err := engines.CreateAPIKey(tenantID, actor, engines.CreateAPIKeyInput{
		Name: req.Name, QuickStart: req.QuickStart,
		Grant:    engines.APIKeyGrant{Modules: req.Modules, Doctypes: req.Doctypes, CanApprove: req.CanApprove},
		AuthMode: req.AuthMode, Expires: req.Expires, Stores: req.Stores, IPAllow: req.IPAllow, Deliver: req.Deliver,
	})
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEventCtx(r.Context(), tenantID, r.Header.Get("Resolved-Username"), "API_KEY_CREATED", "SUCCESS",
		fmt.Sprintf("credential=%s modules=%s can_approve=%t deliver=%s", out.Credential.ID, grantSummary(req.Modules), req.CanApprove, req.Deliver))
	apiAccessJSON(w, http.StatusCreated, out.Present(publicOrigin(r)))
}

func grantSummary(modules map[string]string) string {
	parts := make([]string, 0, len(modules))
	for module, level := range modules {
		parts = append(parts, module+":"+level)
	}
	return strings.Join(parts, ",")
}

// GET /api/v1/api-access/keys/{id}
func handleAPIAccessGetKey(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	apiAccessJSON(w, http.StatusOK, row)
}

// PUT /api/v1/api-access/keys/{id}/modules
func handleAPIAccessUpdateModules(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, _, _ := apiAccessActor(r)
	var g engines.APIKeyGrant
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		writeAPIErrorDetail(w, r, engines.APIKeyInvalidRequest, "", "The request body is not valid JSON.")
		return
	}
	if err := engines.UpdateAPIKeyModules(tenantID, row.ID, g); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEventCtx(r.Context(), tenantID, r.Header.Get("Resolved-Username"), "API_KEY_MODULES_CHANGED", "SUCCESS",
		fmt.Sprintf("credential=%s from=%s to=%s can_approve=%t", row.ID, grantSummary(row.Modules), grantSummary(g.Modules), g.CanApprove))
	fresh, err := engines.GetAPIKey(tenantID, row.OwnerID, true, row.ID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	apiAccessJSON(w, http.StatusOK, fresh)
}

// POST /api/v1/api-access/keys/{id}/rotate
func handleAPIAccessRotateKey(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, _, _ := apiAccessActor(r)
	var req struct {
		Overlap string `json:"overlap"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	overlap, err := engines.ParseAPIOverlap(req.Overlap)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	out, err := engines.RotateAPIKey(tenantID, row.ID, r.Header.Get("Resolved-Username"), overlap)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEventCtx(r.Context(), tenantID, r.Header.Get("Resolved-Username"), "API_KEY_ROTATED", "SUCCESS",
		fmt.Sprintf("credential=%s new=%s overlap=%s", row.ID, out.Credential.ID, overlap))
	if row.AuthMode == "oauth_client" {
		out.Credential.AuthMode = "oauth_client"
	}
	apiAccessJSON(w, http.StatusOK, out.Present(publicOrigin(r)))
}

// DELETE /api/v1/api-access/keys/{id}
func handleAPIAccessRevokeKey(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, _, _ := apiAccessActor(r)
	if err := engines.RevokeAPICredential(tenantID, row.ID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEventCtx(r.Context(), tenantID, r.Header.Get("Resolved-Username"), "API_KEY_REVOKED", "SUCCESS", "credential="+row.ID)
	apiAccessJSON(w, http.StatusOK, map[string]string{"status": "revoked", "id": row.ID})
}

// POST /api/v1/api-access/keys/{id}/take-over (Admin only)
func handleAPIAccessTakeOver(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, isAdmin := apiAccessActor(r)
	if !isAdmin {
		writeAPIErrorDetail(w, r, engines.APIKeyActionNotAllowed, "", "Only an Admin can take over a key.")
		return
	}
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	if err := engines.TakeOverAPIKey(tenantID, row.ID, actor.UserID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEventCtx(r.Context(), tenantID, r.Header.Get("Resolved-Username"), "API_KEY_TAKEN_OVER", "SUCCESS",
		fmt.Sprintf("credential=%s from_owner=%s to_owner=%s", row.ID, row.OwnerID, actor.UserID))
	fresh, err := engines.GetAPIKey(tenantID, actor.UserID, true, row.ID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	apiAccessJSON(w, http.StatusOK, fresh)
}

type apiAccessActivityRow struct {
	At      time.Time `json:"at"`
	Method  string    `json:"method"`
	Path    string    `json:"path"`
	Status  int       `json:"status"`
	Outcome string    `json:"outcome"`
	Code    string    `json:"code,omitempty"`
	Fix     string    `json:"fix,omitempty"`
}

// GET /api/v1/api-access/keys/{id}/activity?errors_only=1&limit=
func handleAPIAccessActivity(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, _, _ := apiAccessActor(r)
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	errorsOnly := r.URL.Query().Get("errors_only") == "1" || r.URL.Query().Get("errors_only") == "true"
	// Ask for more than the page when filtering, so errors hidden among successes still fill it.
	fetch := limit
	if errorsOnly {
		fetch = limit * 5
		if fetch > 500 {
			fetch = 500
		}
	}
	entries, err := engines.ListPublicAPITraffic(tenantID, row.ID, fetch)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	out := []apiAccessActivityRow{}
	for _, e := range entries {
		if errorsOnly && e.StatusCode < 400 {
			continue
		}
		item := apiAccessActivityRow{At: e.CreatedAt, Method: e.Method, Path: e.Path, Status: e.StatusCode, Outcome: e.Outcome}
		if code, found := strings.CutPrefix(e.Outcome, "refused:"); found {
			item.Code = code
			if entry, known := errorCatalog[code]; known {
				item.Fix = entry.UserAction
			}
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	apiAccessJSON(w, http.StatusOK, out)
}

// publicOrigin is the address a developer reaches this server at, built the
// way publicCallbackURL does (https unless proxied or local).
func publicOrigin(r *http.Request) string {
	scheme := "https"
	if fp := r.Header.Get("X-Forwarded-Proto"); fp != "" {
		scheme = strings.TrimSpace(strings.Split(fp, ",")[0])
	} else if r.TLS == nil && (strings.HasPrefix(r.Host, "localhost") || strings.HasPrefix(r.Host, "127.")) {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

// PUT /api/v1/api-access/keys/{id}/limits - the same change the admin route
// makes, open to the key's own holder (capability api.share).
func handleAPIAccessSetLimits(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, _, _ := apiAccessActor(r)
	var req struct {
		RateLimitPerMinute *int `json:"rate_limit_per_minute"`
		DailyQuota         *int `json:"daily_quota"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorDetail(w, r, engines.APIKeyInvalidRequest, "", "The request body is not valid JSON.")
		return
	}
	if err := engines.SetPublicAPICredentialLimits(tenantID, row.ID, req.RateLimitPerMinute, req.DailyQuota); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEventCtx(r.Context(), tenantID, r.Header.Get("Resolved-Username"), "API_CREDENTIAL_LIMITS_SET", "SUCCESS", "credential="+row.ID)
	fresh, err := engines.GetAPIKey(tenantID, row.OwnerID, true, row.ID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	apiAccessJSON(w, http.StatusOK, fresh.Limits)
}
