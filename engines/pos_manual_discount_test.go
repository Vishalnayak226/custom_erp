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
