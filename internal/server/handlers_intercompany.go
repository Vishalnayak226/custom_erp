package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 37.2.2: Intercompany Transaction endpoints. Submit-for-approval/
// approve/reject reuse the existing generic /api/v1/approval/submit|decide
// endpoints (doctype "IntercompanyTransaction"), the same JournalVoucher
// precedent - no new endpoint needed for that part.

func handleCreateIntercompanyTransaction(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		TransactionDate string  `json:"transaction_date"`
		Narration       string  `json:"narration"`
		FromEntity      string  `json:"from_entity"`
		FromAccountCode string  `json:"from_account_code"`
		ToEntity        string  `json:"to_entity"`
		ToAccountCode   string  `json:"to_account_code"`
		Amount          float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	id, err := engines.CreateIntercompanyTransaction(tenantID, req.TransactionDate, req.Narration,
		req.FromEntity, req.FromAccountCode, req.ToEntity, req.ToAccountCode, req.Amount, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Draft"})
}

func handleRetryPostIntercompanyTransaction(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	id := r.PathValue("id")
	if err := engines.RetryPostApprovedIntercompanyTransaction(tenantID, id); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Posted"})
}
