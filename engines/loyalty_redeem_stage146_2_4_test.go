package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// Stage 146.2.4 - a point worth less than Rs 1. loyalty.redemption_paise_per_point
// is read by loyaltyPointValuePaise; points stay whole, and the quote, the
// checkout and the liability report all convert through it.
func TestStage14624PointWorthLessThanARupee(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	// Default: nothing changes (1 point = Rs 1 = 100 paise).
	if v := loyaltyPointValuePaise(tenantID); v != 100 {
		t.Fatalf("default point value = %d paise, want 100", v)
	}
	if err := SetSetting(tenantID, "loyalty.redemption_paise_per_point", "25", "tester"); err != nil {
		t.Fatalf("set paise per point: %v", err)
	}
	if v := loyaltyPointValuePaise(tenantID); v != 25 {
		t.Fatalf("point value = %d paise, want 25", v)
	}
	if got := LoyaltyRedemptionValuePaise(tenantID, 10); got != 250 {
		t.Errorf("10 points = %d paise, want 250", got)
	}
	if got := loyaltyPaiseToPoints(tenantID, 999); got != 39 {
		t.Errorf("999 paise = %d points, want 39 (whole points, rounded down)", got)
	}
	raw, _ := json.Marshal(map[string]interface{}{"code": "QP", "name": "QP", "sale_price": 100.0, "hsn_code": "6912", "tax_treatment": "Exempt"})
	if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ('QP', 'Item', $1, 'Active', 'system')", raw); err != nil {
		t.Fatal(err)
	}
	if _, err := PostInventoryLedger(tenantID, "STORE-QP", []interface{}{map[string]interface{}{"sku": "QP", "qty": 5.0}}, false); err != nil {
		t.Fatal(err)
	}
	if err := insertLoyaltyLedgerEntryInSchema(schemaName, "CUST-QP", "Earn", 1000, "Seed", "SEED", nil); err != nil {
		t.Fatal(err)
	}
	q, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: "STORE-QP", CustomerID: "CUST-QP", RedeemPoints: 40,
		Lines: []QuoteLineRequest{{Sku: "QP", Qty: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	// Rs 100 bill at 25 paise a point: 400 points cover it; 40 points are Rs 10.
	if q.Loyalty.MaxPoints != 400 || q.Loyalty.Points != 40 || q.Loyalty.Value != 10.0 || q.Loyalty.ValuePaisePerPoint != 25 {
		t.Errorf("quote: max %d points %d value %v paise/pt %d, want 400 / 40 / 10 / 25",
			q.Loyalty.MaxPoints, q.Loyalty.Points, q.Loyalty.Value, q.Loyalty.ValuePaisePerPoint)
	}
	cartData, _ := json.Marshal(map[string]interface{}{"location": "STORE-QP", "payment_mode": "Cash", "customer_id": "CUST-QP", "redeem_points": 40,
		"items": POSCartItemsFromQuote(q)})
	if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ('CART-QP', 'POSCart', $1, 'Processing', 'system')", cartData); err != nil {
		t.Fatal(err)
	}
	out, err := FinalizePOSCheckoutCommitted(tenantID, "CART-QP", "", "", nil)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if out.LoyaltyDiscount != 10.0 {
		t.Errorf("discount %v, want 10", out.LoyaltyDiscount)
	}
	rep, err := GetPointsLiabilityReport(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if want := float64(rep.TotalOutstandingPoints) * 0.25; rep.TotalLiabilityValue != want {
		t.Errorf("liability %v, want %v (points x 0.25)", rep.TotalLiabilityValue, want)
	}
}
