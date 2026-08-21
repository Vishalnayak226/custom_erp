package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 25 Batch 3: transactional-doctype validation for the GRN/Purchase
// Order/Transfer Order/Manufacturing/HR modules, called from
// handlers_core_doc_engine.go's generic doc POST path - the same choke
// point Batch 2's ValidateMasterDataRules already uses for Item/Vendor/
// Customer. priorStatus/priorData are the document's state immediately
// before this write (docID == "" or priorData == nil on a create); only the
// doctypes that need to compare against their own prior state
// (PurchaseOrder, ProductionOrder) actually use them.
func ValidateTransactionalRules(tenantID, doctype, docID, priorStatus string, priorData, payload map[string]interface{}) error {
	// Stage 29.8: the per-doctype status-transition map runs for EVERY
	// doctype, ahead of the per-doctype switch below - it is the generic
	// half of the same job those bespoke rules do case by case. Attaching it
	// here rather than at each call site means every current and future
	// caller of the generic doc API is covered by construction. Opt-in
	// strict, so this is a no-op for any doctype that hasn't been flagged.
	if err := ValidateStatusTransition(tenantID, doctype, priorStatus, resolveWrittenStatus(payload), payload); err != nil {
		return err
	}

	// Stage 30.7: the configurable per-doctype edit window, attached at this
	// same shared choke point for the same reason - every current and future
	// caller of the generic doc API is covered by construction, and adding a
	// window for another doctype is one map entry plus one RegisterSetting.
	if err := validateDocumentEditWindow(tenantID, doctype, docID, priorData); err != nil {
		return err
	}

	switch doctype {
	case "StatusTransitionRule":
		return validateStatusTransitionRule(tenantID, payload)
	case "GRN":
		return validateGRNRules(tenantID, docID, payload)
	case "ASN":
		return validateASNRules(tenantID, payload)
	case "VendorInvoice":
		return validateVendorInvoiceCreateRules(tenantID, payload)
	case "PurchaseOrder":
		return validatePurchaseOrderEditRules(tenantID, docID, priorStatus, priorData, payload)
	case "TransferOrder":
		return validateTransferOrderCreateRules(payload)
	case "ProductionOrder":
		return validateProductionOrderEditRules(priorStatus, priorData, payload)
	case "Employee":
		return validateEmployeeRules(tenantID, docID, payload)
	case "Leave":
		return validateLeaveRules(tenantID, docID, payload)
	case "Attendance":
		return validateAttendanceRules(tenantID, payload)
	case "PurchaseRequisition":
		return validatePurchaseRequisitionEditRules(priorStatus, priorData)
	}
	return nil
}

// validatePurchaseRequisitionEditRules covers RFQ-0251: once
// ConvertRequisitionToOrder (engines/procurement.go) has moved a
// requisition to Converted, it must never change again - the downstream
// RFQ/PO it spawned is now the live document. Before this check, the
// generic doc-update path let a Converted requisition be edited like any
// other document, silently diverging from what was actually converted.
func validatePurchaseRequisitionEditRules(priorStatus string, priorData map[string]interface{}) error {
	if priorData == nil || priorStatus != "Converted" {
		return nil
	}
	return &ValidationError{Code: "RFQ-0251", Message: "this requisition is already converted to RFQ/PO - further changes are not allowed"}
}

