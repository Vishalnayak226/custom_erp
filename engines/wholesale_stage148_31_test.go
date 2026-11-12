package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"testing"
)

// Off means off: with the module off, no screen's meta carries a wholesale field.
func TestStage14831FieldsFollowTheSwitch(t *testing.T) {
	f := newWSFix(t, false)
	want := map[string]string{"Customer": "bill_to_customer", "SalesOrderLine": "instalments", "SalesOrder": "delivery_date", "Item": "dropship", "PurchaseOrder": "dropship_order_id"}
	for doctype, field := range want {
		fields, err := GetDocTypeMeta(f.tenantID, doctype)
		if err != nil {
			t.Fatal(err)
		}
		if wsHasField(fields, field) {
			t.Errorf("%s.%s is in the meta with wholesale off", doctype, field)
		}
	}
	f.module(true)
	for doctype, field := range want {
		fields, _ := GetDocTypeMeta(f.tenantID, doctype)
		if !wsHasField(fields, field) {
			t.Errorf("%s.%s is missing with wholesale on", doctype, field)
		}
	}
	f.module(false)
	if fields, _ := GetDocTypeMeta(f.tenantID, "Customer"); wsHasField(fields, "summary_invoice") {
		t.Error("switching the module off did not hide the fields again")
	}
}

// The bill-to customer receives the invoice (and so the balance and the credit check).
func TestStage14831BillToReceivesTheInvoice(t *testing.T) {
	f := newWSFix(t, true)
	f.customer("WS-PAY", nil)
	f.customer("WS-SHIP", map[string]interface{}{"bill_to_customer": "WS-PAY"})
	so := f.order("WS-SHIP", "Karnataka", "PO-1", "Shipped", wsLine{sku: "WS-A", qty: 2, price: 118})
	inv, err := CreateSalesInvoiceFromOrder(f.tenantID, so, "system")
	if err != nil || inv == "" {
		t.Fatalf("invoice: %q %v", inv, err)
	}
	d := f.doc("SalesInvoice", inv)
	if strField(d, "customer_id") != "WS-PAY" || strField(d, "ship_to_customer_id") != "WS-SHIP" {
		t.Fatalf("customer_id=%q ship_to_customer_id=%q, want WS-PAY / WS-SHIP", strField(d, "customer_id"), strField(d, "ship_to_customer_id"))
	}
}

func wsSummaryCustomers(f *wsFix, payExtra map[string]interface{}) {
	extra := map[string]interface{}{"summary_invoice": "Yes"}
	for k, v := range payExtra {
		extra[k] = v
	}
	f.customer("WS-PAY", extra)
	f.customer("WS-S1", map[string]interface{}{"bill_to_customer": "WS-PAY"})
	f.customer("WS-S2", map[string]interface{}{"bill_to_customer": "WS-PAY"})
}

