package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// Stage 42.1.10 tests. Own file, same db.InitDB / tenantID="default"
// convention as TestTraceability; every subtest cleans up its own markers
// since this suite shares one database with the rest of the package.

func uomCleanup(t *testing.T, schema, marker string) {
	t.Helper()
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'UOMConversion' AND data->>'item' LIKE '" + marker + "%'")
	// seedUOM's codes are "CASE-"+marker / "EA-"+marker - marker is a suffix,
	// not a prefix, so this has to be a contains match.
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'UOM' AND data->>'code' LIKE '%" + marker + "%'")
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Item' AND data->>'code' LIKE '" + marker + "%'")
}

func seedUOM(t *testing.T, schema, code string) {
	t.Helper()
	data, _ := json.Marshal(map[string]interface{}{"code": code, "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'UOM', $2, 'Active', 'system')",
		"UOM-"+code, data); err != nil {
		t.Fatalf("seed UOM %s: %v", code, err)
	}
}

func TestConvertUOMQty(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const sku = "SKU-UOMCONV-TEST"
	uomCleanup(t, schema, sku)
	defer uomCleanup(t, schema, sku)

	// Same or blank UOM is always a no-op, with no row required at all - the
	// fast path every caller that has never heard of a UOM takes.
	if got, err := ConvertUOMQty(tenantID, sku, 10, "EA", "EA"); err != nil || got != 10 {
		t.Errorf("expected same-UOM to be a no-op (10, nil), got (%v, %v)", got, err)
	}
	if got, err := ConvertUOMQty(tenantID, sku, 10, "", "CASE"); err != nil || got != 10 {
		t.Errorf("expected a blank fromUOM to be a no-op (10, nil), got (%v, %v)", got, err)
	}
	if _, err := ConvertUOMQty(tenantID, sku, 10, "CASE", "EA"); err == nil {
		t.Error("expected an undefined conversion to error")
	}

	convData, _ := json.Marshal(map[string]interface{}{
		"item": sku, "from_uom": "CASE", "to_uom": "EA", "factor": 12, "status": "Active",
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'UOMConversion', $2, 'Active', 'system')",
		"UOMCONV-TEST-1", convData); err != nil {
		t.Fatalf("seed UOMConversion: %v", err)
	}

	// The direct edge: 2 CASE -> 24 EA.
	if got, err := ConvertUOMQty(tenantID, sku, 2, "CASE", "EA"); err != nil || got != 24 {
		t.Errorf("expected 2 CASE = 24 EA, got (%v, %v)", got, err)
	}
	// The inverse edge, never separately entered: 24 EA -> 2 CASE.
	if got, err := ConvertUOMQty(tenantID, sku, 24, "EA", "CASE"); err != nil || got != 2 {
		t.Errorf("expected 24 EA = 2 CASE via the inverse edge, got (%v, %v)", got, err)
	}
	// A different item with no rows of its own still errors - the factor is
	// per-item, never a tenant-wide default.
	if _, err := ConvertUOMQty(tenantID, "SKU-UOMCONV-OTHER", 1, "CASE", "EA"); err == nil {
		t.Error("expected an item with no conversion rows to error, not silently reuse another item's factor")
	}
}

func TestUOMConversionMasterRules(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const sku = "SKU-UOMRULE-TEST"
	uomCleanup(t, schema, sku)
	defer uomCleanup(t, schema, sku)

	itemData, _ := json.Marshal(map[string]interface{}{
		"code": sku, "name": sku, "barcode": "BC-" + sku,
		"hsn_code": "6109", "tax_treatment": "Taxable", "gst_rate": 5,
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')",
		"ITEM-"+sku, itemData); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	seedUOM(t, schema, "CASE-"+sku)
	seedUOM(t, schema, "EA-"+sku)

	payload := map[string]interface{}{
		"item": sku, "from_uom": "CASE-" + sku, "to_uom": "EA-" + sku, "factor": 12.0, "status": "Active",
	}
	if err := ValidateMasterDataRules(tenantID, "UOMCONV-RULE-01", "UOMConversion", payload); err != nil {
		t.Fatalf("expected a well-formed conversion to validate, got %v", err)
	}

	selfConv := map[string]interface{}{"item": sku, "from_uom": "EA-" + sku, "to_uom": "EA-" + sku, "factor": 1.0}
	if err := ValidateMasterDataRules(tenantID, "UOMCONV-RULE-02", "UOMConversion", selfConv); err == nil {
		t.Error("expected a UOM converting to itself to be refused")
	}

	zeroFactor := map[string]interface{}{"item": sku, "from_uom": "CASE-" + sku, "to_uom": "EA-" + sku, "factor": 0.0}
	if err := ValidateMasterDataRules(tenantID, "UOMCONV-RULE-03", "UOMConversion", zeroFactor); err == nil {
		t.Error("expected a non-positive factor to be refused")
	}

	badItem := map[string]interface{}{"item": "SKU-UOMRULE-NOTREAL", "from_uom": "CASE-" + sku, "to_uom": "EA-" + sku, "factor": 12.0}
	if err := ValidateMasterDataRules(tenantID, "UOMCONV-RULE-04", "UOMConversion", badItem); err == nil {
		t.Error("expected a nonexistent item to be refused")
	}

	badUOM := map[string]interface{}{"item": sku, "from_uom": "PALLET-NOTREAL", "to_uom": "EA-" + sku, "factor": 40.0}
	if err := ValidateMasterDataRules(tenantID, "UOMCONV-RULE-05", "UOMConversion", badUOM); err == nil {
		t.Error("expected a nonexistent From UOM to be refused")
	}

	data, _ := json.Marshal(payload)
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'UOMConversion', $2, 'Active', 'system')",
		"UOMCONV-RULE-01", data); err != nil {
		t.Fatalf("seed conversion: %v", err)
	}
	dup := map[string]interface{}{"item": sku, "from_uom": "CASE-" + sku, "to_uom": "EA-" + sku, "factor": 24.0}
	if err := ValidateMasterDataRules(tenantID, "UOMCONV-RULE-06", "UOMConversion", dup); err == nil {
		t.Error("expected a duplicate (item, from_uom, to_uom) to be refused")
	}
	if err := ValidateMasterDataRules(tenantID, "UOMCONV-RULE-01", "UOMConversion", payload); err != nil {
		t.Errorf("expected editing the same row to not collide with itself: %v", err)
	}
}

