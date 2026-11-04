package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strconv"
)

// Stage 148.11: PO line control - the instalments, tolerance, confirmation and
// full-delivery flag of a line, stopping a line, closing its rest, and
// cancelling lines of a transfer.

func poLineRequest(w http.ResponseWriter, r *http.Request) (tenantID string, lineNo int, ok bool) {
	tenantID = r.Header.Get("Resolved-Tenant-ID")
	allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "PurchaseOrder", "update")
	if err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return "", 0, false
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid line number")
		return "", 0, false
	}
	return tenantID, n, true
}

func handlePOLineControl(w http.ResponseWriter, r *http.Request) {
	tenantID, n, ok := poLineRequest(w, r)
	if !ok {
		return
	}
	var patch map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if err := engines.SetPOLineControl(tenantID, r.PathValue("id"), r.Header.Get("Resolved-User-ID"), n, patch); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "line": n})
}

func handlePOLineStop(w http.ResponseWriter, r *http.Request) { poLineClose(w, r, true) }

func handlePOLineCloseRest(w http.ResponseWriter, r *http.Request) { poLineClose(w, r, false) }

func poLineClose(w http.ResponseWriter, r *http.Request, stop bool) {
	tenantID, n, ok := poLineRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		ReasonCode string `json:"reason_code"`
		Note       string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	user := r.Header.Get("Resolved-User-ID")
	var closed float64
	var err error
	if stop {
		err = engines.StopPOLine(tenantID, r.PathValue("id"), user, n, req.ReasonCode, req.Note)
	} else {
		closed, err = engines.ClosePOLineRest(tenantID, r.PathValue("id"), user, n, req.ReasonCode, req.Note)
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "line": n, "closed_qty": closed})
}

func handleCancelTransferLines(w http.ResponseWriter, r *http.Request) {
	if !requireTransferUpdate(w, r) {
		return
	}
	var req struct {
		TransferOrderID string   `json:"transfer_order_id"`
		ReasonCode      string   `json:"reason_code"`
		Skus            []string `json:"skus"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TransferOrderID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'transfer_order_id' is required")
		return
	}
	if err := engines.CancelTransferLines(r.Header.Get("Resolved-Tenant-ID"), req.TransferOrderID,
		r.Header.Get("Resolved-User-ID"), req.ReasonCode, req.Skus); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "transfer_order_id": req.TransferOrderID})
}
