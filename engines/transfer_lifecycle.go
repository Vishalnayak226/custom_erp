package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 73.1.7 - the transfer order lifecycle past dispatch, and D04's
// approval rule.
//
// What a dispatched transfer owes is kept as a balance per line (and per lot,
// and per serial unit): dispatched = received + damaged + lost + returned +
// still in transit. Every receipt and every return-to-source is one event
// that moves units out of "in transit" under the order's row lock, so a
// receipt can be split over as many deliveries as it takes and a unit can
// never be received twice. Excess (more arrived than was sent) is recorded
// beside the balance, never against it.
//
// Discrepancies (damaged, lost, excess, returned) carry a reason code from
// the ReasonCode master, category "Transfer Discrepancy" - the same master
// and the same requireActiveReasonCode check holds, cancellations and short
// picks already use. The one exception is the pre-73.1.7 single-call API
// (ReceiveTransferOrder), whose free-text reason for a short close is kept so
// its existing callers keep working.

// TransferDirectApprovalLimitSetting is D04's per-client value below which a
// Store Manager may approve a transfer without a second person. 0 (the
// default) means every transfer goes through maker-checker.
const TransferDirectApprovalLimitSetting = "inventory.transfer_direct_approval_limit"

// TransferDiscrepancyReasonCategory is the ReasonCode category every
// transfer discrepancy and return-to-source must use.
const TransferDiscrepancyReasonCategory = "Transfer Discrepancy"

const (
	transferSerialInTransit = "In transit"
	transferSerialReceived  = "Received"
	transferSerialDamaged   = "Damaged"
	transferSerialLost      = "Lost"
	transferSerialReturned  = "Returned"
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: TransferDirectApprovalLimitSetting, Module: "Inventory",
		Label: "Transfer direct-approval limit", Type: SettingTypeFloat, Default: "0", Unit: "₹",
		Min:         settingBound(0),
		Description: "A stock transfer valued below this amount (at item cost) can be approved by a Store Manager of the sending or receiving store without a second person. 0 sends every transfer for approval by someone other than its creator.",
	})
}

type transferLotBalance struct {
	BatchNo    string `json:"batch_no"`
	ExpiryDate string `json:"expiry_date,omitempty"`
	Dispatched int    `json:"dispatched"`
	Received   int    `json:"received"`
	Damaged    int    `json:"damaged"`
	Lost       int    `json:"lost"`
	Returned   int    `json:"returned"`
	Excess     int    `json:"excess"`
}

func (l transferLotBalance) Open() int {
	return l.Dispatched - l.Received - l.Damaged - l.Lost - l.Returned
}

type transferLineBalance struct {
	Sku          string               `json:"sku"`
	Dispatched   int                  `json:"dispatched"`
	Received     int                  `json:"received"`
	Damaged      int                  `json:"damaged"`
	Lost         int                  `json:"lost"`
	Excess       int                  `json:"excess"`
	Returned     int                  `json:"returned"`
	Lots         []transferLotBalance `json:"lots,omitempty"`
	SerialStatus map[string]string    `json:"serials,omitempty"`
	SerialLot    map[string]string    `json:"serial_lots,omitempty"`
}

func (b transferLineBalance) Open() int {
	return b.Dispatched - b.Received - b.Damaged - b.Lost - b.Returned
}

func lotBalancesFrom(lots []transferLot) []transferLotBalance {
	out := make([]transferLotBalance, 0, len(lots))
	for _, l := range lots {
		out = append(out, transferLotBalance{BatchNo: l.BatchNo, ExpiryDate: l.ExpiryDate, Dispatched: l.Qty})
	}
	return out
}

// loadTransferBalances reads the stored balance, or builds one from
// dispatched_items for a transfer dispatched before 73.1.7 (no lots, no
// serials, nothing received yet - it can only have been Dispatched).
func loadTransferBalances(data map[string]interface{}) ([]transferLineBalance, error) {
	if raw, _ := data["transfer_balance"].(string); raw != "" {
		var out []transferLineBalance
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return nil, fmt.Errorf("stored transfer balance is unreadable: %v", err)
		}
		return out, nil
	}
	dispatchedStr, _ := data["dispatched_items"].(string)
	lines, err := parseTransferItems(dispatchedStr)
	if err != nil {
		return nil, err
	}
	out := make([]transferLineBalance, 0, len(lines))
	for _, l := range lines {
		out = append(out, transferLineBalance{Sku: l.Sku, Dispatched: l.Qty, Lots: lotBalancesFrom(l.Lots)})
	}
	return out, nil
}

