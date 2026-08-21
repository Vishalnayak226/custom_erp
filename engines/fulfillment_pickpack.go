package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Stage 26.12.3 (OMS Maturity Sprint): splits FulfillmentTask's pick step
// into distinct, scan-validated Pick and Pack stages on top of the existing
// single-stage task (Pending -> Rejected/Dispatched, engines/fulfillment.go)
// - see docs/specs/oms_master_blueprint_reference.md §6/§12. Deliberately
// layered alongside engines/wms.go's GenerateBinPickList rather than merged
// into it: GenerateBinPickList answers "which bin do I walk to for this
// SKU" (a warehouse-internal, bin_stock-backed concern that doesn't apply to
// every FulfillmentTask location), while this file answers "did the picker/
// packer actually gather and box the right units" against the task's own
// items - reusing wms.go's structure would have coupled these two
// orthogonal concerns for no real benefit, which is exactly the kind of
// duplication/rebuild the design note warns against avoiding.
//
// Deliberately out of scope: auto-generating an invoice from the completed
// pack task (the retired prototype did this, §12 "worth preserving" notes
// it) - this repo's Order Engine (26.12.1) has no Order->Shipment->Invoice
// wiring yet for SalesOrder, so there is nothing real to generate the
// invoice from yet; a later Stage 26.12.4/26.12.5 item is the natural place
// to revisit this once that chain exists.

// pickPackTerminalStatuses guards every scan/short-pick/complete action
// below - a task that has already left the fulfillment lifecycle (shipped,
// or rejected/rerouted by TransitionTaskStatus) can no longer be scanned
// into. "Packed" is checked separately at each call site since it's not
// quite terminal (TransitionTaskStatus can still move it to Dispatched).
var pickPackTerminalStatuses = map[string]bool{
	"Dispatched": true,
	"Rejected":   true,
}

// fulfillmentTaskItem is one line of a FulfillmentTask's `items` array, with
// this Stage's pick/pack progress fields layered on top of the pre-existing
// sku/qty pair - additive JSON keys, so a task's existing items (created
// before this Stage, or never touched by it) simply default every new field
// to zero on first read.
type fulfillmentTaskItem struct {
	SKU       string
	Qty       int
	PickedQty int
	PackedQty int
	ShortQty  int
}

func fulfillmentItemFromMap(m map[string]interface{}) fulfillmentTaskItem {
	sku, _ := m["sku"].(string)
	return fulfillmentTaskItem{
		SKU:       sku,
		Qty:       int(numFromInterface(m["qty"])),
		PickedQty: int(numFromInterface(m["picked_qty"])),
		PackedQty: int(numFromInterface(m["packed_qty"])),
		ShortQty:  int(numFromInterface(m["short_qty"])),
	}
}

func (it fulfillmentTaskItem) toMap() map[string]interface{} {
	return map[string]interface{}{
		"sku":        it.SKU,
		"qty":        it.Qty,
		"picked_qty": it.PickedQty,
		"packed_qty": it.PackedQty,
		"short_qty":  it.ShortQty,
	}
}

// pickPackTask is a FulfillmentTask loaded for pick/pack mutation - data
// holds every other top-level document field (code/order_id/location_code/
// etc.) untouched so save() never clobbers them, only items/status.
type pickPackTask struct {
	id     string
	status string
	data   map[string]interface{}
	items  []fulfillmentTaskItem
}

func loadPickPackTask(schema, taskID string) (*pickPackTask, error) {
	var status, dataStr string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status, data FROM %s.documents WHERE doctype = 'FulfillmentTask' AND id = $1`, schema), taskID).
		Scan(&status, &dataStr)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("fulfillment task %s not found", taskID)
	} else if err != nil {
		return nil, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, err
	}
	var items []fulfillmentTaskItem
	if rawItems, ok := data["items"].([]interface{}); ok {
		for _, ri := range rawItems {
			if m, ok := ri.(map[string]interface{}); ok {
				items = append(items, fulfillmentItemFromMap(m))
			}
		}
	}
	return &pickPackTask{id: taskID, status: status, data: data, items: items}, nil
}

// FulfillmentTaskLocationCode returns a FulfillmentTask's location_code.
// Exists so a caller outside this package (Stage 42.2.2's WarehouseTask
// retrofit, called from the pick/pack scan handlers) can log a completed
// task without ScanPickItem/ScanPackItem themselves taking on a userID
// parameter neither has today - the same signature-change 26.5.13's own
// instrumentation attempt was rejected over.
func FulfillmentTaskLocationCode(tenantID, taskID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	task, err := loadPickPackTask(schema, taskID)
	if err != nil {
		return "", err
	}
	loc, _ := task.data["location_code"].(string)
	return loc, nil
}

func (t *pickPackTask) save(schema string) error {
	itemMaps := make([]map[string]interface{}, len(t.items))
	for i, it := range t.items {
		itemMaps[i] = it.toMap()
	}
	t.data["items"] = itemMaps
	t.data["status"] = t.status
	marshaled, err := json.Marshal(t.data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'FulfillmentTask' AND id = $3`, schema),
		marshaled, t.status, t.id)
	return err
}

