package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Stage 26.5 (WMS Enterprise Maturity Sprint): three putaway-adjacent
// additions layered on top of engines/wms.go's PutawayToBin/bin_stock
// without changing either - 26.5.3 cross-dock staging, 26.5.4 LPN/carton/
// pallet grouping, 26.5.5 bin-to-bin replenishment suggestions.

// ------------------------------------------------------------------
// 26.5.3: Cross-dock / flow-through putaway
// ------------------------------------------------------------------

// CrossDockOpportunity is one already-open outbound demand for a sku at a
// location that newly-received stock could flow straight through to
// instead of taking a shelf slot.
type CrossDockOpportunity struct {
	RefType string `json:"ref_type"` // "FulfillmentTask" or "TransferOrder"
	RefID   string `json:"ref_id"`
	Qty     int    `json:"qty"`
}

// CheckCrossDockOpportunity reports how much of sku at locationCode is
// already wanted by open outbound demand - an unpicked/unshort-picked
// remainder on a still-open FulfillmentTask, or an un-dispatched line on an
// Approved TransferOrder sourced from this location. Putaway can route
// straight to cross-dock staging for up to that matched qty instead of
// shelving it, per the blueprint's cross-dock goal of skipping put-away
// labor for stock about to turn straight back around.
func CheckCrossDockOpportunity(tenantID, sku, locationCode string) (matchedQty int, opportunities []CrossDockOpportunity, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, nil, err
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		WHERE doctype = 'FulfillmentTask' AND data->>'location_code' = $1
		  AND status IN ('Pending', 'Picking')`, schema), locationCode)
	if err != nil {
		return 0, nil, err
	}
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			rows.Close()
			return 0, nil, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			continue
		}
		rawItems, _ := data["items"].([]interface{})
		for _, ri := range rawItems {
			m, ok := ri.(map[string]interface{})
			if !ok {
				continue
			}
			itemSKU, _ := m["sku"].(string)
			if itemSKU != sku {
				continue
			}
			need := int(numFromInterface(m["qty"]) - numFromInterface(m["picked_qty"]) - numFromInterface(m["short_qty"]))
			if need > 0 {
				matchedQty += need
				opportunities = append(opportunities, CrossDockOpportunity{RefType: "FulfillmentTask", RefID: id, Qty: need})
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}

	rows, err = db.DB.Query(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		WHERE doctype = 'TransferOrder' AND data->>'from_warehouse' = $1 AND status = 'Approved'`, schema), locationCode)
	if err != nil {
		return matchedQty, opportunities, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			return matchedQty, opportunities, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			continue
		}
		itemsStr, _ := data["items"].(string)
		lines, err := parseTransferItems(itemsStr)
		if err != nil {
			continue
		}
		for _, l := range lines {
			if l.Sku == sku && l.Qty > 0 {
				matchedQty += l.Qty
				opportunities = append(opportunities, CrossDockOpportunity{RefType: "TransferOrder", RefID: id, Qty: l.Qty})
			}
		}
	}
	return matchedQty, opportunities, rows.Err()
}

// crossDockBinCode is the synthetic per-location staging bin cross-docked
// stock is recorded under. Deliberately not a real Bin document: requiring
// one per location/tenant would be setup busywork for a staging concept,
// not a real shelf - bin_stock.bin_code is a plain VARCHAR with no FK to
// the Bin doctype, so this is a legal, if synthetic, key.
func crossDockBinCode(locationCode string) string {
	return "XDOCK-" + locationCode
}

