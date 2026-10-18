package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Item types (Stage 60.3/60.4). The user: "item type default Product; if any
// accessory / non-asset / goes free with item / gift / backflush, I should be
// able to define it, and the barcode will be fixed for that SKU" - and a
// barcode rule settable per client, per item type and per item.
//
// ItemType is an ordinary master (Setup -> Item Type), so a tenant adds its
// own types. Each carries:
//
//	barcode_generate_at       Client default | SKU creation | Goods receipt | Off
//	barcode_per_receipt_date  Client default | Fixed per SKU | New per receipt date
//	sell_at_zero_price        Yes | No   (Gift, Free With Item: priced 0 at the till)
//	needs_parent_item         Yes | No   (Free With Item: only alongside its parent SKU)
//	backflush                 Yes | No   (consumed at production completion, not issued up front)
//	fixed_asset               Yes | No   (bought on a PO, raised as an Asset, never sold)
//
// An Item may override the two barcode settings ("Item type default" keeps the
// type's). Precedence: Item, then Item Type, then the client setting.
//
// Item.item_type was a Select of Stock / Fixed Asset before this Stage. A
// blank or "Stock" value reads as Product, so no stored Item is rewritten; the
// value is normalised only when that Item is next saved.

// ItemTypeProduct is the default type of every Item.
const ItemTypeProduct = "Product"

// ItemTypeRules is what an item type decides.
type ItemTypeRules struct {
	Name            string
	BarcodeAt       string // "" (inherit), BarcodeAtSKUCreate, BarcodeAtGRN, BarcodeAtOff
	BarcodePerDate  string // "" (inherit), "yes", "no"
	BatchMode       string // "" (inherit), or a GRNBatch* mode (Stage 61.6)
	StickerTemplate string // StickerTemplate id its items' labels use, "" = next rule (Stage 70)
	SellAtZeroPrice bool
	NeedsParentItem bool
	Backflush       bool
	FixedAsset      bool
	// Policy holds the Stage 72 dimension fields this type sets (field ->
	// canonical value); ExpenseAccount where its expensed issues post. Read
	// through ItemPolicyFor (engines/item_policy.go), never directly.
	Policy         map[string]string
	ExpenseAccount string
}

// builtInItemTypeRules covers a tenant whose ItemType records have not been
// created (or were deleted): the two types the engine itself relies on still
// mean what they always meant.
var builtInItemTypeRules = map[string]ItemTypeRules{
	ItemTypeProduct:    {Name: ItemTypeProduct},
	ItemTypeFixedAsset: {Name: ItemTypeFixedAsset, BarcodeAt: BarcodeAtOff, FixedAsset: true},
}

// ItemTypeName is an Item's type, with blank and the legacy "Stock" read as
// Product.
func ItemTypeName(data map[string]interface{}) string {
	t, _ := data["item_type"].(string)
	t = strings.TrimSpace(t)
	if t == "" || strings.EqualFold(t, "Stock") {
		return ItemTypeProduct
	}
	return t
}

// normalizeItemType stamps the effective type onto an Item being saved, so a
// legacy "Stock" or a blank becomes "Product" - the Link to ItemType then
// resolves. Called from ValidateDocument, the one path every save, import and
// bulk edit runs through.
func normalizeItemType(data map[string]interface{}) {
	data["item_type"] = ItemTypeName(data)
}

// ItemTypeRulesFor reads one ItemType record. A type with no record falls back
// to the built-in meaning of Product / Fixed Asset, and otherwise to "inherit
// everything" - an unknown type never invents behaviour.
func ItemTypeRulesFor(tenantID, typeName string) ItemTypeRules {
	if typeName == "" || strings.EqualFold(typeName, "Stock") {
		typeName = ItemTypeProduct
	}
	if r, ok := cachedItemTypes(tenantID)[strings.ToLower(typeName)]; ok {
		return r
	}
	for name, r := range builtInItemTypeRules {
		if strings.EqualFold(name, typeName) {
			return r
		}
	}
	return ItemTypeRules{Name: typeName}
}

// Item types are read on every Item save, import row, receipt line and till
// line, so a tenant's whole list (a handful of rows) is loaded in one query and
// kept for itemTypeCacheTTL. Saving an ItemType drops the tenant's copy
// (validateDocumentWith), so an edit shows up on the next read.
const itemTypeCacheTTL = 30 * time.Second

