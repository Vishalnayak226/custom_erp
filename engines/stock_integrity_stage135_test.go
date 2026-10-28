package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

// stage135Tenant provisions a throwaway tenant and a document seeder for the
// Stage 135.16-135.19 tests below.
func stage135Tenant(t *testing.T) (string, string, func(id, doctype, status string, data map[string]interface{})) {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
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
	return tenantID, schema, insert
}

func stage135Stock(t *testing.T, schema, sku, loc string) (onHand, available, reserved int) {
	t.Helper()
	_ = db.DB.QueryRow("SELECT on_hand, available, reserved FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, loc).Scan(&onHand, &available, &reserved)
	return
}

// stage135BookAgrees fails the test when Stock Book vs Ledger finds any
// difference at loc - book against ledger, or own locations against blocked.
func stage135BookAgrees(t *testing.T, tenantID, loc string) {
	t.Helper()
	rows, err := StockBookVsLedger(tenantID, "", loc, true)
	if err != nil {
		t.Fatalf("StockBookVsLedger: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("book and ledger disagree at %s: %+v", loc, rows)
	}
}

// 135.18 (user decision 2026-10-27): a till sale leaves units reserved for an
// order at the same store alone.
func TestStage135TillRespectsOrderReservations(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	const loc, cust = "STORE-135", "CUST-135"
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "Store 135", "type": "Store", "sellable": "Yes"})
	insert("TEE", "Item", "Active", map[string]interface{}{"code": "TEE", "name": "TEE", "sale_price": 500.0, "hsn_code": "6109", "tax_treatment": "Taxable", "gst_rate": 5.0})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "TEE", "qty": 5.0}}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateReservation(tenantID, "TEE", loc, 3, "Online", 0); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	sell := func(cart string, qty int) error {
		t.Helper()
		q, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: loc, CustomerID: cust, Lines: []QuoteLineRequest{{Sku: "TEE", Qty: qty}}})
		if err != nil {
			t.Fatalf("quote %s: %v", cart, err)
		}
		insert(cart, "POSCart", "Processing", map[string]interface{}{"location": loc, "payment_mode": "Cash", "customer_id": cust,
			"items": POSCartItemsFromQuote(q), "gst_breakdown": q.GSTBreakdown})
		_, _, err = FinalizePOSCheckout(tenantID, cart, "")
		return err
	}

	err := sell("CART-135-A", 3)
	var short *InsufficientStockError
	if !errors.As(err, &short) || short.Available != 2 || short.Reserved != 3 {
		t.Fatalf("selling 3 of 5 with 3 reserved: want a shortage of 2 sellable / 3 reserved, got %v", err)
	}
	if !strings.Contains(err.Error(), "reserved for orders") {
		t.Fatalf("the cashier is not told why: %v", err)
	}
	if err := sell("CART-135-B", 2); err != nil {
		t.Fatalf("the 2 unreserved units must sell: %v", err)
	}
	if onHand, available, reserved := stage135Stock(t, schema, "TEE", loc); onHand != 3 || available != 3 || reserved != 3 {
		t.Fatalf("after the sale on_hand/available/reserved = %d/%d/%d, want 3/3/3", onHand, available, reserved)
	}
}

// 135.16: dispatch takes stock through the posting choke point - a shortage
// refuses it (it used to floor on_hand at zero while the ledger recorded the
// full shipment), and a dispatch leaves book and ledger agreeing.
func TestStage135DispatchThroughChokePoint(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	const loc, sku, orderID, lineID = "DC-135", "MUG", "SO-135", "SO-135-L1"
	insert(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "Mug", "standard_cost": 40})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": sku, "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	insert(orderID, "SalesOrder", "Reserved", map[string]interface{}{"code": orderID, "order_status": "Reserved", "total_amount": 200, "customer_name": "Dispatch Test"})
	insert(lineID, "SalesOrderLine", "Reserved", map[string]interface{}{"code": lineID, "order_id": orderID, "sku": sku, "qty": 2, "unit_price": 100, "location_code": loc, "line_status": "Reserved"})
	if _, err := CreateReservation(tenantID, sku, loc, 2, "Online", 0, ReservationAttribution{OrderID: orderID, LineID: lineID}); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	taskIDs, _, err := ReleaseOrderToFulfillment(tenantID, orderID, "admin")
	if err != nil || len(taskIDs) == 0 {
		t.Fatalf("release: %v %v", taskIDs, err)
	}

	// Units gone from the shelf behind the order's back (a pre-135.18 till
	// sale, an unrecorded loss): Main holds 1 for 2 to ship.
	if _, err := db.DB.Exec("UPDATE "+schema+".inventory_availability SET on_hand = 1, available = 1 WHERE sku = $1 AND location_code = $2", sku, loc); err != nil {
		t.Fatal(err)
	}
	if err := TransitionTaskStatus(tenantID, taskIDs[0], "Dispatched"); err == nil || !strings.Contains(err.Error(), "mark the rest short") {
		t.Fatalf("dispatching 2 with 1 on hand must be refused, got %v", err)
	}
	if onHand, _, reserved := stage135Stock(t, schema, sku, loc); onHand != 1 || reserved != 2 {
		t.Fatalf("a refused dispatch changed stock: on_hand %d reserved %d", onHand, reserved)
	}

	if _, err := db.DB.Exec("UPDATE "+schema+".inventory_availability SET on_hand = 10, available = 10 WHERE sku = $1 AND location_code = $2", sku, loc); err != nil {
		t.Fatal(err)
	}
	if err := TransitionTaskStatus(tenantID, taskIDs[0], "Dispatched"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if onHand, available, reserved := stage135Stock(t, schema, sku, loc); onHand != 8 || available != 8 || reserved != 0 {
		t.Fatalf("after dispatch on_hand/available/reserved = %d/%d/%d, want 8/8/0", onHand, available, reserved)
	}
	stage135BookAgrees(t, tenantID, loc)
}

