package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Stage 148.32: drop-ship. The vendor ships to the customer; our stores never
// touch the goods.

func dsFixture(t *testing.T) *wsFix {
	t.Helper()
	f := newWSFix(t, true)
	f.vendor("WS-V")
	f.insert("WS-D", "Item", "Active", "system", map[string]interface{}{"code": "WS-D", "name": "Drop item", "gst_rate": 18.0, "hsn_code": "1234",
		"standard_cost": 60.0, "tax_treatment": "Taxable", "status": "Active", "item_type": "Product", "dropship": "Yes", "default_vendor": "WS-V"})
	f.insert("WS-D2", "Item", "Active", "system", map[string]interface{}{"code": "WS-D2", "name": "Drop item 2", "gst_rate": 18.0, "hsn_code": "1234",
		"standard_cost": 40.0, "tax_treatment": "Taxable", "status": "Active", "item_type": "Product", "dropship": "Yes", "default_vendor": "WS-V"})
	f.customer("WS-C", nil)
	return f
}

func (f *wsFix) dropOrder(lines ...SalesOrderLineInput) string {
	f.t.Helper()
	id, err := CreateSalesOrder(f.tenantID, SalesOrderInput{Channel: "Manual", CustomerName: "Cust WS-C", CustomerID: "WS-C", ShippingAddress: "9 Vendor Lane",
		ShippingCity: "Pune", ShippingPincode: "411001", ShippingState: "Maharashtra", PaymentStatus: "Confirmed", Lines: lines})
	if err != nil {
		f.t.Fatalf("CreateSalesOrder: %v", err)
	}
	return id
}

