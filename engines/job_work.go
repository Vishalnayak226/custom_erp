package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Stage 130.16 (74.3.3, BRD 4.5, DEC-09/10): job work, on the existing
// SubcontractOrder rather than a parallel record. An order has many source
// lines (lot, serials, gross / net / fine weight, purity, value), each with
// the item that comes back (the same item, or an output item at a yield).
// Sending raises and dispatches a Delivery Challan (74.3.1), so the goods
// sit in the job worker's custody - owned, out of available stock, value
// unchanged. Returns are partial and repeated against the lines:
//
//   - the same item back: accepted to available, rejected to QC hold
//     (through the challan's own return);
//   - transformed: what was consumed leaves custody and stock, and the output
//     item comes in at the consumed value (rejected output to QC hold),
//     with the source lot kept beside it (genealogy);
//   - scrap and loss within the client's tolerance (DEC-09) are booked at
//     once to job-work loss; above it they wait for a Challan Write-off
//     approval.
//
// Per line, in qty and in each weight: outward - accepted - rejected -
// approved scrap / loss = open. The processing charge is booked from the
// vendor's bill (Dr job-work conversion cost, Cr the payable) and spread
// over the lines by what was accepted. Orders past their due date, and
// inputs near the statutory return limit (ITC-04: inputs 1 year, capital
// goods 3 years), are flagged and reminded.
//
// An order made before this (one SKU out, one back) is a one-line order;
// one already sent the old way has its material out of stock already, so its
// receipt only brings the output in.

const SettingJobworkLossTolerance = "manufacturing.jobwork_loss_tolerance_pct"

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingJobworkLossTolerance, Module: "Manufacturing",
		Label: "Job work scrap and loss booked without approval", Type: SettingTypeFloat, Default: "1", Unit: "%",
		Min:         settingBound(0),
		Description: "Scrap and loss a job worker reports, up to this share of what was sent on a line, are booked at once as job-work loss. More waits for a Challan Write-off approval (Settings > Approval Rules: ChallanWriteOff).",
	})
	RegisterReport(ReportDefinition{ID: "job-work-outstanding", Label: "Job work outstanding (qty and weight)", Category: "Manufacturing",
		Columns: []ReportColumn{{Key: "order", Label: "Order"}, {Key: "vendor", Label: "Job worker"}, {Key: "line_no", Label: "Line"}, {Key: "sku", Label: "Item"},
			{Key: "outward", Label: "Sent"}, {Key: "accepted", Label: "Accepted"}, {Key: "rejected", Label: "Rejected"}, {Key: "loss", Label: "Scrap / loss"},
			{Key: "pending_loss", Label: "Loss awaiting approval"}, {Key: "open", Label: "Still out"}, {Key: "open_net_weight", Label: "Net weight out"},
			{Key: "open_fine_weight", Label: "Fine weight out"}, {Key: "sent_date", Label: "Sent"}, {Key: "due_date", Label: "Due back"}, {Key: "days_overdue", Label: "Days overdue"},
			{Key: "statutory_due", Label: "Statutory return by"}},
		Run: jobWorkOutstanding, StoreKey: "-"})
	RegisterReport(ReportDefinition{ID: "job-work-yield", Label: "Job work cost, yield and wastage", Category: "Manufacturing",
		Columns: []ReportColumn{{Key: "order", Label: "Order"}, {Key: "vendor", Label: "Job worker"}, {Key: "sku", Label: "Sent"}, {Key: "output_sku", Label: "Came back as"},
			{Key: "consumed", Label: "Consumed"}, {Key: "output_qty", Label: "Output accepted"}, {Key: "output_rejected", Label: "Output rejected"},
			{Key: "yield_pct", Label: "Yield % (of expected)"}, {Key: "wastage_pct", Label: "Scrap / loss %"},
			{Key: "material_value", Label: "Material value"}, {Key: "charges", Label: "Processing charge"}, {Key: "cost_per_output", Label: "Cost per output"}},
		Run: jobWorkYield, StoreKey: "-"})
}

func jobWorkFail(msg string) error {
	return &ValidationError{Code: "MANUFA-0142", Message: msg}
}

// JobWorkLine is one source line of a job work order.
type JobWorkLine struct {
	LineNo        int      `json:"line_no"`
	SKU           string   `json:"sku"`
	Qty           int      `json:"qty"`
	BatchNo       string   `json:"batch_no,omitempty"`
	Serials       []string `json:"serials,omitempty"`
	GrossWeight   float64  `json:"gross_weight,omitempty"`
	NetWeight     float64  `json:"net_weight,omitempty"`
	FineWeight    float64  `json:"fine_weight,omitempty"`
	Purity        string   `json:"purity,omitempty"`
	Value         float64  `json:"value,omitempty"`
	OutputSKU     string   `json:"output_sku,omitempty"`
	OutputPerUnit float64  `json:"output_per_unit,omitempty"`
}

