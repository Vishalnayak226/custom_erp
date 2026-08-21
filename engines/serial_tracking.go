package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Stage 42.1.8/42.1.9 - the SerialNumber register, the serial half of the
// traceability foundation Stage 42.1 opened for batch (engines/traceability.go).
//
// 42.D3 was the open decision gating this: batch and serial, or batch only?
// Resolved 2026-08-17 as batch AND serial. Item.tracking_mode already carries
// 'Serial' and 'Batch and Serial' (42.1.1) and ItemTracking.TracksSerial()
// already answers correctly - both were built ahead of the decision on
// purpose, so this file adds nothing to that half.
//
// The one place this deliberately does NOT mirror Batch's shape: there is no
// bin_stock_serial breakdown table the way bin_stock_batch breaks down
// bin_stock. Batch needs a breakdown table because many units share one lot
// row (bin_stock_batch.qty sums them). A serial number IS one unit - there is
// nothing to sum. So a SerialNumber document carries current_bin and status
// directly and is updated in place as the unit moves, the same shape a
// FulfillmentTask uses for its own lifecycle. This also means there is no
// "invariant against a parent bin_stock row" to enforce the way
// RecordBatchPutaway enforces one - a serial unit's location is simply
// whatever its own document says it is.
//
// Every gate here is a no-op for an item that does not carry Serial or
// Batch and Serial as its tracking_mode, the same opt-in posture 42.1
// established for batch.

// ---------------------------------------------------------------------------
// 42.1.8 - the SerialNumber master.
// ---------------------------------------------------------------------------

// Serial status values, matching SerialNumber.status's Select options.
const (
	SerialInStock   = "InStock"
	SerialAllocated = "Allocated"
	SerialShipped   = "Shipped"
	SerialReturned  = "Returned"
	SerialScrapped  = "Scrapped"
)

// SerialInfo is one SerialNumber document, flattened.
type SerialInfo struct {
	DocID        string `json:"doc_id"`
	SerialNo     string `json:"serial_no"`
	Item         string `json:"item"`
	BatchNo      string `json:"batch_no,omitempty"`
	CurrentBin   string `json:"current_bin,omitempty"`
	LocationCode string `json:"location_code,omitempty"`
	Vendor       string `json:"vendor,omitempty"`
	Owner        string `json:"owner,omitempty"`
	ReservedFor  string `json:"reserved_for,omitempty"`
	Status       string `json:"status"`
}

// Allocatable reports whether this unit may be picked/allocated at all - an
// already-shipped or scrapped unit is invisible to allocation just as a
// non-Active batch is.
func (s SerialInfo) Allocatable() bool {
	return s.Status == SerialInStock || s.Status == SerialAllocated
}

func serialFromData(docID, dataStr, status string) SerialInfo {
	out := SerialInfo{DocID: docID, Status: status}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return out
	}
	out.SerialNo, _ = data["serial_no"].(string)
	out.Item, _ = data["item"].(string)
	out.BatchNo, _ = data["batch_no"].(string)
	out.CurrentBin, _ = data["current_bin"].(string)
	out.LocationCode, _ = data["location_code"].(string)
	out.Vendor, _ = data["vendor"].(string)
	out.Owner, _ = data["owner"].(string)
	out.ReservedFor, _ = data["reserved_for"].(string)
	// Same split Batch's own batchFromData documents: the JSON status key and
	// the documents.status column are separate values, and the column is
	// authoritative for a lifecycle decision.
	if out.Status == "" {
		out.Status, _ = data["status"].(string)
	}
	return out
}

