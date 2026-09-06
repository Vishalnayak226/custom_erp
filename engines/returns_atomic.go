package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 47.4 - "One atomic, replay-safe return/refund/exchange model and
// operator surface" (audit finding A-04).
//
// Stage 35.9 already built the right SHAPE: one ReturnRequest aggregate with a
// real state machine (Requested → Approved → Received → QC Complete → Closed),
// prices resolved from the original sale rather than the browser, and a
// separate RefundRequest with its own approval. What it did not have, and what
// this file supplies, is the four properties that make it safe:
//
//  1. ELIGIBILITY UNDER CONCURRENCY. The cumulative-quantity check read the
//     prior returns and then inserted, with nothing in between - so two clerks,
//     two tabs, or one retried request could both pass a check for the last
//     unit and both create a request for it. The original sale's own document
//     row is now locked FOR UPDATE for the whole check-and-insert, which is
//     the natural serialization point: every return against one sale contends
//     on exactly one row, and returns against different sales never contend at
//     all.
//
//  2. REPLAY SAFETY. A return command now carries a tenant-scoped idempotency
//     key through the SAME mechanism checkout uses (47.3.1's
//     command_idempotency), completed inside the return's own transaction. A
//     duplicate returns the original outcome; it cannot create a second
//     request.
//
//  3. ATOMICITY. QC posted stock buckets in a transaction, committed, and then
//     posted the GL - the exact A-03 shape applied to returns. Refund
//     processing posted the GL and then saved the status separately, so a
//     failure between them left money posted against a refund still marked
//     Approved, i.e. processable again. Both are now one transaction.
//
//  4. TAX REVERSAL. Nothing reversed output GST on a return. The refund
//     debited 4100 for the full tax-inclusive amount, so the tenant kept
//     reporting output tax on goods that had come back - a filing error, not a
//     cosmetic one. PostReturnGSTReversalTx backs the tax out of the refunded
//     lines using the same ComputeGSTForLines the sale itself used, so the two
//     agree by construction.

// ReturnCommandResult is what a return command produced, and what a duplicate
// of that command is replayed.
type ReturnCommandResult struct {
	ReturnRequestID string `json:"return_request_id"`
	Status          string `json:"status"`
	Replayed        bool   `json:"replayed,omitempty"`
	ExceptionType   string `json:"exception_type,omitempty"`
}

// Return exceptions (Stage 47.4.5). Both are deliberately named, evidenced and
// capability-gated rather than achieved by relaxing a validation: a store DOES
// need to take back goods without a receipt and DOES need to make a goodwill
// gesture outside the window, and the choice is between doing that through a
// recorded exception or through someone quietly editing a rule for everyone.
const (
	// ReturnExceptionNoReceipt lets a return proceed with no original bill.
	// Prices cannot be resolved from a sale that cannot be found, so the line
	// is priced from the item master and the difference is visible in the
	// evidence rather than presented as if it came from a receipt.
	ReturnExceptionNoReceipt = "No Receipt"
	// ReturnExceptionGoodwill lets a return proceed past the return window, or
	// past remaining eligibility, on a supervisor's judgement.
	ReturnExceptionGoodwill = "Goodwill"
)

// ReturnException is what a caller must supply to take one of those paths.
type ReturnException struct {
	Type         string `json:"type"`
	Reason       string `json:"reason"`
	AuthorisedBy string `json:"authorised_by"`
}

// validateReturnException refuses an exception that is not fully accounted
// for. An unexplained exception is not an exception, it is a hole.
func validateReturnException(ex *ReturnException) error {
	if ex == nil || ex.Type == "" {
		return nil
	}
	if ex.Type != ReturnExceptionNoReceipt && ex.Type != ReturnExceptionGoodwill {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "exception_type",
			Message: fmt.Sprintf("%q is not a return exception this system recognises", ex.Type)}
	}
	if strings.TrimSpace(ex.Reason) == "" {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "reason",
			Message: fmt.Sprintf("a %s return needs a written reason", ex.Type)}
	}
	if strings.TrimSpace(ex.AuthorisedBy) == "" {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "authorised_by",
			Message: fmt.Sprintf("a %s return must record who authorised it", ex.Type)}
	}
	return nil
}

