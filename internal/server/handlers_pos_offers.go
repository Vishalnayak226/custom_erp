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
		Location    string                  `json:"location"` // 215.8: an offer's stores
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
		Location:    req.Location,
	})
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to evaluate offers")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(eval)
}

// handleOfferItemListImport (215.10): an offer's item lists from a CSV of
// offer, list, code rows. ?dry_run=1 previews; nothing is written. Each list
// the file names replaces that list; the offers are saved through the CSV
// import, so the Offer rights and checks are the ones every save meets.
func handleOfferItemListImport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	role := r.Header.Get("Resolved-Role")
	allowed, err := checkPermission(tenantID, role, "Offer", "update")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		writeAPIError(w, r, "GLOBAL-0007", "")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "CSV file is mandatory under multipart FormFile 'file'")
		return
	}
	defer file.Close()
	dryRun := r.URL.Query().Get("dry_run") == "1"
	res, err := engines.ImportOfferItemLists(r.Context(), tenantID, userID, role, engines.NamedUpload(file, header.Filename), dryRun)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}
