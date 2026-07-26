package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Stage 26.9 (Manufacturing/MRP Maturity Sprint) - extends Stage 13.13e's
// scoped-MVP single-level BOM + linear Production Order rather than
// replacing it. 26.9.9 (finite/infinite capacity scheduling, subcontracting)
// stays out of scope per the checklist's own [P2] note.

// ============================================================
// 26.9.1: Multi-level BOM
// ============================================================

const maxBOMExplosionDepth = 10

// explodeBOMComponents recursively resolves a BOM's components into a flat
// list of pure raw-material (leaf) requirements per one unit of the BOM's
// own parent item, multiplying down through any nested sub_bom references
// and folding each line's scrap_percent (26.9.4) in as it goes. Multiple
// paths reaching the same leaf SKU are merged into one line, so a caller
// (IssueProductionMaterial, GetMRPSuggestions) still only has to multiply
// by the order quantity once - identical to how a single-level BOM's flat
// components list already worked before this Stage. depth/visited guard
// against a BOM referencing itself directly or through a cycle.
func explodeBOMComponents(tenantID, bomID string, multiplier float64, visited map[string]bool, depth int) ([]bomComponent, error) {
	if depth > maxBOMExplosionDepth {
		return nil, &ValidationError{Message: fmt.Sprintf("BOM %s nests more than %d levels deep or contains a circular reference", bomID, maxBOMExplosionDepth)}
	}
	if visited[bomID] {
		return nil, &ValidationError{Message: fmt.Sprintf("BOM %s references itself, directly or through a sub-BOM - circular BOM structure", bomID)}
	}
	nextVisited := make(map[string]bool, len(visited)+1)
	for k := range visited {
		nextVisited[k] = true
	}
	nextVisited[bomID] = true

	_, components, err := fetchBOM(tenantID, bomID)
	if err != nil {
		return nil, err
	}

	merged := map[string]float64{}
	var order []string
	addQty := func(sku string, qty float64) {
		if _, ok := merged[sku]; !ok {
			order = append(order, sku)
		}
		merged[sku] += qty
	}

	for _, c := range components {
		lineQty := c.Qty * multiplier * (1 + c.ScrapPercent/100)
		if c.SubBom != "" {
			sub, err := explodeBOMComponents(tenantID, c.SubBom, lineQty, nextVisited, depth+1)
			if err != nil {
				return nil, err
			}
			for _, s := range sub {
				addQty(s.Sku, s.Qty)
			}
			continue
		}
		addQty(c.Sku, lineQty)
	}

	out := make([]bomComponent, 0, len(order))
	for _, sku := range order {
		out = append(out, bomComponent{Sku: sku, Qty: merged[sku]})
	}
	return out, nil
}

