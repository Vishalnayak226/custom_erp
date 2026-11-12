package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// Stage 130.1 (129.6 decision): a GST-inclusive goods receipt is worked per
// line from its incl. price, so a correct bill adds up exactly and raises no
// paisa note. 60 lines x 7 pcs x Rs 99 incl. 3% GST is Rs 41,580; worked per
// piece (Rs 96.12 ex-GST) it read Rs 41,581.51.
func TestStage130GSTInclusiveGRNWorksPerLine(t *testing.T) {
	var lines []map[string]interface{}
	for i := 1; i <= 60; i++ {
		lines = append(lines, map[string]interface{}{
			"line_no": float64(i), "sku": "S", "qty": 7.0, "received_qty": 7.0, "invoice_qty": 7.0,
			"gst_inclusive": true, "gst_rate": 3.0,
			"po_rate": round2(99 / 1.03), "po_rate_incl": 99.0, "invoice_rate": round2(99 / 1.03),
		})
	}
	iv, ig := grnInvoiceLineTotals(lines)
	if got := round2(iv + ig); got != 41580 {
		t.Fatalf("invoice lines total = %.2f, want 41580.00 (value %.2f + GST %.2f)", got, iv, ig)
	}
	if c := grnInvoiceCheck(lines, 41580); c != nil {
		t.Fatalf("a correct bill was refused at Verify: %s", c.Message)
	}
	ov, og := grnOurTotals(lines)
	if round2(ov+og) != 41580 || ov != iv || og != ig {
		t.Fatalf("our totals %.2f + %.2f differ from the invoice's %.2f + %.2f", ov, og, iv, ig)
	}
	notes, taxable, gst := grnNoteLines(map[string]interface{}{}, lines)
	if len(notes) != 0 || taxable != 0 || gst != 0 {
		t.Fatalf("an unchanged inclusive receipt raised note lines: %+v", notes)
	}

	// 28% GST, where grossing the rounded ex-GST price back up is a paisa
	// off (Rs 100 incl. -> 78.13 -> 100.01): the price as keyed wins.
	l := map[string]interface{}{"gst_inclusive": true, "gst_rate": 28.0, "invoice_qty": 3.0, "received_qty": 3.0,
		"invoice_rate": round2(100 / 1.28), "invoice_rate_incl": 100.0}
	v, g := grnInvoiceLineAmount(l)
	if round2(v+g) != 300 {
		t.Fatalf("3 x Rs 100 incl. 28%% = %.2f + %.2f, want 300.00", v, g)
	}
	// A typed incl. price that no longer matches the ex-GST price is ignored.
	l["invoice_rate"] = 80.0
	if inc, _ := grnLineInclPrice(l, 80, "invoice_rate_incl"); math.Abs(inc-102.4) > 0.001 {
		t.Fatalf("stale incl. price used: got %.2f, want 102.40", inc)
	}
	// A line priced before GST keeps qty x price plus GST.
	ex := map[string]interface{}{"gst_rate": 5.0, "invoice_qty": 3.0, "received_qty": 3.0, "invoice_rate": 10.0}
	if v, g := grnInvoiceLineAmount(ex); v != 30 || math.Abs(g-1.5) > 1e-9 {
		t.Fatalf("ex-GST line = %.2f + %.4f, want 30 + 1.5", v, g)
	}
}

// Stage 130.4 (81.6): once a library attribute replaces a free-text field,
// a sticker printing the field's name still gets the attribute's value - and
// an item's own field value still wins.
func TestStage130StickerFieldFallsBackToAttribute(t *testing.T) {
	f := stickerItemFields(map[string]interface{}{"variant_option_values": "COLOR:Red;POLISH:Gold", "polish": "Rhodium"})
	if f["color"] != "Red" {
		t.Fatalf("color = %q, want Red from COLOR", f["color"])
	}
	if f["polish"] != "Rhodium" {
		t.Fatalf("polish = %q, the item's own value must win", f["polish"])
	}
	if f["variant_option_values"] != "Red / Gold" {
		t.Fatalf("variant text = %q", f["variant_option_values"])
	}
}

// Stage 130.8 (72.D h / 84.6): a card stored as typed before 130.8 is sealed
// by the boot backfill - found by its hash, printed by its last four - and
// only an administrator can show its full number again.
func TestStage130GiftCardNumbersSealed(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	const id, number = "SVI-T130-SEAL", "6123450000789012"
	cleanup := func() { db.DB.Exec(`DELETE FROM `+schema+`.stored_value_instruments WHERE instrument_id = $1`, id) }
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.stored_value_instruments (instrument_id, card_number, profile, status, created_by)
		VALUES ($1, $2, 'Standard Gift Card', 'Inactive', 'system')`, id, number); err != nil {
		t.Fatalf("legacy card: %v", err)
	}
	if n, err := EncryptStoredValueCards(schema); err != nil || n < 1 {
		t.Fatalf("backfill sealed %d (%v), want the legacy card", n, err)
	}
	var stored, last4, enc string
	_ = db.DB.QueryRow(`SELECT card_number, COALESCE(card_last4, ''), COALESCE(card_enc, '') FROM `+schema+`.stored_value_instruments WHERE instrument_id = $1`, id).Scan(&stored, &last4, &enc)
	if stored == number || !strings.HasPrefix(stored, svHashPrefix) || last4 != "9012" || enc == "" || strings.Contains(enc, number) {
		t.Fatalf("sealed row = %q / %q / %q", stored, last4, enc)
	}
	if n, _ := EncryptStoredValueCards(schema); n != 0 {
		t.Fatalf("a second backfill sealed %d again", n)
	}
	v, err := StoredValueBalance("default", "6123 4500 0078 9012")
	if err != nil || v.Masked != "**** 9012" {
		t.Fatalf("lookup by the typed number: %v %+v", err, v)
	}
	if _, err := RevealStoredValueCard("default", RoleCashier, "cashier1", id); err == nil {
		t.Fatalf("a cashier was shown a full card number")
	}
	got, err := RevealStoredValueCard("default", RoleSuperAdmin, "admin", id)
	if err != nil || got["card_number"] != number {
		t.Fatalf("reveal: %v %v", err, got)
	}
}

// Stage 130.9 (72.D c): a manager cancels today's bill - every line back in
// stock, the money back to its tender, points reversed - once; a cashier
// cannot, and a partly returned bill is returned instead.
func TestStage130CancelPOSBill(t *testing.T) {
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
	const loc, cust = "STORE-CB", "CUST-CB"
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "Store CB", "type": "Store", "sellable": "Yes"})
	insert("SHIRT", "Item", "Active", map[string]interface{}{"code": "SHIRT", "name": "SHIRT", "sale_price": 1000.0, "hsn_code": "6109", "tax_treatment": "Taxable", "gst_rate": 5.0})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "SHIRT", "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	sell := func(cart string, qty int) {
		t.Helper()
		q, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: loc, CustomerID: cust, Lines: []QuoteLineRequest{{Sku: "SHIRT", Qty: qty}}})
		if err != nil {
			t.Fatalf("quote %s: %v", cart, err)
		}
		insert(cart, "POSCart", "Processing", map[string]interface{}{"location": loc, "payment_mode": "Cash", "customer_id": cust,
			"items": POSCartItemsFromQuote(q), "gst_breakdown": q.GSTBreakdown})
		if _, _, err := FinalizePOSCheckout(tenantID, cart, ""); err != nil {
			t.Fatalf("finalize %s: %v", cart, err)
		}
	}
	stock := func() float64 {
		var v float64
		_ = db.DB.QueryRow(`SELECT COALESCE(SUM(on_hand), 0) FROM `+schemaName+`.inventory_availability WHERE sku = 'SHIRT' AND location_code = $1`, loc).Scan(&v)
		return v
	}
	sell("CART-CB-1", 2)
	before := stock()
	if _, err := CancelPOSBill(tenantID, RoleCashier, "cashier1", "CART-CB-1", loc, "wrong item"); err == nil {
		t.Fatalf("a cashier cancelled a bill")
	}
	if _, err := CancelPOSBill(tenantID, RoleStoreManager, "manager1", "CART-CB-1", loc, ""); err == nil {
		t.Fatalf("a bill was cancelled with no reason")
	}
	out, err := CancelPOSBill(tenantID, RoleStoreManager, "manager1", "CART-CB-1", loc, "customer changed mind")
	if err != nil || out.Refund != 2000 || out.ReturnRequestID == "" {
		t.Fatalf("cancel: %v %+v", err, out)
	}
	if after := stock(); after != before+2 {
		t.Errorf("stock after cancel %v, want %v", after, before+2)
	}
	if b, _ := GetLoyaltyBalance(tenantID, cust); b != 0 {
		t.Errorf("points earned on the bill are reversed: %d", b)
	}
	again, err := CancelPOSBill(tenantID, RoleStoreManager, "manager1", "CART-CB-1", loc, "again")
	if err != nil || !again.Replayed || again.ReturnRequestID != out.ReturnRequestID {
		t.Fatalf("a second cancel must replay the first: %v %+v", err, again)
	}
	// Partly returned: refused.
	sell("CART-CB-2", 2)
	if _, err := StartTillExchange(tenantID, "cashier1", ExchangeReturnRequest{OriginalOrderID: "CART-CB-2", Location: loc, Lines: []ReturnItemInput{{SKU: "SHIRT", Qty: 1}}}); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if _, err := CancelPOSBill(tenantID, RoleStoreManager, "manager1", "CART-CB-2", loc, "late"); err == nil {
		t.Fatalf("a partly returned bill was cancelled")
	}
}

// Stage 130.9 (72.D a): with release approval on, a new item starts as Draft
// (refused while no one approves items), cannot be released by an edit or
// sold at the till, and its approval makes it Active; a rejection Blocks it.
func TestStage130ItemReleaseApproval(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	payload := map[string]interface{}{"name": "Ring", "status": "Active"}
	if err := PrepareItemRelease(tenantID, true, "", payload); err != nil || payload["status"] != "Active" {
		t.Fatalf("setting off: nothing changes (%v, %v)", err, payload["status"])
	}
	if err := SetSetting(tenantID, SettingItemReleaseApproval, "true", "test"); err != nil {
		t.Fatal(err)
	}
	_, _ = db.DB.Exec(`DELETE FROM ` + schemaName + `.approval_rules WHERE doctype = 'Item'`)
	if err := PrepareItemRelease(tenantID, true, "", map[string]interface{}{"name": "Ring"}); err == nil {
		t.Fatalf("a new item was accepted with no one to approve it")
	}
	if _, err := UpsertApprovalRule(tenantID, "Item", 0, nil, RoleStoreManager, nil); err != nil {
		t.Fatal(err)
	}
	payload = map[string]interface{}{"name": "Ring", "status": "Active"}
	if err := PrepareItemRelease(tenantID, true, "", payload); err != nil || payload["status"] != "Draft" {
		t.Fatalf("a new item starts as Draft: %v %v", err, payload["status"])
	}
	for _, id := range []string{"T130-RING", "T130-BAD"} {
		raw, _ := json.Marshal(map[string]interface{}{"code": id, "name": id, "status": "Draft", "sale_price": 100.0})
		if _, err := db.DB.Exec(`INSERT INTO `+schemaName+`.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Draft', 'maker')`, id, raw); err != nil {
			t.Fatal(err)
		}
		if err := SubmitForApproval(tenantID, "Item", id, "maker", RoleCashier); err != nil {
			t.Fatalf("submit %s: %v", id, err)
		}
	}
	if err := PrepareItemRelease(tenantID, false, "Pending Approval", map[string]interface{}{"status": "Active"}); err == nil {
		t.Fatalf("an edit released a pending item")
	}
	if err := itemReleaseGate(map[string]interface{}{"status": "Pending Approval", "name": "Ring"}, "T130-RING"); err == nil {
		t.Fatalf("the till sold a pending item")
	}
	if err := DecideApproval(tenantID, "Item", "T130-RING", "checker", RoleStoreManager, "", "Approved", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := DecideApproval(tenantID, "Item", "T130-BAD", "checker", RoleStoreManager, "", "Rejected", "duplicate"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	status := func(id string) string {
		var s string
		_ = db.DB.QueryRow(`SELECT status FROM `+schemaName+`.documents WHERE doctype = 'Item' AND id = $1`, id).Scan(&s)
		return s
	}
	if status("T130-RING") != "Active" || status("T130-BAD") != ItemStatusBlocked {
		t.Fatalf("after the decisions: %s / %s, want Active / Blocked", status("T130-RING"), status("T130-BAD"))
	}
}

// Stage 130.9 (72.D d): a store's "Items not sold here" stops the till, its
// "Items not stocked here" stops a goods receipt - by code, category or
// product type; other stores, and blank lists, are unaffected.
func TestStage130StoreItemRules(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO `+schemaName+`.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')`, id, doctype, raw); err != nil {
			t.Fatal(err)
		}
	}
	insert("S-NOGOLD", "Location", map[string]interface{}{"code": "S-NOGOLD", "items_not_sold_here": "gold, RING-9", "items_not_stocked_here": "Silver"})
	insert("S-ALL", "Location", map[string]interface{}{"code": "S-ALL"})
	insert("RING-1", "Item", map[string]interface{}{"code": "RING-1", "name": "Ring", "category": "GOLD", "product_type": "Ring"})
	insert("CHAIN-1", "Item", map[string]interface{}{"code": "CHAIN-1", "name": "Chain", "category": "SILVER"})
	ring := map[string]interface{}{"code": "RING-1", "category": "GOLD"}
	if err := storeSellGate(tenantID, "S-NOGOLD", "RING-1", ring); err == nil {
		t.Errorf("a GOLD item was sold where gold is not sold")
	}
	if err := storeSellGate(tenantID, "S-ALL", "RING-1", ring); err != nil {
		t.Errorf("another store refused it: %v", err)
	}
	if err := storeSellGate(tenantID, "S-NOGOLD", "CHAIN-1", map[string]interface{}{"code": "CHAIN-1", "category": "SILVER"}); err != nil {
		t.Errorf("a silver chain is sold there: %v", err)
	}
	if err := storeStockGate(tenantID, "S-NOGOLD", []string{"CHAIN-1"}); err == nil {
		t.Errorf("silver was received where silver is not stocked")
	}
	if err := storeStockGate(tenantID, "S-NOGOLD", []string{"RING-1"}); err != nil {
		t.Errorf("gold is stocked there: %v", err)
	}
}

