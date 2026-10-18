package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 59.2-59.5 - mixed-owner (3PL) isolation, built on 59.1's bin stock
// that tracks reality (decision D07: full isolation).
//
// Whose stock is a unit? The attribution rule bin_stock_owner and the billing
// queries already use (wms_owner_stock.go), applied to one (bin, sku,
// condition) slice:
//   1. if the slice has explicit owner rows (bin_stock_owner), those rows say
//      whose units they are, and any unsplit remainder belongs to nobody;
//   2. otherwise, if the Bin names an owner (Bin.owner_id), all of it is that
//      owner's;
//   3. otherwise, if the warehouse is dedicated to an owner (warehouse_owner),
//      all of it is that owner's;
//   4. otherwise it is unattributed.
//
// What this file adds on top of that rule:
//   - 59.2: an order carries one owner (SalesOrder.owner_id) and each of its
//     FulfillmentTasks inherits it at release (ResolveTaskOwnerQ), defaulting
//     to the warehouse's owner; an ownerless order at a location holding more
//     than one owner's stock is refused.
//   - 59.3: allocation for a task with an owner offers only that owner's
//     units (allocateFromStockForOwnerQ), reservations record the owner, and
//     owner availability (OwnerAvailableToSell, FindBestFulfillmentNode)
//     counts only that owner's stock.
//   - 59.4: putaway, bin moves and condition changes carry the owner's slice
//     with the units; a Bin's owner cannot be changed under its stock.
//   - 59.5: an exception queue (report owner-stock-exceptions) lists every
//     place the attribution no longer adds up, with a reasoned correction.

// OwnershipMixed is the supported mixed-owner mode (Stage 59.6). The pre-59
// value "mixed_unsupported" still means mixed, so a tenant that opted in
// before keeps working - now isolated.
const OwnershipMixed = "mixed"

// MixedOwnershipEnabled reports whether a warehouse may hold several owners.
func MixedOwnershipEnabled(tenantID string) bool { return !SingleOwnerEnforced(tenantID) }

func warehouseOwnerQ(q stockQuerier, schema, locationCode string) (string, error) {
	var owner string
	err := q.QueryRow(fmt.Sprintf(`SELECT owner_id FROM %s.warehouse_owner WHERE location_code = $1`, schema), locationCode).Scan(&owner)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return owner, err
}

