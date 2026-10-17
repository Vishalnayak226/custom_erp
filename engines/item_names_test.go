package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 65.3/65.4: the three masters are numbered on the server, an item name
// is named from its two parts, a design takes the item name's HSN / GST, and
// a PO can add a value - or a new attribute - to the PIM attributes.
func TestItemNamesAndInlineAttributes(t *testing.T) {
	db.InitDB(testConnStr())
	const tenant = "default"
	const cat, ptype, item, attr = "CAT-T65", "PT-T65", "ITEMNAME-T65", "ATTR_T65"
	clean := func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id IN ($1, $2, $3, $4) OR (doctype = 'ProductAttributeDef' AND data->>'label' = 'Stone T65')`, cat, ptype, item, attr)
	}
	clean()
	t.Cleanup(clean)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES
		($1, 'Category', '{"code":"CAT-T65","name":"Necklaces","status":"Active"}', 'Active', 'system'),
		($2, 'ProductType', '{"code":"PT-T65","name":"Antique","status":"Active"}', 'Active', 'system'),
		($3, 'ItemName', '{"code":"ITEMNAME-T65","name":"Necklaces Antique","category":"CAT-T65","product_type":"PT-T65","hsn_code":"7117","gst_rate":3}', 'Active', 'system'),
		($4, 'ProductAttributeDef', '{"code":"ATTR_T65","label":"Colour T65","value_type":"Select","value_options":"Gold,Silver","status":"Active"}', 'Active', 'system')`,
		cat, ptype, item, attr); err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("server numbers a new master; a typed code is kept", func(t *testing.T) {
		blank := map[string]interface{}{"name": "Rings"}
		if err := PrepareMasterNumber(tenant, "HQ", "Category", true, blank); err != nil {
			t.Fatal(err)
		}
		if code := strField(blank, "code"); !strings.HasPrefix(code, "CAT/") || blank["id"] != code {
			t.Errorf("new Category code = %q id = %v, want CAT/... with id = code", code, blank["id"])
		}
		typed := map[string]interface{}{"code": "MY-CAT"}
		if err := PrepareMasterNumber(tenant, "HQ", "Category", true, typed); err != nil || typed["code"] != "MY-CAT" {
			t.Errorf("typed code changed: %v (%v)", typed["code"], err)
		}
		other := map[string]interface{}{}
		if err := PrepareMasterNumber(tenant, "HQ", "Brand", true, other); err != nil || other["code"] != nil {
			t.Errorf("Brand is not server-numbered, got %v (%v)", other["code"], err)
		}
	})

	t.Run("item name is named from its parts", func(t *testing.T) {
		p := map[string]interface{}{"category": cat, "product_type": ptype}
		if err := PrepareItemNameFields(tenant, "ItemName", p); err != nil {
			t.Fatal(err)
		}
		if p["name"] != "Necklaces Antique" {
			t.Errorf("name = %v, want Necklaces Antique", p["name"])
		}
		named := map[string]interface{}{"category": cat, "product_type": ptype, "name": "Temple Necklaces"}
		PrepareItemNameFields(tenant, "ItemName", named)
		if named["name"] != "Temple Necklaces" {
			t.Errorf("a typed name was replaced: %v", named["name"])
		}
	})

	t.Run("a design takes its item name's HSN and GST", func(t *testing.T) {
		p := map[string]interface{}{"item_name": item, "name": "Design A"}
		if err := PrepareItemNameFields(tenant, "ProductFamily", p); err != nil {
			t.Fatal(err)
		}
		if p["hsn_code"] != "7117" || numFromInterface(p["gst_rate"]) != 3 {
			t.Errorf("hsn = %v gst = %v, want 7117 / 3", p["hsn_code"], p["gst_rate"])
		}
		own := map[string]interface{}{"item_name": item, "hsn_code": "7113", "gst_rate": 5}
		PrepareItemNameFields(tenant, "ProductFamily", own)
		if own["hsn_code"] != "7113" || numFromInterface(own["gst_rate"]) != 5 {
			t.Errorf("the design's own HSN / GST were replaced: %v / %v", own["hsn_code"], own["gst_rate"])
		}
	})

	t.Run("a PO adds a value, and a new attribute", func(t *testing.T) {
		a, err := AddDesignAttributeValue(tenant, "system", attr, "", "Rose Gold")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(a.Options, ",") != "Gold,Silver,Rose Gold" {
			t.Errorf("options = %v", a.Options)
		}
		again, _ := AddDesignAttributeValue(tenant, "system", attr, "", "rose gold")
		if len(again.Options) != 3 {
			t.Errorf("a value already there (other case) was added twice: %v", again.Options)
		}
		var stored string
		db.DB.QueryRow(`SELECT data->>'value_options' FROM tenant_default.documents WHERE id = $1`, attr).Scan(&stored)
		if stored != "Gold,Silver,Rose Gold" {
			t.Errorf("stored value_options = %q", stored)
		}
		n, err := AddDesignAttributeValue(tenant, "system", "", "Stone T65", "Ruby")
		if err != nil {
			t.Fatal(err)
		}
		if n.Code != "STONE_T65" || n.ValueType != "Select" || len(n.Options) != 1 {
			t.Errorf("new attribute = %+v", n)
		}
		if _, err := AddDesignAttributeValue(tenant, "system", attr, "", "a,b"); err == nil {
			t.Error("a value with a comma was accepted")
		}
	})
}
