package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// Stage 20 Track B.2 (WMS Maturity): putaway, bin-grouped pick lists,
// damaged/QC-Hold/RTV condition tracking, and cycle-count reconciliation.
// tenant_default.bin_stock (migrations_stage20b_wms_maturity.sql) is a
// bin+condition breakdown of the same on-hand stock inventory_availability
// already tracks at the location level - it is never a second source of
// truth for total quantity, only for *where within the location* and *what
// condition* that quantity is in.

// PutawayToBin (20.17) records that qty of sku, already on-hand at the
// bin's location, is physically placed in binCode. Refuses to assign more
// than the location's unassigned on-hand stock (on_hand minus whatever is
// already binned across all bins/conditions for that sku/location) - a bin
// can never claim stock the location doesn't actually have.
func PutawayToBin(tenantID, binCode, sku string, qty int, userID string) error {
	if qty <= 0 {
		return errors.New("putaway qty must be positive")
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

	var location, binStatus, binOpState, binZone string
	var maxQty, maxWeight, maxVolume float64
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT data->>'location', status, COALESCE(data->>'bin_status', ''),
		        COALESCE(NULLIF(data->>'capacity', '')::numeric, 0),
		        COALESCE(NULLIF(data->>'max_weight', '')::numeric, 0),
		        COALESCE(NULLIF(data->>'max_volume', '')::numeric, 0),
		        COALESCE(data->>'zone', '')
		 FROM %s.documents WHERE doctype = 'Bin' AND data->>'bin_code' = $1`, schema),
		binCode).Scan(&location, &binStatus, &binOpState, &maxQty, &maxWeight, &maxVolume, &binZone)
	if err == sql.ErrNoRows {
		return fmt.Errorf("bin %s not found", binCode)
	} else if err != nil {
		return err
	}
	if binStatus != "Active" {
		return fmt.Errorf("bin %s is not Active", binCode)
	}
	// Stage 42.2.6: bin_status is the bin's OPERATIONAL state, distinct from
	// the Active/Inactive record-status check above - a bin can be an Active
	// record and still be temporarily unavailable for placement. Blank (every
	// bin before this Stage, and any bin that never sets it) is treated as
	// Available.
	if binOpState == "Blocked" || binOpState == "Full" || binOpState == "Counting" {
		return fmt.Errorf("bin %s is %s and cannot receive putaway right now", binCode, binOpState)
	}
	// Stage 42.3.9: hazmat compatibility. A hazmat-classified item (anything
	// other than blank/None) may only be placed in a zone that has opted in
	// (Zone.hazmat_allowed = Yes) - the same field 42.2.5 added and defaults
	// every pre-existing/auto-created zone to, so a tenant that never
	// classifies an item as hazmat sees no change in behavior at all.
	if err := checkHazmatCompatibility(tx, schema, sku, binZone, binCode); err != nil {
		return err
	}

	var onHand int
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT on_hand FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2 FOR UPDATE`, schema),
		sku, location).Scan(&onHand)
	if err == sql.ErrNoRows {
		return fmt.Errorf("no on-hand stock for SKU %s at location %s", sku, location)
	} else if err != nil {
		return err
	}

	var alreadyBinned int
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM(qty), 0) FROM %s.bin_stock WHERE sku = $1 AND location_code = $2`, schema),
		sku, location).Scan(&alreadyBinned); err != nil {
		return err
	}
	if alreadyBinned+qty > onHand {
		return fmt.Errorf("putaway qty exceeds unassigned on-hand stock at %s (on_hand=%d, already binned=%d, requested=%d)",
			location, onHand, alreadyBinned, qty)
	}

	if err := enforceBinCapacity(tx, schema, binCode, sku, qty, maxQty, maxWeight, maxVolume); err != nil {
		return err
	}

	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.bin_stock (bin_code, sku, location_code, condition, qty)
		VALUES ($1, $2, $3, 'Good', $4)
		ON CONFLICT (bin_code, sku, condition) DO UPDATE SET
			qty = %s.bin_stock.qty + EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema, schema),
		binCode, sku, location, qty); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	// 26.10.1: putaway never changes on_hand/available (the stock was already
	// on-hand, unassigned, before this call) - a Qty 0 entry still records
	// the physical move from unassigned stock into binCode.
	if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
		ItemID: sku, WarehouseID: location, Qty: 0,
		VoucherType: "Putaway", VoucherID: binCode, UserID: userID, ToLocationID: binCode,
	}); lerr != nil {
		LogSystemError(tenantID, "", "WARN", "PutawayToBin", fmt.Sprintf("stock ledger write failed for %s: %v", sku, lerr), "")
	}
	LogAuditEvent(tenantID, userID, "WMS_PUTAWAY", "SUCCESS", fmt.Sprintf("Put away %d x %s into bin %s", qty, sku, binCode))
	logTaskCompletion(tenantID, "Putaway", userID, location, binCode, float64(qty))
	// Stage 42.2.2: additive WarehouseTask retrofit - a real queue/cockpit
	// record alongside the productivity log above, never a gate on the
	// putaway itself.
	LogCompletedWarehouseTask(tenantID, NewWarehouseTask{
		TaskType: "Putaway", LocationCode: location, ToBin: binCode, Item: sku, Qty: float64(qty),
	}, userID)
	return nil
}

// enforceBinCapacity (Stage 42.2.6) refuses a putaway that would push a
// bin's contents past whichever of max_qty/max_weight/max_volume are
// actually configured on it - zero/unset on all three (every bin before
// this Stage) is a no-op, so PutawayToBin's behaviour is unchanged until a
// tenant opts a bin into capacity tracking. Runs inside PutawayToBin's own
// transaction so the check and the insert that follows it see the same
// snapshot of bin_stock.
func enforceBinCapacity(tx *sql.Tx, schema, binCode, sku string, addQty int, maxQty, maxWeight, maxVolume float64) error {
	if maxQty <= 0 && maxWeight <= 0 && maxVolume <= 0 {
		return nil
	}
	var curQty, curWeight, curVolume float64
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(bs.qty), 0),
		       COALESCE(SUM(bs.qty * COALESCE(NULLIF(i.data->>'weight', '')::numeric, 0)), 0),
		       COALESCE(SUM(bs.qty * COALESCE(NULLIF(i.data->>'volume', '')::numeric, 0)), 0)
		FROM %s.bin_stock bs
		LEFT JOIN %s.documents i ON i.doctype = 'Item' AND i.data->>'code' = bs.sku
		WHERE bs.bin_code = $1`, schema, schema), binCode).Scan(&curQty, &curWeight, &curVolume); err != nil {
		return err
	}
	var itemWeight, itemVolume float64
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(data->>'weight', '')::numeric, 0), COALESCE(NULLIF(data->>'volume', '')::numeric, 0)
		FROM %s.documents WHERE doctype = 'Item' AND data->>'code' = $1`, schema), sku).Scan(&itemWeight, &itemVolume); err != nil && err != sql.ErrNoRows {
		return err
	}
	newQty := curQty + float64(addQty)
	newWeight := curWeight + float64(addQty)*itemWeight
	newVolume := curVolume + float64(addQty)*itemVolume
	if maxQty > 0 && newQty > maxQty {
		return fmt.Errorf("putaway would leave bin %s holding %.0f units, over its capacity of %.0f", binCode, newQty, maxQty)
	}
	if maxWeight > 0 && newWeight > maxWeight {
		return fmt.Errorf("putaway would leave bin %s holding %.2f weight units, over its max_weight of %.2f", binCode, newWeight, maxWeight)
	}
	if maxVolume > 0 && newVolume > maxVolume {
		return fmt.Errorf("putaway would leave bin %s holding %.2f volume units, over its max_volume of %.2f", binCode, newVolume, maxVolume)
	}
	return nil
}

// checkHazmatCompatibility (Stage 42.3.9) refuses a putaway of a
// hazmat-classified item (Item.hazmat_class not blank/None) into a bin whose
// zone has not opted into hazmat storage (Zone.hazmat_allowed != Yes). A bin
// with no zone set, or a zone with no hazmat_allowed value, is left exactly
// as validateBinMasterRules/the Zone migration already default it -
// hazmat_allowed defaults to Yes on every auto-created zone, so this is a
// no-op for any tenant that never classifies an item as hazmat, and only
// bites for a zone someone has explicitly marked hazmat_allowed = No.
func checkHazmatCompatibility(tx *sql.Tx, schema, sku, binZone, binCode string) error {
	var hazmatClass string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>'hazmat_class', '') FROM %s.documents WHERE doctype = 'Item' AND data->>'code' = $1`, schema),
		sku).Scan(&hazmatClass); err != nil && err != sql.ErrNoRows {
		return err
	}
	if hazmatClass == "" || hazmatClass == "None" || binZone == "" {
		return nil
	}
	var hazmatAllowed string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>'hazmat_allowed', 'Yes') FROM %s.documents WHERE doctype = 'Zone' AND data->>'code' = $1 AND status = 'Active'`, schema),
		binZone).Scan(&hazmatAllowed); err != nil {
		if err == sql.ErrNoRows {
			return nil // no matching Zone master - validateBinMasterRules already covers that gap
		}
		return err
	}
	if hazmatAllowed != "Yes" {
		return fmt.Errorf("item %s is classified %s - bin %s is in zone %s, which does not allow hazmat storage", sku, hazmatClass, binCode, binZone)
	}
	return nil
}

// PickListLine is one "go to this bin and pick this much" instruction.
type PickListLine struct {
	Sku       string `json:"sku"`
	BinCode   string `json:"bin_code"`
	Zone      string `json:"zone"`
	Aisle     string `json:"aisle"`
	Rack      string `json:"rack"`
	PickQty   int    `json:"pick_qty"`
	Shortfall int    `json:"shortfall,omitempty"`
	// Stage 42.1.5: which lot to take, and when it expires. Both empty for a
	// non-batch-tracked item, which is what keeps this payload unchanged for
	// every warehouse that has not opted into traceability.
	BatchNo    string `json:"batch_no,omitempty"`
	ExpiryDate string `json:"expiry_date,omitempty"`
	// Stage 42.1.10: PickQty stays in eaches always - allocation, bin_stock and
	// every existing reader all assume that unit. PickUOM/PickQtyInUOM are a
	// pure display convenience for a task whose line requested one ("pick 2
	// CASE" reads better than "pick 24 EA" on a floor where cases are how
	// people count) and are both empty when the task named no UOM, which is
	// every task created before this Stage.
	PickUOM      string  `json:"pick_uom,omitempty"`
	PickQtyInUOM float64 `json:"pick_qty_in_uom,omitempty"`
}

// GenerateBinPickList (20.18) builds a bin-grouped, walk-route-sorted (by
// zone/aisle/rack/bin) pick list for a FulfillmentTask's items, greedily
// allocating each line's required qty across bins holding Good-condition
// stock at the task's location. A shortfall (not enough binned stock to
// satisfy the line) is reported per-SKU rather than erroring the whole
// list, so a picker still gets a usable list for what a warehouse can
// actually fulfil right now, plus a visible flag on what needs handling.
func GenerateBinPickList(tenantID, taskID string) ([]PickListLine, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'FulfillmentTask' AND id = $1`, schema), taskID).
		Scan(&dataStr); err != nil {
		return nil, fmt.Errorf("fulfillment task not found: %v", err)
	}
	var task struct {
		LocationCode string `json:"location_code"`
		Items        []struct {
			Sku string `json:"sku"`
			Qty int    `json:"qty"`
			// Stage 42.1.10: optional pick-display UOM, e.g. "CASE". Blank for
			// every task created before this Stage, and for any caller that
			// still just wants eaches.
			PickUOM string `json:"pick_uom"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(dataStr), &task); err != nil {
		return nil, err
	}

	var lines []PickListLine
	for _, item := range task.Items {
		// Stage 42.1.5: bin selection moved to AllocateFromStock, the one choke
		// point that decides FIFO vs FEFO from the item's tracking_mode and
		// applies the expiry gates. Its FIFO branch runs the same query this
		// block used to; the only visible change for a non-batch item is the
		// walk-route sort, which is re-applied below rather than in SQL.
		candidates, shortfall, err := AllocateFromStock(tenantID, item.Sku, task.LocationCode, item.Qty)
		if err != nil {
			return nil, err
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			a, b := candidates[i], candidates[j]
			if a.Zone != b.Zone {
				return a.Zone < b.Zone
			}
			if a.Aisle != b.Aisle {
				return a.Aisle < b.Aisle
			}
			if a.Rack != b.Rack {
				return a.Rack < b.Rack
			}
			if a.BinCode != b.BinCode {
				return a.BinCode < b.BinCode
			}
			return a.BatchNo < b.BatchNo
		})
		for _, c := range candidates {
			line := PickListLine{
				Sku: item.Sku, BinCode: c.BinCode, Zone: c.Zone, Aisle: c.Aisle, Rack: c.Rack,
				PickQty: c.Qty, BatchNo: c.BatchNo, ExpiryDate: c.ExpiryDate,
			}
			// Display-only: a conversion that isn't defined is silently
			// skipped rather than failing the pick line - a picker missing a
			// "how many cases is that" hint still has the real eaches qty to
			// work from, and must never be blocked from picking real stock
			// because a UOM master row happens to be missing.
			if item.PickUOM != "" {
				if inUOM, err := ConvertUOMQty(tenantID, item.Sku, float64(c.Qty), "EA", item.PickUOM); err == nil {
					line.PickUOM, line.PickQtyInUOM = item.PickUOM, inUOM
				}
			}
			lines = append(lines, line)
		}
		if shortfall > 0 {
			lines = append(lines, PickListLine{Sku: item.Sku, BinCode: "", PickQty: 0, Shortfall: shortfall})
		}
	}
	return lines, nil
}

var validBinConditions = map[string]bool{"Good": true, "Damaged": true, "QC-Hold": true, "RTV": true}

// TransitionBinStockCondition (20.23) moves qty of a SKU in a bin from one
// condition to another (Good -> Damaged/QC-Hold, QC-Hold -> Good/Damaged/RTV,
// Damaged -> RTV, etc. - any pair is allowed, the workflow judgment of which
// transitions make sense is left to the operator, not hard-coded here).
// Keeps tenant_default.inventory_availability.available in sync: moving
// stock OUT of Good makes it unsellable (available decreases); moving INTO
// Good makes it sellable again (available increases). on_hand never changes
// here - the stock is still physically in the building throughout, exactly
// like the existing reserved-stock pattern in engines/inventory.go.
func TransitionBinStockCondition(tenantID, binCode, sku string, qty int, fromCondition, toCondition, userID string) error {
	if qty <= 0 {
		return errors.New("qty must be positive")
	}
	if !validBinConditions[fromCondition] || !validBinConditions[toCondition] {
		return fmt.Errorf("condition must be one of Good, Damaged, QC-Hold, RTV")
	}
	if fromCondition == toCondition {
		return errors.New("fromCondition and toCondition must differ")
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
		`SELECT location_code, qty FROM %s.bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = $3 FOR UPDATE`, schema),
		binCode, sku, fromCondition).Scan(&locationCode, &have)
	if err == sql.ErrNoRows {
		return fmt.Errorf("no %s-condition stock for SKU %s in bin %s", fromCondition, sku, binCode)
	} else if err != nil {
		return err
	}
	if have < qty {
		return fmt.Errorf("only %d units of %s in %s condition, cannot move %d", have, sku, fromCondition, qty)
	}

	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.bin_stock SET qty = qty - $1, updated_at = CURRENT_TIMESTAMP WHERE bin_code = $2 AND sku = $3 AND condition = $4`, schema),
		qty, binCode, sku, fromCondition); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.bin_stock (bin_code, sku, location_code, condition, qty)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (bin_code, sku, condition) DO UPDATE SET
			qty = %s.bin_stock.qty + EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema, schema),
		binCode, sku, locationCode, toCondition, qty); err != nil {
		return err
	}

	availabilityDelta := 0
	if fromCondition == "Good" && toCondition != "Good" {
		availabilityDelta = -qty
	} else if fromCondition != "Good" && toCondition == "Good" {
		availabilityDelta = qty
	}
	if availabilityDelta != 0 {
		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.inventory_availability SET available = GREATEST(0, available + $1), updated_at = CURRENT_TIMESTAMP
			 WHERE sku = $2 AND location_code = $3`, schema),
			availabilityDelta, sku, locationCode); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	// 26.10.1: Qty carries whatever moved in/out of the `available` bucket
	// (0 when neither side of the transition is Good, e.g. Damaged -> RTV) -
	// FromStatus/ToStatus are what make this entry meaningful either way.
	if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
		ItemID: sku, WarehouseID: locationCode, Qty: float64(availabilityDelta),
		VoucherType: "ConditionChange", VoucherID: binCode, UserID: userID,
		ToLocationID: binCode, FromStatus: fromCondition, ToStatus: toCondition,
	}); lerr != nil {
		LogSystemError(tenantID, "", "WARN", "TransitionBinStockCondition", fmt.Sprintf("stock ledger write failed for %s: %v", sku, lerr), "")
	}
	LogAuditEvent(tenantID, userID, "WMS_CONDITION_CHANGE", "SUCCESS",
		fmt.Sprintf("Moved %d x %s in bin %s from %s to %s", qty, sku, binCode, fromCondition, toCondition))
	return nil
}

// ReconcileCycleCount (20.20) computes system_qty/variance for every
// not-yet-reconciled CycleCountLine in a count session and, per line:
// zero variance posts immediately (nothing to approve); non-zero variance
// routes through the existing maker-checker engine (engines/approval.go,
// same reuse as the Stage 20.10 POS discount gate) rather than adjusting
// inventory unreviewed - satisfies 20.22's "posts to inventory only after
// approval" requirement. Returns how many lines fell into each bucket.
func ReconcileCycleCount(tenantID, countSession, actorUserID, actorRole string) (posted int, pendingApproval int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, err
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		WHERE doctype = 'CycleCountLine' AND data->>'count_session' = $1
		  AND status NOT IN ('Pending Approval', 'Approved', 'Rejected', 'Posted', 'Recount Requested')`, schema), countSession)
	if err != nil {
		return 0, 0, err
	}
	type lineToProcess struct {
		id   string
		data map[string]interface{}
	}
	var toProcess []lineToProcess
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			rows.Close()
			return 0, 0, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			continue
		}
		toProcess = append(toProcess, lineToProcess{id: id, data: data})
	}
	rows.Close()

	for _, line := range toProcess {
		sku, _ := line.data["sku"].(string)
		location, _ := line.data["location"].(string)
		countedQty := numFromInterface(line.data["counted_qty"])

		var systemQty int
		_ = db.DB.QueryRow(fmt.Sprintf(
			`SELECT on_hand FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema),
			sku, location).Scan(&systemQty)

		variance := int(countedQty) - systemQty
		line.data["system_qty"] = systemQty
		line.data["variance"] = variance

		if variance == 0 {
			line.data["status"] = "Posted"
			marshaled, _ := json.Marshal(line.data)
			_, execErr := db.DB.Exec(fmt.Sprintf(
				`UPDATE %s.documents SET data = $1, status = 'Posted', updated_at = CURRENT_TIMESTAMP WHERE id = $2`, schema),
				marshaled, line.id)
			if execErr != nil {
				return posted, pendingApproval, execErr
			}
			posted++
			continue
		}

		// Stored under "variance_qty" (not "variance") for extractAmount's
		// routing purposes - see engines/approval.go's comment on why this
		// doctype's routing key is separate from POSCart's "discount_amount".
		line.data["variance_qty"] = math.Abs(float64(variance))
		line.data["status"] = "Draft"
		marshaled, _ := json.Marshal(line.data)
		// SubmitForApproval below requires the documents.status *column* (not
		// just the JSON data.status field above) to already read 'Draft' -
		// it's a separate value from the JSON one, same distinction the
		// POSCart discount gate (Stage 20.10) has to manage.
		if _, execErr := db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = $1, status = 'Draft', updated_at = CURRENT_TIMESTAMP WHERE id = $2`, schema),
			marshaled, line.id); execErr != nil {
			return posted, pendingApproval, execErr
		}
		if err := SubmitForApproval(tenantID, "CycleCountLine", line.id, actorUserID, actorRole); err != nil {
			return posted, pendingApproval, fmt.Errorf("line %s: %v", line.id, err)
		}
		pendingApproval++
	}
	return posted, pendingApproval, nil
}

