package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 148.9 - vendor master basics.
//
//   - Hold: a vendor can be held from new purchase orders, from payment, or
//     both. The PO side is RequirePOStates' third check (every PO path already
//     runs it: record save, CSV import, submit for approval, RFQ award); the
//     payment side is PostDoubleEntryTx, which every vendor-payment path posts
//     through (PayVendorInvoice, the payment run, an override, TDS and part
//     payments, the GRN-step payments), so no path skips it.
//   - Terms: the vendor's default payment term is the fallback when the PO has
//     none; a term can run to the end of the month; an MSME vendor's bill is
//     due no later than 45 days after acceptance (MSMED Act s.15).
//   - Early-payment discount: when the payment run pays a bill inside its
//     term's discount window the discount is taken off the goods leg first.
//
// A blank hold / term / MSME flag changes nothing: every due date and every
// payment is what it was.

const (
	VendorHoldNoPO      = "No new PO"
	VendorHoldNoPayment = "No payment"
	VendorHoldBoth      = "Both"

	VendorPOHoldCode      = "VENDOR-0190"
	VendorPaymentHoldCode = "VENDOR-0191"
	VendorChangeWaitCode  = "VENDOR-0192"

	// MSMEPaymentDays is the legal ceiling from acceptance for an MSME vendor.
	MSMEPaymentDays = 45

	earlyDiscountAccount = "5150" // Purchase Returns & Allowances: the contra-expense the chart already has
)

type docQuerier interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

// findVendorRow is the Vendor document a bill / PO names (by id or code).
func findVendorRow(q docQuerier, schema, ref string) (id string, data map[string]interface{}, ok bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil, false
	}
	var raw []byte
	err := q.QueryRow(fmt.Sprintf(`SELECT id, data FROM %s.documents
		WHERE doctype = 'Vendor' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1)
		ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END LIMIT 1`, schema), ref).Scan(&id, &raw)
	if err != nil || json.Unmarshal(raw, &data) != nil {
		return "", nil, false
	}
	return id, data, true
}

// checkPOVendorHold refuses a purchase order still being drafted for a vendor
// held from new orders. An order already past Draft is not touched, so a held
// vendor's open orders can still be received and closed.
func checkPOVendorHold(tenantID string, po map[string]interface{}) error {
	if st := strings.TrimSpace(strField(po, "status")); st != "" && st != "Draft" {
		return nil
	}
	return checkVendorNoNewPO(tenantID, firstNonEmpty(strField(po, "vendor"), strField(po, "vendor_id")))
}

// checkVendorNoNewPO is the hold test alone, for a caller that has only the vendor.
func checkVendorNoNewPO(tenantID, vendorRef string) error {
	if db.DB == nil || strings.TrimSpace(vendorRef) == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, v, ok := findVendorRow(db.DB, schema, vendorRef)
	if !ok {
		return nil
	}
	if h := strField(v, "hold"); h == VendorHoldNoPO || h == VendorHoldBoth {
		return &ValidationError{Code: VendorPOHoldCode, SubFor: "Vendor", Message: fmt.Sprintf(
			"Vendor %s is on hold for new purchase orders - take the hold off the vendor first", firstNonEmpty(strField(v, "name"), vendorRef))}
	}
	return nil
}

// checkVendorPaymentAllowedTx is PostDoubleEntryTx's gate for a vendor payment:
// a bill, GRN or advance payment to a vendor held from payment, or whose bank / terms
// change is still waiting for approval, is refused. Cancellations are let
// through, as the account check does.
func checkVendorPaymentAllowedTx(tx *sql.Tx, schema, docType, docID, postingKey string) error {
	if (docType != "VendorInvoice" && docType != "GRN" && docType != VendorAdvanceDoctype) || !strings.Contains(postingKey, ":PAY") || strings.Contains(postingKey, ":CANCEL") {
		return nil
	}
	var ref string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'vendor_id', ''), data->>'vendor', '') FROM %s.documents WHERE doctype = $1 AND id = $2`, schema), docType, docID).Scan(&ref); err != nil {
		return nil
	}
	vid, v, ok := findVendorRow(tx, schema, ref)
	if !ok {
		return nil
	}
	name := firstNonEmpty(strField(v, "name"), ref)
	if h := strField(v, "hold"); h == VendorHoldNoPayment || h == VendorHoldBoth {
		return &ValidationError{Code: VendorPaymentHoldCode, SubFor: "Vendor", Message: fmt.Sprintf(
			"Vendor %s is on hold for payment - take the hold off the vendor first", name)}
	}
	var waiting bool
	_ = tx.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.documents WHERE doctype = 'VendorChange' AND deleted_at IS NULL AND status = 'Pending Approval' AND data->>'vendor_id' = $1)`, schema), vid).Scan(&waiting)
	if waiting {
		return &ValidationError{Code: VendorChangeWaitCode, SubFor: "Vendor", Message: fmt.Sprintf(
			"A change to vendor %s is waiting for approval - payments to it resume once it is decided", name)}
	}
	return nil
}

// billPaymentTerm is the term a bill is due under: its PO's, else the vendor's.
func billPaymentTerm(tenantID string, bill map[string]interface{}) string {
	if po := strField(bill, "po_id"); po != "" {
		if p, _, err := fetchDocData(tenantID, "PurchaseOrder", po); err == nil && p != nil && strField(p, "payment_term") != "" {
			return strField(p, "payment_term")
		}
	}
	return vendorDefaultTerm(tenantID, firstNonEmpty(strField(bill, "vendor_id"), strField(bill, "vendor")))
}