// fetchBOMData is the map[string]interface{} sibling of fetchBOM, for
// callers that need fields fetchBOM's narrow (parentItem, components)
// return doesn't carry - is_default/effective dates, yield/by-products,
// qc_required, standard_cost.
func fetchBOMData(tenantID, bomID string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'BOM' AND id = $1`, schema), bomID).Scan(&dataStr); err != nil {
		return nil, &ValidationError{Code: "MANUFA-0140", Message: fmt.Sprintf("BOM %s not found", bomID)}
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, err
	}
	return data, nil
}

// ============================================================
// 26.9.2: Alternate BOM + effective-dating
// ============================================================

// GetActiveBOMForItem resolves which BOM a new Production Order for
// parentItem should default to: the Active BOM marked is_default="Yes"
// whose effective date window (if set) covers asOfDate, falling back to the
// most recently created Active BOM for the item if no default is
// configured - so a tenant with exactly one BOM per item (the only shape
// that existed before this Stage) sees identical behavior with zero setup.
func GetActiveBOMForItem(tenantID, parentItem, asOfDate string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if asOfDate == "" {
		asOfDate = time.Now().Format("2006-01-02")
	}

	var id string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'BOM' AND status = 'Active' AND data->>'parent_item' = $1
		  AND COALESCE(data->>'is_default', '') = 'Yes'
		  AND (COALESCE(data->>'effective_from', '') = '' OR data->>'effective_from' <= $2)
		  AND (COALESCE(data->>'effective_to', '') = '' OR data->>'effective_to' >= $2)
		ORDER BY updated_at DESC LIMIT 1`, schema), parentItem, asOfDate).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'BOM' AND status = 'Active' AND data->>'parent_item' = $1
		ORDER BY created_at DESC LIMIT 1`, schema), parentItem).Scan(&id)
	if err == sql.ErrNoRows {
		return "", &ValidationError{Code: "MANUFA-0140", Message: fmt.Sprintf("no active BOM is maintained for finished good %s", parentItem)}
	}
	return id, err
}

// AcknowledgeBOMVariance lets a user explicitly accept that the BOM changed
// after a production order's material was issued (MFG-0276) and proceed to
// completion anyway, rather than being permanently blocked.
func AcknowledgeBOMVariance(tenantID, orderID, actorUserID string) error {
	data, status, err := fetchProductionOrder(tenantID, orderID)
	if err != nil {
		return fmt.Errorf("production order not found: %v", err)
	}
	data["bom_variance_acknowledged"] = true
	if err := saveProductionOrderStatus(tenantID, orderID, status, data); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actorUserID, "PRODUCTION_ORDER_BOM_VARIANCE_ACK", "SUCCESS", fmt.Sprintf("BOM variance acknowledged for production order %s", orderID))
	return nil
}

// ============================================================
// 26.9.4: Scrap/yield + co/by-product output
// ============================================================

type byProductLine struct {
	Sku        string  `json:"sku"`
	QtyPerUnit float64 `json:"qty_per_unit"`
}

func byProducts(bomData map[string]interface{}) []byProductLine {
	raw, _ := bomData["by_products"].(string)
	if raw == "" {
		return nil
	}
	var out []byProductLine
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func qcRequired(bomData map[string]interface{}) bool {
	v, _ := bomData["qc_required"].(string)
	return v == "Yes"
}

type scrapEntry struct {
	Sku       string  `json:"sku"`
	Qty       float64 `json:"qty"`
	Reason    string  `json:"reason"`
	Timestamp string  `json:"timestamp"`
}

// PostScrap logs a scrap write-off against a production order. Audit-only -
// the scrapped quantity was already decremented from inventory at material
// issue time (or was never producible in the first place), so there is
// nothing further to post to inventory_availability here.
func PostScrap(tenantID, orderID, sku string, qty float64, reason, actorUserID string) error {
	if strings.TrimSpace(reason) == "" {
		return &ValidationError{Code: "MANUFA-0146", Message: "a reason is required before posting a scrap quantity"}
	}
	if qty <= 0 {
		return fmt.Errorf("scrap quantity must be positive")
	}
	data, status, err := fetchProductionOrder(tenantID, orderID)
	if err != nil {
		return fmt.Errorf("production order not found: %v", err)
	}

	var log []scrapEntry
	if raw, ok := data["scrap_log"].(string); ok && raw != "" {
		_ = json.Unmarshal([]byte(raw), &log)
	}
	log = append(log, scrapEntry{Sku: sku, Qty: qty, Reason: reason, Timestamp: time.Now().Format(time.RFC3339)})
	marshaled, _ := json.Marshal(log)
	data["scrap_log"] = string(marshaled)

	if err := saveProductionOrderStatus(tenantID, orderID, status, data); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actorUserID, "PRODUCTION_SCRAP_POSTED", "SUCCESS", fmt.Sprintf("Scrap posted for order %s: %s qty %v (%s)", orderID, sku, qty, reason))
	return nil
}

// ============================================================
// 26.9.3 + 26.9.6: Work centers/routing, WIP tracking, rework
// ============================================================

type routingOperation struct {
	Seq                int     `json:"seq"`
	OperationName      string  `json:"operation_name"`
	WorkCenterID       string  `json:"work_center_id"`
	SetupTimeMins      float64 `json:"setup_time_mins"`
	RunTimeMinsPerUnit float64 `json:"run_time_mins_per_unit"`
}

type operationProgress struct {
	Seq         int    `json:"seq"`
	Status      string `json:"status"`
	ConfirmedAt string `json:"confirmed_at"`
}

func fetchRoutingOperations(tenantID, routingID string) ([]routingOperation, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var opsRaw, status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'operations', status FROM %s.documents WHERE doctype = 'Routing' AND id = $1`, schema), routingID).Scan(&opsRaw, &status); err != nil {
		return nil, &ValidationError{Code: "MANUFA-0142", Message: fmt.Sprintf("routing %s not found", routingID)}
	}
	if status != "" && status != "Active" {
		return nil, &ValidationError{Code: "MANUFA-0142", Message: fmt.Sprintf("routing %s is %s, not Active", routingID, status)}
	}
	var ops []routingOperation
	if err := json.Unmarshal([]byte(opsRaw), &ops); err != nil {
		return nil, fmt.Errorf("routing operations field is not valid JSON: %v", err)
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].Seq < ops[j].Seq })
	return ops, nil
}

