package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Typed product relations go to Shopify as the two recommendation metafields,
// only for related items already on the shop, in Order order, and only when
// the PIM content setting is on.
func TestShopifyRelationMetafields(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	f, cred := withFakeShopify(t)
	ctx := context.Background()
	const ch, store, design = "SHOP-T168H", "S168HR-STORE", "S168HR-DES"
	const sku = "S168HR-RED-M"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".shopify_links WHERE channel = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".channel_credentials WHERE channel_code = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id LIKE 'S168HR-%' OR id = $1", ch)
		_ = SetSetting(tenantID, shopifyPIMContentSetting, "false", "tester")
	}
	cleanup()
	defer cleanup()
	doc := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatal(err)
		}
	}
	doc(store, "Location", "Active", map[string]interface{}{"code": store, "name": "Store", "type": "Store", "status": "Active", "state": "Karnataka"})
	doc(ch, "Channel", "Active", map[string]interface{}{"code": ch, "name": "Web", "platform": "Shopify", "location_code": store, "default_locale": "en"})
	doc(design, "ProductFamily", "Active", map[string]interface{}{"code": design, "name": "Plain Kurta", "hsn_code": "6109", "brand": "Acme"})
	doc(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "Kurta Red", "family": design, "variant_option_values": "Colour:Red;Size:M",
		"sale_price": 999, "mrp": 1299, "barcode": "891465000099", "hsn_code": "6109", "gst_rate": 5, "status": "Active", "category": "Kurtas"})
	rel := func(n int, typ, other string, pos int) {
		doc("S168HR-REL-"+string(rune('0'+n)), "ProductRelation", "Active", map[string]interface{}{"item": design, "relation_type": typ, "related_item": other, "position": pos, "status": "Active"})
	}
	// on the shop: A, B, C. Not on the shop: NOPE.
	rel(1, "Upsell", "S168HR-B", 2)
	rel(2, "Similar", "S168HR-A", 1)
	rel(3, "Cross-sell", "S168HR-C", 1)
	rel(4, "Alternative", "S168HR-NOPE", 0)
	for _, o := range []string{"A", "B", "C"} {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".shopify_links (channel, sku, design, product_gid) VALUES ($1, $2, $2, $3)", ch, "S168HR-"+o, "gid://shopify/Product/"+o); err != nil {
			t.Fatal(err)
		}
	}
	if err := SaveChannelCredential(tenantID, ch, map[string]string{"shop_domain": cred["shop_domain"], "access_token": cred["access_token"], "webhook_secret": "whsec"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveShopifySettings(tenantID, ch, ShopifySettings{Locations: map[string]string{"gid://shopify/Location/1": store}}, "tester"); err != nil {
		t.Fatal(err)
	}
	relMF := func() map[string]string {
		in := f.seen["productSet"][len(f.seen["productSet"])-1]["input"].(map[string]interface{})
		out := map[string]string{}
		mfs, _ := in["metafields"].([]interface{})
		for _, mf := range mfs {
			m := mf.(map[string]interface{})
			if m["namespace"] == shopifyRecommendationNamespace {
				if m["type"] != "list.product_reference" {
					t.Errorf("type: %v", m["type"])
				}
				out[m["key"].(string)] = m["value"].(string)
			}
		}
		return out
	}

	// Setting off: nothing is sent.
	if res, err := PublishToShopify(ctx, tenantID, ch, []string{design}, "tester"); err != nil || len(res.Errors) != 0 {
		t.Fatalf("publish off: %+v %v", res, err)
	}
	if got := relMF(); len(got) != 0 {
		t.Errorf("setting off: no relation metafields, got %v", got)
	}

	if err := SetSetting(tenantID, shopifyPIMContentSetting, "true", "tester"); err != nil {
		t.Fatal(err)
	}
	if res, err := PublishToShopify(ctx, tenantID, ch, []string{design}, "tester"); err != nil || len(res.Errors) != 0 {
		t.Fatalf("publish on: %+v %v", res, err)
	}
	got := relMF()
	if got["related_products"] != `["gid://shopify/Product/A","gid://shopify/Product/B"]` {
		t.Errorf("related: Similar then Upsell by order, the unpublished one left out: %v", got)
	}
	if got["complementary_products"] != `["gid://shopify/Product/C"]` {
		t.Errorf("complementary: %v", got)
	}
	if n := len(f.seen["mfDelete"]); n != 0 {
		t.Errorf("both lists sent: nothing to clear, got %d delete calls", n)
	}
	// 321.1: the lists the publish does not send are deleted on the shop.
	cleared := func() []string {
		t.Helper()
		if len(f.seen["mfDelete"]) == 0 {
			t.Fatal("no metafieldsDelete call")
		}
		var keys []string
		for _, raw := range f.seen["mfDelete"][len(f.seen["mfDelete"])-1]["metafields"].([]interface{}) {
			m := raw.(map[string]interface{})
			if m["ownerId"] != "gid://shopify/Product/300" || m["namespace"] != shopifyRecommendationNamespace {
				t.Errorf("clears this product's recommendation list only: %v", m)
			}
			keys = append(keys, m["key"].(string))
		}
		return keys
	}

	// The Cross-sell deleted: related still sent, complementary cleared.
	db.DB.Exec("UPDATE " + schema + ".documents SET deleted_at = NOW() WHERE id = 'S168HR-REL-3'")
	if _, err := PublishToShopify(ctx, tenantID, ch, []string{design}, "tester"); err != nil {
		t.Fatal(err)
	}
	if got := relMF(); got["related_products"] == "" || got["complementary_products"] != "" {
		t.Errorf("one list left: %v", got)
	}
	if keys := cleared(); strings.Join(keys, ",") != "complementary_products" {
		t.Errorf("cleared %v, want complementary_products", keys)
	}

	// No relations: no metafield, both lists cleared.
	db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ProductRelation' AND id LIKE 'S168HR-%'")
	if _, err := PublishToShopify(ctx, tenantID, ch, []string{design}, "tester"); err != nil {
		t.Fatal(err)
	}
	if got := relMF(); len(got) != 0 || strings.Contains(strings.Join(mapVals(got), ""), "gid") {
		t.Errorf("no relations: no metafield, got %v", got)
	}
	if keys := cleared(); strings.Join(keys, ",") != "related_products,complementary_products" {
		t.Errorf("cleared %v, want both lists", keys)
	}

	// Setting off: the ERP does not manage the lists, so none is cleared.
	before := len(f.seen["mfDelete"])
	if err := SetSetting(tenantID, shopifyPIMContentSetting, "false", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishToShopify(ctx, tenantID, ch, []string{design}, "tester"); err != nil {
		t.Fatal(err)
	}
	if n := len(f.seen["mfDelete"]); n != before {
		t.Errorf("setting off: no delete call, got %d more", n-before)
	}
}

func mapVals(m map[string]string) []string {
	var v []string
	for _, x := range m {
		v = append(v, x)
	}
	return v
}
