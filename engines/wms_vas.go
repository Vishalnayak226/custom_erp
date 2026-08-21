package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
)

// Stage 42.4.11 - VAS / kitting / production staging as a WarehouseTask
// type (task_type 'VAS', already valid since 42.2.1). Reuses the existing
// Manufacturing BOM as the component source (engines/manufacturing.go's
// fetchBOM/explodeBOMComponents, engines/manufacturing_mrp.go's
// explodeBOMComponents) rather than defining a second BOM, per the plan's
// own instruction - and reuses PostInventoryLedger for the actual
// consume/produce stock movement, the same primitive
// IssueProductionMaterial (regular Production Orders) already uses for
// exactly this. What's deliberately NOT reused is ProductionOrder's own
// lifecycle/bom_snapshot/QC-gate machinery - a VAS task has its own
// lifecycle already (WarehouseTask's TransitionWarehouseTaskStatus), and
// dragging in ProductionOrder-specific state would be building a second
// task system on top of the first, the exact thing 42.2 exists to prevent.

// CreateVASTask (42.4.11) opens a VAS WarehouseTask whose output is
// outputQty units of outputItem, consuming bomID's components. bomID's own
// parent_item must match outputItem - a VAS task claiming to produce one
// item from a BOM built for a different one is refused at creation, not
// discovered at completion.
func CreateVASTask(tenantID, locationCode, fromBin, toBin, outputItem string, outputQty float64, bomID, queue, userID string) (string, error) {
	if bomID == "" || outputItem == "" || outputQty <= 0 {
		return "", errors.New("bom_id, output item and a positive output qty are required")
	}
	parentItem, _, err := fetchBOM(tenantID, bomID)
	if err != nil {
		return "", err
	}
	if parentItem != outputItem {
		return "", fmt.Errorf("BOM %s produces %s, not %s", bomID, parentItem, outputItem)
	}
	taskID, err := CreateWarehouseTask(tenantID, NewWarehouseTask{
		TaskType: "VAS", LocationCode: locationCode, FromBin: fromBin, ToBin: toBin,
		Item: outputItem, Qty: outputQty, Queue: queue,
	}, userID)
	if err != nil {
		return "", err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return taskID, err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || jsonb_build_object('bom_id', $1::text, 'output_qty', $2::numeric) WHERE doctype = 'WarehouseTask' AND id = $3`, schema),
		bomID, outputQty, taskID); err != nil {
		return taskID, err
	}
	return taskID, nil
}

// CompleteVASTask (42.4.11) explodes bom_id's components scaled to
// output_qty, consumes each from the task's location via PostInventoryLedger
// (refusing the whole call if any component is short, the same pre-check
// IssueProductionMaterial runs), then produces output_qty of the task's item
// at the same location and lands it in to_bin's bin_stock - a real putaway
// of the VAS output, not just an on-hand bump. outputLPN, when given, groups
// the output into that LPN via the existing AssignToLPN (26.5.4). Finally
// transitions the task to Completed.
func CompleteVASTask(tenantID, taskID, outputLPN, userID string) error {
	task, err := GetWarehouseTask(tenantID, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return fmt.Errorf("warehouse task %s not found", taskID)
	}
	if task.TaskType != "VAS" {
		return fmt.Errorf("task %s is not a VAS task", taskID)
	}
	if warehouseTaskTerminal(task.Status) {
		return fmt.Errorf("task %s is %s, a terminal state", taskID, task.Status)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'WarehouseTask' AND id = $1`, schema), taskID).Scan(&dataStr); err != nil {
		return err
	}
	var raw map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &raw)
	bomID := strField(raw, "bom_id")
	outputQty := numFromInterface(raw["output_qty"])
	if outputQty <= 0 {
		outputQty = task.Qty
	}
	if bomID == "" || outputQty <= 0 {
		return fmt.Errorf("task %s has no bom_id/output_qty configured", taskID)
	}
	if task.ToBin == "" {
		return fmt.Errorf("task %s has no to_bin configured for its output", taskID)
	}

	components, err := explodeBOMComponents(tenantID, bomID, outputQty, map[string]bool{}, 0)
	if err != nil {
		return err
	}
	if len(components) == 0 {
		return fmt.Errorf("BOM %s has no components to consume", bomID)
	}
	for _, c := range components {
		var available float64
		if errQ := db.DB.QueryRow(fmt.Sprintf(
			`SELECT available FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), c.Sku, task.LocationCode).Scan(&available); errQ != nil {
			available = 0
		}
		if available < c.Qty {
			return fmt.Errorf("VAS component stock is insufficient for SKU %s at %s: available %v, required %v", c.Sku, task.LocationCode, available, c.Qty)
		}
	}

	consumeItems := make([]interface{}, len(components))
	for i, c := range components {
		consumeItems[i] = map[string]interface{}{"sku": c.Sku, "qty": -c.Qty}
	}
	if _, err := PostInventoryLedgerWithVoucher(tenantID, task.LocationCode, consumeItems, false, "VAS", taskID, userID); err != nil {
		return fmt.Errorf("VAS component consumption failed: %v", err)
	}
	produceItems := []interface{}{map[string]interface{}{"sku": task.Item, "qty": outputQty}}
	if _, err := PostInventoryLedgerWithVoucher(tenantID, task.LocationCode, produceItems, false, "VAS", taskID, userID); err != nil {
		return fmt.Errorf("VAS output production failed: %v", err)
	}

	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.bin_stock (bin_code, sku, location_code, condition, qty)
		VALUES ($1, $2, $3, 'Good', $4)
		ON CONFLICT (bin_code, sku, condition) DO UPDATE SET
			qty = %s.bin_stock.qty + EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema, schema),
		task.ToBin, task.Item, task.LocationCode, int(outputQty)); err != nil {
		LogSystemError(tenantID, "", "WARN", "CompleteVASTask", fmt.Sprintf("failed to bin-stock VAS output for task %s: %v", taskID, err), "")
	}
	if outputLPN != "" {
		if err := AssignToLPN(tenantID, outputLPN, task.ToBin, task.Item, "Good", int(outputQty), userID); err != nil {
			LogSystemError(tenantID, "", "WARN", "CompleteVASTask", fmt.Sprintf("failed to assign VAS output of task %s to LPN %s: %v", taskID, outputLPN, err), "")
		} else if _, err := db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = data || jsonb_build_object('output_lpn', $1::text) WHERE doctype = 'WarehouseTask' AND id = $2`, schema),
			outputLPN, taskID); err != nil {
			LogSystemError(tenantID, "", "WARN", "CompleteVASTask", fmt.Sprintf("failed to record output_lpn for task %s: %v", taskID, err), "")
		}
	}

	LogAuditEvent(tenantID, userID, "WMS_VAS_COMPLETE", "SUCCESS",
		fmt.Sprintf("VAS task %s produced %v x %s into %s, consuming %d BOM %s component line(s)", taskID, outputQty, task.Item, task.ToBin, len(components), bomID))
	return TransitionWarehouseTaskStatus(tenantID, taskID, WTStatusCompleted, "", "", userID)
}
