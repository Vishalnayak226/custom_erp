package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// handleSetupPath is Stage 55.3: GET /api/v1/setup/path - the core masters in
// the order they depend on each other, with what is done and what is next.
func handleSetupPath(w http.ResponseWriter, r *http.Request) {
	steps, err := engines.GetSetupPath(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"steps": steps})
}
