package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/lib/pq"
)

// Stage 59.1 - bin stock that tracks reality (foundation for mixed-owner 3PL
// isolation, Stage 59).
//
// Before this, bin_stock only ever grew: putaway, cross-dock and VAS added to
// it, and neither a pick scan nor dispatch took anything out - dispatch only
// reduced the location-level inventory_availability. The pick list was
// recomputed from those inflated numbers on every call and never remembered,
// so two open tasks could be sent to the same last unit, and a shipped unit
// stayed on its bin forever. Owner isolation built on top of that would hold
// on paper only.
//
// The model, deliberately the conventional one:
//
//   - reserve: when a task's pick list is produced (or the task is picked or
//     dispatched without one), each SKU's unpicked demand is reserved against
//     specific bins/lots as bin_allocation rows. Allocation skips stock already
//     reserved for another live task, so a bin is promised once.
//   - consume: each picked unit, and each unit dispatched without having been
//     scanned, is taken out of the bin it was reserved on - bin_stock, plus the
//     lot (bin_stock_batch) and owner (bin_stock_owner) breakdowns.
//   - release: a short pick, a rejection, or a dispatch that leaves reserved
//     units unpicked hands them back.
//
// Locations that do not use bins are unaffected: with no bin_stock there is
// nothing to reserve, and every pick and dispatch proceeds exactly as before.

// stockQuerier is the slice of *sql.DB / *sql.Tx allocation needs, so the same
// query runs standalone (a pick-list preview) or inside the transaction that
// then writes the reservation.
type stockQuerier interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

// liveTaskStatuses are the FulfillmentTask states whose Open reservations
// still hold stock. A reservation on a task that is gone, Dispatched or
// Rejected holds nothing, even if a crash or a deleted test fixture left the
// row Open - the stock must never be stranded by a row nobody will release.
const liveTaskStatusesSQL = `('Pending', 'Picking', 'Packed')`

// openBinReservationsSQL is the "already promised" subquery both allocation
// strategies join against: units still reserved (qty - picked_qty) on Open
// rows of live tasks, for $1 = sku and $2 = location. byBatch groups per lot
// as well, for the FEFO query that allocates lots rather than whole bins.
func openBinReservationsSQL(schema string, byBatch bool) string {
	cols := "ba.bin_code"
	if byBatch {
		cols = "ba.bin_code, ba.batch_no"
	}
	return fmt.Sprintf(`
		SELECT %s, SUM(ba.qty - ba.picked_qty) AS reserved
		  FROM %s.bin_allocation ba
		  JOIN %s.documents ft ON ft.id = ba.task_id AND ft.doctype = 'FulfillmentTask'
		                      AND ft.status IN %s
		 WHERE ba.status = 'Open' AND ba.sku = $1 AND ba.location_code = $2
		 GROUP BY %s`, cols, schema, schema, liveTaskStatusesSQL, cols)
}