// CrossDockPutaway (26.5.3) skips shelf placement for up to the qty an open
// outbound demand actually wants, recording it in the cross-dock staging
// bin instead of a real shelf bin - still visible to GenerateBinPickList
// (which only requires Good-condition bin_stock, not a real Bin document)
// so it's pickable the same way any other bin stock is. Refuses to stage
// more than either the caller's qty or the matched opportunity, whichever
// is smaller - any true excess belongs on a shelf via the ordinary
// PutawayToBin, not staged for a demand that doesn't exist.
func CrossDockPutaway(tenantID, sku, locationCode string, qty int, userID string) (staged int, opportunities []CrossDockOpportunity, err error) {
	if qty <= 0 {
		return 0, nil, errors.New("qty must be positive")
	}
	matchedQty, opportunities, err := CheckCrossDockOpportunity(tenantID, sku, locationCode)
	if err != nil {
		return 0, nil, err
	}
	if matchedQty <= 0 {
		return 0, opportunities, fmt.Errorf("no open transfer/sale is waiting on SKU %s at %s - use ordinary putaway instead", sku, locationCode)
	}
	staged = qty
	if staged > matchedQty {
		staged = matchedQty
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, opportunities, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, opportunities, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return 0, opportunities, err
	}

	var onHand int
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT on_hand FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2 FOR UPDATE`, schema),
		sku, locationCode).Scan(&onHand)
	if err == sql.ErrNoRows {
		return 0, opportunities, fmt.Errorf("no on-hand stock for SKU %s at location %s", sku, locationCode)
	} else if err != nil {
		return 0, opportunities, err
	}
	var alreadyBinned int
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM(qty), 0) FROM %s.bin_stock WHERE sku = $1 AND location_code = $2`, schema),
		sku, locationCode).Scan(&alreadyBinned); err != nil {
		return 0, opportunities, err
	}
	if alreadyBinned+staged > onHand {
		return 0, opportunities, fmt.Errorf("cross-dock qty exceeds unassigned on-hand stock at %s (on_hand=%d, already binned=%d, requested=%d)",
			locationCode, onHand, alreadyBinned, staged)
	}

	binCode := crossDockBinCode(locationCode)
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.bin_stock (bin_code, sku, location_code, condition, qty)
		VALUES ($1, $2, $3, 'Good', $4)
		ON CONFLICT (bin_code, sku, condition) DO UPDATE SET
			qty = %s.bin_stock.qty + EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema, schema),
		binCode, sku, locationCode, staged); err != nil {
		return 0, opportunities, err
	}

	if err := tx.Commit(); err != nil {
		return 0, opportunities, err
	}
	LogAuditEvent(tenantID, userID, "WMS_CROSSDOCK_PUTAWAY", "SUCCESS",
		fmt.Sprintf("Staged %d x %s at %s for cross-dock instead of shelving (matched demand: %d)", staged, sku, binCode, matchedQty))
	// Stage 42.2.2: additive WarehouseTask retrofit.
	LogCompletedWarehouseTask(tenantID, NewWarehouseTask{
		TaskType: "Putaway", LocationCode: locationCode, ToBin: binCode, Item: sku, Qty: float64(staged),
		Notes: "cross-dock",
	}, userID)
	return staged, opportunities, nil
}

// ------------------------------------------------------------------
// 42.3.8: Planned cross-dock / flow-through / transship
// ------------------------------------------------------------------

// crossShipBinCode is Transship's own synthetic staging bin, separate from
// crossDockBinCode's CrossDock/FlowThrough one so a pick list (or a human
// scanning the staging area) can tell "waiting on an internal
// task/transfer" apart from "waiting on a TransferOrder that doesn't exist
// yet" at a glance.
func crossShipBinCode(destination string) string {
	return "XSHIP-" + destination
}

