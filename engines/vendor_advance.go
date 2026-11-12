package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 148.15: a vendor advance against a purchase order.
//
// The money paid ahead is not a payable: it sits in its own asset account, 1810
// Vendor Advances (Dr 1810 / Cr 1100 bank). The vendor's bill stays at its gross
// amount. Applying the advance is a settle line on the bill, like a part payment
// whose cash is the advance: it clears the bill's open 2100 and input-GST legs
// (the same split a payment makes, so the GST credit is taken once) against 1810.
// An advance left over is refunded (Dr bank / Cr 1810) or kept for the next bill.
//
// One advance is one VendorAdvance document; its applications and refunds are
// lines on it, the bill's side is a "payments" line of kind "Advance". Every
// posting carries a key (VendorAdvance:<id>:PAY, VendorInvoice:<bill>:ADVANCE:<ref>,
// VendorAdvance:<id>:REFUND:<ref>), so a retry posts nothing twice. The payment
// hold (148.9) and the period lock come in through PostDoubleEntryTx, which every
// posting here runs through.
//
// TDS: nothing is withheld on an advance - the same as a plain Pay, which
// withholds nothing either; TDS is the separate Pay with TDS on a whole bill. A
// bill with an advance applied counts as part-paid, so Pay with TDS refuses it
// (FIN-0521, as for any part-paid bill).

const (
	VendorAdvanceDoctype  = "VendorAdvance"
	AccountVendorAdvances = "1810"

	vendorAdvanceOpen    = "Open"
	vendorAdvancePart    = "Part Applied"
	vendorAdvanceApplied = "Applied"
	vendorAdvanceRefund  = "Refunded"
	vendorAdvanceCancel  = "Cancelled"

	settlementKindAdvance = "Advance"

	vendorAdvanceInputCode = "FIN-0920" // vendor, amount or date not usable
	vendorAdvancePOCode    = "FIN-0921" // PO not found, not the vendor's, or closed
	vendorAdvanceOverCode  = "FIN-0922" // more than the advance holds
	vendorAdvanceBillCode  = "FIN-0923" // bill cannot take it (state, vendor, PO, currency)
	vendorAdvanceCancelCod = "FIN-0924" // cancel refused: already used or refunded
	vendorAdvanceNotOpen   = "FIN-0925" // advance has nothing open
)

// validateVendorMoneyDocument runs first in validateDocumentWith: an advance
// or a rebate claim is written only by its own routes, which post the money.
func validateVendorMoneyDocument(doctype string) error {
	switch doctype {
	case VendorAdvanceDoctype:
		return &ValidationError{Code: "FIN-0926", Message: "Vendor advances are made by Give advance."}
	case "VendorRebateClaim":
		return &ValidationError{Code: "PURCHA-5025", Message: "Rebate claims are made by Calculate."}
	case InspectionLotDoctype, StockAgeInspectionDoctype:
		return &ValidationError{Code: inspectionTypedCode, Message: "Inspections are made by a receipt or the daily check."}
	}
	return nil
}

// VendorAdvanceRequest is one advance given to a vendor.
type VendorAdvanceRequest struct {
	VendorID       string  `json:"vendor_id"`
	POID           string  `json:"po_id"`
	Amount         float64 `json:"amount"`
	Date           string  `json:"advance_date"`
	Reference      string  `json:"reference"`
	Note           string  `json:"note"`
	IdempotencyKey string  `json:"idempotency_key"`
}

// VendorAdvanceResult is what one advance, application or refund did.
type VendorAdvanceResult struct {
	ID              string  `json:"id"`
	Status          string  `json:"status"`
	Amount          float64 `json:"amount"`
	OpenAmount      float64 `json:"open_amount"`
	InvoiceID       string  `json:"invoice_id,omitempty"`
	InvoiceStatus   string  `json:"invoice_status,omitempty"`
	InvoiceOutstand float64 `json:"invoice_outstanding,omitempty"`
	Reference       string  `json:"reference,omitempty"`
	Replayed        bool    `json:"replayed,omitempty"`
}

type vendorAdvanceLine struct {
	InvoiceID string  `json:"invoice_id,omitempty"`
	Amount    float64 `json:"amount"`
	Reference string  `json:"reference"`
	Date      string  `json:"date"`
	By        string  `json:"by"`
}