// TransferReceiptLine is one line of a receipt. Qty is the good quantity;
// for a serial-tracked item the quantities come from the serial lists.
type TransferReceiptLine struct {
	Sku            string   `json:"sku"`
	BatchNo        string   `json:"batch_no,omitempty"`
	Qty            int      `json:"qty"`
	DamagedQty     int      `json:"damaged_qty,omitempty"`
	LostQty        int      `json:"lost_qty,omitempty"`
	ExcessQty      int      `json:"excess_qty,omitempty"`
	ReasonCode     string   `json:"reason_code,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	Serials        []string `json:"serials,omitempty"`
	DamagedSerials []string `json:"damaged_serials,omitempty"`
	LostSerials    []string `json:"lost_serials,omitempty"`
}

// TransferReceiptRequest is one delivery. KeepOpen leaves whatever this
// receipt does not account for in transit for a later receipt; without it the
// remainder is closed as lost (and needs a reason). ReceiptKey makes a retried
// call a no-op.
type TransferReceiptRequest struct {
	Lines      []TransferReceiptLine `json:"lines"`
	KeepOpen   bool                  `json:"keep_open"`
	ReceiptKey string                `json:"receipt_key,omitempty"`
}

// TransferReturnLine is one line sent back to the source.
type TransferReturnLine struct {
	Sku     string   `json:"sku"`
	BatchNo string   `json:"batch_no,omitempty"`
	Qty     int      `json:"qty"`
	Serials []string `json:"serials,omitempty"`
}

// TransferMovementResult is what a receipt or return leaves behind.
type TransferMovementResult struct {
	TransferOrderID string                `json:"transfer_order_id"`
	Status          string                `json:"status"`
	EventNo         int                   `json:"event_no"`
	Replayed        bool                  `json:"replayed,omitempty"`
	Balance         []transferLineBalance `json:"balance"`
}

type transferEventLine struct {
	Sku        string   `json:"sku"`
	BatchNo    string   `json:"batch_no,omitempty"`
	Received   int      `json:"received,omitempty"`
	Damaged    int      `json:"damaged,omitempty"`
	Lost       int      `json:"lost,omitempty"`
	Excess     int      `json:"excess,omitempty"`
	Returned   int      `json:"returned,omitempty"`
	ReasonCode string   `json:"reason_code,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Serials    []string `json:"serials,omitempty"`
}

type transferEvent struct {
	No    int                 `json:"no"`
	Kind  string              `json:"kind"`
	Key   string              `json:"key,omitempty"`
	At    string              `json:"at"`
	By    string              `json:"by"`
	Lines []transferEventLine `json:"lines"`
}

