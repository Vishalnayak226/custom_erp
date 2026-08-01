package engines

import (
	"custom_erp/db"
	"testing"
)

// TestLoyaltyRedemptionDeferralAndReversal covers Stage 30.2.5. The defect:
// clicking "Redeem Points" burned the points immediately, independent of
// whether the sale ever completed, and the discount was never applied to the
// cart - the UI told the cashier to type it into a line's price by hand. A
// customer could lose points on an abandoned sale with no way back.
func TestLoyaltyRedemptionDeferralAndReversal(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const customer = "TEST-LOYALTY-DEFER-CUST"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".loyalty_point_ledger WHERE customer_id = $1", customer)
	}
	cleanup()
	defer cleanup()

	if err := insertLoyaltyLedgerEntry(tenantID, customer, "Earn", 500, "POSCart", "TEST-SEED"); err != nil {
		t.Fatalf("seed earn: %v", err)
	}

	balance := func() int {
		b, err := GetLoyaltyBalance(tenantID, customer)
		if err != nil {
			t.Fatalf("balance: %v", err)
		}
		return b
	}
	if balance() != 500 {
		t.Fatalf("seed balance is %d, want 500", balance())
	}

	t.Run("valuing a redemption never touches the ledger", func(t *testing.T) {
		// This is what the POS screen now calls when the cashier adds points
		// to a cart. Before 30.2.5 the equivalent action burned them.
		if v := LoyaltyRedemptionValue(tenantID, 100); v != 100*redemptionValuePerPointFor(tenantID) {
			t.Fatalf("value = %d, want 100 at the tenant's configured rate", v)
		}
		if balance() != 500 {
			t.Fatalf("balance changed to %d just from pricing a redemption", balance())
		}
	})

	t.Run("a redemption reduces the balance when the sale goes through", func(t *testing.T) {
		discount, err := RedeemLoyaltyPoints(tenantID, customer, 120, "TEST-CART-DEFER-1")
		if err != nil {
			t.Fatalf("redeem: %v", err)
		}
		if discount != 120 {
			t.Fatalf("discount = %d, want 120", discount)
		}
		if balance() != 380 {
			t.Fatalf("balance = %d, want 380", balance())
		}
	})

	t.Run("reversing a failed sale gives the points back", func(t *testing.T) {
		if err := ReverseLoyaltyRedemption(tenantID, customer, 120, "TEST-CART-DEFER-1"); err != nil {
			t.Fatalf("reverse: %v", err)
		}
		if balance() != 500 {
			t.Fatalf("balance after reversal = %d, want the original 500", balance())
		}

		// Append-only: the reversal is a compensating entry, and both it and
		// the original burn stay in the customer's visible history.
		ledger, err := GetLoyaltyLedger(tenantID, customer)
		if err != nil {
			t.Fatalf("ledger: %v", err)
		}
		var burns, reversals int
		for _, e := range ledger {
			if e.TransactionType == "Burn" {
				burns++
			}
			if e.ReferenceID == "TEST-CART-DEFER-1:REDEMPTION-REVERSAL" {
				reversals++
			}
		}
		if burns != 1 || reversals != 1 {
			t.Fatalf("expected the burn and its reversal both on record, got %d burn(s)/%d reversal(s)", burns, reversals)
		}
	})

	t.Run("reversing zero or an unknown customer is a no-op, not an error", func(t *testing.T) {
		if err := ReverseLoyaltyRedemption(tenantID, customer, 0, "X"); err != nil {
			t.Fatalf("zero-point reversal errored: %v", err)
		}
		if err := ReverseLoyaltyRedemption(tenantID, "", 50, "X"); err != nil {
			t.Fatalf("empty-customer reversal errored: %v", err)
		}
		if balance() != 500 {
			t.Fatalf("no-op reversals changed the balance to %d", balance())
		}
	})

	t.Run("more points than the balance is refused before anything is burned", func(t *testing.T) {
		_, err := RedeemLoyaltyPoints(tenantID, customer, 9999, "TEST-CART-DEFER-2")
		ve, ok := err.(*ValidationError)
		if !ok || ve.Code != "CUSTOM-0134" {
			t.Fatalf("expected CUSTOM-0134, got %#v", err)
		}
		if balance() != 500 {
			t.Fatalf("a refused redemption still moved the balance to %d", balance())
		}
	})
}

// TestSalesBookingSplitsLoyaltyTender checks the GL side of Stage 30.2.5: a
// sale paid partly with points still credits revenue in full, and the points
// portion is debited to 5250 instead of overstating cash collected.
func TestSalesBookingSplitsLoyaltyTender(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const cart = "TEST-LOYALTY-GL-CART"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_type = 'POSCart' AND document_id = $1", cart)
	}
	cleanup()
	defer cleanup()

	if err := PostSalesFinanceBooking(tenantID, cart, 1000, 600, "Cash", 150); err != nil {
		t.Fatalf("PostSalesFinanceBooking: %v", err)
	}

	amountOn := func(account string) int {
		var debit, credit int
		if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_id = $1 AND account_code = $2", cart, account).Scan(&debit, &credit); err != nil {
			t.Fatalf("query %s: %v", account, err)
		}
		return debit - credit
	}

	if got := amountOn("4100"); got != -1000 {
		t.Fatalf("revenue credited %d, want the full sale value (1000)", -got)
	}
	if got := amountOn("1100"); got != 850 {
		t.Fatalf("cash debited %d, want 850 (1000 less 150 paid in points)", got)
	}
	if got := amountOn("5250"); got != 150 {
		t.Fatalf("loyalty account debited %d, want 150", got)
	}

	t.Run("a redemption larger than the sale is refused rather than posted", func(t *testing.T) {
		if err := PostSalesFinanceBooking(tenantID, cart+"-OVER", 500, 300, "Cash", 900); err == nil {
			t.Fatalf("expected a rejection when points exceed the sale value")
		}
		db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", cart+"-OVER")
	})

	t.Run("a sale with no redemption posts exactly as it always did", func(t *testing.T) {
		const plain = "TEST-LOYALTY-GL-PLAIN"
		defer db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", plain)
		if err := PostSalesFinanceBooking(tenantID, plain, 1000, 600, "Cash", 0); err != nil {
			t.Fatalf("PostSalesFinanceBooking: %v", err)
		}
		var loyaltyRows int
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".gl_postings WHERE document_id = $1 AND account_code = '5250'", plain).Scan(&loyaltyRows); err != nil {
			t.Fatalf("query: %v", err)
		}
		if loyaltyRows != 0 {
			t.Fatalf("a sale with no redemption touched the loyalty account %d time(s)", loyaltyRows)
		}
	})
}
