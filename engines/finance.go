package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"math"
	"strings"
)

// RupeesToPaise and PaiseToRupees (Stage 45) are the one rounding rule for
// every rupee<->paise conversion in the codebase. gl_postings.debit/credit
// store paise (int64, no fractional loss); every API/report response
// converts back to rupees at the boundary so the external contract - and
// public/app.js's existing money formatting - is unchanged. Converting
// straight from the original float (never from an already-truncated int)
// is what actually fixes the GST-truncation bug this migration exists for.
func RupeesToPaise(r float64) int64 {
	return int64(math.Round(r * 100))
}

func PaiseToRupees(p int64) float64 {
	return float64(p) / 100
}

// PaiseMap mechanically scales an already-whole-rupee debit/credit map to
// paise for PostDoubleEntry (Stage 45). For a subsystem whose internal
// arithmetic is deliberately kept in whole rupees - because it carries
// cross-period persisted state (FX revaluation's cumulative unrealised
// balance) or its inputs are already whole-rupee by the business domain
// (payroll, TDS, settlements) - reworking that arithmetic to invent
// fractional precision it never validated would be a bigger, riskier change
// than this migration's scope. RupeesToPaise is for a genuine float64
// source instead; use it when one exists this close to the posting.
func PaiseMap(rupees map[string]int) map[string]int64 {
	paise := make(map[string]int64, len(rupees))
	for k, v := range rupees {
		paise[k] = int64(v) * 100
	}
	return paise
}

