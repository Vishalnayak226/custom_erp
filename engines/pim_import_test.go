package engines

import (
	"custom_erp/db"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Stage 36.3 - import depth tests. Pure-decode logic needs no database;
// the validators do (they cross-check a target doctype's real fields and a
// referenced transform rule/template really exists), so those reuse the
// pimTestFixture/pimInsertDoc fixtures Stage 36.2's own tests established,
// scoped to a uniquely-prefixed id set and cleaned up regardless of outcome.

func TestDecodePIMImportColumnMappings(t *testing.T) {
	mappings, err := decodePIMImportColumnMappings(`[
		{"source_column":"Product Name","target_field":"name","default_value":""},
		{"source_column":"Brand","target_field":"brand","transform_rule":"TR-UPPER"}]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mappings) != 2 {
		t.Fatalf("got %d mappings, want 2", len(mappings))
	}
	if mappings[0].SourceColumn != "Product Name" || mappings[0].TargetField != "name" {
		t.Fatalf("mapping 0 decoded wrong: %+v", mappings[0])
	}
	if mappings[1].TransformRule != "TR-UPPER" {
		t.Fatalf("mapping 1 lost its transform_rule: %+v", mappings[1])
	}
}

func TestValidatePIMImportTemplateDocument(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'PIMIMPTEST%'")
	}
	cleanup()
	defer cleanup()

	base := func(mappingsJSON string) map[string]interface{} {
		return map[string]interface{}{"target_doctype": "Item", "column_mappings": mappingsJSON}
	}

	t.Run("refuses no target doctype", func(t *testing.T) {
		if err := ValidatePIMImportTemplateDocument("default", map[string]interface{}{"column_mappings": `[]`}); err == nil {
			t.Fatal("expected an error for a missing target doctype")
		}
	})
	t.Run("refuses no mappings", func(t *testing.T) {
		if err := ValidatePIMImportTemplateDocument("default", base(`[]`)); err == nil {
			t.Fatal("expected an error for an empty mapping list")
		}
	})
	t.Run("refuses a mapping with no source column", func(t *testing.T) {
		if err := ValidatePIMImportTemplateDocument("default", base(`[{"target_field":"name"}]`)); err == nil {
			t.Fatal("expected an error for a missing source column")
		}
	})
	t.Run("refuses a target field the doctype does not have", func(t *testing.T) {
		payload := base(`[{"source_column":"X","target_field":"definitely_not_a_real_field_xyz"}]`)
		if err := ValidatePIMImportTemplateDocument("default", payload); err == nil {
			t.Fatal("expected an error for an unknown target field")
		}
	})
	t.Run("refuses the same target field mapped twice", func(t *testing.T) {
		payload := base(`[{"source_column":"A","target_field":"name"},{"source_column":"B","target_field":"name"}]`)
		if err := ValidatePIMImportTemplateDocument("default", payload); err == nil {
			t.Fatal("expected an error for a duplicate target field")
		}
	})
	t.Run("refuses an unknown transform rule", func(t *testing.T) {
		payload := base(`[{"source_column":"Name","target_field":"name","transform_rule":"NO-SUCH-RULE-PIMIMPTEST"}]`)
		if err := ValidatePIMImportTemplateDocument("default", payload); err == nil {
			t.Fatal("expected an error for an unknown transform rule")
		}
	})
	t.Run("accepts a well-formed template", func(t *testing.T) {
		payload := base(`[{"source_column":"Name","target_field":"name"},{"source_column":"Code","target_field":"id"}]`)
		if err := ValidatePIMImportTemplateDocument("default", payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	pimInsertDoc(t, schema, "PIMIMPTEST-TEMPLATE", "PIMImportTemplate", "Active", map[string]interface{}{
		"id": "PIMIMPTEST-TEMPLATE", "code": "PIMIMPTEST-TEMPLATE", "name": "Test Template",
		"target_doctype": "Item", "column_mappings": `[{"source_column":"Name","target_field":"name"}]`,
	})

	t.Run("import schedule refuses no template", func(t *testing.T) {
		if err := ValidatePIMImportScheduleDocument("default", map[string]interface{}{"source_type": "API Hook"}); err == nil {
			t.Fatal("expected an error for a missing template")
		}
	})
	t.Run("import schedule refuses an unknown source type", func(t *testing.T) {
		payload := map[string]interface{}{"template": "PIMIMPTEST-TEMPLATE", "source_type": "Carrier Pigeon"}
		if err := ValidatePIMImportScheduleDocument("default", payload); err == nil {
			t.Fatal("expected an error for an unrecognized source type")
		}
	})
	t.Run("drop directory schedule refuses a missing path", func(t *testing.T) {
		payload := map[string]interface{}{"template": "PIMIMPTEST-TEMPLATE", "source_type": "Drop Directory", "frequency": "Daily", "next_run_date": "2026-09-01"}
		if err := ValidatePIMImportScheduleDocument("default", payload); err == nil {
			t.Fatal("expected an error for a missing source path")
		}
	})
	t.Run("drop directory schedule refuses a bad frequency", func(t *testing.T) {
		payload := map[string]interface{}{"template": "PIMIMPTEST-TEMPLATE", "source_type": "Drop Directory", "source_path": "/tmp/x", "frequency": "Fortnightly", "next_run_date": "2026-09-01"}
		if err := ValidatePIMImportScheduleDocument("default", payload); err == nil {
			t.Fatal("expected an error for an invalid frequency")
		}
	})
	t.Run("drop directory schedule accepts a well-formed payload", func(t *testing.T) {
		payload := map[string]interface{}{"template": "PIMIMPTEST-TEMPLATE", "source_type": "Drop Directory", "source_path": "/tmp/x", "frequency": "Daily", "next_run_date": "2026-09-01"}
		if err := ValidatePIMImportScheduleDocument("default", payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("api hook schedule refuses drop-directory-only fields being set", func(t *testing.T) {
		payload := map[string]interface{}{"template": "PIMIMPTEST-TEMPLATE", "source_type": "API Hook", "source_path": "/tmp/x"}
		if err := ValidatePIMImportScheduleDocument("default", payload); err == nil {
			t.Fatal("expected an error for source_path set on an API Hook schedule")
		}
	})
	t.Run("api hook schedule accepts a bare payload", func(t *testing.T) {
		payload := map[string]interface{}{"template": "PIMIMPTEST-TEMPLATE", "source_type": "API Hook"}
		if err := ValidatePIMImportScheduleDocument("default", payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// TestPIMVariantParentPreflight exercises the 36.3.5 variant-parent check
// directly: a variant whose parent is neither already in the database nor
// earlier in the same file is refused; one whose parent appears earlier in
// the same file (a fresh product being imported alongside its variants) is
// not.
func TestPIMVariantParentPreflight(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'PIMIMPTEST%'")
	}
	cleanup()
	defer cleanup()

	pimInsertDoc(t, schema, "PIMIMPTEST-PARENT-DB", "Item", "Active", map[string]interface{}{
		"id": "PIMIMPTEST-PARENT-DB", "code": "PIMIMPTEST-PARENT-DB", "name": "Already In DB",
	})

	rows := []map[string]interface{}{
		{"id": "PIMIMPTEST-CHILD-1", "parent_product_code": "PIMIMPTEST-PARENT-DB"},    // parent already in DB - fine
		{"id": "PIMIMPTEST-NEWPARENT", "parent_product_code": ""},                      // the new parent itself, no parent of its own
		{"id": "PIMIMPTEST-CHILD-2", "parent_product_code": "PIMIMPTEST-NEWPARENT"},    // parent defined earlier in this same file - fine
		{"id": "PIMIMPTEST-CHILD-3", "parent_product_code": "PIMIMPTEST-GHOST-PARENT"}, // parent nowhere - refused
	}
	errs := pimVariantParentPreflight("default", rows)
	if len(errs) != len(rows) {
		t.Fatalf("got %d error slots, want %d", len(errs), len(rows))
	}
	if errs[0] != "" {
		t.Fatalf("row 0 (parent already in DB) should pass, got: %q", errs[0])
	}
	if errs[1] != "" {
		t.Fatalf("row 1 (no parent) should pass, got: %q", errs[1])
	}
	if errs[2] != "" {
		t.Fatalf("row 2 (parent earlier in file) should pass, got: %q", errs[2])
	}
	if errs[3] == "" {
		t.Fatal("row 3 (parent nowhere) should have been refused")
	}
}

// TestProcessPIMImportSchedulesDropDirectory exercises 36.3.1/36.3.2 end to
// end at the Go level, since the real worker only ever fires on an hourly
// ticker (StartPIMImportScheduleWorker) - processPIMImportSchedules is the
// exact function that ticker calls, so calling it directly with a due
// schedule proves the same code path without waiting an hour.
func TestProcessPIMImportSchedulesDropDirectory(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}
	cleanup := func() {
		// The imported Item gets an auto-generated sequence id (its own
		// "code" field, not "id", is what the CSV set), so cleanup also
		// has to match on the code it was given, not just the id prefix.
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'PIMIMPTEST%' OR data->>'code' LIKE 'PIMIMPTEST%'")
	}
	cleanup()
	defer cleanup()

	dropDir, err := os.MkdirTemp("", "pimimptest-drop-*")
	if err != nil {
		t.Fatalf("create temp drop dir: %v", err)
	}
	defer os.RemoveAll(dropDir)

	pimInsertDoc(t, schema, "PIMIMPTEST-DROPTPL", "PIMImportTemplate", "Active", map[string]interface{}{
		"id": "PIMIMPTEST-DROPTPL", "code": "PIMIMPTEST-DROPTPL", "name": "Drop Directory Test Template",
		"target_doctype": "Item",
		"column_mappings": `[
			{"source_column":"Name","target_field":"name"},
			{"source_column":"Code","target_field":"code"},
			{"source_column":"Barcode","target_field":"barcode"},
			{"source_column":"HSN","target_field":"hsn_code"},
			{"source_column":"GSTRate","target_field":"gst_rate"}]`,
	})

	csvBody := "Name,Code,Barcode,HSN,GSTRate\nDrop Test Product,PIMIMPTEST-DROPITEM,1234567890123,9999,18\n"
	if err := os.WriteFile(filepath.Join(dropDir, "batch1.csv"), []byte(csvBody), 0o644); err != nil {
		t.Fatalf("write fixture CSV: %v", err)
	}

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	pimInsertDoc(t, schema, "PIMIMPTEST-DROPSCHED", "PIMImportSchedule", "Active", map[string]interface{}{
		"id": "PIMIMPTEST-DROPSCHED", "code": "PIMIMPTEST-DROPSCHED", "name": "Drop Directory Test Schedule",
		"template": "PIMIMPTEST-DROPTPL", "source_type": "Drop Directory",
		"source_path": dropDir, "frequency": "Daily", "next_run_date": yesterday,
	})

	processPIMImportSchedules(schema)

	var itemName string
	if err := db.DB.QueryRow("SELECT data->>'name' FROM "+schema+".documents WHERE doctype = 'Item' AND data->>'code' = $1",
		"PIMIMPTEST-DROPITEM").Scan(&itemName); err != nil {
		t.Fatalf("expected the dropped file's row to have been imported as an Item: %v", err)
	}
	if itemName != "Drop Test Product" {
		t.Fatalf("imported Item name = %q, want %q", itemName, "Drop Test Product")
	}

	if _, err := os.Stat(filepath.Join(dropDir, "imported", "batch1.csv")); err != nil {
		t.Fatalf("expected batch1.csv to have been moved to imported/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dropDir, "batch1.csv")); !os.IsNotExist(err) {
		t.Fatal("batch1.csv should no longer be sitting in the drop directory itself")
	}

	var nextRunDate, lastRunStatus string
	if err := db.DB.QueryRow("SELECT data->>'next_run_date', data->>'last_run_status' FROM "+schema+".documents WHERE doctype = 'PIMImportSchedule' AND id = $1",
		"PIMIMPTEST-DROPSCHED").Scan(&nextRunDate, &lastRunStatus); err != nil {
		t.Fatalf("re-read schedule: %v", err)
	}
	today := time.Now().Format("2006-01-02")
	if nextRunDate <= today {
		t.Fatalf("next_run_date = %q, want something after %q (advanced past the run)", nextRunDate, today)
	}
	if lastRunStatus != "Completed" {
		t.Fatalf("last_run_status = %q, want %q", lastRunStatus, "Completed")
	}
}
