package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Items 324.1-324.4 (ask 324): stock requests. A location asks another for
// stock (a transfer order sent for approval); the source accepts, accepts
// less or rejects; the sourcing engine ranks where to ask. Pack, dispatch
// and receive stay on the transfer endpoints.

// requireTransferAction is the TransferOrder doctype permission for one
// action ("read", "create" or "update").
func requireTransferAction(w http.ResponseWriter, r *http.Request, action string) bool {
	allowed, err := checkPermission(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), "TransferOrder", action)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return false
	}
	if !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return false
	}
	return true
}

// GET /api/v1/stock-requests?side=mine|incoming&location=&state=open|closed
func handleListStockRequests(w http.ResponseWriter, r *http.Request) {
	if !requireTransferAction(w, r, "read") {
		return
	}
	q := r.URL.Query()
	rows, err := engines.ListStockRequests(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"),
		r.Header.Get("Resolved-Location"), q.Get("side"), q.Get("location"), q.Get("state"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(rows)
}

// GET /api/v1/stock-requests/reasons
func handleStockRequestReasons(w http.ResponseWriter, r *http.Request) {
	if !requireTransferAction(w, r, "read") {
		return
	}
	list, err := engines.ListStockRequestReasons(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(list)
}

// POST /api/v1/stock-requests/sources {location, lines:[{sku, qty, from}]}
func handleStockRequestSources(w http.ResponseWriter, r *http.Request) {
	if !requireTransferAction(w, r, "read") {
		return
	}
	var req struct {
		Location string                       `json:"location"`
		Lines    []engines.StockRequestLineIn `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if req.Location == "" {
		req.Location = r.Header.Get("Resolved-Location")
	}
	plan, err := engines.SuggestRequestSources(r.Header.Get("Resolved-Tenant-ID"), req.Location, req.Lines)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(plan)
}

// POST /api/v1/stock-requests {location, reason_code, reason, note, needed_by, lines}
func handleCreateStockRequest(w http.ResponseWriter, r *http.Request) {
	if !requireTransferAction(w, r, "create") {
		return
	}
	var req engines.StockRequestInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	res, err := engines.CreateStockRequest(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"),
		r.Header.Get("Resolved-Role"), r.Header.Get("Resolved-Location"), req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(res)
}

// POST /api/v1/stock-requests/accept {id, lines:[{sku, qty}]} - lines only to accept less.
func handleAcceptStockRequest(w http.ResponseWriter, r *http.Request) {
	if !requireTransferAction(w, r, "update") {
		return
	}
	var req struct {
		ID    string                       `json:"id"`
		Lines []engines.StockRequestLineIn `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'id' is required")
		return
	}
	if err := engines.AcceptStockRequest(r.Header.Get("Resolved-Tenant-ID"), req.ID, r.Header.Get("Resolved-User-ID"),
		r.Header.Get("Resolved-Role"), r.Header.Get("Resolved-Location"), req.Lines); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "Approved", "id": req.ID})
}

// POST /api/v1/stock-requests/reject {id, reason}
func handleRejectStockRequest(w http.ResponseWriter, r *http.Request) {
	if !requireTransferAction(w, r, "update") {
		return
	}
	var req struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'id' is required")
		return
	}
	if err := engines.RejectStockRequest(r.Header.Get("Resolved-Tenant-ID"), req.ID, r.Header.Get("Resolved-User-ID"),
		r.Header.Get("Resolved-Role"), r.Header.Get("Resolved-Location"), req.Reason); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "Rejected", "id": req.ID})
}

// POST /api/v1/stock-requests/auto-run {location} - the Run now button.
func handleAutoRunStockRequests(w http.ResponseWriter, r *http.Request) {
	if !requireTransferAction(w, r, "create") {
		return
	}
	var req struct {
		Location string `json:"location"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Location == "" {
		req.Location = r.Header.Get("Resolved-Location")
	}
	if req.Location == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'location' is required")
		return
	}
	res, err := engines.RunAutoStockRequests(r.Header.Get("Resolved-Tenant-ID"), req.Location, r.Header.Get("Resolved-User-ID"),
		r.Header.Get("Resolved-Role"), r.Header.Get("Resolved-Location"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
