package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// Manufacturing (Stage 13.13e, scoped MVP per the Manufacturing add-on
// blueprint Sec.7.2/7.3): single-level BOM + a linear Production Order
// (Draft -> Material Issued -> Completed) only. Routing/Work Centers,
// MRP/Planning, Quality Plans/QC gates, Costing Sheets/variance, and the
// other manufacturing models (process/assembly/MTO/subcontracting/repair)
// are explicitly out of scope.

// bomComponent is one line of a BOM's components JSON array.
type bomComponent struct {
	Sku string  `json:"sku"`
	Qty float64 `json:"qty"`
}

func fetchProductionOrder(tenantID, orderID string) (data map[string]interface{}, status string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", err
	}
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'ProductionOrder' AND id = $1`, schema), orderID).Scan(&dataStr, &status)
	if err != nil {
		return nil, "", err
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", err
	}
	return data, status, nil
}

func fetchBOM(tenantID, bomID string) (parentItem string, components []bomComponent, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, err
	}
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'BOM' AND id = $1`, schema), bomID).Scan(&dataStr)
	if err != nil {
		return "", nil, fmt.Errorf("BOM not found: %v", err)
	}
	var bom struct {
		ParentItem string `json:"parent_item"`
		Components string `json:"components"`
	}
	if err := json.Unmarshal([]byte(dataStr), &bom); err != nil {
		return "", nil, err
	}
	if err := json.Unmarshal([]byte(bom.Components), &components); err != nil {
		return "", nil, fmt.Errorf("BOM components field is not valid JSON: %v", err)
	}
	return bom.ParentItem, components, nil
}

func saveProductionOrderStatus(tenantID, orderID, newStatus string, data map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data["status"] = newStatus
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ProductionOrder' AND id = $3`, schema), marshaled, newStatus, orderID)
	return err
}

// IssueProductionMaterial implements the "Material Issue" step: looks up
// the order's BOM, computes each component's required quantity
// (component.qty * order quantity), and decrements them from inventory at
// the order's location via the existing PostInventoryLedger engine (Stage
// 1/hardening_roadmap Phase 2.1's stock floor-check applies here too - an
// order can't issue material it doesn't have).
func IssueProductionMaterial(tenantID, orderID string) error {
	data, status, err := fetchProductionOrder(tenantID, orderID)
	if err != nil {
		return fmt.Errorf("production order not found: %v", err)
	}
	if status != "Draft" {
		return fmt.Errorf("only a Draft production order can have material issued (current status: %s)", status)
	}

	bomID, _ := data["bom_id"].(string)
	location, _ := data["location"].(string)
	orderQty := 0.0
	if v, ok := data["quantity"].(float64); ok {
		orderQty = v
	}
	if orderQty <= 0 {
		return fmt.Errorf("production order quantity must be positive")
	}

	_, components, err := fetchBOM(tenantID, bomID)
	if err != nil {
		return err
	}
	if len(components) == 0 {
		return fmt.Errorf("BOM %s has no components to issue", bomID)
	}

	items := make([]interface{}, len(components))
	for i, c := range components {
		items[i] = map[string]interface{}{
			"sku": c.Sku,
			"qty": -(c.Qty * orderQty), // negative to decrement, matching checkout's convention
		}
	}
	if err := PostInventoryLedger(tenantID, location, items); err != nil {
		return fmt.Errorf("material issue failed: %v", err)
	}

	return saveProductionOrderStatus(tenantID, orderID, "Material Issued", data)
}

// CompleteProductionOrder implements "Finished Goods Receipt": posts the
// produced quantity of the BOM's parent item into inventory at the order's
// location.
func CompleteProductionOrder(tenantID, orderID string) error {
	data, status, err := fetchProductionOrder(tenantID, orderID)
	if err != nil {
		return fmt.Errorf("production order not found: %v", err)
	}
	if status != "Material Issued" {
		return fmt.Errorf("only a production order with material already issued can be completed (current status: %s)", status)
	}

	bomID, _ := data["bom_id"].(string)
	location, _ := data["location"].(string)
	orderQty := 0.0
	if v, ok := data["quantity"].(float64); ok {
		orderQty = v
	}

	parentItem, _, err := fetchBOM(tenantID, bomID)
	if err != nil {
		return err
	}
	if parentItem == "" {
		return fmt.Errorf("BOM %s has no parent_item configured", bomID)
	}

	items := []interface{}{
		map[string]interface{}{"sku": parentItem, "qty": orderQty},
	}
	if err := PostInventoryLedger(tenantID, location, items); err != nil {
		return fmt.Errorf("finished goods receipt failed: %v", err)
	}

	return saveProductionOrderStatus(tenantID, orderID, "Completed", data)
}
