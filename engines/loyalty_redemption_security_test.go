package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
	"time"
)

func TestLoyaltyRedemptionSecurity(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const custID = "TEST-LRS-CUST"

	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".loyalty_redemption_otp_challenges WHERE customer_id = '" + custID + "'")
		db.DB.Exec("DELETE FROM " + schema + ".loyalty_point_ledger WHERE customer_id = '" + custID + "'")
		db.DB.Exec("DELETE FROM " + schema + ".approval_log WHERE document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'LoyaltyRedemptionRequest' AND data->>'customer_id' = '" + custID + "')")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'LoyaltyRedemptionRequest' AND data->>'customer_id' = '" + custID + "'")
	}
	cleanup()
	defer cleanup()

	seedEarn := func(points int) {
		if err := insertLoyaltyLedgerEntryInSchema(schema, custID, "Earn", points, "POSCart", "TEST-LRS-SEED", nil); err != nil {
			t.Fatalf("seed earn: %v", err)
		}
	}

	seedChallenge := func(id string, points int, code string) {
		if _, err := db.DB.Exec(fmt.Sprintf(`
			INSERT INTO %s.loyalty_redemption_otp_challenges (id, customer_id, points, reference_id, initiated_by, otp_hash, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, schema),
			id, custID, points, "TEST-LRS-REF", "manager1", hashOTP(code), time.Now().Add(5*time.Minute)); err != nil {
			t.Fatalf("seed challenge %s: %v", id, err)
		}
	}

	t.Run("insufficient balance rejected at initiate", func(t *testing.T) {
		cleanup()
		seedEarn(10)
		if _, err := InitiateSecureLoyaltyRedemption(tenantID, custID, 100, "TEST-LRS-REF", "manager1"); err == nil {
			t.Fatalf("expected insufficient-balance rejection")
		}
	})

	t.Run("velocity guard blocks after daily redemption cap", func(t *testing.T) {
		cleanup()
		seedEarn(1000)
		maxPerDay := GetSettingInt(tenantID, "security.loyalty_max_redemptions_per_day")
		for i := 0; i < maxPerDay; i++ {
			if err := insertLoyaltyLedgerEntryInSchema(schema, custID, "Burn", 1, "POSCart", fmt.Sprintf("TEST-LRS-BURN-%d", i), nil); err != nil {
				t.Fatalf("seed burn %d: %v", i, err)
			}
		}
		if _, err := InitiateSecureLoyaltyRedemption(tenantID, custID, 10, "TEST-LRS-REF", "manager1"); err == nil {
			t.Fatalf("expected velocity guard to reject after %d redemptions today", maxPerDay)
		}
	})

	t.Run("wrong OTP is rejected and does not consume the challenge", func(t *testing.T) {
		cleanup()
		seedEarn(1000)
		seedChallenge("TEST-LRS-CH-WRONG", 10, "123456")
		if _, _, _, err := VerifyAndRedeemLoyaltyOTP(tenantID, "TEST-LRS-CH-WRONG", "000000"); err == nil {
			t.Fatalf("expected wrong OTP to be rejected")
		}
		// The correct code should still work afterwards - a wrong guess
		// doesn't burn the challenge.
		if result, discount, _, err := VerifyAndRedeemLoyaltyOTP(tenantID, "TEST-LRS-CH-WRONG", "123456"); err != nil || result != "Redeemed" || discount != 10 {
			t.Fatalf("expected the correct OTP to still redeem after a wrong guess, got result=%s discount=%d err=%v", result, discount, err)
		}
		// Re-using the same (now-verified) challenge must fail.
		if _, _, _, err := VerifyAndRedeemLoyaltyOTP(tenantID, "TEST-LRS-CH-WRONG", "123456"); err == nil {
			t.Fatalf("expected a re-used challenge to be rejected")
		}
	})

	t.Run("below the staff-restriction threshold redeems immediately", func(t *testing.T) {
		cleanup()
		seedEarn(1000)
		seedChallenge("TEST-LRS-CH-SMALL", 10, "654321") // value 10 << the 500 threshold
		result, discount, requestID, err := VerifyAndRedeemLoyaltyOTP(tenantID, "TEST-LRS-CH-SMALL", "654321")
		if err != nil {
			t.Fatalf("VerifyAndRedeemLoyaltyOTP: %v", err)
		}
		if result != "Redeemed" || discount != 10 || requestID != "" {
			t.Fatalf("expected immediate Redeemed with discount=10, got result=%s discount=%d requestID=%s", result, discount, requestID)
		}
		balance, err := GetLoyaltyBalance(tenantID, custID)
		if err != nil {
			t.Fatalf("GetLoyaltyBalance: %v", err)
		}
		if balance != 990 {
			t.Fatalf("expected balance=990 after redeeming 10, got %d", balance)
		}
	})

	t.Run("at/above the staff-restriction threshold requires approval before redeeming", func(t *testing.T) {
		cleanup()
		seedEarn(1000)
		seedChallenge("TEST-LRS-CH-BIG", 600, "111222") // value 600 >= the 500 threshold -> Store Manager approval required
		result, discount, requestID, err := VerifyAndRedeemLoyaltyOTP(tenantID, "TEST-LRS-CH-BIG", "111222")
		if err != nil {
			t.Fatalf("VerifyAndRedeemLoyaltyOTP: %v", err)
		}
		if result != "PendingApproval" || discount != 0 || requestID == "" {
			t.Fatalf("expected PendingApproval with a requestID, got result=%s discount=%d requestID=%s", result, discount, requestID)
		}

		// Not yet redeemed - the points must still be in the customer's balance.
		balance, err := GetLoyaltyBalance(tenantID, custID)
		if err != nil {
			t.Fatalf("GetLoyaltyBalance (pre-approval): %v", err)
		}
		if balance != 1000 {
			t.Fatalf("expected balance still 1000 before approval, got %d", balance)
		}

		if err := SubmitForApproval(tenantID, "LoyaltyRedemptionRequest", requestID, "manager1", "Store Manager"); err != nil {
			t.Fatalf("SubmitForApproval: %v", err)
		}
		if err := DecideApproval(tenantID, "LoyaltyRedemptionRequest", requestID, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
			t.Fatalf("DecideApproval: %v", err)
		}

		var status string
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id=$1", requestID).Scan(&status); err != nil {
			t.Fatalf("query request status: %v", err)
		}
		if status != "Redeemed" {
			t.Fatalf("expected request status Redeemed after approval, got %s", status)
		}
		balance, err = GetLoyaltyBalance(tenantID, custID)
		if err != nil {
			t.Fatalf("GetLoyaltyBalance (post-approval): %v", err)
		}
		if balance != 400 {
			t.Fatalf("expected balance=400 after the approved 600-point redemption, got %d", balance)
		}
	})

	t.Run("expired challenge is rejected", func(t *testing.T) {
		cleanup()
		seedEarn(1000)
		// A 25-hour margin, not a small one: this dev Postgres session's
		// naive "timestamp without time zone" columns read back several
		// hours off from Go's time.Now() via lib/pq (see project_ledger.md
		// §52's SLA Breach test for the same precedent) - a 1-minute margin
		// would be swallowed by that skew and this assertion would pass for
		// the wrong reason (or not at all).
		if _, err := db.DB.Exec(fmt.Sprintf(`
			INSERT INTO %s.loyalty_redemption_otp_challenges (id, customer_id, points, reference_id, initiated_by, otp_hash, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, schema),
			"TEST-LRS-CH-EXPIRED", custID, 10, "TEST-LRS-REF", "manager1", hashOTP("999999"), time.Now().Add(-25*time.Hour)); err != nil {
			t.Fatalf("seed expired challenge: %v", err)
		}
		if _, _, _, err := VerifyAndRedeemLoyaltyOTP(tenantID, "TEST-LRS-CH-EXPIRED", "999999"); err == nil {
			t.Fatalf("expected an expired challenge to be rejected")
		}
	})

	// BLD-020 (2026-09-22): the wide 25-hour margin above proves rejection
	// but can't tell an SQL-side comparison from an unrepaired ~5.5-hour Go
	// vs Postgres skew - the margin swallows exactly the discrepancy the
	// comment above describes. This pair proves the fix at a real boundary
	// (seconds, not hours, but wide enough not to flake on process/network
	// scheduling jitter between the seed and the verify call): a challenge
	// that expired 10 seconds ago is rejected, and one expiring 10 seconds
	// from now still succeeds - both fixtures computed by Postgres's own
	// NOW() (interval arithmetic in the INSERT itself), never by Go's
	// time.Now(), so the fixture itself carries none of the skew being
	// tested for.
	t.Run("expiry boundary is evaluated in SQL, not skewed by app-server-vs-database clock", func(t *testing.T) {
		cleanup()
		seedEarn(1000)
		if _, err := db.DB.Exec(fmt.Sprintf(`
			INSERT INTO %s.loyalty_redemption_otp_challenges (id, customer_id, points, reference_id, initiated_by, otp_hash, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW() - INTERVAL '10 seconds')`, schema),
			"TEST-LRS-CH-BOUND-PAST", custID, 10, "TEST-LRS-REF", "manager1", hashOTP("111111")); err != nil {
			t.Fatalf("seed just-expired challenge: %v", err)
		}
		if _, _, _, err := VerifyAndRedeemLoyaltyOTP(tenantID, "TEST-LRS-CH-BOUND-PAST", "111111"); err == nil {
			t.Fatalf("counterexample: a challenge that expired 10 seconds ago (per Postgres's own clock) was accepted - the app-server-vs-database timezone skew this fix closes would show up here")
		}

		if _, err := db.DB.Exec(fmt.Sprintf(`
			INSERT INTO %s.loyalty_redemption_otp_challenges (id, customer_id, points, reference_id, initiated_by, otp_hash, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW() + INTERVAL '10 seconds')`, schema),
			"TEST-LRS-CH-BOUND-FUTURE", custID, 10, "TEST-LRS-REF", "manager1", hashOTP("222222")); err != nil {
			t.Fatalf("seed not-yet-expired challenge: %v", err)
		}
		if _, _, _, err := VerifyAndRedeemLoyaltyOTP(tenantID, "TEST-LRS-CH-BOUND-FUTURE", "222222"); err != nil {
			t.Fatalf("counterexample: a challenge expiring 10 seconds from now (per Postgres's own clock) was rejected as already expired: %v", err)
		}
	})
}
