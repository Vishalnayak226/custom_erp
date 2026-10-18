package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 37.9: Quality & maintenance endpoints. InspectionPlan/
// MaintenanceSchedule are read/list/create via the generic doc API (Masters,
// like Project/CostCenter); only the lifecycle actions on
// CertificateOfAnalysis/NonConformanceReport/MaintenanceOrder need handlers.

func handleCreateCertificateOfAnalysis(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		BatchNo        string                   `json:"batch_no"`
		Item           string                   `json:"item"`
		InspectionPlan string                   `json:"inspection_plan"`
		TestResults    []map[string]interface{} `json:"test_results"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	id, err := engines.CreateCertificateOfAnalysis(tenantID, req.BatchNo, req.Item, req.InspectionPlan, req.TestResults, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Draft"})
}

func handleReleaseCertificateOfAnalysis(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	id := r.PathValue("id")
	if err := engines.ReleaseCertificateOfAnalysis(tenantID, id); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Released"})
}

func handleRejectCertificateOfAnalysis(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	id := r.PathValue("id")
	binsAffected, err := engines.RejectCertificateOfAnalysis(tenantID, id, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Rejected", "bins_quarantined": binsAffected})
}

func handleCreateNonConformanceReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Description   string `json:"description"`
		SourceDoctype string `json:"source_doctype"`
		SourceID      string `json:"source_id"`
		Item          string `json:"item"`
		BatchNo       string `json:"batch_no"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if !rfqAllowed(w, r, "NonConformanceReport", "create") {
		return
	}
	id, err := engines.RaiseNonConformance(tenantID, req.Description, req.SourceDoctype, req.SourceID, req.Item, req.BatchNo, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Draft"})
}

func handleInvestigateNonConformanceReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		RootCauseReasonCode string `json:"root_cause_reason_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	id := r.PathValue("id")
	if !rfqAllowed(w, r, "NonConformanceReport", "update") {
		return
	}
	if err := engines.InvestigateNonConformanceReport(tenantID, id, req.RootCauseReasonCode); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Investigating"})
}

func handlePlanCorrectiveAction(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		CorrectiveAction string `json:"corrective_action"`
		Owner            string `json:"owner"`
		DueDate          string `json:"due_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	id := r.PathValue("id")
	if !rfqAllowed(w, r, "NonConformanceReport", "update") {
		return
	}
	if err := engines.PlanCorrectiveActionFor(tenantID, id, req.CorrectiveAction, req.Owner, req.DueDate); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "CorrectiveActionPlanned"})
}

func handleCloseNonConformanceReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		ClosureEvidence string `json:"closure_evidence"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	id := r.PathValue("id")
	if !rfqAllowed(w, r, "NonConformanceReport", "update") {
		return
	}
	if err := engines.CloseNonConformanceWithEvidence(tenantID, id, req.ClosureEvidence, r.Header.Get("Resolved-User-ID")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Closed"})
}

func handleStartMaintenanceOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	id := r.PathValue("id")
	if !rfqAllowed(w, r, "MaintenanceOrder", "update") {
		return
	}
	if err := engines.StartMaintenanceOrderBy(tenantID, id, r.Header.Get("Resolved-User-ID")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "InProgress"})
}

func handleCompleteMaintenanceOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req engines.MaintenanceCompletion
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	id := r.PathValue("id")
	if !rfqAllowed(w, r, "MaintenanceOrder", "update") {
		return
	}
	if err := engines.CompleteMaintenanceOrderWith(tenantID, id, req, r.Header.Get("Resolved-User-ID")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Completed"})
}

func handleCancelMaintenanceOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	id := r.PathValue("id")
	if !rfqAllowed(w, r, "MaintenanceOrder", "update") {
		return
	}
	if err := engines.CancelMaintenanceOrder(tenantID, id); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Cancelled"})
}

// Stage 55.13: inspections with a source, readings and a disposition, and
// maintenance orders scheduled from Draft.

func handleRecordQualityInspection(w http.ResponseWriter, r *http.Request) {
	var in engines.InspectionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if !rfqAllowed(w, r, "QualityInspection", "create") {
		return
	}
	id, result, err := engines.RecordQualityInspection(r.Header.Get("Resolved-Tenant-ID"), in, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "result": result, "status": "Draft"})
}

func handleDisposeQualityInspection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disposition string `json:"disposition"`
		Note        string `json:"note"`
		HoldCode    string `json:"hold_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if !rfqAllowed(w, r, "QualityInspection", "update") {
		return
	}
	out, err := engines.DisposeQualityInspection(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), req.Disposition, req.Note, req.HoldCode, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func handleScheduleMaintenanceOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScheduledDate string `json:"scheduled_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if !rfqAllowed(w, r, "MaintenanceOrder", "update") {
		return
	}
	id := r.PathValue("id")
	if err := engines.ScheduleMaintenanceOrderOn(r.Header.Get("Resolved-Tenant-ID"), id, req.ScheduledDate); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Scheduled"})
}

func handleCreateMaintenanceOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Asset         string `json:"asset"`
		Description   string `json:"description"`
		ScheduledDate string `json:"scheduled_date"`
		Checklist     string `json:"checklist"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if !rfqAllowed(w, r, "MaintenanceOrder", "create") {
		return
	}
	id, err := engines.CreateMaintenanceOrder(r.Header.Get("Resolved-Tenant-ID"), req.Asset, req.Description, req.ScheduledDate, req.Checklist, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
}
