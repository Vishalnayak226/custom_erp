package engines

import (
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
)

// Stage 42.4.7 - Deconsolidation: split a received HU (LPN) across multiple
// outbound-destination LPNs at the same bin, before packing. Scoped to
// re-tagging within 26.5.4's existing bin_stock_lpn ledger (AssignToLPN/
// GetLPNContents, engines/wms_putaway_ext.go) rather than a new table - a
// physical bin-to-bin move is already ExecuteBinReplenishment/
// CrossDockPutaway's job, and deconsolidation is specifically about one
// container's contents needing to serve more than one outbound destination,
// not about the stock moving anywhere physically. Logs a Move WarehouseTask
// through the existing LogCompletedWarehouseTask choke point so the split is
// visible in the cockpit/task history like every other floor action.

// DeconsolidationSplit is one destination LPN's share of the source LPN's
// qty at one bin/sku/condition.
type DeconsolidationSplit struct {
	DestLPN string
	Qty     int
}

// DeconsolidateLPN (42.4.7) moves qty out of sourceLPN's assignment at
// (binCode, sku, condition) into one or more destination LPNs, all inside
// one transaction - the source's total assigned qty at that bin/sku/
// condition must cover the sum of every split, or the whole call is
// refused, leaving bin_stock_lpn exactly as it was.
func DeconsolidateLPN(tenantID, sourceLPN, binCode, sku, condition string, splits []DeconsolidationSplit, userID string) error {
	if sourceLPN == "" || binCode == "" || sku == "" {
		return errors.New("source_lpn, bin_code and sku are required")
	}
	if len(splits) == 0 {
		return errors.New("at least one destination split is required")
	}
	if condition == "" {
		condition = "Good"
	}
	total := 0
	for _, s := range splits {
		if s.DestLPN == "" || s.Qty <= 0 {
			return errors.New("every split needs a non-blank dest_lpn and a positive qty")
		}
		if s.DestLPN == sourceLPN {
			return errors.New("a destination LPN cannot be the same as the source LPN")
		}
		total += s.Qty
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

	var have int
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT qty FROM %s.bin_stock_lpn WHERE lpn_code = $1 AND bin_code = $2 AND sku = $3 AND condition = $4 FOR UPDATE`, schema),
		sourceLPN, binCode, sku, condition).Scan(&have)
	if err == sql.ErrNoRows {
		return fmt.Errorf("LPN %s has no %s-condition assignment of %s in bin %s", sourceLPN, condition, sku, binCode)
	} else if err != nil {
		return err
	}
	if total > have {
		return fmt.Errorf("splits total %d exceeds LPN %s's assigned qty of %d", total, sourceLPN, have)
	}

	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.bin_stock_lpn SET qty = qty - $1, updated_at = CURRENT_TIMESTAMP WHERE lpn_code = $2 AND bin_code = $3 AND sku = $4 AND condition = $5`, schema),
		total, sourceLPN, binCode, sku, condition); err != nil {
		return err
	}
	for _, s := range splits {
		if _, err := tx.Exec(fmt.Sprintf(`
			INSERT INTO %s.bin_stock_lpn (lpn_code, bin_code, sku, condition, qty)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (lpn_code, bin_code, sku, condition) DO UPDATE SET
				qty = %s.bin_stock_lpn.qty + EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema, schema),
			s.DestLPN, binCode, sku, condition, s.Qty); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "WMS_LPN_DECONSOLIDATE", "SUCCESS",
		fmt.Sprintf("Split %d x %s from LPN %s into %d destination LPN(s) at bin %s", total, sku, sourceLPN, len(splits), binCode))
	var locationCode string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT location_code FROM %s.bin_stock WHERE bin_code = $1 LIMIT 1`, schema), binCode).Scan(&locationCode)
	LogCompletedWarehouseTask(tenantID, NewWarehouseTask{
		TaskType: "Move", LocationCode: locationCode, FromBin: binCode, ToBin: binCode, Item: sku, Qty: float64(total),
		Notes: fmt.Sprintf("Deconsolidated LPN %s into %d destination(s)", sourceLPN, len(splits)),
	}, userID)
	return nil
}
