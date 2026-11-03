package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// handleSetupPath is Stage 55.3: GET /api/v1/setup/path - the core masters in
// the order they depend on each other, with what is done and what is next.
func handleSetupPath(w http.ResponseWriter, r *http.Request) {
	steps, err := engines.GetSetupPathAs(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"steps": steps})
}

// Stage 47.12.4: a step's state history, and a note (with an attachment
// reference) added to it. The history is read-only; rows are only appended.
func handleSetupStepHistory(w http.ResponseWriter, r *http.Request) {
	rows, err := engines.ListSetupStepHistory(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("step"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"history": rows})
}

func handleSetupStepNote(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	var req struct {
		Note       string `json:"note"`
		Attachment string `json:"attachment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if err := engines.AddSetupStepNote(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("step"), req.Note, req.Attachment, r.Header.Get("Resolved-Username")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}