func loadTransferEvents(data map[string]interface{}) []transferEvent {
	var out []transferEvent
	if raw, _ := data["transfer_events"].(string); raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

// lotPortion is part of a quantity applied to one lot (batchNo "" for an
// untracked line).
type lotPortion struct {
	batchNo string
	qty     int
}

// applyToLots books qty of one kind (received/damaged/lost/returned) against
// the line's lots - the named lot, or the open lots in dispatch order.
func applyToLots(b *transferLineBalance, batchNo string, qty int, kind string) ([]lotPortion, error) {
	if qty == 0 {
		return nil, nil
	}
	if len(b.Lots) == 0 {
		if batchNo != "" {
			return nil, &ValidationError{Code: "STOCKT-0485", Message: fmt.Sprintf("%s was not sent as lot %s", b.Sku, batchNo)}
		}
		return []lotPortion{{qty: qty}}, nil
	}
	var out []lotPortion
	remaining := qty
	for i := range b.Lots {
		lot := &b.Lots[i]
		if (batchNo != "" && lot.BatchNo != batchNo) || lot.Open() <= 0 {
			continue
		}
		take := lot.Open()
		if take > remaining {
			take = remaining
		}
		switch kind {
		case transferSerialReceived:
			lot.Received += take
		case transferSerialDamaged:
			lot.Damaged += take
		case transferSerialLost:
			lot.Lost += take
		case transferSerialReturned:
			lot.Returned += take
		}
		out = append(out, lotPortion{batchNo: lot.BatchNo, qty: take})
		remaining -= take
		if remaining == 0 {
			break
		}
	}
	if remaining > 0 {
		what := b.Sku
		if batchNo != "" {
			what = fmt.Sprintf("lot %s of %s", batchNo, b.Sku)
		}
		return nil, &ValidationError{Code: "STOCKT-0485", Message: fmt.Sprintf("only %d unit(s) of %s are still in transit, not %d", qty-remaining, what, qty)}
	}
	return out, nil
}

func addToBalance(b *transferLineBalance, kind string, qty int) {
	switch kind {
	case transferSerialReceived:
		b.Received += qty
	case transferSerialDamaged:
		b.Damaged += qty
	case transferSerialLost:
		b.Lost += qty
	case transferSerialReturned:
		b.Returned += qty
	}
}

// requireTransferReason checks a discrepancy's reason code.
func requireTransferReason(tenantID, reasonCode, category, what string) error {
	if strings.TrimSpace(reasonCode) == "" {
		return &ValidationError{Code: "STOCKT-0484", Message: fmt.Sprintf("%s needs a reason code (Reason Codes, category %q)", what, category)}
	}
	if err := requireActiveReasonCode(tenantID, reasonCode, category); err != nil {
		return &ValidationError{Code: "STOCKT-0484", Message: fmt.Sprintf("%s: %v", what, err)}
	}
	return nil
}

// transferPosting accumulates one event's stock effect per SKU.
type transferPosting struct {
	accounted int // units leaving the source's in_transit
	toOnHand  int
	toAvail   int
	toDamaged int
	// ledger lines at the destination (or the source, for a return)
	moves []transferLedgerLine
	// serial documents to move
	serials []transferSerialMove
}

type transferLedgerLine struct {
	qty      int
	batchNo  string
	serialNo string
	toStatus string
	kind     string
}

type transferSerialMove struct {
	serial string
	kind   string
}

// lockTransferOrderTx loads a TransferOrder under its row lock.
func lockTransferOrderTx(tx *sql.Tx, schema, transferOrderID string) (map[string]interface{}, string, error) {
	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'TransferOrder' AND id = $1 FOR UPDATE`, schema),
		transferOrderID).Scan(&dataStr, &status); err != nil {
		return nil, "", fmt.Errorf("transfer order not found: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", err
	}
	return data, status, nil
}

func transferStatusAfter(balances []transferLineBalance) string {
	open, arrived, returned := 0, 0, 0
	for _, b := range balances {
		open += b.Open()
		arrived += b.Received + b.Damaged
		returned += b.Returned
	}
	switch {
	case open > 0:
		return "Partially Received"
	case arrived == 0 && returned > 0:
		return "Returned"
	}
	return "Received"
}

func findBalance(balances []transferLineBalance, sku string) *transferLineBalance {
	for i := range balances {
		if balances[i].Sku == sku {
			return &balances[i]
		}
	}
	return nil
}

func replayedTransferEvent(events []transferEvent, key string) *transferEvent {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	for i := range events {
		if events[i].Key == key {
			return &events[i]
		}
	}
	return nil
}

// ReceiveTransferOrderLines records one delivery against a Dispatched or
// Partially Received transfer.
func ReceiveTransferOrderLines(tenantID, transferOrderID, userID string, req TransferReceiptRequest) (*TransferMovementResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}

	data, status, err := lockTransferOrderTx(tx, schema, transferOrderID)
	if err != nil {
		return nil, err
	}
	events := loadTransferEvents(data)
	if prior := replayedTransferEvent(events, req.ReceiptKey); prior != nil {
		balances, _ := loadTransferBalances(data)
		return &TransferMovementResult{TransferOrderID: transferOrderID, Status: status, EventNo: prior.No, Replayed: true, Balance: balances}, nil
	}
	if status != "Dispatched" && status != "Partially Received" {
		return nil, fmt.Errorf("transfer order must be Dispatched to receive (current status: %s)", status)
	}
	toWarehouse, _ := data["to_warehouse"].(string)
	if toWarehouse == "" {
		return nil, fmt.Errorf("transfer order is missing to_warehouse")
	}
	fromWarehouse, _ := data["from_warehouse"].(string)
	balances, err := loadTransferBalances(data)
	if err != nil {
		return nil, err
	}

	postings := map[string]*transferPosting{}
	postingFor := func(sku string) *transferPosting {
		if p, ok := postings[sku]; ok {
			return p
		}
		p := &transferPosting{}
		postings[sku] = p
		return p
	}
	reasonForSku := map[string][2]string{} // sku -> {reason_code, free-text reason}
	var eventLines []transferEventLine

	for _, line := range req.Lines {
		line.Sku = strings.TrimSpace(line.Sku)
		if line.Qty < 0 || line.DamagedQty < 0 || line.LostQty < 0 || line.ExcessQty < 0 {
			return nil, fmt.Errorf("received qty for SKU %s must be between 0 and the dispatched qty", line.Sku)
		}
		bal := findBalance(balances, line.Sku)
		if bal == nil {
			return nil, &ValidationError{Code: "STOCKT-0485", Message: fmt.Sprintf("%s is not on this transfer - receive it on its own document", line.Sku)}
		}
		if line.ReasonCode != "" || line.Reason != "" {
			reasonForSku[line.Sku] = [2]string{line.ReasonCode, line.Reason}
		}
		tracking, err := GetItemTracking(tenantID, line.Sku)
		if err != nil {
			return nil, err
		}
		if line.DamagedQty+line.LostQty+line.ExcessQty > 0 || len(line.DamagedSerials)+len(line.LostSerials) > 0 {
			if err := requireTransferReason(tenantID, line.ReasonCode, TransferDiscrepancyReasonCategory, fmt.Sprintf("the damaged / lost / excess quantity of %s", line.Sku)); err != nil {
				return nil, err
			}
		}
		p := postingFor(line.Sku)
		ev := transferEventLine{Sku: line.Sku, BatchNo: line.BatchNo, ReasonCode: line.ReasonCode, Reason: line.Reason}

		// A serial-tracked line dispatched before 73.1.7 recorded no serials,
		// so it is received by quantity like any other line.
		if tracking.TracksSerial() && len(bal.SerialStatus) > 0 {
			if line.ExcessQty > 0 {
				return nil, &ValidationError{Code: "STOCKT-0483", Message: fmt.Sprintf("%s is serial-tracked: a unit that was not sent cannot be received as excess - receive it on a goods receipt", line.Sku)}
			}
			groups := []struct {
				kind    string
				serials []string
				qty     int
			}{
				{transferSerialReceived, uniqueTrimmed(line.Serials), line.Qty},
				{transferSerialDamaged, uniqueTrimmed(line.DamagedSerials), line.DamagedQty},
				{transferSerialLost, uniqueTrimmed(line.LostSerials), line.LostQty},
			}
			for _, g := range groups {
				if g.qty != 0 && g.qty != len(g.serials) {
					return nil, &ValidationError{Code: "STOCKT-0483", Message: fmt.Sprintf("%s is serial-tracked: scan the %d serial number(s) instead of typing a quantity", line.Sku, g.qty)}
				}
				for _, sn := range g.serials {
					if bal.SerialStatus[sn] != transferSerialInTransit {
						return nil, &ValidationError{Code: "STOCKT-0483", Message: fmt.Sprintf("serial %s of %s is not in transit on this transfer (%s)", sn, line.Sku, orDash(bal.SerialStatus[sn]))}
					}
					bal.SerialStatus[sn] = g.kind
					if _, err := applyToLots(bal, bal.SerialLot[sn], 1, g.kind); err != nil {
						return nil, err
					}
					addToBalance(bal, g.kind, 1)
					p.serials = append(p.serials, transferSerialMove{serial: sn, kind: g.kind})
					p.recordArrival(g.kind, 1, bal.SerialLot[sn], sn)
					ev.Serials = append(ev.Serials, sn)
				}
				ev.add(g.kind, len(g.serials))
			}
			eventLines = append(eventLines, ev)
			continue
		}

		for _, part := range []struct {
			kind string
			qty  int
		}{{transferSerialReceived, line.Qty}, {transferSerialDamaged, line.DamagedQty}, {transferSerialLost, line.LostQty}} {
			if part.qty == 0 {
				continue
			}
			if part.qty > bal.Open() {
				return nil, &ValidationError{Code: "STOCKT-0485", Message: fmt.Sprintf(
					"received qty for SKU %s must be between 0 and the %d still in transit (got %d) - record more arriving than was sent as excess, with a reason", line.Sku, bal.Open(), part.qty)}
			}
			portions, err := applyToLots(bal, line.BatchNo, part.qty, part.kind)
			if err != nil {
				return nil, err
			}
			addToBalance(bal, part.kind, part.qty)
			for _, pr := range portions {
				p.recordArrival(part.kind, pr.qty, pr.batchNo, "")
			}
			ev.add(part.kind, part.qty)
		}
		if line.ExcessQty > 0 {
			if tracking.TracksBatch() {
				if strings.TrimSpace(line.BatchNo) == "" {
					return nil, &ValidationError{Code: "STOCKT-0482", Message: fmt.Sprintf("%s is batch-tracked: name the lot the extra units belong to", line.Sku)}
				}
				if b, _ := GetBatch(tenantID, line.Sku, line.BatchNo); b == nil {
					return nil, &ValidationError{Code: "STOCKT-0482", Message: fmt.Sprintf("lot %s of %s is not registered", line.BatchNo, line.Sku)}
				}
				found := false
				for i := range bal.Lots {
					if bal.Lots[i].BatchNo == line.BatchNo {
						bal.Lots[i].Excess += line.ExcessQty
						found = true
					}
				}
				if !found {
					bal.Lots = append(bal.Lots, transferLotBalance{BatchNo: line.BatchNo, Excess: line.ExcessQty})
				}
			}
			bal.Excess += line.ExcessQty
			p.toOnHand += line.ExcessQty
			p.toAvail += line.ExcessQty
			p.moves = append(p.moves, transferLedgerLine{qty: line.ExcessQty, batchNo: line.BatchNo, kind: "X"})
			ev.Excess += line.ExcessQty
		}
		eventLines = append(eventLines, ev)
	}

	// Without KeepOpen, whatever is still in transit is closed now as lost -
	// the original single-receipt rule, which needs a reason.
	if !req.KeepOpen {
		for i := range balances {
			bal := &balances[i]
			open := bal.Open()
			if open <= 0 {
				continue
			}
			reason := reasonForSku[bal.Sku]
			if strings.TrimSpace(reason[0]) == "" && strings.TrimSpace(reason[1]) == "" {
				return nil, &ValidationError{Code: "TRN-0259", Message: fmt.Sprintf("received quantity for SKU %s (%d) does not match dispatched quantity (%d) - a shortage/damage reason is required", bal.Sku, bal.Received, bal.Dispatched)}
			}
			if reason[0] != "" {
				if err := requireTransferReason(tenantID, reason[0], TransferDiscrepancyReasonCategory, fmt.Sprintf("the shortfall of %s", bal.Sku)); err != nil {
					return nil, err
				}
			}
			p := postingFor(bal.Sku)
			ev := transferEventLine{Sku: bal.Sku, ReasonCode: reason[0], Reason: reason[1]}
			if len(bal.SerialStatus) > 0 {
				for sn, st := range bal.SerialStatus {
					if st != transferSerialInTransit {
						continue
					}
					bal.SerialStatus[sn] = transferSerialLost
					if _, err := applyToLots(bal, bal.SerialLot[sn], 1, transferSerialLost); err != nil {
						return nil, err
					}
					p.serials = append(p.serials, transferSerialMove{serial: sn, kind: transferSerialLost})
					ev.Serials = append(ev.Serials, sn)
				}
			} else if _, err := applyToLots(bal, "", open, transferSerialLost); err != nil {
				return nil, err
			}
			addToBalance(bal, transferSerialLost, open)
			p.accounted += open
			ev.Lost = open
			eventLines = append(eventLines, ev)
		}
	}
	if len(eventLines) == 0 {
		return nil, fmt.Errorf("nothing to receive - enter at least one quantity")
	}

	for sku, p := range postings {
		if p.accounted > 0 {
			if _, err := tx.Exec(fmt.Sprintf(
				`UPDATE %s.inventory_availability SET in_transit = GREATEST(0, in_transit - $1), updated_at = CURRENT_TIMESTAMP
				 WHERE sku = $2 AND location_code = $3`, schema),
				p.accounted, sku, fromWarehouse); err != nil {
				return nil, err
			}
		}
		if p.toOnHand > 0 {
			if _, err := tx.Exec(fmt.Sprintf(
				`INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available, damaged)
				 VALUES ($1, $2, $3, $4, $5)
				 ON CONFLICT (sku, location_code) DO UPDATE SET
					on_hand = %s.inventory_availability.on_hand + EXCLUDED.on_hand,
					available = %s.inventory_availability.available + EXCLUDED.available,
					damaged = %s.inventory_availability.damaged + EXCLUDED.damaged,
					updated_at = CURRENT_TIMESTAMP`, schema, schema, schema, schema),
				sku, toWarehouse, p.toOnHand, p.toAvail, p.toDamaged); err != nil {
				return nil, err
			}
			// Stage 85.B4: received good stock lands in Transfer In's
			// default location (Main unless the client chose otherwise).
			if p.toAvail > 0 {
				if err := applyReceiptDefaultTx(tx, schema, "Transfer In", toWarehouse, []PostedStockLine{{SKU: sku, Qty: p.toAvail}}); err != nil {
					return nil, err
				}
			}
		}
		for _, sm := range p.serials {
			docID, _, err := lockSerialTx(tx, schema, sku, sm.serial)
			if err != nil {
				return nil, err
			}
			if sm.kind == transferSerialLost {
				err = patchSerialTx(tx, schema, docID, SerialScrapped, map[string]interface{}{
					"reserved_for": "", "notes": "Lost in transit on " + transferOrderID})
			} else {
				err = patchSerialTx(tx, schema, docID, SerialInStock, map[string]interface{}{
					"reserved_for": "", "current_bin": "", "location_code": toWarehouse})
			}
			if err != nil {
				return nil, err
			}
		}
	}

	ev := transferEvent{No: len(events) + 1, Kind: "Receipt", Key: strings.TrimSpace(req.ReceiptKey),
		At: time.Now().UTC().Format(time.RFC3339), By: userID, Lines: eventLines}
	newStatus := transferStatusAfter(balances)
	if err := saveTransferMovementTx(tx, schema, transferOrderID, data, balances, append(events, ev), newStatus); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	for sku, p := range postings {
		for _, mv := range p.moves {
			if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
				ItemID: sku, WarehouseID: toWarehouse, Qty: float64(mv.qty),
				VoucherType: "TransferOrder", VoucherID: transferOrderID, UserID: userID,
				FromLocationID: fromWarehouse, FromStatus: "In Transit", ToStatus: mv.toStatus,
				BatchNo: mv.batchNo, SerialNo: mv.serialNo,
				IdempotencyKey: fmt.Sprintf("TO-RECEIVE:%s:%d:%s:%s:%s:%s", transferOrderID, ev.No, sku, mv.kind, mv.batchNo, mv.serialNo),
			}); lerr != nil {
				LogSystemError(tenantID, "", "WARN", "ReceiveTransferOrder", fmt.Sprintf("stock ledger write failed for %s: %v", sku, lerr), "")
			}
		}
	}
	evJSON, _ := json.Marshal(eventLines)
	LogAuditEvent(tenantID, userID, "RECEIVE_TRANSFER_ORDER", "SUCCESS",
		fmt.Sprintf("Receipt %d of transfer order %s at %s (now %s): %s", ev.No, transferOrderID, toWarehouse, newStatus, string(evJSON)))
	return &TransferMovementResult{TransferOrderID: transferOrderID, Status: newStatus, EventNo: ev.No, Balance: balances}, nil
}

// recordArrival books one portion of a receipt into the posting.
func (p *transferPosting) recordArrival(kind string, qty int, batchNo, serialNo string) {
	switch kind {
	case transferSerialReceived:
		p.accounted += qty
		p.toOnHand += qty
		p.toAvail += qty
		p.moves = append(p.moves, transferLedgerLine{qty: qty, batchNo: batchNo, serialNo: serialNo, kind: "R"})
	case transferSerialDamaged:
		p.accounted += qty
		p.toOnHand += qty
		p.toDamaged += qty
		p.moves = append(p.moves, transferLedgerLine{qty: qty, batchNo: batchNo, serialNo: serialNo, toStatus: "Damaged", kind: "D"})
	case transferSerialLost:
		// Lost units leave in_transit and arrive nowhere: the dispatch line
		// already took them off the source's on_hand.
		p.accounted += qty
	}
}

func (e *transferEventLine) add(kind string, qty int) {
	switch kind {
	case transferSerialReceived:
		e.Received += qty
	case transferSerialDamaged:
		e.Damaged += qty
	case transferSerialLost:
		e.Lost += qty
	case transferSerialReturned:
		e.Returned += qty
	}
}

// saveTransferMovementTx writes the balance, the event log, and the
// pre-73.1.7 summary fields the print formats and older screens read
// (received_items, receive_variance).
func saveTransferMovementTx(tx *sql.Tx, schema, transferOrderID string, data map[string]interface{}, balances []transferLineBalance, events []transferEvent, newStatus string) error {
	balanceBytes, _ := json.Marshal(balances)
	eventBytes, _ := json.Marshal(events)
	type receivedLine struct {
		Sku    string `json:"sku"`
		Qty    int    `json:"qty"`
		Reason string `json:"reason,omitempty"`
	}
	// The Transfer In print shows a short line's reason: every distinct
	// reason recorded against the item, oldest first.
	reasons := map[string][]string{}
	for _, ev := range events {
		for _, l := range ev.Lines {
			for _, r := range []string{l.ReasonCode, l.Reason} {
				if r = strings.TrimSpace(r); r != "" && !containsString(reasons[l.Sku], r) {
					reasons[l.Sku] = append(reasons[l.Sku], r)
				}
			}
		}
	}
	type varianceLine struct {
		Sku        string `json:"sku"`
		Dispatched int    `json:"dispatched_qty"`
		Received   int    `json:"received_qty"`
		Shortfall  int    `json:"shortfall"`
		Damaged    int    `json:"damaged,omitempty"`
		Lost       int    `json:"lost,omitempty"`
		Excess     int    `json:"excess,omitempty"`
		Returned   int    `json:"returned,omitempty"`
	}
	var received []receivedLine
	var variances []varianceLine
	for _, b := range balances {
		received = append(received, receivedLine{Sku: b.Sku, Qty: b.Received, Reason: strings.Join(reasons[b.Sku], "; ")})
		if b.Damaged+b.Lost+b.Excess+b.Returned > 0 {
			variances = append(variances, varianceLine{Sku: b.Sku, Dispatched: b.Dispatched, Received: b.Received,
				Shortfall: b.Damaged + b.Lost, Damaged: b.Damaged, Lost: b.Lost, Excess: b.Excess, Returned: b.Returned})
		}
	}
	receivedBytes, _ := json.Marshal(received)
	data["transfer_balance"] = string(balanceBytes)
	data["transfer_events"] = string(eventBytes)
	data["received_items"] = string(receivedBytes)
	if len(variances) > 0 {
		varianceBytes, _ := json.Marshal(variances)
		data["receive_variance"] = string(varianceBytes)
	}
	data["status"] = newStatus
	updatedBytes, _ := json.Marshal(data)
	_, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'TransferOrder' AND id = $3`, schema),
		updatedBytes, newStatus, transferOrderID)
	return err
}