// BinReservedQty reports how many units of sku in binCode are reserved for
// live tasks - what a shelf move (MoveBinStock), a condition change or a
// count adjustment must leave in place. batchNo narrows it to one lot.
func BinReservedQty(q stockQuerier, schema, binCode, sku, batchNo string) (int, error) {
	var reserved int
	err := q.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(ba.qty - ba.picked_qty), 0)
		  FROM %s.bin_allocation ba
		  JOIN %s.documents ft ON ft.id = ba.task_id AND ft.doctype = 'FulfillmentTask'
		                      AND ft.status IN %s
		 WHERE ba.status = 'Open' AND ba.bin_code = $1 AND ba.sku = $2
		   AND ($3 = '' OR ba.batch_no = $3)`, schema, schema, liveTaskStatusesSQL),
		binCode, sku, batchNo).Scan(&reserved)
	return reserved, err
}

// taskSKUNeed is one SKU's position on a FulfillmentTask, summed across lines
// (a task may list the same SKU twice; reservations are per task and SKU).
type taskSKUNeed struct {
	SKU    string
	Demand int // qty - short_qty: what will actually leave the building
	Picked int // picked_qty: units already scanned
}

// taskSKUNeeds reads a FulfillmentTask's items, in SKU order - the order every
// caller takes per-SKU locks in, so two tasks sharing SKUs cannot deadlock.
func taskSKUNeeds(data map[string]interface{}) []taskSKUNeed {
	bySKU := map[string]*taskSKUNeed{}
	raw, _ := data["items"].([]interface{})
	for _, ri := range raw {
		m, ok := ri.(map[string]interface{})
		if !ok {
			continue
		}
		sku, _ := m["sku"].(string)
		if sku == "" {
			continue
		}
		n := bySKU[sku]
		if n == nil {
			n = &taskSKUNeed{SKU: sku}
			bySKU[sku] = n
		}
		n.Demand += int(numFromInterface(m["qty"]) - numFromInterface(m["short_qty"]))
		n.Picked += int(numFromInterface(m["picked_qty"]))
	}
	out := make([]taskSKUNeed, 0, len(bySKU))
	for _, n := range bySKU {
		out = append(out, *n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SKU < out[j].SKU })
	return out
}

// lockSKUAtLocationTx serialises reservation of one SKU at one location for
// the rest of the transaction. Allocation reads free stock and then inserts a
// reservation; without this, two tasks reading at once would both see the
// same free unit.
func lockSKUAtLocationTx(tx *sql.Tx, schema, locationCode, sku string) error {
	_, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`,
		"bin_allocation|"+schema+"|"+locationCode+"|"+sku)
	return err
}

// reserveTaskSKUTx tops up a task's reservations for one SKU until they cover
// its unpicked demand. Returns the units it could not reserve (the shortfall
// against binned stock), which is a reportable fact, not an error - same
// judgement AllocateFromStock makes.
func reserveTaskSKUTx(tx *sql.Tx, tenantID, schema, taskID, locationCode, owner string, n taskSKUNeed) (int, error) {
	want := n.Demand - n.Picked
	if want <= 0 || locationCode == "" {
		return 0, nil
	}
	if err := lockSKUAtLocationTx(tx, schema, locationCode, n.SKU); err != nil {
		return 0, err
	}
	var have int
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(qty - picked_qty), 0) FROM %s.bin_allocation
		 WHERE task_id = $1 AND sku = $2 AND status = 'Open'`, schema), taskID, n.SKU).Scan(&have); err != nil {
		return 0, err
	}
	gap := want - have
	if gap <= 0 {
		return 0, nil
	}
	// Stage 59.3: a task working for an owner is offered only that owner's
	// units, and its reservation rows say so.
	candidates, shortfall, err := allocateFromStockForOwnerQ(tx, tenantID, n.SKU, locationCode, gap, owner)
	if err != nil {
		return 0, err
	}
	for _, c := range candidates {
		if c.Qty <= 0 {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf(`
			INSERT INTO %s.bin_allocation (task_id, sku, location_code, bin_code, batch_no, owner_id, qty)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, schema),
			taskID, n.SKU, locationCode, c.BinCode, c.BatchNo, owner, c.Qty); err != nil {
			return 0, err
		}
	}
	return shortfall, nil
}

// ReserveTaskBinStockTx tops up every SKU of a task the caller has already
// locked (SELECT ... FOR UPDATE on its documents row). A task that is no
// longer being picked reserves nothing.
func ReserveTaskBinStockTx(tx *sql.Tx, tenantID, schema, taskID, status string, data map[string]interface{}) error {
	if status != "Pending" && status != "Picking" {
		return nil
	}
	location, _ := data["location_code"].(string)
	owner, _ := data["owner_id"].(string)
	for _, n := range taskSKUNeeds(data) {
		if _, err := reserveTaskSKUTx(tx, tenantID, schema, taskID, location, owner, n); err != nil {
			return err
		}
	}
	return nil
}

