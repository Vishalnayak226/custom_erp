package engines

import (
	"custom_erp/db"
	"encoding/json"
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
	case "ProductContent":
		return validateProductContentDuplicate(tenantID, docID, payload)
	case "Batch":
		return validateBatchMasterRules(tenantID, docID, payload)
	}
	return nil
}

// validateItemTrackingFields (Stage 42.1.1) checks the traceability field
// group. All four fields are optional, so every check below is conditional on
// the tenant having typed something - an item that never opts in is validated
// exactly as it was before Stage 42.
//
// The rules exist because each of these numbers silently drives a gate that is
// hard to debug from its symptom. A negative shelf life derives an expiry in the
// past and quarantines a lot the moment it is received; a pick minimum larger
// than the shelf life itself makes the item permanently unpickable - stock
// arrives, is never allocatable, and nothing in the pick list says why. Both are
// far cheaper to refuse here than to diagnose on a warehouse floor.
func validateItemTrackingFields(payload map[string]interface{}) error {
	mode := strField(payload, "tracking_mode")
	switch mode {
	case "", TrackingNone, TrackingBatch, TrackingSerial, TrackingBatchAndSerial:
	default:
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Traceability", Message: fmt.Sprintf(
			"traceability %q is not recognised - expected one of %s, %s, %s or %s",
			mode, TrackingNone, TrackingBatch, TrackingSerial, TrackingBatchAndSerial)}
	}

	shelfLife := int(numFromInterface(payload["shelf_life_days"]))
	onReceipt := int(numFromInterface(payload["min_shelf_life_on_receipt_days"]))
	onPick := int(numFromInterface(payload["min_shelf_life_on_pick_days"]))

	for _, f := range []struct {
		label string
		value int
	}{
		{"Shelf Life (days)", shelfLife},
		{"Min Shelf Life on Receipt (days)", onReceipt},
		{"Min Shelf Life on Pick (days)", onPick},
	} {
		if f.value < 0 {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: f.label,
				Message: fmt.Sprintf("%s cannot be negative", f.label)}
		}
	}

	if shelfLife > 0 && onReceipt > shelfLife {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Min Shelf Life on Receipt (days)", Message: fmt.Sprintf(
			"a %d-day minimum on receipt is longer than the item's own %d-day shelf life, so no delivery could ever be accepted",
			onReceipt, shelfLife)}
	}
	if shelfLife > 0 && onPick > shelfLife {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Min Shelf Life on Pick (days)", Message: fmt.Sprintf(
			"a %d-day minimum on pick is longer than the item's own %d-day shelf life, so no stock could ever be allocated",
			onPick, shelfLife)}
	}
	return nil
}

