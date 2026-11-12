package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

// Fix pass on 148.31: re-creating a summary invoice after a cancel, packages of
// a summary payer, and the lines an e-invoice / print reads from the invoice.

func wsMakeSummary(t *testing.T, f *wsFix) (ids []string, orders []string) {
	t.Helper()
	wsSummaryCustomers(f, nil)
	o1 := f.order("WS-S1", "Karnataka", "PO-1", "Shipped", wsLine{sku: "WS-A", qty: 2, price: 118})
	o2 := f.order("WS-S2", "Karnataka", "PO-2", "Shipped", wsLine{sku: "WS-B", qty: 1, price: 118})
	p, err := PreviewSummaryInvoices(f.tenantID, "", "", "")
	if err != nil || len(p.Groups) != 1 {
		t.Fatalf("preview: %+v %v", p, err)
	}
	ids, err = CreateSummaryInvoices(f.tenantID, "system", "", "", "", []string{p.Groups[0].Key})
	if err != nil || len(ids) != 1 {
		t.Fatalf("create: %v %v", ids, err)
	}
	return ids, []string{o1, o2}
}

func TestStage14831FixSummaryInvoiceCanBeMadeAgainAfterACancel(t *testing.T) {
	f := newWSFix(t, true)
	ids, orders := wsMakeSummary(t, f)
	if ids[0] != "INV-SUM-"+orders[0] {
		t.Fatalf("first id %s", ids[0])
	}
	if err := CancelSalesInvoice(f.tenantID, ids[0], "system", "made in error"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	p, err := PreviewSummaryInvoices(f.tenantID, "", "", "")
	if err != nil || len(p.Groups) != 1 {
		t.Fatalf("preview after cancel: %+v %v", p, err)
	}
	again, err := CreateSummaryInvoices(f.tenantID, "system", "", "", "", []string{p.Groups[0].Key})
	if err != nil || len(again) != 1 {
		t.Fatalf("create again: %v %v", again, err)
	}
	if again[0] == ids[0] || again[0] != ids[0]+"-2" {
		t.Fatalf("second id %s", again[0])
	}
}

func TestStage14831FixPackageOfASummaryPayerIsNotBilledOnItsOwn(t *testing.T) {
	f := newWSFix(t, true)
	wsSummaryCustomers(f, nil)
	o1 := f.order("WS-S1", "Karnataka", "PO-1", "Shipped", wsLine{sku: "WS-A", qty: 2, price: 118})
	plain := f.order("WS-PLAIN", "Karnataka", "", "Shipped", wsLine{sku: "WS-A", qty: 2, price: 118})
	f.customer("WS-PLAIN", nil)
	for id, order := range map[string]string{"PKG-FXW-1": o1, "PKG-FXW-2": plain} {
		f.insert(id, "ShippingPackage", "Draft", "system", map[string]interface{}{"code": id, "order_id": order, "location_code": "WS-HQ",
			"items": []map[string]interface{}{{"sku": "WS-A", "qty": 2}}})
	}
	_, err := GenerateInvoiceForPackage(f.tenantID, "PKG-FXW-1", "system", PackInvoiceOptions{})
	wantCode(t, err, wsNothingCode)
	_, _, err = GenerateOMSBillForPackage(f.tenantID, "PKG-FXW-1", "system", PackInvoiceOptions{})
	wantCode(t, err, wsNothingCode)
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype IN ('OMSBill','SalesInvoice') AND data->>'shipping_package_id' = 'PKG-FXW-1'`, f.schema)).Scan(&n)
	if n != 0 {
		t.Fatalf("%d bills made for the summary payer's package", n)
	}
	// an ordinary customer's package is billed as before
	if _, err := GenerateInvoiceForPackage(f.tenantID, "PKG-FXW-2", "system", PackInvoiceOptions{}); err != nil {
		t.Fatalf("ordinary package refused: %v", err)
	}
	// module off: the summary payer's package is billed as before the module
	f.module(false)
	if err := refuseSummaryPackageBill(f.tenantID, o1); err != nil {
		t.Fatalf("module off but refused: %v", err)
	}
}

func TestStage14831FixSummaryInvoiceCarriesItsLinesForEInvoiceAndPrint(t *testing.T) {
	f := newWSFix(t, true)
	ids, _ := wsMakeSummary(t, f)
	lines := salesInvoiceLines(f.tenantID, f.schema, f.doc("SalesInvoice", ids[0]))
	if len(lines) != 2 {
		t.Fatalf("%d invoice lines, want 2: %+v", len(lines), lines)
	}
	bySKU := map[string]einvoiceLineSource{}
	for _, l := range lines {
		bySKU[l.SKU] = l
	}
	if a := bySKU["WS-A"]; a.Qty != 2 || a.Gross != 23600 {
		t.Fatalf("WS-A line %+v", a)
	}
	if b := bySKU["WS-B"]; b.Qty != 1 || b.Gross != 11800 {
		t.Fatalf("WS-B line %+v", b)
	}
	_ = json.Marshal
}
