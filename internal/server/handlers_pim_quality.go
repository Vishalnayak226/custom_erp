package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"custom_erp/engines"
)

// Stage 36.7: enrichment & quality. This file covers the handlers with no
// natural home in an existing PIM handlers file - related products
// (36.7.3), UPC/EAN generation (36.7.4), joined by bulk catalog translation
// (36.7.5) as it lands.

// handlePIMSeedTranslations (36.7.5) bulk-creates Draft ProductContent rows
// in a target language, seeded from each item's Approved source-language
// content - see engines/pim_translation.go for why this is a seeding
// workflow and not a translation-provider integration. Gated on
// ProductContent "create", the same right a single Draft save already
// requires.
func handlePIMSeedTranslations(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodPost) {
		return
	}
	tenantID, actor, ok := pimTaskGuard(w, r, "ProductContent", "create")
	if !ok {
		return
	}
	var req struct {
		GroupID        string   `json:"group_id"`
		ItemCodes      []string `json:"item_codes"`
		SourceLanguage string   `json:"source_language"`
		TargetLanguage string   `json:"target_language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "invalid request body")
		return
	}
	if strings.TrimSpace(req.GroupID) != "" {
		role := r.Header.Get("Resolved-Role")
		allowed, err := checkPermission(tenantID, role, "PIMProductGroup", "read")
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if !allowed {
			writeAPIErrorGeneric(w, r, http.StatusForbidden, "You do not have permission to read product groups.")
			return
		}
	}
	outcomes, err := engines.BulkSeedCatalogTranslations(tenantID, req.GroupID, req.ItemCodes, req.SourceLanguage, req.TargetLanguage, actor)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(outcomes)
}

// handlePIMContentAssistShapes (36.7.1) publishes exactly the shape
// vocabulary GenerateContentSuggestion implements, so a shape picker can
// never offer one the engine will refuse.
func handlePIMContentAssistShapes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	_ = json.NewEncoder(w).Encode(engines.ListPIMContentAssistShapes())
}

// handlePIMRelatedProducts (36.7.3) returns other Active items in the same
// family that share attribute values with itemCode, most-shared first -
// gated on Item "read" like any other PIM catalog lookup.
func handlePIMRelatedProducts(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodGet) {
		return
	}
	tenantID, _, ok := pimTaskGuard(w, r, "Item", "read")
	if !ok {
		return
	}
	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	related, err := engines.FindRelatedProducts(tenantID, r.PathValue("itemCode"), limit)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(related)
}

// handlePIMGenerateBarcode (36.7.4) mints a fresh, check-digit-correct
// EAN-13 for the caller to paste into an Item's barcode field - gated on
// Item "create" (the same right needed to actually use the code), not a
// bespoke PIM permission of its own.
func handlePIMGenerateBarcode(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodPost) {
		return
	}
	tenantID, _, ok := pimTaskGuard(w, r, "Item", "create")
	if !ok {
		return
	}
	barcode, err := engines.GenerateEANBarcode(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"barcode": barcode})
}
