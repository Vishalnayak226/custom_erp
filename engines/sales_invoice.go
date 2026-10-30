package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

	// Stage 37.6.1: an invoice flagged deferred_revenue credits 2600 Deferred
	// Revenue instead of 4100 Sales Revenue - the service/goods have been
	// billed but not yet delivered over their term, so recognising the whole
	// amount as revenue today would be wrong. Every other invoice's posting
	// (deferred_revenue blank/"No", i.e. every invoice before this stage
	// existed) is byte-identical to before.
	revenueAccount := "4100"
	deferred := data["deferred_revenue"] == "Yes"
	if deferred {
		revenueAccount = "2600"
	}
	debits := map[string]int{"1300": amount}
	credits := map[string]int{revenueAccount: amount}
	// Output GST (2026-10-07): an invoice that carries its tax split
	// (package invoices since 35.4, order invoices since this change) books
	// that tax to 2200/2201/2202 GST Output Payable and only the rest to
	// revenue - the same split POS checkout makes (PostSalesGSTBooking). It
	// used to credit the whole tax-inclusive amount to 4100, so no credit
	// sale's GST ever reached the GST return. Domestic, non-deferred only;
	// anything else posts exactly as before.
	var postErr error
	split := false
	if taxCredits := outputGSTCreditsPaise(data); len(taxCredits) > 0 && !position.Foreign && !deferred {
		totalPaise := RupeesToPaise(float64(amount))
		var taxPaise int64
		for _, v := range taxCredits {
			taxPaise += v
		}
		if taxPaise < totalPaise {
			paiseCredits := map[string]int64{revenueAccount: totalPaise - taxPaise}
			for account, v := range taxCredits {
				paiseCredits[account] = v
			}
			postErr = PostDoubleEntryTx(tx, tenantID, schema, "SalesInvoice", invoiceID, map[string]int64{"1300": totalPaise}, paiseCredits, "", fmt.Sprintf("SalesInvoice:%s:POST", invoiceID))
			split = true
		}
	}
	if !split {
		postErr = PostDoubleEntryTx(tx, tenantID, schema, "SalesInvoice", invoiceID, PaiseMap(debits), PaiseMap(credits), "", fmt.Sprintf("SalesInvoice:%s:POST", invoiceID),
			postingOptionsFor(position, position.Rate, debits, credits, map[string]float64{
				"1300":         position.TransactionAmount,
				revenueAccount: position.TransactionAmount,
			}))
	}
	if postErr != nil {
		return 0, fmt.Errorf("GL posting failed, invoice not marked Approved: %v", postErr)
	}
	// 140.12.4: the goods leave stock with the invoice, in the same
	// transaction as its revenue.
	if err := issueSalesInvoiceStockTx(tx, tenantID, schema, invoiceID, userID, dataStr, data); err != nil {
		return 0, err
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

	// Stage 37.6.1: the schedule is created here, after the posting above has
	// already committed, exactly the recognition decision this invoice was
	// just posted with - it needs no maker-checker of its own (the human
	// already decided, by flagging + posting this invoice) and a failure
	// here must never unwind a posting that has already committed real GL
	// entries, so it is best-effort/logged like the batch/serial
	// registration precedent (engines/wms_receiving.go).
	if deferred {
		termMonths := int(numFromInterface(data["deferred_term_months"]))
		if termMonths > 0 {
			if err := createDeferredRevenueSchedule(tenantID, invoiceID, float64(amount), termMonths); err != nil {
				LogSystemError(tenantID, "", "ERROR", "PostSalesInvoice", fmt.Sprintf("invoice %s posted but its DeferredRevenueSchedule could not be created: %v", invoiceID, err), "")
			}
		} else {
			LogSystemError(tenantID, "", "WARN", "PostSalesInvoice", fmt.Sprintf("invoice %s is flagged deferred_revenue but has no positive deferred_term_months - no schedule created", invoiceID), "")
		}
	}
	return amount, nil
}