// grnReceivedLine is GRN's received_items line shape, extended (optionally -
// every field below the first two is only checked when a caller actually
// populates it) beyond the plain {sku, qty} vendor_invoice.go's grnItemLine
// already reads, to carry the accepted/rejected split GOODSR-0089/0090
// describe. Existing GRNs that only ever set qty are unaffected.
// DamagedQty/DamageReason (Stage 26.5.2) add a genuine third QC-sampling
// bucket alongside accepted/rejected - engines/wms_receiving.go's
// PostGRNReceiptWithQC is what actually acts on all three at posting time.
type grnReceivedLine struct {
	Sku             string   `json:"sku"`
	Qty             float64  `json:"qty"`
	AcceptedQty     *float64 `json:"accepted_qty,omitempty"`
	RejectedQty     *float64 `json:"rejected_qty,omitempty"`
	RejectionReason string   `json:"rejection_reason,omitempty"`
	DamagedQty      *float64 `json:"damaged_qty,omitempty"`
	DamageReason    string   `json:"damage_reason,omitempty"`
	// Stage 42.1.4: batch/lot capture. Optional like everything above the
	// first two - a line that omits them is exactly the line this struct
	// already described, and PostGRNReceiptWithQC registers the lot from the
	// same three keys at posting time.
	BatchNo       string `json:"batch_no,omitempty"`
	MfgDate       string `json:"mfg_date,omitempty"`
	ExpiryDate    string `json:"expiry_date,omitempty"`
	SupplierBatch string `json:"supplier_batch,omitempty"`
	// Stage 42.1.8: serial capture. One entry per accepted unit - a
	// serial-tracked line with N units accepted must list N distinct serial
	// numbers, checked by ValidateReceiptSerialLine below. Empty/omitted for
	// every line this struct already described.
	SerialNumbers []string `json:"serial_numbers,omitempty"`
	// Stage 42.3.7: catch weight + dimensional capture. ActualWeight is
	// mandatory (GOODSR-0098 below) only for a line whose Item has
	// is_catch_weight = Yes (e.g. meat/produce received by variable actual
	// weight against a nominal ordered qty) - every other line leaves it
	// blank exactly as before this Stage. Dimensions are always optional;
	// nothing downstream requires them yet, but 42.4.4 (VAS/kitting) and
	// 42.6.9 (slotting by cube) both need somewhere to read them from, and
	// this received-line snapshot is that place.
	ActualWeight *float64 `json:"actual_weight,omitempty"`
	WeightUOM    string   `json:"weight_uom,omitempty"`
	Length       *float64 `json:"length,omitempty"`
	Width        *float64 `json:"width,omitempty"`
	Height       *float64 `json:"height,omitempty"`
	DimUOM       string   `json:"dim_uom,omitempty"`
}

// derivedAcceptedQty is the same accepted-quantity derivation
// PostGRNReceiptWithQC applies at posting time (explicit AcceptedQty wins,
// otherwise qty minus the rejected/damaged split), pulled out so
// validateGRNRules can check the serial-count-matches-accepted-qty rule
// against the same number that will actually be posted, not a re-guess of
// it.
func (l grnReceivedLine) derivedAcceptedQty() float64 {
	accepted := l.Qty
	if l.RejectedQty != nil {
		accepted -= *l.RejectedQty
	}
	if l.DamagedQty != nil {
		accepted -= *l.DamagedQty
	}
	if l.AcceptedQty != nil {
		accepted = *l.AcceptedQty
	}
	if accepted < 0 {
		accepted = 0
	}
	return accepted
}

// PrepareGRNReceipt fills in a GRN's receiving `location` from its referenced
// PurchaseOrder when the caller didn't supply one, and is Stage 30.2.1's half
// of that fix (the other half is db/migrations_stage30_2_1_grn_location.sql,
// which declares `location` as a real mandatory GRN field).
//
// The defect: the stock-posting hook in handlers_core_doc_engine.go reads
// payload["location"], but `location` was never a declared GRN field - only
// the bespoke GRN Workbench screen happened to send it. A GRN created through
// the generic record-list form, the API, or bulk import therefore could not
// supply it at all, so it saved with HTTP 200, counted toward the PO's
// received quantity (closing it to further receipts, PURCHA-0084), and posted
// exactly zero stock, silently.
//
// Called before ValidateDocument for the same reason PreparePurchaseRequisition
// is: the field it populates is mandatory, so the default has to exist by the
// time the metadata check runs. A GRN with no po_id, or a PO with no warehouse
// of its own, falls through and is rejected by that mandatory check with a
// message naming the field - which is the correct outcome, since there is no
// location this receipt could post to.
func PrepareGRNReceipt(tenantID string, payload map[string]interface{}) error {
	if strings.TrimSpace(strField(payload, "location")) != "" {
		return nil
	}
	poID := strField(payload, "po_id")
	if poID == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var poDataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'PurchaseOrder' AND id = $1 AND deleted_at IS NULL`, schema),
		poID).Scan(&poDataStr); err != nil {
		if err == sql.ErrNoRows {
			// The Link field's own existence check (META-0198) reports this
			// far better than a bespoke message here would.
			return nil
		}
		return err
	}
	var poData map[string]interface{}
	if err := json.Unmarshal([]byte(poDataStr), &poData); err != nil {
		return nil
	}
	// target_warehouse is PurchaseOrder's own mandatory "where is this going"
	// field (db/migration.sql); `location` is its store/site sibling, used as
	// the fallback for POs that carry one but no warehouse.
	for _, key := range []string{"target_warehouse", "location"} {
		if v := strings.TrimSpace(fmt.Sprintf("%v", poData[key])); v != "" && v != "<nil>" {
			payload["location"] = v
			return nil
		}
	}
	return nil
}

// fetchPOItemQuantities sums a PurchaseOrder's own ordered qty per SKU from
// its "items" JSON field (poItemLine, shared with vendor_invoice.go's
// Match3Way).
func fetchPOItemQuantities(tenantID, poID string) (map[string]float64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var poDataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'PurchaseOrder' AND id = $1`, schema), poID).Scan(&poDataStr); err != nil {
		return nil, err
	}
	var poData map[string]interface{}
	if err := json.Unmarshal([]byte(poDataStr), &poData); err != nil {
		return nil, err
	}
	itemsStr, _ := poData["items"].(string)
	var items []poItemLine
	if itemsStr != "" {
		// 24.33: a malformed items JSON must surface as an error, not
		// silently compute a zero ordered-qty map - callers that fold "PO
		// not found" and "no parsed lines" into a skip (see validateGRNRules/
		// validateASNRules below) must be able to tell that case apart from
		// "the PO exists but its data is corrupt," which they now fail
		// closed on instead of letting an over-receipt check pass.
		if err := json.Unmarshal([]byte(itemsStr), &items); err != nil {
			return nil, fmt.Errorf("PO %s has malformed items JSON: %w", poID, err)
		}
	}
	ordered := map[string]float64{}
	for _, it := range items {
		ordered[it.Sku] += float64(it.Qty)
	}
	return ordered, nil
}