// PostCycleCountAdjustment (20.22) applies an Approved CycleCountLine's
// variance to inventory_availability (both on_hand and available move by
// the same signed amount - a physical count correction, not a sale/hold),
// then marks the line Posted. Called from handleDecideApproval on an
// Approved decision, the same finalize-on-approve pattern Stage 20.10 uses
// for POSCart.
func PostCycleCountAdjustment(tenantID, lineID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	var status, dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status, data FROM %s.documents WHERE doctype = 'CycleCountLine' AND id = $1`, schema), lineID).
		Scan(&status, &dataStr); err != nil {
		return fmt.Errorf("cycle count line not found: %v", err)
	}
	// 26.5.10: this function is now reachable a second time (a manual
	// retry after SetCycleCountVarianceReason fixes what the check below
	// rejected) - guard against double-posting the same adjustment twice.
	if status == "Posted" {
		return fmt.Errorf("cycle count line %s is already Posted", lineID)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	sku, _ := data["sku"].(string)
	location, _ := data["location"].(string)
	variance := int(numFromInterface(data["variance"]))

	// 26.5.10: every line reaching this function came through
	// ReconcileCycleCount's non-zero-variance branch (a zero-variance line
	// posts immediately there and never reaches an approval decision at
	// all), so a root-cause ReasonCode is always required here - not
	// conditionally, unconditionally, since variance != 0 is already
	// guaranteed by construction. Set via SetCycleCountVarianceReason
	// (handler-only, same as system_qty/variance/status themselves).
	if strVal, _ := data["variance_reason_code"].(string); strVal == "" {
		return fmt.Errorf("cycle count line %s cannot post: a variance root-cause reason code is required first (see SetCycleCountVarianceReason)", lineID)
	}

	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available)
		VALUES ($1, $2, $3, $3)
		ON CONFLICT (sku, location_code) DO UPDATE SET
			on_hand = GREATEST(0, %s.inventory_availability.on_hand + $3),
			available = GREATEST(0, %s.inventory_availability.available + $3),
			updated_at = CURRENT_TIMESTAMP`, schema, schema, schema),
		sku, location, variance); err != nil {
		return err
	}

	data["status"] = "Posted"
	marshaled, _ := json.Marshal(data)
	if _, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Posted', updated_at = CURRENT_TIMESTAMP WHERE id = $2`, schema),
		marshaled, lineID); err != nil {
		return err
	}
	// 26.10.1: the physical count correction itself, signed the same way as
	// the inventory_availability update just above.
	if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
		ItemID: sku, WarehouseID: location, Qty: float64(variance),
		VoucherType: "CycleCount", VoucherID: lineID, UserID: userID,
	}); lerr != nil {
		LogSystemError(tenantID, "", "WARN", "PostCycleCountAdjustment", fmt.Sprintf("stock ledger write failed for %s: %v", sku, lerr), "")
	}
	logTaskCompletion(tenantID, "CycleCount", userID, location, lineID, 1)
	// Stage 42.2.2: additive WarehouseTask retrofit.
	LogCompletedWarehouseTask(tenantID, NewWarehouseTask{
		TaskType: "Count", LocationCode: location, Item: sku, Qty: float64(variance),
		SourceDocType: "CycleCountLine", SourceDocID: lineID,
	}, userID)
	return nil
}

// numFromInterface reads a number that may have come from real JSON (a
// generic-doc create posts real JS numbers, unmarshalled as float64) or from
// engines.BulkImportCSV (every CSV cell, including numeric ones, is stored
// as a raw string - see import.go's docData[fieldName] = sanitizeCSVCell(...)).
// CycleCountLine's counted_qty is populated by CSV import as a matter of
// course (Stage 20.21), so silently treating the string case as 0 here -
// the original bug this comment replaces - would have posted a bogus,
// wildly-wrong inventory adjustment on every CSV-imported line.
func numFromInterface(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0
		}
		return f
	default:
		return 0
	}
}