// PlannedCrossDockPutaway (42.3.8) is CrossDockPutaway's ahead-of-time
// counterpart: instead of scanning live outbound demand at the moment of
// putaway (CheckCrossDockOpportunity), it consumes an already-Planned
// CrossDockPlan raised against the ASN/PO before the shipment even arrived.
// Requires an exact SKU + Active-plan match at locationCode; stages the
// lesser of qty and the plan's remaining planned qty, same "never stage more
// than actually matched" discipline as 26.5.3. A CrossDock/FlowThrough plan
// stages into the ordinary cross-dock bin (still visible to
// GenerateBinPickList, still fulfilling the same internal
// FulfillmentTask/TransferOrder 26.5.3 would have matched opportunistically);
// a Transship plan stages into its own XSHIP-<destination> bin instead,
// since there is no existing internal document for it to flow into yet.
func PlannedCrossDockPutaway(tenantID, sku, locationCode string, qty int, userID string) (staged int, planID string, err error) {
	if qty <= 0 {
		return 0, "", errors.New("qty must be positive")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, "", err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return 0, "", err
	}

	var planDataStr string
	var planQty float64
	err = tx.QueryRow(fmt.Sprintf(`
		SELECT id, data, (data->>'qty')::numeric FROM %s.documents
		WHERE doctype = 'CrossDockPlan' AND status = 'Planned' AND data->>'sku' = $1
		ORDER BY created_at ASC LIMIT 1 FOR UPDATE`, schema), sku).Scan(&planID, &planDataStr, &planQty)
	if err == sql.ErrNoRows {
		return 0, "", fmt.Errorf("no Planned cross-dock plan exists for SKU %s - use ordinary or opportunistic cross-dock putaway instead", sku)
	} else if err != nil {
		return 0, "", err
	}
	var planData map[string]interface{}
	if err := json.Unmarshal([]byte(planDataStr), &planData); err != nil {
		return 0, "", err
	}
	planType := strField(planData, "plan_type")
	destinationRef := strField(planData, "destination_ref")

	staged = qty
	if staged > int(planQty) {
		staged = int(planQty)
	}
	if staged <= 0 {
		return 0, planID, fmt.Errorf("cross-dock plan %s for SKU %s has no remaining planned qty", planID, sku)
	}

	var onHand int
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT on_hand FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2 FOR UPDATE`, schema),
		sku, locationCode).Scan(&onHand)
	if err == sql.ErrNoRows {
		return 0, planID, fmt.Errorf("no on-hand stock for SKU %s at location %s", sku, locationCode)
	} else if err != nil {
		return 0, planID, err
	}
	var alreadyBinned int
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM(qty), 0) FROM %s.bin_stock WHERE sku = $1 AND location_code = $2`, schema),
		sku, locationCode).Scan(&alreadyBinned); err != nil {
		return 0, planID, err
	}
	if alreadyBinned+staged > onHand {
		return 0, planID, fmt.Errorf("planned cross-dock qty exceeds unassigned on-hand stock at %s (on_hand=%d, already binned=%d, requested=%d)",
			locationCode, onHand, alreadyBinned, staged)
	}

	binCode := crossDockBinCode(locationCode)
	if planType == "Transship" {
		binCode = crossShipBinCode(destinationRef)
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.bin_stock (bin_code, sku, location_code, condition, qty)
		VALUES ($1, $2, $3, 'Good', $4)
		ON CONFLICT (bin_code, sku, condition) DO UPDATE SET
			qty = %s.bin_stock.qty + EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema, schema),
		binCode, sku, locationCode, staged); err != nil {
		return 0, planID, err
	}

	remainingQty := planQty - float64(staged)
	planData["qty"] = remainingQty
	if remainingQty <= 1e-9 {
		planData["status"] = "Fulfilled"
	}
	marshaled, err := json.Marshal(planData)
	if err != nil {
		return 0, planID, err
	}
	newStatus := "Planned"
	if remainingQty <= 1e-9 {
		newStatus = "Fulfilled"
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'CrossDockPlan' AND id = $3`, schema),
		marshaled, newStatus, planID); err != nil {
		return 0, planID, err
	}

	if err := tx.Commit(); err != nil {
		return 0, planID, err
	}
	LogAuditEvent(tenantID, userID, "WMS_PLANNED_CROSSDOCK_PUTAWAY", "SUCCESS",
		fmt.Sprintf("Staged %d x %s at %s for planned %s (plan %s) instead of shelving", staged, sku, binCode, planType, planID))
	LogCompletedWarehouseTask(tenantID, NewWarehouseTask{
		TaskType: "Putaway", LocationCode: locationCode, ToBin: binCode, Item: sku, Qty: float64(staged),
		Notes: "planned cross-dock: " + planType,
	}, userID)
	return staged, planID, nil
}

// ------------------------------------------------------------------
// 26.5.4: LPN/carton/pallet grouping
// ------------------------------------------------------------------

// AssignToLPN (26.5.4) records that qty of a bin's sku/condition stock is
// physically grouped inside container lpnCode - a further breakdown of
// bin_stock the same way bin_stock is itself a breakdown of
// inventory_availability, never a second source of truth for the bin's own
// total. Refuses to assign more than the bin actually holds, net of what's
// already assigned to other containers from that same bin/sku/condition.
func AssignToLPN(tenantID, lpnCode, binCode, sku, condition string, qty int, userID string) error {
	if qty <= 0 {
		return errors.New("qty must be positive")
	}
	if condition == "" {
		condition = "Good"
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	var binQty int
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT qty FROM %s.bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = $3 FOR UPDATE`, schema),
		binCode, sku, condition).Scan(&binQty)
	if err == sql.ErrNoRows {
		return fmt.Errorf("no %s-condition stock for SKU %s in bin %s", condition, sku, binCode)
	} else if err != nil {
		return err
	}
	var alreadyAssigned int
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM(qty), 0) FROM %s.bin_stock_lpn WHERE bin_code = $1 AND sku = $2 AND condition = $3`, schema),
		binCode, sku, condition).Scan(&alreadyAssigned); err != nil {
		return err
	}
	if alreadyAssigned+qty > binQty {
		return fmt.Errorf("LPN assignment exceeds bin's own qty (bin qty=%d, already assigned=%d, requested=%d)", binQty, alreadyAssigned, qty)
	}

	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.bin_stock_lpn (lpn_code, bin_code, sku, condition, qty)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (lpn_code, bin_code, sku, condition) DO UPDATE SET
			qty = %s.bin_stock_lpn.qty + EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema, schema),
		lpnCode, binCode, sku, condition, qty); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "WMS_LPN_ASSIGN", "SUCCESS",
		fmt.Sprintf("Assigned %d x %s (%s) from bin %s to LPN %s", qty, sku, condition, binCode, lpnCode))
	return nil
}

// LPNContentLine is one sku/condition/qty entry inside a carton or pallet.
type LPNContentLine struct {
	BinCode   string `json:"bin_code"`
	Sku       string `json:"sku"`
	Condition string `json:"condition"`
	Qty       int    `json:"qty"`
}

// GetLPNContents lists everything currently assigned to lpnCode.
func GetLPNContents(tenantID, lpnCode string) ([]LPNContentLine, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT bin_code, sku, condition, qty FROM %s.bin_stock_lpn WHERE lpn_code = $1 AND qty > 0 ORDER BY bin_code, sku`, schema),
		lpnCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lines []LPNContentLine
	for rows.Next() {
		var l LPNContentLine
		if err := rows.Scan(&l.BinCode, &l.Sku, &l.Condition, &l.Qty); err != nil {
			return nil, err
		}
		lines = append(lines, l)
	}
	return lines, rows.Err()
}

