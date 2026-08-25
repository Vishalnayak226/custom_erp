package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"testing"
)

// TestSettlementReconcile covers Stage 35.8's matching engine end to end:
// row-math validation, an auto-matched line's balanced GL split, a held
// Variance, and closing it via write-off - all against the real schema
// db/migrations_stage35_8_settlement_reconciliation.sql adds.
func TestSettlementReconcile(t *testing.T) {
	spInitDB()
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	var ready bool
	if err := db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM " + schema + ".doctype_meta WHERE name='MarketplaceSettlementLine')").Scan(&ready); err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Skip("db/migrations_stage35_8_settlement_reconciliation.sql has not been applied")
	}

	const (
		orderMatched = "SO-STL-TEST-MATCHED"
		orderVariant = "SO-STL-TEST-VARIANCE"
	)
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'MarketplaceSettlementLine' AND id LIKE 'MSL-STL-TEST-%'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'SalesInvoice' AND id LIKE 'INV-STL-TEST-%'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ReasonCode' AND id = 'RC-STL-TEST'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".channel_order_mapping WHERE channel = 'stltest'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'MarketplaceSettlementLine' AND document_id LIKE 'MSL-STL-TEST-%'")
	}
	cleanup()
	defer cleanup()

	seedMapping := func(orderID, channelOrderID string) {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".channel_order_mapping (order_id, channel, channel_order_id) VALUES ($1, 'stltest', $2)", orderID, channelOrderID); err != nil {
			t.Fatalf("seed channel_order_mapping: %v", err)
		}
	}
	seedInvoice := func(id, orderID string, total float64) {
		data, _ := json.Marshal(map[string]interface{}{"code": id, "sales_order_id": orderID, "total_amount": total})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesInvoice', $2, 'Approved', 'system')", id, data); err != nil {
			t.Fatalf("seed SalesInvoice: %v", err)
		}
	}
	seedLine := func(id, channelOrderID string, gross, commission, shippingFee, tds, tcs, netPayout float64) {
		data, _ := json.Marshal(map[string]interface{}{
			"channel": "stltest", "channel_order_id": channelOrderID, "settlement_batch_id": "BATCH-STL-1",
			"settlement_date": "2026-08-20", "gross_amount": gross, "commission": commission,
			"shipping_fee": shippingFee, "other_fee": 0, "tds": tds, "tcs": tcs, "net_payout": netPayout,
			"match_status": "Unmatched",
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'MarketplaceSettlementLine', $2, 'Active', 'system')", id, data); err != nil {
			t.Fatalf("seed MarketplaceSettlementLine: %v", err)
		}
	}
	matchStatus := func(id string) string {
		var s string
		if err := db.DB.QueryRow("SELECT data->>'match_status' FROM "+schema+".documents WHERE id = $1", id).Scan(&s); err != nil {
			t.Fatalf("read match_status for %s: %v", id, err)
		}
		return s
	}
	glBalanced := func(id string) (debit, credit int64) {
		_ = db.DB.QueryRow("SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_type = 'MarketplaceSettlementLine' AND document_id = $1", id).Scan(&debit, &credit)
		return
	}

	// 1. Row-math invalid: gross - deductions != net_payout.
	seedLine("MSL-STL-TEST-BAD", "CHORD-STL-BAD", 1000, 100, 0, 0, 0, 999)
	if _, err := ReconcileMarketplaceSettlements(tenantID, "tester"); err != nil {
		t.Fatalf("ReconcileMarketplaceSettlements: %v", err)
	}
	if got := matchStatus("MSL-STL-TEST-BAD"); got != "Invalid" {
		t.Errorf("bad-math line: expected Invalid, got %q", got)
	}

	// 2. Clean match: gross equals the invoice total exactly, auto-posts.
	seedMapping(orderMatched, "CHORD-STL-MATCH")
	seedInvoice("INV-STL-TEST-1", orderMatched, 900)
	// 900 gross - 90 commission - 20 shipping - 10 tds - 5 tcs = 775 net.
	seedLine("MSL-STL-TEST-MATCH", "CHORD-STL-MATCH", 900, 90, 20, 10, 5, 775)
	if _, err := ReconcileMarketplaceSettlements(tenantID, "tester"); err != nil {
		t.Fatalf("ReconcileMarketplaceSettlements: %v", err)
	}
	if got := matchStatus("MSL-STL-TEST-MATCH"); got != "Matched" {
		t.Errorf("clean-match line: expected Matched, got %q", got)
	}
	debit, credit := glBalanced("MSL-STL-TEST-MATCH")
	if debit != credit || debit != 90000 { // 900.00 rupees in paise
		t.Errorf("clean-match GL: debit=%d credit=%d, want both 90000 (balanced at gross=900)", debit, credit)
	}

	// 3. Variance held, then written off. Invoice says 900, marketplace
	// reports gross of only 700 - an 200 gap past any sane default tolerance.
	seedMapping(orderVariant, "CHORD-STL-VAR")
	seedInvoice("INV-STL-TEST-2", orderVariant, 900)
	// 700 gross - 70 commission - 10 shipping - 0 tds - 0 tcs = 620 net.
	seedLine("MSL-STL-TEST-VAR", "CHORD-STL-VAR", 700, 70, 10, 0, 0, 620)
	result, err := ReconcileMarketplaceSettlements(tenantID, "tester")
	if err != nil {
		t.Fatalf("ReconcileMarketplaceSettlements: %v", err)
	}
	if result.Variance < 1 {
		t.Errorf("expected at least one Variance line in this pass, got result=%+v", result)
	}
	if got := matchStatus("MSL-STL-TEST-VAR"); got != "Variance" {
		t.Fatalf("variance line: expected Variance, got %q", got)
	}
	if d, c := glBalanced("MSL-STL-TEST-VAR"); d != 0 || c != 0 {
		t.Errorf("a held Variance must not post to GL yet: debit=%d credit=%d", d, c)
	}

	reasonData, _ := json.Marshal(map[string]interface{}{"code": "RC-STL-TEST", "description": "Marketplace short-paid, accepted", "category": "Settlement", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReasonCode', $2, 'Active', 'system')", "RC-STL-TEST", reasonData); err != nil {
		t.Fatalf("seed Settlement ReasonCode: %v", err)
	}
	if err := WriteOffSettlementVariance(tenantID, "MSL-STL-TEST-VAR", "RC-STL-TEST", "tester"); err != nil {
		t.Fatalf("WriteOffSettlementVariance: %v", err)
	}
	if got := matchStatus("MSL-STL-TEST-VAR"); got != "WrittenOff" {
		t.Errorf("after write-off: expected WrittenOff, got %q", got)
	}
	debit, credit = glBalanced("MSL-STL-TEST-VAR")
	if debit != credit || debit != 90000 { // clears the full 900 invoiced value
		t.Errorf("write-off GL: debit=%d credit=%d, want both 90000 (balanced at expected=900)", debit, credit)
	}
}

