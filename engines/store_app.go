package engines

import (
	"fmt"
	"sort"
	"strings"

	"custom_erp/db"
)

// Stage 102.3.1: the store app's "scan an item". One answer for whatever is on
// the tag - the item code, its barcode, a receipt-date barcode (57.7) or a GRN
// batch number (61) - built only from what the ERP already decides elsewhere:
// ResolveItemBySKU for the code, GetBatchAgeing for the batch and its age, the
// till's own price order (ResolvePOSQuote's reference price: the default price
// list, then the item's sale price, then its MRP) and inventory_availability
// with the same ATS rule as /api/v1/availability. Cost never appears here: the
// app is used on the shop floor by the same roles that use the till.

// StoreAppStock is one store's stock of the scanned item.
type StoreAppStock struct {
	Store     string `json:"store"`
	Name      string `json:"name"`
	Available int    `json:"available"`
	OnHand    int    `json:"on_hand"`
	Working   bool   `json:"working"`
}

// StoreAppItem is everything the scan screen shows.
type StoreAppItem struct {
	Code        string           `json:"code"`
	Name        string           `json:"name"`
	Barcode     string           `json:"barcode,omitempty"`
	Brand       string           `json:"brand,omitempty"`
	Category    string           `json:"category,omitempty"`
	Status      string           `json:"status"`
	Price       float64          `json:"price"`
	PriceSource string           `json:"price_source,omitempty"`
	MRP         float64          `json:"mrp"`
	Currency    string           `json:"currency"`
	Image       string           `json:"image,omitempty"`
	MatchedOn   string           `json:"matched_on"`
	Batch       *BatchAgeingRow  `json:"batch,omitempty"`
	Batches     []BatchAgeingRow `json:"batches"`
	Stock       []StoreAppStock  `json:"stock"`
	StockMore   int              `json:"stock_more,omitempty"`
}

// storeAppBatchLimit and storeAppStockLimit keep the answer phone-sized: the
// oldest batches still holding stock, and the stores that hold the item.
const (
	storeAppBatchLimit = 5
	storeAppStockLimit = 30
)

// StoreAppLookup answers a scan for a user working at workingStore. Stock is
// listed only for stores the user may work at (85), working store first.
func StoreAppLookup(tenantID, userID, workingStore, code string) (*StoreAppItem, error) {
	access, err := UserStoreAccess(tenantID, userID)
	if err != nil {
		return nil, err
	}
	return storeAppLookup(tenantID, access, workingStore, code)
}

func storeAppLookup(tenantID string, access StoreAccess, workingStore, code string) (*StoreAppItem, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, ErrItemNotFound
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	batches, err := GetBatchAgeing(tenantID, code, 0, false)
	if err != nil {
		return nil, err
	}
	var scannedBatch *BatchAgeingRow
	sku := code
	if len(batches) > 0 && strings.EqualFold(batches[0].BatchNo, code) {
		b := batches[0]
		scannedBatch = &b
		sku = b.Sku
	}

	item, err := ResolveItemBySKU(tenantID, sku)
	if err != nil {
		return nil, err
	}
	itemCode := firstNonEmpty(strField(item.Data, "code"), item.ID)
	out := &StoreAppItem{
		Code:      itemCode,
		Name:      firstNonEmpty(strField(item.Data, "item_name"), strField(item.Data, "name"), itemCode),
		Barcode:   strField(item.Data, "barcode"),
		Brand:     strField(item.Data, "brand"),
		Category:  strField(item.Data, "category"),
		Status:    item.Status,
		MatchedOn: item.MatchedOn,
		Batch:     scannedBatch,
		Batches:   []BatchAgeingRow{},
		Stock:     []StoreAppStock{},
	}
	if scannedBatch != nil {
		out.MatchedOn = "batch"
		// A batch scan listed only that batch; the item's other lots come
		// from a second read by item code.
		if batches, err = GetBatchAgeing(tenantID, itemCode, 0, false); err != nil {
			return nil, err
		}
	}
	for _, b := range batches {
		if b.EstQtyLeft > 0 && len(out.Batches) < storeAppBatchLimit {
			out.Batches = append(out.Batches, b)
		}
	}

	out.Price, out.PriceSource = storeAppReferencePrice(tenantID, item.Data, itemCode)
	out.MRP, _ = parityNumber(item.Data["mrp"])
	out.Currency = FunctionalCurrency(tenantID)
	out.Image = itemThumbnailDataURL(tenantID, itemCode)

	stock, more, err := storeAppStock(tenantID, schema, access, workingStore, itemCode)
	if err != nil {
		return nil, err
	}
	out.Stock, out.StockMore = stock, more
	return out, nil
}

