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

// Stage 130.16 (74.3.1, BRD 4.1, DEC-01/02): the Delivery Challan - goods
// that leave a store without being sold: to a job worker, for repair, to an
// exhibition, as a sample, or given away.
//
// Returnable goods stay ours. At dispatch they leave the store's available
// stock into its "with party" pool - still in on_hand, so stock value and GL
// do not move, but not on the shelf (a cycle count does not expect them) -
// and the challan keeps what each counterparty holds, per line, lot and
// serial: issued = returned good + damaged + rejected + lost + kept + open.
// Returns are partial and repeated, each an event with its own key so a
// retried receipt is never taken twice (73.1.7's balance and replay
// pattern); good comes back to available, damaged to the damaged pool,
// rejected to QC hold, and a returned serial must be one that went out. What
// never comes back stays open until a Challan Write-off is approved (Lost:
// the adjustment-loss account, or job-work loss; Kept by the party: the
// non-returnable challan expense) - never an automatic close.
//
// Non-returnable goods leave stock at dispatch and their cost goes to the
// non-returnable challan posting profile's expense.
//
// The e-way bill decision is worked out and kept on the challan with its
// rule version (generating the bill is 74.4).

const (
	DeliveryChallanDoctype = "DeliveryChallan"
	ChallanWriteOffDoctype = "ChallanWriteOff"
	SettingEWBThreshold    = "gst.ewb_threshold"
	ewbRuleVersion         = "EWB-R138-2026.1"
	challanKindLost        = "Lost"
	challanKindKept        = "Kept by party"
	// The chart's own accounts (74.1.3): the posting profiles' defaults.
	accountChallanExpense = "5440" // Samples / Non-returnable Challan Expense
	accountJobworkLoss    = "5430" // Job-Work Loss
	accountJobworkCharge  = "5420" // Job-Work Conversion Cost
)

// challanPurposes are DEC-01's purposes and whether each is returnable by
// default; job work and repair always are.
var challanPurposes = map[string]bool{"Job work": true, "Repair": true, "Exhibition": true, "Sample": true,
	"Returnable - other": true, "Non-returnable": false, "Other": false}

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingEWBThreshold, Module: "Inventory",
		Label: "E-way bill needed from (consignment value)", Type: SettingTypeFloat, Default: "50000", Unit: "₹",
		Min:         settingBound(0),
		Description: "A delivery challan whose goods are worth this much or more is marked as needing an e-way bill. Job work sent to another state needs one whatever the value.",
	})
	RegisterApprovalDecisionHook(DeliveryChallanDoctype, false, func(tenantID, docID, decision, actorUserID string) {})
	RegisterApprovalDecisionHook(ChallanWriteOffDoctype, true, func(tenantID, docID, decision, actorUserID string) {
		if decision == "Approved" {
			executeChallanWriteOff(tenantID, docID, actorUserID)
		}
	})
	RegisterReport(ReportDefinition{ID: "delivery-challan-register", Label: "Delivery challan register", Category: "Inventory",
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "date", Label: "Date"}, {Key: "number", Label: "Challan"}, {Key: "purpose", Label: "Purpose"}, {Key: "location", Label: "Store"},
			{Key: "party", Label: "Party"}, {Key: "sku", Label: "Item"}, {Key: "batch_no", Label: "Lot"}, {Key: "issued", Label: "Issued"}, {Key: "returned", Label: "Back good"},
			{Key: "damaged", Label: "Back damaged"}, {Key: "rejected", Label: "Back rejected"}, {Key: "written_off", Label: "Written off"}, {Key: "open", Label: "Still out"},
			{Key: "expected_return", Label: "Expected back"}, {Key: "status", Label: "Status"}, {Key: "ewb_required", Label: "E-way bill"}},
		Run: deliveryChallanRegister})
	RegisterReport(ReportDefinition{ID: "challan-outstanding-returnables", Label: "Goods out on challan, by party", Category: "Inventory",
		Columns: []ReportColumn{{Key: "party", Label: "Party"}, {Key: "challans", Label: "Challans"}, {Key: "open_qty", Label: "Still out"}, {Key: "open_value", Label: "Value"},
			{Key: "not_due", Label: "Not due"}, {Key: "overdue_30", Label: "Overdue up to 30 days"}, {Key: "overdue_60", Label: "31-60 days"}, {Key: "overdue_more", Label: "Over 60 days"}},
		Run: challanOutstandingByParty, StoreKey: "-"})
}

func challanFail(sub, msg string) error {
	return &ValidationError{Code: "INVENT-1318", SubFor: sub, Message: msg}
}

