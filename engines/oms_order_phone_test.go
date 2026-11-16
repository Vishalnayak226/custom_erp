package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// 215.15: an online order keeps the phone exactly as the channel sent it and a
// cleaned one. The bill takes the cleaned one, with + and the country code for
// a number from abroad, so a ten-digit UK number is never read as an Indian
// mobile.
func TestOrderKeepsPhoneAsReceived(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	insert("ITEM-215P", "Item", "Active", "system", map[string]interface{}{"code": "ITEM-215P", "name": "Mug", "sale_price": 200.0, "hsn_code": "6912", "tax_treatment": "Exempt"})
	if _, err := PostInventoryLedger(tenantID, "HO", []interface{}{map[string]interface{}{"sku": "ITEM-215P", "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ raw, clean, country, bill string }{
		{"+91 98765 43210", "9876543210", "IN", "9876543210"},
		{" +91-98765-43210 ", "9876543210", "IN", "9876543210"},
		{"098765 43210", "9876543210", "IN", "9876543210"},
		{"919876543210", "9876543210", "IN", "9876543210"},
		{"+44 7700 900123", "7700900123", "GB", "+447700900123"},
	}
	lastID := ""
	for i, c := range cases {
		id, err := CreateSalesOrder(tenantID, SalesOrderInput{Channel: "Shopify", ChannelOrderID: fmt.Sprintf("#215P%d", i), CustomerName: "Ravi",
			CustomerPhone: c.raw, ShippingAddress: "12 MG Road, Bengaluru, Karnataka 560001", PaymentStatus: "Prepaid",
			Lines: []SalesOrderLineInput{{SKU: "ITEM-215P", Qty: 1}}})
		if err != nil {
			t.Fatalf("%q: CreateSalesOrder: %v", c.raw, err)
		}
		var raw []byte
		if err := db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE id = $1", id).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var d map[string]interface{}
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatal(err)
		}
		if strField(d, "customer_phone") != c.clean || strField(d, "customer_country") != c.country {
			t.Errorf("%q: cleaned to %q (%s), want %q (%s)", c.raw, strField(d, "customer_phone"), strField(d, "customer_country"), c.clean, c.country)
		}
		if got := strField(d, "customer_phone_as_received"); got != strings.TrimSpace(c.raw) {
			t.Errorf("%q: as received %q", c.raw, got)
		}
		if got := BillPhone(tenantID, strField(d, "customer_phone"), strField(d, "customer_country")); got != c.bill {
			t.Errorf("%q: the bill gets %q, want %q", c.raw, got, c.bill)
		}
		lastID = id
	}
	// The printed bill of the UK order (no Customer record) carries +44.
	insert("SINV-215P", "SalesInvoice", "Paid", "system", map[string]interface{}{"invoice_number": "SINV-215P", "customer": "Ravi", "sales_order_id": lastID, "location": "HO", "items": []interface{}{}})
	doc, err := BuildPrintDocument(tenantID, PrintTxnSalesInvoice, "SINV-215P", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Party.Phone != "+447700900123" {
		t.Errorf("printed bill phone %q, want +447700900123", doc.Party.Phone)
	}
	// A save cleans customer_phone; the copy as received is never rewritten.
	if !IsPhoneField("customer_phone") || IsPhoneField("customer_phone_as_received") {
		t.Errorf("phone fields: customer_phone %v, customer_phone_as_received %v", IsPhoneField("customer_phone"), IsPhoneField("customer_phone_as_received"))
	}
	if BillPhone(tenantID, "", "GB") != "" || BillPhone(tenantID, "+447700900123", "GB") != "+447700900123" {
		t.Error("BillPhone: a blank stays blank, an international number is not prefixed twice")
	}
}
