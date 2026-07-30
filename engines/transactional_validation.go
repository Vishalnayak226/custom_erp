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

	// PURCHA-0088: every received line's SKU must actually be on the PO.
	for _, line := range lines {
		if _, ok := ordered[line.Sku]; !ok {
			return &ValidationError{Code: "PURCHA-0088", Message: fmt.Sprintf("SKU %q is not part of PO %s", line.Sku, poID)}
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
		if receivedBefore[line.Sku]+line.Qty > ordered[line.Sku]+1e-9 {
			return &ValidationError{Code: "PURCHA-0087", Message: fmt.Sprintf("received quantity for SKU %q (%v) would exceed open PO quantity (%v)", line.Sku, receivedBefore[line.Sku]+line.Qty, ordered[line.Sku])}
		}
	}
	return nil
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