func vendorDefaultTerm(tenantID, vendorRef string) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || db.DB == nil {
		return ""
	}
	_, v, ok := findVendorRow(db.DB, schema, vendorRef)
	if !ok {
		return ""
	}
	return strings.TrimSpace(strField(v, "payment_term"))
}

func poHasTerm(tenantID, poID string) bool {
	p, _, err := fetchDocData(tenantID, "PurchaseOrder", poID)
	return err == nil && p != nil && strField(p, "payment_term") != ""
}

// VendorBillDueDate is a vendor bill's due date: the PO's payment term, else
// the vendor's default term, from the invoice date; an MSME vendor's bill is
// never due later than MSMEPaymentDays after acceptance (the goods receipt;
// the invoice date when none is given). With no term and no MSME flag it is "",
// as before.
func VendorBillDueDate(tenantID, poID, vendorRef, invoiceDate, acceptanceDate string) string {
	due := POPaymentDueDate(tenantID, poID, invoiceDate)
	if due == "" && (poID == "" || !poHasTerm(tenantID, poID)) {
		due = paymentTermDueDate(tenantID, vendorDefaultTerm(tenantID, vendorRef), invoiceDate)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || db.DB == nil {
		return due
	}
	if _, v, ok := findVendorRow(db.DB, schema, vendorRef); ok && strField(v, "msme") == "Yes" {
		base := firstNonEmpty(acceptanceDate, invoiceDate)
		if d, perr := time.ParseInLocation("2006-01-02", strings.TrimSpace(base), time.Local); perr == nil {
			limit := d.AddDate(0, 0, MSMEPaymentDays).Format("2006-01-02")
			if due == "" || due > limit {
				due = limit
			}
		}
	}
	return due
}

// paymentTermDiscount is a term's early-payment discount: days and percent (0, 0 when none).
func paymentTermDiscount(tenantID, term string) (int, float64) {
	if term == "" {
		return 0, 0
	}
	t, _, err := fetchDocData(tenantID, "PaymentTerm", term)
	if err != nil || t == nil {
		return 0, 0
	}
	return int(numFromInterface(t["discount_days"])), numFromInterface(t["discount_pct"])
}

// applyEarlyPaymentDiscount is the payment run's pre-step: a Matched bill paid
// inside its term's discount window has the discount taken off its goods leg
// (Dr 2100 GRN Suspense / Cr 5150), recorded as a settlement on the bill, so
// the payment that follows clears only the remainder. Returns the discount in
// paise (0 when none applies or it was already taken). The input GST is not
// reduced. A hold on the vendor stops it before anything posts.
func applyEarlyPaymentDiscount(tenantID, userID, billID string) (int64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	bill, status, err := fetchDocData(tenantID, "VendorInvoice", billID)
	if err != nil || bill == nil || status != "Matched" {
		return 0, nil
	}
	days, pct := paymentTermDiscount(tenantID, billPaymentTerm(tenantID, bill))
	if days <= 0 || pct <= 0 {
		return 0, nil
	}
	inv, perr := time.ParseInLocation("2006-01-02", strings.TrimSpace(strField(bill, "invoice_date")), time.Local)
	if perr != nil {
		return 0, nil
	}
	today := BusinessToday()
	if today > inv.AddDate(0, 0, days).Format("2006-01-02") {
		return 0, nil
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var raw []byte
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1 FOR UPDATE`, schema), billID).Scan(&raw); err != nil {
		return 0, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return 0, err
	}
	if err := checkVendorPaymentAllowedTx(tx, schema, "VendorInvoice", billID, "VendorInvoice:"+billID+":PAY"); err != nil {
		return 0, err
	}
	entries, err := readSettlementEntries(data, "payments")
	if err != nil {
		return 0, err
	}
	ref := "EPD:" + billID
	if _, done := findSettlementByReference(entries, ref); done {
		return 0, nil
	}
	position := documentFXPosition(tenantID, data, "invoice_amount")
	open, _ := vendorOpenLegs(data, position)
	disc := int64(math.Round(float64(open["2100"]) * pct / 100))
	if disc <= 0 {
		return 0, nil
	}
	key := fmt.Sprintf("VendorInvoice:%s:EPD", billID)
	// Stage 162.9: the discount posts in this transaction, with the bill's update.
	if err := PostDoubleEntryTx(tx, tenantID, schema, "VendorInvoice", billID, map[string]int64{"2100": disc}, map[string]int64{earlyDiscountAccount: disc}, today, key); err != nil {
		return 0, err
	}
	noteVendorPayment(data, entries, "Early-payment discount", ref, key, userID, today, map[string]int64{"2100": disc}, position, 0)
	b, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorInvoice' AND id = $2`, schema), b, billID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	LogAuditEvent(tenantID, userID, "VENDOR_EARLY_PAYMENT_DISCOUNT", "SUCCESS", fmt.Sprintf("vendor invoice %s: early-payment discount %.2f (%.2f%% within %d days)", billID, PaiseToRupees(disc), pct, days))
	return disc, nil
}

func isVendorHoldCode(code string) bool {
	return code == VendorPaymentHoldCode || code == VendorChangeWaitCode
}