// ChallanLineInput is one item on a challan.
type ChallanLineInput struct {
	SKU            string   `json:"sku"`
	Qty            int      `json:"qty"`
	BatchNo        string   `json:"batch_no"`
	Serials        []string `json:"serials"`
	GrossWeight    float64  `json:"gross_weight"`
	NetWeight      float64  `json:"net_weight"`
	DeclaredValue  float64  `json:"declared_value"`
	ExpectedReturn string   `json:"expected_return"`
	Condition      string   `json:"condition"`
}

// ChallanInput is a new challan.
type ChallanInput struct {
	Location       string             `json:"location"`
	Purpose        string             `json:"purpose"`
	Returnable     *bool              `json:"returnable"`
	PartyType      string             `json:"party_type"`
	Party          string             `json:"party"`
	PartyName      string             `json:"party_name"`
	PartyGSTIN     string             `json:"party_gstin"`
	PartyAddress   string             `json:"party_address"`
	PartyState     string             `json:"party_state"`
	ExpectedReturn string             `json:"expected_return"`
	Reference      string             `json:"reference"`
	Note           string             `json:"note"`
	Lines          []ChallanLineInput `json:"lines"`
}

// challanBalance is what one line (item and lot) owes.
type challanBalance struct {
	SKU      string `json:"sku"`
	BatchNo  string `json:"batch_no,omitempty"`
	Issued   int    `json:"issued"`
	Returned int    `json:"returned"`
	Damaged  int    `json:"damaged"`
	Rejected int    `json:"rejected"`
	Lost     int    `json:"lost"`
	Kept     int    `json:"kept"`
	// Converted (Stage 130.16, 74.3.3): consumed by job work into another
	// item - it will not come back as itself.
	Converted int               `json:"converted,omitempty"`
	UnitCost  float64           `json:"unit_cost"`
	Serials   map[string]string `json:"serials,omitempty"` // serial -> With party / Returned / Damaged / Rejected / Lost / Kept
}

func (b challanBalance) Open() int {
	return b.Issued - b.Returned - b.Damaged - b.Rejected - b.Lost - b.Kept - b.Converted
}

type challanEvent struct {
	Key   string                   `json:"key,omitempty"`
	Kind  string                   `json:"kind"`
	At    string                   `json:"at"`
	By    string                   `json:"by"`
	Lines []map[string]interface{} `json:"lines,omitempty"`
}

func challanState(data map[string]interface{}) ([]challanBalance, []challanEvent) {
	var bal []challanBalance
	var events []challanEvent
	_ = json.Unmarshal([]byte(strField(data, "challan_balance")), &bal)
	_ = json.Unmarshal([]byte(strField(data, "challan_events")), &events)
	return bal, events
}

func findChallanBalance(bal []challanBalance, sku, batch string) *challanBalance {
	for i := range bal {
		if bal[i].SKU == sku && (batch == "" || bal[i].BatchNo == batch) {
			return &bal[i]
		}
	}
	return nil
}

func challanStatusAfter(bal []challanBalance) string {
	open, writtenOff := 0, 0
	for _, b := range bal {
		open += b.Open()
		writtenOff += b.Lost + b.Kept
	}
	switch {
	case open > 0:
		return "Partly Returned"
	case writtenOff > 0:
		return "Closed"
	}
	return "Returned"
}

// stateCodeOf is a party's state code: its GSTIN's first two digits, else
// the state as given.
func stateCodeOf(gstin, state string) string {
	if g := strings.TrimSpace(gstin); len(g) >= 2 {
		return g[:2]
	}
	return strings.ToUpper(strings.TrimSpace(state))
}

// challanEWBDecision is the e-way bill decision and its evidence.
func challanEWBDecision(tenantID, location, purpose, partyGSTIN, partyState string, value float64) map[string]interface{} {
	threshold := GetSettingFloat(tenantID, SettingEWBThreshold)
	from := LoadPrintStore(tenantID, location)
	fromState, toState := stateCodeOf(from.GSTIN, firstNonEmpty(from.StateCode, from.State)), stateCodeOf(partyGSTIN, partyState)
	interstate := fromState != "" && toState != "" && fromState != toState
	out := map[string]interface{}{"rule_version": ewbRuleVersion, "value": round2(value), "threshold": threshold, "interstate": interstate, "required": false}
	switch {
	case purpose == "Job work" && interstate:
		out["required"], out["reason"] = true, "job work sent to another state needs an e-way bill whatever the value"
	case value >= threshold:
		out["required"], out["reason"] = true, fmt.Sprintf("goods worth %.2f, at or over %.2f", value, threshold)
	default:
		out["reason"] = fmt.Sprintf("goods worth %.2f, under %.2f", value, threshold)
	}
	return out
}

