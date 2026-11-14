package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// The clean-up also renumbers a combination made before the Item series
// (its ID is its design's code and its values), and nothing else.
func TestRecodeDashedCombinationIDs(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	var migrated bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = 'master_code_changes')`, schema).Scan(&migrated)
	if !migrated {
		t.Skip("needs the master_code_changes migration - run erp-server -migrate on this database")
	}
	doc := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,'Active','tester')", id, doctype, b); err != nil {
			t.Fatal(err)
		}
	}
	old := "D00004-Red-Temple-Red-Temple"
	doc("D00004", "ProductFamily", map[string]interface{}{"code": "D00004", "name": "Temple"})
	doc(old, "Item", map[string]interface{}{"code": old, "name": "Temple red", "parent_product_code": "D00004"})
	doc("D00004-Blue", "Item", map[string]interface{}{"code": "D00004-Blue", "name": "Temple blue", "family": "D00004"})
	doc("KURTA-RED-M", "Item", map[string]interface{}{"code": "KURTA-RED-M", "name": "Typed SKU"})
	doc("X-9", "Item", map[string]interface{}{"code": "X-9", "name": "Other design", "parent_product_code": "D00004"})
	doc("PO-1", "PurchaseOrder", map[string]interface{}{"item": old})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".shopify_links (channel, sku, design) VALUES ('shop1', $1, 'D00004')", old); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanMasterRecodeWithTouches(tenantID, "Item", NewMasterCodeFormat("I", 5))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rows) != 2 || plan.ShopifyLinked != 1 || !strings.Contains(plan.Warning, "Shopify") {
		t.Fatalf("preview: want the two dashed combinations and 1 Shopify link, got %+v", plan)
	}
	done, err := ApplyMasterRecode(tenantID, "Item", NewMasterCodeFormat("I", 5), true, "tester")
	if err != nil || len(done.Rows) != 2 {
		t.Fatalf("apply: %v %+v", err, done)
	}
	var newCode string
	for _, r := range done.Rows {
		if r.Old == old {
			newCode = r.New
		}
	}
	if newCode == "" || strings.Contains(newCode, "-") {
		t.Fatalf("no new Item number for %s: %+v", old, done.Rows)
	}
	var ref string
	db.DB.QueryRow("SELECT data->>'item' FROM " + schema + ".documents WHERE id = 'PO-1'").Scan(&ref)
	var sku string
	db.DB.QueryRow("SELECT sku FROM " + schema + ".shopify_links WHERE channel = 'shop1'").Scan(&sku)
	if ref != newCode || sku != newCode {
		t.Errorf("references should follow: PO %q, link %q, want %q", ref, sku, newCode)
	}
	if got := RenamedMasterCode(tenantID, "Item", old); got != newCode {
		t.Errorf("the old dashed ID should resolve to %s, got %q", newCode, got)
	}
	var kept int
	db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE id IN ('KURTA-RED-M','X-9','D00004')").Scan(&kept)
	if kept != 3 {
		t.Errorf("a typed SKU, a design and a code not built from its design must stay: %d of 3", kept)
	}
	if again, _ := ApplyMasterRecode(tenantID, "Item", nil, true, "tester"); len(again.Rows) != 0 {
		t.Errorf("a second run must find nothing: %+v", again.Rows)
	}
}
