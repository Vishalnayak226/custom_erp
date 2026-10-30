package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 142.3: Sales from one Excel file - the live template reads back
// empty, rows and their problems read as typed, and against a real database
// an upload makes its customer and one posted Sales Invoice per Invoice Ref
// with each line's discount, and the same file uploaded again makes nothing
// twice.

var bsTestSalesHead = []string{"Invoice Ref *", "Customer *", "Store *", "Invoice Date *", "Barcode *", "Qty *", "Rate (₹, incl. GST)", "Discount"}
var bsTestCustomerHead = []string{"Customer Name *", "Phone", "GSTIN", "State", "City"}

func bsRunFile(t *testing.T, c *swCaller, file []byte, apply bool) *bsResult {
	t.Helper()
	customers, lines, err := bsRead(file, engines.ClientDateOrder(c.tenantID))
	if err != nil {
		t.Fatal(err)
	}
	run, err := newBSRun(c, apply)
	if err != nil {
		t.Fatal(err)
	}
	res := run.process(customers, lines)
	for _, cu := range res.Customers {
		if cu.Action == "error" || cu.Result == "failed" {
			t.Logf("customer row %d %s: %s", cu.Row, cu.Key, cu.Message)
		}
	}
	for _, inv := range res.Invoices {
		if inv.Action == "error" || inv.Result == "failed" {
			t.Logf("invoice %s: %s", inv.Ref, inv.Message)
			for _, l := range inv.Lines {
				if l.Message != "" {
					t.Logf("  row %d %s: %s", l.Row, l.Barcode, l.Message)
				}
			}
		}
	}
	return res
}

func TestBulkSalesTemplateReadsEmpty(t *testing.T) {
	c := swTestCaller(t)
	run, err := newSWRun(c, false, false)
	if err != nil {
		t.Fatal(err)
	}
	file, err := bsTemplate(run)
	if err != nil {
		t.Fatal(err)
	}
	customers, lines, err := bsRead(file, engines.DateOrderDMY)
	if err != nil {
		t.Fatal(err)
	}
	if len(customers) != 0 || len(lines) != 0 {
		t.Fatalf("the blank template should read no rows, got %d customers and %d sales rows", len(customers), len(lines))
	}
}

func TestBulkSalesReadsRowsAsTyped(t *testing.T) {
	file := swBook(t, "", map[string][][]string{
		"Customers": {bsTestCustomerHead, {"Sharma Traders", "9876543210", "29ABCDE1234F1Z5", "Karnataka", ""}},
		"Sales": {bsTestSalesHead,
			{"B-1", "Sharma Traders", "HO", "20-10-2026", "890123", "2", "1250", "10%"},
			{"B-1", "Sharma Traders", "HO", "20-10-2026", "890124", "1", "", "12.50"},
			{"B-2", "Anita", "HO", "not a date", "890125", "1.5", "0", "abc"},
			{"", "", "", "", "", "", "", ""},
		},
	}, "Customers", "Sales")
	customers, lines, err := bsRead(file, engines.DateOrderDMY)
	if err != nil {
		t.Fatal(err)
	}
	if len(customers) != 1 || customers[0].GSTIN != "29ABCDE1234F1Z5" {
		t.Fatalf("customers: %+v", customers)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 sales rows (the blank one skipped), got %d", len(lines))
	}
	if l := lines[0]; l.err != "" || l.Qty != 2 || l.Rate != 1250 || l.DiscPct != 10 || l.Date != "2026-10-20" {
		t.Fatalf("row 1 read wrong: %+v", l)
	}
	if l := lines[1]; l.err != "" || l.Rate != 0 || l.DiscEach != 12.5 {
		t.Fatalf("row 2 read wrong: %+v", l)
	}
	bad := lines[2].err
	for _, want := range []string{"not a date", "Qty must be a whole number", "Rate must be an amount above 0", "Discount must be an amount per piece"} {
		if !strings.Contains(bad, want) {
			t.Errorf("row 3 should say %q, said %q", want, bad)
		}
	}
	if _, _, err := bsRead(swBook(t, "", map[string][][]string{"Other": {{"A"}, {"1"}}}, "Other"), engines.DateOrderDMY); err == nil || !strings.Contains(err.Error(), "no Sales sheet") {
		t.Fatalf("a file without a Sales sheet should be refused, got %v", err)
	}
}

