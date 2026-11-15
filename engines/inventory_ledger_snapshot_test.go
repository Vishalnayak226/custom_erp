package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// ledgerSnapshot reads the Stock Ledger line one voucher wrote for one SKU.
func ledgerSnapshot(t *testing.T, schema, voucherType, voucherID, sku string) map[string]interface{} {
	t.Helper()
	var raw []byte
	if err := db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_type' = $1 AND data->>'voucher_id' = $2 AND data->>'item_id' = $3",
		voucherType, voucherID, sku).Scan(&raw); err != nil {
		t.Fatalf("ledger line %s / %s: %v", voucherID, sku, err)
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// 49.5.5b.1: two lines of one SKU and lot post as one movement - each rate
// weighted by qty, the UOM and tax rate kept only where both agree - and a
// 0% tax rate is still written.
func TestStockLineSnapshotMergeAndShape(t *testing.T) {
	five, twelve := 5.0, 12.0
	a := stockLineSnapshot{UOM: "PCS", ValuationRate: 100, SaleRate: 250, TaxRate: &five}
	b := stockLineSnapshot{UOM: "PCS", ValuationRate: 100, SaleRate: 280, TaxRate: &twelve}
	m := a.merge(-1, b, -2)
	if m.UOM != "PCS" || m.ValuationRate != 100 || m.SaleRate != 270 || m.TaxRate != nil {
		t.Errorf("merge = %+v, want PCS, 100, 270 and no tax rate", m)
	}
	if m := a.merge(1, stockLineSnapshot{UOM: "BOX"}, 1); m.UOM != "" {
		t.Errorf("two UOMs merged into %q, want none", m.UOM)
	}
	zero := 0.0
	doc := map[string]interface{}{}
	stockLineSnapshot{TaxRate: &zero}.writeTo(doc)
	if v, ok := doc["tax_rate"]; !ok || v != 0.0 {
		t.Errorf("a 0%% tax rate must be written, got %v", doc)
	}
	doc = map[string]interface{}{}
	stockLineSnapshot{}.writeTo(doc)
	if len(doc) != 0 {
		t.Errorf("an empty snapshot wrote %v", doc)
	}
}

// 49.5.5b.1 / .2: a till sale's ledger lines keep the item's UOM, the unit cost
// the server valued them at and the unit price charged - and still say so
// after the item master changes.
func TestPOSSaleLedgerLinesKeepRateAndUOM(t *testing.T) {
	m := newMC1481(t)
	a := m.item(map[string]interface{}{"uom": "PCS"})
	b := m.item(nil) // no UOM on the item: none on the line
	m.receiveAt(m.loc, a, 5, 100, nil)
	m.receiveAt(m.loc, b, 5, 40, nil)

	cart := m.id("CART")
	m.doc(cart, "POSCart", "Processing", map[string]interface{}{"location": m.loc, "payment_mode": "Cash",
		"gst_breakdown": map[string]interface{}{"exempt_amount": 610.0}, "items": []map[string]interface{}{
			{"sku": a, "qty": 1, "sale_price": 250.0}, {"sku": a, "qty": 1, "sale_price": 270.0}, {"sku": b, "qty": 1, "sale_price": 90.0}}})
	if _, _, err := FinalizePOSCheckout(m.tenant, cart, ""); err != nil {
		t.Fatalf("sell: %v", err)
	}
	// The item master changes after the sale.
	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET data = data || '{\"uom\":\"BOX\"}' WHERE id = $1", a); err != nil {
		t.Fatal(err)
	}

	la := ledgerSnapshot(t, m.schema, "POSInvoice", cart, a)
	if la["qty"] != -2.0 || la["uom"] != "PCS" || la["valuation_rate"] != 100.0 || la["sale_rate"] != 260.0 {
		t.Errorf("sale line for A = qty %v uom %v valuation %v sale %v, want -2 PCS 100 260", la["qty"], la["uom"], la["valuation_rate"], la["sale_rate"])
	}
	if _, ok := la["tax_rate"]; ok {
		t.Errorf("a till sale's GST is per cart, not per line - no tax_rate expected, got %v", la["tax_rate"])
	}
	lb := ledgerSnapshot(t, m.schema, "POSInvoice", cart, b)
	if _, ok := lb["uom"]; ok || lb["valuation_rate"] != 40.0 || lb["sale_rate"] != 90.0 {
		t.Errorf("sale line for B = uom %v valuation %v sale %v, want no uom, 40, 90", lb["uom"], lb["valuation_rate"], lb["sale_rate"])
	}

	// 49.5.5b.12: the Stock Ledger report shows what the line was posted with.
	rows, err := GetStockLedgerReport(m.tenant, a, m.loc, "POSInvoice", "", "")
	if err != nil || len(rows) != 1 {
		t.Fatalf("report: %v rows, %v", len(rows), err)
	}
	if r := rows[0]; r["uom"] != "PCS" || r["valuation_rate"] != "100" || r["sale_rate"] != "260" || r["tax_rate"] != "" {
		t.Errorf("report row = uom %v cost %v price %v tax %v, want PCS 100 260 and blank", r["uom"], r["valuation_rate"], r["sale_rate"], r["tax_rate"])
	}
}

