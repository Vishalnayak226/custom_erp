package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Purchase Order pricing preview, printed copy and vendor dispatch
// (Stage 40.1).
//
// All three exist so the PO screen never re-derives tax on the client. HSN,
// GST rate and the inter-state decision come from the Item master, the Legal
// Entity and the Vendor - resolving any of that in JavaScript would create a
// second source of truth that drifts from what the save path actually stores.
// The screen sends what the maker has typed and renders what comes back.

// handlePreviewPurchaseOrder prices an unsaved set of PO lines.
//
// Read-only: it writes nothing, which is what lets the screen call it on
// every line edit.
//
// Gated on create OR update rather than plain read, so it stays away from
// roles with no PO write access at all (vendor purchase prices are in the
// response) while still serving both callers the composer actually has: the
// maker raising a PO, and the approver amending one. Gating on create alone
// would 403 a Store Manager mid-amendment - they have update but not create
// on PurchaseOrder (db/migration.sql) - and silently blank every derived
// column on the screen.
func handlePreviewPurchaseOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")

	allowed, err := checkPermission(tenantID, role, "PurchaseOrder", "create")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if !allowed {
		if allowed, err = checkPermission(tenantID, role, "PurchaseOrder", "update"); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if !allowed {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "You do not have permission to raise or amend purchase orders")
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}

	preview, err := engines.PreviewPurchaseOrder(tenantID, payload)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(preview)
}

// handlePrintPurchaseOrder returns the fully-resolved vendor copy of a PO.
//
// MRP is stripped here rather than in the print sheet's markup: the buying
// side's expected retail price is not the vendor's business, and leaving it
// in the response would put it one "view source" away from the vendor on any
// copy that gets forwarded. Doing it server-side means every consumer of this
// endpoint - the print sheet today, an emailed copy tomorrow - drops it.
func handlePrintPurchaseOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")

	allowed, err := checkPermission(tenantID, role, "PurchaseOrder", "read")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if !allowed {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "You do not have permission to view purchase orders")
		return
	}

	doc, err := engines.BuildPurchaseOrderPrint(tenantID, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	for i := range doc.Lines {
		doc.Lines[i].MRP = 0
	}
	_ = json.NewEncoder(w).Encode(doc)
}

// handleSendPurchaseOrder records that a PO's vendor copy went out and fires
// the PurchaseOrderIssued notification event.
//
// The response carries the vendor's email and a ready-made subject/body so
// the UI can fall back to the user's own mail client when the tenant has no
// notification channel configured - which is the normal state of a tenant
// that has not set one up, and must not be a dead end.
func handleSendPurchaseOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")

	allowed, err := checkPermission(tenantID, role, "PurchaseOrder", "update")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if !allowed {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "You do not have permission to send purchase orders")
		return
	}

	doc, err := engines.MarkPurchaseOrderSent(tenantID, r.PathValue("id"), userID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	for i := range doc.Lines {
		doc.Lines[i].MRP = 0
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "sent",
		"sent_at":        doc.SentAt,
		"vendor_email":   doc.Vendor.Email,
		"purchase_order": doc,
	})
}