func workCenterDailyCapacityMins(tenantID, workCenterID string) (float64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'WorkCenter' AND id = $1`, schema), workCenterID).Scan(&dataStr); err != nil {
		return 0, err
	}
	var d map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &d); err != nil {
		return 0, err
	}
	return numFromInterface(d["capacity_hours_per_day"]) * 60, nil
}

// ConfirmOperation marks one routing operation confirmed for a production
// order, enforcing sequence (MANUFA-0147: an operation can't be confirmed
// before the ones ahead of it) and flagging - not blocking, matching
// MFG-0277's own Blocking:false/HTTP-200 catalog row - a work center whose
// capacity this single order's run alone would exceed for one day.
func ConfirmOperation(tenantID, orderID string, seq int, actorUserID string) (capacityWarning string, err error) {
	data, status, ferr := fetchProductionOrder(tenantID, orderID)
	if ferr != nil {
		return "", fmt.Errorf("production order not found: %v", ferr)
	}
	if status != "Material Issued" && status != "In Process" {
		return "", fmt.Errorf("operations can only be confirmed once material has been issued (current status: %s)", status)
	}
	routingID, _ := data["routing_id"].(string)
	if routingID == "" {
		return "", &ValidationError{Code: "MANUFA-0142", Message: "no routing is configured for this production order"}
	}
	ops, ferr := fetchRoutingOperations(tenantID, routingID)
	if ferr != nil {
		return "", ferr
	}

	var progress []operationProgress
	if raw, ok := data["operations_progress"].(string); ok && raw != "" {
		_ = json.Unmarshal([]byte(raw), &progress)
	}
	confirmedSeqs := map[int]bool{}
	for _, p := range progress {
		if p.Status == "Confirmed" {
			confirmedSeqs[p.Seq] = true
		}
	}
	if confirmedSeqs[seq] {
		return "", fmt.Errorf("operation %d is already confirmed", seq)
	}
	var target *routingOperation
	for i := range ops {
		if ops[i].Seq == seq {
			target = &ops[i]
			break
		}
		if ops[i].Seq < seq && !confirmedSeqs[ops[i].Seq] {
			return "", &ValidationError{Code: "MANUFA-0147", Message: fmt.Sprintf("operation %d must be confirmed before operation %d", ops[i].Seq, seq)}
		}
	}
	if target == nil {
		return "", fmt.Errorf("operation sequence %d not found on routing %s", seq, routingID)
	}

	orderQty := numFromInterface(data["quantity"])
	if target.WorkCenterID != "" {
		if capMinsPerDay, cerr := workCenterDailyCapacityMins(tenantID, target.WorkCenterID); cerr == nil && capMinsPerDay > 0 {
			needed := target.SetupTimeMins + target.RunTimeMinsPerUnit*orderQty
			if needed > capMinsPerDay {
				capacityWarning = fmt.Sprintf("MFG-0277: operation %d needs %.0f minutes at work center %s, exceeding its %.0f minute/day capacity", seq, needed, target.WorkCenterID, capMinsPerDay)
				LogSystemError(tenantID, "", "WARN", "ConfirmOperation", capacityWarning, "")
			}
		}
	}

	progress = append(progress, operationProgress{Seq: seq, Status: "Confirmed", ConfirmedAt: time.Now().Format(time.RFC3339)})
	marshaled, _ := json.Marshal(progress)
	data["operations_progress"] = string(marshaled)
	if err := saveProductionOrderStatus(tenantID, orderID, "In Process", data); err != nil {
		return capacityWarning, err
	}
	LogAuditEvent(tenantID, actorUserID, "PRODUCTION_OPERATION_CONFIRMED", "SUCCESS", fmt.Sprintf("Operation %d confirmed for order %s", seq, orderID))
	return capacityWarning, nil
}

// finishProductionQty posts a batch of finished goods (and any BOM-
// configured by-products) into inventory for a production order, advancing
// its cumulative completed_qty. Shared by CompleteProductionOrder
// (engines/manufacturing.go, one-shot "complete the remaining balance") and
// ReportPartialCompletion below (explicit partial qty), so both stay
// behaviorally identical for a simple order that completes in one call.
func finishProductionQty(tenantID, orderID string, qty float64) error {
	data, status, err := fetchProductionOrder(tenantID, orderID)
	if err != nil {
		return fmt.Errorf("production order not found: %v", err)
	}
	if status != "Material Issued" && status != "In Process" {
		return fmt.Errorf("only a production order with material issued (or already in process) can report completion (current status: %s)", status)
	}
	if qty <= 0 {
		return fmt.Errorf("completion quantity must be positive")
	}

	bomID, _ := data["bom_id"].(string)
	location, _ := data["location"].(string)
	orderQty := numFromInterface(data["quantity"])
	completedSoFar := numFromInterface(data["completed_qty"])

	newCompleted := completedSoFar + qty
	if newCompleted > orderQty+0.0001 {
		return &ValidationError{Code: "MANUFA-0145", Message: fmt.Sprintf("completing %v more units would bring total completed to %v, exceeding the order quantity of %v", qty, newCompleted, orderQty)}
	}

	bomData, err := fetchBOMData(tenantID, bomID)
	if err != nil {
		return err
	}
	parentItem, _ := bomData["parent_item"].(string)
	if parentItem == "" {
		return fmt.Errorf("BOM %s has no parent_item configured", bomID)
	}

	willClose := newCompleted >= orderQty-0.0001
	if willClose {
		// 26.9.2/MFG-0276: has the BOM changed since material was issued?
		if snap, _ := data["bom_snapshot"].(string); snap != "" {
			if fresh, ferr := explodeBOMComponents(tenantID, bomID, 1.0, map[string]bool{}, 0); ferr == nil {
				if freshBytes, merr := json.Marshal(fresh); merr == nil {
					ack, _ := data["bom_variance_acknowledged"].(bool)
					if snap != string(freshBytes) && !ack {
						return &ValidationError{Code: "MFG-0276", Message: "the BOM was changed after this production order's material was issued - re-plan the order or acknowledge the variance to proceed"}
					}
				}
			}
		}
		// 26.9.7: QC gate.
		if qcRequired(bomData) {
			passed, failed, qerr := qcGateStatus(tenantID, orderID)
			if qerr != nil {
				return qerr
			}
			if failed {
				return &ValidationError{Code: "MFG-0278", Message: "quality inspection for this production order failed - it cannot be released to available stock. Route the batch to rework/scrap or record a new passing inspection."}
			}
			if !passed {
				return &ValidationError{Code: "MANUFA-0148", Message: "a Quality Inspection for this production order must be submitted and Approved (with result Pass) before it can be completed"}
			}
		}
	}

	items := []interface{}{map[string]interface{}{"sku": parentItem, "qty": qty}}
	for _, bp := range byProducts(bomData) {
		items = append(items, map[string]interface{}{"sku": bp.Sku, "qty": bp.QtyPerUnit * qty})
	}
	if _, err := PostInventoryLedger(tenantID, location, items, false); err != nil {
		return fmt.Errorf("finished goods receipt failed: %v", err)
	}

	data["completed_qty"] = newCompleted
	newStatus := "In Process"
	if willClose {
		newStatus = "Completed"
	}
	return saveProductionOrderStatus(tenantID, orderID, newStatus, data)
}

// ReportPartialCompletion (26.9.6) posts a partial finished-goods batch
// without necessarily closing the order - it stays "In Process" until
// cumulative completed_qty reaches the order quantity.
func ReportPartialCompletion(tenantID, orderID string, qty float64) error {
	return finishProductionQty(tenantID, orderID, qty)
}

type reworkEntry struct {
	Qty       float64 `json:"qty"`
	Reason    string  `json:"reason"`
	Timestamp string  `json:"timestamp"`
}

// SendToRework (26.9.6) logs a defective quantity pulled aside for rework.
// Deliberately light: it records the event (audit trail + a visible
// rework_qty total) rather than modeling a full re-issue/rework production
// sub-order loop, which is out of scope for this Stage's MVP.
func SendToRework(tenantID, orderID string, qty float64, reason, actorUserID string) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("a reason is required to send a quantity to rework")
	}
	if qty <= 0 {
		return fmt.Errorf("rework quantity must be positive")
	}
	data, status, err := fetchProductionOrder(tenantID, orderID)
	if err != nil {
		return fmt.Errorf("production order not found: %v", err)
	}

	reworkSoFar := numFromInterface(data["rework_qty"])
	data["rework_qty"] = reworkSoFar + qty

	var log []reworkEntry
	if raw, ok := data["rework_log"].(string); ok && raw != "" {
		_ = json.Unmarshal([]byte(raw), &log)
	}
	log = append(log, reworkEntry{Qty: qty, Reason: reason, Timestamp: time.Now().Format(time.RFC3339)})
	marshaled, _ := json.Marshal(log)
	data["rework_log"] = string(marshaled)

	if err := saveProductionOrderStatus(tenantID, orderID, status, data); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actorUserID, "PRODUCTION_REWORK_LOGGED", "SUCCESS", fmt.Sprintf("Rework logged for order %s: qty %v (%s)", orderID, qty, reason))
	return nil
}

// ============================================================
// 26.9.7: QC gate before completion
// ============================================================

// qcGateStatus reuses the existing generic approval engine (SubmitForApproval
// / DecideApproval) via the QualityInspection doctype - a QualityInspection
// linked to this order (production_order_id) submitted and Approved, with
// result "Pass", clears the gate; one Approved with result "Fail" blocks
// completion outright (MFG-0278) rather than just "still pending" (MANUFA-0148).
func qcGateStatus(tenantID, orderID string) (passed bool, failed bool, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, false, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data, status FROM %s.documents
		WHERE doctype = 'QualityInspection' AND data->>'production_order_id' = $1
		ORDER BY updated_at DESC`, schema), orderID)
	if err != nil {
		return false, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var dataStr, status string
		if err := rows.Scan(&dataStr, &status); err != nil {
			return false, false, err
		}
		if status != "Approved" {
			continue
		}
		var d map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &d); err != nil {
			continue
		}
		result, _ := d["result"].(string)
		if result == "Pass" {
			return true, false, nil
		}
		if result == "Fail" {
			return false, true, nil
		}
	}
	return false, false, nil
}

