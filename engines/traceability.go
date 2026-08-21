package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 42.1 - the traceability foundation.
//
// Before this file the tree had no lot/batch concept at all: no batch_no, no
// expiry, no shelf life, anywhere in engines/ or db/. That is why FEFO, expiry
// blocking and recall traceability were unbuildable, and it is the first of the
// two foundational holes Stage 42's plan exists to close.
//
// The shape, and why it is this shape:
//
//   - tenant_default.bin_stock_batch is a *further* breakdown of bin_stock,
//     never a second source of truth - exactly the relationship bin_stock_lpn
//     (26.5.4) has to bin_stock, and bin_stock has to inventory_availability.
//     The breakdown may be INCOMPLETE (stock received before this Stage carries
//     no batch, and a tracking_mode = None item never will) but it can never
//     exceed the parent bin's own qty. RecordBatchPutaway enforces that against
//     the same FOR UPDATE-locked parent row AssignToLPN locks.
//
//   - Every gate here is a no-op for a tracking_mode = None item. A tenant that
//     never opens the Traceability field group sees byte-identical behaviour to
//     what it had before this file existed. That is deliberate: traceability is
//     a per-item opt-in, not a mode the whole warehouse is switched into.
//
//   - Batch documents are ordinary Master documents, so the generic list/form
//     screens, the CSV importer, field permissions and the audit trail all work
//     on them with no new code. The one thing that is NOT generic is
//     (item, batch_no) uniqueness - see validateBatchMasterRules in
//     engines/master_data_validation.go for why that cannot be the document id.

// ---------------------------------------------------------------------------
// 42.1.1 - Item tracking flags.
// ---------------------------------------------------------------------------

// Tracking mode values, matching Item.tracking_mode's Select options.
const (
	TrackingNone           = "None"
	TrackingBatch          = "Batch"
	TrackingSerial         = "Serial"
	TrackingBatchAndSerial = "Batch and Serial"
)

// ItemTracking is one Item's traceability configuration. The zero value is a
// correct "not tracked, no shelf life" answer, which is what makes it safe for
// GetItemTracking to return it for an item that does not exist - a caller
// asking about an unknown SKU should get "no gates apply", not an error that
// blocks a receipt.
type ItemTracking struct {
	Sku                       string `json:"sku"`
	Mode                      string `json:"tracking_mode"`
	ShelfLifeDays             int    `json:"shelf_life_days"`
	MinShelfLifeOnReceiptDays int    `json:"min_shelf_life_on_receipt_days"`
	MinShelfLifeOnPickDays    int    `json:"min_shelf_life_on_pick_days"`
}

// TracksBatch reports whether this item's stock must carry a batch number.
func (t ItemTracking) TracksBatch() bool {
	return t.Mode == TrackingBatch || t.Mode == TrackingBatchAndSerial
}

// TracksSerial reports whether this item's units must be individually
// registered. Nothing in 42.1 consumes this yet - the SerialNumber register is
// 42.1.8 - but the flag is read from the same field group and returning it here
// keeps that item from having to re-derive the mode.
func (t ItemTracking) TracksSerial() bool {
	return t.Mode == TrackingSerial || t.Mode == TrackingBatchAndSerial
}