// Stage 130.9 (72.D b): a scheduled tax change taxes from its date - not
// before - and is then folded into the item's own fields with its history.
func TestStage130ScheduledItemTaxChange(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	for id, from := range map[string]string{"TAX-DUE": yesterday, "TAX-LATER": tomorrow} {
		raw, _ := json.Marshal(map[string]interface{}{"code": id, "name": id, "hsn_code": "7113", "tax_treatment": "Taxable", "gst_rate": 3.0,
			"tax_change_from": from, "tax_change_gst_rate": 5.0})
		if _, err := db.DB.Exec(`INSERT INTO `+schemaName+`.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')`, id, raw); err != nil {
			t.Fatal(err)
		}
	}
	if info, err := GetItemTaxInfo(tenantID, "TAX-DUE"); err != nil || info.GSTRate != 5 {
		t.Fatalf("due change: %v rate %v, want 5", err, info.GSTRate)
	}
	if info, err := GetItemTaxInfo(tenantID, "TAX-LATER"); err != nil || info.GSTRate != 3 {
		t.Fatalf("future change: %v rate %v, want 3 until its date", err, info.GSTRate)
	}
	if n, err := ApplyDueTaxChanges(schemaName); err != nil || n != 1 {
		t.Fatalf("fold-in: %d (%v), want 1", n, err)
	}
	var raw string
	_ = db.DB.QueryRow(`SELECT data FROM ` + schemaName + `.documents WHERE id = 'TAX-DUE'`).Scan(&raw)
	var d map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &d)
	if d["gst_rate"] != 5.0 || strField(d, "tax_change_from") != "" || !strings.Contains(strField(d, "tax_change_history"), `"old_gst_rate":3`) {
		t.Fatalf("folded item: rate %v, from %q, history %q", d["gst_rate"], strField(d, "tax_change_from"), strField(d, "tax_change_history"))
	}
	for _, bad := range []map[string]interface{}{
		{"tax_change_gst_rate": 5.0},
		{"tax_change_from": tomorrow},
		{"tax_change_from": "next week", "tax_change_gst_rate": 5.0},
		{"tax_change_from": tomorrow, "tax_change_hsn": "71"},
	} {
		if err := validateScheduledTaxChange(bad); err == nil {
			t.Errorf("accepted an incomplete change %v", bad)
		}
	}
	if err := validateScheduledTaxChange(map[string]interface{}{"tax_change_from": tomorrow, "tax_change_gst_rate": 18.0, "tax_change_hsn": "711319"}); err != nil {
		t.Errorf("refused a complete change: %v", err)
	}
}

// Stage 130.9 (72.D f): a product type that allows negative stock sells past
// zero (recorded as a negative-stock event); others still stop at zero. And
// the free gift comes back when the whole purchase does, unless kept.
func TestStage130NegativeStockAndGiftReturn(t *testing.T) {
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
	const loc = "STORE-NEG"
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "Store NEG", "type": "Store", "sellable": "Yes"})
	insert("PT-MTO", "ProductType", "Active", map[string]interface{}{"code": "PT-MTO", "name": "Made to order", "allow_negative_stock": "Yes"})
	insert("CAKE", "Item", "Active", map[string]interface{}{"code": "CAKE", "name": "Cake", "sale_price": 500.0, "hsn_code": "1905", "tax_treatment": "Taxable", "gst_rate": 5.0, "product_type": "PT-MTO"})
	insert("BREAD", "Item", "Active", map[string]interface{}{"code": "BREAD", "name": "Bread", "sale_price": 50.0, "hsn_code": "1905", "tax_treatment": "Taxable", "gst_rate": 5.0})
	sell := func(cart, sku string) error {
		q, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: loc, Lines: []QuoteLineRequest{{Sku: sku, Qty: 1}}})
		if err != nil {
			return err
		}
		insert(cart, "POSCart", "Processing", map[string]interface{}{"location": loc, "payment_mode": "Cash", "items": POSCartItemsFromQuote(q), "gst_breakdown": q.GSTBreakdown})
		_, _, err = FinalizePOSCheckout(tenantID, cart, "")
		return err
	}
	if err := sell("CART-NEG-1", "CAKE"); err != nil {
		t.Fatalf("a made-to-order item with no stock was refused: %v", err)
	}
	if err := sell("CART-NEG-2", "BREAD"); err == nil {
		t.Fatalf("an ordinary item sold with no stock")
	}

	// Free gift: a bill of 1 RING (paid) + 1 POUCH (gift).
	insert("CART-GWP", "POSCart", "Paid", map[string]interface{}{"items": []map[string]interface{}{
		{"sku": "RING", "qty": 1.0}, {"sku": "POUCH", "qty": 1.0, "promotion_ref": "OFF-GWP"}}})
	if err := checkGiftComesBack(tenantID, "CART-GWP", []ReturnItemInput{{SKU: "RING", Qty: 1}}, map[string]int{}); err == nil {
		t.Errorf("the whole purchase came back without its gift")
	}
	if err := checkGiftComesBack(tenantID, "CART-GWP", []ReturnItemInput{{SKU: "RING", Qty: 1}, {SKU: "POUCH", Qty: 1}}, map[string]int{}); err != nil {
		t.Errorf("purchase and gift together: %v", err)
	}
	if err := SetSetting(tenantID, SettingGiftReturnRule, GiftReturnKeep, "test"); err != nil {
		t.Fatal(err)
	}
	if err := checkGiftComesBack(tenantID, "CART-GWP", []ReturnItemInput{{SKU: "RING", Qty: 1}}, map[string]int{}); err != nil {
		t.Errorf("with keep, the gift stays: %v", err)
	}
}

// Stage 130.10 (78.3 a-h): each day limit, off at 0, does its one thing.
func TestStage130DayLimits(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO public.tenants (tenant_id, name, schema_name) VALUES ($1, $1, $2) ON CONFLICT DO NOTHING`, tenantID, schemaName); err != nil {
		t.Logf("tenant row: %v", err)
	}
	day := func(n int) string { return time.Now().AddDate(0, 0, n).Format("2006-01-02") }
	set := func(k, v string) {
		if err := SetSetting(tenantID, k, v, "test"); err != nil {
			t.Fatal(err)
		}
	}
	insert := func(id, doctype, status string, data map[string]interface{}, ageDays int) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO `+schemaName+`.documents (id, doctype, data, status, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'system', NOW() - make_interval(days => $5), NOW() - make_interval(days => $5))`, id, doctype, raw, status, ageDays); err != nil {
			t.Fatal(err)
		}
	}
	// (c) off: anything goes; on: a cashier is held, an admin is not.
	old := map[string]interface{}{"vendor_invoice_date": day(-40)}
	if err := ValidateDateLimits(tenantID, RoleCashier, "GRN", old); err != nil {
		t.Fatalf("limit off: %v", err)
	}
	set(SettingBackdateDays, "30")
	set(SettingForwardDateDays, "2")
	if err := ValidateDateLimits(tenantID, RoleStoreManager, "GRN", old); err == nil {
		t.Errorf("a 40-day-old invoice date passed a 30-day limit")
	}
	if err := ValidateDateLimits(tenantID, RoleSuperAdmin, "GRN", old); err != nil {
		t.Errorf("an administrator is not held to it: %v", err)
	}
	if err := ValidateDateLimits(tenantID, RoleStoreManager, "VendorInvoice", map[string]interface{}{"invoice_date": day(5)}); err == nil {
		t.Errorf("a date 5 days ahead passed a 2-day limit")
	}
	if err := ValidateDateLimits(tenantID, RoleStoreManager, "PurchaseOrder", map[string]interface{}{"expected_delivery_date": day(60)}); err != nil {
		t.Errorf("a PO's expected date may be far ahead: %v", err)
	}
	// (b) receipt timing, (h) input credit deadline, (f) quote validity.
	insert("PO-DL", "PurchaseOrder", "Approved", map[string]interface{}{"expected_delivery_date": day(-10)}, 20)
	set(SettingGRNLateAfterDays, "3")
	grn := map[string]interface{}{"po_id": "PO-DL", "vendor_invoice_date": "2024-06-15"}
	set(SettingITCDeadlineWarning, "true")
	PrepareDayLimitStamps(tenantID, "GRN", true, grn)
	if grn["receipt_timing"] != "Late by 10 days" || !strings.Contains(strField(grn, "itc_time_barred"), "2025-11-30") {
		t.Errorf("GRN stamps: timing %v, itc %v", grn["receipt_timing"], grn["itc_time_barred"])
	}
	set(SettingQuoteValidityDays, "15")
	q := map[string]interface{}{}
	PrepareDayLimitStamps(tenantID, "VendorQuote", true, q)
	if q["valid_until"] != day(15) {
		t.Errorf("quote valid until %v, want %s", q["valid_until"], day(15))
	}
	// (a) reminder, (e) requisition expiry, (g) transit - the worker's jobs.
	insert("PO-REMIND", "PurchaseOrder", "Approved", map[string]interface{}{"expected_delivery_date": day(2)}, 1)
	insert("PR-OLD", "PurchaseRequisition", "Approved", map[string]interface{}{}, 20)
	insert("PR-NEW", "PurchaseRequisition", "Approved", map[string]interface{}{}, 1)
	insert("TO-SLOW", "TransferOrder", "Dispatched", map[string]interface{}{"dispatched_at": time.Now().AddDate(0, 0, -9).Format(time.RFC3339)}, 9)
	set(SettingVendorReminderDays, "3")
	set(SettingRequisitionExpiryDays, "14")
	set(SettingTransitAlertDays, "7")
	if err := runDayLimitJobs(schemaName); err != nil {
		t.Fatalf("jobs: %v", err)
	}
	field := func(id, key string) string {
		var v string
		_ = db.DB.QueryRow(`SELECT COALESCE(data->>$2, '') || '|' || status FROM `+schemaName+`.documents WHERE id = $1`, id, key).Scan(&v)
		return v
	}
	if got := field("PO-REMIND", "vendor_reminded_on"); !strings.HasPrefix(got, day(0)) {
		t.Errorf("reminder not stamped: %q", got)
	}
	if field("PR-OLD", "status") != "Expired|Expired" || field("PR-NEW", "status") == "Expired|Expired" {
		t.Errorf("requisition expiry: old %q, new %q", field("PR-OLD", "status"), field("PR-NEW", "status"))
	}
	if got := field("TO-SLOW", "in_transit_overdue_on"); !strings.HasPrefix(got, day(0)) {
		t.Errorf("transit not flagged: %q", got)
	}
	if rows, err := transfersInTransitReport(tenantID, nil); err != nil || len(rows) != 1 {
		t.Errorf("transit report: %v %v", err, rows)
	}
	if rows, err := itcPastDeadlineReport(tenantID, nil); err != nil || len(rows) != 0 {
		t.Errorf("itc report on an empty client: %v %v", err, rows)
	}
}

// Stage 130.12 (74.2.2): a refund the customer takes as store credit lands on
// their store credit (2650, no card), the return gets its own CN number, the
// credit pays a later bill of the same customer only, within the manager
// limit, and a walk-in bill cannot be refunded to store credit.
func TestStage130StoreCredit(t *testing.T) {
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
	const loc, cust = "STORE-SC", "CUST-SC"
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "Store SC", "type": "Store", "sellable": "Yes"})
	insert(cust, "Customer", "Active", map[string]interface{}{"code": cust, "name": "Asha", "phone": "9000000001"})
	insert("SHIRT", "Item", "Active", map[string]interface{}{"code": "SHIRT", "name": "SHIRT", "sale_price": 1000.0, "hsn_code": "6109", "tax_treatment": "Taxable", "gst_rate": 5.0})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "SHIRT", "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	sell := func(cart, customer string) {
		t.Helper()
		q, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: loc, CustomerID: customer, Lines: []QuoteLineRequest{{Sku: "SHIRT", Qty: 1}}})
		if err != nil {
			t.Fatalf("quote %s: %v", cart, err)
		}
		insert(cart, "POSCart", "Processing", map[string]interface{}{"location": loc, "payment_mode": "Cash", "customer_id": customer,
			"items": POSCartItemsFromQuote(q), "gst_breakdown": q.GSTBreakdown})
		if _, _, err := FinalizePOSCheckout(tenantID, cart, ""); err != nil {
			t.Fatalf("finalize %s: %v", cart, err)
		}
	}
	refundAs := func(cart, method string) (string, error) {
		t.Helper()
		cmd, err := CreateReturnRequestCommand(tenantID, "Customer Return", loc, cart, "", "cashier1", "sc:"+cart, "", []ReturnItemInput{{SKU: "SHIRT", Qty: 1}}, nil)
		if err != nil {
			t.Fatalf("return %s: %v", cart, err)
		}
		if err := approveReturnRequest(tenantID, cmd.ReturnRequestID, "manager1", false); err != nil {
			t.Fatal(err)
		}
		if err := ReceiveReturnRequest(tenantID, cmd.ReturnRequestID, "manager1"); err != nil {
			t.Fatal(err)
		}
		_, refundID, err := ApplyReturnQC(tenantID, cmd.ReturnRequestID, map[string]string{"SHIRT": "Sellable"}, "manager1")
		if err != nil {
			t.Fatal(err)
		}
		if err := approveRefundRequest(tenantID, refundID, "manager1", false); err != nil {
			t.Fatal(err)
		}
		return cmd.ReturnRequestID, ProcessRefundRequest(tenantID, refundID, "manager1", method)
	}
	sell("CART-SC-1", cust)
	retID, err := refundAs("CART-SC-1", RefundMethodStoreCredit)
	if err != nil {
		t.Fatalf("refund as store credit: %v", err)
	}
	if bal, _ := StoreCreditBalance(tenantID, cust); bal != 1000 {
		t.Errorf("store credit after the refund: %v, want 1000", bal)
	}
	var liab, cash int64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(credit - debit), 0) FROM ` + schemaName + `.gl_postings WHERE document_type = 'RefundRequest' AND account_code = '2650'`).Scan(&liab)
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(credit - debit), 0) FROM ` + schemaName + `.gl_postings WHERE document_type = 'RefundRequest' AND account_code = '1100'`).Scan(&cash)
	if liab != 100000 || cash != 0 {
		t.Errorf("refund GL: 2650 %d, cash %d - want 100000 to store credit and no cash", liab, cash)
	}
	var cn string
	_ = db.DB.QueryRow(`SELECT COALESCE(data->>'credit_note_no', '') FROM `+schemaName+`.documents WHERE id = $1`, retID).Scan(&cn)
	if !strings.HasPrefix(cn, "CN") || len(cn) > 16 {
		t.Errorf("the return's credit note number = %q", cn)
	}
	// Using it: the same customer only, within the manager limit.
	if _, err := PrepareStoreCreditTender(tenantID, RoleCashier, "CUST-OTHER", 100, 100000); err == nil {
		t.Errorf("another customer used this customer's store credit")
	}
	if err := SetSetting(tenantID, SettingStoreCreditApprovalAbove, "500", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareStoreCreditTender(tenantID, RoleCashier, cust, 800, 100000); err == nil {
		t.Errorf("a cashier used 800 past the 500 limit")
	}
	tenders, err := PrepareStoreCreditTender(tenantID, RoleStoreManager, cust, 800, 100000)
	if err != nil || len(tenders) != 1 {
		t.Fatalf("manager uses 800: %v %v", err, tenders)
	}
	tx, _ := db.DB.Begin()
	if err := redeemGiftCardTendersTx(tx, schemaName, tenantID, "CART-SC-USE", loc, "manager1", "Cash", tenders); err != nil {
		tx.Rollback()
		t.Fatalf("redeem: %v", err)
	}
	_ = tx.Commit()
	if bal, _ := StoreCreditBalance(tenantID, cust); bal != 200 {
		t.Errorf("after using 800: %v, want 200", bal)
	}
	// A walk-in bill: no store credit.
	sell("CART-SC-2", "")
	if _, err := refundAs("CART-SC-2", RefundMethodStoreCredit); err == nil {
		t.Errorf("a walk-in bill was refunded to store credit")
	}
	if rows, err := storeCreditLiabilityReport(tenantID, nil); err != nil || len(rows) != 1 || rows[0]["balance"] != 200.0 || rows[0]["gl_2650"] != 200.0 {
		t.Errorf("store credit report: %v %v", err, rows)
	}
}