// ReturnTransferToSource sends units still in transit back to the source: the
// receiving store refused them, or they never left the carrier. They go back
// into the source's stock (lots and serial units with them, to be put away
// again) and leave the transfer's balance as Returned.
func ReturnTransferToSource(tenantID, transferOrderID, userID, reasonCode, note, returnKey string, lines []TransferReturnLine) (*TransferMovementResult, error) {
	if err := requireTransferReason(tenantID, reasonCode, TransferDiscrepancyReasonCategory, "a return to the source"); err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}
	data, status, err := lockTransferOrderTx(tx, schema, transferOrderID)
	if err != nil {
		return nil, err
	}
	events := loadTransferEvents(data)
	if prior := replayedTransferEvent(events, returnKey); prior != nil {
		balances, _ := loadTransferBalances(data)
		return &TransferMovementResult{TransferOrderID: transferOrderID, Status: status, EventNo: prior.No, Replayed: true, Balance: balances}, nil
	}
	if status != "Dispatched" && status != "Partially Received" {
		return nil, &ValidationError{Code: "STOCKT-0486", Message: fmt.Sprintf("only goods still in transit can be returned to the source (this transfer is %s)", status)}
	}
	fromWarehouse, _ := data["from_warehouse"].(string)
	balances, err := loadTransferBalances(data)
	if err != nil {
		return nil, err
	}

	type returnPosting struct {
		qty     int
		moves   []transferLedgerLine
		serials []string
	}
	postings := map[string]*returnPosting{}
	var eventLines []transferEventLine
	for _, line := range lines {
		bal := findBalance(balances, strings.TrimSpace(line.Sku))
		if bal == nil {
			return nil, &ValidationError{Code: "STOCKT-0485", Message: fmt.Sprintf("%s is not on this transfer", line.Sku)}
		}
		p := postings[bal.Sku]
		if p == nil {
			p = &returnPosting{}
			postings[bal.Sku] = p
		}
		ev := transferEventLine{Sku: bal.Sku, BatchNo: line.BatchNo, ReasonCode: reasonCode, Reason: note}
		if len(bal.SerialStatus) > 0 {
			serials := uniqueTrimmed(line.Serials)
			if len(serials) == 0 || (line.Qty != 0 && line.Qty != len(serials)) {
				return nil, &ValidationError{Code: "STOCKT-0483", Message: fmt.Sprintf("%s is serial-tracked: list the serial numbers going back", bal.Sku)}
			}
			for _, sn := range serials {
				if bal.SerialStatus[sn] != transferSerialInTransit {
					return nil, &ValidationError{Code: "STOCKT-0483", Message: fmt.Sprintf("serial %s of %s is not in transit on this transfer (%s)", sn, bal.Sku, orDash(bal.SerialStatus[sn]))}
				}
				bal.SerialStatus[sn] = transferSerialReturned
				if _, err := applyToLots(bal, bal.SerialLot[sn], 1, transferSerialReturned); err != nil {
					return nil, err
				}
				addToBalance(bal, transferSerialReturned, 1)
				p.qty++
				p.serials = append(p.serials, sn)
				p.moves = append(p.moves, transferLedgerLine{qty: 1, batchNo: bal.SerialLot[sn], serialNo: sn, kind: "T"})
			}
			ev.Returned = len(serials)
			ev.Serials = serials
			eventLines = append(eventLines, ev)
			continue
		}
		if line.Qty <= 0 || line.Qty > bal.Open() {
			return nil, &ValidationError{Code: "STOCKT-0485", Message: fmt.Sprintf("%d unit(s) of %s are still in transit; a return must be between 1 and that", bal.Open(), bal.Sku)}
		}
		portions, err := applyToLots(bal, line.BatchNo, line.Qty, transferSerialReturned)
		if err != nil {
			return nil, err
		}
		addToBalance(bal, transferSerialReturned, line.Qty)
		p.qty += line.Qty
		for _, pr := range portions {
			p.moves = append(p.moves, transferLedgerLine{qty: pr.qty, batchNo: pr.batchNo, kind: "T"})
		}
		ev.Returned = line.Qty
		eventLines = append(eventLines, ev)
	}
	if len(eventLines) == 0 {
		return nil, fmt.Errorf("nothing to return - enter at least one line")
	}
	for sku, p := range postings {
		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.inventory_availability SET in_transit = GREATEST(0, in_transit - $1),
				on_hand = on_hand + $1, available = available + $1, updated_at = CURRENT_TIMESTAMP
			 WHERE sku = $2 AND location_code = $3`, schema),
			p.qty, sku, fromWarehouse); err != nil {
			return nil, err
		}
		for _, sn := range p.serials {
			docID, _, err := lockSerialTx(tx, schema, sku, sn)
			if err != nil {
				return nil, err
			}
			if err := patchSerialTx(tx, schema, docID, SerialInStock, map[string]interface{}{
				"reserved_for": "", "current_bin": "", "location_code": fromWarehouse}); err != nil {
				return nil, err
			}
		}
	}
	ev := transferEvent{No: len(events) + 1, Kind: "Return to source", Key: strings.TrimSpace(returnKey),
		At: time.Now().UTC().Format(time.RFC3339), By: userID, Lines: eventLines}
	newStatus := transferStatusAfter(balances)
	if err := saveTransferMovementTx(tx, schema, transferOrderID, data, balances, append(events, ev), newStatus); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	toWarehouse, _ := data["to_warehouse"].(string)
	for sku, p := range postings {
		for _, mv := range p.moves {
			if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
				ItemID: sku, WarehouseID: fromWarehouse, Qty: float64(mv.qty),
				VoucherType: "TransferOrder", VoucherID: transferOrderID, UserID: userID,
				FromLocationID: toWarehouse, FromStatus: "In Transit", BatchNo: mv.batchNo, SerialNo: mv.serialNo,
				IdempotencyKey: fmt.Sprintf("TO-RETURN:%s:%d:%s:%s:%s", transferOrderID, ev.No, sku, mv.batchNo, mv.serialNo),
			}); lerr != nil {
				LogSystemError(tenantID, "", "WARN", "ReturnTransferToSource", fmt.Sprintf("stock ledger write failed for %s: %v", sku, lerr), "")
			}
		}
	}
	evJSON, _ := json.Marshal(eventLines)
	LogAuditEvent(tenantID, userID, "RETURN_TRANSFER_TO_SOURCE", "SUCCESS",
		fmt.Sprintf("Transfer order %s: returned to %s (reason %s, now %s): %s", transferOrderID, fromWarehouse, reasonCode, newStatus, string(evJSON)))
	return &TransferMovementResult{TransferOrderID: transferOrderID, Status: newStatus, EventNo: ev.No, Balance: balances}, nil
}

// CancelTransferOrder cancels a transfer before it is dispatched. Nothing has
// moved yet, so nothing is reversed; once goods are in transit the way back
// is ReturnTransferToSource.
func CancelTransferOrder(tenantID, transferOrderID, userID, reasonCode, note string) error {
	if strings.TrimSpace(reasonCode) == "" {
		return &ValidationError{Code: "STOCKT-0484", Message: "cancelling a transfer needs a reason code (Reason Codes, category \"Cancellation\")"}
	}
	if err := requireActiveReasonCode(tenantID, reasonCode, "Cancellation"); err != nil {
		return &ValidationError{Code: "STOCKT-0484", Message: fmt.Sprintf("cancelling a transfer: %v", err)}
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
	data, status, err := lockTransferOrderTx(tx, schema, transferOrderID)
	if err != nil {
		return err
	}
	switch status {
	case "Draft", "Pending Approval", "Approved", "Packed":
	default:
		return &ValidationError{Code: "STOCKT-0486", Message: fmt.Sprintf(
			"a %s transfer cannot be cancelled - once dispatched, send what is still in transit back with Return to source", status)}
	}
	data["status"] = "Cancelled"
	data["cancel_reason_code"] = reasonCode
	data["cancel_note"] = strings.TrimSpace(note)
	data["cancelled_by"] = userID
	data["cancelled_at"] = time.Now().UTC().Format(time.RFC3339)
	data["cancelled_from_status"] = status
	updatedBytes, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Cancelled', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'TransferOrder' AND id = $2`, schema),
		updatedBytes, transferOrderID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "CANCEL_TRANSFER_ORDER", "SUCCESS",
		fmt.Sprintf("Cancelled transfer order %s from %s (reason %s)%s", transferOrderID, status, reasonCode, reasonSuffix(note)))
	return nil
}