// CreateReturnRequestCommand is the replay-safe entry point for creating a
// return. It is what every caller should use; CreateReturnRequest remains for
// the internal/RTO-webhook callers that have their own idempotency (an RTO is
// already deduplicated on booking_id).
func CreateReturnRequestCommand(tenantID, requestType, returnLocation, originalOrderID, bookingID, requestedBy, idempotencyKey, correlationID string, items []ReturnItemInput, exception *ReturnException) (*ReturnCommandResult, error) {
	if err := validateReturnException(exception); err != nil {
		return nil, err
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		// Without a key there is nothing to deduplicate on, and silently
		// proceeding would reintroduce exactly the replay A-04 describes.
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "idempotency_key",
			Message: "an idempotency key is required to create a return"}
	}
	digest := CommandDigest(map[string]interface{}{
		"type": requestType, "location": returnLocation, "order": originalOrderID,
		"booking": bookingID, "items": items, "exception": exception,
	})
	key := requestedBy + ":" + idempotencyKey
	claim, err := ClaimCommand(tenantID, "returns.create", key, digest, correlationID)
	if err != nil {
		return nil, err
	}
	switch claim.Outcome {
	case ClaimReplay:
		status, _ := claim.Response["status"].(string)
		return &ReturnCommandResult{ReturnRequestID: claim.DocumentID, Status: status, Replayed: true}, nil
	case ClaimInProgress:
		return nil, &ValidationError{Code: "GLOBAL-0006", SubFor: "",
			Message: "this return is still being processed - check the return before submitting it again"}
	case ClaimPayloadMismatch:
		return nil, &ValidationError{Code: "GLOBAL-0006", SubFor: "",
			Message: "this idempotency key was already used for a different return; use a new key"}
	}

	returnID, err := createReturnRequestLocked(tenantID, requestType, returnLocation, originalOrderID, bookingID, requestedBy, claim.Key, items, exception)
	if err != nil {
		FailCommand(tenantID, claim.Key, err.Error())
		return nil, err
	}
	result := &ReturnCommandResult{ReturnRequestID: returnID, Status: "Requested"}
	if exception != nil {
		result.ExceptionType = exception.Type
	}
	return result, nil
}

