package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 170.10-170.13: what a production order records and costs as it runs -
// operation confirmations (quantity, time, operator), scrap and rework that move
// stock, a by-product's share of the cost, and WIP accounting with a variance at
// close. Every posting is behind a setting that defaults OFF; with both off the
// production flow is exactly what it was.

const (
	SettingProductionScrapRework = "manufacturing.scrap_rework_posting"
	SettingProductionWIP         = "manufacturing.wip_accounting"

	accountWIP                 = "1250"
	accountProductionScrapLoss = "5360"
	accountProductionVariance  = "5370"

	// Posting-profile events, so a client can map each production posting to
	// its own accounts (by legal entity); the accounts above are the defaults.
	PostingEventProductionScrap     = "production_scrap"
	PostingEventProductionWIPIssue  = "production_wip_issue"
	PostingEventProductionWIPFinish = "production_wip_finish"
	PostingEventProductionVariance  = "production_variance"
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingProductionScrapRework, Module: "Manufacturing",
		Label: "Scrap and rework move stock and cost", Type: SettingTypeBool, Default: "false",
		Description: "Stage 170.11: on, a scrap entry takes the quantity out of the order's store at its cost and posts the loss to Production Scrap Loss (5360), and sending a quantity to rework opens a rework order for it, linked to the original. Off: both only write the order's log.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingProductionWIP, Module: "Manufacturing",
		Label: "Work-in-progress accounting", Type: SettingTypeBool, Default: "false",
		Description: "Stage 170.13: on, material issued to an order posts to Work in Progress (1250) instead of staying in stock value, finished goods move it back to Inventory (1200), and what is left in WIP when the order closes goes to Production Variance (5370). Applies to orders issued after it is turned on. Off: production posts no ledger entries.",
	})
	postingEvents = append(postingEvents, PostingEventProductionScrap, PostingEventProductionWIPIssue,
		PostingEventProductionWIPFinish, PostingEventProductionVariance)
}

// ---------------------------------------------------------------------------
// 170.10: operation confirmations
// ---------------------------------------------------------------------------

// OperationConfirmation is what one confirmation records. All zero (and no
// employee) is the original quantity-less confirmation of a whole operation.
type OperationConfirmation struct {
	GoodQty    float64 `json:"good_qty"`
	ScrapQty   float64 `json:"scrap_qty"`
	Minutes    float64 `json:"minutes"`
	EmployeeID string  `json:"employee_id"`
}

type ProductionConfirmation struct {
	ID           int64   `json:"id"`
	Seq          int     `json:"seq"`
	GoodQty      float64 `json:"good_qty"`
	ScrapQty     float64 `json:"scrap_qty"`
	Minutes      float64 `json:"minutes"`
	OperatorKind string  `json:"operator_kind"`
	OperatorID   string  `json:"operator_id"`
	ConfirmedBy  string  `json:"confirmed_by"`
	ConfirmedAt  string  `json:"confirmed_at"`
}

