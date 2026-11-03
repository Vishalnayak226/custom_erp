package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Stage 49.5.5: the order -> shipment -> invoice -> payment report lists each
// kind of mismatch, and an exception cannot be dismissed without a note.
func TestStage49155OrderToCashMismatches(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	lines := func(qty int) string {
		b, _ := json.Marshal([]map[string]interface{}{{"sku": "SKU-1", "qty": qty}})
		return string(b)
	}
	// SO-A: shipped 5, invoiced 3 -> shipped not invoiced
	insert("SO-A", "SalesOrder", "Confirmed", map[string]interface{}{"customer": "C1"})
	insert("SP-A", "ShippingPackage", "Shipped", map[string]interface{}{"order_id": "SO-A", "items": lines(5)})
	insert("SI-A", "SalesInvoice", "Approved", map[string]interface{}{"sales_order_id": "SO-A", "items": lines(3), "total_amount": 300.0, "customer": "C1"})
	// SO-B: shipped 2, invoiced 4 -> invoiced more than shipped
	insert("SO-B", "SalesOrder", "Confirmed", map[string]interface{}{"customer": "C2"})
	insert("SP-B", "ShippingPackage", "Shipped", map[string]interface{}{"order_id": "SO-B", "items": lines(2)})
	insert("SI-B", "SalesInvoice", "Approved", map[string]interface{}{"sales_order_id": "SO-B", "items": lines(4), "total_amount": 400.0, "customer": "C2"})
	// SO-C: matches, but the invoice is paid 500 against 300
	insert("SO-C", "SalesOrder", "Confirmed", map[string]interface{}{"customer": "C3"})
	insert("SP-C", "ShippingPackage", "Shipped", map[string]interface{}{"order_id": "SO-C", "items": lines(3)})
	insert("SI-C", "SalesInvoice", "Paid", map[string]interface{}{"sales_order_id": "SO-C", "items": lines(3), "total_amount": 300.0, "customer": "C3"})
	insert("RC-C", "CustomerReceipt", "Posted", map[string]interface{}{"invoices": []map[string]interface{}{{"ref": "SI-C", "amount": 500.0}}})
	// SO-D: fully matching - must not appear
	insert("SO-D", "SalesOrder", "Confirmed", map[string]interface{}{"customer": "C4"})
	insert("SP-D", "ShippingPackage", "Shipped", map[string]interface{}{"order_id": "SO-D", "items": lines(1)})
	insert("SI-D", "SalesInvoice", "Paid", map[string]interface{}{"sales_order_id": "SO-D", "items": lines(1), "total_amount": 100.0, "customer": "C4"})
	insert("RC-D", "CustomerReceipt", "Posted", map[string]interface{}{"invoices": []map[string]interface{}{{"ref": "SI-D", "amount": 100.0}}})
	// SI-E: an invoice with no order at all
	insert("SI-E", "SalesInvoice", "Approved", map[string]interface{}{"items": lines(1), "total_amount": 50.0, "customer": "C5"})

	find := func(rows []OrderToCashRow, kind, ref string) *OrderToCashRow {
		for i := range rows {
			if rows[i].Kind == kind && rows[i].Ref == ref {
				return &rows[i]
			}
		}
		return nil
	}
	rows, err := OrderToCashMismatches(tenantID, false)
	if err != nil {
		t.Fatalf("OrderToCashMismatches: %v", err)
	}
	if r := find(rows, "Shipped not invoiced", "SO-A"); r == nil || r.ShippedQty != 5 || r.InvoicedQty != 3 || r.Difference != 2 {
		t.Errorf("SO-A: %+v, want shipped 5 invoiced 3 diff 2", r)
	}
	if r := find(rows, "Invoiced more than shipped", "SO-B"); r == nil || r.Difference != 2 {
		t.Errorf("SO-B: %+v, want invoiced more than shipped by 2", r)
	}
	if r := find(rows, "Paid more than invoiced", "SI-C"); r == nil || r.PaidAmount != 500 || r.Difference != 200 {
		t.Errorf("SI-C: %+v, want paid 500 vs 300, diff 200", r)
	}
	if r := find(rows, "Invoice without order", "SI-E"); r == nil {
		t.Errorf("SI-E: invoice without an order not listed")
	}
	for _, r := range rows {
		if strings.HasSuffix(r.Ref, "-D") {
			t.Errorf("a matching order/invoice was listed: %+v", r)
		}
	}
	if len(rows) != 4 {
		t.Errorf("rows = %d, want 4: %+v", len(rows), rows)
	}

	// an exception needs a note
	if err := SetReconciliationException(tenantID, "Shipped not invoiced", "SO-A", "Dismissed", "  ", "", "asha"); err == nil {
		t.Fatalf("dismissing without a note should be refused")
	}
	if err := SetReconciliationException(tenantID, "Shipped not invoiced", "SO-NOPE", "Dismissed", "x", "", "asha"); err == nil {
		t.Errorf("dismissing something that is not a mismatch should be refused")
	}
	if err := SetReconciliationException(tenantID, "Shipped not invoiced", "SO-A", "Dismissed", "second invoice follows next week", "mail.pdf", "asha"); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	rows, _ = OrderToCashMismatches(tenantID, false)
	if find(rows, "Shipped not invoiced", "SO-A") != nil || len(rows) != 3 {
		t.Errorf("dismissed mismatch still listed: %d rows", len(rows))
	}
	all, _ := OrderToCashMismatches(tenantID, true)
	if r := find(all, "Shipped not invoiced", "SO-A"); r == nil || r.Exception != "Dismissed" || r.ExceptionNote == "" {
		t.Errorf("with accepted rows included: %+v, want Dismissed with its note", r)
	}
	// the figures change: the dismissal lapses
	if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = jsonb_set(data, '{items}', to_jsonb($1::text)) WHERE id = 'SI-A'", lines(2)); err != nil {
		t.Fatal(err)
	}
	rows, _ = OrderToCashMismatches(tenantID, false)
	if r := find(rows, "Shipped not invoiced", "SO-A"); r == nil || r.Difference != 3 || r.Exception != "" {
		t.Errorf("after the figures changed: %+v, want listed again, diff 3", r)
	}
	var n int
	db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".reconciliation_exceptions").Scan(&n)
	if n != 1 {
		t.Errorf("exception rows = %d, want 1", n)
	}
}