// GetItemTracking reads one Item's traceability configuration.
//
// An unknown SKU returns the zero value and no error, deliberately: this is
// called on the receipt path for every line, and an item that has been deleted
// (or a SKU typed into a CSV that never existed) must not turn into a hard
// failure *here* - the existing item-existence validation owns that rejection,
// and duplicating it would mean two different error messages for one cause.
func GetItemTracking(tenantID, sku string) (ItemTracking, error) {
	out := ItemTracking{Sku: sku, Mode: TrackingNone}
	if strings.TrimSpace(sku) == "" {
		return out, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'Item' AND data->>'code' = $1 LIMIT 1`, schema), sku).Scan(&dataStr)
	if err == sql.ErrNoRows {
		return out, nil
	} else if err != nil {
		return out, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return out, nil
	}
	if mode, _ := data["tracking_mode"].(string); mode != "" {
		out.Mode = mode
	}
	out.ShelfLifeDays = int(numFromInterface(data["shelf_life_days"]))
	out.MinShelfLifeOnReceiptDays = int(numFromInterface(data["min_shelf_life_on_receipt_days"]))
	out.MinShelfLifeOnPickDays = int(numFromInterface(data["min_shelf_life_on_pick_days"]))
	return out, nil
}

// ---------------------------------------------------------------------------
// Dates. Every date on a Batch is an ISO yyyy-mm-dd string in documents.data,
// the same convention Asset.capitalisation_date and the accounting-period
// fields already use.
// ---------------------------------------------------------------------------

const isoDate = "2006-01-02"

// parseTraceDate accepts the ISO date this repo stores, and tolerates a
// timestamp-shaped value by taking its date half - a CSV import or a channel
// payload can legitimately deliver "2026-08-15T00:00:00Z" for a field the UI
// renders as a plain date, and rejecting the receipt over the T would be the
// wrong trade.
func parseTraceDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if len(s) > 10 {
		s = s[:10]
	}
	t, err := time.Parse(isoDate, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// today is the date every shelf-life comparison is made against, truncated so
// "expires today" is a whole-day answer rather than depending on the hour a
// pick happens to run at.
func today() time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// DaysToExpiry returns whole days from today to an ISO expiry date, and false
// if the batch has no expiry at all. Negative means already expired.
func DaysToExpiry(expiryDate string) (int, bool) {
	exp, ok := parseTraceDate(expiryDate)
	if !ok {
		return 0, false
	}
	return int(exp.Sub(today()).Hours() / 24), true
}

// ---------------------------------------------------------------------------
// 42.1.2 - the Batch master.
// ---------------------------------------------------------------------------

// Batch status values, matching Batch.status's Select options.
const (
	BatchActive      = "Active"
	BatchQuarantined = "Quarantined"
	BatchExpired     = "Expired"
	BatchBlocked     = "Blocked"
	BatchConsumed    = "Consumed"
)

// BatchInfo is one Batch document, flattened.
type BatchInfo struct {
	DocID         string            `json:"doc_id"`
	BatchNo       string            `json:"batch_no"`
	Item          string            `json:"item"`
	MfgDate       string            `json:"mfg_date,omitempty"`
	ExpiryDate    string            `json:"expiry_date,omitempty"`
	SupplierBatch string            `json:"supplier_batch,omitempty"`
	Vendor        string            `json:"vendor,omitempty"`
	Status        string            `json:"status"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

// Allocatable reports whether stock from this batch may be issued at all. A
// quarantined, blocked, expired or fully-consumed lot is invisible to
// allocation - which is the whole point of the status field.
func (b BatchInfo) Allocatable() bool { return b.Status == BatchActive }

// batchFromData flattens a Batch document's JSON into a BatchInfo.
func batchFromData(docID, dataStr, status string) BatchInfo {
	out := BatchInfo{DocID: docID, Status: status}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return out
	}
	out.BatchNo, _ = data["batch_no"].(string)
	out.Item, _ = data["item"].(string)
	out.MfgDate, _ = data["mfg_date"].(string)
	out.ExpiryDate, _ = data["expiry_date"].(string)
	out.SupplierBatch, _ = data["supplier_batch"].(string)
	out.Vendor, _ = data["vendor"].(string)
	// The JSON status field and the documents.status column are separate values
	// in this schema (the CycleCountLine gate documents the same distinction).
	// The column is authoritative for a lifecycle decision, so it is only
	// overridden when the column is empty.
	if out.Status == "" {
		out.Status, _ = data["status"].(string)
	}
	if raw, _ := data["attributes"].(string); raw != "" {
		out.Attributes = parseBatchAttributes(raw)
	}
	return out
}

// GetBatch looks one batch up by its natural key (item + batch number), which
// is the only key a warehouse floor ever knows - nobody scans a document UUID.
// Returns nil, nil when there is no such batch, so a caller can distinguish
// "not registered" from "registration lookup failed".
func GetBatch(tenantID, sku, batchNo string) (*BatchInfo, error) {
	if strings.TrimSpace(sku) == "" || strings.TrimSpace(batchNo) == "" {
		return nil, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var docID, dataStr, status string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id, data, COALESCE(status, '') FROM %s.documents
		WHERE doctype = 'Batch' AND data->>'item' = $1 AND data->>'batch_no' = $2 LIMIT 1`, schema),
		sku, batchNo).Scan(&docID, &dataStr, &status)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	info := batchFromData(docID, dataStr, status)
	return &info, nil
}

// EnsureBatch returns the existing batch for (sku, batchNo), or registers one
// if this is the first time that lot has been seen.
//
// Auto-registration is what makes batch capture at receipt (42.1.4) a single
// extra field on the GRN line rather than a separate "create the Batch master
// first" step - a receiving clerk types the lot number off the carton, and the
// master appears. Dates supplied on a later receipt of an already-known lot
// fill in blanks but never overwrite a value that is already there: the first
// receipt is the authoritative one, and a mistyped date on receipt #4 must not
// silently move the expiry of stock that is already on the shelf.
func EnsureBatch(tenantID string, in BatchInfo, receivedQty float64, userID string) (*BatchInfo, error) {
	sku := strings.TrimSpace(in.Item)
	batchNo := strings.TrimSpace(in.BatchNo)
	if sku == "" || batchNo == "" {
		return nil, errors.New("both item and batch number are required to register a batch")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	existing, err := GetBatch(tenantID, sku, batchNo)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		patch := map[string]interface{}{}
		if existing.MfgDate == "" && in.MfgDate != "" {
			patch["mfg_date"] = in.MfgDate
			existing.MfgDate = in.MfgDate
		}
		if existing.ExpiryDate == "" && in.ExpiryDate != "" {
			patch["expiry_date"] = in.ExpiryDate
			existing.ExpiryDate = in.ExpiryDate
		}
		if existing.SupplierBatch == "" && in.SupplierBatch != "" {
			patch["supplier_batch"] = in.SupplierBatch
			existing.SupplierBatch = in.SupplierBatch
		}
		if existing.Vendor == "" && in.Vendor != "" {
			patch["vendor"] = in.Vendor
			existing.Vendor = in.Vendor
		}
		if len(patch) > 0 {
			patchJSON, _ := json.Marshal(patch)
			if _, err := db.DB.Exec(fmt.Sprintf(`
				UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP
				WHERE doctype = 'Batch' AND id = $2`, schema), patchJSON, existing.DocID); err != nil {
				return nil, err
			}
		}
		return existing, nil
	}

	// A brand-new lot. The expiry is derived from the item's shelf life when
	// the receipt gave a manufacture date but no expiry - the common case, since
	// a carton usually prints one or the other and the clerk types what they can
	// see.
	expiry := strings.TrimSpace(in.ExpiryDate)
	if expiry == "" {
		if mfg, ok := parseTraceDate(in.MfgDate); ok {
			tracking, terr := GetItemTracking(tenantID, sku)
			if terr == nil && tracking.ShelfLifeDays > 0 {
				expiry = mfg.AddDate(0, 0, tracking.ShelfLifeDays).Format(isoDate)
			}
		}
	}

	data := map[string]interface{}{
		"batch_no":       batchNo,
		"item":           sku,
		"mfg_date":       strings.TrimSpace(in.MfgDate),
		"expiry_date":    expiry,
		"supplier_batch": strings.TrimSpace(in.SupplierBatch),
		"vendor":         strings.TrimSpace(in.Vendor),
		"received_qty":   receivedQty,
		"status":         BatchActive,
	}
	if len(in.Attributes) > 0 {
		attrJSON, _ := json.Marshal(in.Attributes)
		data["attributes"] = string(attrJSON)
	}
	// NewDocID rather than the batch number: documents.id is the primary key
	// across every doctype in this schema, and a batch number is only unique
	// within its item (see the migration's note).
	docID := NewDocID("BATCH")
	payload, _ := json.Marshal(data)
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'Batch', $2, 'Active', $3)`, schema), docID, payload, userID); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "BATCH_REGISTER", "SUCCESS",
		fmt.Sprintf("Registered batch %s of %s (expiry %s)", batchNo, sku, orDash(expiry)))

	return &BatchInfo{
		DocID: docID, BatchNo: batchNo, Item: sku, MfgDate: strings.TrimSpace(in.MfgDate),
		ExpiryDate: expiry, SupplierBatch: strings.TrimSpace(in.SupplierBatch),
		Vendor: strings.TrimSpace(in.Vendor), Status: BatchActive, Attributes: in.Attributes,
	}, nil
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// SetBatchStatus moves a batch through its lifecycle (quarantine, release,
// block, write-off). Routed through one function rather than letting callers
// UPDATE the row so the audit line and the transition check happen once, for
// every caller, including the ones written after this.
func SetBatchStatus(tenantID, sku, batchNo, newStatus, reason, userID string) error {
	switch newStatus {
	case BatchActive, BatchQuarantined, BatchExpired, BatchBlocked, BatchConsumed:
	default:
		return fmt.Errorf("batch status must be one of Active, Quarantined, Expired, Blocked, Consumed")
	}
	batch, err := GetBatch(tenantID, sku, batchNo)
	if err != nil {
		return err
	}
	if batch == nil {
		return fmt.Errorf("batch %s of %s is not registered", batchNo, sku)
	}
	if batch.Status == newStatus {
		return nil
	}
	if batch.Status == BatchConsumed {
		return fmt.Errorf("batch %s of %s is Consumed - a consumed lot is terminal, receive a new batch instead", batchNo, sku)
	}
	// Coming back out of a hold is the transition a recall audit asks about, so
	// it needs a stated reason. The seeded StatusTransitionRule rows say the
	// same thing declaratively; this is the enforcement for the engine-side
	// callers that do not pass through the generic document PUT.
	if (batch.Status == BatchQuarantined || batch.Status == BatchBlocked) && newStatus == BatchActive && strings.TrimSpace(reason) == "" {
		return fmt.Errorf("releasing batch %s of %s from %s requires a reason", batchNo, sku, batch.Status)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	patch := map[string]interface{}{"status": newStatus}
	if strings.TrimSpace(reason) != "" {
		patch["notes"] = reason
	}
	patchJSON, _ := json.Marshal(patch)
	if _, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data = data || $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'Batch' AND id = $3`, schema), patchJSON, newStatus, batch.DocID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "BATCH_STATUS_CHANGE", "SUCCESS",
		fmt.Sprintf("Batch %s of %s: %s -> %s%s", batchNo, sku, batch.Status, newStatus, reasonSuffix(reason)))
	return nil
}

func reasonSuffix(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return ""
	}
	return " (" + strings.TrimSpace(reason) + ")"
}

// ---------------------------------------------------------------------------
// 42.1.3 - batch on the stock record.
// ---------------------------------------------------------------------------

// BatchStockRow is one lot's quantity in one bin, with the batch's own dates
// joined on so a caller never has to make a second round trip to decide whether
// the stock is pickable.
type BatchStockRow struct {
	BinCode      string `json:"bin_code"`
	Sku          string `json:"sku"`
	BatchNo      string `json:"batch_no"`
	Condition    string `json:"condition"`
	LocationCode string `json:"location_code"`
	Qty          int    `json:"qty"`
	ExpiryDate   string `json:"expiry_date,omitempty"`
	MfgDate      string `json:"mfg_date,omitempty"`
	BatchStatus  string `json:"batch_status,omitempty"`
	DaysToExpiry *int   `json:"days_to_expiry,omitempty"`
}

// RecordBatchPutaway records that qty of a bin's sku/condition stock belongs to
// batchNo. It is the batch analogue of AssignToLPN (26.5.4) and enforces the
// identical invariant against the identical parent row: the sum of a bin's
// batch breakdown can never exceed the bin's own qty for that sku/condition.
//
// This does NOT move stock. bin_stock already holds the quantity; this records
// which lot it is. That separation is what lets batch tracking be switched on
// for an item whose stock is already binned - the existing quantity stays
// exactly where it is and simply acquires a lot identity.
func RecordBatchPutaway(tenantID, binCode, sku, batchNo, condition string, qty int, userID string) error {
	if qty <= 0 {
		return errors.New("batch putaway qty must be positive")
	}
	if strings.TrimSpace(batchNo) == "" {
		return errors.New("batch number is required")
	}
	if condition == "" {
		condition = "Good"
	}
	if !validBinConditions[condition] {
		return fmt.Errorf("condition must be one of Good, Damaged, QC-Hold, RTV")
	}
	batch, err := GetBatch(tenantID, sku, batchNo)
	if err != nil {
		return err
	}
	if batch == nil {
		return fmt.Errorf("batch %s of %s is not registered - register the batch before assigning stock to it", batchNo, sku)
	}
	if batch.Status == BatchConsumed {
		return fmt.Errorf("batch %s of %s is Consumed and cannot receive more stock", batchNo, sku)
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
	var alreadyBatched int
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM(qty), 0) FROM %s.bin_stock_batch WHERE bin_code = $1 AND sku = $2 AND condition = $3`, schema),
		binCode, sku, condition).Scan(&alreadyBatched); err != nil {
		return err
	}
	if alreadyBatched+qty > binQty {
		return fmt.Errorf("batch assignment exceeds the bin's own qty (bin qty=%d, already assigned to batches=%d, requested=%d)",
			binQty, alreadyBatched, qty)
	}

	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.bin_stock_batch (bin_code, sku, batch_no, condition, location_code, qty)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (bin_code, sku, condition, batch_no) DO UPDATE SET
			qty = %s.bin_stock_batch.qty + EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema, schema),
		binCode, sku, batchNo, condition, locationCode, qty); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Qty 0 for the same reason PutawayToBin writes a 0: no stock entered or
	// left the building, the movement being recorded is an identity being
	// attached to quantity that was already there.
	if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
		ItemID: sku, WarehouseID: locationCode, Qty: 0,
		VoucherType: "BatchPutaway", VoucherID: batchNo, UserID: userID, ToLocationID: binCode,
		BatchNo: batchNo,
	}); lerr != nil {
		LogSystemError(tenantID, "", "WARN", "RecordBatchPutaway", fmt.Sprintf("stock ledger write failed for %s: %v", sku, lerr), "")
	}
	LogAuditEvent(tenantID, userID, "WMS_BATCH_PUTAWAY", "SUCCESS",
		fmt.Sprintf("Assigned %d x %s in bin %s to batch %s", qty, sku, binCode, batchNo))
	return nil
}