// GetSerial looks a unit up by its natural key (item + serial number) - the
// only key a warehouse floor ever knows. Returns nil, nil when there is no
// such unit registered.
func GetSerial(tenantID, sku, serialNo string) (*SerialInfo, error) {
	if strings.TrimSpace(sku) == "" || strings.TrimSpace(serialNo) == "" {
		return nil, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var docID, dataStr, status string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id, data, COALESCE(status, '') FROM %s.documents
		WHERE doctype = 'SerialNumber' AND data->>'item' = $1 AND data->>'serial_no' = $2 LIMIT 1`, schema),
		sku, serialNo).Scan(&docID, &dataStr, &status)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	info := serialFromData(docID, dataStr, status)
	return &info, nil
}

// RegisterSerial records a physical unit's identity at receipt and writes the
// zero-qty ledger entry that makes the unit's history readable from the
// stock ledger, the same "identity attach, not a stock move" pattern
// RecordBatchPutaway uses.
//
// A serial number is not idempotent to re-register the way a batch is safe
// to EnsureBatch twice: a lot is a shared bucket where a second sighting is
// just more of the same stock, but a serial number is one physical unit, and
// seeing it while it is already InStock or Allocated is either a duplicate
// scan or two different units printed with the same code - both are data
// problems to surface, not merge silently. The one legitimate "already
// exists" case is a unit coming back from Returned: RegisterSerial restocks
// it rather than erroring, since an RMA genuinely is the same unit re-entering
// the building.
func RegisterSerial(tenantID string, in SerialInfo, locationCode, voucherType, voucherID, userID string) (*SerialInfo, error) {
	sku := strings.TrimSpace(in.Item)
	serialNo := strings.TrimSpace(in.SerialNo)
	if sku == "" || serialNo == "" {
		return nil, errors.New("both item and serial number are required to register a unit")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	existing, err := GetSerial(tenantID, sku, serialNo)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.Status != SerialReturned {
			return nil, fmt.Errorf("serial %s of %s is already registered and %s - a duplicate scan or a reused code, not a new receipt", serialNo, sku, existing.Status)
		}
		patch := map[string]interface{}{"status": SerialInStock, "location_code": locationCode, "current_bin": ""}
		patchJSON, _ := json.Marshal(patch)
		if _, err := db.DB.Exec(fmt.Sprintf(`
			UPDATE %s.documents SET data = data || $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP
			WHERE doctype = 'SerialNumber' AND id = $3`, schema), patchJSON, SerialInStock, existing.DocID); err != nil {
			return nil, err
		}
		existing.Status, existing.LocationCode, existing.CurrentBin = SerialInStock, locationCode, ""
		if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
			ItemID: sku, WarehouseID: locationCode, Qty: 0,
			VoucherType: voucherType, VoucherID: voucherID, UserID: userID, ToStatus: SerialInStock,
			SerialNo: serialNo,
		}); lerr != nil {
			LogSystemError(tenantID, "", "WARN", "RegisterSerial", fmt.Sprintf("stock ledger write failed for %s: %v", sku, lerr), "")
		}
		LogAuditEvent(tenantID, userID, "SERIAL_RESTOCK", "SUCCESS",
			fmt.Sprintf("Restocked returned serial %s of %s at %s", serialNo, sku, locationCode))
		return existing, nil
	}

	data := map[string]interface{}{
		"serial_no":     serialNo,
		"item":          sku,
		"batch_no":      strings.TrimSpace(in.BatchNo),
		"current_bin":   "",
		"location_code": locationCode,
		"vendor":        strings.TrimSpace(in.Vendor),
		"owner":         strings.TrimSpace(in.Owner),
		"status":        SerialInStock,
	}
	// documents.id stays a generated id rather than the serial number itself
	// for the identical reason Batch's does: a serial number is only unique
	// within its item (two different SKUs may reuse the same vendor-assigned
	// code), and documents.id is the primary key across every doctype.
	docID := NewDocID("SERIAL")
	payload, _ := json.Marshal(data)
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'SerialNumber', $2, 'InStock', $3)`, schema), docID, payload, userID); err != nil {
		return nil, err
	}
	if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
		ItemID: sku, WarehouseID: locationCode, Qty: 0,
		VoucherType: voucherType, VoucherID: voucherID, UserID: userID, ToStatus: SerialInStock,
		SerialNo: serialNo,
	}); lerr != nil {
		LogSystemError(tenantID, "", "WARN", "RegisterSerial", fmt.Sprintf("stock ledger write failed for %s: %v", sku, lerr), "")
	}
	LogAuditEvent(tenantID, userID, "SERIAL_REGISTER", "SUCCESS",
		fmt.Sprintf("Registered serial %s of %s at %s", serialNo, sku, locationCode))

	return &SerialInfo{
		DocID: docID, SerialNo: serialNo, Item: sku, BatchNo: strings.TrimSpace(in.BatchNo),
		LocationCode: locationCode, Vendor: strings.TrimSpace(in.Vendor), Owner: strings.TrimSpace(in.Owner),
		Status: SerialInStock,
	}, nil
}

