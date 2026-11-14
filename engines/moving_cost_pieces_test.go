package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Stage 148.1, the piece paths: what production and job work make carries its own
// cost, a piece named at dispatch costs what it cost, and a bundle or a handled
// till line keeps what the sale booked. Stock value equals GL 1200 after each.

// wantValueEqualsGL holds the whole control total: every SKU's value in the
// valuation against 1200 on the documents that moved them.
func (m *mc1481) wantValueEqualsGL(skus []string, docs ...string) {
	m.t.Helper()
	var value int64
	rows, err := GetInventoryValuation(m.tenant)
	if err != nil {
		m.t.Fatalf("GetInventoryValuation: %v", err)
	}
	for _, sku := range skus {
		for _, r := range rows {
			if r["sku"] == sku {
				value += RupeesToPaise(numFromInterface(r["total_value"]))
			}
		}
	}
	ledger := m.glAll("1200", docs...)
	if diff := value - ledger; diff < -100 || diff > 100 {
		m.t.Errorf("stock value %d against GL 1200 %d: more than Rs 1 apart", value, ledger)
	}
}

func (m *mc1481) code(prefix string) string { return m.id(prefix) }

// --- Production ---------------------------------------------------------------

// mc1481Bom makes an item needing two of rm per unit.
func (m *mc1481) mc1481Bom(rm string, output map[string]interface{}) (fg, bom string) {
	m.t.Helper()
	fg = m.item(output)
	bom = m.id("BOM")
	comps, _ := json.Marshal([]map[string]interface{}{{"sku": rm, "qty": 2}})
	m.doc(bom, "BOM", "Active", map[string]interface{}{"code": bom, "parent_item": fg, "components": string(comps), "status": "Active"})
	return fg, bom
}

func (m *mc1481) issuedOrder(bom string, qty float64) string {
	m.t.Helper()
	id := m.id("PRO")
	m.doc(id, "ProductionOrder", "Draft", map[string]interface{}{"bom_id": bom, "quantity": qty, "location": m.loc, "status": "Draft"})
	if err := IssueProductionMaterial(m.tenant, id); err != nil {
		m.t.Fatalf("issue %s: %v", id, err)
	}
	return id
}

