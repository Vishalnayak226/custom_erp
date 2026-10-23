package engines

import (
	"fmt"
	"strings"

	"custom_erp/db"
)

// Stage 130.9 (72.D d, user decision 2026-10-22): sellable / stocked per
// store. A store (Location) record may list the items it does not sell
// (items_not_sold_here) and does not stock (items_not_stocked_here), each a
// comma-separated list of item codes, categories or product types. The till
// refuses an item its store does not sell; a goods receipt refuses one its
// store does not stock. Blank lists change nothing.

const (
	StoreItemsNotSold    = "items_not_sold_here"
	StoreItemsNotStocked = "items_not_stocked_here"
)

// storeItemList is one of a store's two lists, upper-cased; nil when blank.
func storeItemList(tenantID, location, field string) map[string]bool {
	location = strings.TrimSpace(location)
	if location == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil
	}
	var raw string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>$2, '') FROM %s.documents
		WHERE doctype = 'Location' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1) LIMIT 1`, schema), location, field).Scan(&raw); err != nil {
		return nil
	}
	var out map[string]bool
	for _, tok := range strings.Split(raw, ",") {
		if tok = strings.ToUpper(strings.TrimSpace(tok)); tok != "" {
			if out == nil {
				out = map[string]bool{}
			}
			out[tok] = true
		}
	}
	return out
}

// storeListNames reports whether a list names the item by its code, SKU,
// category or product type.
func storeListNames(list map[string]bool, sku string, item map[string]interface{}) bool {
	if len(list) == 0 {
		return false
	}
	for _, v := range []string{sku, strField(item, "code"), strField(item, "category"), strField(item, "product_type")} {
		if v = strings.ToUpper(strings.TrimSpace(v)); v != "" && list[v] {
			return true
		}
	}
	return false
}

// storeSellGate is the till's refusal of an item its store does not sell.
func storeSellGate(tenantID, location, sku string, item map[string]interface{}) error {
	if !storeListNames(storeItemList(tenantID, location, StoreItemsNotSold), sku, item) {
		return nil
	}
	return &ValidationError{Code: "POSOFF-1304", SubFor: sku, Message: fmt.Sprintf("%s is not sold at %s (the store's \"Items not sold here\")", posLineLabel(item, sku), location)}
}

// storeStockGate refuses goods coming into a store that does not stock them.
func storeStockGate(tenantID, location string, skus []string) error {
	list := storeItemList(tenantID, location, StoreItemsNotStocked)
	if len(list) == 0 {
		return nil
	}
	for _, sku := range skus {
		var item map[string]interface{}
		if r, err := ResolveItemBySKU(tenantID, sku); err == nil {
			item = r.Data
		}
		if storeListNames(list, sku, item) {
			return &ValidationError{Code: "GOODSR-1305", SubFor: sku, Message: fmt.Sprintf("%s is not stocked at %s (the store's \"Items not stocked here\") - receive it at another store", sku, location)}
		}
	}
	return nil
}
