package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

func TestStage1487LineAndOrderDiscounts(t *testing.T) {
	f := newAgreementFixture(t)
	defer f.cleanup()
	const (
		i1, i2  = "TEST1487-I1", "TEST1487-I2"
		v, vAgr = "TEST1487-V", "TEST1487-VAGR"
	)
	f.item(i1, "TEST1487-D1", "TEST1487-C", 5)
	f.item(i2, "TEST1487-D2", "TEST1487-C", 5)
	f.insert(v, "Vendor", "Active", map[string]interface{}{"code": v, "name": v})
	f.insert(vAgr, "Vendor", "Active", map[string]interface{}{"code": vAgr, "name": vAgr})
	f.agreement("TEST1487-A1", vAgr, "2026-01-01", "",
		map[string]interface{}{"sku": i1, "price": 100, "discount_pct": 30, "buy_qty": 10, "get_qty": 2})

	apply := func(vendor string, extra map[string]interface{}, lines ...map[string]interface{}) map[string]interface{} {
		p := map[string]interface{}{"vendor": vendor, "order_date": "2026-11-03", "gst_mode": GSTModeExclusive, "items": itemsJSON(lines...)}
		for k, x := range extra {
			p[k] = x
		}
		if err := ApplyPurchaseOrderTotals("default", p); err != nil {
			t.Fatalf("ApplyPurchaseOrderTotals: %v", err)
		}
		return p
	}

	t.Run("a line discount in percent or rupees lowers the line value", func(t *testing.T) {
		p := apply(v, nil,
			map[string]interface{}{"sku": i1, "qty": 10, "rate": 100, "discount_pct": 10},
			map[string]interface{}{"sku": i2, "qty": 10, "rate": 100, "discount_amt": 50})
		// 1000 less 10% = 900; 1000 less Rs50 = 950; GST 5% on 1850 = 92.5 -> 1942.5 -> 1943
		if numFromInterface(p["total_amount"]) != 1850 || numFromInterface(p["grand_total"]) != 1943 {
			t.Fatalf("total %v grand %v, want 1850 / 1943", p["total_amount"], p["grand_total"])
		}
	})
	t.Run("two discounts compound and the line states it", func(t *testing.T) {
		p := apply(vAgr, nil, map[string]interface{}{"sku": i1, "qty": 10, "discount_pct": 20})
		ls := poLinesOf(t, p)
		// 1000 less 30% (agreement) less 20% = 560
		if numFromInterface(p["total_amount"]) != 560 {
			t.Fatalf("total %v, want 560", p["total_amount"])
		}
		if n, _ := ls[0]["discount_note"].(string); !strings.Contains(n, "44%") {
			t.Fatalf("discount note %q must state 44%%", n)
		}
	})
	t.Run("the order discount is spread over the lines", func(t *testing.T) {
		lines := []map[string]interface{}{
			{"sku": i1, "qty": 10, "rate": 100}, {"sku": i2, "qty": 5, "rate": 100}}
		p := apply(v, map[string]interface{}{"order_discount_pct": 10}, lines...)
		if numFromInterface(p["total_amount"]) != 1350 {
			t.Fatalf("total %v, want 1350", p["total_amount"])
		}
		p = apply(v, map[string]interface{}{"order_discount_amt": 100}, lines...)
		ls := poLinesOf(t, p)
		if numFromInterface(p["total_amount"]) != 1400 {
			t.Fatalf("total %v, want 1400", p["total_amount"])
		}
		// 1000/1500 of 100 = 66.67, the remainder on the last line: 33.33
		if numFromInterface(ls[0]["net_value"]) != 933.33 || numFromInterface(ls[1]["net_value"]) != 466.67 {
			t.Fatalf("net values %v / %v", ls[0]["net_value"], ls[1]["net_value"])
		}
	})
	t.Run("buy N get M is recalculated on the server for every quantity", func(t *testing.T) {
		for qty, want := range map[int]float64{9: 0, 10: 2, 25: 4, 30: 6} {
			p := apply(vAgr, nil, map[string]interface{}{"sku": i1, "qty": qty, "free_qty": 99})
			if got := numFromInterface(poLinesOf(t, p)[0]["free_qty"]); got != want {
				t.Fatalf("qty %d: free %v, want %v", qty, got, want)
			}
		}
	})
	t.Run("an invalid order discount is refused", func(t *testing.T) {
		p := map[string]interface{}{"vendor": v, "order_discount_pct": 120, "items": itemsJSON(map[string]interface{}{"sku": i1, "qty": 1, "rate": 10})}
		if err := ApplyPurchaseOrderTotals("default", p); err == nil || !strings.Contains(err.Error(), "discount") {
			t.Fatalf("want a discount error, got %v", err)
		}
	})
}