// ============================================================
// 26.9.5: Basic MRP reorder suggestion for manufactured items
// ============================================================

// MRPSuggestion reuses GetReplenishmentSuggestions' own reorder-point
// formula (velocity * lead time + safety stock) against a manufactured
// item's finished-good stock, rather than a new planning engine - the
// checklist's own instruction. RawMaterialShortfalls additionally explodes
// the suggested production quantity through the item's active BOM so a
// planner can see which raw materials would need to be purchased first.
type MRPSuggestion struct {
	ParentItem             string                 `json:"parent_item"`
	LocationCode           string                 `json:"location_code"`
	Available              int                    `json:"available"`
	ReorderPoint           int                    `json:"reorder_point"`
	SuggestedProductionQty int                    `json:"suggested_production_qty"`
	BOMID                  string                 `json:"bom_id"`
	RawMaterialShortfalls  []RawMaterialShortfall `json:"raw_material_shortfalls"`
}

type RawMaterialShortfall struct {
	Sku          string  `json:"sku"`
	Required     float64 `json:"required"`
	Available    float64 `json:"available"`
	ShortfallQty float64 `json:"shortfall_qty"`
}

func GetMRPSuggestions(tenantID, locationCode string, leadTimeDays, safetyStock int) ([]MRPSuggestion, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	rows, err := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT data->>'parent_item' FROM %s.documents WHERE doctype = 'BOM' AND status = 'Active'`, schema))
	if err != nil {
		return nil, err
	}
	var parentItems []string
	for rows.Next() {
		var item string
		if err := rows.Scan(&item); err == nil && item != "" {
			parentItems = append(parentItems, item)
		}
	}
	rows.Close()

	out := []MRPSuggestion{}
	for _, item := range parentItems {
		var available int
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT available FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), item, locationCode).Scan(&available)

		velocity, verr := CalculateSalesVelocity(tenantID, locationCode, item, 30)
		if verr != nil {
			velocity = 0
		}
		reorderPoint := int(math.Ceil(velocity*float64(leadTimeDays))) + safetyStock
		if available >= reorderPoint {
			continue
		}
		suggestedQty := reorderPoint - available

		s := MRPSuggestion{ParentItem: item, LocationCode: locationCode, Available: available, ReorderPoint: reorderPoint, SuggestedProductionQty: suggestedQty, RawMaterialShortfalls: []RawMaterialShortfall{}}
		if bomID, berr := GetActiveBOMForItem(tenantID, item, ""); berr == nil {
			s.BOMID = bomID
			if components, eerr := explodeBOMComponents(tenantID, bomID, float64(suggestedQty), map[string]bool{}, 0); eerr == nil {
				for _, c := range components {
					var rawAvail int
					_ = db.DB.QueryRow(fmt.Sprintf(`SELECT available FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), c.Sku, locationCode).Scan(&rawAvail)
					if float64(rawAvail) < c.Qty {
						s.RawMaterialShortfalls = append(s.RawMaterialShortfalls, RawMaterialShortfall{Sku: c.Sku, Required: c.Qty, Available: float64(rawAvail), ShortfallQty: c.Qty - float64(rawAvail)})
					}
				}
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// ============================================================
// 26.9.8: Standard/actual costing + variance report
// ============================================================

