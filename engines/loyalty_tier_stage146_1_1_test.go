package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 146.1.1 - a tier is re-evaluated before the earn and once a day, and
// every change lands on the customer's timeline. Own throwaway tenant.
func TestStage14611TierReevaluatedBeforeEarnAndDaily(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}, ageDays int) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by, created_at) VALUES ($1, $2, $3, $4, 'system', NOW() - make_interval(days => $5::int))",
			id, doctype, raw, status, ageDays); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	for _, r := range []LoyaltyTierRule{{Tier: "Bronze", MinSpend: 0, EarnMultiplier: 1}, {Tier: "Gold", MinSpend: 20000, EarnMultiplier: 2}} {
		if err := SaveLoyaltyTierRule(tenantID, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.DB.Exec(`DELETE FROM ` + schema + `.loyalty_tier_rules WHERE tier NOT IN ('Bronze', 'Gold')`); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(tenantID, "loyalty.tier_window_days", "30", "tester"); err != nil {
		t.Fatal(err)
	}
	tierOf := func(id string) string { return customerLoyaltyTier(schema, id) }

	// A Gold member whose only big bill is 90 days old: outside the 30-day window.
	insert("T146-LAPSED", "Customer", "Active", map[string]interface{}{"code": "T146-LAPSED", "name": "Lapsed", "loyalty_tier": "Gold"}, 0)
	insert("T146-CART-OLD", "POSCart", "Paid", map[string]interface{}{"customer_id": "T146-LAPSED", "amount_paid": 30000, "location": "S1"}, 90)
	// A member who still qualifies: a recent big bill.
	insert("T146-ACTIVE", "Customer", "Active", map[string]interface{}{"code": "T146-ACTIVE", "name": "Active", "loyalty_tier": "Gold"}, 0)
	insert("T146-CART-NEW", "POSCart", "Paid", map[string]interface{}{"customer_id": "T146-ACTIVE", "amount_paid": 30000, "location": "S1"}, 2)

	// The bill earns at the tier the member holds TODAY, not the lapsed one.
	if err := EarnLoyaltyPoints(tenantID, "T146-LAPSED", 1000, "T146-BILL-1"); err != nil {
		t.Fatal(err)
	}
	if bal, _ := GetLoyaltyBalance(tenantID, "T146-LAPSED"); bal != 10 {
		t.Errorf("a lapsed Gold member earns at Bronze (10 points on 1000), got %d", bal)
	}
	if got := tierOf("T146-LAPSED"); got != "Bronze" {
		t.Errorf("the earn re-evaluated the tier: %q", got)
	}
	if err := EarnLoyaltyPoints(tenantID, "T146-ACTIVE", 1000, "T146-BILL-2"); err != nil {
		t.Fatal(err)
	}
	if bal, _ := GetLoyaltyBalance(tenantID, "T146-ACTIVE"); bal != 20 {
		t.Errorf("a member who still qualifies keeps the Gold multiplier (20), got %d", bal)
	}

	// The daily pass: back to Gold for the first, so the job has work to do.
	if _, err := db.DB.Exec(`UPDATE ` + schema + `.documents SET data = jsonb_set(data, '{loyalty_tier}', '"Gold"') WHERE id = 'T146-LAPSED'`); err != nil {
		t.Fatal(err)
	}
	changed := reevaluateLoyaltyTiersForSchema(schema)
	if changed != 1 || tierOf("T146-LAPSED") != "Bronze" || tierOf("T146-ACTIVE") != "Gold" {
		t.Errorf("the daily pass moves only the lapsed member: changed %d, %q / %q", changed, tierOf("T146-LAPSED"), tierOf("T146-ACTIVE"))
	}
	events := func() int {
		var n int
		_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.crm_events WHERE customer_id = 'T146-LAPSED' AND event_type = 'tier_changed'`).Scan(&n)
		return n
	}
	if events() != 2 { // the earn's change (Gold to Bronze) and the daily pass's (Gold to Bronze again)
		t.Errorf("each tier change is on the timeline: %d event(s)", events())
	}
	if again := reevaluateLoyaltyTiersForSchema(schema); again != 0 || events() != 2 {
		t.Errorf("a second pass changes nothing: %d, %d event(s)", again, events())
	}

	// Spend below every tier clears the tier (it used to stay).
	if _, err := db.DB.Exec(`DELETE FROM ` + schema + `.loyalty_tier_rules WHERE tier = 'Bronze'`); err != nil {
		t.Fatal(err)
	}
	if _, err := RecomputeLoyaltyTier(tenantID, "T146-LAPSED"); err != nil {
		t.Fatal(err)
	}
	if got := tierOf("T146-LAPSED"); got != "" {
		t.Errorf("a member below the lowest tier has none: %q", got)
	}
	tl, err := GetCRMTimeline(tenantID, "T146-LAPSED", "", 50, []string{"tier"})
	if err != nil || len(tl.Entries) == 0 || tl.Entries[0].Kind != "tier" {
		t.Errorf("the timeline shows tier changes: %v %+v", err, tl)
	}
}
