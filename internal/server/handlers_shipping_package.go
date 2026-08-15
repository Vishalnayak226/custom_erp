package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 35.4 - the HTTP surface for the outbound document chain: shipping
// packages, invoice-from-pack, and gate passes.
//
// Shaped like the 35.3 order-mutation routes: a POST to the thing being acted
// on, one action per URL, so the console's action bar hits one consistent
// family. Every handler reads its actor from Resolved-User-ID, the
// apiMiddleware convention, rather than accepting one in the body.

// handleCreateShippingPackage is 35.4.1: turn a completed pack task into a
// package. Idempotent in the engine, so a double-tapped button is harmless.
func handleCreateShippingPackage(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		FulfillmentTaskID string `json:"fulfillment_task_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	packageID, err := engines.CreateShippingPackageFromTask(tenantID, req.FulfillmentTaskID, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "created", "package_id": packageID})
}

// handleListShippingPackages answers "what boxes exist for this task/order".
func handleListShippingPackages(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	packages, err := engines.ListShippingPackages(tenantID,
		r.URL.Query().Get("fulfillment_task_id"), r.URL.Query().Get("order_id"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(packages)
}

// handleUpdateShippingPackage sets weight/dimensions/type on a Draft package.
func handleUpdateShippingPackage(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req engines.ShippingPackageUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if err := engines.UpdateShippingPackage(tenantID, r.PathValue("id"), req, r.Header.Get("Resolved-User-ID")); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// handleSplitShippingPackage is 35.4.1's split: move part of a Draft package
// into a new one.
func handleSplitShippingPackage(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Move []engines.ShippingPackageLine `json:"move"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	newID, err := engines.SplitShippingPackage(tenantID, r.PathValue("id"), req.Move, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "split", "package_id": newID})
}

// handleCancelShippingPackage voids a package that has not shipped.
func handleCancelShippingPackage(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		ReasonCode string `json:"reason_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if err := engines.CancelShippingPackage(tenantID, r.PathValue("id"), req.ReasonCode, r.Header.Get("Resolved-User-ID")); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

// handleInvoiceShippingPackage is 35.4.2 - the route that closes 26.12.3's
// deferred invoice-from-pack item. Interstate is a pointer so an omitted key
// means "derive it" rather than "intra-state".
func handleInvoiceShippingPackage(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Interstate *bool `json:"interstate"`
	}
	// An empty body is the normal case here (derive everything), so a decode
	// failure on no content must not be an error.
	_ = json.NewDecoder(r.Body).Decode(&req)

	invoiceID, err := engines.GenerateInvoiceForPackage(tenantID, r.PathValue("id"),
		r.Header.Get("Resolved-User-ID"), engines.PackInvoiceOptions{Interstate: req.Interstate})
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "invoiced", "invoice_id": invoiceID})
}

// handleCreateGatePass is 35.4.4.
func handleCreateGatePass(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req engines.GatePassInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	gatePassID, err := engines.CreateGatePass(tenantID, req, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "created", "gate_pass_id": gatePassID})
}

// handleUpdateGatePass amends vehicle/driver details before completion.
func handleUpdateGatePass(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req engines.GatePassInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if err := engines.UpdateGatePass(tenantID, r.PathValue("id"), req, r.Header.Get("Resolved-User-ID")); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// handleGatePassTransition serves issue / complete / discard off one handler,
// because the three differ only in the target state and whether a reason code
// is required. Three near-identical handlers would be three places for the
// next change to be applied to two of.
func handleGatePassTransition(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get("Resolved-Tenant-ID")
		if r.Method != http.MethodPost {
			writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		var req struct {
			ReasonCode string `json:"reason_code"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		gatePassID := r.PathValue("id")
		userID := r.Header.Get("Resolved-User-ID")
		var err error
		switch action {
		case "issue":
			err = engines.IssueGatePass(tenantID, gatePassID, userID)
		case "complete":
			err = engines.CompleteGatePass(tenantID, gatePassID, userID)
		case "discard":
			err = engines.DiscardGatePass(tenantID, gatePassID, req.ReasonCode, userID)
		default:
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Unknown gate pass action")
			return
		}
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": action + "d"})
	}
}

// handleSearchGatePasses is the gate desk's lookup.
func handleSearchGatePasses(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	q := r.URL.Query()
	results, err := engines.SearchGatePasses(tenantID,
		q.Get("location_code"), q.Get("vehicle_number"), q.Get("manifest_id"), q.Get("status"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(results)
}

// handleOrderCreditNotes is 35.4.5's manual entry point. CancelOrder already
// raises the notes automatically; this exists for an order cancelled before
// 35.4.5 shipped, and for the case where the automatic pass failed and an
// operator needs to retry it. Idempotent, so retrying is safe.
func handleOrderCreditNotes(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		ReasonCode string `json:"reason_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if req.ReasonCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'reason_code' is required")
		return
	}
	notes, err := engines.IssueCancellationCreditNotes(tenantID, r.PathValue("id"), req.ReasonCode, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "issued", "credit_note_ids": notes})
}
