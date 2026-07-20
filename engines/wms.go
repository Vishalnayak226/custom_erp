package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

	var location, binStatus string
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT data->>'location', status FROM %s.documents WHERE doctype = 'Bin' AND data->>'bin_code' = $1`, schema),
		binCode).Scan(&location, &binStatus)
	if err == sql.ErrNoRows {
		return fmt.Errorf("bin %s not found", binCode)
	} else if err != nil {
		return err
	}
	if binStatus != "Active" {
		return fmt.Errorf("bin %s is not Active", binCode)
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
	LogAuditEvent(tenantID, userID, "WMS_PUTAWAY", "SUCCESS", fmt.Sprintf("Put away %d x %s into bin %s", qty, sku, binCode))
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
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(dataStr), &task); err != nil {
		return nil, err
	}

	var lines []PickListLine
	for _, item := range task.Items {
		needed := item.Qty
		rows, err := db.DB.Query(fmt.Sprintf(`
			SELECT bs.bin_code, bs.qty, COALESCE(b.data->>'zone', ''), COALESCE(b.data->>'aisle', ''), COALESCE(b.data->>'rack', '')
			FROM %s.bin_stock bs
			LEFT JOIN %s.documents b ON b.doctype = 'Bin' AND b.data->>'bin_code' = bs.bin_code
			WHERE bs.sku = $1 AND bs.location_code = $2 AND bs.condition = 'Good' AND bs.qty > 0
			ORDER BY COALESCE(b.data->>'zone', ''), COALESCE(b.data->>'aisle', ''), COALESCE(b.data->>'rack', ''), bs.bin_code`,
			schema, schema), item.Sku, task.LocationCode)
		if err != nil {
			return nil, err
		}
		for rows.Next() && needed > 0 {
			var binCode, zone, aisle, rack string
			var available int
			if err := rows.Scan(&binCode, &available, &zone, &aisle, &rack); err != nil {
				rows.Close()
				return nil, err
			}
			pick := available
			if pick > needed {
				pick = needed
			}
			needed -= pick
			lines = append(lines, PickListLine{Sku: item.Sku, BinCode: binCode, Zone: zone, Aisle: aisle, Rack: rack, PickQty: pick})
		}
		rows.Close()
		if needed > 0 {
			lines = append(lines, PickListLine{Sku: item.Sku, BinCode: "", PickQty: 0, Shortfall: needed})
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
		  AND status NOT IN ('Pending Approval', 'Approved', 'Rejected', 'Posted')`, schema), countSession)
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
func PostCycleCountAdjustment(tenantID, lineID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'CycleCountLine' AND id = $1`, schema), lineID).
		Scan(&dataStr); err != nil {
		return fmt.Errorf("cycle count line not found: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	sku, _ := data["sku"].(string)
	location, _ := data["location"].(string)
	variance := int(numFromInterface(data["variance"]))

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
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Posted', updated_at = CURRENT_TIMESTAMP WHERE id = $2`, schema),
		marshaled, lineID)
	return err
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
