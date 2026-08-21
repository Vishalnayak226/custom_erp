package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
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
	case "SerialNumber":
		return validateSerialMasterRules(tenantID, docID, payload)
	case "LottableConstraint":
		return validateLottableConstraintMasterRules(tenantID, docID, payload)
	case "UOMConversion":
		return validateUOMConversionMasterRules(tenantID, docID, payload)
	case "TaskDispatchStrategy":
		return validateTaskDispatchStrategyMasterRules(tenantID, docID, payload)
	case "Zone":
		return validateZoneMasterRules(tenantID, docID, payload)
	case "Bin":
		return validateBinMasterRules(tenantID, docID, payload)
	case "PutawayStrategy":
		return validatePutawayStrategyMasterRules(tenantID, docID, payload)
	case "AllocationStrategy":
		return validateAllocationStrategyMasterRules(tenantID, docID, payload)
	case "DockDoor":
		return validateDockDoorMasterRules(tenantID, docID, payload)
	case "Appointment":
		return validateAppointmentMasterRules(tenantID, docID, payload)
	case "YardCheckIn":
		return validateYardCheckInMasterRules(tenantID, docID, payload)
	case "ReceiptValidationRule":
		return validateReceiptValidationRuleMasterRules(tenantID, docID, payload)
	case "Hold":
		return validateHoldMasterRules(tenantID, docID, payload)
	case "WaveTemplate":
		return validateWaveTemplateMasterRules(tenantID, docID, payload)
	case "Wave":
		return validateWaveMasterRules(tenantID, docID, payload)
	case "SortStation":
		return validateSortStationMasterRules(tenantID, docID, payload)
	case "SortSlot":
		return validateSortSlotMasterRules(tenantID, docID, payload)
	case "PackTemplate":
		return validatePackTemplateMasterRules(tenantID, docID, payload)
	case "PackingValidationTemplate":
		return validatePackingValidationTemplateMasterRules(tenantID, docID, payload)
	case "LoadingTask":
		return validateLoadingTaskMasterRules(tenantID, docID, payload)
	case "PreShipValidationRule":
		return validatePreShipValidationRuleMasterRules(tenantID, docID, payload)
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

