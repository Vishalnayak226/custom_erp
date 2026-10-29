package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
)

// Stage 49.5.6b (the user, 2026-10-28: "This will be avoided by making vendor
// state mandatory to save and not allow PO if state is absent"). A PO's GST
// split - IGST, or CGST + SGST - was a free switch: interstate_override kept
// whatever supply type the maker ticked over the derived one, with no reason
// and no role. Now:
//
//   - a Vendor's State is required by rule (ruleMandatoryFields), whatever the
//     field editor says. A valid GSTIN fills a blank State and a State that
//     contradicts the GSTIN is refused - both already in applyDocumentRegions,
//     which runs before the mandatory check;
//   - a PO whose vendor has no state is refused: on save, CSV import and any
//     other ValidateDocument caller (validateSupplyStateDocument), on submit
//     for approval (an approval submit check), and where an RFQ award or a
//     supply proposal drafts one;
//   - the supply type is always the derivation's (ApplyPlaceOfSupply). A
//     client's interstate / interstate_override is ignored - the override
//     column stays (additive rule) and is stored false.
//
// Vendors already saved without a state, and POs already saved, keep what
// they hold until they are saved again. 49.5.6e adds the billing store: a PO
// billed to a store with no state is refused at the same places.

// ruleMandatoryFields are required by a business rule, not only by the
// field's own Mandatory setting, so the field editor cannot make them
// optional. GetDocTypeMeta reports them mandatory, which is what the record
// form, the import's header check and ValidateDocument all read.
var ruleMandatoryFields = map[string]map[string]bool{
	"Vendor": {"state": true},
}

// POVendorNoStateCode is the refusal of a purchase order whose vendor has no
// state recorded.
const POVendorNoStateCode = "PURCHA-4961"

// POStoreNoStateCode (49.5.6e) is the refusal of a purchase order whose
// billing store has no state: neither a GSTIN or State on its legal entity nor
// a State of its own (buyerStateCode).
const POStoreNoStateCode = "PURCHA-4965"

// serverSetFields are stored by the server on every save and ignored from the
// client, so the record form neither shows nor sends them (49.5.6e: the PO's
// two supply-type ticks, which ApplyPlaceOfSupply always sets).
var serverSetFields = map[string]map[string]bool{
	"PurchaseOrder": {"interstate": true, "interstate_override": true},
}

func init() {
	RegisterApprovalSubmitCheck("PurchaseOrder", func(tenantID, docID string, data map[string]interface{}) error {
		return RequirePOStates(tenantID, data)
	})
}

// validateSupplyStateDocument is ValidateDocument's purchase order step: the
// vendor and the billing store must have a state, and the supply type is then
// derived.
func validateSupplyStateDocument(tenantID, doctype string, docData map[string]interface{}) error {
	if doctype != "PurchaseOrder" {
		return nil
	}
	if err := RequirePOStates(tenantID, docData); err != nil {
		return err
	}
	ApplyPlaceOfSupply(tenantID, docData)
	return nil
}

// RequirePOStates is both state checks a purchase order needs before its GST
// split can be worked out: the vendor's, then the billing store's.
func RequirePOStates(tenantID string, po map[string]interface{}) error {
	if err := RequirePOVendorState(tenantID, po); err != nil {
		return err
	}
	return RequirePOStoreState(tenantID, po)
}

// RequirePOStoreState (49.5.6e, user 2026-10-29: "all recommended") refuses a
// purchase order billed to a store with no state - it was saved as intra-state
// before. A blank or unknown store is left to the mandatory and link checks.
// The PO screen asks for the missing state and saves it to the store (126.2).
func RequirePOStoreState(tenantID string, po map[string]interface{}) error {
	location := strings.TrimSpace(strField(po, "location"))
	if location == "" || db.DB == nil {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var code, name string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(data->>'code', ''), COALESCE(data->>'name', '')
		  FROM %s.documents
		 WHERE doctype = 'Location' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1)
		 LIMIT 1`, schema), location).Scan(&code, &name)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	state, err := buyerStateCode(tenantID, location)
	if err != nil || state != "" {
		return err
	}
	label := firstNonEmpty(strings.TrimSpace(name), code, location)
	if c := firstNonEmpty(code, location); c != label {
		label = fmt.Sprintf("%s (%s)", label, c)
	}
	return &ValidationError{Code: POStoreNoStateCode, SubFor: "Store", Message: fmt.Sprintf(
		"Store %s has no state - add its State on the store (or a GSTIN on its legal entity), then save the purchase order again", label)}
}

// RequirePOVendorState refuses a purchase order whose vendor has neither a
// State nor a GSTIN to read one from. A blank or unknown vendor is left to the
// mandatory and link checks, which name it better.
func RequirePOVendorState(tenantID string, po map[string]interface{}) error {
	vendor := strings.TrimSpace(firstNonEmpty(strField(po, "vendor"), strField(po, "vendor_id")))
	if vendor == "" || db.DB == nil {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var code, name, gstin, state string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(data->>'code', ''), COALESCE(data->>'name', ''), COALESCE(data->>'gstin', ''), COALESCE(data->>'state', '')
		  FROM %s.documents
		 WHERE doctype = 'Vendor' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1)
		 ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END
		 LIMIT 1`, schema), vendor).Scan(&code, &name, &gstin, &state)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(state) != "" || StateCodeFromGSTIN(gstin) != "" {
		return nil
	}
	label := firstNonEmpty(strings.TrimSpace(name), code, vendor)
	if c := firstNonEmpty(code, vendor); c != label {
		label = fmt.Sprintf("%s (%s)", label, c)
	}
	return &ValidationError{Code: POVendorNoStateCode, SubFor: "Vendor", Message: fmt.Sprintf(
		"Vendor %s has no state - add its State (or GSTIN) on the vendor, then save the purchase order again", label)}
}
