package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// transferLot is one lot of a batch-tracked transfer line. On the request
// (`items`) it is an optional manual lot choice; on `dispatched_items` it is
// what actually left the source, FEFO-picked unless the request named it.
type transferLot struct {
	BatchNo    string `json:"batch_no"`
	Qty        int    `json:"qty"`
	ExpiryDate string `json:"expiry_date,omitempty"`
}

type transferLine struct {
	Sku     string        `json:"sku"`
	Qty     int           `json:"qty"`
	Lots    []transferLot `json:"lots,omitempty"`
	Serials []string      `json:"serials,omitempty"`
}

// TransferLine and TransferLot name the line shapes for callers outside this
// package (the dispatch handler's lot / serial picks).
type TransferLine = transferLine
type TransferLot = transferLot

func parseTransferItems(itemsJSON string) ([]transferLine, error) {
	var lines []transferLine
	if itemsJSON == "" {
		return nil, fmt.Errorf("transfer order has no items")
	}
	if err := json.Unmarshal([]byte(itemsJSON), &lines); err != nil {
		return nil, fmt.Errorf("could not parse items JSON: %v", err)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("transfer order has no items")
	}
	for _, l := range lines {
		if l.Sku == "" || l.Qty <= 0 {
			return nil, fmt.Errorf("every transfer line needs a sku and a positive qty (sku=%q qty=%d)", l.Sku, l.Qty)
		}
	}
	return lines, nil
}

// PackTransferOrder (Stage 20.19) records box/carton-to-item mapping for an
// Approved transfer order and moves it to Packed - an optional confirmation
// step before dispatch, not a mandatory gate: DispatchTransferOrder below
// still accepts a plain Approved order too, so this stays additive on top
// of the already-shipped (and UAT-documented) Approved -> Dispatch flow
// rather than a breaking change to it.
func PackTransferOrder(tenantID, transferOrderID, userID string, boxes []map[string]interface{}) error {
	if len(boxes) == 0 {
		return errors.New("at least one box is required to pack a transfer order")
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

	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'TransferOrder' AND id = $1 FOR UPDATE`, schema),
		transferOrderID).Scan(&dataStr, &status); err != nil {
		return fmt.Errorf("transfer order not found: %v", err)
	}
	if status != "Approved" {
		return fmt.Errorf("transfer order must be Approved to pack (current status: %s)", status)
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	// Every box needs a box_id and at least one {sku, qty} line - malformed
	// entries are rejected up front rather than stored and discovered later
	// at pack-list-printing time.
	for i, box := range boxes {
		boxID, _ := box["box_id"].(string)
		if boxID == "" {
			return fmt.Errorf("box %d is missing box_id", i+1)
		}
		items, ok := box["items"].([]interface{})
		if !ok || len(items) == 0 {
			return fmt.Errorf("box %s has no items", boxID)
		}
	}

	boxesBytes, err := json.Marshal(boxes)
	if err != nil {
		return err
	}
	data["boxes"] = string(boxesBytes)
	data["status"] = "Packed"
	updatedBytes, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Packed', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'TransferOrder' AND id = $2`, schema),
		updatedBytes, transferOrderID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "PACK_TRANSFER_ORDER", "SUCCESS", fmt.Sprintf("Packed transfer order %s into %d box(es)", transferOrderID, len(boxes)))
	fromWarehouse, _ := data["from_warehouse"].(string)
	logTaskCompletion(tenantID, "Pack", userID, fromWarehouse, transferOrderID, float64(len(boxes)))
	return nil
}

// DispatchTransferOrder moves an Approved *or* Packed TransferOrder to
// Dispatched: each line's qty is floor-checked and moved out of the source
// location's `on_hand` and `available` into its `in_transit` (Stage 17.6 migration) -
// never received out of thin air, and never dispatched twice (row-locked,
// status-gated).
//
// Stage 73.1.7: tracked stock keeps its identity.
//   - A batch-tracked line leaves as named lots. The request may name them;
//     otherwise a store with bins picks them FEFO (the same allocateFEFO every
//     pick list uses, so expired, blocked or reserved lots are never sent). A
//     store without bins has no lot ledger to pick from, so its line must
//     name the lot.
//   - A serial-tracked line must list exactly qty serial numbers, each InStock
//     at the source. They become Allocated to this transfer until received.
//   - Units taken from bins leave those bins in the same transaction, under
//     the per-(location, sku) lock 59.1 established, so a bin never shows a
//     lot that has gone.
//
// Untracked lines behave exactly as before.
func DispatchTransferOrder(tenantID, transferOrderID, userID string) error {
	return DispatchTransferOrderWith(tenantID, transferOrderID, userID, nil)
}

