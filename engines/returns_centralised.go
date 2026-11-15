package engines

// Stage 77.8 (85.E4): a centralised store return. With returns.centralised on,
// a bill sold at store A and brought back at store B is booked at A - its
// Sales Return number, the stock it puts back and its credit note are A's -
// and B is kept on the return as collected_at, so B lists it and pays any cash
// from its own till. Once the return is inspected the goods move on paper the
// way they moved in fact: a transfer A -> B is made, dispatched from where
// each line landed at A and received at B - Good lines into Inward, Damage and
// QC Hold lines into the same pool at B. Off (the default), a return is booked
// where it is taken, as before.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"custom_erp/db"
)

const SettingCentralisedReturn = "returns.centralised"

func init() {
	RegisterSetting(SettingDefinition{Key: SettingCentralisedReturn, Module: "OMS", Label: "Book a return at the store that sold it",
		Type: SettingTypeBool, Default: "false",
		Description: "On: a bill returned at another store is booked at the store that sold it, and the goods come to the store that took them on a transfer, received into Inward."})
}

// centraliseReturnDoc books a return of a bill sold at soldAt there, keeping
// the store that took it as collected_at. Nothing changes when the setting is
// off, the bill has no store, or it comes back where it was sold.
func centraliseReturnDoc(tenantID string, doc map[string]interface{}, soldAt string) {
	taken := strField(doc, "return_location")
	if soldAt == "" || taken == "" || soldAt == taken || !GetSettingBool(tenantID, SettingCentralisedReturn) {
		return
	}
	doc["return_location"] = soldAt
	doc["collected_at"] = taken
}

// refuseCentralisedExchange stops an exchange on a return booked at another
// store: the new item would come out of the selling store's stock while the
// customer takes it from this store's shelf.
func refuseCentralisedExchange(data map[string]interface{}, exchangeFor []map[string]string) error {
	at := strField(data, "collected_at")
	if at == "" || at == strField(data, "return_location") || len(exchangeFor) == 0 || len(exchangeFor[0]) == 0 {
		return nil
	}
	return fmt.Errorf("this return is booked at %s, the store that sold the bill - refund it and sell the new item on a new bill here", strField(data, "return_location"))
}

// centralisedLine is the part of a return line the transfer needs, read
// from the saved JSON so it does not depend on the in-memory record type.
type centralisedLine struct {
	SKU         string   `json:"sku"`
	Qty         int      `json:"qty"`
	Disposition string   `json:"disposition"`
	NextAction  string   `json:"next_action"`
	BatchNo     string   `json:"batch_no"`
	SerialNos   []string `json:"serial_nos"`
	Parts       []struct {
		SKU string `json:"sku"`
		Qty int    `json:"qty"`
	} `json:"parts"`
}

// centralisedLeg is one transfer of the chain: the goods that landed in one
// location at the selling store and where they land at the taking store.
type centralisedLeg struct {
	From  string
	To    string
	Lines []transferLine
}

// TransferCentralisedReturn makes, dispatches and receives the transfers of a
// centralised return once it has been inspected. It is safe to run again: the
// transfers are made once (recorded on the return) and each step is skipped
// when its transfer is already past it. A step that fails leaves its transfer
// on the Transfers screen to finish by hand, and the return says so.
func TransferCentralisedReturn(tenantID, returnID, user string) ([]string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	ids, legs, err := makeCentralisedTransfers(tenantID, schema, returnID)
	if err != nil {
		noteCentralisedTransfer(schema, returnID, err)
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var firstErr error
	for _, id := range ids {
		if err := moveCentralisedTransfer(tenantID, schema, returnID, id, legs[id], user); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("transfer %s: %w", id, err)
		}
	}
	noteCentralisedTransfer(schema, returnID, firstErr)
	return ids, firstErr
}

