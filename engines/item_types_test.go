package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// Stage 60.3/60.4 - item types: barcode rule precedence (Item > Item Type >
// client setting), zero-price types at the till, the Free With Item parent
// rule, and backflush consumption at production completion. Own throwaway
// tenant schema, so the shared dev tenant is never touched.
func TestItemTypesBarcodePricingAndBackflush(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	tax := func(m map[string]interface{}) map[string]interface{} {
		m["hsn_code"], m["tax_treatment"] = "6109", "Exempt"
		return m
	}

	// Provisioning copies the built-in types.
	var types int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schemaName + `.documents WHERE doctype = 'ItemType'`).Scan(&types); err != nil || types < 7 {
		t.Fatalf("provisioned item types = %d (err %v), want the 7 built-ins", types, err)
	}

	// --- barcode precedence -------------------------------------------------
	if err := SetSetting(tenantID, "inventory.barcode_generate_at", "grn", "test"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := SetSetting(tenantID, "inventory.barcode_per_receipt_date", "yes", "test"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	product := map[string]interface{}{"item_type": "Stock"}
	if got := EffectiveBarcodeGenerateAt(tenantID, product); got != BarcodeAtGRN {
		t.Errorf("legacy Stock item follows the client setting: got %q, want grn", got)
	}
	if !EffectiveBarcodePerReceiptDate(tenantID, product) {
		t.Errorf("Product (Client default) should follow the client's per-receipt-date = yes")
	}
	gift := map[string]interface{}{"item_type": "Gift"}
	if EffectiveBarcodePerReceiptDate(tenantID, gift) {
		t.Errorf("Gift type is Fixed per SKU and must override the client's per-date setting")
	}
	override := map[string]interface{}{"item_type": "Gift", "barcode_generate_at": "SKU creation", "barcode_per_receipt_date": "New per receipt date"}
	if got := EffectiveBarcodeGenerateAt(tenantID, override); got != BarcodeAtSKUCreate {
		t.Errorf("item override: got %q, want sku_create", got)
	}
	if !EffectiveBarcodePerReceiptDate(tenantID, override) {
		t.Errorf("item override New per receipt date should win over its type's Fixed per SKU")
	}
	if got := EffectiveBarcodeGenerateAt(tenantID, map[string]interface{}{"item_type": "Fixed Asset"}); got != BarcodeAtOff {
		t.Errorf("Fixed Asset type issues no barcode: got %q", got)
	}
	if !IsFixedAssetItem(tenantID, map[string]interface{}{"item_type": "Fixed Asset"}) || IsFixedAssetItem(tenantID, product) {
		t.Errorf("IsFixedAssetItem should follow the type's flag")
	}

	// --- zero price and the parent rule ----------------------------------------
	insert("PHONE", "Item", tax(map[string]interface{}{"code": "PHONE", "name": "Phone", "sale_price": 1000.0}))
	insert("CHARGER", "Item", tax(map[string]interface{}{"code": "CHARGER", "name": "Charger", "sale_price": 500.0, "item_type": "Free With Item", "free_with": "PHONE"}))
	insert("MUG", "Item", tax(map[string]interface{}{"code": "MUG", "name": "Mug", "sale_price": 200.0, "item_type": "Gift"}))

	// Stage 72.7: the Gift type is Promo only - a gift needs a Gift With
	// Purchase offer this bill qualifies for (blueprint UAT-14).
	var verr *ValidationError
	cart := []QuoteLineRequest{{Sku: "PHONE", Qty: 1}, {Sku: "CHARGER", Qty: 1}, {Sku: "MUG", Qty: 2}}
	if _, err := ResolvePOSQuote(tenantID, QuoteRequest{Lines: cart}); !errors.As(err, &verr) || verr.Code != "POSOFF-0412" {
		t.Errorf("gift with no offer: err = %v, want POSOFF-0412", err)
	}
	insert("OFF-MUG", "Offer", map[string]interface{}{"name": "Mugs with a phone", "offer_type": "Gift With Purchase", "scope": "Item", "scope_value": "PHONE", "gift_item": "MUG", "gift_qty": 2.0})
	q, err := ResolvePOSQuote(tenantID, QuoteRequest{Lines: cart})
	if err != nil {
		t.Fatalf("ResolvePOSQuote: %v", err)
	}
	for _, l := range q.Lines {
		switch l.Sku {
		case "PHONE":
			if l.UnitPrice != 1000 {
				t.Errorf("PHONE unit price = %v, want 1000", l.UnitPrice)
			}
		case "CHARGER":
			if l.UnitPrice != 0 || l.ReferencePrice != 0 || l.PriceSource != PriceSourceItemTypeFree {
				t.Errorf("%s should be free: %+v", l.Sku, l)
			}
		case "MUG":
			if l.UnitPrice != 0 || l.ReferencePrice != 0 || l.PriceSource != PriceSourceGWP || l.PromotionRef != "OFF-MUG" {
				t.Errorf("MUG should be a free gift under OFF-MUG: %+v", l)
			}
		}
	}
	if q.Subtotal != 1000 || q.ManualDiscountPct != 0 {
		t.Errorf("free lines must not count as a discount: subtotal %v, manual discount %v%%", q.Subtotal, q.ManualDiscountPct)
	}

	_, err = ResolvePOSQuote(tenantID, QuoteRequest{Lines: []QuoteLineRequest{{Sku: "CHARGER", Qty: 1}}})
	if !errors.As(err, &verr) || verr.Code != "POSOFF-0314" {
		t.Errorf("charger without its phone: err = %v, want POSOFF-0314", err)
	}
	_, err = ResolvePOSQuote(tenantID, QuoteRequest{Lines: []QuoteLineRequest{{Sku: "PHONE", Qty: 1}, {Sku: "CHARGER", Qty: 2}}})
	if !errors.As(err, &verr) || verr.Code != "POSOFF-0314" {
		t.Errorf("two chargers with one phone: err = %v, want POSOFF-0314", err)
	}

	err = ValidateMasterDataRules(tenantID, "CABLE", "Item", map[string]interface{}{"code": "CABLE", "item_type": "Free With Item"})
	if !errors.As(err, &verr) || verr.Code != "MASTER-0315" {
		t.Errorf("Free With Item with no parent: err = %v, want MASTER-0315", err)
	}
	err = ValidateMasterDataRules(tenantID, "CABLE", "Item", map[string]interface{}{"code": "CABLE", "item_type": "Free With Item", "free_with": "CABLE"})
	if !errors.As(err, &verr) || verr.Code != "MASTER-0315" {
		t.Errorf("free with itself: err = %v, want MASTER-0315", err)
	}

	// --- backflush ------------------------------------------------------------
	const loc = "WH-BF"
	insert(loc, "Location", map[string]interface{}{"code": loc, "name": "Backflush Plant", "type": "Warehouse"})
	insert("FABRIC", "Item", tax(map[string]interface{}{"code": "FABRIC", "name": "Fabric"}))
	insert("THREAD", "Item", tax(map[string]interface{}{"code": "THREAD", "name": "Thread", "item_type": "Backflush"}))
	insert("SHIRT", "Item", tax(map[string]interface{}{"code": "SHIRT", "name": "Shirt"}))
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{
		map[string]interface{}{"sku": "FABRIC", "qty": 10.0},
		map[string]interface{}{"sku": "THREAD", "qty": 10.0},
	}, false); err != nil {
		t.Fatalf("seed stock: %v", err)
	}
	insert("BOM-SHIRT", "BOM", map[string]interface{}{"parent_item": "SHIRT", "components": `[{"sku":"FABRIC","qty":2},{"sku":"THREAD","qty":1}]`})
	insert("PRO-1", "ProductionOrder", map[string]interface{}{"bom_id": "BOM-SHIRT", "location": loc, "quantity": 4.0, "status": "Draft"})
	if _, err := db.DB.Exec(`UPDATE ` + schemaName + `.documents SET status = 'Draft' WHERE id = 'PRO-1'`); err != nil {
		t.Fatalf("draft: %v", err)
	}

	avail := func(sku string) float64 {
		var v float64
		_ = db.DB.QueryRow(`SELECT available FROM `+schemaName+`.inventory_availability WHERE sku = $1 AND location_code = $2`, sku, loc).Scan(&v)
		return v
	}
	if err := IssueProductionMaterial(tenantID, "PRO-1"); err != nil {
		t.Fatalf("IssueProductionMaterial: %v", err)
	}
	if avail("FABRIC") != 2 || avail("THREAD") != 10 {
		t.Errorf("after issue: fabric %v (want 2), thread %v (want 10 - backflush is not issued up front)", avail("FABRIC"), avail("THREAD"))
	}
	if err := ReportPartialCompletion(tenantID, "PRO-1", 3); err != nil {
		t.Fatalf("ReportPartialCompletion: %v", err)
	}
	if avail("THREAD") != 7 || avail("SHIRT") != 3 {
		t.Errorf("after completing 3: thread %v (want 7), shirt %v (want 3)", avail("THREAD"), avail("SHIRT"))
	}
}
