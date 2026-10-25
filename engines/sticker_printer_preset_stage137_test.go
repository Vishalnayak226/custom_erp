package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

func TestStage137RollShapeParsing(t *testing.T) {
	v3 := parseStickerRollShape("46.9", "14.3", `{"v":2,"settings":{"per_row":"3","per_col":1,"direction":"vertical","sheet":{"mode":"roll"}}}`)
	if v3 != (stickerRollShape{W: 46.9, H: 14.3, PerRow: 3, PerCol: 1, Vertical: true}) {
		t.Fatalf("3-across vertical = %+v", v3)
	}
	if got := parseStickerRollShape("", "0", ""); got != builtInStickerShape {
		t.Fatalf("a blank template reads as the engine's 50 x 25 default, got %+v", got)
	}
	if got := parseStickerRollShape("50", "25", `{"settings":{"per_row":40}}`); got.PerRow != 10 {
		t.Fatalf("labels across are capped at 10 as in normaliseSettings, got %d", got.PerRow)
	}
	if got := parseStickerRollShape("38", "25", `not json`); got != (stickerRollShape{W: 38, H: 25, PerRow: 1, PerCol: 1}) {
		t.Fatalf("an unreadable design keeps the size and one across, got %+v", got)
	}
	if !v3.fits(stickerRollShape{W: 46.92, H: 14.3, PerRow: 3, PerCol: 1, Vertical: true}) {
		t.Fatal("a size within 0.05 mm fits")
	}
	for _, other := range []stickerRollShape{
		{W: 100, H: 14.3, PerRow: 1, PerCol: 1},
		{W: 46.9, H: 14.3, PerRow: 1, PerCol: 1, Vertical: true},
		{W: 46.9, H: 14.3, PerRow: 3, PerCol: 1},
		{W: 46.9, H: 14.3, PerRow: 3, PerCol: 1, Vertical: true, Sheet: true},
	} {
		if v3.fits(other) {
			t.Fatalf("%+v must not fit the 3-across vertical roll", other)
		}
	}
}

func TestStage137PrinterRollWins(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	vertical3 := `{"v":2,"settings":{"per_row":3,"per_col":1,"direction":"vertical"}}`
	docs := []struct {
		id, doctype string
		data        map[string]interface{}
	}{
		{"TPL-H1", "StickerTemplate", map[string]interface{}{"code": "TPL-H1", "name": "Tag 100 x 14.3", "status": "Active", "is_default": true,
			"label_width_mm": 100, "label_height_mm": 14.3, "elements": "[]", "design": `{"v":2,"settings":{"per_row":1}}`}},
		{"TPL-V3", "StickerTemplate", map[string]interface{}{"code": "TPL-V3", "name": "Vertical x 3", "status": "Active",
			"label_width_mm": 46.9, "label_height_mm": 14.3, "elements": "[]", "design": vertical3}},
		{"TPL-V3B", "StickerTemplate", map[string]interface{}{"code": "TPL-V3B", "name": "Vertical x 3 ring", "status": "Active",
			"label_width_mm": 46.9, "label_height_mm": 14.3, "elements": "[]", "design": vertical3}},
		{"TPL-OLD", "StickerTemplate", map[string]interface{}{"code": "TPL-OLD", "name": "Retired roll", "status": "Inactive",
			"label_width_mm": 46.9, "label_height_mm": 14.3, "elements": "[]", "design": vertical3}},
		{"PRN-ROLL", "Printer", map[string]interface{}{"code": "PRN-ROLL", "name": "Jewellery", "status": "Active", "sticker_template": "TPL-V3"}},
		{"PRN-PLAIN", "Printer", map[string]interface{}{"code": "PRN-PLAIN", "name": "Plain", "status": "Active"}},
		{"PRN-OLD", "Printer", map[string]interface{}{"code": "PRN-OLD", "name": "Old roll", "status": "Active", "sticker_template": "TPL-OLD"}},
		{"ITEM-137-1", "Item", map[string]interface{}{"code": "S137-1", "name": "No rule"}},
		{"ITEM-137-2", "Item", map[string]interface{}{"code": "S137-2", "name": "Own ring design", "sticker_template": "TPL-V3B"}},
	}
	for _, d := range docs {
		raw, _ := json.Marshal(d.data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", d.id, d.doctype, raw); err != nil {
			t.Fatal(err)
		}
	}
	preview := StickerPrintOptions{Preview: true}
	templateOf := func(printer, sku string) string {
		t.Helper()
		labels, err := PrintStickers(tenantID, []string{sku}, printer, "admin", "", 1, preview)
		if err != nil || len(labels) != 1 {
			t.Fatalf("print %s on %s: %v %v", sku, printer, labels, err)
		}
		return labels[0].TemplateID
	}
	if got := templateOf("PRN-PLAIN", "S137-1"); got != "TPL-H1" {
		t.Fatalf("a printer with no preset follows the rules (Default), got %q", got)
	}
	if got := templateOf("PRN-ROLL", "S137-1"); got != "TPL-V3" {
		t.Fatalf("the Default's 1-across tag does not fit the 3-across roll: want the roll's TPL-V3, got %q", got)
	}
	if got := templateOf("PRN-ROLL", "S137-2"); got != "TPL-V3B" {
		t.Fatalf("an item design of the roll's own shape is kept, got %q", got)
	}
	if got := templateOf("PRN-OLD", "S137-1"); got != "TPL-H1" {
		t.Fatalf("a preset naming an Inactive template is no preset, got %q", got)
	}
	label, err := resolveStickerLabel(tenantID, schema, "S137-2", "PRN-ROLL", "", "", "", "", "TPL-H1")
	if err != nil || label.TemplateID != "TPL-V3" {
		t.Fatalf("a hand-picked template that does not fit the roll gives way to it: %q %v", label.TemplateID, err)
	}
	if label, err := resolveStickerLabel(tenantID, schema, "S137-1", "PRN-ROLL", "", "", "", "", StickerTemplateStandard); err != nil || label.TemplateID != "TPL-V3" {
		t.Fatalf("the built-in 50 x 25 label does not fit the roll either: %q %v", label.TemplateID, err)
	}
	printers, err := ListQZPrinters(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range printers {
		if p.Code == "PRN-ROLL" && p.StickerTemplate != "TPL-V3" {
			t.Fatalf("print routing carries the preset to the browser, got %+v", p)
		}
	}
}