// makeCentralisedTransfers records the chain's transfers on the return, once,
// and returns them with where each one's goods land at the taking store.
func makeCentralisedTransfers(tenantID, schema, returnID string) ([]string, map[string]string, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	var raw []byte
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'ReturnRequest' AND id = $1 FOR UPDATE`, schema), returnID).Scan(&raw); err != nil {
		return nil, nil, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, nil, err
	}
	soldAt, takenAt := strField(data, "return_location"), strField(data, "collected_at")
	if takenAt == "" || takenAt == soldAt {
		return nil, nil, nil
	}
	// Already made: resume with the recorded transfers.
	if made, ok := data["transfer_orders"].([]interface{}); ok && len(made) > 0 {
		ids := make([]string, 0, len(made))
		legs := map[string]string{}
		for _, m := range made {
			if e, ok := m.(map[string]interface{}); ok && strField(e, "id") != "" {
				ids = append(ids, strField(e, "id"))
				legs[strField(e, "id")] = strField(e, "stock_to")
			}
		}
		return ids, legs, nil
	}
	if s := strField(data, "status"); s == "Requested" || s == "Approved" || s == "Received" || s == "Rejected" || s == "Cancelled" {
		return nil, nil, nil // not inspected: nothing has come back into stock yet
	}
	var lines []centralisedLine
	b, _ := json.Marshal(data["items"])
	if err := json.Unmarshal(b, &lines); err != nil {
		return nil, nil, err
	}
	legs, skipped := centralisedLegs(tx, schema, soldAt, takenAt, returnID, lines)

	number := strField(data, "code")
	made := make([]interface{}, 0, len(legs))
	ids := make([]string, 0, len(legs))
	stockTo := map[string]string{}
	for _, leg := range legs {
		id := NewDocID("TO")
		items, _ := json.Marshal(leg.Lines)
		doc := map[string]interface{}{
			"from_warehouse": soldAt, "to_warehouse": takenAt, "items": string(items), "status": "Approved",
			"stock_from": leg.From, "stock_to": leg.To, "source_return": returnID, "source_return_no": number,
		}
		issueNumberInto(doc, tenantID, "TransferOrder", id, "", soldAt)
		if err := ValidateDocument(tenantID, "TransferOrder", doc); err != nil {
			return nil, nil, err
		}
		docRaw, _ := json.Marshal(doc)
		// created_by stays 'system' (users foreign key), as for the return itself.
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'TransferOrder', $2, 'Approved', 'system')`, schema), id, docRaw); err != nil {
			return nil, nil, err
		}
		made = append(made, map[string]interface{}{"id": id, "number": strField(doc, "transfer_number"), "stock_from": leg.From, "stock_to": leg.To})
		ids = append(ids, id)
		stockTo[id] = leg.To
	}
	if len(made) == 0 && skipped == "" {
		return nil, nil, nil // nothing came back into stock (Missing / Rejected lines)
	}
	data["transfer_orders"] = made
	if skipped != "" {
		data["transfer_note"] = skipped
	}
	out, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ReturnRequest' AND id = $2`, schema), out, returnID); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return ids, stockTo, nil
}

// centralisedLegs groups the lines that came back into stock at the selling
// store by the location they landed in - the same rule ApplyReturnQC posts
// by - and names where each group lands at the taking store. A group whose
// location the selling store does not use cannot be drawn by a transfer; it
// is named in the note instead, to move by hand.
func centralisedLegs(tx *sql.Tx, schema, soldAt, takenAt, returnID string, lines []centralisedLine) ([]centralisedLeg, string) {
	good := centralisedGoodLanding(tx, schema, soldAt, returnID)
	byFrom := map[string]*centralisedLeg{}
	var order []string
	var skipped []string
	add := func(from, sku string, qty int, batch string, serials []string) {
		if qty <= 0 || sku == "" {
			return
		}
		if from != "MAIN" && storeLocationFor(schema, soldAt, from) == nil {
			skipped = append(skipped, fmt.Sprintf("%d x %s in %s", qty, sku, from))
			return
		}
		leg, ok := byFrom[from]
		if !ok {
			to := "INWARD"
			if from == "DAMAGE" || from == "QCHOLD" {
				to = from
			}
			if storeLocationFor(schema, takenAt, to) == nil {
				to = "INWARD" // a pool the taking store does not use: Inward, never the shelf
			}
			leg = &centralisedLeg{From: from, To: to}
			byFrom[from] = leg
			order = append(order, from)
		}
		for i := range leg.Lines {
			if leg.Lines[i].Sku == sku { // one line per item on a transfer
				leg.Lines[i].Qty += qty
				leg.Lines[i].Serials = append(leg.Lines[i].Serials, serials...)
				if batch != "" {
					leg.Lines[i].Lots = append(leg.Lines[i].Lots, transferLot{BatchNo: batch, Qty: qty})
				}
				return
			}
		}
		l := transferLine{Sku: sku, Qty: qty, Serials: serials}
		if batch != "" {
			l.Lots = []transferLot{{BatchNo: batch, Qty: qty}}
		}
		leg.Lines = append(leg.Lines, l)
	}
	for _, l := range lines {
		rule, ok := returnDispositionRule[l.Disposition]
		if !ok || !rule.ReceivesStock {
			continue
		}
		bucket := rule.Bucket
		if l.NextAction == returnActionScrap {
			bucket = "qc_hold"
		}
		from := map[string]string{"available": good, "damaged": "DAMAGE", "qc_hold": "QCHOLD"}[bucket]
		if from == "" {
			continue
		}
		if len(l.Parts) > 0 { // a bundle came back as its components (148.1d)
			for _, p := range l.Parts {
				add(from, p.SKU, p.Qty, "", nil)
			}
			continue
		}
		add(from, l.SKU, l.Qty, l.BatchNo, l.SerialNos)
	}
	legs := make([]centralisedLeg, 0, len(order))
	for _, f := range order {
		sort.SliceStable(byFrom[f].Lines, func(i, j int) bool { return byFrom[f].Lines[i].Sku < byFrom[f].Lines[j].Sku })
		legs = append(legs, *byFrom[f])
	}
	note := ""
	if len(skipped) > 0 {
		note = fmt.Sprintf("Not moved to %s - %s does not use the location: %s.", takenAt, soldAt, strings.Join(skipped, ", "))
	}
	return legs, note
}