// TransferValue is a transfer's value at item cost (moving average, else the
// item's standard cost). Complete is false when any line's item has no cost
// on record - such a transfer can never be approved directly.
type TransferValue struct {
	ValuePaise int64    `json:"value_paise"`
	Value      float64  `json:"value"`
	Complete   bool     `json:"complete"`
	Uncosted   []string `json:"uncosted,omitempty"`
	LimitValue float64  `json:"direct_approval_limit"`
}

func transferValueOf(tenantID string, lines []transferLine) TransferValue {
	v := TransferValue{Complete: true}
	for _, l := range lines {
		cost, ok := ResolveQuoteUnitCostPaise(tenantID, l.Sku)
		if !ok {
			v.Complete = false
			v.Uncosted = append(v.Uncosted, l.Sku)
			continue
		}
		v.ValuePaise += cost * int64(l.Qty)
	}
	v.Value = PaiseToRupees(v.ValuePaise)
	v.LimitValue = GetSettingFloat(tenantID, TransferDirectApprovalLimitSetting)
	return v
}

// TransferDetail is what the transfer screen needs before approving or
// dispatching: the value against the direct-approval limit, and how each
// line's item is tracked (so dispatch asks for serials or lots only where
// they are needed).
type TransferDetail struct {
	TransferValue
	Lines []TransferDetailLine `json:"lines"`
}

