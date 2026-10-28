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
		return putawayRefused("putaway qty must be positive")
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
		return putawayRefused("bin %s not found", binCode)
	} else if err != nil {
		return err
	}
	if binStatus != "Active" {
		return putawayRefused("bin %s is not Active", binCode)
	}
	// Stage 42.2.6: bin_status is the bin's OPERATIONAL state, distinct from
	// the Active/Inactive record-status check above - a bin can be an Active
	// record and still be temporarily unavailable for placement. Blank (every
	// bin before this Stage, and any bin that never sets it) is treated as
	// Available.
	if binOpState == "Blocked" || binOpState == "Full" || binOpState == "Counting" {
		return putawayRefused("bin %s is %s and cannot receive putaway right now", binCode, binOpState)
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
		return putawayRefused("no on-hand stock for SKU %s at location %s", sku, location)
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
		return putawayRefused("putaway qty exceeds unassigned on-hand stock at %s (on_hand=%d, already binned=%d, requested=%d)",
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

	// 26.10.1: putaway never changes on_hand/available (the stock was already
	// on-hand, unassigned, before this call) - a Qty 0 entry still records
	// the physical move from unassigned stock into binCode.
	// Stage 132.20: the ledger line commits with the movement (it was written after, best-effort).
	if err := insertStockLedgerEntry(tx, schema, StockLedgerEntry{
		ItemID: sku, WarehouseID: location, Qty: 0,
		VoucherType: "Putaway", VoucherID: binCode, UserID: userID, ToLocationID: binCode,
	}); err != nil {
		return fmt.Errorf("stock ledger: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
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
		return putawayRefused("putaway would leave bin %s holding %.0f units, over its capacity of %.0f", binCode, newQty, maxQty)
	}
	if maxWeight > 0 && newWeight > maxWeight {
		return putawayRefused("putaway would leave bin %s holding %.2f weight units, over its max_weight of %.2f", binCode, newWeight, maxWeight)
	}
	if maxVolume > 0 && newVolume > maxVolume {
		return putawayRefused("putaway would leave bin %s holding %.2f volume units, over its max_volume of %.2f", binCode, newVolume, maxVolume)
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
		`SELECT COALESCE(data->>'hazmat_allowed', 'Yes') FROM %s.documents WHERE doctype = 'Zone' AND data->>'code' = $1 AND status = 'Active' AND deleted_at IS NULL`, schema),
		binZone).Scan(&hazmatAllowed); err != nil {
		if err == sql.ErrNoRows {
			return nil // no matching Zone master - validateBinMasterRules already covers that gap
		}
		return err
	}
	if hazmatAllowed != "Yes" {
		return putawayRefused("item %s is classified %s - bin %s is in zone %s, which does not allow hazmat storage", sku, hazmatClass, binCode, binZone)
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

	// Stage 59.1: the list is the task's own reservation, not a fresh guess.
	// ReserveTaskBinStock tops up whatever is not yet reserved (through
	// AllocateFromStock's FIFO/FEFO choke point, skipping stock promised to
	// other tasks) and the lines below are read back from bin_allocation - so
	// calling this twice shows the same bins, and the picker is sent to stock
	// nobody else has been sent to. Lines show what is still to pick.
	if err := ReserveTaskBinStock(tenantID, taskID); err != nil {
		return nil, err
	}
	var rawData map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &rawData)
	reserved, err := openTaskAllocations(schema, []string{taskID})
	if err != nil {
		return nil, err
	}
	reservedBySKU := map[string][]taskAllocationLine{}
	for _, r := range reserved {
		reservedBySKU[r.SKU] = append(reservedBySKU[r.SKU], r)
	}
	pickUOMBySKU := map[string]string{}
	for _, item := range task.Items {
		if _, seen := pickUOMBySKU[item.Sku]; !seen {
			pickUOMBySKU[item.Sku] = item.PickUOM
		}
	}

	var lines []PickListLine
	for _, need := range taskSKUNeeds(rawData) {
		item := struct{ Sku, PickUOM string }{need.SKU, pickUOMBySKU[need.SKU]}
		candidates := reservedBySKU[need.SKU]
		shortfall := need.Demand - need.Picked
		for _, c := range candidates {
			shortfall -= c.Qty
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

// binConditionBucket is each bin condition's stock-location bucket (Stage
// 85.B6; the master's wms_condition column says the same the other way).
var binConditionBucket = map[string]string{"Good": "available", "Damaged": "damaged", "QC-Hold": "qc_hold", "RTV": "rtv"}

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
	return TransitionBinStockConditionForOwner(tenantID, binCode, sku, qty, fromCondition, toCondition, "", userID)
}

// TransitionBinStockConditionForOwner (Stage 59.4) changes the condition of
// one owner's units when the bin holds several owners' stock; the owner's
// slice moves to the new condition with them.
func TransitionBinStockConditionForOwner(tenantID, binCode, sku string, qty int, fromCondition, toCondition, ownerID, userID string) error {
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
	// Stage 59.1: Good stock can be promised to a pick task, so take the SKU's
	// reservation lock first - the same lock-then-row order picks use.
	if fromCondition == "Good" {
		if err := tx.QueryRow(fmt.Sprintf(
			`SELECT location_code FROM %s.bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = 'Good'`, schema),
			binCode, sku).Scan(&locationCode); err == nil {
			if err := lockSKUAtLocationTx(tx, schema, locationCode, sku); err != nil {
				return err
			}
		}
	}
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
	if fromCondition == "Good" {
		// A unit promised to an open pick task cannot quietly stop being Good
		// underneath it: short-pick or reject that task first, which hands
		// the reservation back, then mark the stock.
		reserved, err := BinReservedQty(tx, schema, binCode, sku, "")
		if err != nil {
			return err
		}
		if have-reserved < qty {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Quantity", Message: fmt.Sprintf(
				"bin %s holds %d Good units of %s but %d are reserved for open pick tasks, so only %d can change condition - short-pick or reject the task holding them first",
				binCode, have, sku, reserved, have-reserved)}
		}
	}

	movedOwner, err := moveOwnerSliceTx(tx, tenantID, schema, binCode, binCode, sku, fromCondition, toCondition, locationCode, have, qty, ownerID)
	if err != nil {
		return err
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
	// Stage 85.B6: the WMS takes its stock picture from the ERP's stock
	// locations - a Damaged / QC-Hold / RTV bin condition IS the store's
	// Damage / QC Hold / Purchase Return location - so the units leave one
	// bucket and arrive in the other, instead of only leaving available and
	// being counted nowhere.
	if fromCondition != toCondition {
		fromCol, toCol := binConditionBucket[fromCondition], binConditionBucket[toCondition]
		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.inventory_availability SET %s = GREATEST(0, %s - $1), %s = %s + $1, updated_at = CURRENT_TIMESTAMP
			 WHERE sku = $2 AND location_code = $3`, schema, fromCol, fromCol, toCol, toCol),
			qty, sku, locationCode); err != nil {
			return err
		}
	}

	// 26.10.1: Qty carries whatever moved in/out of the `available` bucket
	// (0 when neither side of the transition is Good, e.g. Damaged -> RTV) -
	// FromStatus/ToStatus are what make this entry meaningful either way.
	// Stage 132.20: the ledger line commits with the movement (it was written after, best-effort).
	if err := insertStockLedgerEntry(tx, schema, StockLedgerEntry{
		ItemID: sku, WarehouseID: locationCode, Qty: float64(availabilityDelta),
		VoucherType: "ConditionChange", VoucherID: binCode, UserID: userID,
		ToLocationID: binCode, FromStatus: fromCondition, ToStatus: toCondition, OwnerID: movedOwner,
	}); err != nil {
		return fmt.Errorf("stock ledger: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
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
	res, err := ReconcileCycleCountDetailed(tenantID, countSession, actorUserID, actorRole)
	return res.Posted, res.PendingApproval, err
}

// Stage 73.1.8: a first count whose variance crosses any of these (each 0 =
// off) is not sent for approval - a blind recount is requested instead, and
// only the recount's variance goes to the approver.
const (
	RecountTriggerQtySetting   = "wms.recount_trigger_qty"
	RecountTriggerPctSetting   = "wms.recount_trigger_pct"
	RecountTriggerValueSetting = "wms.recount_trigger_value"
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: RecountTriggerQtySetting, Module: "Warehouse",
		Label: "Recount when a count is off by", Type: SettingTypeInt, Default: "0", Unit: "units",
		Min:         settingBound(0),
		Description: "A first count that differs from the books by at least this many units gets a blind recount before anyone approves it. 0 = off.",
	})
	RegisterSetting(SettingDefinition{
		Key: RecountTriggerPctSetting, Module: "Warehouse",
		Label: "Recount when a count is off by (percent)", Type: SettingTypeFloat, Default: "0", Unit: "%",
		Min:         settingBound(0),
		Description: "A first count that differs from the books by at least this percentage gets a blind recount (a count against zero on the books is 100%). 0 = off.",
	})
	RegisterSetting(SettingDefinition{
		Key: RecountTriggerValueSetting, Module: "Warehouse",
		Label: "Recount when a count is off by (value)", Type: SettingTypeFloat, Default: "0", Unit: "₹",
		Min:         settingBound(0),
		Description: "A first count whose difference is worth at least this much at item cost gets a blind recount. 0 = off.",
	})
}

// CycleCountReconcileResult is what one reconcile run did.
type CycleCountReconcileResult struct {
	Posted           int                 `json:"posted_no_variance"`
	PendingApproval  int                 `json:"pending_approval"`
	RecountRequested int                 `json:"recount_requested"`
	Recounts         []CycleCountRecount `json:"recounts,omitempty"`
	AwaitingCount    int                 `json:"awaiting_count"`
}

// CycleCountRecount names a line sent back for a blind recount and why.
type CycleCountRecount struct {
	LineID    string `json:"line_id"`
	NewLineID string `json:"recount_line_id"`
	Sku       string `json:"sku"`
	Location  string `json:"location"`
	Trigger   string `json:"trigger"`
}

// recountTrigger says which configured trigger a variance crosses, or "".
func recountTrigger(tenantID, sku string, systemQty, variance int) string {
	abs := variance
	if abs < 0 {
		abs = -abs
	}
	if abs == 0 {
		return ""
	}
	if n := GetSettingInt(tenantID, RecountTriggerQtySetting); n > 0 && abs >= n {
		return fmt.Sprintf("off by %d units (trigger %d)", abs, n)
	}
	if p := GetSettingFloat(tenantID, RecountTriggerPctSetting); p > 0 {
		pct := 100.0
		if systemQty != 0 {
			pct = math.Abs(float64(abs) / float64(systemQty) * 100)
		}
		if pct >= p {
			return fmt.Sprintf("off by %.1f%% (trigger %.1f%%)", pct, p)
		}
	}
	if v := GetSettingFloat(tenantID, RecountTriggerValueSetting); v > 0 {
		if cost, ok := ResolveQuoteUnitCostPaise(tenantID, sku); ok {
			if worth := PaiseToRupees(cost * int64(abs)); worth >= v {
				return fmt.Sprintf("off by ₹%.2f at cost (trigger ₹%.2f)", worth, v)
			}
		}
	}
	return ""
}

// ReconcileCycleCountDetailed is ReconcileCycleCount with the recount
// outcome (Stage 73.1.8) reported too.
func ReconcileCycleCountDetailed(tenantID, countSession, actorUserID, actorRole string) (res CycleCountReconcileResult, err error) {
	posted, pendingApproval := 0, 0
	defer func() { res.Posted, res.PendingApproval = posted, pendingApproval }()
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		WHERE doctype = 'CycleCountLine' AND data->>'count_session' = $1
		  AND status NOT IN ('Pending Approval', 'Approved', 'Rejected', 'Posted', 'Recount Requested')`, schema), countSession)
	if err != nil {
		return res, err
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
			return res, err
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
		// Stage 73.1.8: a recount placeholder nobody has counted yet is not a
		// count of zero - it waits for SubmitRecountValue.
		if raw, ok := line.data["counted_qty"]; !ok || raw == nil || raw == "" {
			res.AwaitingCount++
			continue
		}
		countedQty := numFromInterface(line.data["counted_qty"])

		var systemQty int
		// Stage 130.16: goods out on a delivery challan are owned but not here.
		_ = db.DB.QueryRow(fmt.Sprintf(
			`SELECT on_hand - COALESCE(with_party, 0) FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema),
			sku, location).Scan(&systemQty)
		// Stage 107: stock in a location marked not physically in the store
		// is in on_hand but not on the shelf, so the count does not expect it.
		systemQty -= notPhysicalCustomQty(schema, sku, location)

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
				return res, execErr
			}
			posted++
			continue
		}

		// Stage 73.1.8: a first count past a recount trigger goes back for a
		// blind recount (RequestRecount) instead of to an approver. A recount
		// line is never re-triggered - its variance is what gets approved,
		// with the first count kept beside it for the approver.
		if origID, _ := line.data["recount_of"].(string); origID == "" {
			if trigger := recountTrigger(tenantID, sku, systemQty, variance); trigger != "" {
				line.data["recount_trigger"] = trigger
				marshaled, _ := json.Marshal(line.data)
				if _, execErr := db.DB.Exec(fmt.Sprintf(
					`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, schema),
					marshaled, line.id); execErr != nil {
					return res, execErr
				}
				newID, rerr := RequestRecount(tenantID, line.id, actorUserID)
				if rerr != nil {
					return res, fmt.Errorf("line %s: %v", line.id, rerr)
				}
				res.RecountRequested++
				res.Recounts = append(res.Recounts, CycleCountRecount{LineID: line.id, NewLineID: newID, Sku: sku, Location: location, Trigger: trigger})
				continue
			}
		} else {
			var origData string
			if db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'CycleCountLine' AND id = $1`, schema), origID).Scan(&origData) == nil {
				var orig map[string]interface{}
				if json.Unmarshal([]byte(origData), &orig) == nil {
					line.data["first_count_qty"] = orig["counted_qty"]
					line.data["first_count_variance"] = orig["variance"]
				}
			}
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
			return res, execErr
		}
		if err := SubmitForApproval(tenantID, "CycleCountLine", line.id, actorUserID, actorRole); err != nil {
			return res, fmt.Errorf("line %s: %v", line.id, err)
		}
		pendingApproval++
	}
	return res, nil
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

	// Stage 73.1.8: one transaction under the line's row lock - the stock
	// correction and the line's Posted status commit together or not at all,
	// so a retry or a double click can never post the same variance twice
	// (the old read-check-write could), and only an Approved line posts.
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil { // the GL leg posts in this transaction (74.2.3)
		return err
	}
	var status, dataStr string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT status, data FROM %s.documents WHERE doctype = 'CycleCountLine' AND id = $1 FOR UPDATE`, schema), lineID).
		Scan(&status, &dataStr); err != nil {
		return fmt.Errorf("cycle count line not found: %v", err)
	}
	// 26.5.10: this function is now reachable a second time (a manual
	// retry after SetCycleCountVarianceReason fixes what the check below
	// rejected) - guard against double-posting the same adjustment twice.
	if status == "Posted" {
		return fmt.Errorf("cycle count line %s is already Posted", lineID)
	}
	if status != "Approved" {
		return fmt.Errorf("cycle count line %s is %s - only an Approved variance posts", lineID, status)
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

	// Stage 135.16: the variance was measured against the book at count time.
	// A book that has since fallen so far that the variance takes it below
	// zero means the count is stale: refuse, rather than floor on_hand at zero
	// while the ledger line below records the full variance.
	var onHand int
	if err := tx.QueryRow(fmt.Sprintf(`SELECT on_hand FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2 FOR UPDATE`, schema),
		sku, location).Scan(&onHand); err != nil && err != sql.ErrNoRows {
		return err
	}
	if onHand+variance < 0 {
		return fmt.Errorf("cycle count line %s cannot post: %s at %s has %d on hand now, and its variance of %d would take that below zero - stock moved since the count, so count it again", lineID, sku, location, onHand, variance)
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available)
		VALUES ($1, $2, $3, $3)
		ON CONFLICT (sku, location_code) DO UPDATE SET
			on_hand = %s.inventory_availability.on_hand + $3,
			available = GREATEST(0, %s.inventory_availability.available + $3),
			updated_at = CURRENT_TIMESTAMP`, schema, schema, schema),
		sku, location, variance); err != nil {
		return err
	}

	// Stage 130.13 (80.7 f): a count of one bin sets that bin to what was
	// counted, so bins and the location agree after the count.
	if err := applyCycleCountToBinTx(tx, schema, strField(data, "bin"), sku, location, int(numFromInterface(data["counted_qty"]))); err != nil {
		return err
	}

	// Stage 74.2.3 (closing 74.0.2 b): the variance's value is booked with it,
	// in this transaction - a gain Dr 1200 / Cr 5190, a shortage the other way,
	// at the item's cost, through the adjustment posting profiles (74.1.3). An
	// item with no cost on record books nothing, as a till sale does.
	if costPaise, ok := ResolveQuoteUnitCostPaise(tenantID, sku); ok && costPaise > 0 && variance != 0 {
		units := int64(variance)
		event, debit, credit := PostingEventAdjustmentGain, accountInventory, accountStockAdjustment
		if variance < 0 {
			units, event, debit, credit = -units, PostingEventAdjustmentLoss, accountStockAdjustment, accountInventory
		}
		value := costPaise * units
		acc := ResolvePostingAccounts(tenantID, event, PostingContext{LegalEntity: locationLegalEntity(tenantID, location), ReasonCode: strField(data, "variance_reason_code")}, debit, credit)
		if err := PostDoubleEntryTx(tx, tenantID, schema, "CycleCountLine", lineID, map[string]int64{acc.Debit: value}, map[string]int64{acc.Credit: value}, "", "CycleCount:"+lineID+":GL"); err != nil {
			return err
		}
		data["variance_value"] = PaiseToRupees(value)
	}
	// 26.10.1: the physical count correction itself, signed the same way as
	// the inventory_availability update just above - now inside the
	// transaction (74.2.3), so the ledger can never miss a posted count.
	if err := insertStockLedgerEntry(tx, schema, StockLedgerEntry{
		ItemID: sku, WarehouseID: location, Qty: float64(variance),
		VoucherType: "CycleCount", VoucherID: lineID, UserID: userID,
		IdempotencyKey: "CYCLECOUNT:" + lineID,
		// Stage 59.4: a count of one owner's stock says so on the ledger.
		OwnerID: strField(data, "owner_id"),
	}); err != nil {
		return err
	}

	data["status"] = "Posted"
	data["posted_by"] = userID
	marshaled, _ := json.Marshal(data)
	if _, err = tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Posted', updated_at = CURRENT_TIMESTAMP WHERE id = $2`, schema),
		marshaled, lineID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
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