func (l JobWorkLine) transforms() bool { return l.OutputSKU != "" && l.OutputSKU != l.SKU }

func (l JobWorkLine) yield() float64 {
	if l.OutputPerUnit > 0 {
		return l.OutputPerUnit
	}
	return 1
}

// jobWorkBalance is what one line owes. Weights are gross, net, fine.
type jobWorkBalance struct {
	LineNo         int        `json:"line_no"`
	SKU            string     `json:"sku"`
	BatchNo        string     `json:"batch_no,omitempty"`
	OutputSKU      string     `json:"output_sku,omitempty"`
	Outward        int        `json:"outward"`
	Accepted       int        `json:"accepted"`
	Rejected       int        `json:"rejected"`
	Loss           int        `json:"loss"`
	PendingLoss    int        `json:"pending_loss"`
	OutputQty      int        `json:"output_qty"`
	OutputRejected int        `json:"output_rejected"`
	UnitCost       float64    `json:"unit_cost"`
	Charges        float64    `json:"charges"`
	OutW           [3]float64 `json:"out_w"`
	BackW          [3]float64 `json:"back_w"`
	LossW          [3]float64 `json:"loss_w"`
	WriteOffs      []string   `json:"write_offs,omitempty"` // pending ones
}

func (b jobWorkBalance) Open() int { return b.Outward - b.Accepted - b.Rejected - b.Loss }

func (b jobWorkBalance) OpenWeight(i int) float64 {
	return round2(b.OutW[i] - b.BackW[i] - b.LossW[i])
}

// jobWorkLines are an order's lines - or the one line an older order is.
func jobWorkLines(data map[string]interface{}) []JobWorkLine {
	var lines []JobWorkLine
	if raw := strField(data, "lines"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &lines)
	}
	if len(lines) == 0 && strField(data, "sent_item_id") != "" {
		qty := int(numFromInterface(data["sent_qty"]))
		l := JobWorkLine{LineNo: 1, SKU: strField(data, "sent_item_id"), Qty: qty, OutputSKU: strField(data, "received_item_id")}
		if exp := numFromInterface(data["expected_received_qty"]); exp > 0 && qty > 0 {
			l.OutputPerUnit = exp / float64(qty)
		}
		lines = append(lines, l)
	}
	for i := range lines {
		if lines[i].LineNo == 0 {
			lines[i].LineNo = i + 1
		}
		lines[i].SKU, lines[i].OutputSKU = strings.TrimSpace(lines[i].SKU), strings.TrimSpace(lines[i].OutputSKU)
	}
	return lines
}

func jobWorkState(data map[string]interface{}) ([]jobWorkBalance, []challanEvent) {
	var bal []jobWorkBalance
	var events []challanEvent
	_ = json.Unmarshal([]byte(strField(data, "jw_balance")), &bal)
	_ = json.Unmarshal([]byte(strField(data, "jw_events")), &events)
	return bal, events
}

func findJobWorkBalance(bal []jobWorkBalance, lineNo int) *jobWorkBalance {
	for i := range bal {
		if bal[i].LineNo == lineNo {
			return &bal[i]
		}
	}
	return nil
}

// statutoryReturnBy is ITC-04's limit: inputs back within a year, capital
// goods within three.
func statutoryReturnBy(sent string, capitalGoods bool) string {
	t, err := time.ParseInLocation("2006-01-02", sent, time.Local)
	if err != nil {
		return ""
	}
	years := 1
	if capitalGoods {
		years = 3
	}
	return t.AddDate(years, 0, 0).Format("2006-01-02")
}

