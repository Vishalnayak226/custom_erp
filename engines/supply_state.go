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
// they hold until they are saved again.

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

func init() {
	RegisterApprovalSubmitCheck("PurchaseOrder", func(tenantID, docID string, data map[string]interface{}) error {
		return RequirePOVendorState(tenantID, data)
	})
}

// validateSupplyStateDocument is ValidateDocument's purchase order step: the
// vendor must have a state, and the supply type is then derived.
func validateSupplyStateDocument(tenantID, doctype string, docData map[string]interface{}) error {
	if doctype != "PurchaseOrder" {
		return nil
	}
	if err := RequirePOVendorState(tenantID, docData); err != nil {
		return err
	}
	ApplyPlaceOfSupply(tenantID, docData)
	return nil
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
