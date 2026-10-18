package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"custom_erp/engines"
)

// Stage 89 - the Item master asked once (engines/item_setup.go,
// engines/item_attribute_map.go).

// handleGetItemSetup - GET /api/v1/inventory/item-setup: the org's item
// defaults, the kinds of item, the name order and where each field is asked.
// Loaded once by the Item form, the PO and the GRN.
func handleGetItemSetup(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "Item", "read", "You do not have permission to view items") {
		return
	}
	setup, err := engines.GetItemSetup(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to read the item setup")
		return
	}
	// ?item= (an edit form): whether the item has stock, which locks its
	// treatment to the admin's correction (Stage 89.8).
	hasStock := false
	if ref := strings.TrimSpace(r.URL.Query().Get("item")); ref != "" {
		hasStock = engines.ItemHasStockHistory(r.Header.Get("Resolved-Tenant-ID"), ref)
	} else if ref := strings.TrimSpace(r.URL.Query().Get("item_type")); ref != "" {
		hasStock = engines.ItemTypeHasStock(r.Header.Get("Resolved-Tenant-ID"), ref)
	}
	_ = json.NewEncoder(w).Encode(struct {
		*engines.ItemSetup
		IsAdmin      bool `json:"is_admin"`
		ItemHasStock bool `json:"item_has_stock"`
	}{setup, engines.IsSuperAdmin(r.Header.Get("Resolved-Role")), hasStock})
}

// handleSaveItemFieldRules - PUT /api/v1/inventory/item-setup/fields
// {fields:[...]}: where each field is asked. Admins only - it is the org's
// configuration, like the rest of Configuration.
func handleSaveItemFieldRules(w http.ResponseWriter, r *http.Request) {
	if !engines.IsSuperAdmin(r.Header.Get("Resolved-Role")) {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Only an administrator can change where item fields are asked")
		return
	}
	var body struct {
		Fields []engines.ItemFieldRule `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if err := engines.SaveItemFieldRules(tenantID, r.Header.Get("Resolved-Username"), body.Fields); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	fields, err := engines.ItemFieldRules(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Saved, but failed to read the fields back")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"fields": fields})
}

// handleGetItemAttributes - GET /api/v1/inventory/item-attributes?item_name=:
// the attributes and values an item name offers (its type's map when
// "Attributes follow the item's type" is on).
func handleGetItemAttributes(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "Item", "read", "You do not have permission to view items") {
		return
	}
	set, err := engines.ItemAttributesFor(r.Header.Get("Resolved-Tenant-ID"), strings.TrimSpace(r.URL.Query().Get("item_name")))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(set)
}

// handleSuggestAttributeMap - GET /api/v1/inventory/attribute-map/suggest?doctype=ProductType&ref=:
// what the existing items of that type (or category) use.
func handleSuggestAttributeMap(w http.ResponseWriter, r *http.Request) {
	doctype := r.URL.Query().Get("doctype")
	if doctype != "ProductType" && doctype != "Category" {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "doctype must be ProductType or Category")
		return
	}
	if !requireDocPermission(w, r, doctype, "read", "You do not have permission to view this list") {
		return
	}
	out, err := engines.SuggestAttributeMap(r.Header.Get("Resolved-Tenant-ID"), doctype, r.URL.Query().Get("ref"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"suggestions": out})
}

// handleAddItemAttributeValue - POST /api/v1/inventory/attribute-value
// {code, label, value, item_name}: "+ Add 'x' to Color" from the Item form (or
// the GRN). The value goes on the attribute's own master, and - with mapping
// on - onto the map the item name was offered it under.
func handleAddItemAttributeValue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code     string `json:"code"`
		Label    string `json:"label"`
		Value    string `json:"value"`
		ItemName string `json:"item_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	action, denied := "update", "You do not have permission to change item attributes"
	if body.Code == "" {
		action, denied = "create", "You do not have permission to create item attributes"
	}
	if !requireDocPermission(w, r, engines.ItemAttributeDoctype, action, denied) {
		return
	}
	tenantID, userID := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID")
	attr, err := engines.AddDesignAttributeValue(tenantID, userID, body.Code, body.Label, body.Value)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	if err := engines.MapAttributeValueToItemName(tenantID, userID, body.ItemName, attr.Code, strings.TrimSpace(body.Value)); err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(attr)
}

// handleCreateItemFieldList - POST /api/v1/inventory/item-setup/fields/list
// {field}: make a free-text Item field a dropdown of its own new Item
// Attribute, seeded with the values items already carry. Admins only.
func handleCreateItemFieldList(w http.ResponseWriter, r *http.Request) {
	if !engines.IsSuperAdmin(r.Header.Get("Resolved-Role")) {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Only an administrator can change where item fields are asked")
		return
	}
	var body struct {
		Field string `json:"field"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	rule, err := engines.CreateAttributeListFromField(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Username"), strings.TrimSpace(body.Field))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(rule)
}

// requireTreatmentAdmin: once an item has stock, only an admin corrects its
// treatment; anyone else is told to tell their admin (MASTER-0892).
func requireTreatmentAdmin(w http.ResponseWriter, r *http.Request) bool {
	if engines.IsSuperAdmin(r.Header.Get("Resolved-Role")) {
		return true
	}
	writeAPIError(w, r, "MASTER-0892", "")
	return false
}

// handleTreatmentCorrectionPreview - POST /api/v1/inventory/treatment-correction/preview
// {target, ref, item_type, stock_tracking, valuation, accounting_treatment, expense_account, unit_value}:
// what correcting an item (or item type) with stock would do. Changes nothing.
func handleTreatmentCorrectionPreview(w http.ResponseWriter, r *http.Request) {
	if !requireTreatmentAdmin(w, r) {
		return
	}
	var ch engines.TreatmentChange
	if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	pv, err := engines.PreviewTreatmentCorrection(r.Header.Get("Resolved-Tenant-ID"), ch)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(pv)
}

// handleTreatmentCorrectionApply - POST /api/v1/inventory/treatment-correction/apply
// {...the change, reason, confirm (the item code typed), token (from the preview)}.
func handleTreatmentCorrectionApply(w http.ResponseWriter, r *http.Request) {
	if !requireTreatmentAdmin(w, r) {
		return
	}
	var req engines.TreatmentApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	out, err := engines.ApplyTreatmentCorrection(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"),
		r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-Role"), req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}