// CreateDeliveryChallan saves a challan: Approved at once, or sent for
// approval when a rule covers DeliveryChallan.
func CreateDeliveryChallan(tenantID string, in ChallanInput, userID, role string) (string, string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", err
	}
	location := DocumentStoreCode(tenantID, in.Location)
	if location == "" {
		return "", "", challanFail("location", "choose the store the goods leave from")
	}
	purpose := strings.TrimSpace(in.Purpose)
	defaultReturnable, known := challanPurposes[purpose]
	if !known {
		return "", "", challanFail("purpose", "choose what the goods are going out for (job work, repair, exhibition, sample, returnable, non-returnable or other)")
	}
	returnable := defaultReturnable
	if in.Returnable != nil {
		returnable = *in.Returnable
	}
	if (purpose == "Job work" || purpose == "Repair") && !returnable {
		return "", "", challanFail("returnable", purpose+" goods come back - the challan is returnable")
	}
	if purpose == "Non-returnable" && returnable {
		returnable = false
	}
	partyName := firstNonEmpty(strings.TrimSpace(in.PartyName), strings.TrimSpace(in.Party))
	if partyName == "" {
		return "", "", challanFail("party", "say who the goods go to")
	}
	if len(in.Lines) == 0 {
		return "", "", challanFail("lines", "add at least one item")
	}
	headerReturn := strings.TrimSpace(in.ExpectedReturn)
	var lines []map[string]interface{}
	var declared, cost float64
	seen := map[string]bool{}
	for _, l := range in.Lines {
		item, err := ResolveItemBySKU(tenantID, strings.TrimSpace(l.SKU))
		if err != nil || item == nil {
			return "", "", challanFail(l.SKU, fmt.Sprintf("no item %q", l.SKU))
		}
		sku := firstNonEmpty(strField(item.Data, "code"), item.ID)
		if ItemPolicyFor(tenantID, item.Data).StockTracking == StockNotTracked {
			return "", "", challanFail(sku, sku+" is not a stock item - nothing leaves the store")
		}
		key := sku + "|" + strings.TrimSpace(l.BatchNo)
		if seen[key] {
			return "", "", challanFail(sku, sku+" is on the challan twice - put it on one line")
		}
		seen[key] = true
		serials := cleanSerials(l.Serials)
		if len(serials) > 0 && len(serials) != l.Qty {
			return "", "", challanFail(sku, fmt.Sprintf("%s: %d serial numbers for %d pieces", sku, len(serials), l.Qty))
		}
		if l.Qty <= 0 {
			return "", "", challanFail(sku, "enter how many "+sku+" go out")
		}
		expected := firstNonEmpty(strings.TrimSpace(l.ExpectedReturn), headerReturn)
		if returnable && expected == "" {
			return "", "", challanFail("expected_return", "say when the goods are expected back")
		}
		unit := adjustmentUnitCost(tenantID, sku, item.Data)
		value := l.DeclaredValue
		if value <= 0 {
			value = round2(unit * float64(l.Qty))
		}
		declared, cost = round2(declared+value), round2(cost+unit*float64(l.Qty))
		line := map[string]interface{}{"sku": sku, "item_name": strField(item.Data, "name"), "hsn_code": strField(item.Data, "hsn_code"),
			"uom": firstNonEmpty(strField(item.Data, "uom"), "PCS"), "qty": l.Qty, "batch_no": strings.TrimSpace(l.BatchNo), "unit_cost": unit,
			"declared_value": round2(value), "gross_weight": l.GrossWeight, "net_weight": l.NetWeight, "condition": firstNonEmpty(strings.TrimSpace(l.Condition), "Good")}
		if returnable {
			line["expected_return"] = expected
		}
		if len(serials) > 0 {
			line["serials"] = serials
		}
		lines = append(lines, line)
	}
	ewb := challanEWBDecision(tenantID, location, purpose, in.PartyGSTIN, in.PartyState, declared)
	id := NewDocID("DC")
	encoded, _ := json.Marshal(lines)
	data := map[string]interface{}{"code": id, "purpose": purpose, "returnable": returnable, "location": location,
		"party_type": firstNonEmpty(strings.TrimSpace(in.PartyType), "Other"), "party": strings.TrimSpace(in.Party), "party_name": partyName,
		"party_gstin": strings.ToUpper(strings.TrimSpace(in.PartyGSTIN)), "party_address": strings.TrimSpace(in.PartyAddress), "party_state": strings.TrimSpace(in.PartyState),
		"expected_return": headerReturn, "reference": strings.TrimSpace(in.Reference), "note": strings.TrimSpace(in.Note),
		"items": string(encoded), "amount": declared, "cost_value": cost, "ewb_decision": ewb, "ewb_required": map[bool]string{true: "Yes", false: "No"}[ewb["required"].(bool)],
		"requested_by": userID, "status": "Draft"}
	issueNumberInto(data, tenantID, DeliveryChallanDoctype, id, "", location)
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema),
		id, DeliveryChallanDoctype, raw, userID); err != nil {
		return "", "", err
	}
	LogAuditEvent(tenantID, userID, "CREATE_DELIVERY_CHALLAN", "SUCCESS", fmt.Sprintf("%s: %s to %s, %d line(s), %.2f", id, purpose, partyName, len(lines), declared))
	if gated, _ := IsApprovalGated(tenantID, DeliveryChallanDoctype); gated {
		if err := SubmitForApproval(tenantID, DeliveryChallanDoctype, id, userID, role); err != nil {
			return id, "Draft", err
		}
		return id, "Pending Approval", nil
	}
	if err := setDocumentStatus(tenantID, DeliveryChallanDoctype, id, "Approved", data); err != nil {
		return id, "Draft", err
	}
	return id, "Approved", nil
}

