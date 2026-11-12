package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

// Fix pass on 148.32: one bill per shipped line, input GST on the bill, and a
// cancelled order takes its Draft PO with it.

func dsApprovedOrder(t *testing.T, f *wsFix, qty int) (order, poID string) {
	t.Helper()
	order = f.dropOrder(SalesOrderLineInput{SKU: "WS-D", Qty: qty, UnitPrice: 100})
	poID, _, _ = f.poOf(order)
	f.setPOStatus(poID, "Approved")
	return order, poID
}

func (f *wsFix) billsOf(poID string) int {
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype='VendorInvoice' AND data->>'source_doc_id' = $1`, f.schema), poID).Scan(&n)
	return n
}

func TestStage14832FixConcurrentConfirmBillsOnce(t *testing.T) {
	f := dsFixture(t)
	order, poID := dsApprovedOrder(t, f, 5)
	// two confirmations read the same open list before either commits
	stale, err := OpenDropShipLines(f.tenantID, "")
	if err != nil || len(stale) != 1 || stale[0].LineID != order+"-L1" {
		t.Fatalf("open lines: %+v %v", stale, err)
	}
	if _, err := confirmDropShipLines(f.tenantID, "u1", stale, &DropShipConfirmResult{}); err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	_, err = confirmDropShipLines(f.tenantID, "u2", stale, &DropShipConfirmResult{})
	wantCode(t, err, wsAlreadyCode)
	if n := f.billsOf(poID); n != 1 {
		t.Fatalf("%d vendor bills for one shipped line, want 1", n)
	}
	var cost int64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(sum(debit),0) FROM %s.gl_postings WHERE document_type='VendorInvoice' AND account_code='5100'`, f.schema)).Scan(&cost)
	if cost != 30000 {
		t.Fatalf("5100 debited %d, want 30000", cost)
	}
}

func TestStage14832FixBillCarriesInputGST(t *testing.T) {
	f := dsFixture(t)
	order, _ := dsApprovedOrder(t, f, 5)
	_ = order
	res, err := ConfirmDropShip(f.tenantID, "u1", nil, true, "")
	if err != nil || len(res.Bills) != 1 {
		t.Fatalf("confirm: %+v %v", res, err)
	}
	bill := res.Bills[0]
	// 5 x 60 = 300.00 taxable, 18% input GST = 54.00, owed to the vendor 354.00
	if dr, cr := f.gl("VendorInvoice", bill, "5100"); dr != 30000 || cr != 0 {
		t.Fatalf("5100 dr=%d cr=%d", dr, cr)
	}
	if dr, cr := f.gl("VendorInvoice", bill, "1500"); dr != 5400 || cr != 0 {
		t.Fatalf("1500 dr=%d cr=%d", dr, cr)
	}
	if dr, cr := f.gl("VendorInvoice", bill, "2100"); dr != 0 || cr != 35400 {
		t.Fatalf("2100 dr=%d cr=%d", dr, cr)
	}
	d := f.doc("VendorInvoice", bill)
	if numFromInterface(d["invoice_amount"]) != 354 || numFromInterface(d["gst_amount"]) != 54 {
		t.Fatalf("bill amounts: %v %v", d["invoice_amount"], d["gst_amount"])
	}
	// the vendor can be paid in full: the payment basis is the gross, 2100 only
	_, total := vendorOpenLegs(d, DocumentFXPosition{})
	if total != 35400 {
		t.Fatalf("open to pay %d, want 35400", total)
	}
}

