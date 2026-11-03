package engines

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"custom_erp/db"
)

// Stage 146.5.1: Shopify gets the approved PIM content, behind a setting.
func TestStage14651ShopifyPIMContent(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	var ready bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'shopify_links' AND column_name = 'images_hash')`, schema).Scan(&ready)
	if !ready {
		t.Skip("needs migrations_stage146_5_1_shopify_images_hash.sql - run erp-server -migrate on this database")
	}
	f, cred := withFakeShopify(t)
	// extra operations the shared fake does not know
	var mu sync.Mutex
	extra := map[string][]map[string]interface{}{}
	count := func(op string) int {
		mu.Lock()
		defer mu.Unlock()
		return len(extra[op])
	}
	base := f.handler(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string                 `json:"query"`
			Variables map[string]interface{} `json:"variables"`
		}
		_ = json.Unmarshal(body, &req)
		reply := func(op, data string) {
			mu.Lock()
			extra[op] = append(extra[op], req.Variables)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"data":` + data + `}`))
		}
		switch {
		case strings.Contains(req.Query, "metafieldDefinitionCreate"):
			reply("def", `{"metafieldDefinitionCreate":{"createdDefinition":{"id":"x"},"userErrors":[]}}`)
		case strings.Contains(req.Query, "productDeleteMedia"):
			reply("delmedia", `{"productDeleteMedia":{"deletedMediaIds":["m1"],"mediaUserErrors":[]}}`)
		case strings.Contains(req.Query, "media(first"):
			reply("media", `{"product":{"media":{"edges":[{"node":{"id":"gid://shopify/MediaImage/1"}}]}}}`)
		default:
			r.Body = io.NopCloser(bytes.NewReader(body))
			base(w, r)
		}
	}))
	old := shopifyGraphQLURL
	shopifyGraphQLURL = func(string) string { return server.URL }
	t.Cleanup(func() { shopifyGraphQLURL = old; server.Close() })

	ctx := context.Background()
	const ch, store, design = "SHOP-T1465", "S1465-STORE", "S1465-DES"
	skus := []string{"S1465-RED-M", "S1465-BLUE-M"}
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".shopify_links WHERE channel = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".channel_credentials WHERE channel_code = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id LIKE 'S1465-%' OR id = $1", ch)
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
	for i, sku := range skus {
		colour := []string{"Red", "Blue"}[i]
		doc(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "Kurta " + colour, "family": design, "variant_option_values": "Colour:" + colour + ";Size:M",
			"sale_price": 999, "mrp": 1299, "barcode": "89146500000" + string(rune('1'+i)), "hsn_code": "6109", "gst_rate": 5, "status": "Active", "category": "Kurtas"})
	}
	doc("S1465-AD-FAB", "ProductAttributeDef", "Active", map[string]interface{}{"code": "fabric", "name": "Fabric", "used_for": "Product information"})
	doc("S1465-AD-FIT", "ProductAttributeDef", "Active", map[string]interface{}{"code": "fit_note", "name": "Fit", "used_for": "Variant"})
	doc("S1465-AV-1", "ProductAttributeValue", "Active", map[string]interface{}{"item": design, "attribute": "fabric", "value": "Cotton"})
	doc("S1465-AV-2", "ProductAttributeValue", "Active", map[string]interface{}{"item": skus[0], "attribute": "fit_note", "value": "Runs small"})
	doc("S1465-PC-1", "ProductContent", "Approved", map[string]interface{}{"product_id": design, "language": "en", "title": "Approved Kurta Title",
		"short_desc": "<p>Soft &amp; light</p>", "long_desc": "<p>Long <b>cotton</b> kurta</p>", "seo_title": "Cotton Kurta | Acme", "tags": "cotton, kurta, Cotton"})
	doc("S1465-PC-2", "ProductContent", "Draft", map[string]interface{}{"product_id": skus[1], "language": "en", "title": "DRAFT TITLE"})
	if err := SaveChannelCredential(tenantID, ch, map[string]string{"shop_domain": cred["shop_domain"], "access_token": cred["access_token"], "webhook_secret": "whsec"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveShopifySettings(tenantID, ch, ShopifySettings{Locations: map[string]string{"gid://shopify/Location/1": store}}, "tester"); err != nil {
		t.Fatal(err)
	}
	lastInput := func() map[string]interface{} {
		return f.seen["productSet"][len(f.seen["productSet"])-1]["input"].(map[string]interface{})
	}

	// Setting off: today's payload exactly.
	if res, err := PublishToShopify(ctx, tenantID, ch, []string{design}, "tester"); err != nil || len(res.Errors) != 0 {
		t.Fatalf("publish off: %+v %v", res, err)
	}
	in := lastInput()
	if in["title"] != "Plain Kurta" || in["tags"] != nil || in["seo"] != nil {
		t.Errorf("off: the payload is unchanged: title=%v tags=%v seo=%v", in["title"], in["tags"], in["seo"])
	}
	for _, mf := range in["metafields"].([]interface{}) {
		if mf.(map[string]interface{})["key"] != "hsn_code" {
			t.Errorf("off: only the HSN metafield goes: %v", mf)
		}
	}
	if count("def") != 0 {
		t.Errorf("off: no definitions are created")
	}

	// Setting on: approved content, tags, SEO, attribute metafields.
	if err := SetSetting(tenantID, shopifyPIMContentSetting, "true", "tester"); err != nil {
		t.Fatal(err)
	}
	if res, err := PublishToShopify(ctx, tenantID, ch, []string{design}, "tester"); err != nil || len(res.Errors) != 0 {
		t.Fatalf("publish on: %+v %v", res, err)
	}
	in = lastInput()
	if in["title"] != "Approved Kurta Title" {
		t.Errorf("on: the approved title: %v", in["title"])
	}
	if d, _ := in["descriptionHtml"].(string); !strings.Contains(d, "cotton") {
		t.Errorf("on: the approved description: %v", in["descriptionHtml"])
	}
	if tags, _ := in["tags"].([]interface{}); len(tags) != 2 {
		t.Errorf("on: tags are split and de-duplicated: %v", in["tags"])
	}
	seo, _ := in["seo"].(map[string]interface{})
	if seo["title"] != "Cotton Kurta | Acme" || seo["description"] != "Soft & light" {
		t.Errorf("on: SEO title and description: %v", seo)
	}
	if in["productType"] != "Kurtas" {
		t.Errorf("on: the product type: %v", in["productType"])
	}
	keys := map[string]string{}
	for _, mf := range in["metafields"].([]interface{}) {
		m := mf.(map[string]interface{})
		keys[m["namespace"].(string)+"."+m["key"].(string)] = m["value"].(string)
	}
	if keys["custom.fabric"] != "Cotton" {
		t.Errorf("on: the design value is a product metafield: %v", keys)
	}
	foundVariant := false
	for _, raw := range in["variants"].([]interface{}) {
		v := raw.(map[string]interface{})
		if v["sku"] == skus[0] {
			mfs, _ := v["metafields"].([]interface{})
			for _, mf := range mfs {
				m := mf.(map[string]interface{})
				if m["key"] == "fit_note" && m["value"] == "Runs small" {
					foundVariant = true
				}
			}
		}
		if v["sku"] == skus[1] {
			if mfs, _ := v["metafields"].([]interface{}); len(mfs) != 0 {
				t.Errorf("on: a combination with no value sends no metafield: %v", mfs)
			}
		}
	}
	if !foundVariant {
		t.Errorf("on: the combination value is a variant metafield")
	}
	if count("def") != 2 {
		t.Errorf("on: one product and one variant definition: %d", count("def"))
	}
	// Definitions once: a second publish creates none.
	if _, err := PublishToShopify(ctx, tenantID, ch, []string{design}, "tester"); err != nil {
		t.Fatal(err)
	}
	if count("def") != 2 {
		t.Errorf("definitions are created on first publish only: %d", count("def"))
	}

	// Pictures: the fingerprint is stored with the links; unchanged is not re-sent;
	// changed removes the old pictures.
	var h string
	db.DB.QueryRow("SELECT COALESCE(MAX(images_hash), '') FROM "+schema+".shopify_links WHERE channel = $1", ch).Scan(&h)
	if h == "" {
		t.Fatalf("the picture fingerprint is stored with the links")
	}
	before := count("delmedia")
	if _, err := PublishToShopify(ctx, tenantID, ch, []string{design}, "tester"); err != nil {
		t.Fatal(err)
	}
	if count("delmedia") != before {
		t.Errorf("unchanged pictures are not re-sent")
	}
	db.DB.Exec("UPDATE "+schema+".shopify_links SET images_hash = 'stale' WHERE channel = $1", ch)
	if _, err := PublishToShopify(ctx, tenantID, ch, []string{design}, "tester"); err != nil {
		t.Fatal(err)
	}
	if count("delmedia") != before+1 {
		t.Errorf("changed pictures: the old ones are removed first: %d", count("delmedia"))
	}
	db.DB.QueryRow("SELECT COALESCE(MAX(images_hash), '') FROM "+schema+".shopify_links WHERE channel = $1", ch).Scan(&h)
	if h == "stale" || h == "" {
		t.Errorf("the new fingerprint is stored: %q", h)
	}

	// Unapproved content is never sent.
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Draft' WHERE id = 'S1465-PC-1'")
	if _, err := PublishToShopify(ctx, tenantID, ch, []string{design}, "tester"); err != nil {
		t.Fatal(err)
	}
	in = lastInput()
	if tags, _ := in["tags"].([]interface{}); in["title"] != "Plain Kurta" || len(tags) != 0 {
		t.Errorf("unapproved content is not sent: title=%v tags=%v", in["title"], in["tags"])
	}
}
