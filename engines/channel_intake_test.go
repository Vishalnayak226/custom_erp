package engines

import (
	"custom_erp/db"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Stage 35.1 regression tests: channel intake must land a SalesOrder, on every
// path, including the two the plan found still unwired (BigCommerce, Magento)
// and the legacy signatures that used to create no document at all.

// seedChannelIntakeItem creates an Item with stock at HO and returns a cleanup
// that removes it plus any SalesOrder written against the given channel order.
func seedChannelIntakeItem(t *testing.T, schema, sku, channel, channelOrderID string) {
	t.Helper()
	clean := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".channel_order_mapping WHERE channel = $1 AND channel_order_id = $2", channel, channelOrderID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' IN (SELECT id FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel' = $1 AND data->>'channel_order_id' = $2)", channel, channelOrderID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel' = $1 AND data->>'channel_order_id' = $2", channel, channelOrderID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", "ITEM-"+sku)
	}
	clean()
	t.Cleanup(clean)

	item, _ := json.Marshal(map[string]interface{}{"code": sku, "name": "Stage 35 channel intake item"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEM-"+sku, item); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, 'HO', 50, 50)", sku); err != nil {
		t.Fatal(err)
	}
}

func assertSalesOrder(t *testing.T, schema, orderID string) {
	t.Helper()
	var doctype string
	if err := db.DB.QueryRow("SELECT doctype FROM "+schema+".documents WHERE id = $1 AND deleted_at IS NULL", orderID).Scan(&doctype); err != nil {
		t.Fatalf("order %q has no document: %v", orderID, err)
	}
	if doctype != "SalesOrder" {
		t.Fatalf("expected a SalesOrder, got doctype %q", doctype)
	}
}

// TestLegacyImportChannelOrderCreatesSalesOrder pins 35.1.1. The old body
// reserved stock and wrote a channel_order_mapping row pointing at a synthetic
// "ORD-<channel>-<id>" that was never created - so this used to "succeed"
// while leaving nothing in the order list.
func TestLegacyImportChannelOrderCreatesSalesOrder(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const sku = "TEST35-LEGACY-SKU"
	const channel = "Stage35LegacyChannel"
	const channelOrderID = "TEST35-LEGACY-1"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	seedChannelIntakeItem(t, schema, sku, channel, channelOrderID)

	orderID, err := ImportChannelOrder(tenantID, channel, channelOrderID, []map[string]interface{}{
		{"sku": sku, "qty": 3, "unit_price": 120.50},
	})
	if err != nil {
		t.Fatalf("ImportChannelOrder: %v", err)
	}
	assertSalesOrder(t, schema, orderID)
	if strings.HasPrefix(orderID, "ORD-") {
		t.Errorf("expected a real SalesOrder id, got the retired synthetic form %q", orderID)
	}

	var qty int
	var unitPrice float64
	if err := db.DB.QueryRow("SELECT (data->>'qty')::int, (data->>'unit_price')::numeric FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1", orderID).Scan(&qty, &unitPrice); err != nil {
		t.Fatalf("no SalesOrderLine for %q: %v", orderID, err)
	}
	if qty != 3 || unitPrice != 120.50 {
		t.Errorf("expected qty 3 @ 120.50 carried through the loose-map adapter, got qty=%d price=%.2f", qty, unitPrice)
	}

	// The legacy signature's documented replay contract is an error, not the
	// idempotent id the SalesOrder path returns.
	if _, err := ImportChannelOrder(tenantID, channel, channelOrderID, []map[string]interface{}{{"sku": sku, "qty": 3}}); err == nil || err.Error() != "ORDER_ALREADY_IMPORTED" {
		t.Errorf("expected ORDER_ALREADY_IMPORTED on replay, got %v", err)
	}
}

