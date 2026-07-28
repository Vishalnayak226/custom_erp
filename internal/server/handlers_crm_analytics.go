package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 26.7.9/26.7.11 (CRM/Loyalty Sprint P2 follow-up) handlers. Kept in
// their own file, separate from the existing loyalty/voucher handlers, to
// avoid colliding with any concurrent session's in-flight edits to those
// shared files - same precedent Stage 26.5's handlers_wms_enterprise.go set.

func handleMergeCustomers(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		PrimaryCustomerID   string `json:"primary_customer_id"`
		DuplicateCustomerID string `json:"duplicate_customer_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PrimaryCustomerID == "" || req.DuplicateCustomerID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'primary_customer_id' and 'duplicate_customer_id' are required")
		return
	}
	if err := engines.MergeCustomers(tenantID, req.PrimaryCustomerID, req.DuplicateCustomerID, userID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "merged"})
}

// handleCleverTapSegmentSync (26.7.11) is the inbound half of "two-way"
// segment sync - authenticated via the same clevertap_credentials.passcode
// SaveCleverTapCredential stores, in a header rather than a bearer token
// since this is called by CleverTap itself, not a logged-in user (same
// "per-tenant shared secret" shape handleBigCommerceWebhook uses).
func handleCleverTapSegmentSync(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if !engines.VerifyCleverTapWebhookPasscode(tenantID, r.Header.Get("X-CleverTap-Passcode")) {
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "invalid or missing CleverTap passcode")
		return
	}
	var req struct {
		CustomerID string   `json:"customer_id"`
		Segments   []string `json:"segments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "invalid request body")
		return
	}
	if err := engines.ReceiveCleverTapSegmentSync(tenantID, req.CustomerID, req.Segments); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "acknowledged"})
}