// 49.5.5b.3: a sales invoice's stock line keeps the unit cost, the price before
// tax and the GST rate it was billed at; its cancellation keeps the price and
// tax the goods came back at.
func TestSalesInvoiceLedgerLinesKeepRateAndTax(t *testing.T) {
	m := newMC1481(t)
	a := m.item(map[string]interface{}{"uom": "PCS"})
	m.receiveAt(m.loc, a, 5, 100, nil)
	inv := m.id("SINV")
	m.doc(inv, "SalesInvoice", "Draft", map[string]interface{}{"invoice_number": inv, "total_amount": 1120.0, "location": m.loc, "items": []map[string]interface{}{
		{"sku": a, "qty": 2, "rate": 500.0, "gst_rate": 12.0, "amount": 1120.0}}})
	if _, err := PostSalesInvoice(m.tenant, inv, "admin"); err != nil {
		t.Fatalf("invoice: %v", err)
	}
	l := ledgerSnapshot(t, m.schema, "SalesInvoice", inv, a)
	if l["qty"] != -2.0 || l["uom"] != "PCS" || l["valuation_rate"] != 100.0 || l["sale_rate"] != 500.0 || l["tax_rate"] != 12.0 {
		t.Errorf("invoice line = qty %v uom %v valuation %v sale %v tax %v, want -2 PCS 100 500 12", l["qty"], l["uom"], l["valuation_rate"], l["sale_rate"], l["tax_rate"])
	}
	if err := CancelSalesInvoice(m.tenant, inv, "admin", "49.5.5b.3 test"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	c := ledgerSnapshot(t, m.schema, "SalesInvoiceCancel", inv, a)
	if c["qty"] != 2.0 || c["uom"] != "PCS" || c["sale_rate"] != 500.0 || c["tax_rate"] != 12.0 {
		t.Errorf("cancel line = qty %v uom %v sale %v tax %v, want 2 PCS 500 12", c["qty"], c["uom"], c["sale_rate"], c["tax_rate"])
	}
}

// 49.5.5b.4: a goods receipt's stock line keeps the unit cost the receipt is
// booked at and the line's GST rate; a line with no GST rate has none.
func TestGRNLedgerLinesKeepCostAndTax(t *testing.T) {
	m := newMC1481(t)
	a := m.item(map[string]interface{}{"uom": "KG"})
	b := m.item(map[string]interface{}{"uom": "PCS"})
	grnA, _ := m.receiveAt(m.loc, a, 3, 80, map[string]interface{}{"gst_rate": 5.0})
	grnB, _ := m.receiveAt(m.loc, b, 2, 45, nil)
	la := ledgerSnapshot(t, m.schema, "GRN", grnA, a)
	if la["qty"] != 3.0 || la["uom"] != "KG" || la["valuation_rate"] != 80.0 || la["tax_rate"] != 5.0 {
		t.Errorf("receipt line A = qty %v uom %v valuation %v tax %v, want 3 KG 80 5", la["qty"], la["uom"], la["valuation_rate"], la["tax_rate"])
	}
	lb := ledgerSnapshot(t, m.schema, "GRN", grnB, b)
	if _, ok := lb["tax_rate"]; ok || lb["uom"] != "PCS" || lb["valuation_rate"] != 45.0 {
		t.Errorf("receipt line B = uom %v valuation %v tax %v, want PCS 45 and no tax rate", lb["uom"], lb["valuation_rate"], lb["tax_rate"])
	}
}

// 49.5.5b.5: a purchase return's accepted line goes back at its unit cost and
// the GST rate the GRN received it at; its set-aside line keeps the UOM and
// the cost, and no tax rate - that stock was never billed.
func TestPurchaseReturnLedgerLinesKeepCostAndTax(t *testing.T) {
	m := newMC1481(t)
	a := m.item(map[string]interface{}{"uom": "PCS"})
	grn, _ := m.receiveAt(m.loc, a, 5, 100, map[string]interface{}{"gst_rate": 12.0, "accepted_qty": 4.0, "rejected_qty": 1.0, "rejection_reason": "scratched"})
	vendor := m.id("VEND")
	m.doc(vendor, "Vendor", "Active", map[string]interface{}{"code": vendor, "name": vendor})
	lines, _ := json.Marshal([]map[string]interface{}{{"sku": a, "stock_bucket": "Accepted", "qty": 2}, {"sku": a, "stock_bucket": "Rejected", "qty": 1}})
	p := map[string]interface{}{"grn_id": grn, "reason": "wrong size", "return_items": string(lines), "vendor_id": vendor, "location": m.loc}
	if err := validatePurchaseReturnRules(m.tenant, "", "", nil, p); err != nil {
		t.Fatalf("validate: %v", err)
	}
	ret := m.id("PR")
	m.doc(ret, "PurchaseReturn", "Draft", p)
	if _, err := PostPurchaseReturn(m.tenant, ret, "system", "HQ"); err != nil {
		t.Fatalf("return: %v", err)
	}
	var raw []byte
	rows, err := db.DB.Query("SELECT data FROM "+m.schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_type' = 'PurchaseReturn' AND data->>'voucher_id' = $1", ret)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	byFrom := map[string]map[string]interface{}{}
	for rows.Next() {
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		l := map[string]interface{}{}
		if err := json.Unmarshal(raw, &l); err != nil {
			t.Fatal(err)
		}
		from, _ := l["from_status"].(string)
		byFrom[from] = l
	}
	if l := byFrom[""]; l == nil || l["qty"] != -2.0 || l["uom"] != "PCS" || l["valuation_rate"] != 100.0 || l["tax_rate"] != 12.0 {
		t.Errorf("accepted line = %v, want -2 PCS at 100 and 12%%", l)
	}
	if l := byFrom["QC-Hold"]; l == nil || l["qty"] != -1.0 || l["uom"] != "PCS" {
		t.Errorf("set-aside line = %v, want -1 PCS", l)
	} else if _, ok := l["tax_rate"]; ok {
		t.Errorf("set-aside line has a tax rate %v; it was never billed", l["tax_rate"])
	}
}

// 49.5.5b.6: a customer return's stock comes back in at the cost the sale
// booked, with the GST rate the original invoice line billed; a till return
// has no tax rate, as the till sale's own line has none.
func TestCustomerReturnLedgerLinesKeepCostAndTax(t *testing.T) {
	m := newMC1481(t)
	a := m.item(map[string]interface{}{"uom": "PCS"})
	m.receiveAt(m.loc, a, 5, 100, nil)

	inv := m.id("SINV")
	m.doc(inv, "SalesInvoice", "Draft", map[string]interface{}{"invoice_number": inv, "total_amount": 1120.0, "location": m.loc, "items": []map[string]interface{}{
		{"sku": a, "qty": 2, "rate": 500.0, "gst_rate": 12.0, "amount": 1120.0}}})
	if _, err := PostSalesInvoice(m.tenant, inv, "admin"); err != nil {
		t.Fatalf("invoice: %v", err)
	}
	ret := m.custReturn(inv, a, "Sellable", ReturnItemInput{Qty: 1})
	if l := ledgerSnapshot(t, m.schema, "SalesReturn", ret, a); l["qty"] != 1.0 || l["uom"] != "PCS" || l["valuation_rate"] != 100.0 || l["tax_rate"] != 12.0 {
		t.Errorf("invoice return line = qty %v uom %v valuation %v tax %v, want 1 PCS 100 12", l["qty"], l["uom"], l["valuation_rate"], l["tax_rate"])
	}

	cart, _, err := m.sell(a, 1, nil)
	if err != nil {
		t.Fatalf("sell: %v", err)
	}
	ret = m.custReturn(cart, a, "Sellable", ReturnItemInput{Qty: 1})
	l := ledgerSnapshot(t, m.schema, "SalesReturn", ret, a)
	if l["qty"] != 1.0 || l["uom"] != "PCS" || l["valuation_rate"] != 100.0 {
		t.Errorf("till return line = qty %v uom %v valuation %v, want 1 PCS 100", l["qty"], l["uom"], l["valuation_rate"])
	}
	if _, ok := l["tax_rate"]; ok {
		t.Errorf("a till return has no per-line tax rate, got %v", l["tax_rate"])
	}
}

// 49.5.5b.7: a transfer's dispatch and receipt lines keep the item's UOM and
// the unit cost the goods travel at - a transfer moves no cost.
func TestTransferLedgerLinesKeepCost(t *testing.T) {
	m := newMC1481(t)
	a := m.item(map[string]interface{}{"uom": "PCS"})
	to := "L5B7-" + m.run
	m.doc(to, "Location", "Active", map[string]interface{}{"code": to, "name": to, "type": "Store", "sellable": "Yes"})
	m.receive(a, 10, 100)
	toID := m.id("TO")
	items, _ := json.Marshal([]map[string]interface{}{{"sku": a, "qty": 4}})
	m.doc(toID, "TransferOrder", "Approved", map[string]interface{}{"id": toID, "transfer_number": toID, "from_warehouse": m.loc, "to_warehouse": to, "status": "Approved", "items": string(items)})
	if err := DispatchTransferOrder(m.tenant, toID, "system"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if err := ReceiveTransferOrder(m.tenant, toID, "system", []interface{}{map[string]interface{}{"sku": a, "qty": 4}}); err != nil {
		t.Fatalf("receive: %v", err)
	}
	rows, err := db.DB.Query("SELECT data FROM "+m.schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_type' = 'TransferOrder' AND data->>'voucher_id' = $1", toID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		l := map[string]interface{}{}
		if err := json.Unmarshal(raw, &l); err != nil {
			t.Fatal(err)
		}
		n++
		if l["uom"] != "PCS" || l["valuation_rate"] != 100.0 {
			t.Errorf("transfer line qty %v at %v = uom %v valuation %v, want PCS 100", l["qty"], l["warehouse_id"], l["uom"], l["valuation_rate"])
		}
	}
	if n != 2 {
		t.Errorf("%d transfer ledger lines, want dispatch + receipt", n)
	}
}

// outLedgerLine reads the one stock-out ledger line of a SKU.
func outLedgerLine(t *testing.T, schema, sku string) map[string]interface{} {
	t.Helper()
	var raw []byte
	if err := db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1 AND (data->>'qty')::numeric < 0", sku).Scan(&raw); err != nil {
		t.Fatalf("stock-out ledger line of %s: %v", sku, err)
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// 49.5.5b.8: a valuated consumption issue and a stock adjustment write the
// cost their expense or loss was booked at.
func TestConsumptionAndAdjustmentLedgerLinesKeepCost(t *testing.T) {
	m := newMC1481(t)
	tape := m.item(map[string]interface{}{"uom": "PCS", "item_type": "Packaging", "consumption_method": ConsumeManual})
	m.receive(tape, 10, 100)
	if _, err := PostConsumptionEntry(m.tenant, "system", ConsumptionEntryRequest{Location: m.loc, Mode: "Manual issue", Reason: "fit-out",
		Lines: []ConsumptionEntryLine{{SKU: tape, IssuedQty: 3}}}); err != nil {
		t.Fatalf("consumption: %v", err)
	}
	if l := outLedgerLine(t, m.schema, tape); l["uom"] != "PCS" || l["valuation_rate"] != 100.0 {
		t.Errorf("consumption line = uom %v valuation %v, want PCS 100", l["uom"], l["valuation_rate"])
	}

	glass := m.item(map[string]interface{}{"uom": "PCS"})
	m.receive(glass, 10, 80)
	if _, status, err := CreateInventoryAdjustment(m.tenant, InventoryAdjustmentInput{Location: m.loc,
		Lines: []InventoryAdjustmentLine{{SKU: glass, Qty: -2, ReasonCode: "IA-DAMAGE"}}}, "system", RoleStoreManager); err != nil || status != "Posted" {
		t.Fatalf("adjustment: %s %v", status, err)
	}
	if l := outLedgerLine(t, m.schema, glass); l["uom"] != "PCS" || l["valuation_rate"] != 80.0 {
		t.Errorf("adjustment line = uom %v valuation %v, want PCS 80", l["uom"], l["valuation_rate"])
	}
}

// 49.5.5b.9: job work writes the material at the cost it went out at and the
// output at the material it was made from - 2 at 100 make 1, so 200 each.
func TestJobWorkLedgerLinesKeepCost(t *testing.T) {
	m := newMC1481(t)
	m.dropChallans()
	rm := m.item(map[string]interface{}{"uom": "PCS", "valuation": ValuationValuated})
	out := m.item(map[string]interface{}{"uom": "PCS", "valuation": ValuationValuated})
	m.receive(rm, 2, 100)
	jw := m.jobWorkSent(rm, out, 2)
	if _, err := ReceiveJobWork(m.tenant, jw, "system", RoleStoreManager, "K1", []JobWorkReceiveLine{{LineNo: 1, AcceptedQty: 2}}); err != nil { // 2 consumed, 1 made
		t.Fatalf("receive: %v", err)
	}
	if l := ledgerSnapshot(t, m.schema, "SubcontractOrder", jw, out); l["uom"] != "PCS" || l["valuation_rate"] != 200.0 {
		t.Errorf("output line = uom %v valuation %v, want PCS 200", l["uom"], l["valuation_rate"])
	}
	if l := ledgerSnapshot(t, m.schema, "SubcontractOrder", jw, rm); l["uom"] != "PCS" || l["valuation_rate"] != 100.0 {
		t.Errorf("material line = uom %v valuation %v, want PCS 100", l["uom"], l["valuation_rate"])
	}
}

// 49.5.5b.11: a line whose path set no UOM or rate gets its item's UOM and the
// cost it stands at as it is written - moving average, else the standard cost -
// and a rate the path set is kept.
func TestLedgerLineFillsWhatItsPathLeftOut(t *testing.T) {
	m := newMC1481(t)
	avg := m.item(map[string]interface{}{"uom": "KG"})
	m.receive(avg, 4, 50)
	std := m.item(map[string]interface{}{"uom": "BOX", "standard_cost": 30})
	write := func(e StockLedgerEntry) map[string]interface{} {
		t.Helper()
		e.WarehouseID, e.Qty, e.VoucherType, e.VoucherID = m.loc, -1, "LedgerFillTest", m.id("LF")
		tx, err := db.DB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := insertStockLedgerEntry(tx, m.schema, e); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return ledgerSnapshot(t, m.schema, "LedgerFillTest", e.VoucherID, e.ItemID)
	}
	if l := write(StockLedgerEntry{ItemID: avg}); l["uom"] != "KG" || l["valuation_rate"] != 50.0 {
		t.Errorf("moving-average item = uom %v valuation %v, want KG 50", l["uom"], l["valuation_rate"])
	}
	if l := write(StockLedgerEntry{ItemID: std}); l["uom"] != "BOX" || l["valuation_rate"] != 30.0 {
		t.Errorf("standard-cost item = uom %v valuation %v, want BOX 30", l["uom"], l["valuation_rate"])
	}
	if l := write(StockLedgerEntry{ItemID: avg, stockLineSnapshot: stockLineSnapshot{UOM: "G", ValuationRate: 7}}); l["uom"] != "G" || l["valuation_rate"] != 7.0 {
		t.Errorf("what the path set = uom %v valuation %v, want G 7", l["uom"], l["valuation_rate"])
	}
}