// ConsumeBatchStock removes qty of a lot from a bin - the movement that makes
// "which orders shipped units of batch X" answerable, because every call writes
// a batch-stamped StockLedgerEntry against the consuming voucher.
//
// Deliberately scoped to the batch sub-ledger: it does NOT touch bin_stock or
// inventory_availability, because the caller's own flow (a pick confirmation, a
// production issue) already owns that posting and double-counting it here would
// make the batch breakdown a second source of truth - the one thing this table
// must never become.
func ConsumeBatchStock(tenantID, binCode, sku, batchNo, condition string, qty int, voucherType, voucherID, userID string) error {
	if qty <= 0 {
		return errors.New("consume qty must be positive")
	}
	if condition == "" {
		condition = "Good"
	}
	if voucherType == "" {
		voucherType = "BatchConsume"
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
		SELECT location_code, qty FROM %s.bin_stock_batch
		WHERE bin_code = $1 AND sku = $2 AND condition = $3 AND batch_no = $4 FOR UPDATE`, schema),
		binCode, sku, condition, batchNo).Scan(&locationCode, &have)
	if err == sql.ErrNoRows {
		return fmt.Errorf("bin %s holds no %s-condition stock of batch %s (%s)", binCode, condition, batchNo, sku)
	} else if err != nil {
		return err
	}
	if have < qty {
		return fmt.Errorf("bin %s holds only %d of batch %s (%s), cannot consume %d", binCode, have, batchNo, sku, qty)
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.bin_stock_batch SET qty = qty - $1, updated_at = CURRENT_TIMESTAMP
		WHERE bin_code = $2 AND sku = $3 AND condition = $4 AND batch_no = $5`, schema),
		qty, binCode, sku, condition, batchNo); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
		ItemID: sku, WarehouseID: locationCode, Qty: -float64(qty),
		VoucherType: voucherType, VoucherID: voucherID, UserID: userID,
		FromLocationID: binCode, BatchNo: batchNo,
	}); lerr != nil {
		LogSystemError(tenantID, "", "WARN", "ConsumeBatchStock", fmt.Sprintf("stock ledger write failed for %s: %v", sku, lerr), "")
	}
	LogAuditEvent(tenantID, userID, "WMS_BATCH_CONSUME", "SUCCESS",
		fmt.Sprintf("Consumed %d x %s from batch %s in bin %s (%s %s)", qty, sku, batchNo, binCode, voucherType, voucherID))
	return nil
}

