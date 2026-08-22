package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type BundleComponent struct {
	SKU         string  `json:"sku"`
	Quantity    int     `json:"quantity"`
	UnitPrice   float64 `json:"unit_price,omitempty"`
	PriceWeight float64 `json:"price_weight,omitempty"`
}

type ProductBundle struct {
	ID              string            `json:"id"`
	BundleSKU       string            `json:"bundle_sku"`
	Name            string            `json:"name"`
	FulfillmentMode string            `json:"fulfillment_mode"`
	PricingMode     string            `json:"pricing_mode"`
	FixedPrice      float64           `json:"fixed_price"`
	Components      []BundleComponent `json:"components"`
	Status          string            `json:"status"`
}

type BundleAvailability struct {
	BundleSKU      string `json:"bundle_sku"`
	Location       string `json:"location_code"`
	ATS            int    `json:"ats"`
	Derived        bool   `json:"derived_from_components"`
	ComponentCount int    `json:"component_count"`
}

func decodeBundleComponents(raw interface{}) ([]BundleComponent, error) {
	var encoded []byte
	var err error
	switch value := raw.(type) {
	case string:
		encoded = []byte(value)
	case json.RawMessage:
		encoded = value
	default:
		encoded, err = json.Marshal(value)
		if err != nil {
			return nil, err
		}
	}
	var components []BundleComponent
	if err := json.Unmarshal(encoded, &components); err != nil {
		return nil, fmt.Errorf("components must be a JSON array: %w", err)
	}
	return components, nil
}

