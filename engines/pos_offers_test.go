package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

// TestPOSOffers (Stage 30.7) covers the offer evaluator across all four offer
// families plus the conditions that gate them: thresholds, coupon codes,
// customer tiers, validity windows, caps and stacking. Cleans the shared dev
// DB before and after (the documented shared-DB test-pollution gotcha) so it
// is idempotent across reruns.
func TestPOSOffers(t *testing.T) {
	connStr := testConnStr()
	db.InitDB(connStr)

	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("get schema: %v", err)
	}

	const idPrefix = "OFFERTEST-"

	cleanup := func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id LIKE $1`, schema), idPrefix+"%")
	}
	cleanup()
	defer cleanup()

	// seedOffer writes one Active Offer document.
	seedOffer := func(t *testing.T, id string, fields map[string]interface{}) {
		t.Helper()
		body, err := json.Marshal(fields)
		if err != nil {
			t.Fatalf("marshal offer: %v", err)
		}
		if _, err := db.DB.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Offer', $2, 'Active', 'system')`, schema),
			idPrefix+id, string(body)); err != nil {
			t.Fatalf("seed offer %s: %v", id, err)
		}
	}
	clearOffers := func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id LIKE $1 AND doctype = 'Offer'`, schema), idPrefix+"%")
	}

	// A plain 2-line cart: 2 x 500 + 1 x 200 = 1200.
	baseCart := func() []OfferCartLine {
		return []OfferCartLine{
			{Sku: "SKU-A", Qty: 2, SalePrice: 500},
			{Sku: "SKU-B", Qty: 1, SalePrice: 200},
		}
	}

	evaluate := func(t *testing.T, lines []OfferCartLine, customerID string, codes []string) *OfferEvaluation {
		t.Helper()
		eval, err := EvaluatePOSOffers(tenantID, OfferEvaluationInput{
			Lines: lines, CustomerID: customerID, CouponCodes: codes,
		})
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		return eval
	}

	t.Run("no offers configured leaves the bill untouched", func(t *testing.T) {
		clearOffers()
		eval := evaluate(t, baseCart(), "", nil)
		if eval.TotalDiscount != 0 || eval.NetAmount != 1200 {
			t.Fatalf("expected no discount on a 1200 bill, got discount=%v net=%v", eval.TotalDiscount, eval.NetAmount)
		}
	})

	t.Run("percentage off the whole bill", func(t *testing.T) {
		clearOffers()
		seedOffer(t, "PCT", map[string]interface{}{
			"name": "10% off", "offer_type": "Percentage Off", "scope": "Bill",
			"discount_pct": 10, "stackable": "No",
		})
		eval := evaluate(t, baseCart(), "", nil)
		if eval.TotalDiscount != 120 {
			t.Fatalf("expected 120 off a 1200 bill, got %v", eval.TotalDiscount)
		}
		if eval.NetAmount != 1080 {
			t.Fatalf("expected net 1080, got %v", eval.NetAmount)
		}
	})

	t.Run("flat off is capped at the scoped amount", func(t *testing.T) {
		clearOffers()
		// 5000 off a 1200 bill must not produce a negative bill.
		seedOffer(t, "FLAT", map[string]interface{}{
			"name": "Huge flat", "offer_type": "Flat Off", "scope": "Bill",
			"discount_amount": 5000, "stackable": "No",
		})
		eval := evaluate(t, baseCart(), "", nil)
		if eval.TotalDiscount != 1200 || eval.NetAmount != 0 {
			t.Fatalf("expected the bill floored at 0, got discount=%v net=%v", eval.TotalDiscount, eval.NetAmount)
		}
	})

	t.Run("item-scoped percentage only touches its own line", func(t *testing.T) {
		clearOffers()
		seedOffer(t, "ITEM", map[string]interface{}{
			"name": "SKU-B half off", "offer_type": "Percentage Off", "scope": "Item",
			"scope_value": "SKU-B", "discount_pct": 50, "stackable": "No",
		})
		eval := evaluate(t, baseCart(), "", nil)
		// 50% of SKU-B's 200 line = 100, NOT 50% of the 1200 bill.
		if eval.TotalDiscount != 100 {
			t.Fatalf("expected 100 (half of the 200 line), got %v", eval.TotalDiscount)
		}
	})

	t.Run("minimum bill threshold gates the offer", func(t *testing.T) {
		clearOffers()
		seedOffer(t, "THRESH", map[string]interface{}{
			"name": "Spend 2000 get 10%", "offer_type": "Percentage Off", "scope": "Bill",
			"discount_pct": 10, "min_bill_amount": 2000, "stackable": "No",
		})
		if eval := evaluate(t, baseCart(), "", nil); eval.TotalDiscount != 0 {
			t.Fatalf("1200 bill is under the 2000 threshold, expected no discount, got %v", eval.TotalDiscount)
		}
		// Push the same cart over the threshold.
		bigCart := []OfferCartLine{{Sku: "SKU-A", Qty: 5, SalePrice: 500}} // 2500
		if eval := evaluate(t, bigCart, "", nil); eval.TotalDiscount != 250 {
			t.Fatalf("2500 bill should get 250 off, got %v", eval.TotalDiscount)
		}
	})

	t.Run("buy 2 get 1 frees the cheapest qualifying unit", func(t *testing.T) {
		clearOffers()
		seedOffer(t, "BOGO", map[string]interface{}{
			"name": "Buy 2 get 1", "offer_type": "Buy X Get Y", "scope": "Bill",
			"buy_qty": 2, "get_qty": 1, "stackable": "No",
		})
		// 3 units total: 500, 500, 200 -> one group of 3, cheapest (200) free.
		eval := evaluate(t, baseCart(), "", nil)
		if eval.TotalDiscount != 200 {
			t.Fatalf("expected the cheapest unit (200) free, got %v", eval.TotalDiscount)
		}
	})

	t.Run("buy 2 get 1 needs a complete group", func(t *testing.T) {
		clearOffers()
		seedOffer(t, "BOGO2", map[string]interface{}{
			"name": "Buy 2 get 1", "offer_type": "Buy X Get Y", "scope": "Bill",
			"buy_qty": 2, "get_qty": 1, "stackable": "No",
		})
		twoUnits := []OfferCartLine{{Sku: "SKU-A", Qty: 2, SalePrice: 500}}
		if eval := evaluate(t, twoUnits, "", nil); eval.TotalDiscount != 0 {
			t.Fatalf("2 units can't complete a buy-2-get-1 group, got %v", eval.TotalDiscount)
		}
	})

	t.Run("bundle price discounts the most expensive complete group", func(t *testing.T) {
		clearOffers()
		seedOffer(t, "BUNDLE", map[string]interface{}{
			"name": "Any 3 for 1000", "offer_type": "Bundle Price", "scope": "Bill",
			"bundle_qty": 3, "bundle_price": 1000, "stackable": "No",
		})
		// Units 500+500+200 = 1200 normally, bundled at 1000 -> 200 off.
		eval := evaluate(t, baseCart(), "", nil)
		if eval.TotalDiscount != 200 {
			t.Fatalf("expected 200 off from the bundle, got %v", eval.TotalDiscount)
		}
	})

	t.Run("maximum discount cap is enforced", func(t *testing.T) {
		clearOffers()
		seedOffer(t, "CAP", map[string]interface{}{
			"name": "50% off capped at 100", "offer_type": "Percentage Off", "scope": "Bill",
			"discount_pct": 50, "max_discount_amount": 100, "stackable": "No",
		})
		eval := evaluate(t, baseCart(), "", nil)
		if eval.TotalDiscount != 100 {
			t.Fatalf("50%% of 1200 should be capped at 100, got %v", eval.TotalDiscount)
		}
	})

	t.Run("coupon offers only apply when the code is supplied", func(t *testing.T) {
		clearOffers()
		seedOffer(t, "COUPON", map[string]interface{}{
			"name": "SAVE10", "offer_type": "Percentage Off", "scope": "Bill",
			"discount_pct": 10, "coupon_code": "SAVE10", "stackable": "No",
		})
		if eval := evaluate(t, baseCart(), "", nil); eval.TotalDiscount != 0 {
			t.Fatalf("coupon offer must not auto-apply, got %v", eval.TotalDiscount)
		}
		// Case-insensitive, so a cashier typing lowercase still works.
		eval := evaluate(t, baseCart(), "", []string{"save10"})
		if eval.TotalDiscount != 120 {
			t.Fatalf("expected the coupon to apply for 120, got %v", eval.TotalDiscount)
		}
		if len(eval.UnmatchedCodes) != 0 {
			t.Fatalf("expected no unmatched codes, got %v", eval.UnmatchedCodes)
		}
	})

	t.Run("an unknown coupon code is reported back", func(t *testing.T) {
		clearOffers()
		eval := evaluate(t, baseCart(), "", []string{"NOPE"})
		if len(eval.UnmatchedCodes) != 1 || eval.UnmatchedCodes[0] != "NOPE" {
			t.Fatalf("expected NOPE reported as unmatched, got %v", eval.UnmatchedCodes)
		}
	})

	t.Run("expired offers do not apply", func(t *testing.T) {
		clearOffers()
		seedOffer(t, "EXPIRED", map[string]interface{}{
			"name": "Last year", "offer_type": "Percentage Off", "scope": "Bill",
			"discount_pct": 25, "valid_to": "2000-01-01", "stackable": "No",
		})
		if eval := evaluate(t, baseCart(), "", nil); eval.TotalDiscount != 0 {
			t.Fatalf("an offer valid only until 2000 must not apply, got %v", eval.TotalDiscount)
		}
	})

	t.Run("tier-restricted offers skip a customerless sale", func(t *testing.T) {
		clearOffers()
		seedOffer(t, "TIER", map[string]interface{}{
			"name": "Gold only", "offer_type": "Percentage Off", "scope": "Bill",
			"discount_pct": 20, "customer_tier": "Gold", "stackable": "No",
		})
		if eval := evaluate(t, baseCart(), "", nil); eval.TotalDiscount != 0 {
			t.Fatalf("a walk-in with no customer has no tier, expected no discount, got %v", eval.TotalDiscount)
		}
	})

	t.Run("stackable offers combine, a non-stackable one stops the stack", func(t *testing.T) {
		clearOffers()
		// priority 1 stackable 10%, then priority 2 stackable flat 50.
		seedOffer(t, "S1", map[string]interface{}{
			"name": "Stack A", "offer_type": "Percentage Off", "scope": "Bill",
			"discount_pct": 10, "priority": 1, "stackable": "Yes",
		})
		seedOffer(t, "S2", map[string]interface{}{
			"name": "Stack B", "offer_type": "Flat Off", "scope": "Bill",
			"discount_amount": 50, "priority": 2, "stackable": "Yes",
		})
		eval := evaluate(t, baseCart(), "", nil)
		if len(eval.Applied) != 2 {
			t.Fatalf("expected both stackable offers to apply, got %d", len(eval.Applied))
		}
		if eval.TotalDiscount != 170 { // 120 + 50
			t.Fatalf("expected 170 combined, got %v", eval.TotalDiscount)
		}

		// Now make the first one non-stackable: it should win alone.
		clearOffers()
		seedOffer(t, "N1", map[string]interface{}{
			"name": "Solo", "offer_type": "Percentage Off", "scope": "Bill",
			"discount_pct": 10, "priority": 1, "stackable": "No",
		})
		seedOffer(t, "N2", map[string]interface{}{
			"name": "Never reached", "offer_type": "Flat Off", "scope": "Bill",
			"discount_amount": 50, "priority": 2, "stackable": "Yes",
		})
		eval = evaluate(t, baseCart(), "", nil)
		if len(eval.Applied) != 1 || eval.TotalDiscount != 120 {
			t.Fatalf("a non-stackable offer must end the stack, got %d offers / %v", len(eval.Applied), eval.TotalDiscount)
		}
	})

	t.Run("stacked offers never drive the bill below zero", func(t *testing.T) {
		clearOffers()
		seedOffer(t, "Z1", map[string]interface{}{
			"name": "Big A", "offer_type": "Flat Off", "scope": "Bill",
			"discount_amount": 1000, "priority": 1, "stackable": "Yes",
		})
		seedOffer(t, "Z2", map[string]interface{}{
			"name": "Big B", "offer_type": "Flat Off", "scope": "Bill",
			"discount_amount": 1000, "priority": 2, "stackable": "Yes",
		})
		eval := evaluate(t, baseCart(), "", nil)
		if eval.TotalDiscount != 1200 || eval.NetAmount != 0 {
			t.Fatalf("expected the stack floored at the 1200 bill, got discount=%v net=%v", eval.TotalDiscount, eval.NetAmount)
		}
	})

	t.Run("inactive offers are ignored", func(t *testing.T) {
		clearOffers()
		body, _ := json.Marshal(map[string]interface{}{
			"name": "Switched off", "offer_type": "Percentage Off", "scope": "Bill", "discount_pct": 30,
		})
		if _, err := db.DB.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Offer', $2, 'Inactive', 'system')`, schema),
			idPrefix+"INACTIVE", string(body)); err != nil {
			t.Fatalf("seed inactive offer: %v", err)
		}
		if eval := evaluate(t, baseCart(), "", nil); eval.TotalDiscount != 0 {
			t.Fatalf("an Inactive offer must never apply, got %v", eval.TotalDiscount)
		}
	})
}