// Stage 130.13 (80.7): the WMS gaps - holds at pick allocation, picks kept on
// the server, a wave sorted at once, a bin count moving the bin, cross-dock
// plans met on receipt, and the yard following the load.
func TestStage130WMSGaps(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(strings.ReplaceAll(q, "{s}", schemaName), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')`, id, doctype, raw, status)
	}
	exec(`CREATE TABLE IF NOT EXISTS {s}.wave_pick_confirmations (wave_id TEXT NOT NULL, line_key TEXT NOT NULL, qty INT NOT NULL DEFAULT 0,
		confirmed_by TEXT NOT NULL, confirmed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (wave_id, line_key))`)
	const loc = "WH-GAP"
	// (a) 10 in bins, 7 held: a pick takes at most 3.
	insert("BIN-G1", "Bin", "Active", map[string]interface{}{"bin_code": "BIN-G1", "location_code": loc})
	exec(`INSERT INTO {s}.bin_stock (bin_code, sku, location_code, qty, condition) VALUES ('BIN-G1', 'SKU-G', $1, 10, 'Good')`, loc)
	exec(`INSERT INTO {s}.inventory_availability (sku, location_code, on_hand, available, hold_qty) VALUES ('SKU-G', $1, 10, 10, 7)`, loc)
	got, short, err := AllocateFromStock(tenantID, "SKU-G", loc, 5)
	if err != nil || short != 2 || len(got) != 1 || got[0].Qty != 3 {
		t.Errorf("held stock: allocated %v short %d (%v) - want 3 and 2 short", got, short, err)
	}
	exec(`UPDATE {s}.inventory_availability SET hold_qty = 0 WHERE sku = 'SKU-G'`)
	if got, short, _ := AllocateFromStock(tenantID, "SKU-G", loc, 5); short != 0 || len(got) != 1 || got[0].Qty != 5 {
		t.Errorf("no hold: allocated %v short %d", got, short)
	}
	// (b) a confirmed line is kept, once.
	key := WavePickLineKey("SKU-G", "BIN-G1", "")
	if err := ConfirmWavePickLine(tenantID, "WAVE-G", key, 3, "picker1"); err != nil {
		t.Fatal(err)
	}
	_ = ConfirmWavePickLine(tenantID, "WAVE-G", key, 3, "picker2")
	if c, _ := WavePickConfirmations(tenantID, "WAVE-G"); c[key] != "picker1" {
		t.Errorf("confirmations %v, want the first picker kept", c)
	}
	// (d) a whole wave to the put-wall.
	if _, err := ProvisionSortSlots(tenantID, "PW-1", 4, "system"); err != nil {
		t.Fatal(err)
	}
	insert("FT-G1", "FulfillmentTask", "Picking", map[string]interface{}{"wave_id": "WAVE-G"})
	insert("FT-G2", "FulfillmentTask", "Pending", map[string]interface{}{"wave_id": "WAVE-G"})
	if slots, err := AssignWaveToSortSlots(tenantID, "PW-1", "WAVE-G", "system"); err != nil || len(slots) != 2 {
		t.Errorf("wave to slots: %d (%v), want 2", len(slots), err)
	}
	// (f) a bin count sets the bin.
	tx, _ := db.DB.Begin()
	if err := applyCycleCountToBinTx(tx, schemaName, "BIN-G1", "SKU-G", loc, 8); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	_ = tx.Commit()
	var binQty int
	_ = db.DB.QueryRow(`SELECT qty FROM ` + schemaName + `.bin_stock WHERE bin_code = 'BIN-G1' AND sku = 'SKU-G'`).Scan(&binQty)
	if binQty != 8 {
		t.Errorf("bin after the count: %d, want 8", binQty)
	}
	// (g) a cross-dock plan of the PO is met by its receipt.
	insert("XD-1", "CrossDockPlan", "Planned", map[string]interface{}{"po_id": "PO-G", "sku": "SKU-G", "qty": 4.0, "destination_type": "Location", "destination_ref": "STORE-9"})
	insert("XD-2", "CrossDockPlan", "Planned", map[string]interface{}{"po_id": "PO-OTHER", "sku": "SKU-G", "qty": 4.0})
	m, err := MatchCrossDockPlansOnReceipt(tenantID, "GRN-G", "PO-G", "", []string{"SKU-G"})
	if err != nil || len(m) != 1 || m[0].PlanID != "XD-1" || m[0].Destination != "Location STORE-9" {
		t.Errorf("cross-dock match: %v %v", m, err)
	}
	if again, _ := MatchCrossDockPlansOnReceipt(tenantID, "GRN-G2", "PO-G", "", []string{"SKU-G"}); len(again) != 0 {
		t.Errorf("a plan was matched twice: %v", again)
	}
	// (h) the trailer's appointment follows the load; departing checks it out.
	insert("APT-G", "Appointment", "Scheduled", map[string]interface{}{"appointment_type": "Outbound", "trailer_no": "TR-9", "status": "Scheduled"})
	insert("YARD-G", "YardCheckIn", "InYard", map[string]interface{}{"trailer_no": "TR-9", "status": "InYard"})
	loadID, err := CreateLoadingTask(tenantID, "DOOR-1", "TR-9", "", 0, "system")
	if err != nil {
		t.Fatal(err)
	}
	status := func(id string) string {
		var s string
		_ = db.DB.QueryRow(`SELECT status FROM `+schemaName+`.documents WHERE id = $1`, id).Scan(&s)
		return s
	}
	if status("APT-G") != "InProgress" {
		t.Errorf("appointment when loading starts: %s", status("APT-G"))
	}
	exec(`UPDATE {s}.documents SET status = 'Loaded', data = data || '{"status":"Loaded"}'::jsonb WHERE id = $1`, loadID)
	if err := DepartLoadingTask(tenantID, loadID, "system"); err != nil {
		t.Fatal(err)
	}
	if status("YARD-G") != "Departed" || status("APT-G") != "Completed" {
		t.Errorf("after departure: yard %s, appointment %s", status("YARD-G"), status("APT-G"))
	}
}