// DispatchTransferOrderWith dispatches with the lots and serial numbers chosen
// at the dispatch desk (picks, by SKU). A pick names identity only - the
// quantities are the approved ones, so dispatch can never change what was
// approved; a pick for an item not on the transfer is refused.
func DispatchTransferOrderWith(tenantID, transferOrderID, userID string, picks []transferLine) error {
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

	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'TransferOrder' AND id = $1 FOR UPDATE`, schema),
		transferOrderID).Scan(&dataStr, &status); err != nil {
		return fmt.Errorf("transfer order not found: %v", err)
	}
	if status != "Approved" && status != "Packed" {
		return fmt.Errorf("transfer order must be Approved or Packed to dispatch (current status: %s)", status)
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	itemsStr, _ := data["items"].(string)
	lines, err := parseTransferItems(itemsStr)
	if err != nil {
		return err
	}
	fromWarehouse, _ := data["from_warehouse"].(string)
	if fromWarehouse == "" {
		return fmt.Errorf("transfer order is missing from_warehouse")
	}
	seen := map[string]bool{}
	for _, l := range lines {
		if seen[l.Sku] {
			return &ValidationError{Code: "STOCKT-0485", Message: fmt.Sprintf("item %s appears on two lines of this transfer - combine them into one line before dispatch", l.Sku)}
		}
		seen[l.Sku] = true
	}
	for _, p := range picks {
		if len(p.Lots) == 0 && len(p.Serials) == 0 {
			continue
		}
		if !seen[p.Sku] {
			return &ValidationError{Code: "STOCKT-0485", Message: fmt.Sprintf("%s is not on this transfer", p.Sku)}
		}
		for i := range lines {
			if lines[i].Sku == p.Sku {
				if len(p.Lots) > 0 {
					lines[i].Lots = p.Lots
				}
				if len(p.Serials) > 0 {
					lines[i].Serials = p.Serials
				}
			}
		}
	}
	// Lock in a fixed order so two transfers sharing items cannot deadlock.
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Sku < lines[j].Sku })

	dispatched := make([]transferLine, 0, len(lines))
	balances := make([]transferLineBalance, 0, len(lines))
	for _, line := range lines {
		if err := lockSKUAtLocationTx(tx, schema, fromWarehouse, line.Sku); err != nil {
			return err
		}
		// Stage 106.4: dispatch takes the chosen "From" location first.
		if err := drawFromDefaultFromTx(tx, schema, "Transfer Out", fromWarehouse, []interface{}{map[string]interface{}{"sku": line.Sku, "qty": -line.Qty}}); err != nil {
			return err
		}
		var available int
		if err := tx.QueryRow(fmt.Sprintf(
			`SELECT available FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2 FOR UPDATE`, schema),
			line.Sku, fromWarehouse).Scan(&available); err != nil {
			if err == sql.ErrNoRows {
				return &ValidationError{Code: "STOCKT-0112", Message: fmt.Sprintf("insufficient stock for SKU %s at %s: no inventory record", line.Sku, fromWarehouse)}
			}
			return err
		}
		if available < line.Qty {
			return &ValidationError{Code: "STOCKT-0112", Message: fmt.Sprintf("transfer quantity exceeds available stock for SKU %s at %s: available=%d, requested=%d", line.Sku, fromWarehouse, available, line.Qty)}
		}

		out, bal, err := dispatchTrackedLineTx(tx, tenantID, schema, transferOrderID, fromWarehouse, line)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.inventory_availability SET on_hand = on_hand - $1, available = available - $1, in_transit = in_transit + $1, updated_at = CURRENT_TIMESTAMP
			 WHERE sku = $2 AND location_code = $3`, schema),
			line.Qty, line.Sku, fromWarehouse); err != nil {
			return err
		}
		dispatched = append(dispatched, out)
		balances = append(balances, bal)
	}

	dispatchedBytes, _ := json.Marshal(dispatched)
	balanceBytes, _ := json.Marshal(balances)
	data["status"] = "Dispatched"
	data["dispatched_items"] = string(dispatchedBytes)
	data["transfer_balance"] = string(balanceBytes)
	data["dispatched_by"] = userID
	updatedBytes, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Dispatched', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'TransferOrder' AND id = $2`, schema),
		updatedBytes, transferOrderID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	// 26.10.1: one stock ledger line per SKU dispatched, from fromWarehouse -
	// available decreased there (a negative movement), heading to toWarehouse.
	// 73.1.7: a tracked line writes one line per lot / per serial instead, so
	// the ledger answers "where did lot X go".
	toWarehouse, _ := data["to_warehouse"].(string)
	for _, line := range dispatched {
		for _, mv := range transferLedgerMoves(line) {
			if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
				ItemID: line.Sku, WarehouseID: fromWarehouse, Qty: -float64(mv.qty),
				VoucherType: "TransferOrder", VoucherID: transferOrderID, UserID: userID,
				ToLocationID: toWarehouse, ToStatus: "In Transit", BatchNo: mv.batchNo, SerialNo: mv.serialNo,
				IdempotencyKey: fmt.Sprintf("TO-DISPATCH:%s:%s%s", transferOrderID, line.Sku, mv.keySuffix()),
			}); lerr != nil {
				LogSystemError(tenantID, "", "WARN", "DispatchTransferOrder", fmt.Sprintf("stock ledger write failed for %s: %v", line.Sku, lerr), "")
			}
		}
	}
	LogAuditEvent(tenantID, userID, "DISPATCH_TRANSFER_ORDER", "SUCCESS", fmt.Sprintf("Dispatched transfer order %s from %s", transferOrderID, fromWarehouse))
	return nil
}

