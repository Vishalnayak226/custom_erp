package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 36.4 - export & syndication depth tests. Pure-decode logic needs no
// database; the validators/runners do (they cross-check a channel's own
// field map, a product group's live membership, a template that really
// exists), so those reuse the pimInsertDoc/testConnStr fixtures Stage 36.2's
// own tests established, scoped to a uniquely-prefixed id set and cleaned up
// regardless of outcome.

func TestDecodePIMExportColumns(t *testing.T) {
	cols, err := decodePIMExportColumns(`[{"field_key":"item_code","column_header":"SKU"},{"field_key":"title","column_header":"Title"}]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cols) != 2 || cols[0].FieldKey != "item_code" || cols[1].ColumnHeader != "Title" {
		t.Fatalf("decoded wrong: %+v", cols)
	}
}

func TestValidatePIMExportTemplateDocument(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'PIMEXPTEST%'")
	}
	cleanup()
	defer cleanup()

	pimInsertDoc(t, schema, "PIMEXPTEST-CHANNEL", "Channel", "Active", map[string]interface{}{
		"id": "PIMEXPTEST-CHANNEL", "code": "PIMEXPTEST-CHANNEL", "name": "Test Channel",
	})
	pimInsertDoc(t, schema, "PIMEXPTEST-FIELDMAP-1", "ChannelFieldMap", "Active", map[string]interface{}{
		"id": "PIMEXPTEST-FIELDMAP-1", "channel": "PIMEXPTEST-CHANNEL", "source_field": "name", "target_field": "ProductName",
	})

	base := func(mappingsJSON string) map[string]interface{} {
		return map[string]interface{}{"column_mappings": mappingsJSON, "headerless": "No", "variant_mode": "All Rows"}
	}

	t.Run("refuses no columns", func(t *testing.T) {
		if err := ValidatePIMExportTemplateDocument("default", base(`[]`)); err == nil {
			t.Fatal("expected an error for an empty column list")
		}
	})
	t.Run("refuses a column with no field key", func(t *testing.T) {
		if err := ValidatePIMExportTemplateDocument("default", base(`[{"column_header":"X"}]`)); err == nil {
			t.Fatal("expected an error for a missing field key")
		}
	})
	t.Run("refuses a column with no header", func(t *testing.T) {
		if err := ValidatePIMExportTemplateDocument("default", base(`[{"field_key":"item_code"}]`)); err == nil {
			t.Fatal("expected an error for a missing header")
		}
	})
	t.Run("refuses a duplicate field key", func(t *testing.T) {
		payload := base(`[{"field_key":"item_code","column_header":"A"},{"field_key":"item_code","column_header":"B"}]`)
		if err := ValidatePIMExportTemplateDocument("default", payload); err == nil {
			t.Fatal("expected an error for a duplicate field key")
		}
	})
	t.Run("refuses an unknown raw field key", func(t *testing.T) {
		payload := base(`[{"field_key":"definitely_not_a_real_field_xyz","column_header":"X"}]`)
		if err := ValidatePIMExportTemplateDocument("default", payload); err == nil {
			t.Fatal("expected an error for an unknown raw field key")
		}
	})
	t.Run("accepts a well-formed raw template", func(t *testing.T) {
		payload := base(`[{"field_key":"item_code","column_header":"SKU"},{"field_key":"title","column_header":"Title"}]`)
		if err := ValidatePIMExportTemplateDocument("default", payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("refuses an unknown channel", func(t *testing.T) {
		payload := base(`[{"field_key":"title","column_header":"Title"}]`)
		payload["channel"] = "NO-SUCH-CHANNEL-PIMEXPTEST"
		if err := ValidatePIMExportTemplateDocument("default", payload); err == nil {
			t.Fatal("expected an error for an unknown channel")
		}
	})
	t.Run("refuses a field key not published by the channel's field map", func(t *testing.T) {
		payload := base(`[{"field_key":"cost_price_secret","column_header":"Cost"}]`)
		payload["channel"] = "PIMEXPTEST-CHANNEL"
		if err := ValidatePIMExportTemplateDocument("default", payload); err == nil {
			t.Fatal("expected an error for a field the channel does not publish")
		}
	})
	t.Run("accepts a channel field map's own target field, case-insensitively", func(t *testing.T) {
		payload := base(`[{"field_key":"title","column_header":"Title"},{"field_key":"productname","column_header":"Name"}]`)
		payload["channel"] = "PIMEXPTEST-CHANNEL"
		if err := ValidatePIMExportTemplateDocument("default", payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("refuses a bad headerless value", func(t *testing.T) {
		payload := base(`[{"field_key":"item_code","column_header":"SKU"}]`)
		payload["headerless"] = "Sure"
		if err := ValidatePIMExportTemplateDocument("default", payload); err == nil {
			t.Fatal("expected an error for an invalid headerless value")
		}
	})
	t.Run("refuses a bad variant mode", func(t *testing.T) {
		payload := base(`[{"field_key":"item_code","column_header":"SKU"}]`)
		payload["variant_mode"] = "Sideways"
		if err := ValidatePIMExportTemplateDocument("default", payload); err == nil {
			t.Fatal("expected an error for an invalid variant mode")
		}
	})
}

func TestValidatePIMExportScheduleDocument(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'PIMEXPTEST%'")
	}
	cleanup()
	defer cleanup()

	pimInsertDoc(t, schema, "PIMEXPTEST-TEMPLATE", "PIMExportTemplate", "Active", map[string]interface{}{
		"id": "PIMEXPTEST-TEMPLATE", "code": "PIMEXPTEST-TEMPLATE", "name": "Test Export Template",
		"column_mappings": `[{"field_key":"item_code","column_header":"SKU"}]`, "headerless": "No", "variant_mode": "All Rows",
	})

	t.Run("refuses no template", func(t *testing.T) {
		payload := map[string]interface{}{"frequency": "Daily", "next_run_date": "2026-09-01", "recipient_email": "a@b.com"}
		if err := ValidatePIMExportScheduleDocument("default", payload); err == nil {
			t.Fatal("expected an error for a missing template")
		}
	})
	t.Run("refuses a bad frequency", func(t *testing.T) {
		payload := map[string]interface{}{"export_template": "PIMEXPTEST-TEMPLATE", "frequency": "Fortnightly", "next_run_date": "2026-09-01", "recipient_email": "a@b.com"}
		if err := ValidatePIMExportScheduleDocument("default", payload); err == nil {
			t.Fatal("expected an error for an invalid frequency")
		}
	})
	t.Run("refuses no delivery target", func(t *testing.T) {
		payload := map[string]interface{}{"export_template": "PIMEXPTEST-TEMPLATE", "frequency": "Daily", "next_run_date": "2026-09-01"}
		if err := ValidatePIMExportScheduleDocument("default", payload); err == nil {
			t.Fatal("expected an error for neither a recipient email nor a webhook URL")
		}
	})
	t.Run("accepts a well-formed schedule", func(t *testing.T) {
		payload := map[string]interface{}{"export_template": "PIMEXPTEST-TEMPLATE", "frequency": "Daily", "next_run_date": "2026-09-01", "webhook_url": "https://example.com/hook"}
		if err := ValidatePIMExportScheduleDocument("default", payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestValidatePIMCatalogDocument(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'PIMEXPTEST%'")
	}
	cleanup()
	defer cleanup()

	pimInsertDoc(t, schema, "PIMEXPTEST-ITEM", "Item", "Active", map[string]interface{}{
		"id": "PIMEXPTEST-ITEM", "code": "PIMEXPTEST-ITEM", "name": "Catalog Test Item",
	})
	pimInsertDoc(t, schema, "PIMEXPTEST-GROUP", "PIMProductGroup", "Active", map[string]interface{}{
		"id": "PIMEXPTEST-GROUP", "code": "PIMEXPTEST-GROUP", "name": "Catalog Test Group",
		"group_type": "Static", "members": `[{"item_code":"PIMEXPTEST-ITEM"}]`,
	})

	t.Run("refuses no product group", func(t *testing.T) {
		if err := ValidatePIMCatalogDocument("default", map[string]interface{}{}); err == nil {
			t.Fatal("expected an error for a missing product group")
		}
	})
	t.Run("refuses an unknown product group", func(t *testing.T) {
		payload := map[string]interface{}{"product_group": "NO-SUCH-GROUP-PIMEXPTEST"}
		if err := ValidatePIMCatalogDocument("default", payload); err == nil {
			t.Fatal("expected an error for an unknown product group")
		}
	})
	t.Run("refuses a malformed expiry date", func(t *testing.T) {
		payload := map[string]interface{}{"product_group": "PIMEXPTEST-GROUP", "expiry_date": "not-a-date"}
		if err := ValidatePIMCatalogDocument("default", payload); err == nil {
			t.Fatal("expected an error for a malformed expiry date")
		}
	})
	t.Run("accepts a well-formed catalog", func(t *testing.T) {
		payload := map[string]interface{}{"product_group": "PIMEXPTEST-GROUP", "expiry_date": "2030-01-01"}
		if err := ValidatePIMCatalogDocument("default", payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// TestRunPIMExportTemplate exercises 36.4.1/36.4.3 end to end: chosen
// columns in chosen order, headerless mode, and variant collapsing all
// change the CSV RunPIMExportTemplate actually writes.
func TestRunPIMExportTemplate(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'PIMEXPTEST%'")
	}
	cleanup()
	defer cleanup()

	pimInsertDoc(t, schema, "PIMEXPTEST-PARENT", "Item", "Active", map[string]interface{}{
		"id": "PIMEXPTEST-PARENT", "code": "PIMEXPTEST-PARENT", "name": "Parent Product",
	})
	pimInsertDoc(t, schema, "PIMEXPTEST-VARIANT", "Item", "Active", map[string]interface{}{
		"id": "PIMEXPTEST-VARIANT", "code": "PIMEXPTEST-VARIANT", "name": "Variant Product",
		"parent_product_code": "PIMEXPTEST-PARENT",
	})

	pimInsertDoc(t, schema, "PIMEXPTEST-ALLROWS", "PIMExportTemplate", "Active", map[string]interface{}{
		"id": "PIMEXPTEST-ALLROWS", "code": "PIMEXPTEST-ALLROWS", "name": "All Rows",
		"column_mappings": `[{"field_key":"item_code","column_header":"SKU"},{"field_key":"name","column_header":"Name"}]`,
		"headerless": "No", "variant_mode": "All Rows",
	})
	pimInsertDoc(t, schema, "PIMEXPTEST-PARENTONLY", "PIMExportTemplate", "Active", map[string]interface{}{
		"id": "PIMEXPTEST-PARENTONLY", "code": "PIMEXPTEST-PARENTONLY", "name": "Parent Only",
		"column_mappings": `[{"field_key":"item_code","column_header":"SKU"},{"field_key":"variant_count","column_header":"Variants"}]`,
		"headerless": "Yes", "variant_mode": "Parent Only - Variants Collapsed",
	})

	t.Run("all rows, with header, includes the variant", func(t *testing.T) {
		out, err := RunPIMExportTemplate("default", "PIMEXPTEST-ALLROWS")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		text := string(out)
		if !strings.HasPrefix(text, "SKU,Name\n") {
			t.Fatalf("expected a header row, got: %q", text)
		}
		if !strings.Contains(text, "PIMEXPTEST-VARIANT,Variant Product") {
			t.Fatalf("expected the variant row present, got: %q", text)
		}
		if !strings.Contains(text, "PIMEXPTEST-PARENT,Parent Product") {
			t.Fatalf("expected the parent row present, got: %q", text)
		}
	})

	t.Run("parent only, headerless, collapses the variant and counts it", func(t *testing.T) {
		out, err := RunPIMExportTemplate("default", "PIMEXPTEST-PARENTONLY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		text := string(out)
		if strings.Contains(text, "PIMEXPTEST-VARIANT") {
			t.Fatalf("variant row should have been collapsed under its parent, got: %q", text)
		}
		if !strings.Contains(text, "PIMEXPTEST-PARENT,1") {
			t.Fatalf("expected the parent row with a variant_count of 1, got: %q", text)
		}
		if strings.HasPrefix(text, "SKU") {
			t.Fatalf("headerless output should have no header row, got: %q", text)
		}
	})
}

// TestPIMCatalogShareTokenLifecycle exercises 36.4.4 end to end: mint,
// resolve, wrong-token refusal, deactivation and expiry all revoke access.
func TestPIMCatalogShareTokenLifecycle(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'PIMEXPTEST%'")
	}
	cleanup()
	defer cleanup()

	pimInsertDoc(t, schema, "PIMEXPTEST-CATITEM", "Item", "Active", map[string]interface{}{
		"id": "PIMEXPTEST-CATITEM", "code": "PIMEXPTEST-CATITEM", "name": "Shared Item",
	})
	pimInsertDoc(t, schema, "PIMEXPTEST-CATGROUP", "PIMProductGroup", "Active", map[string]interface{}{
		"id": "PIMEXPTEST-CATGROUP", "code": "PIMEXPTEST-CATGROUP", "name": "Shared Group",
		"group_type": "Static", "members": `[{"item_code":"PIMEXPTEST-CATITEM"}]`,
	})
	pimInsertDoc(t, schema, "PIMEXPTEST-CATALOG", "PIMCatalog", "Active", map[string]interface{}{
		"id": "PIMEXPTEST-CATALOG", "code": "PIMEXPTEST-CATALOG", "name": "Test Catalog",
		"product_group": "PIMEXPTEST-CATGROUP",
	})

	token, err := RotatePIMCatalogShareToken("default", "PIMEXPTEST-CATALOG")
	if err != nil {
		t.Fatalf("unexpected error minting a share token: %v", err)
	}
	if token == "" {
		t.Fatal("expected a non-empty raw token")
	}

	view, err := ResolvePIMCatalogShareToken("default", token)
	if err != nil {
		t.Fatalf("unexpected error resolving a fresh token: %v", err)
	}
	if view.CatalogName != "Test Catalog" || len(view.Products) != 1 || view.Products[0].ItemCode != "PIMEXPTEST-CATITEM" {
		t.Fatalf("unexpected share view: %+v", view)
	}

	if _, err := ResolvePIMCatalogShareToken("default", "not-the-real-token"); err == nil {
		t.Fatal("expected a wrong token to be refused")
	}

	// Rotating invalidates the old token immediately.
	newToken, err := RotatePIMCatalogShareToken("default", "PIMEXPTEST-CATALOG")
	if err != nil {
		t.Fatalf("unexpected error rotating: %v", err)
	}
	if _, err := ResolvePIMCatalogShareToken("default", token); err == nil {
		t.Fatal("expected the pre-rotation token to be refused after rotation")
	}
	if _, err := ResolvePIMCatalogShareToken("default", newToken); err != nil {
		t.Fatalf("unexpected error resolving the rotated token: %v", err)
	}

	// Deactivating the catalog revokes access even with a valid token.
	_, _ = db.DB.Exec("UPDATE "+schema+".documents SET status = 'Inactive' WHERE id = $1", "PIMEXPTEST-CATALOG")
	if _, err := ResolvePIMCatalogShareToken("default", newToken); err == nil {
		t.Fatal("expected a deactivated catalog's token to be refused")
	}
	_, _ = db.DB.Exec("UPDATE "+schema+".documents SET status = 'Active' WHERE id = $1", "PIMEXPTEST-CATALOG")

	// An expired catalog also refuses, even while Active.
	_, err = db.DB.Exec("UPDATE "+schema+".documents SET data = jsonb_set(data, '{expiry_date}', '\"2000-01-01\"') WHERE id = $1", "PIMEXPTEST-CATALOG")
	if err != nil {
		t.Fatalf("unexpected error setting an expiry date: %v", err)
	}
	if _, err := ResolvePIMCatalogShareToken("default", newToken); err == nil {
		t.Fatal("expected an expired catalog's token to be refused")
	}
}