// TestChannelLinesFromLooseItemsCoercesJSONNumbers pins the quantity bug the
// old .(int) type assertion carried: a payload that arrived via
// json.Unmarshal has float64 quantities, which silently imported as qty 0.
func TestChannelLinesFromLooseItemsCoercesJSONNumbers(t *testing.T) {
	var decoded []map[string]interface{}
	if err := json.Unmarshal([]byte(`[{"sku":"A","qty":4,"unit_price":"19.99"},{"sku":"","qty":9},{"sku":"B","qty":2.0}]`), &decoded); err != nil {
		t.Fatal(err)
	}
	lines := channelLinesFromLooseItems(decoded)
	if len(lines) != 2 {
		t.Fatalf("expected the SKU-less line to be dropped, got %d lines: %#v", len(lines), lines)
	}
	if lines[0].SKU != "A" || lines[0].Qty != 4 || lines[0].UnitPrice != 19.99 {
		t.Errorf("line 0 wrong: %#v", lines[0])
	}
	if lines[1].SKU != "B" || lines[1].Qty != 2 {
		t.Errorf("line 1 wrong: %#v", lines[1])
	}
}

// TestDanglingChannelMappingDoesNotBlockReimport pins 35.1.3. A pre-35.1
// mapping row points at a document that does not exist; before the guard it
// short-circuited intake and made that channel order permanently unimportable.
func TestDanglingChannelMappingDoesNotBlockReimport(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const sku = "TEST35-DANGLING-SKU"
	const channel = "Stage35DanglingChannel"
	const channelOrderID = "TEST35-DANGLING-1"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	seedChannelIntakeItem(t, schema, sku, channel, channelOrderID)

	// Exactly what the retired importer left behind.
	phantomID := "ORD-" + channel + "-" + channelOrderID
	if _, err := db.DB.Exec("INSERT INTO "+schema+".channel_order_mapping (order_id, channel, channel_order_id) VALUES ($1, $2, $3)", phantomID, channel, channelOrderID); err != nil {
		t.Fatal(err)
	}

	orderID, err := ImportChannelSalesOrder(tenantID, ChannelOrderInput{
		Channel: channel, ChannelOrderID: channelOrderID,
		CustomerName: "Dangling Recovery", ShippingAddress: "5 Recovery Road, Bengaluru 560001",
		PaymentStatus: "Confirmed", Lines: []SalesOrderLineInput{{SKU: sku, Qty: 1, UnitPrice: 75}},
	})
	if err != nil {
		t.Fatalf("ImportChannelSalesOrder over a dangling mapping: %v", err)
	}
	if orderID == phantomID {
		t.Fatalf("intake returned the phantom id %q instead of importing", phantomID)
	}
	assertSalesOrder(t, schema, orderID)

	// The mapping row must now point at the real order, not the phantom.
	var mapped string
	if err := db.DB.QueryRow("SELECT order_id FROM "+schema+".channel_order_mapping WHERE channel = $1 AND channel_order_id = $2", channel, channelOrderID).Scan(&mapped); err != nil {
		t.Fatal(err)
	}
	if mapped != orderID {
		t.Errorf("expected the mapping row to be repointed to %q, still %q", orderID, mapped)
	}

	// And a normal replay is idempotent again.
	replayID, err := ImportChannelSalesOrder(tenantID, ChannelOrderInput{
		Channel: channel, ChannelOrderID: channelOrderID,
		CustomerName: "Dangling Recovery", ShippingAddress: "5 Recovery Road, Bengaluru 560001",
		PaymentStatus: "Confirmed", Lines: []SalesOrderLineInput{{SKU: sku, Qty: 1, UnitPrice: 75}},
	})
	if err != nil || replayID != orderID {
		t.Errorf("expected idempotent replay %q, got %q err=%v", orderID, replayID, err)
	}
}

