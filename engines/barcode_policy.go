package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Barcode policy (Stage 57.7). The user: "Why is barcode generated at Item
// level? It should be on the variant. I should have config to generate it at
// Item-SKU level and at GRN level, and config for a barcode per date versus
// the same barcode for a SKU every time."
//
// In this data model an Item already IS the sellable SKU - a variant's parent
// "design" is a Product Family (PrepareItemVariantCode), which never carries
// a barcode. What was wrong was that the Item form demanded someone type a
// barcode, and that nothing let a business choose when one is issued or
// whether it changes per receipt. Two settings now decide that:
//
//	inventory.barcode_generate_at       sku_create (default) | grn | off
//	inventory.barcode_per_receipt_date  no (default) | yes
//
// sku_create issues the SKU's permanent barcode when the Item is created
// without one; grn waits for the first goods receipt (the Stage 51.6 safety
// net, which also still runs under sku_create for older Items); off issues
// none automatically. Independently, per-receipt-date gives every SKU a
// fresh barcode for each day it is received - for businesses that age or
// rotate stock by arrival date - recorded in the ItemBarcode register so a
// scan of it still resolves to the SKU (ResolveItemBySKU) and a GRN's
// stickers print it (ReceiptBarcodeFor).

const (
	BarcodeAtSKUCreate = "sku_create"
	BarcodeAtGRN       = "grn"
	BarcodeAtOff       = "off"
)

// BarcodeGenerateAt is the tenant's choice of when a SKU's barcode is issued.
func BarcodeGenerateAt(tenantID string) string {
	switch v := GetSettingString(tenantID, "inventory.barcode_generate_at"); v {
	case BarcodeAtGRN, BarcodeAtOff:
		return v
	default:
		return BarcodeAtSKUCreate
	}
}

// BarcodePerReceiptDate reports whether each receipt date gets its own code.
func BarcodePerReceiptDate(tenantID string) bool {
	return GetSettingString(tenantID, "inventory.barcode_per_receipt_date") == "yes"
}

// PrepareItemBarcode issues a new Item's permanent barcode at creation when
// the tenant generates them at SKU creation and none was given. Best-effort:
// a numbering failure leaves the Item without one (the receipt-time safety
// net still covers it) rather than refusing the save.
func PrepareItemBarcode(tenantID string, isCreate bool, payload map[string]interface{}) {
	if !isCreate || BarcodeGenerateAt(tenantID) != BarcodeAtSKUCreate {
		return
	}
	if existing, _ := payload["barcode"].(string); strings.TrimSpace(existing) != "" {
		return
	}
	code, err := GenerateEANBarcode(tenantID)
	if err != nil {
		LogSystemError(tenantID, "", "WARN", "PrepareItemBarcode", fmt.Sprintf("could not issue a barcode at SKU creation: %v", err), "")
		return
	}
	payload["barcode"] = code
}

// datedEANFromParts builds a receipt-date barcode: EAN-13 in the GS1
// internal-use range, "02" + YYMMDD + a four-digit sequence for that day +
// check digit. Stable codes from GenerateEANBarcode start "020" + a running
// number; dated ones start "02" + the year (2026 -> "026..."), so the two
// never collide in this century.
func datedEANFromParts(day time.Time, seq int) (string, error) {
	if seq < 1 || seq > 9999 {
		return "", fmt.Errorf("more than 9,999 receipt-date barcodes issued on %s", day.Format("2006-01-02"))
	}
	base := "02" + day.Format("060102") + fmt.Sprintf("%04d", seq)
	check, err := gs1CheckDigit(base)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%d", base, check), nil
}

// GenerateDatedEANBarcode draws the day's next number from the
// PIMDatedBarcodeSeq series, bucketed by date (an ANNUAL series keyed by the
// day, so the counter restarts daily) - the same row-locked counter every
// document number uses.
func GenerateDatedEANBarcode(tenantID string, day time.Time) (string, error) {
	raw, err := GenerateSequence(tenantID, "PIMDatedBarcodeSeq", "", day.Format("060102"))
	if err != nil {
		return "", err
	}
	digits := raw
	if len(digits) > 4 {
		digits = digits[len(digits)-4:]
	}
	var seq int
	if _, err := fmt.Sscanf(digits, "%d", &seq); err != nil {
		return "", fmt.Errorf("PIMDatedBarcodeSeq returned %q, not a number - check prefix_configs", raw)
	}
	return datedEANFromParts(day, seq)
}

