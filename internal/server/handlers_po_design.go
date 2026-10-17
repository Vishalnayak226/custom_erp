package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 62: the PO's "Add design" panel (read side - designs and their Items
// are created through the ordinary record API), the printed GRN / Debit Note
// / Vendor Credit Note, and posting a Vendor Credit Note by hand.

// requireDocPermission answers 403/500 itself and reports whether to go on.
func requireDocPermission(w http.ResponseWriter, r *http.Request, doctype, action, deniedMessage string) bool {
	allowed, err := checkPermission(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), doctype, action)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return false
	}
	if !allowed {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, deniedMessage)
		return false
	}
	return true
}

// handleGetDesignMatrix returns the org's product attributes and the
// design-number mode.
func handleGetDesignMatrix(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "PurchaseOrder", "create", "You do not have permission to create purchase orders") {
		return
	}
	m, err := engines.GetDesignMatrix(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to load the product attributes")
		return
	}
	_ = json.NewEncoder(w).Encode(m)
}

// handleGetDesign loads one design (by its number) and its combinations.
func handleGetDesign(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "PurchaseOrder", "read", "You do not have permission to view purchase orders") {
		return
	}
	d, err := engines.GetDesign(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("code"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(d)
}

// handleMatchDesign answers "is there already a design with this name and
// these attributes?" for the Reuse-when-it-matches mode.
func handleMatchDesign(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "PurchaseOrder", "create", "You do not have permission to create purchase orders") {
		return
	}
	var body struct {
		Name       string            `json:"name"`
		Attributes map[string]string `json:"attributes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	code, err := engines.FindMatchingDesign(r.Header.Get("Resolved-Tenant-ID"), body.Name, body.Attributes)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to look up existing designs")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
}

// handlePostVendorCreditNote posts a Draft Vendor Credit Note entered by hand
// (one raised from a GRN posts itself).
func handlePostVendorCreditNote(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "VendorCreditNote", "update", "You do not have permission to post vendor credit notes") {
		return
	}
	noteID := r.PathValue("id")
	amount, err := engines.PostVendorCreditNote(r.Header.Get("Resolved-Tenant-ID"), noteID, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "Posted", "id": noteID, "amount": amount})
}

// printTxnDoctype maps a print transaction to the doctype whose read
// permission it needs.
func printTxnDoctype(txn string) string {
	for _, t := range engines.PrintTransactions {
		if t.Key == txn {
			return t.Doctype
		}
	}
	return ""
}

// handlePrintDocument (Stage 63.1) returns one document's print data and the
// templates it may print with, best match first. ?store= picks the store the
// print is generated from; ?images=1 embeds item pictures.
func handlePrintDocument(w http.ResponseWriter, r *http.Request) {
	txn := r.PathValue("txn")
	doctype := printTxnDoctype(txn)
	if doctype == "" {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Unknown print transaction")
		return
	}
	if !requireDocPermission(w, r, doctype, "read", "You do not have permission to view this document") {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	q := r.URL.Query()
	doc, err := engines.BuildPrintDocument(tenantID, txn, r.PathValue("id"), q.Get("store"), q.Get("images") == "1")
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	templates, err := engines.PrintTemplatesFor(tenantID, doc)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to load print templates")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"document": doc, "templates": templates})
}

// handlePrintCatalog (Stage 63.4) lists what the Print Template Studio can
// place for a transaction.
func handlePrintCatalog(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "PrintTemplate", "read", "You do not have permission to view print templates") {
		return
	}
	cat, err := engines.GetPrintCatalog(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("txn"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(cat)
}

// handleVendorPOExcel (Stage 63.6) downloads the Vendor PO as .xlsx, with
// item pictures.
func handleVendorPOExcel(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "PurchaseOrder", "read", "You do not have permission to view purchase orders") {
		return
	}
	body, name, err := engines.BuildVendorPOWorkbook(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.URL.Query().Get("store"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, _ = w.Write(body)
}

// handleAddDesignAttributeValue adds a value to a product attribute, or
// creates the attribute, from the PO's design panel (Stage 65.4). Needs the
// same right the PIM screen would: update on ProductAttributeDef to extend
// one, create to add a new one.
func handleAddDesignAttributeValue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code  string `json:"code"`
		Label string `json:"label"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	action, denied := "update", "You do not have permission to change product attributes"
	if body.Code == "" {
		action, denied = "create", "You do not have permission to create product attributes"
	}
	if !requireDocPermission(w, r, "ProductAttributeDef", action, denied) {
		return
	}
	attr, err := engines.AddDesignAttributeValue(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), body.Code, body.Label, body.Value)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(attr)
}