// lockFulfillmentTaskTx reads and row-locks a FulfillmentTask inside tx.
func lockFulfillmentTaskTx(tx *sql.Tx, schema, taskID string) (string, map[string]interface{}, error) {
	var status string
	var raw []byte
	err := tx.QueryRow(fmt.Sprintf(`
		SELECT status, data FROM %s.documents
		 WHERE doctype = 'FulfillmentTask' AND id = $1 FOR UPDATE`, schema), taskID).Scan(&status, &raw)
	if err == sql.ErrNoRows {
		return "", nil, fmt.Errorf("fulfillment task %s not found", taskID)
	} else if err != nil {
		return "", nil, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return "", nil, err
	}
	return status, data, nil
}

// ReserveTaskBinStock locks one task and tops up its reservations in its own
// transaction - what producing a pick list does.
func ReserveTaskBinStock(tenantID, taskID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	status, data, err := lockFulfillmentTaskTx(tx, schema, taskID)
	if err != nil {
		return err
	}
	if err := ReserveTaskBinStockTx(tx, tenantID, schema, taskID, status, data); err != nil {
		return err
	}
	return tx.Commit()
}

// ConsumeTaskBinStockTx takes n units of sku out of the bins reserved for a
// task the caller has locked, topping the reservation up first if it does not
// cover them (a task dispatched or scanned without a pick list ever being
// produced). Returns how many units came out of a bin; the rest had no binned
// stock to come from - a location without bins, or bins already short - and
// are left to the location-level posting the caller makes anyway.
//
// A bin that holds less than its reservation says (stock moved or adjusted
// outside this flow) is floored at zero and logged rather than failing the
// pick: the unit is physically in the picker's hand, and refusing to record
// it would only add a second discrepancy to the first.
func ConsumeTaskBinStockTx(tx *sql.Tx, tenantID, schema, taskID, locationCode, sku string, n int) (int, error) {
	if n <= 0 {
		return 0, nil
	}
	if err := lockSKUAtLocationTx(tx, schema, locationCode, sku); err != nil {
		return 0, err
	}
	var have int
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(qty - picked_qty), 0) FROM %s.bin_allocation
		 WHERE task_id = $1 AND sku = $2 AND status = 'Open'`, schema), taskID, sku).Scan(&have); err != nil {
		return 0, err
	}
	if have < n {
		owner, err := taskOwnerQ(tx, schema, taskID)
		if err != nil {
			return 0, err
		}
		candidates, _, err := allocateFromStockForOwnerQ(tx, tenantID, sku, locationCode, n-have, owner)
		if err != nil {
			return 0, err
		}
		for _, c := range candidates {
			if _, err := tx.Exec(fmt.Sprintf(`
				INSERT INTO %s.bin_allocation (task_id, sku, location_code, bin_code, batch_no, owner_id, qty)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`, schema),
				taskID, sku, locationCode, c.BinCode, c.BatchNo, owner, c.Qty); err != nil {
				return 0, err
			}
		}
	}

	type slice struct {
		id                int64
		bin, batch, owner string
		remaining         int
	}
	rows, err := tx.Query(fmt.Sprintf(`
		SELECT id, bin_code, batch_no, owner_id, qty - picked_qty FROM %s.bin_allocation
		 WHERE task_id = $1 AND sku = $2 AND status = 'Open'
		 ORDER BY id FOR UPDATE`, schema), taskID, sku)
	if err != nil {
		return 0, err
	}
	var slices []slice
	for rows.Next() {
		var s slice
		if err := rows.Scan(&s.id, &s.bin, &s.batch, &s.owner, &s.remaining); err != nil {
			rows.Close()
			return 0, err
		}
		slices = append(slices, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	consumed := 0
	for _, s := range slices {
		if consumed >= n {
			break
		}
		take := s.remaining
		if take > n-consumed {
			take = n - consumed
		}
		if take <= 0 {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE %s.bin_allocation
			   SET picked_qty = picked_qty + $1,
			       status = CASE WHEN picked_qty + $1 >= qty THEN 'Picked' ELSE status END,
			       updated_at = CURRENT_TIMESTAMP
			 WHERE id = $2`, schema), take, s.id); err != nil {
			return 0, err
		}
		if err := takeFromBinTx(tx, tenantID, schema, s.bin, sku, s.batch, s.owner, take, taskID); err != nil {
			return 0, err
		}
		consumed += take
	}
	return consumed, nil
}

