package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Stage 49.6.8 - the admin-facing surface for the data-subject request
// lifecycle engines/privacy_rights.go implements. Without this, the
// engine's request/decide/execute/legal-hold functions would be reachable
// only from Go code (a future cmd/ tool or a test) - not "genuinely usable"
// per this codebase's own completeness bar. Super Admin only
// (LevelAdmin, route_capabilities.go): a data-subject rights decision
// (erasure, especially) is exactly the kind of action 49.0.6's acceptance
// criteria says cannot be taken unilaterally by whoever merely built the
// feature, so it is deliberately not opened to any lesser role.

func handleCreateDataSubjectRequest(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		SubjectDoctype string `json:"subject_doctype"`
		SubjectID      string `json:"subject_id"`
		RequestType    string `json:"request_type"`
		Reason         string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SubjectDoctype == "" || req.SubjectID == "" || req.RequestType == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'subject_doctype', 'subject_id' and 'request_type' are required")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actor := r.Header.Get("Resolved-Username")
	id, err := engines.RequestDataSubjectAction(tenantID, req.SubjectDoctype, req.SubjectID, req.RequestType, req.Reason, actor)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": engines.PrivacyRequestPending})
}

func handleListDataSubjectRequests(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	subjectDoctype := r.URL.Query().Get("subject_doctype")
	subjectID := r.URL.Query().Get("subject_id")
	if subjectDoctype == "" || subjectID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameters 'subject_doctype' and 'subject_id' are required")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	list, err := engines.ListDataSubjectRequests(tenantID, subjectDoctype, subjectID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(list)
}

// handleDecideDataSubjectRequest is the maker-checker gate: the API layer
// contributes nothing here beyond identifying the actor -
// engines.DecideDataSubjectRequest itself refuses decided_by == requested_by.
func handleDecideDataSubjectRequest(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	id := r.PathValue("id")
	var req struct {
		Approve bool   `json:"approve"`
		Note    string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || id == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "A request id and JSON body with 'approve' are required")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actor := r.Header.Get("Resolved-Username")
	if err := engines.DecideDataSubjectRequest(tenantID, id, actor, req.Approve, req.Note); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "decided"})
}

func handleExecuteDataSubjectRequest(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "A request id is required")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actor := r.Header.Get("Resolved-Username")
	if err := engines.ExecuteDataSubjectRequest(tenantID, id, actor); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "executed"})
}

func handleSetSubjectLegalHold(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		Doctype   string `json:"doctype"`
		SubjectID string `json:"subject_id"`
		On        bool   `json:"on"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Doctype == "" || req.SubjectID == "" || req.Reason == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'doctype', 'subject_id' and 'reason' are required")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actor := r.Header.Get("Resolved-Username")
	if err := engines.SetSubjectLegalHold(tenantID, req.Doctype, req.SubjectID, req.On, actor, req.Reason); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