func cleanSerials(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// ChallanDispatch is the transport of a dispatch.
type ChallanDispatch struct {
	TransportMode string `json:"transport_mode"`
	LRAWBNo       string `json:"lr_awb_no"`
	VehicleNo     string `json:"vehicle_no"`
	TransporterID string `json:"transporter_id"`
	Transporter   string `json:"transporter"`
	Packages      int    `json:"package_count"`
	DispatchDate  string `json:"dispatch_date"`
}

func lockChallanTx(tx *sql.Tx, schema, id string) (map[string]interface{}, string, error) {
	var raw, status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL FOR UPDATE`, schema),
		DeliveryChallanDoctype, id).Scan(&raw, &status); err != nil {
		return nil, "", challanFail(id, "no such delivery challan")
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, "", err
	}
	return data, status, nil
}

func saveChallanTx(tx *sql.Tx, schema, id, status string, patch map[string]interface{}) error {
	raw, _ := json.Marshal(patch)
	_, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = $3::text, data = data || $4::jsonb || jsonb_build_object('status', $3::text), updated_at = CURRENT_TIMESTAMP
		WHERE doctype = $1 AND id = $2`, schema), DeliveryChallanDoctype, id, status, string(raw))
	return err
}

// DispatchDeliveryChallan sends an approved challan's goods out. Dispatching
// a dispatched challan again does nothing.
func DispatchDeliveryChallan(tenantID, id, userID string, in ChallanDispatch) error {
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
	data, status, err := lockChallanTx(tx, schema, id)
	if err != nil {
		return err
	}
	if status != "Approved" {
		if strField(data, "dispatched_at") != "" {
			return nil
		}
		return challanFail(id, fmt.Sprintf("this challan is %s - only an approved challan is dispatched", status))
	}
	location, number := strField(data, "location"), firstNonEmpty(strField(data, "code"), id)
	returnable := truthy(data["returnable"])
	var bal []challanBalance
	var moves []interface{}
	var costPaise int64
	for _, l := range jsonLines(data["items"]) {
		sku, qty := strField(l, "sku"), int(numFromInterface(l["qty"]))
		b := challanBalance{SKU: sku, BatchNo: strField(l, "batch_no"), Issued: qty, UnitCost: numFromInterface(l["unit_cost"])}
		if s, ok := l["serials"].([]interface{}); ok && len(s) > 0 {
			b.Serials = map[string]string{}
			for _, v := range s {
				b.Serials[fmt.Sprint(v)] = "With party"
			}
		}
		bal = append(bal, b)
		if returnable {
			res, err := tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET available = available - $3, with_party = COALESCE(with_party, 0) + $3, updated_at = CURRENT_TIMESTAMP
				WHERE sku = $1 AND location_code = $2 AND available >= $3`, schema), sku, location, qty)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return challanFail(sku, fmt.Sprintf("not %d of %s available at %s to send out", qty, sku, location))
			}
		} else {
			moves = append(moves, map[string]interface{}{"sku": sku, "qty": float64(-qty), "batch_no": b.BatchNo})
			costPaise += RupeesToPaise(b.UnitCost) * int64(qty)
		}
	}
	if !returnable {
		_, ledger, err := PostInventoryLedgerWithVoucherTx(tx, schema, location, moves, false)
		if err != nil {
			var short *InsufficientStockError
			if errors.As(err, &short) {
				return challanFail(short.SKU, fmt.Sprintf("only %d of %s are at %s to send out", short.Available, short.SKU, location))
			}
			return err
		}
		if err := WriteStockLedgerLinesTx(tx, schema, location, DeliveryChallanDoctype, number, userID, "", ledger); err != nil {
			return err
		}
		if costPaise > 0 {
			acc := ResolvePostingAccounts(tenantID, PostingEventChallanNonReturnable, PostingContext{LegalEntity: locationLegalEntity(tenantID, location)}, accountChallanExpense, accountInventory)
			if err := PostDoubleEntryTx(tx, tenantID, schema, DeliveryChallanDoctype, id, map[string]int64{acc.Debit: costPaise}, map[string]int64{acc.Credit: costPaise}, "",
				"DeliveryChallan:"+id+":DISPATCH"); err != nil {
				return err
			}
		}
	}
	balRaw, _ := json.Marshal(bal)
	events, _ := json.Marshal([]challanEvent{{Kind: "dispatch", At: time.Now().UTC().Format(time.RFC3339), By: userID}})
	patch := map[string]interface{}{"challan_balance": string(balRaw), "challan_events": string(events), "dispatched_at": time.Now().UTC().Format(time.RFC3339),
		"dispatched_by": userID, "transport_mode": strings.TrimSpace(in.TransportMode), "lr_awb_no": strings.TrimSpace(in.LRAWBNo), "vehicle_no": strings.ToUpper(strings.TrimSpace(in.VehicleNo)),
		"transporter_id": strings.TrimSpace(in.TransporterID), "transporter": strings.TrimSpace(in.Transporter),
		"dispatch_date": firstNonEmpty(strings.TrimSpace(in.DispatchDate), BusinessToday())}
	if in.Packages > 0 {
		patch["package_count"] = in.Packages
	}
	if err := saveChallanTx(tx, schema, id, "Dispatched", patch); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "DISPATCH_DELIVERY_CHALLAN", "SUCCESS", fmt.Sprintf("%s dispatched from %s", number, location))
	return nil
}

// ChallanReturnLine is goods coming back on one line, in one condition.
type ChallanReturnLine struct {
	SKU        string   `json:"sku"`
	BatchNo    string   `json:"batch_no"`
	Qty        int      `json:"qty"`
	Condition  string   `json:"condition"` // Good (default), Damaged or Rejected
	Serials    []string `json:"serials"`
	ReasonCode string   `json:"reason_code"`
	Reason     string   `json:"reason"`
}

// ReceiveChallanReturn takes goods back against a dispatched challan.
// returnKey makes a retry harmless: the same key is the same receipt.
func ReceiveChallanReturn(tenantID, id, userID, returnKey string, in []ChallanReturnLine) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if len(in) == 0 {
		return "", challanFail("lines", "say what came back")
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}
	data, status, err := lockChallanTx(tx, schema, id)
	if err != nil {
		return "", err
	}
	bal, events := challanState(data)
	if key := strings.TrimSpace(returnKey); key != "" {
		for _, e := range events {
			if e.Key == key {
				return status, nil
			}
		}
	}
	if status != "Dispatched" && status != "Partly Returned" {
		return "", challanFail(id, fmt.Sprintf("this challan is %s - nothing is out to come back", status))
	}
	if !truthy(data["returnable"]) {
		return "", challanFail(id, "a non-returnable challan takes nothing back")
	}
	location := strField(data, "location")
	var evLines []map[string]interface{}
	for _, r := range in {
		b := findChallanBalance(bal, strings.TrimSpace(r.SKU), strings.TrimSpace(r.BatchNo))
		if b == nil {
			return "", challanFail(r.SKU, r.SKU+" did not go out on this challan")
		}
		serials := cleanSerials(r.Serials)
		if len(b.Serials) > 0 {
			if len(serials) == 0 {
				return "", challanFail(r.SKU, "say which serial numbers of "+r.SKU+" came back")
			}
			r.Qty = len(serials)
			for _, s := range serials {
				if b.Serials[s] != "With party" {
					return "", challanFail(r.SKU, fmt.Sprintf("serial %s of %s is not out on this challan", s, r.SKU))
				}
			}
		}
		if r.Qty <= 0 || r.Qty > b.Open() {
			return "", challanFail(r.SKU, fmt.Sprintf("%s: %d can come back, not %d", r.SKU, b.Open(), r.Qty))
		}
		cond := "Good"
		pool := "available"
		switch strings.ToLower(strings.TrimSpace(r.Condition)) {
		case "damaged":
			cond, pool = "Damaged", "damaged"
		case "rejected":
			cond, pool = "Rejected", "qc_hold"
		}
		code, text := "", ""
		if cond != "Good" {
			if code, text, err = ReasonForException(tenantID, ReasonDeliveryChallan, r.ReasonCode, r.Reason); err != nil {
				return "", err
			}
			if code == "" && text == "" {
				return "", challanFail(r.SKU, fmt.Sprintf("say why %s came back %s", r.SKU, strings.ToLower(cond)))
			}
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET with_party = COALESCE(with_party, 0) - $3, %s = COALESCE(%s, 0) + $3, updated_at = CURRENT_TIMESTAMP
			WHERE sku = $1 AND location_code = $2`, schema, pool, pool), b.SKU, location, r.Qty); err != nil {
			return "", err
		}
		switch cond {
		case "Good":
			b.Returned += r.Qty
		case "Damaged":
			b.Damaged += r.Qty
		default:
			b.Rejected += r.Qty
		}
		for _, s := range serials {
			b.Serials[s] = map[string]string{"Good": "Returned", "Damaged": "Damaged", "Rejected": "Rejected"}[cond]
		}
		evLines = append(evLines, map[string]interface{}{"sku": b.SKU, "batch_no": b.BatchNo, "qty": r.Qty, "condition": cond, "serials": serials, "reason_code": code, "reason": text})
	}
	events = append(events, challanEvent{Key: strings.TrimSpace(returnKey), Kind: "return", At: time.Now().UTC().Format(time.RFC3339), By: userID, Lines: evLines})
	newStatus := challanStatusAfter(bal)
	balRaw, _ := json.Marshal(bal)
	evRaw, _ := json.Marshal(events)
	if err := saveChallanTx(tx, schema, id, newStatus, map[string]interface{}{"challan_balance": string(balRaw), "challan_events": string(evRaw)}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "RETURN_DELIVERY_CHALLAN", "SUCCESS", fmt.Sprintf("%s: %d line(s) back, now %s", firstNonEmpty(strField(data, "code"), id), len(evLines), newStatus))
	return newStatus, nil
}

