package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 146.5.13: fill-rate report, brand vocabulary report, SKU paste search.

func TestStage1465131FillRateAndBrandReports(t *testing.T) {
	schema, _, cleanup := pimTestFixture(t, "S1465131")
	t.Cleanup(cleanup)
	pimInsertDoc(t, schema, "S1465131-A", "Item", "Active", map[string]interface{}{"code": "S1465131-A", "name": "a", "brand": "Zorbo"})
	pimInsertDoc(t, schema, "S1465131-B", "Item", "Active", map[string]interface{}{"code": "S1465131-B", "name": "b", "brand": "zorbo "})
	pimInsertDoc(t, schema, "S1465131-DEF", "ProductAttributeDef", "Active", map[string]interface{}{"code": "S1465131-DEF", "label": "Colour", "value_type": "Text"})
	pimInsertDoc(t, schema, "S1465131-V", "ProductAttributeValue", "Active", map[string]interface{}{"item": "S1465131-A", "attribute": "S1465131-DEF", "value": "red"})
	pimInsertDoc(t, schema, "S1465131-BR", "Brand", "Active", map[string]interface{}{"code": "ZORBO", "name": "Zorbo", "status": "Active"})

	_, rows, _, err := RunReport("default", "pim-attribute-fill-rate", RoleSuperAdmin, "system", nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r["attribute"] == "S1465131-DEF" {
			found = true
			if r["filled"] != 1 {
				t.Errorf("filled = %v, want 1", r["filled"])
			}
		}
	}
	if !found {
		t.Fatal("attribute missing from fill-rate report")
	}

	_, rows, _, err = RunReport("default", "pim-brand-vocabulary", RoleSuperAdmin, "system", nil)
	if err != nil {
		t.Fatal(err)
	}
	// "Zorbo" matches the Brand master; "zorbo " is spelt differently.
	var spelt int
	for _, r := range rows {
		if r["brand_value"] == "zorbo" {
			spelt++
			if r["suggestion"] != "Zorbo" {
				t.Errorf("suggestion = %v", r["suggestion"])
			}
		}
		if r["brand_value"] == "Zorbo" {
			t.Errorf("a brand that matches the master was listed")
		}
	}
	if spelt != 1 {
		t.Fatalf("variant rows = %d, want 1: %v", spelt, rows)
	}
}

func TestStage1465132SKUPasteSplit(t *testing.T) {
	got := ParseSKUPaste("a1, b2\nc3\t a1;;d4  e5")
	want := []string{"a1", "b2", "c3", "d4", "e5"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v", got)
		}
	}
}

func TestStage1465133LiveOnLinks(t *testing.T) {
	schema, _, cleanup := pimTestFixture(t, "S1465133")
	t.Cleanup(cleanup)
	pimInsertDoc(t, schema, "S1465133-CH", "Channel", "Active", map[string]interface{}{"code": "S1465133-CH", "name": "Shop", "channel_type": "Website", "enabled": "Yes", "item_url_template": "https://shop.example/p/{item_code}"})
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.pim_publish_log WHERE job_id = 91460513`)
	})
	if _, err := db.DB.Exec(`INSERT INTO ` + schema + `.pim_publish_log (job_id, item_code, channel_code, status, external_id) VALUES (91460513, 'S1465133-ITEM', 'S1465133-CH', 'Published', 'X9')`); err != nil {
		t.Fatal(err)
	}
	links, err := ItemLiveOn("default", "S1465133-ITEM")
	if err != nil || len(links) != 1 {
		t.Fatalf("links %v err %v", links, err)
	}
	if links[0]["url"] != "https://shop.example/p/S1465133-ITEM" || links[0]["channel"] != "S1465133-CH" {
		t.Errorf("link = %v", links[0])
	}
}

func TestStage1465134AmazonProductTypeToken(t *testing.T) {
	schema, _, cleanup := pimTestFixture(t, "S1465134")
	t.Cleanup(cleanup)
	pimInsertDoc(t, schema, "S1465134-M", "ChannelCategoryMap", "Active", map[string]interface{}{"code": "S1465134-M", "channel": "AMZ", "erp_category": "Shoes", "channel_category": "Footwear", "product_type": "SHOES"})
	_, _ = db.DB.Exec(`UPDATE ` + schema + `.documents SET data = data || '{"category":"Shoes"}' WHERE doctype='Item' AND id='S1465134-ITEM'`)
	v, ok := channelExportExtras("default", "AMZ")("S1465134-ITEM", "product_type")
	if !ok || v != "SHOES" {
		t.Fatalf("product_type = %q ok=%v", v, ok)
	}
	if !pimExportChannelFields["product_type"] {
		t.Error("product_type is not an export field")
	}
}