// A 3-way match on a discounted PO and on free units: the receipt and the bill
// carry the same value as the PO line.
func TestStage1487DiscountedPOMatchesGRNAndBill(t *testing.T) {
	f := newAgreementFixture(t)
	defer f.cleanup()
	const (
		sku, vend  = "TEST1487-M1", "TEST1487-MV"
		loc        = "TEST1487-LOC"
		poD, poF   = "TEST1487-POD", "TEST1487-POF"
		grnD, grnF = "TEST1487-GRND", "TEST1487-GRNF"
		viD, viF   = "TEST1487-VID", "TEST1487-VIF"
	)
	f.item(sku, "TEST1487-MD", "TEST1487-MC", 5)
	f.insert(vend, "Vendor", "Active", map[string]interface{}{"code": vend, "name": vend})
	f.agreement("TEST1487-MA", vend, "2026-01-01", "",
		map[string]interface{}{"sku": sku, "buy_qty": 10, "get_qty": 2, "min_qty": 10})

	run := func(poID, grnID, viID string, line map[string]interface{}, recvQty int, billAmount float64, wantStock int64) {
		p := map[string]interface{}{"code": poID, "vendor": vend, "order_date": "2026-11-03", "gst_mode": GSTModeExclusive, "items": itemsJSON(line)}
		if err := ApplyPurchaseOrderTotals("default", p); err != nil {
			t.Fatal(err)
		}
		f.insert(poID, "PurchaseOrder", "Approved", p)
		g := map[string]interface{}{"code": grnID, "po_id": poID, "location": loc, "received_items": itemsJSON(map[string]interface{}{"sku": sku, "qty": recvQty, "accepted_qty": recvQty})}
		if err := PrepareGRNPricing("default", true, g); err != nil {
			t.Fatalf("PrepareGRNPricing: %v", err)
		}
		f.insert(grnID, "GRN", "Approved", g)
		var items []interface{}
		_ = json.Unmarshal([]byte(g["received_items"].(string)), &items)
		if _, err := PostGRNReceiptWithQC("default", loc, items, "system", grnID); err != nil {
			t.Fatalf("PostGRNReceiptWithQC: %v", err)
		}
		var dr int64
		db.DB.QueryRow("SELECT COALESCE(SUM(debit),0) FROM "+f.schema+".gl_postings WHERE document_id = $1 AND account_code = '1200'", grnID).Scan(&dr)
		if dr != wantStock {
			t.Fatalf("%s: stock Dr 1200 = %d paise, want %d", grnID, dr, wantStock)
		}
		f.insert(viID, "VendorInvoice", "Draft", map[string]interface{}{"invoice_number": viID, "vendor_id": vend, "po_id": poID, "grn_id": grnID, "invoice_amount": billAmount, "financial_year": "2026-27"})
		m, err := MatchVendorInvoice("default", viID, "", "", 0)
		if err != nil || !m.Matched {
			t.Fatalf("%s: 3-way match = %+v, %v", viID, m, err)
		}
		// Stage 148.1 posts a bill that differs from the GRN to stock and 5195. A bill
		// at the discounted value does not differ, so nothing moves there.
		var variance int
		db.DB.QueryRow("SELECT COUNT(*) FROM "+f.schema+".gl_postings WHERE account_code = '"+accountCOGSVariance+"' AND document_id IN ($1, $2)", viID, grnID).Scan(&variance)
		if variance != 0 {
			t.Fatalf("%s: %d postings on the variance account, want none", viID, variance)
		}
	}
	t.Run("a discounted line is received and billed at its net value", func(t *testing.T) {
		// 5 units (below the buy-N break) at 200 less 10% = 900, +5% GST = 945.
		run(poD, grnD, viD, map[string]interface{}{"sku": sku, "qty": 5, "rate": 200, "discount_pct": 10}, 5, 945, 90000)
	})
	t.Run("free units are received at zero and the cost sees the real unit price", func(t *testing.T) {
		// 10 bought at 120 = 1200 buys 2 free: 12 received at 100 each.
		run(poF, grnF, viF, map[string]interface{}{"sku": sku, "qty": 10, "rate": 120}, 12, 1260, 120000)
		// Stage 148.1: 5 in hand at 180 (900) + 12 at 100 (1200) = 2100 over 17 units.
		if rate, ok, err := GetItemUnitCost("default", sku); err != nil || !ok || rate != 12353 {
			t.Fatalf("moving average = %d paise (%v, %v), want 12353", rate, ok, err)
		}
		if c, ok := resolvePOBaseUnitCostPaise("default", poF, sku); !ok || c != 10000 {
			t.Fatalf("unit cost = %d (%v), want 10000", c, ok)
		}
	})
}
