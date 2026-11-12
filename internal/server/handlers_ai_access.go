package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"custom_erp/engines"
)

// Stage 148.37 (D9): read-only access for AI assistants. The routes live under
// /api/public/v1/ai/ so they pass the public API's whole chain (credential,
// scope, rate and daily budget, idempotency on the one write, traffic log),
// and sit behind the ai_access module. Each handler then acts as the one user
// the client is tied to. See engines/ai_access_stage148_37.go.

// aiRoute is a public route that acts as the client's user. The module gate
// runs first so a switched-off tenant learns nothing; the user is read fresh
// on every call.
func aiRoute(scope string, h func(w http.ResponseWriter, r *http.Request, a *engines.AIActor)) http.HandlerFunc {
	return publicAPIMiddleware(scope, moduleGate(engines.AIAccessModule, func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get("Resolved-Tenant-ID")
		client := r.Header.Get("Resolved-API-Credential")
		call := r.Method + " " + r.URL.Path
		actor, err := engines.ResolveAIActor(tenantID, client)
		switch {
		case errors.Is(err, engines.ErrAIClientUnbound):
			engines.AuditAICall(tenantID, nil, client, "", call, "refused: client not tied to a user", "")
			writeAPIError(w, r, "SEC-0302", "")
			return
		case errors.Is(err, engines.ErrAIUserInactive):
			engines.AuditAICall(tenantID, actor, client, "", call, "refused: user not active", "")
			writeAPIError(w, r, "SEC-0300", "")
			return
		case errors.Is(err, engines.ErrAIUserFullAccess):
			engines.AuditAICall(tenantID, actor, client, "", call, "refused: user has full access", "")
			writeAPIError(w, r, "SEC-0309", "")
			return
		case err != nil:
			writeAPIErrorGeneric(w, r, http.StatusServiceUnavailable, "Unable to check the user this assistant acts as - please retry shortly.")
			return
		}
		h(w, r, actor)
	}))
}

func aiJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func handleAIStock(w http.ResponseWriter, r *http.Request, a *engines.AIActor) {
	sku := strings.TrimSpace(r.URL.Query().Get("sku"))
	location := strings.TrimSpace(r.URL.Query().Get("location"))
	call := fmt.Sprintf("GET stock sku=%s location=%s", sku, location)
	levels, err := engines.AIStock(a, sku, location)
	switch {
	case errors.Is(err, engines.ErrAIStoreNotAllowed):
		engines.AuditAICall(a.TenantID, a, "", "", call, "refused: store not the user's", "")
		writeAPIError(w, r, "SEC-0301", "")
		return
	case err != nil:
		engines.AuditAICall(a.TenantID, a, "", "", call, "refused: bad request", "")
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.AuditAICall(a.TenantID, a, "", "", call, "allowed", fmt.Sprintf("rows=%d", len(levels)))
	aiJSON(w, http.StatusOK, map[string]interface{}{"stock": levels})
}

func handleAIOrderStatus(w http.ResponseWriter, r *http.Request, a *engines.AIActor) {
	call := "GET order status " + r.PathValue("id")
	status, err := engines.AIOrderStatus(a, r.PathValue("id"))
	if errors.Is(err, engines.ErrAIOrderNotFound) {
		engines.AuditAICall(a.TenantID, a, "", "", call, "refused: not found or not the user's store", "")
		writeAPIError(w, r, "SEC-0305", "")
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.AuditAICall(a.TenantID, a, "", "", call, "allowed", "")
	aiJSON(w, http.StatusOK, status)
}

func handleAIReportList(w http.ResponseWriter, r *http.Request, a *engines.AIActor) {
	list := engines.AIReportList(a)
	engines.AuditAICall(a.TenantID, a, "", "", "GET reports", "allowed", fmt.Sprintf("rows=%d", len(list)))
	aiJSON(w, http.StatusOK, map[string]interface{}{"reports": list})
}

func handleAIReportRun(w http.ResponseWriter, r *http.Request, a *engines.AIActor) {
	id := r.PathValue("id")
	call := "GET report " + id
	params := map[string]string{}
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			params[k] = v[0]
		}
	}
	result, err := engines.AIReportRun(r.Context(), a, id, params)
	switch {
	case errors.Is(err, engines.ErrAIReportNotFound):
		engines.AuditAICall(a.TenantID, a, "", "", call, "refused: no such report", "")
		writeAPIError(w, r, "SEC-0307", "")
		return
	case errors.Is(err, engines.ErrAIReportRefused):
		engines.AuditAICall(a.TenantID, a, "", "", call, "refused: role", "")
		writeAPIError(w, r, "SEC-0308", "")
		return
	case err != nil:
		engines.AuditAICall(a.TenantID, a, "", "", call, "refused: report could not run", "")
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.AuditAICall(a.TenantID, a, "", "", call, "allowed", "")
	aiJSON(w, http.StatusOK, result)
}

func handleAIDraft(w http.ResponseWriter, r *http.Request, a *engines.AIActor) {
	call := "POST sales order draft"
	var in engines.AIDraftInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		engines.AuditAICall(a.TenantID, a, "", "", call, "refused: unreadable", "")
		writeAPIErrorDetail(w, r, "SEC-0303", "", "the body is not readable")
		return
	}
	id, err := engines.CreateAssistantDraft(a, in)
	var bad *engines.AIDraftError
	if errors.As(err, &bad) {
		engines.AuditAICall(a.TenantID, a, "", "", call, "refused: draft not valid", "")
		writeAPIErrorDetail(w, r, "SEC-0303", "", bad.Message)
		return
	}
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "The draft could not be saved.")
		return
	}
	engines.AuditAICall(a.TenantID, a, "", "", call, "allowed", "draft "+id)
	aiJSON(w, http.StatusCreated, map[string]interface{}{"order_id": id, "order_status": "Draft", "made_by_assistant": true})
}

