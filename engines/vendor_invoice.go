package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"math"
)

// defaultVendorInvoiceTolerancePercentFor resolves the tenant's configured
// 3-way-match tolerance (Stage 30.7 - was a hardcoded 2.0). Still only the
// fallback for an explicit caller-supplied tolerance, exactly as before.
func defaultVendorInvoiceTolerancePercentFor(tenantID string) float64 {
	return GetSettingFloat(tenantID, "procurement.vendor_invoice_tolerance_percent")
}

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
		tolerancePercent = defaultVendorInvoiceTolerancePercentFor(tenantID)
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
		// 24.18: read-only below; a malformed poItemsStr degrades to
		// rateBySku staying empty, which rateDataMissing (below) already
		// treats as a real, handled case (skip the GRN-value cross-check,
		// match on PO delta alone) - logged so that degrade path is
		// traceable instead of silent.
		if err := json.Unmarshal([]byte(poItemsStr), &poItems); err != nil {
			log.Printf("[VENDOR-INVOICE] corrupt PO items for %s: %v", poID, err)
		}
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
		// 24.18: a malformed receivedStr leaves receivedItems empty, so
		// grnValue stays 0 - grnDelta then reads as the full invoiceAmount,
		// which fails the tolerance check (MismatchHold) rather than a
		// false-positive Matched. Fails closed either way; logged so the
		// underlying corruption is traceable rather than showing up only
		// as an unexplained mismatch.
		if err := json.Unmarshal([]byte(receivedStr), &receivedItems); err != nil {
			log.Printf("[VENDOR-INVOICE] corrupt GRN received_items for %s: %v", grnID, err)
		}
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
// from status Matched directly.
//
// 24.11: an override (paying a non-Matched invoice) no longer pays
// immediately just because a non-empty overrideReason string was supplied -
// that was an audited bypass (the reason gets logged), but still a
// unilateral one: any caller who typed any reason string could skip the
// match requirement on their own say-so. It now routes through the same
// maker-checker approval engine PurchaseOrder (Stage 13.8), the POS
// discount gate (Stage 20.10), and cycle-count variance (Stage 20.22)
// already reuse - a third reuse of the established pattern, no new approval
// mechanism. actorRole is the caller's own role (for the approval_log
// entry), separate from whatever role ends up required to decide it.
//
// This claims the invoice as Pending Approval with direct SQL inside this
// function's own transaction (see the inline comment at that call site for
// why), rather than calling SubmitForApproval - that function requires the
// document to already be status "Draft", but a VendorInvoice needing an
// override arrives here from Match3Way's MismatchHold, never Draft.
//
// paidAmount is 0 and pendingApproval is true when an override was claimed
// for approval instead of paid; handleDecideApproval finalizes the actual
// payment (FinalizeVendorInvoiceOverridePayment) only once a checker
// Approves it.
// Stage 37.1.3 adds the realised FX leg. opts is variadic so all three existing
// callers needed no change.
func PayVendorInvoice(tenantID, invoiceID, userID, actorRole, overrideReason string, opts ...SettlementOptions) (paidAmount int, pendingApproval bool, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, false, err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return 0, false, err
	}

	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1 FOR UPDATE`, schema),
		invoiceID).Scan(&dataStr, &status); err != nil {
		return 0, false, fmt.Errorf("vendor invoice not found: %v", err)
	}
	if status == "Paid" {
		return 0, false, fmt.Errorf("invoice is already Paid")
	}
	if status == "Pending Approval" {
		return 0, false, fmt.Errorf("invoice is already pending an override approval decision")
	}
	if status != "Matched" && overrideReason == "" {
		return 0, false, &ValidationError{Code: "VENDOR-0092", Message: fmt.Sprintf("invoice is not Matched (status: %s) - pay requires an explicit override_reason", status)}
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, false, err
	}
	amount, _ := data["invoice_amount"].(float64)
	if amount <= 0 {
		return 0, false, fmt.Errorf("invoice_amount must be positive to pay")
	}
	amountInt := int(amount)

	if status != "Matched" {
		requiredRole, rerr := RequiredApproverRoleForAmount(tenantID, "VendorInvoice", amount)
		if rerr != nil {
			return 0, false, rerr
		}
		if requiredRole == "" {
			return 0, false, fmt.Errorf("override requires an approval_rules entry for VendorInvoice - none configured")
		}
		data["payment_override_reason"] = overrideReason
		data["override_from_status"] = status
		data["status"] = "Pending Approval"
		updatedBytes, err := json.Marshal(data)
		if err != nil {
			return 0, false, err
		}
		// Written directly via tx (not setDocumentStatus/logApprovalAction,
		// which run on a separate pooled connection via db.DB) - this
		// function is still holding the FOR UPDATE row lock taken above in
		// this same tx, and a second connection trying to update that same
		// locked row would block waiting for a commit this code is itself
		// waiting to reach, deadlocking. created_by is also reassigned to
		// the override requester (userID): this document already existed
		// before this call (unlike POSCart's Stage 20.10 gate, which claims
		// a brand-new row), so the maker-checker check in DecideApproval
		// (which compares against documents.created_by) must bind to
		// whoever is actually requesting *this* override, not whoever
		// originally created the invoice record - otherwise the override
		// requester could approve their own override as long as they
		// weren't the original invoice creator.
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE %s.documents SET data = $1, status = $2, created_by = $3, updated_at = CURRENT_TIMESTAMP
			WHERE doctype = 'VendorInvoice' AND id = $4`, schema), updatedBytes, "Pending Approval", userID, invoiceID); err != nil {
			return 0, false, err
		}
		if _, err := tx.Exec(fmt.Sprintf(`
			INSERT INTO %s.approval_log (doctype, document_id, action, actor_user_id, actor_role, amount, comment)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, schema), "VendorInvoice", invoiceID, "Submitted", userID, actorRole, amount, overrideReason); err != nil {
			return 0, false, err
		}
		if err := tx.Commit(); err != nil {
			return 0, false, err
		}
		LogAuditEvent(tenantID, userID, "PAY_VENDOR_INVOICE_OVERRIDE_SUBMITTED", "SUCCESS",
			fmt.Sprintf("Vendor invoice %s override routed for %s approval (was %s): %s", invoiceID, requiredRole, status, overrideReason))
		return 0, true, nil
	}

	// GL posted before the status flip is committed below - PostDoubleEntry
	// manages its own transaction (can't be nested into this one), so the
	// row lock above is held across the call to prevent a concurrent second
	// pay attempt on the same invoice, and a GL failure here leaves the
	// invoice's status untouched (this tx is rolled back via defer) rather
	// than marking it Paid with no posting behind it.
	// Stage 37.1.3. The liability is cleared at what the ledger carries for it;
	// the cash leaving the bank is the invoice amount at the payment-date rate.
	// A payable settled after the foreign currency strengthened costs more
	// rupees than it was booked at, and that excess is a realised LOSS - the
	// opposite sign to the receivable side, which is why resolveSettlementFX is
	// told which side it is on rather than inferring it.
	position := documentFXPosition(tenantID, data, "invoice_amount")
	settlement, ferr := resolveSettlementFX(tenantID, position, false, settlementOption(opts))
	if ferr != nil {
		return 0, false, ferr
	}
	amountInt = settlement.SettlementAmount

	debits := map[string]int{"2100": position.CarryingAmount}      // clear GRN Suspense liability
	credits := map[string]int{"1100": settlement.SettlementAmount} // Cash/Bank paid out
	applyRealisedFXLine(debits, credits, settlement.RealisedGainLoss)
	if err := PostDoubleEntry(tenantID, "VendorInvoice", invoiceID, PaiseMap(debits), PaiseMap(credits), settlement.PostingDate(), fmt.Sprintf("VendorInvoice:%s:PAY", invoiceID),
		postingOptionsFor(position, settlement.SettlementRate, debits, credits, map[string]float64{
			"2100": position.TransactionAmount,
			"1100": position.TransactionAmount,
		})); err != nil {
		return 0, false, fmt.Errorf("GL posting failed, invoice not marked Paid: %v", err)
	}
	recordSettlementFX(data, settlement)

	data["status"] = "Paid"
	updatedBytes, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Paid', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorInvoice' AND id = $2`, schema),
		updatedBytes, invoiceID); err != nil {
		return 0, false, err
	}

	if err := tx.Commit(); err != nil {
		return 0, false, err
	}

	LogAuditEvent(tenantID, userID, "PAY_VENDOR_INVOICE", "SUCCESS", settlementAuditDetail("vendor invoice", invoiceID, settlement))
	return amountInt, false, nil
}