// validateSerialMasterRules (Stage 42.1.8) enforces the two things a
// SerialNumber's generic metadata pass cannot express - the serial analogue
// of validateBatchMasterRules, same reasoning throughout: (item, serial_no)
// uniqueness cannot be the document id because a serial number is only
// unique within its item, so it is checked here where it can produce a real
// message instead of a primary-key violation.
func validateSerialMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	serialNo := strField(payload, "serial_no")
	item := strField(payload, "item")
	if serialNo == "" || item == "" {
		// Both are mandatory; ValidateDocument has already said so.
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	var itemExists bool
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Item' AND data->>'code' = $1 AND deleted_at IS NULL)`, schema),
		item).Scan(&itemExists); err != nil {
		return err
	}
	if !itemExists {
		return &ValidationError{Code: "META-0198", SubFor: "Item Code (SKU)",
			Message: fmt.Sprintf("no item with code %q exists - a serial number must belong to a real item", item)}
	}

	if batchNo := strField(payload, "batch_no"); batchNo != "" {
		var batchExists bool
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Batch' AND data->>'item' = $1 AND data->>'batch_no' = $2)`, schema),
			item, batchNo).Scan(&batchExists); err != nil {
			return err
		}
		if !batchExists {
			return &ValidationError{Code: "META-0198", SubFor: "Batch / Lot No (optional)",
				Message: fmt.Sprintf("no batch %q is registered for item %s - register the batch before assigning a serial to it", batchNo, item)}
		}
	}

	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'SerialNumber' AND data->>'item' = $1 AND data->>'serial_no' = $2 AND id != $3
		LIMIT 1`, schema), item, serialNo, docID).Scan(&existingID)
	if err == nil {
		return &ValidationError{Code: "MASTER-0053", Message: fmt.Sprintf(
			"serial %q already exists for item %s (%s) - a serial number must be unique within its item", serialNo, item, existingID)}
	}
	return nil
}

// validateLottableConstraintMasterRules (Stage 42.1.7) enforces the two
// things a LottableConstraint's generic metadata pass cannot express: that
// allowed_values actually names at least one value, and that (customer,
// item, attribute_key) is unique. item is treated as part of that key even
// when blank, so a tenant can have one wildcard ("applies to every item this
// customer buys") row per attribute_key alongside SKU-specific overrides,
// without the wildcard and an override silently colliding.
func validateLottableConstraintMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	customer := strField(payload, "customer")
	attributeKey := strField(payload, "attribute_key")
	if customer == "" || attributeKey == "" {
		// Both are mandatory; ValidateDocument has already said so.
		return nil
	}
	item := strField(payload, "item")

	hasValue := false
	for _, v := range strings.Split(strField(payload, "allowed_values"), ",") {
		if strings.TrimSpace(v) != "" {
			hasValue = true
			break
		}
	}
	if !hasValue {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Allowed Values",
			Message: "allowed values must name at least one value, e.g. \"IN,US\""}
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'LottableConstraint' AND data->>'customer' = $1
		  AND COALESCE(data->>'item', '') = $2 AND data->>'attribute_key' = $3 AND id != $4
		LIMIT 1`, schema), customer, item, attributeKey, docID).Scan(&existingID)
	if err == nil {
		scope := "every item"
		if item != "" {
			scope = "item " + item
		}
		return &ValidationError{Code: "MASTER-0053", Message: fmt.Sprintf(
			"a lottable constraint on %q already exists for customer %s / %s (%s)", attributeKey, customer, scope, existingID)}
	}
	return nil
}

func strField(payload map[string]interface{}, key string) string {
	v, _ := payload[key].(string)
	return strings.TrimSpace(v)
}

// itemCodePattern is the SKU charset tourniquet from the 2026-07-31
// durability audit (finding 6): public/app.js interpolates `item.code` into
// innerHTML and into onclick="...('${sku}')" attributes at ~283 sites with no
// escaping, so an unescaped SKU is a stored-XSS vector that fires in every
// viewer's browser, not just the item's creator. Rejecting the HTML/JS
// metacharacters a SKU never legitimately needs closes that at the one place
// a SKU is minted, without waiting on the larger innerHTML-escaping refactor.
var itemCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._\-\/]*$`)

func validateItemMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	if code := strField(payload, "code"); code != "" && !itemCodePattern.MatchString(code) {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Item Code (SKU)", Message: fmt.Sprintf(
			"Item Code (SKU) %q may only contain letters, numbers, spaces, and . _ - / - no HTML or script characters", code)}
	}

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

// validateZoneMasterRules (Stage 42.2.5) enforces the one thing Zone's
// generic metadata pass cannot express: `code` uniqueness among Active
// zones. Two Active zones sharing a code would make validateBinMasterRules'
// own existence check (below) ambiguous about which zone a Bin actually
// means.
func validateZoneMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	code := strField(payload, "code")
	if code == "" {
		return nil // mandatory; ValidateDocument has already said so.
	}
	status := strField(payload, "status")
	if status != "Active" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'Zone' AND status = 'Active' AND data->>'code' = $1 AND id != $2
		LIMIT 1`, schema), code, docID).Scan(&existingID)
	if err == nil {
		return &ValidationError{Code: "MASTER-0053", Message: fmt.Sprintf(
			"an Active zone %q already exists (%s) - zone codes must be unique", code, existingID)}
	}
	return nil
}

