package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Stage 73.2.9 - customer reference on B2B sales: the customer's PO number is
// kept on the order (trimmed), can be edited there, is carried onto the
// invoice raised from the order, printed on the invoice (HTML and ESC/POS),
// and found by the Sales by Customer Reference report. Own throwaway tenant.
func TestStage73CustomerReference(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	raw, _ := json.Marshal(map[string]interface{}{"code": "ITEM-CR", "name": "Carton", "sale_price": 500.0, "hsn_code": "4819", "tax_treatment": "Exempt"})
	if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ('ITEM-CR', 'Item', $1, 'Active', 'system')", raw); err != nil {
		t.Fatal(err)
	}
	if _, err := PostInventoryLedger(tenantID, "HO", []interface{}{map[string]interface{}{"sku": "ITEM-CR", "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	stored := func(id, key string) string {
		var v string
		_ = db.DB.QueryRow(`SELECT COALESCE(data->>'`+key+`', '') FROM `+schemaName+`.documents WHERE id = $1`, id).Scan(&v)
		return v
	}

	orderID, err := CreateSalesOrder(tenantID, SalesOrderInput{Channel: "Manual", CustomerName: "Acme Traders", CustomerRef: "  PO-4500123 ",
		ShippingAddress: "12 MG Road, Bengaluru, Karnataka 560001", PaymentStatus: "Confirmed",
		Lines: []SalesOrderLineInput{{SKU: "ITEM-CR", Qty: 2, UnitPrice: 500}}})
	if err != nil {
		t.Fatalf("CreateSalesOrder: %v", err)
	}
	if got := stored(orderID, "customer_reference"); got != "PO-4500123" {
		t.Errorf("the order keeps the reference, trimmed: %q", got)
	}
	plain, err := CreateSalesOrder(tenantID, SalesOrderInput{Channel: "Manual", CustomerName: "Walk-in", ShippingAddress: "1 Park St, Kolkata 700016",
		PaymentStatus: "Confirmed", Lines: []SalesOrderLineInput{{SKU: "ITEM-CR", Qty: 1, UnitPrice: 500}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := stored(plain, "customer_reference"); got != "" {
		t.Errorf("an order without one stores none: %q", got)
	}

	ref := "PO-4500124"
	if err := EditSalesOrder(tenantID, orderID, OrderEdit{CustomerRef: &ref}, "tester"); err != nil {
		t.Fatalf("EditSalesOrder: %v", err)
	}
	if got := stored(orderID, "customer_reference"); got != ref {
		t.Errorf("an edit changes it: %q", got)
	}

	if _, err := db.DB.Exec(`UPDATE `+schemaName+`.documents SET status = 'Shipped' WHERE id = $1`, orderID); err != nil {
		t.Fatal(err)
	}
	invoiceID, err := CreateSalesInvoiceFromOrder(tenantID, orderID, "tester")
	if err != nil {
		t.Fatalf("CreateSalesInvoiceFromOrder: %v", err)
	}
	if got := stored(invoiceID, "customer_reference"); got != ref {
		t.Errorf("the invoice carries the order's reference: %q", got)
	}

	html, err := BuildInvoicePayload(tenantID, invoiceID, QZPrinter{})
	if err != nil || len(html.Items) == 0 || !strings.Contains(html.Items[0].Data, "Customer PO / Ref") || !strings.Contains(html.Items[0].Data, ref) {
		t.Errorf("the printed invoice shows the reference: %v", err)
	}
	escpos, err := BuildInvoicePayload(tenantID, invoiceID, QZPrinter{Language: "ESC-POS", WidthMM: "80"})
	if err != nil || len(escpos.Items) == 0 || !strings.Contains(escpos.Items[0].Data, ref) {
		t.Errorf("the receipt-printer invoice shows the reference: %v", err)
	}

	_, rows, _, err := RunReport(tenantID, "sales-by-customer-reference", "HR/Admin", "admin", map[string]string{"customer_reference": "4500124"})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the order and its invoice, got %d: %+v", len(rows), rows)
	}
	docs := map[string]map[string]interface{}{}
	for _, r := range rows {
		docs[r["document"].(string)] = r
	}
	if inv := docs["Sales Invoice"]; inv == nil || inv["order"] != orderID || inv["customer"] != "Acme Traders" || inv["customer_reference"] != ref {
		t.Errorf("invoice row: %+v", inv)
	}
	if so := docs["Sales Order"]; so == nil || so["number"] != orderID || so["amount"] != 1000.0 {
		t.Errorf("order row: %+v", so)
	}
	if _, rows, _, _ = RunReport(tenantID, "sales-by-customer-reference", "HR/Admin", "admin", map[string]string{"customer": "walk"}); len(rows) != 0 {
		t.Errorf("documents without a reference are not listed: %+v", rows)
	}
}