// 135.16: an approved count whose variance would take a since-reduced book
// below zero is refused, not floored at zero beside a full-variance ledger line.
func TestStage135StaleCycleCountRefused(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	const loc, sku = "WH-135", "BOLT"
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": sku, "qty": 3.0}}, false); err != nil {
		t.Fatal(err)
	}
	line := func(id string, variance int) {
		insert(id, "CycleCountLine", "Approved", map[string]interface{}{"sku": sku, "location": loc, "variance": variance,
			"counted_qty": 0, "variance_reason_code": "RC-COUNT", "status": "Approved"})
	}
	line("CCL-135-STALE", -5)
	if err := PostCycleCountAdjustment(tenantID, "CCL-135-STALE", "counter"); err == nil || !strings.Contains(err.Error(), "count it again") {
		t.Fatalf("a variance of -5 against 3 on hand must be refused, got %v", err)
	}
	if onHand, _, _ := stage135Stock(t, schema, sku, loc); onHand != 3 {
		t.Fatalf("a refused count changed on_hand to %d", onHand)
	}
	line("CCL-135-OK", -3)
	if err := PostCycleCountAdjustment(tenantID, "CCL-135-OK", "counter"); err != nil {
		t.Fatalf("count: %v", err)
	}
	if onHand, _, _ := stage135Stock(t, schema, sku, loc); onHand != 0 {
		t.Fatalf("after the count on_hand = %d, want 0", onHand)
	}
	stage135BookAgrees(t, tenantID, loc)
}

// 135.19: material issue and finished-goods receipt run under the order's row
// lock with a ledger key per posting - two issues at once post once, two
// completions at once both count, and every completion reaches the ledger.
func TestStage135ProductionPostsOnceUnderLock(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	const loc = "MFG-135"
	insert("FG-135", "Item", "Active", map[string]interface{}{"code": "FG-135", "name": "Stool"})
	insert("RM-135", "Item", "Active", map[string]interface{}{"code": "RM-135", "name": "Plank"})
	comps, _ := json.Marshal([]map[string]interface{}{{"sku": "RM-135", "qty": 1}})
	insert("BOM-135", "BOM", "Active", map[string]interface{}{"code": "BOM-135", "parent_item": "FG-135", "components": string(comps), "status": "Active"})
	insert("PRO-135", "ProductionOrder", "Draft", map[string]interface{}{"bom_id": "BOM-135", "quantity": 4, "location": loc, "status": "Draft"})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "RM-135", "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	together := func(n int, f func() error) []error {
		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) { defer wg.Done(); errs[i] = f() }(i)
		}
		wg.Wait()
		return errs
	}

	ok := 0
	for _, err := range together(2, func() error { return IssueProductionMaterial(tenantID, "PRO-135") }) {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("two issues at once: %d succeeded, want exactly 1", ok)
	}
	if onHand, _, _ := stage135Stock(t, schema, "RM-135", loc); onHand != 6 {
		t.Fatalf("raw material after the issue = %d, want 6 (issued once)", onHand)
	}

	if err := ReportPartialCompletion(tenantID, "PRO-135", 1); err != nil {
		t.Fatalf("first completion: %v", err)
	}
	if err := ReportPartialCompletion(tenantID, "PRO-135", 1); err != nil {
		t.Fatalf("second completion: %v", err)
	}
	for _, err := range together(2, func() error { return ReportPartialCompletion(tenantID, "PRO-135", 1) }) {
		if err != nil {
			t.Fatalf("concurrent completion: %v", err)
		}
	}
	data, status, _ := fetchProductionOrder(tenantID, "PRO-135")
	if status != "Completed" || numFromInterface(data["completed_qty"]) != 4 {
		t.Fatalf("after four completions: %s, completed %v", status, data["completed_qty"])
	}
	if onHand, _, _ := stage135Stock(t, schema, "FG-135", loc); onHand != 4 {
		t.Fatalf("finished goods = %d, want 4", onHand)
	}
	var lines int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = 'PRO-135' AND data->>'item_id' = 'FG-135'").Scan(&lines)
	if lines != 4 {
		t.Fatalf("finished-goods ledger lines = %d, want 4 (one per completion)", lines)
	}
	stage135BookAgrees(t, tenantID, loc)
}

