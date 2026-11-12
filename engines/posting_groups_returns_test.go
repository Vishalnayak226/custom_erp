package engines

import (
	"testing"

	"custom_erp/db"
)

// Stage 148.4 (returns): a return reverses revenue through the account the
// sale's rule used; no rule, or a rule added after the sale, stays on 4100.

func stage1484ReturnRefund(t *testing.T, f stage1484Fixture, cart, sku string) (returnID, refundID string) {
	t.Helper()
	returnID, err := CreateReturnRequest(f.tenantID, "Customer Return", "S1484", cart, "", "tester", []ReturnItemInput{{SKU: sku, Qty: 1}})
	if err != nil {
		t.Fatalf("create return: %v", err)
	}
	_ = ApproveReturnRequest(f.tenantID, returnID, "approver")
	_ = ReceiveReturnRequest(f.tenantID, returnID, "store")
	_, refundID, err = ApplyReturnQC(f.tenantID, returnID, map[string]string{sku: "Sellable"}, "qc")
	if err != nil {
		t.Fatalf("QC: %v", err)
	}
	_ = ApproveRefundRequest(f.tenantID, refundID, "manager")
	if err := ProcessRefundRequest(f.tenantID, refundID, "manager", "Cash"); err != nil {
		t.Fatalf("process refund: %v", err)
	}
	return returnID, refundID
}

func TestStage1484ReturnFollowsTheSaleRule(t *testing.T) {
	f := stage1484Setup(t)
	f.profile(t, "PP-1484R-POS", map[string]interface{}{"event": "pos_sale_revenue", "item_group": "Footwear", "credit_account": "4150"})
	f.cart(t, "CART-1484-R1", "")
	if _, _, err := FinalizePOSCheckout(f.tenantID, "CART-1484-R1", ""); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	_, refundID := stage1484ReturnRefund(t, f, "CART-1484-R1", "SHOE1484")
	got := stage1484Posted(t, f.schema, "RefundRequest", refundID)
	if got["D:4150"] != 10000 || got["D:4100"] != 0 || got["C:1100"] != 10000 {
		t.Fatalf("a footwear return must debit the footwear account: %v", got)
	}
	// An apparel return stays on 4100.
	f.cart(t, "CART-1484-R2", "")
	if _, _, err := FinalizePOSCheckout(f.tenantID, "CART-1484-R2", ""); err != nil {
		t.Fatal(err)
	}
	_, refund2 := stage1484ReturnRefund(t, f, "CART-1484-R2", "TEE1484")
	if got := stage1484Posted(t, f.schema, "RefundRequest", refund2); got["D:4100"] != 10000 || got["D:4150"] != 0 {
		t.Fatalf("an apparel return stays on 4100: %v", got)
	}
}

func TestStage1484ReturnWithNoRuleStaysOn4100(t *testing.T) {
	f := stage1484Setup(t)
	f.cart(t, "CART-1484-R3", "")
	if _, _, err := FinalizePOSCheckout(f.tenantID, "CART-1484-R3", ""); err != nil {
		t.Fatal(err)
	}
	// A rule added after the sale does not move the return: the sale was on 4100.
	f.profile(t, "PP-1484R-LATE", map[string]interface{}{"event": "pos_sale_revenue", "item_group": "Footwear", "credit_account": "4150"})
	_, refundID := stage1484ReturnRefund(t, f, "CART-1484-R3", "SHOE1484")
	if got := stage1484Posted(t, f.schema, "RefundRequest", refundID); got["D:4100"] != 10000 || got["D:4150"] != 0 {
		t.Fatalf("no rule at sale time: return stays on 4100: %v", got)
	}
}

func TestStage1484ReturnTaxFollowsTheSaleRule(t *testing.T) {
	f := stage1484Setup(t)
	for _, sku := range []string{"TEE1484", "SHOE1484"} {
		if _, err := db.DB.Exec("UPDATE "+f.schema+".documents SET data = data || '{\"hsn_code\":\"6109\",\"tax_treatment\":\"Taxable\",\"gst_rate\":18}'::jsonb WHERE doctype = 'Item' AND id = $1", sku); err != nil {
			t.Fatal(err)
		}
	}
	f.profile(t, "PP-1484R-TAX", map[string]interface{}{"event": "pos_sale_revenue", "item_group": "Footwear", "credit_account": "4150"})
	f.cart(t, "CART-1484-R4", "")
	if _, err := db.DB.Exec("UPDATE " + f.schema + ".documents SET data = data || '{\"gst_breakdown\":{\"cgst\":15.25,\"sgst\":15.26,\"total_tax\":30.51}}'::jsonb WHERE id = 'CART-1484-R4'"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := FinalizePOSCheckout(f.tenantID, "CART-1484-R4", ""); err != nil {
		t.Fatal(err)
	}
	// The sale takes its 30.51 tax back out of the accounts its revenue went to, not all from 4100.
	sale := stage1484Posted(t, f.schema, "POSCart", "CART-1484-R4")
	if sale["D:4100"] == 0 || sale["D:4150"] == 0 || sale["D:4100"]+sale["D:4150"] != 3051 {
		t.Fatalf("the sale's tax comes off the accounts its revenue went to: %v", sale)
	}
	returnID, refundID := stage1484ReturnRefund(t, f, "CART-1484-R4", "SHOE1484")
	ref := stage1484Posted(t, f.schema, "RefundRequest", refundID)
	rr := stage1484Posted(t, f.schema, "ReturnRequest", returnID)
	if ref["D:4100"] != 0 || ref["D:4150"] == 0 || rr["C:4100"] != 0 || rr["C:4150"] == 0 {
		t.Fatalf("a footwear return and its tax reversal stay off 4100: refund %v, return %v", ref, rr)
	}
}

// TestStage1484ReconciliationReadsTheRuleAccounts: a POS sale whose revenue and
// cost a rule moved off 4100 and 5100 reconciles instead of showing a shortfall.
func TestStage1484ReconciliationReadsTheRuleAccounts(t *testing.T) {
	f := stage1484Setup(t)
	f.profile(t, "PP-1484R-RECR", map[string]interface{}{"event": "pos_sale_revenue", "item_group": "Footwear", "credit_account": "4150"})
	f.profile(t, "PP-1484R-RECC", map[string]interface{}{"event": "pos_sale_cogs", "item_group": "Footwear", "debit_account": "5110"})
	f.cart(t, "CART-1484-R5", "")
	if _, _, err := FinalizePOSCheckout(f.tenantID, "CART-1484-R5", ""); err != nil {
		t.Fatal(err)
	}
	sale := stage1484Posted(t, f.schema, "POSCart", "CART-1484-R5")
	if sale["C:4150"] == 0 || sale["D:5110"] == 0 {
		t.Fatalf("the rules did not move the sale: %v", sale)
	}
	row, err := ReconcileSale(f.tenantID, "CART-1484-R5")
	if err != nil {
		t.Fatal(err)
	}
	if !amountsAgree(row.RevenuePosted, row.CartSaleValue) {
		t.Errorf("revenue posted %.2f, the cart totals %.2f", row.RevenuePosted, row.CartSaleValue)
	}
	wantCOGS := PaiseToRupees(sale["D:5100"] + sale["D:5110"])
	if !amountsAgree(row.COGSPosted, wantCOGS) || row.COGSPosted == 0 {
		t.Errorf("COGS posted %.2f, the sale's vouchers debited %.2f", row.COGSPosted, wantCOGS)
	}
}
