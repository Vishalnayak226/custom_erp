package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// Stage 127.3: a transaction with no format of its own prints through the
// generic format; a Master does not, and a format's own key is unchanged.
func TestStage127_3GenericPrint(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insert("SKU-G", "Item", "Active", map[string]interface{}{"code": "SKU-G", "name": "Generic Shirt", "hsn_code": "6109", "gst_rate": 5.0})

	if PrintTxnDoctype(tenantID, "Item") != "" || PrintTxnDoctype(tenantID, "NoSuchDoctype") != "" {
		t.Errorf("a Master and an unknown doctype do not print generically")
	}
	if PrintTxnDoctype(tenantID, PrintTxnGRNVendorCopy) != "GRN" || PrintTxnDoctype(tenantID, "RFQ") != "RFQ" {
		t.Errorf("a format keeps its doctype; a transaction prints as itself")
	}

	// Item lines: the item's name comes in, qty x rate is the amount. (A vendor
	// quote: the RFQ has its own standard layout since Stage 130.17.)
	insert("RFQ-G1", "VendorQuote", "Draft", map[string]interface{}{"code": "RFQ-G1", "title": "Shirts for summer",
		"lines": `[{"sku":"SKU-G","qty":3,"rate":100.5}]`})
	d, err := BuildPrintDocument(tenantID, "VendorQuote", "RFQ-G1", "", false)
	if err != nil {
		t.Fatalf("vendor quote print: %v", err)
	}
	if d.Title != "Vendor Quote" || d.Number != "RFQ-G1" || len(d.Lines) != 1 || d.Lines[0]["item_name"] != "Generic Shirt" || d.Lines[0]["amount"] != 301.5 {
		t.Errorf("vendor quote print: title %q number %q lines %v", d.Title, d.Number, d.Lines)
	}
	if _, ok := d.Fields["generic_print"]; !ok {
		t.Errorf("the generic layout's rows, columns and totals are sent")
	}
	// Stage 127.5: the same lines load in Print stickers; a Master does not.
	if st, err := ResolveDocumentStickerLines(tenantID, "VendorQuote", "RFQ-G1"); err != nil || len(st) != 1 || st[0].SKU != "SKU-G" || st[0].Qty != 3 {
		t.Errorf("vendor quote sticker lines: %v %+v", err, st)
	}
	if _, err := ResolveDocumentStickerLines(tenantID, "Item", "SKU-G"); !errors.Is(err, ErrUnsupportedStickerSource) {
		t.Errorf("a Master is not a sticker source: %v", err)
	}

	// Value lines: each numeric column is totalled.
	insert("LCV-G1", "LandedCostVoucher", "Draft", map[string]interface{}{"code": "LCV-G1",
		"charge_lines": `[{"charge_type":"Freight","amount":250.5},{"charge_type":"Insurance","amount":49.5}]`})
	d, err = BuildPrintDocument(tenantID, "LandedCostVoucher", "LCV-G1", "", false)
	if err != nil {
		t.Fatalf("LCV print: %v", err)
	}
	if d.Title != "Landed Cost Voucher" || len(d.Lines) != 2 || d.Fields["total__amount"] != 300.0 {
		t.Errorf("LCV print: title %q lines %v total %v", d.Title, d.Lines, d.Fields["total__amount"])
	}
	g, _ := d.Fields["generic_print"].(map[string]interface{})
	cols, _ := g["columns"].([]genericPrintColumn)
	if len(cols) != 3 || cols[1].Key != "amount" || cols[2].Key != "charge_type" {
		t.Errorf("LCV columns: %+v", cols)
	}

	// Stage 127.3.1 shapes. A journal voucher keeps paise; it prints rupees,
	// each account by its name, debit and credit totalled.
	insert("JV-G1", "JournalVoucher", "Draft", map[string]interface{}{"voucher_number": "JV-G1", "narration": "Rent",
		"lines": `[{"account_code":"5400","debit":1250050},{"account_code":"1100","credit":1250050}]`})
	d, err = BuildPrintDocument(tenantID, "JournalVoucher", "JV-G1", "", false)
	if err != nil || d.Title != "Journal Voucher" || len(d.Lines) != 2 || d.Lines[0]["debit"] != 12500.5 || d.Lines[1]["account_name"] == "" ||
		d.Fields["total__debit"] != 12500.5 || d.Fields["total__credit"] != 12500.5 {
		t.Errorf("JV print: %v %q lines %v totals %v/%v", err, d.Title, d.Lines, d.Fields["total__debit"], d.Fields["total__credit"])
	}
	// A payslip: gross as earnings, each deduction; the pay heads are not
	// repeated in the header.
	insert("PS-G1", "Payslip", "Draft", map[string]interface{}{"code": "PS-G1", "gross_pay": 30000.0, "pf_deduction": 1800.0, "pt_deduction": 200.0, "net_pay": 28000.0})
	d, err = BuildPrintDocument(tenantID, "Payslip", "PS-G1", "", false)
	if err != nil || d.Title != "Payslip" || len(d.Lines) != 3 || d.Fields["total__earning"] != 30000.0 || d.Fields["total__deduction"] != 2000.0 {
		t.Errorf("payslip print: %v %q lines %v", err, d.Title, d.Lines)
	}
	for _, h := range d.Fields["generic_print"].(map[string]interface{})["header"].([]genericPrintColumn) {
		if h.Key == "gross_pay" || h.Key == "pf_deduction" {
			t.Errorf("payslip header repeats %s", h.Key)
		}
	}
	// A Bill of Lading: the cartons loaded against the task.
	insert("LT-G1", "LoadingTask", "Departed", map[string]interface{}{"code": "LT-G1", "dock_door": "D1", "trailer_no": "KA01"})
	insert("PKG-G1", "ShippingPackage", "Packed", map[string]interface{}{"loading_task_id": "LT-G1", "order_id": "SO-9", "weight_kg": 4.5})
	d, err = BuildPrintDocument(tenantID, "LoadingTask", "LT-G1", "", false)
	if err != nil || d.Title != "Bill of Lading" || len(d.Lines) != 1 || d.Lines[0]["package_code"] != "PKG-G1" {
		t.Errorf("BOL print: %v %q lines %v", err, d.Title, d.Lines)
	}
}
