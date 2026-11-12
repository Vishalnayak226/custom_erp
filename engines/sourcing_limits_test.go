package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 146.4.4: ship-from-store limits - channel, online switch, daily cap,
// real distance from a PIN master, location priority; each hard or soft.
func TestStage14644ShipLimits(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T14644-%'")
		db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku LIKE 'T14644-%'")
		for _, k := range []string{"oms.ship_rule_channel", "oms.ship_rule_online", "oms.ship_rule_cap", "oms.ship_rule_distance", "oms.ship_max_distance_km"} {
			db.DB.Exec("DELETE FROM "+schema+".system_settings WHERE key = $1", k)
		}
	}
	cleanup()
	defer cleanup()

	doc := func(id, doctype string, m map[string]interface{}) {
		t14631Doc(t, schema, id, doctype, "Active", m)
	}
	loc := func(code, pincode string, extra map[string]interface{}) {
		m := map[string]interface{}{"code": code, "name": code, "type": "Warehouse", "status": "Active", "pincode": pincode}
		for k, v := range extra {
			m[k] = v
		}
		b, _ := json.Marshal(m)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", code)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')", code, b); err != nil {
			t.Fatal(err)
		}
	}
	stock := func(sku, location string, qty int) {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, $3, $3)", sku, location, qty); err != nil {
			t.Fatal(err)
		}
	}
	set := func(key, value string) {
		if err := SetSetting(tenantID, key, value, "system"); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
	rule := func(strategy string) {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = 'T14644-RULE'")
		doc("T14644-RULE", "AllocationRule", map[string]interface{}{"code": "T14644-RULE", "rule_name": "t", "strategy": strategy, "priority": -1000, "channel": "", "status": "Active"})
	}
	where := func(channel, address string, skus ...string) string {
		lines := []SalesOrderLineInput{}
		for _, s := range skus {
			lines = append(lines, SalesOrderLineInput{SKU: s, Qty: 5, UnitPrice: 10})
		}
		plan, err := ResolveAllocationPlan(tenantID, channel, address, lines)
		if err != nil {
			t.Fatal(err)
		}
		if plan == nil {
			return ""
		}
		return plan.LineLocations[0]
	}

	const sku, only = "T14644-S", "T14644-ONLY"
	loc("T14644-A", "560001", nil)
	loc("T14644-B", "560011", nil)
	stock(sku, "T14644-A", 50)
	stock(sku, "T14644-B", 30)
	stock(only, "T14644-A", 50)
	rule("Highest ATS")

	// Nothing set: exactly today's routing.
	if got := where("Web", "", sku); got != "T14644-A" {
		t.Fatalf("defaults: want A (highest ATS), got %q", got)
	}

	// Location priority: a lower number is served first; blank ranks last.
	loc("T14644-B", "560011", map[string]interface{}{"ship_priority": 1})
	if got := where("Web", "", sku); got != "T14644-B" {
		t.Fatalf("priority: want B, got %q", got)
	}
	loc("T14644-B", "560011", nil)

	// Online switch: hard excludes, soft only ranks lower.
	loc("T14644-A", "560001", map[string]interface{}{"ship_online": "No"})
	if got := where("Web", "", sku); got != "T14644-B" {
		t.Fatalf("online off: want B, got %q", got)
	}
	if got := where("Web", "", only); got != "" {
		t.Fatalf("online off, hard, only A has stock: want no plan, got %q", got)
	}
	set("oms.ship_rule_online", "soft")
	if got := where("Web", "", only); got != "T14644-A" {
		t.Fatalf("online off, soft, only A has stock: want A, got %q", got)
	}
	if got := where("Web", "", sku); got != "T14644-B" {
		t.Fatalf("online off, soft: B (no violation) must outrank A, got %q", got)
	}
	set("oms.ship_rule_online", "hard")
	loc("T14644-A", "560001", map[string]interface{}{"ship_online": "Yes"})
	if got := where("Web", "", sku); got != "T14644-A" {
		t.Fatalf("online on: want A, got %q", got)
	}

	// Channels: A serves only POS.
	loc("T14644-A", "560001", map[string]interface{}{"ship_channels": "POS, Store"})
	if got := where("Web", "", sku); got != "T14644-B" {
		t.Fatalf("channel not served: want B, got %q", got)
	}
	if got := where("POS", "", sku); got != "T14644-A" {
		t.Fatalf("channel served: want A, got %q", got)
	}

	// Daily cap: orders already sent to A today count.
	loc("T14644-A", "560001", map[string]interface{}{"ship_daily_cap": 1})
	if got := where("Web", "", sku); got != "T14644-A" {
		t.Fatalf("cap 1, none today: want A, got %q", got)
	}
	doc("T14644-L1", "SalesOrderLine", map[string]interface{}{"order_id": "T14644-O1", "sku": sku, "qty": 1, "location_code": "T14644-A", "line_status": "Reserved"})
	doc("T14644-L2", "SalesOrderLine", map[string]interface{}{"order_id": "T14644-O1", "sku": sku, "qty": 1, "location_code": "T14644-A", "line_status": "Reserved"})
	if got := where("Web", "", sku); got != "T14644-B" {
		t.Fatalf("cap 1 reached (two lines, one order): want B, got %q", got)
	}
	loc("T14644-A", "560001", map[string]interface{}{"ship_daily_cap": 2})
	if got := where("Web", "", sku); got != "T14644-A" {
		t.Fatalf("cap 2 not reached: want A, got %q", got)
	}
	loc("T14644-A", "560001", nil)

	// Real distance: the PIN master says A is the near one even though B's
	// PIN is numerically closer to the order's.
	rule("Nearest Pincode")
	if got := where("Web", "Road 560010", sku); got != "T14644-B" {
		t.Fatalf("no PIN master: today's PIN difference picks B, got %q", got)
	}
	doc("T14644-P1", "PinCode", map[string]interface{}{"code": "560010", "latitude": 12.97, "longitude": 77.59, "status": "Active"})
	doc("T14644-P2", "PinCode", map[string]interface{}{"code": "560001", "latitude": 12.975, "longitude": 77.60, "status": "Active"})
	if got := where("Web", "Road 560010", sku); got != "T14644-B" {
		t.Fatalf("B's PIN missing from the master: fall back to PIN difference, got %q", got)
	}
	doc("T14644-P3", "PinCode", map[string]interface{}{"code": "560011", "latitude": 19.07, "longitude": 72.87, "status": "Active"})
	if got := where("Web", "Road 560010", sku); got != "T14644-A" {
		t.Fatalf("PIN master complete: real distance picks A, got %q", got)
	}
	// A distance limit, hard: B (about 840 km) is out.
	set("oms.ship_max_distance_km", "100")
	loc("T14644-A", "560001", map[string]interface{}{"ship_priority": 2})
	loc("T14644-B", "560011", map[string]interface{}{"ship_priority": 1})
	rule("Highest ATS")
	if got := where("Web", "Road 560010", sku); got != "T14644-A" {
		t.Fatalf("hard distance limit: B is too far, want A, got %q", got)
	}
	// An order PIN missing from the master: the limit cannot apply.
	if got := where("Web", "Road 560099", sku); got != "T14644-B" {
		t.Fatalf("order PIN unknown, limit n/a: priority picks B, got %q", got)
	}
}