// A summary payer's shipped orders are not invoiced one by one; the preview
// shows one group with its why, and creating it makes one Draft invoice that
// posts on the normal path: receivable on the payer, revenue, GST output, no stock.
func TestStage14831OneInvoicePerPeriodPostsOnTheNormalPath(t *testing.T) {
	f := newWSFix(t, true)
	wsSummaryCustomers(f, nil)
	o1 := f.order("WS-S1", "Karnataka", "PO-1", "Shipped", wsLine{sku: "WS-A", qty: 2, price: 118})
	o2 := f.order("WS-S2", "Karnataka", "PO-2", "Shipped", wsLine{sku: "WS-B", qty: 1, price: 118})
	if id, err := CreateSalesInvoiceFromOrder(f.tenantID, o1, "system"); err != nil || id != "" {
		t.Fatalf("a summary payer's order was invoiced on its own: %q %v", id, err)
	}
	p, err := PreviewSummaryInvoices(f.tenantID, "", "", "")
	if err != nil || len(p.Groups) != 1 {
		t.Fatalf("preview: %+v %v", p, err)
	}
	g := p.Groups[0]
	if g.Blocked || len(g.Orders) != 2 || g.Total != 354 || len(g.Why) == 0 || g.GSTBasis == "" {
		t.Fatalf("group: %+v", g)
	}
	ids, err := CreateSummaryInvoices(f.tenantID, "system", "", "", "", []string{g.Key})
	if err != nil || len(ids) != 1 {
		t.Fatalf("create: %v %v", ids, err)
	}
	d := f.doc("SalesInvoice", ids[0])
	if strField(d, "customer_id") != "WS-PAY" || strField(d, "summary_order_ids") != o1+","+o2 {
		t.Fatalf("invoice: %+v", d)
	}
	if _, err := PostSalesInvoice(f.tenantID, ids[0], "system"); err != nil {
		t.Fatal(err)
	}
	for acct, want := range map[string][2]int64{"1300": {35400, 0}, "4100": {0, 30000}, "2200": {0, 2700}, "2201": {0, 2700}, "2202": {0, 0}} {
		if dr, cr := f.gl("SalesInvoice", ids[0], acct); dr != want[0] || cr != want[1] {
			t.Errorf("account %s: Dr %d Cr %d, want %v", acct, dr, cr, want)
		}
	}
	var stock int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.gl_postings WHERE document_id = $1 AND account_code NOT IN ('1300','4100','2200','2201','2202')`, f.schema), ids[0]).Scan(&stock)
	if stock != 0 {
		t.Errorf("%d extra journal lines (stock or COGS) on a money-only invoice", stock)
	}
	if again, _ := PreviewSummaryInvoices(f.tenantID, "", "", ""); len(again.Groups) != 0 {
		t.Errorf("billed orders still listed: %+v", again.Groups)
	}
}

// Ship-to states differ and the payer does not split by place of supply: the
// group is shown blocked with its reason and nothing is made. With the split on,
// it is two groups, and the inter-state one posts IGST.
func TestStage14831PlaceOfSupplyFollowsTheShipTo(t *testing.T) {
	f := newWSFix(t, true)
	wsSummaryCustomers(f, nil)
	f.order("WS-S1", "Karnataka", "PO-1", "Shipped", wsLine{sku: "WS-A", qty: 1, price: 118})
	f.order("WS-S2", "Maharashtra", "PO-1", "Shipped", wsLine{sku: "WS-A", qty: 1, price: 118})
	p, _ := PreviewSummaryInvoices(f.tenantID, "", "", "")
	if len(p.Groups) != 1 || !p.Groups[0].Blocked || p.Groups[0].Reason == "" {
		t.Fatalf("want one blocked group: %+v", p.Groups)
	}
	if _, err := CreateSummaryInvoices(f.tenantID, "system", "", "", "", []string{p.Groups[0].Key}); err == nil {
		t.Fatal("a blocked group was invoiced")
	}
	f.customer("WS-PAY", map[string]interface{}{"summary_invoice": "Yes", "group_by_place_of_supply": "Yes"})
	p, _ = PreviewSummaryInvoices(f.tenantID, "", "", "")
	if len(p.Groups) != 2 {
		t.Fatalf("want two groups: %+v", p.Groups)
	}
	var keys []string
	for _, g := range p.Groups {
		if g.Blocked {
			t.Fatalf("blocked: %+v", g)
		}
		keys = append(keys, g.Key)
	}
	ids, err := CreateSummaryInvoices(f.tenantID, "system", "", "", "", keys)
	if err != nil || len(ids) != 2 {
		t.Fatalf("create: %v %v", ids, err)
	}
	igst, cgst := int64(0), int64(0)
	for _, id := range ids {
		if _, err := PostSalesInvoice(f.tenantID, id, "system"); err != nil {
			t.Fatal(err)
		}
		_, c := f.gl("SalesInvoice", id, "2202")
		igst += c
		_, c = f.gl("SalesInvoice", id, "2200")
		cgst += c
	}
	if igst != 1800 || cgst != 900 {
		t.Errorf("IGST %d CGST %d, want 1800 / 900", igst, cgst)
	}
}

// The customer PO splits only when the payer asks; one delivery challan per address.
func TestStage14831CustomerPOAndChallanPerAddress(t *testing.T) {
	f := newWSFix(t, true)
	wsSummaryCustomers(f, map[string]interface{}{"group_by_customer_po": "Yes"})
	f.order("WS-S1", "Karnataka", "PO-1", "Shipped", wsLine{sku: "WS-A", qty: 4, price: 118, extra: map[string]interface{}{"instalments": "2@2026-11-10, 2@2026-11-20"}})
	f.order("WS-S2", "Karnataka", "PO-2", "Shipped", wsLine{sku: "WS-A", qty: 1, price: 118})
	p, _ := PreviewSummaryInvoices(f.tenantID, "", "", "")
	if len(p.Groups) != 2 || p.Groups[0].CustomerPO != "PO-1" {
		t.Fatalf("groups: %+v", p.Groups)
	}
	if len(p.Groups[0].Challans) != 1 || p.Groups[0].Challans[0].Lines[0].Instalment != "2@2026-11-10, 2@2026-11-20" {
		t.Fatalf("challans: %+v", p.Groups[0].Challans)
	}
	// two addresses on one invoice: two challans
	f.customer("WS-PAY", map[string]interface{}{"summary_invoice": "Yes"})
	o3 := f.order("WS-S2", "Karnataka", "PO-3", "Shipped", wsLine{sku: "WS-B", qty: 1, price: 118})
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || '{"shipping_address":"9 Other Rd"}' WHERE id = $1`, f.schema), o3)
	p, _ = PreviewSummaryInvoices(f.tenantID, "", "", "")
	if len(p.Groups) != 1 || len(p.Groups[0].Challans) != 2 {
		t.Fatalf("want one group and a challan per address: %+v", p.Groups)
	}
}

