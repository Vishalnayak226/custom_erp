package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 146.3.6: a packing list per box. A closed (non-Draft) ShippingPackage
// prints with its package no., weights, dimensions and contents; a Draft one
// does not print yet.
func TestStage14636PackingListPerBox(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insert("SKU-PL", "Item", "Active", map[string]interface{}{"code": "SKU-PL", "name": "Packed Shirt"})
	pkg := map[string]interface{}{"code": "PKG-PL1", "fulfillment_task_id": "FT-1", "order_id": "SO-1", "location_code": "WH1",
		"weight_kg": 4.5, "length_cm": 40.0, "width_cm": 30.0, "height_cm": 20.0,
		"items": []map[string]interface{}{{"sku": "SKU-PL", "qty": 3}, {"sku": "SKU-PL2", "qty": 2}}}
	insert("PKG-PL1", "ShippingPackage", "Draft", pkg)
	if _, err := BuildPrintDocument(tenantID, "ShippingPackage", "PKG-PL1", "", false); err == nil {
		t.Fatalf("a Draft package must not print its packing list")
	}
	if _, err := db.DB.Exec("UPDATE " + schemaName + ".documents SET status = 'Invoiced' WHERE id = 'PKG-PL1'"); err != nil {
		t.Fatal(err)
	}
	d, err := BuildPrintDocument(tenantID, "ShippingPackage", "PKG-PL1", "", false)
	if err != nil {
		t.Fatalf("closed package print: %v", err)
	}
	if d.Title != "Packing List" || len(d.Lines) != 2 || d.Lines[0]["sku"] != "SKU-PL" || d.Lines[0]["item_name"] != "Packed Shirt" || d.Lines[0]["qty"] != 3.0 {
		t.Errorf("packing list: title %q lines %v", d.Title, d.Lines)
	}
	if d.Number != "PKG-PL1" || d.Fields["total__qty"] != 5.0 {
		t.Errorf("packing list: number %q total %v", d.Number, d.Fields["total__qty"])
	}
	keys := map[string]bool{}
	for _, h := range d.Fields["generic_print"].(map[string]interface{})["header"].([]genericPrintColumn) {
		keys[h.Key] = true
	}
	for _, k := range []string{"weight_kg", "length_cm", "width_cm", "height_cm"} {
		if !keys[k] {
			t.Errorf("packing list header lacks %s: %v", k, keys)
		}
	}
}