// 135.19: one GL posting per key, even when two transactions post it at once
// and neither caller locked a document.
func TestStage135GLKeyPostsOnceConcurrently(t *testing.T) {
	tenantID, schema, _ := stage135Tenant(t)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = PostDoubleEntry(tenantID, "Stage135Test", "DOC-135", map[string]int64{"1200": 10000}, map[string]int64{"5190": 10000}, "", "Stage135Test:DOC-135:KEY")
		}()
	}
	wg.Wait()
	var debit int64
	if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit), 0) FROM " + schema + ".gl_postings WHERE document_id = 'DOC-135'").Scan(&debit); err != nil {
		t.Fatal(err)
	}
	if debit != 10000 {
		t.Fatalf("four concurrent posts of one key booked %d paise of debit, want 10000", debit)
	}
}

// 135.17: Stock Book vs Ledger also checks that a store's own locations add up
// to what is set aside for them (blocked).
func TestStage135OwnLocationsAddUpToBlocked(t *testing.T) {
	tenantID, schema, _ := stage135Tenant(t)
	const loc, sku = "STORE-135B", "CAP"
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": sku, "qty": 5.0}}, false); err != nil {
		t.Fatal(err)
	}
	exec := func(q string) {
		t.Helper()
		if _, err := db.DB.Exec(q, sku, loc); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("INSERT INTO " + schema + ".stock_location_balance (sku, location_code, stock_location, qty) VALUES ($1, $2, 'ALTERATION', 2)")
	exec("UPDATE " + schema + ".inventory_availability SET blocked = 2 WHERE sku = $1 AND location_code = $2")
	stage135BookAgrees(t, tenantID, loc)

	exec("UPDATE " + schema + ".inventory_availability SET blocked = 1 WHERE sku = $1 AND location_code = $2")
	rows, err := StockBookVsLedger(tenantID, sku, loc, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].InOwnLocations != 2 || rows[0].Blocked != 1 || rows[0].Difference != 0 || !strings.Contains(rows[0].Hint, "own locations hold 2 but 1") {
		t.Fatalf("own locations 2 vs blocked 1 not reported: %+v", rows)
	}
}

// 135.20: QC submissions of one received return at once - one restocks
// and raises a refund request, the rest are refused.
func TestStage135ReturnQCAppliesOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	const loc = "STORE-135R"
	insert("HAT", "Item", "Active", map[string]interface{}{"code": "HAT", "name": "Hat"})
	insert("RR-135", "ReturnRequest", "Received", map[string]interface{}{"code": "RR-135", "status": "Received", "return_location": loc,
		"items": []map[string]interface{}{{"sku": "HAT", "qty": 1, "original_unit_price": 100, "original_cost_price": 40}}})
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, errs[i] = ApplyReturnQC(tenantID, "RR-135", map[string]string{"HAT": "Sellable"}, "qc")
		}(i)
	}
	wg.Wait()
	applied := 0
	for _, err := range errs {
		if err == nil {
			applied++
		}
	}
	if applied != 1 {
		t.Fatalf("eight QC submissions at once: %d applied, want exactly 1", applied)
	}
	var refunds int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'RefundRequest'").Scan(&refunds)
	if onHand, _, _ := stage135Stock(t, schema, "HAT", loc); onHand != 1 || refunds != 1 {
		t.Fatalf("on_hand %d and %d refund requests, want 1 and 1", onHand, refunds)
	}
	stage135BookAgrees(t, tenantID, loc)
}

// 135.16 (found by a parallel session): one SKU on two lines of a goods
// receipt's QC split, or of a return's QC, shared one ledger key per SKU, so
// the second line's ledger row was swallowed while on hand took both.
func TestStage135SameSKUOnTwoLinesReachesTheLedger(t *testing.T) {
	tenantID, _, insert := stage135Tenant(t)
	const loc = "WH-135S"
	items := []interface{}{
		map[string]interface{}{"sku": "NUT", "qty": float64(5), "rejected_qty": float64(1)},
		map[string]interface{}{"sku": "NUT", "qty": float64(4), "rejected_qty": float64(2), "damaged_qty": float64(1)},
	}
	if _, err := PostGRNReceiptWithQC(tenantID, loc, items, "admin", "GRN-135S"); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	stage135BookAgrees(t, tenantID, loc)

	insert("RR-135S", "ReturnRequest", "Received", map[string]interface{}{"code": "RR-135S", "status": "Received", "return_location": loc,
		"items": []map[string]interface{}{{"sku": "NUT", "qty": 1, "original_unit_price": 10}, {"sku": "NUT", "qty": 2, "original_unit_price": 10}}})
	if _, _, err := ApplyReturnQC(tenantID, "RR-135S", map[string]string{"NUT": "Sellable"}, "qc"); err != nil {
		t.Fatalf("return QC: %v", err)
	}
	stage135BookAgrees(t, tenantID, loc)
}