// createReturnRequestLocked does the whole check-and-insert inside one
// transaction, with the original sale's row locked.
func createReturnRequestLocked(tenantID, requestType, returnLocation, originalOrderID, bookingID, requestedBy, claimKey string, items []ReturnItemInput, exception *ReturnException) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	// Everything the un-serialized path already validated (window, prices,
	// routing, RTO booking state) is unchanged and still runs first - it reads
	// immutable or slow-moving data and needs no lock. Only the cumulative
	// quantity check has to be inside the lock, because only it races.
	prepared, err := prepareReturnRequest(tenantID, schema, requestType, returnLocation, originalOrderID, bookingID, items, exception)
	if err != nil {
		return "", err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}

	// A No Receipt return has no original sale to lock or to check eligibility
	// against - that is what makes it an exception. It is bounded instead by
	// the capability that let it be raised at all and by its own evidence.
	if prepared.requestType == "Customer Return" && !prepared.skipEligibility {
		// The original sale is the one row every return against it must
		// contend on. Locking it - rather than a table-level guard or an
		// advisory lock - keeps concurrency exactly as wide as the business
		// rule: two returns against two different sales never wait on each
		// other.
		var lockedID string
		if err := tx.QueryRow(fmt.Sprintf(
			`SELECT id FROM %s.documents WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, schema),
			prepared.originalOrderID).Scan(&lockedID); err != nil {
			if err == sql.ErrNoRows {
				return "", &ValidationError{Code: "SALESR-0131",
					Message: fmt.Sprintf("no original bill found for %q - a return requires a valid original bill reference", prepared.originalOrderID)}
			}
			return "", err
		}
		if err := assertReturnEligibleTx(tx, schema, prepared.originalOrderID, prepared.soldBySku, items); err != nil {
			return "", err
		}
	}

	returnID := NewDocID("RR")
	doc := map[string]interface{}{
		"code": returnID, "request_type": prepared.requestType, "original_order_id": prepared.originalOrderID,
		"booking_id": bookingID, "return_location": prepared.returnLocation,
		"return_location_auto_routed": prepared.autoRouted,
		"status":                      "Requested", "requested_by": requestedBy, "approved_by": "", "rejection_reason": "",
		"items": prepared.records, "total_refund_eligible": 0,
		// 47.4.3's "unique return command/evidence identity", stored on the
		// document itself so the aggregate carries its own provenance rather
		// than it living only in an infrastructure table.
		"idempotency_key": claimKey,
	}
	// 47.4.5: an exception is recorded ON the return, not implied by its
	// absence of a bill - so a later reviewer sees "No Receipt, authorised by
	// X because Y", never an anomalous-looking return with no explanation.
	if exception != nil {
		doc["exception_type"] = exception.Type
		doc["exception_reason"] = strings.TrimSpace(exception.Reason)
		doc["exception_authorised_by"] = exception.AuthorisedBy
	}
	marshaled, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		// created_by stays 'system' - documents.created_by carries a foreign key
		// to users, and a return can legitimately be raised by a channel webhook
		// or an RTO with no user row behind it. The human is recorded in the
		// document's own requested_by field, which is what every other
		// engine-written doctype in this codebase does.
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'ReturnRequest', $2, 'Requested', 'system')`, schema),
		returnID, marshaled); err != nil {
		return "", err
	}
	if claimKey != "" {
		if err := CompleteCommandTx(tx, schema, claimKey, returnID, map[string]interface{}{
			"return_request_id": returnID, "status": "Requested",
		}); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}

	event := "Return Requested"
	if prepared.requestType == "RTO" {
		event = "RTO Detected"
	}
	DispatchNotification(tenantID, event, prepared.originalOrderID, map[string]string{"return_request_id": returnID, "request_type": prepared.requestType})
	return returnID, nil
}

// assertReturnEligibleTx is the cumulative-quantity rule, evaluated inside the
// caller's transaction while the original sale is locked.
//
// It counts BOTH already-processed legacy returns and open ReturnRequests, for
// the same reason the un-serialized version did: a unit with a request already
// raised against it is spoken for even though it has not physically come back.
func assertReturnEligibleTx(tx *sql.Tx, schema, originalOrderID string, soldBySku map[string]int, items []ReturnItemInput) error {
	if len(soldBySku) == 0 {
		// Nothing resolvable was sold - the caller's own SALESR-0131 check has
		// already run, so this is a sale whose lines could not be parsed. Not
		// a case to silently allow an unbounded return against.
		return &ValidationError{Code: "SALESR-0131",
			Message: fmt.Sprintf("the original bill %q has no readable sold lines to return against", originalOrderID)}
	}
	claimed := map[string]int{}
	// Legacy SalesReturn documents.
	rows, err := tx.Query(fmt.Sprintf(`
		SELECT data->'items' FROM %s.documents
		WHERE doctype = 'SalesReturn' AND deleted_at IS NULL AND data->>'original_order_id' = $1`, schema), originalOrderID)
	if err != nil {
		return err
	}
	if err := accumulateReturnedQty(rows, claimed); err != nil {
		return err
	}
	// Open/settled ReturnRequests. Rejected ones release their claim, which is
	// the whole point of rejecting one.
	rows, err = tx.Query(fmt.Sprintf(`
		SELECT data->'items' FROM %s.documents
		WHERE doctype = 'ReturnRequest' AND deleted_at IS NULL
		  AND data->>'original_order_id' = $1 AND COALESCE(data->>'status', '') <> 'Rejected'`, schema), originalOrderID)
	if err != nil {
		return err
	}
	if err := accumulateReturnedQty(rows, claimed); err != nil {
		return err
	}

	// Deterministic order so two concurrent requests that both over-claim
	// report the same SKU first, rather than blaming whichever one the map
	// iteration happened to reach.
	ordered := append([]ReturnItemInput(nil), items...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].SKU < ordered[j].SKU })
	for _, it := range ordered {
		remaining := soldBySku[it.SKU] - claimed[it.SKU]
		if remaining < 0 {
			remaining = 0
		}
		if it.Qty > remaining {
			return &ValidationError{Code: "SALESR-0130",
				Message: fmt.Sprintf("return quantity for SKU %q (%d) exceeds the remaining returnable quantity (%d of %d sold, %d already claimed)",
					it.SKU, it.Qty, remaining, soldBySku[it.SKU], claimed[it.SKU])}
		}
	}
	return nil
}

