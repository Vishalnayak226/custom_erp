package engines

import (
	"custom_erp/db"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// Stage 148.2 - a channel (Shopify) order's stored shipping_charge is billed
// on the invoice raised from the order: in the total, in the tax invoice's
// GST at the order's rate, in the GL, and on the printed invoice. An order
// without shipping invoices exactly as before.
func TestStage1482ChannelShippingOnInvoice(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	raw, _ := json.Marshal(map[string]interface{}{"code": "ITEM-SH", "name": "Kurta", "sale_price": 500.0, "hsn_code": "6104", "tax_treatment": "Taxable", "gst_rate": 18.0})
	if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ('ITEM-SH', 'Item', $1, 'Active', 'system')", raw); err != nil {
		t.Fatal(err)
	}
	if _, err := PostInventoryLedger(tenantID, "HO", []interface{}{map[string]interface{}{"sku": "ITEM-SH", "qty": 20.0}}, false); err != nil {
		t.Fatal(err)
	}
	stored := func(id, key string) float64 {
		var v float64
		_ = db.DB.QueryRow(`SELECT COALESCE((data->>'`+key+`')::numeric, 0) FROM `+schemaName+`.documents WHERE id = $1`, id).Scan(&v)
		return v
	}
	newOrder := func(shipping float64) string {
		id, err := CreateSalesOrder(tenantID, SalesOrderInput{Channel: "Shopify", CustomerName: "Shopify Buyer", ShippingAddress: "12 MG Road, Bengaluru, Karnataka 560001",
			PaymentStatus: "Confirmed", Lines: []SalesOrderLineInput{{SKU: "ITEM-SH", Qty: 2, UnitPrice: 500}}})
		if err != nil {
			t.Fatalf("CreateSalesOrder: %v", err)
		}
		if shipping > 0 {
			if err := mergeSalesOrderData(schemaName, id, map[string]interface{}{"shipping_charge": shipping}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.DB.Exec(`UPDATE `+schemaName+`.documents SET status = 'Shipped' WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// No shipping: the invoice is what it was (1000 incl. 18% = 152.54 tax).
	plain, err := CreateSalesInvoiceFromOrder(tenantID, newOrder(0), "tester")
	if err != nil {
		t.Fatal(err)
	}
	if got := stored(plain, "total_amount"); got != 1000 {
		t.Errorf("no shipping: total %v, want 1000", got)
	}
	if got := stored(plain, "shipping_charge"); got != 0 {
		t.Errorf("no shipping: no charge stored, got %v", got)
	}

	// Shipping 118 (incl. GST): total 1118, tax 152.54 + 18.00.
	inv, err := CreateSalesInvoiceFromOrder(tenantID, newOrder(118), "tester")
	if err != nil {
		t.Fatal(err)
	}
	if got := stored(inv, "total_amount"); got != 1118 {
		t.Errorf("total_amount %v, want 1118", got)
	}
	if got := stored(inv, "shipping_charge"); got != 118 {
		t.Errorf("shipping_charge %v, want 118", got)
	}
	if got := stored(inv, "total_tax"); math.Abs(got-170.54) > 0.02 {
		t.Errorf("total_tax %v, want about 170.54 (items plus shipping GST)", got)
	}
	if got := stored(inv, "taxable_amount"); math.Abs(got-947.46) > 0.02 {
		t.Errorf("taxable_amount %v, want about 947.46", got)
	}
	if c, s := stored(inv, "cgst"), stored(inv, "sgst"); math.Abs(c+s-stored(inv, "total_tax")) > 0.011 && stored(inv, "igst") == 0 {
		t.Errorf("cgst+sgst %v must equal total_tax", c+s)
	}

	// The GL books the whole total and the whole tax.
	if _, err := PostSalesInvoice(tenantID, inv, "tester"); err != nil {
		t.Fatalf("PostSalesInvoice: %v", err)
	}
	var debit, taxCredit, revenue int64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit),0) FROM `+schemaName+`.gl_postings WHERE document_type = 'SalesInvoice' AND document_id = $1`, inv).Scan(&debit)
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(credit),0) FROM `+schemaName+`.gl_postings WHERE document_type = 'SalesInvoice' AND document_id = $1 AND account_code LIKE '22%'`, inv).Scan(&taxCredit)
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(credit),0) FROM `+schemaName+`.gl_postings WHERE document_type = 'SalesInvoice' AND document_id = $1 AND account_code = '4100'`, inv).Scan(&revenue)
	if debit != 111800 {
		t.Errorf("GL debit %d paise, want 111800", debit)
	}
	if d := taxCredit - int64(math.Round(stored(inv, "total_tax")*100)); d > 1 || d < -1 {
		t.Errorf("GL tax credit %d does not match the invoice's tax %v", taxCredit, stored(inv, "total_tax"))
	}
	if taxCredit+revenue != 111800 {
		t.Errorf("GL credits %d, want 111800", taxCredit+revenue)
	}

	// The printed invoice has a shipping line with its GST.
	doc, err := BuildPrintDocument(tenantID, PrintTxnSalesInvoice, inv, "", false)
	if err != nil {
		t.Fatal(err)
	}
	var lineTotal, shipLines float64
	for _, l := range doc.Lines {
		lineTotal += numFromInterface(l["amount"])
		if strings.Contains(strings.ToLower(strField(l, "item_name")+strField(l, "description")), "shipping") {
			shipLines++
		}
	}
	if shipLines != 1 {
		t.Errorf("printed invoice has %v shipping lines, want 1", shipLines)
	}
	if math.Abs(lineTotal-1118) > 0.05 {
		t.Errorf("printed lines add to %v, want 1118", lineTotal)
	}

	// The e-invoice carries it too (SAC 9968, 18%).
	irp, err := BuildCanonicalTaxDocument(tenantID, "SalesInvoice", inv)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range irp.ItemList {
		if it.HsnCd == "9968" && it.GstRt == 18 {
			found = true
		}
	}
	if !found {
		t.Errorf("e-invoice has no shipping item: %+v", irp.ItemList)
	}
}