// storeAppReferencePrice is ResolvePOSQuote's reference price for a walk-in
// customer: the default price list, then the item's sale price, then its MRP.
// Offers and a customer's contract list are the till's business, at billing.
func storeAppReferencePrice(tenantID string, data map[string]interface{}, itemCode string) (float64, string) {
	if list := strings.TrimSpace(GetSettingString(tenantID, "pos.default_price_list")); list != "" {
		if p, found, _ := ResolvePriceForSKU(tenantID, list, itemCode, BusinessToday()); found && p > 0 {
			return p, PriceSourceDefaultList
		}
	}
	if p, _ := parityNumber(data["sale_price"]); p > 0 {
		return p, PriceSourceItemMaster
	}
	if p, _ := parityNumber(data["mrp"]); p > 0 {
		return p, PriceSourceItemMRP
	}
	return 0, ""
}

// storeAppStock lists the item's stock at the stores the user may work at:
// the working store always (even at zero), then every other store holding
// some, most available first, capped at storeAppStockLimit (more = the rest).
func storeAppStock(tenantID, schema string, access StoreAccess, workingStore, sku string) ([]StoreAppStock, int, error) {
	stores, err := StoresFor(tenantID, access)
	if err != nil {
		return nil, 0, err
	}
	names := map[string]string{}
	for _, s := range stores {
		names[s.Code] = s.Name
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT location_code, on_hand, available, reserved, safety_stock, blocked, qc_hold, damaged, channel_buffer, hold_qty
		FROM %s.inventory_availability WHERE sku = $1`, schema), sku)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	policy := atsPolicyFor(schema)
	var working *StoreAppStock
	others := []StoreAppStock{}
	for rows.Next() {
		var loc string
		var onHand, available, reserved, safety, blocked, qcHold, damaged, buffer, held int
		if err := rows.Scan(&loc, &onHand, &available, &reserved, &safety, &blocked, &qcHold, &damaged, &buffer, &held); err != nil {
			return nil, 0, err
		}
		name, ok := names[loc]
		if !ok {
			continue // not a store this user may see
		}
		s := StoreAppStock{Store: loc, Name: name, OnHand: onHand,
			Available: computeATS(policy, available, reserved, safety, blocked, qcHold, damaged, buffer, held)}
		if loc == workingStore {
			s.Working = true
			working = &s
			continue
		}
		if s.Available > 0 || s.OnHand > 0 {
			others = append(others, s)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if working == nil && workingStore != "" {
		if name, ok := names[workingStore]; ok {
			working = &StoreAppStock{Store: workingStore, Name: name, Working: true}
		}
	}
	sort.SliceStable(others, func(i, j int) bool {
		if others[i].Available != others[j].Available {
			return others[i].Available > others[j].Available
		}
		return others[i].Name < others[j].Name
	})
	out := []StoreAppStock{}
	if working != nil {
		out = append(out, *working)
	}
	more := 0
	if len(others) > storeAppStockLimit {
		more = len(others) - storeAppStockLimit
		others = others[:storeAppStockLimit]
	}
	return append(out, others...), more, nil
}
