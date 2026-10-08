package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// FA-20261005-03, end to end: one order from Reserved through release, pick,
// pack, courier booking and manifest handover to Shipped and a draft invoice -
// and the stock, reservation, ledger and COGS effects of the dispatch.
func TestOrderShipEndToEnd(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		orderID = "TEST-SHIP-SO"
		lineID  = "TEST-SHIP-SO-L1"
		sku     = "TEST-SHIP-SKU"
		loc     = "TEST-SHIP-LOC"
		courier = "TEST-SHIP-COURIER"
	)
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id IN (SELECT id FROM "+schema+".documents WHERE doctype = 'FulfillmentTask' AND data->>'order_id' = $1)", orderID)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE data->>'order_id' = $1 OR data->>'sales_order_id' = $1", orderID)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Manifest' AND data->>'courier' = $1", courier)
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'TEST-SHIP-%'")
		db.DB.Exec("DELETE FROM "+schema+".inventory_reservation WHERE sku = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
	}
	cleanup()
	defer cleanup()

	seed := func(id, doctype, status string, data map[string]interface{}) {
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, b, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "Ship Test Item", "barcode": sku, "status": "Active", "standard_cost": 40})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available, reserved) VALUES ($1, $2, 10, 10, 0)", sku, loc); err != nil {
		t.Fatalf("seed stock: %v", err)
	}
	seed(orderID, "SalesOrder", "Reserved", map[string]interface{}{"code": orderID, "order_status": "Reserved", "total_amount": 200, "customer_name": "Ship Test", "shipping_address": "1 Test Rd 560001"})
	seed(lineID, "SalesOrderLine", "Reserved", map[string]interface{}{"code": lineID, "order_id": orderID, "sku": sku, "qty": 2, "unit_price": 100, "location_code": loc, "line_status": "Reserved"})
	if _, err := CreateReservation(tenantID, sku, loc, 2, "Online", 0, ReservationAttribution{OrderID: orderID, LineID: lineID}); err != nil {
		t.Fatalf("reserve: %v", err)
	}

	taskIDs, _, err := ReleaseOrderToFulfillment(tenantID, orderID, "admin")
	if err != nil || len(taskIDs) != 1 {
		t.Fatalf("release: %v %v", taskIDs, err)
	}
	taskID := taskIDs[0]

	for i := 0; i < 2; i++ {
		if _, _, err := ScanPickItem(tenantID, taskID, sku); err != nil {
			t.Fatalf("pick %d: %v", i, err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, _, err := ScanPackItem(tenantID, taskID, sku); err != nil {
			t.Fatalf("pack %d: %v", i, err)
		}
	}
	if err := CompletePackTask(tenantID, taskID); err != nil {
		t.Fatalf("complete pack: %v", err)
	}

	if _, err := CreateLogisticsBooking(tenantID, orderID, taskID, courier, "TEST-SHIP-AWB-1", "560001", 0); err != nil {
		t.Fatalf("booking: %v", err)
	}
	manifestID, count, err := GenerateManifest(tenantID, courier, loc)
	if err != nil || count != 1 {
		t.Fatalf("manifest: id=%s count=%d err=%v", manifestID, count, err)
	}
	if err := HandoverManifest(tenantID, manifestID, "admin"); err != nil {
		t.Fatalf("handover: %v", err)
	}

	if status, _, _ := GetOrderStatus(tenantID, orderID); status != "Shipped" {
		t.Fatalf("order status %q, want Shipped", status)
	}
	var invoices int
	db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'SalesInvoice' AND data->>'sales_order_id' = $1", orderID).Scan(&invoices)
	if invoices != 1 {
		t.Fatalf("expected one draft invoice for the shipped order, got %d", invoices)
	}

	var onHand, reserved int
	db.DB.QueryRow("SELECT on_hand, reserved FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, loc).Scan(&onHand, &reserved)
	if onHand != 8 || reserved != 0 {
		t.Fatalf("stock after dispatch on_hand=%d reserved=%d, want 8/0", onHand, reserved)
	}
	var resRows int
	db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".inventory_reservation WHERE line_id = $1", lineID).Scan(&resRows)
	if resRows != 0 {
		t.Fatalf("the line's reservation row must be consumed at dispatch, %d left", resRows)
	}
	var lineStatus string
	db.DB.QueryRow("SELECT data->>'line_status' FROM "+schema+".documents WHERE id = $1", lineID).Scan(&lineStatus)
	if lineStatus != "Dispatched" {
		t.Fatalf("line_status %q, want Dispatched", lineStatus)
	}
	var cogs, inv int64
	db.DB.QueryRow("SELECT COALESCE(SUM(debit),0) FROM "+schema+".gl_postings WHERE document_id = $1 AND account_code = '5100'", taskID).Scan(&cogs)
	db.DB.QueryRow("SELECT COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_id = $1 AND account_code = '1200'", taskID).Scan(&inv)
	if cogs != 8000 || inv != 8000 {
		t.Fatalf("COGS 5100 debit=%d / 1200 credit=%d paise, want 8000 each (2 x standard cost 40)", cogs, inv)
	}
	var ledgerQty float64
	db.DB.QueryRow("SELECT COALESCE(SUM((data->>'qty')::numeric),0) FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1", taskID).Scan(&ledgerQty)
	if ledgerQty != -2 {
		t.Fatalf("stock ledger qty for the dispatch = %v, want -2", ledgerQty)
	}

	if err := TransitionTaskStatus(tenantID, taskID, "Dispatched"); err == nil {
		t.Fatalf("a second Dispatched must be refused")
	}
	db.DB.QueryRow("SELECT on_hand FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, loc).Scan(&onHand)
	if onHand != 8 {
		t.Fatalf("a refused repeat dispatch changed stock: on_hand=%d", onHand)
	}
}

// A short pick ships only what was picked: stock, COGS, the ledger and the
// draft invoice all follow the shipped quantity, and the line's whole
// reservation is still released.
func TestOrderShipShortPick(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		orderID = "TEST-SHORT-SO"
		lineID  = "TEST-SHORT-SO-L1"
		sku     = "TEST-SHORT-SKU"
		loc     = "TEST-SHORT-LOC"
		courier = "TEST-SHORT-COURIER"
		reason  = "TEST-SHORT-RC"
	)
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id IN (SELECT id FROM "+schema+".documents WHERE doctype = 'FulfillmentTask' AND data->>'order_id' = $1)", orderID)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE data->>'order_id' = $1 OR data->>'sales_order_id' = $1", orderID)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Manifest' AND data->>'courier' = $1", courier)
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'TEST-SHORT-%'")
		db.DB.Exec("DELETE FROM "+schema+".inventory_reservation WHERE sku = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
	}
	cleanup()
	defer cleanup()
	seed := func(id, doctype, status string, data map[string]interface{}) {
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, b, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed(reason, "ReasonCode", "Active", map[string]interface{}{"code": reason, "category": "Short Pick", "status": "Active"})
	seed(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "Short Test Item", "barcode": sku, "status": "Active", "standard_cost": 40})
	db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available, reserved) VALUES ($1, $2, 10, 10, 0)", sku, loc)
	seed(orderID, "SalesOrder", "Reserved", map[string]interface{}{"code": orderID, "order_status": "Reserved", "total_amount": 200, "customer_name": "Short Test"})
	seed(lineID, "SalesOrderLine", "Reserved", map[string]interface{}{"code": lineID, "order_id": orderID, "sku": sku, "qty": 2, "unit_price": 100, "location_code": loc, "line_status": "Reserved"})
	if _, err := CreateReservation(tenantID, sku, loc, 2, "Online", 0, ReservationAttribution{OrderID: orderID, LineID: lineID}); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	taskIDs, _, err := ReleaseOrderToFulfillment(tenantID, orderID, "admin")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	taskID := taskIDs[0]
	if _, _, err := ScanPickItem(tenantID, taskID, sku); err != nil {
		t.Fatalf("pick: %v", err)
	}
	if err := ShortPickLine(tenantID, taskID, sku, reason); err != nil {
		t.Fatalf("short pick: %v", err)
	}
	if _, _, err := ScanPackItem(tenantID, taskID, sku); err != nil {
		t.Fatalf("pack: %v", err)
	}
	if err := CompletePackTask(tenantID, taskID); err != nil {
		t.Fatalf("complete pack: %v", err)
	}
	if _, err := CreateLogisticsBooking(tenantID, orderID, taskID, courier, "TEST-SHORT-AWB", "560001", 0); err != nil {
		t.Fatalf("booking: %v", err)
	}
	manifestID, _, err := GenerateManifest(tenantID, courier, loc)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if err := HandoverManifest(tenantID, manifestID, "admin"); err != nil {
		t.Fatalf("handover: %v", err)
	}

	var onHand, reserved int
	db.DB.QueryRow("SELECT on_hand, reserved FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, loc).Scan(&onHand, &reserved)
	if onHand != 9 || reserved != 0 {
		t.Fatalf("on_hand=%d reserved=%d, want 9/0 (one shipped, reservation for both released)", onHand, reserved)
	}
	var cogs int64
	db.DB.QueryRow("SELECT COALESCE(SUM(debit),0) FROM "+schema+".gl_postings WHERE document_id = $1 AND account_code = '5100'", taskID).Scan(&cogs)
	if cogs != 4000 {
		t.Fatalf("COGS = %d paise, want 4000 (one unit)", cogs)
	}
	var invTotal float64
	db.DB.QueryRow("SELECT (data->>'total_amount')::numeric FROM "+schema+".documents WHERE doctype = 'SalesInvoice' AND data->>'sales_order_id' = $1", orderID).Scan(&invTotal)
	if invTotal != 100 {
		t.Fatalf("draft invoice total = %v, want 100 (the short unit is not billed)", invTotal)
	}
}

