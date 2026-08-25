package engines

import (
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Stage 42.5.5 - multi-owner stock segregation, the last item of Phase 42.5.
// 42.D2 ("is 3PL/multi-owner a real target?") was resolved 2026-08-24: build
// it, the same call already made for 42.6.6-42.6.9.
//
// bin_stock_owner (migrations_stage42_5_5_owner_segregation.sql) is a
// breakdown of bin_stock, the same relationship bin_stock_batch already has -
// see that migration's comment for why this is a new table rather than an
// owner_id column on bin_stock itself. The invariant this file enforces,
// identical in shape to RecordBatchPutaway/ConsumeBatchStock:
//     SUM(bin_stock_owner.qty) for (bin, sku, condition)
//         <= bin_stock.qty for that same (bin, sku, condition)
//
// Bin.owner_id (Stage 26.5) is deliberately left alone and stays meaningful:
// ownerStockQty below treats it as the fallback attribution for any (bin,
// sku, condition) slice that has no explicit bin_stock_owner row, so a
// single-owner warehouse - or a 3PL tenant that hasn't segregated a given
// bin/SKU down to the unit level yet - keeps billing exactly as it did
// before this file existed. Only a bin/SKU an operator has actually split
// with RecordOwnerStock stops using the whole-bin approximation.
//
// Deliberately out of scope: allocation/picking does not filter by owner.
// SalesOrder has no owner_id anywhere in this tree, and giving an order a
// real owner (so picking can refuse another client's stock) is a materially
// larger feature than "26.5.15's billing currently approximates around
// owner_id's absence" - the exact sentence the plan scopes 42.5.5 to. This
// closes the billing gap; order-level owner attribution is a future item if
// a real 3PL pilot needs it.

// OwnerStockRow is one bin's owner-segregated slice of a SKU - the row shape
// GetOwnerStock and the owner-stock-inquiry report return.
type OwnerStockRow struct {
	BinCode      string `json:"bin_code"`
	Sku          string `json:"sku"`
	OwnerID      string `json:"owner_id"`
	Condition    string `json:"condition"`
	LocationCode string `json:"location_code"`
	Qty          int    `json:"qty"`
}

// RecordOwnerStock assigns qty of a bin's sku/condition stock to ownerID. It
// is the owner analogue of RecordBatchPutaway and AssignToLPN, and enforces
// the identical invariant against the identical parent row: the sum of a
// bin's owner-assigned qty can never exceed what the bin actually holds.
func RecordOwnerStock(tenantID, binCode, sku, ownerID, condition string, qty int, userID string) error {
	if qty <= 0 {
		return errors.New("owner stock qty must be positive")
	}
	if strings.TrimSpace(ownerID) == "" {
		return errors.New("owner_id is required")
	}
	if condition == "" {
		condition = "Good"
	}
	if !validBinConditions[condition] {
		return fmt.Errorf("condition must be one of Good, Damaged, QC-Hold, RTV")
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
	var binQty int
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT location_code, qty FROM %s.bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = $3 FOR UPDATE`, schema),
		binCode, sku, condition).Scan(&locationCode, &binQty)
	if err == sql.ErrNoRows {
		return fmt.Errorf("no %s-condition stock for SKU %s in bin %s - put the stock away first", condition, sku, binCode)
	} else if err != nil {
		return err
	}
	var alreadyAssigned int
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM(qty), 0) FROM %s.bin_stock_owner WHERE bin_code = $1 AND sku = $2 AND condition = $3`, schema),
		binCode, sku, condition).Scan(&alreadyAssigned); err != nil {
		return err
	}
	if alreadyAssigned+qty > binQty {
		return fmt.Errorf("owner assignment exceeds the bin's own qty (bin qty=%d, already assigned to owners=%d, requested=%d)",
			binQty, alreadyAssigned, qty)
	}

	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.bin_stock_owner (bin_code, sku, condition, owner_id, location_code, qty)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (bin_code, sku, condition, owner_id) DO UPDATE SET
			qty = %s.bin_stock_owner.qty + EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema, schema),
		binCode, sku, condition, ownerID, locationCode, qty); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Qty 0 for the same reason RecordBatchPutaway writes a 0: no stock
	// entered or left the building, the movement being recorded is an
	// ownership identity being attached to quantity that was already there.
	if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
		ItemID: sku, WarehouseID: locationCode, Qty: 0,
		VoucherType: "OwnerStockAssign", VoucherID: ownerID, UserID: userID, ToLocationID: binCode,
		OwnerID: ownerID,
	}); lerr != nil {
		LogSystemError(tenantID, "", "WARN", "RecordOwnerStock", fmt.Sprintf("stock ledger write failed for %s: %v", sku, lerr), "")
	}
	LogAuditEvent(tenantID, userID, "WMS_OWNER_STOCK_ASSIGN", "SUCCESS",
		fmt.Sprintf("Assigned %d x %s in bin %s to owner %s", qty, sku, binCode, ownerID))
	return nil
}