// ownersAtLocationQ lists every owner holding stock (any condition) at a
// location by the attribution rule: explicit slices, plus whole-bin owners of
// bins whose stock has no slices.
func ownersAtLocationQ(q stockQuerier, schema, locationCode string) ([]string, error) {
	rows, err := q.Query(fmt.Sprintf(`
		SELECT DISTINCT owner FROM (
			SELECT owner_id AS owner FROM %s.bin_stock_owner WHERE location_code = $1 AND qty > 0
			UNION
			SELECT b.data->>'owner_id' AS owner
			  FROM %s.bin_stock bs
			  JOIN %s.documents b ON b.doctype = 'Bin' AND b.deleted_at IS NULL AND b.data->>'bin_code' = bs.bin_code
			 WHERE bs.location_code = $1 AND bs.qty > 0 AND COALESCE(b.data->>'owner_id', '') <> ''
			   AND NOT EXISTS (SELECT 1 FROM %s.bin_stock_owner bso
			                    WHERE bso.bin_code = bs.bin_code AND bso.sku = bs.sku AND bso.condition = bs.condition AND bso.qty > 0)
		) o WHERE COALESCE(owner, '') <> '' ORDER BY 1`, schema, schema, schema, schema), locationCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ResolveTaskOwnerQ decides the owner a FulfillmentTask at locationCode works
// for (59.2): the order's own owner; else the warehouse's dedicated owner;
// else the one owner holding stock there; else none. An order with no owner
// at a location holding several owners' stock is refused - picking it would
// have to guess whose units to ship.
func ResolveTaskOwnerQ(q stockQuerier, tenantID, schema, locationCode, orderOwner string) (string, error) {
	orderOwner = strings.TrimSpace(orderOwner)
	dedicated, err := warehouseOwnerQ(q, schema, locationCode)
	if err != nil {
		return "", err
	}
	if orderOwner != "" {
		if dedicated != "" && dedicated != orderOwner && SingleOwnerEnforced(tenantID) {
			return "", &ValidationError{Code: "INVENT-0491", Message: fmt.Sprintf(
				"%s holds only %s's stock, so it cannot fulfil an order for %s - source the order from %s's own warehouse", locationCode, dedicated, orderOwner, orderOwner)}
		}
		return orderOwner, nil
	}
	if dedicated != "" {
		return dedicated, nil
	}
	owners, err := ownersAtLocationQ(q, schema, locationCode)
	if err != nil {
		return "", err
	}
	switch len(owners) {
	case 0:
		return "", nil
	case 1:
		return owners[0], nil
	}
	return "", &ValidationError{Code: "INVENT-0492", Message: fmt.Sprintf(
		"%s holds stock of %s - set the order's owner (3PL client) before releasing it, so only that owner's stock is picked", locationCode, strings.Join(owners, ", "))}
}

// ownerBinCapacityQ is, per bin, how many Good units of sku at locationCode
// belong to owner and are not already reserved for one of owner's live tasks.
func ownerBinCapacityQ(q stockQuerier, schema, sku, locationCode, owner string) (map[string]int, error) {
	dedicated, err := warehouseOwnerQ(q, schema, locationCode)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(fmt.Sprintf(`
		SELECT bs.bin_code,
		       CASE WHEN anyo.q IS NOT NULL THEN COALESCE(own.q, 0)
		            WHEN COALESCE(b.owner_id, '') <> '' THEN CASE WHEN b.owner_id = $3 THEN bs.qty ELSE 0 END
		            WHEN $4 THEN bs.qty ELSE 0 END
		       - COALESCE(res.reserved, 0)
		  FROM %s.bin_stock bs
		  LEFT JOIN (SELECT DISTINCT ON (data->>'bin_code') data->>'bin_code' AS bin_code, data->>'owner_id' AS owner_id
		               FROM %s.documents WHERE doctype = 'Bin' AND deleted_at IS NULL ORDER BY data->>'bin_code', id) b
		         ON b.bin_code = bs.bin_code
		  LEFT JOIN (SELECT bin_code, SUM(qty) AS q FROM %s.bin_stock_owner
		              WHERE sku = $1 AND location_code = $2 AND condition = 'Good' AND qty > 0 GROUP BY bin_code) anyo
		         ON anyo.bin_code = bs.bin_code
		  LEFT JOIN (SELECT bin_code, SUM(qty) AS q FROM %s.bin_stock_owner
		              WHERE sku = $1 AND location_code = $2 AND condition = 'Good' AND owner_id = $3 GROUP BY bin_code) own
		         ON own.bin_code = bs.bin_code
		  LEFT JOIN (SELECT ba.bin_code, SUM(ba.qty - ba.picked_qty) AS reserved
		               FROM %s.bin_allocation ba
		               JOIN %s.documents ft ON ft.id = ba.task_id AND ft.doctype = 'FulfillmentTask' AND ft.status IN %s
		              WHERE ba.status = 'Open' AND ba.sku = $1 AND ba.location_code = $2 AND ba.owner_id = $3
		              GROUP BY ba.bin_code) res
		         ON res.bin_code = bs.bin_code
		 WHERE bs.sku = $1 AND bs.location_code = $2 AND bs.condition = 'Good'`,
		schema, schema, schema, schema, schema, schema, liveTaskStatusesSQL), sku, locationCode, owner, dedicated == owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var bin string
		var n int
		if err := rows.Scan(&bin, &n); err != nil {
			return nil, err
		}
		if n > 0 {
			out[bin] = n
		}
	}
	return out, rows.Err()
}

// allocateFromStockForOwnerQ is allocateFromStockQ for a task with an owner:
// the same strategy order (FIFO / FEFO / nearest bin ...), net of every live
// reservation, then capped bin by bin to what the owner holds there. A task
// with no owner allocates exactly as before.
func allocateFromStockForOwnerQ(q stockQuerier, tenantID, sku, locationCode string, needed int, owner string) ([]AllocationCandidate, int, error) {
	if owner == "" || needed <= 0 {
		return allocateFromStockQ(q, tenantID, sku, locationCode, needed)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, needed, err
	}
	caps, err := ownerBinCapacityQ(q, schema, sku, locationCode, owner)
	if err != nil {
		return nil, needed, err
	}
	all, _, err := allocateFromStockQ(q, tenantID, sku, locationCode, 1<<30)
	if err != nil {
		return nil, needed, err
	}
	var out []AllocationCandidate
	remaining := needed
	for _, c := range all {
		if remaining == 0 {
			break
		}
		take := c.Qty
		if left := caps[c.BinCode]; take > left {
			take = left
		}
		if take > remaining {
			take = remaining
		}
		if take <= 0 {
			continue
		}
		caps[c.BinCode] -= take
		c.Qty = take
		out = append(out, c)
		remaining -= take
	}
	return out, remaining, nil
}

// taskOwnerQ reads a FulfillmentTask's owner.
func taskOwnerQ(q stockQuerier, schema, taskID string) (string, error) {
	var owner string
	err := q.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>'owner_id', '') FROM %s.documents WHERE doctype = 'FulfillmentTask' AND id = $1`, schema), taskID).Scan(&owner)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return owner, err
}

// OwnerAvailability is one owner's available-to-sell at one location.
type OwnerAvailability struct {
	Sku          string `json:"sku"`
	LocationCode string `json:"location_code"`
	OwnerID      string `json:"owner_id"`
	LocationATS  int    `json:"location_ats"`
	OwnerBinned  int    `json:"owner_binned_free"`
	Available    int    `json:"available"`
	Basis        string `json:"basis"`
}

// ownerAvailabilityQ caps a location's available-to-sell to the owner's own
// free units (59.3). A location dedicated to the owner and holding no one
// else's stock is the owner's whole ATS; a location dedicated to another
// owner offers nothing in one-owner mode.
func ownerAvailabilityQ(q stockQuerier, tenantID, schema, sku, locationCode, owner string, locationATS int) (OwnerAvailability, error) {
	out := OwnerAvailability{Sku: sku, LocationCode: locationCode, OwnerID: owner, LocationATS: locationATS, Available: locationATS, Basis: "location"}
	if owner == "" {
		return out, nil
	}
	dedicated, err := warehouseOwnerQ(q, schema, locationCode)
	if err != nil {
		return out, err
	}
	owners, err := ownersAtLocationQ(q, schema, locationCode)
	if err != nil {
		return out, err
	}
	onlyOwner := true
	for _, o := range owners {
		if o != owner {
			onlyOwner = false
		}
	}
	if dedicated == owner && onlyOwner {
		out.Basis = "warehouse dedicated to " + owner
		return out, nil
	}
	if dedicated != "" && dedicated != owner && SingleOwnerEnforced(tenantID) {
		out.Available, out.Basis = 0, "warehouse dedicated to "+dedicated
		return out, nil
	}
	caps, err := ownerBinCapacityQ(q, schema, sku, locationCode, owner)
	if err != nil {
		return out, err
	}
	for _, n := range caps {
		out.OwnerBinned += n
	}
	if out.OwnerBinned < out.Available {
		out.Available = out.OwnerBinned
	}
	if out.Available < 0 {
		out.Available = 0
	}
	out.Basis = "owner's own bins"
	return out, nil
}

// OwnerCappedATS is the owner-aware ATS FindBestFulfillmentNode uses.
func OwnerCappedATS(tenantID, sku, locationCode, owner string, locationATS int) int {
	if owner == "" {
		return locationATS
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0
	}
	a, err := ownerAvailabilityQ(db.DB, tenantID, schema, sku, locationCode, owner, locationATS)
	if err != nil {
		return 0
	}
	return a.Available
}

// OwnerCheckedPlan keeps a sourcing plan only if the owner's own stock covers
// every line where the plan sends it (59.3); otherwise it re-sources on the
// owner's stock alone, and returns nil (the order goes on allocation hold)
// when no location holds enough of the owner's stock. Without this, an
// owner's order could reserve location stock that belongs to someone else.
func OwnerCheckedPlan(tenantID, owner string, lines []SalesOrderLineInput, plan *AllocationPlan) (*AllocationPlan, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || plan == nil {
		return plan, nil
	}
	type key struct{ loc, sku string }
	need := map[key]int{}
	for i, l := range lines {
		if i < len(plan.LineLocations) {
			need[key{plan.LineLocations[i], l.SKU}] += l.Qty
		}
	}
	covered := true
	for k, q := range need {
		a, err := OwnerAvailableToSell(tenantID, k.sku, k.loc, owner)
		if err != nil {
			return nil, err
		}
		if a.Available < q {
			covered = false
			break
		}
	}
	if covered {
		return plan, nil
	}
	items := make([]map[string]interface{}, 0, len(lines))
	for _, l := range lines {
		items = append(items, map[string]interface{}{"sku": l.SKU, "qty": l.Qty, "owner_id": owner})
	}
	loc, err := FindBestFulfillmentNode(tenantID, items)
	if err != nil || loc == "" {
		return nil, nil
	}
	// FindBestFulfillmentNode falls back to the head office when no node
	// qualifies - that is no answer for an owner's own stock.
	perSku := map[string]int{}
	for _, l := range lines {
		perSku[l.SKU] += l.Qty
	}
	for s, q := range perSku {
		a, err := OwnerAvailableToSell(tenantID, s, loc, owner)
		if err != nil {
			return nil, err
		}
		if a.Available < q {
			return nil, nil
		}
	}
	return uniformPlan(loc, "Owner stock", len(lines)), nil
}

// OwnerAvailableToSell reads one owner's ATS for an item at a location.
func OwnerAvailableToSell(tenantID, sku, locationCode, owner string) (OwnerAvailability, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return OwnerAvailability{}, err
	}
	var available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer, held int
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT available, reserved, safety_stock, blocked, qc_hold, damaged, channel_buffer, hold_qty
		  FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), sku, locationCode).
		Scan(&available, &reserved, &safetyStock, &blocked, &qcHold, &damaged, &channelBuffer, &held)
	if err != nil && err != sql.ErrNoRows {
		return OwnerAvailability{}, err
	}
	ats := computeATS(atsPolicyFor(schema), available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer, held)
	return ownerAvailabilityQ(db.DB, tenantID, schema, sku, locationCode, strings.TrimSpace(owner), ats)
}

// SetOrderOwner sets (or clears) the owner on an order that has not been
// released to the warehouse yet (59.2). After release the tasks carry it.
func SetOrderOwner(tenantID, orderID, owner, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'SalesOrder' AND id = $1 FOR UPDATE`, schema), orderID).Scan(&raw); err != nil {
		return fmt.Errorf("order %s not found", orderID)
	}
	var order map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &order); err != nil {
		return err
	}
	status, _ := order["order_status"].(string)
	switch status {
	case "", "Draft", "On Hold", "Reserved":
	default:
		return &ValidationError{Code: "INVENT-0493", Message: fmt.Sprintf("order %s is %s - its owner can only change before it is released to the warehouse", orderID, status)}
	}
	old, _ := order["owner_id"].(string)
	owner = strings.TrimSpace(owner)
	if old == owner {
		return nil
	}
	patch, _ := json.Marshal(map[string]interface{}{"owner_id": owner})
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrder' AND id = $2`, schema),
		patch, orderID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "ORDER_OWNER_SET", "SUCCESS", fmt.Sprintf("Order %s owner: %q -> %q", orderID, old, owner))
	return nil
}

