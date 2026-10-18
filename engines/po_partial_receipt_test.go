package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// TestPOPartialReceiptRules pins the user's rules (2026-10-17): "PO can't be
// deleted if GRN done. PO can be partially closed if half GRN done. GRN can
// be done partially from multiple PO or single PO."
func TestPOPartialReceiptRules(t *testing.T) {
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
	receipts := func(po string) []string {
		t.Helper()
		r, err := PurchaseOrderReceipts(tenantID, po)
		if err != nil {
			t.Fatalf("receipts %s: %v", po, err)
		}
		return r
	}
	const skuA, skuB, vendor, loc = "PR-A", "PR-B", "PR-V", "PR-LOC"
	const po1, po2, po3 = "PR-PO-1", "PR-PO-2", "PR-PO-3"
	insert(skuA, "Item", "Active", map[string]interface{}{"code": skuA, "name": "Partial shirt", "hsn_code": "6205", "gst_rate": 5.0, "tax_treatment": "Taxable"})
	insert(skuB, "Item", "Active", map[string]interface{}{"code": skuB, "name": "Partial tie", "hsn_code": "6215", "gst_rate": 5.0, "tax_treatment": "Taxable"})
	insert(vendor, "Vendor", "Active", map[string]interface{}{"code": vendor, "name": "Partial Vendor"})
	insert("RC-PR-CXL", "ReasonCode", "Active", map[string]interface{}{"code": "RC-PR-CXL", "category": "Cancellation", "description": "Balance not needed"})
	for po, sku := range map[string]string{po1: skuA, po2: skuB, po3: skuA} {
		qty := map[string]int{po1: 10, po2: 8, po3: 6}[po]
		insert(po, "PurchaseOrder", "Approved", map[string]interface{}{"code": po, "vendor": vendor, "location": loc, "gst_mode": GSTModeExclusive,
			"items": js([]map[string]interface{}{{"sku": sku, "qty": qty, "rate": 100.0}})})
	}
	single := func(po string, qty int) map[string]interface{} {
		return map[string]interface{}{"po_id": po, "vendor": vendor, "location": loc,
			"received_items": js([]map[string]interface{}{{"sku": skuA, "qty": qty, "accepted_qty": qty}})}
	}
	multi := func(b2, a3 int) map[string]interface{} {
		var lines []map[string]interface{}
		if b2 > 0 {
			lines = append(lines, map[string]interface{}{"po_id": po2, "sku": skuB, "qty": b2, "accepted_qty": b2})
		}
		if a3 > 0 {
			lines = append(lines, map[string]interface{}{"po_id": po3, "sku": skuA, "qty": a3, "accepted_qty": a3})
		}
		return map[string]interface{}{"po_id": po2, "po_ids": po2 + "," + po3, "vendor": vendor, "location": loc, "received_items": js(lines)}
	}

	// A PO with no receipt can be deleted.
	if r := receipts(po1); len(r) != 0 {
		t.Fatalf("PO1 before any receipt: %v", r)
	}

	// Single PO, received in two halves.
	if err := validateGRNRules(tenantID, "", single(po1, 5)); err != nil {
		t.Fatalf("half of PO1: %v", err)
	}
	insert("PR-GRN-1", "GRN", "Posted", single(po1, 5))
	if r := receipts(po1); len(r) != 1 || r[0] != "PR-GRN-1" {
		t.Fatalf("PO1 after half received must list PR-GRN-1 (delete refused), got %v", r)
	}
	if got := code(validateGRNRules(tenantID, "", single(po1, 6))); got != "PURCHA-0087" {
		t.Fatalf("6 against the 5 still open: want PURCHA-0087, got %q", got)
	}
	if err := validateGRNRules(tenantID, "", single(po1, 5)); err != nil {
		t.Fatalf("the other half of PO1: %v", err)
	}
	insert("PR-GRN-2", "GRN", "Posted", single(po1, 5))
	if got := code(validateGRNRules(tenantID, "", single(po1, 1))); got != "PURCHA-0084" {
		t.Fatalf("receipt on a fully received PO: want PURCHA-0084, got %q", got)
	}

	// One GRN taking part of two POs.
	if err := validateGRNRules(tenantID, "", multi(3, 2)); err != nil {
		t.Fatalf("3 of PO2 + 2 of PO3 on one GRN: %v", err)
	}
	insert("PR-GRN-3", "GRN", "Posted", multi(3, 2))
	for _, po := range []string{po2, po3} {
		if r := receipts(po); len(r) != 1 || r[0] != "PR-GRN-3" {
			t.Fatalf("%s must list the multi-PO receipt (delete refused), got %v", po, r)
		}
	}
	if got := code(validateGRNRules(tenantID, "", multi(6, 0))); got != "PURCHA-0087" {
		t.Fatalf("6 of PO2's 5 still open: want PURCHA-0087, got %q", got)
	}

	// PO3 is short-closed half received: its 4 open units close, it takes no
	// more receipts, and it still cannot be deleted.
	res, err := ShortClosePurchaseOrder(tenantID, po3, "buyer", "RC-PR-CXL", "rest not needed", "HQ")
	if err != nil || res.ClosedUnits != 4 {
		t.Fatalf("short-close PO3: %v %+v", err, res)
	}
	rec, _ := PurchaseOrderReconciliation(tenantID, po3)
	if rec.Status != "Closed" || !rec.ShortClosed || len(rec.Lines) != 1 || rec.Lines[0].Accepted != 2 || rec.Lines[0].ShortClosed != 4 || rec.Lines[0].Open != 0 {
		t.Fatalf("PO3 after short-close: %+v", rec)
	}
	if got := code(validateGRNRules(tenantID, "", multi(0, 1))); got != "PURCHA-0489" {
		t.Fatalf("receipt on the short-closed PO3: want PURCHA-0489, got %q", got)
	}
	if r := receipts(po3); len(r) != 1 {
		t.Fatalf("short-closed PO3 still has its receipt (delete refused), got %v", r)
	}

	// The rest of PO2 arrives on a later GRN; PO2 is then fully received.
	rest := map[string]interface{}{"po_id": po2, "po_ids": po2, "vendor": vendor, "location": loc,
		"received_items": js([]map[string]interface{}{{"po_id": po2, "sku": skuB, "qty": 5, "accepted_qty": 5}})}
	if err := validateGRNRules(tenantID, "", rest); err != nil {
		t.Fatalf("the rest of PO2: %v", err)
	}
	insert("PR-GRN-4", "GRN", "Posted", rest)
	if r := receipts(po2); len(r) != 2 {
		t.Fatalf("PO2 receipts = %v, want PR-GRN-3 and PR-GRN-4", r)
	}
	if full, err := purchaseOrderFullyReceived(tenantID, po2, ""); err != nil || !full {
		t.Fatalf("PO2 fully received = %v (%v)", full, err)
	}
}