// Stage 130.14 (74.2.3b): an adjustment line by bin with its before / after
// quantity and a preview of its GL; the post refusing a stale snapshot until
// a manager takes it again; a Damaged condition move with no write-off; the
// linked reversal (once); the register and the loss dashboard.
func TestStage130StockAdjustmentRest(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	const sku, loc, bin = "T130-ADJ-SKU", "T130-ADJ-LOC", "T130-ADJ-BIN"
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(strings.ReplaceAll(q, "{s}", schema), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM {s}.documents WHERE doctype = 'InventoryAdjustment' AND data->>'location' = '` + loc + `'`,
			`DELETE FROM {s}.documents WHERE id IN ('` + sku + `', '` + loc + `')`,
			`DELETE FROM {s}.inventory_availability WHERE sku = '` + sku + `'`,
			`DELETE FROM {s}.bin_stock WHERE sku = '` + sku + `'`,
			`DELETE FROM {s}.approval_rules WHERE doctype = 'InventoryAdjustment'`,
		} {
			_, _ = db.DB.Exec(strings.ReplaceAll(q, "{s}", schema))
		}
	}
	cleanup()
	defer cleanup()
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', '{"code":"`+sku+`","name":"Adj test","cost_price":40}', 'Active', 'system')`, sku)
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', '{"code":"`+loc+`","name":"Adj store"}', 'Active', 'system')`, loc)
	exec(`INSERT INTO {s}.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)`, sku, loc)
	exec(`INSERT INTO {s}.bin_stock (bin_code, sku, location_code, qty, condition) VALUES ($1, $2, $3, 10, 'Good')`, bin, sku, loc)
	stock := func() (onHand, avail, dmg, inBin int) {
		_ = db.DB.QueryRow(`SELECT on_hand, available, COALESCE(damaged, 0) FROM `+schema+`.inventory_availability WHERE sku = $1 AND location_code = $2`, sku, loc).Scan(&onHand, &avail, &dmg)
		inBin = binGoodStock(schema, bin, sku)
		return
	}
	take2 := InventoryAdjustmentInput{Location: loc, Lines: []InventoryAdjustmentLine{{SKU: sku, Qty: -2, ReasonCode: "IA-DAMAGE", Bin: bin}}}
	pv, err := PreviewInventoryAdjustment("default", take2)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	lines, _ := pv["lines"].([]map[string]interface{})
	if len(lines) != 1 || lines[0]["before_qty"] != 10 || lines[0]["after_qty"] != 8 || pv["decrease_value"] != 80.0 || len(pv["gl"].([]map[string]interface{})) != 2 {
		t.Fatalf("preview = %v", pv)
	}
	first, status, err := CreateInventoryAdjustment("default", take2, "maker", RoleStoreManager)
	if err != nil || status != "Posted" {
		t.Fatalf("create: %s %v", status, err)
	}
	if on, _, _, b := stock(); on != 8 || b != 8 {
		t.Fatalf("after taking 2 off the bin: on hand %d, bin %d", on, b)
	}
	// Stale: the stock moves while the adjustment waits for approval.
	if _, err := UpsertApprovalRule("default", InventoryAdjustmentDoctype, 0, nil, RoleStoreManager, nil); err != nil {
		t.Fatal(err)
	}
	waiting, status, err := CreateInventoryAdjustment("default", InventoryAdjustmentInput{Location: loc, Lines: []InventoryAdjustmentLine{{SKU: sku, Qty: -1, ReasonCode: "IA-SHRINK"}}}, "maker", RoleStoreManager)
	if err != nil || status != "Pending Approval" {
		t.Fatalf("gated create: %s %v", status, err)
	}
	exec(`UPDATE {s}.inventory_availability SET on_hand = on_hand - 1, available = available - 1 WHERE sku = $1 AND location_code = $2`, sku, loc)
	if err := DecideApproval("default", InventoryAdjustmentDoctype, waiting, "checker", RoleStoreManager, loc, "Approved", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := PostInventoryAdjustment("default", waiting, "checker"); err == nil || !strings.Contains(err.Error(), "was 8") {
		t.Fatalf("a stale snapshot posted: %v", err)
	}
	if err := RefreshAdjustmentSnapshot("default", waiting, "manager1"); err != nil {
		t.Fatal(err)
	}
	if err := PostInventoryAdjustment("default", waiting, "manager1"); err != nil {
		t.Fatalf("post after refresh: %v", err)
	}
	if on, _, _, _ := stock(); on != 6 {
		t.Errorf("on hand after the refreshed post: %d, want 6", on)
	}
	exec(`DELETE FROM {s}.approval_rules WHERE doctype = 'InventoryAdjustment'`)
	// A damaged unit still owned: to Damaged, no write-off.
	if _, status, err := CreateInventoryAdjustment("default", InventoryAdjustmentInput{Location: loc, Lines: []InventoryAdjustmentLine{{SKU: sku, Qty: -3, ReasonCode: "IA-DAMAGE", Bin: bin, Condition: "Damaged"}}}, "maker", RoleStoreManager); err != nil || status != "Posted" {
		t.Fatalf("condition move: %s %v", status, err)
	}
	if on, avail, dmg, b := stock(); on != 6 || avail != 3 || dmg != 3 || b != 5 {
		t.Errorf("after moving 3 to Damaged: on hand %d avail %d damaged %d bin %d - want 6 / 3 / 3 / 5", on, avail, dmg, b)
	}
	// The linked reversal, once.
	rev, status, err := ReverseInventoryAdjustment("default", first, "counted wrong", "maker", RoleStoreManager)
	if err != nil || status != "Posted" {
		t.Fatalf("reverse: %s %s %v", rev, status, err)
	}
	if on, _, _, _ := stock(); on != 8 {
		t.Errorf("on hand after the reversal: %d, want 8", on)
	}
	if _, _, err := ReverseInventoryAdjustment("default", first, "again", "maker", RoleStoreManager); err == nil {
		t.Errorf("an adjustment was reversed twice")
	}
	reg, err := inventoryAdjustmentRegister("default", map[string]string{})
	found := 0
	for _, r := range reg {
		if r["sku"] == sku {
			found++
		}
	}
	if err != nil || found != 4 {
		t.Errorf("register lines for the test item: %d (%v), want 4", found, err)
	}
	loss, _ := inventoryLossByReason("default", map[string]string{})
	var lostUnits int
	for _, r := range loss {
		if r["location"] == loc {
			lostUnits += r["qty"].(int)
		}
	}
	if lostUnits != 3 {
		t.Errorf("losses at the test store: %d units, want 3 (2 damaged + 1 shrink; the condition move is not a loss)", lostUnits)
	}
}

// Stage 130.15 (74.2.4): request -> approve (another person) -> issue (stock
// out, cost to the department's cost centre) -> return Good / Damaged, never
// more than was issued; non-stock and backflushed items are refused, and a
// backflushed item is not entered by hand on a consumption entry either.
func TestStage130MaterialIssue(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	const sku, nonStock, flush, loc, cc, dept = "T130-MI-SKU", "T130-MI-SVC", "T130-MI-FLUSH", "T130-MI-LOC", "T130-MI-CC", "T130-MI-DEPT"
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(strings.ReplaceAll(q, "{s}", schema), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM {s}.gl_postings WHERE document_type IN ('MaterialIssue', 'MaterialReturn') AND document_id IN (SELECT id FROM {s}.documents WHERE doctype IN ('MaterialIssue', 'MaterialReturn') AND data->>'location' = '` + loc + `')`,
			`DELETE FROM {s}.approval_requests WHERE doctype = 'MaterialIssue'`,
			`DELETE FROM {s}.documents WHERE doctype IN ('MaterialIssue', 'MaterialReturn') AND data->>'location' = '` + loc + `'`,
			`DELETE FROM {s}.documents WHERE id IN ('` + sku + `', '` + nonStock + `', '` + flush + `', '` + loc + `', '` + cc + `', '` + dept + `')`,
			`DELETE FROM {s}.inventory_availability WHERE location_code = '` + loc + `'`,
			`DELETE FROM {s}.approval_rules WHERE doctype = 'MaterialIssue'`,
		} {
			_, _ = db.DB.Exec(strings.ReplaceAll(q, "{s}", schema))
		}
	}
	cleanup()
	defer cleanup()
	item := func(code, extra string) {
		exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')`, code,
			`{"code":"`+code+`","name":"MI `+code+`","cost_price":40,"valuation":"VALUATED"`+extra+`}`)
	}
	item(sku, "")
	item(nonStock, `,"stock_tracking":"NOT_TRACKED"`)
	item(flush, `,"consumption_method":"AUTO_BACKFLUSH"`)
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', '{"code":"`+loc+`","name":"MI store"}', 'Active', 'system')`, loc)
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'CostCenter', '{"code":"`+cc+`","name":"MI cost centre"}', 'Active', 'system'), ($2, 'Department', '{"code":"`+dept+`","name":"Housekeeping"}', 'Active', 'system')`, cc, dept)
	exec(`INSERT INTO {s}.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10), ($3, $2, 10, 10)`, sku, loc, flush)
	stock := func() (onHand, avail, qc int) {
		_ = db.DB.QueryRow(`SELECT on_hand, available, COALESCE(qc_hold, 0) FROM `+schema+`.inventory_availability WHERE sku = $1 AND location_code = $2`, sku, loc).Scan(&onHand, &avail, &qc)
		return
	}
	req := func(lines ...MaterialRequestLine) MaterialRequestInput {
		return MaterialRequestInput{Location: loc, Department: dept, CostCenter: cc, Purpose: "Weekly cleaning", Lines: lines}
	}
	for _, bad := range []string{nonStock, flush} {
		if _, _, err := CreateMaterialRequest("default", req(MaterialRequestLine{SKU: bad, Qty: 1}), "manager1", RoleStoreManager); err == nil {
			t.Errorf("%s was accepted on a material request", bad)
		}
	}
	if _, _, err := CreateMaterialRequest("default", MaterialRequestInput{Location: loc, Lines: []MaterialRequestLine{{SKU: sku, Qty: 1}}}, "manager1", RoleStoreManager); err == nil {
		t.Errorf("a request with no department or purpose was accepted")
	}
	if _, _, err := CreateMaterialRequest("default", MaterialRequestInput{Location: loc, Department: "Nobody", Purpose: "x", Lines: []MaterialRequestLine{{SKU: sku, Qty: 1}}}, "manager1", RoleStoreManager); err == nil {
		t.Errorf("a request for an unregistered department was accepted")
	}
	if _, err := UpsertApprovalRule("default", MaterialIssueDoctype, 0, nil, RoleStoreManager, nil); err != nil {
		t.Fatal(err)
	}
	id, status, err := CreateMaterialRequest("default", req(MaterialRequestLine{SKU: sku, Qty: 4}), "manager1", RoleStoreManager)
	if err != nil || status != "Pending Approval" {
		t.Fatalf("gated request: %s %v", status, err)
	}
	if err := IssueMaterial("default", id, "admin"); err == nil {
		t.Errorf("an unapproved request was issued")
	}
	if err := DecideApproval("default", MaterialIssueDoctype, id, "manager1", RoleStoreManager, loc, "Approved", ""); err == nil {
		t.Errorf("the requester approved their own material request")
	}
	if err := DecideApproval("default", MaterialIssueDoctype, id, "admin", RoleStoreManager, loc, "Approved", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := IssueMaterial("default", id, "admin"); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if on, avail, _ := stock(); on != 6 || avail != 6 {
		t.Fatalf("after issuing 4: on hand %d available %d, want 6 / 6", on, avail)
	}
	var debit float64
	var gotCC string
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit), 0), COALESCE(MAX(cost_center), '') FROM `+schema+`.gl_postings WHERE document_type = 'MaterialIssue' AND document_id = $1 AND account_code = $2`, id, AccountConsumablesExpense).Scan(&debit, &gotCC)
	if debit <= 0 || gotCC != cc {
		t.Errorf("issue GL: expense debit %v on cost centre %q, want > 0 on %q", debit, gotCC, cc)
	}
	if err := IssueMaterial("default", id, "admin"); err != nil {
		t.Errorf("issuing again: %v", err)
	}
	if on, _, _ := stock(); on != 6 {
		t.Errorf("issuing again moved stock: on hand %d", on)
	}
	mr, err := ReturnMaterial("default", id, []MaterialReturnLine{{SKU: sku, Qty: 1}, {SKU: sku, Qty: 1, Condition: "Damaged"}}, "admin")
	if err != nil {
		t.Fatalf("return: %v", err)
	}
	if on, avail, qc := stock(); on != 8 || avail != 7 || qc != 1 {
		t.Errorf("after returning 1 good + 1 damaged: on hand %d available %d qc %d, want 8 / 7 / 1", on, avail, qc)
	}
	var credit float64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(credit), 0) FROM `+schema+`.gl_postings WHERE document_type = 'MaterialReturn' AND document_id = $1 AND account_code = $2`, mr, AccountConsumablesExpense).Scan(&credit)
	if credit <= 0 || credit*2 != debit {
		t.Errorf("return GL: expense credit %v, want half of the issue's %v", credit, debit)
	}
	if _, err := ReturnMaterial("default", id, []MaterialReturnLine{{SKU: sku, Qty: 3}}, "admin"); err == nil {
		t.Errorf("more came back than was issued")
	}
	if _, err := ReturnMaterial("default", id, []MaterialReturnLine{{SKU: sku, Qty: 2}}, "admin"); err != nil {
		t.Fatalf("return the rest: %v", err)
	}
	var final string
	_ = db.DB.QueryRow(`SELECT status FROM `+schema+`.documents WHERE doctype = 'MaterialIssue' AND id = $1`, id).Scan(&final)
	if final != "Returned" {
		t.Errorf("issue status after everything came back: %q", final)
	}
	if _, err := PostConsumptionEntry("default", "admin", ConsumptionEntryRequest{Location: loc, Mode: "manual", Lines: []ConsumptionEntryLine{{SKU: flush, IssuedQty: 1}}}); err == nil || !strings.Contains(err.Error(), "backflush") {
		t.Errorf("a backflushed item was entered by hand: %v", err)
	}
	byCC, err := consumptionByCostCentre("default", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	ccFound := false
	for _, r := range byCC {
		if r["cost_center"] == cc {
			ccFound = true
		}
	}
	reg, err := materialIssueRegister("default", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	types := map[interface{}]int{}
	for _, r := range reg {
		if r["location"] == loc {
			types[r["type"]]++
		}
	}
	if !ccFound || len(types) != 2 {
		t.Errorf("reports: cost centre row %v, register types %v", ccFound, types)
	}
}

// Stage 130.16 (74.1.4): the first issued print freezes what it printed and
// later prints render that copy as reprints; reprint control asks for the
// permission, a reason and - past the free reprints - an approved request
// used once; an amendment prints as the next revision; a draft freezes
// nothing.
func TestStage130PrintIssue(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	const id, draft, txn = "T130-PRN-1", "T130-PRN-2", "MaterialIssue"
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(strings.ReplaceAll(q, "{s}", schema), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM {s}.print_job_log WHERE doc_id IN ('` + id + `', '` + draft + `')`,
			`DELETE FROM {s}.print_snapshots WHERE doc_id IN ('` + id + `', '` + draft + `')`,
			`DELETE FROM {s}.documents WHERE doctype = 'ReprintRequest' AND data->>'doc_id' = '` + id + `'`,
			`DELETE FROM {s}.documents WHERE id IN ('` + id + `', '` + draft + `')`,
			`DELETE FROM {s}.approval_rules WHERE doctype = 'ReprintRequest'`,
		} {
			_, _ = db.DB.Exec(strings.ReplaceAll(q, "{s}", schema))
		}
		_ = SetSetting("default", SettingReprintControl, "false", "test")
	}
	cleanup()
	defer cleanup()
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, $2, '{"code":"T130-PRN-1","location":"HO","department":"HK","purpose":"First purpose","items":"[]","status":"Approved"}', 'Approved', 'admin'),
		($3, $2, '{"code":"T130-PRN-2","location":"HO","department":"HK","purpose":"Draft","items":"[]","status":"Draft"}', 'Draft', 'admin')`, id, txn, draft)
	issued := func(doc, user string) PrintDocument {
		t.Helper()
		fresh, err := BuildPrintDocument("default", txn, doc, "", false)
		if err != nil {
			t.Fatal(err)
		}
		out, err := IssuedPrint("default", fresh, user)
		if err != nil || out.Issue == nil {
			t.Fatalf("issued print: %v", err)
		}
		return out
	}
	print := func(p DocumentPrint) error {
		p.Txn, p.Doctype, p.DocID = txn, txn, firstNonEmpty(p.DocID, id)
		if p.Action == "" {
			p.Action = PrintActionPrint
		}
		return RecordDocumentPrint("default", "manager1", p)
	}
	if d := issued(id, "manager1"); !d.Issue.Issued || d.Issue.Reprint || d.Issue.Revision != 1 || d.Issue.PrintedBy != "manager1" || d.Issue.PrintedAt == "" {
		t.Fatalf("first print: %+v", d.Issue)
	}
	if err := print(DocumentPrint{Channel: "browser"}); err != nil {
		t.Fatalf("original: %v", err)
	}
	var outcome string
	var reprinted bool
	_ = db.DB.QueryRow(`SELECT outcome, is_reprint FROM `+schema+`.print_job_log WHERE doc_id = $1 ORDER BY id DESC LIMIT 1`, id).Scan(&outcome, &reprinted)
	if outcome != PrintOutcomeSentToBrowser || reprinted {
		t.Errorf("original logged as %q reprint=%v", outcome, reprinted)
	}
	// The masters move on; the reprint is still the original.
	exec(`UPDATE {s}.documents SET data = data || '{"purpose":"Changed later"}' WHERE id = $1`, id)
	d := issued(id, "manager1")
	if !d.Issue.Reprint || d.Issue.PrintedBefore != 1 || d.Fields["purpose"] != "First purpose" || d.Issue.FirstPrintedBy != "manager1" {
		t.Fatalf("reprint: %+v purpose %v", d.Issue, d.Fields["purpose"])
	}
	// Both stamps on the client's clock: the original was moments ago.
	first, _ := time.Parse("2006-01-02 15:04", d.Issue.FirstPrintedAt)
	now, _ := time.Parse("2006-01-02 15:04", d.Issue.PrintedAt)
	if gap := now.Sub(first); gap < 0 || gap > 5*time.Minute {
		t.Errorf("first printed %s, now %s - the clocks disagree", d.Issue.FirstPrintedAt, d.Issue.PrintedAt)
	}
	if err := print(DocumentPrint{Channel: "qz"}); err != nil {
		t.Fatalf("an uncontrolled reprint was refused: %v", err)
	}
	_ = db.DB.QueryRow(`SELECT outcome, is_reprint FROM `+schema+`.print_job_log WHERE doc_id = $1 ORDER BY id DESC LIMIT 1`, id).Scan(&outcome, &reprinted)
	if outcome != PrintOutcomeSubmitted || !reprinted {
		t.Errorf("reprint logged as %q reprint=%v", outcome, reprinted)
	}
	// Reprint control: permission, reason, then approval past one free reprint.
	if err := SetSetting("default", SettingReprintControl, "true", "test"); err != nil {
		t.Fatal(err)
	}
	if err := print(DocumentPrint{ReprintReason: "torn"}); err == nil {
		t.Errorf("a reprint without the permission was allowed")
	}
	if err := print(DocumentPrint{MayReprint: true}); err == nil {
		t.Errorf("a controlled reprint without a reason was allowed")
	}
	if err := print(DocumentPrint{MayReprint: true, ReprintReason: "torn", Action: PrintActionReprint}); err == nil || !strings.Contains(err.Error(), "approval") {
		t.Fatalf("past the free reprint, no approval asked: %v", err)
	}
	if _, err := RequestReprint("default", txn, id, "torn", "manager1", "manager1", RoleStoreManager); err == nil {
		t.Errorf("a reprint request went out with no approver set")
	}
	if _, err := UpsertApprovalRule("default", ReprintRequestDoctype, 0, nil, RoleStoreManager, nil); err != nil {
		t.Fatal(err)
	}
	req, err := RequestReprint("default", txn, id, "torn", "manager1", "manager1", RoleStoreManager)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if d := issued(id, "manager1"); d.Issue.PendingRequest != req || d.Issue.ApprovedRequest != "" {
		t.Errorf("waiting request not shown: %+v", d.Issue)
	}
	if err := DecideApproval("default", ReprintRequestDoctype, req, "manager1", RoleStoreManager, "HO", "Approved", ""); err == nil {
		t.Errorf("the requester approved their own reprint")
	}
	if err := DecideApproval("default", ReprintRequestDoctype, req, "admin", RoleStoreManager, "HO", "Approved", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if d := issued(id, "manager1"); d.Issue.ApprovedRequest != req {
		t.Fatalf("approved request not offered: %+v", d.Issue)
	}
	if err := print(DocumentPrint{MayReprint: true, ReprintReason: "torn", Action: PrintActionReprint, ApprovalID: req, CheckOnly: true}); err != nil {
		t.Fatalf("check before printing: %v", err)
	}
	if err := print(DocumentPrint{MayReprint: true, ReprintReason: "torn", Action: PrintActionReprint, ApprovalID: req}); err != nil {
		t.Fatalf("approved reprint: %v", err)
	}
	var approvedBy string
	_ = db.DB.QueryRow(`SELECT COALESCE(approved_by, '') FROM `+schema+`.print_job_log WHERE doc_id = $1 ORDER BY id DESC LIMIT 1`, id).Scan(&approvedBy)
	if approvedBy != "admin" {
		t.Errorf("approved reprint logged approver %q", approvedBy)
	}
	if err := print(DocumentPrint{MayReprint: true, ReprintReason: "torn", Action: PrintActionReprint, ApprovalID: req}); err == nil {
		t.Errorf("one approval printed twice")
	}
	// An amendment is the next revision, printed as an original.
	exec(`UPDATE {s}.documents SET data = data || '{"amendment_reason":"qty corrected"}' WHERE id = $1`, id)
	if d := issued(id, "manager1"); d.Issue.Reprint || d.Issue.Revision != 2 || d.Fields["purpose"] != "Changed later" {
		t.Errorf("amended: %+v purpose %v", d.Issue, d.Fields["purpose"])
	}
	// A draft issues nothing.
	if d := issued(draft, "manager1"); d.Issue.Issued || d.Issue.Revision != 0 {
		t.Errorf("draft: %+v", d.Issue)
	}
	if err := print(DocumentPrint{DocID: draft}); err != nil {
		t.Fatal(err)
	}
	var frozen int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM `+schema+`.print_snapshots WHERE doc_id = $1`, draft).Scan(&frozen)
	if frozen != 0 {
		t.Errorf("a draft print was frozen")
	}
}

