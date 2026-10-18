package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

// Stage 73.2.11 / 73.2.12 / 73.3.1 - loyalty administration: manual
// adjustments (posted at once below the approval value, by approval above it,
// a debit never below zero); accrual (the liability in 2630 always equals the
// points outstanding at their value, and the movement report reconciles);
// tiers set by real spend (amount_paid) and held through their grace days.
// Own throwaway tenant.
func TestStage73LoyaltyAdministration(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	code := func(err error) string {
		if ve, ok := err.(*ValidationError); ok {
			return ve.Code
		}
		return ""
	}
	liability := func() int64 {
		var v int64
		_ = db.DB.QueryRow(`SELECT COALESCE(SUM(credit - debit), 0) FROM ` + schemaName + `.gl_postings WHERE account_code = '2630'`).Scan(&v)
		return v
	}
	const cust, loc = "CUST-LA", "STORE-LA"
	insert(cust, "Customer", "Active", map[string]interface{}{"code": cust, "name": "Ravi"})
	insert("LA-GOODWILL", "ReasonCode", "Active", map[string]interface{}{"code": "LA-GOODWILL", "category": "Loyalty Adjustment", "status": "Active"})
	insert("ITEM-LA", "Item", "Active", map[string]interface{}{"code": "ITEM-LA", "name": "Lamp", "sale_price": 3000.0, "hsn_code": "9405", "tax_treatment": "Exempt"})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "ITEM-LA", "qty": 20.0}}, false); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(tenantID, "loyalty.liability_accounting", "accrue", "tester"); err != nil {
		t.Fatal(err)
	}

	// --- 73.2.11 adjustments ------------------------------------------------------
	if _, err := RequestLoyaltyAdjustment(tenantID, cust, 100, "", "", "system", "Store Manager"); code(err) != "CUSTOM-0614" {
		t.Errorf("an adjustment needs a reason: %v", err)
	}
	adj, err := RequestLoyaltyAdjustment(tenantID, cust, 100, "LA-GOODWILL", "service recovery", "system", "Store Manager")
	if err != nil || adj.Status != "Posted" {
		t.Fatalf("a small credit posts at once: %v %+v", err, adj)
	}
	if _, err := RequestLoyaltyAdjustment(tenantID, cust, -150, "LA-GOODWILL", "", "system", "Store Manager"); code(err) != "CUSTOM-0615" {
		t.Errorf("a debit below zero is refused: %v", err)
	}
	big, err := RequestLoyaltyAdjustment(tenantID, cust, 600, "LA-GOODWILL", "migration correction", "system", "Store Manager")
	if err != nil || big.Status != "Pending Approval" || big.RequiredRole != "Store Manager" {
		t.Fatalf("600 points (worth 600) waits for a manager: %v %+v", err, big)
	}
	if b, _ := GetLoyaltyBalance(tenantID, cust); b != 100 {
		t.Errorf("nothing posts before approval: balance %d", b)
	}
	if err := DecideApproval(tenantID, "LoyaltyPointAdjustment", big.ID, "manager-la", "Store Manager", "", "Approved", "ok"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := RequestLoyaltyAdjustment(tenantID, cust, -50, "LA-GOODWILL", "", "system", "Store Manager"); err != nil {
		t.Fatalf("debit: %v", err)
	}
	if b, _ := GetLoyaltyBalance(tenantID, cust); b != 650 {
		t.Errorf("100 + 600 - 50: balance %d, want 650", b)
	}
	if err := checkLoyaltyRedemptionVelocity(tenantID, cust); err != nil {
		t.Errorf("an adjustment is not a redemption for the daily limit: %v", err)
	}
	if list, _ := ListLoyaltyAdjustments(tenantID, cust); len(list) != 3 {
		t.Errorf("adjustments listed: %d, want 3", len(list))
	}
	if got := liability(); got != 65000 {
		t.Errorf("accruing: the liability is the points at their value: %d paise, want 65000", got)
	}

	// --- a sale: earn and redeem, both through the liability ------------------------
	insert("CART-LA", "POSCart", "Processing", map[string]interface{}{"location": loc, "payment_mode": "Cash", "customer_id": cust, "redeem_points": 200,
		"items": []map[string]interface{}{{"sku": "ITEM-LA", "qty": 2, "sale_price": 3000.0}}})
	if _, _, err := FinalizePOSCheckout(tenantID, "CART-LA", ""); err != nil {
		t.Fatalf("sale: %v", err)
	}
	bal, _ := GetLoyaltyBalance(tenantID, cust) // 650 - 200 + (6000-200)/100 = 508
	if bal != 508 {
		t.Errorf("balance after the sale = %d, want 508", bal)
	}
	if got := liability(); got != int64(bal)*100 {
		t.Errorf("the liability follows the points: %d paise for %d points", got, bal)
	}
	var paid float64
	_ = db.DB.QueryRow(`SELECT (data->>'amount_paid')::numeric FROM ` + schemaName + `.documents WHERE id = 'CART-LA'`).Scan(&paid)
	if paid != 6000 {
		t.Errorf("the sale records what it took: amount_paid %.2f, want 6000", paid)
	}
	rows, err := loyaltyMovementReport(tenantID, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]interface{}{}
	for _, r := range rows {
		got[r["movement"].(string)] = r["points"]
	}
	if got["Closing balance"] != int64(508) || got["Redeemed at the till and online"] != int64(-200) || got["Credited by adjustment"] != int64(700) || got["Debited by adjustment"] != int64(-50) {
		t.Errorf("movement report: %v", got)
	}
	for _, r := range rows {
		if r["movement"] == "Books less points outstanding" && r["value"] != 0.0 {
			t.Errorf("the books reconcile to the points: %v", r["value"])
		}
		if r["movement"] == "DIFFERENCE from the ledger (investigate)" {
			t.Errorf("the movements add up to the ledger: %v", r)
		}
	}

	// --- 73.3.1 tiers on real spend, with grace -----------------------------------------
	for _, r := range []LoyaltyTierRule{{Tier: "Bronze", MinSpend: 0, EarnMultiplier: 1}, {Tier: "Silver", MinSpend: 5000, EarnMultiplier: 1.25, Benefits: "Free alterations", GraceDays: 30}} {
		if err := SaveLoyaltyTierRule(tenantID, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.DB.Exec(`DELETE FROM ` + schemaName + `.loyalty_tier_rules WHERE tier NOT IN ('Bronze', 'Silver')`); err != nil {
		t.Fatal(err)
	}
	if tier, err := RecomputeLoyaltyTier(tenantID, cust); err != nil || tier != "Silver" {
		t.Fatalf("6000 spent reaches Silver: %v %q", err, tier)
	}
	// A 2000 refund takes spend to 4000, below Silver: the 30 grace days apply.
	insert("RF-LA", "RefundRequest", "Processed", map[string]interface{}{"original_order_id": "CART-LA", "status": "Processed", "amount": 2000.0, "net_amount": 2000.0})
	if tier, _ := RecomputeLoyaltyTier(tenantID, cust); tier != "Silver" {
		t.Errorf("within the grace days the tier is kept: %q", tier)
	}
	var since string
	_ = db.DB.QueryRow(`SELECT COALESCE(data->>'loyalty_tier_below_since', '') FROM `+schemaName+`.documents WHERE id = $1`, cust).Scan(&since)
	if since == "" {
		t.Fatalf("the drop below Silver is dated")
	}
	old := time.Now().AddDate(0, 0, -40).Format("2006-01-02")
	if _, err := db.DB.Exec(`UPDATE `+schemaName+`.documents SET data = jsonb_set(data, '{loyalty_tier_below_since}', to_jsonb($2::text)) WHERE id = $1`, cust, old); err != nil {
		t.Fatal(err)
	}
	if tier, _ := RecomputeLoyaltyTier(tenantID, cust); tier != "Bronze" {
		t.Errorf("after the grace days the tier drops: %q", tier)
	}
	rules, _ := GetLoyaltyTierRules(tenantID)
	if len(rules) != 2 || rules[1].Benefits != "Free alterations" || rules[1].GraceDays != 30 {
		t.Errorf("tier rules keep benefits and grace: %+v", rules)
	}
}

// Stage 73.3.2 - the till's view of a customer: points and value, tier,
// benefits and the way to the next tier, recent bills, and only the
// vouchers they can use now. Own throwaway tenant.
func TestStage73TillCustomerContext(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insert("CUST-CTX", "Customer", "Active", map[string]interface{}{"code": "CUST-CTX", "name": "Meera", "phone": "9876543210", "loyalty_tier": "Bronze"})
	insert("CART-CTX", "POSCart", "Paid", map[string]interface{}{"customer_id": "CUST-CTX", "amount_paid": 3000.0, "items": []map[string]interface{}{{"sku": "X", "qty": 2, "sale_price": 1500.0}}})
	insert("V-CTX1", "Voucher", "Active", map[string]interface{}{"code": "MEERA10", "discount_type": "Percentage", "discount_value": 10.0, "customer_id": "CUST-CTX", "status": "Active"})
	insert("V-CTX2", "Voucher", "Active", map[string]interface{}{"code": "OLDONE", "discount_type": "Flat", "discount_value": 50.0, "customer_id": "CUST-CTX", "expiry_date": "2020-01-01", "status": "Active"})
	if err := insertLoyaltyLedgerEntryInSchema(schemaName, "CUST-CTX", "Earn", 120, "Seed", "SEED", nil); err != nil {
		t.Fatal(err)
	}
	for _, r := range []LoyaltyTierRule{{Tier: "Bronze", MinSpend: 0, EarnMultiplier: 1, Benefits: "Birthday voucher"}, {Tier: "Silver", MinSpend: 5000, EarnMultiplier: 1.25}} {
		if err := SaveLoyaltyTierRule(tenantID, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.DB.Exec(`DELETE FROM ` + schemaName + `.loyalty_tier_rules WHERE tier NOT IN ('Bronze', 'Silver')`); err != nil {
		t.Fatal(err)
	}
	c, err := TillCustomerContextFor(tenantID, "CUST-CTX")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Meera" || c.Phone != "******3210" || c.Balance != 120 || c.Tier != "Bronze" || c.Benefits != "Birthday voucher" {
		t.Errorf("who and what: %+v", c)
	}
	if c.Spend != 3000 || c.NextTier != "Silver" || c.ToNextTier != 2000 {
		t.Errorf("progress to the next tier: spend %.2f next %q to go %.2f", c.Spend, c.NextTier, c.ToNextTier)
	}
	if len(c.RecentBills) != 1 || c.RecentBills[0].Amount != 3000 || c.RecentBills[0].Lines != 1 {
		t.Errorf("recent bills: %+v", c.RecentBills)
	}
	if len(c.Vouchers) != 1 || c.Vouchers[0].Code != "MEERA10" {
		t.Errorf("only usable vouchers are offered: %+v", c.Vouchers)
	}
}
