package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 35.2 / 35.3 tests. All fixtures are prefixed TEST352- and cleaned up
// either side of the run, per this suite's shared-dev-database convention.

const consoleTestChannel = "Stage352Channel"

// seedConsoleOrder creates an Item with stock and one SalesOrder through the
// real engine, so the console reads exactly what production writes.
func seedConsoleOrder(t *testing.T, tenantID, schema, sku, channelOrderID string, qty int) string {
	t.Helper()
	item, _ := json.Marshal(map[string]interface{}{"code": sku, "name": "Console test item"})
	_, _ = db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system') ON CONFLICT (id) DO NOTHING", "ITEM-"+sku, item)
	_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, 'HO', 500, 500)", sku); err != nil {
		t.Fatal(err)
	}
	orderID, err := ImportChannelSalesOrder(tenantID, ChannelOrderInput{
		Channel: consoleTestChannel, ChannelOrderID: channelOrderID,
		CustomerName: "Console Customer", ShippingAddress: "12 Console Lane, Bengaluru 560001",
		PaymentStatus: "Confirmed", CustomerPhone: "9876500011",
		Lines: []SalesOrderLineInput{{SKU: sku, Qty: qty, UnitPrice: 100}},
	})
	if err != nil {
		t.Fatalf("seed order %s: %v", channelOrderID, err)
	}
	return orderID
}

func cleanConsoleFixtures(t *testing.T, schema, sku string) {
	t.Helper()
	clean := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'SalesOrder' AND data->>'channel' = '" + consoleTestChannel + "')")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel' = $1", consoleTestChannel)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".channel_order_mapping WHERE channel = $1", consoleTestChannel)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_reservation WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", "ITEM-"+sku)
	}
	clean()
	t.Cleanup(clean)
}

