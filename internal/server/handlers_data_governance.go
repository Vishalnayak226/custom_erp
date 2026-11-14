package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Ask 170 group I: the screens' side of retention (170.38), the masked copy
// (170.40), the data-quality queue (170.41) and the import load order (170.42).

func handleListRetentionPolicies(w http.ResponseWriter, r *http.Request) {
	if !requireSuperAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	rows, err := engines.ListRetentionPolicies(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(rows)
}

func handleSaveRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	if !requireSuperAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	var p engines.RetentionPolicy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "A retention rule is required")
		return
	}
	if err := engines.SaveRetentionPolicy(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), p); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func handleMaskedCopy(w http.ResponseWriter, r *http.Request) {
	if !requireSuperAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	var req struct {
		Source  string `json:"source"`
		TTLDays int    `json:"ttl_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "A client to copy is required")
		return
	}
	id, password, res, err := engines.CreateMaskedCopySandbox(req.Source, "", req.TTLDays, r.Header.Get("Resolved-Username"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"tenant_id": id, "admin_password": password, "result": res})
}

func dataQualityAllowed(w http.ResponseWriter, r *http.Request, action string) bool {
	ok, _ := checkPermission(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), "DataQualityIssue", action)
	if !ok {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Not allowed.")
	}
	return ok
}

func handleListDataQuality(w http.ResponseWriter, r *http.Request) {
	if !dataQualityAllowed(w, r, "read") {
		return
	}
	rows, err := engines.ListDataQualityIssues(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("status"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(rows)
}

func handleScanDataQuality(w http.ResponseWriter, r *http.Request) {
	if !dataQualityAllowed(w, r, "update") {
		return
	}
	res, err := engines.ScanDataQuality(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handleSetDataQualityStatus(w http.ResponseWriter, r *http.Request) {
	if !dataQualityAllowed(w, r, "update") {
		return
	}
	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "A status is required")
		return
	}
	if err := engines.SetDataQualityStatus(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), req.Status, req.Reason, r.Header.Get("Resolved-Username")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func handleImportLoadOrder(w http.ResponseWriter, r *http.Request) {
	steps, err := engines.ImportLoadOrder(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("doctype"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"doctype": r.PathValue("doctype"), "steps": steps})
}
