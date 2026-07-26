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

// bomComponent is one line of a BOM's components JSON array. SubBom/
// ScrapPercent (Stage 26.9.1/26.9.4) are additive - a component line with
// neither set behaves exactly as it did before that Stage.
type bomComponent struct {
	Sku          string  `json:"sku"`
	Qty          float64 `json:"qty"`
	SubBom       string  `json:"sub_bom,omitempty"`       // if set, this line is itself a manufactured sub-assembly (references another BOM's id) rather than a directly-issuable raw material
	ScrapPercent float64 `json:"scrap_percent,omitempty"` // extra qty issued to account for expected wastage on this line, e.g. 5 = issue 5% more than the pure qty*order-qty requirement
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
	if bomID == "" {
		return "", nil, &ValidationError{Code: "MANUFA-0140", Message: "no BOM is maintained for this finished good"}
	}
	var dataStr, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'BOM' AND id = $1`, schema), bomID).Scan(&dataStr, &status)
	if err != nil {
		return "", nil, &ValidationError{Code: "MANUFA-0140", Message: fmt.Sprintf("BOM %s not found", bomID)}
	}
	if status != "" && status != "Active" {
		return "", nil, &ValidationError{Code: "MANUFA-0141", Message: fmt.Sprintf("BOM %s is %s, not Active - select an active BOM", bomID, status)}
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

	// Stage 26.9.1: explodeBOMComponents recurses through any sub_bom
	// references and folds each line's scrap_percent (26.9.4) into the
	// returned per-unit quantity, so the loop below is unchanged from
	// before that Stage - it always just sees a flat list of raw materials.
	components, err := explodeBOMComponents(tenantID, bomID, 1.0, map[string]bool{}, 0)
	if err != nil {
		return err
	}
	if len(components) == 0 {
		return fmt.Errorf("BOM %s has no components to issue", bomID)
	}

	// MANUFA-0143: an explicit pre-check (rather than just relying on
	// PostInventoryLedger's own generic floor-check error below) so a raw
	// material shortage reports precisely, the same reason
	// DispatchTransferOrder (engines/transfer_orders.go) checks available
	// stock itself before posting instead of leaving it to the shared
	// ledger call - PostInventoryLedger is also used by checkout/GRN, which
	// have no catalog code of their own for this scenario, so its error
	// can't be changed without misattaching MANUFA-0143 to those callers too.
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	for _, c := range components {
		required := c.Qty * orderQty
		var available float64
		if errQ := db.DB.QueryRow(fmt.Sprintf(`SELECT available FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), c.Sku, location).Scan(&available); errQ != nil {
			available = 0
		}
		if available < required {
			return &ValidationError{Code: "MANUFA-0143", Message: fmt.Sprintf("raw material stock is insufficient for SKU %s at %s: available %v, required %v", c.Sku, location, available, required)}
		}
	}

	items := make([]interface{}, len(components))
	for i, c := range components {
		items[i] = map[string]interface{}{
			"sku": c.Sku,
			"qty": -(c.Qty * orderQty), // negative to decrement, matching checkout's convention
		}
	}
	if _, err := PostInventoryLedger(tenantID, location, items, false); err != nil {
		return fmt.Errorf("material issue failed: %v", err)
	}

	// Stage 26.9.2: snapshot the exploded requirement as issued, so
	// finishProductionQty can detect (MFG-0276) if the BOM document itself
	// is edited after this point, before the order is completed.
	if snapBytes, errM := json.Marshal(components); errM == nil {
		data["bom_snapshot"] = string(snapBytes)
	}

	return saveProductionOrderStatus(tenantID, orderID, "Material Issued", data)
}

// CompleteProductionOrder implements "Finished Goods Receipt": posts the
// produced quantity of the BOM's parent item (and Stage 26.9.4 by-products)
// into inventory at the order's location. Kept as a one-shot "complete the
// entire remaining balance" call for backward compatibility with the
// existing frontend action and handler - Stage 26.9.6's finishProductionQty
// (engines/manufacturing_mrp.go) is what actually does the work now, shared
// with the new explicit ReportPartialCompletion.
func CompleteProductionOrder(tenantID, orderID string) error {
	data, status, err := fetchProductionOrder(tenantID, orderID)
	if err != nil {
		return fmt.Errorf("production order not found: %v", err)
	}
	if status != "Material Issued" && status != "In Process" {
		return fmt.Errorf("only a production order with material already issued can be completed (current status: %s)", status)
	}

	orderQty := numFromInterface(data["quantity"])
	completedSoFar := numFromInterface(data["completed_qty"])
	remaining := orderQty - completedSoFar
	if remaining <= 0 {
		return fmt.Errorf("production order has no remaining quantity to complete")
	}
	return finishProductionQty(tenantID, orderID, remaining)
}