func TestBulkSalesMakesInvoicesAndNothingTwice(t *testing.T) {
	c := swTestCaller(t)
	probe, err := newSWRun(c, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if id, _, err := probe.resolveLink("Location", "HO"); err != nil || id == "" {
		t.Skip("the test database has no store HO")
	}
	sfx := fmt.Sprint(time.Now().UnixNano() % 1e9)
	customer, ref := "BS Customer "+sfx, "BS-"+sfx
	sku1, bar1 := "BSITEM1-"+sfx, "BSB"+sfx+"1"
	sku2, bar2 := "BSITEM2-"+sfx, "BSB"+sfx+"2"
	schema, _ := db.GetTenantSchema("default")
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM %[1]s.documents WHERE doctype = 'SalesInvoice' AND data->>'upload_ref' = '` + ref + `'`,
			`DELETE FROM %[1]s.documents WHERE doctype = 'Customer' AND data->>'name' = '` + customer + `'`,
			`DELETE FROM %[1]s.documents WHERE doctype = 'Item' AND data->>'code' IN ('` + sku1 + `', '` + sku2 + `')`,
			`DELETE FROM %[1]s.inventory_availability WHERE sku IN ('` + sku1 + `', '` + sku2 + `')`,
		} {
			_, _ = db.DB.Exec(fmt.Sprintf(q, schema))
		}
	})
	// Two items with barcodes: the first priced on the sheet, the second by its own sale price.
	for _, it := range []map[string]interface{}{
		{"code": sku1, "name": "BS Item One " + sfx, "barcode": bar1, "hsn_code": "7117", "gst_rate": 3, "sale_price": 1000, "mrp": 1000, "status": "Active"},
		{"code": sku2, "name": "BS Item Two " + sfx, "barcode": bar2, "hsn_code": "7117", "gst_rate": 3, "sale_price": 500, "mrp": 600, "status": "Active"},
	} {
		if err := c.do("/api/v1/doc/{doctype}", "POST", "/api/v1/doc/Item", map[string]string{"doctype": "Item"}, it, handleGenericDoc); err != nil {
			t.Fatalf("item: %v", err)
		}
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, 'HO', 50, 50)
			ON CONFLICT (sku, location_code) DO UPDATE SET on_hand = 50, available = 50`, schema), it["code"]); err != nil {
			t.Fatalf("stock: %v", err)
		}
	}
	file := swBook(t, "", map[string][][]string{
		"Customers": {bsTestCustomerHead, {customer, "98" + sfx[:8], "", "Karnataka", "Bengaluru"}},
		"Sales": {bsTestSalesHead,
			{ref, customer, "HO", "20-10-2026", bar1, "2", "1000", "10%"},
			{ref, customer, "HO", "20-10-2026", bar2, "1", "", "50"},
		},
	}, "Customers", "Sales")

	// A fresh database's HO has no State (49.5.6e): the preview says the
	// invoice is billed within the store's state. Then HO is put in
	// Maharashtra for the inter-state check below, as the purchase upload test does.
	if s, _ := engines.LocationStateCode("default", "HO"); s == "" {
		res := bsRunFile(t, c, file, false)
		if msg := res.Invoices[0].Message; !strings.Contains(msg, "has no State") {
			t.Fatalf("a store with no State should be pointed out in the preview, got %q", msg)
		}
	}
	storeHasState(t, "HO")

	// Preview: one new customer, one new invoice, both lines priced, nothing written.
	res := bsRunFile(t, c, file, false)
	if res.Totals.NewCustomers != 1 || res.Totals.NewInvoices != 1 || res.Totals.Errors != 0 {
		t.Fatalf("preview totals: %+v", res.Totals)
	}
	inv := res.Invoices[0]
	if inv.Action != "new" || len(inv.Lines) != 2 || inv.Qty != 3 {
		t.Fatalf("preview invoice: %+v", inv)
	}
	if l := inv.Lines[0]; l.SKU != sku1 || l.Discount != 200 || l.Amount != 1800 {
		t.Fatalf("line 1 (10%% of 2 x 1000): %+v", l)
	}
	if l := inv.Lines[1]; l.SKU != sku2 || l.Rate != 500 || l.Discount != 50 || l.Amount != 450 {
		t.Fatalf("line 2 (the item's sale price, 50 off): %+v", l)
	}
	if inv.Amount != 2250 || inv.Discount != 250 {
		t.Fatalf("preview amounts: %+v", inv)
	}
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype = 'SalesInvoice' AND data->>'upload_ref' = $1`, schema), ref).Scan(&n)
	if n != 0 {
		t.Fatalf("preview wrote %d invoices", n)
	}

	// Apply: the customer is made, the invoice saved with its discounts and posted.
	res = bsRunFile(t, c, file, true)
	inv = res.Invoices[0]
	if inv.Result != "done" || inv.Status != "Approved" || inv.InvoiceNumber == "" {
		t.Fatalf("apply: %+v", inv)
	}
	if res.Totals.Posted != 1 || res.Totals.Failed != 0 {
		t.Fatalf("apply totals: %+v", res.Totals)
	}
	var raw []byte
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'SalesInvoice' AND id = $1`, schema), inv.InvoiceID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var data map[string]interface{}
	_ = json.Unmarshal(raw, &data)
	if data["customer_reference"] != ref || data["place_of_supply"] != "29" || data["interstate"] != true {
		t.Fatalf("invoice header: ref=%v pos=%v interstate=%v (store HO is in Maharashtra, the customer in Karnataka)", data["customer_reference"], data["place_of_supply"], data["interstate"])
	}
	var items []map[string]interface{}
	_ = json.Unmarshal([]byte(fmt.Sprint(data["items"])), &items)
	if len(items) != 2 || items[0]["discount"] != 200.0 || items[0]["amount"] != 1800.0 || items[1]["discount"] != 50.0 {
		t.Fatalf("invoice lines: %v", items)
	}
	if total, _ := data["total_amount"].(float64); total != 2250 {
		t.Fatalf("total_amount %v, want 2250", data["total_amount"])
	}
	if igst, _ := data["igst"].(float64); igst <= 0 || data["cgst"] != 0.0 {
		t.Fatalf("an inter-state invoice should carry IGST only: cgst=%v igst=%v", data["cgst"], data["igst"])
	}

	// The same file again: the invoice is found, nothing is made twice.
	res = bsRunFile(t, c, file, true)
	inv = res.Invoices[0]
	if inv.Action != "existing" || inv.Result != "done" || inv.InvoiceID == "" {
		t.Fatalf("second upload: %+v", inv)
	}
	if res.Totals.NewInvoices != 0 || res.Totals.NewCustomers != 0 {
		t.Fatalf("second upload totals: %+v", res.Totals)
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype = 'SalesInvoice' AND data->>'upload_ref' = $1`, schema), ref).Scan(&n)
	if n != 1 {
		t.Fatalf("%d invoices for %s after two uploads, want 1", n, ref)
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype = 'Customer' AND data->>'name' = $1 AND deleted_at IS NULL`, schema), customer).Scan(&n)
	if n != 1 {
		t.Fatalf("%d customers called %s, want 1", n, customer)
	}
}

func TestBulkSalesUploadsPassTheEnvelope(t *testing.T) {
	for _, p := range []string{"/api/v1/sales/bulk-upload/preview", "/api/v1/sales/bulk-upload/apply"} {
		if got := envelopePolicyFor(p).maxBody; got < swMaxUpload {
			t.Errorf("%s: envelope allows %d bytes, the handler takes up to %d", p, got, swMaxUpload)
		}
	}
}
