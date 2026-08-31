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
	// Stage 37.1.3. `total_amount` is in the currency the invoice was
	// transacted in; the ledger is in the tenant's functional currency. Posting
	// the raw field - which is what this line did before - booked a USD 1,000
	// invoice as a 1,000-rupee receivable. The base twin 37.1.2 stamps is the
	// functional value, and it is what 1300 and 4100 must receive.
	position := documentFXPosition(tenantID, data, "total_amount")
	if position.TransactionAmount <= 0 {
		return 0, fmt.Errorf("total_amount must be positive to post")
	}
	amount = position.BookedAmount
	if amount <= 0 {
		return 0, fmt.Errorf("total_amount converts to a non-positive %s value at rate %v", position.Functional, position.Rate)
	}

	// Stage 37.4.2: posting is the moment this invoice's amount becomes real
	// AR (see this function's own doc comment) - the deliberate, human-
	// triggered point to check it, rather than at Draft creation (which can
	// fire from an automated shipment cascade, CreateSalesInvoiceFromOrder's
	// own doc comment) or leaving it unchecked forever. A blank/zero
	// Customer.credit_limit is always a no-op.
	if customer, _ := data["customer"].(string); customer != "" {
		if err := CheckCustomerCreditLimit(tenantID, customer, float64(amount)); err != nil {
			return 0, err
		}
	}

	debits := map[string]int{"1300": amount}
	credits := map[string]int{"4100": amount}
	if err := PostDoubleEntry(tenantID, "SalesInvoice", invoiceID, PaiseMap(debits), PaiseMap(credits), "", fmt.Sprintf("SalesInvoice:%s:POST", invoiceID),
		postingOptionsFor(position, position.Rate, debits, credits, map[string]float64{
			"1300": position.TransactionAmount,
			"4100": position.TransactionAmount,
		})); err != nil {
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
// Stage 37.1.3 adds the realised FX leg. opts is variadic so both existing
// callers needed no change; it carries the date the money actually moved and,
// optionally, the rate the bank actually gave.
func SettleSalesInvoice(tenantID, invoiceID, userID string, opts ...SettlementOptions) (amount int, err error) {
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
	// Stage 37.1.3. The cash actually received is the transaction amount at the
	// SETTLEMENT rate; the receivable being cleared is what the ledger carries
	// for it, which is the booking rate plus any period-end revaluation already
	// posted. The gap between those two is a realised gain or loss, and it is
	// the whole reason this stage exists.
	position := documentFXPosition(tenantID, data, "total_amount")
	settlement, err := resolveSettlementFX(tenantID, position, true, settlementOption(opts))
	if err != nil {
		return 0, err
	}
	amount = settlement.SettlementAmount

	debits := map[string]int{"1100": settlement.SettlementAmount}
	credits := map[string]int{"1300": position.CarryingAmount}
	applyRealisedFXLine(debits, credits, settlement.RealisedGainLoss)
	if err := PostDoubleEntry(tenantID, "SalesInvoice", invoiceID, PaiseMap(debits), PaiseMap(credits), settlement.PostingDate(), fmt.Sprintf("SalesInvoice:%s:SETTLE", invoiceID),
		// The cash and receivable lines carry the full foreign amount; the
		// realised gain/loss line is left at zero, because it has no
		// foreign-currency value by construction.
		postingOptionsFor(position, settlement.SettlementRate, debits, credits, map[string]float64{
			"1100": position.TransactionAmount,
			"1300": position.TransactionAmount,
		})); err != nil {
		return 0, fmt.Errorf("GL posting failed, invoice not marked Paid: %v", err)
	}
	recordSettlementFX(data, settlement)

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
	LogAuditEvent(tenantID, userID, "SETTLE_SALES_INVOICE", "SUCCESS", settlementAuditDetail("sales invoice", invoiceID, settlement))
	return amount, nil
}