// EnsureReceiptBarcodes runs after a goods receipt has posted. It applies the
// tenant's policy to every distinct SKU on the receipt: the permanent barcode
// (unless generation is off), and - when per-receipt-date is on - that day's
// dated barcode, registered against the SKU and the receipt. Best-effort like
// EnsureItemBarcodes: stock has already posted, so a failure is logged and
// that one SKU skipped.
func EnsureReceiptBarcodes(tenantID, grnID string, items []interface{}) {
	if BarcodeGenerateAt(tenantID) != BarcodeAtOff {
		EnsureItemBarcodes(tenantID, items)
	}
	if !BarcodePerReceiptDate(tenantID) {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	day := time.Now()
	dayStr := localDateString(day)
	seen := map[string]bool{}
	for _, raw := range items {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		sku, _ := m["sku"].(string)
		if sku == "" || seen[sku] {
			continue
		}
		seen[sku] = true
		resolved, errItem := ResolveItemBySKU(tenantID, sku)
		if errItem != nil {
			continue
		}
		// One dated code per SKU per day, however many receipts that day.
		var existing string
		errQ := db.DB.QueryRow(fmt.Sprintf(`
			SELECT id FROM %s.documents
			WHERE doctype = 'ItemBarcode' AND deleted_at IS NULL
			  AND data->>'item' = $1 AND data->>'kind' = 'Dated' AND data->>'received_on' = $2
			LIMIT 1`, schema), resolved.ID, dayStr).Scan(&existing)
		if errQ == nil {
			// Reused by a later receipt the same day: record that receipt too,
			// so its stickers print the same code (ReceiptBarcodeFor).
			_, _ = db.DB.Exec(fmt.Sprintf(`
				UPDATE %s.documents
				SET data = jsonb_set(data, '{grns}', COALESCE(data->'grns', '[]'::jsonb) || to_jsonb($2::text), true)
				WHERE id = $1 AND NOT COALESCE(data->'grns', '[]'::jsonb) ? $2`, schema), existing, grnID)
			continue
		}
		if errQ != sql.ErrNoRows {
			LogSystemError(tenantID, "", "ERROR", "EnsureReceiptBarcodes", fmt.Sprintf("could not check dated barcode for %s: %v", sku, errQ), "")
			continue
		}
		code, errGen := GenerateDatedEANBarcode(tenantID, day)
		if errGen != nil {
			LogSystemError(tenantID, "", "ERROR", "EnsureReceiptBarcodes", fmt.Sprintf("could not issue a receipt-date barcode for %s: %v", sku, errGen), "")
			continue
		}
		itemCode, _ := resolved.Data["code"].(string)
		data, _ := json.Marshal(map[string]interface{}{
			"code": code, "item": resolved.ID, "item_code": itemCode, "kind": "Dated",
			"received_on": dayStr, "grn": grnID, "grns": []string{grnID}, "status": "Active",
		})
		if _, errIns := db.DB.Exec(fmt.Sprintf(`
			INSERT INTO %s.documents (id, doctype, data, status, created_by)
			VALUES ($1, 'ItemBarcode', $2, 'Active', 'system')
			ON CONFLICT (id) DO NOTHING`, schema), code, data); errIns != nil {
			LogSystemError(tenantID, "", "ERROR", "EnsureReceiptBarcodes", fmt.Sprintf("could not register receipt-date barcode %s for %s: %v", code, sku, errIns), "")
		}
	}
}

// localDateString is the server's local calendar day as YYYY-MM-DD.
func localDateString(t time.Time) string {
	return t.Format("2006-01-02")
}

// resolveRegisteredBarcode returns the Item id a registered (receipt-date)
// barcode belongs to, or "" if the code is not in the register.
func resolveRegisteredBarcode(schema, code string) string {
	var itemID string
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT data->>'item' FROM %s.documents
		WHERE doctype = 'ItemBarcode' AND deleted_at IS NULL AND status = 'Active'
		  AND (id = $1 OR data->>'code' = $1)
		LIMIT 1`, schema), code).Scan(&itemID); err != nil {
		return ""
	}
	return itemID
}

// ReceiptBarcodeFor is the dated barcode a receipt issued for an Item, or ""
// - what a GRN's stickers print when per-receipt-date barcodes are on.
func ReceiptBarcodeFor(tenantID, itemID, grnID string) string {
	if itemID == "" || grnID == "" {
		return ""
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	var code string
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT data->>'code' FROM %s.documents
		WHERE doctype = 'ItemBarcode' AND deleted_at IS NULL
		  AND data->>'item' = $1 AND data->>'kind' = 'Dated'
		  AND (data->>'grn' = $2 OR COALESCE(data->'grns', '[]'::jsonb) ? $2)
		LIMIT 1`, schema), itemID, grnID).Scan(&code); err != nil {
		return ""
	}
	return code
}