// GetBatchStock lists a SKU's batch breakdown at a location, earliest expiry
// first. Used by the batch-stock inquiry screen and by the recall report's
// "where is this lot now" half.
func GetBatchStock(tenantID, sku, locationCode, batchNo string) ([]BatchStockRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	// $1/$2/$3 are all optional filters - an empty string means "any", which is
	// what makes one query serve the per-SKU inquiry, the per-location inquiry
	// and the recall lookup without three near-identical statements.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT bsb.bin_code, bsb.sku, bsb.batch_no, bsb.condition, bsb.location_code, bsb.qty,
		       COALESCE(b.data->>'expiry_date', ''), COALESCE(b.data->>'mfg_date', ''), COALESCE(b.status, '')
		FROM %s.bin_stock_batch bsb
		LEFT JOIN %s.documents b
		       ON b.doctype = 'Batch' AND b.data->>'item' = bsb.sku AND b.data->>'batch_no' = bsb.batch_no
		WHERE bsb.qty > 0
		  AND ($1 = '' OR bsb.sku = $1)
		  AND ($2 = '' OR bsb.location_code = $2)
		  AND ($3 = '' OR bsb.batch_no = $3)
		ORDER BY NULLIF(b.data->>'expiry_date', '') ASC NULLS LAST, bsb.batch_no, bsb.bin_code`,
		schema, schema), sku, locationCode, batchNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BatchStockRow{}
	for rows.Next() {
		var r BatchStockRow
		if err := rows.Scan(&r.BinCode, &r.Sku, &r.BatchNo, &r.Condition, &r.LocationCode, &r.Qty,
			&r.ExpiryDate, &r.MfgDate, &r.BatchStatus); err != nil {
			return nil, err
		}
		if d, ok := DaysToExpiry(r.ExpiryDate); ok {
			dv := d
			r.DaysToExpiry = &dv
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// 42.1.5 / 42.1.6 - allocation strategy, and the gates that filter it.
// ---------------------------------------------------------------------------

// Allocation strategy values. 42.2.8 promotes these to an AllocationStrategy
// master; naming them here first means that item changes where the value comes
// from, not what the values are. FEFO is deliberately not selectable through
// AllocationStrategy - it is ResolveAllocationStrategy's own hard rule for a
// batch-tracked item, not a preference a tenant can configure away (see the
// migration's own note).
const (
	StrategyFIFO          = "FIFO"
	StrategyFEFO          = "FEFO"
	StrategyLIFO          = "LIFO"
	StrategyNearestBin    = "NearestBin"
	StrategyFewestPicks   = "FewestPicks"
	StrategyCleanLocation = "CleanLocation"
)

// allocationOrderFragments (42.2.8) maps each AllocationStrategy-selectable
// token to the ORDER BY clause allocateByOrder splices in - a closed
// whitelist (validateAllocationStrategyMasterRules checks against the same
// map), never string-interpolated tenant text. "zone_pick_seq" is a column
// alias allocateByOrder's own query always selects (via a LEFT JOIN to
// Zone), so NearestBin needs no special-cased query shape.
var allocationOrderFragments = map[string]string{
	StrategyFIFO:          "bs.updated_at ASC",
	StrategyLIFO:          "bs.updated_at DESC",
	StrategyNearestBin:    "zone_pick_seq ASC, bs.updated_at ASC",
	StrategyFewestPicks:   "bs.qty DESC",
	StrategyCleanLocation: "bs.qty ASC",
}

// ResolveAllocationStrategy answers which rotation rule applies to one SKU.
//
// This is the "single highest-value line of code in the phase" the plan names,
// and its whole job is to be boring: FEFO for a batch-tracked item, FIFO for
// everything else. Note what it does NOT do - it does not ask whether any
// stock actually carries an expiry. An item declared batch-tracked is allocated
// by expiry even if today's stock has none, because the answer must not flip
// between two waves just because one lot happened to be dated.
//
// Stage 42.2.8: a batch-tracked item's FEFO answer is still unconditional -
// checked BEFORE looking at any configured AllocationStrategy, so that
// master can never be used to bypass the expiry gate. Only a non-batch-
// tracked item's default (previously always FIFO, hardcoded) is now
// overridable by a configured strategy.
func ResolveAllocationStrategy(tenantID, sku string) string {
	tracking, err := GetItemTracking(tenantID, sku)
	if err != nil {
		return StrategyFIFO
	}
	if tracking.TracksBatch() {
		return StrategyFEFO
	}
	if configured := resolveConfiguredAllocationStrategy(tenantID, sku); configured != "" {
		return configured
	}
	return StrategyFIFO
}

// resolveConfiguredAllocationStrategy looks up the Active AllocationStrategy
// for sku (falling back to the blank-item "applies to every non-batch-
// tracked item" row), returning "" when neither exists - the caller's
// unconditional FIFO default. Errors are swallowed in favour of that
// default for the same reason resolveDispatchOrder swallows them: a
// malformed or missing strategy must never block allocation.
func resolveConfiguredAllocationStrategy(tenantID, sku string) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	var strategy string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data->>'strategy' FROM %s.documents
		WHERE doctype = 'AllocationStrategy' AND COALESCE(status, '') = 'Active'
		  AND data->>'item' = $1
		LIMIT 1`, schema), sku).Scan(&strategy)
	if err == sql.ErrNoRows {
		err = db.DB.QueryRow(fmt.Sprintf(`
			SELECT data->>'strategy' FROM %s.documents
			WHERE doctype = 'AllocationStrategy' AND COALESCE(status, '') = 'Active'
			  AND COALESCE(data->>'item', '') = ''
			LIMIT 1`, schema)).Scan(&strategy)
	}
	if err != nil {
		return ""
	}
	if _, ok := allocationOrderFragments[strategy]; !ok {
		return ""
	}
	return strategy
}

// AllocationCandidate is one "take this much of this lot, from this bin"
// suggestion. BatchNo is empty for a FIFO allocation, which is what keeps the
// existing non-batch pick lists byte-identical to what they were.
type AllocationCandidate struct {
	BinCode    string `json:"bin_code"`
	BatchNo    string `json:"batch_no,omitempty"`
	Zone       string `json:"zone"`
	Aisle      string `json:"aisle"`
	Rack       string `json:"rack"`
	Qty        int    `json:"qty"`
	ExpiryDate string `json:"expiry_date,omitempty"`
}

// AllocateFromStock greedily allocates `needed` units of one SKU at one
// location, honouring that SKU's rotation strategy and every 42.1.6 expiry
// gate. It returns what it could allocate and the shortfall; a shortfall is a
// reportable fact, never an error, because a picker still needs a usable list
// for the stock that IS there - the same judgement GenerateBinPickList made in
// Stage 20.18.
//
// This is the single choke point both pick-list generators now call, so
// 42.2.8's strategies (LIFO, nearest-bin, fewest-picks, clean-location) are
// added in one place and every caller inherits them.
func AllocateFromStock(tenantID, sku, locationCode string, needed int) (allocated []AllocationCandidate, shortfall int, err error) {
	if needed <= 0 {
		return nil, 0, nil
	}
	strategy := ResolveAllocationStrategy(tenantID, sku)
	if strategy == StrategyFEFO {
		return allocateFEFO(tenantID, sku, locationCode, needed)
	}
	orderBy, ok := allocationOrderFragments[strategy]
	if !ok {
		orderBy = allocationOrderFragments[StrategyFIFO]
	}
	return allocateByOrder(tenantID, sku, locationCode, needed, orderBy)
}