func TestStage14832FixCancelOrderTakesTheDraftPO(t *testing.T) {
	f := dsFixture(t)
	_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE doctype = 'ReasonCode' AND id = 'RC-FXW-CANCEL'`, f.schema))
	rc, _ := json.Marshal(map[string]interface{}{"code": "RC-FXW-CANCEL", "description": "x", "category": "Cancellation", "status": "Active"})
	f.insert("RC-FXW-CANCEL", "ReasonCode", "Active", "system", map[string]interface{}{})
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1 WHERE doctype='ReasonCode' AND id='RC-FXW-CANCEL'`, f.schema), rc)

	// a Draft PO is cancelled with the order
	o1 := f.dropOrder(SalesOrderLineInput{SKU: "WS-D", Qty: 2, UnitPrice: 100})
	po1, _, _ := f.poOf(o1)
	if err := CancelOrder(f.tenantID, o1, "RC-FXW-CANCEL", "u1"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	var st string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype='PurchaseOrder' AND id=$1`, f.schema), po1).Scan(&st)
	if st != "Cancelled" {
		t.Fatalf("PO %s after the order was cancelled", st)
	}

	// a PO past Draft refuses the cancel and nothing changes
	o2 := f.dropOrder(SalesOrderLineInput{SKU: "WS-D", Qty: 2, UnitPrice: 100})
	po2, _, _ := f.poOf(o2)
	f.setPOStatus(po2, "Approved")
	wantCode(t, CancelOrder(f.tenantID, o2, "RC-FXW-CANCEL", "u1"), wsPOLockedCode)
	if s := strField(f.doc("SalesOrder", o2), "order_status"); s == "Cancelled" {
		t.Fatal("order cancelled although its PO is live")
	}
}

func TestStage14832FixGenericSaveCannotForgeEngineMarks(t *testing.T) {
	f := dsFixture(t)
	order := f.dropOrder(SalesOrderLineInput{SKU: "WS-D", Qty: 2, UnitPrice: 100})
	lineID := order + "-L1"
	stored := f.doc("SalesOrderLine", lineID)
	if strField(stored, "dropship") != "Yes" || strField(stored, "po_id") == "" {
		t.Fatalf("stored line has no drop-ship marks: %+v", stored)
	}
	// an edit that forges the link keeps what is stored
	forged := map[string]interface{}{"sku": "WS-D", "qty": 2, "dropship": "No", "vendor_id": "WS-OTHER", "po_id": "PO-FORGED", "po_line": 9}
	KeepEngineOnlyFields(f.tenantID, "SalesOrderLine", lineID, forged)
	for _, k := range []string{"dropship", "vendor_id", "po_id"} {
		if forged[k] != stored[k] {
			t.Errorf("line %s forged: %v, stored %v", k, forged[k], stored[k])
		}
	}
	// a new line / invoice / order made by a plain save carries none
	newLine := map[string]interface{}{"sku": "WS-D", "dropship": "Yes", "vendor_id": "WS-V", "po_id": "PO-FORGED"}
	KeepEngineOnlyFields(f.tenantID, "SalesOrderLine", "", newLine)
	inv := map[string]interface{}{"customer_id": "WS-C", "summary_order_ids": order, "summary_lines": []interface{}{}, "delivery_challans": []interface{}{}}
	KeepEngineOnlyFields(f.tenantID, "SalesInvoice", "", inv)
	so := map[string]interface{}{"made_by_assistant": true, "assistant_user": "u", "assistant_client": "c", "dropship": "Yes"}
	KeepEngineOnlyFields(f.tenantID, "SalesOrder", "", so)
	for name, m := range map[string]map[string]interface{}{"line": newLine, "invoice": inv, "order": so} {
		for k := range m {
			switch k {
			case "dropship", "vendor_id", "po_id", "summary_order_ids", "summary_lines", "delivery_challans", "made_by_assistant", "assistant_user", "assistant_client":
				t.Errorf("new %s kept forged %s", name, k)
			}
		}
	}
	// other documents are not touched
	other := map[string]interface{}{"vendor_id": "V1", "po_id": "P1"}
	KeepEngineOnlyFields(f.tenantID, "GRN", "", other)
	if len(other) != 2 {
		t.Errorf("GRN changed: %v", other)
	}
}