// ConfirmOperationRecorded confirms a routing operation (sequence rule MANUFA-0147
// and the capacity flag as before) and records the good and scrap quantity, the
// minutes and the operator. With quantities, an operation can be confirmed in
// several goes; the sum of good and scrap can never pass the order quantity
// (MFG-1740) and the operation counts as confirmed once it reaches it.
func ConfirmOperationRecorded(tenantID, orderID string, seq int, actorUserID string, c OperationConfirmation) (capacityWarning string, err error) {
	if c.GoodQty < 0 || c.ScrapQty < 0 || c.Minutes < 0 {
		return "", &ValidationError{Code: "MFG-1741", Message: "quantities and minutes cannot be negative"}
	}
	recorded := c.GoodQty > 0 || c.ScrapQty > 0
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	employee := strings.TrimSpace(c.EmployeeID)
	if employee != "" {
		var n int
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'Employee' AND id = $1 AND deleted_at IS NULL`, schema), employee).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return "", &ValidationError{Code: "MFG-1742", Message: fmt.Sprintf("employee %s was not found", employee)}
		}
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	data, status, ferr := lockProductionOrderTx(tx, schema, orderID)
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
	var doneSoFar float64
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(good_qty + scrap_qty), 0) FROM %s.production_confirmations WHERE order_id = $1 AND seq = $2`, schema), orderID, seq).Scan(&doneSoFar); err != nil {
		return "", err
	}
	total := doneSoFar + c.GoodQty + c.ScrapQty
	if recorded && total > orderQty+0.0001 {
		return "", &ValidationError{Code: "MFG-1740", Message: fmt.Sprintf("operation %d would have %v confirmed against an order quantity of %v", seq, total, orderQty)}
	}
	if target.WorkCenterID != "" {
		if capMinsPerDay, cerr := workCenterDailyCapacityMins(tenantID, target.WorkCenterID); cerr == nil && capMinsPerDay > 0 {
			needed := target.SetupTimeMins + target.RunTimeMinsPerUnit*orderQty
			if needed > capMinsPerDay {
				capacityWarning = fmt.Sprintf("MFG-0277: operation %d needs %.0f minutes at work center %s, exceeding its %.0f minute/day capacity", seq, needed, target.WorkCenterID, capMinsPerDay)
				LogSystemError(tenantID, "", "WARN", "ConfirmOperation", capacityWarning, "")
			}
		}
	}

	kind, operator := "User", actorUserID
	if employee != "" {
		kind, operator = "Employee", employee
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.production_confirmations (order_id, seq, good_qty, scrap_qty, minutes, operator_kind, operator_id, confirmed_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, schema),
		orderID, seq, c.GoodQty, c.ScrapQty, c.Minutes, kind, operator, actorUserID); err != nil {
		return "", err
	}
	if !recorded || total >= orderQty-0.0001 {
		progress = append(progress, operationProgress{Seq: seq, Status: "Confirmed", ConfirmedAt: time.Now().Format(time.RFC3339)})
	}
	marshaled, _ := json.Marshal(progress)
	data["operations_progress"] = string(marshaled)
	if err := saveProductionOrderStatusTx(tx, schema, orderID, "In Process", data); err != nil {
		return capacityWarning, err
	}
	if err := tx.Commit(); err != nil {
		return capacityWarning, err
	}
	LogAuditEvent(tenantID, actorUserID, "PRODUCTION_OPERATION_CONFIRMED", "SUCCESS", fmt.Sprintf("Operation %d confirmed for order %s: good %v, scrap %v, %v min, %s %s", seq, orderID, c.GoodQty, c.ScrapQty, c.Minutes, kind, operator))
	return capacityWarning, nil
}

