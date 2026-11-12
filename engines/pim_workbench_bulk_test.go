package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// 149.2: ListWorkbench scores from bulk queries; every entry must match what
// CalculateCompleteness gives the same item, and the profile write-through
// must leave a publish-owned status alone.
func TestPIMWorkbenchBulkMatchesSingleItemScore(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	const family = "WB149-FAM"
	full := func(name string) map[string]interface{} {
		return map[string]interface{}{"name": name, "barcode": "1234567890128", "category": "Rings", "hsn_code": "7113", "gst_rate": 3, "family": family}
	}
	type doc struct {
		id, doctype, status string
		data                map[string]interface{}
	}
	docs := []doc{
		{"WB149-ATTR-POLISH", "ProductAttributeDef", "Active", map[string]interface{}{"code": "WB149-ATTR-POLISH", "label": "Polish"}},
		{"WB149-FA-1", "ProductFamilyAttribute", "Active", map[string]interface{}{"family": family, "attribute": "WB149-ATTR-POLISH", "mandatory": "Yes"}},
		{"WB149-FA-2", "ProductFamilyAttribute", "Active", map[string]interface{}{"family": family, "attribute": "WB149-ATTR-STONE", "mandatory": "Yes"}},
		{"WB149-FA-3", "ProductFamilyAttribute", "Active", map[string]interface{}{"family": family, "attribute": "WB149-ATTR-NOTE", "mandatory": "No"}},
		// Both attributes filled, approved content: 100.
		{"WB149-I1", "Item", "Active", full("Complete")},
		{"WB149-I1::WB149-ATTR-POLISH", "ProductAttributeValue", "Active", map[string]interface{}{"item": "WB149-I1", "attribute": "WB149-ATTR-POLISH", "value": "Gloss"}},
		{"WB149-I1::WB149-ATTR-STONE", "ProductAttributeValue", "Active", map[string]interface{}{"item": "WB149-I1", "attribute": "WB149-ATTR-STONE", "value": "Ruby"}},
		{"WB149-I1::en", "ProductContent", "Approved", map[string]interface{}{"product_id": "WB149-I1", "language": "en"}},
		// A blank "en" override hides the global Stone value; Polish only in "en"; content pending.
		{"WB149-I2", "Item", "Active", full("Override")},
		{"WB149-I2::WB149-ATTR-STONE", "ProductAttributeValue", "Active", map[string]interface{}{"item": "WB149-I2", "attribute": "WB149-ATTR-STONE", "value": "Ruby"}},
		{"WB149-I2::WB149-ATTR-STONE::en::", "ProductAttributeValue", "Active", map[string]interface{}{"item": "WB149-I2", "attribute": "WB149-ATTR-STONE", "value": " ", "locale": "en"}},
		{"WB149-I2::WB149-ATTR-POLISH::en::", "ProductAttributeValue", "Active", map[string]interface{}{"item": "WB149-I2", "attribute": "WB149-ATTR-POLISH", "value": "Matte", "locale": "en"}},
		{"WB149-I2::en", "ProductContent", "Pending Approval", map[string]interface{}{"product_id": "WB149-I2", "language": "en"}},
		// Published stays Published, whatever the score.
		{"WB149-I3", "Item", "Active", full("Published")},
		{"WB149-I3::profile", "PIMProductProfile", "Published", map[string]interface{}{"product_id": "WB149-I3", "enrichment_status": "Published"}},
	}
	cleanup := func() {
		for _, d := range docs {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", d.id)
		}
		for _, id := range []string{"WB149-I1::profile", "WB149-I2::profile"} {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	cleanup()
	defer cleanup()
	for _, d := range docs {
		encoded, err := json.Marshal(d.data)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'system')", d.id, d.doctype, encoded, d.status); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := ListWorkbench("default", family)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("want the 3 items of %s, got %+v", family, entries)
	}
	wantScore := map[string]float64{"WB149-I1": 100, "WB149-I2": 77.8, "WB149-I3": 66.7}
	wantStatus := map[string]string{"WB149-I1": "Approved", "WB149-I2": "Pending Approval", "WB149-I3": "Published"}
	for _, e := range entries {
		status, err := fetchPIMProductProfileStatus("default", e.ItemCode)
		if err != nil {
			t.Fatal(err)
		}
		if e.Score != wantScore[e.ItemCode] || status != wantStatus[e.ItemCode] {
			t.Errorf("%s: workbench score %.1f profile %q, want %.1f %q", e.ItemCode, e.Score, status, wantScore[e.ItemCode], wantStatus[e.ItemCode])
		}
		single, err := CalculateCompleteness("default", e.ItemCode, "en", "")
		if err != nil {
			t.Fatal(err)
		}
		if single.Score != e.Score || len(single.MissingFields) != e.MissingCount || single.EnrichmentStatus != status {
			t.Errorf("%s: workbench %.1f/%d/%q, single item %.1f/%d/%q", e.ItemCode, e.Score, e.MissingCount, status, single.Score, len(single.MissingFields), single.EnrichmentStatus)
		}
	}
	if entries[0].ItemCode != "WB149-I3" || entries[2].ItemCode != "WB149-I1" {
		t.Errorf("want worst first, got %s .. %s", entries[0].ItemCode, entries[2].ItemCode)
	}
}