// SendJobWork raises the order's delivery challan and dispatches it. An order
// whose challan waits for approval is "Challan Pending"; sending it again
// once the challan is approved dispatches it.
func SendJobWork(tenantID, id, userID, role string, transport ChallanDispatch) (string, error) {
	data, status, err := fetchSubcontractOrder(tenantID, id)
	if err != nil {
		return "", err
	}
	if status == "Challan Pending" {
		dc := strField(data, "challan_id")
		if _, dcStatus, err := fetchDocData(tenantID, DeliveryChallanDoctype, dc); err != nil || dcStatus != "Approved" {
			return status, jobWorkFail("the order's delivery challan is not approved yet - approve it, then send again")
		}
		if err := DispatchDeliveryChallan(tenantID, dc, userID, transport); err != nil {
			return status, err
		}
		data["sent_date"] = BusinessToday()
		data["statutory_due"] = statutoryReturnBy(strField(data, "sent_date"), truthy(data["capital_goods"]))
		return "Sent", saveSubcontractOrder(tenantID, id, data, "Sent")
	}
	if status != "Draft" {
		return status, jobWorkFail(fmt.Sprintf("only a Draft job work order can be sent (current status: %s)", status))
	}
	lines := jobWorkLines(data)
	location := strings.TrimSpace(strField(data, "location"))
	if location == "" || len(lines) == 0 {
		return status, jobWorkFail("a job work order needs its sending store and at least one line")
	}
	var dcLines []ChallanLineInput
	for _, l := range lines {
		if l.SKU == "" || l.Qty <= 0 {
			return status, jobWorkFail(fmt.Sprintf("line %d needs an item and a quantity", l.LineNo))
		}
		if l.transforms() {
			if item, err := ResolveItemBySKU(tenantID, l.OutputSKU); err != nil || item == nil {
				return status, jobWorkFail(fmt.Sprintf("line %d: no output item %q", l.LineNo, l.OutputSKU))
			}
		}
		dcLines = append(dcLines, ChallanLineInput{SKU: l.SKU, Qty: l.Qty, BatchNo: l.BatchNo, Serials: l.Serials, GrossWeight: l.GrossWeight,
			NetWeight: l.NetWeight, DeclaredValue: l.Value})
	}
	vendor := strField(data, "vendor_id")
	vdoc, _, _ := fetchDocData(tenantID, "Vendor", vendor)
	due := strings.TrimSpace(strField(data, "due_date"))
	if due == "" {
		due = time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	}
	dcID, dcStatus, err := CreateDeliveryChallan(tenantID, ChallanInput{Location: location, Purpose: "Job work", PartyType: "Job worker", Party: vendor,
		PartyName: firstNonEmpty(strField(vdoc, "name"), strField(vdoc, "vendor_name"), vendor), PartyGSTIN: firstNonEmpty(strField(vdoc, "gstin"), strField(vdoc, "tax_id")),
		PartyAddress: strField(vdoc, "address"), PartyState: firstNonEmpty(strField(vdoc, "state_code"), strField(vdoc, "state")), ExpectedReturn: due,
		Reference: firstNonEmpty(strField(data, "code"), id), Lines: dcLines}, userID, role)
	if err != nil && dcID == "" {
		return status, err
	}
	dc, _, _ := fetchDocData(tenantID, DeliveryChallanDoctype, dcID)
	costs := map[string]float64{}
	for _, l := range jsonLines(dc["items"]) {
		costs[strField(l, "sku")+"|"+strField(l, "batch_no")] = numFromInterface(l["unit_cost"])
	}
	var bal []jobWorkBalance
	for _, l := range lines {
		bal = append(bal, jobWorkBalance{LineNo: l.LineNo, SKU: l.SKU, BatchNo: l.BatchNo, OutputSKU: l.OutputSKU, Outward: l.Qty,
			UnitCost: costs[l.SKU+"|"+l.BatchNo], OutW: [3]float64{l.GrossWeight, l.NetWeight, l.FineWeight}})
	}
	linesRaw, _ := json.Marshal(lines)
	balRaw, _ := json.Marshal(bal)
	data["lines"], data["jw_balance"], data["challan_id"], data["challan_number"], data["due_date"] = string(linesRaw), string(balRaw), dcID, firstNonEmpty(strField(dc, "code"), dcID), due
	newStatus := "Challan Pending"
	if dcStatus == "Approved" {
		if err := DispatchDeliveryChallan(tenantID, dcID, userID, transport); err != nil {
			_ = saveSubcontractOrder(tenantID, id, data, newStatus)
			return newStatus, err
		}
		newStatus = "Sent"
		data["sent_date"] = BusinessToday()
		data["statutory_due"] = statutoryReturnBy(strField(data, "sent_date"), truthy(data["capital_goods"]))
	}
	if err := saveSubcontractOrder(tenantID, id, data, newStatus); err != nil {
		return newStatus, err
	}
	LogAuditEvent(tenantID, userID, "SEND_JOB_WORK", "SUCCESS", fmt.Sprintf("%s: %d line(s) on challan %s, %s", id, len(lines), dcID, newStatus))
	return newStatus, nil
}

// SendToSubcontractor (Stage 26.9.11's call) sends through the challan now.
func SendToSubcontractor(tenantID, id, userID string) error {
	_, err := SendJobWork(tenantID, id, userID, "", ChallanDispatch{})
	return err
}

// JobWorkReceiveLine is one line's return, in the source item's units
// (OutputQty in the output item's pieces).
type JobWorkReceiveLine struct {
	LineNo            int      `json:"line_no"`
	AcceptedQty       int      `json:"accepted_qty"`
	RejectedQty       int      `json:"rejected_qty"`
	ScrapQty          int      `json:"scrap_qty"`
	LossQty           int      `json:"loss_qty"`
	OutputQty         *int     `json:"output_qty"`
	OutputRejectedQty *int     `json:"output_rejected_qty"`
	Serials           []string `json:"serials"`
	GrossWeight       float64  `json:"gross_weight"`
	NetWeight         float64  `json:"net_weight"`
	FineWeight        float64  `json:"fine_weight"`
	QC                string   `json:"qc"`
	ReasonCode        string   `json:"reason_code"`
	Reason            string   `json:"reason"`
}