// TestPickUOMDisplay (Stage 42.1.10) locks down GenerateBinPickList's
// display-only wiring: a task line that names a pick_uom gets a converted
// quantity alongside the real (always-eaches) PickQty, and a line that
// doesn't - every task created before this Stage - is completely unaffected.
func TestPickUOMDisplay(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const sku = "SKU-PICKUOM-TEST"
	const bin = "BIN-PICKUOM-TEST"
	uomCleanup(t, schema, sku)
	defer uomCleanup(t, schema, sku)
	defer func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock WHERE sku = '" + sku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku = '" + sku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Bin' AND data->>'bin_code' = '" + bin + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'FulfillmentTask' AND data->>'items' LIKE '%" + sku + "%'")
	}()

	seedBinWithStock(t, schema, bin, sku, "WH01", 24)
	convData, _ := json.Marshal(map[string]interface{}{
		"item": sku, "from_uom": "CASE", "to_uom": "EA", "factor": 12, "status": "Active",
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'UOMConversion', $2, 'Active', 'system')",
		"UOMCONV-PICK-1", convData); err != nil {
		t.Fatalf("seed UOMConversion: %v", err)
	}

	taskID, err := CreateFulfillmentTasks(tenantID, "ORD-PICKUOM-TEST", "WH01", []interface{}{
		map[string]interface{}{"sku": sku, "qty": 24, "pick_uom": "CASE"},
	})
	if err != nil {
		t.Fatalf("CreateFulfillmentTasks: %v", err)
	}

	lines, err := GenerateBinPickList(tenantID, taskID)
	if err != nil {
		t.Fatalf("GenerateBinPickList: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 pick line, got %d: %+v", len(lines), lines)
	}
	if lines[0].PickQty != 24 {
		t.Errorf("expected PickQty to stay in eaches (24), got %d", lines[0].PickQty)
	}
	if lines[0].PickUOM != "CASE" || lines[0].PickQtyInUOM != 2 {
		t.Errorf("expected PickUOM=CASE, PickQtyInUOM=2 (24 EA / 12), got %q / %v", lines[0].PickUOM, lines[0].PickQtyInUOM)
	}

	// A task with no pick_uom at all - every task created before this Stage -
	// must carry neither field.
	const plainSku = "SKU-PICKUOM-PLAIN"
	const plainBin = "BIN-PICKUOM-PLAIN"
	cleanupPlain := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock WHERE sku = '" + plainSku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku = '" + plainSku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Bin' AND data->>'bin_code' = '" + plainBin + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'FulfillmentTask' AND data->>'items' LIKE '%" + plainSku + "%'")
	}
	cleanupPlain()
	defer cleanupPlain()
	seedBinWithStock(t, schema, plainBin, plainSku, "WH01", 5)
	plainTaskID, err := CreateFulfillmentTasks(tenantID, "ORD-PICKUOM-PLAIN", "WH01", []interface{}{
		map[string]interface{}{"sku": plainSku, "qty": 5},
	})
	if err != nil {
		t.Fatalf("CreateFulfillmentTasks (plain): %v", err)
	}
	plainLines, err := GenerateBinPickList(tenantID, plainTaskID)
	if err != nil {
		t.Fatalf("GenerateBinPickList (plain): %v", err)
	}
	if len(plainLines) != 1 || plainLines[0].PickUOM != "" || plainLines[0].PickQtyInUOM != 0 {
		t.Errorf("expected a pick_uom-less task to carry no UOM fields at all, got %+v", plainLines)
	}
}