// takeFromBinTx removes qty Good-condition units from one bin and from the
// lot and owner breakdowns the reservation named. bin_stock.updated_at is
// left alone on purpose: FIFO/LIFO allocation orders by it as the age of the
// stock in the bin, and a pick does not make what is left any younger.
func takeFromBinTx(tx *sql.Tx, tenantID, schema, binCode, sku, batchNo, ownerID string, qty int, taskID string) error {
	var binQty int
	err := tx.QueryRow(fmt.Sprintf(`
		SELECT qty FROM %s.bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = 'Good' FOR UPDATE`, schema),
		binCode, sku).Scan(&binQty)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if binQty < qty {
		LogSystemError(tenantID, "", "WARN", "ConsumeTaskBinStock", fmt.Sprintf(
			"bin %s held %d of %s but task %s's reservation took %d - the bin was changed outside the pick flow; floored at 0, see the bin-stock-reconciliation report",
			binCode, binQty, sku, taskID, qty), "")
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.bin_stock SET qty = GREATEST(0, qty - $1)
		 WHERE bin_code = $2 AND sku = $3 AND condition = 'Good'`, schema), qty, binCode, sku); err != nil {
		return err
	}
	if batchNo != "" {
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE %s.bin_stock_batch SET qty = GREATEST(0, qty - $1), updated_at = CURRENT_TIMESTAMP
			 WHERE bin_code = $2 AND sku = $3 AND condition = 'Good' AND batch_no = $4`, schema),
			qty, binCode, sku, batchNo); err != nil {
			return err
		}
	}
	if ownerID != "" {
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE %s.bin_stock_owner SET qty = GREATEST(0, qty - $1), updated_at = CURRENT_TIMESTAMP
			 WHERE bin_code = $2 AND sku = $3 AND condition = 'Good' AND owner_id = $4`, schema),
			qty, binCode, sku, ownerID); err != nil {
			return err
		}
	}
	return nil
}

// ReleaseTaskBinStockTx hands a task's still-reserved units back to their
// bins: every Open row for the task (or for one SKU of it) becomes Released.
// picked_qty is kept, so the row still records what did leave each bin.
func ReleaseTaskBinStockTx(tx stockQuerier, schema, taskID, sku string) error {
	_, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.bin_allocation SET status = 'Released', updated_at = CURRENT_TIMESTAMP
		 WHERE task_id = $1 AND status = 'Open' AND ($2 = '' OR sku = $2)`, schema), taskID, sku)
	return err
}

// taskAllocationLine is one Open reservation, shaped for a pick list.
type taskAllocationLine struct {
	TaskID, SKU, BinCode, BatchNo, OwnerID string
	Zone, Aisle, Rack, ExpiryDate          string
	Qty                                    int
}

