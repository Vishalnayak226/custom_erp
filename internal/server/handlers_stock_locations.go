package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"custom_erp/engines"
)

// Stage 85.B: store stock locations (engines/stock_locations.go). Setup is
// Super-Admin-only; Move Stock needs create on StockMove and the store.

// handleStockLocations: GET is the whole setup (locations, stores and which
// locations each uses, transaction defaults); POST saves one location.
func handleStockLocations(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method == http.MethodGet {
		locations, err := engines.ListStockLocations(tenantID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to read stock locations")
			return
		}
		stores, err := engines.ListStores(tenantID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to list stores")
			return
		}
		matrix, err := engines.StoreLocationMatrix(tenantID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to read store locations")
			return
		}
		defaults, err := engines.ListStockLocationDefaults(tenantID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to read transaction defaults")
			return
		}
		// Each store's effective list, so the screen never re-derives the
		// "no rows = the defaults" rule.
		uses := map[string][]string{}
		for _, s := range stores {
			ls, err := engines.StoreStockLocations(tenantID, s.Code)
			if err != nil {
				continue
			}
			for _, l := range ls {
				uses[s.Code] = append(uses[s.Code], l.Code)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"locations": locations, "stores": stores, "own_list": matrix, "uses": uses, "defaults": defaults,
		})
		return
	}
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	var req engines.StockLocation
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if err := engines.SaveStockLocation(tenantID, req, r.Header.Get("Resolved-Username")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleStoreStockLocations saves which locations one store uses.
func handleStoreStockLocations(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	var req struct {
		Store     string   `json:"store"`
		Locations []string `json:"locations"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Store) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'store' and 'locations' are required")
		return
	}
	if err := engines.SetStoreStockLocations(r.Header.Get("Resolved-Tenant-ID"), req.Store, req.Locations, r.Header.Get("Resolved-Username")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleStockLocationDefault saves one transaction's from / to.
func handleStockLocationDefault(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	var req struct {
		TxnType string `json:"txn_type"`
		From    string `json:"from_location"`
		To      string `json:"to_location"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if err := engines.SetStockLocationDefault(r.Header.Get("Resolved-Tenant-ID"), req.TxnType, req.From, req.To, r.Header.Get("Resolved-Username")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleStockLocationBalance is one item's stock at one store by location -
// the store defaults to the working store.
func handleStockLocationBalance(w http.ResponseWriter, r *http.Request) {
	store := strings.TrimSpace(r.URL.Query().Get("store"))
	if store == "" {
		store = r.Header.Get("Resolved-Location")
	}
	sku := strings.TrimSpace(r.URL.Query().Get("sku"))
	if sku == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'sku' is required")
		return
	}
	if !requireStoreAccess(w, r, store) {
		return
	}
	rows, err := engines.StockLocationBalances(r.Header.Get("Resolved-Tenant-ID"), sku, store)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to read stock by location")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"sku": sku, "store": store, "locations": rows})
}

// handleStockTxnHeader (Stage 106.5) is a transaction's From / To at the top
// of its screen: ?txn= (Goods Receipt, POS Sale...) at ?store= or the
// working store.
func handleStockTxnHeader(w http.ResponseWriter, r *http.Request) {
	store := strings.TrimSpace(r.URL.Query().Get("store"))
	if store == "" {
		store = r.Header.Get("Resolved-Location")
	}
	if !requireStoreAccess(w, r, store) {
		return
	}
	h, err := engines.StockTxnHeaderFor(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("txn"), store)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(h)
}

// handleMoveStock moves stock between two locations of one store.
func handleMoveStock(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "StockMove", "create")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to check permissions")
		return
	}
	if !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	var req engines.StockMoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if strings.TrimSpace(req.Store) == "" {
		req.Store = r.Header.Get("Resolved-Location")
	}
	if !requireStoreAccess(w, r, req.Store) {
		return
	}
	number, err := engines.MoveStock(tenantID, req, r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Username"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "id": number})
}

// handleResolveLineCodes (Stage 85.E8) says which item each uploaded code is.
func handleResolveLineCodes(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if ok, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "Item", "read"); err != nil || !ok {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	var req struct {
		Codes []string `json:"codes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	out, err := engines.ResolveLineCodes(tenantID, req.Codes)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}