// ReceiveJobWork takes a partial return against the order's lines. key makes
// a retry harmless.
func ReceiveJobWork(tenantID, id, userID, role, key string, in []JobWorkReceiveLine) (string, error) {
	data, status, err := fetchSubcontractOrder(tenantID, id)
	if err != nil {
		return "", err
	}
	key = strings.TrimSpace(key)
	bal, events := jobWorkState(data)
	for _, e := range events {
		if key != "" && e.Key == key {
			return status, nil
		}
	}
	dcID := strField(data, "challan_id")
	if status == "Challan Pending" || (status != "Sent" && status != "Partly Received") {
		return status, jobWorkFail(fmt.Sprintf("this order is %s - only goods sent can come back", status))
	}
	if dcID == "" {
		return status, jobWorkFail("this order was sent before job work lines - receive it with Receive from Subcontractor")
	}
	if len(in) == 0 {
		return status, jobWorkFail("say what came back")
	}
	refreshJobWorkLosses(tenantID, bal)
	lines := map[int]JobWorkLine{}
	for _, l := range jobWorkLines(data) {
		lines[l.LineNo] = l
	}
	tolerance := GetSettingFloat(tenantID, SettingJobworkLossTolerance)
	var sameItem []ChallanReturnLine
	type conversion struct {
		line           JobWorkLine
		consumed       int
		output, outRej int
	}
	var conversions []conversion
	type loss struct {
		b   *jobWorkBalance
		qty int
		now bool
	}
	var losses []loss
	var evLines []map[string]interface{}
	for _, r := range in {
		b := findJobWorkBalance(bal, r.LineNo)
		l, ok := lines[r.LineNo]
		if b == nil || !ok {
			return status, jobWorkFail(fmt.Sprintf("the order has no line %d", r.LineNo))
		}
		total := r.AcceptedQty + r.RejectedQty + r.ScrapQty + r.LossQty
		if r.AcceptedQty < 0 || r.RejectedQty < 0 || r.ScrapQty < 0 || r.LossQty < 0 || total <= 0 {
			return status, jobWorkFail(fmt.Sprintf("line %d: enter what came back", r.LineNo))
		}
		if total > b.Open()-b.PendingLoss {
			return status, jobWorkFail(fmt.Sprintf("line %d: %d still out, not %d", r.LineNo, b.Open()-b.PendingLoss, total))
		}
		if (r.RejectedQty > 0 || r.ScrapQty > 0 || r.LossQty > 0) && strings.TrimSpace(r.Reason) == "" && strings.TrimSpace(r.ReasonCode) == "" {
			return status, jobWorkFail(fmt.Sprintf("line %d: say why some was rejected, scrapped or lost", r.LineNo))
		}
		ev := map[string]interface{}{"line_no": r.LineNo, "sku": l.SKU, "batch_no": l.BatchNo, "accepted": r.AcceptedQty, "rejected": r.RejectedQty,
			"scrap": r.ScrapQty, "loss": r.LossQty, "qc": strings.TrimSpace(r.QC), "weights": []float64{r.GrossWeight, r.NetWeight, r.FineWeight}}
		if l.transforms() {
			c := conversion{line: l, consumed: r.AcceptedQty + r.RejectedQty}
			c.output = int(math.Round(float64(r.AcceptedQty) * l.yield()))
			if r.OutputQty != nil {
				c.output = *r.OutputQty
			}
			c.outRej = int(math.Round(float64(r.RejectedQty) * l.yield()))
			if r.OutputRejectedQty != nil {
				c.outRej = *r.OutputRejectedQty
			}
			if c.consumed > 0 {
				conversions = append(conversions, c)
			}
			b.OutputQty += c.output
			b.OutputRejected += c.outRej
			ev["output_sku"], ev["output_qty"], ev["output_rejected"] = l.OutputSKU, c.output, c.outRej
		} else {
			if r.AcceptedQty > 0 {
				sameItem = append(sameItem, ChallanReturnLine{SKU: l.SKU, BatchNo: l.BatchNo, Qty: r.AcceptedQty, Serials: r.Serials})
			}
			if r.RejectedQty > 0 {
				sameItem = append(sameItem, ChallanReturnLine{SKU: l.SKU, BatchNo: l.BatchNo, Qty: r.RejectedQty, Condition: "Rejected", ReasonCode: r.ReasonCode,
					Reason: firstNonEmpty(r.Reason, r.QC)})
			}
		}
		b.Accepted += r.AcceptedQty
		b.Rejected += r.RejectedQty
		b.BackW[0], b.BackW[1], b.BackW[2] = round2(b.BackW[0]+r.GrossWeight), round2(b.BackW[1]+r.NetWeight), round2(b.BackW[2]+r.FineWeight)
		if lost := r.ScrapQty + r.LossQty; lost > 0 {
			within := float64(b.Loss+b.PendingLoss+lost) <= tolerance*float64(b.Outward)/100
			losses = append(losses, loss{b: b, qty: lost, now: within})
		}
		evLines = append(evLines, ev)
	}
	// (1) The same item back, through the challan's own return.
	if len(sameItem) > 0 {
		if _, err := ReceiveChallanReturn(tenantID, dcID, userID, "jw:"+id+":"+key, sameItem); err != nil {
			return status, err
		}
	}
	// (2) Transformations, with the order, in one transaction.
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return status, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return status, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return status, err
	}
	location := strField(data, "location")
	type costIn struct {
		sku       string
		qty       int
		unitPaise int64
	}
	var costsIn []costIn
	for _, c := range conversions {
		b := findJobWorkBalance(bal, c.line.LineNo)
		if err := convertChallanUnitsTx(tx, schema, dcID, c.line.SKU, c.line.BatchNo, c.consumed); err != nil {
			return status, err
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET with_party = COALESCE(with_party, 0) - $3, on_hand = on_hand - $3, updated_at = CURRENT_TIMESTAMP
			WHERE sku = $1 AND location_code = $2`, schema), c.line.SKU, location, c.consumed); err != nil {
			return status, err
		}
		if err := WriteStockLedgerLinesTx(tx, schema, location, "SubcontractOrder", id, userID, "",
			[]PostedStockLine{{SKU: c.line.SKU, Qty: -c.consumed, BatchNo: c.line.BatchNo}}); err != nil {
			return status, err
		}
		if out := c.output + c.outRej; out > 0 {
			_, ledger, err := PostInventoryLedgerWithVoucherTx(tx, schema, location, []interface{}{map[string]interface{}{"sku": c.line.OutputSKU, "qty": float64(out)}}, false)
			if err != nil {
				return status, err
			}
			if err := WriteStockLedgerLinesTx(tx, schema, location, "SubcontractOrder", id, userID, "", ledger); err != nil {
				return status, err
			}
			if c.outRej > 0 {
				if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET available = available - $3, qc_hold = COALESCE(qc_hold, 0) + $3 WHERE sku = $1 AND location_code = $2`, schema),
					c.line.OutputSKU, location, c.outRej); err != nil {
					return status, err
				}
			}
			// The output carries the material it was made from.
			costsIn = append(costsIn, costIn{sku: c.line.OutputSKU, qty: out, unitPaise: RupeesToPaise(b.UnitCost * float64(c.consumed) / float64(out))})
		}
	}
	// (3) Scrap and loss: within tolerance now, above it for approval.
	var autoWriteOffs []string
	for _, ls := range losses {
		woID, err := createChallanWriteOffTx(tx, schema, tenantID, dcID, data, ls.b, ls.qty, userID, ls.now)
		if err != nil {
			return status, err
		}
		share := float64(ls.qty) / float64(ls.b.Outward)
		if ls.now {
			ls.b.Loss += ls.qty
			ls.b.LossW[0], ls.b.LossW[1], ls.b.LossW[2] = round2(ls.b.LossW[0]+ls.b.OutW[0]*share), round2(ls.b.LossW[1]+ls.b.OutW[1]*share), round2(ls.b.LossW[2]+ls.b.OutW[2]*share)
			autoWriteOffs = append(autoWriteOffs, woID)
		} else {
			ls.b.PendingLoss += ls.qty
			ls.b.WriteOffs = append(ls.b.WriteOffs, woID)
		}
	}
	events = append(events, challanEvent{Key: key, Kind: "receive", At: time.Now().UTC().Format(time.RFC3339), By: userID, Lines: evLines})
	newStatus := jobWorkStatusAfter(bal)
	balRaw, _ := json.Marshal(bal)
	evRaw, _ := json.Marshal(events)
	data["jw_balance"], data["jw_events"], data["received_date"] = string(balRaw), string(evRaw), BusinessToday()
	raw, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SubcontractOrder' AND id = $3`, schema),
		string(raw), newStatus, id); err != nil {
		return status, err
	}
	if err := tx.Commit(); err != nil {
		return status, err
	}
	for _, c := range costsIn {
		if c.unitPaise > 0 {
			_ = RecordStockReceiptCost(tenantID, c.sku, float64(c.qty), c.unitPaise)
		}
	}
	for _, wo := range autoWriteOffs {
		if err := postChallanWriteOff(tenantID, wo, userID); err != nil {
			LogSystemError(tenantID, userID, "WARN", "JobWorkLoss", fmt.Sprintf("%s: %v", wo, err), "")
		}
	}
	for _, ls := range losses {
		if !ls.now {
			_ = SubmitForApproval(tenantID, ChallanWriteOffDoctype, ls.b.WriteOffs[len(ls.b.WriteOffs)-1], userID, role)
		}
	}
	LogAuditEvent(tenantID, userID, "RECEIVE_JOB_WORK", "SUCCESS", fmt.Sprintf("%s: %d line(s) back, now %s", id, len(evLines), newStatus))
	return newStatus, nil
}