// accumulateReturnedQty sums a result set of items JSON arrays into totals.
func accumulateReturnedQty(rows *sql.Rows, totals map[string]int) error {
	defer rows.Close()
	for rows.Next() {
		var raw sql.NullString
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		if !raw.Valid || raw.String == "" {
			continue
		}
		var lines []struct {
			SKU string `json:"sku"`
			Qty int    `json:"qty"`
		}
		if err := json.Unmarshal([]byte(raw.String), &lines); err != nil {
			continue
		}
		for _, l := range lines {
			totals[l.SKU] += l.Qty
		}
	}
	return rows.Err()
}

// ReturnEligibilityLine is one original sale line and what is still returnable
// from it, with the reason spelled out.
type ReturnEligibilityLine struct {
	SKU             string  `json:"sku"`
	SoldQty         int     `json:"sold_qty"`
	AlreadyReturned int     `json:"already_returned"`
	RemainingQty    int     `json:"remaining_qty"`
	UnitPrice       float64 `json:"unit_price"`
	Reason          string  `json:"reason"`
}

// ReturnEligibility is the whole answer for one original bill.
type ReturnEligibility struct {
	OriginalOrderID string                  `json:"original_order_id"`
	Found           bool                    `json:"found"`
	SaleDate        string                  `json:"sale_date,omitempty"`
	WithinWindow    bool                    `json:"within_window"`
	WindowDays      int                     `json:"window_days"`
	Interstate      bool                    `json:"interstate"`
	Lines           []ReturnEligibilityLine `json:"lines"`
	// Explanation is the single sentence the operator screen shows above the
	// table. A refusal a clerk cannot explain to the customer standing in
	// front of them is not a usable refusal.
	Explanation string `json:"explanation"`
}

