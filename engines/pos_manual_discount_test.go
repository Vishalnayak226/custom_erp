package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 85.E7: the till's bill discount comes off the price - shared across
// the lines like a voucher, so the net and the GST follow - as a % or an
// amount, never more than the bill.
func TestPOSManualDiscountInTheQuote(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	ids := []string{"S85H-ITEM-A", "S85H-ITEM-B"}
	cleanup := func() {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	cleanup()
	defer cleanup()
	for i, id := range ids {
		raw, _ := json.Marshal(map[string]interface{}{"code": id, "name": id, "sale_price": float64(1000 * (i + 1)), "mrp": float64(1000 * (i + 1)), "hsn_code": "6205", "gst_rate": 12, "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", id, raw); err != nil {
			t.Fatal(err)
		}
	}
	quote := func(pct, amount float64) *QuoteResult {
		q, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: "HO", Lines: []QuoteLineRequest{{Sku: ids[0], Qty: 1}, {Sku: ids[1], Qty: 1}},
			ManualDiscountPct: pct, ManualDiscountAmount: amount})
		if err != nil {
			t.Fatalf("quote: %v", err)
		}
		return q
	}
	base := quote(0, 0)
	if base.Total != 3000 {
		t.Fatalf("base total %v, want 3000", base.Total)
	}
	ten := quote(10, 0)
	if ten.Total != 2700 || ManualDiscountShare(ten) != 10 {
		t.Errorf("10%%: total %v share %v", ten.Total, ManualDiscountShare(ten))
	}
	if ten.Lines[0].NetTotal != 900 || ten.Lines[1].NetTotal != 1800 {
		t.Errorf("10%% shared by value: nets %v / %v", ten.Lines[0].NetTotal, ten.Lines[1].NetTotal)
	}
	if ten.GSTBreakdown.TotalTax >= base.GSTBreakdown.TotalTax {
		t.Errorf("tax should follow the discounted price: %v vs %v", ten.GSTBreakdown.TotalTax, base.GSTBreakdown.TotalTax)
	}
	amt := quote(0, 300)
	if amt.Total != 2700 {
		t.Errorf("Rs 300 off: total %v", amt.Total)
	}
	if over := quote(0, 5000); over.Total != 0 {
		t.Errorf("a discount larger than the bill takes it to 0, not below: %v", over.Total)
	}
}

