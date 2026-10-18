package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// Stage 73 / 55.10: part-settlement of receivables and payables, and credit
// notes that reduce the invoice they correct.
//
// Before this, a SalesInvoice could only be settled in full and a
// VendorInvoice only paid in full - a customer paying in two instalments, or a
// supplier paid half now and half later, had no way in. Now every receipt or
// payment is its own balanced posting with its own idempotency key, recorded
// on the invoice as a history line. The invoice stays Approved (receivable) or
// Matched (payable) while anything is left and turns Paid only when nothing
// is - no new status, so every screen, report and transition rule that reads
// those statuses keeps working.
//
// The rules that keep the ledger tied:
//
//   - Outstanding is measured, in paise, against what the ledger carries for
//     the invoice (the whole-rupee amount PostSalesInvoice / the GRN booked),
//     so the last receipt or payment takes the exact remainder and 1300, 2100
//     and 1500 clear to zero rather than to a paisa or two.
//   - A payable's debit is split between 2100 GRN Suspense and 1500 GST Input
//     Credit in proportion to what is still open on each. The split basis is
//     frozen at the first payment (payment_basis_paise), so a re-match that
//     recomputes gst_amount halfway through cannot move it.
//   - Foreign-currency invoices and bills settle in full only (FIN-0518). A
//     part-receipt would need the realised exchange difference apportioned per
//     receipt and the remaining carrying amount - and the period-end
//     revaluation state resolveSettlementFX reads - re-based after each one.
//     That is not done, so it is refused rather than booked wrongly.
//   - A TDS payment deducts on the whole bill, so a bill already part-paid is
//     refused there (FIN-0521) and finished with a plain payment instead.
//   - Retries: a caller-supplied reference makes a receipt or payment
//     idempotent. The same reference returns the line already recorded
//     without posting; the GL key <Doctype>:<id>:SETTLE|PAY:<reference> is the
//     second line of defence.

// salesInvoiceOutstandingSQL is an Approved SalesInvoice's open balance: the
// outstanding_amount a receipt or applied credit note recorded, or the whole
// total_amount when nothing has been received yet (every invoice before this
// stage). Receivables Ageing, its drill-down, its control totals, the credit
// limit check and dunning all read this one expression, so they age the same
// number the ledger carries in 1300.
const salesInvoiceOutstandingSQL = `COALESCE((data->>'outstanding_amount')::numeric, (data->>'total_amount')::numeric, 0)`

const (
	settlementKindReceipt    = "Receipt"
	settlementKindPayment    = "Payment"
	settlementKindPaymentTDS = "Payment (TDS)"
	settlementKindCreditNote = "Credit note"
)

// SettlementEntry is one line of an invoice's payment history - a
// SalesInvoice's "settlements", a VendorInvoice's "payments". Amount is in the
// invoice's own currency; FunctionalAmount is set only when that differs.
type SettlementEntry struct {
	Reference        string             `json:"reference"`
	Kind             string             `json:"kind"`
	Amount           float64            `json:"amount"`
	FunctionalAmount float64            `json:"functional_amount,omitempty"`
	Date             string             `json:"date"`
	By               string             `json:"by"`
	PostingKey       string             `json:"posting_key"`
	Legs             map[string]float64 `json:"legs,omitempty"`
	RecordedAt       string             `json:"recorded_at"`
}

// PartSettlementResult is what one receipt or payment did to its invoice.
type PartSettlementResult struct {
	ID                string  `json:"id"`
	Status            string  `json:"status"`
	Amount            float64 `json:"amount"`
	FunctionalAmount  float64 `json:"functional_amount"`
	SettledAmount     float64 `json:"settled_amount"`
	OutstandingAmount float64 `json:"outstanding_amount"`
	Reference         string  `json:"reference"`
	Replayed          bool    `json:"replayed,omitempty"`
}