// registerAIAccessRoutes wires the assistant's routes, which ride the public
// API's middleware. The people's routes are in routes.go, behind moduleGate.
func registerAIAccessRoutes() {
	http.HandleFunc("GET "+"/api/public/v1/ai/stock", aiRoute(engines.AIScopeRead, handleAIStock))
	http.HandleFunc("GET "+"/api/public/v1/ai/orders/{id}/status", aiRoute(engines.AIScopeRead, handleAIOrderStatus))
	http.HandleFunc("GET "+"/api/public/v1/ai/reports", aiRoute(engines.AIScopeRead, handleAIReportList))
	http.HandleFunc("GET "+"/api/public/v1/ai/reports/{id}", aiRoute(engines.AIScopeRead, handleAIReportRun))
	http.HandleFunc("POST "+"/api/public/v1/ai/sales-order-drafts", aiRoute(engines.AIScopeDraft, handleAIDraft))
}

func handleListAIAccessClients(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	clients, err := engines.ListAIAccessClients(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "The list could not be read.")
		return
	}
	aiJSON(w, http.StatusOK, map[string]interface{}{"clients": clients})
}

func handleIssueAIAccessClient(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	var req struct {
		Name       string   `json:"name"`
		ActingUser string   `json:"acting_user"`
		Scopes     []string `json:"scopes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorDetail(w, r, "SEC-0304", "", "the body is not readable")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actor := r.Header.Get("Resolved-Username")
	client, err := engines.IssueAIAccessClient(tenantID, req.Name, req.ActingUser, req.Scopes, actor)
	var bad *engines.AIIssueError
	if errors.As(err, &bad) {
		writeAPIErrorDetail(w, r, "SEC-0304", "", bad.Message)
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEventCtx(r.Context(), tenantID, actor, "AI_ACCESS_CLIENT_ISSUED", "SUCCESS",
		fmt.Sprintf("client=%s acting_user=%s scopes=%s", client.ClientID, req.ActingUser, strings.Join(client.Credential.Scopes, ",")))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(client)
}

// handleSubmitAssistantDraft is the person's step. It only ever runs for a
// signed-in person (the assistant's token is not a session), and it makes the
// order through the ordinary create path.
func handleSubmitAssistantDraft(w http.ResponseWriter, r *http.Request) {
	if !requireCreate(w, r, "SalesOrder") {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	orderID, err := engines.SubmitAssistantDraft(tenantID, r.PathValue("id"), userID)
	var bad *engines.AIDraftError
	switch {
	case errors.Is(err, engines.ErrAIOrderNotFound):
		writeAPIError(w, r, "SEC-0306", "")
		return
	case errors.Is(err, engines.ErrAIStoreNotAllowed):
		writeAPIError(w, r, "SEC-0301", "")
		return
	case errors.As(err, &bad):
		writeAPIErrorDetail(w, r, "SEC-0306", "", bad.Message)
		return
	case err != nil:
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEventCtx(r.Context(), tenantID, userID, "AI_ACCESS_DRAFT_SUBMITTED", "SUCCESS",
		fmt.Sprintf("draft=%s order=%s", r.PathValue("id"), orderID))
	aiJSON(w, http.StatusOK, map[string]interface{}{"order_id": orderID, "draft_id": r.PathValue("id")})
}

// handleListAssistantDrafts lists the open drafts at the signed-in user's stores,
// for the person who will check and Submit them. Needs the right to make orders.
func handleListAssistantDrafts(w http.ResponseWriter, r *http.Request) {
	if !requireCreate(w, r, "SalesOrder") {
		return
	}
	drafts, err := engines.ListAssistantDrafts(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "The drafts could not be read.")
		return
	}
	aiJSON(w, http.StatusOK, map[string]interface{}{"drafts": drafts})
}
