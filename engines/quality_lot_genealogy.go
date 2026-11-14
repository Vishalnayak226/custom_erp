package engines

import (
	"crypto/sha1"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
)

// 170.14 - lot genealogy. When a work order finishes a quantity into a named
// output lot, one LotGenealogy row records each input lot that went into it.
// A recall trace then runs both ways: from a raw-material lot forward to the
// lots made from it and everything that happened to them, and from an output
// lot backward to the input lots and the vendor receipts behind them.
//
// Where the input lots come from: the order's own `input_lots` list (sku,
// batch_no, qty) when the planner recorded one, otherwise the lots the
// batch-tracked components are drawn from first-expiry-first-out at the order's
// location, marked "Inferred". Nothing here moves stock or changes a posting.

const (
	lotGenealogyDoctype = "LotGenealogy"
	lotTraceMaxDepth    = 6
	lotTraceMaxRows     = 2000

	TraceForward  = "Forward"
	TraceBackward = "Backward"
	TraceBoth     = "Both"
)

// lotInput is one input lot of one completion.
type lotInput struct {
	Sku, BatchNo, Source string
	Qty                  float64
}

// recordLotGenealogy is the one call finishProductionQty makes after a
// completion has committed. Idempotent per order, completion and input lot; a
// failure is logged and never fails the completion that already posted.
func recordLotGenealogy(tenantID, orderID, outputSku, outputBatch string, qty float64, completion int) {
	outputBatch = strings.TrimSpace(outputBatch)
	if outputBatch == "" || qty <= 0 {
		return
	}
	if err := writeLotGenealogy(tenantID, orderID, outputSku, outputBatch, qty, completion); err != nil {
		log.Printf("lot genealogy for %s completion %d not recorded: %v", orderID, completion, err)
	}
}

func writeLotGenealogy(tenantID, orderID, outputSku, outputBatch string, qty float64, completion int) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, _, err := fetchProductionOrder(tenantID, orderID)
	if err != nil {
		return err
	}
	inputs := lotInputsFor(tenantID, data, qty)
	for _, in := range inputs {
		sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%s|%s|%s", orderID, completion, outputBatch, in.Sku, in.BatchNo)))
		id := fmt.Sprintf("LGN-%x", sum[:8])
		row := map[string]interface{}{
			"id": id, "work_order": orderID, "completion": completion,
			"output_sku": outputSku, "output_batch": outputBatch, "output_qty": qty,
			"input_sku": in.Sku, "input_batch": in.BatchNo, "input_qty": in.Qty,
			"source": in.Source, "location": strField(data, "location"), "status": "Recorded",
		}
		b, _ := json.Marshal(row)
		if _, err := db.DB.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Recorded', 'system') ON CONFLICT (id) DO NOTHING`, schema),
			id, lotGenealogyDoctype, b); err != nil {
			return err
		}
	}
	return nil
}

// lotInputsFor lists the input lots behind `qty` finished units of an order.
func lotInputsFor(tenantID string, order map[string]interface{}, qty float64) []lotInput {
	orderQty := numFromInterface(order["quantity"])
	share := 1.0
	if orderQty > 0 {
		share = math.Min(qty/orderQty, 1)
	}
	var out []lotInput
	for _, r := range jsonRows(order["input_lots"]) {
		sku, batch := strings.TrimSpace(strField(r, "sku")), strings.TrimSpace(strField(r, "batch_no"))
		if sku == "" || batch == "" {
			continue
		}
		out = append(out, lotInput{Sku: sku, BatchNo: batch, Qty: math.Round(numFromInterface(r["qty"])*share*10000) / 10000, Source: "Recorded"})
	}
	if len(out) > 0 {
		return out
	}
	// Inferred: batch-tracked components, first expiry first out.
	var comps []bomComponent
	if snap, _ := order["bom_snapshot"].(string); snap != "" {
		_ = json.Unmarshal([]byte(snap), &comps)
	}
	location := strField(order, "location")
	for _, c := range comps {
		tr, err := GetItemTracking(tenantID, c.Sku)
		if err != nil || !tr.TracksBatch() {
			continue
		}
		need := int(math.Ceil(c.Qty*qty - 1e-9))
		got, _, err := AllocateFromStock(tenantID, c.Sku, location, need)
		if err != nil {
			continue
		}
		byBatch := map[string]float64{}
		var seenOrder []string
		for _, a := range got {
			if a.BatchNo == "" {
				continue
			}
			if _, seen := byBatch[a.BatchNo]; !seen {
				seenOrder = append(seenOrder, a.BatchNo)
			}
			byBatch[a.BatchNo] += float64(a.Qty)
		}
		for _, b := range seenOrder {
			out = append(out, lotInput{Sku: c.Sku, BatchNo: b, Qty: byBatch[b], Source: "Inferred"})
		}
	}
	return out
}

// LotTraceRow is one line of a two-way lot trace.
type LotTraceRow struct {
	Direction string  `json:"direction"`
	Level     int     `json:"level"`
	Relation  string  `json:"relation"`
	Sku       string  `json:"sku"`
	BatchNo   string  `json:"batch_no"`
	Qty       float64 `json:"qty"`
	Document  string  `json:"document"`
	Party     string  `json:"party,omitempty"`
	On        string  `json:"on,omitempty"`
	Source    string  `json:"source,omitempty"`
}

