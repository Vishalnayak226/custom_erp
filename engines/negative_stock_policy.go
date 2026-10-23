package engines

import (
	"fmt"
	"strings"

	"custom_erp/db"
)

// Stage 130.9 (72.D f, user decision 2026-10-22).
//
// Negative stock per product type (blueprint 6.2): a Product Type may say
// "Allow negative stock: Yes" - made-to-order or weighed goods sold before
// they are booked in. A sale of such an item is not refused for want of
// stock; the shortfall is recorded as a negative-stock event (20.13), the
// same evidence an offline sale's shortfall leaves. Every other item still
// stops at zero.
//
// Free gift on return (blueprint 10.3): when every paid unit of a bill comes
// back, the free gift (a Gift With Purchase line) must come back with it -
// unless Settings > POS > "Free gift when the purchase is returned" says the
// customer keeps it. A partial return keeps the gift.

const (
	SettingGiftReturnRule  = "pos.gift_return_rule"
	GiftReturnWithPurchase = "with_purchase"
	GiftReturnKeep         = "keep"
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingGiftReturnRule, Module: "Point of Sale",
		Label: "Free gift when the purchase is returned", Type: SettingTypeSelect, Default: GiftReturnWithPurchase,
		Options: []SettingOption{
			{Value: GiftReturnWithPurchase, Label: "Comes back with the whole purchase"},
			{Value: GiftReturnKeep, Label: "The customer keeps it"},
		},
		Description: "When everything paid for on a bill comes back, its free gift (Gift With Purchase) comes back too, or the return is refused - unless the customer keeps it. A partial return always keeps the gift.",
	})
}

// productTypeAllowsNegative reports the item's Product Type setting.
func productTypeAllowsNegative(tenantID, sku string) bool {
	item, err := ResolveItemBySKU(tenantID, sku)
	if err != nil {
		return false
	}
	pt := strings.TrimSpace(strField(item.Data, "product_type"))
	if pt == "" {
		return false
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	var allow string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'allow_negative_stock', '') FROM %s.documents
		WHERE doctype = 'ProductType' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1 OR data->>'name' = $1) LIMIT 1`, schema), pt).Scan(&allow)
	return strings.EqualFold(strings.TrimSpace(allow), "Yes")
}

// markNegativeStockAllowed flags the stock lines whose product type allows
// negative stock, for PostInventoryLedgerWithVoucherTx's floor check.
func markNegativeStockAllowed(tenantID string, items []interface{}) {
	cache := map[string]bool{}
	for _, it := range items {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		sku := strField(m, "sku")
		allow, seen := cache[sku]
		if !seen {
			allow = productTypeAllowsNegative(tenantID, sku)
			cache[sku] = allow
		}
		if allow {
			m["allow_negative"] = true
		}
	}
}

// checkGiftComesBack refuses a return that takes back every paid unit of a
// bill but not its free gift.
func checkGiftComesBack(tenantID, originalOrderID string, items []ReturnItemInput, claimed map[string]int) error {
	if GetSettingString(tenantID, SettingGiftReturnRule) == GiftReturnKeep {
		return nil
	}
	sale, _, err := fetchDocData(tenantID, "POSCart", originalOrderID)
	if err != nil || sale == nil {
		return nil
	}
	var lines []map[string]interface{}
	switch v := sale["items"].(type) {
	case []interface{}:
		for _, x := range v {
			if m, ok := x.(map[string]interface{}); ok {
				lines = append(lines, m)
			}
		}
	}
	paid, gifts := map[string]int{}, map[string]int{}
	for _, l := range lines {
		sku, qty := strField(l, "sku"), int(numFromInterface(l["qty"]))
		if strField(l, "promotion_ref") != "" {
			gifts[sku] += qty
		} else {
			paid[sku] += qty
		}
	}
	if len(gifts) == 0 || len(paid) == 0 {
		return nil
	}
	asked := map[string]int{}
	for _, it := range items {
		asked[it.SKU] += it.Qty
	}
	for sku, sold := range paid {
		if claimed[sku]+asked[sku] < sold {
			return nil // something paid for stays with the customer
		}
	}
	for sku, sold := range gifts {
		if claimed[sku]+asked[sku] < sold+paid[sku] {
			return &ValidationError{Code: "SALESR-1307", SubFor: sku, Message: fmt.Sprintf("everything bought on this bill is coming back, so its free gift %s comes back too - add it to the return", sku)}
		}
	}
	return nil
}