func jobWorkStatusAfter(bal []jobWorkBalance) string {
	for _, b := range bal {
		if b.Open() > 0 {
			return "Partly Received"
		}
	}
	return "Received"
}

// createChallanWriteOffTx records scrap / loss against the order's challan:
// Approved at once within tolerance (posted after commit), Draft otherwise
// (submitted for approval after commit).
func createChallanWriteOffTx(tx *sql.Tx, schema, tenantID, dcID string, order map[string]interface{}, b *jobWorkBalance, qty int, userID string, now bool) (string, error) {
	if !now {
		if gated, _ := IsApprovalGated(tenantID, ChallanWriteOffDoctype); !gated {
			return "", jobWorkFail(fmt.Sprintf("line %d: scrap and loss above the %.2f%% tolerance need approval - an administrator adds an approval rule for ChallanWriteOff first",
				b.LineNo, GetSettingFloat(tenantID, SettingJobworkLossTolerance)))
		}
	}
	id := NewDocID("CWO")
	status := "Draft"
	if now {
		status = "Approved"
	}
	raw, _ := json.Marshal(map[string]interface{}{"code": id, "challan_id": dcID, "challan_number": strField(order, "challan_number"), "location": strField(order, "location"),
		"purpose": "Job work", "sku": b.SKU, "batch_no": b.BatchNo, "qty": qty, "kind": challanKindLost, "reason": "job work scrap / loss",
		"job_work_order": strField(order, "code"), "amount": round2(b.UnitCost * float64(qty)), "within_tolerance": now, "requested_by": userID, "status": status})
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, $5)`, schema),
		id, ChallanWriteOffDoctype, string(raw), status, userID); err != nil {
		return "", err
	}
	return id, nil
}

// refreshJobWorkLosses moves approved write-offs from pending to loss, and
// drops rejected ones.
func refreshJobWorkLosses(tenantID string, bal []jobWorkBalance) {
	for i := range bal {
		var keep []string
		for _, wo := range bal[i].WriteOffs {
			data, status, err := fetchDocData(tenantID, ChallanWriteOffDoctype, wo)
			if err != nil || data == nil {
				continue
			}
			qty := int(numFromInterface(data["qty"]))
			switch status {
			case "Posted":
				bal[i].PendingLoss -= qty
				bal[i].Loss += qty
				share := float64(qty) / float64(maxInt(bal[i].Outward, 1))
				for w := 0; w < 3; w++ {
					bal[i].LossW[w] = round2(bal[i].LossW[w] + bal[i].OutW[w]*share)
				}
			case "Rejected":
				bal[i].PendingLoss -= qty
			default:
				keep = append(keep, wo)
			}
		}
		bal[i].WriteOffs = keep
	}
}

// convertChallanUnitsTx marks units of a challan line consumed by job work
// (turned into another item) - they will not come back as themselves.
func convertChallanUnitsTx(tx *sql.Tx, schema, dcID, sku, batch string, qty int) error {
	data, status, err := lockChallanTx(tx, schema, dcID)
	if err != nil {
		return err
	}
	bal, events := challanState(data)
	b := findChallanBalance(bal, sku, batch)
	if b == nil || qty > b.Open() {
		return jobWorkFail(fmt.Sprintf("%s: not %d out on the challan", sku, qty))
	}
	b.Converted += qty
	events = append(events, challanEvent{Kind: "converted", At: time.Now().UTC().Format(time.RFC3339), Lines: []map[string]interface{}{{"sku": sku, "batch_no": batch, "qty": qty}}})
	balRaw, _ := json.Marshal(bal)
	evRaw, _ := json.Marshal(events)
	newStatus := challanStatusAfter(bal)
	if newStatus == "Closed" && status != "Closed" && b.Lost+b.Kept == 0 {
		newStatus = "Returned"
	}
	return saveChallanTx(tx, schema, dcID, newStatus, map[string]interface{}{"challan_balance": string(balRaw), "challan_events": string(evRaw)})
}

// ReceiveFromSubcontractor (Stage 26.9.11's call) receives a one-line order
// in full: an order sent the old way brings its output in; one sent through
// a challan consumes what is out and brings actualQty of the output in.
func ReceiveFromSubcontractor(tenantID, id, userID string, actualQty float64) error {
	data, status, err := fetchSubcontractOrder(tenantID, id)
	if err != nil {
		return err
	}
	if strField(data, "challan_id") != "" {
		bal, _ := jobWorkState(data)
		if len(bal) != 1 {
			return jobWorkFail("this order has several lines - receive it line by line on the Job Work screen")
		}
		out := int(actualQty)
		_, err := ReceiveJobWork(tenantID, id, userID, "", fmt.Sprintf("full:%d", time.Now().UnixNano()),
			[]JobWorkReceiveLine{{LineNo: bal[0].LineNo, AcceptedQty: bal[0].Open() - bal[0].PendingLoss, OutputQty: &out}})
		return err
	}
	if status != "Sent" {
		return &ValidationError{Code: "MANUFA-0142", Message: fmt.Sprintf("only a Sent subcontract order can be received (current status: %s)", status)}
	}
	sku, _ := data["received_item_id"].(string)
	location, _ := data["location"].(string)
	if sku == "" || location == "" || actualQty <= 0 {
		return &ValidationError{Code: "MANUFA-0142", Message: "received_item_id, location, and a positive received qty are required to receive a subcontract order"}
	}
	items := []interface{}{map[string]interface{}{"sku": sku, "qty": actualQty}}
	if _, err := PostInventoryLedgerWithVoucher(tenantID, location, items, false, "SubcontractOrder", id, userID); err != nil {
		return fmt.Errorf("subcontract receive failed: %v", err)
	}
	data["actual_received_qty"] = actualQty
	data["received_date"] = BusinessToday()
	return saveSubcontractOrder(tenantID, id, data, "Received")
}

// PostJobWorkCharge books the job worker's processing bill: Dr job-work
// conversion cost, Cr the payable, spread over the lines by what was
// accepted. One bill number is booked once.
func PostJobWorkCharge(tenantID, id, userID, billNo string, amount float64) error {
	billNo = strings.TrimSpace(billNo)
	if billNo == "" || amount <= 0 {
		return jobWorkFail("enter the job worker's bill number and amount")
	}
	data, _, err := fetchSubcontractOrder(tenantID, id)
	if err != nil {
		return err
	}
	bal, events := jobWorkState(data)
	accepted := 0
	for _, b := range bal {
		accepted += b.Accepted
	}
	if accepted == 0 {
		return jobWorkFail("nothing has come back yet - book the charge against what was received")
	}
	for _, e := range events {
		if e.Kind == "charge" && e.Key == billNo {
			return jobWorkFail("bill " + billNo + " is already booked on this order")
		}
	}
	paise := RupeesToPaise(amount)
	var spent int64
	for i := range bal {
		share := paise * int64(bal[i].Accepted) / int64(accepted)
		if i == len(bal)-1 {
			share = paise - spent
		}
		spent += share
		bal[i].Charges = round2(bal[i].Charges + PaiseToRupees(share))
	}
	location := strField(data, "location")
	acc := ResolvePostingAccounts(tenantID, PostingEventJobworkCharge, PostingContext{LegalEntity: locationLegalEntity(tenantID, location)}, accountJobworkCharge, AccountAPSuspense)
	if err := PostDoubleEntry(tenantID, "SubcontractOrder", id, map[string]int64{acc.Debit: paise}, map[string]int64{acc.Credit: paise}, "", "JobWorkCharge:"+id+":"+billNo); err != nil {
		return err
	}
	events = append(events, challanEvent{Key: billNo, Kind: "charge", At: time.Now().UTC().Format(time.RFC3339), By: userID,
		Lines: []map[string]interface{}{{"bill_no": billNo, "amount": amount}}})
	balRaw, _ := json.Marshal(bal)
	evRaw, _ := json.Marshal(events)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if err := mergeDocData(schema, "SubcontractOrder", id, map[string]interface{}{"jw_balance": string(balRaw), "jw_events": string(evRaw),
		"processing_charges": round2(numFromInterface(data["processing_charges"]) + amount)}); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "JOB_WORK_CHARGE", "SUCCESS", fmt.Sprintf("%s: bill %s %.2f", id, billNo, amount))
	return nil
}

// JobWorkOverdue flags orders past their due date, and inputs within 30 days
// of the statutory limit, once each (the day-limits worker runs it).
func JobWorkOverdue(tenantID string, notify func(event, id string, vars map[string]string)) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	today := todayDate().Format("2006-01-02")
	soon := todayDate().AddDate(0, 0, 30).Format("2006-01-02")
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'due_date', ''), COALESCE(data->>'statutory_due', ''),
		COALESCE(data->>'overdue_flagged_on', ''), COALESCE(data->>'statutory_flagged_on', '')
		FROM %s.documents WHERE doctype = 'SubcontractOrder' AND deleted_at IS NULL AND status IN ('Sent', 'Partly Received')`, schema))
	if err != nil {
		return err
	}
	type flag struct{ id, key, event string }
	var flags []flag
	for rows.Next() {
		var id, due, stat, overdueOn, statOn string
		if rows.Scan(&id, &due, &stat, &overdueOn, &statOn) != nil {
			continue
		}
		if due != "" && due < today && overdueOn == "" {
			flags = append(flags, flag{id, "overdue_flagged_on", "Job Work Overdue"})
		}
		if stat != "" && stat <= soon && statOn == "" {
			flags = append(flags, flag{id, "statutory_flagged_on", "Job Work Statutory Return Due"})
		}
	}
	rows.Close()
	for _, f := range flags {
		if err := mergeDocData(schema, "SubcontractOrder", f.id, map[string]interface{}{f.key: today}); err != nil {
			return err
		}
		notify(f.event, f.id, map[string]string{"order_id": f.id})
	}
	return nil
}