// ConsumeOwnerStock removes qty of an owner's stock from a bin against a
// consuming voucher. Deliberately scoped to the owner sub-ledger exactly the
// way ConsumeBatchStock is scoped to the batch one: it does NOT touch
// bin_stock or inventory_availability, because the caller's own flow already
// owns that posting - double-counting it here would make this breakdown a
// second source of truth, the one thing it must never become.
func ConsumeOwnerStock(tenantID, binCode, sku, ownerID, condition string, qty int, voucherType, voucherID, userID string) error {
	if qty <= 0 {
		return errors.New("consume qty must be positive")
	}
	if strings.TrimSpace(ownerID) == "" {
		return errors.New("owner_id is required")
	}
	if condition == "" {
		condition = "Good"
	}
	if voucherType == "" {
		voucherType = "OwnerStockConsume"
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
	err = tx.QueryRow(fmt.Sprintf(`
		SELECT location_code, qty FROM %s.bin_stock_owner
		WHERE bin_code = $1 AND sku = $2 AND condition = $3 AND owner_id = $4 FOR UPDATE`, schema),
		binCode, sku, condition, ownerID).Scan(&locationCode, &have)
	if err == sql.ErrNoRows {
		return fmt.Errorf("bin %s holds no %s-condition stock owned by %s (%s)", binCode, condition, ownerID, sku)
	} else if err != nil {
		return err
	}
	if have < qty {
		return fmt.Errorf("bin %s holds only %d of %s owned by %s, cannot consume %d", binCode, have, sku, ownerID, qty)
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.bin_stock_owner SET qty = qty - $1, updated_at = CURRENT_TIMESTAMP
		WHERE bin_code = $2 AND sku = $3 AND condition = $4 AND owner_id = $5`, schema),
		qty, binCode, sku, condition, ownerID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
		ItemID: sku, WarehouseID: locationCode, Qty: -float64(qty),
		VoucherType: voucherType, VoucherID: voucherID, UserID: userID,
		FromLocationID: binCode, OwnerID: ownerID,
	}); lerr != nil {
		LogSystemError(tenantID, "", "WARN", "ConsumeOwnerStock", fmt.Sprintf("stock ledger write failed for %s: %v", sku, lerr), "")
	}
	LogAuditEvent(tenantID, userID, "WMS_OWNER_STOCK_CONSUME", "SUCCESS",
		fmt.Sprintf("Consumed %d x %s owned by %s from bin %s (%s %s)", qty, sku, ownerID, binCode, voucherType, voucherID))
	return nil
}

// GetOwnerStock lists the owner-segregated breakdown of a SKU/location/owner.
// $1/$2/$3 are all optional filters, the same "blank means any" convention
// GetBatchStock uses - one query serves the per-owner inquiry, the
// per-location inquiry and a full dump.
func GetOwnerStock(tenantID, sku, locationCode, ownerID string) ([]OwnerStockRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT bin_code, sku, owner_id, condition, location_code, qty
		FROM %s.bin_stock_owner
		WHERE qty > 0
		  AND ($1 = '' OR sku = $1)
		  AND ($2 = '' OR location_code = $2)
		  AND ($3 = '' OR owner_id = $3)
		ORDER BY owner_id, sku, bin_code`, schema), sku, locationCode, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OwnerStockRow{}
	for rows.Next() {
		var r OwnerStockRow
		if err := rows.Scan(&r.BinCode, &r.Sku, &r.OwnerID, &r.Condition, &r.LocationCode, &r.Qty); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ownerStockQty is the combined query every billing path needs: how much of
// (optionally) one SKU does ownerID hold at locationCode, right now. It
// unions the explicit bin_stock_owner breakdown with the legacy whole-bin
// fallback (a bin whose own owner_id names this owner, for any (bin, sku,
// condition) slice that has no explicit breakdown row yet) so a tenant that
// has never used RecordOwnerStock sees byte-identical totals to before this
// file existed.
func ownerStockQty(schema, ownerID, locationCode, sku string) (int, error) {
	var qty int
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(q), 0) FROM (
			SELECT qty AS q FROM %s.bin_stock_owner
			WHERE owner_id = $1 AND location_code = $2 AND condition = 'Good'
			  AND ($3 = '' OR sku = $3)
			UNION ALL
			SELECT bs.qty AS q
			FROM %s.bin_stock bs
			JOIN %s.documents b ON b.doctype = 'Bin' AND b.deleted_at IS NULL AND b.data->>'bin_code' = bs.bin_code
			WHERE b.data->>'owner_id' = $1 AND bs.location_code = $2 AND bs.condition = 'Good'
			  AND ($3 = '' OR bs.sku = $3)
			  AND NOT EXISTS (
				SELECT 1 FROM %s.bin_stock_owner bso
				WHERE bso.bin_code = bs.bin_code AND bso.sku = bs.sku AND bso.condition = bs.condition
			  )
		) combined`, schema, schema, schema, schema), ownerID, locationCode, sku).Scan(&qty)
	return qty, err
}

// OwnerStockQty is ownerStockQty's tenantID-based exported wrapper, for
// callers outside this file (the 3PL storage billing report).
func OwnerStockQty(tenantID, ownerID, locationCode, sku string) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	return ownerStockQty(schema, ownerID, locationCode, sku)
}

// ownerLocationSkuQty is one (owner, location, sku) balance - the row shape
// ownerLocationSkuBalances returns for the daily snapshot capture.
type ownerLocationSkuQty struct {
	Owner, Location, Sku string
	Qty                  float64
}

// ownerLocationSkuBalances is ownerStockQty generalised across every owner,
// location and SKU at once - what CaptureStorageBalanceSnapshot needs to
// write one row per scope per day. Same combined-source union, grouped
// rather than filtered to one owner: the explicit bin_stock_owner breakdown,
// plus the legacy whole-bin fallback for any (bin, sku, condition) slice
// bin_stock_owner has no row for.
func ownerLocationSkuBalances(schema string) ([]ownerLocationSkuQty, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT owner, location, sku, SUM(q) FROM (
			SELECT owner_id AS owner, location_code AS location, sku, qty AS q
			FROM %s.bin_stock_owner WHERE condition = 'Good'
			UNION ALL
			SELECT b.data->>'owner_id' AS owner, bs.location_code AS location, bs.sku, bs.qty AS q
			FROM %s.bin_stock bs
			JOIN %s.documents b ON b.doctype = 'Bin' AND b.deleted_at IS NULL AND b.data->>'bin_code' = bs.bin_code
			WHERE bs.condition = 'Good' AND COALESCE(b.data->>'owner_id','') <> ''
			  AND NOT EXISTS (
				SELECT 1 FROM %s.bin_stock_owner bso
				WHERE bso.bin_code = bs.bin_code AND bso.sku = bs.sku AND bso.condition = bs.condition
			  )
		) combined
		GROUP BY owner, location, sku`, schema, schema, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ownerLocationSkuQty{}
	for rows.Next() {
		var r ownerLocationSkuQty
		if err := rows.Scan(&r.Owner, &r.Location, &r.Sku, &r.Qty); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ownerOfBinStock resolves the owner of one SKU's stock in one bin, for the
// task-completion billing hook. It prefers the explicit bin_stock_owner
// breakdown (a real, per-stock answer) and falls back to the bin's own
// owner_id - stage42TaskOwner's original, whole-bin approximation - only
// when no explicit row exists for that exact (bin, sku, condition).
func ownerOfBinStock(schema, binCode, sku, condition string) (string, error) {
	if condition == "" {
		condition = "Good"
	}
	var owner string
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT owner_id FROM %s.bin_stock_owner
		WHERE bin_code = $1 AND sku = $2 AND condition = $3 AND qty > 0
		ORDER BY qty DESC LIMIT 1`, schema), binCode, sku, condition).Scan(&owner)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	if owner != "" {
		return owner, nil
	}
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(data->>'owner_id','') FROM %s.documents
		WHERE doctype = 'Bin' AND deleted_at IS NULL AND data->>'bin_code' = $1
		ORDER BY id LIMIT 1`, schema), binCode).Scan(&owner)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	return owner, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "owner-stock-inquiry", Label: "Owner Stock Inquiry (3PL)", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "owner_id", Label: "Owner"},
			{Key: "sku", Label: "Item"},
			{Key: "bin_code", Label: "Bin"},
			{Key: "location_code", Label: "Location"},
			{Key: "condition", Label: "Condition"},
			{Key: "qty", Label: "Qty"},
		},
		Params: []ReportParam{
			{Key: "sku", Label: "Item Code (optional)", Type: "text"},
			{Key: "location", Label: "Location (optional)", Type: "text"},
			{Key: "owner_id", Label: "Owner (optional)", Type: "text"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetOwnerStock(tenantID, params["sku"], params["location"], params["owner_id"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}
