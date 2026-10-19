package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"custom_erp/engines"
)

// Stage 102: the store app (public/store/, an installable web app for store
// staff). It signs in, switches store and decides approvals through the
// ERP's existing endpoints; this file holds the one call it adds.

// handleStoreAppItem is "scan an item": GET /api/v1/store-app/item?code= with
// an item code, barcode, receipt-date barcode or GRN batch number.
func handleStoreAppItem(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'code' is required")
		return
	}
	item, err := engines.StoreAppLookup(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"),
		r.Header.Get("Resolved-Location"), code)
	if errors.Is(err, engines.ErrItemNotFound) {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "No item or batch matches \""+code+"\". Check the code, or scan the barcode again.")
		return
	}
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to look up this item")
		return
	}
	_ = json.NewEncoder(w).Encode(item)
}
