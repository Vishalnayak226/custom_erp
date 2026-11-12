package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// OutputPieces names what a production order or a job work receipt made
// (Stage 148.1): piece codes, a lot, or both. Each then carries what its own run
// cost, the way a GRN stamps what it receives. Nothing named posts as it always did.
type OutputPieces struct {
	SerialNos []string `json:"serial_nos"`
	BatchNo   string   `json:"batch_no"`
	UserID    string   `json:"-"`
}

func firstOutput(output []OutputPieces) OutputPieces {
	if len(output) == 0 {
		return OutputPieces{}
	}
	o := output[0]
	o.BatchNo = strings.TrimSpace(o.BatchNo)
	var codes []string
	for _, no := range o.SerialNos {
		if no = strings.TrimSpace(no); no != "" {
			codes = append(codes, no)
		}
	}
	o.SerialNos = codes
	return o
}

func (o OutputPieces) named() bool { return len(o.SerialNos) > 0 || o.BatchNo != "" }

// check refuses what cannot be stamped, before any stock moves: a code or lot on an
// item that keeps none, the wrong count of codes, a code already in use.
func (o OutputPieces) check(tenantID, location, sku string, qty float64) error {
	if !o.named() {
		return nil
	}
	tracking, err := GetItemTracking(tenantID, sku)
	if err != nil {
		return err
	}
	if len(o.SerialNos) > 0 && !tracking.TracksSerial() {
		return &ValidationError{Code: "FIN-0912", Message: fmt.Sprintf("%s keeps no piece codes", sku)}
	}
	if o.BatchNo != "" && !tracking.TracksBatch() {
		return &ValidationError{Code: "FIN-0912", Message: fmt.Sprintf("%s keeps no lots", sku)}
	}
	if err := ValidateReceiptBatchLine(tenantID, sku, o.BatchNo, ""); err != nil {
		return err
	}
	if len(o.SerialNos) > 0 {
		if err := ValidateReceiptPieceLine(tenantID, location, sku, o.SerialNos, qty); err != nil {
			return err
		}
	}
	for _, no := range o.SerialNos {
		piece, err := GetSerial(tenantID, sku, no)
		if err != nil {
			return err
		}
		if piece != nil && piece.Status != SerialReturned {
			return &ValidationError{Code: "FIN-0911", Message: fmt.Sprintf("piece code %s of %s is already in use", no, sku)}
		}
	}
	return nil
}

// register stamps the output after the commit, as a GRN does and for the same
// reason: the goods are in, so a failure here is logged for someone to enter by
// hand rather than undoing a receipt whose stock has moved.
func (o OutputPieces) register(tenantID, location, sku string, qty float64, unitPaise int64, doctype, docID string) {
	if o.UserID == "" {
		o.UserID = "system" // created_by is a real user
	}
	logged := func(what string, err error) {
		LogSystemError(tenantID, "", "WARN", "OutputPieces", fmt.Sprintf("%s of %s could not be registered from %s %s: %v", what, sku, doctype, docID, err), "")
	}
	if o.BatchNo != "" {
		info := BatchInfo{BatchNo: o.BatchNo, Item: sku, MfgDate: BusinessToday(), Location: location, ReceiptCostPaise: unitPaise}
		if _, err := EnsureBatch(tenantID, info, qty, o.UserID); err != nil {
			logged("lot "+o.BatchNo, err)
		}
	}
	for _, no := range o.SerialNos {
		info := SerialInfo{SerialNo: no, Item: sku, BatchNo: o.BatchNo, ReceiptCostPaise: unitPaise}
		if _, err := RegisterSerial(tenantID, info, location, doctype, docID, o.UserID); err != nil {
			logged("piece "+no, err)
		}
	}
}

// --- Dispatch ------------------------------------------------------------------

// taskItemPieces reads the pieces a task line names.
func taskItemPieces(item map[string]interface{}) []string {
	raw, ok := item["serial_nos"]
	if !ok {
		return nil
	}
	text, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var names pieceList
	_ = json.Unmarshal(text, &names)
	return names
}

