package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// Stage 146.2.3 - items that cannot be paid with points: the redeemable cap
// works on the eligible lines' net only, and the points tender is spread over
// the eligible lines only.
func TestStage14623NoPointsItems(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insertDoc := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
			t.Fatal(err)
		}
	}
	item := func(code, flag string) {
		d := map[string]interface{}{"code": code, "name": code, "sale_price": 100.0, "hsn_code": "6912", "tax_treatment": "Exempt"}
		if flag != "" {
			d["loyalty_redemption"] = flag
		}
		insertDoc(code, "Item", d)
		if _, err := PostInventoryLedger(tenantID, "STORE-NP", []interface{}{map[string]interface{}{"sku": code, "qty": 10.0}}, false); err != nil {
			t.Fatal(err)
		}
	}
	item("NP-OK", "")
	item("NP-GOLD", "Not allowed")
	item("NP-GRP", "")
	item("NP-GRP-OVR", "Allowed")
	insertDoc("NP-GROUP", "PIMProductGroup", map[string]interface{}{"name": "No points", "group_type": "Static", "no_points_redemption": true,
		"members": []map[string]interface{}{{"item_code": "NP-GRP"}, {"item_code": "NP-GRP-OVR"}}})
	if err := insertLoyaltyLedgerEntryInSchema(schemaName, "CUST-NP", "Earn", 1000, "Seed", "SEED", nil); err != nil {
		t.Fatal(err)
	}

	quote := func(redeem int, skus ...string) *QuoteResult {
		t.Helper()
		var lines []QuoteLineRequest
		for _, s := range skus {
			lines = append(lines, QuoteLineRequest{Sku: s, Qty: 1})
		}
		q, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: "STORE-NP", CustomerID: "CUST-NP", RedeemPoints: redeem, Lines: lines})
		if err != nil {
			t.Fatalf("quote: %v", err)
		}
		return q
	}

	// Nothing flagged: the cap is the whole bill, as before.
	if q := quote(0, "NP-OK", "NP-OK"); q.Loyalty.MaxPoints != 200 || q.Loyalty.Limited {
		t.Errorf("plain bill: max %d limited %v, want 200 / false", q.Loyalty.MaxPoints, q.Loyalty.Limited)
	}
	// One excluded line (item flag): the cap is the eligible net only.
	q := quote(0, "NP-OK", "NP-GOLD")
	if q.Loyalty.MaxPoints != 100 || !q.Loyalty.Limited || q.Loyalty.EligibleNet != 100 {
		t.Errorf("one excluded line: max %d limited %v net %v, want 100 / true / 100", q.Loyalty.MaxPoints, q.Loyalty.Limited, q.Loyalty.EligibleNet)
	}
	// Group flag takes the member out; the item's own "Allowed" overrides it.
	if q := quote(0, "NP-OK", "NP-GRP"); q.Loyalty.MaxPoints != 100 {
		t.Errorf("group-excluded item: max %d, want 100", q.Loyalty.MaxPoints)
	}
	if q := quote(0, "NP-OK", "NP-GRP-OVR"); q.Loyalty.MaxPoints != 200 {
		t.Errorf("item overrides group: max %d, want 200", q.Loyalty.MaxPoints)
	}
	// Only excluded lines: nothing can be redeemed.
	if q := quote(50, "NP-GOLD"); q.Loyalty.MaxPoints != 0 || q.Loyalty.Points != 0 {
		t.Errorf("all excluded: max %d points %d, want 0 / 0", q.Loyalty.MaxPoints, q.Loyalty.Points)
	}

	// Checkout: asking for more than the eligible net is capped there, and the
	// tender lands on the eligible line only.
	cartData, _ := json.Marshal(map[string]interface{}{"location": "STORE-NP", "payment_mode": "Cash", "customer_id": "CUST-NP", "redeem_points": 150,
		"items": POSCartItemsFromQuote(quote(0, "NP-OK", "NP-GOLD"))})
	if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ('CART-NP', 'POSCart', $1, 'Processing', 'system')", cartData); err != nil {
		t.Fatal(err)
	}
	out, err := FinalizePOSCheckoutCommitted(tenantID, "CART-NP", "", "", nil)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if out.LoyaltyDiscount != 100.0 {
		t.Errorf("points paid %v, want 100 (the eligible net)", out.LoyaltyDiscount)
	}
	if b, _ := GetLoyaltyBalance(tenantID, "CUST-NP"); b != 901 {
		t.Errorf("balance = %d, want 901 (100 used, 50 returned, 1 earned on the cash paid)", b)
	}
	var alloc string
	_ = db.DB.QueryRow("SELECT COALESCE(data->>'allocation','') FROM " + schemaName + ".documents WHERE id = 'CART-NP'").Scan(&alloc)
	var lines []SaleLineAllocation
	if err := json.Unmarshal([]byte(alloc), &lines); err != nil || len(lines) != 2 {
		t.Fatalf("allocation not stored (%v): %q", err, alloc)
	}
	for _, l := range lines {
		if l.SKU == "NP-GOLD" && l.Points != 0 {
			t.Errorf("excluded line took %d points", l.Points)
		}
		if l.SKU == "NP-OK" && l.Points != 100 {
			t.Errorf("eligible line took %d points, want 100", l.Points)
		}
	}
}
