package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"custom_erp/engines"
)

// Stage 85.C: the customer at the till (engines/pos_customers.go).

// handlePOSCustomerLookup finds the customer with a mobile number.
func handlePOSCustomerLookup(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if ok, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "Customer", "read"); err != nil || !ok {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	c, found, phone, err := engines.LookupCustomerByPhone(tenantID, strings.TrimSpace(r.URL.Query().Get("phone")))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"found": found, "phone": phone, "customer": c})
}

// handlePOSWalkInCustomer is the store's walk-in customer, made on first use.
func handlePOSWalkInCustomer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Store string `json:"store"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if strings.TrimSpace(req.Store) == "" {
		req.Store = r.Header.Get("Resolved-Location")
	}
	if !requireStoreAccess(w, r, req.Store) {
		return
	}
	c, err := engines.EnsureWalkInCustomer(r.Header.Get("Resolved-Tenant-ID"), req.Store)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(c)
}

// handlePOSHighValuePolicy is when a bill needs the buyer's address or PAN,
// and the cash limit - so the till asks before Complete Sale.
func handlePOSHighValuePolicy(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(engines.HighValuePolicyFor(r.Header.Get("Resolved-Tenant-ID")))
}

// --- Stage 85.D3: customer advances at the till ------------------------------

// tillSession is the caller's open session at the store, or a written error:
// an advance moves money, so it is taken where a sale would be.
func tillSession(w http.ResponseWriter, r *http.Request, store string) (string, bool) {
	if !requireStoreAccess(w, r, store) {
		return "", false
	}
	id, err := engines.GetOpenSessionForCashier(r.Header.Get("Resolved-Tenant-ID"), store, r.Header.Get("Resolved-Username"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to check cashier session")
		return "", false
	}
	if id == "" {
		writeAPIError(w, r, "POSOFF-0238", "")
		return "", false
	}
	return id, true
}

// handlePOSAdvances: GET lists (customer_id, store, open=1); POST takes one.
func handlePOSAdvances(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method == http.MethodGet {
		q := r.URL.Query()
		list, err := engines.ListCustomerAdvances(tenantID, q.Get("customer_id"), q.Get("store"), q.Get("open") == "1")
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to list advances")
			return
		}
		_ = json.NewEncoder(w).Encode(list)
		return
	}
	var req engines.TakeAdvanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid advance")
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		req.Location = r.Header.Get("Resolved-Location")
	}
	session, ok := tillSession(w, r, req.Location)
	if !ok {
		return
	}
	a, err := engines.TakeCustomerAdvance(tenantID, r.Header.Get("Resolved-Username"), session, req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(a)
}

// handlePOSAdvanceRefund pays an advance back to its original tender.
func handlePOSAdvanceRefund(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Number         string  `json:"number"`
		Amount         float64 `json:"amount"`
		Reason         string  `json:"reason"`
		Location       string  `json:"location"`
		IdempotencyKey string  `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid refund")
		return
	}
	if strings.TrimSpace(req.Location) == "" {
		req.Location = r.Header.Get("Resolved-Location")
	}
	session, ok := tillSession(w, r, req.Location)
	if !ok {
		return
	}
	a, voucher, err := engines.RefundCustomerAdvance(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), session,
		req.Number, req.Amount, req.Reason, req.Location, req.IdempotencyKey)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"advance": a, "voucher": voucher})
}

func firstNonBlank(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// --- Stage 85.E2/85.E3: the store's tenders and salesmen ---------------------

func handlePOSTenders(w http.ResponseWriter, r *http.Request) {
	store := firstNonBlank(r.URL.Query().Get("store"), r.Header.Get("Resolved-Location"))
	list, err := engines.StoreTenders(r.Header.Get("Resolved-Tenant-ID"), store)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to list tenders")
		return
	}
	_ = json.NewEncoder(w).Encode(list)
}

func handlePOSSalesmen(w http.ResponseWriter, r *http.Request) {
	store := firstNonBlank(r.URL.Query().Get("store"), r.Header.Get("Resolved-Location"))
	list, mode, err := engines.StoreSalesmen(r.Header.Get("Resolved-Tenant-ID"), store)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to list salesmen")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"mode": mode, "salesmen": list})
}

// handlePOSBills (Stage 85.E4) finds paid bills to return: by bill number,
// mobile, name and/or date - at the working store, or (scope=mine) every
// store the user has been given.
func handlePOSBills(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	q := r.URL.Query()
	stores := []string{r.Header.Get("Resolved-Location")}
	if q.Get("scope") == "mine" {
		access, err := engines.UserStoreAccess(tenantID, r.Header.Get("Resolved-User-ID"))
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusServiceUnavailable, "Unable to read your stores - please retry shortly")
			return
		}
		stores = nil
		if !access.All {
			stores = access.List()
		}
	}
	bills, err := engines.SearchStoreBills(tenantID, stores, q.Get("q"), q.Get("date"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to search bills")
		return
	}
	_ = json.NewEncoder(w).Encode(bills)
}