// validateBinMasterRules (Stage 42.2.5) is new with this item - Bin had no
// dedicated rules function before. Its only job is the zone-code existence
// check the migration's own note explains: `zone` stays a Data field
// holding the code a person types, so referential integrity is enforced
// here against Zone.data->>'code' rather than by the generic Link check
// (which would require the value to be a Zone document id instead). A blank
// zone is always allowed - not every warehouse zones its bins, and this
// must not become a mandatory field retroactively.
func validateBinMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	zone := strField(payload, "zone")
	if zone == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var zoneExists bool
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Zone' AND data->>'code' = $1 AND status = 'Active')`, schema),
		zone).Scan(&zoneExists); err != nil {
		return err
	}
	if !zoneExists {
		return &ValidationError{Code: "META-0198", SubFor: "Zone",
			Message: fmt.Sprintf("no Active zone with code %q exists - create it on the Zone master first", zone)}
	}
	return nil
}

// validatePutawayStrategyMasterRules (Stage 42.2.7) mirrors
// validateTaskDispatchStrategyMasterRules exactly: `criteria` names only
// real signals SuggestPutawayBin knows how to weigh, and at most one Active
// strategy applies to any given location_code (including the blank
// "everywhere" value).
func validatePutawayStrategyMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	criteria := strField(payload, "criteria")
	for _, tok := range strings.Split(criteria, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if _, ok := putawayCriteriaWeights[tok]; !ok {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Criteria", Message: fmt.Sprintf(
				"%q is not a recognised putaway criterion - expected velocity, zone_sequence, capacity, hazmat_temp or batch_consolidation", tok)}
		}
	}
	status := strField(payload, "status")
	if status != "Active" {
		return nil
	}
	locationCode := strField(payload, "location_code")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'PutawayStrategy' AND status = 'Active'
		  AND COALESCE(data->>'location_code', '') = $1 AND id != $2
		LIMIT 1`, schema), locationCode, docID).Scan(&existingID)
	if err == nil {
		scope := "every location"
		if locationCode != "" {
			scope = "location " + locationCode
		}
		return &ValidationError{Code: "MASTER-0053", Message: fmt.Sprintf(
			"an Active putaway strategy already applies to %s (%s) - deactivate it first", scope, existingID)}
	}
	return nil
}

// validateAllocationStrategyMasterRules (Stage 42.2.8) mirrors the same two
// checks: `strategy` must be a recognised token, and at most one Active row
// may apply to any given item value (including blank, the "every
// non-batch-tracked item" default).
func validateAllocationStrategyMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	strategy := strField(payload, "strategy")
	if strategy != "" {
		if _, ok := allocationOrderFragments[strategy]; !ok {
			return &ValidationError{Code: "META-0199", SubFor: "Strategy", Message: fmt.Sprintf(
				"%q is not a recognised allocation strategy - expected FIFO, LIFO, NearestBin, FewestPicks or CleanLocation", strategy)}
		}
	}
	status := strField(payload, "status")
	if status != "Active" {
		return nil
	}
	item := strField(payload, "item")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'AllocationStrategy' AND status = 'Active'
		  AND COALESCE(data->>'item', '') = $1 AND id != $2
		LIMIT 1`, schema), item, docID).Scan(&existingID)
	if err == nil {
		scope := "every non-batch-tracked item"
		if item != "" {
			scope = "item " + item
		}
		return &ValidationError{Code: "MASTER-0053", Message: fmt.Sprintf(
			"an Active allocation strategy already applies to %s (%s) - deactivate it first", scope, existingID)}
	}
	return nil
}

