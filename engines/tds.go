package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
)

// CalculateTDS looks up a TDSSection by code and, if paymentAmount meets or
// exceeds its threshold, returns the tax-deducted-at-source amount and the
// resulting net payable. Below threshold, tdsAmount is 0 and net equals the
// full payment - calc-only (Stage 20.28), matching how Stage 13.10/17.5
// scoped GST: no e-filing, just the correct arithmetic.
func CalculateTDS(tenantID, sectionCode string, paymentAmount float64) (tdsAmount float64, netPayable float64, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, err
	}
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'TDSSection' AND id = $1`, schema), sectionCode).Scan(&dataStr); err != nil {
		return 0, 0, fmt.Errorf("TDS section '%s' not found: %v", sectionCode, err)
	}
	var section map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &section); err != nil {
		return 0, 0, err
	}
	threshold := numFromInterface(section["threshold_amount"])
	rate := numFromInterface(section["rate_percent"])

	if paymentAmount < threshold {
		return 0, paymentAmount, nil
	}
	tdsAmount = math.Round(paymentAmount * rate / 100)
	return tdsAmount, paymentAmount - tdsAmount, nil
}

// PayVendorInvoiceWithTDS is the TDS-aware sibling of engines.PayVendorInvoice
// (Stage 17.8) - kept as its own function rather than adding a TDS branch to
// PayVendorInvoice so the existing, already-tested no-TDS path (and its
// tests) stay completely unchanged. Requires the invoice to be Matched, same
// as the plain pay path - there is deliberately no override-reason escape
// hatch here, since a TDS-bearing payment is exactly the kind of transaction
// that should never bypass the match gate silently.
func PayVendorInvoiceWithTDS(tenantID, invoiceID, sectionCode, userID string) (netPaid int, tdsAmount int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return 0, 0, err
	}

	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1 FOR UPDATE`, schema),
		invoiceID).Scan(&dataStr, &status); err != nil {
		return 0, 0, fmt.Errorf("vendor invoice not found: %v", err)
	}
	if status == "Paid" {
		return 0, 0, fmt.Errorf("invoice is already Paid")
	}
	if status != "Matched" {
		return 0, 0, &ValidationError{Code: "VENDOR-0092", Message: fmt.Sprintf("invoice is not Matched (status: %s)", status)}
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, 0, err
	}
	amount := numFromInterface(data["invoice_amount"])
	if amount <= 0 {
		return 0, 0, fmt.Errorf("invoice_amount must be positive to pay")
	}
	amountInt := int(amount)

	tdsF, netF, err := CalculateTDS(tenantID, sectionCode, amount)
	if err != nil {
		return 0, 0, err
	}
	tdsAmount = int(tdsF)
	netPaid = int(netF)

	// Held across the row lock above, same reasoning as PayVendorInvoice:
	// GL posted before the status flip commits, so a posting failure leaves
	// the invoice untouched (this tx rolls back) rather than marked Paid
	// with no posting behind it.
	debits := map[string]int{"2100": amountInt}
	credits := map[string]int{"1100": netPaid, "2300": tdsAmount}
	if err := PostDoubleEntry(tenantID, "VendorInvoice", invoiceID, debits, credits, "", fmt.Sprintf("VendorInvoice:%s:PAY_TDS", invoiceID)); err != nil {
		return 0, 0, fmt.Errorf("GL posting failed, invoice not marked Paid: %v", err)
	}

	data["status"] = "Paid"
	data["tds_section"] = sectionCode
	data["tds_amount"] = tdsAmount
	data["net_paid"] = netPaid
	updatedBytes, err := json.Marshal(data)
	if err != nil {
		return 0, 0, err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Paid', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorInvoice' AND id = $2`, schema),
		updatedBytes, invoiceID); err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	LogAuditEvent(tenantID, userID, "PAY_VENDOR_INVOICE_TDS", "SUCCESS",
		fmt.Sprintf("Paid vendor invoice %s net=%d tds=%d (section %s)", invoiceID, netPaid, tdsAmount, sectionCode))
	return netPaid, tdsAmount, nil
}