type itemTypeCacheEntry struct {
	rules    map[string]ItemTypeRules
	loadedAt time.Time
}

var (
	itemTypeCacheMu sync.Mutex
	itemTypeCache   = map[string]itemTypeCacheEntry{}
)

// InvalidateItemTypeCache forgets a tenant's cached item types.
func InvalidateItemTypeCache(tenantID string) {
	itemTypeCacheMu.Lock()
	delete(itemTypeCache, tenantID)
	itemTypeCacheMu.Unlock()
}

func cachedItemTypes(tenantID string) map[string]ItemTypeRules {
	itemTypeCacheMu.Lock()
	entry, ok := itemTypeCache[tenantID]
	itemTypeCacheMu.Unlock()
	if ok && time.Since(entry.loadedAt) < itemTypeCacheTTL {
		return entry.rules
	}
	rules := map[string]ItemTypeRules{}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return rules
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data::text FROM %s.documents WHERE doctype = 'ItemType' AND deleted_at IS NULL`, schema))
	if err != nil {
		return rules // not cached: the next call tries again
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if rows.Scan(&id, &raw) != nil {
			continue
		}
		var data map[string]interface{}
		if json.Unmarshal([]byte(raw), &data) != nil {
			continue
		}
		rules[strings.ToLower(id)] = itemTypeRulesFromRecord(id, data)
	}
	itemTypeCacheMu.Lock()
	itemTypeCache[tenantID] = itemTypeCacheEntry{rules: rules, loadedAt: time.Now()}
	itemTypeCacheMu.Unlock()
	return rules
}

// itemTypeRulesFromRecord reads one ItemType record's rules.
func itemTypeRulesFromRecord(id string, data map[string]interface{}) ItemTypeRules {
	return ItemTypeRules{
		Name:            id,
		BarcodeAt:       barcodeAtFromLabel(pimString(data["barcode_generate_at"])),
		BarcodePerDate:  barcodePerDateFromLabel(pimString(data["barcode_per_receipt_date"])),
		BatchMode:       grnBatchModeFromLabel(pimString(data["grn_batch_number"])),
		StickerTemplate: pimString(data["sticker_template"]),
		SellAtZeroPrice: yesValue(data["sell_at_zero_price"]),
		NeedsParentItem: yesValue(data["needs_parent_item"]),
		Backflush:       yesValue(data["backflush"]),
		FixedAsset:      yesValue(data["fixed_asset"]),
		Policy:          policyFromRecord(data),
		ExpenseAccount:  pimString(data["expense_account"]),
	}
}

// ItemRules is an Item's effective rules: its type's, with the Item's own
// barcode overrides on top. Behaviour flags come from the type only.
func ItemRules(tenantID string, itemData map[string]interface{}) ItemTypeRules {
	r := ItemTypeRulesFor(tenantID, ItemTypeName(itemData))
	if v := barcodeAtFromLabel(pimString(itemData["barcode_generate_at"])); v != "" {
		r.BarcodeAt = v
	}
	if v := barcodePerDateFromLabel(pimString(itemData["barcode_per_receipt_date"])); v != "" {
		r.BarcodePerDate = v
	}
	if v := grnBatchModeFromLabel(pimString(itemData["grn_batch_number"])); v != "" {
		r.BatchMode = v
	}
	return r
}

// EffectiveBarcodeGenerateAt is when this Item's permanent barcode is issued.
func EffectiveBarcodeGenerateAt(tenantID string, itemData map[string]interface{}) string {
	if v := ItemRules(tenantID, itemData).BarcodeAt; v != "" {
		return v
	}
	return BarcodeGenerateAt(tenantID)
}

// EffectiveBarcodePerReceiptDate reports whether this Item gets a fresh
// barcode for each day it is received.
func EffectiveBarcodePerReceiptDate(tenantID string, itemData map[string]interface{}) bool {
	switch ItemRules(tenantID, itemData).BarcodePerDate {
	case "yes":
		return true
	case "no":
		return false
	}
	return BarcodePerReceiptDate(tenantID)
}

// EffectiveGRNBatchMode is how this Item's blank GRN lines get batch numbers
// (Stage 61.6): the Item's own rule, then its type's, then the client's.
func EffectiveGRNBatchMode(tenantID string, itemData map[string]interface{}) string {
	if v := ItemRules(tenantID, itemData).BatchMode; v != "" {
		return v
	}
	return GRNBatchMode(tenantID)
}

// grnBatchModeFromLabel maps the Select labels (and the raw setting values)
// to a GRNBatch* mode; anything else inherits.
func grnBatchModeFromLabel(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "one per sku on each grn", GRNBatchPerGRNSKU:
		return GRNBatchPerGRNSKU
	case "one per grn line", GRNBatchPerLine:
		return GRNBatchPerLine
	case "one per sku per day", GRNBatchPerSKUDay:
		return GRNBatchPerSKUDay
	case GRNBatchOff:
		return GRNBatchOff
	}
	return ""
}

// barcodeAtFromLabel maps the Select labels (and the raw setting values, so a
// CSV import may use either) to a BarcodeAt constant; anything else inherits.
func barcodeAtFromLabel(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "sku creation", BarcodeAtSKUCreate:
		return BarcodeAtSKUCreate
	case "goods receipt", BarcodeAtGRN:
		return BarcodeAtGRN
	case BarcodeAtOff:
		return BarcodeAtOff
	}
	return ""
}

func barcodePerDateFromLabel(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "new per receipt date", "yes":
		return "yes"
	case "fixed per sku", "no":
		return "no"
	}
	return ""
}

func yesValue(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "yes" || s == "true" || s == "1"
	}
	return false
}

// resolveFreeItemLines reports which quote lines are free because of their
// item type (Stage 60.4), and refuses a Free With Item line whose parent item
// is not in the same cart in at least the same quantity - otherwise "free with
// a phone" would let the charger walk out alone at zero.
func resolveFreeItemLines(tenantID string, lines []QuoteLineRequest) (map[string]bool, error) {
	cartQty := map[string]int{}
	for _, l := range lines {
		cartQty[strings.TrimSpace(l.Sku)] += l.Qty
	}
	free := map[string]bool{}
	for sku, qty := range cartQty {
		data, _, err := fetchDocData(tenantID, "Item", sku)
		if err != nil || data == nil {
			continue
		}
		rules := ItemRules(tenantID, data)
		if !rules.SellAtZeroPrice {
			continue
		}
		if rules.NeedsParentItem {
			parent := strings.TrimSpace(pimString(data["free_with"]))
			if parent == "" || cartQty[parent] < qty {
				return nil, &ValidationError{Code: "POSOFF-0314", SubFor: sku, Message: fmt.Sprintf(
					"%s is free only with its parent item %s - add %d of it to the bill, or remove the free item", itemLabel(data, sku), parentLabel(tenantID, parent), qty)}
			}
		}
		free[sku] = true
	}
	return free, nil
}

// validateItemTypeRules (Stage 60.4) runs on Item save: a Free With Item type
// needs the parent it rides along with, and that parent cannot be itself.
func validateItemTypeRules(tenantID, docID string, payload map[string]interface{}) error {
	if !ItemRules(tenantID, payload).NeedsParentItem {
		return nil
	}
	parent := strings.TrimSpace(pimString(payload["free_with"]))
	if parent == "" {
		return &ValidationError{Code: "MASTER-0315", SubFor: "free_with", Message: fmt.Sprintf(
			"item type %s is given free with another item - choose that item in \"Free with item\"", ItemTypeName(payload))}
	}
	if parent == strings.TrimSpace(docID) || parent == strings.TrimSpace(pimString(payload["code"])) {
		return &ValidationError{Code: "MASTER-0315", SubFor: "free_with", Message: "an item cannot be free with itself - choose the item it is given away with"}
	}
	return nil
}

func itemLabel(data map[string]interface{}, fallback string) string {
	if n := strings.TrimSpace(pimString(data["name"])); n != "" {
		return n
	}
	return fallback
}

func parentLabel(tenantID, parent string) string {
	if parent == "" {
		return "(none set on the item)"
	}
	if data, _, err := fetchDocData(tenantID, "Item", parent); err == nil && data != nil {
		return itemLabel(data, parent)
	}
	return parent
}