// --- reports ------------------------------------------------------------------

func jobWorkOrders(tenantID string) ([]reportDoc, error) {
	return docsBetween(tenantID, "SubcontractOrder", map[string]string{})
}

func jobWorkOutstanding(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	docs, err := jobWorkOrders(tenantID)
	if err != nil {
		return nil, err
	}
	today := todayDate()
	var out []map[string]interface{}
	for _, d := range docs {
		if d.status != "Sent" && d.status != "Partly Received" {
			continue
		}
		bal, _ := jobWorkState(d.data)
		refreshJobWorkLosses(tenantID, bal)
		overdue := 0
		if t, err := time.ParseInLocation("2006-01-02", strField(d.data, "due_date"), time.Local); err == nil && today.After(t) {
			overdue = int(today.Sub(t).Hours() / 24)
		}
		for _, b := range bal {
			if b.Open() <= 0 {
				continue
			}
			out = append(out, map[string]interface{}{"order": firstNonEmpty(strField(d.data, "code"), d.id), "vendor": strField(d.data, "vendor_id"), "line_no": b.LineNo,
				"sku": b.SKU, "outward": b.Outward, "accepted": b.Accepted, "rejected": b.Rejected, "loss": b.Loss, "pending_loss": b.PendingLoss, "open": b.Open(),
				"open_net_weight": b.OpenWeight(1), "open_fine_weight": b.OpenWeight(2), "sent_date": strField(d.data, "sent_date"),
				"due_date": strField(d.data, "due_date"), "days_overdue": overdue, "statutory_due": strField(d.data, "statutory_due")})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i]["days_overdue"].(int) > out[j]["days_overdue"].(int) })
	return out, nil
}

