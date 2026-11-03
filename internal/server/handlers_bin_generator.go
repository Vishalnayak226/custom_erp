package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// handleBinGenerator (Stage 146.3.4) previews or creates Aisle x Rack x Shelf x
// Bin masters from range specs. With "preview": true nothing is written.
func handleBinGenerator(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	var in engines.BinGenInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	in.UserID = r.Header.Get("Resolved-User-ID")
	in.Scope = &engines.SessionScope{Role: r.Header.Get("Resolved-Role"), UserID: in.UserID, LocationCode: r.Header.Get("Resolved-Location")}
	if !in.Preview {
		allowed, permErr := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "Bin", "create")
		if permErr != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, permErr.Error())
			return
		}
		if !allowed {
			writeAPIError(w, r, "GLOBAL-0011", "")
			return
		}
	}
	res, err := engines.GenerateBins(tenantID, in)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