// validateDockDoorMasterRules (Stage 42.3.1) checks the service window is a
// well-formed HH:MM pair (start before end - a door open midnight-to-06:00
// isn't representable this way, an acceptable gap for a first pass, see
// docs/micro_checklist.md 42.3.1) and that an Active door's code is unique,
// the same MASTER-0053 shape Zone/PutawayStrategy/AllocationStrategy use.
func validateDockDoorMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	start := strField(payload, "service_window_start")
	end := strField(payload, "service_window_end")
	if start != "" || end != "" {
		startT, err := time.Parse("15:04", start)
		if err != nil {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Service Window Start", Message: "must be in HH:MM 24-hour format"}
		}
		endT, err := time.Parse("15:04", end)
		if err != nil {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Service Window End", Message: "must be in HH:MM 24-hour format"}
		}
		if !startT.Before(endT) {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Service Window", Message: "service window start must be before end"}
		}
	}
	code := strField(payload, "code")
	if code == "" || strField(payload, "status") != "Active" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'DockDoor' AND status = 'Active' AND data->>'code' = $1 AND id != $2
		LIMIT 1`, schema), code, docID).Scan(&existingID)
	if err == nil {
		return &ValidationError{Code: "MASTER-0053", Message: fmt.Sprintf(
			"an Active dock door %q already exists (%s) - door codes must be unique", code, existingID)}
	}
	return nil
}

// validateAppointmentMasterRules (Stage 42.3.2) is the scheduling gate: the
// dock door must exist and be Active, the appointment's direction must be
// one the door actually serves (a "Both" door serves either), start must be
// before end, and the door's max_concurrent_appointments (default 1) caps
// how many non-cancelled appointments may overlap it on the same date - the
// literal "scheduling validated against door capacity" from the plan.
// Overlap is computed as ordinary half-open interval overlap
// (existing.start < new.end AND existing.end > new.start) across every
// Appointment row on the same door/date whose status isn't Cancelled or
// NoShow, docID excluded so re-saving an unchanged appointment never counts
// itself twice.
func validateAppointmentMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	dockDoor := strField(payload, "dock_door")
	apptType := strField(payload, "appointment_type")
	startStr := strField(payload, "start_time")
	endStr := strField(payload, "end_time")
	date := strField(payload, "appointment_date")
	status := strField(payload, "status")

	startT, err := time.Parse("15:04", startStr)
	if err != nil {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Start Time", Message: "must be in HH:MM 24-hour format"}
	}
	endT, err := time.Parse("15:04", endStr)
	if err != nil {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "End Time", Message: "must be in HH:MM 24-hour format"}
	}
	if !startT.Before(endT) {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Appointment Time", Message: "start time must be before end time"}
	}
	if dockDoor == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var doorType string
	var maxConcurrent sql.NullInt64
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data->>'door_type', NULLIF(data->>'max_concurrent_appointments', '')::int
		FROM %s.documents WHERE doctype = 'DockDoor' AND data->>'code' = $1 AND status = 'Active'`, schema),
		dockDoor).Scan(&doorType, &maxConcurrent)
	if err == sql.ErrNoRows {
		return &ValidationError{Code: "META-0198", SubFor: "Dock Door",
			Message: fmt.Sprintf("no Active dock door %q exists - create it on the DockDoor master first", dockDoor)}
	} else if err != nil {
		return err
	}
	if doorType != "Both" && apptType != "" && doorType != apptType {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Appointment Type", Message: fmt.Sprintf(
			"dock door %q only serves %s appointments", dockDoor, doorType)}
	}
	capacity := 1
	if maxConcurrent.Valid && maxConcurrent.Int64 > 0 {
		capacity = int(maxConcurrent.Int64)
	}
	if status == "Cancelled" || status == "NoShow" {
		return nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'start_time', data->>'end_time' FROM %s.documents
		WHERE doctype = 'Appointment' AND id != $1
		  AND data->>'dock_door' = $2 AND data->>'appointment_date' = $3
		  AND COALESCE(data->>'status', '') NOT IN ('Cancelled', 'NoShow')`, schema),
		docID, dockDoor, date)
	if err != nil {
		return err
	}
	defer rows.Close()
	overlapping := 0
	for rows.Next() {
		var existStart, existEnd string
		if err := rows.Scan(&existStart, &existEnd); err != nil {
			return err
		}
		existStartT, err1 := time.Parse("15:04", existStart)
		existEndT, err2 := time.Parse("15:04", existEnd)
		if err1 != nil || err2 != nil {
			continue
		}
		if existStartT.Before(endT) && existEndT.After(startT) {
			overlapping++
		}
	}
	if overlapping+1 > capacity {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Appointment Time", Message: fmt.Sprintf(
			"dock door %q already has %d appointment(s) overlapping %s-%s on %s (capacity %d)",
			dockDoor, overlapping, startStr, endStr, date, capacity)}
	}
	return nil
}

// validateYardCheckInMasterRules (Stage 42.3.4) enforces the one-way status
// order InYard -> AtDoor -> Departed (never back), and that checked_out_at
// is filled exactly when a save moves status to Departed - the frontend's
// "Check Out" action fills it before saving (public/app.js), this rejects a
// hand-crafted request that sets Departed without it.
func validateYardCheckInMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	newStatus := strField(payload, "status")
	if newStatus == "Departed" && strField(payload, "checked_out_at") == "" {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Checked Out At",
			Message: "checked_out_at is required when status is set to Departed"}
	}
	if docID == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var priorStatus string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'YardCheckIn' AND id = $1`, schema), docID).Scan(&priorStatus); err != nil {
		return nil // create path, nothing to compare against
	}
	order := map[string]int{"InYard": 0, "AtDoor": 1, "Departed": 2}
	if priorStatus != "" && newStatus != "" && order[newStatus] < order[priorStatus] {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Status", Message: fmt.Sprintf(
			"a yard check-in cannot move backward from %s to %s", priorStatus, newStatus)}
	}
	return nil
}

