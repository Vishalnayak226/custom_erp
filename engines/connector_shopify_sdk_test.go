package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeShopify answers the GraphQL calls the Stage 99 connector makes, by the
// operation each query names, and records what it was sent.
type fakeShopify struct {
	payoutOrder string // the order the fake payout pays
	mu          sync.Mutex
	seen        map[string][]map[string]interface{} // operation -> variables sent
}

func (f *fakeShopify) record(op string, vars map[string]interface{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seen == nil {
		f.seen = map[string][]map[string]interface{}{}
	}
	f.seen[op] = append(f.seen[op], vars)
}

func (f *fakeShopify) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Shopify-Access-Token") != "tok" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"errors":"[API] Invalid API key or access token"}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string                 `json:"query"`
			Variables map[string]interface{} `json:"variables"`
		}
		_ = json.Unmarshal(body, &req)
		q := req.Query
		data := `{}`
		switch {
		case strings.Contains(q, "currentAppInstallation"):
			f.record("check", req.Variables)
			data = `{"shop":{"name":"Demo","myshopifyDomain":"demo.myshopify.com","currencyCode":"INR"},
				"currentAppInstallation":{"accessScopes":[{"handle":"write_products"},{"handle":"read_inventory"},{"handle":"read_locations"},{"handle":"write_orders"},{"handle":"read_customers"},{"handle":"write_merchant_managed_fulfillment_orders"}]},
				"locations":{"edges":[{"node":{"id":"gid://shopify/Location/1","name":"Bengaluru","isActive":true}}]}}`
		case strings.Contains(q, "orders(first"):
			f.record("orders", req.Variables)
			data = `{"orders":{"pageInfo":{"hasNextPage":false,"endCursor":"c1"},"edges":[{"node":{
				"id":"gid://shopify/Order/9001","legacyResourceId":"9001","name":"#1001","email":"a@b.in","phone":"+919800000001",
				"createdAt":"2026-10-20T10:00:00Z","updatedAt":"2026-10-20T10:05:00Z","cancelledAt":null,
				"displayFinancialStatus":"PAID","displayFulfillmentStatus":"UNFULFILLED","paymentGatewayNames":["razorpay"],"currencyCode":"INR","tags":["web"],"note":"",
				"totalDiscountsSet":{"shopMoney":{"amount":"100.00"}},"totalShippingPriceSet":{"shopMoney":{"amount":"50.00"}},"totalPriceSet":{"shopMoney":{"amount":"1950.00"}},
				"taxLines":[{"title":"CGST","rate":0.025,"priceSet":{"shopMoney":{"amount":"45.00"}}},{"title":"SGST","rate":0.025,"priceSet":{"shopMoney":{"amount":"45.00"}}}],
				"customer":{"id":"gid://shopify/Customer/77","firstName":"Asha","lastName":"Rao","email":"a@b.in","phone":"+919800000001"},
				"shippingAddress":{"name":"Asha Rao","address1":"1 MG Road","address2":"","city":"Bengaluru","province":"Karnataka","provinceCode":"KA","zip":"560001","country":"India","phone":""},
				"billingAddress":null,
				"lineItems":{"edges":[{"node":{"id":"gid://shopify/LineItem/501","sku":"KURTA-RED-M","title":"Kurta","quantity":2,
					"originalUnitPriceSet":{"shopMoney":{"amount":"1000.00"}},"totalDiscountSet":{"shopMoney":{"amount":"100.00"}},
					"taxLines":[{"title":"CGST","rate":0.025,"priceSet":{"shopMoney":{"amount":"45.00"}}}]}}]}}}]}}`
		case strings.Contains(q, "inventorySetQuantities"):
			f.record("stock", req.Variables)
			data = `{"inventorySetQuantities":{"userErrors":[]}}`
		case strings.Contains(q, "fulfillmentOrders"):
			data = `{"order":{"fulfillmentOrders":{"edges":[{"node":{"id":"gid://shopify/FulfillmentOrder/1","status":"OPEN","lineItems":{"edges":[
				{"node":{"id":"gid://shopify/FulfillmentOrderLineItem/11","remainingQuantity":2,"lineItem":{"id":"gid://shopify/LineItem/501","sku":"KURTA-RED-M"}}},
				{"node":{"id":"gid://shopify/FulfillmentOrderLineItem/12","remainingQuantity":1,"lineItem":{"id":"gid://shopify/LineItem/502","sku":"KURTA-BLUE-M"}}}]}}}]}}}`
		case strings.Contains(q, "fulfillmentCreate"):
			f.record("fulfil", req.Variables)
			data = `{"fulfillmentCreate":{"fulfillment":{"id":"gid://shopify/Fulfillment/1","status":"SUCCESS"},"userErrors":[]}}`
		case strings.Contains(q, "orderCancel"):
			f.record("cancel", req.Variables)
			data = `{"orderCancel":{"orderCancelUserErrors":[]}}`
		case strings.Contains(q, "tagsAdd"):
			f.record("tags", req.Variables)
			data = `{"tagsAdd":{"userErrors":[]}}`
		case strings.Contains(q, "webhookSubscriptions(first"):
			data = `{"webhookSubscriptions":{"edges":[{"node":{"id":"gid://shopify/WebhookSubscription/1","topic":"ORDERS_CREATE","endpoint":{"__typename":"WebhookHttpEndpoint","callbackUrl":"https://erp.example/api/v1/integration/shopify/webhook?t=x"}}},
				{"node":{"id":"gid://shopify/WebhookSubscription/2","topic":"ORDERS_CREATE","endpoint":{"__typename":"WebhookHttpEndpoint","callbackUrl":"https://other.app/hook"}}}]}}`
		case strings.Contains(q, "webhookSubscriptionDelete"):
			f.record("whdel", req.Variables)
			data = `{"webhookSubscriptionDelete":{"userErrors":[]}}`
		case strings.Contains(q, "webhookSubscriptionCreate"):
			f.record("whadd", req.Variables)
			data = `{"webhookSubscriptionCreate":{"webhookSubscription":{"id":"x"},"userErrors":[]}}`
		case strings.Contains(q, "productSet("):
			f.record("productSet", req.Variables)
			// The product echoes back the variants it was sent, ids from the SKU.
			in, _ := req.Variables["input"].(map[string]interface{})
			pid := "gid://shopify/Product/300"
			if id, ok := in["id"].(string); ok && id != "" {
				pid = id
			}
			var edges []string
			for _, raw := range in["variants"].([]interface{}) {
				v := raw.(map[string]interface{})
				sku, _ := v["sku"].(string)
				bc, _ := v["barcode"].(string)
				price, _ := v["price"].(string)
				edges = append(edges, fmt.Sprintf(`{"node":{"id":"gid://shopify/ProductVariant/%s","sku":%q,"barcode":%q,"price":%q,"compareAtPrice":null,"inventoryItem":{"id":"gid://shopify/InventoryItem/%s"}}}`, sku, sku, bc, price, sku))
			}
			data = fmt.Sprintf(`{"productSet":{"product":{"id":%q,"variants":{"edges":[%s]}},"userErrors":[]}}`, pid, strings.Join(edges, ","))
		case strings.Contains(q, "productVariantsBulkUpdate"):
			f.record("prices", req.Variables)
			data = `{"productVariantsBulkUpdate":{"userErrors":[]}}`
		case strings.Contains(q, "payouts(first"):
			data = `{"shopifyPaymentsAccount":{"payouts":{"pageInfo":{"hasNextPage":false,"endCursor":""},"edges":[{"node":{"id":"gid://shopify/ShopifyPaymentsPayout/7","legacyResourceId":"7","issuedAt":"` + time.Now().UTC().Format(time.RFC3339) + `","status":"PAID","net":{"amount":"970.00"}}}]}}}`
		case strings.Contains(q, "balanceTransactions"):
			data = `{"shopifyPaymentsAccount":{"balanceTransactions":{"pageInfo":{"hasNextPage":false,"endCursor":""},"edges":[{"node":{"type":"CHARGE","amount":{"amount":"1000.00"},"fee":{"amount":"30.00"},"net":{"amount":"970.00"},"associatedOrder":{"id":"gid://shopify/Order/` + f.payoutOrder + `","name":"#1099"}}}]}}}`
		case strings.Contains(q, "productVariants(first"):
			data = `{"productVariants":{"pageInfo":{"hasNextPage":false,"endCursor":""},"edges":[{"node":{"id":"gid://shopify/ProductVariant/31","sku":"KURTA-RED-M","barcode":"890000000001","price":"999.00","compareAtPrice":null,"product":{"id":"gid://shopify/Product/300"},"inventoryItem":{"id":"gid://shopify/InventoryItem/41"}}}]}}`
		case strings.Contains(q, "nodes(ids"):
			data = `{"nodes":[{"id":"gid://shopify/InventoryItem/41","inventoryLevels":{"edges":[{"node":{"location":{"id":"gid://shopify/Location/1"},"quantities":[{"name":"available","quantity":7}]}}]}}]}`
		default:
			t.Errorf("unexpected query: %.80s", q)
		}
		_, _ = w.Write([]byte(`{"data":` + data + `}`))
	}
}