func jobWorkYield(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	docs, err := jobWorkOrders(tenantID)
	if err != nil {
		return nil, err
	}
	var out []map[string]interface{}
	for _, d := range docs {
		bal, _ := jobWorkState(d.data)
		refreshJobWorkLosses(tenantID, bal)
		perUnit := map[int]float64{}
		for _, l := range jobWorkLines(d.data) {
			perUnit[l.LineNo] = l.yield()
		}
		for _, b := range bal {
			consumed := b.Accepted + b.Rejected
			if consumed+b.Loss == 0 {
				continue
			}
			// Yield is what came out against what the material should give:
			// the output's pieces (good and rejected) over consumed x yield
			// per unit; for the same item back, the share accepted.
			output, outputRejected := b.OutputQty, b.OutputRejected
			if b.OutputSKU == "" || b.OutputSKU == b.SKU {
				output, outputRejected = b.Accepted, b.Rejected
			}
			expected := float64(consumed) * perUnit[b.LineNo]
			material := round2(b.UnitCost * float64(consumed+b.Loss))
			row := map[string]interface{}{"order": firstNonEmpty(strField(d.data, "code"), d.id), "vendor": strField(d.data, "vendor_id"), "sku": b.SKU,
				"output_sku": firstNonEmpty(b.OutputSKU, b.SKU), "consumed": consumed, "output_qty": output, "output_rejected": outputRejected, "material_value": material, "charges": b.Charges,
				"yield_pct": 0.0, "wastage_pct": 0.0, "cost_per_output": 0.0}
			if consumed+b.Loss > 0 {
				row["wastage_pct"] = round2(float64(b.Loss) * 100 / float64(consumed+b.Loss))
			}
			if b.OutputSKU == "" || b.OutputSKU == b.SKU {
				if consumed > 0 {
					row["yield_pct"] = round2(float64(output) * 100 / float64(consumed))
				}
			} else if expected > 0 {
				row["yield_pct"] = round2(float64(output+outputRejected) * 100 / expected)
			}
			if output > 0 {
				row["cost_per_output"] = round2((material + b.Charges) / float64(output))
			}
			out = append(out, row)
		}
	}
	return out, nil
}