// resolvePieceScan reads a scan no item owns as a piece's own code: it counts one
// unit of that piece's item and names the unit.
func resolvePieceScan(tenantID, schema, scan string) (sku, name, piece string, err error) {
	piece = strings.TrimSpace(scan)
	sku = pieceItemCode(tenantID, piece)
	if sku == "" {
		return "", "", "", nil
	}
	resolved, itemName, found, err := resolveScanToItem(schema, sku)
	if err != nil {
		return "", "", "", err
	}
	if !found {
		return "", "", "", nil
	}
	return resolved, itemName, piece, nil
}

func (t *pickPackTask) namesPiece(piece string) bool {
	for _, it := range t.items {
		for _, no := range it.SerialNos {
			if no == piece {
				return true
			}
		}
	}
	return false
}

// checkScannedPiece refuses a piece this task cannot take. The dispatch checks the
// piece again under its lock, so a sale in between refuses there, not silently.
func (t *pickPackTask) checkScannedPiece(tenantID, schema, sku, code string) error {
	if t.namesPiece(code) {
		return fmt.Errorf("piece %s is already on this task - duplicate scan", code)
	}
	piece, err := GetSerial(tenantID, sku, code)
	if err != nil {
		return err
	}
	if piece == nil || !piece.Allocatable() {
		status := "not registered"
		if piece != nil {
			status = piece.Status
		}
		return fmt.Errorf("piece %s of %s is %s", code, sku, status)
	}
	if location, _ := t.data["location_code"].(string); piece.LocationCode != location {
		return fmt.Errorf("piece %s is at %s, not %s", code, piece.LocationCode, location)
	}
	if herr := pieceHoldRefusal(*piece, orderCustomerID(tenantID, strField(t.data, "order_id"))); herr != nil { // Stage 148.41
		return herr
	}
	holder, _ := json.Marshal([]map[string]interface{}{{"serial_nos": []string{code}}})
	var other string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents WHERE doctype = 'FulfillmentTask' AND id <> $1 AND deleted_at IS NULL
		  AND status NOT IN ('Dispatched', 'Rejected') AND data->'items' @> $2::jsonb LIMIT 1`, schema), t.id, string(holder)).Scan(&other)
	if err == nil {
		return fmt.Errorf("piece %s is on task %s", code, other)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

// dispatchedSaleCosts is what an order's dispatches booked for the pieces they named.
func dispatchedSaleCosts(schema, orderID string) ([]saleCostLine, error) {
	if orderID == "" {
		return nil, nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->'sale_costs' FROM %s.documents WHERE doctype = 'FulfillmentTask' AND status = 'Dispatched'
		  AND deleted_at IS NULL AND data->>'order_id' = $1 AND data->'sale_costs' IS NOT NULL`, schema), orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []saleCostLine
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var lines []saleCostLine
		if err := json.Unmarshal(raw, &lines); err != nil {
			return nil, err
		}
		out = append(out, lines...)
	}
	return out, rows.Err()
}

// useDispatchedSaleCosts is useBookedSaleCosts for an online order, whose invoice
// moved no stock: a named piece comes back at what its dispatch booked. A line the
// dispatch named nothing on keeps the figure the caller already has.
func useDispatchedSaleCosts(prices map[string]originalLinePrice, schema, orderID string) error {
	lines, err := dispatchedSaleCosts(schema, orderID)
	if err != nil || len(lines) == 0 {
		return err
	}
	raw, err := json.Marshal(map[string]interface{}{"sale_costs": lines})
	if err != nil {
		return err
	}
	useBookedSaleCosts(prices, string(raw))
	return nil
}

// saleOrderOf is the order a sales invoice bills, if it bills one.
func saleOrderOf(invoiceData string) string {
	var d struct {
		SalesOrderID string `json:"sales_order_id"`
	}
	_ = json.Unmarshal([]byte(invoiceData), &d)
	return d.SalesOrderID
}

// addPart puts a bundle component on the sold line's record. The line's source is
// the weakest its parts carry.
func (l *saleCostLine) addPart(part saleCostLine) {
	l.Parts = append(l.Parts, part)
	l.ValuePaise += part.ValuePaise
	if part.ValuePaise > 0 && l.Source != costSourceStandard {
		l.Source = part.Source
	}
}
