package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

// Stage 75.3.5 Wave A on known data: a throwaway tenant with one two-line
// till bill (an offer, two tenders), a part return, a PO received in part at
// a different invoice rate, a transfer that arrived one short, and stock at a
// known cost - and the figures each report must show, worked out by hand.
func TestCatalogWaveAFigures(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	doc := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		b, _ := json.Marshal(data)
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'admin')", id, doctype, b, status)
	}
	doc("LE1", "LegalEntity", "Active", map[string]interface{}{"code": "LE1", "name": "Wave A Retail Pvt Ltd", "gstin": "29AAACW1234A1Z5", "state": "Karnataka"})
	doc("S1", "Location", "Active", map[string]interface{}{"code": "S1", "name": "Jayanagar", "legal_entity": "LE1", "state": "Karnataka"})
	doc("S2", "Location", "Active", map[string]interface{}{"code": "S2", "name": "Indiranagar", "legal_entity": "LE1", "state": "Karnataka"})
	doc("CAT1", "Category", "Active", map[string]interface{}{"code": "CAT1", "name": "Kurtas"})
	doc("F1", "ProductFamily", "Active", map[string]interface{}{"code": "F1", "name": "Anarkali", "design_number": "D-100"})
	doc("I1", "Item", "Active", map[string]interface{}{"code": "I1", "name": "Anarkali Red M", "mrp": 1050, "gst_rate": 5, "hsn_code": "6109", "family": "F1", "category": "CAT1", "barcode": "8900000000011"})
	doc("I2", "Item", "Active", map[string]interface{}{"code": "I2", "name": "Shirt Blue L", "mrp": 2240, "gst_rate": 12, "hsn_code": "6205"})
	doc("C1", "Customer", "Active", map[string]interface{}{"code": "C1", "name": "Asha Rao", "phone": "9876543210", "gstin": "29BBBCA1234B1Z5"})
	doc("V1", "Vendor", "Active", map[string]interface{}{"code": "V1", "name": "Silver Works", "gstin": "29CCCCV1234C1Z5"})

	// The bill: I1 x2 at 1,000 (MRP 1,050) with a 100 offer, I2 x1 at 2,240.
	doc("B1", "POSCart", "Paid", map[string]interface{}{"location": "S1", "bill_number": "S1/2627/0001", "customer_id": "C1", "payment_mode": "Cash",
		"amount_paid": 4140,
		"items": []map[string]interface{}{
			{"sku": "I1", "qty": 2, "sale_price": 1000, "reference_price": 1050, "offer_share": 100, "net_total": 1900},
			{"sku": "I2", "qty": 1, "sale_price": 2240, "reference_price": 2240, "offer_share": 0, "net_total": 2240}},
		"gst_breakdown": map[string]interface{}{"taxable_amount": 3809.52, "cgst": 165.24, "sgst": 165.24, "igst": 0, "total_tax": 330.48, "total_amount": 4140},
		"allocation": []map[string]interface{}{
			{"sku": "I1", "qty": 2, "net_paise": 190000, "tenders": map[string]interface{}{"Cash": 190000}},
			{"sku": "I2", "qty": 1, "net_paise": 224000, "tenders": map[string]interface{}{"Card": 224000}}}})
	doc("R1", "ReturnRequest", "Approved", map[string]interface{}{"code": "SR/S1/0001", "return_location": "S1", "original_order_id": "B1",
		"items": []map[string]interface{}{{"sku": "I1", "qty": 1, "original_unit_price": 1000, "net_paise": 95000, "disposition": "Restock"}}})
	doc("P1", "PurchaseOrder", "Approved", map[string]interface{}{"po_number": "PO/S1/0001", "vendor_id": "V1", "location": "S1", "gst_mode": "Exclusive",
		"items":         `[{"sku":"I1","qty":10,"rate":500}]`,
		"gst_breakdown": map[string]interface{}{"taxable_amount": 5000, "total_tax": 250, "total_amount": 5250}, "grand_total": 5250})
	doc("G1", "GRN", "Approved", map[string]interface{}{"code": "GRN/S1/0001", "po_id": "P1", "vendor": "V1", "location": "S1",
		"received_items":    `[{"sku":"I1","po_id":"P1","ordered_qty":10,"qty":6,"accepted_qty":5,"rejected_qty":1,"rejection_reason":"Stone missing","po_rate":500,"invoice_rate":510,"gst_rate":5}]`,
		"vendor_invoice_no": "INV-9", "invoice_value": 3060, "invoice_gst": 153, "invoice_total": 3213})
	doc("T1", "TransferOrder", "Received", map[string]interface{}{"transfer_number": "TO/S1/0001", "from_warehouse": "S1", "to_warehouse": "S2",
		"items": `[{"sku":"I1","qty":4}]`, "dispatched_items": `[{"sku":"I1","qty":4}]`,
		"receive_variance": `[{"sku":"I1","dispatched_qty":4,"received_qty":3,"shortfall":1}]`,
		"transfer_events":  `[{"no":1,"kind":"Receipt","at":"2026-10-17T10:00:00Z","by":"u2","lines":[{"sku":"I1","received":3}]}]`})
	exec("INSERT INTO " + schema + ".inventory_availability (sku, location_code, on_hand, available) VALUES ('I1', 'S1', 7, 7)")
	exec("INSERT INTO " + schema + ".item_cost (item_code, cumulative_qty_received, cumulative_value_received_paise, avg_unit_cost_paise) VALUES ('I1', 10, 500000, 50000)")

	run := func(id string, p map[string]string) []map[string]interface{} {
		t.Helper()
		def, ok := reportRegistry[id]
		if !ok {
			t.Fatalf("%s is not wired", id)
		}
		rows, err := def.Run(tenantID, p)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return rows
	}
	near := func(what string, got interface{}, want float64) {
		t.Helper()
		if math.Abs(catNum(got)-want) > 0.011 {
			t.Errorf("%s = %v, want %.2f", what, got, want)
		}
	}
	is := func(what string, got interface{}, want string) {
		t.Helper()
		if catStr(got) != want {
			t.Errorf("%s = %q, want %q", what, catStr(got), want)
		}
	}
	none := map[string]string{}

	// Sales Register: one bill; Gross at MRP (2 x 1,050 + 2,240), Discount the rest.
	reg := run("cat-sales-register", none)
	if len(reg) != 1 {
		t.Fatalf("sales register: %d rows, want 1", len(reg))
	}
	r := reg[0]
	is("legal entity", r["legal_entity"], "Wave A Retail Pvt Ltd")
	is("store", r["store_name"], "Jayanagar")
	is("bill no", r["sales_invoice_no"], "S1/2627/0001")
	is("customer", r["customer_name"], "Asha Rao")
	is("customer GSTIN", r["customer_gstin"], "29BBBCA1234B1Z5")
	near("gross", r["gross_amount"], 4340)
	near("discount", r["discount_amount"], 200)
	near("taxable", r["taxable_amount"], 3809.52)
	near("GST", r["gst_amount"], 330.48)
	near("net", r["bill_net_amount"], 4140)
	is("tenders", r["tender_type"], "Card + Cash")
	if got := run("cat-sales-register", map[string]string{"location": "S2"}); len(got) != 0 {
		t.Errorf("store S2 should have no bills, got %d", len(got))
	}

	// Itemwise: I1's line split back to taxable value at 5%.
	items := run("cat-sales-itemwise-detail", none)
	var l1 map[string]interface{}
	for _, row := range items {
		if row["sku"] == "I1" {
			l1 = row
		}
	}
	if l1 == nil {
		t.Fatalf("itemwise: no I1 line in %v", items)
	}
	is("design no", l1["design_no"], "D-100")
	is("category", l1["category_name"], "Kurtas")
	near("I1 gross value", l1["gross_value"], 2000)      // 2 x 1,050 / 1.05
	near("I1 item discount", l1["item_discount"], 95.24) // 2 x 50 / 1.05
	near("I1 bill discount", l1["bill_discount"], 95.24) // 100 / 1.05
	near("I1 taxable", l1["item_taxable_amount"], 1809.52)
	near("I1 GST", l1["gst_amount"], 90.48)
	near("I1 net", l1["item_net_amount"], 1900)

	// Tenders: Cash 1,900 and Card 2,240.
	tenders := map[string]float64{}
	for _, row := range run("cat-sales-tender-detail", none) {
		tenders[catStr(row["tender_type"])] += catNum(row["tender_amount"])
	}
	near("cash", tenders["Cash"], 1900)
	near("card", tenders["Card"], 2240)

	// Return and net sales: 3 sold, 1 back worth 950 paid.
	ret := run("cat-sales-return-register", none)
	if len(ret) != 1 {
		t.Fatalf("returns: %d rows", len(ret))
	}
	is("original bill", ret[0]["original_bill_no"], "S1/2627/0001")
	near("return qty", ret[0]["return_qty"], 1)
	near("return net", ret[0]["net_amount"], 950)
	near("return taxable", ret[0]["taxable_amount"], 904.76)
	net := run("cat-net-sales-summary", none)
	if len(net) != 1 {
		t.Fatalf("net sales: %d rows, want one day-store-channel row: %v", len(net), net)
	}
	near("sold qty", net[0]["sold_qty"], 3)
	near("return qty", net[0]["return_qty"], 1)
	near("net qty", net[0]["net_sales_qty"], 2)
	near("net sales", net[0]["net_sales_amount"], 4140-950)
	near("bill count", net[0]["bill_count"], 1)

	// GSTR-1 HSN: 6109 at 5% and 6205 at 12%, CGST = SGST within the state.
	hsn := map[string]map[string]interface{}{}
	for _, row := range run("cat-gstr-1-hsn-summary", none) {
		hsn[catStr(row["hsn_code"])] = row
	}
	near("6109 taxable", hsn["6109"]["taxable_amount"], 1809.52)
	near("6109 CGST", hsn["6109"]["cgst_amount"], 45.24)
	near("6205 SGST", hsn["6205"]["sgst_amount"], 120)
	is("6109 GSTIN", hsn["6109"]["gstin"], "29AAACW1234A1Z5")

	// PO: 10 ordered, 5 accepted, 5 pending; the GRN billed 10 over the PO rate on 6.
	po := run("cat-purchase-order-itemwise-detail", none)
	if len(po) != 1 {
		t.Fatalf("PO lines: %d", len(po))
	}
	near("PO qty", po[0]["po_qty"], 10)
	near("received", po[0]["received_qty"], 5)
	near("pending", po[0]["pending_qty"], 5)
	near("PO basic", po[0]["po_basic_value"], 5000)
	near("PO GST", po[0]["po_gst_amount"], 250)
	if pg := run("cat-po-vs-grn-status", none); len(pg) != 1 || catStr(pg[0]["grn_no"]) != "GRN/S1/0001" || catStr(pg[0]["grn_date"]) == "" {
		t.Errorf("PO vs GRN: %v", pg)
	}
	if len(run("cat-pending-open-po-report", none)) != 1 {
		t.Error("the PO line is still open")
	}
	vari := run("cat-grn-po-invoice-variance", none)
	if len(vari) != 1 {
		t.Fatalf("GRN variance: %d rows", len(vari))
	}
	near("rate variance", vari[0]["rate_variance"], 10)
	near("amount variance", vari[0]["amount_variance"], 60)
	is("PO no", vari[0]["po_no"], "PO/S1/0001")
	qc := run("cat-grn-qc-rejection-exception", none)
	if len(qc) != 1 || catStr(qc[0]["reason"]) != "Stone missing" {
		t.Errorf("QC exception: %v", qc)
	}
	pr := run("cat-purchase-register", none)
	near("purchase CGST", pr[0]["cgst_amount"], 76.5)
	near("purchase net", pr[0]["net_amount"], 3213)

	// Transfer: 4 sent, 3 arrived - one short, still in transit.
	short := run("cat-transfer-shortage-excess-exception", none)
	if len(short) != 1 {
		t.Fatalf("transfer shortage: %d rows", len(short))
	}
	near("difference", short[0]["difference_qty"], -1)
	is("reason", short[0]["reason"], "shortfall 1")
	tin := run("cat-transfer-in-register", none)
	if len(tin) != 1 {
		t.Fatalf("transfer in: %d rows", len(tin))
	}
	is("transfer in no", tin[0]["transfer_in_no"], "TO/S1/0001/R1")
	near("received", tin[0]["qty"], 3)
	is("to store", tin[0]["to_store"], "Indiranagar")
	transit := run("cat-transfer-in-transit-aging", none)
	if len(transit) != 1 {
		t.Fatalf("in transit: %d rows", len(transit))
	}
	near("in-transit qty", transit[0]["in_transit_qty"], 1)

	// Stock: 7 on hand at 500 cost, MRP 1,050 (5% GST).
	val := run("cat-stock-valuation-summary", none)
	if len(val) != 1 {
		t.Fatalf("valuation: %d rows", len(val))
	}
	near("closing qty", val[0]["closing_stock_qty"], 7)
	near("at cost", val[0]["stock_value_at_purchase_price"], 3500)
	near("at MRP", val[0]["stock_value_at_mrp"], 7350)
	near("excl tax", val[0]["stock_value_excl_tax"], 7000)
	totals, err := reportControlTotals["cat-stock-valuation-summary"](tenantID, none, val)
	if err != nil || totals.DetailVsSummary != 0 {
		t.Errorf("stock valuation detail should equal its summary: %v %+v", err, totals)
	}
	sales, err := reportControlTotals["cat-sales-register"](tenantID, none, reg)
	if err != nil || sales.DetailVsSummary != 0 {
		t.Errorf("sales register detail should equal its summary: %v %+v", err, sales)
	}
	_ = fmt.Sprint
}
