package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

// Item 273.4: with pos.below_cost_sale on, a till line whose price after the
// cashier's own price and bill discount, without GST, is under the item's cost
// is flagged - never with the rule Off, never for an offer or a gift, never
// for an item with no cost. Only the flag leaves the server; the cart keeps
// SKU, qty and the price sold at, and the report shows the booked cost and
// the loss to a manager only.
func TestPOSSaleBelowCostIsFlaggedAndReported(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, daysAgo int, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by, created_at) VALUES ($1, $2, $3, $4, 'cashier1', now() - make_interval(days => $5))",
			id, doctype, raw, status, daysAgo); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	// 1120 with 12% GST is 1000 before GST; it cost 900. BC-NOCOST has no cost.
	insert("BC-TEE", "Item", "Active", 0, map[string]interface{}{"code": "BC-TEE", "name": "Tee", "sale_price": 1120.0, "mrp": 1120.0,
		"hsn_code": "6205", "gst_rate": 12, "standard_cost": 900.0, "status": "Active"})
	insert("BC-NOCOST", "Item", "Active", 0, map[string]interface{}{"code": "BC-NOCOST", "name": "Cap", "sale_price": 560.0, "mrp": 560.0,
		"hsn_code": "6505", "gst_rate": 12, "status": "Active"})

	quote := func(billPct float64) *QuoteResult {
		t.Helper()
		q, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: "HO", ManualDiscountPct: billPct,
			Lines: []QuoteLineRequest{{Sku: "BC-TEE", Qty: 2}, {Sku: "BC-NOCOST", Qty: 1}}})
		if err != nil {
			t.Fatalf("quote: %v", err)
		}
		return q
	}
	setRule := func(v string) {
		t.Helper()
		if err := SetSetting(tenantID, SettingBelowCostSale, v, "test"); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
	}

	t.Run("Off flags nothing", func(t *testing.T) {
		if q := quote(30); q.Lines[0].BelowCost || len(BelowCostLines(tenantID, q)) != 0 {
			t.Fatalf("with the rule Off nothing is flagged, got %+v", q.Lines[0])
		}
	})

	setRule(BelowCostWarn)
	t.Run("a bill discount that takes the price under cost is flagged", func(t *testing.T) {
		if q := quote(0); q.Lines[0].BelowCost {
			t.Fatalf("1000 before GST against a cost of 900 is not below cost")
		}
		if q := quote(5); q.Lines[0].BelowCost {
			t.Fatalf("950 before GST is still above 900")
		}
		q := quote(15) // 1120 x 0.85 = 952, 850 before GST
		if !q.Lines[0].BelowCost || q.Lines[1].BelowCost {
			t.Fatalf("15%% off: tee flagged %v (want true), cap flagged %v (want false: no cost)", q.Lines[0].BelowCost, q.Lines[1].BelowCost)
		}
		lines := BelowCostLines(tenantID, q)
		if len(lines) != 1 || lines[0]["sku"] != "BC-TEE" || lines[0]["qty"] != 2 || lines[0]["sold_at"] != 850.0 {
			t.Fatalf("cart lines = %v, want BC-TEE x2 sold at 850", lines)
		}
		if _, leaked := lines[0]["cost"]; leaked || len(lines[0]) != 3 {
			t.Fatalf("the cart keeps sku, qty and sold_at only, got %v", lines[0])
		}
		if msg := BelowCostMessage(lines); msg != "Sold below cost: BC-TEE" {
			t.Fatalf("message = %q", msg)
		}
	})

	t.Run("an offer, a voucher or a gift is not the cashier's price", func(t *testing.T) {
		q := &QuoteResult{Lines: []QuoteLine{
			{Sku: "BC-TEE", Qty: 1, LineTotal: 1120, OfferShare: 400},                      // an offer took 400 off
			{Sku: "BC-TEE", Qty: 1, LineTotal: 1120, OfferShare: 400, BillShare: 400},      // the cashier's 400
			{Sku: "BC-TEE", Qty: 1, LineTotal: 0, PromotionRef: "GWP-1"},                   // a gift
			{Sku: "BC-TEE", Qty: 1, LineTotal: 0, PriceSource: PriceSourceItemTypeFree},    // free with an item
			{Sku: "BC-TEE", Qty: -1, LineTotal: -1120, OfferShare: 0, PriceSource: "item"}, // a return line
		}}
		markBelowCostLines(tenantID, q)
		got := []bool{q.Lines[0].BelowCost, q.Lines[1].BelowCost, q.Lines[2].BelowCost, q.Lines[3].BelowCost, q.Lines[4].BelowCost}
		if want := []bool{false, true, false, false, false}; !equalBools(got, want) {
			t.Fatalf("flags = %v, want %v", got, want)
		}
	})

	t.Run("the report shows the booked cost and the loss to a manager only", func(t *testing.T) {
		costs := func(valuePaise int64) []map[string]interface{} {
			return []map[string]interface{}{{"sku": "BC-TEE", "qty": 2, "value_paise": valuePaise, "source": "average"}}
		}
		below := []map[string]interface{}{{"sku": "BC-TEE", "qty": 2, "sold_at": 850.0}}
		insert("BC-CART-1", "POSCart", "Paid", 1, map[string]interface{}{"bill_number": "B-1", "location": "HO",
			"below_cost_lines": below, "below_cost_rule": BelowCostWarn, "sale_costs": costs(180000)})
		insert("BC-CART-2", "POSCart", "Paid", 0, map[string]interface{}{"bill_number": "B-2", "location": "HO",
			"below_cost_lines": below, "below_cost_rule": BelowCostApproval, "sale_costs": costs(180000)})
		insert("BC-CART-3", "POSCart", "Draft", 0, map[string]interface{}{"bill_number": "B-3", "location": "HO",
			"below_cost_lines": below, "below_cost_rule": BelowCostApproval}) // still waiting for approval
		insert("BC-CART-4", "POSCart", "Paid", 0, map[string]interface{}{"bill_number": "B-4", "location": "HO"}) // nothing below cost
		insert("BC-CART-5", "POSCart", "Paid", 40, map[string]interface{}{"bill_number": "B-5", "location": "HO",
			"below_cost_lines": below, "below_cost_rule": BelowCostOffline, "sale_costs": costs(180000)})

		rows, err := SoldBelowCost(tenantID, "", "")
		if err != nil {
			t.Fatalf("SoldBelowCost: %v", err)
		}
		if len(rows) != 3 || rows[0]["bill"] != "B-2" || rows[1]["bill"] != "B-1" || rows[2]["bill"] != "B-5" {
			t.Fatalf("rows = %v, want B-2, B-1, B-5 (paid bills with below-cost lines, newest first)", rows)
		}
		if rows[0]["unit_cost"] != 900.0 || rows[0]["loss"] != 100.0 || rows[0]["cleared"] != "Manager" || rows[1]["cleared"] != "Cashier" || rows[2]["cleared"] != "Offline check" {
			t.Fatalf("B-2: cost %v loss %v cleared %v / %v / %v, want 900, 100 (50 x 2), Manager, Cashier, Offline check",
				rows[0]["unit_cost"], rows[0]["loss"], rows[0]["cleared"], rows[1]["cleared"], rows[2]["cleared"])
		}
		if rows[0]["bill_doctype"] != "POSCart" || rows[0]["bill_id"] != "BC-CART-2" || rows[0]["cashier"] != "cashier1" {
			t.Fatalf("bill link / cashier = %v %v %v", rows[0]["bill_doctype"], rows[0]["bill_id"], rows[0]["cashier"])
		}
		day := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
		if recent, _ := SoldBelowCost(tenantID, day, ""); len(recent) != 2 {
			t.Fatalf("from %s: %d rows, want 2 (B-5 is 40 days old)", day, len(recent))
		}

		// RunReport masks with these (the throwaway tenant has no Reports module).
		def, ok := reportRegistry["sold-below-cost"]
		if !ok || !reportMasked(def.Columns, RoleCashier) {
			t.Fatalf("the report is registered (%v) and masked for a cashier", ok)
		}
		fresh := func() []map[string]interface{} { r, _ := SoldBelowCost(tenantID, "", ""); return r }
		cashierRows := maskSensitiveColumns(def.Columns, fresh(), RoleCashier)
		if cashierRows[0]["unit_cost"] == 900.0 || cashierRows[0]["loss"] == 100.0 || cashierRows[0]["sold_at"] != 850.0 {
			t.Fatalf("a cashier must not read the cost or the loss: %v", cashierRows[0])
		}
		if managerRows := maskSensitiveColumns(def.Columns, fresh(), RoleStoreManager); managerRows[0]["unit_cost"] != 900.0 {
			t.Fatalf("a store manager reads the cost: %v", managerRows[0])
		}
	})
}

func equalBools(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