// openTaskAllocations lists the still-to-pick reservations of the given tasks.
func openTaskAllocations(schema string, taskIDs []string) ([]taskAllocationLine, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT ba.task_id, ba.sku, ba.bin_code, ba.batch_no, ba.owner_id, ba.qty - ba.picked_qty,
		       COALESCE(b.data->>'zone', ''), COALESCE(b.data->>'aisle', ''), COALESCE(b.data->>'rack', ''),
		       COALESCE(bt.data->>'expiry_date', '')
		  FROM %s.bin_allocation ba
		  LEFT JOIN LATERAL (SELECT data FROM %s.documents
		                      WHERE doctype = 'Bin' AND data->>'bin_code' = ba.bin_code
		                      ORDER BY id LIMIT 1) b ON TRUE
		  LEFT JOIN LATERAL (SELECT data FROM %s.documents
		                      WHERE doctype = 'Batch' AND data->>'item' = ba.sku AND data->>'batch_no' = ba.batch_no
		                      ORDER BY id LIMIT 1) bt ON ba.batch_no <> ''
		 WHERE ba.task_id = ANY($1) AND ba.status = 'Open' AND ba.qty > ba.picked_qty
		 ORDER BY ba.id`, schema, schema, schema), pq.Array(taskIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []taskAllocationLine
	for rows.Next() {
		var l taskAllocationLine
		if err := rows.Scan(&l.TaskID, &l.SKU, &l.BinCode, &l.BatchNo, &l.OwnerID, &l.Qty,
			&l.Zone, &l.Aisle, &l.Rack, &l.ExpiryDate); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// BinStockReconciliationRow compares one SKU's stock at a location as the
// bins tell it against the location-level on_hand.
type BinStockReconciliationRow struct {
	SKU          string `json:"sku"`
	LocationCode string `json:"location_code"`
	OnHand       int    `json:"on_hand"`
	Binned       int    `json:"binned"`
	InPick       int    `json:"in_pick"`
	Unbinned     int    `json:"unbinned"`
	Status       string `json:"status"`
}

// BinStockReconciliation (59.1) lists every SKU/location that has bins,
// with on_hand split into what the bins hold, what has been picked off a bin
// but not yet dispatched (in_pick), and the remainder (unbinned). A NEGATIVE
// remainder means the bins claim more than the location holds - which is what
// every bin looks like that shipped stock before Stage 59.1, since nothing
// took shipped units off a bin. That is reported, not corrected: how far each
// bin is off depends on history this code cannot see, and a count settles it.
func BinStockReconciliation(tenantID, sku, locationCode string, mismatchesOnly bool) ([]BinStockReconciliationRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH binned AS (
			SELECT sku, location_code, SUM(qty) AS qty FROM %s.bin_stock GROUP BY sku, location_code
		), in_pick AS (
			SELECT ba.sku, ba.location_code, SUM(ba.picked_qty) AS qty
			  FROM %s.bin_allocation ba
			  JOIN %s.documents ft ON ft.id = ba.task_id AND ft.doctype = 'FulfillmentTask'
			                      AND ft.status IN %s
			 GROUP BY ba.sku, ba.location_code
		)
		SELECT b.sku, b.location_code, COALESCE(ia.on_hand, 0), b.qty, COALESCE(p.qty, 0)
		  FROM binned b
		  LEFT JOIN %s.inventory_availability ia ON ia.sku = b.sku AND ia.location_code = b.location_code
		  LEFT JOIN in_pick p ON p.sku = b.sku AND p.location_code = b.location_code
		 WHERE ($1 = '' OR b.sku = $1) AND ($2 = '' OR b.location_code = $2)
		 ORDER BY b.location_code, b.sku`, schema, schema, schema, liveTaskStatusesSQL, schema), sku, locationCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BinStockReconciliationRow{}
	for rows.Next() {
		var r BinStockReconciliationRow
		if err := rows.Scan(&r.SKU, &r.LocationCode, &r.OnHand, &r.Binned, &r.InPick); err != nil {
			return nil, err
		}
		r.Unbinned = r.OnHand - r.Binned - r.InPick
		switch {
		case r.Unbinned < 0:
			r.Status = "Bins over-state stock"
		case r.Unbinned > 0:
			r.Status = "Stock not in any bin"
		default:
			r.Status = "Matches"
		}
		if mismatchesOnly && r.Unbinned == 0 {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "bin-stock-reconciliation", Label: "Bin Stock Reconciliation", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "location_code", Label: "Location"},
			{Key: "sku", Label: "Item"},
			{Key: "on_hand", Label: "On Hand"},
			{Key: "binned", Label: "In Bins"},
			{Key: "in_pick", Label: "Picked, Not Dispatched"},
			{Key: "unbinned", Label: "Difference"},
			{Key: "status", Label: "Status"},
		},
		Params: []ReportParam{
			{Key: "sku", Label: "Item Code (optional)", Type: "text"},
			{Key: "location", Label: "Location (optional)", Type: "text"},
			{Key: "mismatches_only", Label: "Show", Type: "select", Options: "Mismatches only,All rows"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			only := params["mismatches_only"] != "All rows"
			rows, err := BinStockReconciliation(tenantID, params["sku"], params["location"], only)
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}