func (f *wsFix) poOf(orderID string) (string, string, map[string]interface{}) {
	f.t.Helper()
	var id, status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT id, status FROM %s.documents WHERE doctype='PurchaseOrder' AND data->>'dropship_order_id' = $1 AND status <> 'Cancelled'`, f.schema), orderID).Scan(&id, &status); err != nil {
		f.t.Fatalf("no purchase order for %s: %v", orderID, err)
	}
	return id, status, f.doc("PurchaseOrder", id)
}

func (f *wsFix) setPOStatus(id, status string) {
	f.t.Helper()
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = $1 WHERE doctype='PurchaseOrder' AND id = $2`, f.schema), status, id); err != nil {
		f.t.Fatal(err)
	}
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestStage14832SavingTheOrderRaisesADraftPO(t *testing.T) {
	f := dsFixture(t)
	order := f.dropOrder(SalesOrderLineInput{SKU: "WS-D", Qty: 5, UnitPrice: 100})
	poID, status, po := f.poOf(order)
	if status != "Draft" || strField(po, "vendor_id") != "WS-V" {
		t.Fatalf("PO %s status %q vendor %q", poID, status, strField(po, "vendor_id"))
	}
	if !strings.Contains(strField(po, "ship_to_address"), "9 Vendor Lane") {
		t.Fatalf("ship-to address %q", strField(po, "ship_to_address"))
	}
	if strField(f.doc("SalesOrder", order), "dropship") != "Yes" {
		t.Fatal("order not flagged")
	}
	line := f.doc("SalesOrderLine", order+"-L1")
	if strField(line, "po_id") != poID {
		t.Fatalf("line not linked: %v", line)
	}
	// nothing of ours moves: no pick task, no stock reservation
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype IN ('PickTask','StockReservation','Reservation') AND data::text LIKE $1`, f.schema), "%"+order+"%").Scan(&n)
	if n != 0 {
		t.Fatalf("%d pick or reservation rows for a drop-ship order", n)
	}
	// raising again changes nothing
	if made, err := raiseDropShipPOs(f.tenantID, order, "system"); err != nil || len(made) != 0 {
		t.Fatalf("second raise: %v %v", made, err)
	}
}

func TestStage14832QtyAndDateFollowWhileDraftThenStop(t *testing.T) {
	f := dsFixture(t)
	order := f.dropOrder(SalesOrderLineInput{SKU: "WS-D", Qty: 5, UnitPrice: 100})
	poID, _, _ := f.poOf(order)
	if _, err := EditOrderLines(f.tenantID, order, []OrderLineChange{{LineID: order + "-L1", Qty: 8}}, "customer asked", "u1"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	_, _, po := f.poOf(order)
	if !strings.Contains(strField(po, "items"), `"qty":8`) && !strings.Contains(strField(po, "items"), `"quantity":8`) {
		t.Fatalf("PO lines did not follow: %s", strField(po, "items"))
	}
	date := "2026-12-01"
	if err := EditSalesOrder(f.tenantID, order, OrderEdit{DeliveryDate: &date}, "u1"); err != nil {
		t.Fatalf("date: %v", err)
	}
	if _, _, po = f.poOf(order); strField(po, "expected_delivery_date") != date {
		t.Fatalf("date %q", strField(po, "expected_delivery_date"))
	}
	f.setPOStatus(poID, "Approved")
	_, err := EditOrderLines(f.tenantID, order, []OrderLineChange{{LineID: order + "-L1", Qty: 9}}, "later", "u1")
	wantCode(t, err, wsPOLockedCode)
	later := "2026-12-09"
	wantCode(t, EditSalesOrder(f.tenantID, order, OrderEdit{DeliveryDate: &later}, "u1"), wsPOLockedCode)
}

func TestStage14832ConfirmShipsInvoicesAndPostsTheCost(t *testing.T) {
	f := dsFixture(t)
	order := f.dropOrder(SalesOrderLineInput{SKU: "WS-D", Qty: 5, UnitPrice: 100})
	poID, _, _ := f.poOf(order)
	// the PO is not approved: a named line is refused, confirm-all skips it
	lineID := order + "-L1"
	_, err := ConfirmDropShip(f.tenantID, "u1", []string{lineID}, false, "")
	wantCode(t, err, wsPONotReadyCode)
	res, err := ConfirmDropShip(f.tenantID, "u1", nil, true, "")
	if err != nil || len(res.Confirmed) != 0 || len(res.Skipped) != 1 {
		t.Fatalf("confirm-all before approval: %+v %v", res, err)
	}
	f.setPOStatus(poID, "Approved")
	res, err = ConfirmDropShip(f.tenantID, "u1", nil, true, "")
	if err != nil || len(res.Confirmed) != 1 || len(res.Bills) != 1 || len(res.Invoices) != 1 {
		t.Fatalf("confirm-all: %+v %v", res, err)
	}
	if st := strField(f.doc("SalesOrder", order), "order_status"); st != "Shipped" {
		t.Fatalf("order %s", st)
	}
	// vendor bill: cost of goods against payables, at the PO rate x qty
	if dr, cr := f.gl("VendorInvoice", res.Bills[0], "5100"); dr != 30000 || cr != 0 {
		t.Fatalf("5100 dr=%d cr=%d", dr, cr)
	}
	if dr, cr := f.gl("VendorInvoice", res.Bills[0], "1500"); dr != 5400 || cr != 0 { // input GST, 18% interstate
		t.Fatalf("1500 dr=%d cr=%d", dr, cr)
	}
	if dr, cr := f.gl("VendorInvoice", res.Bills[0], "2100"); dr != 0 || cr != 35400 {
		t.Fatalf("2100 dr=%d cr=%d", dr, cr)
	}
	// customer invoice posts on the normal path, with no stock effect
	if _, err := PostSalesInvoice(f.tenantID, res.Invoices[0], "system"); err != nil {
		t.Fatalf("post invoice: %v", err)
	}
	dr, _ := f.gl("SalesInvoice", res.Invoices[0], "1300")
	var credits int64
	for _, a := range []string{"4100", "2200", "2201", "2202"} {
		_, c := f.gl("SalesInvoice", res.Invoices[0], a)
		credits += c
	}
	if dr != 50000 || dr != credits {
		t.Fatalf("1300 dr=%d, credits=%d", dr, credits)
	}
	if _, cr := f.gl("SalesInvoice", res.Invoices[0], "4100"); cr != 42373 {
		t.Fatalf("4100 cr=%d", cr)
	}
	var stockRows int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.gl_postings WHERE document_id = $1 AND account_code IN ('1400','1410','1200','5000')`, f.schema), res.Invoices[0]).Scan(&stockRows)
	if stockRows != 0 {
		t.Fatalf("%d stock/COGS rows on the invoice", stockRows)
	}
	// shipping twice is refused
	_, err = ConfirmDropShip(f.tenantID, "u1", []string{lineID}, false, "")
	wantCode(t, err, wsAlreadyCode)
}

