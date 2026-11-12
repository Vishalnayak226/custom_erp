package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// Stage 148.45: a returned gift gives its cost back to the account the sale
// charged it to (the promotional expense), not to COGS.

func stage14845Cart(t *testing.T, f stage1484Fixture, id string, gift bool) {
	t.Helper()
	items := []map[string]interface{}{{"sku": "TEE1484", "qty": 1, "sale_price": 100.0}}
	if gift {
		items = append(items, map[string]interface{}{"sku": "GIFT14845", "qty": 1, "sale_price": 0.0})
	}
	data, _ := json.Marshal(map[string]interface{}{"location": "S1484", "payment_mode": "Cash", "items": items})
	if _, err := db.DB.Exec("INSERT INTO "+f.schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Processing', 'cashier1484')", id, data); err != nil {
		t.Fatal(err)
	}
}

func stage14845Return(t *testing.T, f stage1484Fixture, cart, sku string) string {
	t.Helper()
	returnID, err := CreateReturnRequest(f.tenantID, "Customer Return", "S1484", cart, "", "tester", []ReturnItemInput{{SKU: sku, Qty: 1}})
	if err != nil {
		t.Fatalf("create return: %v", err)
	}
	_ = ApproveReturnRequest(f.tenantID, returnID, "approver")
	_ = ReceiveReturnRequest(f.tenantID, returnID, "store")
	if _, _, err := ApplyReturnQC(f.tenantID, returnID, map[string]string{sku: "Sellable"}, "qc"); err != nil {
		t.Fatalf("QC: %v", err)
	}
	return returnID
}

func TestStage14845GiftReturnReversesTheGiftAccount(t *testing.T) {
	f := stage1484Setup(t)
	f.insert("GIFT14845", "Item", "Active", "system", map[string]interface{}{"code": "GIFT14845", "name": "Free mug", "category": "Gifts", "sale_price": 0.0, "standard_cost": 25.0, "sellability": SellPromoOnly})
	if _, err := PostInventoryLedger(f.tenantID, "S1484", []interface{}{map[string]interface{}{"sku": "GIFT14845", "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	stage14845Cart(t, f, "CART-14845-G", true)
	if _, _, err := FinalizePOSCheckout(f.tenantID, "CART-14845-G", ""); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	sale := stage1484Posted(t, f.schema, "POSCart", "CART-14845-G")
	t.Logf("sale journal: %v", sale)
	giftAcct := "5180"
	if sale["D:"+giftAcct] != 2500 {
		t.Fatalf("the sale must charge the gift's 25.00 to %s: %v", giftAcct, sale)
	}
	if sale["D:5100"] != 4000 {
		t.Fatalf("the sale's COGS is the tee only: %v", sale)
	}

	rid := stage14845Return(t, f, "CART-14845-G", "GIFT14845")
	got := stage1484Posted(t, f.schema, "ReturnRequest", rid)
	t.Logf("gift return journal: %v", got)
	if got["C:"+giftAcct] != 2500 || got["C:5100"] != 0 || got["D:1200"] != 2500 {
		t.Fatalf("a gift return must credit %s, not COGS: %v", giftAcct, got)
	}

	// A normal line's return is untouched.
	stage14845Cart(t, f, "CART-14845-N", true)
	if _, _, err := FinalizePOSCheckout(f.tenantID, "CART-14845-N", ""); err != nil {
		t.Fatal(err)
	}
	rid2 := stage14845Return(t, f, "CART-14845-N", "TEE1484")
	if got := stage1484Posted(t, f.schema, "ReturnRequest", rid2); got["C:5100"] != 4000 || got["D:1200"] != 4000 || len(got) != 2 {
		t.Fatalf("a normal return stays on COGS: %v", got)
	}
}