// Credit control runs on the payer: a stopped payer over its limit cannot post.
func TestStage14831PayerCreditStopsThePosting(t *testing.T) {
	f := newWSFix(t, true)
	wsSummaryCustomers(f, map[string]interface{}{"credit_limit": 100.0, "credit_stop": "Yes"})
	f.order("WS-S1", "Karnataka", "PO-1", "Shipped", wsLine{sku: "WS-A", qty: 2, price: 118})
	p, _ := PreviewSummaryInvoices(f.tenantID, "", "", "")
	ids, err := CreateSummaryInvoices(f.tenantID, "system", "", "", "", []string{p.Groups[0].Key})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PostSalesInvoice(f.tenantID, ids[0], "system"); err == nil || !strings.Contains(err.Error(), "over their credit limit") {
		t.Fatalf("want the credit refusal, got %v", err)
	}
}

// Off means off: the engine calls refuse, fields are refused on a save, and an
// order of a would-be summary payer is invoiced as before.
func TestStage14831OffMeansOff(t *testing.T) {
	f := newWSFix(t, true)
	wsSummaryCustomers(f, nil)
	so := f.order("WS-S1", "Karnataka", "PO-1", "Shipped", wsLine{sku: "WS-A", qty: 1, price: 118})
	f.module(false)
	if _, err := PreviewSummaryInvoices(f.tenantID, "", "", ""); err == nil {
		t.Error("preview ran with wholesale off")
	}
	if err := validateWholesaleDocument(f.tenantID, "Customer", map[string]interface{}{"code": "WS-X", "name": "X", "bill_to_customer": "WS-PAY"}); err == nil {
		t.Error("a bill-to was saved with wholesale off")
	}
	inv, err := CreateSalesInvoiceFromOrder(f.tenantID, so, "system")
	if err != nil || inv == "" {
		t.Fatalf("the order was not invoiced as before: %q %v", inv, err)
	}
	if d := f.doc("SalesInvoice", inv); strField(d, "customer_id") != "WS-S1" || strField(d, "ship_to_customer_id") != "" {
		t.Errorf("bill-to applied with wholesale off: %+v", d)
	}
}

// Bill-to rules and instalments are checked on save.
func TestStage14831SaveRules(t *testing.T) {
	f := newWSFix(t, true)
	f.customer("WS-PAY", nil)
	f.customer("WS-S1", map[string]interface{}{"bill_to_customer": "WS-PAY"})
	for name, c := range map[string]map[string]interface{}{
		"self":    {"code": "WS-Y", "name": "Y", "bill_to_customer": "WS-Y"},
		"missing": {"code": "WS-Y", "name": "Y", "bill_to_customer": "WS-NONE"},
		"chained": {"code": "WS-Y", "name": "Y", "bill_to_customer": "WS-S1"},
	} {
		err := validateWholesaleDocument(f.tenantID, "Customer", c)
		if ve, ok := err.(*ValidationError); !ok || ve.Code != wsBillToCode {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, ins := range map[string]string{"badshape": "five", "baddate": "2@soon", "toomany": "9@2026-11-10"} {
		err := validateWholesaleDocument(f.tenantID, "SalesOrderLine", map[string]interface{}{"qty": 4, "instalments": ins})
		if ve, ok := err.(*ValidationError); !ok || ve.Code != wsInstalmentCode {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := validateWholesaleDocument(f.tenantID, "SalesOrderLine", map[string]interface{}{"qty": 4, "instalments": "2@2026-11-10, 2@2026-11-20"}); err != nil {
		t.Errorf("good instalments refused: %v", err)
	}
}