// TraceLot walks the recall both ways (or one way) from a lot.
func TraceLot(tenantID, sku, batchNo, direction string) ([]LotTraceRow, error) {
	batchNo = strings.TrimSpace(batchNo)
	if batchNo == "" {
		return nil, &ValidationError{Code: "MFG-1760", Message: "a lot number is required"}
	}
	switch direction {
	case "", TraceBoth:
		direction = TraceBoth
	case TraceForward, TraceBackward:
	default:
		return nil, &ValidationError{Code: "MFG-1761", Message: "trace direction must be Forward, Backward or Both"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	t := &lotTracer{tenantID: tenantID, schema: schema}
	if direction != TraceBackward {
		t.forward(strings.TrimSpace(sku), batchNo, 0, map[string]bool{})
	}
	if direction != TraceForward {
		t.backward(strings.TrimSpace(sku), batchNo, 0, map[string]bool{})
	}
	if t.err != nil {
		return nil, t.err
	}
	return t.rows, nil
}

type lotTracer struct {
	tenantID, schema string
	rows             []LotTraceRow
	err              error
}

func (t *lotTracer) add(r LotTraceRow) {
	if len(t.rows) < lotTraceMaxRows {
		t.rows = append(t.rows, r)
	}
}

func (t *lotTracer) links(col string, sku, batchNo string) []map[string]interface{} {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
		AND data->>'%s_batch' = $2 AND ($3 = '' OR data->>'%s_sku' = $3) ORDER BY created_at, id LIMIT 500`, t.schema, col, col),
		lotGenealogyDoctype, batchNo, sku)
	if err != nil {
		t.err = err
		return nil
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var raw []byte
		var m map[string]interface{}
		if rows.Scan(&raw) == nil && json.Unmarshal(raw, &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

func (t *lotTracer) forward(sku, batchNo string, level int, seen map[string]bool) {
	key := sku + "|" + batchNo
	if seen[key] || level > lotTraceMaxDepth || t.err != nil {
		return
	}
	seen[key] = true
	moves, err := GetBatchMovementHistory(t.tenantID, sku, batchNo)
	if err != nil {
		t.err = err
		return
	}
	for _, m := range moves {
		t.add(LotTraceRow{Direction: TraceForward, Level: level, Relation: "Movement", Sku: m.Sku, BatchNo: m.BatchNo,
			Qty: m.Qty, Document: strings.TrimSpace(m.VoucherType + " " + m.VoucherID), Party: m.Warehouse, On: m.MovedAt})
	}
	for _, l := range t.links("input", sku, batchNo) {
		osku, ob := strField(l, "output_sku"), strField(l, "output_batch")
		t.add(LotTraceRow{Direction: TraceForward, Level: level + 1, Relation: "Made into", Sku: osku, BatchNo: ob,
			Qty: numFromInterface(l["output_qty"]), Document: strField(l, "work_order"), Source: strField(l, "source")})
		t.forward(osku, ob, level+1, seen)
	}
}

func (t *lotTracer) backward(sku, batchNo string, level int, seen map[string]bool) {
	key := sku + "|" + batchNo
	if seen[key] || level > lotTraceMaxDepth || t.err != nil {
		return
	}
	seen[key] = true
	links := t.links("output", sku, batchNo)
	for _, l := range links {
		isku, ib := strField(l, "input_sku"), strField(l, "input_batch")
		t.add(LotTraceRow{Direction: TraceBackward, Level: level + 1, Relation: "Made from", Sku: isku, BatchNo: ib,
			Qty: numFromInterface(l["input_qty"]), Document: strField(l, "work_order"), Source: strField(l, "source")})
		t.backward(isku, ib, level+1, seen)
	}
	// A lot nothing was made from came in by receipt: name the vendor receipt.
	if len(links) == 0 {
		if b, err := GetBatch(t.tenantID, sku, batchNo); err == nil && b != nil {
			t.add(LotTraceRow{Direction: TraceBackward, Level: level, Relation: "Received", Sku: b.Item, BatchNo: b.BatchNo,
				Document: b.GRN, Party: b.Vendor, On: b.ReceivedOn})
		}
	}
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "lot-trace", Label: "Lot Trace (Both Ways)", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "direction", Label: "Direction"}, {Key: "level", Label: "Step"}, {Key: "relation", Label: "What"},
			{Key: "sku", Label: "Item"}, {Key: "batch_no", Label: "Batch / Lot"}, {Key: "qty", Label: "Qty"},
			{Key: "document", Label: "Document"}, {Key: "party", Label: "Vendor / Location"}, {Key: "on", Label: "When"},
			{Key: "source", Label: "Lot source"},
		},
		Params: []ReportParam{
			{Key: "batch_no", Label: "Batch / Lot", Type: "text", Required: true},
			{Key: "sku", Label: "Item Code (optional)", Type: "text"},
			{Key: "direction", Label: "Direction", Type: "select", Options: "Both,Forward,Backward"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := TraceLot(tenantID, params["sku"], params["batch_no"], params["direction"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}