// TestApplyReturnQCExchange covers Stage 35.9.2: a same-value exchange line
// deducts the exchange SKU's own stock atomically alongside the returned
// item's receipt, and is excluded from the refund total.
func TestApplyReturnQCExchange(t *testing.T) {
	spInitDB()
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	const (
		cartID   = "PC-EXC-TEST-1"
		location = "LOC-EXC-TEST"
		sku      = "SKU-EXC-ORIGINAL"
		exchSKU  = "SKU-EXC-REPLACEMENT"
	)
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'POSCart' AND id = $1", cartID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Item' AND id IN ($1, $2)", sku, exchSKU)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1", exchSKU)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku IN ($1, $2)", sku, exchSKU)
	}
	cleanup()
	defer cleanup()

	itemData, _ := json.Marshal(map[string]interface{}{"code": exchSKU, "name": "Exchange Target", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", exchSKU, itemData); err != nil {
		t.Fatalf("seed exchange Item: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 5, 5)", exchSKU, location); err != nil {
		t.Fatalf("seed exchange stock: %v", err)
	}
	cartData, _ := json.Marshal(map[string]interface{}{
		"items": []map[string]interface{}{{"sku": sku, "qty": 2, "sale_price": 500.0, "cost_price": 300.0}},
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Paid', 'system')", cartID, cartData); err != nil {
		t.Fatalf("seed POSCart: %v", err)
	}

	returnID, err := CreateReturnRequest(tenantID, "Customer Return", location, cartID, "", "tester", []ReturnItemInput{{SKU: sku, Qty: 1}})
	if err != nil {
		t.Fatalf("CreateReturnRequest: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", returnID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'RefundRequest' AND data->>'return_request_id' = $1", returnID)
	})
	if err := ApproveReturnRequest(tenantID, returnID, "approver1"); err != nil {
		t.Fatalf("ApproveReturnRequest: %v", err)
	}
	if err := ReceiveReturnRequest(tenantID, returnID, "warehouse1"); err != nil {
		t.Fatalf("ReceiveReturnRequest: %v", err)
	}

	refundTotal, refundRequestID, err := ApplyReturnQC(tenantID, returnID, map[string]string{sku: "Sellable"}, "qc1", map[string]string{sku: exchSKU})
	if err != nil {
		t.Fatalf("ApplyReturnQC with exchange: %v", err)
	}
	if refundTotal != 0 || refundRequestID != "" {
		t.Errorf("an exchanged line must not create a refund: total=%v refundID=%q", refundTotal, refundRequestID)
	}

	var exchAvailable, origAvailable int
	if err := db.DB.QueryRow("SELECT available FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", exchSKU, location).Scan(&exchAvailable); err != nil {
		t.Fatal(err)
	}
	if exchAvailable != 4 {
		t.Errorf("exchange sku stock: expected 5-1=4, got %d", exchAvailable)
	}
	if err := db.DB.QueryRow("SELECT available FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, location).Scan(&origAvailable); err != nil {
		t.Fatal(err)
	}
	if origAvailable != 1 {
		t.Errorf("returned sku stock: expected 1 unit received into available, got %d", origAvailable)
	}

	var exchangeSKUOnRecord string
	if err := db.DB.QueryRow("SELECT data->'items'->0->>'exchange_sku' FROM "+schema+".documents WHERE id = $1", returnID).Scan(&exchangeSKUOnRecord); err != nil {
		t.Fatal(err)
	}
	if exchangeSKUOnRecord != exchSKU {
		t.Errorf("expected the return record to keep exchange_sku=%q, got %q", exchSKU, exchangeSKUOnRecord)
	}
}

// TestApplyReturnQCExchangeInsufficientStockRollsBack asserts the whole QC
// call - including the returned item's own stock receipt - rolls back when
// the exchange leg cannot be fulfilled, per ApplyReturnQC's own atomicity
// comment.
func TestApplyReturnQCExchangeInsufficientStockRollsBack(t *testing.T) {
	spInitDB()
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	const (
		cartID   = "PC-EXC-TEST-2"
		location = "LOC-EXC-TEST-2"
		sku      = "SKU-EXC-ORIGINAL-2"
		exchSKU  = "SKU-EXC-OUTOFSTOCK"
	)
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'POSCart' AND id = $1", cartID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Item' AND id = $1", exchSKU)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku IN ($1, $2)", sku, exchSKU)
	}
	cleanup()
	defer cleanup()

	itemData, _ := json.Marshal(map[string]interface{}{"code": exchSKU, "name": "Out Of Stock Item", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", exchSKU, itemData); err != nil {
		t.Fatalf("seed exchange Item: %v", err)
	}
	// No inventory_availability row for exchSKU at all - zero ATS.
	cartData, _ := json.Marshal(map[string]interface{}{
		"items": []map[string]interface{}{{"sku": sku, "qty": 1, "sale_price": 500.0, "cost_price": 300.0}},
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Paid', 'system')", cartID, cartData); err != nil {
		t.Fatalf("seed POSCart: %v", err)
	}

	returnID, err := CreateReturnRequest(tenantID, "Customer Return", location, cartID, "", "tester", []ReturnItemInput{{SKU: sku, Qty: 1}})
	if err != nil {
		t.Fatalf("CreateReturnRequest: %v", err)
	}
	t.Cleanup(func() { _, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", returnID) })
	if err := ApproveReturnRequest(tenantID, returnID, "approver1"); err != nil {
		t.Fatalf("ApproveReturnRequest: %v", err)
	}
	if err := ReceiveReturnRequest(tenantID, returnID, "warehouse1"); err != nil {
		t.Fatalf("ReceiveReturnRequest: %v", err)
	}

	if _, _, err := ApplyReturnQC(tenantID, returnID, map[string]string{sku: "Sellable"}, "qc1", map[string]string{sku: exchSKU}); err == nil {
		t.Fatal("expected ApplyReturnQC to refuse an exchange with insufficient ATS")
	}

	var origAvailable sql.NullInt64
	_ = db.DB.QueryRow("SELECT available FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, location).Scan(&origAvailable)
	if origAvailable.Valid && origAvailable.Int64 != 0 {
		t.Errorf("a rolled-back QC call must not have received the original item's stock either: available=%v", origAvailable)
	}
	var status string
	if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", returnID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "Received" {
		t.Errorf("a rolled-back QC call must leave the return request Received (unchanged), got %q", status)
	}
}

// TestCreateReturnRequestAutoRouting covers Stage 35.9.3: an RTO with no
// explicit return_location resolves one via the same Nearest-Pincode
// strategy engines/sourcing.go already uses for order allocation.
func TestCreateReturnRequestAutoRouting(t *testing.T) {
	spInitDB()
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	const (
		bookingID = "LOG-ROUTE-TEST-1"
		orderID   = "SO-ROUTE-TEST-1"
		sku       = "SKU-ROUTE-TEST"
		locNear   = "LOC-ROUTE-TEST-NEAR"
		locFar    = "LOC-ROUTE-TEST-FAR"
	)
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ReturnRequest' AND data->>'booking_id' = '" + bookingID + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'LogisticsBooking' AND id = '" + bookingID + "'")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Location' AND id IN ($1, $2)", locNear, locFar)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
	}
	cleanup()
	defer cleanup()

	seedLocation := func(id, pincode string) {
		data, _ := json.Marshal(map[string]interface{}{"code": id, "name": id, "type": "Warehouse", "status": "Active", "pincode": pincode})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')", id, data); err != nil {
			t.Fatalf("seed Location %s: %v", id, err)
		}
	}
	seedLocation(locNear, "560001")
	seedLocation(locFar, "560099")
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 20, 20)", sku, locNear); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 20, 20)", sku, locFar); err != nil {
		t.Fatal(err)
	}

	bookingData, _ := json.Marshal(map[string]interface{}{
		"code": bookingID, "order_id": orderID, "destination_pincode": "560005", "status": "RTO",
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'LogisticsBooking', $2, 'RTO', 'system')", bookingID, bookingData); err != nil {
		t.Fatalf("seed LogisticsBooking: %v", err)
	}

	returnID, err := CreateReturnRequest(tenantID, "RTO", "", "", bookingID, "tester", []ReturnItemInput{{SKU: sku, Qty: 1}})
	if err != nil {
		t.Fatalf("CreateReturnRequest with no explicit return_location: %v", err)
	}
	t.Cleanup(func() { _, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", returnID) })

	var resolvedLocation string
	var autoRouted bool
	if err := db.DB.QueryRow("SELECT data->>'return_location', COALESCE((data->>'return_location_auto_routed')::boolean, false) FROM "+schema+".documents WHERE id = $1", returnID).Scan(&resolvedLocation, &autoRouted); err != nil {
		t.Fatal(err)
	}
	if resolvedLocation != locNear {
		t.Errorf("expected auto-routing to pick %s (560001, nearer to 560005 than 560099's %s), got %q", locNear, locFar, resolvedLocation)
	}
	if !autoRouted {
		t.Error("expected return_location_auto_routed=true")
	}
}