// centralisedGoodLanding is where a Good return landed at the store: the
// return's own To, else the Sales Return setting, else Main - exactly as
// applyReceiptDefaultTx placed it.
func centralisedGoodLanding(tx *sql.Tx, schema, store, returnID string) string {
	to := stockDefaultTo(schema, "Sales Return")
	if o := stockOverride("Sales Return", false, []string{docStockField(tx, schema, returnID, "stock_to")}); o != "" {
		to = o
	}
	if to == "" || to == "MAIN" {
		return "MAIN"
	}
	if l := storeLocationFor(schema, store, to); l == nil || l.Bucket == "available" {
		return "MAIN"
	}
	return to
}

// moveCentralisedTransfer dispatches one transfer of the chain and receives
// it in full at the taking store, skipping a step already done.
func moveCentralisedTransfer(tenantID, schema, returnID, toID, stockTo, user string) error {
	status, lines, err := centralisedTransferState(schema, toID)
	if err != nil {
		return err
	}
	if status == "Approved" || status == "Packed" {
		if err := DispatchTransferOrderOpts(tenantID, toID, user, DispatchOptions{Picks: lines, internal: true}); err != nil {
			// A second run may have dispatched it first.
			if s, _, _ := centralisedTransferState(schema, toID); s != "Dispatched" {
				return err
			}
		}
		status = "Dispatched"
	}
	if status != "Dispatched" {
		return nil // received already, or taken over by hand
	}
	req := TransferReceiptRequest{ReceiptKey: "return:" + returnID + ":" + toID, StockTo: stockTo, internal: true}
	for _, l := range lines {
		rl := TransferReceiptLine{Sku: l.Sku, Qty: l.Qty, Serials: l.Serials}
		if len(l.Lots) == 1 {
			rl.BatchNo = l.Lots[0].BatchNo
		}
		req.Lines = append(req.Lines, rl)
	}
	_, err = ReceiveTransferOrderLines(tenantID, toID, user, req)
	return err
}

func centralisedTransferState(schema, toID string) (string, []transferLine, error) {
	var status, items string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status, COALESCE(data->>'items', '') FROM %s.documents WHERE doctype = 'TransferOrder' AND id = $1`, schema), toID).Scan(&status, &items); err != nil {
		return "", nil, err
	}
	lines, err := parseTransferItems(items)
	return status, lines, err
}

// noteCentralisedTransfer records on the return whether its goods reached the
// taking store, so the Returns list can say so.
func noteCentralisedTransfer(schema, returnID string, failed error) {
	state := "Received"
	if failed != nil {
		state = "Open: " + failed.Error()
		log.Printf("return %s: centralised transfer not finished: %v", returnID, failed)
	}
	patch, _ := json.Marshal(map[string]interface{}{"transfer_state": state})
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'ReturnRequest' AND id = $2 AND data->>'collected_at' IS NOT NULL`, schema), patch, returnID)
}