// RecordActualProductionCost logs the real total cost incurred for a
// Completed production order. Full absorption costing (material + labor +
// overhead allocation) is out of scope per this codebase's lightweight
// principle - the user supplies the total (typically summed from GRN/labor
// postings elsewhere); this engine's job is comparing it against the BOM's
// standard_cost * quantity for the variance report below.
func RecordActualProductionCost(tenantID, orderID string, actualCost float64, actorUserID string) error {
	data, status, err := fetchProductionOrder(tenantID, orderID)
	if err != nil {
		return fmt.Errorf("production order not found: %v", err)
	}
	if status != "Completed" {
		return fmt.Errorf("actual cost can only be recorded once a production order is Completed (current status: %s)", status)
	}
	if actualCost < 0 {
		return fmt.Errorf("actual cost cannot be negative")
	}
	data["actual_cost"] = actualCost
	if err := saveProductionOrderStatus(tenantID, orderID, status, data); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actorUserID, "PRODUCTION_ACTUAL_COST_RECORDED", "SUCCESS", fmt.Sprintf("Actual cost %v recorded for order %s", actualCost, orderID))
	return nil
}

const productionCostVarianceTolerancePercent = 10.0

func init() {
	RegisterReport(ReportDefinition{
		ID:       "production_cost_variance",
		Label:    "Production Cost Variance",
		Category: "Manufacturing",
		Columns: []ReportColumn{
			{Key: "order_id", Label: "Production Order"},
			{Key: "bom_id", Label: "BOM"},
			{Key: "quantity", Label: "Quantity"},
			{Key: "standard_cost_total", Label: "Standard Cost (Total)"},
			{Key: "actual_cost", Label: "Actual Cost"},
			{Key: "variance", Label: "Variance"},
			{Key: "variance_percent", Label: "Variance %"},
			{Key: "flag", Label: "Flag"},
		},
		Run: runProductionCostVarianceReport,
	})
}

func runProductionCostVarianceReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'ProductionOrder' AND status = 'Completed'`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []map[string]interface{}{}
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			continue
		}
		var d map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &d); err != nil {
			continue
		}
		actualCostRaw, hasActual := d["actual_cost"]
		if !hasActual {
			continue
		}
		bomID, _ := d["bom_id"].(string)
		qty := numFromInterface(d["quantity"])
		standardCostPerUnit := 0.0
		if bomData, berr := fetchBOMData(tenantID, bomID); berr == nil {
			standardCostPerUnit = numFromInterface(bomData["standard_cost"])
		}
		standardTotal := standardCostPerUnit * qty
		actual := numFromInterface(actualCostRaw)
		variance := actual - standardTotal
		variancePct := 0.0
		if standardTotal != 0 {
			variancePct = variance / standardTotal * 100
		}
		flag := ""
		if math.Abs(variancePct) > productionCostVarianceTolerancePercent {
			flag = "MFG-0279: variance exceeds tolerance - review required"
		}
		out = append(out, map[string]interface{}{
			"order_id": id, "bom_id": bomID, "quantity": qty,
			"standard_cost_total": standardTotal, "actual_cost": actual,
			"variance": variance, "variance_percent": variancePct, "flag": flag,
		})
	}
	return out, nil
}