// Stage 130.16 (74.3.2): PO terms required at approval when asked for, kept
// when approved, logged before / after on an amendment; a GRN refuses POs in
// two currencies and a vendor invoice already booked as a bill; the four
// reports read the terms, discrepancies, transport and GRNI.
func TestStage130POTermsGRNLogistics(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	const vendor, sku, poINR, poUSD, grn, bill = "T130-POT-V", "T130-POT-SKU", "T130-POT-PO1", "T130-POT-PO2", "T130-POT-GRN", "T130-POT-BILL"
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(strings.ReplaceAll(q, "{s}", schema), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'admin')`, id, doctype, string(raw), status)
	}
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM {s}.gl_postings WHERE document_id IN ('` + grn + `', '` + bill + `')`,
			`DELETE FROM {s}.documents WHERE id IN ('` + vendor + `', '` + sku + `', '` + poINR + `', '` + poUSD + `', '` + grn + `', '` + bill + `')`,
		} {
			_, _ = db.DB.Exec(strings.ReplaceAll(q, "{s}", schema))
		}
		_ = SetSetting("default", SettingPOTermsRequired, "false", "test")
	}
	cleanup()
	defer cleanup()
	code := func(err error) string {
		var verr *ValidationError
		if errors.As(err, &verr) {
			return verr.Code
		}
		return ""
	}
	terms := map[string]interface{}{"payment_term": "NET30", "delivery_terms": "Door delivery", "expected_delivery_date": "2099-01-01", "freight_by": "Vendor"}
	if err := validatePOTermsForApproval("default", map[string]interface{}{}); err != nil {
		t.Errorf("terms required with the setting off: %v", err)
	}
	if err := SetSetting("default", SettingPOTermsRequired, "true", "test"); err != nil {
		t.Fatal(err)
	}
	if got := code(validatePOTermsForApproval("default", map[string]interface{}{"payment_term": "NET30"})); got != "PURCHA-1316" {
		t.Errorf("missing terms: %q", got)
	}
	expired := map[string]interface{}{"quote_valid_until": "2000-01-01"}
	for k, v := range terms {
		expired[k] = v
	}
	if got := code(validatePOTermsForApproval("default", expired)); got != "PURCHA-1316" {
		t.Errorf("an expired quote went for approval: %q", got)
	}
	// A draft never needs them; approving does, keeps them and names the term.
	draft := map[string]interface{}{"status": "Draft"}
	if err := preparePOTerms("default", "", nil, draft); err != nil {
		t.Errorf("a draft needed its terms: %v", err)
	}
	approving := map[string]interface{}{"status": "Approved"}
	for k, v := range terms {
		approving[k] = v
	}
	if err := preparePOTerms("default", "Draft", map[string]interface{}{"status": "Draft"}, approving); err != nil {
		t.Fatalf("approve with terms: %v", err)
	}
	kept, _ := approving["approved_terms"].(map[string]interface{})
	if approving["payment_terms"] != "Net 30 days" || kept == nil || kept["delivery_terms"] != "Door delivery" {
		t.Errorf("approved: payment_terms %v, approved_terms %v", approving["payment_terms"], kept)
	}
	// An approved PO's terms change only with a reason, and the change is logged.
	prior := map[string]interface{}{"status": "Approved"}
	for k, v := range approving {
		prior[k] = v
	}
	if got := code(preparePOTerms("default", "Approved", prior, map[string]interface{}{"status": "Approved", "freight_by": "Buyer"})); got != "PURCHA-0085" {
		t.Errorf("terms changed without a reason: %q", got)
	}
	amend := map[string]interface{}{"status": "Approved", "freight_by": "Buyer", "amendment_reason": "vendor asked"}
	if err := preparePOTerms("default", "Approved", prior, amend); err != nil {
		t.Fatal(err)
	}
	var log []map[string]interface{}
	_ = json.Unmarshal([]byte(strField(amend, "amendment_log")), &log)
	if len(log) != 1 || fmt.Sprint(log[0]["changes"]) != "map[freight_by:[Vendor Buyer]]" {
		t.Errorf("amendment log: %v", log)
	}
	if err := preparePOTerms("default", "Approved", prior, map[string]interface{}{"status": "Approved", "freight_by": "Vendor", "freight_amount": 0}); err != nil {
		t.Errorf("an unchanged save of an approved PO needed a reason: %v", err)
	}
	// The approval engine runs the same check before routing.
	insert(poINR, "PurchaseOrder", "Draft", map[string]interface{}{"code": poINR, "vendor": vendor, "total_amount": 400, "items": `[{"sku":"` + sku + `","qty":1,"rate":400}]`})
	if got := code(SubmitForApproval("default", "PurchaseOrder", poINR, "manager1", RoleStoreManager)); got != "PURCHA-1316" {
		t.Errorf("submitted without terms: %q", got)
	}
	_ = SetSetting("default", SettingPOTermsRequired, "false", "test")
	// GRN: one currency per receipt; a vendor bill booked on its own is a duplicate.
	exec(`UPDATE {s}.documents SET status = 'Approved' WHERE id = $1`, poINR)
	insert(poUSD, "PurchaseOrder", "Approved", map[string]interface{}{"code": poUSD, "vendor": vendor, "currency": "USD", "total_amount": 5, "items": `[{"sku":"` + sku + `","qty":1,"rate":5}]`})
	p := map[string]interface{}{"po_id": poINR, "po_ids": poINR + "," + poUSD,
		"received_items": `[{"po_id":"` + poINR + `","sku":"` + sku + `","qty":1},{"po_id":"` + poUSD + `","sku":"` + sku + `","qty":1}]`}
	if got := code(PrepareGRNPricing("default", true, p)); got != "GOODSR-1317" {
		t.Errorf("two currencies on one receipt: %q", got)
	}
	insert(bill, "VendorInvoice", "Posted", map[string]interface{}{"invoice_number": "INV-130", "vendor_id": vendor, "financial_year": "2099-00"})
	if got := code(checkDuplicateVendorInvoice("default", "", vendor, "inv-130")); got != "GOODSR-0941" {
		t.Errorf("an invoice booked as a bill was received again: %q", got)
	}
	// Reports.
	insert(grn, "GRN", "Posted", map[string]interface{}{"code": grn, "vendor": vendor, "location": "HO", "vendor_invoice_no": "INV-131", "lr_awb_no": "LR-9", "transporter": "Fast Freight",
		"package_count": 3, "received_items": `[{"sku":"` + sku + `","qty":10,"received_qty":10,"accepted_qty":8,"rejected_qty":2,"invoice_qty":12}]`})
	exec(`UPDATE {s}.documents SET data = data || jsonb_build_object('grn_id', $2::text) WHERE id = $1`, bill, grn)
	if err := PostDoubleEntry("default", "GRN", grn, map[string]int64{"1200": 80000}, map[string]int64{AccountAPSuspense: 80000}, "", "T130POT:GRN"); err != nil {
		t.Fatal(err)
	}
	if err := PostDoubleEntry("default", "VendorInvoice", bill, map[string]int64{AccountAPSuspense: 50000}, map[string]int64{"1100": 50000}, "", "T130POT:BILL"); err != nil {
		t.Fatal(err)
	}
	find := func(rows []map[string]interface{}, key, want string) map[string]interface{} {
		for _, r := range rows {
			if fmt.Sprint(r[key]) == want {
				return r
			}
		}
		return nil
	}
	grni, err := grniReport("default", map[string]string{})
	if r := find(grni, "grn", grn); err != nil || r == nil || r["accepted_value"] != 800.0 || r["invoiced_value"] != 500.0 || r["open_value"] != 300.0 {
		t.Errorf("GRNI: %v %v", r, err)
	}
	disc, err := grnDiscrepancyByVendor("default", map[string]string{})
	if r := find(disc, "vendor", vendor); err != nil || r == nil || r["rejected"] != 2.0 || r["short_vs_invoice"] != 2.0 || r["rejected_pct"] != 20.0 {
		t.Errorf("discrepancy: %v %v", r, err)
	}
	reg, err := vendorDocumentTransportRegister("default", map[string]string{})
	if r := find(reg, "grn", grn); err != nil || r == nil || r["lr_awb_no"] != "LR-9" || r["package_count"] != 3.0 {
		t.Errorf("transport register: %v %v", r, err)
	}
	exec(`UPDATE {s}.documents SET data = data || '{"payment_term":"NET30"}' WHERE id = $1`, poINR)
	if due := POPaymentDueDate("default", poINR, "2099-01-10"); due != "2099-02-09" {
		t.Errorf("bill due by the payment term: %q", due)
	}
	terms2, err := poDeliveryAndTerms("default", map[string]string{})
	if r := find(terms2, "po", poINR); err != nil || r == nil {
		t.Errorf("PO terms report: %v %v", r, err)
	}
}

// Stage 130.16 (74.3.1): a returnable challan moves goods to "with party"
// (on_hand and value unchanged), takes partial returns by condition and by
// the serials that went out, a retried return is taken once, what will not
// come back is written off only on another person's approval, and a
// non-returnable challan books its cost at dispatch.
func TestStage130DeliveryChallan(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	const sku, ser, svc, loc = "T130-DC-SKU", "T130-DC-SER", "T130-DC-SVC", "T130-DC-LOC"
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(strings.ReplaceAll(q, "{s}", schema), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM {s}.gl_postings WHERE document_type IN ('DeliveryChallan', 'ChallanWriteOff') AND document_id IN (SELECT id FROM {s}.documents WHERE doctype IN ('DeliveryChallan', 'ChallanWriteOff') AND data->>'location' = '` + loc + `')`,
			`DELETE FROM {s}.documents WHERE doctype IN ('DeliveryChallan', 'ChallanWriteOff') AND data->>'location' = '` + loc + `'`,
			`DELETE FROM {s}.documents WHERE id IN ('` + sku + `', '` + ser + `', '` + svc + `', '` + loc + `')`,
			`DELETE FROM {s}.inventory_availability WHERE location_code = '` + loc + `'`,
			`DELETE FROM {s}.approval_rules WHERE doctype = 'ChallanWriteOff'`,
		} {
			_, _ = db.DB.Exec(strings.ReplaceAll(q, "{s}", schema))
		}
		_ = SetSetting("default", SettingEWBThreshold, "50000", "test")
	}
	cleanup()
	defer cleanup()
	for _, it := range [][2]string{{sku, ``}, {ser, ``}, {svc, `,"stock_tracking":"NOT_TRACKED"`}} {
		exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')`, it[0],
			`{"code":"`+it[0]+`","name":"DC `+it[0]+`","cost_price":50,"hsn_code":"7113","valuation":"VALUATED"`+it[1]+`}`)
	}
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', '{"code":"`+loc+`","name":"DC store","type":"Store"}', 'Active', 'system')`, loc)
	exec(`INSERT INTO {s}.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 20, 20), ($3, $2, 20, 20)`, sku, loc, ser)
	stock := func(item string) (onHand, avail, withParty, dmg, qc int) {
		_ = db.DB.QueryRow(`SELECT on_hand, available, with_party, COALESCE(damaged, 0), COALESCE(qc_hold, 0) FROM `+schema+`.inventory_availability WHERE sku = $1 AND location_code = $2`, item, loc).
			Scan(&onHand, &avail, &withParty, &dmg, &qc)
		return
	}
	jw := ChallanInput{Location: loc, Purpose: "Job work", PartyType: "Job worker", PartyName: "Polish Works", PartyState: "29",
		Lines: []ChallanLineInput{{SKU: sku, Qty: 5, BatchNo: "L1"}, {SKU: ser, Qty: 2, Serials: []string{"S1", "S2"}}}}
	if _, _, err := CreateDeliveryChallan("default", jw, "manager1", RoleStoreManager); err == nil {
		t.Errorf("a returnable challan with no return date was saved")
	}
	jw.ExpectedReturn = "2099-01-31"
	bad := jw
	bad.Lines = []ChallanLineInput{{SKU: svc, Qty: 1}}
	if _, _, err := CreateDeliveryChallan("default", bad, "manager1", RoleStoreManager); err == nil {
		t.Errorf("a non-stock item went on a challan")
	}
	id, status, err := CreateDeliveryChallan("default", jw, "manager1", RoleStoreManager)
	if err != nil || status != "Approved" {
		t.Fatalf("create: %s %v", status, err)
	}
	if err := CancelDeliveryChallan("default", id, "manager1", ""); err == nil {
		t.Errorf("a cancellation without a reason")
	}
	if err := DispatchDeliveryChallan("default", id, "manager1", ChallanDispatch{TransportMode: "Road", VehicleNo: "ka01ab1234"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if on, av, wp, _, _ := stock(sku); on != 20 || av != 15 || wp != 5 {
		t.Fatalf("after dispatch: on hand %d available %d with party %d - want 20 / 15 / 5", on, av, wp)
	}
	if err := CancelDeliveryChallan("default", id, "manager1", "changed mind"); err == nil {
		t.Errorf("a dispatched challan was cancelled")
	}
	ret := []ChallanReturnLine{{SKU: sku, BatchNo: "L1", Qty: 2}, {SKU: sku, BatchNo: "L1", Qty: 1, Condition: "Damaged", Reason: "scratched"}}
	if st, err := ReceiveChallanReturn("default", id, "manager1", "R1", ret); err != nil || st != "Partly Returned" {
		t.Fatalf("return: %s %v", st, err)
	}
	if _, err := ReceiveChallanReturn("default", id, "manager1", "R1", ret); err != nil {
		t.Fatalf("retried return: %v", err)
	}
	if on, av, wp, dmg, _ := stock(sku); on != 20 || av != 17 || wp != 2 || dmg != 1 {
		t.Errorf("after 2 good + 1 damaged (retried once): on hand %d available %d with party %d damaged %d - want 20 / 17 / 2 / 1", on, av, wp, dmg)
	}
	if _, err := ReceiveChallanReturn("default", id, "manager1", "R2", []ChallanReturnLine{{SKU: sku, Qty: 3}}); err == nil {
		t.Errorf("more came back than was out")
	}
	if _, err := ReceiveChallanReturn("default", id, "manager1", "R3", []ChallanReturnLine{{SKU: ser, Serials: []string{"S9"}}}); err == nil {
		t.Errorf("a serial that never went out came back")
	}
	if _, err := ReceiveChallanReturn("default", id, "manager1", "R4", []ChallanReturnLine{{SKU: ser, Serials: []string{"S1"}, Condition: "Rejected", Reason: "wrong finish"}}); err != nil {
		t.Fatalf("serial return: %v", err)
	}
	if _, _, wp, _, qc := stock(ser); wp != 1 || qc != 1 {
		t.Errorf("serial S1 rejected: with party %d qc %d", wp, qc)
	}
	// What will not come back: only through another person's approval.
	if _, err := RequestChallanWriteOff("default", id, "manager1", RoleStoreManager, ChallanWriteOffInput{SKU: sku, Qty: 2, Kind: challanKindLost, Reason: "lost in transit"}); err == nil {
		t.Errorf("a write-off went out with no approver set")
	}
	if _, err := UpsertApprovalRule("default", ChallanWriteOffDoctype, 0, nil, RoleStoreManager, nil); err != nil {
		t.Fatal(err)
	}
	wo, err := RequestChallanWriteOff("default", id, "manager1", RoleStoreManager, ChallanWriteOffInput{SKU: sku, BatchNo: "L1", Qty: 2, Kind: challanKindLost, Reason: "lost in transit"})
	if err != nil {
		t.Fatalf("write-off request: %v", err)
	}
	if _, _, wp, _, _ := stock(sku); wp != 2 {
		t.Errorf("a write-off moved stock before approval")
	}
	if err := DecideApproval("default", ChallanWriteOffDoctype, wo, "manager1", RoleStoreManager, loc, "Approved", ""); err == nil {
		t.Errorf("the requester approved their own write-off")
	}
	if err := DecideApproval("default", ChallanWriteOffDoctype, wo, "admin", RoleStoreManager, loc, "Approved", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if on, _, wp, _, _ := stock(sku); on != 18 || wp != 0 {
		t.Errorf("after the lost write-off: on hand %d with party %d - want 18 / 0", on, wp)
	}
	var loss int64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(credit), 0) FROM `+schema+`.gl_postings WHERE document_type = 'ChallanWriteOff' AND document_id = $1 AND account_code = '1200'`, wo).Scan(&loss)
	if loss != 10000 {
		t.Errorf("write-off GL: Cr 1200 %d paise, want 10000", loss)
	}
	wo2, err := RequestChallanWriteOff("default", id, "manager1", RoleStoreManager, ChallanWriteOffInput{SKU: ser, Serials: []string{"S2"}, Kind: challanKindKept, Reason: "worker kept the sample"})
	if err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval("default", ChallanWriteOffDoctype, wo2, "admin", RoleStoreManager, loc, "Approved", ""); err != nil {
		t.Fatal(err)
	}
	var final string
	_ = db.DB.QueryRow(`SELECT status FROM `+schema+`.documents WHERE id = $1`, id).Scan(&final)
	if final != "Closed" {
		t.Errorf("challan with everything back or written off: %q", final)
	}
	// Non-returnable: stock and cost leave at dispatch; the e-way bill rule.
	if err := SetSetting("default", SettingEWBThreshold, "100", "test"); err != nil {
		t.Fatal(err)
	}
	gift, status, err := CreateDeliveryChallan("default", ChallanInput{Location: loc, Purpose: "Non-returnable", PartyName: "Charity", Lines: []ChallanLineInput{{SKU: sku, Qty: 3}}}, "manager1", RoleStoreManager)
	if err != nil || status != "Approved" {
		t.Fatalf("non-returnable: %s %v", status, err)
	}
	if data, _, _ := fetchDocData("default", DeliveryChallanDoctype, gift); strField(data, "ewb_required") != "Yes" {
		t.Errorf("150 of goods over a 100 threshold: e-way bill %q", strField(data, "ewb_required"))
	}
	if err := DispatchDeliveryChallan("default", gift, "manager1", ChallanDispatch{}); err != nil {
		t.Fatalf("non-returnable dispatch: %v", err)
	}
	var cost int64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit), 0) FROM `+schema+`.gl_postings WHERE document_type = 'DeliveryChallan' AND document_id = $1`, gift).Scan(&cost)
	if on, _, _, _, _ := stock(sku); on != 15 || cost != 15000 {
		t.Errorf("non-returnable: on hand %d (want 15), cost booked %d paise (want 15000)", on, cost)
	}
	// The print: consignee and Rule 55's three copies.
	doc, err := BuildPrintDocument("default", PrintTxnDeliveryChallan, id, "", false)
	if err != nil || doc.Party.Name != "Polish Works" || len(doc.Lines) != 2 {
		t.Fatalf("print: %v party %q lines %d", err, doc.Party.Name, len(doc.Lines))
	}
	if pp, _ := doc.Fields["print_profile"].(*PrintProfile); pp == nil || len(pp.CopyLabels) != 3 {
		t.Errorf("challan copies: %v", doc.Fields["print_profile"])
	}
	reg, err := deliveryChallanRegister("default", map[string]string{})
	rows := 0
	for _, r := range reg {
		if r["location"] == loc {
			rows++
		}
	}
	if err != nil || rows != 3 {
		t.Errorf("register rows %d %v", rows, err)
	}
	if _, err := challanOutstandingByParty("default", nil); err != nil {
		t.Errorf("outstanding: %v", err)
	}
}