// resolveScanToItem resolves a scanned string to an Item's SKU + display
// name - tries an exact barcode match first, then falls back to treating the
// scan as the SKU/code itself, same degrade-gracefully convention the GRN
// workbench's barcode preview already uses (Stage 26.3.1).
func resolveScanToItem(schema, scan string) (sku, name string, found bool, err error) {
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data->>'code', data->>'name' FROM %s.documents WHERE doctype = 'Item' AND data->>'barcode' = $1 AND deleted_at IS NULL LIMIT 1`, schema),
		scan).Scan(&sku, &name)
	if err == nil {
		return sku, name, true, nil
	} else if err != sql.ErrNoRows {
		return "", "", false, err
	}
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data->>'code', data->>'name' FROM %s.documents WHERE doctype = 'Item' AND data->>'code' = $1 AND deleted_at IS NULL LIMIT 1`, schema),
		scan).Scan(&sku, &name)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	} else if err != nil {
		return "", "", false, err
	}
	return sku, name, true, nil
}

// recomputeTaskStatus flips a still-Pending task to Picking the moment any
// pick progress happens (a scan or a short-pick) - this repo's existing
// FulfillmentTask.status vocabulary (Pending/Picking/Packed/Dispatched/
// Rejected, db/migration.sql) already anticipated exactly this granularity
// and no more, so both the pick and pack stages live under "Picking" until
// CompletePackTask explicitly sets "Packed" - no schema change needed to
// add finer-grained intermediate statuses that nothing else would consume.
func recomputeTaskStatus(task *pickPackTask) {
	if task.status == "Pending" {
		task.status = "Picking"
	}
}

// scanGuard is the shared entry check for every pick/pack action below - a
// task that's Dispatched/Rejected/already-Packed can't be scanned into or
// short-picked any further.
func scanGuard(task *pickPackTask) error {
	if task.status == "Packed" || pickPackTerminalStatuses[task.status] {
		return fmt.Errorf("fulfillment task %s is %s and can no longer be scanned", task.id, task.status)
	}
	return nil
}

// ScanPickItem is the Pick stage's scan-first validation (§12): resolves the
// scanned barcode/SKU, matches it against the first task line for that SKU
// still short of its pick target (qty minus any already-short-picked
// remainder, so a settled shortfall is never re-solicited), and increments
// that line's picked_qty by one. A scan that doesn't belong to this task at
// all, or belongs to a line already fully picked/short-picked, is rejected
// with a message naming the actual product involved (§12: "not just
// 'invalid scan'") rather than a bare error.
func ScanPickItem(tenantID, taskID, scan string) (sku string, pickedQty int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", 0, err
	}
	resolvedSKU, name, found, err := resolveScanToItem(schema, scan)
	if err != nil {
		return "", 0, err
	}
	if !found {
		return "", 0, fmt.Errorf("scanned code %q does not match any known item", scan)
	}

	task, err := loadPickPackTask(schema, taskID)
	if err != nil {
		return "", 0, err
	}
	if err := scanGuard(task); err != nil {
		return "", 0, err
	}

	matchIdx, sameSKUExists := -1, false
	for i, it := range task.items {
		if it.SKU != resolvedSKU {
			continue
		}
		sameSKUExists = true
		if it.PickedQty < it.Qty-it.ShortQty {
			matchIdx = i
			break
		}
	}
	if matchIdx == -1 {
		if sameSKUExists {
			return "", 0, fmt.Errorf("%q (%s) is already fully picked (or short-picked) for this task - duplicate scan", name, resolvedSKU)
		}
		return "", 0, fmt.Errorf("scanned code belongs to %q (%s), which is not part of this task", name, resolvedSKU)
	}

	task.items[matchIdx].PickedQty++
	recomputeTaskStatus(task)
	if err := task.save(schema); err != nil {
		return "", 0, err
	}
	return resolvedSKU, task.items[matchIdx].PickedQty, nil
}

