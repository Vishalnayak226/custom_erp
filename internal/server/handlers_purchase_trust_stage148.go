package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"custom_erp/engines"
)

// Stage 148.12 / 148.13: the purchase-rate history shown on an item, and the
// vendor scorecard shown on a vendor and in the RFQ comparison. Both are reads.

func handlePurchaseRateHistory(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "Item", "read", "You do not have permission to view items") {
		return
	}
	// The rate is Item.standard_cost: a role that cannot see it on the item
	// cannot read its history either.
	if !engines.RoleMaySeeCost(r.Header.Get("Resolved-Role")) {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	sku := strings.TrimSpace(r.URL.Query().Get("sku"))
	if sku == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'sku' is required")
		return
	}
	h, err := engines.ItemPurchaseRateHistory(r.Header.Get("Resolved-Tenant-ID"), sku, 50)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"sku": sku, "history": h})
}

func handleVendorScorecard(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "Vendor", "read", "You do not have permission to view vendors") {
		return
	}
	q := r.URL.Query()
	vendor := strings.TrimSpace(q.Get("vendor"))
	if vendor == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'vendor' is required")
		return
	}
	s, err := engines.VendorScorecardFor(r.Header.Get("Resolved-Tenant-ID"), vendor, q.Get("from"), q.Get("to"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(s)
}