// allocateByOrder (42.2.8, generalising 26.5.6's original allocateFIFO) is
// the one query every non-FEFO strategy shares - only the ORDER BY clause
// changes. zone_pick_seq is always selected (a LEFT JOIN to the bin's
// Zone), so NearestBin needs no special-cased query, only a different
// fragment from allocationOrderFragments; every other strategy's ORDER BY
// simply ignores that column.
func allocateByOrder(tenantID, sku, locationCode string, needed int, orderBy string) ([]AllocationCandidate, int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, needed, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT bs.bin_code, bs.qty, COALESCE(b.data->>'zone', ''), COALESCE(b.data->>'aisle', ''), COALESCE(b.data->>'rack', ''),
		       COALESCE(NULLIF(z.data->>'pick_sequence', '')::int, 999999) AS zone_pick_seq
		FROM %s.bin_stock bs
		LEFT JOIN %s.documents b ON b.doctype = 'Bin' AND b.data->>'bin_code' = bs.bin_code
		LEFT JOIN %s.documents z ON z.doctype = 'Zone' AND z.status = 'Active' AND z.data->>'code' = b.data->>'zone'
		WHERE bs.sku = $1 AND bs.location_code = $2 AND bs.condition = 'Good' AND bs.qty > 0
		ORDER BY %s`, schema, schema, schema, orderBy), sku, locationCode)
	if err != nil {
		return nil, needed, err
	}
	defer rows.Close()

	var out []AllocationCandidate
	remaining := needed
	for rows.Next() && remaining > 0 {
		var c AllocationCandidate
		var available, zonePickSeq int
		if err := rows.Scan(&c.BinCode, &available, &c.Zone, &c.Aisle, &c.Rack, &zonePickSeq); err != nil {
			return nil, needed, err
		}
		take := available
		if take > remaining {
			take = remaining
		}
		remaining -= take
		c.Qty = take
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, needed, err
	}
	return out, remaining, nil
}

// allocateFEFO allocates earliest-expiry-first across the batch sub-ledger,
// applying 42.1.6's gates in the query itself rather than filtering afterwards:
//
//   - only Active batches (a Quarantined / Blocked / Expired / Consumed lot is
//     simply not stock as far as allocation is concerned);
//   - only batches that still have at least the item's
//     min_shelf_life_on_pick_days left, so goods that would expire in the
//     customer's hands are never allocated in the first place;
//   - undated lots sort LAST, not first - an unknown expiry must never jump
//     ahead of a known-soon one, and NULLS LAST is what encodes that.
//
// Stock in the bin that has NOT been assigned to any batch is deliberately
// invisible here. For a batch-tracked item that is not a loss of stock, it is
// the correct refusal: issuing units nobody can trace to a lot is exactly what
// a batch-tracked item exists to prevent, and the resulting shortfall tells the
// warehouse there is untraced stock to reconcile.
func allocateFEFO(tenantID, sku, locationCode string, needed int) ([]AllocationCandidate, int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, needed, err
	}
	tracking, err := GetItemTracking(tenantID, sku)
	if err != nil {
		return nil, needed, err
	}
	// The earliest expiry date a lot may carry and still be pickable today.
	// An item with no minimum still refuses already-expired stock, because a
	// zero minimum means "usable up to the expiry date", not "expiry ignored".
	cutoff := today().AddDate(0, 0, tracking.MinShelfLifeOnPickDays).Format(isoDate)

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT bsb.bin_code, bsb.batch_no, bsb.qty,
		       COALESCE(bn.data->>'zone', ''), COALESCE(bn.data->>'aisle', ''), COALESCE(bn.data->>'rack', ''),
		       COALESCE(b.data->>'expiry_date', '')
		FROM %s.bin_stock_batch bsb
		JOIN %s.documents b
		     ON b.doctype = 'Batch' AND b.data->>'item' = bsb.sku AND b.data->>'batch_no' = bsb.batch_no
		LEFT JOIN %s.documents bn ON bn.doctype = 'Bin' AND bn.data->>'bin_code' = bsb.bin_code
		WHERE bsb.sku = $1 AND bsb.location_code = $2 AND bsb.condition = 'Good' AND bsb.qty > 0
		  AND COALESCE(b.status, '') = 'Active'
		  AND (NULLIF(b.data->>'expiry_date', '') IS NULL OR b.data->>'expiry_date' >= $3)
		ORDER BY NULLIF(b.data->>'expiry_date', '') ASC NULLS LAST, bsb.batch_no, bsb.bin_code`,
		schema, schema, schema), sku, locationCode, cutoff)
	if err != nil {
		return nil, needed, err
	}
	defer rows.Close()

	var out []AllocationCandidate
	remaining := needed
	for rows.Next() && remaining > 0 {
		var c AllocationCandidate
		var available int
		if err := rows.Scan(&c.BinCode, &c.BatchNo, &available, &c.Zone, &c.Aisle, &c.Rack, &c.ExpiryDate); err != nil {
			return nil, needed, err
		}
		take := available
		if take > remaining {
			take = remaining
		}
		remaining -= take
		c.Qty = take
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, needed, err
	}
	return out, remaining, nil
}

// ValidateBatchForIssue is the shared gate every path that issues batch-tracked
// stock runs through - allocation applies it as a query filter for speed, and
// this is the same rule expressed for a single, already-chosen lot (an RF scan,
// a manual pick confirmation, a production issue).
//
// Attaching it in one function rather than at each call site is the same
// choke-point reasoning ValidateDocument and showApiError already embody: a
// path added in 42.3/42.4 inherits the gate instead of having to remember it.
func ValidateBatchForIssue(tenantID, sku, batchNo string) error {
	tracking, err := GetItemTracking(tenantID, sku)
	if err != nil {
		return err
	}
	if !tracking.TracksBatch() {
		return nil
	}
	if strings.TrimSpace(batchNo) == "" {
		return &ValidationError{Code: "INVENT-0115", Message: fmt.Sprintf("item %s is batch-tracked - a batch number is required to issue it", sku)}
	}
	batch, err := GetBatch(tenantID, sku, batchNo)
	if err != nil {
		return err
	}
	if batch == nil {
		return &ValidationError{Code: "INVENT-0115", Message: fmt.Sprintf("batch %s is not registered for item %s", batchNo, sku)}
	}
	if !batch.Allocatable() {
		return &ValidationError{Code: "INVENT-0104", Message: fmt.Sprintf("batch %s of %s is %s and cannot be issued", batchNo, sku, batch.Status)}
	}
	days, dated := DaysToExpiry(batch.ExpiryDate)
	if !dated {
		return nil
	}
	if days < 0 {
		return &ValidationError{Code: "INVENT-0106", Message: fmt.Sprintf("batch %s of %s expired on %s", batchNo, sku, batch.ExpiryDate)}
	}
	if days < tracking.MinShelfLifeOnPickDays {
		return &ValidationError{Code: "INV-0256", Message: fmt.Sprintf(
			"batch %s of %s has %d day(s) of shelf life left, below the %d-day minimum required to pick it",
			batchNo, sku, days, tracking.MinShelfLifeOnPickDays)}
	}
	return nil
}

