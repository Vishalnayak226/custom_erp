package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"custom_erp/engines"
)

// 205.16, 205.17, 205.19, 205.21: the api-access routes for a key's docs,
// webhooks, attention list and fix. Every one is capability "api.share"; a
// holder reaches only their own keys, an Admin all of them.

func apiAccessAudit(r *http.Request, tenantID, event, detail string) {
	engines.LogAuditEventCtx(r.Context(), tenantID, r.Header.Get("Resolved-Username"), event, "SUCCESS", detail)
}

func apiAccessSpecOptions(r *http.Request, tenantID, role string, row *engines.APIKeyRow, share bool) (apiSpecOptions, error) {
	modules, err := engines.APIAccessModules(tenantID, role)
	if err != nil {
		return apiSpecOptions{}, err
	}
	return apiSpecOptions{TenantCode: tenantID, BaseURL: publicOrigin(r), Role: role, Row: row, Modules: modules, Share: share}, nil
}

// GET /api/v1/api-access/openapi.json - everything the asker may give a key.
func handleAPIAccessOpenAPI(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, _ := apiAccessActor(r)
	opt, err := apiAccessSpecOptions(r, tenantID, actor.Role, nil, false)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	apiSpecWrite(w, BuildAPISpec31(opt))
}

// GET /api/v1/api-access/keys/{id}/openapi.json
func handleAPIAccessKeyOpenAPI(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, actor, _ := apiAccessActor(r)
	opt, err := apiAccessSpecOptions(r, tenantID, actor.Role, row, false)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	apiSpecWrite(w, BuildAPISpec31(opt))
}

// GET /api/v1/api-access/keys/{id}/postman.json
func handleAPIAccessKeyPostman(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, actor, _ := apiAccessActor(r)
	opt, err := apiAccessSpecOptions(r, tenantID, actor.Role, row, false)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Disposition", attachmentDisposition("erp-api.postman_collection.json"))
	apiSpecWrite(w, apiSpecPostman(BuildAPISpec31(opt), "ERP API: "+row.Name, tenantID, opt.BaseURL))
}

// GET /api/v1/api-access/keys/{id}/share-docs -> {active, expires_at}
func handleAPIAccessShareDocsGet(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, _, _ := apiAccessActor(r)
	exp, err := engines.ShareDocsExpiry(tenantID, row.ID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	apiAccessJSON(w, http.StatusOK, map[string]interface{}{"active": exp != nil, "expires_at": exp})
}

// POST /api/v1/api-access/keys/{id}/share-docs {days} -> {url, expires_at}
func handleAPIAccessShareDocsCreate(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, _, _ := apiAccessActor(r)
	var req struct {
		Days int `json:"days"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	token, expires, err := engines.SetShareDocs(tenantID, row.ID, req.Days)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	apiAccessAudit(r, tenantID, "API_DOCS_SHARED", "credential="+row.ID)
	apiAccessJSON(w, http.StatusOK, map[string]interface{}{
		"url": "/api-docs.html?t=" + token, "expires_at": expires,
	})
}

// DELETE /api/v1/api-access/keys/{id}/share-docs
func handleAPIAccessShareDocsWithdraw(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, _, _ := apiAccessActor(r)
	if err := engines.WithdrawShareDocs(tenantID, row.ID); err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	apiAccessAudit(r, tenantID, "API_DOCS_WITHDRAWN", "credential="+row.ID)
	apiAccessJSON(w, http.StatusOK, map[string]interface{}{"active": false})
}

// handlePublicShareDocs serves GET /api/public/v1/docs/{token}: the key's spec
// without secrets. Every bad token gets the same 404 and nothing else.
func handlePublicShareDocs(w http.ResponseWriter, r *http.Request) {
	notFound := func() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Not found"}` + "\n"))
	}
	tenantID, credID, ok := engines.ResolveShareDocsToken(r.PathValue("token"))
	if !ok {
		notFound()
		return
	}
	access, err := engines.LoadAPIKeyAccess(tenantID, credID)
	if err != nil || access == nil {
		notFound()
		return
	}
	row, err := engines.GetAPIKey(tenantID, "", true, credID)
	if err != nil || row == nil {
		notFound()
		return
	}
	role := "Admin"
	if state, serr := engines.OwnerStateForKey(tenantID, access); serr == nil {
		role = state.Role
	}
	opt, err := apiAccessSpecOptions(r, tenantID, role, row, true)
	if err != nil {
		notFound()
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	apiSpecWrite(w, BuildAPISpec31(opt))
}

