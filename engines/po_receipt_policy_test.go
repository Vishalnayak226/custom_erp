package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// TestPOReceiptPolicyStage7324 runs decision D05 on a provisioned throwaway
// client: a rejected unit stays open on the PO for replacement, the buyer
// short-closes the rest with a reason (raising the claim on the rejected
// goods), a closed PO takes no more receipts or amendments, and the
// reconciliation view adds up - ordered / accepted / rejected / open /
// invoiced / paid.
func TestPOReceiptPolicyStage7324(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	js := func(v interface{}) string { b, _ := json.Marshal(v); return string(b) }
	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	const skuA, skuB, vendor, po, loc = "D05-A", "D05-B", "D05-V", "D05-PO", "D05-LOC"
	insert(skuA, "Item", "Active", map[string]interface{}{"code": skuA, "name": "D05 shirt", "hsn_code": "6205", "gst_rate": 5.0, "tax_treatment": "Taxable"})
	insert(skuB, "Item", "Active", map[string]interface{}{"code": skuB, "name": "D05 tie", "hsn_code": "6215", "gst_rate": 5.0, "tax_treatment": "Taxable"})
	insert(vendor, "Vendor", "Active", map[string]interface{}{"code": vendor, "name": "D05 Vendor"})
	insert("RC-D05-CXL", "ReasonCode", "Active", map[string]interface{}{"code": "RC-D05-CXL", "category": "Cancellation", "description": "Vendor cannot supply"})
	insert(po, "PurchaseOrder", "Approved", map[string]interface{}{"code": po, "vendor": vendor, "location": loc, "gst_mode": GSTModeExclusive,
		"total_amount": 1250, "items": js([]map[string]interface{}{{"sku": skuA, "qty": 10, "rate": 100.0}, {"sku": skuB, "qty": 5, "rate": 50.0}})})
	grn := func(id string, lines ...map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"code": id, "po_id": po, "vendor": vendor, "location": loc, "received_items": js(lines)}
	}

	// Delivery 1: 10 shirts, 2 rejected; 5 ties all fine.
	g1 := grn("D05-GRN-1", map[string]interface{}{"sku": skuA, "qty": 10, "accepted_qty": 8, "rejected_qty": 2, "rejection_reason": "torn"},
		map[string]interface{}{"sku": skuB, "qty": 5, "accepted_qty": 5})
	if err := validateGRNRules(tenantID, "", g1); err != nil {
		t.Fatalf("first receipt: %v", err)
	}
	insert("D05-GRN-1", "GRN", "Posted", g1)

	rec, err := PurchaseOrderReconciliation(tenantID, po)
	if err != nil {
		t.Fatalf("reconciliation: %v", err)
	}
	byS := map[string]POReconLine{}
	for _, l := range rec.Lines {
		byS[l.Sku] = l
	}
	if a := byS[skuA]; a.Ordered != 10 || a.Accepted != 8 || a.Rejected != 2 || a.Open != 2 || a.Rate != 100 {
		t.Fatalf("shirt line %+v, want ordered 10 accepted 8 rejected 2 open 2 at 100", a)
	}
	if b := byS[skuB]; b.Open != 0 || b.Accepted != 5 {
		t.Fatalf("tie line %+v", b)
	}
	if rec.OpenValue != 200 || rec.AcceptedValue != 1050 {
		t.Fatalf("values open %v accepted %v, want 200 / 1050", rec.OpenValue, rec.AcceptedValue)
	}

	// D05: the 2 rejected shirts stay open - a replacement of 2 is accepted,
	// 3 is an over-receipt.
	if err := validateGRNRules(tenantID, "", grn("D05-GRN-X", map[string]interface{}{"sku": skuA, "qty": 2, "accepted_qty": 2})); err != nil {
		t.Fatalf("a replacement for the rejected units must be allowed: %v", err)
	}
	if got := code(validateGRNRules(tenantID, "", grn("D05-GRN-Y", map[string]interface{}{"sku": skuA, "qty": 3, "accepted_qty": 3}))); got != "PURCHA-0087" {
		t.Fatalf("3 against 2 open: want PURCHA-0087, got %q", got)
	}
	// A delivery whose extra unit is rejected is not an over-receipt: only accepted units count.
	if err := validateGRNRules(tenantID, "", grn("D05-GRN-Z", map[string]interface{}{"sku": skuA, "qty": 3, "accepted_qty": 2, "rejected_qty": 1, "rejection_reason": "crushed"})); err != nil {
		t.Fatalf("2 accepted + 1 rejected against 2 open: %v", err)
	}

	// Billing: the receipt's invoice is paid.
	insert("D05-VI", "VendorInvoice", "Paid", map[string]interface{}{"invoice_number": "INV-1", "vendor_id": vendor, "po_id": po, "grn_id": "D05-GRN-1", "invoice_amount": 1102.5})
	rec, _ = PurchaseOrderReconciliation(tenantID, po)
	for _, l := range rec.Lines {
		if l.Invoiced != l.Accepted || l.Paid != l.Accepted {
			t.Fatalf("line %s invoiced %v paid %v, want the accepted %v", l.Sku, l.Invoiced, l.Paid, l.Accepted)
		}
	}
	if rec.InvoicedAmount != 1102.5 || rec.PaidAmount != 1102.5 || len(rec.GRNs) != 1 || len(rec.GRNs[0].Invoices) != 1 {
		t.Fatalf("money/receipts: %+v", rec)
	}

	// Short-close: a reason is required; then the balance closes and the
	// rejected shirts are put on a Draft purchase return - the claim.
	if _, err := ShortClosePurchaseOrder(tenantID, po, "buyer", "", "", "HQ"); code(err) != "PURCHA-0487" {
		t.Fatalf("no reason: want PURCHA-0487, got %v", err)
	}
	res, err := ShortClosePurchaseOrder(tenantID, po, "buyer", "RC-D05-CXL", "vendor out of stock", "HQ")
	if err != nil || res.ClosedUnits != 2 || len(res.Claims) != 1 {
		t.Fatalf("short-close: %v %+v", err, res)
	}
	claim, status, err := fetchDocData(tenantID, "PurchaseReturn", res.Claims[0])
	if err != nil || status != "Draft" || str(claim, "grn_id") != "D05-GRN-1" || str(claim, "vendor_id") != vendor {
		t.Fatalf("claim %s: %v %s %v", res.Claims[0], err, status, claim)
	}
	var claimLines []PurchaseReturnLine
	json.Unmarshal([]byte(str(claim, "return_items")), &claimLines)
	if len(claimLines) != 1 || claimLines[0].SKU != skuA || claimLines[0].StockBucket != PurchaseReturnBucketRejected || claimLines[0].Qty != 2 {
		t.Fatalf("claim lines %+v, want 2 rejected shirts", claimLines)
	}
	rec, _ = PurchaseOrderReconciliation(tenantID, po)
	if rec.Status != "Closed" || !rec.ShortClosed || len(rec.Claims) != 1 {
		t.Fatalf("after short-close: %+v", rec)
	}
	for _, l := range rec.Lines {
		if l.Sku == skuA && (l.ShortClosed != 2 || l.Open != 0) {
			t.Fatalf("shirt line after short-close %+v", l)
		}
	}

	// A closed PO takes no more receipts and no amendment; it cannot close twice.
	if got := code(validateGRNRules(tenantID, "", grn("D05-GRN-W", map[string]interface{}{"sku": skuA, "qty": 1, "accepted_qty": 1}))); got != "PURCHA-0489" {
		t.Fatalf("receipt on a closed PO: want PURCHA-0489, got %q", got)
	}
	poData, _, _ := fetchDocData(tenantID, "PurchaseOrder", po)
	amended := map[string]interface{}{"items": js([]map[string]interface{}{{"sku": skuA, "qty": 12, "rate": 100.0}}), "total_amount": 1200}
	if got := code(validatePurchaseOrderEditRules(tenantID, po, "Closed", poData, amended)); got != "PURCHA-0489" {
		t.Fatalf("amending a closed PO: want PURCHA-0489, got %q", got)
	}
	if _, err := ShortClosePurchaseOrder(tenantID, po, "buyer", "RC-D05-CXL", "", "HQ"); code(err) != "PURCHA-0488" {
		t.Fatalf("closing twice: want PURCHA-0488, got %v", err)
	}
}