// FinalizeVendorInvoiceOverridePayment (24.11) is called from
// handleDecideApproval once a checker Approves a VendorInvoice override
// claimed as Pending Approval by PayVendorInvoice above - the actual GL
// posting and Paid transition never happened at submit time, same
// finalize-on-approve shape as FinalizePOSCheckout (Stage 20.10) and
// PostCycleCountAdjustment (Stage 20.22).
func FinalizeVendorInvoiceOverridePayment(tenantID, invoiceID, userID string, opts ...SettlementOptions) (paidAmount int, err error) {
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
	if status != "Approved" {
		return 0, fmt.Errorf("invoice override was not Approved (current status: %s)", status)
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, err
	}
	// Stage 37.1.3, identical treatment to PayVendorInvoice's own leg. The
	// override path posts on approval rather than on submission, so the rate
	// that applies is the one on the day the money actually leaves - which is
	// today, here, not the day the override was requested.
	position := documentFXPosition(tenantID, data, "invoice_amount")
	settlement, ferr := resolveSettlementFX(tenantID, position, false, settlementOption(opts))
	if ferr != nil {
		return 0, ferr
	}
	amountInt := settlement.SettlementAmount

	debits := map[string]int{"2100": position.CarryingAmount}
	credits := map[string]int{"1100": settlement.SettlementAmount}
	applyRealisedFXLine(debits, credits, settlement.RealisedGainLoss)
	if err := PostDoubleEntry(tenantID, "VendorInvoice", invoiceID, PaiseMap(debits), PaiseMap(credits), settlement.PostingDate(), fmt.Sprintf("VendorInvoice:%s:PAY_OVERRIDE", invoiceID),
		postingOptionsFor(position, settlement.SettlementRate, debits, credits, map[string]float64{
			"2100": position.TransactionAmount,
			"1100": position.TransactionAmount,
		})); err != nil {
		return 0, fmt.Errorf("GL posting failed, invoice not marked Paid: %v", err)
	}
	recordSettlementFX(data, settlement)

	data["status"] = "Paid"
	updatedBytes, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Paid', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorInvoice' AND id = $2`, schema),
		updatedBytes, invoiceID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	LogAuditEvent(tenantID, userID, "PAY_VENDOR_INVOICE_OVERRIDE_FINALIZED", "SUCCESS",
		"Override-approved "+settlementAuditDetail("vendor invoice", invoiceID, settlement))
	return amountInt, nil
}