// ------------------------------------------------------------------
// 26.5.5: Bin-to-bin replenishment min/max triggers
// ------------------------------------------------------------------

// BinReplenishmentSuggestion is one "move stock from a reserve bin into a
// pick-face bin that's fallen below its min" recommendation.
type BinReplenishmentSuggestion struct {
	BinCode     string `json:"bin_code"`
	Sku         string `json:"sku"`
	CurrentQty  int    `json:"current_qty"`
	MinQty      int    `json:"min_qty"`
	MaxQty      int    `json:"max_qty"`
	Shortage    int    `json:"shortage"`
	FromBinCode string `json:"from_bin_code"`
	MoveQty     int    `json:"move_qty"`
}

type binReplenishmentRule struct {
	BinCode string
	Sku     string
	MinQty  int
	MaxQty  int
}

// GetBinReplenishmentSuggestions (26.5.5) implements the design note
// validated against the retired WMS prototype: shortage = max_qty -
// current_qty per active bin/SKU rule whose current Good-condition qty has
// fallen below min_qty; fill from reserve bins holding that SKU at the same
// location (bins tagged bin_type='Reserve' if any exist, else any other bin
// - so this still works before a warehouse has bothered tagging reserve
// bins), ordered by highest qty first, until the shortage is covered or
// reserves run out. Read-only, same "suggestion report, no auto-executed
// document" precedent GetReplenishmentSuggestions (Stage 10) already set -
// ExecuteBinReplenishment is the separate, explicit action that actually
// moves stock.
func GetBinReplenishmentSuggestions(tenantID, locationCode string) ([]BinReplenishmentSuggestion, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	ruleRows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data FROM %s.documents WHERE doctype = 'BinReplenishmentRule' AND status = 'Active' AND deleted_at IS NULL`, schema))
	if err != nil {
		return nil, err
	}
	var rules []binReplenishmentRule
	for ruleRows.Next() {
		var id, dataStr string
		if err := ruleRows.Scan(&id, &dataStr); err != nil {
			ruleRows.Close()
			return nil, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			continue
		}
		binCode, _ := data["bin_code"].(string)
		sku, _ := data["sku"].(string)
		if binCode == "" || sku == "" {
			continue
		}
		rules = append(rules, binReplenishmentRule{
			BinCode: binCode, Sku: sku,
			MinQty: int(numFromInterface(data["min_qty"])),
			MaxQty: int(numFromInterface(data["max_qty"])),
		})
	}
	ruleRows.Close()
	if err := ruleRows.Err(); err != nil {
		return nil, err
	}

	var suggestions []BinReplenishmentSuggestion
	for _, rule := range rules {
		var currentQty int
		_ = db.DB.QueryRow(fmt.Sprintf(
			`SELECT COALESCE(qty, 0) FROM %s.bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = 'Good'`, schema),
			rule.BinCode, rule.Sku).Scan(&currentQty)
		if currentQty >= rule.MinQty {
			continue
		}
		shortage := rule.MaxQty - currentQty
		if shortage <= 0 {
			continue
		}

		type reserveBin struct {
			BinCode string
			Qty     int
		}
		fetchReserves := func(reserveOnly bool) ([]reserveBin, error) {
			q := fmt.Sprintf(`
				SELECT bs.bin_code, bs.qty FROM %s.bin_stock bs
				LEFT JOIN %s.documents b ON b.doctype = 'Bin' AND b.data->>'bin_code' = bs.bin_code
				WHERE bs.sku = $1 AND bs.location_code = $2 AND bs.condition = 'Good'
				  AND bs.bin_code != $3 AND bs.qty > 0`, schema, schema)
			if reserveOnly {
				q += ` AND b.data->>'bin_type' = 'Reserve'`
			}
			q += ` ORDER BY bs.qty DESC`
			rows, err := db.DB.Query(q, rule.Sku, locationCode, rule.BinCode)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			var out []reserveBin
			for rows.Next() {
				var rb reserveBin
				if err := rows.Scan(&rb.BinCode, &rb.Qty); err != nil {
					return nil, err
				}
				out = append(out, rb)
			}
			return out, rows.Err()
		}

		reserves, err := fetchReserves(true)
		if err != nil {
			return nil, err
		}
		if len(reserves) == 0 {
			reserves, err = fetchReserves(false)
			if err != nil {
				return nil, err
			}
		}

		remaining := shortage
		for _, rb := range reserves {
			if remaining <= 0 {
				break
			}
			move := rb.Qty
			if move > remaining {
				move = remaining
			}
			suggestions = append(suggestions, BinReplenishmentSuggestion{
				BinCode: rule.BinCode, Sku: rule.Sku, CurrentQty: currentQty, MinQty: rule.MinQty, MaxQty: rule.MaxQty,
				Shortage: shortage, FromBinCode: rb.BinCode, MoveQty: move,
			})
			remaining -= move
		}
		if remaining == shortage {
			// No reserves at all - still surface the shortage (FromBinCode
			// blank, MoveQty 0) so it's visible on the suggestions list
			// rather than silently vanishing because nowhere had spare stock.
			suggestions = append(suggestions, BinReplenishmentSuggestion{
				BinCode: rule.BinCode, Sku: rule.Sku, CurrentQty: currentQty, MinQty: rule.MinQty, MaxQty: rule.MaxQty,
				Shortage: shortage,
			})
		}
	}

	sort.Slice(suggestions, func(i, j int) bool { return suggestions[i].BinCode < suggestions[j].BinCode })
	return suggestions, nil
}

// ExecuteBinReplenishment (26.5.5) moves qty of sku from fromBin to toBin,
// both Good-condition, at the same location - a pure bin-to-bin shelf move
// that never touches inventory_availability (unlike
// TransitionBinStockCondition, which changes sellability; this doesn't -
// the stock was already Good and available before and after).
func ExecuteBinReplenishment(tenantID, fromBin, toBin, sku string, qty int, userID string) error {
	if qty <= 0 {
		return errors.New("qty must be positive")
	}
	if fromBin == toBin {
		return errors.New("from and to bin must differ")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	var locationCode string
	var have int
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT location_code, qty FROM %s.bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = 'Good' FOR UPDATE`, schema),
		fromBin, sku).Scan(&locationCode, &have)
	if err == sql.ErrNoRows {
		return fmt.Errorf("no Good-condition stock for SKU %s in bin %s", sku, fromBin)
	} else if err != nil {
		return err
	}
	if have < qty {
		return fmt.Errorf("only %d units of %s in bin %s, cannot move %d", have, sku, fromBin, qty)
	}

	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.bin_stock SET qty = qty - $1, updated_at = CURRENT_TIMESTAMP WHERE bin_code = $2 AND sku = $3 AND condition = 'Good'`, schema),
		qty, fromBin, sku); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.bin_stock (bin_code, sku, location_code, condition, qty)
		VALUES ($1, $2, $3, 'Good', $4)
		ON CONFLICT (bin_code, sku, condition) DO UPDATE SET
			qty = %s.bin_stock.qty + EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema, schema),
		toBin, sku, locationCode, qty); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// 26.10.1: a pure bin-to-bin shelf move - on_hand/available are
	// unaffected (see this function's own doc comment), so Qty is 0; the
	// from/to bin codes are what make this entry meaningful.
	if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
		ItemID: sku, WarehouseID: locationCode, Qty: 0,
		VoucherType: "BinReplenishment", VoucherID: fmt.Sprintf("%s-%s", fromBin, toBin), UserID: userID,
		FromLocationID: fromBin, ToLocationID: toBin,
	}); lerr != nil {
		LogSystemError(tenantID, "", "WARN", "ExecuteBinReplenishment", fmt.Sprintf("stock ledger write failed for %s: %v", sku, lerr), "")
	}
	LogAuditEvent(tenantID, userID, "WMS_BIN_REPLENISH", "SUCCESS",
		fmt.Sprintf("Moved %d x %s from bin %s to bin %s", qty, sku, fromBin, toBin))
	// Stage 42.2.2: additive WarehouseTask retrofit.
	LogCompletedWarehouseTask(tenantID, NewWarehouseTask{
		TaskType: "Replenish", LocationCode: locationCode, FromBin: fromBin, ToBin: toBin, Item: sku, Qty: float64(qty),
	}, userID)
	return nil
}
