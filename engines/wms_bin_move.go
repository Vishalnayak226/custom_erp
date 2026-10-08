package engines

import (
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Stage 57.9 - Location Movement (user decision 2026-10-07: "bin-to-bin
// move"). Moving stock from one bin to another inside the same warehouse or
// store, with a record of who moved what, where, and why.
//
// One choke point for every shelf move: ExecuteBinReplenishment (26.5.5) now
// runs through MoveBinStock too, so the replenishment screen and the Location
// Movement screen validate the destination the same way. Before this, a
// replenishment move accepted any to-bin string - a bin that did not exist, a
// Blocked or Full one, one at another location - and wrote bin_stock rows for
// it regardless.

// BinMoveInput is one shelf move.
type BinMoveInput struct {
	FromBin string
	ToBin   string
	SKU     string
	Qty     int
	// BatchNo names the lot to move for batch-tracked stock. Optional when the
	// from-bin holds exactly one lot of the SKU (that lot is used); required
	// when it holds several.
	BatchNo string
	Reason  string
	UserID  string
	// VoucherType/TaskType record which screen the move came from on the
	// stock ledger and the WarehouseTask log.
	VoucherType string
	TaskType    string
}

// MoveBinStock moves Good-condition stock between two bins at the same
// location. Availability (on_hand/available) is untouched - the stock never
// leaves the location - so the stock-ledger entry carries Qty 0 with the
// from/to bins, exactly as 26.10.1 records any pure shelf move.
func MoveBinStock(tenantID string, in BinMoveInput) error {
	in.FromBin, in.ToBin, in.SKU = strings.TrimSpace(in.FromBin), strings.TrimSpace(in.ToBin), strings.TrimSpace(in.SKU)
	if in.Qty <= 0 {
		return &ValidationError{Code: "GLOBAL-0014", SubFor: "Quantity", Message: "quantity to move must be positive"}
	}
	if in.FromBin == "" || in.ToBin == "" || in.SKU == "" {
		return &ValidationError{Code: "GLOBAL-0001", Message: "from bin, to bin and item are all required"}
	}
	if in.FromBin == in.ToBin {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "To Bin", Message: "from and to bin must differ"}
	}
	if in.VoucherType == "" {
		in.VoucherType = "BinMove"
	}
	if in.TaskType == "" {
		in.TaskType = "Move"
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

	// Source: the bin row, locked.
	var locationCode string
	var have int
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT location_code, qty FROM %s.bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = 'Good' FOR UPDATE`, schema),
		in.FromBin, in.SKU).Scan(&locationCode, &have)
	if err == sql.ErrNoRows {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "From Bin", Message: fmt.Sprintf("no Good-condition stock of %s in bin %s", in.SKU, in.FromBin)}
	} else if err != nil {
		return err
	}
	if have < in.Qty {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Quantity", Message: fmt.Sprintf("only %d of %s in bin %s, cannot move %d", have, in.SKU, in.FromBin, in.Qty)}
	}

	// Destination: the same checks putaway makes, plus "same location".
	var toLocation, toStatus, toOpState, toZone string
	var maxQty, maxWeight, maxVolume float64
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>'location', ''), status, COALESCE(data->>'bin_status', ''),
		        COALESCE(NULLIF(data->>'capacity', '')::numeric, 0),
		        COALESCE(NULLIF(data->>'max_weight', '')::numeric, 0),
		        COALESCE(NULLIF(data->>'max_volume', '')::numeric, 0),
		        COALESCE(data->>'zone', '')
		 FROM %s.documents WHERE doctype = 'Bin' AND data->>'bin_code' = $1 AND deleted_at IS NULL`, schema),
		in.ToBin).Scan(&toLocation, &toStatus, &toOpState, &maxQty, &maxWeight, &maxVolume, &toZone)
	if err == sql.ErrNoRows {
		return &ValidationError{Code: "GLOBAL-0004", SubFor: "To Bin", Message: fmt.Sprintf("bin %s not found", in.ToBin)}
	} else if err != nil {
		return err
	}
	if toStatus != "Active" {
		return &ValidationError{Code: "GLOBAL-0018", SubFor: "To Bin", Message: fmt.Sprintf("bin %s is not Active", in.ToBin)}
	}
	if toOpState == "Blocked" || toOpState == "Full" || toOpState == "Counting" {
		return &ValidationError{Code: "GLOBAL-0019", SubFor: "To Bin", Message: fmt.Sprintf("bin %s is %s and cannot take stock right now", in.ToBin, toOpState)}
	}
	if toLocation != locationCode {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "To Bin", Message: fmt.Sprintf("bin %s is at another location - use Stock Transfer to move stock between locations", in.ToBin)}
	}
	if err := checkHazmatCompatibility(tx, schema, in.SKU, toZone, in.ToBin); err != nil {
		return err
	}
	if err := enforceBinCapacity(tx, schema, in.ToBin, in.SKU, in.Qty, maxQty, maxWeight, maxVolume); err != nil {
		return err
	}

	// Lot breakdown, when the stock is batch-tracked in this bin.
	batchNo, err := resolveMoveBatch(tx, schema, in.FromBin, in.SKU, in.BatchNo, in.Qty)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.bin_stock SET qty = qty - $1, updated_at = CURRENT_TIMESTAMP WHERE bin_code = $2 AND sku = $3 AND condition = 'Good'`, schema),
		in.Qty, in.FromBin, in.SKU); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.bin_stock (bin_code, sku, location_code, condition, qty)
		VALUES ($1, $2, $3, 'Good', $4)
		ON CONFLICT (bin_code, sku, condition) DO UPDATE SET
			qty = %s.bin_stock.qty + EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema, schema),
		in.ToBin, in.SKU, locationCode, in.Qty); err != nil {
		return err
	}
	if batchNo != "" {
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE %s.bin_stock_batch SET qty = qty - $1, updated_at = CURRENT_TIMESTAMP
			WHERE bin_code = $2 AND sku = $3 AND condition = 'Good' AND batch_no = $4`, schema),
			in.Qty, in.FromBin, in.SKU, batchNo); err != nil {
			return err
		}
		if _, err := tx.Exec(fmt.Sprintf(`
			INSERT INTO %s.bin_stock_batch (bin_code, sku, batch_no, condition, location_code, qty)
			VALUES ($1, $2, $3, 'Good', $4, $5)
			ON CONFLICT (bin_code, sku, condition, batch_no) DO UPDATE SET
				qty = %s.bin_stock_batch.qty + EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema, schema),
			in.ToBin, in.SKU, batchNo, locationCode, in.Qty); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
		ItemID: in.SKU, WarehouseID: locationCode, Qty: 0,
		VoucherType: in.VoucherType, VoucherID: fmt.Sprintf("%s-%s", in.FromBin, in.ToBin), UserID: in.UserID,
		FromLocationID: in.FromBin, ToLocationID: in.ToBin, BatchNo: batchNo,
	}); lerr != nil {
		LogSystemError(tenantID, "", "WARN", "MoveBinStock", fmt.Sprintf("stock ledger write failed for %s: %v", in.SKU, lerr), "")
	}
	detail := fmt.Sprintf("Moved %d x %s from bin %s to bin %s", in.Qty, in.SKU, in.FromBin, in.ToBin)
	if batchNo != "" {
		detail += " (batch " + batchNo + ")"
	}
	if in.Reason != "" {
		detail += ": " + in.Reason
	}
	LogAuditEvent(tenantID, in.UserID, "WMS_BIN_MOVE", "SUCCESS", detail)
	LogCompletedWarehouseTask(tenantID, NewWarehouseTask{
		TaskType: in.TaskType, LocationCode: locationCode, FromBin: in.FromBin, ToBin: in.ToBin, Item: in.SKU, Qty: float64(in.Qty),
	}, in.UserID)
	return nil
}

// resolveMoveBatch decides which lot a move takes. Stock with no lot rows in
// the from-bin is not batch-tracked there and returns "". With lots: the named
// one must hold enough; unnamed is allowed only when there is exactly one.
func resolveMoveBatch(tx *sql.Tx, schema, fromBin, sku, batchNo string, qty int) (string, error) {
	rows, err := tx.Query(fmt.Sprintf(`
		SELECT batch_no, qty FROM %s.bin_stock_batch
		WHERE bin_code = $1 AND sku = $2 AND condition = 'Good' AND qty > 0
		ORDER BY batch_no FOR UPDATE`, schema), fromBin, sku)
	if err != nil {
		return "", err
	}
	lots := map[string]int{}
	var order []string
	for rows.Next() {
		var b string
		var q int
		if err := rows.Scan(&b, &q); err != nil {
			rows.Close()
			return "", err
		}
		lots[b] = q
		order = append(order, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(lots) == 0 {
		return "", nil
	}
	batchNo = strings.TrimSpace(batchNo)
	if batchNo == "" {
		if len(lots) > 1 {
			return "", &ValidationError{Code: "GLOBAL-0001", SubFor: "Batch", Message: fmt.Sprintf("bin %s holds %d batches of %s (%s) - choose which batch to move", fromBin, len(lots), sku, strings.Join(order, ", "))}
		}
		batchNo = order[0]
	}
	have, ok := lots[batchNo]
	if !ok {
		return "", &ValidationError{Code: "GLOBAL-0002", SubFor: "Batch", Message: fmt.Sprintf("bin %s holds no batch %s of %s", fromBin, batchNo, sku)}
	}
	if have < qty {
		return "", &ValidationError{Code: "GLOBAL-0002", SubFor: "Quantity", Message: fmt.Sprintf("batch %s has only %d in bin %s, cannot move %d", batchNo, have, fromBin, qty)}
	}
	return batchNo, nil
}

// BinContentRow is one SKU's Good-condition stock in one bin, for the
// Location Movement screen's pickers.
type BinContentRow struct {
	BinCode  string   `json:"bin_code"`
	SKU      string   `json:"sku"`
	ItemName string   `json:"item_name"`
	Qty      int      `json:"qty"`
	Batches  []string `json:"batches,omitempty"`
}

// GetBinContents lists what each bin at a location holds (Good condition,
// qty > 0). There was no reader of bin_stock for a screen before 57.9 -
// putaway, replenishment and condition moves all asked for bin and SKU as
// typed text.
func GetBinContents(tenantID, locationCode string) ([]BinContentRow, error) {
	if strings.TrimSpace(locationCode) == "" {
		return nil, errors.New("location is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT b.bin_code, b.sku, COALESCE(i.data->>'name', ''), b.qty,
		       COALESCE((SELECT string_agg(bb.batch_no, ',' ORDER BY bb.batch_no) FROM %s.bin_stock_batch bb
		                  WHERE bb.bin_code = b.bin_code AND bb.sku = b.sku AND bb.condition = 'Good' AND bb.qty > 0), '')
		FROM %s.bin_stock b
		LEFT JOIN %s.documents i ON i.doctype = 'Item' AND i.deleted_at IS NULL
		      AND (i.id = b.sku OR i.data->>'code' = b.sku)
		WHERE b.location_code = $1 AND b.condition = 'Good' AND b.qty > 0
		ORDER BY b.bin_code, b.sku`, schema, schema, schema), locationCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BinContentRow{}
	for rows.Next() {
		var r BinContentRow
		var batches string
		if err := rows.Scan(&r.BinCode, &r.SKU, &r.ItemName, &r.Qty, &batches); err != nil {
			return nil, err
		}
		if batches != "" {
			r.Batches = strings.Split(batches, ",")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