func withFakeShopify(t *testing.T) (*fakeShopify, map[string]string) {
	t.Helper()
	f := &fakeShopify{}
	server := httptest.NewServer(f.handler(t))
	old := shopifyGraphQLURL
	shopifyGraphQLURL = func(string) string { return server.URL }
	t.Cleanup(func() { shopifyGraphQLURL = old; server.Close() })
	return f, map[string]string{"shop_domain": "demo.myshopify.com", "access_token": "tok"}
}

// Stage 99.1: Shopify as a full SDK connector, every call against a fake.
func TestStage991ShopifyConnector(t *testing.T) {
	f, cred := withFakeShopify(t)
	ctx := context.Background()
	c, err := ResolveOmnichannelConnector("Shopify")
	if err != nil {
		t.Fatal(err)
	}
	if d := c.Descriptor(); !d.PullOrders || !d.PushInventory || !d.PushStatus || !d.PublishCatalogue {
		t.Fatalf("Shopify is a full connector now: %+v", d)
	}
	sdk := c.(shopifySDKConnector)
	if err := sdk.ValidateCredentials(map[string]string{"shop_domain": "x"}); err == nil || !strings.Contains(err.Error(), "access_token") {
		t.Errorf("a missing token is named: %v", err)
	}

	// 99.1.2: the token works, the locations are listed, missing access named.
	chk, err := sdk.CheckShopify(ctx, cred)
	if err != nil {
		t.Fatal(err)
	}
	if chk.Shop != "Demo" || len(chk.Locations) != 1 || chk.Locations[0].Name != "Bengaluru" {
		t.Errorf("shop and locations: %+v", chk)
	}
	if strings.Join(chk.MissingScopes, ",") != "write_inventory" {
		t.Errorf("only write_inventory is missing (write_* covers read_*): %v", chk.MissingScopes)
	}
	if _, err := sdk.CheckShopify(ctx, map[string]string{"shop_domain": "demo.myshopify.com", "access_token": "bad"}); err == nil {
		t.Errorf("a wrong token is refused")
	}

	// Orders: the whole order, from the pull.
	orders, next, err := sdk.PullShopifyOrders(ctx, cred, time.Now().Add(-time.Hour), "")
	if err != nil || len(orders) != 1 || next != "" {
		t.Fatalf("pull: %v %v %q", orders, err, next)
	}
	o := orders[0]
	if o.ID != "9001" || o.Number != "#1001" || o.Shipping != 50 || o.Discount != 100 || len(o.Tax) != 2 || o.Ship.Province != "Karnataka" ||
		o.Phone != "+919800000001" || len(o.Lines) != 1 || o.Lines[0].Discount != 100 || o.Lines[0].UnitPrice != 1000 || shopifyPaymentStatus(o) != "Confirmed" {
		t.Errorf("the order as captured: %+v", o)
	}
	if !strings.Contains(f.seen["orders"][0]["query"].(string), "updated_at:>") {
		t.Errorf("the pull asks for orders updated since the last run: %v", f.seen["orders"][0])
	}

	// The webhook body (REST) reads into the same shape.
	w, err := ParseShopifyOrderWebhook([]byte(`{"id":9002,"name":"#1002","email":"c@d.in","phone":null,"created_at":"2026-10-20T10:00:00Z","updated_at":"2026-10-20T10:00:00Z",
		"financial_status":"pending","payment_gateway_names":["Cash on Delivery (COD)"],"currency":"INR","tags":"vip, web","total_discounts":"0.00","total_price":"500.00",
		"tax_lines":[{"title":"IGST","rate":0.05,"price":"23.81"}],"customer":{"id":88,"first_name":"Ravi","last_name":"K","phone":"9800000002"},
		"shipping_address":{"first_name":"Ravi","last_name":"K","address1":"2 Park St","city":"Kolkata","province":"West Bengal","province_code":"WB","zip":"700016","country":"India"},
		"shipping_lines":[{"price":"40.00"}],"line_items":[{"id":601,"sku":"KURTA-BLUE-M","title":"Kurta","quantity":1,"price":"460.00","total_discount":"0.00","tax_lines":[]}]}`))
	if err != nil || w.ID != "9002" || !w.COD() || shopifyPaymentStatus(w) != "COD" || w.Shipping != 40 || w.Ship.Name != "Ravi K" || len(w.Tags) != 2 || w.Phone != "9800000002" {
		t.Errorf("the webhook order: %+v %v", w, err)
	}

	// Stock: grouped, never below zero.
	if err := sdk.PushInventory(ctx, cred, []ConnectorInventoryUpdate{{ChannelSKU: "A", ProductID: "gid://shopify/InventoryItem/41", LocationCode: "gid://shopify/Location/1", Quantity: -3},
		{ChannelSKU: "B", ProductID: "gid://shopify/InventoryItem/42", LocationCode: "gid://shopify/Location/1", Quantity: 5}}); err != nil {
		t.Fatal(err)
	}
	qs := f.seen["stock"][0]["input"].(map[string]interface{})["quantities"].([]interface{})
	if len(f.seen["stock"]) != 1 || len(qs) != 2 || qs[0].(map[string]interface{})["quantity"].(float64) != 0 {
		t.Errorf("one grouped call, a negative sent as 0: %v", f.seen["stock"])
	}
	if err := sdk.PushInventory(ctx, cred, []ConnectorInventoryUpdate{{ChannelSKU: "A"}}); err == nil {
		t.Errorf("stock without its inventory item is refused")
	}
	if lv, err := sdk.ReadStockLevels(ctx, cred, []string{"gid://shopify/InventoryItem/41"}); err != nil || len(lv) != 1 || lv[0].Available != 7 {
		t.Errorf("stock levels read back: %v %v", lv, err)
	}

	// 99.6: a part shipment by SKU with tracking; a cancellation; RTO.
	if err := sdk.PushOrderStatus(ctx, cred, ConnectorStatusUpdate{ChannelOrderID: "9001", Status: "Shipped", Carrier: "Delhivery", TrackingNumber: "TRK1",
		Items: []ConnectorStatusLine{{OrderItemID: "KURTA-RED-M", Quantity: 1}}}); err != nil {
		t.Fatal(err)
	}
	ff := f.seen["fulfil"][0]["fulfillment"].(map[string]interface{})
	lines := ff["lineItemsByFulfillmentOrder"].([]interface{})[0].(map[string]interface{})["fulfillmentOrderLineItems"].([]interface{})
	if len(lines) != 1 || lines[0].(map[string]interface{})["quantity"].(float64) != 1 || ff["trackingInfo"].(map[string]interface{})["number"] != "TRK1" {
		t.Errorf("part shipment of one red kurta with tracking: %v", ff)
	}
	if err := sdk.PushOrderStatus(ctx, cred, ConnectorStatusUpdate{ChannelOrderID: "9001", Status: "Cancelled"}); err != nil || f.seen["cancel"][0]["orderId"] != "gid://shopify/Order/9001" {
		t.Errorf("cancellation: %v %v", err, f.seen["cancel"])
	}
	if err := sdk.PushOrderStatus(ctx, cred, ConnectorStatusUpdate{ChannelOrderID: "9001", Status: "RTO"}); err != nil || len(f.seen["tags"]) != 1 {
		t.Errorf("RTO tag: %v", err)
	}

	// 99.2.2: our old subscription replaced, another app's kept.
	topics, err := sdk.RegisterWebhooks(ctx, cred, "https://erp.example/api/v1/integration/shopify/webhook?t=x&c=Shopify")
	if err != nil || len(topics) != len(ShopifyWebhookTopics) || len(f.seen["whdel"]) != 1 || f.seen["whdel"][0]["id"] != "gid://shopify/WebhookSubscription/1" {
		t.Errorf("webhooks: %v %v %v", topics, err, f.seen["whdel"])
	}

	// 99.3.1: a design with two variants, MRP as compare-at, weight.
	pid, links, err := sdk.PublishShopifyProduct(ctx, cred, ShopifyProductInput{Title: "Kurta", Vendor: "Acme", HSN: "6109", OptionOrder: []string{"Colour", "Size"},
		Variants: []ShopifyVariantInput{{SKU: "KURTA-RED-M", Barcode: "890000000001", Price: 999, CompareAt: 1299, WeightGram: 250, Options: map[string]string{"Colour": "Red", "Size": "M"}},
			{SKU: "KURTA-BLUE-M", Price: 999, CompareAt: 999, Options: map[string]string{"Colour": "Blue", "Size": "M"}}}})
	if err != nil || pid != "gid://shopify/Product/300" || len(links) != 2 || links[0].InventoryItemID == "" {
		t.Fatalf("publish: %v %v %v", pid, links, err)
	}
	in := f.seen["productSet"][0]["input"].(map[string]interface{})
	vs := in["variants"].([]interface{})
	if len(in["productOptions"].([]interface{})) != 2 || vs[0].(map[string]interface{})["compareAtPrice"] != "1299.00" || vs[1].(map[string]interface{})["compareAtPrice"] != nil || in["vendor"] != "Acme" {
		t.Errorf("options, compare-at only above the price, the brand: %v", in)
	}
	if vl, err := sdk.ListVariants(ctx, cred); err != nil || len(vl) != 1 || vl[0].SKU != "KURTA-RED-M" {
		t.Errorf("variants listed for linking: %v %v", vl, err)
	}
}
