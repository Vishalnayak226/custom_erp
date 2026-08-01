package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

func TestLoyaltyTieringAndExpiry(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const custID = "TEST-TIER-CUST"

	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".loyalty_point_ledger WHERE customer_id = '" + custID + "'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Customer' AND id = '" + custID + "'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'POSCart' AND id LIKE 'TEST-TIER-CART-%'")
		db.DB.Exec("DELETE FROM " + schema + ".loyalty_tier_rules WHERE tier = 'TestTier'")
	}
	cleanup()
	defer cleanup()

	seedCustomer := func(tier string) {
		data := map[string]interface{}{"id": custID, "code": custID, "name": "Test Tier Customer"}
		if tier != "" {
			data["loyalty_tier"] = tier
		}
		bytes, _ := json.Marshal(data)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype='Customer' AND id=$1", custID)
		if _, err := db.DB.Exec(
			"INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Customer', $2, 'Active', 'system')",
			custID, bytes); err != nil {
			t.Fatalf("seed customer: %v", err)
		}
	}

	seedPaidCart := func(id string, amountPaid int) {
		data := map[string]interface{}{"id": id, "cart_number": id, "customer_id": custID, "amount_paid": amountPaid, "location": "HO", "payment_mode": "Cash"}
		bytes, _ := json.Marshal(data)
		if _, err := db.DB.Exec(
			"INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Paid', 'system')",
			id, bytes); err != nil {
			t.Fatalf("seed cart %s: %v", id, err)
		}
	}

	t.Run("tier rule CRUD", func(t *testing.T) {
		if err := UpsertLoyaltyTierRule(tenantID, "TestTier", 1234, 1.75); err != nil {
			t.Fatalf("UpsertLoyaltyTierRule: %v", err)
		}
		rules, err := GetLoyaltyTierRules(tenantID)
		if err != nil {
			t.Fatalf("GetLoyaltyTierRules: %v", err)
		}
		found := false
		var ruleID int
		for _, r := range rules {
			if r.Tier == "TestTier" {
				found = true
				ruleID = r.ID
				if r.MinSpend != 1234 || r.EarnMultiplier != 1.75 {
					t.Fatalf("expected min_spend=1234 earn_multiplier=1.75, got %+v", r)
				}
			}
		}
		if !found {
			t.Fatalf("expected TestTier rule to be present")
		}
		if err := UpsertLoyaltyTierRule(tenantID, "TestTier", 4321, 2.0); err != nil {
			t.Fatalf("UpsertLoyaltyTierRule (update): %v", err)
		}
		if err := DeleteLoyaltyTierRule(tenantID, ruleID); err != nil {
			t.Fatalf("DeleteLoyaltyTierRule: %v", err)
		}
		if err := DeleteLoyaltyTierRule(tenantID, ruleID); err == nil {
			t.Fatalf("expected deleting an already-deleted rule to error")
		}
	})

	t.Run("RecomputeLoyaltyTier picks the highest threshold met and EarnLoyaltyPoints applies its multiplier", func(t *testing.T) {
		cleanup()
		seedCustomer("")
		// Gold's seeded threshold is 20000 (db/migrations_stage26_7_crm_loyalty.sql).
		seedPaidCart("TEST-TIER-CART-1", 25000)

		tier, err := RecomputeLoyaltyTier(tenantID, custID)
		if err != nil {
			t.Fatalf("RecomputeLoyaltyTier: %v", err)
		}
		if tier != "Gold" {
			t.Fatalf("expected tier Gold for 25000 lifetime spend, got %s", tier)
		}
		var stored string
		db.DB.QueryRow("SELECT data->>'loyalty_tier' FROM "+schema+".documents WHERE id=$1", custID).Scan(&stored)
		if stored != "Gold" {
			t.Fatalf("expected Customer.loyalty_tier=Gold, got %s", stored)
		}

		multiplier := loyaltyEarnMultiplierForCustomer(tenantID, custID)
		if multiplier != 1.5 {
			t.Fatalf("expected Gold's earn_multiplier=1.5, got %v", multiplier)
		}

		if err := EarnLoyaltyPoints(tenantID, custID, 1000, "TEST-TIER-CART-1"); err != nil {
			t.Fatalf("EarnLoyaltyPoints: %v", err)
		}
		// base points = 1000/100 = 10, Gold multiplier 1.5 -> 15
		balance, err := GetLoyaltyBalance(tenantID, custID)
		if err != nil {
			t.Fatalf("GetLoyaltyBalance: %v", err)
		}
		if balance != 15 {
			t.Fatalf("expected balance=15 (10 base * 1.5 Gold multiplier), got %d", balance)
		}

		var expiresAt *time.Time
		if err := db.DB.QueryRow("SELECT expires_at FROM "+schema+".loyalty_point_ledger WHERE customer_id=$1 AND transaction_type='Earn' ORDER BY id DESC LIMIT 1", custID).Scan(&expiresAt); err != nil {
			t.Fatalf("query expires_at: %v", err)
		}
		if expiresAt == nil {
			t.Fatalf("expected the earned lot to have an expires_at set")
		}
		if daysOut := expiresAt.Sub(time.Now()).Hours() / 24; daysOut < 360 || daysOut > 370 {
			t.Fatalf("expected expires_at ~%d days out, got %.1f days", 365, daysOut)
		}
	})

	t.Run("no Customer record or no matching rule defaults the multiplier to 1", func(t *testing.T) {
		if m := loyaltyEarnMultiplierForCustomer(tenantID, "TEST-TIER-NONEXISTENT-CUSTOMER"); m != 1 {
			t.Fatalf("expected multiplier=1 for a customer with no record, got %v", m)
		}
	})

	t.Run("expiry sweep expires lapsed lots capped at current balance, idempotently", func(t *testing.T) {
		cleanup()
		past := time.Now().AddDate(0, 0, -1)
		if err := insertLoyaltyLedgerEntryInSchema(schema, custID, "Earn", 40, "POSCart", "TEST-TIER-CART-1", &past); err != nil {
			t.Fatalf("seed expired earn: %v", err)
		}
		// Customer already redeemed some of it - only 30 should still be
		// outstanding, so the sweep must cap at 30, not the full 40.
		if err := insertLoyaltyLedgerEntryInSchema(schema, custID, "Burn", 10, "POSCart", "TEST-TIER-BURN", nil); err != nil {
			t.Fatalf("seed burn: %v", err)
		}

		sweepLoyaltyExpiryForSchema(schema)

		balance, err := GetLoyaltyBalance(tenantID, custID)
		if err != nil {
			t.Fatalf("GetLoyaltyBalance: %v", err)
		}
		if balance != 0 {
			t.Fatalf("expected balance=0 after expiring the remaining 30, got %d", balance)
		}
		var expireCount int
		db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".loyalty_point_ledger WHERE customer_id=$1 AND transaction_type='Burn' AND reference_doctype='LoyaltyExpiry'", custID).Scan(&expireCount)
		if expireCount != 1 {
			t.Fatalf("expected exactly one LoyaltyExpiry burn row, got %d", expireCount)
		}

		// Idempotent: a second sweep tick must not expire anything further.
		sweepLoyaltyExpiryForSchema(schema)
		balance, _ = GetLoyaltyBalance(tenantID, custID)
		if balance != 0 {
			t.Fatalf("expected balance to remain 0 after a second sweep tick, got %d", balance)
		}
		db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".loyalty_point_ledger WHERE customer_id=$1 AND transaction_type='Burn' AND reference_doctype='LoyaltyExpiry'", custID).Scan(&expireCount)
		if expireCount != 1 {
			t.Fatalf("expected still exactly one LoyaltyExpiry burn row after a second sweep, got %d", expireCount)
		}
	})
}