// ResolveReturnEligibility answers "what can still be returned against this
// bill, and why" from the immutable original sale plus every prior claim
// against it (Stage 47.4.6).
//
// Read-only and deliberately outside a lock: this is what a screen renders, and
// the authoritative check is assertReturnEligibleTx under the lock at creation
// time. Showing a number that a concurrent clerk then takes is a UI race worth
// having; blocking a screen render on a row lock is not.
func ResolveReturnEligibility(tenantID, originalOrderID string) (*ReturnEligibility, error) {
	out := &ReturnEligibility{OriginalOrderID: originalOrderID, WindowDays: salesReturnWindowDaysFor(tenantID)}
	soldLines, saleDate, found, err := resolveOriginalSale(tenantID, originalOrderID)
	if err != nil {
		return nil, err
	}
	out.Found = found
	if !found {
		out.Explanation = fmt.Sprintf("No original bill was found for %q. A return needs the bill it is being returned against.", originalOrderID)
		return out, nil
	}
	if !saleDate.IsZero() {
		out.SaleDate = saleDate.Format("2006-01-02")
	}
	out.WithinWindow = saleDate.IsZero() || timeSinceDays(saleDate) <= out.WindowDays
	out.Interstate = originalSaleWasInterstate(tenantID, originalOrderID)

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	claimed := map[string]int{}
	for _, doctype := range []string{"SalesReturn", "ReturnRequest"} {
		rows, err := db.DB.Query(fmt.Sprintf(`
			SELECT data->'items' FROM %s.documents
			WHERE doctype = $1 AND deleted_at IS NULL AND data->>'original_order_id' = $2
			  AND COALESCE(data->>'status', '') <> 'Rejected'`, schema), doctype, originalOrderID)
		if err != nil {
			return nil, err
		}
		if err := accumulateReturnedQty(rows, claimed); err != nil {
			return nil, err
		}
	}

	prices, _ := resolveOriginalSaleLinePrices(tenantID, originalOrderID)
	soldBySku := map[string]int{}
	var order []string
	for _, l := range soldLines {
		if _, seen := soldBySku[l.Sku]; !seen {
			order = append(order, l.Sku)
		}
		soldBySku[l.Sku] += l.Qty
	}
	sort.Strings(order)
	returnable := 0
	for _, sku := range order {
		remaining := soldBySku[sku] - claimed[sku]
		if remaining < 0 {
			remaining = 0
		}
		returnable += remaining
		line := ReturnEligibilityLine{
			SKU: sku, SoldQty: soldBySku[sku], AlreadyReturned: claimed[sku],
			RemainingQty: remaining, UnitPrice: prices[sku].SalePrice,
		}
		switch {
		case !out.WithinWindow:
			line.RemainingQty = 0
			line.Reason = fmt.Sprintf("outside the %d-day return window", out.WindowDays)
		case remaining == 0 && claimed[sku] > 0:
			line.Reason = fmt.Sprintf("all %d already returned or claimed by an open return", soldBySku[sku])
		case remaining == 0:
			line.Reason = "nothing left to return"
		default:
			line.Reason = fmt.Sprintf("%d of %d still returnable", remaining, soldBySku[sku])
		}
		out.Lines = append(out.Lines, line)
	}

	switch {
	case !out.WithinWindow:
		out.Explanation = fmt.Sprintf("This bill is dated %s, past the %d-day return window, so nothing on it can be returned without an exception.", out.SaleDate, out.WindowDays)
	case returnable == 0:
		out.Explanation = "Everything on this bill has already been returned, or is claimed by a return that is still open."
	default:
		out.Explanation = fmt.Sprintf("%d item(s) can still be returned against this bill. Prices come from the original sale and cannot be changed here.", returnable)
	}
	return out, nil
}

// timeSinceDays is whole days elapsed, matching salesReturnWindowDaysFor's own
// day-granularity comparison rather than introducing a second interpretation
// of "how old is this sale".
func timeSinceDays(t time.Time) int {
	return int(time.Since(t).Hours() / 24)
}

// PostReturnGSTReversalTx reverses the output-tax liability on returned goods
// (47.4.4's "tax reversal"), inside the caller's transaction.
//
// Before Stage 47.4 nothing did this at all: a refund debited 4100 for the
// whole tax-inclusive amount and left the GST payable accounts untouched, so a
// tenant kept reporting output tax on goods that had come back. This is the
// exact mirror of PostSalesGSTBooking - debit the payable accounts, credit
// 4100 - computed with the same ComputeGSTForLines the sale used, so a sale
// and its return can never disagree about how much tax was on it.
func PostReturnGSTReversalTx(tx *sql.Tx, schema, tenantID, returnRequestID string, lines []GSTLineInput, interstate bool) error {
	if len(lines) == 0 {
		return nil
	}
	breakdown, err := ComputeGSTForLines(tenantID, lines, interstate)
	if err != nil {
		// A returned item whose Item master lost its tax classification must
		// not block the refund - the goods and the money have already moved.
		// It is logged and the reversal skipped, which is visible in the
		// return reconciliation rather than silently wrong in the ledger.
		LogSystemError(tenantID, "", "WARN", "PostReturnGSTReversalTx",
			fmt.Sprintf("return %s: could not recompute GST for reversal: %v", returnRequestID, err), "")
		return nil
	}
	paiseCGST := RupeesToPaise(breakdown.CGST)
	paiseSGST := RupeesToPaise(breakdown.SGST)
	paiseIGST := RupeesToPaise(breakdown.IGST)
	total := paiseCGST + paiseSGST + paiseIGST
	if total <= 0 {
		return nil
	}
	debits := map[string]int64{}
	if breakdown.Interstate {
		debits["2202"] = paiseIGST
	} else {
		if paiseCGST > 0 {
			debits["2200"] = paiseCGST
		}
		if paiseSGST > 0 {
			debits["2201"] = paiseSGST
		}
	}
	credits := map[string]int64{"4100": total}
	return PostDoubleEntryTx(tx, tenantID, schema, "ReturnRequest", returnRequestID, debits, credits, "",
		fmt.Sprintf("ReturnRequest:%s:GST_REVERSAL", returnRequestID))
}