func TestOrderConsoleListFacetsAndPaging(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const sku = "TEST352-CONSOLE-SKU"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	cleanConsoleFixtures(t, schema, sku)
	for _, ref := range []string{"TEST352-A", "TEST352-B", "TEST352-C"} {
		seedConsoleOrder(t, tenantID, schema, sku, ref, 1)
	}

	res, err := ListOrdersForConsole(tenantID, OrderConsoleFilter{Channel: consoleTestChannel})
	if err != nil {
		t.Fatalf("ListOrdersForConsole: %v", err)
	}
	if res.Total != 3 {
		t.Fatalf("expected 3 orders on the test channel, got %d", res.Total)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(res.Rows))
	}
	row := res.Rows[0]
	if row["channel"] != consoleTestChannel {
		t.Errorf("channel not carried through: %v", row["channel"])
	}
	if row["line_count"].(int) != 1 {
		t.Errorf("expected line_count 1, got %v", row["line_count"])
	}
	if row["locations"] != "HO" {
		t.Errorf("expected the allocated location on the row, got %v", row["locations"])
	}

	// The channel facet must still offer the *other* channels, not collapse to
	// the one already selected - that is what skipDimension is for.
	channelFacets := res.Facets["channel"]
	var sawTestChannel bool
	for _, f := range channelFacets {
		if f.Value == consoleTestChannel {
			sawTestChannel = true
			if f.Count != 3 {
				t.Errorf("expected the test channel facet to count 3, got %d", f.Count)
			}
		}
	}
	if !sawTestChannel {
		t.Errorf("channel facet did not include the selected channel: %#v", channelFacets)
	}

	// Paging.
	page, err := ListOrdersForConsole(tenantID, OrderConsoleFilter{Channel: consoleTestChannel, Limit: 2, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 2 || page.Total != 3 {
		t.Errorf("expected 2 of 3 rows on the first page, got %d of %d", len(page.Rows), page.Total)
	}
	page2, err := ListOrdersForConsole(tenantID, OrderConsoleFilter{Channel: consoleTestChannel, Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Rows) != 1 {
		t.Errorf("expected 1 row on the second page, got %d", len(page2.Rows))
	}
}

func TestOrderConsoleDetailAssembly(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const sku = "TEST352-DETAIL-SKU"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	cleanConsoleFixtures(t, schema, sku)
	orderID := seedConsoleOrder(t, tenantID, schema, sku, "TEST352-DETAIL", 2)

	detail, err := GetOrderConsoleDetail(tenantID, orderID)
	if err != nil {
		t.Fatalf("GetOrderConsoleDetail: %v", err)
	}
	lines, _ := detail["lines"].([]map[string]interface{})
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0]["sku"] != sku || lines[0]["qty"].(int) != 2 {
		t.Errorf("line wrong: %#v", lines[0])
	}
	if lines[0]["line_total"].(float64) != 200 {
		t.Errorf("expected line_total 200, got %v", lines[0]["line_total"])
	}
	reservations, _ := detail["reservations"].([]map[string]interface{})
	if len(reservations) == 0 {
		t.Errorf("expected the order's reservation to be visible on the detail")
	}
	// Every section must be present even when empty - a missing key would make
	// the screen unable to tell "none" from "failed to load".
	for _, key := range []string{"fulfillment_tasks", "shipments", "returns", "refunds", "notifications", "invoices", "audit_trail"} {
		if _, ok := detail[key]; !ok {
			t.Errorf("detail is missing the %q section entirely", key)
		}
	}
}

func TestGlobalOrderSearchMatchesAcrossIdentifiers(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const sku = "TEST352-SEARCH-SKU"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	cleanConsoleFixtures(t, schema, sku)
	orderID := seedConsoleOrder(t, tenantID, schema, sku, "TEST352-SEARCHREF", 1)

	for _, probe := range []struct{ query, expectMatch string }{
		{"TEST352-SEARCHREF", "Channel order ID"},
		{sku, "SKU"},
		{"9876500011", "Customer phone"},
		{orderID, "Order ID"},
	} {
		results, err := SearchOrdersGlobal(tenantID, probe.query, 0)
		if err != nil {
			t.Fatalf("SearchOrdersGlobal(%q): %v", probe.query, err)
		}
		found := false
		for _, r := range results {
			if r["order_id"] == orderID {
				found = true
			}
		}
		if !found {
			t.Errorf("searching %q did not find order %s (expected a %s match); got %d results", probe.query, orderID, probe.expectMatch, len(results))
		}
	}

	if results, err := SearchOrdersGlobal(tenantID, "", 0); err != nil || len(results) != 0 {
		t.Errorf("an empty query must return nothing rather than the whole table, got %d results err=%v", len(results), err)
	}
}

func TestOrderLineHoldReleasesAndRestoresStock(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const sku = "TEST352-LINEHOLD-SKU"
	const reasonCode = "TEST352-RC-HOLD"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	cleanConsoleFixtures(t, schema, sku)

	// HoldOrderLine requires an Active ReasonCode in the Hold category.
	rc, _ := json.Marshal(map[string]interface{}{"code": reasonCode, "category": "Hold", "description": "Stage 35.3 test"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReasonCode', $2, 'Active', 'system') ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, status = 'Active'", reasonCode, rc); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", reasonCode) })

	orderID := seedConsoleOrder(t, tenantID, schema, sku, "TEST352-LINEHOLD", 5)

	var lineID string
	if err := db.DB.QueryRow("SELECT id FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1", orderID).Scan(&lineID); err != nil {
		t.Fatal(err)
	}

	reservedBefore := readReserved(t, schema, sku, "HO")
	if reservedBefore < 5 {
		t.Fatalf("expected the seeded order to have reserved 5, reserved=%d", reservedBefore)
	}

	if err := HoldOrderLine(tenantID, lineID, reasonCode, "tester"); err != nil {
		t.Fatalf("HoldOrderLine: %v", err)
	}
	if got := readReserved(t, schema, sku, "HO"); got != reservedBefore-5 {
		t.Errorf("holding a line must give its stock back: reserved %d -> %d, expected %d", reservedBefore, got, reservedBefore-5)
	}
	var lineStatus, lineHoldReason string
	if err := db.DB.QueryRow("SELECT status, COALESCE(data->>'hold_reason','') FROM "+schema+".documents WHERE id = $1", lineID).Scan(&lineStatus, &lineHoldReason); err != nil {
		t.Fatal(err)
	}
	if lineStatus != "On Hold" || lineHoldReason != reasonCode {
		t.Errorf("expected line On Hold with the reason recorded, got status=%q reason=%q", lineStatus, lineHoldReason)
	}

	// The ORDER must not have been dragged to On Hold - that is the whole
	// point of holding at line level.
	var orderStatus string
	if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", orderID).Scan(&orderStatus); err != nil {
		t.Fatal(err)
	}
	if orderStatus == "On Hold" {
		t.Errorf("holding one line must not put the whole order On Hold")
	}

	if err := ReleaseOrderLineHold(tenantID, lineID, "tester"); err != nil {
		t.Fatalf("ReleaseOrderLineHold: %v", err)
	}
	if got := readReserved(t, schema, sku, "HO"); got != reservedBefore {
		t.Errorf("releasing the line must re-reserve: expected %d, got %d", reservedBefore, got)
	}
}

func readReserved(t *testing.T, schema, sku, location string) int {
	t.Helper()
	var reserved int
	if err := db.DB.QueryRow("SELECT reserved FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, location).Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	return reserved
}

func TestEditSalesOrderRevalidatesThroughTheSameChain(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const sku = "TEST352-EDIT-SKU"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	cleanConsoleFixtures(t, schema, sku)
	orderID := seedConsoleOrder(t, tenantID, schema, sku, "TEST352-EDIT", 1)

	// An address with no pincode fails validateOrderChain, so the edit must
	// put the order On Hold rather than quietly storing a broken address.
	bad := "no pincode here"
	if err := EditSalesOrder(tenantID, orderID, OrderEdit{ShippingAddress: &bad}, "tester"); err != nil {
		t.Fatalf("EditSalesOrder: %v", err)
	}
	var status, holdReason, address string
	if err := db.DB.QueryRow("SELECT status, COALESCE(data->>'hold_reason',''), COALESCE(data->>'shipping_address','') FROM "+schema+".documents WHERE id = $1", orderID).Scan(&status, &holdReason, &address); err != nil {
		t.Fatal(err)
	}
	if status != "On Hold" || holdReason != HoldAddressInvalid {
		t.Errorf("expected an invalid edited address to hold the order with %s, got status=%q reason=%q", HoldAddressInvalid, status, holdReason)
	}
	if address != bad {
		t.Errorf("the edit should still have been stored, got %q", address)
	}

	// A no-op edit is refused rather than silently rewriting the document.
	same := bad
	if err := EditSalesOrder(tenantID, orderID, OrderEdit{ShippingAddress: &same}, "tester"); err == nil {
		t.Errorf("expected an error when an edit changes nothing")
	}

	// Custom fields are namespaced so they cannot collide with engine keys.
	if err := EditSalesOrder(tenantID, orderID, OrderEdit{CustomFields: map[string]string{"order_status": "Delivered"}}, "tester"); err != nil {
		t.Fatalf("custom field edit: %v", err)
	}
	var statusAfter, custom string
	if err := db.DB.QueryRow("SELECT status, COALESCE(data->>'custom_order_status','') FROM "+schema+".documents WHERE id = $1", orderID).Scan(&statusAfter, &custom); err != nil {
		t.Fatal(err)
	}
	if statusAfter == "Delivered" {
		t.Errorf("a custom field named order_status must not be able to move the order's real status")
	}
	if custom != "Delivered" {
		t.Errorf("expected the custom field to be stored namespaced, got %q", custom)
	}
}

func TestSetOrderPriorityAndPickQueueOrdering(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const sku = "TEST352-PRIORITY-SKU"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	cleanConsoleFixtures(t, schema, sku)
	orderID := seedConsoleOrder(t, tenantID, schema, sku, "TEST352-PRIORITY", 1)

	if err := SetOrderPriority(tenantID, orderID, "Nonsense", "tester"); err == nil {
		t.Errorf("expected an unknown priority to be rejected")
	}
	if err := SetOrderPriority(tenantID, orderID, "Expedite", "tester"); err != nil {
		t.Fatalf("SetOrderPriority: %v", err)
	}

	res, err := ListOrdersForConsole(tenantID, OrderConsoleFilter{Channel: consoleTestChannel})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) == 0 || res.Rows[0]["order_id"] != orderID {
		t.Errorf("an Expedite order must sort to the top of the console list")
	}
	if res.Rows[0]["priority"] != "Expedite" {
		t.Errorf("expected priority on the row, got %v", res.Rows[0]["priority"])
	}
}

func TestSplitOrderRefusesToEmptyTheOriginal(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const sku = "TEST352-SPLIT-SKU"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	cleanConsoleFixtures(t, schema, sku)

	item, _ := json.Marshal(map[string]interface{}{"code": sku, "name": "Split test item"})
	_, _ = db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system') ON CONFLICT (id) DO NOTHING", "ITEM-"+sku, item)
	_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
	_, _ = db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, 'HO', 500, 500)", sku)

	orderID, err := ImportChannelSalesOrder(tenantID, ChannelOrderInput{
		Channel: consoleTestChannel, ChannelOrderID: "TEST352-SPLIT",
		CustomerName: "Split Customer", ShippingAddress: "9 Split Street, Bengaluru 560001",
		PaymentStatus: "Confirmed",
		Lines: []SalesOrderLineInput{
			{SKU: sku, Qty: 1, UnitPrice: 10},
			{SKU: sku, Qty: 2, UnitPrice: 10},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	rows, err := db.DB.Query("SELECT id FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 ORDER BY id", orderID)
	if err != nil {
		t.Fatal(err)
	}
	var lineIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		lineIDs = append(lineIDs, id)
	}
	rows.Close()
	if len(lineIDs) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lineIDs))
	}

	if _, err := SplitOrder(tenantID, orderID, lineIDs, "tester"); err == nil {
		t.Errorf("splitting every line out must be refused - it leaves the original group empty")
	}

	groupID, err := SplitOrder(tenantID, orderID, lineIDs[:1], "tester")
	if err != nil {
		t.Fatalf("SplitOrder: %v", err)
	}
	var storedGroup string
	if err := db.DB.QueryRow("SELECT COALESCE(data->>'fulfillment_group','') FROM "+schema+".documents WHERE id = $1", lineIDs[0]).Scan(&storedGroup); err != nil {
		t.Fatal(err)
	}
	if storedGroup != groupID {
		t.Errorf("expected the split line to carry group %q, got %q", groupID, storedGroup)
	}
	// The order itself is untouched - a split is not a second order.
	var stillOne int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = 'TEST352-SPLIT' AND deleted_at IS NULL").Scan(&stillOne); err != nil {
		t.Fatal(err)
	}
	if stillOne != 1 {
		t.Errorf("a split must not clone the order, found %d SalesOrders", stillOne)
	}
}

