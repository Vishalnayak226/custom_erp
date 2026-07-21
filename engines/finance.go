package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
)

// PostDoubleEntry writes balanced debit/credit transactions to the GL
// Ledger.
//
// transactionDate (YYYY-MM-DD, 24.6) is the document's own transaction date
// for the closed-period check below - pass "" to check against today
// (CURRENT_DATE), which is every existing caller's behavior today (see
// rejectIfCurrentPeriodClosed's own comment for why).
//
// postingKey (24.5) is a caller-supplied idempotency key, unique per logical
// posting event - not per document, since one document can legitimately be
// posted more than once over its lifecycle for different reasons (e.g. a
// SalesInvoice is posted on approval and again on settlement). The
// convention every call site below uses is "<DocType>:<DocID>:<PURPOSE>".
// A client retry after a dropped response (not just a malicious replay)
// previously had no way to detect it was retrying an already-completed
// posting and would double-post; now a repeat call with the same key is a
// silent no-op instead. Pass "" to opt out (e.g. test helpers that don't
// care about this).
func PostDoubleEntry(tenantID string, docType string, docID string, debits map[string]int, credits map[string]int, transactionDate string, postingKey string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	sumDebits := 0
	for _, val := range debits {
		sumDebits += val
	}

	sumCredits := 0
	for _, val := range credits {
		sumCredits += val
	}

	if sumDebits != sumCredits {
		return fmt.Errorf("unbalanced double-entry journal: sum of debits (%d) must equal sum of credits (%d)", sumDebits, sumCredits)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	if err := rejectIfCurrentPeriodClosed(tx, schema, transactionDate); err != nil {
		return err
	}

	if postingKey != "" {
		var alreadyPosted bool
		if err := tx.QueryRow(fmt.Sprintf(
			`SELECT EXISTS(SELECT 1 FROM %s.gl_postings WHERE idempotency_key = $1)`, schema), postingKey).Scan(&alreadyPosted); err != nil {
			return err
		}
		if alreadyPosted {
			return tx.Commit()
		}
	}

	var keyArg interface{}
	if postingKey != "" {
		keyArg = postingKey
	}

	// Insert debits
	for code, val := range debits {
		if val <= 0 {
			continue
		}
		query := fmt.Sprintf(`
			INSERT INTO %s.gl_postings (account_code, debit, credit, document_type, document_id, idempotency_key)
			VALUES ($1, $2, 0, $3, $4, $5)`, schema)
		_, err := tx.Exec(query, code, val, docType, docID, keyArg)
		if err != nil {
			return fmt.Errorf("error posting debit for account %s: %v", code, err)
		}
	}

	// Insert credits
	for code, val := range credits {
		if val <= 0 {
			continue
		}
		query := fmt.Sprintf(`
			INSERT INTO %s.gl_postings (account_code, debit, credit, document_type, document_id, idempotency_key)
			VALUES ($1, 0, $2, $3, $4, $5)`, schema)
		_, err := tx.Exec(query, code, val, docType, docID, keyArg)
		if err != nil {
			return fmt.Errorf("error posting credit for account %s: %v", code, err)
		}
	}

	return tx.Commit()
}

// GetTrialBalance fetches summary trial balances for the current tenant accounts
func GetTrialBalance(tenantID string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT a.account_code, a.account_name, a.account_type, 
		       COALESCE(SUM(p.debit), 0) as total_debit, 
		       COALESCE(SUM(p.credit), 0) as total_credit
		FROM %s.gl_accounts a
		LEFT JOIN %s.gl_postings p ON a.account_code = p.account_code
		GROUP BY a.account_code, a.account_name, a.account_type
		ORDER BY a.account_code`, schema, schema)

	rows, err := db.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type AccountBalance struct {
		Code   string `json:"account_code"`
		Name   string `json:"account_name"`
		Type   string `json:"account_type"`
		Debit  int    `json:"debit"`
		Credit int    `json:"credit"`
	}

	var balances []AccountBalance
	totalDebits := 0
	totalCredits := 0

	for rows.Next() {
		var b AccountBalance
		err := rows.Scan(&b.Code, &b.Name, &b.Type, &b.Debit, &b.Credit)
		if err != nil {
			return nil, err
		}
		totalDebits += b.Debit
		totalCredits += b.Credit
		balances = append(balances, b)
	}

	balanced := totalDebits == totalCredits
	var statusMsg string
	if balanced {
		statusMsg = "Balanced trial ledger"
	} else {
		statusMsg = "Unbalanced trial ledger exception detected"
	}

	return map[string]interface{}{
		"balances":      balances,
		"total_debits":  totalDebits,
		"total_credits": totalCredits,
		"status":        statusMsg,
		"balanced":      balanced,
	}, nil
}

// PostGRNFinanceBooking creates dynamic financial postings for warehouse receiving
func PostGRNFinanceBooking(tenantID string, grnID string, amount int) error {
	if amount <= 0 {
		return errors.New("GRN transaction value must be positive")
	}

	debits := map[string]int{"1200": amount}  // Debit: Inventory Control Account
	credits := map[string]int{"2100": amount} // Credit: GRN Suspense Account

	return PostDoubleEntry(tenantID, "GRN", grnID, debits, credits, "", fmt.Sprintf("GRN:%s:RECEIPT", grnID))
}

// paymentModeClearingAccount maps a POS payment mode to the GL asset
// account it settles into (Stage 20.9). Unknown/empty modes fall back to
// 1100 (Cash) - the behavior every payment mode had before this stage.
func paymentModeClearingAccount(paymentMode string) string {
	switch paymentMode {
	case "Card":
		return "1101"
	case "UPI":
		return "1102"
	default:
		return "1100"
	}
}

// PostSalesFinanceBooking creates dynamic financial postings for sales cart checkout.
// paymentMode selects which GL clearing account the sale settles into
// (Stage 20.9) - previously every mode posted to 1100 regardless.
func PostSalesFinanceBooking(tenantID string, checkoutID string, salePrice int, costPrice int, paymentMode string) error {
	if salePrice <= 0 || costPrice <= 0 {
		return errors.New("sales and cost prices must be positive")
	}

	// 1. Post Revenue Bookings
	revenueDebits := map[string]int{paymentModeClearingAccount(paymentMode): salePrice} // Debit: Cash/Card/UPI clearing account
	revenueCredits := map[string]int{"4100": salePrice}                                 // Credit: Sales Revenue Account
	err := PostDoubleEntry(tenantID, "POSCart", checkoutID, revenueDebits, revenueCredits, "", fmt.Sprintf("POSCart:%s:SALE_REVENUE", checkoutID))
	if err != nil {
		return err
	}

	// 2. Post COGS / Inventory Bookings
	cogsDebits := map[string]int{"5100": costPrice}  // Debit: Cost of Goods Sold Account
	cogsCredits := map[string]int{"1200": costPrice} // Credit: Inventory Control Account
	return PostDoubleEntry(tenantID, "POSCart", checkoutID, cogsDebits, cogsCredits, "", fmt.Sprintf("POSCart:%s:SALE_COGS", checkoutID))
}

// PostSalesGSTBooking books the output-tax liability split for a completed
// sale (Stage 17.5), on top of PostSalesFinanceBooking's revenue posting
// above. That posting credited the full tax-inclusive salePrice to Sales
// Revenue (4100); this one moves the tax portion back out of 4100 and into
// the appropriate payable account(s), leaving 4100 holding only the
// taxable (net-of-tax) amount - Cash (1100) still holds the full amount
// actually collected, unchanged.
func PostSalesGSTBooking(tenantID, checkoutID string, breakdown GSTBreakdown) error {
	// Round each component to int first, then sum those - not the other way
	// around - so the debit side below always exactly matches what the
	// credit side actually posts (independent per-component truncation
	// could otherwise leave the two off by a rupee and fail PostDoubleEntry's
	// balance check).
	intCGST := int(breakdown.CGST)
	intSGST := int(breakdown.SGST)
	intIGST := int(breakdown.IGST)
	totalTax := intCGST + intSGST + intIGST
	if totalTax <= 0 {
		return nil
	}
	debits := map[string]int{"4100": totalTax}
	credits := map[string]int{}
	if breakdown.Interstate {
		credits["2202"] = intIGST // GST Output Payable - IGST
	} else {
		credits["2200"] = intCGST // GST Output Payable - CGST
		credits["2201"] = intSGST // GST Output Payable - SGST
	}
	return PostDoubleEntry(tenantID, "POSCart", checkoutID, debits, credits, "", fmt.Sprintf("POSCart:%s:SALE_GST", checkoutID))
}
