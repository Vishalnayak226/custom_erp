package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
)

// PostingOptions carries optional per-posting metadata that doesn't affect
// the debit/credit balance check itself (Stage 26.6.8).
type PostingOptions struct {
	CostCenter string
	Department string
}

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
//
// opts (Stage 26.6.8, variadic so every one of this function's 28
// pre-existing call sites needs zero changes) optionally tags every row
// this call inserts with one cost_center/department - a whole-posting
// dimension, not per-line (this function's debits/credits maps already
// aggregate by account_code, so a per-line dimension isn't representable
// without restructuring that aggregation). Only the first element is used.
func PostDoubleEntry(tenantID string, docType string, docID string, debits map[string]int, credits map[string]int, transactionDate string, postingKey string, opts ...PostingOptions) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var opt PostingOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if err := validateCostCenterReference(tenantID, opt.CostCenter); err != nil {
		return err
	}
	if err := validateDepartmentReference(tenantID, opt.Department); err != nil {
		return err
	}
	var costCenterArg, departmentArg interface{}
	if opt.CostCenter != "" {
		costCenterArg = opt.CostCenter
	}
	if opt.Department != "" {
		departmentArg = opt.Department
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

	if err := rejectIfCurrentPeriodClosed(tx, schema, docType, docID, transactionDate); err != nil {
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
			INSERT INTO %s.gl_postings (account_code, debit, credit, document_type, document_id, idempotency_key, cost_center, department)
			VALUES ($1, $2, 0, $3, $4, $5, $6, $7)`, schema)
		_, err := tx.Exec(query, code, val, docType, docID, keyArg, costCenterArg, departmentArg)
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
			INSERT INTO %s.gl_postings (account_code, debit, credit, document_type, document_id, idempotency_key, cost_center, department)
			VALUES ($1, 0, $2, $3, $4, $5, $6, $7)`, schema)
		_, err := tx.Exec(query, code, val, docType, docID, keyArg, costCenterArg, departmentArg)
		if err != nil {
			return fmt.Errorf("error posting credit for account %s: %v", code, err)
		}
	}

	return tx.Commit()
}

// GetTrialBalance fetches summary trial balances for the current tenant
// accounts as of a single date - every gl_posting up to and including
// asOfDate, which is how a trial balance is conventionally scoped and matches
// GetBalanceSheet's own parameter shape.
//
// asOfDate is mandatory (Stage 29.7.4). Before that this summed the *entire*
// ledger with no date filter, so it had to touch every posting by definition
// and no index could help it - the QC report's "add an index" framing of O1
// was wrong on this specific query, because the query itself was the problem.
// The predicate is spelled as the half-open `created_at < ($1::date + 1)`
// rather than `created_at::date <= $1`, per the convention documented at the
// top of finance_reports_stage26.go: a cast wraps the indexed column in a
// function call, which can never be a range seek, so the sargable spelling is
// what actually lets idx_gl_postings_account_created serve this.
func GetTrialBalance(tenantID, asOfDate string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if asOfDate == "" {
		return nil, &ValidationError{Code: "GLOBAL-0001", SubFor: "As Of Date", Message: "as_of date is required for a trial balance"}
	}

	query := fmt.Sprintf(`
		SELECT a.account_code, a.account_name, a.account_type,
		       COALESCE(SUM(p.debit), 0) as total_debit,
		       COALESCE(SUM(p.credit), 0) as total_credit
		FROM %s.gl_accounts a
		LEFT JOIN %s.gl_postings p ON a.account_code = p.account_code AND p.created_at < ($1::date + 1)
		GROUP BY a.account_code, a.account_name, a.account_type
		ORDER BY a.account_code`, schema, schema)

	rows, err := db.DB.Query(query, asOfDate)
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
		"as_of":         asOfDate,
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
// loyaltyDiscount (Stage 30.2.5) is the rupee value of loyalty points the
// customer paid with. Revenue is still credited at the full sale value - the
// goods were sold for that price, and the GST posting on top of this one is
// computed on that same value - but the debit side splits: only the cash
// actually collected hits the payment clearing account, and the points portion
// hits 5250 (Loyalty Points Redeemed), where the cost of the loyalty programme
// belongs. Pass 0 for a sale with no redemption and the postings are
// byte-for-byte what they always were.
func PostSalesFinanceBooking(tenantID string, checkoutID string, salePrice int, costPrice int, paymentMode string, loyaltyDiscount int) error {
	if salePrice <= 0 || costPrice <= 0 {
		return errors.New("sales and cost prices must be positive")
	}
	if loyaltyDiscount < 0 {
		return errors.New("loyalty discount cannot be negative")
	}
	if loyaltyDiscount > salePrice {
		// Points can cover a whole sale, never more than one - otherwise the
		// clearing-account debit below would go negative and the customer
		// would in effect be paid to shop.
		return fmt.Errorf("loyalty discount (%d) cannot exceed the sale value (%d)", loyaltyDiscount, salePrice)
	}

	// 1. Post Revenue Bookings
	revenueDebits := map[string]int{}
	if cash := salePrice - loyaltyDiscount; cash > 0 {
		revenueDebits[paymentModeClearingAccount(paymentMode)] = cash // Debit: Cash/Card/UPI clearing account
	}
	if loyaltyDiscount > 0 {
		revenueDebits["5250"] = loyaltyDiscount // Debit: Loyalty Points Redeemed (5250)
	}
	revenueCredits := map[string]int{"4100": salePrice} // Credit: Sales Revenue Account
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
