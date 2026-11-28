package engines

import (
	"context"
	"custom_erp/db"
	"strings"
	"testing"
)

// 215.17: a return the website sends (a Shopify refund) is booked by itself
// against the online bill that holds the refunded item - the second parcel's
// bill when the order went out in two - taken back into that bill's store,
// once however often the webhook repeats, and listed in Sales History.
func TestChannelReturnTakesTheRightBill(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	for _, sku := range []string{"ITEM-215A", "ITEM-215B"} {
		insert(sku, "Item", "Active", "system", map[string]interface{}{"code": sku, "name": "Mug " + sku, "sale_price": 500.0, "hsn_code": "6912", "tax_treatment": "Exempt"})
		if _, err := PostInventoryLedger(tenantID, "HO", []interface{}{map[string]interface{}{"sku": sku, "qty": 10.0}}, false); err != nil {
			t.Fatal(err)
		}
	}
	orderID, err := CreateSalesOrder(tenantID, SalesOrderInput{Channel: "Shopify", ChannelOrderID: "215901", CustomerName: "Ravi",
		CustomerPhone: "+91 98765 43210", ShippingAddress: "12 MG Road, Bengaluru, Karnataka 560001", PaymentStatus: "Prepaid",
		Lines: []SalesOrderLineInput{{SKU: "ITEM-215A", Qty: 1}, {SKU: "ITEM-215B", Qty: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	// Two parcels, a bill each: A on the first, B on the second.
	for i, b := range []struct{ id, sku string }{{"SINV-215R1", "ITEM-215A"}, {"SINV-215R2", "ITEM-215B"}} {
		insert(b.id, "SalesInvoice", "Paid", "system", map[string]interface{}{"invoice_number": b.id, "customer": "Ravi", "sales_order_id": orderID,
			"location": "HO", "total_amount": 500.0, "items": `[{"sku": "` + b.sku + `", "qty": 1, "unit_price": 500}]`})
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET created_at = now() - make_interval(mins => $2) WHERE id = $1", b.id, 10-i); err != nil {
			t.Fatal(err)
		}
	}

	refund := []byte(`{"id": 7701, "order_id": 215901, "refund_line_items": [{"quantity": 1, "line_item": {"sku": "ITEM-215B"}}]}`)
	r, err := handleShopifyRefund(tenantID, "Shopify", refund)
	if err != nil || !strings.HasPrefix(r, "return ") {
		t.Fatalf("refund after shipping: %q %v", r, err)
	}
	returnID := strings.TrimSuffix(strings.TrimPrefix(r, "return "), " raised")
	var against, location string
	if err := db.DB.QueryRow("SELECT data->>'original_order_id', COALESCE(data->>'return_location', '') FROM "+schema+".documents WHERE id = $1", returnID).Scan(&against, &location); err != nil {
		t.Fatal(err)
	}
	if against != "SINV-215R2" || location != "HO" {
		t.Errorf("the return is against %q at %q, want SINV-215R2 at HO (the bill holding ITEM-215B)", against, location)
	}

	// The webhook repeats: still one return.
	if r2, err := handleShopifyRefund(tenantID, "Shopify", refund); err != nil || r2 != r {
		t.Errorf("a repeated refund raised %q %v, want %q", r2, err, r)
	}
	var n int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'ReturnRequest' AND deleted_at IS NULL").Scan(&n)
	if n != 1 {
		t.Errorf("%d returns, want 1", n)
	}

	// A return quoting the order itself (the website's API) finds the same bill.
	if got := salesInvoiceForOrderItems(tenantID, orderID, []ReturnItemInput{{SKU: "ITEM-215A", Qty: 1}}); got != "SINV-215R1" {
		t.Errorf("ITEM-215A is on %q, want SINV-215R1", got)
	}
	if got := salesInvoiceForOrderItems(tenantID, orderID, []ReturnItemInput{{SKU: "ITEM-215B", Qty: 1}}); got != "SINV-215R1" {
		t.Errorf("ITEM-215B, already returned, falls back to the first bill; got %q", got)
	}

	// Sales History lists it as an online return against that bill.
	h, err := SearchSalesHistory(context.Background(), tenantID, SalesHistoryFilter{Kind: "Return"})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Rows) != 1 || h.Rows[0].Source != "Online" || h.Rows[0].AgainstID != "SINV-215R2" {
		t.Errorf("Sales History returns: %+v", h.Rows)
	}
}