func vendorAdvanceErr(code, format string, args ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

func vendorAdvanceValidDate(d string) bool {
	_, err := time.Parse("2006-01-02", d)
	return err == nil
}

func vendorAdvanceLines(data map[string]interface{}, key string) []vendorAdvanceLine {
	raw, ok := data[key]
	if !ok || raw == nil {
		return nil
	}
	b, _ := json.Marshal(raw)
	var out []vendorAdvanceLine
	_ = json.Unmarshal(b, &out)
	return out
}

func vendorAdvanceSumPaise(lines []vendorAdvanceLine) int64 {
	var total int64
	for _, l := range lines {
		total += RupeesToPaise(l.Amount)
	}
	return total
}

// vendorAdvanceSettle recomputes applied, refunded, open and status from the
// advance's own lines, so they can never drift from what was recorded.
func vendorAdvanceSettle(data map[string]interface{}) int64 {
	applied := vendorAdvanceSumPaise(vendorAdvanceLines(data, "applications"))
	refunded := vendorAdvanceSumPaise(vendorAdvanceLines(data, "refunds"))
	open := RupeesToPaise(numFromInterface(data["amount"])) - applied - refunded
	if open < 0 {
		open = 0
	}
	data["applied_amount"] = PaiseToRupees(applied)
	data["refunded_amount"] = PaiseToRupees(refunded)
	data["open_amount"] = PaiseToRupees(open)
	switch {
	case strField(data, "status") == vendorAdvanceCancel:
	case open > 0 && applied+refunded == 0:
		data["status"] = vendorAdvanceOpen
	case open > 0:
		data["status"] = vendorAdvancePart
	case applied == 0 && refunded > 0:
		data["status"] = vendorAdvanceRefund
	default:
		data["status"] = vendorAdvanceApplied
	}
	return open
}

func vendorAdvanceResult(id string, data map[string]interface{}) VendorAdvanceResult {
	return VendorAdvanceResult{ID: id, Status: strField(data, "status"),
		Amount: numFromInterface(data["amount"]), OpenAmount: numFromInterface(data["open_amount"])}
}

func loadVendorAdvanceTx(tx *sql.Tx, schema, id string) (map[string]interface{}, error) {
	var raw []byte
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'VendorAdvance' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), id).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, vendorAdvanceErr(vendorAdvanceInputCode, "vendor advance %s was not found", id)
		}
		return nil, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("vendor advance %s is unreadable: %v", id, err)
	}
	return data, nil
}

func saveVendorAdvanceTx(tx *sql.Tx, schema, id string, data map[string]interface{}) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorAdvance' AND id = $3`, schema),
		b, strField(data, "status"), id)
	return err
}

