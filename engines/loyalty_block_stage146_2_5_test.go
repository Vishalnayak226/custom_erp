package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 146.2.5 - a blocked member neither earns nor redeems (the balance is
// kept, the reason and who set it are on the customer); customer tags listed in
// loyalty.never_earn_tags never earn; an offer can target a customer tag.
func TestStage14625BlockedMemberAndNeverEarnTags(t *testing.T) {
	tenantID, schema, insert := stage1462Fixture(t)
	if err := SetSetting(tenantID, "loyalty.rupees_per_point", "100", "tester"); err != nil {
		t.Fatal(err)
	}
	code := func(err error) string {
		if v, ok := err.(*ValidationError); ok {
			return v.Code
		}
		return ""
	}
	insert("T146-MEMBER", "Customer", "Active", map[string]interface{}{"code": "T146-MEMBER", "name": "Member"})
	insert("T146-STAFF", "Customer", "Active", map[string]interface{}{"code": "T146-STAFF", "name": "Staff member", "tags": "Staff, Local"})
	insert("T146-VIP", "Customer", "Active", map[string]interface{}{"code": "T146-VIP", "name": "Tagged", "tags": "VIP"})

	// The member earns, then is blocked: no more earn, no redeem, balance kept.
	if err := EarnLoyaltyPoints(tenantID, "T146-MEMBER", 1000, "T146-B1"); err != nil {
		t.Fatal(err)
	}
	if bal, _ := GetLoyaltyBalance(tenantID, "T146-MEMBER"); bal != 10 {
		t.Fatalf("balance before the block: %d, want 10", bal)
	}
	payload := map[string]interface{}{"code": "T146-MEMBER", "name": "Member", "loyalty_blocked": "Yes"}
	if c := code(ValidateMasterDataRules(tenantID, "T146-MEMBER", "Customer", payload)); c != "CUSTOM-0692" {
		t.Errorf("blocking without a reason: code %q, want CUSTOM-0692", c)
	}
	payload["loyalty_block_reason"] = "Points abuse"
	if err := ValidateMasterDataRules(tenantID, "T146-MEMBER", "Customer", payload); err != nil {
		t.Errorf("blocking with a reason: %v", err)
	}
	StampLoyaltyBlock(tenantID, "T146-MEMBER", "manager1", "Customer", payload)
	if payload["loyalty_blocked_by"] != "manager1" || payload["loyalty_blocked_at"] == nil || payload["loyalty_blocked_at"] == "" {
		t.Errorf("who and when are stamped on the customer: %v", payload)
	}
	if _, err := db.DB.Exec(`UPDATE ` + schema + `.documents SET data = data || '{"loyalty_blocked":"Yes","loyalty_block_reason":"Points abuse"}'::jsonb WHERE id = 'T146-MEMBER'`); err != nil {
		t.Fatal(err)
	}
	if err := EarnLoyaltyPoints(tenantID, "T146-MEMBER", 5000, "T146-B2"); err != nil {
		t.Fatal(err)
	}
	if bal, _ := GetLoyaltyBalance(tenantID, "T146-MEMBER"); bal != 10 {
		t.Errorf("a blocked member earns nothing: balance %d, want the 10 kept", bal)
	}
	if _, err := RedeemLoyaltyPoints(tenantID, "T146-MEMBER", 5, "T146-B3"); code(err) != "CUSTOM-0691" {
		t.Errorf("a blocked member cannot redeem: %v, want CUSTOM-0691", err)
	}
	if bal, _ := GetLoyaltyBalance(tenantID, "T146-MEMBER"); bal != 10 {
		t.Errorf("the refused redeem burned nothing: %d", bal)
	}
	p, err := PlanLoyaltyEarn(tenantID, LoyaltyEarnInput{CustomerID: "T146-MEMBER", Net: 5000})
	if err != nil || p.Total != 0 || p.Blocked != "Points abuse" {
		t.Errorf("the till preview of a blocked member: %v %+v, want 0 and the reason", err, p)
	}
	if _, err := db.DB.Exec(`UPDATE ` + schema + `.documents SET data = data || '{"loyalty_blocked":"No"}'::jsonb WHERE id = 'T146-MEMBER'`); err != nil {
		t.Fatal(err)
	}
	if err := EarnLoyaltyPoints(tenantID, "T146-MEMBER", 1000, "T146-B4"); err != nil {
		t.Fatal(err)
	}
	if bal, _ := GetLoyaltyBalance(tenantID, "T146-MEMBER"); bal != 20 {
		t.Errorf("unblocked, the member earns again: %d, want 20", bal)
	}

	// Tags that never earn: empty by default, then "staff" (any case).
	if err := EarnLoyaltyPoints(tenantID, "T146-STAFF", 1000, "T146-S1"); err != nil {
		t.Fatal(err)
	}
	if bal, _ := GetLoyaltyBalance(tenantID, "T146-STAFF"); bal != 10 {
		t.Fatalf("with no never-earn tags a tagged customer earns: %d, want 10", bal)
	}
	if err := SetSetting(tenantID, "loyalty.never_earn_tags", "staff, Intern", "tester"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"T146-STAFF", "T146-VIP"} {
		if err := EarnLoyaltyPoints(tenantID, id, 1000, "T146-S2-"+id); err != nil {
			t.Fatal(err)
		}
	}
	if bal, _ := GetLoyaltyBalance(tenantID, "T146-STAFF"); bal != 10 {
		t.Errorf("a Staff-tagged customer earns nothing more: %d, want 10", bal)
	}
	if bal, _ := GetLoyaltyBalance(tenantID, "T146-VIP"); bal != 10 {
		t.Errorf("an untagged-for-this customer still earns: %d, want 10", bal)
	}
	if _, err := RedeemLoyaltyPoints(tenantID, "T146-STAFF", 5, "T146-S3"); err != nil {
		t.Errorf("a never-earn tag does not stop redeeming the balance they have: %v", err)
	}

	// Offers by customer tag.
	insert("T146-OFF", "Offer", "Active", map[string]interface{}{"name": "Staff 10", "offer_type": "Percentage Off", "scope": "Bill", "discount_pct": 10, "customer_tag": "staff", "priority": 1, "stackable": "Yes"})
	lines := []OfferCartLine{{Sku: "X", Qty: 1, SalePrice: 1000}}
	ev := func(customer string) float64 {
		t.Helper()
		e, err := EvaluatePOSOffers(tenantID, OfferEvaluationInput{Lines: append([]OfferCartLine(nil), lines...), CustomerID: customer})
		if err != nil {
			t.Fatal(err)
		}
		return e.TotalDiscount
	}
	if got := ev("T146-STAFF"); got != 100 {
		t.Errorf("a Staff-tagged customer gets the Staff offer: %.2f, want 100", got)
	}
	if got := ev("T146-VIP"); got != 0 {
		t.Errorf("another customer does not: %.2f, want 0", got)
	}
	if got := ev(""); got != 0 {
		t.Errorf("an anonymous bill does not: %.2f, want 0", got)
	}
}