// CancelDeliveryChallan cancels a challan before its goods leave.
func CancelDeliveryChallan(tenantID, id, userID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return challanFail("reason", "say why the challan is cancelled")
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
	data, status, err := lockChallanTx(tx, schema, id)
	if err != nil {
		return err
	}
	if strField(data, "dispatched_at") != "" || (status != "Draft" && status != "Pending Approval" && status != "Approved") {
		return challanFail(id, "the goods have left - take them back with Receive return, or write off what will not come back")
	}
	if err := saveChallanTx(tx, schema, id, "Cancelled", map[string]interface{}{"cancel_reason": strings.TrimSpace(reason), "cancelled_by": userID}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "CANCEL_DELIVERY_CHALLAN", "SUCCESS", fmt.Sprintf("%s cancelled: %s", id, reason))
	return nil
}

// ChallanWriteOffInput is goods that will not come back.
type ChallanWriteOffInput struct {
	SKU        string   `json:"sku"`
	BatchNo    string   `json:"batch_no"`
	Qty        int      `json:"qty"`
	Kind       string   `json:"kind"` // Lost, or Kept by party
	Serials    []string `json:"serials"`
	ReasonCode string   `json:"reason_code"`
	Reason     string   `json:"reason"`
}

// RequestChallanWriteOff sends goods that will not come back to the write-off
// approver (an approval rule on ChallanWriteOff); approval posts it.
func RequestChallanWriteOff(tenantID, challanID, userID, role string, in ChallanWriteOffInput) (string, error) {
	data, status, err := fetchDocData(tenantID, DeliveryChallanDoctype, challanID)
	if err != nil || data == nil {
		return "", challanFail(challanID, "no such delivery challan")
	}
	if status != "Dispatched" && status != "Partly Returned" {
		return "", challanFail(challanID, fmt.Sprintf("this challan is %s - nothing is out to write off", status))
	}
	if in.Kind != challanKindLost && in.Kind != challanKindKept {
		return "", challanFail("kind", "say whether the goods are lost or kept by the party")
	}
	bal, _ := challanState(data)
	b := findChallanBalance(bal, strings.TrimSpace(in.SKU), strings.TrimSpace(in.BatchNo))
	if b == nil {
		return "", challanFail(in.SKU, in.SKU+" did not go out on this challan")
	}
	serials := cleanSerials(in.Serials)
	if len(b.Serials) > 0 {
		if len(serials) == 0 {
			return "", challanFail(in.SKU, "say which serial numbers of "+in.SKU+" will not come back")
		}
		in.Qty = len(serials)
	}
	if in.Qty <= 0 || in.Qty > b.Open() {
		return "", challanFail(in.SKU, fmt.Sprintf("%s: %d still out, not %d", in.SKU, b.Open(), in.Qty))
	}
	code, text, err := ReasonForException(tenantID, ReasonDeliveryChallan, in.ReasonCode, in.Reason)
	if err != nil {
		return "", err
	}
	if code == "" && text == "" {
		return "", challanFail("reason", "say why it will not come back")
	}
	if gated, _ := IsApprovalGated(tenantID, ChallanWriteOffDoctype); !gated {
		return "", challanFail("approval", "no one is set to approve challan write-offs - an administrator adds an approval rule for ChallanWriteOff (Settings > Approval Rules)")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	id := NewDocID("CWO")
	raw, _ := json.Marshal(map[string]interface{}{"code": id, "challan_id": challanID, "challan_number": firstNonEmpty(strField(data, "code"), challanID),
		"location": strField(data, "location"), "party_name": strField(data, "party_name"), "purpose": strField(data, "purpose"),
		"sku": b.SKU, "batch_no": b.BatchNo, "qty": in.Qty, "kind": in.Kind, "serials": serials, "reason_code": code, "reason": text,
		"amount": round2(b.UnitCost * float64(in.Qty)), "requested_by": userID, "status": "Draft"})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema),
		id, ChallanWriteOffDoctype, raw, userID); err != nil {
		return "", err
	}
	if err := SubmitForApproval(tenantID, ChallanWriteOffDoctype, id, userID, role); err != nil {
		return id, err
	}
	return id, nil
}