// fetchGRNReceivedQuantities sums received qty per SKU across every GRN
// already posted against poID, excluding excludeGRNID (the GRN currently
// being validated, so its own not-yet-saved lines aren't double-counted
// against themselves).
func fetchGRNReceivedQuantities(tenantID, poID, excludeGRNID string) (map[string]float64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'GRN' AND data->>'po_id' = $1 AND status != 'Cancelled'`, schema), poID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	received := map[string]float64{}
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			return nil, err
		}
		if excludeGRNID != "" && id == excludeGRNID {
			continue
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			continue
		}
		receivedStr, _ := data["received_items"].(string)
		if receivedStr == "" {
			continue
		}
		var lines []grnReceivedLine
		if err := json.Unmarshal([]byte(receivedStr), &lines); err != nil {
			continue
		}
		for _, l := range lines {
			received[l.Sku] += l.Qty
		}
	}
	return received, rows.Err()
}

// purchaseOrderFullyReceived reports whether every ordered line on poID has
// already been fully received by GRNs other than excludeGRNID. A PO with no
// parsed item lines is never treated as fully received (nothing to close).
func purchaseOrderFullyReceived(tenantID, poID, excludeGRNID string) (bool, error) {
	ordered, err := fetchPOItemQuantities(tenantID, poID)
	if err != nil || len(ordered) == 0 {
		return false, err
	}
	received, err := fetchGRNReceivedQuantities(tenantID, poID, excludeGRNID)
	if err != nil {
		return false, err
	}
	for sku, qty := range ordered {
		if received[sku]+1e-9 < qty {
			return false, nil
		}
	}
	return true, nil
}

// validateGRNRules covers GOODSR-0089/0090 (accepted/rejected quantity),
// GRN-0253 (cancellation blocked by a downstream invoice), and the PO
// cross-checks PURCHA-0082/0084/0086/0087/0088 - all of them things a GRN
// can only get wrong in relation to the PO/GRNs it references, so they live
// together rather than split across GRN vs PurchaseOrder validators.
func validateGRNRules(tenantID, docID string, payload map[string]interface{}) error {
	receivedStr, _ := payload["received_items"].(string)
	var lines []grnReceivedLine
	if receivedStr != "" {
		if err := json.Unmarshal([]byte(receivedStr), &lines); err != nil {
			return fmt.Errorf("received_items is not valid JSON: %v", err)
		}
	}

	for _, line := range lines {
		if line.AcceptedQty != nil && *line.AcceptedQty > line.Qty+1e-9 {
			return &ValidationError{Code: "GOODSR-0089", Message: fmt.Sprintf("accepted quantity (%v) for SKU %q cannot exceed received quantity (%v)", *line.AcceptedQty, line.Sku, line.Qty)}
		}
		if line.RejectedQty != nil && *line.RejectedQty > 0 && strings.TrimSpace(line.RejectionReason) == "" {
			return &ValidationError{Code: "GOODSR-0090", Message: fmt.Sprintf("rejection reason is required for SKU %q (rejected qty %v)", line.Sku, *line.RejectedQty)}
		}
		// 26.5.2: QC sampling's third bucket - a damaged qty needs its own
		// reason, same GOODSR-0090 convention as rejected qty.
		if line.DamagedQty != nil && *line.DamagedQty > 0 && strings.TrimSpace(line.DamageReason) == "" {
			return &ValidationError{Code: "GOODSR-0096", Message: fmt.Sprintf("damage reason is required for SKU %q (damaged qty %v)", line.Sku, *line.DamagedQty)}
		}
		// 26.5.2: the three QC buckets are a partition of what was actually
		// received - they can never sum past it, whether or not accepted_qty
		// was explicitly supplied (an omitted accepted_qty is derived as
		// qty-rejected-damaged by PostGRNReceiptWithQC, so this check has to
		// hold before that derivation for the derived value to stay
		// non-negative and meaningful).
		var rejected, damaged float64
		if line.RejectedQty != nil {
			rejected = *line.RejectedQty
		}
		if line.DamagedQty != nil {
			damaged = *line.DamagedQty
		}
		if rejected+damaged > line.Qty+1e-9 {
			return &ValidationError{Code: "GOODSR-0097", Message: fmt.Sprintf("rejected (%v) plus damaged (%v) quantity for SKU %q cannot exceed received quantity (%v)", rejected, damaged, line.Sku, line.Qty)}
		}
		// 42.1.4: a batch-tracked item must arrive with a lot number, and
		// short-dated goods are refused at the door. Validated HERE, before the
		// document is written, rather than only at posting time: the GRN create
		// hook cancels the receipt if posting fails, so a rejection raised later
		// reaches the user as an unexpected-error 500 with a cancelled GRN
		// behind it instead of a field message they can act on.
		if err := ValidateReceiptBatchLine(tenantID, line.Sku, line.BatchNo, line.ExpiryDate); err != nil {
			return err
		}
		// 42.1.8: a serial-tracked item must arrive with one serial number per
		// accepted unit, checked against the same accepted-qty derivation
		// PostGRNReceiptWithQC posts with. Same pre-write rationale as the
		// batch check immediately above.
		if err := ValidateReceiptSerialLine(tenantID, line.Sku, line.SerialNumbers, line.derivedAcceptedQty()); err != nil {
			return err
		}
		// Stage 42.3.7: a catch-weight item (Item.is_catch_weight = Yes) must
		// carry its actual received weight - the whole point of the flag is
		// that its billable/on-hand quantity is the real scaled weight, not
		// the nominal ordered qty. Same pre-write rationale as the batch/
		// serial checks immediately above.
		if err := ValidateReceiptCatchWeightLine(tenantID, line.Sku, line.ActualWeight); err != nil {
			return err
		}
		// A lot dated to expire before it was made is a typo that would make
		// FEFO allocate it first forever - the same rule the Batch master
		// applies, enforced on the receipt that would create that master.
		if mfg, okM := parseTraceDate(line.MfgDate); okM {
			if exp, okE := parseTraceDate(line.ExpiryDate); okE && !exp.After(mfg) {
				return &ValidationError{Code: "GLOBAL-0002", SubFor: "Expiry Date", Message: fmt.Sprintf(
					"expiry date %s for SKU %q is not after its manufacture date %s",
					exp.Format(isoDate), line.Sku, mfg.Format(isoDate))}
			}
		}
	}

	if strField(payload, "status") == "Cancelled" {
		schema, err := db.GetTenantSchema(tenantID)
		if err != nil {
			return err
		}
		var invoiceID string
		err = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'VendorInvoice' AND data->>'grn_id' = $1 LIMIT 1`, schema), docID).Scan(&invoiceID)
		if err == nil {
			return &ValidationError{Code: "GRN-0253", Message: fmt.Sprintf("GRN %s cannot be cancelled: vendor invoice %s already references it", docID, invoiceID)}
		}
	}

	poID := strField(payload, "po_id")
	if poID == "" {
		return nil
	}
	ordered, err := fetchPOItemQuantities(tenantID, poID)
	if err != nil && err != sql.ErrNoRows {
		// 24.33: fail closed on a corrupt PO row instead of silently
		// skipping the over-receipt checks below.
		return fmt.Errorf("could not verify PO %s item quantities: %w", poID, err)
	}
	if err == sql.ErrNoRows || len(ordered) == 0 {
		// PO not found or has no parsed item lines - the Link field's own
		// existence is already enforced by ValidateDocument (META-0198);
		// nothing further to cross-check here.
		return nil
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var poStatus, poDataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'PurchaseOrder' AND id = $1`, schema), poID).Scan(&poDataStr, &poStatus); err != nil {
		return nil
	}
	var poData map[string]interface{}
	_ = json.Unmarshal([]byte(poDataStr), &poData)

	// Stage 42.3.6: tenant-configurable tolerance on the two hardcoded checks
	// just below (PURCHA-0088/0087). No ReceiptValidationRule configured
	// (getReceiptValidationRule's own zero-value default) reproduces exactly
	// today's behavior - 0% over-receipt tolerance, unexpected items blocked.
	poVendor, _ := poData["vendor"].(string)
	overReceiptTolerancePct, allowUnexpectedItems, err := getReceiptValidationRule(tenantID, poVendor)
	if err != nil {
		return err
	}

	// PURCHA-0082 / PURCHA-0086: only meaningful when PurchaseOrder is
	// actually approval-gated (an admin configured a rule for it) - an
	// ungated PO has no "pending approval" state to block receiving on.
	gated, _ := IsApprovalGated(tenantID, "PurchaseOrder")
	if gated && poStatus != "Approved" {
		if amendReason, _ := poData["amendment_reason"].(string); amendReason != "" {
			return &ValidationError{Code: "PURCHA-0086", Message: fmt.Sprintf("PO %s amendment is pending approval (status: %s)", poID, poStatus)}
		}
		return &ValidationError{Code: "PURCHA-0082", Message: fmt.Sprintf("PO %s is pending approval (status: %s)", poID, poStatus)}
	}

	// PURCHA-0088: every received line's SKU must actually be on the PO,
	// unless a Stage 42.3.6 ReceiptValidationRule for this vendor (or the
	// tenant default) explicitly allows unexpected items.
	if !allowUnexpectedItems {
		for _, line := range lines {
			if _, ok := ordered[line.Sku]; !ok {
				return &ValidationError{Code: "PURCHA-0088", Message: fmt.Sprintf("SKU %q is not part of PO %s", line.Sku, poID)}
			}
		}
	}

	receivedBefore, err := fetchGRNReceivedQuantities(tenantID, poID, docID)
	if err != nil {
		return err
	}
	fullyReceivedBefore := true
	for sku, qty := range ordered {
		if receivedBefore[sku]+1e-9 < qty {
			fullyReceivedBefore = false
			break
		}
	}
	if fullyReceivedBefore && len(lines) > 0 {
		return &ValidationError{Code: "PURCHA-0084", Message: fmt.Sprintf("PO %s is already fully received - no further GRNs are allowed", poID)}
	}

	for _, line := range lines {
		orderedQty, onPO := ordered[line.Sku]
		if !onPO {
			// Not on the PO at all - already let through (or rejected) by the
			// PURCHA-0088 check above; there is no PO qty to compare against.
			continue
		}
		allowedQty := orderedQty * (1 + overReceiptTolerancePct/100)
		if receivedBefore[line.Sku]+line.Qty > allowedQty+1e-9 {
			return &ValidationError{Code: "PURCHA-0087", Message: fmt.Sprintf("received quantity for SKU %q (%v) would exceed open PO quantity (%v) plus tolerance (%v)", line.Sku, receivedBefore[line.Sku]+line.Qty, orderedQty, allowedQty)}
		}
	}
	return nil
}

// getReceiptValidationRule (Stage 42.3.6) resolves the Active
// ReceiptValidationRule that applies to a receipt - a row scoped to
// `vendor` if one exists, else the Active row with a blank vendor (the
// tenant default), else the zero value (0% tolerance, unexpected items
// blocked) that reproduces every pre-42.3.6 tenant's existing behavior
// exactly. validateReceiptValidationRuleMasterRules (master_data_validation.go)
// guarantees at most one Active row per scope, so this never has to choose
// between two conflicting matches.
func getReceiptValidationRule(tenantID, vendor string) (tolerancePct float64, allowUnexpectedItems bool, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, false, err
	}
	var toleranceStr, allowStr string
	scanErr := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(data->>'over_receipt_tolerance_pct', '0'), COALESCE(data->>'allow_unexpected_items', 'No')
		FROM %s.documents
		WHERE doctype = 'ReceiptValidationRule' AND status = 'Active' AND data->>'vendor' = $1
		LIMIT 1`, schema), vendor).Scan(&toleranceStr, &allowStr)
	if scanErr == sql.ErrNoRows && vendor != "" {
		scanErr = db.DB.QueryRow(fmt.Sprintf(`
			SELECT COALESCE(data->>'over_receipt_tolerance_pct', '0'), COALESCE(data->>'allow_unexpected_items', 'No')
			FROM %s.documents
			WHERE doctype = 'ReceiptValidationRule' AND status = 'Active' AND COALESCE(data->>'vendor', '') = ''
			LIMIT 1`, schema)).Scan(&toleranceStr, &allowStr)
	}
	if scanErr == sql.ErrNoRows {
		return 0, false, nil
	}
	if scanErr != nil {
		return 0, false, scanErr
	}
	return numFromInterface(toleranceStr), allowStr == "Yes", nil
}

