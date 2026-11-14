package server

import (
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
)

// Ask 170 item 170.30: pay an employee advance (Dr advance / Cr bank).
func handlePayEmployeeAdvance(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	var req struct {
		AdvanceID string `json:"advance_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AdvanceID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'advance_id' is required")
		return
	}
	if err := engines.PayEmployeeAdvance(tenantID, req.AdvanceID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEvent(tenantID, userID, "EXPENSE_ADVANCE_PAY", "SUCCESS", fmt.Sprintf("Employee advance %s paid", req.AdvanceID))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "paid"})
}