// TestOrphanedChannelOrdersReport pins that 35.1.3's read-only view actually
// finds pre-35.1 debris.
func TestOrphanedChannelOrdersReport(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const channel = "Stage35OrphanChannel"
	const channelOrderID = "TEST35-ORPHAN-1"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.DB.Exec("DELETE FROM "+schema+".channel_order_mapping WHERE channel = $1", channel)
	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".channel_order_mapping WHERE channel = $1", channel)
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".channel_order_mapping (order_id, channel, channel_order_id) VALUES ($1, $2, $3)", "ORD-"+channel+"-"+channelOrderID, channel, channelOrderID); err != nil {
		t.Fatal(err)
	}

	rows, err := getOrphanedChannelOrdersReport(tenantID, nil)
	if err != nil {
		t.Fatalf("orphaned-channel-orders report: %v", err)
	}
	found := false
	for _, r := range rows {
		if r["channel"] == channel && r["channel_order_id"] == channelOrderID {
			found = true
			if r["source"] != "channel_order_mapping" {
				t.Errorf("wrong source column: %v", r["source"])
			}
		}
	}
	if !found {
		t.Errorf("expected the seeded orphan %s/%s in the report; got %d rows", channel, channelOrderID, len(rows))
	}
}

// TestImportBigCommerceOrderCreatesSalesOrder pins 35.1.2 for BigCommerce.
// Before this, the webhook verified the signature, audited receipt, and then
// dropped the order on the floor - it never created anything.
func TestImportBigCommerceOrderCreatesSalesOrder(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const sku = "TEST35-BC-SKU"
	const channelCode = "Stage35BigCommerce"
	const bcOrderID = int64(4411)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	seedChannelIntakeItem(t, schema, sku, channelCode, "4411")

	var hitPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitPaths = append(hitPaths, r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/products"):
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"sku": sku, "quantity": 2, "base_price": "249.0000"},
			})
		case strings.HasSuffix(r.URL.Path, "/shipping_addresses"):
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"street_1": "9 Commerce Street", "city": "Bengaluru", "zip": "560001", "phone": "9876543210"},
			})
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

	if err := SaveChannelCredential(tenantID, channelCode, map[string]string{"store_hash": "bcstore", "access_token": "bc_tok"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".channel_credentials WHERE channel_code = $1", channelCode)
	})

	orderID, err := ImportBigCommerceOrder(tenantID, channelCode, bcOrderID)
	if err != nil {
		t.Fatalf("ImportBigCommerceOrder: %v", err)
	}
	assertSalesOrder(t, schema, orderID)
	if len(hitPaths) != 3 {
		t.Errorf("expected order + products + shipping_addresses reads, got %v", hitPaths)
	}

	var status, customer string
	if err := db.DB.QueryRow("SELECT status, COALESCE(data->>'customer_name','') FROM "+schema+".documents WHERE id = $1", orderID).Scan(&status, &customer); err != nil {
		t.Fatal(err)
	}
	if status != "Reserved" {
		t.Errorf("a captured BigCommerce order with stock and a valid address should reserve, got status %q", status)
	}
	if customer != "Bee Cee" {
		t.Errorf("customer name not carried from billing_address, got %q", customer)
	}

	var lineQty int
	var linePrice float64
	if err := db.DB.QueryRow("SELECT (data->>'qty')::int, (data->>'unit_price')::numeric FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1", orderID).Scan(&lineQty, &linePrice); err != nil {
		t.Fatal(err)
	}
	if lineQty != 2 || linePrice != 249 {
		t.Errorf("expected 2 @ 249 from BigCommerce's decimal-string base_price, got %d @ %.2f", lineQty, linePrice)
	}
}

// TestImportBigCommerceOrderRequiresCredentials pins the credential gate - the
// 26.2.x "code-complete, inert until configured" shape.
func TestImportBigCommerceOrderRequiresCredentials(t *testing.T) {
	db.InitDB(testConnStr())
	if _, err := ImportBigCommerceOrder("default", "Stage35NoSuchChannel", 1); err == nil {
		t.Error("expected an error when the channel has no credentials configured")
	}
}
