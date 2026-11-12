package engines

import (
	"database/sql"
	"fmt"
	"strings"

	"custom_erp/db"
)

// Stage 148.33: a failed lot's units stay in QC hold (never sellable) and the
// vendor bill waits while any unit is held. This is the way out, so a bill is
// never stuck for good: the held units are SCRAPPED (a real stock adjustment,
// with its journal) or RELEASED anyway (a person takes the risk, with a note).
// Returning them to the vendor on a purchase return is not offered: a return
// can only take back what the receipt booked as accepted or rejected, and
// these units were accepted and then held (purchase_return.go).

// InspectionDisposeRequest says what becomes of a lot's held units.
type InspectionDisposeRequest struct {
	LotID      string `json:"lot_id"`
	Outcome    string `json:"outcome"` // Scrap, Release
	ReasonCode string `json:"reason_code,omitempty"`
	Note       string `json:"note,omitempty"`
}

// InspectionDisposeResult is what the disposal did.
type InspectionDisposeResult struct {
	ID           string  `json:"id"`
	Outcome      string  `json:"outcome"`
	Qty          float64 `json:"qty"`
	AdjustmentID string  `json:"adjustment_id,omitempty"`
	Status       string  `json:"status,omitempty"` // Done, Posted, Pending Approval
}

// DisposeInspectionHeld takes a decided lot's held units out of QC hold.
//
// Scrap never frees the units: the write-off draws from quarantine (the same
// line 148.28 uses), so they stay held until it posts. It follows the client's
// adjustment approval rule, and the lot closes in the posting transaction
// (closeScrappedLotTx); until then the bill keeps waiting. Release moves the
// units back to available with a reason.
func DisposeInspectionHeld(tenantID, userID, role, store string, in InspectionDisposeRequest) (*InspectionDisposeResult, error) {
	if err := requireInspectionByRule(tenantID); err != nil {
		return nil, err
	}
	if ok, _ := RoleMayOnDoctype(tenantID, role, InspectionLotDoctype, "update"); !ok {
		return nil, inspectionErr("GLOBAL-0011", "not allowed")
	}
	outcome := strings.TrimSpace(in.Outcome)
	if outcome != "Scrap" && outcome != "Release" {
		return nil, inspectionErr(inspectionWaiveCode, "Choose Scrap or Release.")
	}
	if outcome == "Release" && strings.TrimSpace(in.Note) == "" {
		return nil, inspectionErr(inspectionWaiveCode, "Give a reason to release failed stock.")
	}
	if outcome == "Scrap" && strings.TrimSpace(in.ReasonCode) == "" {
		return nil, inspectionErr(inspectionWaiveCode, "Scrap needs a reason code.")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(in.LotID)
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	lot, status, err := lockDocTx(tx, schema, InspectionLotDoctype, id)
	if err != nil {
		return nil, inspectionErr(inspectionLotCode, "That inspection lot was not found.")
	}
	held := numFromInterface(lot["held_qty"])
	sku, loc, batch := strField(lot, "sku"), strField(lot, "location"), strField(lot, "batch_no")
	if (status != lotRejected && status != lotPart) || held <= 0 {
		return nil, inspectionErr(inspectionLotCode, "That lot has no failed stock held.")
	}
	if store != "" && loc != store {
		return nil, inspectionErr(inspectionLotCode, "That lot is not at your store.")
	}
	if adj := strField(lot, "adjustment_id"); adj != "" {
		var st string
		_ = tx.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype = $1 AND id = $2`, schema), InventoryAdjustmentDoctype, adj).Scan(&st)
		if st == "Draft" || st == "Pending Approval" || st == "Approved" {
			return nil, inspectionErr(inspectionLotCode, "Scrap %s is waiting for approval.", adj)
		}
	}
	n := int(held)
	res := &InspectionDisposeResult{ID: id, Outcome: outcome, Qty: held, Status: "Done"}
	if outcome == "Release" {
		moved, err := tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET available = available + $3, qc_hold = COALESCE(qc_hold, 0) - $3, updated_at = CURRENT_TIMESTAMP
			WHERE sku = $1 AND location_code = $2 AND COALESCE(qc_hold, 0) >= $3`, schema), sku, loc, n)
		if err != nil {
			return nil, err
		}
		if rows, _ := moved.RowsAffected(); rows == 0 {
			return nil, inspectionErr(inspectionLotCode, "QC hold for %s no longer has %d units.", sku, n)
		}
		if err := insertStockLedgerEntry(tx, schema, StockLedgerEntry{ItemID: sku, WarehouseID: loc, Qty: 0, VoucherType: InspectionLotDoctype,
			VoucherID: id, UserID: userID, FromStatus: "QC-Hold", ToStatus: "Available", BatchNo: batch, IdempotencyKey: "IQL:" + id + ":dispose"}); err != nil {
			return nil, err
		}
		lot["held_qty"], lot["disposal"], lot["disposal_note"] = 0.0, outcome, strings.TrimSpace(in.Note)
		lot["disposed_by"], lot["disposed_on"] = userID, BusinessToday()
		if err := writeDocTx(tx, schema, InspectionLotDoctype, id, lot, status); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	} else {
		// The posting locks the lot itself, so this lock ends first.
		_ = tx.Rollback()
		adj, st, err := CreateInventoryAdjustment(tenantID, InventoryAdjustmentInput{Location: loc, Note: "Failed inspection lot " + id, LossClass: "Damage", InspectionLot: id,
			Lines: []InventoryAdjustmentLine{{SKU: sku, Qty: -n, ReasonCode: strings.TrimSpace(in.ReasonCode), BatchNo: batch, Note: "Inspection lot " + id, Pool: AdjustmentPoolQCHold}}}, userID, role)
		if err != nil {
			return nil, err
		}
		res.AdjustmentID, res.Status = adj, st
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('adjustment_id', $2::text, 'disposal_note', $3::text) WHERE doctype = $4 AND id = $1`, schema),
			id, adj, strings.TrimSpace(in.Note), InspectionLotDoctype); err != nil {
			return nil, err
		}
	}
	LogAuditEvent(tenantID, userID, "INSPECTION_LOT_DISPOSED", "SUCCESS", fmt.Sprintf("Inspection lot %s: %s %d (%s)", id, outcome, n, res.Status))
	return res, nil
}

// closeScrappedLotTx clears a failed lot when its write-off posts. It refuses if
// the lot no longer holds those units, so a second write-off can never scrap
// another lot's stock.
func closeScrappedLotTx(tx *sql.Tx, schema, adjustmentID, userID string, adjustment map[string]interface{}) error {
	lotID := strField(adjustment, "inspection_lot_id")
	if lotID == "" {
		return nil
	}
	lot, status, err := lockDocTx(tx, schema, InspectionLotDoctype, lotID)
	if err != nil {
		return err
	}
	if numFromInterface(lot["held_qty"]) <= 0 {
		return inspectionErr(inspectionLotCode, "Inspection lot %s has no failed stock held.", lotID)
	}
	lot["held_qty"], lot["disposal"], lot["adjustment_id"] = 0.0, "Scrap", adjustmentID
	lot["disposed_by"], lot["disposed_on"] = userID, BusinessToday()
	return writeDocTx(tx, schema, InspectionLotDoctype, lotID, lot, status)
}

// StoreAppInspectionDispose is DisposeInspectionHeld for the caller's own store.
func StoreAppInspectionDispose(tenantID, userID, role, store string, in InspectionDisposeRequest) (*InspectionDisposeResult, error) {
	store, err := storeAppStoreFor(tenantID, userID, store)
	if err != nil {
		return nil, err
	}
	return DisposeInspectionHeld(tenantID, userID, role, store, in)
}
