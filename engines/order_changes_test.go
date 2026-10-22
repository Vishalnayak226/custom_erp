package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Stage 73.2.14 + 73.2.15 on a throwaway client: exception queues, one
// timeline with the next allowed step, line edits up to the pick cutoff,
// cancel after release, and linked replacement / reship orders. Stock is
// checked after every change - it must move exactly once.
func TestOrderChangesAfterReleaseAndExceptionQueues(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	doc := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status)
	}
	for _, sku := range []string{"CHG-A", "CHG-B"} {
		doc("ITEM-"+sku, "Item", "Active", map[string]interface{}{"code": sku, "name": sku})
		exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, 'HO', 10, 10)", sku)
	}
	doc("RC-CANCEL", "ReasonCode", "Active", map[string]interface{}{"code": "RC-CANCEL", "category": "Cancellation"})
	reserved := func(sku string) int {
		t.Helper()
		var n int
		if err := db.DB.QueryRow("SELECT reserved FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = 'HO'", sku).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	newOrder := func(ref, payment string, qty int) string {
		t.Helper()
		id, err := CreateSalesOrder(tenantID, SalesOrderInput{
			Channel: "ChgTest", ChannelOrderID: ref, CustomerName: "Change Customer",
			ShippingAddress: "1 Change Road, Pune 411001", PaymentStatus: payment, PreferredLocation: "HO",
			Lines: []SalesOrderLineInput{{SKU: "CHG-A", Qty: qty, UnitPrice: 100}},
		})
		if err != nil {
			t.Fatalf("create order %s: %v", ref, err)
		}
		return id
	}
	codeOf := func(err error) string {
		if verr, ok := err.(*ValidationError); ok {
			return verr.Code
		}
		return ""
	}
	taskStatus := func(orderID string) []string {
		rows, _ := db.DB.Query("SELECT status FROM "+schema+".documents WHERE doctype = 'FulfillmentTask' AND data->>'order_id' = $1 ORDER BY created_at, id", orderID)
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			rows.Scan(&s)
			out = append(out, s)
		}
		return out
	}

	// --- 73.2.15 line edits up to the pick cutoff --------------------------
	order := newOrder("CHG-1", "Confirmed", 2)
	if s, _, _ := GetOrderStatus(tenantID, order); s != "Reserved" {
		t.Fatalf("seed order is %s, want Reserved", s)
	}
	if _, _, err := ReleaseOrderToFulfillment(tenantID, order, "desk"); err != nil {
		t.Fatalf("release: %v", err)
	}
	price := 50.0
	res, err := EditOrderLines(tenantID, order, []OrderLineChange{
		{LineID: order + "-L1", Qty: 3},
		{SKU: "CHG-B", Qty: 1, UnitPrice: &price},
	}, "customer added an item", "desk")
	if err != nil {
		t.Fatalf("edit released order before picking: %v", err)
	}
	if got := taskStatus(order); len(got) != 1 || got[0] != FulfillmentTaskCancelled {
		t.Fatalf("untouched pick task should be Cancelled for re-release, got %v", got)
	}
	if reserved("CHG-A") != 3 || reserved("CHG-B") != 1 {
		t.Fatalf("reservations after edit: A=%d B=%d, want 3 and 1", reserved("CHG-A"), reserved("CHG-B"))
	}
	if res["total_after"].(float64) != 350 || res["order_status"] != "On Hold" || res["hold_reason"] != HoldPaymentPending {
		t.Fatalf("a confirmed payment that no longer covers the total must hold the order for payment: %v", res)
	}
	if _, err := EditOrderLines(tenantID, order, []OrderLineChange{{LineID: order + "-L1", Qty: 50}}, "too many", "desk"); codeOf(err) != "ORDERM-0469" {
		t.Fatalf("over-stock edit: got %v, want ORDERM-0469", err)
	}
	if reserved("CHG-A") != 3 {
		t.Fatalf("a refused edit must change nothing, reserved A=%d", reserved("CHG-A"))
	}
	if _, err := EditOrderLines(tenantID, order, []OrderLineChange{{LineID: order + "-L1", Remove: true}, {LineID: order + "-L2", Remove: true}}, "x", "desk"); codeOf(err) != "ORDERM-0467" {
		t.Fatalf("removing every line: got %v, want ORDERM-0467", err)
	}
	confirmed := "Confirmed"
	if err := EditSalesOrder(tenantID, order, OrderEdit{PaymentStatus: &confirmed}, "desk"); err != nil {
		t.Fatal(err)
	}
	if err := ReleaseOrderHold(tenantID, order); err != nil {
		t.Fatal(err)
	}
	tasks, _, err := ReleaseOrderToFulfillment(tenantID, order, "desk")
	if err != nil || len(tasks) != 1 {
		t.Fatalf("re-release after edit: %v %v", tasks, err)
	}
	var items string
	db.DB.QueryRow("SELECT data->>'items' FROM "+schema+".documents WHERE id = $1", tasks[0]).Scan(&items)
	if !strings.Contains(items, `"qty": 3`) || !strings.Contains(items, "CHG-B") {
		t.Fatalf("the new pick task must carry the edited lines, got %s", items)
	}

	// Past the cutoff: picking started.
	exec("UPDATE "+schema+".documents SET status = 'Picking', data = jsonb_set(data, '{status}', '\"Picking\"') WHERE id = $1", tasks[0])
	if _, err := EditOrderLines(tenantID, order, []OrderLineChange{{LineID: order + "-L1", Qty: 1}}, "late", "desk"); codeOf(err) != "ORDERM-0466" {
		t.Fatalf("edit after picking started: got %v, want ORDERM-0466", err)
	}

	// --- cancel after Released --------------------------------------------
	if err := CancelOrder(tenantID, order, "RC-CANCEL", "desk"); err != nil {
		t.Fatalf("cancel a released order still being picked: %v", err)
	}
	if got := taskStatus(order); got[len(got)-1] != FulfillmentTaskCancelled {
		t.Fatalf("cancel must stop the picker, task statuses %v", got)
	}
	if reserved("CHG-A") != 0 || reserved("CHG-B") != 0 {
		t.Fatalf("cancel must give every unit back: A=%d B=%d", reserved("CHG-A"), reserved("CHG-B"))
	}
	if err := TransitionTaskStatus(tenantID, tasks[0], "Packed"); err == nil {
		t.Fatal("a cancelled pick task must not move again")
	}

	packed := newOrder("CHG-2", "Confirmed", 1)
	ptasks, _, _ := ReleaseOrderToFulfillment(tenantID, packed, "desk")
	exec("UPDATE "+schema+".documents SET status = 'Packed', data = jsonb_set(data, '{status}', '\"Packed\"') WHERE id = $1", ptasks[0])
	if err := CancelOrder(tenantID, packed, "RC-CANCEL", "desk"); err == nil || !strings.Contains(err.Error(), "Packed") {
		t.Fatalf("cancel after packing must be refused, got %v", err)
	}
	if s, _, _ := GetOrderStatus(tenantID, packed); s != OrderStatusReleased {
		t.Fatalf("a refused cancel left the order %s", s)
	}

	// --- replacement / reship --------------------------------------------
	shipped := newOrder("CHG-3", "Confirmed", 2)
	exec("UPDATE "+schema+".documents SET status = 'Delivered', data = data || '{\"order_status\":\"Delivered\"}' WHERE id = $1", shipped)
	exec("UPDATE "+schema+".documents SET status = 'Dispatched', data = data || '{\"line_status\":\"Dispatched\"}' WHERE id = $1", shipped+"-L1")
	repl, created, err := CreateReplacementOrder(tenantID, shipped, "Reship", []ReplacementLine{{OriginalLineID: shipped + "-L1", Qty: 1}}, "lost in transit", "key-1", "desk")
	if err != nil || !created {
		t.Fatalf("reship: %v created=%v", err, created)
	}
	again, created, err := CreateReplacementOrder(tenantID, shipped, "Reship", []ReplacementLine{{OriginalLineID: shipped + "-L1", Qty: 1}}, "lost in transit", "key-1", "desk")
	if err != nil || created || again != repl {
		t.Fatalf("same request key must return the same order: %s %v %v", again, created, err)
	}
	if _, _, err := CreateReplacementOrder(tenantID, shipped, "Reship", []ReplacementLine{{OriginalLineID: shipped + "-L1", Qty: 2}}, "again", "key-2", "desk"); codeOf(err) != "ORDERM-0468" {
		t.Fatalf("more than shipped: got %v, want ORDERM-0468", err)
	}
	if _, _, err := CreateReplacementOrder(tenantID, shipped, "Reship", []ReplacementLine{{OriginalLineID: shipped + "-L1", SKU: "CHG-B", Qty: 1}}, "exchange", "key-3", "desk"); codeOf(err) != "ORDERM-0468" {
		t.Fatalf("a reship of a different item: got %v, want ORDERM-0468", err)
	}
	var link, price0 string
	db.DB.QueryRow("SELECT data->>'original_order_id' FROM "+schema+".documents WHERE id = $1", repl).Scan(&link)
	db.DB.QueryRow("SELECT data->>'unit_price' FROM "+schema+".documents WHERE id = $1", repl+"-L1").Scan(&price0)
	if link != shipped || price0 != "0" {
		t.Fatalf("replacement link %q price %q, want %s and 0", link, price0, shipped)
	}
	detail, err := GetOrderConsoleDetail(tenantID, shipped)
	if err != nil {
		t.Fatal(err)
	}
	if reps, _ := detail["replacements"].([]map[string]interface{}); len(reps) != 1 || reps[0]["id"] != repl {
		t.Fatalf("original order must list its replacement, got %v", detail["replacements"])
	}

	// --- 73.2.14 one timeline + next action -------------------------------
	timeline, _ := detail["timeline"].([]map[string]interface{})
	if len(timeline) < 2 || timeline[0]["title"] != "Order received" {
		t.Fatalf("timeline must start with the order, got %v", timeline)
	}
	for i := 1; i < len(timeline); i++ {
		a, _ := timeline[i-1]["at"].(time.Time)
		b, _ := timeline[i]["at"].(time.Time)
		if b.Before(a) {
			t.Fatalf("timeline out of order at %d", i)
		}
	}
	foundReplace := false
	for _, a := range detail["next_actions"].([]map[string]interface{}) {
		if a["action"] == "replace" {
			foundReplace = true
		}
	}
	if !foundReplace {
		t.Fatalf("a delivered order must offer Reship or replace, got %v", detail["next_actions"])
	}
	held := newOrder("CHG-4", "Pending", 1)
	hd, _ := GetOrderConsoleDetail(tenantID, held)
	if next := hd["next_actions"].([]map[string]interface{}); len(next) == 0 || next[0]["label"] != "Confirm payment" {
		t.Fatalf("a payment hold's first step is Confirm payment, got %v", hd["next_actions"])
	}

	// --- 73.2.14 exception queues -----------------------------------------
	exec("UPDATE "+schema+".documents SET updated_at = CURRENT_TIMESTAMP - INTERVAL '2 hours' WHERE id = $1", held)
	doc("LB-CHG", "LogisticsBooking", "In-Transit", map[string]interface{}{"order_id": shipped, "tracking_number": "AWB9"})
	// A customer return names the order's sales invoice in original_order_id
	// (the real shape); the order screen and queue must follow it to the order.
	doc("INV-CHG", "SalesInvoice", "Draft", map[string]interface{}{"sales_order_id": shipped, "total_amount": 500})
	doc("RR-CHG", "ReturnRequest", "Requested", map[string]interface{}{"original_order_id": "INV-CHG", "request_type": "Customer Return"})
	doc("RF-CHG", "RefundRequest", "Pending", map[string]interface{}{"original_order_id": shipped, "refund_mode": "Cash"})
	doc("MSL-CHG", "MarketplaceSettlementLine", "Active", map[string]interface{}{"order_id": shipped, "match_status": "Variance", "variance_amount": 12.5})
	exec("UPDATE " + schema + ".documents SET updated_at = CURRENT_TIMESTAMP - INTERVAL '3 days' WHERE id IN ('LB-CHG', 'RR-CHG', 'RF-CHG')")
	// Stage 99.5.2: a Shopify edit after picking started waits in Channel changes.
	exec("UPDATE " + schema + ".documents SET data = data || '{\"channel_change_pending\":\"Edited in Shopify after picking started\"}'::jsonb WHERE id = '" + held + "'")
	want := map[string]string{"payment": held, "carrier": shipped, "return": shipped, "settlement": shipped, "channel": held}
	for queue, orderID := range want {
		rows, err := OMSExceptionQueue(tenantID, queue)
		if err != nil {
			t.Fatalf("queue %s: %v", queue, err)
		}
		found := false
		for _, r := range rows {
			if r["order_id"] == orderID && r["next_action"] != "" {
				found = true
			}
		}
		if !found {
			t.Fatalf("queue %s should hold order %s with a next action, got %v", queue, orderID, rows)
		}
	}
	if _, err := OMSExceptionQueue(tenantID, "nope"); codeOf(err) != "ORDERM-0464" {
		t.Fatalf("unknown queue: %v", err)
	}
	counts := OMSExceptionQueueCounts(tenantID)
	if len(counts) != 5 {
		t.Fatalf("want 5 queues, got %v", counts)
	}
	for _, c := range counts {
		if c["error"] != nil || c["count"].(int) < 1 {
			t.Fatalf("queue count %v", c)
		}
	}
}