// GET /api/v1/api-access/keys/{id}/webhooks
func handleAPIAccessWebhooksList(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, _, _ := apiAccessActor(r)
	list, err := engines.ListKeyWebhooks(tenantID, row.ID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	apiAccessJSON(w, http.StatusOK, list)
}

// POST /api/v1/api-access/keys/{id}/webhooks {url, events[]} - secret shown once.
func handleAPIAccessWebhooksCreate(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, _, _ := apiAccessActor(r)
	var req struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorDetail(w, r, engines.APIKeyInvalidRequest, "", "The request body is not valid JSON.")
		return
	}
	hook, err := engines.CreateKeyWebhook(tenantID, row.ID, r.Header.Get("Resolved-User-ID"), req.URL, req.Events)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	apiAccessAudit(r, tenantID, "API_WEBHOOK_ADDED", "credential="+row.ID+" subscription="+hook.ID)
	apiAccessJSON(w, http.StatusCreated, hook)
}

// DELETE /api/v1/api-access/keys/{id}/webhooks/{sub}
func handleAPIAccessWebhooksDelete(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, _, _ := apiAccessActor(r)
	if err := engines.DeleteKeyWebhook(tenantID, row.ID, r.PathValue("sub")); err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	apiAccessAudit(r, tenantID, "API_WEBHOOK_REMOVED", "credential="+row.ID+" subscription="+r.PathValue("sub"))
	apiAccessJSON(w, http.StatusOK, map[string]interface{}{"deleted": true})
}

// GET /api/v1/api-access/keys/{id}/webhooks/deliveries?limit=
func handleAPIAccessWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, _, _ := apiAccessActor(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := engines.ListKeyWebhookDeliveries(tenantID, row.ID, limit)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	apiAccessJSON(w, http.StatusOK, list)
}

// POST /api/v1/api-access/keys/{id}/webhooks/deliveries/{d}/resend
func handleAPIAccessWebhookResend(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, _, _ := apiAccessActor(r)
	jobID, err := engines.ResendKeyWebhookDelivery(tenantID, row.ID, r.PathValue("d"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	apiAccessAudit(r, tenantID, "API_WEBHOOK_RESENT", "credential="+row.ID+" delivery="+r.PathValue("d"))
	apiAccessJSON(w, http.StatusOK, map[string]interface{}{"queued": true, "id": jobID})
}

// GET /api/v1/api-access/attention
func handleAPIAccessAttention(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, isAdmin := apiAccessActor(r)
	list, err := engines.APIKeyAttentionList(tenantID, actor, isAdmin)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []engines.APIAttention{}
	}
	apiAccessJSON(w, http.StatusOK, list)
}

// POST /api/v1/api-access/keys/{id}/fix {module, level}
func handleAPIAccessFix(w http.ResponseWriter, r *http.Request) {
	row, ok := apiAccessOwnKey(w, r)
	if !ok {
		return
	}
	tenantID, actor, _ := apiAccessActor(r)
	var req struct {
		Module string `json:"module"`
		Level  string `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorDetail(w, r, engines.APIKeyInvalidRequest, "", "The request body is not valid JSON.")
		return
	}
	fresh, err := engines.FixAPIKey(tenantID, actor, row.ID, req.Module, req.Level)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	apiAccessAudit(r, tenantID, "API_KEY_FIXED", "credential="+row.ID+" module="+req.Module+" level="+req.Level)
	apiAccessJSON(w, http.StatusOK, fresh)
}