// executeChallanWriteOff posts an approved write-off: the units leave the
// with-party pool and on_hand, and their cost goes to the loss (Lost) or
// the non-returnable expense (Kept by party).
func executeChallanWriteOff(tenantID, writeOffID, approver string) {
	fail := func(err error) {
		if schema, e := db.GetTenantSchema(tenantID); e == nil {
			_ = mergeDocData(schema, ChallanWriteOffDoctype, writeOffID, map[string]interface{}{"post_error": err.Error()})
		}
		LogSystemError(tenantID, approver, "WARN", "ChallanWriteOff", fmt.Sprintf("%s: %v", writeOffID, err), "")
	}
	if err := postChallanWriteOff(tenantID, writeOffID, approver); err != nil {
		fail(err)
	}
}

func postChallanWriteOff(tenantID, writeOffID, approver string) error {
	wo, _, err := fetchDocData(tenantID, ChallanWriteOffDoctype, writeOffID)
	if err != nil || wo == nil {
		return fmt.Errorf("write-off %s not found", writeOffID)
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
	challanID := strField(wo, "challan_id")
	data, status, err := lockChallanTx(tx, schema, challanID)
	if err != nil {
		return err
	}
	if status != "Dispatched" && status != "Partly Returned" {
		return fmt.Errorf("challan %s is %s", challanID, status)
	}
	bal, events := challanState(data)
	b := findChallanBalance(bal, strField(wo, "sku"), strField(wo, "batch_no"))
	qty := int(numFromInterface(wo["qty"]))
	if b == nil {
		return fmt.Errorf("%s is not on challan %s", strField(wo, "sku"), challanID)
	}
	if qty <= 0 || qty > b.Open() {
		return fmt.Errorf("only %d still out", b.Open())
	}
	var serials []string
	if s, ok := wo["serials"].([]interface{}); ok {
		for _, v := range s {
			serials = append(serials, fmt.Sprint(v))
		}
	}
	for _, s := range serials {
		if b.Serials[s] != "With party" {
			return fmt.Errorf("serial %s is not out", s)
		}
	}
	location := strField(data, "location")
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET with_party = COALESCE(with_party, 0) - $3, on_hand = on_hand - $3, updated_at = CURRENT_TIMESTAMP
		WHERE sku = $1 AND location_code = $2`, schema), b.SKU, location, qty); err != nil {
		return err
	}
	kind := strField(wo, "kind")
	newSerial := "Lost"
	if kind == challanKindKept {
		b.Kept += qty
		newSerial = "Kept"
	} else {
		b.Lost += qty
	}
	for _, s := range serials {
		b.Serials[s] = newSerial
	}
	if costPaise := RupeesToPaise(b.UnitCost) * int64(qty); costPaise > 0 {
		pctx := PostingContext{LegalEntity: locationLegalEntity(tenantID, location), ReasonCode: strField(wo, "reason_code")}
		event, debit := PostingEventChallanNonReturnable, accountChallanExpense
		if kind == challanKindLost {
			event, debit = PostingEventAdjustmentLoss, accountStockAdjustment
			if strField(data, "purpose") == "Job work" {
				event, debit = PostingEventJobworkLoss, accountJobworkLoss
			}
		}
		acc := ResolvePostingAccounts(tenantID, event, pctx, debit, accountInventory)
		if err := PostDoubleEntryTx(tx, tenantID, schema, ChallanWriteOffDoctype, writeOffID, map[string]int64{acc.Debit: costPaise}, map[string]int64{acc.Credit: costPaise}, "",
			"ChallanWriteOff:"+writeOffID); err != nil {
			return err
		}
	}
	events = append(events, challanEvent{Key: "writeoff:" + writeOffID, Kind: "write-off", At: time.Now().UTC().Format(time.RFC3339), By: approver,
		Lines: []map[string]interface{}{{"sku": b.SKU, "batch_no": b.BatchNo, "qty": qty, "kind": kind, "serials": serials, "write_off": writeOffID}}})
	balRaw, _ := json.Marshal(bal)
	evRaw, _ := json.Marshal(events)
	if err := saveChallanTx(tx, schema, challanID, challanStatusAfter(bal), map[string]interface{}{"challan_balance": string(balRaw), "challan_events": string(evRaw)}); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Posted', data = data || '{"status":"Posted"}'::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = $1 AND id = $2`, schema),
		ChallanWriteOffDoctype, writeOffID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, approver, "POST_CHALLAN_WRITE_OFF", "SUCCESS", fmt.Sprintf("%s: %d %s %s on %s", writeOffID, qty, b.SKU, strings.ToLower(kind), challanID))
	return nil
}