// asnExpectedLine is ASN's expected_items line shape - just sku/qty.
type asnExpectedLine struct {
	Sku string  `json:"sku"`
	Qty float64 `json:"qty"`
}

// validateASNRules (26.5.1) cross-checks an ASN's expected items against
// its referenced PO the same way validateGRNRules' PURCHA-0088 does for a
// GRN - an ASN naming a SKU the PO never ordered is a data-entry mistake
// worth catching before the GRN Workbench trusts it as a prefill source,
// not after.
func validateASNRules(tenantID string, payload map[string]interface{}) error {
	expectedStr, _ := payload["expected_items"].(string)
	if expectedStr == "" {
		return nil
	}
	var lines []asnExpectedLine
	if err := json.Unmarshal([]byte(expectedStr), &lines); err != nil {
		return fmt.Errorf("expected_items is not valid JSON: %v", err)
	}
	poID := strField(payload, "po_id")
	if poID == "" {
		return nil
	}
	ordered, err := fetchPOItemQuantities(tenantID, poID)
	if err != nil && err != sql.ErrNoRows {
		// 24.33: fail closed on a corrupt PO row instead of silently
		// skipping the SKU cross-check below.
		return fmt.Errorf("could not verify PO %s item quantities: %w", poID, err)
	}
	if err == sql.ErrNoRows || len(ordered) == 0 {
		return nil
	}
	for _, line := range lines {
		if _, ok := ordered[line.Sku]; !ok {
			return &ValidationError{Code: "ASN-0271", Message: fmt.Sprintf("SKU %q is not part of PO %s", line.Sku, poID)}
		}
	}
	return nil
}