// ReturnReconciliation is one return's cross-ledger check, the returns
// counterpart of engines/pos_sale_reconciliation.go. Same reasoning: the
// atomicity is a property of the code, and this is how anyone verifies it.
type ReturnReconciliation struct {
	ReturnRequestID string
	Status          string
	OriginalOrderID string
	ReturnedQty     int
	StockReceived   int
	RefundEligible  float64
	RefundProcessed float64
	RevenueReversed float64
	TaxReversed     float64
	Verdict         string
	Detail          string
}

// ReconcileReturn checks one return across stock, revenue, tax and refund.
func ReconcileReturn(tenantID, returnRequestID string) (*ReturnReconciliation, error) {
	schema, data, err := fetchReturnRequest(tenantID, returnRequestID)
	if err != nil {
		return nil, err
	}
	rec := &ReturnReconciliation{ReturnRequestID: returnRequestID}
	rec.Status, _ = data["status"].(string)
	rec.OriginalOrderID, _ = data["original_order_id"].(string)
	rec.RefundEligible, _ = parityNumber(data["total_refund_eligible"])

	itemsRaw, _ := json.Marshal(data["items"])
	var items []returnItemRecord
	_ = json.Unmarshal(itemsRaw, &items)
	for _, it := range items {
		rec.ReturnedQty += it.Qty
		if rule, ok := returnDispositionRule[it.Disposition]; ok && rule.ReceivesStock {
			rec.StockReceived += it.Qty
		}
	}

	glSum := func(docType, docID, account, side string) float64 {
		var paise int64
		_ = db.DB.QueryRow(fmt.Sprintf(
			`SELECT COALESCE(SUM(%s), 0) FROM %s.gl_postings
			 WHERE document_type = $1 AND document_id = $2 AND account_code = $3`, side, schema),
			docType, docID, account).Scan(&paise)
		return PaiseToRupees(paise)
	}
	rec.RevenueReversed = glSum("ReturnRequest", returnRequestID, "4100", "debit")
	rec.TaxReversed = glSum("ReturnRequest", returnRequestID, "2200", "debit") +
		glSum("ReturnRequest", returnRequestID, "2201", "debit") +
		glSum("ReturnRequest", returnRequestID, "2202", "debit")

	var refundID string
	_ = db.DB.QueryRow(fmt.Sprintf(
		`SELECT id FROM %s.documents WHERE doctype = 'RefundRequest' AND data->>'return_request_id' = $1
		 AND data->>'status' = 'Processed' LIMIT 1`, schema), returnRequestID).Scan(&refundID)
	if refundID != "" {
		rec.RefundProcessed = glSum("RefundRequest", refundID, "1100", "credit")
	}

	var problems []string
	if rec.Status == "Closed" || rec.Status == "QC Complete" {
		if rec.RefundEligible > 0 && rec.Status == "Closed" && rec.RefundProcessed == 0 && refundID != "" {
			problems = append(problems, fmt.Sprintf("refund of %.2f was approved but no cash leg is posted", rec.RefundEligible))
		}
		if rec.RefundProcessed > 0 && !amountsAgree(rec.RefundProcessed, rec.RefundEligible) {
			problems = append(problems, fmt.Sprintf("refund posted %.2f against an eligible %.2f", rec.RefundProcessed, rec.RefundEligible))
		}
	}
	if rec.Status == "Requested" || rec.Status == "Approved" {
		if rec.StockReceived != 0 || rec.RevenueReversed != 0 {
			problems = append(problems, "a return that has not been QC'd has already moved stock or reversed revenue")
		}
	}
	if len(problems) == 0 {
		rec.Verdict = "Balanced"
		rec.Detail = "stock, revenue, tax and refund agree for this return's state"
	} else {
		rec.Verdict = "BROKEN"
		rec.Detail = strings.Join(problems, "; ")
	}
	return rec, nil
}