// ListProductionConfirmations is a work order's confirmations, oldest first.
func ListProductionConfirmations(tenantID, orderID string) ([]ProductionConfirmation, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, seq, good_qty, scrap_qty, minutes, operator_kind, operator_id, confirmed_by, to_char(confirmed_at, 'YYYY-MM-DD HH24:MI') FROM %s.production_confirmations WHERE order_id = $1 ORDER BY id`, schema), orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProductionConfirmation{}
	for rows.Next() {
		var p ProductionConfirmation
		if err := rows.Scan(&p.ID, &p.Seq, &p.GoodQty, &p.ScrapQty, &p.Minutes, &p.OperatorKind, &p.OperatorID, &p.ConfirmedBy, &p.ConfirmedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// 170.11: scrap and rework that move stock and cost
// ---------------------------------------------------------------------------

// postCostedScrap is PostScrap with the setting on: the quantity leaves the
// order's store at cost (floor-checked like any issue) and the value posts
// Dr 5360 / Cr 1200, in one transaction with the log entry.
func postCostedScrap(tenantID, orderID, sku string, qty float64, reason, actorUserID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	data, status, err := lockProductionOrderTx(tx, schema, orderID)
	if err != nil {
		return fmt.Errorf("production order not found: %v", err)
	}
	location, _ := data["location"].(string)
	if location == "" {
		return fmt.Errorf("production order %s has no location to take the scrap from", orderID)
	}
	var log []scrapEntry
	if raw, ok := data["scrap_log"].(string); ok && raw != "" {
		_ = json.Unmarshal([]byte(raw), &log)
	}
	n := len(log) + 1
	items := []interface{}{map[string]interface{}{"sku": sku, "qty": -qty}}
	if err := postProductionStockTx(tx, schema, location, orderID, fmt.Sprintf("SCRAP:%d", n), items); err != nil {
		return fmt.Errorf("scrap stock posting failed: %v", err)
	}
	value, err := consumedValuePaiseTx(tx, schema, items)
	if err != nil {
		return fmt.Errorf("scrap cost: %v", err)
	}
	if value > 0 {
		acc := ResolvePostingAccounts(tenantID, PostingEventProductionScrap,
			PostingContext{LegalEntity: locationLegalEntity(tenantID, location)}, accountProductionScrapLoss, accountInventory)
		if err := PostDoubleEntryTx(tx, tenantID, schema, "ProductionOrder", orderID,
			map[string]int64{acc.Debit: value}, map[string]int64{acc.Credit: value}, "",
			fmt.Sprintf("ProductionScrap:%s:%d", orderID, n)); err != nil {
			return err
		}
	}
	log = append(log, scrapEntry{Sku: sku, Qty: qty, Reason: reason, Timestamp: time.Now().Format(time.RFC3339), CostPaise: value})
	marshaled, _ := json.Marshal(log)
	data["scrap_log"] = string(marshaled)
	if err := saveProductionOrderStatusTx(tx, schema, orderID, status, data); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actorUserID, "PRODUCTION_SCRAP_POSTED", "SUCCESS", fmt.Sprintf("Scrap posted for order %s: %s qty %v (%s), cost %d paise", orderID, sku, qty, reason, value))
	return nil
}

// openReworkOrder is SendToRework with the setting on: a Draft production order
// for the quantity on the same BOM, routing and store, naming the original in
// rework_of, created in the same transaction as the original's rework log.
func openReworkOrder(tenantID, orderID string, qty float64, reason, actorUserID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	data, status, err := lockProductionOrderTx(tx, schema, orderID)
	if err != nil {
		return fmt.Errorf("production order not found: %v", err)
	}
	orderQty := numFromInterface(data["quantity"])
	reworkSoFar := numFromInterface(data["rework_qty"])
	if reworkSoFar+qty > orderQty+0.0001 {
		return &ValidationError{Code: "MFG-1744", Message: fmt.Sprintf("%v in rework would pass the order quantity of %v", reworkSoFar+qty, orderQty)}
	}
	var log []reworkEntry
	if raw, ok := data["rework_log"].(string); ok && raw != "" {
		_ = json.Unmarshal([]byte(raw), &log)
	}
	newID := fmt.Sprintf("%s-RW%d", orderID, len(log)+1)
	rework := map[string]interface{}{
		"code": newID, "bom_id": data["bom_id"], "quantity": qty, "location": data["location"],
		"status": "Draft", "rework_of": orderID,
	}
	if r, ok := data["routing_id"]; ok {
		rework["routing_id"] = r
	}
	raw, _ := json.Marshal(rework)
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'ProductionOrder', $2, 'Draft', $3)`, schema), newID, raw, actorUserID); err != nil {
		return fmt.Errorf("rework order: %v", err)
	}
	data["rework_qty"] = reworkSoFar + qty
	log = append(log, reworkEntry{Qty: qty, Reason: reason, Timestamp: time.Now().Format(time.RFC3339), OrderID: newID})
	marshaled, _ := json.Marshal(log)
	data["rework_log"] = string(marshaled)
	if err := saveProductionOrderStatusTx(tx, schema, orderID, status, data); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actorUserID, "PRODUCTION_REWORK_LOGGED", "SUCCESS", fmt.Sprintf("Rework logged for order %s: qty %v (%s), rework order %s", orderID, qty, reason, newID))
	return nil
}

// ---------------------------------------------------------------------------
// 170.12: by-product cost share
// ---------------------------------------------------------------------------

type byProductValue struct {
	Sku   string
	Qty   float64
	Value int64
}