// --- reports ------------------------------------------------------------------

func deliveryChallanRegister(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	docs, err := docsBetween(tenantID, DeliveryChallanDoctype, p)
	if err != nil {
		return nil, err
	}
	var out []map[string]interface{}
	for _, d := range docs {
		bal, _ := challanState(d.data)
		for _, l := range jsonLines(d.data["items"]) {
			row := map[string]interface{}{"date": d.at.Format("2006-01-02"), "number": firstNonEmpty(strField(d.data, "code"), d.id), "purpose": strField(d.data, "purpose"),
				"location": strField(d.data, "location"), "party": strField(d.data, "party_name"), "sku": strField(l, "sku"), "batch_no": strField(l, "batch_no"),
				"issued": int(numFromInterface(l["qty"])), "expected_return": strField(l, "expected_return"), "status": d.status, "ewb_required": strField(d.data, "ewb_required")}
			if b := findChallanBalance(bal, strField(l, "sku"), strField(l, "batch_no")); b != nil {
				row["returned"], row["damaged"], row["rejected"], row["written_off"], row["open"] = b.Returned, b.Damaged, b.Rejected, b.Lost+b.Kept, b.Open()
			}
			out = append(out, row)
		}
	}
	return out, nil
}

func challanOutstandingByParty(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	docs, err := docsBetween(tenantID, DeliveryChallanDoctype, map[string]string{})
	if err != nil {
		return nil, err
	}
	type agg struct {
		challans                    map[string]bool
		qty, notDue, d30, d60, more int
		value                       float64
	}
	by := map[string]*agg{}
	today := todayDate()
	for _, d := range docs {
		if d.status != "Dispatched" && d.status != "Partly Returned" {
			continue
		}
		bal, _ := challanState(d.data)
		due := map[string]string{}
		for _, l := range jsonLines(d.data["items"]) {
			due[strField(l, "sku")+"|"+strField(l, "batch_no")] = strField(l, "expected_return")
		}
		for _, b := range bal {
			open := b.Open()
			if open <= 0 {
				continue
			}
			party := strField(d.data, "party_name")
			a := by[party]
			if a == nil {
				a = &agg{challans: map[string]bool{}}
				by[party] = a
			}
			a.challans[d.id] = true
			a.qty += open
			a.value = round2(a.value + b.UnitCost*float64(open))
			days := 0
			if t, err := time.ParseInLocation("2006-01-02", due[b.SKU+"|"+b.BatchNo], time.Local); err == nil {
				days = int(today.Sub(t).Hours() / 24)
			}
			switch {
			case days <= 0:
				a.notDue += open
			case days <= 30:
				a.d30 += open
			case days <= 60:
				a.d60 += open
			default:
				a.more += open
			}
		}
	}
	var out []map[string]interface{}
	for party, a := range by {
		out = append(out, map[string]interface{}{"party": party, "challans": len(a.challans), "open_qty": a.qty, "open_value": a.value,
			"not_due": a.notDue, "overdue_30": a.d30, "overdue_60": a.d60, "overdue_more": a.more})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["open_value"].(float64) > out[j]["open_value"].(float64) })
	return out, nil
}
