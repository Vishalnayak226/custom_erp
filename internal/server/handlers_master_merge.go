package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 47.13.5: merging a duplicate supplier or item (engines/master_merge.go).
// Admin only. GET lists the history; POST {doctype, keep_id, merge_id, reason}
// asks for a merge (47.13.5, ask 179: it waits in Approvals where a MasterMerge
// approval rule exists, else it is done at once); POST /{id}/undo puts the
// references back.
func handleMasterMerges(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	tenantID, actor := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username")
	if r.Method == http.MethodGet {
		list, err := engines.ListMasterMerges(tenantID)
		if err != nil {
			writeEngineError(w, r, err, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"merges": list})
		return
	}
	var req struct {
		Doctype string `json:"doctype"`
		KeepID  string `json:"keep_id"`
		MergeID string `json:"merge_id"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	res, err := engines.RequestMasterMerge(tenantID, req.Doctype, req.KeepID, req.MergeID, req.Reason, actor, r.Header.Get("Resolved-Role"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handleMasterMergeUndo(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	res, err := engines.UnmergeMasters(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.Header.Get("Resolved-Username"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
