package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// SalesInvoice has existed since Stage 1 (db/migration.sql) as a registered
// Transaction doctype with a Draft/Approved/Paid/Cancelled status flow, but
// was never wired to a GL posting or an amount field - a dormant shell, not
// a working credit-sales flow (unlike POS, which settles in cash/card/UPI
// at checkout). Stage 20.33 needed a real, mutable-over-time receivable
// balance to build Receivables Ageing on, symmetric to how
// GetPayablesAgeingReport already reads PurchaseOrder's "Approved" status -
// this file closes that gap directly instead of inventing a parallel
// doctype.

// PostSalesInvoice moves a Draft SalesInvoice to Approved and recognizes
// the receivable: debit Accounts Receivable (1300), credit Sales Revenue
// (4100). This is the point a credit sale to a customer becomes real money
// owed, and the moment GetReceivablesAgeingReport starts ageing it.
func PostSalesInvoice(tenantID, invoiceID, userID string) (amount int, err error) {
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
		`SELECT data, status FROM %s.documents WHERE doctype = 'SalesInvoice' AND id = $1 FOR UPDATE`, schema),
		invoiceID).Scan(&dataStr, &status); err != nil {
		return 0, fmt.Errorf("sales invoice not found: %v", err)
	}
	if status != "Draft" {
		return 0, fmt.Errorf("only a Draft sales invoice can be posted (current status: %s)", status)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, err
	}
	amountF := numFromInterface(data["total_amount"])
	if amountF <= 0 {
		return 0, fmt.Errorf("total_amount must be positive to post")
	}
	amount = int(amountF)

	debits := map[string]int{"1300": amount}
	credits := map[string]int{"4100": amount}
	if err := PostDoubleEntry(tenantID, "SalesInvoice", invoiceID, debits, credits, "", fmt.Sprintf("SalesInvoice:%s:POST", invoiceID)); err != nil {
		return 0, fmt.Errorf("GL posting failed, invoice not marked Approved: %v", err)
	}

	data["status"] = "Approved"
	updatedBytes, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Approved', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesInvoice' AND id = $2`, schema),
		updatedBytes, invoiceID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	LogAuditEvent(tenantID, userID, "POST_SALES_INVOICE", "SUCCESS", fmt.Sprintf("Posted sales invoice %s amount=%d, AR recognized", invoiceID, amount))
	return amount, nil
}

// SettleSalesInvoice moves an Approved SalesInvoice to Paid once the
// customer actually pays: debit Cash/Bank (1100), credit Accounts
// Receivable (1300), clearing the receivable this invoice booked in
// PostSalesInvoice. Symmetric to PayVendorInvoice on the payable side.
func SettleSalesInvoice(tenantID, invoiceID, userID string) (amount int, err error) {
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
		`SELECT data, status FROM %s.documents WHERE doctype = 'SalesInvoice' AND id = $1 FOR UPDATE`, schema),
		invoiceID).Scan(&dataStr, &status); err != nil {
		return 0, fmt.Errorf("sales invoice not found: %v", err)
	}
	if status != "Approved" {
		return 0, fmt.Errorf("only an Approved sales invoice can be settled (current status: %s)", status)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, err
	}
	amountF := numFromInterface(data["total_amount"])
	amount = int(amountF)

	debits := map[string]int{"1100": amount}
	credits := map[string]int{"1300": amount}
	if err := PostDoubleEntry(tenantID, "SalesInvoice", invoiceID, debits, credits, "", fmt.Sprintf("SalesInvoice:%s:SETTLE", invoiceID)); err != nil {
		return 0, fmt.Errorf("GL posting failed, invoice not marked Paid: %v", err)
	}

	data["status"] = "Paid"
	updatedBytes, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Paid', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesInvoice' AND id = $2`, schema),
		updatedBytes, invoiceID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	LogAuditEvent(tenantID, userID, "SETTLE_SALES_INVOICE", "SUCCESS", fmt.Sprintf("Settled sales invoice %s amount=%d", invoiceID, amount))
	return amount, nil
}