// ReceiptOwner is the owner a goods receipt was received for (GRN.owner_id),
// which putaway inherits (59.4). "" when the receipt names none.
func ReceiptOwner(tenantID, grnID string) string {
	data, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil {
		return ""
	}
	owner, _ := data["owner_id"].(string)
	return strings.TrimSpace(owner)
}

// binDocOwnerQ reads a bin's location and whole-bin owner.
func binDocOwnerQ(q stockQuerier, schema, binCode string) (location, owner string, err error) {
	err = q.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(data->>'location', ''), COALESCE(data->>'owner_id', '') FROM %s.documents
		 WHERE doctype = 'Bin' AND deleted_at IS NULL AND data->>'bin_code' = $1 ORDER BY id LIMIT 1`, schema), binCode).Scan(&location, &owner)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return
}

// PutawayToBinForOwner puts stock away for an owner (59.4): a bin dedicated
// to another owner refuses it, a one-owner warehouse only takes its owner,
// and the units are recorded as the owner's slice unless the bin itself
// already names that owner. With no owner given, a mixed warehouse holding
// owner stock asks whose stock it is rather than leaving it unattributed.
func PutawayToBinForOwner(tenantID, binCode, sku string, qty int, owner, userID string) error {
	owner = strings.TrimSpace(owner)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	location, binOwner, err := binDocOwnerQ(db.DB, schema, binCode)
	if err != nil {
		return err
	}
	if owner == "" {
		if MixedOwnershipEnabled(tenantID) && binOwner == "" && location != "" {
			dedicated, err := warehouseOwnerQ(db.DB, schema, location)
			if err != nil {
				return err
			}
			owners, err := ownersAtLocationQ(db.DB, schema, location)
			if err != nil {
				return err
			}
			if dedicated == "" && len(owners) > 0 {
				return &ValidationError{Code: "INVENT-0496", Message: fmt.Sprintf(
					"%s holds stock of %s - say whose stock this is (the owner) so it is never picked for someone else", location, strings.Join(owners, ", "))}
			}
		}
		return PutawayToBin(tenantID, binCode, sku, qty, userID)
	}
	if binOwner != "" && binOwner != owner {
		return &ValidationError{Code: "INVENT-0495", Message: fmt.Sprintf("bin %s belongs to %s - put %s's stock in another bin", binCode, binOwner, owner)}
	}
	if location != "" {
		if err := AssertSingleOwnerForLocation(tenantID, location, owner); err != nil {
			return err
		}
	}
	var sliced bool
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT EXISTS (SELECT 1 FROM %s.bin_stock_owner WHERE bin_code = $1 AND sku = $2 AND condition = 'Good' AND qty > 0)`, schema),
		binCode, sku).Scan(&sliced); err != nil {
		return err
	}
	implicit, err := implicitBinOwnerQ(db.DB, schema, binCode, location)
	if err != nil {
		return err
	}
	if !sliced && implicit == owner {
		// The bin's (or warehouse's) own owner: attribution is implied.
		return PutawayToBin(tenantID, binCode, sku, qty, userID)
	}
	if !sliced && implicit != "" {
		// The bin's existing units are someone else's by implication - write
		// that down before the first explicit row would orphan them.
		if err := materializeImplicitSliceTx(db.DB, schema, binCode, sku, "Good", location, implicit); err != nil {
			return err
		}
	}
	if err := PutawayToBin(tenantID, binCode, sku, qty, userID); err != nil {
		return err
	}
	if err := RecordOwnerStock(tenantID, binCode, sku, owner, "Good", qty, userID); err != nil {
		LogSystemError(tenantID, "", "WARN", "PutawayToBinForOwner", fmt.Sprintf(
			"put away %d x %s in %s but could not record it as %s's: %v - see the owner-stock-exceptions report", qty, sku, binCode, owner, err), "")
		return err
	}
	return nil
}

