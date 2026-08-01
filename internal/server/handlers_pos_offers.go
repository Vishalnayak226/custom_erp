package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Stage 30.7: the POS offer preview endpoint.
//
// The POS screen calls this as the cart changes so the cashier can see which
// offers apply and what they take off, before taking payment. It is a
// read-only preview and is deliberately NOT what the sale trusts - checkout
// re-runs the same evaluator server-side against the tenant's Offer rows, so
// the discount that actually reaches the books is always recomputed, never
// carried over from whatever the client displayed.
func handlePOSOffersPreview(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		CustomerID  string                  `json:"customer_id"`
		CouponCodes []string                `json:"coupon_codes"`
		Items       []engines.OfferCartLine `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid offer preview payload")
		return
	}
	if len(req.Items) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "At least one item is required to evaluate offers")
		return
	}
	for _, it := range req.Items {
		if it.Sku == "" || it.Qty <= 0 || it.SalePrice < 0 {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Each item needs a sku, a positive qty and a non-negative sale price")
			return
		}
	}

	eval, err := engines.EvaluatePOSOffers(tenantID, engines.OfferEvaluationInput{
		Lines:       req.Items,
		CustomerID:  req.CustomerID,
		CouponCodes: req.CouponCodes,
	})
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to evaluate offers")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(eval)
}