// Stage 130.16 (74.3.3): a two-line job work order goes out on a challan
// into custody; a line that is transformed consumes its material and brings
// the output in at the material's value (rejected output to QC hold), a
// same-item line comes back through the challan; scrap and loss above the
// tolerance wait for approval, within it they are booked at once; the
// charge is booked once per bill; overdue orders are flagged once.
func TestStage130JobWork(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	const gold, ring, pol, loc, order = "T130-JW-GOLD", "T130-JW-RING", "T130-JW-POL", "T130-JW-LOC", "T130-JW-1"
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(strings.ReplaceAll(q, "{s}", schema), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM {s}.gl_postings WHERE document_id = '` + order + `' OR document_id IN (SELECT id FROM {s}.documents WHERE doctype IN ('DeliveryChallan', 'ChallanWriteOff') AND data->>'location' = '` + loc + `')`,
			`DELETE FROM {s}.documents WHERE doctype IN ('DeliveryChallan', 'ChallanWriteOff') AND data->>'location' = '` + loc + `'`,
			`DELETE FROM {s}.documents WHERE id IN ('` + gold + `', '` + ring + `', '` + pol + `', '` + loc + `', '` + order + `')`,
			`DELETE FROM {s}.inventory_availability WHERE location_code = '` + loc + `'`,
			`DELETE FROM {s}.item_cost WHERE item_code = '` + ring + `'`,
			`DELETE FROM {s}.approval_rules WHERE doctype = 'ChallanWriteOff'`,
		} {
			_, _ = db.DB.Exec(strings.ReplaceAll(q, "{s}", schema))
		}
		_ = SetSetting("default", SettingJobworkLossTolerance, "1", "test")
	}
	cleanup()
	defer cleanup()
	for _, it := range [][2]string{{gold, "100"}, {ring, "0"}, {pol, "40"}} {
		exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')`, it[0],
			`{"code":"`+it[0]+`","name":"JW `+it[0]+`","cost_price":`+it[1]+`,"valuation":"VALUATED"}`)
	}
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', '{"code":"`+loc+`","name":"JW store","type":"Store"}', 'Active', 'system')`, loc)
	exec(`INSERT INTO {s}.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 100, 100), ($3, $2, 10, 10)`, gold, loc, pol)
	lines := `[{"sku":"` + gold + `","qty":50,"net_weight":50,"fine_weight":45.8,"output_sku":"` + ring + `","output_per_unit":0.2},{"sku":"` + pol + `","qty":10,"gross_weight":20}]`
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'SubcontractOrder', $2, 'Draft', 'admin')`, order,
		`{"code":"`+order+`","vendor_id":"T130-JW-VENDOR","location":"`+loc+`","due_date":"2000-01-01","lines":`+strconvQuote(lines)+`}`)
	stock := func(item string) (onHand, avail, withParty, qc int) {
		_ = db.DB.QueryRow(`SELECT on_hand, available, with_party, COALESCE(qc_hold, 0) FROM `+schema+`.inventory_availability WHERE sku = $1 AND location_code = $2`, item, loc).
			Scan(&onHand, &avail, &withParty, &qc)
		return
	}
	if st, err := SendJobWork("default", order, "admin", RoleStoreManager, ChallanDispatch{VehicleNo: "KA01"}); err != nil || st != "Sent" {
		t.Fatalf("send: %s %v", st, err)
	}
	if on, av, wp, _ := stock(gold); on != 100 || av != 50 || wp != 50 {
		t.Fatalf("gold sent: on hand %d available %d with party %d - want 100 / 50 / 50", on, av, wp)
	}
	five, one := 5, 1
	recv := []JobWorkReceiveLine{{LineNo: 1, AcceptedQty: 40, RejectedQty: 5, OutputRejectedQty: &one, Reason: "porosity", NetWeight: 44.5}, {LineNo: 2, AcceptedQty: 6, RejectedQty: 2, Reason: "dull finish"}}
	_ = five
	if st, err := ReceiveJobWork("default", order, "admin", RoleStoreManager, "K1", recv); err != nil || st != "Partly Received" {
		t.Fatalf("receive: %s %v", st, err)
	}
	if _, err := ReceiveJobWork("default", order, "admin", RoleStoreManager, "K1", recv); err != nil {
		t.Fatalf("retried receive: %v", err)
	}
	if on, _, wp, _ := stock(gold); on != 55 || wp != 5 {
		t.Errorf("gold after 45 consumed (retried once): on hand %d with party %d - want 55 / 5", on, wp)
	}
	if on, av, _, qc := stock(ring); on != 9 || av != 8 || qc != 1 {
		t.Errorf("rings: on hand %d available %d qc %d - want 9 / 8 / 1", on, av, qc)
	}
	if _, av, wp, qc := stock(pol); av != 6 || wp != 2 || qc != 2 {
		t.Errorf("polish line: available %d with party %d qc %d - want 6 / 2 / 2", av, wp, qc)
	}
	var ringCost int64
	_ = db.DB.QueryRow(`SELECT COALESCE(avg_unit_cost_paise, 0) FROM `+schema+`.item_cost WHERE item_code = $1`, ring).Scan(&ringCost)
	if ringCost != 50000 {
		t.Errorf("a ring carries its gold: %d paise, want 50000 (45 x 100 / 9)", ringCost)
	}
	if _, err := ReceiveJobWork("default", order, "admin", RoleStoreManager, "K2", []JobWorkReceiveLine{{LineNo: 1, AcceptedQty: 6}}); err == nil {
		t.Errorf("more came back than was out")
	}
	// Above the 1% tolerance: approval first.
	if _, err := ReceiveJobWork("default", order, "admin", RoleStoreManager, "K3", []JobWorkReceiveLine{{LineNo: 1, LossQty: 1, Reason: "melting loss"}}); err == nil {
		t.Errorf("a loss over tolerance was booked with no approver set")
	}
	if _, err := UpsertApprovalRule("default", ChallanWriteOffDoctype, 0, nil, RoleStoreManager, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ReceiveJobWork("default", order, "manager1", RoleStoreManager, "K4", []JobWorkReceiveLine{{LineNo: 1, LossQty: 1, Reason: "melting loss"}}); err != nil {
		t.Fatalf("loss for approval: %v", err)
	}
	if _, _, wp, _ := stock(gold); wp != 5 {
		t.Errorf("a loss waiting for approval moved stock: with party %d", wp)
	}
	data, _, _ := fetchDocData("default", "SubcontractOrder", order)
	bal, _ := jobWorkState(data)
	if len(bal[0].WriteOffs) != 1 || bal[0].PendingLoss != 1 {
		t.Fatalf("pending loss: %+v", bal[0])
	}
	if err := DecideApproval("default", ChallanWriteOffDoctype, bal[0].WriteOffs[0], "admin", RoleStoreManager, loc, "Approved", ""); err != nil {
		t.Fatalf("approve loss: %v", err)
	}
	if on, _, wp, _ := stock(gold); on != 54 || wp != 4 {
		t.Errorf("after the approved loss: on hand %d with party %d - want 54 / 4", on, wp)
	}
	// Within tolerance: booked at once.
	if err := SetSetting("default", SettingJobworkLossTolerance, "10", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := ReceiveJobWork("default", order, "admin", RoleStoreManager, "K5", []JobWorkReceiveLine{{LineNo: 1, ScrapQty: 2, Reason: "filings"}}); err != nil {
		t.Fatalf("scrap within tolerance: %v", err)
	}
	if _, _, wp, _ := stock(gold); wp != 2 {
		t.Errorf("scrap within tolerance not booked: with party %d", wp)
	}
	var jwLoss int64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit), 0) FROM `+schema+`.gl_postings WHERE account_code = '5430' AND document_type = 'ChallanWriteOff'
		AND document_id IN (SELECT id FROM `+schema+`.documents WHERE doctype = 'ChallanWriteOff' AND data->>'location' = $1)`, loc).Scan(&jwLoss)
	if jwLoss != 30000 {
		t.Errorf("job-work loss booked %d paise, want 30000 (3 x 100)", jwLoss)
	}
	// The charge: once per bill, spread by what was accepted.
	if err := PostJobWorkCharge("default", order, "admin", "B1", 460); err != nil {
		t.Fatalf("charge: %v", err)
	}
	if err := PostJobWorkCharge("default", order, "admin", "B1", 460); err == nil {
		t.Errorf("one bill booked twice")
	}
	data, _, _ = fetchDocData("default", "SubcontractOrder", order)
	bal, _ = jobWorkState(data)
	if bal[0].Charges != 400 || bal[1].Charges != 60 {
		t.Errorf("charge spread: %v / %v, want 400 / 60", bal[0].Charges, bal[1].Charges)
	}
	// Reports and the overdue flag.
	out, err := jobWorkOutstanding("default", nil)
	found := false
	for _, r := range out {
		if r["order"] == order && r["line_no"] == 1 && r["open"] == 2 {
			found = true
		}
	}
	if err != nil || !found {
		t.Errorf("outstanding: %v %v", out, err)
	}
	yields, _ := jobWorkYield("default", nil)
	for _, r := range yields {
		if r["order"] == order && r["sku"] == gold && r["yield_pct"] != 100.0 {
			t.Errorf("gold yield: %v", r)
		}
	}
	var events []string
	if err := JobWorkOverdue("default", func(event, id string, _ map[string]string) {
		if id == order {
			events = append(events, event)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := JobWorkOverdue("default", func(event, id string, _ map[string]string) {
		if id == order {
			events = append(events, event)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0] != "Job Work Overdue" {
		t.Errorf("overdue flagged: %v", events)
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Stage 130.16 (74.3.4): template versions (and their approval), profile
// precedence by store / entity / channel, the GSTIN rule for tax documents,
// the receipt's tenders, and the print reports.
func TestStage130PrintProfilesRest(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	const tpl, loc, ent, other = "T130-PT-1", "T130-PT-LOC", "T130-PT-ENT", "T130-PT-ENT2"
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(strings.ReplaceAll(q, "{s}", schema), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM {s}.print_template_versions WHERE template_id = '` + tpl + `'`,
			`DELETE FROM {s}.documents WHERE id IN ('` + tpl + `', '` + loc + `', '` + ent + `', '` + other + `', 'T130-SI-1') OR (doctype = 'PrintProfile' AND id LIKE 'T130-PP-%')`,
		} {
			_, _ = db.DB.Exec(strings.ReplaceAll(q, "{s}", schema))
		}
		_ = SetSetting("default", SettingTemplateApproval, "false", "test")
		_ = SetSetting("default", SettingTaxDocumentNeedsGSTIN, "false", "test")
	}
	cleanup()
	defer cleanup()
	// Versions.
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'PrintTemplate', '{"code":"T130-PT-1","name":"T","transaction":"GRN","layout":"L1"}', 'Active', 'admin')`, tpl)
	save := func(user string, isCreate bool, layout string) map[string]interface{} {
		t.Helper()
		prior, _, _ := fetchDocData("default", "PrintTemplate", tpl)
		payload := map[string]interface{}{"layout": layout}
		if err := PreparePrintTemplateVersion("default", tpl, user, isCreate, prior, payload); err != nil {
			t.Fatal(err)
		}
		if err := mergeDocData(schema, "PrintTemplate", tpl, payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	save("manager1", true, "L1")
	save("manager1", false, "L2")
	vs, _ := ListPrintTemplateVersions("default", tpl)
	if len(vs) != 2 || vs[0].Status != "Active" || vs[1].Status != "Superseded" {
		t.Fatalf("versions: %+v", vs)
	}
	if err := SetSetting("default", SettingTemplateApproval, "true", "test"); err != nil {
		t.Fatal(err)
	}
	if p := save("manager1", false, "L3"); p["layout"] != "L2" || p["pending_version"] != 3 {
		t.Errorf("a change needing approval went live: %v", p)
	}
	if err := DecidePrintTemplateVersion("default", tpl, 3, true, "manager1", ""); err == nil {
		t.Errorf("the person who saved a change approved it")
	}
	if err := DecidePrintTemplateVersion("default", tpl, 3, true, "admin", "ok"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if d, _, _ := fetchDocData("default", "PrintTemplate", tpl); strField(d, "layout") != "L3" || numFromInterface(d["version"]) != 3 || d["pending_version"] != nil {
		t.Errorf("after approval: %v", d)
	}
	_ = SetSetting("default", SettingTemplateApproval, "false", "test")
	if n, err := RestorePrintTemplateVersion("default", tpl, 1, "admin"); err != nil || n != 4 {
		t.Fatalf("restore: %d %v", n, err)
	}
	if d, _, _ := fetchDocData("default", "PrintTemplate", tpl); strField(d, "layout") != "L1" {
		t.Errorf("restored layout: %v", strField(d, "layout"))
	}
	if lbl := templateVersionLabel("default", tpl); lbl != tpl+" v4" {
		t.Errorf("print log label %q", lbl)
	}
	// Profile precedence.
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'LegalEntity', '{"code":"T130-PT-ENT","name":"Unregistered Co"}', 'Active', 'system'),
		($2, 'LegalEntity', '{"code":"T130-PT-ENT2","name":"Registered Co","gstin":"29ABCDE1234F1Z5"}', 'Active', 'system'),
		($3, 'Location', '{"code":"T130-PT-LOC","name":"PT store","legal_entity":"T130-PT-ENT"}', 'Active', 'system')`, ent, other, loc)
	prof := func(id, data string) {
		exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'PrintProfile', $2, 'Active', 'system')`, id, data)
	}
	prof("T130-PP-1", `{"code":"T130-PP-1","transaction":"All","copy_labels":"ALL"}`)
	prof("T130-PP-2", `{"code":"T130-PP-2","legal_entity":"T130-PT-ENT","copy_labels":"ENTITY"}`)
	prof("T130-PP-3", `{"code":"T130-PP-3","legal_entity":"T130-PT-ENT","channel":"POS","copy_labels":"ENTITY+POS"}`)
	prof("T130-PP-4", `{"code":"T130-PP-4","legal_entity":"T130-PT-ENT2","copy_labels":"OTHER"}`)
	if p := ResolvePrintProfile("default", PrintTxnPOSTaxInvoice, "POSCart", loc); p == nil || p.Code != "T130-PP-3" {
		t.Errorf("till receipt at the store: %+v", p)
	}
	if p := ResolvePrintProfile("default", PrintTxnGRN, "GRN", loc); p == nil || p.Code != "T130-PP-2" {
		t.Errorf("GRN at the store: %+v", p)
	}
	prof("T130-PP-5", `{"code":"T130-PP-5","store":"T130-PT-LOC","copy_labels":"STORE"}`)
	if p := ResolvePrintProfile("default", PrintTxnPOSTaxInvoice, "POSCart", loc); p == nil || p.Code != "T130-PP-5" {
		t.Errorf("a store's own profile beats the entity's: %+v", p)
	}
	// The GSTIN rule: a registered business may not print a tax invoice from a store with no GSTIN.
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ('T130-SI-1', 'SalesInvoice', '{"invoice_number":"T130-SI-1","location":"T130-PT-LOC","total_amount":100,"items":"[]"}', 'Approved', 'admin')`)
	if _, err := BuildPrintDocument("default", PrintTxnSalesInvoice, "T130-SI-1", "", false); err != nil {
		t.Fatalf("the rule is off by default: %v", err)
	}
	if err := SetSetting("default", SettingTaxDocumentNeedsGSTIN, "true", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPrintDocument("default", PrintTxnSalesInvoice, "T130-SI-1", "", false); err == nil || !strings.Contains(err.Error(), "GSTIN") {
		t.Errorf("a tax invoice printed from a store with no GSTIN: %v", err)
	}
	if _, err := BuildPrintDocument("default", PrintTxnGRN, "T130-SI-1", "", false); err != nil && strings.Contains(err.Error(), "GSTIN") {
		t.Errorf("a non-tax document was held to the GSTIN rule: %v", err)
	}
	// The receipt's tenders.
	tenders, points := receiptTenders(map[string]interface{}{"allocation": []interface{}{
		map[string]interface{}{"sku": "A", "points": 10, "tenders": map[string]interface{}{"Cash": 30000, "UPI": 10000}},
		map[string]interface{}{"sku": "B", "tenders": map[string]interface{}{"Cash": 20000}}}})
	if len(tenders) != 2 || tenders[0]["mode"] != "Cash" || tenders[0]["amount"] != 500.0 || points != 10 {
		t.Errorf("tenders: %v points %d", tenders, points)
	}
	// Reports.
	cov, err := printTemplateCoverage("default", nil)
	dcRow := false
	for _, r := range cov {
		// The challan has a row and a copy set (an all-documents profile, or the built-in Rule 55 set).
		if r["doctype"] == "DeliveryChallan" && fmt.Sprint(r["copy_labels"]) != "" {
			dcRow = true
		}
	}
	if err != nil || !dcRow {
		t.Errorf("coverage: %v %v", err, dcRow)
	}
	if _, err := printFailures("default", map[string]string{}); err != nil {
		t.Errorf("failures: %v", err)
	}
	miss, err := missingRegulatoryPrintData("default", nil)
	found := false
	for _, r := range miss {
		if r["code"] == ent && strings.Contains(fmt.Sprint(r["missing"]), "GSTIN") {
			found = true
		}
	}
	if err != nil || !found {
		t.Errorf("missing data: %v %v", err, found)
	}
}

// Stage 130.16 (74.4, e-way bills): the decision with its evidence, the
// checks before generating, generate / lookup-after-timeout / refusal,
// vehicle update, extension window, cancel window, recording a portal bill,
// expiry and reconciliation.
func TestStage130EWayBill(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	const sku, svc, loc, ent = "T130-EWB-SKU", "T130-EWB-SVC", "Q9", "T130-EWB-ENT"
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(strings.ReplaceAll(q, "{s}", schema), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM {s}.ewb_documents WHERE doc_id IN (SELECT id FROM {s}.documents WHERE data->>'location' = '` + loc + `')`,
			`DELETE FROM {s}.documents WHERE data->>'location' = '` + loc + `' AND doctype IN ('DeliveryChallan', 'SalesInvoice')`,
			`DELETE FROM {s}.documents WHERE id IN ('` + sku + `', '` + svc + `', '` + loc + `', '` + ent + `')`,
			`DELETE FROM {s}.inventory_availability WHERE location_code = '` + loc + `'`,
		} {
			_, _ = db.DB.Exec(strings.ReplaceAll(q, "{s}", schema))
		}
		_ = SetSetting("default", SettingEWBProvider, "", "test")
	}
	cleanup()
	defer cleanup()
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES
		($1, 'Item', '{"code":"T130-EWB-SKU","name":"Bangle","cost_price":20000,"hsn_code":"7113","valuation":"VALUATED"}', 'Active', 'system'),
		($2, 'Item', '{"code":"T130-EWB-SVC","name":"Polishing service","hsn_code":"998898"}', 'Active', 'system'),
		($3, 'LegalEntity', '{"code":"T130-EWB-ENT","name":"EWB Co","gstin":"29AAPFU0939F1ZV","state_code":"29","pincode":"560001"}', 'Active', 'system'),
		($4, 'Location', '{"code":"Q9","name":"EWB store","type":"Store","legal_entity":"T130-EWB-ENT","pincode":"560001"}', 'Active', 'system')`, sku, svc, ent, loc)
	exec(`INSERT INTO {s}.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 50, 50)`, sku, loc)
	challan := func(qty int, party string) string {
		t.Helper()
		id, _, err := CreateDeliveryChallan("default", ChallanInput{Location: loc, Purpose: "Exhibition", PartyName: party, PartyState: "27", ExpectedReturn: "2099-01-01",
			Lines: []ChallanLineInput{{SKU: sku, Qty: qty}}}, "manager1", RoleStoreManager)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	big, small, other, portal := challan(3, "Expo Mumbai"), challan(1, "Small"), challan(5, "Expo Pune"), challan(4, "Expo Nagpur")
	_ = small
	rec, err := EnsureEWBRecord("default", DeliveryChallanDoctype, big)
	if err != nil || rec.Status != EWBReady || rec.Decision["interstate"] != true || rec.Decision["rule_version"] != ewbDecisionVersion {
		t.Fatalf("60,000 of goods to another state: %+v %v", rec, err)
	}
	if r, _ := EnsureEWBRecord("default", DeliveryChallanDoctype, small); r == nil || r.Status != EWBNotRequired {
		t.Errorf("20,000 of goods needed a bill: %+v", r)
	}
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ('T130-EWB-SI', 'SalesInvoice', $1, 'Approved', 'admin')`,
		`{"invoice_number":"T130-EWB-SI","location":"`+loc+`","place_of_supply":"27","total_amount":90000,"items":"[{\"sku\":\"`+svc+`\",\"hsn_code\":\"998898\",\"amount\":90000}]"}`)
	if r, err := EnsureEWBRecord("default", "SalesInvoice", "T130-EWB-SI"); err != nil || r.Status != EWBNotRequired {
		t.Errorf("a services-only invoice needed a bill: %+v %v", r, err)
	}
	if _, err := GenerateEWB("default", DeliveryChallanDoctype, big, "manager1", EWBPartB{VehicleNo: "KA01AB1234", DistanceKm: 450}); err == nil {
		t.Errorf("generated with no provider connected")
	}
	if err := SetSetting("default", SettingEWBProvider, "fake", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateEWB("default", DeliveryChallanDoctype, big, "manager1", EWBPartB{VehicleNo: "KA01AB1234"}); err == nil || !strings.Contains(err.Error(), "distance") {
		t.Errorf("generated with no distance: %v", err)
	}
	rec, err = GenerateEWB("default", DeliveryChallanDoctype, big, "manager1", EWBPartB{VehicleNo: "ka01ab1234", DistanceKm: 450})
	if err != nil || rec.Status != EWBGenerated || len(rec.EWBNo) != 12 || rec.ValidUpto == nil {
		t.Fatalf("generate: %+v %v", rec, err)
	}
	if days := rec.ValidUpto.Sub(time.Now()).Hours() / 24; days < 2 || days > 4 {
		t.Errorf("450 km should be valid about 3 days: %.1f", days)
	}
	if r, err := GenerateEWB("default", DeliveryChallanDoctype, other, "manager1", EWBPartB{VehicleNo: "TIMEOUT", DistanceKm: 100}); err != nil || r.Status != EWBGenerated {
		t.Errorf("a timeout was not looked up before a retry: %+v %v", r, err)
	}
	if r, err := GenerateEWB("default", DeliveryChallanDoctype, portal, "manager1", EWBPartB{VehicleNo: "FAIL", DistanceKm: 100}); err == nil || r == nil || r.Status != EWBFailed {
		t.Errorf("a refusal was not Failed: %+v %v", r, err)
	}
	if _, err := UpdateEWBVehicle("default", DeliveryChallanDoctype, big, "manager1", EWBPartB{VehicleNo: "XX"}); err == nil {
		t.Errorf("a bad vehicle number was taken")
	}
	if r, err := UpdateEWBVehicle("default", DeliveryChallanDoctype, big, "manager1", EWBPartB{VehicleNo: "MH12CD5678"}); err != nil || r.VehicleNo != "MH12CD5678" {
		t.Errorf("vehicle update: %+v %v", r, err)
	}
	if _, err := ExtendEWB("default", DeliveryChallanDoctype, big, "manager1", 100, "breakdown"); err == nil {
		t.Errorf("extended days before expiry")
	}
	exec(`UPDATE {s}.ewb_documents SET valid_upto = $2 WHERE doctype = 'DeliveryChallan' AND doc_id = $1`, big, time.Now().UTC().Add(2*time.Hour))
	if r, err := ExtendEWB("default", DeliveryChallanDoctype, big, "manager1", 100, "breakdown"); err != nil || r.Extensions != 1 {
		t.Errorf("extend near expiry: %+v %v", r, err)
	}
	exec(`UPDATE {s}.ewb_documents SET ewb_date = $2 WHERE doctype = 'DeliveryChallan' AND doc_id = $1`, other, time.Now().UTC().Add(-30*time.Hour))
	if _, err := CancelEWB("default", DeliveryChallanDoctype, other, "manager1", "wrong party"); err == nil {
		t.Errorf("cancelled after 24 hours")
	}
	if r, err := CancelEWB("default", DeliveryChallanDoctype, big, "manager1", "order cancelled"); err != nil || r.Status != EWBCancelled {
		t.Errorf("cancel within 24 hours: %+v %v", r, err)
	}
	if _, err := RecordPortalEWB("default", DeliveryChallanDoctype, portal, "manager1", "12345", time.Now().Add(48*time.Hour), EWBPartB{}); err == nil {
		t.Errorf("a 5-digit e-way bill number was recorded")
	}
	if r, err := RecordPortalEWB("default", DeliveryChallanDoctype, portal, "manager1", "381234567890", time.Now().Add(48*time.Hour), EWBPartB{VehicleNo: "KA05EF1111"}); err != nil || r.Source != "portal" || r.Status != EWBGenerated {
		t.Errorf("portal bill: %+v %v", r, err)
	}
	// Expiry and reconciliation.
	exec(`UPDATE {s}.ewb_documents SET valid_upto = $2, ewb_date = $3 WHERE doctype = 'DeliveryChallan' AND doc_id = $1`, portal, time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(-50*time.Hour))
	if err := RefreshEWBQueue("default"); err != nil {
		t.Fatal(err)
	}
	if r, _ := loadEWB(db.DB, schema, DeliveryChallanDoctype, portal); r == nil || r.Status != EWBExpired {
		t.Errorf("a lapsed bill is not Expired: %+v", r)
	}
	if r, _ := loadEWB(db.DB, schema, DeliveryChallanDoctype, other); r == nil || r.Status != EWBReconciled {
		t.Errorf("a provider bill was not reconciled: %+v", r)
	}
	rows, _, err := ListEWBs("default", "all")
	found := 0
	for _, r := range rows {
		if r.DocID == big || r.DocID == other || r.DocID == portal {
			found++
		}
	}
	if err != nil || found != 3 {
		t.Errorf("workbench rows %d %v", found, err)
	}
	if reg, err := ewbRegister("default", map[string]string{}); err != nil || len(reg) == 0 {
		t.Errorf("register: %v", err)
	}
}

// Stage 130.16 (74.5): the registers, the reconciliation run and its custody
// check, the exception worklist, the SoD catalog, and every new control
// shipping off (old clients unchanged, NFR-15).
func TestStage130TransactionControls(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	const sku, loc = "T130-TC-SKU", "T130-TC-LOC"
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(strings.ReplaceAll(q, "{s}", schema), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM {s}.inventory_availability WHERE location_code = '` + loc + `'`,
			`DELETE FROM {s}.print_job_log WHERE doc_id = 'T130-TC-DOC'`,
			`DELETE FROM {s}.ewb_documents WHERE doc_id = 'T130-TC-EWB'`,
		} {
			_, _ = db.DB.Exec(strings.ReplaceAll(q, "{s}", schema))
		}
	}
	cleanup()
	defer cleanup()
	// Off by default.
	defaults := map[string]string{}
	for _, d := range ListSettingDefinitions() {
		defaults[d.Key] = d.Default
	}
	for _, k := range []string{SettingReprintControl, SettingTemplateApproval, SettingTaxDocumentNeedsGSTIN, SettingPOTermsRequired, SettingItemReleaseApproval} {
		if defaults[k] != "false" {
			t.Errorf("%s ships %q, want off", k, defaults[k])
		}
	}
	if defaults[SettingEWBProvider] != "" || defaults[SettingReceiptReturnPolicy] != "" {
		t.Errorf("e-way bill provider / return policy ship set")
	}
	// Registers.
	for _, id := range []string{"advance-outstanding-ageing", "transfer-shortage-by-reason", "print-reprint-audit"} {
		if _, _, _, err := RunReport("default", id, RoleSuperAdmin, "system", map[string]string{}); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	for i := 0; i < 4; i++ {
		exec(`INSERT INTO {s}.print_job_log (job_type, document_ref, copies, printed_by, status, txn, doctype, doc_id, doc_number, print_action, is_reprint, reprint_reason)
			VALUES ('Document', 'T130-TC-DOC', 1, 'manager1', 'Submitted', 'GRN', 'GRN', 'T130-TC-DOC', 'T130-TC-DOC', $1, $2, $3)`,
			map[bool]string{true: "Print", false: "Reprint"}[i == 0], i > 0, map[bool]string{true: "", false: "torn"}[i == 0 || i == 3])
	}
	audit, _ := printReprintAudit("default", map[string]string{})
	flagged := false
	for _, r := range audit {
		if r["document"] == "T130-TC-DOC" && strings.Contains(fmt.Sprint(r["anomaly"]), "3 times") && strings.Contains(fmt.Sprint(r["anomaly"]), "no reason") {
			flagged = true
		}
	}
	if !flagged {
		t.Errorf("repeat reprints not flagged: %v", audit)
	}
	// The liability control totals tie to their GL accounts.
	if _, _, _, c, err := RunReportWithControlTotals(context.Background(), "default", "store-credit-liability", RoleSuperAdmin, "system", map[string]string{}); err != nil || c == nil || len(c.LedgerAccounts) == 0 {
		t.Errorf("store credit control totals: %+v %v", c, err)
	}
	// The custody check catches goods "with a party" that no challan explains.
	exec(`INSERT INTO {s}.inventory_availability (sku, location_code, on_hand, available, with_party) VALUES ($1, $2, 10, 5, 5)`, sku, loc)
	if ok, diff, detail, err := checkCustody(schema); err != nil || ok || diff < 5 || !strings.Contains(detail, sku) {
		t.Errorf("custody mismatch not caught: %v %v %q %v", ok, diff, detail, err)
	}
	results, err := RunReconciliationChecks("default", true)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, r := range results {
		ids[r.CheckID] = true
	}
	for _, id := range []string{"custody-with-party", "stock-pools", "print-evidence", "inventory-valuation", "store-credit-liability"} {
		if !ids[id] {
			t.Errorf("reconciliation run has no %s", id)
		}
	}
	// The exception worklist.
	exec(`INSERT INTO {s}.ewb_documents (doctype, doc_id, doc_number, status, last_error) VALUES ('DeliveryChallan', 'T130-TC-EWB', 'T130-TC-EWB', 'Failed', 'refused')`)
	inbox, err := ListTaskInbox("default", TaskInboxScope{UserID: "admin", Role: RoleSuperAdmin, Exceptions: true})
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, it := range inbox.Items {
		if it.Source == "Exceptions" {
			titles = append(titles, it.Title)
		}
	}
	if !strings.Contains(strings.Join(titles, "|"), "E-way bills failed") || !strings.Contains(strings.Join(titles, "|"), "Reconciliation checks not tied") {
		t.Errorf("exception worklist: %v", titles)
	}
	// SoD.
	have := map[string]bool{}
	for _, c := range SoDConflicts() {
		have[c.ID] = true
	}
	for _, id := range []string{"challan-and-write-off", "template-and-reprint-approval", "material-request-and-issue-approval"} {
		if !have[id] {
			t.Errorf("SoD catalog has no %s", id)
		}
	}
}

// Stage 130.16 (74.5.4): twenty returns of one unit each, at once, against a
// challan with ten out - exactly ten are taken, the stock pools end right.
func TestStage130ChallanReturnRace(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	const sku, loc = "T130-RACE-SKU", "T130-RACE-LOC"
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(strings.ReplaceAll(q, "{s}", schema), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM {s}.documents WHERE doctype = 'DeliveryChallan' AND data->>'location' = '` + loc + `'`,
			`DELETE FROM {s}.documents WHERE id IN ('` + sku + `', '` + loc + `')`,
			`DELETE FROM {s}.inventory_availability WHERE location_code = '` + loc + `'`,
		} {
			_, _ = db.DB.Exec(strings.ReplaceAll(q, "{s}", schema))
		}
	}
	cleanup()
	defer cleanup()
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', '{"code":"T130-RACE-SKU","name":"Race","cost_price":10}', 'Active', 'system'),
		($2, 'Location', '{"code":"T130-RACE-LOC","name":"Race store","type":"Store"}', 'Active', 'system')`, sku, loc)
	exec(`INSERT INTO {s}.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)`, sku, loc)
	id, _, err := CreateDeliveryChallan("default", ChallanInput{Location: loc, Purpose: "Repair", PartyName: "Fixers", ExpectedReturn: "2099-01-01",
		Lines: []ChallanLineInput{{SKU: sku, Qty: 10}}}, "manager1", RoleStoreManager)
	if err != nil {
		t.Fatal(err)
	}
	if err := DispatchDeliveryChallan("default", id, "manager1", ChallanDispatch{}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	taken := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := ReceiveChallanReturn("default", id, "manager1", fmt.Sprintf("race-%d", i), []ChallanReturnLine{{SKU: sku, Qty: 1}}); err == nil {
				mu.Lock()
				taken++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	var avail, withParty int
	_ = db.DB.QueryRow(`SELECT available, with_party FROM `+schema+`.inventory_availability WHERE sku = $1 AND location_code = $2`, sku, loc).Scan(&avail, &withParty)
	if taken != 10 || avail != 10 || withParty != 0 {
		t.Errorf("20 parallel returns of 10 out: %d taken, available %d, with party %d - want 10 / 10 / 0", taken, avail, withParty)
	}
}

// Stage 130.17 (127.3.2): the standard prints that were still generic.
func TestStage130StandardPrints(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	ids := []string{"T130-SP-SI", "T130-SP-VI", "T130-SP-PP", "T130-SP-BOM", "T130-SP-PO", "T130-SP-PI", "T130-SP-CC1", "T130-SP-CC2", "T130-SP-PR", "T130-SP-RFQ", "T130-SP-JO", "T130-SP-ITEM"}
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id = ANY($1)`, pqStringArray(ids))
	}
	cleanup()
	defer cleanup()
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'admin')`, id, doctype, string(raw), status); err != nil {
			t.Fatal(err)
		}
	}
	insert("T130-SP-ITEM", "Item", "Active", map[string]interface{}{"code": "T130-SP-ITEM", "name": "Brass wire"})
	insert("T130-SP-SI", "SalesInvoice", "Approved", map[string]interface{}{"invoice_number": "T130-SP-SI", "customer": "T130-SP-CUST", "location": "HO", "total_amount": 1000.0,
		"invoice_date": "2026-10-01", "settlements": []interface{}{map[string]interface{}{"reference": "r1", "kind": "Receipt", "amount": 400.0, "date": "2026-10-05", "by": "admin"}}})
	insert("T130-SP-VI", "VendorInvoice", "Approved", map[string]interface{}{"invoice_number": "T130-SP-VI", "vendor_id": "T130-SP-VEND", "invoice_amount": 700.0, "invoice_date": "2026-10-02",
		"payments": []interface{}{map[string]interface{}{"reference": "p1", "kind": "Payment", "amount": 700.0, "date": "2026-10-06", "by": "admin"}}})
	build := func(txn, id string) PrintDocument {
		t.Helper()
		d, err := BuildPrintDocument("default", txn, id, "", false)
		if err != nil {
			t.Fatalf("%s %s: %v", txn, id, err)
		}
		return d
	}
	if d := build(PrintTxnReceiptVoucher, "T130-SP-SI~r1"); d.Title != "Receipt Voucher" || len(d.Lines) != 1 || d.Lines[0]["amount"] != 400.0 || d.Number != "T130-SP-SI/1" {
		t.Errorf("receipt voucher: %s %s %v", d.Title, d.Number, d.Lines)
	}
	if _, err := BuildPrintDocument("default", PrintTxnReceiptVoucher, "T130-SP-SI~nope", "", false); err == nil {
		t.Errorf("a voucher for a receipt that is not there")
	}
	if d := build(PrintTxnPaymentVoucher, "T130-SP-VI~p1"); d.Title != "Payment Voucher" || d.Lines[0]["amount"] != 700.0 {
		t.Errorf("payment voucher: %v", d.Lines)
	}
	if d := build(PrintTxnCustomerStatement, "T130-SP-CUST"); d.Fields["closing_balance"] != 600.0 || len(d.Lines) != 2 {
		t.Errorf("customer statement: %v %v", d.Fields["closing_balance"], d.Lines)
	}
	if d := build(PrintTxnVendorStatement, "T130-SP-VEND"); d.Fields["closing_balance"] != 0.0 {
		t.Errorf("vendor statement: %v", d.Fields["closing_balance"])
	}
	insert("T130-SP-PP", "PaymentProposal", "Approved", map[string]interface{}{"proposal_number": "T130-SP-PP", "invoice_ids": "T130-SP-VI", "total_amount": 700.0})
	if d := build("PaymentProposal", "T130-SP-PP"); d.Title != "Payment Advice" || len(d.Lines) != 1 || d.Lines[0]["invoice_number"] != "T130-SP-VI" {
		t.Errorf("payment advice: %s %v", d.Title, d.Lines)
	}
	insert("T130-SP-BOM", "BOM", "Active", map[string]interface{}{"code": "T130-SP-BOM", "parent_item": "X", "components": `[{"sku":"T130-SP-ITEM","qty":2.2,"scrap_pct":0}]`})
	insert("T130-SP-PO", "ProductionOrder", "Draft", map[string]interface{}{"code": "T130-SP-PO", "bom_id": "T130-SP-BOM", "quantity": 4.0, "location": "HO"})
	if d := build("ProductionOrder", "T130-SP-PO"); d.Title != "Material Issue Slip" || len(d.Lines) != 1 || d.Lines[0]["qty"] != 9.0 || d.Lines[0]["item_name"] != "Brass wire" {
		t.Errorf("material issue slip: %s %v", d.Title, d.Lines)
	}
	insert("T130-SP-PI", "PhysicalInventory", "Open", map[string]interface{}{"code": "T130-SP-PI", "location": "HO"})
	insert("T130-SP-CC1", "CycleCountLine", "Open", map[string]interface{}{"physical_inventory": "T130-SP-PI", "bin": "A1", "sku": "T130-SP-ITEM", "system_qty": 12.0})
	insert("T130-SP-CC2", "CycleCountLine", "Open", map[string]interface{}{"physical_inventory": "T130-SP-PI", "bin": "A2", "sku": "T130-SP-ITEM", "system_qty": 3.0})
	if d := build("PhysicalInventory", "T130-SP-PI"); d.Title != "Count Sheet" || len(d.Lines) != 2 || d.Lines[0]["system_qty"] != nil {
		t.Errorf("count sheet (blind): %s %v", d.Title, d.Lines)
	}
	insert("T130-SP-PR", "PurchaseRequisition", "Draft", map[string]interface{}{"code": "T130-SP-PR", "description": "Packing tape", "quantity": 20.0, "department": "Stores"})
	if d := build("PurchaseRequisition", "T130-SP-PR"); d.Title != "Purchase Requisition" || d.Lines[0]["description"] != "Packing tape" {
		t.Errorf("requisition: %s %v", d.Title, d.Lines)
	}
	insert("T130-SP-RFQ", "RFQ", "Open", map[string]interface{}{"code": "T130-SP-RFQ", "description": "Wire", "lines": `[{"sku":"T130-SP-ITEM","qty":50}]`})
	if d := build("RFQ", "T130-SP-RFQ"); d.Title != "Request for Quotation" || d.Lines[0]["quantity"] != 50.0 {
		t.Errorf("rfq: %s %v", d.Title, d.Lines)
	}
	insert("T130-SP-JO", "SubcontractOrder", "Draft", map[string]interface{}{"code": "T130-SP-JO", "vendor_id": "V", "location": "HO",
		"lines": `[{"line_no":1,"sku":"T130-SP-ITEM","qty":10,"net_weight":5,"output_sku":"T130-SP-ITEM","output_per_unit":1}]`})
	if d := build("SubcontractOrder", "T130-SP-JO"); d.Title != "Job Work Order" || len(d.Lines) != 2 || d.Lines[0]["net_weight"] != 5.0 {
		t.Errorf("job work order: %s %v", d.Title, d.Lines)
	}
}

// Stage 130.19 (55.21): a channel's setup check - not ready with nothing
// saved, not ready with half the credentials, ready with them all - and it
// never hands a credential back.
func TestStage130ConnectorSetup(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	const ch = "T130-SHOP"
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.channel_credentials WHERE channel_code = $1`, ch)
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id = $1`, ch)
	}
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, 'Channel', '{"platform":"Shopify"}', 'Active', 'system')`, ch); err != nil {
		t.Fatal(err)
	}
	c, err := CheckChannelSetup("default", ch)
	if err != nil || c["ready"] != false || !strings.Contains(fmt.Sprint(c["problem"]), "no credentials") || len(c["steps"].([]string)) == 0 {
		t.Fatalf("nothing saved: %v %v", c, err)
	}
	if err := SaveChannelCredential("default", ch, map[string]string{"shop_domain": "t130.myshopify.com"}); err != nil {
		t.Fatal(err)
	}
	if c, _ = CheckChannelSetup("default", ch); c["ready"] != false || !strings.Contains(fmt.Sprint(c["missing"]), "Admin API access token") {
		t.Errorf("half saved: %v", c)
	}
	if err := SaveChannelCredential("default", ch, map[string]string{"shop_domain": "t130.myshopify.com", "access_token": "shpat_T130SECRET"}); err != nil {
		t.Fatal(err)
	}
	c, _ = CheckChannelSetup("default", ch)
	raw, _ := json.Marshal(c)
	if c["ready"] != true || strings.Contains(string(raw), "T130SECRET") || strings.Contains(string(raw), "t130.myshopify.com") {
		t.Errorf("all saved: ready %v, leaks a value: %s", c["ready"], raw)
	}
}
