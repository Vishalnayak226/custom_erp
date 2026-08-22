package engines

import (
	"context"
	"custom_erp/db"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type stage356PagerConnector struct {
	mu       *sync.Mutex
	requests *[]ConnectorPullRequest
}

func (stage356PagerConnector) Descriptor() ConnectorDescriptor {
	return ConnectorDescriptor{Code: "Stage356Pager", Kind: "marketplace", PullOrders: true}
}
func (stage356PagerConnector) RateLimit() (int, time.Duration)             { return 100, time.Minute }
func (stage356PagerConnector) ValidateCredentials(map[string]string) error { return nil }
func (stage356PagerConnector) PublishProduct(context.Context, map[string]string, ChannelProductPayload) (string, error) {
	return "", nil
}
func (c stage356PagerConnector) PullOrders(_ context.Context, _ map[string]string, req ConnectorPullRequest) (ConnectorOrderPage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	*c.requests = append(*c.requests, req)
	if len(*c.requests) == 1 {
		return ConnectorOrderPage{NextCursor: "PAGE-2"}, nil
	}
	return ConnectorOrderPage{}, nil
}
func (stage356PagerConnector) PushInventory(context.Context, map[string]string, []ConnectorInventoryUpdate) error {
	return nil
}
func (stage356PagerConnector) PushOrderStatus(context.Context, map[string]string, ConnectorStatusUpdate) error {
	return nil
}
func (stage356PagerConnector) MapError(err error) string { return DefaultConnectorErrorCode(err) }

func TestStage356ConnectorDescriptorCoverage(t *testing.T) {
	want := map[string]bool{"Amazon": true, "Flipkart": true, "Myntra": true, "Meesho": true, "Ajio": true, "Nykaa": true, "Blinkit": true, "Zepto": true, "SwiggyInstamart": true, "WooCommerce": true, "Shopify": true, "BigCommerce": true, "Magento": true}
	for _, descriptor := range ListConnectorDescriptors() {
		delete(want, descriptor.Code)
		if descriptor.Kind == "quick_commerce" && (!descriptor.RequiresLocation || !descriptor.NoSplit) {
			t.Errorf("quick-commerce connector %s must require one location and forbid splits", descriptor.Code)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing connector descriptors: %#v", want)
	}
	legacy, err := ResolveOmnichannelConnector("Shopify")
	if err != nil || legacy.Descriptor().PushInventory {
		t.Fatalf("legacy capability declaration is inaccurate: %#v %v", legacy, err)
	}
}

func TestStage356WooCommerceOperations(t *testing.T) {
	var mu sync.Mutex
	seen := []string{}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("ck_test:cs_test"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != auth {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/wp-json/wc/v3/orders":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{{"id": 35601, "status": "processing", "billing": map[string]interface{}{"phone": "9999999999"}, "shipping": map[string]interface{}{"first_name": "Test", "last_name": "Buyer", "address_1": "1 Main St", "city": "Bengaluru", "state": "KA", "postcode": "560001", "country": "IN"}, "line_items": []map[string]interface{}{{"sku": "WC-SKU", "quantity": 2, "price": 125.5}}}})
		case "/wp-json/wc/v3/products":
			_, _ = w.Write([]byte(`{"id":991}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	connector := wooCommerceConnector{}
	cred := map[string]string{"base_url": server.URL, "consumer_key": "ck_test", "consumer_secret": "cs_test"}
	page, err := connector.PullOrders(context.Background(), cred, ConnectorPullRequest{Limit: 100})
	if err != nil || len(page.Orders) != 1 || page.Orders[0].Lines[0].ChannelSKU != "WC-SKU" {
		t.Fatalf("pull: %#v %v", page, err)
	}
	if err := connector.PushInventory(context.Background(), cred, []ConnectorInventoryUpdate{{ChannelSKU: "WC-SKU", ProductID: "991", Quantity: 7}}); err != nil {
		t.Fatal(err)
	}
	if err := connector.PushOrderStatus(context.Background(), cred, ConnectorStatusUpdate{ChannelOrderID: "35601", Status: "Completed"}); err != nil {
		t.Fatal(err)
	}
	if id, err := connector.PublishProduct(context.Background(), cred, ChannelProductPayload{ItemCode: "WC-SKU", Title: "Test"}); err != nil || id != "991" {
		t.Fatalf("publish id=%q err=%v", id, err)
	}
	mu.Lock()
	joined := strings.Join(seen, "\n")
	mu.Unlock()
	for _, call := range []string{"GET /wp-json/wc/v3/orders", "PUT /wp-json/wc/v3/products/991", "PUT /wp-json/wc/v3/orders/35601", "POST /wp-json/wc/v3/products"} {
		if !strings.Contains(joined, call) {
			t.Errorf("missing %s in:\n%s", call, joined)
		}
	}
}

func TestStage356AmazonOrderContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orders/2026-01-01/orders" {
			t.Errorf("path = %s", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("includedData") != "RECIPIENT,BUYER" || query.Get("paginationToken") != "NEXT-1" || query.Get("nextToken") != "" || query.Get("maxResultsPerPage") != "50" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		if r.Header.Get("x-amz-access-token") != "token" {
			t.Errorf("missing LWA access token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"orders":[{"orderId":"AMZ-356","buyer":{"buyerName":"Buyer"},"recipient":{"deliveryAddress":{"name":"Buyer","addressLine1":"1 Main","city":"Bengaluru","stateOrRegion":"KA","postalCode":"560001","countryCode":"IN","phone":"9999999999"}},"orderItems":[{"quantityOrdered":2,"product":{"sellerSku":"AMZ-SKU","price":{"unitPrice":{"amount":"199.50","currencyCode":"INR"}}}}]}],"pagination":{"nextToken":"NEXT-2"}}`))
	}))
	defer server.Close()
	connector := amazonSPConnector{}
	page, err := connector.PullOrders(context.Background(), map[string]string{"base_url": server.URL, "seller_id": "SELLER", "marketplace_id": "A21TJRUUN4KGV", "access_token": "token"}, ConnectorPullRequest{Cursor: "NEXT-1", Limit: 50})
	if err != nil || len(page.Orders) != 1 || page.NextCursor != "NEXT-2" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	order := page.Orders[0]
	if order.CustomerName != "Buyer" || order.ShippingState != "KA" || order.Lines[0].ChannelSKU != "AMZ-SKU" || order.Lines[0].UnitPrice != 199.5 {
		t.Fatalf("order=%#v", order)
	}
}

func TestStage356FlipkartOrderContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/sellers/v3/shipments/filter/" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"nextPageUrl":"https://next.example/page","shipments":[{"shipmentId":"SHIP-356","locationId":"LOC-FK","orderItems":[{"orderItemId":"ITEM-1","sku":"FK-SKU","quantity":3,"priceComponents":{"sellingPrice":275.25}}]}]}`))
	}))
	defer server.Close()
	connector := flipkartConnector{}
	page, err := connector.PullOrders(context.Background(), map[string]string{"base_url": server.URL, "access_token": "token"}, ConnectorPullRequest{})
	if err != nil || len(page.Orders) != 1 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	order := page.Orders[0]
	if order.LocationCode != "LOC-FK" || order.Lines[0].ChannelSKU != "FK-SKU" || order.Lines[0].UnitPrice != 275.25 {
		t.Fatalf("order=%#v", order)
	}
}