type TransferDetailLine struct {
	Sku          string `json:"sku"`
	Name         string `json:"name,omitempty"`
	Qty          int    `json:"qty"`
	TrackingMode string `json:"tracking_mode"`
}

// GetTransferDetail reads a transfer's value and line tracking.
func GetTransferDetail(tenantID, transferOrderID string) (TransferDetail, error) {
	data, _, err := fetchDocData(tenantID, "TransferOrder", transferOrderID)
	if err != nil {
		return TransferDetail{}, err
	}
	itemsStr, _ := data["items"].(string)
	lines, err := parseTransferItems(itemsStr)
	if err != nil {
		return TransferDetail{}, err
	}
	out := TransferDetail{TransferValue: transferValueOf(tenantID, lines)}
	for _, l := range lines {
		row := TransferDetailLine{Sku: l.Sku, Qty: l.Qty, TrackingMode: TrackingNone}
		if tr, err := GetItemTracking(tenantID, l.Sku); err == nil && tr.Mode != "" {
			row.TrackingMode = tr.Mode
		}
		if item, _, err := fetchDocData(tenantID, "Item", l.Sku); err == nil {
			row.Name, _ = item["name"].(string)
		}
		out.Lines = append(out.Lines, row)
	}
	return out, nil
}

// GetTransferValue values a transfer for the approval screen.
func GetTransferValue(tenantID, transferOrderID string) (TransferValue, error) {
	data, _, err := fetchDocData(tenantID, "TransferOrder", transferOrderID)
	if err != nil {
		return TransferValue{}, err
	}
	itemsStr, _ := data["items"].(string)
	lines, err := parseTransferItems(itemsStr)
	if err != nil {
		return TransferValue{}, err
	}
	return transferValueOf(tenantID, lines), nil
}