func loadActiveProductBundle(tenantID, bundleSKU string) (ProductBundle, bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ProductBundle{}, false, err
	}
	var id, status string
	var data []byte
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id,data,status FROM %s.documents WHERE doctype='ProductBundle' AND data->>'bundle_sku'=$1 AND status='Active' AND deleted_at IS NULL LIMIT 1`, schema), bundleSKU).Scan(&id, &data, &status)
	if err == sql.ErrNoRows {
		return ProductBundle{}, false, nil
	}
	if err != nil {
		return ProductBundle{}, false, err
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return ProductBundle{}, false, err
	}
	components, err := decodeBundleComponents(raw["components"])
	if err != nil {
		return ProductBundle{}, false, err
	}
	return ProductBundle{ID: id, BundleSKU: strings.TrimSpace(fmt.Sprint(raw["bundle_sku"])), Name: strings.TrimSpace(fmt.Sprint(raw["name"])), FulfillmentMode: strings.TrimSpace(fmt.Sprint(raw["fulfillment_mode"])), PricingMode: strings.TrimSpace(fmt.Sprint(raw["pricing_mode"])), FixedPrice: numericFromAny(raw["fixed_price"]), Components: components, Status: status}, true, nil
}

func validateProductBundleMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	bundleSKU := strings.TrimSpace(fmt.Sprint(payload["bundle_sku"]))
	mode := strings.TrimSpace(fmt.Sprint(payload["fulfillment_mode"]))
	pricing := strings.TrimSpace(fmt.Sprint(payload["pricing_mode"]))
	if bundleSKU == "" || bundleSKU == "<nil>" {
		return errors.New("bundle_sku is required")
	}
	if mode != "Virtual" && mode != "Stocked" {
		return errors.New("fulfillment_mode must be Virtual or Stocked")
	}
	if pricing != "Parent Price" && pricing != "Fixed Price" && pricing != "Component Price" {
		return errors.New("pricing_mode must be Parent Price, Fixed Price or Component Price")
	}
	if pricing == "Fixed Price" && numericFromAny(payload["fixed_price"]) < 0 {
		return errors.New("fixed_price cannot be negative")
	}
	item, err := ResolveItemBySKU(tenantID, bundleSKU)
	if err != nil || item.Status != "Active" {
		return fmt.Errorf("bundle SKU %s must be an active Item", bundleSKU)
	}
	components, err := decodeBundleComponents(payload["components"])
	if err != nil {
		return err
	}
	if len(components) == 0 {
		return errors.New("a product bundle needs at least one component")
	}
	seen := map[string]bool{}
	schema, _ := db.GetTenantSchema(tenantID)
	for _, component := range components {
		component.SKU = strings.TrimSpace(component.SKU)
		if component.SKU == "" || component.Quantity <= 0 {
			return errors.New("every bundle component needs a SKU and positive integer quantity")
		}
		if component.SKU == bundleSKU {
			return fmt.Errorf("bundle %s cannot contain itself", bundleSKU)
		}
		if seen[component.SKU] {
			return fmt.Errorf("component SKU %s is repeated", component.SKU)
		}
		seen[component.SKU] = true
		componentItem, resolveErr := ResolveItemBySKU(tenantID, component.SKU)
		if resolveErr != nil || componentItem.Status != "Active" {
			return fmt.Errorf("component SKU %s must be an active Item", component.SKU)
		}
		var nested bool
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype='ProductBundle' AND status='Active' AND deleted_at IS NULL AND id<>$1 AND data->>'bundle_sku'=$2)`, schema), docID, component.SKU).Scan(&nested); err != nil {
			return err
		}
		if nested {
			return fmt.Errorf("nested bundle component %s is not supported", component.SKU)
		}
		if pricing == "Component Price" && component.UnitPrice < 0 {
			return fmt.Errorf("component SKU %s has a negative unit_price", component.SKU)
		}
	}
	var duplicate bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype='ProductBundle' AND status='Active' AND deleted_at IS NULL AND id<>$1 AND data->>'bundle_sku'=$2)`, schema), docID, bundleSKU).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate {
		return fmt.Errorf("an active ProductBundle already exists for %s", bundleSKU)
	}
	return nil
}

func rawSKUATS(tenantID, sku, location string) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	query := fmt.Sprintf(`SELECT available,reserved,safety_stock,blocked,qc_hold,damaged,channel_buffer,hold_qty FROM %s.inventory_availability WHERE sku=$1`, schema)
	args := []interface{}{sku}
	if location != "" {
		query += ` AND location_code=$2`
		args = append(args, location)
	}
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	total := 0
	for rows.Next() {
		var available, reserved, safety, blocked, qc, damaged, buffer, held int
		if err := rows.Scan(&available, &reserved, &safety, &blocked, &qc, &damaged, &buffer, &held); err != nil {
			return 0, err
		}
		ats := computeATS(available, reserved, safety, blocked, qc, damaged, buffer, held)
		if ats > 0 {
			total += ats
		}
	}
	return total, rows.Err()
}

func virtualBundleATSAtLocation(tenantID string, bundle ProductBundle, location string) (int, error) {
	result := math.MaxInt
	for _, component := range bundle.Components {
		ats, err := rawSKUATS(tenantID, component.SKU, location)
		if err != nil {
			return 0, err
		}
		possible := ats / component.Quantity
		if possible < result {
			result = possible
		}
	}
	if result == math.MaxInt || result < 0 {
		return 0, nil
	}
	return result, nil
}

// ComputeSellableSKUATS is the common channel/public seam. Stocked kits and
// ordinary Items read their own ATS; virtual bundle availability is the
// scarcest component ratio, with each component bucket calculated through
// computeATS rather than stored as another stock balance.
func ComputeSellableSKUATS(tenantID, sku, location string) (int, error) {
	bundle, found, err := loadActiveProductBundle(tenantID, sku)
	if err != nil {
		return 0, err
	}
	if !found || bundle.FulfillmentMode != "Virtual" {
		return rawSKUATS(tenantID, sku, location)
	}
	if location != "" {
		return virtualBundleATSAtLocation(tenantID, bundle, location)
	}
	// A network figure is the sum of complete bundles each location can
	// fulfill by itself. Taking min(sum(component ATS)) would advertise a kit
	// whose parts are stranded in different facilities even when the tenant's
	// allocation policy forbids split fulfillment.
	schema, _ := db.GetTenantSchema(tenantID)
	locations := map[string]bool{}
	for _, component := range bundle.Components {
		rows, queryErr := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT location_code FROM %s.inventory_availability WHERE sku=$1`, schema), component.SKU)
		if queryErr != nil {
			return 0, queryErr
		}
		for rows.Next() {
			var candidate string
			if rows.Scan(&candidate) == nil {
				locations[candidate] = true
			}
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			rows.Close()
			return 0, rowsErr
		}
		rows.Close()
	}
	total := 0
	for candidate := range locations {
		ats, err := virtualBundleATSAtLocation(tenantID, bundle, candidate)
		if err != nil {
			return 0, err
		}
		total += ats
	}
	return total, nil
}