// CycleCountLineView is one line of a count session as the Cycle Count
// screen shows it (Stage 55.6): the book quantity and difference only once
// the line has been reconciled, so a counter is never shown what the system
// expects, and a recount line never shows the first count.
type CycleCountLineView struct {
	ID            string   `json:"id"`
	Sku           string   `json:"sku"`
	ItemName      string   `json:"item_name,omitempty"`
	Location      string   `json:"location"`
	Bin           string   `json:"bin,omitempty"`
	Status        string   `json:"status"`
	CountedQty    *float64 `json:"counted_qty,omitempty"`
	SystemQty     *float64 `json:"system_qty,omitempty"`
	Variance      *float64 `json:"variance,omitempty"`
	ReasonCode    string   `json:"variance_reason_code,omitempty"`
	RecountOf     string   `json:"recount_of,omitempty"`
	RecountLineID string   `json:"recount_line_id,omitempty"`
	Trigger       string   `json:"recount_trigger,omitempty"`
	FirstCountQty *float64 `json:"first_count_qty,omitempty"`
	OwnerID       string   `json:"owner_id,omitempty"`
}

// CycleCountSessionLines lists a session's lines for the work table.
func CycleCountSessionLines(tenantID, session string) ([]CycleCountLineView, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(status, ''), data FROM %s.documents
		 WHERE doctype = 'CycleCountLine' AND deleted_at IS NULL AND data->>'count_session' = $1
		 ORDER BY created_at, id`, schema), session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CycleCountLineView{}
	recountOf := map[string]string{}
	for rows.Next() {
		var id, status, raw string
		if err := rows.Scan(&id, &status, &raw); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &d)
		v := CycleCountLineView{ID: id, Sku: strField(d, "sku"), Location: strField(d, "location"), Bin: strField(d, "bin"),
			Status: status, ReasonCode: strField(d, "variance_reason_code"), RecountOf: strField(d, "recount_of"),
			Trigger: strField(d, "recount_trigger"), OwnerID: strField(d, "owner_id")}
		num := func(k string) *float64 {
			if x, ok := d[k]; ok && x != nil && x != "" {
				f := numFromInterface(x)
				return &f
			}
			return nil
		}
		v.CountedQty = num("counted_qty")
		if _, reconciled := d["system_qty"]; reconciled {
			v.SystemQty, v.Variance = num("system_qty"), num("variance")
			if v.RecountOf != "" {
				v.FirstCountQty = num("first_count_qty")
			}
		}
		if v.RecountOf != "" {
			recountOf[v.RecountOf] = id
		}
		if item, _, err := fetchDocData(tenantID, "Item", v.Sku); err == nil {
			v.ItemName = strField(item, "name")
		}
		out = append(out, v)
	}
	for i := range out {
		out[i].RecountLineID = recountOf[out[i].ID]
	}
	return out, rows.Err()
}

// putawayRefused (73.B.10) is every putaway refusal - a bin that is missing,
// inactive, blocked or full, a quantity the location's unbinned stock cannot
// cover, a hazmat zone - as INVENT-0971 with its reason, instead of a plain
// error the API reported as a field-format problem (GLOBAL-0002).
func putawayRefused(format string, args ...interface{}) error {
	return &ValidationError{Code: "INVENT-0971", SubFor: "Bin", Message: fmt.Sprintf(format, args...)}
}
