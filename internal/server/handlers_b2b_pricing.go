package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 148.26: pricing reaches B2B. The price lookup for an invoice line, the
// Margin release, the 9 / 49 / 99 ending tools and the open-lines flag list.

// handlePricingLine answers what a customer pays for an item on their price
// list. It only reads; a typed rate still wins, which the screen decides.
func handlePricingLine(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CustomerID string  `json:"customer_id"`
		Customer   string  `json:"customer"`
		SKU        string  `json:"sku"`
		Qty        float64 `json:"qty"`
		AsOf       string  `json:"as_of"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.SKU) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'sku' is required")
		return
	}
	tenantID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role")
	cust := strings.TrimSpace(req.CustomerID)
	if cust == "" {
		cust = engines.ResolveCustomerLink(tenantID, strings.TrimSpace(req.Customer))
	}
	res, err := engines.ResolveCustomerLinePrice(tenantID, engines.CustomerPriceQuery{CustomerID: cust, SKU: req.SKU, Qty: req.Qty, AsOf: req.AsOf})
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	out := map[string]interface{}{"found": res.Found, "rate": res.Rate, "reason": res.Reason, "has_floor": res.Floor > 0,
		"floor_mode": engines.BelowFloorMode(tenantID)}
	if engines.RoleMaySeeCost(role) {
		out["floor"] = res.Floor // the lowest rate is a margin figure: cost roles only
	}
	_ = json.NewEncoder(w).Encode(out)
}

// handleMarginRelease lets a Store Manager or an administrator release an
// order held for a rate below its floor, with a reason.
func handleMarginRelease(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	tenantID, orderID := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id")
	if err := engines.ReleaseMarginHold(tenantID, orderID, r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"), req.Reason); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	orderStatus, holdReason, err := engines.GetOrderStatus(tenantID, orderID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"order_status": orderStatus, "hold_reason": holdReason})
}

// handlePricingFlags lists the open order lines a price-list change touched.
func handlePricingFlags(w http.ResponseWriter, r *http.Request) {
	list, err := engines.ListFlaggedPriceLines(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"lines": list})
}

func endingsRequest(w http.ResponseWriter, r *http.Request) (tenantID, id, ending string, ok bool) {
	var req struct {
		VersionID string `json:"version_id"`
		Ending    string `json:"ending"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.VersionID) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'version_id' is required")
		return
	}
	tenantID = r.Header.Get("Resolved-Tenant-ID")
	allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "PriceListVersion", "update")
	if err != nil || !allowed {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Not allowed to change price lists")
		return
	}
	return tenantID, strings.TrimSpace(req.VersionID), strings.TrimSpace(req.Ending), true
}

func handlePriceEndingsPreview(w http.ResponseWriter, r *http.Request) {
	tenantID, id, ending, ok := endingsRequest(w, r)
	if !ok {
		return
	}
	rows, err := engines.PreviewPriceListEndings(tenantID, id, ending)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"rows": rows})
}

func handlePriceEndingsApply(w http.ResponseWriter, r *http.Request) {
	tenantID, id, ending, ok := endingsRequest(w, r)
	if !ok {
		return
	}
	n, err := engines.ApplyPriceListEndings(tenantID, id, ending)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEvent(tenantID, r.Header.Get("Resolved-User-ID"), "PRICE_LIST_ENDINGS_APPLIED", "SUCCESS", "version="+id+" ending="+ending)
	_ = json.NewEncoder(w).Encode(map[string]int{"changed": n})
}