// stampTransferValue writes the value onto the document as total_amount, the
// field the approval engine routes slabs by.
func stampTransferValue(tenantID, transferOrderID string) (TransferValue, error) {
	v, err := GetTransferValue(tenantID, transferOrderID)
	if err != nil {
		return v, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return v, err
	}
	patch, _ := json.Marshal(map[string]interface{}{
		"total_amount": v.Value, "transfer_value_complete": v.Complete,
	})
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'TransferOrder' AND id = $2`, schema),
		patch, transferOrderID)
	return v, err
}

// SubmitTransferOrderForApproval values a Draft transfer and sends it to the
// approval engine (D04: maker-checker by default).
func SubmitTransferOrderForApproval(tenantID, transferOrderID, userID, role string) error {
	if _, status, err := fetchDocData(tenantID, "TransferOrder", transferOrderID); err != nil {
		return fmt.Errorf("transfer order not found: %v", err)
	} else if status != "Draft" {
		return fmt.Errorf("only a Draft transfer can be submitted for approval (current status: %s)", status)
	}
	if _, err := stampTransferValue(tenantID, transferOrderID); err != nil {
		return err
	}
	if err := SubmitForApproval(tenantID, "TransferOrder", transferOrderID, userID, role); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "SUBMIT_TRANSFER_ORDER", "SUCCESS", fmt.Sprintf("Submitted transfer order %s for approval", transferOrderID))
	return nil
}

// ApproveTransferOrderDirect is D04's low-value path: a Store Manager of the
// sending or receiving store approves a transfer worth less than the client's
// limit, without a second person. Anything else goes through
// SubmitTransferOrderForApproval and DecideApproval's maker-checker.
func ApproveTransferOrderDirect(tenantID, transferOrderID, userID, role, userLocation string) (TransferValue, error) {
	refuse := func(v TransferValue, msg string) (TransferValue, error) {
		return v, &ValidationError{Code: "STOCKT-0481", Message: msg}
	}
	if !HasFullAccess(role) && CanonicalRole(role) != "Store Manager" {
		return refuse(TransferValue{}, "only a Store Manager can approve a transfer directly - submit it for approval instead")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return TransferValue{}, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return TransferValue{}, err
	}
	defer tx.Rollback()
	data, status, err := lockTransferOrderTx(tx, schema, transferOrderID)
	if err != nil {
		return TransferValue{}, err
	}
	if status != "Draft" && status != "Pending Approval" {
		return refuse(TransferValue{}, fmt.Sprintf("only a Draft or Pending Approval transfer can be approved (current status: %s)", status))
	}
	from, _ := data["from_warehouse"].(string)
	to, _ := data["to_warehouse"].(string)
	if !HasFullAccess(role) && userLocation != "" && userLocation != from && userLocation != to {
		return refuse(TransferValue{}, "you can approve directly only a transfer from or to your own store")
	}
	itemsStr, _ := data["items"].(string)
	lines, err := parseTransferItems(itemsStr)
	if err != nil {
		return TransferValue{}, err
	}
	v := transferValueOf(tenantID, lines)
	switch {
	case v.LimitValue <= 0:
		return refuse(v, "direct approval of transfers is switched off for this business - submit it for approval")
	case !v.Complete:
		return refuse(v, fmt.Sprintf("the value of this transfer is not known (no cost on record for %s) - submit it for approval", strings.Join(v.Uncosted, ", ")))
	case v.Value >= v.LimitValue:
		return refuse(v, fmt.Sprintf("this transfer is worth ₹%.2f, at or above the ₹%.2f direct-approval limit - submit it for approval", v.Value, v.LimitValue))
	}
	data["status"] = "Approved"
	data["total_amount"] = v.Value
	data["transfer_value_complete"] = true
	data["approved_by"] = userID
	data["approval_mode"] = "Direct (below limit)"
	updatedBytes, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Approved', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'TransferOrder' AND id = $2`, schema),
		updatedBytes, transferOrderID); err != nil {
		return v, err
	}
	comment := fmt.Sprintf("Direct approval: ₹%.2f is below the ₹%.2f limit", v.Value, v.LimitValue)
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.approval_log (doctype, document_id, action, actor_user_id, actor_role, amount, comment)
		VALUES ('TransferOrder', $1, 'Approved', $2, $3, $4, $5)`, schema),
		transferOrderID, userID, role, v.Value, comment); err != nil {
		return v, err
	}
	if err := tx.Commit(); err != nil {
		return v, err
	}
	LogAuditEvent(tenantID, userID, "APPROVE_TRANSFER_ORDER_DIRECT", "SUCCESS", fmt.Sprintf("Transfer order %s: %s", transferOrderID, comment))
	return v, nil
}