// moveOwnerSliceTx keeps a move's owner attribution with the units (59.4):
// qty of (fromBin, sku, fromCond) going to (toBin, sku, toCond), inside the
// caller's transaction and before the caller changes bin_stock. owner names
// whose units move; "" means the bin's only owner, or its unsplit part.
// Returns the owner whose units moved ("" for unattributed stock).
func moveOwnerSliceTx(tx *sql.Tx, tenantID, schema, fromBin, toBin, sku, fromCond, toCond, locationCode string, binQty, qty int, owner string) (string, error) {
	owner = strings.TrimSpace(owner)
	fromImplicit, err := implicitBinOwnerQ(tx, schema, fromBin, locationCode)
	if err != nil {
		return "", err
	}
	toImplicit, err := implicitBinOwnerQ(tx, schema, toBin, locationCode)
	if err != nil {
		return "", err
	}
	_, toBinOwner, err := binDocOwnerQ(tx, schema, toBin)
	if err != nil {
		return "", err
	}
	slices, err := ownerSlicesTx(tx, schema, fromBin, sku, fromCond)
	if err != nil {
		return "", err
	}
	sliced := 0
	for _, n := range slices {
		sliced += n
	}

	moved := ""
	if len(slices) == 0 {
		// No split: the units are the bin's (or warehouse's) owner's, or nobody's.
		if owner != "" && fromImplicit != "" && owner != fromImplicit {
			return "", &ValidationError{Code: "INVENT-0495", Message: fmt.Sprintf("the stock in bin %s belongs to %s, not %s", fromBin, fromImplicit, owner)}
		}
		moved = fromImplicit
	} else {
		unsplit := binQty - sliced
		switch {
		case owner != "":
			if slices[owner] < qty {
				return "", &ValidationError{Code: "INVENT-0495", Message: fmt.Sprintf("bin %s holds only %d of %s for %s, not %d", fromBin, slices[owner], sku, owner, qty)}
			}
			moved = owner
		case unsplit >= qty:
			moved = ""
		case len(slices) == 1 && unsplit <= 0:
			for o := range slices {
				moved = o
			}
			if slices[moved] < qty {
				return "", &ValidationError{Code: "INVENT-0495", Message: fmt.Sprintf("bin %s holds only %d of %s for %s, not %d", fromBin, slices[moved], sku, moved, qty)}
			}
		default:
			owners := make([]string, 0, len(slices))
			for o := range slices {
				owners = append(owners, o)
			}
			return "", &ValidationError{Code: "INVENT-0496", Message: fmt.Sprintf("bin %s holds %s of several owners (%s) - say whose units move", fromBin, sku, strings.Join(owners, ", "))}
		}
	}
	if moved != "" && toBinOwner != "" && toBinOwner != moved {
		return "", &ValidationError{Code: "INVENT-0495", Message: fmt.Sprintf("bin %s belongs to %s - %s's stock cannot go there", toBin, toBinOwner, moved)}
	}
	if moved != "" && fromCond == "Good" {
		var reservedOwn int
		if err := tx.QueryRow(fmt.Sprintf(`
			SELECT COALESCE(SUM(ba.qty - ba.picked_qty), 0) FROM %s.bin_allocation ba
			  JOIN %s.documents ft ON ft.id = ba.task_id AND ft.doctype = 'FulfillmentTask' AND ft.status IN %s
			 WHERE ba.status = 'Open' AND ba.bin_code = $1 AND ba.sku = $2 AND ba.owner_id = $3`, schema, schema, liveTaskStatusesSQL),
			fromBin, sku, moved).Scan(&reservedOwn); err != nil {
			return "", err
		}
		have := binQty
		if len(slices) > 0 {
			have = slices[moved]
		}
		if have-reservedOwn < qty {
			return "", &ValidationError{Code: "GLOBAL-0002", SubFor: "Quantity", Message: fmt.Sprintf(
				"%d of %s's %d units of %s in bin %s are reserved for open pick tasks, so only %d can move", reservedOwn, moved, have, sku, fromBin, have-reservedOwn)}
		}
	}
	if moved == "" {
		return "", nil
	}
	if len(slices) > 0 {
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE %s.bin_stock_owner SET qty = qty - $1, updated_at = CURRENT_TIMESTAMP
			 WHERE bin_code = $2 AND sku = $3 AND condition = $4 AND owner_id = $5`, schema),
			qty, fromBin, sku, fromCond, moved); err != nil {
			return "", err
		}
	} else if toBin == fromBin {
		return moved, nil // a condition change inside an unsplit bin stays its implicit owner's
	}
	destSlices, err := ownerSlicesTx(tx, schema, toBin, sku, toCond)
	if err != nil {
		return "", err
	}
	if len(destSlices) == 0 {
		if toImplicit == moved {
			return moved, nil // the destination's own owner: attribution is implied
		}
		if err := materializeImplicitSliceTx(tx, schema, toBin, sku, toCond, locationCode, toImplicit); err != nil {
			return "", err
		}
	}
	if err := addOwnerSliceTx(tx, schema, toBin, sku, toCond, locationCode, moved, qty); err != nil {
		return "", err
	}
	return moved, nil
}

// implicitBinOwnerQ is who owns a bin's unsplit stock: the bin's own owner,
// else the warehouse's dedicated owner, else nobody.
func implicitBinOwnerQ(q stockQuerier, schema, binCode, locationCode string) (string, error) {
	loc, owner, err := binDocOwnerQ(q, schema, binCode)
	if err != nil || owner != "" {
		return owner, err
	}
	if loc == "" {
		loc = locationCode
	}
	return warehouseOwnerQ(q, schema, loc)
}

func ownerSlicesTx(q stockQuerier, schema, binCode, sku, condition string) (map[string]int, error) {
	rows, err := q.Query(fmt.Sprintf(`
		SELECT owner_id, qty FROM %s.bin_stock_owner
		 WHERE bin_code = $1 AND sku = $2 AND condition = $3 AND qty > 0`, schema), binCode, sku, condition)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var o string
		var n int
		if err := rows.Scan(&o, &n); err != nil {
			return nil, err
		}
		out[o] = n
	}
	return out, rows.Err()
}

func addOwnerSliceTx(q stockQuerier, schema, binCode, sku, condition, locationCode, owner string, qty int) error {
	_, err := q.Exec(fmt.Sprintf(`
		INSERT INTO %s.bin_stock_owner (bin_code, sku, condition, owner_id, location_code, qty)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (bin_code, sku, condition, owner_id) DO UPDATE SET
			qty = %s.bin_stock_owner.qty + EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema, schema),
		binCode, sku, condition, owner, locationCode, qty)
	return err
}