// ScanPackItem is the Pack stage's analog to ScanPickItem, matching the
// first line where packed_qty is still below picked_qty. The blueprint's
// hard rule that packed quantity can never exceed picked quantity (§6)
// falls directly out of this match condition - a line with
// packed_qty == picked_qty can never match again, so packing ahead of
// picking is structurally impossible rather than separately enforced.
func ScanPackItem(tenantID, taskID, scan string) (sku string, packedQty int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", 0, err
	}
	resolvedSKU, name, found, err := resolveScanToItem(schema, scan)
	if err != nil {
		return "", 0, err
	}
	if !found {
		return "", 0, fmt.Errorf("scanned code %q does not match any known item", scan)
	}

	task, err := loadPickPackTask(schema, taskID)
	if err != nil {
		return "", 0, err
	}
	if err := scanGuard(task); err != nil {
		return "", 0, err
	}

	matchIdx, sameSKUExists := -1, false
	for i, it := range task.items {
		if it.SKU != resolvedSKU {
			continue
		}
		sameSKUExists = true
		if it.PackedQty < it.PickedQty {
			matchIdx = i
			break
		}
	}
	if matchIdx == -1 {
		if sameSKUExists {
			return "", 0, fmt.Errorf("%q (%s) is already fully packed (or has nothing picked yet) for this task - duplicate scan", name, resolvedSKU)
		}
		return "", 0, fmt.Errorf("scanned code belongs to %q (%s), which is not part of this task", name, resolvedSKU)
	}

	task.items[matchIdx].PackedQty++
	recomputeTaskStatus(task)
	if err := task.save(schema); err != nil {
		return "", 0, err
	}
	return resolvedSKU, task.items[matchIdx].PackedQty, nil
}

// ShortPickLine marks a line's entire remaining pick shortfall as short in
// one action (§12: "no partial-short-then-more-picking modeled" - the
// picker isn't asked how much is short, only that the rest can't be found).
// Requires a mandatory Active ReasonCode in a new "Short Pick" category
// (db/migrations_stage26_12_3_pick_pack.sql extends the Stage 26.12.9
// ReasonCode.category Select options), the same mandatory-reason-code
// convention the Order Engine's Hold/Cancel actions already use.
func ShortPickLine(tenantID, taskID, sku, reasonCode string) error {
	if err := requireActiveReasonCode(tenantID, reasonCode, "Short Pick"); err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	task, err := loadPickPackTask(schema, taskID)
	if err != nil {
		return err
	}
	if err := scanGuard(task); err != nil {
		return err
	}

	found := false
	for i, it := range task.items {
		if it.SKU != sku {
			continue
		}
		found = true
		remaining := it.Qty - it.PickedQty - it.ShortQty
		if remaining <= 0 {
			return fmt.Errorf("SKU %s has no remaining shortfall to short-pick (qty=%d, picked=%d, already short=%d)", sku, it.Qty, it.PickedQty, it.ShortQty)
		}
		task.items[i].ShortQty += remaining
		break
	}
	if !found {
		return fmt.Errorf("SKU %s is not part of fulfillment task %s", sku, taskID)
	}

	recomputeTaskStatus(task)
	return task.save(schema)
}

// CompletePackTask is the explicit, blockable pack-completion action (§12:
// "block pack completion on any exact-qty mismatch, not just a shortfall").
// Two invariants must both hold for every line: the pick stage must be
// fully resolved (picked_qty+short_qty == qty - no line left silently
// untouched), and packed_qty must exactly equal picked_qty (not just
// no-more-than, per the checklist's own "not just a shortfall" wording).
// Only once both hold does the task move to Packed.
func CompletePackTask(tenantID, taskID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	task, err := loadPickPackTask(schema, taskID)
	if err != nil {
		return err
	}
	if task.status == "Packed" || pickPackTerminalStatuses[task.status] {
		return fmt.Errorf("fulfillment task %s is already %s", taskID, task.status)
	}

	for _, it := range task.items {
		if it.PickedQty+it.ShortQty != it.Qty {
			return fmt.Errorf("cannot complete packing: SKU %s still has an unresolved pick shortfall (qty=%d, picked=%d, short=%d) - pick or short-pick the remainder first", it.SKU, it.Qty, it.PickedQty, it.ShortQty)
		}
		if it.PackedQty != it.PickedQty {
			return fmt.Errorf("cannot complete packing: SKU %s has picked_qty=%d but packed_qty=%d", it.SKU, it.PickedQty, it.PackedQty)
		}
	}

	task.status = "Packed"
	return task.save(schema)
}
