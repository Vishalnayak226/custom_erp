package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Stage 57.8 (user decision 2026-10-06): non-sellable asset records belong
// in the Fixed Assets module (custody, depreciation) and must never appear in
// POS, sales, OMS or sellable stock.
//
// An Item whose item_type is "Fixed Asset" can be bought on a PO like any
// other Item. When its GRN posts, the accepted units do not enter stock or
// 1200 Inventory: each unit becomes a Draft Asset (PostGRNReceiptWithQC ->
// createAssetsFromReceipt), which is capitalised, depreciated, transferred
// and disposed in the existing Fixed Assets module. Capitalisation books
// Dr 1400 / Cr 2100, exactly as a hand-entered asset always has.
//
// "Never sellable" is enforced once, by RejectNonSellableItem, at the sale-
// side choke points: ComputeGSTForLines (every sale-side tax computation -
// POS quote/checkout, order and pack invoices, customer returns) and
// validateOrderChain (every OMS order, create and hold release). Picking needs
// an order line and stock, and a Fixed Asset item has neither.

// ItemTypeFixedAsset is the Item.item_type value for a non-sellable asset.
const ItemTypeFixedAsset = "Fixed Asset"

// IsFixedAssetItem reports whether an Item's data marks it as a fixed asset.
func IsFixedAssetItem(data map[string]interface{}) bool {
	t, _ := data["item_type"].(string)
	return strings.EqualFold(strings.TrimSpace(t), ItemTypeFixedAsset)
}

// RejectNonSellableItem refuses a sale-side line for a Fixed Asset item. An
// unknown SKU is left to the caller's own not-found handling.
func RejectNonSellableItem(tenantID, sku string) error {
	item, err := ResolveItemBySKU(tenantID, sku)
	if errors.Is(err, ErrItemNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if IsFixedAssetItem(item.Data) {
		return &ValidationError{Code: "ASSET-0273", Message: fmt.Sprintf(
			"item %s is a fixed asset, not stock for sale - it cannot be sold, ordered or invoiced", sku)}
	}
	return nil
}

// isFixedAssetSKU is RejectNonSellableItem's read for the receiving path,
// which needs a yes/no rather than a refusal.
func isFixedAssetSKU(tenantID, sku string) (bool, error) {
	item, err := ResolveItemBySKU(tenantID, sku)
	if errors.Is(err, ErrItemNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return IsFixedAssetItem(item.Data), nil
}

// assetReceiptLine is one GRN line of a Fixed Asset item.
type assetReceiptLine struct {
	SKU      string
	Accepted int
	Serials  []string
}

// createAssetsFromReceipt raises one Draft Asset per accepted unit, costed at
// the GRN's PO ex-GST rate (the figure the GRN would have put into 1200 for a
// stock item). Useful life is left for the Fixed Assets screen to ask for at
// Capitalise. Returns the asset ids raised.
func createAssetsFromReceipt(tenantID, schema, grnID, locationCode, userID string, lines []assetReceiptLine) ([]string, error) {
	poID := grnPurchaseOrderID(schema, grnID)
	vendor := grnVendor(schema, grnID)
	if vendor == "" && poID != "" {
		if po, _, err := fetchDocData(tenantID, "PurchaseOrder", poID); err == nil {
			vendor = strField(po, "vendor")
		}
	}
	today := time.Now().Format("2006-01-02")
	var ids []string
	for _, l := range lines {
		cost := 0.0
		if paise, ok := resolvePOBaseUnitCostPaise(tenantID, poID, l.SKU); ok {
			cost = PaiseToRupees(paise)
		}
		category := ""
		if item, err := ResolveItemBySKU(tenantID, l.SKU); err == nil {
			category = strField(item.Data, "category")
			if category == "" {
				category = strField(item.Data, "name")
			}
		}
		for i := 0; i < l.Accepted; i++ {
			id, err := GenerateSequence(tenantID, "AST", locationCode, documentFinancialYear(time.Now()))
			if err != nil {
				return ids, fmt.Errorf("could not number the asset for %s: %w", l.SKU, err)
			}
			data := map[string]interface{}{
				"code": id, "category": category, "vendor": vendor, "acquisition_date": today,
				"cost": math.Round(cost*100) / 100, "location": locationCode,
				"item_code": l.SKU, "source_grn": grnID, "source_po": poID,
			}
			if i < len(l.Serials) {
				data["serial_number"] = l.Serials[i]
			}
			raw, err := json.Marshal(data)
			if err != nil {
				return ids, err
			}
			if _, err := db.DB.Exec(fmt.Sprintf(
				`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Asset', $2, 'Draft', $3)`, schema),
				id, raw, userID); err != nil {
				return ids, fmt.Errorf("could not raise the asset for %s: %v", l.SKU, err)
			}
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		LogAuditEvent(tenantID, userID, "ASSET_FROM_GRN", "SUCCESS", fmt.Sprintf("GRN %s raised %d Draft asset(s): %s", grnID, len(ids), strings.Join(ids, ", ")))
	}
	return ids, nil
}
