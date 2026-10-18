package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 59.2-59.5: mixed-owner (3PL) isolation endpoints. The exception
// queue itself is the owner-stock-exceptions report (generic report
// endpoint); these are its correction and the order / availability reads.

// handleSetOrderOwner sets the owner (3PL client) on an order before it is
// released to the warehouse.
func handleSetOrderOwner(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "SalesOrder", "update")
	if err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	var req struct {
		OrderID string `json:"order_id"`
		OwnerID string `json:"owner_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.OrderID) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'order_id' is required")
		return
	}
	if err := engines.SetOrderOwner(tenantID, req.OrderID, req.OwnerID, r.Header.Get("Resolved-User-ID")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"order_id": req.OrderID, "owner_id": strings.TrimSpace(req.OwnerID)})
}

// handleOwnerAvailability is one owner's available-to-sell for an item at a
// location.
func handleOwnerAvailability(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sku, loc := strings.TrimSpace(q.Get("sku")), strings.TrimSpace(q.Get("location"))
	if sku == "" || loc == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameters 'sku' and 'location' are required")
		return
	}
	res, err := engines.OwnerAvailableToSell(r.Header.Get("Resolved-Tenant-ID"), sku, loc, q.Get("owner_id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

// handleCorrectOwnerStock is the exception queue's resolution: set one
// owner's slice of a bin to a counted figure, with a reason. Store Manager or
// administrator only - it re-attributes stock between clients.
func handleCorrectOwnerStock(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !engines.IsSuperAdmin(role) && engines.CanonicalRole(role) != "Store Manager" {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	var req struct {
		BinCode   string `json:"bin_code"`
		Sku       string `json:"sku"`
		Condition string `json:"condition"`
		OwnerID   string `json:"owner_id"`
		Qty       int    `json:"qty"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BinCode == "" || req.Sku == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'bin_code' and 'sku' are required")
		return
	}
	if err := engines.SetOwnerSliceQty(r.Header.Get("Resolved-Tenant-ID"), req.BinCode, req.Sku, req.Condition, req.OwnerID,
		req.Qty, req.Reason, r.Header.Get("Resolved-User-ID")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "corrected"})
}