// ledgerMove is one stock-ledger line of a transfer movement: a whole
// untracked line, one lot, or one serial unit.
type ledgerMove struct {
	qty      int
	batchNo  string
	serialNo string
}

func (m ledgerMove) keySuffix() string {
	switch {
	case m.serialNo != "":
		return ":S:" + m.serialNo
	case m.batchNo != "":
		return ":B:" + m.batchNo
	}
	return ""
}

// transferLedgerMoves splits a dispatched line into its ledger lines. The
// first key form (no suffix) is the pre-73.1.7 one, kept for untracked lines
// so a replay of an old dispatch still dedupes.
func transferLedgerMoves(line transferLine) []ledgerMove {
	if len(line.Serials) > 0 {
		out := make([]ledgerMove, 0, len(line.Serials))
		for _, s := range line.Serials {
			out = append(out, ledgerMove{qty: 1, serialNo: s})
		}
		return out
	}
	if len(line.Lots) > 0 {
		out := make([]ledgerMove, 0, len(line.Lots))
		for _, l := range line.Lots {
			out = append(out, ledgerMove{qty: l.Qty, batchNo: l.BatchNo})
		}
		return out
	}
	return []ledgerMove{{qty: line.Qty}}
}

// dispatchTrackedLineTx takes a line's tracked identity out of the source:
// lots from bins (FEFO, or the lots the request named), serial units from
// InStock to Allocated. It returns the line as dispatched and its opening
// balance. An untracked line passes through unchanged.
func dispatchTrackedLineTx(tx *sql.Tx, tenantID, schema, transferOrderID, fromWarehouse string, line transferLine) (transferLine, transferLineBalance, error) {
	out := transferLine{Sku: line.Sku, Qty: line.Qty}
	bal := transferLineBalance{Sku: line.Sku, Dispatched: line.Qty}
	tracking, err := GetItemTracking(tenantID, line.Sku)
	if err != nil {
		return out, bal, err
	}
	voucher := "TO:" + transferOrderID

	if tracking.TracksSerial() {
		serials := uniqueTrimmed(line.Serials)
		if len(serials) != line.Qty {
			return out, bal, &ValidationError{Code: "STOCKT-0483", Message: fmt.Sprintf(
				"%s is serial-tracked: scan exactly %d serial number(s) for this line (got %d distinct)", line.Sku, line.Qty, len(serials))}
		}
		lotQty := map[string]int{}
		var lotOrder []string
		bal.SerialStatus = map[string]string{}
		bal.SerialLot = map[string]string{}
		for _, sn := range serials {
			docID, unit, err := lockSerialTx(tx, schema, line.Sku, sn)
			if err != nil {
				return out, bal, err
			}
			if unit.Status != SerialInStock || (unit.LocationCode != "" && unit.LocationCode != fromWarehouse) || unit.ReservedFor != "" {
				return out, bal, &ValidationError{Code: "STOCKT-0483", Message: fmt.Sprintf(
					"serial %s of %s is %s at %s - only an unreserved unit in stock at %s can be transferred", sn, line.Sku, unit.Status, orDash(unit.LocationCode), fromWarehouse)}
			}
			if unit.CurrentBin != "" {
				if err := takeFromBinTx(tx, tenantID, schema, unit.CurrentBin, line.Sku, unit.BatchNo, "", 1, voucher); err != nil {
					return out, bal, err
				}
			}
			if err := patchSerialTx(tx, schema, docID, SerialAllocated, map[string]interface{}{
				"reserved_for": transferOrderID, "current_bin": "", "location_code": fromWarehouse,
			}); err != nil {
				return out, bal, err
			}
			bal.SerialStatus[sn] = transferSerialInTransit
			if unit.BatchNo != "" {
				bal.SerialLot[sn] = unit.BatchNo
				if _, ok := lotQty[unit.BatchNo]; !ok {
					lotOrder = append(lotOrder, unit.BatchNo)
				}
				lotQty[unit.BatchNo]++
			}
		}
		out.Serials = serials
		for _, b := range lotOrder {
			out.Lots = append(out.Lots, transferLot{BatchNo: b, Qty: lotQty[b]})
		}
		bal.Lots = lotBalancesFrom(out.Lots)
		return out, bal, nil
	}

	if !tracking.TracksBatch() {
		return out, bal, nil
	}

	var binned bool
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT EXISTS (SELECT 1 FROM %s.bin_stock WHERE sku = $1 AND location_code = $2 AND qty > 0)`, schema),
		line.Sku, fromWarehouse).Scan(&binned); err != nil {
		return out, bal, err
	}

	if len(line.Lots) > 0 {
		sum := 0
		for _, l := range line.Lots {
			if strings.TrimSpace(l.BatchNo) == "" || l.Qty <= 0 {
				return out, bal, &ValidationError{Code: "STOCKT-0482", Message: fmt.Sprintf("every lot on %s needs a batch number and a positive quantity", line.Sku)}
			}
			sum += l.Qty
		}
		if sum != line.Qty {
			return out, bal, &ValidationError{Code: "STOCKT-0482", Message: fmt.Sprintf("the lots named for %s add up to %d, but the line is for %d", line.Sku, sum, line.Qty)}
		}
		for _, l := range line.Lots {
			if err := ValidateBatchForIssue(tenantID, line.Sku, l.BatchNo); err != nil {
				return out, bal, err
			}
			expiry := ""
			if b, _ := GetBatch(tenantID, line.Sku, l.BatchNo); b != nil {
				expiry = b.ExpiryDate
			}
			if binned {
				if err := takeLotFromBinsTx(tx, tenantID, schema, fromWarehouse, line.Sku, l.BatchNo, l.Qty, voucher); err != nil {
					return out, bal, err
				}
			}
			out.Lots = append(out.Lots, transferLot{BatchNo: l.BatchNo, Qty: l.Qty, ExpiryDate: expiry})
		}
		bal.Lots = lotBalancesFrom(out.Lots)
		return out, bal, nil
	}

	if !binned {
		return out, bal, &ValidationError{Code: "STOCKT-0482", Message: fmt.Sprintf(
			"%s is batch-tracked and %s keeps no bin lots to pick from - name the batch for this line", line.Sku, fromWarehouse)}
	}
	candidates, shortfall, err := allocateFEFO(tx, tenantID, line.Sku, fromWarehouse, line.Qty)
	if err != nil {
		return out, bal, err
	}
	if shortfall > 0 {
		return out, bal, &ValidationError{Code: "STOCKT-0482", Message: fmt.Sprintf(
			"only %d of %d units of %s at %s can be traced to an active, unexpired, unreserved lot in a bin - put the rest away against its lot, or name the lots",
			line.Qty-shortfall, line.Qty, line.Sku, fromWarehouse)}
	}
	lotIndex := map[string]int{}
	for _, c := range candidates {
		if err := takeFromBinTx(tx, tenantID, schema, c.BinCode, line.Sku, c.BatchNo, "", c.Qty, voucher); err != nil {
			return out, bal, err
		}
		if i, ok := lotIndex[c.BatchNo]; ok {
			out.Lots[i].Qty += c.Qty
			continue
		}
		lotIndex[c.BatchNo] = len(out.Lots)
		out.Lots = append(out.Lots, transferLot{BatchNo: c.BatchNo, Qty: c.Qty, ExpiryDate: c.ExpiryDate})
	}
	bal.Lots = lotBalancesFrom(out.Lots)
	return out, bal, nil
}

// takeLotFromBinsTx removes qty of one named lot from the source's bins,
// earliest-expiry ordering being moot for a single lot. Uses allocateFEFO so
// the same gates (Active, unexpired, not reserved, bin not frozen) apply to a
// hand-picked lot as to a FEFO one.
func takeLotFromBinsTx(tx *sql.Tx, tenantID, schema, locationCode, sku, batchNo string, qty int, voucher string) error {
	candidates, _, err := allocateFEFO(tx, tenantID, sku, locationCode, 1<<30)
	if err != nil {
		return err
	}
	remaining := qty
	for _, c := range candidates {
		if c.BatchNo != batchNo || remaining == 0 {
			continue
		}
		take := c.Qty
		if take > remaining {
			take = remaining
		}
		if err := takeFromBinTx(tx, tenantID, schema, c.BinCode, sku, batchNo, "", take, voucher); err != nil {
			return err
		}
		remaining -= take
	}
	if remaining > 0 {
		return &ValidationError{Code: "STOCKT-0482", Message: fmt.Sprintf(
			"the bins at %s hold only %d free unit(s) of lot %s of %s, not %d", locationCode, qty-remaining, batchNo, sku, qty)}
	}
	return nil
}

func uniqueTrimmed(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// lockSerialTx reads one SerialNumber unit under a row lock, so a transfer
// and a pick cannot both take the same unit.
func lockSerialTx(tx *sql.Tx, schema, sku, serialNo string) (string, SerialInfo, error) {
	var docID, dataStr, status string
	err := tx.QueryRow(fmt.Sprintf(`
		SELECT id, data, COALESCE(status, '') FROM %s.documents
		WHERE doctype = 'SerialNumber' AND data->>'item' = $1 AND data->>'serial_no' = $2 LIMIT 1 FOR UPDATE`, schema),
		sku, serialNo).Scan(&docID, &dataStr, &status)
	if err == sql.ErrNoRows {
		return "", SerialInfo{}, &ValidationError{Code: "INVENT-0115", Message: fmt.Sprintf("serial %s is not registered for item %s", serialNo, sku)}
	} else if err != nil {
		return "", SerialInfo{}, err
	}
	return docID, serialFromData(docID, dataStr, status), nil
}

// patchSerialTx moves a serial unit inside the caller's transaction - the
// in-transaction twin of TransitionSerialStatus's write, used where the move
// must commit or roll back together with the stock it belongs to. The edges
// used here (InStock -> Allocated -> InStock / Scrapped) are all ones
// serialTransitions allows.
func patchSerialTx(tx *sql.Tx, schema, docID, newStatus string, fields map[string]interface{}) error {
	patch := map[string]interface{}{"status": newStatus}
	for k, v := range fields {
		patch[k] = v
	}
	patchJSON, _ := json.Marshal(patch)
	_, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data = data || $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'SerialNumber' AND id = $3`, schema), patchJSON, newStatus, docID)
	return err
}

// ReceiveTransferOrder is the original single-call receipt API, kept for its
// callers: each item is {sku, qty, reason}. A line received short closes with
// the remainder recorded as lost, which needs a reason (TRN-0259); receiving
// more than was dispatched is refused. See ReceiveTransferOrderLines for the
// partial / discrepancy-aware form.
func ReceiveTransferOrder(tenantID, transferOrderID, userID string, receivedItems []interface{}) error {
	req := TransferReceiptRequest{}
	for _, itemVal := range receivedItems {
		itemMap, ok := itemVal.(map[string]interface{})
		if !ok {
			continue
		}
		line := TransferReceiptLine{}
		line.Sku, _ = itemMap["sku"].(string)
		line.Qty = int(numFromInterface(itemMap["qty"]))
		if q, ok := itemMap["qty"].(int); ok {
			line.Qty = q
		}
		line.Reason, _ = itemMap["reason"].(string)
		req.Lines = append(req.Lines, line)
	}
	_, err := ReceiveTransferOrderLines(tenantID, transferOrderID, userID, req)
	return err
}