// Ask 215.5: item, offer and bill discounts are kept apart. A price override
// is the item discount, the cashier's bill discount is stored per line as
// bill_share, and the reports split the three - also on bills made before
// bill_share existed, from the Manual offer's total.
func TestPOSThreeDiscountsKeptApart(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	ids := []string{"T2155-ITEM-A", "T2155-ITEM-B"}
	cart, ovID := "T2155-CART", "T2155-OVERRIDE"
	cleanup := func() {
		for _, id := range append(ids, ovID) {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	cleanup()
	defer cleanup()
	for i, id := range ids {
		raw, _ := json.Marshal(map[string]interface{}{"code": id, "name": id, "sale_price": float64(1000 * (i + 1)), "mrp": float64(1000 * (i + 1)), "hsn_code": "6205", "gst_rate": 12, "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", id, raw); err != nil {
			t.Fatal(err)
		}
	}
	// Line A is overridden from 1000 to 900: Rs 100 of item discount.
	ov, _ := json.Marshal(map[string]interface{}{"cart_number": cart, "sku": ids[0], "override_price": "900", "reason": "test"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSPriceOverride', $2, 'Approved', 'system')", ovID, ov); err != nil {
		t.Fatal(err)
	}
	q, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: "HO", CartNumber: cart,
		Lines: []QuoteLineRequest{{Sku: ids[0], Qty: 1}, {Sku: ids[1], Qty: 1}}, ManualDiscountPct: 10})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	// 900 + 2000 = 2900; 10% bill discount = 290, shared 90 / 200.
	if q.ItemDiscount != 100 || q.BillDiscount != 290 || q.Total != 2610 {
		t.Fatalf("item %v bill %v total %v, want 100 / 290 / 2610", q.ItemDiscount, q.BillDiscount, q.Total)
	}
	// The approval gate sees both cuts together: (100 + 290) / 3000 = 13%,
	// more than either part (3.33% off list, 10% off the bill).
	if p := ManualDiscountTotalPct(q); p != 13 {
		t.Errorf("combined manual discount %v%%, want 13%%", p)
	}
	if q.Lines[0].BillShare != 90 || q.Lines[1].BillShare != 200 {
		t.Errorf("bill share per line %v / %v, want 90 / 200", q.Lines[0].BillShare, q.Lines[1].BillShare)
	}
	items := POSCartItemsFromQuote(q)
	if catNum(items[0]["bill_share"]) != 90 || catNum(items[1]["bill_share"]) != 200 {
		t.Errorf("stored items lack bill_share: %v / %v", items[0]["bill_share"], items[1]["bill_share"])
	}

	// The reports, on the bill as stored (a JSON round trip, as the database does).
	raw, _ := json.Marshal(map[string]interface{}{"items": items, "applied_offers": q.AppliedOffers, "item_discount": q.ItemDiscount, "bill_discount": q.BillDiscount, "amount_paid": q.Total})
	var bill map[string]interface{}
	if err := json.Unmarshal(raw, &bill); err != nil {
		t.Fatal(err)
	}
	row := func(h map[string]interface{}, n int) *catRow {
		return &catRow{H: h, L: catLineList(h["items"])[n], N: n + 1, src: posLineShape}
	}
	if b := posLineBillShare(row(bill, 0)); b != 90 {
		t.Errorf("line A bill share %v, want 90", b)
	}
	// The line columns are taxable values: back to GST-inclusive at the line's rate.
	m := posLine(row(bill, 0))
	div := 1 + m.rate/100
	if !near(m.itemDisc*div, 100) || !near(m.billDisc*div, 90) || !near(m.offerDisc, 0) {
		t.Errorf("line A item %v bill %v offer %v, want 100 / 90 / 0 with GST", m.itemDisc*div, m.billDisc*div, m.offerDisc*div)
	}
	if p := posBillDiscountParts(&catRow{H: bill}); p != [3]float64{100, 0, 290} {
		t.Errorf("bill parts %v, want item 100, offer 0, bill 290", p)
	}
	if n := len(posPromoOffers(bill)); n != 0 {
		t.Errorf("the bill discount is not a promotion: %d offer-use rows", n)
	}

	// A bill from before 215.5: no bill_share or bill_discount. Line A also had
	// a Rs 100 promotion, so its offer_share is 190 (100 offer + 90 bill).
	old := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"sku": "X", "qty": 1.0, "sale_price": 1000.0, "reference_price": 1000.0, "gst_rate": 12.0, "offer_share": 190.0, "net_total": 810.0},
			map[string]interface{}{"sku": "Y", "qty": 1.0, "sale_price": 2000.0, "reference_price": 2000.0, "gst_rate": 12.0, "offer_share": 200.0, "net_total": 1800.0},
		},
		"applied_offers": []interface{}{
			map[string]interface{}{"offer_id": "P1", "name": "Promo", "offer_type": "Flat", "discount": 100.0},
			map[string]interface{}{"offer_id": "MANUAL", "name": "10% bill discount", "offer_type": OfferTypeManual, "discount": 290.0},
		},
	}
	if a, b := posLineBillShare(row(old, 0)), posLineBillShare(row(old, 1)); a != 90 || b != 200 {
		t.Errorf("old bill: bill share %v / %v, want 90 / 200", a, b)
	}
	if p := posBillDiscountParts(&catRow{H: old}); p != [3]float64{0, 100, 290} {
		t.Errorf("old bill parts %v, want item 0, offer 100, bill 290", p)
	}
	if n := len(posPromoOffers(old)); n != 1 {
		t.Errorf("old bill: %d promotions, want 1 (the bill discount left out)", n)
	}
}