func TestStage356SKUExceptionResolution(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var ready bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.doctype_meta WHERE name='ChannelSKUException')`).Scan(&ready); err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Skip("db/migrations_stage35_6_channel_breadth.sql has not been applied")
	}
	const channel, channelSKU, sku = "TEST-STAGE356", "EXT-356", "ERP-356"
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE doctype='ChannelSKUException' AND data->>'channel'=$1`, channel)
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.channel_product_mapping WHERE channel=$1`, channel)
	}
	cleanup()
	t.Cleanup(cleanup)
	if err := RecordChannelSKUException("default", channel, channelSKU, "ORDER-1"); err != nil {
		t.Fatal(err)
	}
	if err := RecordChannelSKUException("default", channel, channelSKU, "ORDER-2"); err != nil {
		t.Fatal(err)
	}
	exceptions, err := ListOpenChannelSKUExceptions("default", channel)
	if err != nil || len(exceptions) != 1 || exceptions[0].Occurrences != 2 || exceptions[0].LastOrderID != "ORDER-2" {
		t.Fatalf("exceptions=%#v err=%v", exceptions, err)
	}
	if err := UpsertChannelSKUMapping("default", ChannelSKUMapping{SKU: sku, Channel: channel, ChannelSKU: channelSKU, ExternalProductID: "PRODUCT-356", LocationCode: "LOC-356"}); err != nil {
		t.Fatal(err)
	}
	exceptions, err = ListOpenChannelSKUExceptions("default", channel)
	if err != nil || len(exceptions) != 0 {
		t.Fatalf("resolved exceptions=%#v err=%v", exceptions, err)
	}
	mappings, err := ListChannelSKUMappings("default", channel)
	if err != nil || len(mappings) != 1 || mappings[0].ExternalProductID != "PRODUCT-356" || mappings[0].LocationCode != "LOC-356" {
		t.Fatalf("mappings=%#v err=%v", mappings, err)
	}
}

func TestStage356OrderPaginationKeepsWindowAndClearsCursor(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	const channel = "TEST-STAGE356-PAGER"
	_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id=$1 OR (doctype='ChannelSyncRun' AND data->>'channel'=$1)`, channel)
	_, _ = db.DB.Exec(`DELETE FROM `+schema+`.channel_credentials WHERE channel_code=$1`, channel)
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id=$1 OR (doctype='ChannelSyncRun' AND data->>'channel'=$1)`, channel)
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.channel_credentials WHERE channel_code=$1`, channel)
	})
	data, _ := json.Marshal(map[string]interface{}{"code": channel, "platform": "Stage356Pager", "sync_interval_minutes": 15, "status": "Active"})
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents(id,doctype,data,status,created_by) VALUES($1,'Channel',$2,'Active','system')`, channel, data); err != nil {
		t.Fatal(err)
	}
	if err := SaveChannelCredential("default", channel, map[string]string{"token": "test"}); err != nil {
		t.Fatal(err)
	}
	requests := []ConnectorPullRequest{}
	registerOmnichannelConnector(stage356PagerConnector{mu: &sync.Mutex{}, requests: &requests})
	processed, failed, err := PullChannelOrders(context.Background(), "default", channel)
	if err != nil || processed != 0 || failed != 0 || len(requests) != 2 {
		t.Fatalf("processed=%d failed=%d requests=%#v err=%v", processed, failed, requests, err)
	}
	if requests[0].Cursor != "" || requests[1].Cursor != "PAGE-2" || !requests[0].UpdatedAfter.Equal(requests[1].UpdatedAfter) {
		t.Fatalf("pagination did not preserve its window: %#v", requests)
	}
	var cursor, cursorStarted string
	if err := db.DB.QueryRow(`SELECT COALESCE(data->>'last_cursor',''),COALESCE(data->>'last_cursor_started_at','') FROM `+schema+`.documents WHERE id=$1`, channel).Scan(&cursor, &cursorStarted); err != nil {
		t.Fatal(err)
	}
	if cursor != "" || cursorStarted != "" {
		t.Fatalf("completed pagination left cursor=%q started=%q", cursor, cursorStarted)
	}
}