// PutawaySerial records which bin a registered unit has been placed in - the
// serial analogue of RecordBatchPutaway, and the same separation of
// "register the identity" (at receipt) from "place the stock" (at putaway).
func PutawaySerial(tenantID, sku, serialNo, binCode, locationCode, userID string) error {
	if strings.TrimSpace(binCode) == "" {
		return errors.New("bin code is required")
	}
	unit, err := GetSerial(tenantID, sku, serialNo)
	if err != nil {
		return err
	}
	if unit == nil {
		return fmt.Errorf("serial %s of %s is not registered", serialNo, sku)
	}
	if unit.Status != SerialInStock {
		return fmt.Errorf("serial %s of %s is %s and cannot be put away", serialNo, sku, unit.Status)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if locationCode == "" {
		locationCode = unit.LocationCode
	}
	patch := map[string]interface{}{"current_bin": binCode, "location_code": locationCode}
	patchJSON, _ := json.Marshal(patch)
	if _, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'SerialNumber' AND id = $2`, schema), patchJSON, unit.DocID); err != nil {
		return err
	}
	if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
		ItemID: sku, WarehouseID: locationCode, Qty: 0,
		VoucherType: "SerialPutaway", VoucherID: serialNo, UserID: userID,
		FromLocationID: unit.CurrentBin, ToLocationID: binCode, SerialNo: serialNo,
	}); lerr != nil {
		LogSystemError(tenantID, "", "WARN", "PutawaySerial", fmt.Sprintf("stock ledger write failed for %s: %v", sku, lerr), "")
	}
	LogAuditEvent(tenantID, userID, "WMS_SERIAL_PUTAWAY", "SUCCESS",
		fmt.Sprintf("Put away serial %s of %s to bin %s", serialNo, sku, binCode))
	return nil
}

// serialTransitions declares which status moves TransitionSerialStatus
// allows and which of them require a reason - the Go-side enforcement of the
// same edges the migration seeds into StatusTransitionRule, the identical
// split SetBatchStatus draws between the declarative seed (for the generic
// document PUT path, once strict is switched on) and this hard-coded
// enforcement (for every engine-side caller today).
var serialTransitions = map[string]map[string]bool{
	SerialInStock:   {SerialAllocated: false, SerialScrapped: true},
	SerialAllocated: {SerialInStock: false, SerialShipped: false, SerialScrapped: true},
	SerialShipped:   {SerialReturned: true},
	SerialReturned:  {SerialInStock: false, SerialScrapped: true},
}

// TransitionSerialStatus is the single choke point every lifecycle move
// (allocate at pick, ship at pack, return an RMA, scrap a written-off unit)
// goes through - the serial analogue of SetBatchStatus, folded together with
// what RecordBatchPutaway/ConsumeBatchStock split into two functions for
// batch, because a serial unit's "sub-ledger" IS its own document; there is
// no separate qty table to keep in step.
//
// Allocated -> Shipped is where "verify at pack" (42.1.8's own wording,
// docs/specs/wms_parity_plan.md:195-198) becomes a real check rather than a
// bare status flip: if the unit was allocated to a voucher (a pick task, a
// sales order) and this call's voucherID names a different one, it is
// rejected. That is new ground batch's own pick/pack path never needed,
// because a lot is fungible across orders and a serialised unit is not - see
// this file's header.
func TransitionSerialStatus(tenantID, sku, serialNo, newStatus, voucherType, voucherID, reason, userID string) error {
	switch newStatus {
	case SerialInStock, SerialAllocated, SerialShipped, SerialReturned, SerialScrapped:
	default:
		return fmt.Errorf("serial status must be one of %s, %s, %s, %s, %s",
			SerialInStock, SerialAllocated, SerialShipped, SerialReturned, SerialScrapped)
	}
	unit, err := GetSerial(tenantID, sku, serialNo)
	if err != nil {
		return err
	}
	if unit == nil {
		return &ValidationError{Code: "INVENT-0115", Message: fmt.Sprintf("serial %s is not registered for item %s", serialNo, sku)}
	}
	if unit.Status == newStatus {
		return nil
	}
	if unit.Status == SerialScrapped {
		return fmt.Errorf("serial %s of %s is Scrapped - that is terminal, a replacement unit is received and registered fresh", serialNo, sku)
	}
	edges, ok := serialTransitions[unit.Status]
	if !ok {
		return fmt.Errorf("serial %s of %s is %s, which has no further transitions", serialNo, sku, unit.Status)
	}
	needsReason, allowed := edges[newStatus]
	if !allowed {
		return fmt.Errorf("serial %s of %s cannot move from %s to %s", serialNo, sku, unit.Status, newStatus)
	}
	if needsReason && strings.TrimSpace(reason) == "" {
		return fmt.Errorf("moving serial %s of %s from %s to %s requires a reason", serialNo, sku, unit.Status, newStatus)
	}

	// The pack-verify check: a unit allocated to one voucher cannot be shipped
	// against another. An empty ReservedFor (a unit moved to Allocated by a
	// caller that predates this field, or a manual override) is not held to
	// this - only a unit that actually named what it was reserved for is
	// checked against what is trying to ship it.
	if newStatus == SerialShipped && unit.ReservedFor != "" && voucherID != "" && unit.ReservedFor != voucherID {
		return &ValidationError{Code: "INVENT-0104", Message: fmt.Sprintf(
			"serial %s of %s is allocated to %s, not %s - it cannot be packed against a different order", serialNo, sku, unit.ReservedFor, voucherID)}
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	patch := map[string]interface{}{"status": newStatus}
	if newStatus == SerialAllocated {
		patch["reserved_for"] = voucherID
	}
	if newStatus == SerialInStock || newStatus == SerialShipped {
		// Cleared once the reservation has either been cancelled (back to
		// InStock) or fulfilled (Shipped) - a stale reserved_for would
		// otherwise block a future, unrelated allocation's own ship check.
		patch["reserved_for"] = ""
	}
	if strings.TrimSpace(reason) != "" {
		patch["notes"] = reason
	}
	patchJSON, _ := json.Marshal(patch)
	if _, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data = data || $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'SerialNumber' AND id = $3`, schema), patchJSON, newStatus, unit.DocID); err != nil {
		return err
	}
	if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
		ItemID: sku, WarehouseID: unit.LocationCode, Qty: 0,
		VoucherType: voucherType, VoucherID: voucherID, UserID: userID,
		FromStatus: unit.Status, ToStatus: newStatus, SerialNo: serialNo,
	}); lerr != nil {
		LogSystemError(tenantID, "", "WARN", "TransitionSerialStatus", fmt.Sprintf("stock ledger write failed for %s: %v", sku, lerr), "")
	}
	LogAuditEvent(tenantID, userID, "SERIAL_STATUS_CHANGE", "SUCCESS",
		fmt.Sprintf("Serial %s of %s: %s -> %s%s", serialNo, sku, unit.Status, newStatus, reasonSuffix(reason)))
	return nil
}

