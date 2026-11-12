package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 49.5.7 (webhook duplication): a provider that re-sends the same order under a NEW delivery id
// (a retry after a timeout, a replay from its dashboard, the order pulled and pushed) posts it
// once. The receipt table stops same-id repeats; the order lock stops the rest.
func TestStage4957ShopifyOrderRedeliveredUnderNewIdsPostsOnce(t *testing.T) {
	handler := bld031ShopifyFixture(t)
	bld031SetShopifySecret(t, "stage4957-shopify-secret")
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	sku := engines.NewDocID("S4957SKU")
	orderID := engines.NewDocID("S4957ORDER")
	seedShopifyItem(t, schema, sku)
	defer cleanupShopifyOrder(schema, orderID, sku)

	body := bld031ShopifyOrderPayload(orderID, sku)
	sig := bld031ShopifySign(body, "stage4957-shopify-secret")
	for i := 0; i < 4; i++ { // four deliveries, four different ids, the second one late
		req := httptest.NewRequest("POST", "/api/v1/integration/shopify/order", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", "default")
		req.Header.Set("X-Shopify-Hmac-Sha256", sig)
		req.Header.Set("X-Shopify-Webhook-Id", engines.NewDocID("WH"))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("delivery %d: %d %s, want 200", i, rec.Code, rec.Body.String())
		}
	}
	var orders, lines int
	_ = db.DB.QueryRow("SELECT count(*) FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = $1", orderID).Scan(&orders)
	_ = db.DB.QueryRow("SELECT count(*) FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' IN (SELECT id FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = $1)", orderID).Scan(&lines)
	if orders != 1 {
		t.Fatalf("%d SalesOrders after 4 deliveries of one order, want 1", orders)
	}
	if lines != 1 {
		t.Fatalf("%d order lines after 4 deliveries, want 1 (stock reserved once)", lines)
	}
}