// Two stools, made when planks cost Rs 50 and Rs 150: Rs 100 and Rs 300 of
// material, an average of Rs 200. Sold by name, the cheap one costs Rs 100 and the
// one left in stock is worth what it is worth.
func TestStage1481ProductionPieceCarriesItsOwnCost(t *testing.T) {
	m := newMC1481(t)
	rm := m.item(nil)
	cheap, dear := m.code("P"), m.code("P")
	fg, bom := m.mc1481Bom(rm, map[string]interface{}{"tracking_mode": TrackingSerial})

	grn1, _ := m.receive(rm, 2, 50)
	if err := CompleteProductionOrder(m.tenant, m.issuedOrder(bom, 1), OutputPieces{SerialNos: []string{cheap}}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	grn2, _ := m.receive(rm, 2, 150)
	if err := ReportPartialCompletion(m.tenant, m.issuedOrder(bom, 1), 1, OutputPieces{SerialNos: []string{dear}}); err != nil {
		t.Fatalf("partial: %v", err)
	}
	m.wantAvg(fg, 20000, "two made")
	skus := []string{rm, fg}
	m.wantValueEqualsGL(skus, grn1, grn2)
	if got := numFromInterface(m.pieceData("SerialNumber", "serial_no", cheap)["receipt_cost_paise"]); got != 10000 {
		t.Errorf("cheap stool stamped %v, want 10000", got)
	}
	if got := numFromInterface(m.pieceData("SerialNumber", "serial_no", dear)["receipt_cost_paise"]); got != 30000 {
		t.Errorf("dear stool stamped %v, want 30000", got)
	}
	cart, _ := m.mustSell(fg, 1, map[string]interface{}{"serial_nos": []string{cheap}})
	if got := m.gl(cart, "5100"); got != 10000 {
		t.Errorf("COGS %d, want 10000", got)
	}
	m.wantAvg(fg, 30000, "the dear one left")
	m.wantValueEqualsGL(skus, grn1, grn2, cart)
}

// Rs 100 a unit for lot A (two of them), Rs 300 for lot B.
func TestStage1481ProductionLotCarriesItsOwnCost(t *testing.T) {
	m := newMC1481(t)
	rm := m.item(nil)
	lotA, lotB := m.code("LOT"), m.code("LOT")
	fg, bom := m.mc1481Bom(rm, map[string]interface{}{"tracking_mode": TrackingBatch})

	grn1, _ := m.receive(rm, 4, 50)
	if err := CompleteProductionOrder(m.tenant, m.issuedOrder(bom, 2), OutputPieces{BatchNo: lotA}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	grn2, _ := m.receive(rm, 2, 150)
	if err := CompleteProductionOrder(m.tenant, m.issuedOrder(bom, 1), OutputPieces{BatchNo: lotB}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got := numFromInterface(m.pieceData("Batch", "batch_no", lotA)["receipt_cost_paise"]); got != 10000 {
		t.Errorf("lot A stamped %v, want 10000", got)
	}
	cart, _ := m.mustSell(fg, 1, map[string]interface{}{"batch_no": lotB})
	if got := m.gl(cart, "5100"); got != 30000 {
		t.Errorf("COGS %d, want 30000", got)
	}
	// The average of three is 16667, a third of a paisa up: lot A is left at 10001.
	m.wantAvg(fg, 10001, "lot A left")
	m.wantValueEqualsGL([]string{rm, fg}, grn1, grn2, cart)
	if v, _ := m.ledgerBatch(fg, lotA); v != 2 {
		t.Errorf("lot A in the ledger: %v, want 2", v)
	}
}

func (m *mc1481) ledgerBatch(sku, lot string) (float64, error) {
	var qty float64
	err := db.DB.QueryRow(`SELECT COALESCE(SUM((data->>'qty')::numeric), 0) FROM `+m.schema+`.documents
		WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1 AND data->>'batch_no' = $2`, sku, lot).Scan(&qty)
	return qty, err
}

// A code already in use refuses before any stock moves, and the order stays open.
func TestStage1481ProductionDuplicateCodeRefusedBeforeStockMoves(t *testing.T) {
	m := newMC1481(t)
	rm := m.item(nil)
	code := m.code("P")
	fg, bom := m.mc1481Bom(rm, map[string]interface{}{"tracking_mode": TrackingSerial})
	m.receive(rm, 4, 50)
	if err := CompleteProductionOrder(m.tenant, m.issuedOrder(bom, 1), OutputPieces{SerialNos: []string{code}}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	second := m.issuedOrder(bom, 1)
	before := m.onHand(fg)
	err := CompleteProductionOrder(m.tenant, second, OutputPieces{SerialNos: []string{code}})
	if t1481Code(err) != "FIN-0911" {
		t.Fatalf("duplicate: %v, want FIN-0911", err)
	}
	if got := m.onHand(fg); got != before {
		t.Errorf("stock moved: %v to %v", before, got)
	}
	if data, status, _ := fetchProductionOrder(m.tenant, second); status == "Completed" || numFromInterface(data["completed_qty"]) != 0 {
		t.Errorf("order %s, completed %v", status, data["completed_qty"])
	}
	// The count of codes has to match what was made.
	err = CompleteProductionOrder(m.tenant, second, OutputPieces{SerialNos: []string{m.code("P"), m.code("P")}})
	if err == nil {
		t.Error("two codes for one made went through")
	}
	if err := CompleteProductionOrder(m.tenant, second, OutputPieces{SerialNos: []string{m.code("P")}}); err != nil {
		t.Errorf("a fresh code: %v", err)
	}
}

// With nothing named, the output posts as it always did.
func TestStage1481UnnamedOutputStaysAsItWas(t *testing.T) {
	m := newMC1481(t)
	rm := m.item(nil)
	for _, output := range []map[string]interface{}{nil, {"tracking_mode": TrackingSerial}} {
		fg, bom := m.mc1481Bom(rm, output)
		grn, _ := m.receive(rm, 2, 50)
		if err := CompleteProductionOrder(m.tenant, m.issuedOrder(bom, 1)); err != nil {
			t.Fatalf("complete: %v", err)
		}
		m.wantAvg(fg, 10000, "unnamed")
		m.wantValueEqualsGL([]string{rm, fg}, grn)
		var pieces int
		if err := db.DB.QueryRow(`SELECT COUNT(*) FROM `+m.schema+`.documents WHERE doctype = 'SerialNumber' AND data->>'item' = $1`, fg).Scan(&pieces); err != nil || pieces != 0 {
			t.Errorf("pieces %d (%v)", pieces, err)
		}
		cart, _ := m.mustSell(fg, 1, nil)
		if got := m.gl(cart, "5100"); got != 10000 {
			t.Errorf("COGS %d, want 10000", got)
		}
	}
}

// --- Job work -----------------------------------------------------------------

// jobWorkSent sends material out on a converting order: two of it make one output.
func (m *mc1481) jobWorkSent(material, output string, qty int) string {
	m.t.Helper()
	id := m.id("JW")
	lines, _ := json.Marshal([]map[string]interface{}{{"sku": material, "qty": qty, "output_sku": output, "output_per_unit": 0.5}})
	m.doc(id, "SubcontractOrder", "Draft", map[string]interface{}{"code": id, "vendor_id": m.id("V"), "location": m.loc, "due_date": "2099-01-01", "lines": string(lines)})
	if st, err := SendJobWork(m.tenant, id, "system", RoleStoreManager, ChallanDispatch{VehicleNo: "KA01"}); err != nil || st != "Sent" {
		m.t.Fatalf("send: %s %v", st, err)
	}
	return id
}

func (m *mc1481) dropChallans() {
	m.t.Cleanup(func() {
		q := `DELETE FROM ` + m.schema + `.documents WHERE doctype IN ('DeliveryChallan', 'ChallanWriteOff') AND data->>'location' = $1`
		_, _ = db.DB.Exec(q, m.loc)
	})
}

func TestStage1481JobWorkPieceCarriesItsOwnCost(t *testing.T) {
	m := newMC1481(t)
	m.dropChallans()
	rm := m.item(map[string]interface{}{"valuation": ValuationValuated})
	ring := m.item(map[string]interface{}{"tracking_mode": TrackingSerial, "valuation": ValuationValuated})
	cheap, dear := m.code("P"), m.code("P")

	grn1, _ := m.receive(rm, 2, 100)
	one := m.jobWorkSent(rm, ring, 2)
	if _, err := ReceiveJobWork(m.tenant, one, "system", RoleStoreManager, "K1", []JobWorkReceiveLine{{LineNo: 1, AcceptedQty: 2, OutputSerials: []string{cheap}}}); err != nil {
		t.Fatalf("receive: %v", err)
	}
	grn2, _ := m.receive(rm, 2, 300)
	two := m.jobWorkSent(rm, ring, 2)
	if _, err := ReceiveJobWork(m.tenant, two, "system", RoleStoreManager, "K1", []JobWorkReceiveLine{{LineNo: 1, AcceptedQty: 2, OutputSerials: []string{dear}}}); err != nil {
		t.Fatalf("receive: %v", err)
	}
	m.wantAvg(ring, 40000, "two made")
	if got := numFromInterface(m.pieceData("SerialNumber", "serial_no", dear)["receipt_cost_paise"]); got != 60000 {
		t.Errorf("dear ring stamped %v, want 60000", got)
	}
	skus := []string{rm, ring}
	m.wantValueEqualsGL(skus, grn1, grn2)
	cart, _ := m.mustSell(ring, 1, map[string]interface{}{"serial_nos": []string{dear}})
	if got := m.gl(cart, "5100"); got != 60000 {
		t.Errorf("COGS %d, want 60000", got)
	}
	m.wantAvg(ring, 20000, "the cheap one left")
	m.wantValueEqualsGL(skus, grn1, grn2, cart)
}

func TestStage1481JobWorkLotAndRefusal(t *testing.T) {
	m := newMC1481(t)
	m.dropChallans()
	rm := m.item(map[string]interface{}{"valuation": ValuationValuated})
	ring := m.item(map[string]interface{}{"tracking_mode": TrackingBatch, "valuation": ValuationValuated})
	piece := m.item(map[string]interface{}{"tracking_mode": TrackingSerial, "valuation": ValuationValuated})
	lot, code := m.code("LOT"), m.code("P")

	grn, _ := m.receive(rm, 8, 100)
	one := m.jobWorkSent(rm, ring, 4)
	if _, err := ReceiveJobWork(m.tenant, one, "system", RoleStoreManager, "K1", []JobWorkReceiveLine{{LineNo: 1, AcceptedQty: 4, OutputBatchNo: lot}}); err != nil {
		t.Fatalf("receive: %v", err)
	}
	if got := numFromInterface(m.pieceData("Batch", "batch_no", lot)["receipt_cost_paise"]); got != 20000 {
		t.Errorf("lot stamped %v, want 20000", got)
	}
	cart, _ := m.mustSell(ring, 1, map[string]interface{}{"batch_no": lot})
	if got := m.gl(cart, "5100"); got != 20000 {
		t.Errorf("COGS %d, want 20000", got)
	}

	// A code in use refuses before anything moves, and the receipt can be retried.
	if _, err := RegisterSerial(m.tenant, SerialInfo{SerialNo: code, Item: piece}, m.loc, "Test", "T1481", "system"); err != nil {
		t.Fatal(err)
	}
	two := m.jobWorkSent(rm, piece, 2)
	wantOnHand := m.onHand(rm)
	if _, err := ReceiveJobWork(m.tenant, two, "system", RoleStoreManager, "K2", []JobWorkReceiveLine{{LineNo: 1, AcceptedQty: 2, OutputSerials: []string{code}}}); t1481Code(err) != "FIN-0911" {
		t.Fatalf("duplicate: %v, want FIN-0911", err)
	}
	if got := m.onHand(rm); got != wantOnHand {
		t.Errorf("material moved: %v to %v", wantOnHand, got)
	}
	if _, err := ReceiveJobWork(m.tenant, two, "system", RoleStoreManager, "K2", []JobWorkReceiveLine{{LineNo: 1, AcceptedQty: 2, OutputSerials: []string{m.code("P")}}}); err != nil {
		t.Fatalf("a fresh code: %v", err)
	}
	m.wantValueEqualsGL([]string{rm, ring, piece}, grn, cart)
}

// --- Dispatch -----------------------------------------------------------------

// mc1481Order is a reserved order, released to one task.
type mc1481Order struct {
	id, task string
}

func (m *mc1481) releasedOrder(sku string, qty int) mc1481Order {
	m.t.Helper()
	order, line := m.id("SO"), m.id("SOL")
	m.doc(order, "SalesOrder", "Reserved", map[string]interface{}{"code": order, "order_status": "Reserved", "total_amount": 1000 * qty, "customer_name": "T1481"})
	m.doc(line, "SalesOrderLine", "Reserved", map[string]interface{}{"code": line, "order_id": order, "sku": sku, "qty": qty, "unit_price": 1000, "location_code": m.loc, "line_status": "Reserved"})
	if _, err := CreateReservation(m.tenant, sku, m.loc, qty, "Online", 0, ReservationAttribution{OrderID: order, LineID: line}); err != nil {
		m.t.Fatalf("reserve: %v", err)
	}
	tasks, _, err := ReleaseOrderToFulfillment(m.tenant, order, "system")
	if err != nil || len(tasks) == 0 {
		m.t.Fatalf("release: %v %v", tasks, err)
	}
	m.docs = append(m.docs, tasks...)
	return mc1481Order{id: order, task: tasks[0]}
}

// serialStock receives two pieces at Rs 100 and Rs 300.
func (m *mc1481) serialStock() (sku, cheap, dear string, docs []string) {
	m.t.Helper()
	sku = m.item(map[string]interface{}{"tracking_mode": TrackingSerial})
	cheap, dear = m.code("P"), m.code("P")
	g1, _ := m.receiveAt(m.loc, sku, 1, 100, map[string]interface{}{"serial_numbers": []interface{}{cheap}})
	g2, _ := m.receiveAt(m.loc, sku, 1, 300, map[string]interface{}{"serial_numbers": []interface{}{dear}})
	return sku, cheap, dear, []string{g1, g2}
}

// The Rs 300 piece scanned at Pick costs Rs 300 at dispatch, not the Rs 200 average,
// and the one left is worth Rs 100.
func TestStage1481DispatchNamedPieceCostsItsOwn(t *testing.T) {
	m := newMC1481(t)
	sku, _, dear, docs := m.serialStock()
	o := m.releasedOrder(sku, 1)
	got, picked, err := ScanPickItem(m.tenant, o.task, dear)
	if err != nil || got != sku || picked != 1 {
		t.Fatalf("pick: %s %d %v", got, picked, err)
	}
	if _, packed, err := ScanPackItem(m.tenant, o.task, dear); err != nil || packed != 1 {
		t.Fatalf("pack: %d %v", packed, err)
	}
	if err := TransitionTaskStatus(m.tenant, o.task, "Dispatched"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if a := m.gl(o.task, "5100"); a != 30000 {
		t.Errorf("COGS %d, want 30000", a)
	}
	m.wantBalanced(o.task)
	m.wantAvg(sku, 10000, "the cheap one left")
	m.wantStockEqualsGL(sku, append(docs, o.task)...)
	if s := m.pieceData("SerialNumber", "serial_no", dear)["status"]; s != SerialShipped {
		t.Errorf("piece %v, want Shipped", s)
	}
	var costs string
	if err := db.DB.QueryRow(`SELECT COALESCE(data->>'sale_costs', '') FROM `+m.schema+`.documents WHERE id = $1`, o.task).Scan(&costs); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(costs, dear) {
		t.Errorf("the task keeps no record of the piece: %q", costs)
	}
}

// Named at Pack when Pick scanned the item's own barcode.
func TestStage1481DispatchPieceNamedAtPack(t *testing.T) {
	m := newMC1481(t)
	sku, cheap, _, docs := m.serialStock()
	o := m.releasedOrder(sku, 1)
	if _, _, err := ScanPickItem(m.tenant, o.task, sku); err != nil {
		t.Fatalf("pick: %v", err)
	}
	if _, _, err := ScanPackItem(m.tenant, o.task, cheap); err != nil {
		t.Fatalf("pack: %v", err)
	}
	if err := TransitionTaskStatus(m.tenant, o.task, "Dispatched"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if a := m.gl(o.task, "5100"); a != 10000 {
		t.Errorf("COGS %d, want 10000", a)
	}
	m.wantAvg(sku, 30000, "the dear one left")
	m.wantStockEqualsGL(sku, append(docs, o.task)...)
}

// Nothing named: the average, the same posting, no record on the task.
func TestStage1481DispatchUnnamedStaysAtTheAverage(t *testing.T) {
	m := newMC1481(t)
	sku, _, _, docs := m.serialStock()
	o := m.releasedOrder(sku, 1)
	if err := TransitionTaskStatus(m.tenant, o.task, "Dispatched"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if a := m.gl(o.task, "5100"); a != 20000 {
		t.Errorf("COGS %d, want 20000", a)
	}
	var has bool
	if err := db.DB.QueryRow(`SELECT data ? 'sale_costs' FROM `+m.schema+`.documents WHERE id = $1`, o.task).Scan(&has); err != nil || has {
		t.Errorf("sale_costs on an unnamed task: %v %v", has, err)
	}
	m.wantAvg(sku, 20000, "unnamed")
	m.wantStockEqualsGL(sku, append(docs, o.task)...)
}

// What a scan can't name: a code from another item, one elsewhere, one already out,
// one on another task, and the same one twice.
func TestStage1481PickScanRefusesAPieceItCannotName(t *testing.T) {
	m := newMC1481(t)
	sku, cheap, dear, _ := m.serialStock()
	other, otherPiece, _, _ := m.serialStock()
	o := m.releasedOrder(sku, 2)
	if _, _, err := ScanPickItem(m.tenant, o.task, otherPiece); err == nil || !strings.Contains(err.Error(), other) {
		t.Errorf("another item's piece: %v", err)
	}
	if _, _, err := ScanPickItem(m.tenant, o.task, cheap); err != nil {
		t.Fatalf("pick: %v", err)
	}
	if _, picked, err := ScanPickItem(m.tenant, o.task, cheap); err == nil {
		t.Errorf("the same piece twice: picked %d", picked)
	}
	// On another live task.
	second := m.releasedOrderAnyQty(other, 1)
	third := m.releasedOrderAnyQty(other, 1)
	if _, _, err := ScanPickItem(m.tenant, second.task, otherPiece); err != nil {
		t.Fatalf("pick: %v", err)
	}
	if _, _, err := ScanPickItem(m.tenant, third.task, otherPiece); err == nil {
		t.Error("a piece on another task")
	}
	// Sold at the till after it was picked: the dispatch refuses, nothing posts.
	if _, _, err := ScanPickItem(m.tenant, o.task, dear); err != nil {
		t.Fatalf("pick: %v", err)
	}
	// One more on the shelf, as the order reserves the two it has.
	m.receiveAt(m.loc, sku, 1, 100, map[string]interface{}{"serial_numbers": []interface{}{m.code("P")}})
	m.mustSell(sku, 1, map[string]interface{}{"serial_nos": []string{dear}})
	err := TransitionTaskStatus(m.tenant, o.task, "Dispatched")
	if t1481Code(err) != "FIN-0850" {
		t.Errorf("a sold piece: %v, want FIN-0850", err)
	}
	if a := m.gl(o.task, "5100"); a != 0 {
		t.Errorf("posted %d", a)
	}
	// Rejecting clears the names, so the next node starts clean.
	if err := TransitionTaskStatus(m.tenant, o.task, "Rejected"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	var left string
	if err := db.DB.QueryRow(`SELECT data->>'items' FROM `+m.schema+`.documents WHERE id = $1`, o.task).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(left, "serial_nos") {
		t.Errorf("names kept on a rejected task: %s", left)
	}
}

func (m *mc1481) releasedOrderAnyQty(sku string, qty int) mc1481Order {
	return m.releasedOrder(sku, qty)
}

// A customer's return of a dispatched piece comes back at what the dispatch booked,
// the piece goes back in stock, and the books agree.
func TestStage1481DispatchedPieceComesBackAtItsCost(t *testing.T) {
	m := newMC1481(t)
	sku, _, dear, docs := m.serialStock()
	o := m.releasedOrder(sku, 1)
	if _, _, err := ScanPickItem(m.tenant, o.task, dear); err != nil {
		t.Fatalf("pick: %v", err)
	}
	if err := TransitionTaskStatus(m.tenant, o.task, "Dispatched"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	inv := m.id("SI")
	lines, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 1, "unit_price": 1000.0}})
	m.doc(inv, "SalesInvoice", "Paid", map[string]interface{}{"code": inv, "sales_order_id": o.id, "items": string(lines), "invoice_date": BusinessToday()})
	prices, err := resolveOriginalSaleLinePrices(m.tenant, inv)
	if err != nil {
		t.Fatal(err)
	}
	p := prices[sku]
	if p.CostPrice != 300 || p.SoldQty != 1 || len(p.Pieces) != 1 || p.Pieces[0].SerialNo != dear {
		t.Fatalf("the return would use %+v", p)
	}
	// RTO reads the order, not the invoice.
	rto, err := resolveSalesOrderLinePrices(m.tenant, o.id)
	if err != nil {
		t.Fatal(err)
	}
	if rto[sku].CostPrice != 300 {
		t.Errorf("RTO cost %v, want 300", rto[sku].CostPrice)
	}
	ret := m.custReturn(inv, sku, "Sellable", ReturnItemInput{SerialNo: dear})
	if a := m.gl(ret, "5100"); a != -30000 {
		t.Errorf("COGS reversal %d, want -30000", a)
	}
	if s := m.pieceData("SerialNumber", "serial_no", dear)["status"]; s != SerialInStock {
		t.Errorf("piece %v, want In Stock", s)
	}
	m.wantAvg(sku, 20000, "both back")
	m.wantStockEqualsGL(sku, append(docs, o.task, ret)...)
}

// A credit note naming the piece finds it on the order's dispatch.
func TestStage1481CreditNoteFindsADispatchedPiece(t *testing.T) {
	m := newMC1481(t)
	sku, cheap, _, docs := m.serialStock()
	o := m.releasedOrder(sku, 1)
	if _, _, err := ScanPickItem(m.tenant, o.task, cheap); err != nil {
		t.Fatalf("pick: %v", err)
	}
	if err := TransitionTaskStatus(m.tenant, o.task, "Dispatched"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	inv, note := m.id("SI"), m.id("CN")
	m.doc(inv, "SalesInvoice", "Paid", map[string]interface{}{"code": inv, "sales_order_id": o.id})
	m.docs = append(m.docs, note)
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	data := map[string]interface{}{"serial_no": cheap, "sales_invoice_id": inv, "return_location": m.loc}
	if err := creditNotePieceBackTx(tx, m.tenant, m.schema, note, "system", data); err != nil {
		t.Fatalf("credit note: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if a := m.gl(note, "5100"); a != -10000 {
		t.Errorf("COGS reversal %d, want -10000", a)
	}
	m.wantStockEqualsGL(sku, append(docs, o.task, note)...)
}

// --- Bundles and handled till lines ------------------------------------------

func (m *mc1481) saleCostsOf(cart string) []saleCostLine {
	m.t.Helper()
	var raw string
	if err := db.DB.QueryRow(`SELECT data FROM `+m.schema+`.documents WHERE id = $1`, cart).Scan(&raw); err != nil {
		m.t.Fatal(err)
	}
	return readSaleCosts(raw)
}

// parts, and the record says so. A return brings the parts back at those figures
// (148.1d), so stock value stays equal to the GL.
// back onto a code that holds no cost would put the GL ahead of the stock value.
func TestStage1481BundleKeepsWhatTheSaleBooked(t *testing.T) {
	m := newMC1481(t)
	a, b := m.item(nil), m.item(nil)
	kit := m.item(map[string]interface{}{"item_type": "Virtual Kit"})
	bundle := m.id("PB")
	comps, _ := json.Marshal([]map[string]interface{}{{"sku": a, "quantity": 2}, {"sku": b, "quantity": 1}})
	m.doc(bundle, "ProductBundle", "Active", map[string]interface{}{"bundle_sku": kit, "name": "Gift set", "fulfillment_mode": "Virtual", "pricing_mode": "Fixed Price", "fixed_price": 1000.0, "components": string(comps)})
	g1, _ := m.receive(a, 4, 100)
	g2, _ := m.receive(b, 4, 50)
	cart, _ := m.mustSell(kit, 1, nil)
	if got := m.gl(cart, "5100"); got != 25000 {
		t.Fatalf("COGS %d, want 25000", got)
	}
	costs := m.saleCostsOf(cart)
	if len(costs) != 1 || costs[0].SKU != kit || costs[0].Qty != 1 || costs[0].ValuePaise != 25000 || len(costs[0].Parts) != 2 {
		t.Fatalf("sale_costs %+v", costs)
	}
	if p := costs[0].Parts[0]; p.SKU != a || p.Qty != 2 || p.ValuePaise != 20000 {
		t.Errorf("first part %+v", p)
	}
	skus := []string{a, b, kit}
	m.wantValueEqualsGL(skus, g1, g2, cart)
	ret := m.custReturn(cart, kit, "Sellable", ReturnItemInput{})
	if got, stock := m.gl(ret, "5100"), m.gl(ret, "1200"); got != -25000 || stock != 25000 {
		t.Errorf("return posted 5100 %d, 1200 %d: want the parts back at 250 (148.1d)", got, stock)
	}
	m.wantValueEqualsGL(skus, g1, g2, cart, ret)
}

// A component sold on the same bill as its kit keeps its own figure: the kit's
// parts are not on that line's record.
func TestStage1481BundlePartStaysApartFromAComponentOnTheBill(t *testing.T) {
	m := newMC1481(t)
	a := m.item(nil)
	kit := m.item(map[string]interface{}{"item_type": "Virtual Kit"})
	comps, _ := json.Marshal([]map[string]interface{}{{"sku": a, "quantity": 2}})
	m.doc(m.id("PB"), "ProductBundle", "Active", map[string]interface{}{"bundle_sku": kit, "name": "Pair", "fulfillment_mode": "Virtual", "pricing_mode": "Fixed Price", "fixed_price": 1000.0, "components": string(comps)})
	g1, _ := m.receive(a, 2, 100)
	g2, _ := m.receive(a, 2, 300)
	cart := m.id("CART")
	m.doc(cart, "POSCart", "Processing", map[string]interface{}{"location": m.loc, "payment_mode": "Cash", "items": []map[string]interface{}{
		{"sku": kit, "qty": 1, "sale_price": 1000.0}, {"sku": a, "qty": 1, "sale_price": 500.0},
	}})
	if _, _, err := FinalizePOSCheckout(m.tenant, cart, ""); err != nil {
		t.Fatalf("sell: %v", err)
	}
	costs := m.saleCostsOf(cart)
	if len(costs) != 2 {
		t.Fatalf("sale_costs %+v", costs)
	}
	ret := m.custReturn(cart, a, "Sellable", ReturnItemInput{})
	if got := m.gl(ret, "5100"); got != -20000 {
		t.Errorf("COGS reversal %d, want the one unit's -20000", got)
	}
	m.wantValueEqualsGL([]string{a, kit}, g1, g2, cart, ret)
}

// A non-valuated line booked nothing, so a return takes back nothing: it reversed
// the standard cost before.
func TestStage1481NonValuatedLineComesBackAtNothing(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(map[string]interface{}{"valuation": ValuationNonValuated, "standard_cost": 400.0})
	real := m.item(nil)
	g, _ := m.receive(real, 1, 100)
	if _, err := PostInventoryLedger(m.tenant, m.loc, []interface{}{map[string]interface{}{"sku": sku, "qty": 5.0}}, false); err != nil {
		t.Fatal(err)
	}
	cart, _ := m.mustSell(sku, 1, nil)
	if got := m.gl(cart, "5100"); got != 0 {
		t.Fatalf("COGS %d, want 0", got)
	}
	costs := m.saleCostsOf(cart)
	if len(costs) != 1 || costs[0].ValuePaise != 0 || costs[0].Source != costSourceNone {
		t.Fatalf("sale_costs %+v", costs)
	}
	ret := m.custReturn(cart, sku, "Sellable", ReturnItemInput{})
	if got := m.gl(ret, "5100"); got != 0 {
		t.Errorf("COGS reversal %d, want 0", got)
	}
	if got := m.gl(ret, "1200"); got != 0 {
		t.Errorf("1200 %d, want 0", got)
	}
	m.wantStockEqualsGL(real, g)
}

// A service sells no stock and is on the record at nothing.
func TestStage1481ServiceLineIsOnTheRecord(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(map[string]interface{}{"item_type": "Service"})
	cart, _ := m.mustSell(sku, 1, nil)
	costs := m.saleCostsOf(cart)
	if len(costs) != 1 || costs[0].SKU != sku || costs[0].ValuePaise != 0 {
		t.Fatalf("sale_costs %+v", costs)
	}
}

// A gift booked to its expense account keeps the sale-day cost on the record, so
// its return no longer follows today's rate.
func TestStage1481GiftKeepsItsSaleDayCost(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(map[string]interface{}{"sellability": SellPromoOnly})
	g1, _ := m.receive(sku, 2, 100)
	cart, _ := m.mustSell(sku, 1, nil)
	costs := m.saleCostsOf(cart)
	if len(costs) != 1 || costs[0].ValuePaise != 10000 {
		t.Fatalf("sale_costs %+v", costs)
	}
	m.receive(sku, 1, 400) // today's rate moves: Rs 250
	prices, err := resolveOriginalSaleLinePrices(m.tenant, cart)
	if err != nil {
		t.Fatal(err)
	}
	if prices[sku].CostPrice != 100 {
		t.Errorf("return cost %v, want 100", prices[sku].CostPrice)
	}
	_ = g1
}
