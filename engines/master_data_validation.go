package engines

import (
	"custom_erp/db"
	"fmt"
	"regexp"
	"strings"
)

// Stage 25 Batch 2: real business-rule validation for the Master Data
// catalog codes that have an actual field to check today. Several of the
// matrix's other Master Data rows (MASTER-0044 tax category, MASTER-0045/46
// UOM, MASTER-0047/48 MRP/cost-price, MASTER-0050 vendor PAN) describe
// fields that don't exist on Item/Vendor yet - adding those would mean
// growing the doctype's fields first, not just validating one, so they're
// left for a follow-up pass rather than guessed at here (see
// docs/micro_checklist.md Stage 25).

var gstinPattern = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$`)
var bankAccountPattern = regexp.MustCompile(`^[0-9]{9,18}$`)
var mobilePattern = regexp.MustCompile(`^[6-9][0-9]{9}$`)

// ValidateMasterDataRules checks the Master Data-specific rules (Stage 25)
// that ValidateDocument's generic metadata pass can't express - format
// checks on optional fields (GSTIN, bank account, mobile) that only fire
// when a value is actually present, plus the one cross-row check (duplicate
// barcode) that needs a query. Called once from handlers_core_doc_engine.go
// alongside the existing per-doctype checks (ValidateItemVariantUniqueness,
// ComputePurchaseOrderGST) for the same doctype.
func ValidateMasterDataRules(tenantID, docID, doctype string, payload map[string]interface{}) error {
	switch doctype {
	case "Item":
		return validateItemMasterRules(tenantID, docID, payload)
	case "Vendor":
		return validateVendorMasterRules(payload)
	case "Customer":
		return validateCustomerMasterRules(tenantID, docID, payload)
	}
	return nil
}

func strField(payload map[string]interface{}, key string) string {
	v, _ := payload[key].(string)
	return strings.TrimSpace(v)
}

func validateItemMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	hsn := strField(payload, "hsn_code")
	gstRateSet := false
	if v, exists := payload["gst_rate"]; exists && v != nil {
		if s := strings.TrimSpace(fmt.Sprintf("%v", v)); s != "" && s != "0" {
			gstRateSet = true
		}
	}

	if hsn != "" {
		// HSN/SAC codes are 4, 6, or 8 digits (GST rules) - MASTER-0043.
		if len(hsn) != 4 && len(hsn) != 6 && len(hsn) != 8 {
			for _, c := range hsn {
				if c < '0' || c > '9' {
					return &ValidationError{Code: "MASTER-0043", Message: fmt.Sprintf("HSN code %q must be numeric, 4/6/8 digits", hsn)}
				}
			}
			return &ValidationError{Code: "MASTER-0043", Message: fmt.Sprintf("HSN code %q must be 4, 6, or 8 digits", hsn)}
		}
	} else if gstRateSet {
		// MASTER-0042: HSN isn't mandatory on every Item (many are non-taxable
		// or services), but an Item with a GST rate configured is a taxable
		// good and needs one - same rule GetItemGSTInfo (engines/gst.go)
		// already enforces at sale time; this just catches it earlier, at
		// master-creation time.
		return &ValidationError{Code: "MASTER-0042", Message: "HSN Code is required when a GST rate is set on the item"}
	}

	barcode := strField(payload, "barcode")
	if barcode == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'Item' AND data->>'barcode' = $1 AND id != $2 AND status != 'Cancelled'
		LIMIT 1`, schema), barcode, docID).Scan(&existingID)
	if err == nil {
		return &ValidationError{Code: "MASTER-0053", Message: fmt.Sprintf("Barcode %q is already used by item %s", barcode, existingID)}
	}
	return nil
}

func validateVendorMasterRules(payload map[string]interface{}) error {
	if gstin := strField(payload, "gstin"); gstin != "" && !gstinPattern.MatchString(strings.ToUpper(gstin)) {
		return &ValidationError{Code: "MASTER-0049", Message: fmt.Sprintf("GSTIN %q is not a valid 15-character GSTIN", gstin)}
	}
	if acct := strField(payload, "bank_account_number"); acct != "" && !bankAccountPattern.MatchString(acct) {
		return &ValidationError{Code: "MASTER-0052", Message: fmt.Sprintf("Bank account number %q must be 9-18 digits", acct)}
	}
	return nil
}

func validateCustomerMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	phone := strField(payload, "phone")
	if phone == "" {
		return nil
	}
	if !mobilePattern.MatchString(phone) {
		return &ValidationError{Code: "MASTER-0051", Message: fmt.Sprintf("Mobile number %q is not a valid 10-digit number", phone)}
	}

	// CUSTOM-0133 (Stage 25.5): "Customer duplicate mobile" - same
	// duplicate-field query shape as MASTER-0053's Item barcode check and
	// HRPAYR-0149's Employee code check.
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'Customer' AND data->>'phone' = $1 AND id != $2 AND status != 'Cancelled'
		LIMIT 1`, schema), phone, docID).Scan(&existingID)
	if err == nil {
		return &ValidationError{Code: "CUSTOM-0133", Message: fmt.Sprintf("A customer with mobile number %q already exists (%s)", phone, existingID)}
	}
	return nil
}
