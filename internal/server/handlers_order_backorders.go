package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 73.4.2.2: retry every order waiting for stock now, oldest first - the
// same sweep the backorder worker runs when orders.backorder_auto_release is on.
func handleReleaseBackorders(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "SalesOrder", "update"); err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	released, waiting, err := engines.ReleaseBackorders(tenantID, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]int{"released": released, "waiting": waiting})
}