func GetBundleAvailability(tenantID, bundleSKU string) ([]BundleAvailability, error) {
	bundle, found, err := loadActiveProductBundle(tenantID, bundleSKU)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("active ProductBundle for %s not found", bundleSKU)
	}
	schema, _ := db.GetTenantSchema(tenantID)
	locationSet := map[string]bool{}
	lookupSKUs := bundle.Components
	if bundle.FulfillmentMode == "Stocked" {
		lookupSKUs = []BundleComponent{{SKU: bundle.BundleSKU, Quantity: 1}}
	}
	for _, component := range lookupSKUs {
		rows, qErr := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT location_code FROM %s.inventory_availability WHERE sku=$1`, schema), component.SKU)
		if qErr != nil {
			return nil, qErr
		}
		for rows.Next() {
			var location string
			if rows.Scan(&location) == nil {
				locationSet[location] = true
			}
		}
		rows.Close()
	}
	out := []BundleAvailability{}
	for location := range locationSet {
		ats, err := ComputeSellableSKUATS(tenantID, bundleSKU, location)
		if err != nil {
			return nil, err
		}
		out = append(out, BundleAvailability{BundleSKU: bundleSKU, Location: location, ATS: ats, Derived: bundle.FulfillmentMode == "Virtual", ComponentCount: len(bundle.Components)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Location < out[j].Location })
	return out, nil
}

func ExpandSalesOrderBundles(tenantID string, input []SalesOrderLineInput) ([]SalesOrderLineInput, []map[string]interface{}, error) {
	expanded := make([]SalesOrderLineInput, 0, len(input))
	summaries := []map[string]interface{}{}
	for _, line := range input {
		bundle, found, err := loadActiveProductBundle(tenantID, line.SKU)
		if err != nil {
			return nil, nil, err
		}
		if !found || bundle.FulfillmentMode != "Virtual" {
			expanded = append(expanded, line)
			continue
		}
		if line.Qty <= 0 {
			return nil, nil, fmt.Errorf("bundle %s quantity must be positive", line.SKU)
		}
		bundleUnitPrice := line.UnitPrice
		if bundle.PricingMode == "Fixed Price" {
			bundleUnitPrice = bundle.FixedPrice
		} else if bundle.PricingMode == "Component Price" {
			bundleUnitPrice = 0
			for _, component := range bundle.Components {
				bundleUnitPrice += component.UnitPrice * float64(component.Quantity)
			}
		}
		totalPrice := bundleUnitPrice * float64(line.Qty)
		weightTotal := 0.0
		for _, component := range bundle.Components {
			weight := component.PriceWeight
			if weight <= 0 {
				weight = float64(component.Quantity)
			}
			weightTotal += weight
		}
		allocated := 0.0
		for index, component := range bundle.Components {
			childQty := line.Qty * component.Quantity
			unitPrice := component.UnitPrice
			if bundle.PricingMode != "Component Price" {
				weight := component.PriceWeight
				if weight <= 0 {
					weight = float64(component.Quantity)
				}
				componentTotal := totalPrice * weight / weightTotal
				if index == len(bundle.Components)-1 {
					componentTotal = totalPrice - allocated
				}
				allocated += componentTotal
				unitPrice = componentTotal / float64(childQty)
			}
			expanded = append(expanded, SalesOrderLineInput{SKU: component.SKU, Qty: childQty, UnitPrice: unitPrice, BundleSKU: bundle.BundleSKU, BundleQuantity: line.Qty, BundleComponentQuantity: component.Quantity, BundlePricingMode: bundle.PricingMode})
		}
		summaries = append(summaries, map[string]interface{}{"bundle_sku": bundle.BundleSKU, "quantity": line.Qty, "unit_price": bundleUnitPrice, "pricing_mode": bundle.PricingMode})
	}
	return expanded, summaries, nil
}

func PostBundleAssembly(tenantID, bundleSKU, location string, quantity int, operation, userID string, requestKeys ...string) (string, error) {
	if bundleSKU == "" || location == "" || quantity <= 0 {
		return "", errors.New("bundle_sku, location_code and positive quantity are required")
	}
	if operation != "Assemble" && operation != "Disassemble" {
		return "", errors.New("operation must be Assemble or Disassemble")
	}
	bundle, found, err := loadActiveProductBundle(tenantID, bundleSKU)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("active ProductBundle for %s not found", bundleSKU)
	}
	if bundle.FulfillmentMode != "Stocked" {
		return "", fmt.Errorf("virtual bundle %s cannot be physically assembled; change it to Stocked first", bundleSKU)
	}
	schema, _ := db.GetTenantSchema(tenantID)
	requestKey := ""
	if len(requestKeys) > 0 {
		requestKey = strings.TrimSpace(requestKeys[0])
	}
	if requestKey != "" {
		var existingID, existingStatus string
		err := db.DB.QueryRow(fmt.Sprintf(`SELECT id,status FROM %s.documents WHERE doctype='BundleAssembly' AND data->>'request_key'=$1 AND deleted_at IS NULL`, schema), requestKey).Scan(&existingID, &existingStatus)
		if err == nil {
			if existingStatus == "Completed" {
				return existingID, nil
			}
			return existingID, fmt.Errorf("bundle operation request %s is %s", requestKey, existingStatus)
		}
		if err != sql.ErrNoRows {
			return "", err
		}
	}
	id := NewDocID("BOP")
	snapshot, _ := json.Marshal(bundle.Components)
	postedAt := time.Now().UTC().Format(time.RFC3339)
	data, _ := json.Marshal(map[string]interface{}{"code": id, "bundle": bundle.ID, "bundle_sku": bundleSKU, "location_code": location, "quantity": quantity, "operation": operation, "request_key": requestKey, "component_snapshot": json.RawMessage(snapshot), "posted_at": postedAt, "status": "Completed"})
	type movement struct {
		sku string
		qty int
	}
	items := make([]movement, 0, len(bundle.Components)+1)
	direction := -1
	if operation == "Disassemble" {
		direction = 1
		items = append(items, movement{sku: bundleSKU, qty: -quantity})
	}
	for _, component := range bundle.Components {
		items = append(items, movement{sku: component.SKU, qty: direction * quantity * component.Quantity})
	}
	if operation == "Assemble" {
		items = append(items, movement{sku: bundleSKU, qty: quantity})
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}
	createdBy := userID
	if createdBy == "" {
		createdBy = "system"
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents(id,doctype,data,status,created_by) VALUES($1,'BundleAssembly',$2,'Completed',$3)`, schema), id, data, createdBy); err != nil {
		return "", err
	}
	for _, item := range items {
		if item.qty < 0 {
			var available, reserved, safety, blocked, qc, damaged, buffer, held int
			err := tx.QueryRow(fmt.Sprintf(`SELECT available,reserved,safety_stock,blocked,qc_hold,damaged,channel_buffer,hold_qty FROM %s.inventory_availability WHERE sku=$1 AND location_code=$2 FOR UPDATE`, schema), item.sku, location).Scan(&available, &reserved, &safety, &blocked, &qc, &damaged, &buffer, &held)
			if err == sql.ErrNoRows {
				return id, fmt.Errorf("insufficient stock for SKU %s at %s: no inventory record", item.sku, location)
			}
			if err != nil {
				return id, err
			}
			ats := computeATS(available, reserved, safety, blocked, qc, damaged, buffer, held)
			if ats+item.qty < 0 {
				return id, fmt.Errorf("insufficient ATS for SKU %s at %s: ATS %d, requested %d", item.sku, location, ats, -item.qty)
			}
		}
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.inventory_availability(sku,location_code,on_hand,available) VALUES($1,$2,$3,$3) ON CONFLICT(sku,location_code) DO UPDATE SET on_hand=%s.inventory_availability.on_hand+EXCLUDED.on_hand,available=%s.inventory_availability.available+EXCLUDED.available,updated_at=CURRENT_TIMESTAMP`, schema, schema, schema), item.sku, location, item.qty); err != nil {
			return id, err
		}
		ledgerID := NewDocIDCompact("SLE")
		ledgerData, _ := json.Marshal(map[string]interface{}{"id": ledgerID, "code": ledgerID, "item_id": item.sku, "warehouse_id": location, "qty": item.qty, "voucher_type": "BundleAssembly", "voucher_id": id, "idempotency_key": fmt.Sprintf("BundleAssembly:%s:%s:%s", id, location, item.sku), "user_id": userID, "status": "Active"})
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents(id,doctype,data,status,created_by) VALUES($1,'StockLedgerEntry',$2,'Active',$3)`, schema), ledgerID, ledgerData, createdBy); err != nil {
			return id, err
		}
	}
	if err := tx.Commit(); err != nil {
		return id, err
	}
	LogAuditEvent(tenantID, userID, "BUNDLE_"+strings.ToUpper(operation), "SUCCESS", fmt.Sprintf("%s %d x %s at %s (%s)", operation, quantity, bundleSKU, location, id))
	return id, nil
}
