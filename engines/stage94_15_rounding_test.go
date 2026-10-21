package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 94.15: money at 2 decimals, bills and POs to the whole rupee with a
// round-off line, MRP always a whole rupee.
func TestStage94_15RoundingHelpers(t *testing.T) {
	for _, c := range []struct{ in, rounded, off float64 }{
		{970.8737864077669, 971, 0.13}, {970.40, 970, -0.40}, {970.50, 971, 0.50}, {1000, 1000, 0}, {0, 0, 0},
	} {
		if r, o := RoundToRupee(c.in); r != c.rounded || o != c.off {
			t.Errorf("RoundToRupee(%v) = %v, %v; want %v, %v", c.in, r, o, c.rounded, c.off)
		}
	}
	for in, want := range map[int64]int64{97087: 13, 97040: -40, 97050: 50, 97000: 0, 0: 0} {
		if got := roundOffPaise(in); got != want {
			t.Errorf("roundOffPaise(%d) = %d, want %d", in, got, want)
		}
	}
	for in, want := range map[float64]float64{999.5: 1000, 1199.4: 1199, 1199: 1199, 0: 0} {
		if got := WholeRupeeMRP(in); got != want {
			t.Errorf("WholeRupeeMRP(%v) = %v, want %v", in, got, want)
		}
	}

	item := map[string]interface{}{"code": "X", "mrp": 1199.6}
	NormalizeDocumentMRP("Item", item)
	typed := map[string]interface{}{"code": "Y", "mrp": "849.40"}
	NormalizeDocumentMRP("Item", typed)
	if item["mrp"] != 1200.0 || typed["mrp"] != 849.0 {
		t.Errorf("an Item's MRP is saved as a whole rupee: %v, %v", item["mrp"], typed["mrp"])
	}
	po := map[string]interface{}{"items": `[{"sku":"A","qty":2,"rate":100.5,"mrp":299.5},{"sku":"B","qty":1,"rate":10}]`}
	NormalizeDocumentMRP("PurchaseOrder", po)
	var lines []map[string]interface{}
	_ = json.Unmarshal([]byte(po["items"].(string)), &lines)
	if len(lines) != 2 || lines[0]["mrp"] != 300.0 || lines[0]["rate"] != 100.5 || lines[1]["mrp"] != nil {
		t.Errorf("a PO line's MRP is rounded and nothing else changes: %v", po["items"])
	}

	// The round-off is shared by line value and each line's tenders add up to
	// its net plus its share - rounding up and rounding down.
	for _, ro := range []int64{-40, 30} {
		net := int64(60040 + 37000)
		alloc := buildSaleAllocation([]string{"A", "B"}, []int{1, 1}, []int64{60040, 37000}, []int64{0, 0}, ro, 0, 100, 0,
			[]tenderPart{{Key: "Cash", Paise: net + ro}})
		var shares int64
		for _, a := range alloc {
			var paid int64
			for _, v := range a.Tenders {
				paid += v
			}
			if paid != a.NetPaise+a.RoundOffPaise {
				t.Errorf("round-off %d, line %s: tenders %d, want net %d + share %d", ro, a.SKU, paid, a.NetPaise, a.RoundOffPaise)
			}
			shares += a.RoundOffPaise
		}
		if shares != ro {
			t.Errorf("round-off shares add up to %d, want %d", shares, ro)
		}
	}
}

func TestStage94_15POTotalAndPOSBillRoundOff(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	gl := func(docType, docID, account string) int64 { // net debit
		var v int64
		_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit - credit), 0) FROM `+schemaName+`.gl_postings WHERE document_type = $1 AND document_id = $2 AND account_code = $3`, docType, docID, account).Scan(&v)
		return v
	}
	const loc = "STORE-R"
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "Store R", "type": "Store", "sellable": "Yes"})
	for code, price := range map[string]float64{"SHIRT-R": 970.40, "SOCKS-R": 500.70} {
		insert(code, "Item", "Active", map[string]interface{}{"code": code, "name": code, "sale_price": price, "hsn_code": "6109", "tax_treatment": "Exempt"})
		if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": code, "qty": 10.0}}, false); err != nil {
			t.Fatal(err)
		}
	}

	// A PO that comes to 100.40 is a 100 PO with a -0.40 round-off line.
	preview, err := PreviewPurchaseOrder(tenantID, map[string]interface{}{"gst_mode": GSTModeExclusive, "items": `[{"sku":"SHIRT-R","qty":1,"rate":100.4}]`})
	if err != nil || preview.GrandTotal != 100 || preview.RoundOff != -0.4 {
		t.Fatalf("PO preview: %v total %v round-off %v, want 100 / -0.40", err, preview.GrandTotal, preview.RoundOff)
	}

	// 970.40 rounds down to 970; 500.70 rounds up to 501. The customer pays
	// the rounded bill, revenue keeps the lines' value, 5280 takes the rest.
	for cart, c := range map[string]struct {
		sku       string
		cash, off int64
	}{"CART-RD": {"SHIRT-R", 97000, 40}, "CART-RU": {"SOCKS-R", 50100, -30}} {
		quote, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: loc, Lines: []QuoteLineRequest{{Sku: c.sku, Qty: 1}}})
		if err != nil {
			t.Fatal(err)
		}
		insert(cart, "POSCart", "Processing", map[string]interface{}{"location": loc, "payment_mode": "Cash", "items": POSCartItemsFromQuote(quote)})
		if _, _, err := FinalizePOSCheckout(tenantID, cart, ""); err != nil {
			t.Fatalf("finalize %s: %v", cart, err)
		}
		if got := gl("POSCart", cart, "1100"); got != c.cash {
			t.Errorf("%s: cash %d, want %d", cart, got, c.cash)
		}
		if got := gl("POSCart", cart, "5280"); got != c.off {
			t.Errorf("%s: round-off account %d, want %d", cart, got, c.off)
		}
		if rec, err := ReconcileSale(tenantID, cart); err != nil || rec.Verdict != "Balanced" {
			t.Errorf("%s reconciles: %v %+v", cart, err, rec)
		}
	}

	// Returning the 970.40 shirt pays back the 970 the customer paid and
	// reverses the 0.40 from 5280, not from revenue.
	returnID, err := CreateReturnRequest(tenantID, "Customer Return", loc, "CART-RD", "", "tester", []ReturnItemInput{{SKU: "SHIRT-R", Qty: 1}})
	if err != nil {
		t.Fatal(err)
	}
	_ = ApproveReturnRequest(tenantID, returnID, "approver")
	_ = ReceiveReturnRequest(tenantID, returnID, "store")
	refund, refundID, err := ApplyReturnQC(tenantID, returnID, map[string]string{"SHIRT-R": "Sellable"}, "qc")
	if err != nil || refund != 970 {
		t.Fatalf("QC: %v refund %.2f, want 970", err, refund)
	}
	_ = ApproveRefundRequest(tenantID, refundID, "manager")
	if err := ProcessRefundRequest(tenantID, refundID, "manager", "Cash"); err != nil {
		t.Fatalf("process: %v", err)
	}
	for acct, want := range map[string]int64{"1100": -97000, "5280": -40, "4100": 97040} {
		if got := gl("RefundRequest", refundID, acct); got != want {
			t.Errorf("refund: account %s net %d, want %d", acct, got, want)
		}
	}
	if rec, err := ReconcileReturn(tenantID, returnID); err != nil || rec.Verdict != "Balanced" {
		t.Errorf("the return reconciles: %v %+v", err, rec)
	}
}
