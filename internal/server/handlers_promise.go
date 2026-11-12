package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strconv"
)

// Stage 148.27: the promise for a quantity of an item, for a new order line.
// Reads only; empty while the setting is off.
func handlePromisePreview(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "SalesOrder", "read"); err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	q := r.URL.Query()
	qty, _ := strconv.ParseFloat(q.Get("qty"), 64)
	if loc := q.Get("location"); loc != "" && !requireStoreAccess(w, r, loc) {
		return
	}
	p, err := engines.PromiseForLine(tenantID, q.Get("sku"), qty, q.Get("location"), q.Get("channel"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(p)
}