// validateBatchMasterRules (Stage 42.1.2) enforces the three things a Batch's
// generic metadata pass cannot express.
//
// The uniqueness rule is the important one, and it is why batch_no is a field
// rather than the document id: documents.id is the primary key across EVERY
// doctype in this schema, but a batch number is only unique within its item -
// two suppliers both shipping "LOT-001" of different SKUs is completely normal,
// and keying on batch_no alone would make the second one unsaveable with a raw
// primary-key violation instead of a sentence. So the pair is checked here,
// where it can produce a real message.
func validateBatchMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	batchNo := strField(payload, "batch_no")
	item := strField(payload, "item")
	if batchNo == "" || item == "" {
		// Both are mandatory; ValidateDocument has already said so more
		// precisely than this function could.
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	// The item must exist, by CODE. See the migration's note on why `item` is
	// not a Link field: the generic Link check resolves against documents.id,
	// and an Item's id is not its code in this tree.
	var itemExists bool
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Item' AND data->>'code' = $1 AND deleted_at IS NULL)`, schema),
		item).Scan(&itemExists); err != nil {
		return err
	}
	if !itemExists {
		return &ValidationError{Code: "META-0198", SubFor: "Item Code (SKU)",
			Message: fmt.Sprintf("no item with code %q exists - a batch must belong to a real item", item)}
	}

	// (item, batch_no) uniqueness.
	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'Batch' AND data->>'item' = $1 AND data->>'batch_no' = $2 AND id != $3
		LIMIT 1`, schema), item, batchNo, docID).Scan(&existingID)
	if err == nil {
		return &ValidationError{Code: "MASTER-0053", Message: fmt.Sprintf(
			"batch %q already exists for item %s (%s) - a lot number must be unique within its item", batchNo, item, existingID)}
	}

	// Expiry must follow manufacture. A lot dated to expire before it was made
	// is always a typo, and letting it save means FEFO allocates it first,
	// forever - the single most damaging bad row this master can hold.
	mfg, hasMfg := parseTraceDate(strField(payload, "mfg_date"))
	expiry, hasExpiry := parseTraceDate(strField(payload, "expiry_date"))
	if hasMfg && hasExpiry && !expiry.After(mfg) {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Expiry Date", Message: fmt.Sprintf(
			"expiry date %s is not after the manufacture date %s", expiry.Format(isoDate), mfg.Format(isoDate))}
	}

	// Lottable attributes are JSON by contract (Infor §16 validates customer
	// constraints against them). Storing an unparseable string would make every
	// later constraint check silently pass, which is worse than refusing it now.
	if raw := strField(payload, "attributes"); raw != "" {
		var probe map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &probe); err != nil {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Lottable Attributes JSON",
				Message: "lottable attributes must be a JSON object, e.g. {\"country_of_origin\": \"IN\", \"grade\": \"A\"}"}
		}
	}
	return nil
}

func strField(payload map[string]interface{}, key string) string {
	v, _ := payload[key].(string)
	return strings.TrimSpace(v)
}

func validateItemMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	hsn := strField(payload, "hsn_code")
	gstRatePositive := false
	if v, exists := payload["gst_rate"]; exists && v != nil {
		if s := strings.TrimSpace(fmt.Sprintf("%v", v)); s != "" {
			var rate float64
			if _, err := fmt.Sscanf(s, "%f", &rate); err == nil && rate > 0 {
				gstRatePositive = true
			}
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
	} else {
		// MASTER-0042. Until Stage 30.1.2 this only fired when a GST rate was
		// also set, on the reasoning that some items are non-taxable - but
		// GetItemGSTInfo (engines/gst.go) rejects an HSN-less item at BOTH
		// checkout and PO creation regardless, so "HSN optional" only ever
		// meant "saveable now, unusable later". The two layers now agree:
		// what the master accepts is exactly what a transaction accepts.
		//
		// Stage 26.6.11 kept this unconditional across all four tax
		// treatments: HSN belongs on the invoice whatever the rate is, and
		// GSTR-1 reports the nil/exempt table HSN-wise too, so an exempt item
		// needs one exactly as much as a taxable one does.
		return &ValidationError{Code: "MASTER-0042", Message: "HSN Code is required on every item - both POS checkout and Purchase Order creation reject an item without one"}
	}

	// Stage 26.6.11. Stage 30.1.2 required a positive gst_rate on every item,
	// which made genuinely untaxed goods - unbranded grain, fresh produce,
	// books, exports - unsaveable, so a tenant selling them could not create
	// the Item at all. The fix is not to allow a bare 0: a 0 is
	// indistinguishable from "not filled in yet", which is the hole 30.1.2
	// closed. The item declares its treatment instead, and 0 becomes valid -
	// mandatory, in fact - only once that declaration exists.
	treatment, treatmentOK := NormalizeTaxTreatment(strField(payload, "tax_treatment"))
	if !treatmentOK {
		return &ValidationError{Code: "MASTER-0044", Message: fmt.Sprintf("Tax Treatment %q is not recognized - expected one of %s, %s, %s or %s", strField(payload, "tax_treatment"), TaxTreatmentTaxable, TaxTreatmentExempt, TaxTreatmentNilRated, TaxTreatmentZeroRated)}
	}

	if IsTaxableTreatment(treatment) {
		if !gstRatePositive {
			// MASTER-0044 ("Tax category is required for this item"), not
			// MASTER-0042: this is no longer an HSN problem - which is what
			// MASTER-0042's catalog headline says - but a statement that the
			// item has not said how it is taxed. The detail below names the
			// way out, since a user hitting this on genuinely untaxed goods
			// would otherwise be stuck typing rates that get rejected.
			return &ValidationError{Code: "MASTER-0044", Message: fmt.Sprintf("GST Rate (%%) must be greater than zero for a %s item - both POS checkout and Purchase Order creation reject an item without one. If this item is genuinely untaxed, set Tax Treatment to %s, %s or %s instead of entering a 0 rate", TaxTreatmentTaxable, TaxTreatmentExempt, TaxTreatmentNilRated, TaxTreatmentZeroRated)}
		}
	} else if gstRatePositive {
		// The mirror check, and the reason the treatment is trustworthy
		// downstream: a non-taxable item can never carry a rate, so
		// GetItemTaxInfo can return 0 for one without having to reconcile two
		// contradictory fields, and the returns cannot report turnover as
		// exempt while the till charged tax on it.
		return &ValidationError{Code: "MASTER-0044", Message: fmt.Sprintf("A %s item cannot carry a positive GST Rate (%%) - set the rate to 0, or change Tax Treatment to %s", treatment, TaxTreatmentTaxable)}
	}

	// Stage 42.1.1: the traceability field group. Every check here only fires
	// on a value the tenant actually typed, so an item that never opts into
	// tracking is validated exactly as it was before Stage 42.
	if err := validateItemTrackingFields(payload); err != nil {
		return err
	}

	barcode := strField(payload, "barcode")
	if barcode != "" {
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
	}

	// Stage 26.4.2: duplicate-item detection. A near-duplicate catalog entry
	// (same name re-created under the same family) is a common PIM data-
	// quality problem the barcode check above doesn't catch, since two
	// distinct items can each get their own valid barcode. Scoped to family
	// (not global) so "Blue Cotton Shirt" in Clothing and an unrelated
	// "Blue Cotton Shirt" cleaning rag in a different family don't collide.
	family := strField(payload, "family")
	name := strField(payload, "name")
	if family == "" || name == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'Item' AND data->>'family' = $1 AND LOWER(BTRIM(data->>'name')) = LOWER(BTRIM($2)) AND id != $3 AND status != 'Cancelled'
		LIMIT 1`, schema), family, name, docID).Scan(&existingID)
	if err == nil {
		return fmt.Errorf("an item named %q already exists in family %q: %s", name, family, existingID)
	}
	return nil
}