// ValidateReceiptBatchLine is the inbound gate for ONE received line, and the
// single definition of what a batch-tracked receipt must carry.
//
// It is called from two places on purpose:
//
//   - engines/transactional_validation.go's validateGRNRules, which runs
//     BEFORE the GRN document is written. That is the path a user actually
//     travels, and running here is what makes a missing lot number a clean
//     GOODSR-style field rejection rather than a saved-then-cancelled receipt.
//     The GRN create hook cancels the document if posting fails, so a rejection
//     raised at posting time would surface to the user as a 500 with a
//     cancelled GRN behind it - which is exactly what it did before this
//     function existed, found in live verification rather than in the tests.
//   - PostGRNReceiptWithQC, which is the choke point every OTHER caller of the
//     receipt path goes through (an API client posting a receipt directly, a
//     future RF receiving screen). Keeping it there too means a caller that
//     bypasses the document validator still cannot post untraceable stock.
//
// One function rather than two copies so the two paths cannot drift into
// disagreeing about what a valid receipt line is.
func ValidateReceiptBatchLine(tenantID, sku, batchNo, expiryDate string) error {
	if strings.TrimSpace(sku) == "" {
		return nil
	}
	tracking, err := GetItemTracking(tenantID, sku)
	if err != nil {
		return err
	}
	if tracking.TracksBatch() && strings.TrimSpace(batchNo) == "" {
		return &ValidationError{Code: "INVENT-0115", Message: fmt.Sprintf(
			"item %s is batch-tracked - every received line must carry a batch/lot number", sku)}
	}
	if strings.TrimSpace(batchNo) == "" {
		return nil
	}
	return ValidateBatchForReceipt(tracking, sku, batchNo, expiryDate)
}

// ValidateReceiptCatchWeightLine (Stage 42.3.7) requires an actual weight on
// any received line whose Item is flagged is_catch_weight = Yes - a nominal
// qty means nothing for an item billed/stocked by variable actual weight
// (meat, produce, cable-by-the-reel), so a receipt that omits it is a real
// data gap, not an optional nicety.
func ValidateReceiptCatchWeightLine(tenantID, sku string, actualWeight *float64) error {
	if strings.TrimSpace(sku) == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var isCatchWeight string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>'is_catch_weight', 'No') FROM %s.documents WHERE doctype = 'Item' AND data->>'code' = $1`, schema),
		sku).Scan(&isCatchWeight); err != nil {
		if err == sql.ErrNoRows {
			return nil // Item existence is enforced elsewhere (META-0198)
		}
		return err
	}
	if isCatchWeight == "Yes" && (actualWeight == nil || *actualWeight <= 0) {
		// GLOBAL-0002, not a new GOODSR-* code: error_catalog_generated.go is
		// generated from ERP_Standard_Message_Control_Matrix_Final.xlsx (DO
		// NOT EDIT BY HAND) - a code writeAPIErrorDetail can't find in that
		// map falls back to a bare 500 with no log line at all (confirmed
		// live: this returned GLOBAL-0302 until this was changed), so a new
		// business-rule check reuses an existing catalog code exactly the
		// way every other Stage 42.3 validator in this file already does.
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Actual Weight", Message: fmt.Sprintf(
			"item %s is a catch weight item - every received line must carry an actual_weight greater than zero", sku)}
	}
	return nil
}

// ValidateBatchForReceipt is the shelf-life half of the inbound gate: a
// supplier delivering short-dated goods is rejected at the door rather than
// discovered when the stock turns out to be unpickable. Only fires when the
// item declares a receipt minimum, so it is silent for everyone who has not
// asked for it.
func ValidateBatchForReceipt(tracking ItemTracking, sku, batchNo, expiryDate string) error {
	if tracking.MinShelfLifeOnReceiptDays <= 0 {
		return nil
	}
	days, dated := DaysToExpiry(expiryDate)
	if !dated {
		return nil
	}
	if days < tracking.MinShelfLifeOnReceiptDays {
		return &ValidationError{Code: "INVENT-0106", Message: fmt.Sprintf(
			"batch %s of %s expires in %d day(s), below the %d-day minimum this item accepts on receipt",
			batchNo, sku, days, tracking.MinShelfLifeOnReceiptDays)}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 42.1.6 - the near-expiry sweep.
// ---------------------------------------------------------------------------

// ExpirySweepResult is what one sweep did, so an operator gets a report rather
// than a silent background mutation.
type ExpirySweepResult struct {
	BatchesExpired     int      `json:"batches_expired"`
	BatchesQuarantined int      `json:"batches_quarantined"`
	QtyQuarantined     int      `json:"qty_quarantined"`
	Notes              []string `json:"notes,omitempty"`
}

// SweepExpiredBatches marks every past-expiry Active batch as Expired and moves
// its remaining Good-condition stock into the existing qc_hold bucket.
//
// Reusing TransitionBinStockCondition rather than writing a second
// condition-move is the point: that function already keeps
// inventory_availability.available in step and already writes the ledger entry,
// so quarantined stock stops being sellable through the exact same path a
// damaged-goods call uses. The batch sub-ledger is moved alongside it so the
// two never disagree about which lot is in which condition.
//
// Idempotent: a batch already Expired is skipped, so running this twice in a
// day quarantines nothing twice.
func SweepExpiredBatches(tenantID, userID string) (ExpirySweepResult, error) {
	var out ExpirySweepResult
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	cutoff := today().Format(isoDate)

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'item', data->>'batch_no', data->>'expiry_date'
		FROM %s.documents
		WHERE doctype = 'Batch' AND COALESCE(status, '') = 'Active'
		  AND NULLIF(data->>'expiry_date', '') IS NOT NULL
		  AND data->>'expiry_date' < $1
		ORDER BY data->>'expiry_date'`, schema), cutoff)
	if err != nil {
		return out, err
	}
	type expiredBatch struct{ sku, batchNo, expiry string }
	var expired []expiredBatch
	for rows.Next() {
		var e expiredBatch
		if err := rows.Scan(&e.sku, &e.batchNo, &e.expiry); err != nil {
			rows.Close()
			return out, err
		}
		expired = append(expired, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	for _, e := range expired {
		if err := SetBatchStatus(tenantID, e.sku, e.batchNo, BatchExpired,
			fmt.Sprintf("auto-expired: expiry date %s has passed", e.expiry), userID); err != nil {
			out.Notes = append(out.Notes, fmt.Sprintf("batch %s (%s): could not mark Expired: %v", e.batchNo, e.sku, err))
			continue
		}
		out.BatchesExpired++

		stock, err := GetBatchStock(tenantID, e.sku, "", e.batchNo)
		if err != nil {
			out.Notes = append(out.Notes, fmt.Sprintf("batch %s (%s): marked Expired but its stock could not be read: %v", e.batchNo, e.sku, err))
			continue
		}
		movedAny := false
		for _, s := range stock {
			if s.Condition != "Good" || s.Qty <= 0 {
				continue
			}
			if err := TransitionBinStockCondition(tenantID, s.BinCode, s.Sku, s.Qty, "Good", "QC-Hold", userID); err != nil {
				out.Notes = append(out.Notes, fmt.Sprintf("batch %s in bin %s: expired but not quarantined: %v", e.batchNo, s.BinCode, err))
				continue
			}
			if err := moveBatchStockCondition(schema, s.BinCode, s.Sku, e.batchNo, "Good", "QC-Hold", s.Qty); err != nil {
				out.Notes = append(out.Notes, fmt.Sprintf("batch %s in bin %s: bin condition moved but the batch breakdown did not: %v", e.batchNo, s.BinCode, err))
				continue
			}
			out.QtyQuarantined += s.Qty
			movedAny = true
		}
		if movedAny {
			out.BatchesQuarantined++
		}
	}

	if out.BatchesExpired > 0 {
		LogAuditEvent(tenantID, userID, "WMS_BATCH_EXPIRY_SWEEP", "SUCCESS", fmt.Sprintf(
			"Expired %d batch(es); quarantined %d unit(s) into QC-Hold", out.BatchesExpired, out.QtyQuarantined))
	}
	return out, nil
}

// moveBatchStockCondition moves one lot's qty between conditions inside the
// batch sub-ledger. Unexported and schema-taking because it is only ever
// correct alongside the matching TransitionBinStockCondition call on the parent
// bin row - calling it on its own would leave the breakdown claiming a
// condition split the bin itself does not have.
func moveBatchStockCondition(schema, binCode, sku, batchNo, from, to string, qty int) error {
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
		SELECT location_code, qty FROM %s.bin_stock_batch
		WHERE bin_code = $1 AND sku = $2 AND condition = $3 AND batch_no = $4 FOR UPDATE`, schema),
		binCode, sku, from, batchNo).Scan(&locationCode, &have)
	if err == sql.ErrNoRows {
		return fmt.Errorf("no %s-condition stock of batch %s in bin %s", from, batchNo, binCode)
	} else if err != nil {
		return err
	}
	if have < qty {
		return fmt.Errorf("bin %s holds only %d of batch %s in %s condition, cannot move %d", binCode, have, batchNo, from, qty)
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.bin_stock_batch SET qty = qty - $1, updated_at = CURRENT_TIMESTAMP
		WHERE bin_code = $2 AND sku = $3 AND condition = $4 AND batch_no = $5`, schema),
		qty, binCode, sku, from, batchNo); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.bin_stock_batch (bin_code, sku, batch_no, condition, location_code, qty)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (bin_code, sku, condition, batch_no) DO UPDATE SET
			qty = %s.bin_stock_batch.qty + EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema, schema),
		binCode, sku, batchNo, to, locationCode, qty); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------------------------------------------------------------------------
