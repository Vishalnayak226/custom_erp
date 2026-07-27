package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

func TestImportChannelSalesOrderUsesOrderEngine(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	const tenantID = "default"
	const sku = "CHANNEL-SO-TEST-SKU"
	const channel = "ChannelOrderTest"
	const channelOrderID = "CHANNEL-SO-TEST-1"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.DB.Exec("DELETE FROM "+schema+".channel_order_mapping WHERE channel = $1 AND channel_order_id = $2", channel, channelOrderID)
	_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' IN (SELECT id FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = $1)", channelOrderID)
	_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = $1", channelOrderID)
	_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
	_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", "ITEM-"+sku)
	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".channel_order_mapping WHERE channel = $1 AND channel_order_id = $2", channel, channelOrderID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' IN (SELECT id FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = $1)", channelOrderID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = $1", channelOrderID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", "ITEM-"+sku)
	})
	item, _ := json.Marshal(map[string]interface{}{"code": sku, "name": "Channel order test item"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEM-"+sku, item); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, 'HO', 20, 20)", sku); err != nil {
		t.Fatal(err)
	}
	input := ChannelOrderInput{Channel: channel, ChannelOrderID: channelOrderID, CustomerName: "Channel Customer", ShippingAddress: "1 Channel Road, Bengaluru 560001", PaymentStatus: "Confirmed", Lines: []SalesOrderLineInput{{SKU: sku, Qty: 2, UnitPrice: 55}}}
	orderID, err := ImportChannelSalesOrder(tenantID, input)
	if err != nil {
		t.Fatalf("ImportChannelSalesOrder: %v", err)
	}
	var doctype, status string
	if err := db.DB.QueryRow("SELECT doctype, status FROM "+schema+".documents WHERE id = $1", orderID).Scan(&doctype, &status); err != nil || doctype != "SalesOrder" || status != "Reserved" {
		t.Fatalf("expected Reserved SalesOrder, got doctype=%q status=%q err=%v", doctype, status, err)
	}
	replayID, err := ImportChannelSalesOrder(tenantID, input)
	if err != nil || replayID != orderID {
		t.Fatalf("expected idempotent replay %q, got %q err=%v", orderID, replayID, err)
	}
}