// validateVendorInvoiceCreateRules covers GOODSR-0095: grn_id's own
// existence is already enforced by ValidateDocument's Link check
// (META-0198, it's a mandatory Link field per
// db/migrations_stage17g_vendor_invoice.sql) - what that doesn't catch is a
// GRN that exists but was never actually completed (no received_items at
// all, a shell record), which is what "GRN is not completed" describes.
func validateVendorInvoiceCreateRules(tenantID string, payload map[string]interface{}) error {
	grnID := strField(payload, "grn_id")
	if grnID == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var receivedItemsStr string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'received_items', '') FROM %s.documents WHERE doctype = 'GRN' AND id = $1`, schema), grnID).Scan(&receivedItemsStr); err != nil {
		return nil
	}
	if strings.TrimSpace(receivedItemsStr) == "" || strings.TrimSpace(receivedItemsStr) == "[]" {
		return &ValidationError{Code: "GOODSR-0095", Message: fmt.Sprintf("GRN %s has no received items - it is not completed", grnID)}
	}
	return nil
}

// validatePurchaseOrderEditRules covers PURCHA-0085 (amendment reason
// required to edit an Approved PO) and PO-0252 (amendment blocked once the
// PO is fully received) - both only apply to an edit (docID/priorData
// present) that actually changes items or total_amount, not a create and
// not a status-only update (e.g. the approval flow moving Draft ->
// Approved touches nothing this function cares about).
func validatePurchaseOrderEditRules(tenantID, docID, priorStatus string, priorData, payload map[string]interface{}) error {
	if docID == "" || priorData == nil {
		return nil
	}
	oldItems, _ := priorData["items"].(string)
	newItems, _ := payload["items"].(string)
	oldAmount := numFromInterface(priorData["total_amount"])
	newAmount := numFromInterface(payload["total_amount"])
	if oldItems == newItems && oldAmount == newAmount {
		return nil
	}

	fullyReceived, err := purchaseOrderFullyReceived(tenantID, docID, "")
	if err != nil {
		return err
	}
	if fullyReceived {
		return &ValidationError{Code: "PO-0252", Message: "purchase order cannot be amended after full receipt"}
	}

	if priorStatus == "Approved" {
		if strings.TrimSpace(strField(payload, "amendment_reason")) == "" {
			return &ValidationError{Code: "PURCHA-0085", Message: "amendment reason is required to change an Approved purchase order"}
		}
	}
	return nil
}

// validateTransferOrderCreateRules covers STOCKT-0111 - a plain field
// comparison, cheap enough to run on every save rather than gating it to
// create-only.
func validateTransferOrderCreateRules(payload map[string]interface{}) error {
	from := strField(payload, "from_warehouse")
	to := strField(payload, "to_warehouse")
	if from != "" && to != "" && from == to {
		return &ValidationError{Code: "STOCKT-0111", Message: "source and destination location cannot be the same"}
	}
	return nil
}

// validateProductionOrderEditRules covers MANUFA-0144: once a production
// order has left Draft (material issued or completed), its bom_id/quantity
// can no longer change - those are exactly the two fields
// IssueProductionMaterial/CompleteProductionOrder already trusted as fixed
// at issue time.
func validateProductionOrderEditRules(priorStatus string, priorData, payload map[string]interface{}) error {
	if priorData == nil || priorStatus == "" || priorStatus == "Draft" {
		return nil
	}
	oldBOM, _ := priorData["bom_id"].(string)
	newBOM, _ := payload["bom_id"].(string)
	if oldBOM != newBOM || numFromInterface(priorData["quantity"]) != numFromInterface(payload["quantity"]) {
		return &ValidationError{Code: "MANUFA-0144", Message: fmt.Sprintf("production order cannot be amended after release (current status: %s)", priorStatus)}
	}
	return nil
}

// validateEmployeeRules covers HRPAYR-0149: same duplicate-field query
// shape as master_data_validation.go's Item barcode check, just against
// Employee's own "code" field instead of Item's "barcode".
func validateEmployeeRules(tenantID, docID string, payload map[string]interface{}) error {
	code := strField(payload, "code")
	if code == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'Employee' AND data->>'code' = $1 AND id != $2 AND status != 'Cancelled'
		LIMIT 1`, schema), code, docID).Scan(&existingID)
	if err == nil {
		return &ValidationError{Code: "HRPAYR-0149", Message: fmt.Sprintf("employee code %q is already used by %s", code, existingID)}
	}

	// Stage 26.8.6 (HRPAYR-0156): offboarding's exit-date field, additive on
	// Employee - only checked when both dates are actually set, so an
	// Employee with neither (every pre-26.8.6 record) is unaffected.
	joinDate := strField(payload, "date_of_joining")
	exitDate := strField(payload, "date_of_exit")
	if joinDate != "" && exitDate != "" && exitDate < joinDate {
		return &ValidationError{Code: "HRPAYR-0156", Message: "exit date cannot be earlier than the joining date"}
	}
	return nil
}