// issueSalesInvoiceStockTx (140.12.4) takes a credit invoice's stock items out
// of its store at moving-average cost - Dr 5100 Cost of Goods Sold / Cr 1200
// Inventory - and writes the Stock Ledger, when the invoice is posted. An
// invoice whose goods already left stock posts money only: one raised from an
// order's shipment, a shipping package or an OMS bill, or a recurring contract
// (each carries its source's id), and one that quotes the delivery challan the
// goods went out on. Lines that are
// not stock items (a service, a free-text code) are skipped. A store that does
// not hold enough refuses the posting, unless the item's product type allows
// negative stock.
func issueSalesInvoiceStockTx(tx *sql.Tx, tenantID, schema, invoiceID, userID, dataStr string, data map[string]interface{}) error {
	for _, source := range []string{"sales_order_id", "shipping_package_id", "oms_bill_id", "recurring_contract_id"} {
		if strings.TrimSpace(strField(data, source)) != "" {
			return nil
		}
	}
	if challan := strings.TrimSpace(strField(data, "delivery_challan")); challan != "" {
		var found string
		_ = tx.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
			AND (id = $2 OR data->>'number' = $2) AND status IN ('Dispatched', 'Partly Returned', 'Returned', 'Closed') LIMIT 1`, schema),
			DeliveryChallanDoctype, challan).Scan(&found)
		if found == "" {
			return &ValidationError{Code: "FIN-0517", Message: fmt.Sprintf("delivery challan %s was not found or has not been dispatched - post the invoice without it to issue the goods from the store", challan)}
		}
		data["stock_issued_by"] = "Delivery challan " + challan
		return nil
	}
	var items []interface{}
	var cogsPaise int64
	for _, l := range salesInvoiceLineItems(dataStr) {
		if l.Sku == "" || l.Qty <= 0 {
			continue
		}
		if policy, _, known := ItemPolicyForSKU(tenantID, l.Sku); !known || !policy.Tracked() {
			continue
		}
		items = append(items, map[string]interface{}{"sku": l.Sku, "qty": -l.Qty})
		cogsPaise += int64(l.Qty) * ResolveCOGSUnitCostPaise(tenantID, l.Sku, itemStandardCostRupees(tenantID, l.Sku))
	}
	if len(items) == 0 {
		return nil
	}
	location := strings.TrimSpace(strField(data, "location"))
	if location == "" {
		return &ValidationError{Code: "FIN-0517", Message: "choose the store the invoice is issued from - its stock items leave that store's stock"}
	}
	markNegativeStockAllowed(tenantID, items)
	_, posted, err := PostInventoryLedgerWithVoucherTx(tx, schema, location, items, false)
	var short *InsufficientStockError
	if errors.As(err, &short) {
		return &ValidationError{Code: "FIN-0517", Message: fmt.Sprintf("only %d of %s at %s - receive or transfer the stock in before posting the invoice", short.Available, short.SKU, location)}
	}
	if err != nil {
		return err
	}
	if err := writeStockLedgerLinesInTx(tx, schema, location, "SalesInvoice", invoiceID, userID, posted); err != nil {
		return err
	}
	if cogsPaise > 0 {
		if err := PostDoubleEntryTx(tx, tenantID, schema, "SalesInvoice", invoiceID,
			map[string]int64{"5100": cogsPaise}, map[string]int64{"1200": cogsPaise}, "",
			fmt.Sprintf("SalesInvoice:%s:COGS", invoiceID)); err != nil {
			return fmt.Errorf("COGS posting failed, invoice not posted: %v", err)
		}
	}
	data["stock_issued_from"] = location
	data["cogs_amount"] = PaiseToRupees(cogsPaise)
	return nil
}

// outputGSTCreditsPaise reads the tax split an invoice stored when it was
// generated (cgst/sgst/igst, rupees) as GST Output Payable credits in paise.
// Empty when the invoice carries no tax - every invoice created before its
// generator computed GST, which then posts exactly as it always did.
func outputGSTCreditsPaise(data map[string]interface{}) map[string]int64 {
	out := map[string]int64{}
	for field, account := range map[string]string{"cgst": "2200", "sgst": "2201", "igst": "2202"} {
		if v := RupeesToPaise(numFromInterface(data[field])); v > 0 {
			out[account] = v
		}
	}
	return out
}

// SettleSalesInvoice moves an Approved SalesInvoice to Paid once the
// customer actually pays: debit Cash/Bank (1100), credit Accounts
// Receivable (1300), clearing the receivable this invoice booked in
// PostSalesInvoice. Symmetric to PayVendorInvoice on the payable side.
// Stage 37.1.3 adds the realised FX leg. opts is variadic so both existing
// callers needed no change; it carries the date the money actually moved and,
// optionally, the rate the bank actually gave.
func SettleSalesInvoice(tenantID, invoiceID, userID string, opts ...SettlementOptions) (amount int, err error) {
	// Stage 73 / 55.10: one code path for every receipt. Amount 0 settles the
	// whole outstanding balance - for an invoice nothing has been received
	// against, the same posting under the same SalesInvoice:<id>:SETTLE key
	// this function always made; for a part-paid one, only what is left.
	res, err := SettleSalesInvoiceAmount(tenantID, invoiceID, userID, 0, "", opts...)
	if err != nil {
		return 0, err
	}
	return roundToRupees(res.FunctionalAmount), nil
}

// CancelSalesInvoice (140.12.8) cancels an invoice that has taken no money:
// a Draft simply turns Cancelled; a posted one has every GL line it posted
// reversed (revenue, output tax, cost of goods) and the stock it issued put
// back, in one transaction. An invoice with receipts, a credit note or a
// return against it, an e-invoice IRN or a deferred-revenue schedule is
// corrected with a credit note instead.
func CancelSalesInvoice(tenantID, invoiceID, userID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return &ValidationError{Code: "FIN-0517", Message: "say why the invoice is being cancelled"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}
	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'SalesInvoice' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema),
		invoiceID).Scan(&dataStr, &status); err != nil {
		return fmt.Errorf("sales invoice not found: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	refuse := func(why string) error {
		return &ValidationError{Code: "FIN-0517", Message: fmt.Sprintf("sales invoice %s %s - raise a credit note instead", invoiceID, why)}
	}
	switch {
	case status == "Cancelled":
		return nil
	case status != "Draft" && status != "Approved":
		return refuse("is " + status)
	case status == "Approved" && sumSettledPaise(mustSettlements(data)) > 0:
		return refuse("has receipts or credits against it")
	case status == "Approved" && salesInvoiceHasIRN(tenantID, invoiceID):
		return refuse("has an e-invoice IRN (cancel the IRN within 24 hours first)")
	case status == "Approved" && data["deferred_revenue"] == "Yes":
		return refuse("recognises its revenue over a schedule")
	}
	if status == "Approved" {
		rows, err := tx.Query(fmt.Sprintf(`SELECT account_code, SUM(debit), SUM(credit) FROM %s.gl_postings
			WHERE document_type = 'SalesInvoice' AND document_id = $1 GROUP BY account_code`, schema), invoiceID)
		if err != nil {
			return err
		}
		debits, credits := map[string]int64{}, map[string]int64{}
		for rows.Next() {
			var account string
			var d, c int64
			if err := rows.Scan(&account, &d, &c); err != nil {
				rows.Close()
				return err
			}
			if net := d - c; net > 0 {
				credits[account] = net
			} else if net < 0 {
				debits[account] = -net
			}
		}
		rows.Close()
		if len(debits) > 0 {
			if err := PostDoubleEntryTx(tx, tenantID, schema, "SalesInvoice", invoiceID, debits, credits, "", fmt.Sprintf("SalesInvoice:%s:CANCEL", invoiceID)); err != nil {
				return postingError(err, "invoice not cancelled")
			}
		}
		if location := strField(data, "stock_issued_from"); location != "" {
			var items []interface{}
			for _, l := range salesInvoiceLineItems(dataStr) {
				if l.Sku == "" || l.Qty <= 0 {
					continue
				}
				if policy, _, known := ItemPolicyForSKU(tenantID, l.Sku); known && policy.Tracked() {
					items = append(items, map[string]interface{}{"sku": l.Sku, "qty": l.Qty})
				}
			}
			if len(items) > 0 {
				_, posted, err := PostInventoryLedgerWithVoucherTx(tx, schema, location, items, true)
				if err != nil {
					return err
				}
				if err := writeStockLedgerLinesInTx(tx, schema, location, "SalesInvoiceCancel", invoiceID, userID, posted); err != nil {
					return err
				}
			}
		}
	}
	data["status"] = "Cancelled"
	data["cancelled_by"], data["cancel_reason"], data["cancelled_on"] = userID, reason, BusinessToday()
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Cancelled', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesInvoice' AND id = $2`, schema),
		raw, invoiceID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "CANCEL_SALES_INVOICE", "SUCCESS", fmt.Sprintf("Cancelled sales invoice %s (was %s): %s", invoiceID, status, reason))
	return nil
}

func salesInvoiceHasIRN(tenantID, invoiceID string) bool {
	r, err := GetIRPRecordFor(tenantID, "SalesInvoice", invoiceID)
	return err == nil && r != nil && r.State != IRPStateCancelled && r.IRN != ""
}

func mustSettlements(data map[string]interface{}) []SettlementEntry {
	entries, _ := readSettlementEntries(data, "settlements")
	return entries
}