// GiveVendorAdvance records an advance paid to a vendor, optionally against one
// of its purchase orders, and posts Dr 1810 / Cr 1100. A repeated
// idempotency_key returns the advance already given.
func GiveVendorAdvance(tenantID, userID string, req VendorAdvanceRequest) (VendorAdvanceResult, error) {
	var res VendorAdvanceResult
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	req.VendorID = strings.TrimSpace(req.VendorID)
	req.POID = strings.TrimSpace(req.POID)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	if req.Date == "" {
		req.Date = BusinessToday()
	}
	paise := RupeesToPaise(req.Amount)
	if paise <= 0 || req.Amount != PaiseToRupees(paise) {
		return res, vendorAdvanceErr(vendorAdvanceInputCode, "the advance amount must be a positive amount in rupees and paise")
	}
	if !vendorAdvanceValidDate(req.Date) {
		return res, vendorAdvanceErr(vendorAdvanceInputCode, "the advance date %q is not a date (use YYYY-MM-DD)", req.Date)
	}
	vendorID, vendor, ok := findVendorRow(db.DB, schema, req.VendorID)
	if !ok {
		return res, vendorAdvanceErr(vendorAdvanceInputCode, "vendor %q was not found", req.VendorID)
	}
	if req.POID != "" {
		po, _, perr := fetchDocData(tenantID, "PurchaseOrder", req.POID)
		if perr != nil || po == nil {
			return res, vendorAdvanceErr(vendorAdvancePOCode, "%s was not found", poLabel(db.DB, schema, req.POID))
		}
		if poVendor, _, pok := findVendorRow(db.DB, schema, firstNonEmpty(strField(po, "vendor_id"), strField(po, "vendor"))); !pok || poVendor != vendorID {
			return res, vendorAdvanceErr(vendorAdvancePOCode, "%s is not for vendor %s", poLabel(db.DB, schema, req.POID), firstNonEmpty(strField(vendor, "name"), vendorID))
		}
		if st := strField(po, "status"); st == "Cancelled" || st == "Rejected" || st == "Closed" {
			return res, vendorAdvanceErr(vendorAdvancePOCode, "%s is %s - an advance cannot be given against it", poLabel(db.DB, schema, req.POID), st)
		}
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	if req.IdempotencyKey != "" {
		if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, "vadv:"+schema+":"+req.IdempotencyKey); err != nil {
			return res, err
		}
		var raw []byte
		var id string
		err := tx.QueryRow(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'VendorAdvance' AND data->>'idempotency_key' = $1 LIMIT 1`, schema), req.IdempotencyKey).Scan(&id, &raw)
		if err == nil {
			var prior map[string]interface{}
			_ = json.Unmarshal(raw, &prior)
			res = vendorAdvanceResult(id, prior)
			res.Replayed = true
			return res, nil
		}
		if err != sql.ErrNoRows {
			return res, err
		}
	}
	id := NewDocID("VADV")
	data := map[string]interface{}{
		"code": id, "vendor_id": vendorID, "po_id": req.POID, "amount": PaiseToRupees(paise), "advance_date": req.Date,
		"reference": strings.TrimSpace(req.Reference), "note": strings.TrimSpace(req.Note), "status": vendorAdvanceOpen,
		"idempotency_key": req.IdempotencyKey, "given_by": userID,
	}
	vendorAdvanceSettle(data)
	b, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'VendorAdvance', $2, $3, $4)`, schema),
		id, b, vendorAdvanceOpen, userID); err != nil {
		return res, err
	}
	if err := PostDoubleEntryTx(tx, tenantID, schema, VendorAdvanceDoctype, id,
		map[string]int64{AccountVendorAdvances: paise}, map[string]int64{"1100": paise}, req.Date, "VendorAdvance:"+id+":PAY"); err != nil {
		return res, postingError(err, "advance not recorded")
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	LogAuditEvent(tenantID, userID, "VENDOR_ADVANCE_GIVEN", "SUCCESS", fmt.Sprintf("Advance %s of %.2f to vendor %s%s", id, PaiseToRupees(paise), vendorID, map[bool]string{true: " against PO " + req.POID, false: ""}[req.POID != ""]))
	return vendorAdvanceResult(id, data), nil
}

// ApplyVendorAdvance takes some or all of an advance off a Matched vendor bill:
// a settle line on the bill, Dr 2100 (and input GST) / Cr 1810. amount 0 applies
// as much as both hold. The reference makes a retry idempotent.
func ApplyVendorAdvance(tenantID, userID, advanceID, invoiceID string, amount float64, reference string) (VendorAdvanceResult, error) {
	var res VendorAdvanceResult
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	reference = strings.TrimSpace(reference)
	if err := validateSettlementInput(amount, reference); err != nil {
		return res, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	adv, err := loadVendorAdvanceTx(tx, schema, advanceID)
	if err != nil {
		return res, err
	}
	var billRaw []byte
	var billStatus string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1 FOR UPDATE`, schema), invoiceID).Scan(&billRaw, &billStatus); err != nil {
		return res, vendorAdvanceErr(vendorAdvanceBillCode, "%s was not found", billLabel(tx, schema, invoiceID, nil))
	}
	var bill map[string]interface{}
	if err := json.Unmarshal(billRaw, &bill); err != nil {
		return res, err
	}
	entries, err := readSettlementEntries(bill, "payments")
	if err != nil {
		return res, fmt.Errorf("vendor invoice %s has an unreadable payment history: %v", invoiceID, err)
	}
	if reference != "" {
		if prior, ok := findSettlementByReference(entries, reference); ok {
			res = vendorAdvanceResult(advanceID, adv)
			res.InvoiceID, res.InvoiceStatus, res.Reference, res.Replayed = invoiceID, billStatus, reference, true
			res.InvoiceOutstand = numFromInterface(bill["outstanding_amount"])
			res.Amount = prior.Amount
			return res, nil
		}
	}
	if st := strField(adv, "status"); st != vendorAdvanceOpen && st != vendorAdvancePart {
		return res, vendorAdvanceErr(vendorAdvanceNotOpen, "%s has nothing open (it is %s)", advanceLabel(tx, schema, adv), st)
	}
	if billStatus != "Matched" {
		return res, vendorAdvanceErr(vendorAdvanceBillCode, "%s is %s - an advance is applied to a Matched bill", billLabel(tx, schema, invoiceID, bill), billStatus)
	}
	if err := retroPOBlocksBill(tenantID, bill); err != nil { // Stage 148.38
		return res, err
	}
	advVendor, _, _ := findVendorRow(tx, schema, strField(adv, "vendor_id"))
	billVendor, _, _ := findVendorRow(tx, schema, firstNonEmpty(strField(bill, "vendor_id"), strField(bill, "vendor")))
	if advVendor == "" || advVendor != billVendor {
		return res, vendorAdvanceErr(vendorAdvanceBillCode, "%s is not for the vendor of %s", advanceLabel(tx, schema, adv), billLabel(tx, schema, invoiceID, bill))
	}
	if po := strField(adv, "po_id"); po != "" && strField(bill, "po_id") != po {
		return res, vendorAdvanceErr(vendorAdvanceBillCode, "%s was given against %s; %s is not for it", advanceLabel(tx, schema, adv), poLabel(tx, schema, po), billLabel(tx, schema, invoiceID, bill))
	}
	position := documentFXPosition(tenantID, bill, "invoice_amount")
	if position.Foreign {
		return res, vendorAdvanceErr(vendorAdvanceBillCode, "%s is in %s; an advance is applied to a rupee bill", billLabel(tx, schema, invoiceID, bill), position.Currency)
	}
	open, outstanding := vendorOpenLegs(bill, position)
	if outstanding <= 0 {
		return res, nothingOutstandingError("vendor invoice", invoiceID)
	}
	advOpen := RupeesToPaise(numFromInterface(adv["open_amount"]))
	pay := RupeesToPaise(amount)
	if amount == 0 {
		pay = advOpen
		if outstanding < pay {
			pay = outstanding
		}
	}
	if pay > advOpen {
		return res, vendorAdvanceErr(vendorAdvanceOverCode, "%.2f is more than the %.2f still open on %s", PaiseToRupees(pay), PaiseToRupees(advOpen), advanceLabel(tx, schema, adv))
	}
	if pay <= 0 || pay > outstanding {
		return res, overSettlementError("vendor invoice", invoiceID, pay, outstanding)
	}
	if reference == "" {
		reference = fmt.Sprintf("%s.%d", advanceID, len(vendorAdvanceLines(adv, "applications"))+1)
	}
	legs := splitVendorPayment(open, outstanding, pay)
	key := fmt.Sprintf("VendorInvoice:%s:ADVANCE:%s", invoiceID, reference)
	today := BusinessToday()
	if err := PostDoubleEntryTx(tx, tenantID, schema, "VendorInvoice", invoiceID, legs, map[string]int64{AccountVendorAdvances: pay}, today, key); err != nil {
		return res, postingError(err, "advance not applied")
	}
	remaining := noteVendorPayment(bill, entries, settlementKindAdvance, reference, key, userID, today, legs, position, PaiseToRupees(pay))
	newStatus := "Matched"
	if remaining <= 0 {
		newStatus = "Paid"
	}
	bill["status"] = newStatus
	var appliedToBill int64
	for _, e := range bill["payments"].([]SettlementEntry) {
		if e.Kind == settlementKindAdvance {
			appliedToBill += RupeesToPaise(e.Amount)
		}
	}
	bill["advance_applied"] = PaiseToRupees(appliedToBill)
	bb, _ := json.Marshal(bill)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorInvoice' AND id = $3`, schema), bb, newStatus, invoiceID); err != nil {
		return res, err
	}
	adv["applications"] = append(vendorAdvanceLines(adv, "applications"), vendorAdvanceLine{InvoiceID: invoiceID, Amount: PaiseToRupees(pay), Reference: reference, Date: today, By: userID})
	vendorAdvanceSettle(adv)
	if err := saveVendorAdvanceTx(tx, schema, advanceID, adv); err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	LogAuditEvent(tenantID, userID, "VENDOR_ADVANCE_APPLIED", "SUCCESS", fmt.Sprintf("Applied %.2f of advance %s to vendor invoice %s", PaiseToRupees(pay), advanceID, invoiceID))
	res = vendorAdvanceResult(advanceID, adv)
	res.Amount, res.InvoiceID, res.InvoiceStatus, res.Reference = PaiseToRupees(pay), invoiceID, newStatus, reference
	res.InvoiceOutstand = PaiseToRupees(remaining)
	return res, nil
}

