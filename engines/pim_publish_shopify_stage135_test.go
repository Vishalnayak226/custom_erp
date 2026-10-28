package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 135.22: PIM's item-level Publish on a Shopify channel goes through
// the Shopify page's publish - the item's design as one linked product, and
// a second item of the same design updates that product instead of making
// another.
func TestStage135PIMPublishToShopifyIsLinked(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	f, cred := withFakeShopify(t)
	const ch, store, design = "S13522-SHOP", "S13522-STORE", "S13522-DES"
	skus := []string{"S13522-RED-M", "S13522-BLUE-M"}
	insert(store, "Location", "Active", map[string]interface{}{"code": store, "name": "S13522 Web Store", "type": "Store", "status": "Active"})
	insert(ch, "Channel", "Active", map[string]interface{}{"code": ch, "name": "Web shop", "platform": "Shopify", "location_code": store})
	insert(design, "ProductFamily", "Active", map[string]interface{}{"code": design, "name": "S13522 Kurta", "hsn_code": "6109", "brand": "Acme"})
	for i, sku := range skus {
		colour := []string{"Red", "Blue"}[i]
		insert(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "S13522 Kurta " + colour + " M", "family": design,
			"variant_option_values": "Colour:" + colour + ";Size:M", "sale_price": 999, "mrp": 1299, "hsn_code": "6109", "gst_rate": 5, "status": "Active"})
	}
	if err := SaveChannelCredential(tenantID, ch, map[string]string{"shop_domain": cred["shop_domain"], "access_token": cred["access_token"]}); err != nil {
		t.Fatal(err)
	}

	publish := func(sku string) (status, externalID, errMsg string) {
		t.Helper()
		var jobID int
		if err := db.DB.QueryRow("INSERT INTO "+schema+".pim_publish_queue (item_code, channel_code, payload_hash, status) VALUES ($1, $2, $3, 'Queued') RETURNING job_id",
			sku, ch, computePublishPayloadHash(tenantID, sku, ch)).Scan(&jobID); err != nil {
			t.Fatalf("queue: %v", err)
		}
		processPublishQueue(schema)
		_ = db.DB.QueryRow("SELECT status, COALESCE(external_id, ''), COALESCE(error_message, '') FROM "+schema+".pim_publish_log WHERE job_id = $1", jobID).Scan(&status, &externalID, &errMsg)
		return
	}

	status, externalID, errMsg := publish(skus[0])
	if status != "Published" {
		t.Fatalf("first publish: %s %s", status, errMsg)
	}
	links, _ := ListShopifyLinks(tenantID, ch)
	if len(links) != 2 || links[0].Design != design || links[0].ProductGID == "" {
		t.Fatalf("the design's two variants are linked on the channel: %+v", links)
	}
	if externalID != links[0].ProductGID {
		t.Errorf("publish log external id %q, want the linked product %q", externalID, links[0].ProductGID)
	}
	if in := f.seen["productSet"][len(f.seen["productSet"])-1]["input"].(map[string]interface{}); in["id"] != nil {
		t.Errorf("first publish creates the product: %v", in["id"])
	}

	if status, _, errMsg := publish(skus[1]); status != "Published" {
		t.Fatalf("second publish: %s %s", status, errMsg)
	}
	if in := f.seen["productSet"][len(f.seen["productSet"])-1]["input"].(map[string]interface{}); in["id"] != links[0].ProductGID {
		t.Errorf("the design's other item updates the linked product, never a duplicate: %v", in["id"])
	}
}