// leaveAnnualEntitlement (26.8.5) is a deliberately simple flat per-type
// yearly day count, not a configurable leave-policy engine - matching this
// codebase's lightweight principle for a first pass at self-service leave.
// "Unpaid" has no entitlement ceiling (an employee can always go unpaid).
var leaveAnnualEntitlement = map[string]float64{
	"Casual": 12,
	"Sick":   10,
	"Earned": 15,
}

// validateLeaveBalance covers HRPAYR-0151: sums this employee's already
// Applied/Approved days of the same leave_type within the same calendar
// year as fromDate, and blocks a request that would push the total over
// the flat annual entitlement above.
func validateLeaveBalance(tenantID, docID, employeeID, leaveType, fromDate string, days float64) error {
	entitlement, capped := leaveAnnualEntitlement[leaveType]
	if !capped {
		return nil
	}
	year := fromDate
	if len(year) >= 4 {
		year = fromDate[:4]
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var usedRaw sql.NullFloat64
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM((data->>'days')::numeric), 0) FROM %s.documents
		WHERE doctype = 'Leave' AND data->>'employee_id' = $1 AND data->>'leave_type' = $2
		AND id != $3 AND status IN ('Applied', 'Approved') AND (data->>'from_date') LIKE $4`,
		schema), employeeID, leaveType, docID, year+"%").Scan(&usedRaw); err != nil {
		return err
	}
	used := usedRaw.Float64
	if used+days > entitlement {
		return &ValidationError{Code: "HRPAYR-0151", Message: fmt.Sprintf("insufficient %s leave balance: %v days already used/applied this year, %v requested, entitlement is %v days/year", leaveType, used, days, entitlement)}
	}
	return nil
}

// validateAttendanceRules covers HR-0269: an employee whose roster actually
// uses ShiftAssignment (Stage 26.8.1) must have one for the exact date an
// Attendance record marks - Holiday/WeeklyOff/Leave statuses are exempt,
// they don't represent a worked shift. An employee with zero ShiftAssignment
// rows ever (i.e. not using shift rostering at all) is completely unaffected
// - this is opt-in by existing data, not a new mandatory field.
func validateAttendanceRules(tenantID string, payload map[string]interface{}) error {
	employeeID := strField(payload, "employee_id")
	date := strField(payload, "date")
	status := strField(payload, "status")
	if employeeID == "" || date == "" || status == "Holiday" || status == "WeeklyOff" || status == "Leave" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var usesRoster bool
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'ShiftAssignment' AND data->>'employee_id' = $1)`,
		schema), employeeID).Scan(&usesRoster); err != nil {
		return err
	}
	if !usesRoster {
		return nil
	}
	var assignedToday bool
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'ShiftAssignment'
		AND data->>'employee_id' = $1 AND data->>'date' = $2 AND status != 'Cancelled')`,
		schema), employeeID, date).Scan(&assignedToday); err != nil {
		return err
	}
	if !assignedToday {
		return &ValidationError{Code: "HR-0269", Message: "shift is not assigned for this employee/date - configure a shift assignment before marking attendance"}
	}
	return nil
}

// validateLeaveRules covers HRPAYR-0152: an overlapping-date-range query
// against the same employee's other still-live Leave records.
func validateLeaveRules(tenantID, docID string, payload map[string]interface{}) error {
	employeeID := strField(payload, "employee_id")
	fromDate := strField(payload, "from_date")
	toDate := strField(payload, "to_date")
	if employeeID == "" || fromDate == "" || toDate == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'Leave' AND data->>'employee_id' = $1 AND id != $2
		AND status NOT IN ('Rejected', 'Cancelled')
		AND (data->>'from_date') <= $4 AND (data->>'to_date') >= $3
		LIMIT 1`, schema), employeeID, docID, fromDate, toDate).Scan(&existingID)
	if err == nil {
		return &ValidationError{Code: "HRPAYR-0152", Message: fmt.Sprintf("overlaps existing leave record %s for this employee", existingID)}
	}

	leaveType := strField(payload, "leave_type")
	days := numFromInterface(payload["days"])
	if leaveType != "" && days > 0 {
		if err := validateLeaveBalance(tenantID, docID, employeeID, leaveType, fromDate, days); err != nil {
			return err
		}
	}
	return nil
}

// CheckAttendanceLocationMismatch (HR-0268) is a non-blocking Warning
// (catalog Blocking:false) - it reports a mismatch for the caller to
// log/audit, it never rejects the save. employeeID/attLocation are read
// from the same payload the generic doc engine already has in hand.
func CheckAttendanceLocationMismatch(tenantID string, payload map[string]interface{}) (mismatched bool, message string) {
	employeeID := strField(payload, "employee_id")
	attLocation := strField(payload, "location")
	if employeeID == "" || attLocation == "" {
		return false, ""
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, ""
	}
	var empLocation string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'location' FROM %s.documents WHERE doctype = 'Employee' AND id = $1`, schema), employeeID).Scan(&empLocation); err != nil {
		return false, ""
	}
	if empLocation == "" || empLocation == attLocation {
		return false, ""
	}
	return true, fmt.Sprintf("Attendance for employee %s recorded at %q, assigned work location is %q", employeeID, attLocation, empLocation)
}
