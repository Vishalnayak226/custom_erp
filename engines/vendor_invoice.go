package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
)

const defaultVendorInvoiceTolerancePercent = 2.0

type poItemLine struct {
	Sku  string  `json:"sku"`
	Qty  int     `json:"qty"`
	Rate float64 `json:"rate"`
}

type grnItemLine struct {
	Sku string `json:"sku"`
	Qty int    `json:"qty"`
}

// Match3Way compares a PurchaseOrder's ordered amount, the value of what a
// GRN actually received (received qty x the PO's own per-sku rate - GRN
// itself carries no pricing, only quantities), and a VendorInvoice's billed
// amount, all within tolerancePercent of the PO amount. Sets the invoice to
// Matched or MismatchHold and stores the comparison for audit/review.
// tolerancePercent <= 0 falls back to a 2% system default.
func Match3Way(tenantID, poID, grnID, invoiceID string, tolerancePercent float64) (matched bool, err error) {
	if tolerancePercent <= 0 {
		tolerancePercent = defaultVendorInvoiceTolerancePercent
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return false, err
	}

	var invDataStr, invStatus string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1 FOR UPDATE`, schema),
		invoiceID).Scan(&invDataStr, &invStatus); err != nil {
		return false, fmt.Errorf("vendor invoice not found: %v", err)
	}
	var invData map[string]interface{}
	if err := json.Unmarshal([]byte(invDataStr), &invData); err != nil {
		return false, err
	}
	invoiceAmount, _ := invData["invoice_amount"].(float64)

	var poDataStr string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'PurchaseOrder' AND id = $1`, schema), poID).Scan(&poDataStr); err != nil {
		return false, fmt.Errorf("purchase order not found: %v", err)
	}
	var poData map[string]interface{}
	if err := json.Unmarshal([]byte(poDataStr), &poData); err != nil {
		return false, err
	}
	poAmount, _ := poData["total_amount"].(float64)
	poItemsStr, _ := poData["items"].(string)
	var poItems []poItemLine
	rateBySku := map[string]float64{}
	if poItemsStr != "" {
		_ = json.Unmarshal([]byte(poItemsStr), &poItems)
		for _, it := range poItems {
			rateBySku[it.Sku] = it.Rate
		}
	}

	var grnDataStr string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'GRN' AND id = $1`, schema), grnID).Scan(&grnDataStr); err != nil {
		return false, fmt.Errorf("GRN not found: %v", err)
	}
	var grnData map[string]interface{}
	if err := json.Unmarshal([]byte(grnDataStr), &grnData); err != nil {
		return false, err
	}
	if grnPOID, _ := grnData["po_id"].(string); grnPOID != poID {
		return false, fmt.Errorf("GRN %s does not reference PO %s", grnID, poID)
	}
	receivedStr, _ := grnData["received_items"].(string)
	var receivedItems []grnItemLine
	rateDataMissing := len(rateBySku) == 0
	grnValue := 0.0
	if receivedStr != "" {
		_ = json.Unmarshal([]byte(receivedStr), &receivedItems)
		for _, line := range receivedItems {
			rate, ok := rateBySku[line.Sku]
			if !ok {
				rateDataMissing = true
				continue
			}
			grnValue += rate * float64(line.Qty)
		}
	}

	tolerance := poAmount * tolerancePercent / 100
	poDelta := math.Abs(poAmount - invoiceAmount)
	grnDelta := math.Abs(grnValue - invoiceAmount)

	matched = poDelta <= tolerance
	if !rateDataMissing {
		matched = matched && grnDelta <= tolerance
	}

	newStatus := "MismatchHold"
	if matched {
		newStatus = "Matched"
	}
	invData["status"] = newStatus
	invData["match_details"] = map[string]interface{}{
		"po_amount":         poAmount,
		"grn_value":         grnValue,
		"invoice_amount":    invoiceAmount,
		"po_delta":          poDelta,
		"grn_delta":         grnDelta,
		"tolerance_percent": tolerancePercent,
		"rate_data_missing": rateDataMissing,
	}
	updatedBytes, _ := json.Marshal(invData)
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorInvoice' AND id = $3`, schema),
		updatedBytes, newStatus, invoiceID); err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	LogAuditEvent(tenantID, "system", "MATCH_3WAY_VENDOR_INVOICE", "SUCCESS",
		fmt.Sprintf("3-way match for invoice %s (PO %s, GRN %s): %s (po_delta=%.2f grn_delta=%.2f tolerance=%.2f%%)",
			invoiceID, poID, grnID, newStatus, poDelta, grnDelta, tolerancePercent))
	return matched, nil
}

// PayVendorInvoice settles a VendorInvoice's GRN Suspense liability. Allowed
// from status Matched directly; any other status requires an explicit,
// audited overrideReason - never a silent bypass of a mismatch hold.
func PayVendorInvoice(tenantID, invoiceID, userID, overrideReason string) (paidAmount int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return 0, err
	}

	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1 FOR UPDATE`, schema),
		invoiceID).Scan(&dataStr, &status); err != nil {
		return 0, fmt.Errorf("vendor invoice not found: %v", err)
	}
	if status == "Paid" {
		return 0, fmt.Errorf("invoice is already Paid")
	}
	if status != "Matched" && overrideReason == "" {
		return 0, fmt.Errorf("invoice is not Matched (status: %s) - pay requires an explicit override_reason", status)
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, err
	}
	amount, _ := data["invoice_amount"].(float64)
	if amount <= 0 {
		return 0, fmt.Errorf("invoice_amount must be positive to pay")
	}
	amountInt := int(amount)

	// GL posted before the status flip is committed below - PostDoubleEntry
	// manages its own transaction (can't be nested into this one), so the
	// row lock above is held across the call to prevent a concurrent second
	// pay attempt on the same invoice, and a GL failure here leaves the
	// invoice's status untouched (this tx is rolled back via defer) rather
	// than marking it Paid with no posting behind it.
	debits := map[string]int{"2100": amountInt}  // clear GRN Suspense liability
	credits := map[string]int{"1100": amountInt} // Cash/Bank paid out
	if err := PostDoubleEntry(tenantID, "VendorInvoice", invoiceID, debits, credits); err != nil {
		return 0, fmt.Errorf("GL posting failed, invoice not marked Paid: %v", err)
	}

	data["status"] = "Paid"
	if overrideReason != "" {
		data["payment_override_reason"] = overrideReason
	}
	updatedBytes, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Paid', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorInvoice' AND id = $2`, schema),
		updatedBytes, invoiceID); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	details := fmt.Sprintf("Paid vendor invoice %s amount=%d", invoiceID, amountInt)
	if overrideReason != "" {
		details = fmt.Sprintf("%s (override: %s, status was %s)", details, overrideReason, status)
	}
	LogAuditEvent(tenantID, userID, "PAY_VENDOR_INVOICE", "SUCCESS", details)
	return amountInt, nil
}