func TestStage14832NoGoodsReceiptAndNoMix(t *testing.T) {
	f := dsFixture(t)
	order := f.dropOrder(SalesOrderLineInput{SKU: "WS-D", Qty: 2, UnitPrice: 100})
	poID, _, _ := f.poOf(order)
	wantCode(t, refuseDropShipGRN(f.tenantID, poID), wsNoGRNCode)
	if err := refuseDropShipGRN(f.tenantID, "NOT-A-DROPSHIP"); err != nil {
		t.Fatalf("ordinary PO refused: %v", err)
	}
	_, err := CreateSalesOrder(f.tenantID, SalesOrderInput{Channel: "Manual", CustomerName: "Cust WS-C", CustomerID: "WS-C", ShippingAddress: "1 Rd", ShippingState: "Karnataka",
		PaymentStatus: "Confirmed", Lines: []SalesOrderLineInput{{SKU: "WS-D", Qty: 1, UnitPrice: 100}, {SKU: "WS-A", Qty: 1, UnitPrice: 100}}})
	wantCode(t, err, wsMixedOrderCode)
}

func TestStage14832SaveRulesAndPOForge(t *testing.T) {
	f := dsFixture(t)
	f.insert("WS-NV", "Item", "Active", "system", map[string]interface{}{"code": "WS-NV", "name": "x", "status": "Active"})
	wantCode(t, ValidateDocument(f.tenantID, "Item", map[string]interface{}{"code": "WS-NV2", "name": "x", "dropship": "Yes", "status": "Active"}), wsNoVendorCode)
	order := f.dropOrder(SalesOrderLineInput{SKU: "WS-D", Qty: 1, UnitPrice: 100})
	_, _, po := f.poOf(order)
	po["dropship_order_id"] = "SO-OTHER"
	wantCode(t, ValidateDocument(f.tenantID, "PurchaseOrder", po), wsNotDropShipCode)
	plain := map[string]interface{}{"code": "PO-FAKE", "vendor": "WS-V", "dropship_order_id": "SO-1"}
	wantCode(t, ValidateDocument(f.tenantID, "PurchaseOrder", plain), wsNotDropShipCode)
}

func TestStage14832OffMeansOff(t *testing.T) {
	f := dsFixture(t)
	f.module(false)
	oid, _ := CreateSalesOrder(f.tenantID, SalesOrderInput{Channel: "Manual", CustomerName: "Cust WS-C", CustomerID: "WS-C", ShippingAddress: "9 Vendor Lane", ShippingState: "Maharashtra", PaymentStatus: "Confirmed", Lines: []SalesOrderLineInput{{SKU: "WS-D", Qty: 2, UnitPrice: 100}}})
	if oid != "" && strField(f.doc("SalesOrder", oid), "dropship") == "Yes" {
		t.Fatal("module off but the order was flagged drop-ship")
	}
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype='PurchaseOrder' AND data->>'dropship_order_id' IS NOT NULL`, f.schema)).Scan(&n)
	if n != 0 {
		t.Fatalf("module off but a drop-ship PO was raised (%d)", n)
	}
	_, err := OpenDropShipLines(f.tenantID, "")
	wantCode(t, err, wsOffCode)
	_, err = ConfirmDropShip(f.tenantID, "u1", []string{"x"}, false, "")
	wantCode(t, err, wsOffCode)
}
