package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// TestFindRelatedProducts (Stage 36.7.3) exercises the "same family + most
// shared attribute values" ranking directly against real rows, rather than
// mocking the query: ITEM-B shares both of ITEM-A's attributes and must
// rank first, ITEM-C shares one and ranks second, and two decoys prove the
// two things that must NOT count - a same-family item with zero overlap,
// and a different-family item that would otherwise be the top match.
func TestFindRelatedProducts(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}

	itemIDs := []string{"PIM-REL-A", "PIM-REL-B", "PIM-REL-C", "PIM-REL-D", "PIM-REL-E"}
	attrIDs := []string{
		"PIM-REL-A::color", "PIM-REL-A::material",
		"PIM-REL-B::color", "PIM-REL-B::material",
		"PIM-REL-C::color",
		"PIM-REL-D::color",
		"PIM-REL-E::material",
	}
	cleanup := func() {
		for _, id := range attrIDs {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'ProductAttributeValue' AND id = $1", id)
		}
		for _, id := range itemIDs {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Item' AND id = $1", id)
		}
	}
	cleanup()
	defer cleanup()

	insertItem := func(id, family string) {
		t.Helper()
		encoded, _ := json.Marshal(map[string]interface{}{"code": id, "name": id, "family": family})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", id, encoded); err != nil {
			t.Fatalf("insert item %s: %v", id, err)
		}
	}
	insertAttr := func(id, item, attribute, value string) {
		t.Helper()
		encoded, _ := json.Marshal(map[string]interface{}{
			"item": item, "attribute": attribute, "value": value, "locale": "", "channel": "",
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ProductAttributeValue', $2, 'Active', 'system')", id, encoded); err != nil {
			t.Fatalf("insert attribute value %s: %v", id, err)
		}
	}

	insertItem("PIM-REL-A", "Apparel")
	insertItem("PIM-REL-B", "Apparel") // shares color + material with A
	insertItem("PIM-REL-C", "Apparel") // shares color only with A
	insertItem("PIM-REL-D", "Footwear") // same color as A, different family - must be excluded
	insertItem("PIM-REL-E", "Apparel") // same family, zero attribute overlap - must be excluded

	insertAttr("PIM-REL-A::color", "PIM-REL-A", "color", "Red")
	insertAttr("PIM-REL-A::material", "PIM-REL-A", "material", "Cotton")
	insertAttr("PIM-REL-B::color", "PIM-REL-B", "color", "Red")
	insertAttr("PIM-REL-B::material", "PIM-REL-B", "material", "Cotton")
	insertAttr("PIM-REL-C::color", "PIM-REL-C", "color", "Red")
	insertAttr("PIM-REL-D::color", "PIM-REL-D", "color", "Red")
	insertAttr("PIM-REL-E::material", "PIM-REL-E", "material", "Silk")

	related, err := FindRelatedProducts("default", "PIM-REL-A", 10)
	if err != nil {
		t.Fatalf("FindRelatedProducts: %v", err)
	}
	if len(related) != 2 {
		t.Fatalf("related = %#v, want exactly 2 (B and C, D/E excluded)", related)
	}
	if related[0].ItemCode != "PIM-REL-B" || related[0].SharedAttributes != 2 {
		t.Errorf("rank 1 = %#v, want PIM-REL-B with 2 shared attributes", related[0])
	}
	if related[1].ItemCode != "PIM-REL-C" || related[1].SharedAttributes != 1 {
		t.Errorf("rank 2 = %#v, want PIM-REL-C with 1 shared attribute", related[1])
	}

	// An item with no family has nothing to compare against - empty result,
	// not an error.
	insertItem("PIM-REL-NOFAMILY", "")
	defer func() { _, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = 'PIM-REL-NOFAMILY'") }()
	noFamily, err := FindRelatedProducts("default", "PIM-REL-NOFAMILY", 10)
	if err != nil {
		t.Fatalf("FindRelatedProducts (no family): %v", err)
	}
	if len(noFamily) != 0 {
		t.Errorf("no-family item returned %d related products, want 0", len(noFamily))
	}
}