// validateHoldMasterRules (Stage 42.3.5) is defense-in-depth against exactly
// the gap role_permissions.allow_update=FALSE alone does not close: HR/Admin
// bypasses the generic doc endpoint's RBAC check entirely (the same
// systemwide super-admin override DecideApproval's own location check
// documents), so without this a Hold could still be released by a bare
// HR/Admin edit through the generic API, skipping HoldReleaseRequest's
// maker-checker gate completely. Unlike role_permissions, a doctype
// validator runs for every caller including HR/Admin - there is no bypass
// list here. ReleaseHold (engines/wms_holds.go) writes its own Active ->
// Released transition with a direct SQL UPDATE that never reaches
// ValidateMasterDataRules at all, so this can never block the legitimate
// path, only a generic-endpoint one.
func validateHoldMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	newStatus := strField(payload, "status")
	if docID == "" || newStatus != "Released" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var priorStatus string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'Hold' AND id = $1`, schema), docID).Scan(&priorStatus); err != nil {
		return nil // create path, nothing to compare against
	}
	if priorStatus == "Active" {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Status",
			Message: "a Hold can only be released by submitting and approving a HoldReleaseRequest, not by editing it directly"}
	}
	return nil
}

// validateReceiptValidationRuleMasterRules (Stage 42.3.6) caps tolerance to
// a sane 0-100% and enforces the same "at most one Active row per scope"
// shape PutawayStrategy/AllocationStrategy already use - one Active rule per
// vendor (including blank, the tenant-wide default), so
// getReceiptValidationRule never has to pick between two conflicting
// Active rows for the same scope.
func validateReceiptValidationRuleMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	tolerance := numFromInterface(payload["over_receipt_tolerance_pct"])
	if tolerance < 0 || tolerance > 100 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Over-Receipt Tolerance %", Message: "must be between 0 and 100"}
	}
	if strField(payload, "status") != "Active" {
		return nil
	}
	vendor := strField(payload, "vendor")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'ReceiptValidationRule' AND status = 'Active'
		  AND COALESCE(data->>'vendor', '') = $1 AND id != $2
		LIMIT 1`, schema), vendor, docID).Scan(&existingID)
	if err == nil {
		scope := "the tenant default"
		if vendor != "" {
			scope = "vendor " + vendor
		}
		return &ValidationError{Code: "MASTER-0053", Message: fmt.Sprintf(
			"an Active receipt validation rule already applies to %s (%s) - deactivate it first", scope, existingID)}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Stage 42.4 - Outbound depth.
// ---------------------------------------------------------------------------

// validateWaveTemplateMasterRules (42.4.1) checks the two HH:MM time fields
// and enforces at most one Active template per location_code (including
// blank, the tenant-wide default) - the same "at most one Active row per
// scope" shape TaskDispatchStrategy/ReceiptValidationRule already use, so
// engines/wms_wave.go's auto-run scan never has to arbitrate between two
// conflicting Active templates for the same location.
func validateWaveTemplateMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	for _, f := range []string{"cutoff_time", "run_daily_at"} {
		if v := strField(payload, f); v != "" {
			if _, err := time.Parse("15:04", v); err != nil {
				return &ValidationError{Code: "GLOBAL-0002", SubFor: f, Message: "must be in HH:MM 24-hour format"}
			}
		}
	}
	if strField(payload, "status") != "Active" {
		return nil
	}
	locationCode := strField(payload, "location_code")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'WaveTemplate' AND status = 'Active'
		  AND COALESCE(data->>'location_code', '') = $1 AND id != $2
		LIMIT 1`, schema), locationCode, docID).Scan(&existingID)
	if err == nil {
		scope := "every location"
		if locationCode != "" {
			scope = "location " + locationCode
		}
		return &ValidationError{Code: "MASTER-0053", Message: fmt.Sprintf(
			"an Active wave template already applies to %s (%s) - deactivate it first", scope, existingID)}
	}
	return nil
}

// waveStatusOrder is Wave's one-way lifecycle (42.4.2), same shape as
// warehouseTaskTerminal/the Hold Active->Released guard: engines/
// wms_wave.go's TransitionWaveStatus is the only path that should legally
// advance a Wave, and this validator is what stops the generic doc-save
// endpoint from being used to skip a step or move a Wave backward, the exact
// bypass class 42.3's Hold entry documented and fixed the same way.
var waveStatusOrder = map[string]int{"Planned": 0, "Released": 1, "In Progress": 2, "Complete": 3, "Closed": 4}

func validateWaveMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	newStatus := strField(payload, "status")
	if docID == "" || newStatus == "" {
		return nil // create path, nothing to compare against
	}
	newRank, ok := waveStatusOrder[newStatus]
	if !ok {
		return nil // an unrecognised value; the Select fieldtype check already refuses it
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var priorStatus string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'Wave' AND id = $1`, schema), docID).Scan(&priorStatus); err != nil {
		return nil
	}
	priorRank, ok := waveStatusOrder[priorStatus]
	if !ok || newRank == priorRank {
		return nil
	}
	if newRank != priorRank+1 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Status", Message: fmt.Sprintf(
			"a Wave can only move forward one step at a time (%s -> %s is not a valid transition)", priorStatus, newStatus)}
	}
	return nil
}

