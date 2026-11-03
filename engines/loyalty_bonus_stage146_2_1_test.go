package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 146.2.1 - Bonus Points is an Offer type: extra points per rupee on a
// category, per unit or per bill, with a window, a tier, stores and stacking.
// Base points always apply; each rule is its own ledger row naming the offer;
// the till preview shows what the bill will earn.
func TestStage14621BonusPointsOffer(t *testing.T) {
	tenantID, schema, insert := stage1462Fixture(t)
	for k, v := range map[string]string{"loyalty.rupees_per_point": "100", "loyalty.pending_points": "true", "sales.return_window_days": "7"} {
		if err := SetSetting(tenantID, k, v, "tester"); err != nil {
			t.Fatal(err)
		}
	}
	const loc = "S146"
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "Store 146", "type": "Store", "sellable": "Yes"})
	for _, it := range []struct {
		code, cat string
	}{{"SWEET", "Sweets"}, {"LAMP", "Decor"}} {
		insert(it.code, "Item", "Active", map[string]interface{}{"code": it.code, "name": it.code + " name", "category": it.cat, "sale_price": 500, "hsn_code": "7113", "tax_treatment": "Exempt"})
	}
	insert("T146-CUST", "Customer", "Active", map[string]interface{}{"code": "T146-CUST", "name": "Bonus customer"})
	offer := func(id string, data map[string]interface{}) {
		t.Helper()
		data["name"], data["offer_type"] = id, "Bonus Points"
		if _, ok := data["scope"]; !ok {
			data["scope"] = "Bill"
		}
		insert(id, "Offer", "Active", data)
	}
	lines := []LoyaltyEarnLine{{Sku: "SWEET", Qty: 2, Net: 1000}, {Sku: "LAMP", Qty: 2, Net: 1000}}
	plan := func(customer, store string) *LoyaltyEarnPlan {
		t.Helper()
		p, err := PlanLoyaltyEarn(tenantID, LoyaltyEarnInput{CustomerID: customer, Location: store, Net: 2000, Lines: lines})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	// No bonus offer: base points only, as today.
	if p := plan("T146-CUST", loc); p.Total != 20 || len(p.Rows) != 1 {
		t.Fatalf("no offer: %+v, want the 20 base points alone", p)
	}

	// The Diwali rule: 2 points per Rs 100 on Sweets only.
	offer("Diwali", map[string]interface{}{"scope": "Category", "scope_value": "Sweets", "bonus_basis": "Spend", "bonus_points": 2, "bonus_spend_step": 100,
		"valid_from": "2020-01-01", "valid_to": "2099-12-31", "priority": 1, "stackable": "Yes"})
	p := plan("T146-CUST", loc)
	if p.Total != 40 || len(p.Rows) != 2 || p.Rows[1].Points != 20 || !p.Rows[1].Bonus || p.Rows[1].Reason != "Bonus: Diwali" {
		t.Fatalf("Diwali on Rs 1000 of sweets: %+v, want base 20 + bonus 20 named for the offer", p)
	}

	// Per unit, per bill (with a minimum), stacking with the Diwali rule.
	offer("PerUnit", map[string]interface{}{"scope": "Item", "scope_value": "LAMP", "bonus_basis": "Unit", "bonus_points": 3, "priority": 2, "stackable": "Yes"})
	offer("PerBillBig", map[string]interface{}{"bonus_basis": "Bill", "bonus_points": 50, "min_bill_amount": 5000, "priority": 3, "stackable": "Yes"})
	offer("PerBill", map[string]interface{}{"bonus_basis": "Bill", "bonus_points": 5, "priority": 4, "stackable": "Yes"})
	if p := plan("T146-CUST", loc); p.Total != 20+20+6+5 || len(p.Rows) != 4 {
		t.Errorf("stacked rules: %+v, want 20 base + 20 + 6 (2 lamps x 3) + 5 (per bill; the Rs 5000 one needs a bigger bill)", p)
	}

	// Window, store and tier gate a rule; blank means everyone.
	offer("Expired", map[string]interface{}{"bonus_basis": "Bill", "bonus_points": 99, "valid_to": "2020-12-31", "priority": 5, "stackable": "Yes"})
	offer("OtherStore", map[string]interface{}{"bonus_basis": "Bill", "bonus_points": 98, "stores": "S999", "priority": 6, "stackable": "Yes"})
	offer("GoldOnly", map[string]interface{}{"bonus_basis": "Bill", "bonus_points": 97, "customer_tier": "Gold", "priority": 7, "stackable": "Yes"})
	if p := plan("T146-CUST", loc); p.Total != 51 {
		t.Errorf("expired, other-store and Gold-only rules must not apply: %+v", p)
	}
	if p := plan("T146-CUST", "S999"); p.Total != 20+20+6+5+98 {
		t.Errorf("a rule for store S999 applies there: %+v", p)
	}

	// Non-stackable: the highest-priority applicable rule alone ends the list.
	if _, err := db.DB.Exec(`UPDATE ` + schema + `.documents SET status = 'Inactive' WHERE doctype = 'Offer'`); err != nil {
		t.Fatal(err)
	}
	offer("Solo1", map[string]interface{}{"bonus_basis": "Bill", "bonus_points": 7, "priority": 1, "stackable": "No"})
	offer("Solo2", map[string]interface{}{"bonus_basis": "Bill", "bonus_points": 9, "priority": 2, "stackable": "Yes"})
	if p := plan("T146-CUST", loc); p.Total != 27 || len(p.Rows) != 2 || p.Rows[1].Reason != "Bonus: Solo1" {
		t.Errorf("a non-stackable rule wins alone: %+v, want 20 + 7", p)
	}

	// The ledger: one row per rule, the offer named, pending like base points.
	if _, err := db.DB.Exec(`UPDATE ` + schema + `.documents SET status = 'Inactive' WHERE doctype = 'Offer'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`UPDATE ` + schema + `.documents SET status = 'Active' WHERE doctype = 'Offer' AND id IN ('Diwali', 'PerBill')`); err != nil {
		t.Fatal(err)
	}
	insert("T146-BILL", "POSCart", "Paid", map[string]interface{}{"customer_id": "T146-CUST", "location": loc,
		"items": []map[string]interface{}{{"sku": "SWEET", "qty": 2, "sale_price": 500}, {"sku": "LAMP", "qty": 2, "sale_price": 500}}})
	if err := EarnLoyaltyPoints(tenantID, "T146-CUST", 2000, "T146-BILL"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.DB.Query(`SELECT points, COALESCE(reason, ''), pending_until IS NOT NULL, COALESCE(location, '') FROM ` + schema + `.loyalty_point_ledger WHERE reference_id = 'T146-BILL' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type row struct {
		points  int
		reason  string
		pending bool
		loc     string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.points, &r.reason, &r.pending, &r.loc); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	want := []row{{20, "", true, loc}, {20, "Bonus: Diwali", true, loc}, {5, "Bonus: PerBill", true, loc}}
	if len(got) != len(want) {
		t.Fatalf("ledger rows %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ledger row %d: %+v, want %+v", i, got[i], want[i])
		}
	}

	// The till preview: the quote says what the bill will earn.
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "SWEET", "qty": 10.0}, map[string]interface{}{"sku": "LAMP", "qty": 10.0}}, false); err != nil {
		t.Fatalf("stock: %v", err)
	}
	q, err := ResolvePOSQuote(tenantID, QuoteRequest{CartNumber: "T146-Q", Location: loc, CustomerID: "T146-CUST", Lines: []QuoteLineRequest{{Sku: "SWEET", Qty: 2}, {Sku: "LAMP", Qty: 2}}})
	if err != nil || q.Loyalty == nil {
		t.Fatalf("quote: %v %+v", err, q)
	}
	if q.Loyalty.EarnPoints != 45 {
		t.Errorf("the till preview says %d, want 45 (20 base + 20 Diwali + 5 per bill)", q.Loyalty.EarnPoints)
	}
}