// validateProductContentDuplicate (Stage 26.4.2) blocks two different
// products from carrying identical ProductContent titles in the same
// language - a common copy-paste mistake that hurts channel/SEO quality
// (duplicate listings). Scoped to the same language only, excluding the
// content's own product (an item can obviously keep its own title across
// edits) and excluding Rejected content (a rejected duplicate shouldn't
// block a legitimate rewrite).
func validateProductContentDuplicate(tenantID, docID string, payload map[string]interface{}) error {
	title := strField(payload, "title")
	language := strField(payload, "language")
	productID := strField(payload, "product_id")
	if title == "" || language == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var existingID, existingProduct string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'product_id', '') FROM %s.documents
		WHERE doctype = 'ProductContent' AND data->>'language' = $1 AND LOWER(BTRIM(data->>'title')) = LOWER(BTRIM($2))
			AND id != $3 AND COALESCE(data->>'product_id', '') != $4 AND status != 'Rejected'
		LIMIT 1`, schema), language, title, docID, productID).Scan(&existingID, &existingProduct)
	if err == nil {
		return fmt.Errorf("content title %q for language %q is already used by product %s (%s)", title, language, existingProduct, existingID)
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

// validateCustomerMasterRules enforces MASTER-0051 against the tenant's
// configured home country (Stage 41) rather than the India-only
// `^[6-9][0-9]{9}$` it used before.
//
// By the time this runs, ValidateDocument has already cleaned the number
// through NormalizeDocumentPhones - so `phone` here is digits-only with any
// dial code and trunk prefix already resolved, and what is compared against
// the country's length is the same value that will be stored. That ordering
// is what makes this both stricter (a wrong-length number is still refused)
// and far more forgiving (a correct number written "+91 98765-43210" now
// saves, where before it was rejected outright).
//
// A number that carries another country's dial code explicitly is accepted on
// its own country's rule - a real business has foreign customers, and this is
// a customer master, not a domestic-only list. The country is recorded on the
// record so it stays a usable targeting signal.
func validateCustomerMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	phone := strField(payload, "phone")
	if phone == "" {
		return nil
	}
	// Which country's rule to hold this number to is decided by
	// NormalizeDocumentPhones, which ran first (from ValidateDocument) and is
	// the only pass that could still see an explicit "+1"/"+971" before it was
	// stripped. Re-deriving it here from the already-cleaned digits would
	// silently retag every foreign customer as domestic - a 10-digit US number
	// and a 10-digit Indian one are indistinguishable once the dial code is
	// gone. So: trust the stamp when there is one, fall back to the tenant's
	// home country when there isn't.
	country := strField(payload, "phone_country")
	if country == "" {
		country = TenantPhoneCountry(tenantID)
	}
	parsed := NormalizePhone(phone, country)
	if !parsed.Valid {
		return &ValidationError{Code: "MASTER-0051", Message: parsed.Reason}
	}
	payload["phone"] = parsed.National
	if parsed.CountryISO2 != "" {
		payload["phone_country"] = parsed.CountryISO2
	}
	// The duplicate check below compares the CLEANED number, which is the
	// other half of the fix: before this, "9876543210" and "+91 98765 43210"
	// were two different strings and the same customer could be created twice.
	phone = parsed.National

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