// validateSortStationMasterRules (42.4.3) is the same zone-code-existence
// check validateBinMasterRules already runs for Bin, applied to
// SortStation's own optional zone field.
func validateSortStationMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	zone := strField(payload, "zone")
	if zone == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var exists bool
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Zone' AND data->>'code' = $1)`, schema), zone).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return &ValidationError{Code: "META-0198", SubFor: "Zone", Message: fmt.Sprintf("no Zone %q exists - create it on the Zone master first", zone)}
	}
	return nil
}

// validateSortSlotMasterRules (42.4.3) confirms the station exists and is
// Active, and that slot_no falls within the station's own num_slots -
// nothing else meaningfully validates a put-wall slot, since which order/sku
// a slot currently holds is exactly what AssignSortSlot/ConfirmSortSlot
// (engines/wms_sortation.go) manage through their own choke points.
func validateSortSlotMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	station := strField(payload, "station")
	if station == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var numSlots int
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(NULLIF(data->>'num_slots', '')::int, 0) FROM %s.documents WHERE doctype = 'SortStation' AND data->>'code' = $1 AND status = 'Active'`, schema),
		station).Scan(&numSlots)
	if err == sql.ErrNoRows {
		return &ValidationError{Code: "META-0198", SubFor: "Sort Station", Message: fmt.Sprintf("no Active sort station %q exists", station)}
	} else if err != nil {
		return err
	}
	slotNo := int(numFromInterface(payload["slot_no"]))
	if numSlots > 0 && (slotNo < 1 || slotNo > numSlots) {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Slot Number", Message: fmt.Sprintf(
			"slot %d is outside station %q's configured range of 1-%d", slotNo, station, numSlots)}
	}
	_ = docID
	return nil
}

// validatePackTemplateMasterRules (42.4.5) checks carton_type, when given,
// against an Active CartonType - the same referential-integrity pattern
// applied throughout this Stage to every Data field that names another
// master by code rather than by Link id.
func validatePackTemplateMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	cartonType := strField(payload, "carton_type")
	if cartonType == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var exists bool
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'CartonType' AND data->>'code' = $1 AND status = 'Active')`, schema), cartonType).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return &ValidationError{Code: "META-0198", SubFor: "Carton Type", Message: fmt.Sprintf("no Active carton type %q exists", cartonType)}
	}
	_ = docID
	return nil
}