func TestBulkOrderActionReportsPerOrderOutcomes(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const sku = "TEST352-BULK-SKU"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	cleanConsoleFixtures(t, schema, sku)
	orderID := seedConsoleOrder(t, tenantID, schema, sku, "TEST352-BULK", 1)

	// A mixed selection: one real order, one that does not exist. The result
	// must report both rather than failing the whole call.
	res, err := BulkOrderAction(tenantID, "release", []string{orderID, "SO-DOES-NOT-EXIST"}, "", "tester")
	if err != nil {
		t.Fatalf("BulkOrderAction: %v", err)
	}
	if len(res.Failed) == 0 {
		t.Errorf("expected the nonexistent order to be reported as failed")
	}
	if _, listed := res.Failed["SO-DOES-NOT-EXIST"]; !listed {
		t.Errorf("expected the missing order id to be keyed in Failed, got %#v", res.Failed)
	}

	if _, err := BulkOrderAction(tenantID, "nonsense", []string{orderID}, "", "tester"); err == nil {
		t.Errorf("expected an unknown bulk action to be rejected outright")
	}
}

func TestSaveAndListOMSViews(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const userID = "TEST352-USER"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'OMSSavedView' AND data->>'owner' = $1", userID)
	})

	viewID, err := SaveOMSView(tenantID, userID, "Held Shopify orders", OrderConsoleFilter{Channel: "Shopify", Status: "On Hold"})
	if err != nil {
		t.Fatalf("SaveOMSView: %v", err)
	}
	if _, err := SaveOMSView(tenantID, userID, "   ", OrderConsoleFilter{}); err == nil {
		t.Errorf("expected a blank view name to be refused")
	}

	views, err := ListOMSViews(tenantID, userID)
	if err != nil {
		t.Fatalf("ListOMSViews: %v", err)
	}
	if len(views) != 1 || views[0]["id"] != viewID {
		t.Fatalf("expected exactly the saved view back, got %#v", views)
	}
	filter, _ := views[0]["filter"].(map[string]interface{})
	if filter["Channel"] != "Shopify" || filter["Status"] != "On Hold" {
		t.Errorf("filter did not round-trip: %#v", filter)
	}

	// Another user cannot see it, and cannot delete it.
	if others, err := ListOMSViews(tenantID, "TEST352-OTHER"); err != nil || len(others) != 0 {
		t.Errorf("saved views must be per-owner, got %d for another user (err=%v)", len(others), err)
	}
	if err := DeleteOMSView(tenantID, "TEST352-OTHER", viewID); err == nil {
		t.Errorf("expected deleting another user's view to fail")
	}
	if err := DeleteOMSView(tenantID, userID, viewID); err != nil {
		t.Errorf("owner should be able to delete their own view: %v", err)
	}
}