// ValidateSerialForIssue is the shared gate every path that allocates or
// ships serial-tracked stock runs through - the serial analogue of
// ValidateBatchForIssue.
func ValidateSerialForIssue(tenantID, sku, serialNo string) error {
	tracking, err := GetItemTracking(tenantID, sku)
	if err != nil {
		return err
	}
	if !tracking.TracksSerial() {
		return nil
	}
	if strings.TrimSpace(serialNo) == "" {
		return &ValidationError{Code: "INVENT-0115", Message: fmt.Sprintf("item %s is serial-tracked - a serial number is required to issue it", sku)}
	}
	unit, err := GetSerial(tenantID, sku, serialNo)
	if err != nil {
		return err
	}
	if unit == nil {
		return &ValidationError{Code: "INVENT-0115", Message: fmt.Sprintf("serial %s is not registered for item %s", serialNo, sku)}
	}
	if !unit.Allocatable() {
		return &ValidationError{Code: "INVENT-0104", Message: fmt.Sprintf("serial %s of %s is %s and cannot be issued", serialNo, sku, unit.Status)}
	}
	return nil
}

// ValidateReceiptSerialLine is the inbound gate for ONE received line: a
// serial-tracked item must arrive with exactly as many distinct serial
// numbers as units accepted - one identity per physical unit, no more, no
// fewer. Mirrors ValidateReceiptBatchLine's dual call sites (validateGRNRules
// pre-write, PostGRNReceiptWithQC as defence-in-depth) for the identical
// reason: a rejection here must be a clean field message, not a cancelled
// GRN discovered at posting time.
func ValidateReceiptSerialLine(tenantID, sku string, serialNumbers []string, acceptedQty float64) error {
	if strings.TrimSpace(sku) == "" {
		return nil
	}
	tracking, err := GetItemTracking(tenantID, sku)
	if err != nil {
		return err
	}
	if !tracking.TracksSerial() {
		return nil
	}
	if acceptedQty <= 0 {
		return nil
	}
	want := int(acceptedQty + 0.5)
	seen := map[string]bool{}
	var clean []string
	for _, s := range serialNumbers {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if seen[s] {
			return &ValidationError{Code: "INVENT-0115", Message: fmt.Sprintf(
				"serial number %s is listed twice on the same received line for item %s", s, sku)}
		}
		seen[s] = true
		clean = append(clean, s)
	}
	if len(clean) != want {
		return &ValidationError{Code: "INVENT-0115", Message: fmt.Sprintf(
			"item %s is serial-tracked - %d unit(s) accepted but %d serial number(s) were listed; every accepted unit needs its own serial number",
			sku, want, len(clean))}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 42.1.9 - serial inquiry + movement history.
// ---------------------------------------------------------------------------

// SerialStockRow is one unit's current whereabouts, for the serial-inquiry
// report - the serial analogue of BatchStockRow.
type SerialStockRow struct {
	Sku          string `json:"sku"`
	SerialNo     string `json:"serial_no"`
	BatchNo      string `json:"batch_no,omitempty"`
	Status       string `json:"status"`
	CurrentBin   string `json:"current_bin,omitempty"`
	LocationCode string `json:"location_code,omitempty"`
	ReservedFor  string `json:"reserved_for,omitempty"`
	Vendor       string `json:"vendor,omitempty"`
}

// GetSerialInquiry answers "where is this unit now" (a single serial) or
// "what serial-tracked stock of this SKU do we have" (a whole item), the
// same optional-filter convention GetBatchStock uses: an empty argument means
// "any". Deliberately reads the SerialNumber documents directly rather than
// the stock ledger - unlike batch, a unit's current location and status ARE
// its document, so there is no separate breakdown table to join.
func GetSerialInquiry(tenantID, sku, serialNo, status string) ([]SerialStockRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'item', data->>'serial_no', COALESCE(data->>'batch_no', ''), COALESCE(status, ''),
		       COALESCE(data->>'current_bin', ''), COALESCE(data->>'location_code', ''),
		       COALESCE(data->>'reserved_for', ''), COALESCE(data->>'vendor', '')
		FROM %s.documents
		WHERE doctype = 'SerialNumber'
		  AND ($1 = '' OR data->>'item' = $1)
		  AND ($2 = '' OR data->>'serial_no' = $2)
		  AND ($3 = '' OR COALESCE(status, '') = $3)
		ORDER BY data->>'item', data->>'serial_no'`, schema), sku, serialNo, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SerialStockRow{}
	for rows.Next() {
		var r SerialStockRow
		if err := rows.Scan(&r.Sku, &r.SerialNo, &r.BatchNo, &r.Status, &r.CurrentBin, &r.LocationCode, &r.ReservedFor, &r.Vendor); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SerialMovementRow is one movement of one unit, read straight off the
// append-only stock ledger - the serial analogue of RecallMovementRow.
type SerialMovementRow struct {
	MovedAt     string  `json:"moved_at"`
	Sku         string  `json:"sku"`
	SerialNo    string  `json:"serial_no"`
	Qty         float64 `json:"qty"`
	VoucherType string  `json:"voucher_type"`
	VoucherID   string  `json:"voucher_id"`
	Warehouse   string  `json:"warehouse"`
	FromStatus  string  `json:"from_status,omitempty"`
	ToStatus    string  `json:"to_status,omitempty"`
	UserID      string  `json:"user_id,omitempty"`
}

// GetSerialMovementHistory is "everywhere this unit has been", in order, from
// the ledger - the same read-of-an-existing-append-only-table shape
// GetBatchMovementHistory uses, and for the same reason: 42.1.8's every
// identity-attach/status-change writes a StockLedgerEntry carrying serial_no,
// so this needs no history store of its own.
func GetSerialMovementHistory(tenantID, sku, serialNo string) ([]SerialMovementRow, error) {
	if strings.TrimSpace(serialNo) == "" {
		return nil, errors.New("a serial number is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(TO_CHAR(created_at, 'YYYY-MM-DD HH24:MI'), ''),
		       COALESCE(data->>'item_id', ''), COALESCE(data->>'serial_no', ''),
		       COALESCE(data->>'qty', '0'),
		       COALESCE(data->>'voucher_type', ''), COALESCE(data->>'voucher_id', ''),
		       COALESCE(data->>'warehouse_id', ''),
		       COALESCE(data->>'from_status', ''), COALESCE(data->>'to_status', ''),
		       COALESCE(data->>'user_id', '')
		FROM %s.documents
		WHERE doctype = 'StockLedgerEntry' AND data->>'serial_no' = $1
		  AND ($2 = '' OR data->>'item_id' = $2)
		ORDER BY created_at ASC`, schema), serialNo, sku)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SerialMovementRow{}
	for rows.Next() {
		var r SerialMovementRow
		var qtyStr string
		if err := rows.Scan(&r.MovedAt, &r.Sku, &r.SerialNo, &qtyStr, &r.VoucherType, &r.VoucherID,
			&r.Warehouse, &r.FromStatus, &r.ToStatus, &r.UserID); err != nil {
			return nil, err
		}
		r.Qty = numFromInterface(qtyStr)
		out = append(out, r)
	}
	return out, rows.Err()
}