// RefundVendorAdvance books back unused advance the vendor returned: Dr 1100 /
// Cr 1810. amount 0 refunds all that is open.
func RefundVendorAdvance(tenantID, userID, advanceID string, amount float64, reference, date string) (VendorAdvanceResult, error) {
	var res VendorAdvanceResult
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	reference = strings.TrimSpace(reference)
	if err := validateSettlementInput(amount, reference); err != nil {
		return res, err
	}
	if date == "" {
		date = BusinessToday()
	}
	if !vendorAdvanceValidDate(date) {
		return res, vendorAdvanceErr(vendorAdvanceInputCode, "the refund date %q is not a date (use YYYY-MM-DD)", date)
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	adv, err := loadVendorAdvanceTx(tx, schema, advanceID)
	if err != nil {
		return res, err
	}
	refunds := vendorAdvanceLines(adv, "refunds")
	if reference != "" {
		for _, l := range refunds {
			if l.Reference == reference {
				res = vendorAdvanceResult(advanceID, adv)
				res.Amount, res.Reference, res.Replayed = l.Amount, reference, true
				return res, nil
			}
		}
	}
	if st := strField(adv, "status"); st != vendorAdvanceOpen && st != vendorAdvancePart {
		return res, vendorAdvanceErr(vendorAdvanceNotOpen, "%s has nothing open (it is %s)", advanceLabel(tx, schema, adv), st)
	}
	advOpen := RupeesToPaise(numFromInterface(adv["open_amount"]))
	pay := RupeesToPaise(amount)
	if amount == 0 {
		pay = advOpen
	}
	if pay <= 0 || pay > advOpen {
		return res, vendorAdvanceErr(vendorAdvanceOverCode, "%.2f is more than the %.2f still open on %s", PaiseToRupees(pay), PaiseToRupees(advOpen), advanceLabel(tx, schema, adv))
	}
	if reference == "" {
		reference = fmt.Sprintf("R%d", len(refunds)+1)
	}
	if err := PostDoubleEntryTx(tx, tenantID, schema, VendorAdvanceDoctype, advanceID,
		map[string]int64{"1100": pay}, map[string]int64{AccountVendorAdvances: pay}, date, "VendorAdvance:"+advanceID+":REFUND:"+reference); err != nil {
		return res, postingError(err, "refund not recorded")
	}
	adv["refunds"] = append(refunds, vendorAdvanceLine{Amount: PaiseToRupees(pay), Reference: reference, Date: date, By: userID})
	vendorAdvanceSettle(adv)
	if err := saveVendorAdvanceTx(tx, schema, advanceID, adv); err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	LogAuditEvent(tenantID, userID, "VENDOR_ADVANCE_REFUNDED", "SUCCESS", fmt.Sprintf("Refund of %.2f booked on advance %s", PaiseToRupees(pay), advanceID))
	res = vendorAdvanceResult(advanceID, adv)
	res.Amount, res.Reference = PaiseToRupees(pay), reference
	return res, nil
}

// CancelVendorAdvance reverses an advance nothing has touched. One already
// applied to a bill or refunded in part is refused: the bill carries it.
func CancelVendorAdvance(tenantID, userID, advanceID string) (VendorAdvanceResult, error) {
	var res VendorAdvanceResult
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	adv, err := loadVendorAdvanceTx(tx, schema, advanceID)
	if err != nil {
		return res, err
	}
	if strField(adv, "status") == vendorAdvanceCancel {
		res = vendorAdvanceResult(advanceID, adv)
		res.Replayed = true
		return res, nil
	}
	if len(vendorAdvanceLines(adv, "applications")) > 0 || len(vendorAdvanceLines(adv, "refunds")) > 0 {
		return res, vendorAdvanceErr(vendorAdvanceCancelCod, "%s has been applied to a bill or refunded, so it cannot be cancelled", advanceLabel(tx, schema, adv))
	}
	paise := RupeesToPaise(numFromInterface(adv["amount"]))
	date := BusinessToday()
	if err := PostDoubleEntryTx(tx, tenantID, schema, VendorAdvanceDoctype, advanceID,
		map[string]int64{"1100": paise}, map[string]int64{AccountVendorAdvances: paise}, date, "VendorAdvance:"+advanceID+":CANCEL"); err != nil {
		return res, postingError(err, "advance not cancelled")
	}
	adv["status"] = vendorAdvanceCancel
	adv["cancelled_by"] = userID
	vendorAdvanceSettle(adv)
	adv["open_amount"] = float64(0)
	if err := saveVendorAdvanceTx(tx, schema, advanceID, adv); err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	LogAuditEvent(tenantID, userID, "VENDOR_ADVANCE_CANCELLED", "SUCCESS", fmt.Sprintf("Advance %s cancelled", advanceID))
	return vendorAdvanceResult(advanceID, adv), nil
}