// materializeImplicitSliceTx writes a bin's implicitly owned stock down as
// an explicit slice, before another owner's units join it - otherwise the
// first explicit row would leave the existing units owned by nobody.
func materializeImplicitSliceTx(q stockQuerier, schema, binCode, sku, condition, locationCode, implicitOwner string) error {
	if implicitOwner == "" {
		return nil
	}
	var existing int
	if err := q.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM(qty), 0) FROM %s.bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = $3`, schema),
		binCode, sku, condition).Scan(&existing); err != nil {
		return err
	}
	if existing <= 0 {
		return nil
	}
	return addOwnerSliceTx(q, schema, binCode, sku, condition, locationCode, implicitOwner, existing)
}

// validateBinOwnerChange closes 59.4's gap: Bin.owner_id was an ordinary
// editable field, so changing it silently handed every unsplit unit in the
// bin to someone else. It may change only while the bin holds no stock that
// the old value attributes; a one-owner warehouse also refuses a second owner.
func validateBinOwnerChange(tenantID, docID string, payload map[string]interface{}) error {
	if _, present := payload["owner_id"]; !present {
		return nil
	}
	newOwner := strings.TrimSpace(strField(payload, "owner_id"))
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var oldOwner, binCode, location string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(data->>'owner_id', ''), COALESCE(data->>'bin_code', ''), COALESCE(data->>'location', '')
		  FROM %s.documents WHERE doctype = 'Bin' AND id = $1`, schema), docID).Scan(&oldOwner, &binCode, &location)
	if err == sql.ErrNoRows {
		oldOwner, binCode, location = "", strField(payload, "bin_code"), strField(payload, "location")
	} else if err != nil {
		return err
	}
	if newOwner != "" && location != "" && SingleOwnerEnforced(tenantID) {
		if dedicated, err := WarehouseOwnerOf(tenantID, location); err != nil {
			return err
		} else if dedicated != "" && dedicated != newOwner {
			return &ValidationError{Code: "INVENT-0491", SubFor: "owner_id", Message: fmt.Sprintf(
				"%s is dedicated to owner %s - a bin there cannot belong to %s in one-owner mode", location, dedicated, newOwner)}
		}
	}
	if oldOwner == newOwner || binCode == "" {
		return nil
	}
	var unsplit int
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(GREATEST(0, bs.qty - COALESCE(o.q, 0))), 0)
		  FROM %s.bin_stock bs
		  LEFT JOIN (SELECT sku, condition, SUM(qty) AS q FROM %s.bin_stock_owner WHERE bin_code = $1 GROUP BY sku, condition) o
		         ON o.sku = bs.sku AND o.condition = bs.condition
		 WHERE bs.bin_code = $1 AND bs.qty > 0`, schema, schema), binCode).Scan(&unsplit); err != nil {
		return err
	}
	if unsplit > 0 {
		return &ValidationError{Code: "INVENT-0494", SubFor: "owner_id", Message: fmt.Sprintf(
			"bin %s holds %d unit(s) that its owner field attributes to %s - move them out, or record whose they are (Owner Stock), before changing the bin's owner", binCode, unsplit, orDash(oldOwner))}
	}
	return nil
}

// OwnerStockException is one place 59.5's constraints do not hold.
type OwnerStockException struct {
	Kind         string `json:"kind"`
	LocationCode string `json:"location_code"`
	BinCode      string `json:"bin_code,omitempty"`
	Sku          string `json:"sku,omitempty"`
	Condition    string `json:"condition,omitempty"`
	BinQty       int    `json:"bin_qty"`
	OwnerQty     int    `json:"owner_qty"`
	Detail       string `json:"detail"`
}

// OwnerStockExceptions is the exception queue (59.5): owner slices that add
// up to more than their bin holds, bins owned by one owner holding another's
// slices, several owners in a one-owner warehouse, and live pick tasks with
// no owner at a location holding several owners' stock.
func OwnerStockExceptions(tenantID string) ([]OwnerStockException, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := []OwnerStockException{}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT o.location_code, o.bin_code, o.sku, o.condition, COALESCE(bs.qty, 0), o.q
		  FROM (SELECT bin_code, sku, condition, MAX(location_code) AS location_code, SUM(qty) AS q
		          FROM %s.bin_stock_owner WHERE qty > 0 GROUP BY bin_code, sku, condition) o
		  LEFT JOIN %s.bin_stock bs ON bs.bin_code = o.bin_code AND bs.sku = o.sku AND bs.condition = o.condition
		 WHERE o.q > COALESCE(bs.qty, 0)
		 ORDER BY o.location_code, o.bin_code, o.sku`, schema, schema))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e OwnerStockException
		if err := rows.Scan(&e.LocationCode, &e.BinCode, &e.Sku, &e.Condition, &e.BinQty, &e.OwnerQty); err != nil {
			rows.Close()
			return nil, err
		}
		e.Kind = "Owner slices exceed the bin"
		e.Detail = fmt.Sprintf("owners are credited with %d but the bin holds %d - correct the slices", e.OwnerQty, e.BinQty)
		out = append(out, e)
	}
	rows.Close()

	rows, err = db.DB.Query(fmt.Sprintf(`
		SELECT bso.location_code, bso.bin_code, bso.sku, bso.condition, bso.qty, b.owner_id, bso.owner_id
		  FROM %s.bin_stock_owner bso
		  JOIN (SELECT DISTINCT ON (data->>'bin_code') data->>'bin_code' AS bin_code, data->>'owner_id' AS owner_id
		          FROM %s.documents WHERE doctype = 'Bin' AND deleted_at IS NULL ORDER BY data->>'bin_code', id) b
		    ON b.bin_code = bso.bin_code
		 WHERE bso.qty > 0 AND COALESCE(b.owner_id, '') <> '' AND b.owner_id <> bso.owner_id
		 ORDER BY 1, 2, 3`, schema, schema))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e OwnerStockException
		var binOwner, sliceOwner string
		if err := rows.Scan(&e.LocationCode, &e.BinCode, &e.Sku, &e.Condition, &e.OwnerQty, &binOwner, &sliceOwner); err != nil {
			rows.Close()
			return nil, err
		}
		e.Kind = "Another owner's stock in an owned bin"
		e.Detail = fmt.Sprintf("bin belongs to %s but holds %d for %s", binOwner, e.OwnerQty, sliceOwner)
		out = append(out, e)
	}
	rows.Close()

	if SingleOwnerEnforced(tenantID) {
		mixed, err := ListMixedOwnerLocations(tenantID)
		if err != nil {
			return nil, err
		}
		for _, m := range mixed {
			out = append(out, OwnerStockException{Kind: "Several owners in a one-owner warehouse", LocationCode: m.LocationCode,
				OwnerQty: m.TotalQty, Detail: "owners " + strings.Join(m.Owners, ", ") + " - move one owner's stock out, or switch on mixed ownership"})
		}
	}

	rows, err = db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'location_code', '') FROM %s.documents
		 WHERE doctype = 'FulfillmentTask' AND status IN %s AND COALESCE(data->>'owner_id', '') = ''
		 ORDER BY created_at, id`, schema, liveTaskStatusesSQL))
	if err != nil {
		return nil, err
	}
	type task struct{ id, loc string }
	var tasks []task
	for rows.Next() {
		var tk task
		if err := rows.Scan(&tk.id, &tk.loc); err != nil {
			rows.Close()
			return nil, err
		}
		tasks = append(tasks, tk)
	}
	rows.Close()
	ownersByLoc := map[string][]string{}
	for _, tk := range tasks {
		owners, ok := ownersByLoc[tk.loc]
		if !ok {
			if owners, err = ownersAtLocationQ(db.DB, schema, tk.loc); err != nil {
				return nil, err
			}
			ownersByLoc[tk.loc] = owners
		}
		if len(owners) > 1 {
			out = append(out, OwnerStockException{Kind: "Pick task without an owner at a mixed location", LocationCode: tk.loc,
				Detail: fmt.Sprintf("task %s picks without an owner where %s hold stock - cancel and re-release its order with an owner", tk.id, strings.Join(owners, ", "))})
		}
	}
	return out, nil
}

// SetOwnerSliceQty corrects one owner's slice of a bin (the exception queue's
// resolution, 59.5) - to a counted figure, with a reason, never past what the
// bin holds.
func SetOwnerSliceQty(tenantID, binCode, sku, condition, owner string, qty int, reason, userID string) error {
	owner, reason = strings.TrimSpace(owner), strings.TrimSpace(reason)
	if condition == "" {
		condition = "Good"
	}
	if owner == "" || qty < 0 || !validBinConditions[condition] {
		return &ValidationError{Code: "GLOBAL-0002", Message: "an owner, a valid condition and a quantity of 0 or more are required"}
	}
	if reason == "" {
		return &ValidationError{Code: "GLOBAL-0001", SubFor: "reason", Message: "say why the owner's quantity is being corrected"}
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
	var location string
	var binQty int
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT location_code, qty FROM %s.bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = $3 FOR UPDATE`, schema),
		binCode, sku, condition).Scan(&location, &binQty)
	if err == sql.ErrNoRows {
		binQty = 0
		location, _, _ = binDocOwnerQ(tx, schema, binCode)
	} else if err != nil {
		return err
	}
	var others, old int
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(qty) FILTER (WHERE owner_id <> $4), 0), COALESCE(SUM(qty) FILTER (WHERE owner_id = $4), 0)
		  FROM %s.bin_stock_owner WHERE bin_code = $1 AND sku = $2 AND condition = $3`, schema),
		binCode, sku, condition, owner).Scan(&others, &old); err != nil {
		return err
	}
	if others+qty > binQty {
		return &ValidationError{Code: "INVENT-0494", Message: fmt.Sprintf(
			"bin %s holds %d of %s (%s); other owners already hold %d, so %s can hold at most %d", binCode, binQty, sku, condition, others, owner, binQty-others)}
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.bin_stock_owner (bin_code, sku, condition, owner_id, location_code, qty)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (bin_code, sku, condition, owner_id) DO UPDATE SET qty = EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema),
		binCode, sku, condition, owner, location, qty); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
		ItemID: sku, WarehouseID: location, Qty: 0, VoucherType: "OwnerStockAssign",
		VoucherID: fmt.Sprintf("CORRECT-%s-%d", binCode, time.Now().UnixNano()), UserID: userID,
		ToLocationID: binCode, OwnerID: owner,
	}); lerr != nil {
		LogSystemError(tenantID, "", "WARN", "SetOwnerSliceQty", fmt.Sprintf("stock ledger write failed for %s: %v", sku, lerr), "")
	}
	LogAuditEvent(tenantID, userID, "WMS_OWNER_STOCK_CORRECTED", "SUCCESS",
		fmt.Sprintf("Owner %s's %s %s in bin %s: %d -> %d (%s)", owner, condition, sku, binCode, old, qty, reason))
	return nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "owner-stock-exceptions", Label: "Owner Stock Exceptions (3PL)", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "kind", Label: "Exception"},
			{Key: "location_code", Label: "Location"},
			{Key: "bin_code", Label: "Bin"},
			{Key: "sku", Label: "Item"},
			{Key: "condition", Label: "Condition"},
			{Key: "bin_qty", Label: "Bin Qty"},
			{Key: "owner_qty", Label: "Owner Qty"},
			{Key: "detail", Label: "What to do"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := OwnerStockExceptions(tenantID)
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}