// validatePackingValidationTemplateMasterRules (42.4.6) requires
// applies_to_value whenever applies_to narrows the scope - an "applies to
// Customer" row with no customer named would silently match nothing ever,
// which is worse than refusing it at authoring time.
func validatePackingValidationTemplateMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	appliesTo := strField(payload, "applies_to")
	value := strField(payload, "applies_to_value")
	if appliesTo != "" && appliesTo != "All" && value == "" {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Applies To Value", Message: fmt.Sprintf(
			"a value is required when Applies To is %s", appliesTo)}
	}
	_ = tenantID
	_ = docID
	return nil
}

// validateLoadingTaskMasterRules (42.4.8) confirms dock_door and trailer_no
// each reference an Active DockDoor/Trailer - the same existence check
// validateAppointmentMasterRules already runs for dock_door, applied a
// second time here since Appointment and LoadingTask are independent
// doctypes with no shared validator to reuse.
func validateLoadingTaskMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if dockDoor := strField(payload, "dock_door"); dockDoor != "" {
		var exists bool
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'DockDoor' AND data->>'code' = $1 AND status = 'Active')`, schema), dockDoor).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return &ValidationError{Code: "META-0198", SubFor: "Dock Door", Message: fmt.Sprintf("no Active dock door %q exists", dockDoor)}
		}
	}
	if trailer := strField(payload, "trailer_no"); trailer != "" {
		var exists bool
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Trailer' AND data->>'code' = $1 AND status = 'Active')`, schema), trailer).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return &ValidationError{Code: "META-0198", SubFor: "Trailer", Message: fmt.Sprintf("no Active trailer %q exists", trailer)}
		}
	}
	_ = docID
	return nil
}

// validatePreShipValidationRuleMasterRules (42.4.10) is the same "at most
// one Active rule per location_code, including blank" shape as
// WaveTemplate/TaskDispatchStrategy/ReceiptValidationRule above.
func validatePreShipValidationRuleMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	if strField(payload, "status") != "Active" {
		return nil
	}
	locationCode := strField(payload, "location_code")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'PreShipValidationRule' AND status = 'Active'
		  AND COALESCE(data->>'location_code', '') = $1 AND id != $2
		LIMIT 1`, schema), locationCode, docID).Scan(&existingID)
	if err == nil {
		scope := "every location"
		if locationCode != "" {
			scope = "location " + locationCode
		}
		return &ValidationError{Code: "MASTER-0053", Message: fmt.Sprintf(
			"an Active pre-ship validation rule already applies to %s (%s) - deactivate it first", scope, existingID)}
	}
	return nil
}