// PostingOptions carries optional per-posting metadata that doesn't affect
// the debit/credit balance check itself (Stage 26.6.8).
type PostingOptions struct {
	CostCenter string
	Department string
	// Stage 37.1.2. Currency is the currency the source document was
	// transacted in and ExchangeRate is the rate that produced the
	// functional-currency debits/credits below. Both empty/zero means the
	// posting is already in the functional currency - which is every existing
	// caller, and why this needed no change at 28 call sites.
	//
	// The critical invariant: `debits` and `credits` are ALWAYS functional
	// currency. Every report, trial balance and reconciliation in this codebase
	// sums the debit/credit columns, and none of them will be taught about
	// currency. TransactionDebits/TransactionCredits carry the original amounts
	// alongside, for FX revaluation (37.1.4) and currency reporting (37.1.5).
	Currency           string
	ExchangeRate       float64
	TransactionDebits  map[string]float64
	TransactionCredits map[string]float64
	// Stage 37.2.1. Entity is a LegalEntity document id - another
	// whole-posting dimension, the same shape as CostCenter/Department. A
	// mirrored intercompany posting (engines/intercompany.go) is the reason
	// this exists: its two legs tag two DIFFERENT entities, which is why
	// they must be two separate PostDoubleEntry calls rather than one -
	// this field, like CostCenter/Department, applies to every line a
	// single call inserts.
	Entity string
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
func PostDoubleEntry(tenantID string, docType string, docID string, debits map[string]int64, credits map[string]int64, transactionDate string, postingKey string, opts ...PostingOptions) error {
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
	if err := validateLegalEntityReference(tenantID, opt.Entity); err != nil {
		return err
	}
	var costCenterArg, departmentArg, entityArg interface{}
	if opt.CostCenter != "" {
		costCenterArg = opt.CostCenter
	}
	if opt.Department != "" {
		departmentArg = opt.Department
	}
	if opt.Entity != "" {
		entityArg = opt.Entity
	}

	sumDebits := int64(0)
	for _, val := range debits {
		sumDebits += val
	}

	sumCredits := int64(0)
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

	// Stage 37.1.2: the currency columns. NULL for a functional-currency
	// posting, which keeps every pre-37.1.2 row and every single-currency
	// tenant's rows byte-identical to what they are today.
	currencyArg, rateArg := currencyPostingArgs(tenantID, opt)
	transactionAmount := func(source map[string]float64, code string, functional int64) interface{} {
		if currencyArg == nil {
			return nil
		}
		if value, ok := source[code]; ok {
			return value
		}
		// A caller that named a currency but not the per-account original is
		// telling us the rate, so the original is recoverable from it. Deriving
		// it is better than storing NULL and losing the transaction amount.
		if rate, ok := rateArg.(float64); ok && rate > 0 {
			return float64(functional) / rate
		}
		return nil
	}

	// Insert debits
	for code, val := range debits {
		if val <= 0 {
			continue
		}
		query := fmt.Sprintf(`
			INSERT INTO %s.gl_postings (account_code, debit, credit, document_type, document_id, idempotency_key, cost_center, department, currency, exchange_rate, transaction_debit, entity)
			VALUES ($1, $2, 0, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, schema)
		_, err := tx.Exec(query, code, val, docType, docID, keyArg, costCenterArg, departmentArg,
			currencyArg, rateArg, transactionAmount(opt.TransactionDebits, code, val), entityArg)
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
			INSERT INTO %s.gl_postings (account_code, debit, credit, document_type, document_id, idempotency_key, cost_center, department, currency, exchange_rate, transaction_credit, entity)
			VALUES ($1, 0, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, schema)
		_, err := tx.Exec(query, code, val, docType, docID, keyArg, costCenterArg, departmentArg,
			currencyArg, rateArg, transactionAmount(opt.TransactionCredits, code, val), entityArg)
		if err != nil {
			return fmt.Errorf("error posting credit for account %s: %v", code, err)
		}
	}

	return tx.Commit()
}

// currencyPostingArgs decides what goes in the currency columns. A posting is
// only marked foreign when the caller named a currency that genuinely differs
// from the tenant's functional currency - naming the functional currency
// explicitly is not an error, it just adds nothing, and writing it would make
// "is this a foreign posting?" a string comparison instead of a NULL check.
func currencyPostingArgs(tenantID string, opt PostingOptions) (currency interface{}, rate interface{}) {
	code := strings.ToUpper(strings.TrimSpace(opt.Currency))
	if code == "" || code == FunctionalCurrency(tenantID) {
		return nil, nil
	}
	appliedRate := opt.ExchangeRate
	if appliedRate <= 0 {
		appliedRate = 1
	}
	return code, appliedRate
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
// AccountBalance is one GetTrialBalance row - package-scoped (not declared
// inside the function) so Stage 37.2.5's GetConsolidatedTrialBalance can
// consume "balances" back out of the map by its real type instead of
// re-querying the identical SQL a second time.
type AccountBalance struct {
	Code   string  `json:"account_code"`
	Name   string  `json:"account_name"`
	Type   string  `json:"account_type"`
	Debit  float64 `json:"debit"`
	Credit float64 `json:"credit"`
}

// FinancialReportFilter (Stage 37.5.1) narrows a statement to one dimension
// value - the same whole-posting dimensions PostingOptions already carries
// (CostCenter/Department Stage 26.6.8, Entity Stage 37.2.1). Variadic on
// every function below, the PostingOptions/JournalVoucherOptions precedent,
// so GetConsolidatedTrialBalance's own existing call (Stage 37.2.5) and
// every other pre-37.5 caller need zero changes. At most one field is
// expected to be set per call - combining two is accepted but answers a
// narrower question ("this cost center AND this entity"), never an error.
type FinancialReportFilter struct {
	CostCenter string
	Department string
	Entity     string
}

// dimensionJoinClause turns a filter into an extra `AND` clause plus its
// bind arg, appended inside a LEFT JOIN's own ON condition (never a WHERE)
// so a filtered statement still lists every account at zero rather than
// silently dropping any account with no matching posting - a WHERE on the
// joined side would turn the LEFT JOIN into an inner join for exactly the
// accounts a statement most needs to show as zero.
func dimensionJoinClause(filter FinancialReportFilter, nextArgPos int) (clause string, arg interface{}) {
	switch {
	case filter.CostCenter != "":
		return fmt.Sprintf(" AND p.cost_center = $%d", nextArgPos), filter.CostCenter
	case filter.Department != "":
		return fmt.Sprintf(" AND p.department = $%d", nextArgPos), filter.Department
	case filter.Entity != "":
		return fmt.Sprintf(" AND p.entity = $%d", nextArgPos), filter.Entity
	default:
		return "", nil
	}
}

func GetTrialBalance(tenantID, asOfDate string, filters ...FinancialReportFilter) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if asOfDate == "" {
		return nil, &ValidationError{Code: "GLOBAL-0001", SubFor: "As Of Date", Message: "as_of date is required for a trial balance"}
	}
	var filter FinancialReportFilter
	if len(filters) > 0 {
		filter = filters[0]
	}
	dimClause, dimArg := dimensionJoinClause(filter, 2)
	args := []interface{}{asOfDate}
	if dimArg != nil {
		args = append(args, dimArg)
	}

	query := fmt.Sprintf(`
		SELECT a.account_code, a.account_name, a.account_type,
		       COALESCE(SUM(p.debit), 0) as total_debit,
		       COALESCE(SUM(p.credit), 0) as total_credit
		FROM %s.gl_accounts a
		LEFT JOIN %s.gl_postings p ON a.account_code = p.account_code AND p.created_at < ($1::date + 1)%s
		GROUP BY a.account_code, a.account_name, a.account_type
		ORDER BY a.account_code`, schema, schema, dimClause)

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type accountBalancePaise struct {
		Code   string
		Name   string
		Type   string
		Debit  int64
		Credit int64
	}

	var balances []AccountBalance
	totalDebitsPaise := int64(0)
	totalCreditsPaise := int64(0)

	for rows.Next() {
		var b accountBalancePaise
		err := rows.Scan(&b.Code, &b.Name, &b.Type, &b.Debit, &b.Credit)
		if err != nil {
			return nil, err
		}
		totalDebitsPaise += b.Debit
		totalCreditsPaise += b.Credit
		balances = append(balances, AccountBalance{
			Code: b.Code, Name: b.Name, Type: b.Type,
			Debit: PaiseToRupees(b.Debit), Credit: PaiseToRupees(b.Credit),
		})
	}

	totalDebits := PaiseToRupees(totalDebitsPaise)
	totalCredits := PaiseToRupees(totalCreditsPaise)
	balanced := totalDebitsPaise == totalCreditsPaise
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

// PostGRNFinanceBooking creates dynamic financial postings for warehouse
// receiving. amount is rupees; converted to paise at this boundary since
// this function has no caller anywhere in the repo today (dead code) and so
// no upstream float to convert from instead.
func PostGRNFinanceBooking(tenantID string, grnID string, amount int) error {
	if amount <= 0 {
		return errors.New("GRN transaction value must be positive")
	}

	amountPaise := RupeesToPaise(float64(amount))
	debits := map[string]int64{"1200": amountPaise}  // Debit: Inventory Control Account
	credits := map[string]int64{"2100": amountPaise} // Credit: GRN Suspense Account

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
// loyaltyDiscount (Stage 30.2.5) is the paise value of loyalty points the
// customer paid with. Revenue is still credited at the full sale value - the
// goods were sold for that price, and the GST posting on top of this one is
// computed on that same value - but the debit side splits: only the cash
// actually collected hits the payment clearing account, and the points portion
// hits 5250 (Loyalty Points Redeemed), where the cost of the loyalty programme
// belongs. Pass 0 for a sale with no redemption and the postings are
// byte-for-byte what they always were.
//
// salePrice/costPrice/loyaltyDiscount are paise (Stage 45), not rupees - the
// caller is expected to convert from its own float64 rupee amount via
// RupeesToPaise before calling, so precision survives from the original
// float all the way into the ledger instead of being floor-truncated here.
func PostSalesFinanceBooking(tenantID string, checkoutID string, salePrice int64, costPrice int64, paymentMode string, loyaltyDiscount int64) error {
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
	revenueDebits := map[string]int64{}
	if cash := salePrice - loyaltyDiscount; cash > 0 {
		revenueDebits[paymentModeClearingAccount(paymentMode)] = cash // Debit: Cash/Card/UPI clearing account
	}
	if loyaltyDiscount > 0 {
		revenueDebits["5250"] = loyaltyDiscount // Debit: Loyalty Points Redeemed (5250)
	}
	revenueCredits := map[string]int64{"4100": salePrice} // Credit: Sales Revenue Account
	err := PostDoubleEntry(tenantID, "POSCart", checkoutID, revenueDebits, revenueCredits, "", fmt.Sprintf("POSCart:%s:SALE_REVENUE", checkoutID))
	if err != nil {
		return err
	}

	// 2. Post COGS / Inventory Bookings
	cogsDebits := map[string]int64{"5100": costPrice}  // Debit: Cost of Goods Sold Account
	cogsCredits := map[string]int64{"1200": costPrice} // Credit: Inventory Control Account
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
	// Round each component to paise first, then sum those - not the other way
	// around - so the debit side below always exactly matches what the
	// credit side actually posts (independent per-component rounding could
	// otherwise leave the two off by a paisa and fail PostDoubleEntry's
	// balance check). Stage 45: paise via RupeesToPaise, not int() truncation
	// - this is the exact spot the durability audit's finding #7 named as
	// where GST output-tax liability was silently understated.
	paiseCGST := RupeesToPaise(breakdown.CGST)
	paiseSGST := RupeesToPaise(breakdown.SGST)
	paiseIGST := RupeesToPaise(breakdown.IGST)
	totalTax := paiseCGST + paiseSGST + paiseIGST
	if totalTax <= 0 {
		return nil
	}
	debits := map[string]int64{"4100": totalTax}
	credits := map[string]int64{}
	if breakdown.Interstate {
		credits["2202"] = paiseIGST // GST Output Payable - IGST
	} else {
		credits["2200"] = paiseCGST // GST Output Payable - CGST
		credits["2201"] = paiseSGST // GST Output Payable - SGST
	}
	return PostDoubleEntry(tenantID, "POSCart", checkoutID, debits, credits, "", fmt.Sprintf("POSCart:%s:SALE_GST", checkoutID))
}

// PostExemptSalesReclass (Stage 26.6.11) is the non-taxable counterpart of
// PostSalesGSTBooking above. That one leaves 4100 holding the taxable value by
// moving the tax portion out; this one leaves 4100 holding ONLY taxable value
// by moving exempt/nil-rated/zero-rated turnover out into its own revenue
// account.
//
// Without it, an exempt sale would sit in 4100 untouched - no tax to move -
// and GetGSTReturnSummary, which reads 4100's net balance as the period's
// taxable value, would report exempt turnover as taxable: GSTR-3B 3.1(a)
// overstated and 3.1(c) understated by the same amount. Total revenue is
// unaffected either way; this only reclassifies within Revenue, which is why
// it is safe to run after the sale is already booked.
//
// Three destination accounts rather than one, because the returns do not
// accept them merged: GSTR-1's nil/exempt/non-GST table has a column each for
// nil-rated and exempt, and GSTR-3B reports zero-rated in 3.1(b) separately
// from exempt+nil in 3.1(c).
func PostExemptSalesReclass(tenantID, checkoutID string, breakdown GSTBreakdown) error {
	// Round each bucket to paise first and sum those, for the same reason
	// PostSalesGSTBooking does: summing first and rounding after can leave
	// the debit a paisa off the credits and fail PostDoubleEntry's balance
	// check.
	paiseExempt := RupeesToPaise(breakdown.ExemptAmount)
	paiseNilRated := RupeesToPaise(breakdown.NilRatedAmount)
	paiseZeroRated := RupeesToPaise(breakdown.ZeroRatedAmount)
	total := paiseExempt + paiseNilRated + paiseZeroRated
	if total <= 0 {
		return nil
	}
	debits := map[string]int64{"4100": total}
	credits := map[string]int64{}
	if paiseExempt > 0 {
		credits["4110"] = paiseExempt // Exempt Sales Revenue
	}
	if paiseNilRated > 0 {
		credits["4111"] = paiseNilRated // Nil-Rated Sales Revenue
	}
	if paiseZeroRated > 0 {
		credits["4112"] = paiseZeroRated // Zero-Rated Sales Revenue
	}
	return PostDoubleEntry(tenantID, "POSCart", checkoutID, debits, credits, "", fmt.Sprintf("POSCart:%s:SALE_EXEMPT", checkoutID))
}
