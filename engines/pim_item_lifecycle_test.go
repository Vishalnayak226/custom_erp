package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

func lifecycleSeedItem(t *testing.T, schema, code string, extra map[string]interface{}) {
	t.Helper()
	data := map[string]interface{}{"code": code, "name": code, "sale_price": 100, "hsn_code": "610910", "gst_rate": 5, "tax_treatment": "Taxable"}
	for k, v := range extra {
		data[k] = v
	}
	enc, _ := json.Marshal(data)
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", code, enc); err != nil {
		t.Fatalf("seed item %s: %v", code, err)
	}
}

func lifecycleCleanup(schema string, ids ...string) {
	for _, id := range ids {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
	}
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".field_permissions WHERE role = 'Cashier' AND fieldname = 'lifecycle_stage'")
}

func poLines(skus ...string) string {
	var ls []map[string]interface{}
	for _, s := range skus {
		ls = append(ls, map[string]interface{}{"sku": s, "qty": 1, "rate": 10})
	}
	b, _ := json.Marshal(ls)
	return string(b)
}

func TestItemLifecyclePORefusal(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	ids := []string{"LC-ACT", "LC-PHASE", "LC-EOL", "LC-SUB"}
	lifecycleCleanup(schema, ids...)
	defer lifecycleCleanup(schema, ids...)
	lifecycleSeedItem(t, schema, "LC-SUB", nil)
	lifecycleSeedItem(t, schema, "LC-ACT", nil)
	lifecycleSeedItem(t, schema, "LC-PHASE", map[string]interface{}{"lifecycle_stage": "Phase-out"})
	lifecycleSeedItem(t, schema, "LC-EOL", map[string]interface{}{"lifecycle_stage": "End of life", "substitute_item": "LC-SUB"})

	po := func(prior map[string]interface{}, skus ...string) error {
		payload := map[string]interface{}{"items": poLines(skus...), "vendor": "V", "status": "Draft"}
		return validatePOLifecycle("default", prior, payload)
	}
	if err := po(nil, "LC-ACT"); err != nil {
		t.Fatalf("active item must pass: %v", err)
	}
	err := po(nil, "LC-ACT", "LC-EOL")
	ve, ok := err.(*ValidationError)
	if !ok || ve.Code != "PIM-9200" || !strings.Contains(ve.Message, "LC-SUB") {
		t.Fatalf("EOL item must be refused naming the substitute, got %v", err)
	}
	if err := po(nil, "LC-PHASE"); err == nil {
		t.Fatal("Phase-out item must be refused")
	}
	// A PO that already holds the item may still be saved (received, amended).
	prior := map[string]interface{}{"items": poLines("LC-EOL")}
	if err := po(prior, "LC-EOL"); err != nil {
		t.Fatalf("a line already on the PO must not be refused: %v", err)
	}
	// And the shared PO save point runs it.
	if err := validatePurchaseOrderEditRules("default", "", "", nil, map[string]interface{}{"items": poLines("LC-EOL")}); err == nil {
		t.Fatal("the PO edit rules must refuse the End of life line")
	}
	// Setting off or on does not change the PO rule.
	_ = SetSetting("default", SettingLifecycleBlockEOLSale, "false", "test")
	if err := po(nil, "LC-EOL"); err == nil {
		t.Fatal("PO refusal does not depend on the sale setting")
	}
}

func TestItemLifecycleSaleSetting(t *testing.T) {
	db.InitDB(testConnStr())
	defer SetSetting("default", SettingLifecycleBlockEOLSale, "false", "test")
	eol := map[string]interface{}{"code": "X1", "lifecycle_stage": "End of life", "substitute_item": "X2"}
	launch := map[string]interface{}{"code": "X3", "lifecycle_stage": "Launch", "launch_date": "2030-01-01"}
	active := map[string]interface{}{"code": "X4"}

	_ = SetSetting("default", SettingLifecycleBlockEOLSale, "false", "test")
	w, err := lifecycleSale("default", "X1", eol, "2026-11-13")
	if err != nil || !strings.Contains(w, "X2") {
		t.Fatalf("setting off: warn naming substitute, no refusal; got %q %v", w, err)
	}
	if w, err := lifecycleSale("default", "X3", launch, "2026-11-13"); err != nil || w == "" {
		t.Fatalf("setting off: launch warns, got %q %v", w, err)
	}
	if w, err := lifecycleSale("default", "X3", launch, "2030-01-01"); err != nil || w != "" {
		t.Fatalf("on the launch date it sells silently, got %q %v", w, err)
	}
	if w, err := lifecycleSale("default", "X4", active, "2026-11-13"); err != nil || w != "" {
		t.Fatalf("active item: nothing to say, got %q %v", w, err)
	}
	_ = SetSetting("default", SettingLifecycleBlockEOLSale, "true", "test")
	if _, err := lifecycleSale("default", "X1", eol, "2026-11-13"); err == nil {
		t.Fatal("setting on: End of life is refused")
	}
	if _, err := lifecycleSale("default", "X3", launch, "2026-11-13"); err == nil {
		t.Fatal("setting on: not yet launched is refused")
	}
	if _, err := lifecycleSale("default", "X4", active, "2026-11-13"); err != nil {
		t.Fatalf("active still sells: %v", err)
	}
}

func TestItemLifecycleFieldsAndBulkSet(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	ids := []string{"LC-B1", "LC-B2"}
	lifecycleCleanup(schema, ids...)
	defer lifecycleCleanup(schema, ids...)
	lifecycleSeedItem(t, schema, "LC-B1", nil)
	lifecycleSeedItem(t, schema, "LC-B2", nil)

	if err := validateItemLifecycleFields(map[string]interface{}{"code": "A", "substitute_item": "a"}); err == nil {
		t.Fatal("own substitute must be refused")
	}
	if err := validateItemLifecycleFields(map[string]interface{}{"code": "A", "launch_date": "soon"}); err == nil {
		t.Fatal("bad launch date must be refused")
	}
	if err := validateItemLifecycleFields(map[string]interface{}{"code": "A", "substitute_item": "B", "launch_date": "2027-01-31"}); err != nil {
		t.Fatalf("valid fields: %v", err)
	}

	// Bulk edit sets the stage on both.
	if _, err := BulkUpdateDocuments("default", "Item", ids, "lifecycle_stage", "Phase-out", "u1", RoleSuperAdmin); err != nil {
		t.Fatalf("bulk set: %v", err)
	}
	for _, id := range ids {
		var stage string
		_ = db.DB.QueryRow("SELECT COALESCE(data->>'lifecycle_stage','') FROM "+schema+".documents WHERE id=$1", id).Scan(&stage)
		if stage != "Phase-out" {
			t.Fatalf("%s stage = %q", id, stage)
		}
	}
	// A bad stage is refused for the whole selection.
	if _, err := BulkUpdateDocuments("default", "Item", ids, "lifecycle_stage", "Retired", "u1", RoleSuperAdmin); err == nil {
		t.Fatal("unknown stage must be refused")
	}
	// Wrong role: a role barred from writing the field cannot bulk set it.
	if _, err := db.DB.Exec("INSERT INTO " + schema + ".field_permissions (role, doctype_name, fieldname, allow_read, allow_write) VALUES ('Cashier','Item','lifecycle_stage',true,false)"); err != nil {
		t.Fatalf("seed field permission: %v", err)
	}
	if _, err := BulkUpdateDocuments("default", "Item", ids, "lifecycle_stage", "Active", "u1", RoleCashier); err == nil {
		t.Fatal("a role barred from the field must be refused")
	}
}
