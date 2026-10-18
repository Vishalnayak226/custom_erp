package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 73.1.7: the transfer order lifecycle around dispatch and receipt -
// D04 approval (submit for maker-checker, or a Store Manager's direct
// approval below the client's limit), cancel before dispatch, and return of
// in-transit goods to the source. Receipt itself stays on
// POST /api/v1/transfer/receive (handlers_operations.go).

// requireTransferUpdate is the doctype permission every state change here
// checks, the same one the generic approval submit endpoint applies.
func requireTransferUpdate(w http.ResponseWriter, r *http.Request) bool {
	allowed, err := checkPermission(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), "TransferOrder", "update")
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

func handleTransferDetail(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "TransferOrder", "read")
	if err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("transfer_order_id"))
	if id == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'transfer_order_id' is required")
		return
	}
	v, err := engines.GetTransferDetail(tenantID, id)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func handleSubmitTransferOrder(w http.ResponseWriter, r *http.Request) {
	if !requireTransferUpdate(w, r) {
		return
	}
	var req struct {
		TransferOrderID string `json:"transfer_order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TransferOrderID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'transfer_order_id' is required")
		return
	}
	if err := engines.SubmitTransferOrderForApproval(r.Header.Get("Resolved-Tenant-ID"), req.TransferOrderID,
		r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Pending Approval", "transfer_order_id": req.TransferOrderID})
}

func handleApproveTransferOrderDirect(w http.ResponseWriter, r *http.Request) {
	if !requireTransferUpdate(w, r) {
		return
	}
	var req struct {
		TransferOrderID string `json:"transfer_order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TransferOrderID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'transfer_order_id' is required")
		return
	}
	v, err := engines.ApproveTransferOrderDirect(r.Header.Get("Resolved-Tenant-ID"), req.TransferOrderID,
		r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"), r.Header.Get("Resolved-Location"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "Approved", "transfer_order_id": req.TransferOrderID, "value": v})
}

func handleCancelTransferOrder(w http.ResponseWriter, r *http.Request) {
	if !requireTransferUpdate(w, r) {
		return
	}
	var req struct {
		TransferOrderID string `json:"transfer_order_id"`
		ReasonCode      string `json:"reason_code"`
		Note            string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TransferOrderID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'transfer_order_id' is required")
		return
	}
	if err := engines.CancelTransferOrder(r.Header.Get("Resolved-Tenant-ID"), req.TransferOrderID,
		r.Header.Get("Resolved-User-ID"), req.ReasonCode, req.Note); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Cancelled", "transfer_order_id": req.TransferOrderID})
}

func handleReturnTransferToSource(w http.ResponseWriter, r *http.Request) {
	if !requireTransferUpdate(w, r) {
		return
	}
	var req struct {
		TransferOrderID string                       `json:"transfer_order_id"`
		ReasonCode      string                       `json:"reason_code"`
		Note            string                       `json:"note"`
		ReturnKey       string                       `json:"return_key"`
		Lines           []engines.TransferReturnLine `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TransferOrderID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'transfer_order_id' is required")
		return
	}
	res, err := engines.ReturnTransferToSource(r.Header.Get("Resolved-Tenant-ID"), req.TransferOrderID,
		r.Header.Get("Resolved-User-ID"), req.ReasonCode, req.Note, req.ReturnKey, req.Lines)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
