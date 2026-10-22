package engines

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
)

// Stage 99.2-99.7 end to end against the fake Shopify: settings and links,
// publishing a design (and again, never a duplicate), prices and stock sent
// only when they change, an order imported once with its customer and GST,
// a Shopify change applied, the status job, a refund, payouts.
func TestStage999ShopifySync(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	var ready bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = 'shopify_links')`, schema).Scan(&ready)
	if !ready {
		t.Skip("needs migrations_stage99_shopify_links.sql - run erp-server -migrate on this database")
	}
	f, cred := withFakeShopify(t)
	f.payoutOrder = "99001"
	ctx := context.Background()
	const ch, store, design = "SHOP-T999", "S999-STORE", "S999-DES"
	skus := []string{"S999-RED-M", "S999-BLUE-M"}
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".shopify_links WHERE channel = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".channel_product_mapping WHERE channel = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".channel_credentials WHERE channel_code = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = ANY($1)", "{"+strings.Join(skus, ",")+"}")
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' IN (SELECT id FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel' = $1)", ch)
		db.DB.Exec("DELETE FROM "+schema+".channel_order_mapping WHERE channel = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE (doctype = 'SalesOrder' AND data->>'channel' = $1) OR (doctype = 'MarketplaceSettlementLine' AND data->>'channel' = $1)"+
			" OR (doctype = 'ChannelSyncRun' AND data->>'channel' = $1) OR id IN ($1, $2, $3, $4, $5) OR (doctype = 'Customer' AND data->>'phone' = '9811199901')", ch, store, design, skus[0], skus[1])
	}
	cleanup()
	defer cleanup()
	doc := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
			t.Fatal(err)
		}
	}
	doc(store, "Location", map[string]interface{}{"code": store, "name": "S999 Web Store", "type": "Store", "status": "Active", "state": "Karnataka"})
	doc(ch, "Channel", map[string]interface{}{"code": ch, "name": "Web shop", "platform": "Shopify", "location_code": store, "inventory_buffer": 1, "sync_interval_minutes": 15})
	doc(design, "ProductFamily", map[string]interface{}{"code": design, "name": "S999 Kurta", "hsn_code": "6109", "brand": "Acme"})
	for i, sku := range skus {
		colour := []string{"Red", "Blue"}[i]
		doc(sku, "Item", map[string]interface{}{"code": sku, "name": "S999 Kurta " + colour + " M", "family": design, "variant_option_values": "Colour:" + colour + ";Size:M",
			"sale_price": 999, "mrp": 1299, "barcode": "89099900000" + string(rune('1'+i)), "hsn_code": "6109", "gst_rate": 5, "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)", sku, store); err != nil {
			t.Fatal(err)
		}
	}
	if err := SaveChannelCredential(tenantID, ch, map[string]string{"shop_domain": cred["shop_domain"], "access_token": cred["access_token"], "webhook_secret": "whsec"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadShopifyChannel(tenantID, "S999-NOPE"); err == nil {
		t.Errorf("an unknown channel is refused")
	}
	if err := SaveShopifySettings(tenantID, ch, ShopifySettings{Locations: map[string]string{"gid://shopify/Location/1": "NO-SUCH-STORE"}}, "tester"); err == nil {
		t.Errorf("a location mapped to a store that does not exist is refused")
	}
	if err := SaveShopifySettings(tenantID, ch, ShopifySettings{Locations: map[string]string{"gid://shopify/Location/1": store}, SyncPrices: true, SyncStock: true, SyncOrders: true}, "tester"); err != nil {
		t.Fatal(err)
	}

	// 99.3: the design as one product with two variants; again, the same product.
	res, err := PublishToShopify(ctx, tenantID, ch, []string{design}, "tester")
	if err != nil || res.Products != 1 || res.Variants != 2 || len(res.Errors) != 0 {
		t.Fatalf("publish: %+v %v", res, err)
	}
	in := f.seen["productSet"][len(f.seen["productSet"])-1]["input"].(map[string]interface{})
	if in["vendor"] != "Acme" || len(in["productOptions"].([]interface{})) != 2 || in["id"] != nil {
		t.Errorf("first publish: a new product with Colour and Size and the brand: %v", in)
	}
	links, _ := ListShopifyLinks(tenantID, ch)
	if len(links) != 2 || links[0].ProductGID == "" || links[0].InventoryItemID == "" || links[0].Design != design {
		t.Fatalf("links: %+v", links)
	}
	if _, err := PublishToShopify(ctx, tenantID, ch, []string{design}, "tester"); err != nil {
		t.Fatal(err)
	}
	in = f.seen["productSet"][len(f.seen["productSet"])-1]["input"].(map[string]interface{})
	if in["id"] != links[0].ProductGID {
		t.Errorf("publishing again updates the linked product, never a duplicate: %v", in["id"])
	}

	// 99.3.3: prices go only when they change.
	if n, err := PushShopifyPrices(ctx, tenantID, ch, false); err != nil || n != 0 {
		t.Errorf("nothing changed, nothing sent: %d %v", n, err)
	}
	db.DB.Exec("UPDATE "+schema+".documents SET data = data || '{\"sale_price\": 899}'::jsonb WHERE id = $1", skus[0])
	if n, err := PushShopifyPrices(ctx, tenantID, ch, false); err != nil || n != 1 {
		t.Errorf("one price changed, one sent: %d %v", n, err)
	}

	// 99.4: stock per location less the buffer; only changes after the full send.
	if n, err := PushShopifyStock(ctx, tenantID, ch, true, nil); err != nil || n != 2 {
		t.Fatalf("full stock: %d %v", n, err)
	}
	q := f.seen["stock"][len(f.seen["stock"])-1]["input"].(map[string]interface{})["quantities"].([]interface{})[0].(map[string]interface{})
	if q["quantity"].(float64) != 9 || q["locationId"] != "gid://shopify/Location/1" {
		t.Errorf("10 on hand less the buffer of 1, at the mapped location: %v", q)
	}
	if n, _ := PushShopifyStock(ctx, tenantID, ch, false, nil); n != 0 {
		t.Errorf("no change, nothing sent: %d", n)
	}
	db.DB.Exec("UPDATE "+schema+".inventory_availability SET on_hand = 4, available = 4 WHERE sku = $1", skus[1])
	if n, _ := PushShopifyStock(ctx, tenantID, ch, false, nil); n != 1 {
		t.Errorf("one item's stock changed, one sent: %d", n)
	}
	if mm, err := ShopifyStockMismatch(ctx, tenantID, ch); err != nil || len(mm) == 0 {
		t.Errorf("the fake shows 7 where the ERP sends 9 / 3: %v %v", mm, err)
	}

	// 99.5.1: the whole order, imported once, the customer made by phone.
	o := ShopifyOrder{ID: "99001", GID: "gid://shopify/Order/99001", Number: "#1099", Email: "s999@example.com", Phone: "+919811199901", FirstName: "Asha", LastName: "Rao",
		UpdatedAt: time.Now().Add(-time.Hour), Financial: "paid", Gateways: []string{"razorpay"}, Shipping: 50, Discount: 100,
		Tax:   []ShopifyTaxLine{{Title: "CGST 2.5%", Amount: 22.5}, {Title: "SGST 2.5%", Amount: 22.5}},
		Ship:  ShopifyAddress{Name: "Asha Rao", Address1: "1 MG Road", City: "Bengaluru", Province: "Karnataka", Zip: "560001", Country: "India"},
		Lines: []ShopifyOrderLine{{SKU: skus[0], Qty: 2, UnitPrice: 1000, Discount: 100}}}
	orderID, action, err := ImportShopifyOrder(ctx, tenantID, ch, o)
	if err != nil || action != "imported" {
		t.Fatalf("import: %s %s %v", orderID, action, err)
	}
	var raw string
	_ = db.DB.QueryRow("SELECT data::text FROM "+schema+".documents WHERE id = $1", orderID).Scan(&raw)
	var od map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &od)
	if od["shopify_order_number"] != "#1099" || od["tax_cgst"].(float64) != 22.5 || od["shipping_charge"].(float64) != 50 || od["customer_id"] == nil || od["place_of_supply_state"] != "Karnataka" {
		t.Errorf("the order's Shopify details: %v", od)
	}
	// Stage 101's cart journey: a known customer's open checkout is noted.
	if r, err := handleShopifyCheckout(tenantID, []byte(`{"token": "cart-999", "phone": "+919811199901", "abandoned_checkout_url": "https://shop/cart"}`)); err != nil || r != "cart noted" {
		t.Errorf("checkout for a known customer: %s %v", r, err)
	}
	if r, _ := handleShopifyCheckout(tenantID, []byte(`{"token": "cart-x", "phone": "+919800000999"}`)); r != "ignored (no known customer)" {
		t.Errorf("an unknown buyer's cart is ignored: %s", r)
	}
	again, action, _ := ImportShopifyOrder(ctx, tenantID, ch, o)
	if again != orderID || action != "unchanged" {
		t.Errorf("imported once only: %s %s", again, action)
	}
	// 99.5.2: a later edit in Shopify is applied (or queued if it cannot be).
	o.UpdatedAt = time.Now()
	o.Lines[0].Qty = 3
	if _, action, err := ImportShopifyOrder(ctx, tenantID, ch, o); err != nil || action == "unchanged" {
		t.Errorf("an edit is applied or queued: %s %v", action, err)
	}

	// 99.6.2: a refund before shipping waits for a person.
	if r, err := handleShopifyRefund(tenantID, ch, []byte(`{"id": 5501, "order_id": 99001, "refund_line_items": [{"quantity": 1, "line_item": {"sku": "`+skus[0]+`"}}]}`)); err != nil || !strings.Contains(r, "Channel changes") {
		t.Errorf("refund before shipping: %s %v", r, err)
	}
	if rows, err := OMSExceptionQueue(tenantID, "channel"); err != nil || len(rows) == 0 {
		t.Errorf("the Channel changes queue holds it: %v %v", rows, err)
	}
	if err := ClearChannelChange(tenantID, orderID, "tester"); err != nil {
		t.Errorf("mark done: %v", err)
	}

	// 99.6: the status job sends to Shopify when side effects are on.
	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "1")
	items, _ := json.Marshal([]ConnectorStatusLine{{OrderItemID: "KURTA-RED-M", Quantity: 1}}) // the fake's fulfilment line
	if _, err := pushShopifyStatusJob(ctx, schema, Job{Payload: map[string]interface{}{"tenant_id": tenantID, "channel": ch, "order_id": orderID,
		"channel_order_id": "99001", "status": "Shipped", "carrier": "Delhivery", "tracking": "TRK9", "items": string(items)}}); err != nil {
		t.Fatalf("status job: %v", err)
	}
	if len(f.seen["fulfil"]) == 0 {
		t.Errorf("the shipment reached Shopify")
	}

	// 99.7: the payout's order line comes in as a settlement line.
	if n, err := SyncShopifyPayouts(ctx, tenantID, ch, "tester"); err != nil || n != 1 {
		t.Errorf("payouts: %d %v", n, err)
	}
	var fee string
	_ = db.DB.QueryRow("SELECT data->>'commission' FROM "+schema+".documents WHERE doctype = 'MarketplaceSettlementLine' AND data->>'channel' = $1", ch).Scan(&fee)
	if fee != "30" {
		t.Errorf("Shopify's fee is the commission: %q", fee)
	}
	if ov, err := GetShopifyOverview(tenantID, ch); err != nil || !ov.CredentialsSet || !ov.WebhookSecret || ov.Links != 2 || len(ov.Runs) == 0 {
		t.Errorf("overview: %+v %v", ov, err)
	}
	if out, err := HandleShopifyWebhook(ctx, tenantID, ch, "app/uninstalled", []byte(`{}`)); err != nil || !strings.Contains(out, "removed") {
		t.Errorf("uninstall noted: %s %v", out, err)
	}
}