// Stage 55.9: returns and refunds reach their OMS order through
// original_order_id, directly or via the order's sales invoice.
func TestOrderDetailFindsReturnsThroughInvoice(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	doc := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatal(err)
		}
	}
	doc("SO-RT-1", "SalesOrder", "Delivered", map[string]interface{}{"order_status": "Delivered"})
	doc("INV-RT-1", "SalesInvoice", "Approved", map[string]interface{}{"sales_order_id": "SO-RT-1", "total_amount": 300})
	doc("RR-RT-1", "ReturnRequest", "Approved", map[string]interface{}{"original_order_id": "INV-RT-1", "request_type": "Customer Return"})
	doc("RF-RT-1", "RefundRequest", "Approved", map[string]interface{}{"original_order_id": "SO-RT-1", "refund_mode": "Cash"})
	detail, err := GetOrderConsoleDetail(tenantID, "SO-RT-1")
	if err != nil {
		t.Fatal(err)
	}
	returns, _ := detail["returns"].([]map[string]interface{})
	refunds, _ := detail["refunds"].([]map[string]interface{})
	if len(returns) != 1 || len(refunds) != 1 {
		t.Fatalf("the order shows its return (via invoice) and refund: returns=%v refunds=%v", returns, refunds)
	}
	labels := map[string]bool{}
	for _, a := range detail["next_actions"].([]map[string]interface{}) {
		labels[a["label"].(string)] = true
	}
	if !labels["Work the return"] || !labels["Process the refund"] {
		t.Fatalf("next steps include the open return and refund: %v", detail["next_actions"])
	}
}