// A reference becomes part of a GL idempotency key, so it is kept to a plain
// alphabet - no ':' (the key's own separator) and nothing that needs escaping.
var settlementReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,63}$`)

func validateSettlementInput(amount float64, reference string) error {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
		return &ValidationError{Code: "FIN-0517", Message: "the amount must be a positive number - leave it out to settle the whole outstanding balance"}
	}
	if reference != "" && !settlementReferencePattern.MatchString(reference) {
		return &ValidationError{Code: "FIN-0520", Message: fmt.Sprintf("reference %q is not valid: use up to 64 letters, digits, '.', '_', '/' or '-'", reference)}
	}
	return nil
}

// readSettlementEntries reads an invoice's history. A history that is present
// but unreadable is an error, never "nothing received yet" - reading it as
// empty would let the whole amount be received a second time.
func readSettlementEntries(data map[string]interface{}, field string) ([]SettlementEntry, error) {
	raw, ok := data[field]
	if !ok || raw == nil {
		return nil, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var entries []SettlementEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// findSettlementByReference finds a receipt/payment already recorded under a
// caller's reference. Credit-note lines are keyed by the note, not a caller
// reference, so they never answer a retry.
func findSettlementByReference(entries []SettlementEntry, reference string) (SettlementEntry, bool) {
	if reference == "" {
		return SettlementEntry{}, false
	}
	for _, e := range entries {
		if e.Kind != settlementKindCreditNote && e.Reference == reference {
			return e, true
		}
	}
	return SettlementEntry{}, false
}

func sumSettledPaise(entries []SettlementEntry) int64 {
	var total int64
	for _, e := range entries {
		total += RupeesToPaise(e.Amount)
	}
	return total
}

// nextAutoReference numbers a receipt the caller gave no reference for. It
// never reuses one already on the invoice, so the GL key it produces is new.
func nextAutoReference(entries []SettlementEntry) string {
	used := map[string]bool{}
	for _, e := range entries {
		used[e.Reference] = true
	}
	for n := len(entries) + 1; ; n++ {
		if ref := fmt.Sprintf("auto-%d", n); !used[ref] {
			return ref
		}
	}
}

// replaySettlement answers a retried receipt or payment from the line already
// recorded. The same reference for a different amount is refused: that is a
// second, different payment, and it needs a reference of its own.
func replaySettlement(res PartSettlementResult, prior SettlementEntry, amount float64, status string, data map[string]interface{}, settledField string) (PartSettlementResult, error) {
	if amount > 0 && RupeesToPaise(amount) != RupeesToPaise(prior.Amount) {
		return res, &ValidationError{Code: "FIN-0519", Message: fmt.Sprintf(
			"reference %s was already used for %.2f on this invoice; a different amount needs a new reference", prior.Reference, prior.Amount)}
	}
	res.Status = status
	res.Amount = prior.Amount
	res.FunctionalAmount = prior.FunctionalAmount
	if res.FunctionalAmount == 0 {
		res.FunctionalAmount = prior.Amount
	}
	res.SettledAmount = numFromInterface(data[settledField])
	res.OutstandingAmount = numFromInterface(data["outstanding_amount"])
	res.Reference = prior.Reference
	res.Replayed = true
	return res, nil
}

// postingError keeps a coded engine error (a closed period's FIN-0260) coded,
// and only wraps an uncoded one with what did not happen.
func postingError(err error, what string) error {
	if verr, ok := err.(*ValidationError); ok {
		return verr
	}
	return fmt.Errorf("GL posting failed, %s: %v", what, err)
}

func nothingOutstandingError(label, id string) error {
	return &ValidationError{Code: "FIN-0517", Message: fmt.Sprintf("%s %s has nothing outstanding", label, id)}
}

func overSettlementError(label, id string, pay, outstanding int64) error {
	return &ValidationError{Code: "FIN-0517", Message: fmt.Sprintf(
		"%.2f is more than the %.2f still outstanding on %s %s", PaiseToRupees(pay), PaiseToRupees(outstanding), label, id)}
}

// SettleSalesInvoiceAmount records a customer receipt against an Approved
// SalesInvoice: Dr 1100 Cash/Bank / Cr 1300 Accounts Receivable for exactly
// the amount received. amount 0 settles the whole outstanding balance. The
// invoice turns Paid when nothing is left. reference (optional, and what the
// screen always sends) makes the call idempotent: a retry with the same
// reference posts nothing and returns the receipt already recorded.
//
// A foreign-currency invoice settles in full only, with the realised exchange
// line exactly as SettleSalesInvoice always booked it.
func SettleSalesInvoiceAmount(tenantID, invoiceID, userID string, amount float64, reference string, opts ...SettlementOptions) (PartSettlementResult, error) {
	res := PartSettlementResult{ID: invoiceID}
	reference = strings.TrimSpace(reference)
	if err := validateSettlementInput(amount, reference); err != nil {
		return res, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return res, err
	}

	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'SalesInvoice' AND id = $1 FOR UPDATE`, schema),
		invoiceID).Scan(&dataStr, &status); err != nil {
		return res, fmt.Errorf("sales invoice not found: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return res, err
	}
	entries, err := readSettlementEntries(data, "settlements")
	if err != nil {
		return res, fmt.Errorf("sales invoice %s has an unreadable settlement history: %v", invoiceID, err)
	}
	if prior, ok := findSettlementByReference(entries, reference); ok {
		return replaySettlement(res, prior, amount, status, data, "settled_amount")
	}
	if status != "Approved" {
		return res, fmt.Errorf("only an Approved sales invoice can be settled (current status: %s)", status)
	}

	// The cash received is valued at the SETTLEMENT rate; the receivable being
	// cleared is what the ledger carries (booking rate plus any revaluation).
	// For a functional-currency invoice the two are the same number.
	position := documentFXPosition(tenantID, data, "total_amount")
	already := sumSettledPaise(entries)
	// Refused before any rate is looked up, so the reason given is the real one.
	if position.Foreign && (already > 0 || (amount > 0 && RupeesToPaise(amount) != RupeesToPaise(position.TransactionAmount))) {
		return res, &ValidationError{Code: "FIN-0518", Message: fmt.Sprintf(
			"sales invoice %s is in %s, so it can only be settled in full (%.2f %s)", invoiceID, position.Currency, position.TransactionAmount, position.Currency)}
	}
	settlement, err := resolveSettlementFX(tenantID, position, true, settlementOption(opts))
	if err != nil {
		return res, err
	}
	var debits, credits map[string]int64
	var postOpts []PostingOptions
	var pay, outstanding int64 // paise, in the invoice's own currency
	var functional float64
	if position.Foreign {
		outstanding = RupeesToPaise(position.TransactionAmount)
		pay = outstanding
		d := map[string]int{"1100": settlement.SettlementAmount}
		c := map[string]int{"1300": position.CarryingAmount}
		applyRealisedFXLine(d, c, settlement.RealisedGainLoss)
		debits, credits = PaiseMap(d), PaiseMap(c)
		// The cash and receivable lines carry the full foreign amount; the
		// realised gain/loss line has no foreign value by construction.
		postOpts = append(postOpts, postingOptionsFor(position, settlement.SettlementRate, d, c, map[string]float64{
			"1100": position.TransactionAmount,
			"1300": position.TransactionAmount,
		}))
		functional = float64(settlement.SettlementAmount)
	} else {
		outstanding = int64(position.CarryingAmount)*100 - already
		if outstanding <= 0 {
			return res, nothingOutstandingError("sales invoice", invoiceID)
		}
		pay = outstanding
		if amount > 0 {
			pay = RupeesToPaise(amount)
		}
		if pay <= 0 || pay > outstanding {
			return res, overSettlementError("sales invoice", invoiceID, pay, outstanding)
		}
		debits = map[string]int64{"1100": pay}
		credits = map[string]int64{"1300": pay}
		functional = PaiseToRupees(pay)
	}

	// A one-shot full settlement with no reference keeps the key it has always
	// had, so an invoice settled the old way and one settled now look the same
	// in the ledger. Everything else is keyed by its reference.
	oneShot := reference == "" && already == 0 && pay == outstanding
	key := fmt.Sprintf("SalesInvoice:%s:SETTLE", invoiceID)
	if !oneShot {
		if reference == "" {
			reference = nextAutoReference(entries)
		}
		key += ":" + reference
	}
	if err := PostDoubleEntryTx(tx, tenantID, schema, "SalesInvoice", invoiceID, debits, credits, settlement.PostingDate(), key, postOpts...); err != nil {
		return res, postingError(err, "receipt not recorded")
	}

	entry := SettlementEntry{
		Reference: reference, Kind: settlementKindReceipt, Amount: PaiseToRupees(pay),
		Date: settlement.SettlementDate, By: userID, PostingKey: key, RecordedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if position.Foreign {
		entry.FunctionalAmount = functional
	}
	entries = append(entries, entry)
	remaining := outstanding - pay
	data["settlements"] = entries
	data["settled_amount"] = PaiseToRupees(already + pay)
	data["outstanding_amount"] = PaiseToRupees(remaining)
	newStatus := "Approved"
	if remaining == 0 {
		newStatus = "Paid"
		recordSettlementFX(data, settlement)
	}
	data["status"] = newStatus
	updatedBytes, err := json.Marshal(data)
	if err != nil {
		return res, err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesInvoice' AND id = $3`, schema),
		updatedBytes, newStatus, invoiceID); err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	if oneShot {
		LogAuditEvent(tenantID, userID, "SETTLE_SALES_INVOICE", "SUCCESS", settlementAuditDetail("sales invoice", invoiceID, settlement))
	} else {
		LogAuditEvent(tenantID, userID, "SETTLE_SALES_INVOICE", "SUCCESS", fmt.Sprintf(
			"Received %.2f against sales invoice %s (reference %s); outstanding now %.2f", PaiseToRupees(pay), invoiceID, reference, PaiseToRupees(remaining)))
	}

	res.Status = newStatus
	res.Amount = PaiseToRupees(pay)
	res.FunctionalAmount = functional
	res.SettledAmount = PaiseToRupees(already + pay)
	res.OutstandingAmount = PaiseToRupees(remaining)
	res.Reference = reference
	return res, nil
}

// paiseLegs reads a stored {account: paise} map.
func paiseLegs(raw interface{}) map[string]int64 {
	out := map[string]int64{}
	if m, ok := raw.(map[string]interface{}); ok {
		for k, v := range m {
			out[k] = int64(math.Round(numFromInterface(v)))
		}
	}
	if m, ok := raw.(map[string]int64); ok {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func sumLegs(legs map[string]int64) int64 {
	var total int64
	for _, v := range legs {
		total += v
	}
	return total
}

// vendorPaymentBasis is the 2100 / 1500 split a bill clears in full, in paise:
// frozen on the bill at its first payment, otherwise exactly the debit legs
// the full-payment paths have always posted (inputGSTDebits).
func vendorPaymentBasis(data map[string]interface{}, position DocumentFXPosition) map[string]int64 {
	if stored := paiseLegs(data["payment_basis_paise"]); len(stored) > 0 {
		return stored
	}
	return PaiseMap(inputGSTDebits(data, position))
}

// vendorOpenLegs is what a bill still owes, leg by leg, in paise.
func vendorOpenLegs(data map[string]interface{}, position DocumentFXPosition) (open map[string]int64, total int64) {
	basis := vendorPaymentBasis(data, position)
	cleared := paiseLegs(data["payment_cleared_paise"])
	open = map[string]int64{}
	for account, v := range basis {
		if rest := v - cleared[account]; rest > 0 {
			open[account] = rest
			total += rest
		}
	}
	return open, total
}

// splitVendorPayment apportions a part-payment between the open 2100 and 1500
// legs in proportion to what is open on each. The payment that clears the
// bill takes the open legs exactly, so rounding never strands a paisa.
func splitVendorPayment(open map[string]int64, total, pay int64) map[string]int64 {
	legs := map[string]int64{}
	if pay >= total {
		for k, v := range open {
			legs[k] = v
		}
		return legs
	}
	gst := int64(math.Round(float64(pay) * float64(open["1500"]) / float64(total)))
	if gst > open["1500"] {
		gst = open["1500"]
	}
	if gst < 0 {
		gst = 0
	}
	goods := pay - gst
	if goods > open["2100"] {
		goods = open["2100"]
		gst = pay - goods
	}
	if goods > 0 {
		legs["2100"] = goods
	}
	if gst > 0 {
		legs["1500"] = gst
	}
	return legs
}

// noteVendorPayment records a payment on the bill's data - history line,
// cleared legs, paid and outstanding - and returns what is still outstanding
// in paise. legs are the 2100 / 1500 debits this payment cleared (never an FX
// line). Every payment path calls it, full or part, so the history is whole.
func noteVendorPayment(data map[string]interface{}, entries []SettlementEntry, kind, reference, key, userID, date string, legs map[string]int64, position DocumentFXPosition, functional float64) int64 {
	basis := vendorPaymentBasis(data, position)
	cleared := paiseLegs(data["payment_cleared_paise"])
	legRupees := map[string]float64{}
	for k, v := range legs {
		cleared[k] += v
		legRupees[k] = PaiseToRupees(v)
	}
	paid := sumLegs(cleared)
	outstanding := sumLegs(basis) - paid
	if outstanding < 0 {
		outstanding = 0
	}
	entry := SettlementEntry{
		Reference: reference, Kind: kind, Amount: PaiseToRupees(sumLegs(legs)), Legs: legRupees,
		Date: date, By: userID, PostingKey: key, RecordedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data["payment_basis_paise"] = basis
	data["payment_cleared_paise"] = cleared
	data["paid_amount"] = PaiseToRupees(paid)
	data["outstanding_amount"] = PaiseToRupees(outstanding)
	if position.Foreign {
		// Foreign bills are paid in full only, in their own currency.
		entry.Amount = position.TransactionAmount
		entry.FunctionalAmount = functional
		data["paid_amount"] = position.TransactionAmount
		data["outstanding_amount"] = 0
	}
	data["payments"] = append(entries, entry)
	return outstanding
}

// vendorFinalPayment is the posting that clears whatever a bill still owes,
// for the three full-payment paths (PayVendorInvoice, its override
// finalisation, and a payment proposal through PayVendorInvoice). A bill with
// no earlier payment gets the caller's own full-bill maps back untouched - the
// posting those paths have always made. A part-paid bill gets its open legs
// and their total instead.
type vendorFinalPayment struct {
	Debits, Credits map[string]int64
	Legs            map[string]int64
	Entries         []SettlementEntry
	PartPaid        bool
	Total           int64
}

func vendorFinalPaymentFor(invoiceID string, data map[string]interface{}, position DocumentFXPosition, debits, credits map[string]int) (vendorFinalPayment, error) {
	var out vendorFinalPayment
	entries, err := readSettlementEntries(data, "payments")
	if err != nil {
		return out, fmt.Errorf("vendor invoice %s has an unreadable payment history: %v", invoiceID, err)
	}
	out.Entries = entries
	if len(entries) == 0 {
		out.Debits, out.Credits = PaiseMap(debits), PaiseMap(credits)
		out.Legs = vendorPaymentBasis(data, position)
		out.Total = out.Credits["1100"]
		return out, nil
	}
	open, total := vendorOpenLegs(data, position)
	if total <= 0 {
		return out, nothingOutstandingError("vendor invoice", invoiceID)
	}
	out.Debits, out.Credits, out.Legs = open, map[string]int64{"1100": total}, open
	out.PartPaid, out.Total = true, total
	return out, nil
}

// PayVendorInvoiceAmount pays a Matched VendorInvoice in part or in full:
// Dr 2100 GRN Suspense and 1500 GST Input Credit (in proportion to what is
// open on each) / Cr 1100 Cash/Bank. amount 0 pays the whole outstanding
// balance. The bill stays Matched until nothing is left, then turns Paid.
// reference makes it idempotent, exactly as SettleSalesInvoiceAmount.
//
// Not for an unmatched bill - Override & Pay (PayVendorInvoice with a reason)
// stays a whole-bill, approval-routed payment. A foreign-currency bill pays in
// full only, with the realised exchange line PayVendorInvoice always booked.
func PayVendorInvoiceAmount(tenantID, invoiceID, userID string, amount float64, reference string, opts ...SettlementOptions) (PartSettlementResult, error) {
	res := PartSettlementResult{ID: invoiceID}
	reference = strings.TrimSpace(reference)
	if err := validateSettlementInput(amount, reference); err != nil {
		return res, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return res, err
	}

	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1 FOR UPDATE`, schema),
		invoiceID).Scan(&dataStr, &status); err != nil {
		return res, fmt.Errorf("vendor invoice not found: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return res, err
	}
	entries, err := readSettlementEntries(data, "payments")
	if err != nil {
		return res, fmt.Errorf("vendor invoice %s has an unreadable payment history: %v", invoiceID, err)
	}
	if prior, ok := findSettlementByReference(entries, reference); ok {
		return replaySettlement(res, prior, amount, status, data, "paid_amount")
	}
	if status == "Paid" {
		return res, fmt.Errorf("invoice is already Paid")
	}
	if status != "Matched" {
		return res, &ValidationError{Code: "VENDOR-0092", Message: fmt.Sprintf(
			"invoice is not Matched (status: %s) - match it first, or use Override & Pay for the whole bill", status)}
	}
	if numFromInterface(data["invoice_amount"]) <= 0 {
		return res, fmt.Errorf("invoice_amount must be positive to pay")
	}

	position := documentFXPosition(tenantID, data, "invoice_amount")
	if position.Foreign && (len(entries) > 0 || (amount > 0 && RupeesToPaise(amount) != RupeesToPaise(position.TransactionAmount))) {
		return res, &ValidationError{Code: "FIN-0518", Message: fmt.Sprintf(
			"vendor invoice %s is in %s, so it can only be paid in full (%.2f %s)", invoiceID, position.Currency, position.TransactionAmount, position.Currency)}
	}
	settlement, err := resolveSettlementFX(tenantID, position, false, settlementOption(opts))
	if err != nil {
		return res, err
	}
	var debits, credits, legs map[string]int64
	var postOpts []PostingOptions
	var pay, outstanding int64
	var functional float64
	if position.Foreign {
		outstanding = RupeesToPaise(position.TransactionAmount)
		pay = outstanding
		d := inputGSTDebits(data, position)
		legs = PaiseMap(d)
		c := map[string]int{"1100": settlement.SettlementAmount}
		applyRealisedFXLine(d, c, settlement.RealisedGainLoss)
		debits, credits = PaiseMap(d), PaiseMap(c)
		postOpts = append(postOpts, postingOptionsFor(position, settlement.SettlementRate, d, c, map[string]float64{
			"2100": position.TransactionAmount,
			"1100": position.TransactionAmount,
		}))
		functional = float64(settlement.SettlementAmount)
	} else {
		var open map[string]int64
		open, outstanding = vendorOpenLegs(data, position)
		if outstanding <= 0 {
			return res, nothingOutstandingError("vendor invoice", invoiceID)
		}
		pay = outstanding
		if amount > 0 {
			pay = RupeesToPaise(amount)
		}
		if pay <= 0 || pay > outstanding {
			return res, overSettlementError("vendor invoice", invoiceID, pay, outstanding)
		}
		legs = splitVendorPayment(open, outstanding, pay)
		debits = legs
		credits = map[string]int64{"1100": pay}
		functional = PaiseToRupees(pay)
	}

	oneShot := reference == "" && len(entries) == 0 && pay == outstanding
	key := fmt.Sprintf("VendorInvoice:%s:PAY", invoiceID)
	if !oneShot {
		if reference == "" {
			reference = nextAutoReference(entries)
		}
		key += ":" + reference
	}
	if err := PostDoubleEntryTx(tx, tenantID, schema, "VendorInvoice", invoiceID, debits, credits, settlement.PostingDate(), key, postOpts...); err != nil {
		return res, postingError(err, "payment not recorded")
	}
	remaining := noteVendorPayment(data, entries, settlementKindPayment, reference, key, userID, settlement.SettlementDate, legs, position, functional)
	newStatus := "Matched"
	if remaining == 0 || position.Foreign {
		newStatus = "Paid"
		recordSettlementFX(data, settlement)
	}
	data["status"] = newStatus
	updatedBytes, err := json.Marshal(data)
	if err != nil {
		return res, err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorInvoice' AND id = $3`, schema),
		updatedBytes, newStatus, invoiceID); err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	if oneShot {
		LogAuditEvent(tenantID, userID, "PAY_VENDOR_INVOICE", "SUCCESS", settlementAuditDetail("vendor invoice", invoiceID, settlement))
	} else {
		LogAuditEvent(tenantID, userID, "PAY_VENDOR_INVOICE", "SUCCESS", fmt.Sprintf(
			"Paid %.2f against vendor invoice %s (reference %s); outstanding now %.2f", PaiseToRupees(pay), invoiceID, reference, PaiseToRupees(remaining)))
	}

	res.Status = newStatus
	res.Amount = PaiseToRupees(pay)
	res.FunctionalAmount = functional
	res.SettledAmount = numFromInterface(data["paid_amount"])
	res.OutstandingAmount = numFromInterface(data["outstanding_amount"])
	res.Reference = reference
	return res, nil
}

// vendorInvoiceHasPayments reports whether any payment is recorded on a bill.
func vendorInvoiceHasPayments(data map[string]interface{}) bool {
	entries, err := readSettlementEntries(data, "payments")
	return err != nil || len(entries) > 0
}

// vendorInvoiceOpenAmount is what a bill still owes, for a payment proposal's
// total: its recorded outstanding once any payment exists, else the bill.
func vendorInvoiceOpenAmount(data map[string]interface{}) float64 {
	if v, ok := data["outstanding_amount"]; ok && v != nil {
		return numFromInterface(v)
	}
	return numFromInterface(data["invoice_amount"])
}

// applyCreditNoteToInvoice applies a CreditNote to the open SalesInvoice it
// names, inside the note's own transaction: the note first reduces what the
// customer still owes (Cr 1300), and only an amount beyond that is a cash
// refund (Cr 1100). Outstanding never goes below zero; at zero the invoice
// turns Paid. ok is false when the invoice is not open (not Approved, or not
// found) - a note against a Paid invoice is a straight refund, as it always
// was, and the caller posts that itself.
func applyCreditNoteToInvoice(tx *sql.Tx, schema, tenantID, noteID, invoiceID, userID string, notePaise int64, note map[string]interface{}) (applied, refunded int64, ok bool, err error) {
	var dataStr, status string
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'SalesInvoice' AND id = $1 FOR UPDATE`, schema),
		invoiceID).Scan(&dataStr, &status)
	if err == sql.ErrNoRows {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	if status != "Approved" {
		return 0, 0, false, nil
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, 0, false, err
	}
	position := documentFXPosition(tenantID, data, "total_amount")
	if position.Foreign {
		return 0, 0, false, &ValidationError{Code: "FIN-0522", Message: fmt.Sprintf(
			"credit note %s names sales invoice %s, which is in %s; settle that invoice in full instead, or post the note without the invoice as a refund", noteID, invoiceID, position.Currency)}
	}
	entries, err := readSettlementEntries(data, "settlements")
	if err != nil {
		return 0, 0, false, fmt.Errorf("sales invoice %s has an unreadable settlement history: %v", invoiceID, err)
	}
	already := sumSettledPaise(entries)
	outstanding := int64(position.CarryingAmount)*100 - already
	if outstanding <= 0 {
		return 0, 0, false, nil
	}
	applied = notePaise
	if applied > outstanding {
		applied = outstanding
	}
	refunded = notePaise - applied
	credits := map[string]int64{"1300": applied}
	if refunded > 0 {
		credits["1100"] = refunded
	}
	key := fmt.Sprintf("CreditNote:%s:POST", noteID)
	debits, err := creditNoteDebitsPaise(tx, schema, invoiceID, noteID, notePaise, note) // 73.D.10: GST back too
	if err != nil {
		return 0, 0, false, err
	}
	if err := PostDoubleEntryTx(tx, tenantID, schema, "CreditNote", noteID, debits, credits, "", key); err != nil {
		return 0, 0, false, postingError(err, "note not marked Posted")
	}
	entries = append(entries, SettlementEntry{
		Reference: noteID, Kind: settlementKindCreditNote, Amount: PaiseToRupees(applied),
		Date: time.Now().Format("2006-01-02"), By: userID, PostingKey: key, RecordedAt: time.Now().UTC().Format(time.RFC3339),
	})
	remaining := outstanding - applied
	data["settlements"] = entries
	data["settled_amount"] = PaiseToRupees(already + applied)
	data["outstanding_amount"] = PaiseToRupees(remaining)
	newStatus := "Approved"
	if remaining == 0 {
		newStatus = "Paid"
	}
	data["status"] = newStatus
	updatedBytes, err := json.Marshal(data)
	if err != nil {
		return 0, 0, false, err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesInvoice' AND id = $3`, schema),
		updatedBytes, newStatus, invoiceID); err != nil {
		return 0, 0, false, err
	}
	return applied, refunded, true, nil
}