// byProductCostSplit divides total (what this completion cost) between the
// by-products that carry a cost share and the main product, which takes the
// rest. No share set anywhere: nothing split, the main product takes it all.
func byProductCostSplit(bps []byProductLine, qty float64, total int64) (parts []byProductValue, mainValue int64, err error) {
	var pct float64
	for _, bp := range bps {
		if bp.CostSharePercent < 0 {
			return nil, 0, &ValidationError{Code: "MFG-1743", Message: "a by-product cost share cannot be negative"}
		}
		pct += bp.CostSharePercent
	}
	if pct > 100.0001 {
		return nil, 0, &ValidationError{Code: "MFG-1743", Message: fmt.Sprintf("the by-product cost shares add up to %v%%, over 100%%", pct)}
	}
	mainValue = total
	if total <= 0 {
		return nil, total, nil
	}
	for _, bp := range bps {
		if bp.CostSharePercent <= 0 || bp.QtyPerUnit <= 0 {
			continue
		}
		v := sharePaise(total, bp.CostSharePercent, 100)
		parts = append(parts, byProductValue{Sku: bp.Sku, Qty: bp.QtyPerUnit * qty, Value: v})
		mainValue -= v
	}
	return parts, mainValue, nil
}

// ---------------------------------------------------------------------------
// 170.13: WIP accounting
// ---------------------------------------------------------------------------

// wipOrderOn says whether an order's postings run through WIP. It is fixed when
// the material is issued, so turning the setting on or off mid-order never
// leaves an order with only half its entries.
func wipOrderOn(data map[string]interface{}) bool {
	on, _ := data["wip_on"].(bool)
	return on
}

// wipPostTx posts one WIP step: the event's posting profile for the order's
// legal entity sets the accounts, the defaults otherwise. reverse swaps the
// sides (a negative remainder at close).
func wipPostTx(tx *sql.Tx, tenantID, schema, orderID string, data map[string]interface{}, key, event, debit, credit string, amount int64, reverse bool) error {
	if amount <= 0 {
		return nil
	}
	location, _ := data["location"].(string)
	acc := ResolvePostingAccounts(tenantID, event, PostingContext{LegalEntity: locationLegalEntity(tenantID, location)}, debit, credit)
	dr, cr := acc.Debit, acc.Credit
	if reverse {
		dr, cr = cr, dr
	}
	return PostDoubleEntryTx(tx, tenantID, schema, "ProductionOrder", orderID,
		map[string]int64{dr: amount}, map[string]int64{cr: amount}, "", "ProductionWIP:"+orderID+":"+key)
}

// wipIssueTx: material issued to the order moves from Inventory into WIP.
func wipIssueTx(tx *sql.Tx, tenantID, schema, orderID string, data map[string]interface{}, issued int64) error {
	if issued <= 0 || !GetSettingBool(tenantID, SettingProductionWIP) {
		return nil
	}
	if err := wipPostTx(tx, tenantID, schema, orderID, data, "ISSUE", PostingEventProductionWIPIssue, accountWIP, accountInventory, issued, false); err != nil {
		return err
	}
	data["wip_on"] = true
	data["wip_paise"] = issued
	return nil
}

// wipCompletionTx: backflushed material enters WIP, the finished goods take their
// cost out of it, and closing the order sends whatever is left to Production
// Variance (a negative remainder goes the other way).
func wipCompletionTx(tx *sql.Tx, tenantID, schema, orderID string, data map[string]interface{}, completion int, backflush, finished int64, closing bool) error {
	if !wipOrderOn(data) {
		return nil
	}
	wip := int64(numFromInterface(data["wip_paise"]))
	if err := wipPostTx(tx, tenantID, schema, orderID, data, fmt.Sprintf("BACKFLUSH:%d", completion), PostingEventProductionWIPIssue, accountWIP, accountInventory, backflush, false); err != nil {
		return err
	}
	wip += max(backflush, 0)
	if err := wipPostTx(tx, tenantID, schema, orderID, data, fmt.Sprintf("FINISH:%d", completion), PostingEventProductionWIPFinish, accountInventory, accountWIP, finished, false); err != nil {
		return err
	}
	wip -= max(finished, 0)
	if closing && wip != 0 {
		var err error
		if wip > 0 {
			err = wipPostTx(tx, tenantID, schema, orderID, data, "CLOSE", PostingEventProductionVariance, accountProductionVariance, accountWIP, wip, false)
		} else {
			err = wipPostTx(tx, tenantID, schema, orderID, data, "CLOSE", PostingEventProductionVariance, accountProductionVariance, accountWIP, -wip, true)
		}
		if err != nil {
			return err
		}
		wip = 0
	}
	data["wip_paise"] = wip
	return nil
}