// A shipped order's draft invoice carries its GST, and posting it books the
// tax to GST Output Payable instead of revenue (2 x 118 incl. 18% =
// 200 taxable + 18 CGST + 18 SGST).
func TestOrderInvoiceBooksOutputGST(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const orderID, sku = "TEST-OGST-SO", "TEST-OGST-SKU"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id IN (SELECT id FROM "+schema+".documents WHERE doctype = 'SalesInvoice' AND data->>'sales_order_id' = $1)", orderID)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE data->>'order_id' = $1 OR data->>'sales_order_id' = $1", orderID)
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'TEST-OGST-%'")
	}
	cleanup()
	defer cleanup()
	seed := func(id, doctype, status string, data map[string]interface{}) {
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, b, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "OGST Item", "status": "Active", "hsn_code": "7113", "gst_rate": 18})
	seed(orderID, "SalesOrder", "Shipped", map[string]interface{}{"code": orderID, "order_status": "Shipped", "total_amount": 236})
	seed(orderID+"-L1", "SalesOrderLine", "Dispatched", map[string]interface{}{"order_id": orderID, "sku": sku, "qty": 2, "shipped_qty": 2, "unit_price": 118, "location_code": "TEST-OGST-LOC", "line_status": "Dispatched"})

	invID, err := CreateSalesInvoiceFromOrder(tenantID, orderID, "admin")
	if err != nil {
		t.Fatalf("invoice: %v", err)
	}
	var cgst, sgst, taxable float64
	db.DB.QueryRow("SELECT (data->>'cgst')::numeric, (data->>'sgst')::numeric, (data->>'taxable_amount')::numeric FROM "+schema+".documents WHERE id = $1", invID).Scan(&cgst, &sgst, &taxable)
	if cgst != 18 || sgst != 18 || taxable != 200 {
		t.Fatalf("invoice GST cgst=%v sgst=%v taxable=%v, want 18/18/200", cgst, sgst, taxable)
	}
	if _, err := PostSalesInvoice(tenantID, invID, "admin"); err != nil {
		t.Fatalf("post: %v", err)
	}
	sum := func(account, side string) int64 {
		var v int64
		db.DB.QueryRow("SELECT COALESCE(SUM("+side+"),0) FROM "+schema+".gl_postings WHERE document_id = $1 AND account_code = $2", invID, account).Scan(&v)
		return v
	}
	if ar, rev, c, s := sum("1300", "debit"), sum("4100", "credit"), sum("2200", "credit"), sum("2201", "credit"); ar != 23600 || rev != 20000 || c != 1800 || s != 1800 {
		t.Fatalf("GL paise AR=%d revenue=%d CGST=%d SGST=%d, want 23600/20000/1800/1800", ar, rev, c, s)
	}
}
