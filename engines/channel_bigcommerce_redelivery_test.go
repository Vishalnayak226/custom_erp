package engines

import (
	"custom_erp/db"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// 49.5.7 (webhook duplication, BigCommerce): a BigCommerce hook carries only
// the order id, and one order raises several hooks with different bodies
// (store/order/created, /updated, /statusUpdated, a dashboard replay), so the
// handler's body-hash receipt cannot fold them. Each one reads the order back
// over the API; four of them arriving at once still make one SalesOrder with
// one line, and every caller is told the same order id.
func TestStage4957BigCommerceOrderReadBackFourTimesImportsOnce(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const sku = "T4957-BC-SKU"
	const channelCode = "Stage4957BigCommerce"
	const bcOrderID = int64(495707)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	seedChannelIntakeItem(t, schema, sku, channelCode, "495707")

	var mu sync.Mutex
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reads++
		mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/products"):
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{{"sku": sku, "quantity": 2, "base_price": "249.0000"}})
		case strings.HasSuffix(r.URL.Path, "/shipping_addresses"):
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{{"street_1": "9 Commerce Street", "city": "Bengaluru", "zip": "560001", "phone": "9876543210"}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": bcOrderID, "payment_status": "captured",
				"billing_address": map[string]interface{}{"first_name": "Bee", "last_name": "Cee"},
			})
		}
	}))
	defer server.Close()
	origURL := bigCommerceV2BaseURL
	bigCommerceV2BaseURL = func(storeHash string) string { return server.URL + "/stores/" + storeHash + "/v2" }
	defer func() { bigCommerceV2BaseURL = origURL }()

	if err := SaveChannelCredential(tenantID, channelCode, map[string]string{"store_hash": "bcstore4957", "access_token": "bc_tok"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".channel_credentials WHERE channel_code = $1", channelCode)
	})

	ids := make([]string, 4)
	errs := make([]error, 4)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i], errs[i] = ImportBigCommerceOrder(tenantID, channelCode, bcOrderID)
		}(i)
	}
	wg.Wait()
	// The late replay, after the burst.
	late, lateErr := ImportBigCommerceOrder(tenantID, channelCode, bcOrderID)
	ids, errs = append(ids, late), append(errs, lateErr)

	for i := range ids {
		if errs[i] != nil {
			t.Fatalf("hook %d: %v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Errorf("hook %d was told order %q, hook 0 was told %q", i, ids[i], ids[0])
		}
	}
	assertSalesOrder(t, schema, ids[0])
	var orders, lines int
	_ = db.DB.QueryRow("SELECT count(*) FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel' = $1 AND data->>'channel_order_id' = '495707'", channelCode).Scan(&orders)
	_ = db.DB.QueryRow("SELECT count(*) FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1", ids[0]).Scan(&lines)
	if orders != 1 || lines != 1 {
		t.Fatalf("five hooks for one BigCommerce order made %d orders and %d lines, want 1 and 1", orders, lines)
	}
}
