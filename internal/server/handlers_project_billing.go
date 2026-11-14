package server

import (
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
)

// Ask 170 items 170.32 and 170.33: complete a project milestone (raising its
// Draft invoice when billable) and run a month's project revenue recognition.
func handleCompleteProjectMilestone(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	var req struct {
		MilestoneID string `json:"milestone_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MilestoneID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'milestone_id' is required")
		return
	}
	invoiceID, err := engines.CompleteProjectMilestone(tenantID, req.MilestoneID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEvent(tenantID, userID, "PROJECT_MILESTONE_COMPLETE", "SUCCESS", fmt.Sprintf("milestone=%s invoice=%s", req.MilestoneID, invoiceID))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "completed", "invoice_id": invoiceID})
}

func handleRunProjectRevenue(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	var req struct {
		Period string `json:"period"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Period == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'period' (YYYY-MM) is required")
		return
	}
	res, err := engines.RunProjectRevenue(tenantID, req.Period)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEvent(tenantID, userID, "PROJECT_REVENUE_RUN", "SUCCESS", fmt.Sprintf("period=%s projects=%d", req.Period, len(res)))
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"period": req.Period, "projects": res})
}