// 42.1.6 / recall traceability - the three reports.
// ---------------------------------------------------------------------------

// NearExpiryRow is one lot approaching (or past) its expiry, with the stock
// still sitting behind it.
type NearExpiryRow struct {
	Sku          string `json:"sku"`
	BatchNo      string `json:"batch_no"`
	ExpiryDate   string `json:"expiry_date"`
	DaysToExpiry int    `json:"days_to_expiry"`
	Status       string `json:"status"`
	QtyOnHand    int    `json:"qty_on_hand"`
	Locations    string `json:"locations"`
}

// GetNearExpiryBatches lists batches expiring within `withinDays` (default 30),
// worst first. Stock that has already gone is excluded - a fully-picked lot
// hitting its expiry is not an action for anybody.
func GetNearExpiryBatches(tenantID string, withinDays int) ([]NearExpiryRow, error) {
	if withinDays <= 0 {
		withinDays = 30
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	horizon := today().AddDate(0, 0, withinDays).Format(isoDate)
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT b.data->>'item', b.data->>'batch_no', b.data->>'expiry_date', COALESCE(b.status, ''),
		       COALESCE(SUM(bsb.qty), 0)::int,
		       COALESCE(STRING_AGG(DISTINCT bsb.location_code, ', '), '')
		FROM %s.documents b
		LEFT JOIN %s.bin_stock_batch bsb
		       ON bsb.sku = b.data->>'item' AND bsb.batch_no = b.data->>'batch_no' AND bsb.qty > 0
		WHERE b.doctype = 'Batch'
		  AND NULLIF(b.data->>'expiry_date', '') IS NOT NULL
		  AND b.data->>'expiry_date' <= $1
		  AND COALESCE(b.status, '') NOT IN ('Consumed')
		GROUP BY b.data->>'item', b.data->>'batch_no', b.data->>'expiry_date', b.status
		HAVING COALESCE(SUM(bsb.qty), 0) > 0
		ORDER BY b.data->>'expiry_date' ASC`, schema, schema), horizon)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NearExpiryRow{}
	for rows.Next() {
		var r NearExpiryRow
		if err := rows.Scan(&r.Sku, &r.BatchNo, &r.ExpiryDate, &r.Status, &r.QtyOnHand, &r.Locations); err != nil {
			return nil, err
		}
		if d, ok := DaysToExpiry(r.ExpiryDate); ok {
			r.DaysToExpiry = d
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecallMovementRow is one movement of one lot, read straight off the
// append-only stock ledger.
type RecallMovementRow struct {
	MovedAt     string  `json:"moved_at"`
	Sku         string  `json:"sku"`
	BatchNo     string  `json:"batch_no"`
	Qty         float64 `json:"qty"`
	VoucherType string  `json:"voucher_type"`
	VoucherID   string  `json:"voucher_id"`
	Warehouse   string  `json:"warehouse"`
	FromBin     string  `json:"from_bin,omitempty"`
	ToBin       string  `json:"to_bin,omitempty"`
	UserID      string  `json:"user_id,omitempty"`
}

// GetBatchMovementHistory is the forward recall direction: every movement of a
// lot, in order, from the ledger. "Which orders shipped units of batch X" is
// this list filtered to the outbound voucher types, which is why the voucher id
// is carried rather than summarised - it is the thread back to the customer.
//
// It is a read of an existing append-only table, not a new store: the whole
// reason 42.1.3 stamps batch_no onto every StockLedgerEntry is so recall needs
// no second history of its own.
func GetBatchMovementHistory(tenantID, sku, batchNo string) ([]RecallMovementRow, error) {
	if strings.TrimSpace(batchNo) == "" {
		return nil, errors.New("a batch number is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(TO_CHAR(created_at, 'YYYY-MM-DD HH24:MI'), ''),
		       COALESCE(data->>'item_id', ''), COALESCE(data->>'batch_no', ''),
		       COALESCE(data->>'qty', '0'),
		       COALESCE(data->>'voucher_type', ''), COALESCE(data->>'voucher_id', ''),
		       COALESCE(data->>'warehouse_id', ''),
		       COALESCE(data->>'from_location_id', ''), COALESCE(data->>'to_location_id', ''),
		       COALESCE(data->>'user_id', '')
		FROM %s.documents
		WHERE doctype = 'StockLedgerEntry' AND data->>'batch_no' = $1
		  AND ($2 = '' OR data->>'item_id' = $2)
		ORDER BY created_at ASC`, schema), batchNo, sku)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecallMovementRow{}
	for rows.Next() {
		var r RecallMovementRow
		var qtyStr string
		if err := rows.Scan(&r.MovedAt, &r.Sku, &r.BatchNo, &qtyStr, &r.VoucherType, &r.VoucherID,
			&r.Warehouse, &r.FromBin, &r.ToBin, &r.UserID); err != nil {
			return nil, err
		}
		r.Qty = numFromInterface(qtyStr)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// 42.1.7 - outbound lottable validation (Infor §16).
//
// Batch.attributes has carried arbitrary lottable JSON since 42.1.2
// ({"country_of_origin": "IN", "grade": "A"}); what was missing was a master
// to declare which values a customer's contract actually requires, and a
// check that reads it. LottableConstraint is that master: one row is
// (customer, item-or-blank-for-all, attribute_key, allowed_values). A blank
// item applies the rule to every SKU that customer buys, matching the plan's
// "per-customer/per-order" framing without forcing a duplicate row per SKU.
//
// Scope decision, stated plainly because it is the one a reader will
// question: this lands as ValidateLotForCustomer, a single-lot choke point
// wired into the manual/RF batch-consume path (handleBatchConsume), the same
// "expressed for one already-chosen lot" shape ValidateBatchForIssue already
// has. It is deliberately NOT pushed down into AllocateFromStock's FEFO query
// to pre-filter which lots a pick list even offers. Doing that right needs a
// customer identity threaded through GenerateBinPickList/GenerateWavePickList,
// and the wave path (wms_picking.go) deliberately erases per-task identity
// before allocation - it pools demand by SKU across every task in the wave
// before a single lot is chosen, precisely so one wave can serve many orders
// with one query. Threading a single customer through that pool is a real
// design question (whose constraint wins when two customers in the same wave
// disagree?), not a filter to bolt on under time pressure - it belongs with
// 42.2's task-spine retrofit, the same deferral this phase already made for
// automatic batch consumption at pick-scan.
// ---------------------------------------------------------------------------

// lottableConstraint is one Active LottableConstraint row, resolved down to
// what the check actually needs.
type lottableConstraint struct {
	AttributeKey  string
	AllowedValues []string
}

// parseBatchAttributes turns a Batch document's raw `attributes` JSON string
// (the Data field 42.1.2 defined) into a flat map. Shared by batchFromData
// and the lottable check below so the two can never parse it differently.
func parseBatchAttributes(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var attrs map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &attrs); err != nil {
		return nil
	}
	out := map[string]string{}
	for k, v := range attrs {
		out[k] = strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	return out
}

// fetchLottableConstraints returns the Active constraints that apply to one
// (customer, sku) pair - both the SKU-specific rows and the customer's
// blank-item ("applies to everything I buy") rows. An empty customer or no
// matching rows returns (nil, nil), which is the fast path every caller that
// doesn't know a customer, or whose customer has no contract on file, takes.
func fetchLottableConstraints(tenantID, customer, sku string) ([]lottableConstraint, error) {
	if strings.TrimSpace(customer) == "" {
		return nil, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'attribute_key', COALESCE(data->>'allowed_values', '')
		FROM %s.documents
		WHERE doctype = 'LottableConstraint' AND COALESCE(status, '') = 'Active'
		  AND data->>'customer' = $1
		  AND (COALESCE(data->>'item', '') = '' OR data->>'item' = $2)`, schema), customer, sku)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lottableConstraint
	for rows.Next() {
		var key, valsRaw string
		if err := rows.Scan(&key, &valsRaw); err != nil {
			return nil, err
		}
		var vals []string
		for _, v := range strings.Split(valsRaw, ",") {
			if v = strings.TrimSpace(v); v != "" {
				vals = append(vals, v)
			}
		}
		if key != "" && len(vals) > 0 {
			out = append(out, lottableConstraint{AttributeKey: key, AllowedValues: vals})
		}
	}
	return out, rows.Err()
}

// satisfiesLottableConstraints reports whether attrs (a batch's lottable
// attributes) matches every constraint, and names the first attribute that
// doesn't - a missing key fails the same as a wrong value, because a
// constraint the batch never recorded is exactly as unverifiable as one it
// contradicts.
func satisfiesLottableConstraints(attrs map[string]string, constraints []lottableConstraint) (bool, string) {
	for _, c := range constraints {
		val, ok := attrs[c.AttributeKey]
		if !ok {
			return false, c.AttributeKey
		}
		matched := false
		for _, allowed := range c.AllowedValues {
			if strings.EqualFold(val, allowed) {
				matched = true
				break
			}
		}
		if !matched {
			return false, c.AttributeKey
		}
	}
	return true, ""
}

// ValidateLotForCustomer is the single-lot expression of 42.1.7's outbound
// lottable check - the same relationship ValidateBatchForIssue has to the
// FEFO query's expiry filter, applied to a customer's contract instead of a
// shelf-life minimum. A blank customer or blank batch number is a no-op, so
// every caller that doesn't (yet) know its customer is unaffected.
//
// Reuses INVENT-0114 ("Reserved stock blocked" / "This stock is reserved for
// another order and cannot be used") rather than adding a new catalog code -
// the message matrix is machine-generated from an xlsx and not hand-editable,
// and the sentence already fits: a lot that fails a customer's lottable
// contract is stock this order cannot use, exactly as reserved stock is.
func ValidateLotForCustomer(tenantID, customer, sku, batchNo string) error {
	if strings.TrimSpace(customer) == "" || strings.TrimSpace(batchNo) == "" {
		return nil
	}
	constraints, err := fetchLottableConstraints(tenantID, customer, sku)
	if err != nil {
		return err
	}
	if len(constraints) == 0 {
		return nil
	}
	batch, err := GetBatch(tenantID, sku, batchNo)
	if err != nil {
		return err
	}
	if batch == nil {
		// Not registered - ValidateBatchForIssue's INVENT-0115 already owns
		// that rejection, so this is a no-op rather than a second complaint.
		return nil
	}
	if ok, failedKey := satisfiesLottableConstraints(batch.Attributes, constraints); !ok {
		return &ValidationError{Code: "INVENT-0114", Message: fmt.Sprintf(
			"batch %s of %s does not satisfy customer %s's %q requirement - this lot cannot be issued against this order",
			batchNo, sku, customer, failedKey)}
	}
	return nil
}

// sortBatchStockByExpiry orders a batch-stock slice earliest-expiry-first with
// undated lots last - the same ordering the SQL applies, available to callers
// that have already got the rows in hand.
func sortBatchStockByExpiry(rows []BatchStockRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].ExpiryDate, rows[j].ExpiryDate
		if (a == "") != (b == "") {
			return b == ""
		}
		if a != b {
			return a < b
		}
		return rows[i].BinCode < rows[j].BinCode
	})
}
