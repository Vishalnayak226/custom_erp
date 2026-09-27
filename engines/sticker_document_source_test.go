package engines

import (
	"custom_erp/db"
	"encoding/json"
	"sort"
	"testing"
)

// Shared test scaffolding, same shape as costing_test.go's insert/itemsJSON
// helpers: direct document_table inserts, since these tests exercise
// engines-layer resolution logic, not the generic doc-API's own validation.
func stickerTestInsert(t *testing.T, schema, id, doctype string, data map[string]interface{}) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal %s %s: %v", doctype, id, err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
		t.Fatalf("insert %s %s: %v", doctype, id, err)
	}
}

func stickerTestCleanup(schema string, ids []string) {
	for _, id := range ids {
		db.DB.Exec("DELETE FROM " + schema + ".sticker_print_log WHERE source_doc_id = '" + id + "'")
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
	}
}

func TestResolveDocumentStickerLinesGRN(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const grnID = "TEST52-GRN-LINES"
	ids := []string{grnID}
	stickerTestCleanup(schema, ids)
	defer stickerTestCleanup(schema, ids)

	receivedJSON, _ := json.Marshal([]map[string]interface{}{
		// Fully accepted line.
		{"sku": "TEST52-SKU-A", "qty": 10, "accepted_qty": 10},
		// Partially rejected/damaged - accepted_qty must derive to 10-2-1=7.
		{"sku": "TEST52-SKU-B", "qty": 10, "rejected_qty": 2, "damaged_qty": 1},
		// Fully rejected - contributes zero stickers, must not appear at all.
		{"sku": "TEST52-SKU-C", "qty": 5, "rejected_qty": 5},
		// Same SKU as A, different batch - must stay a separate line, not merge.
		{"sku": "TEST52-SKU-A", "qty": 3, "accepted_qty": 3, "batch_no": "LOT-2"},
	})
	stickerTestInsert(t, schema, grnID, "GRN", map[string]interface{}{
		"code": grnID, "location": "TEST52-LOC", "received_items": string(receivedJSON),
	})

	lines, err := ResolveDocumentStickerLines(tenantID, "GRN", grnID)
	if err != nil {
		t.Fatalf("ResolveDocumentStickerLines: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (SKU-C fully rejected excluded, SKU-A split by batch), got %d: %+v", len(lines), lines)
	}
	byKey := map[string]DocStickerLine{}
	for _, l := range lines {
		byKey[l.SKU+"|"+l.BatchNo] = l
	}
	if l, ok := byKey["TEST52-SKU-A|"]; !ok || l.Qty != 10 {
		t.Errorf("expected SKU-A (no batch) qty=10, got %+v (ok=%v)", l, ok)
	}
	if l, ok := byKey["TEST52-SKU-A|LOT-2"]; !ok || l.Qty != 3 {
		t.Errorf("expected SKU-A/LOT-2 qty=3, got %+v (ok=%v)", l, ok)
	}
	if l, ok := byKey["TEST52-SKU-B|"]; !ok || l.Qty != 7 {
		t.Errorf("expected SKU-B derived accepted qty=7 (10-2-1), got %+v (ok=%v)", l, ok)
	}
	if _, ok := byKey["TEST52-SKU-C|"]; ok {
		t.Error("fully-rejected SKU-C should not appear in stickerable lines")
	}
}

func TestResolveDocumentStickerLinesTransferOrder(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const toID = "TEST52-TO-LINES"
	stickerTestCleanup(schema, []string{toID})
	defer stickerTestCleanup(schema, []string{toID})

	itemsJSON, _ := json.Marshal([]map[string]interface{}{
		{"sku": "TEST52-SKU-A", "qty": 4},
		{"sku": "TEST52-SKU-D", "qty": 6},
	})
	stickerTestInsert(t, schema, toID, "TransferOrder", map[string]interface{}{
		"transfer_number": toID, "items": string(itemsJSON),
	})

	lines, err := ResolveDocumentStickerLines(tenantID, "TransferOrder", toID)
	if err != nil {
		t.Fatalf("ResolveDocumentStickerLines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %+v", len(lines), lines)
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].SKU < lines[j].SKU })
	if lines[0].SKU != "TEST52-SKU-A" || lines[0].Qty != 4 {
		t.Errorf("unexpected first line: %+v", lines[0])
	}
	if lines[1].SKU != "TEST52-SKU-D" || lines[1].Qty != 6 {
		t.Errorf("unexpected second line: %+v", lines[1])
	}
}

func TestResolveDocumentStickerLinesUnsupportedDoctype(t *testing.T) {
	if _, err := ResolveDocumentStickerLines("default", "SalesOrder", "whatever"); err == nil {
		t.Fatal("expected an error for an unsupported source doctype")
	}
}

func TestResolveStickerTemplateCategoryMatchAndDefaultFallback(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const (
		earringTemplateID = "TEST52-TMPL-EARRING"
		defaultTemplateID = "TEST52-TMPL-DEFAULT"
	)
	ids := []string{earringTemplateID, defaultTemplateID}
	stickerTestCleanup(schema, ids)
	defer stickerTestCleanup(schema, ids)

	elements, _ := json.Marshal([]map[string]interface{}{
		{"id": "e1", "field": "name", "x_mm": 2, "y_mm": 2, "w_mm": 30, "h_mm": 6},
	})
	stickerTestInsert(t, schema, earringTemplateID, "StickerTemplate", map[string]interface{}{
		"code": earringTemplateID, "name": "Earring Tag", "categories": "Earrings, Studs",
		"label_width_mm": 40, "label_height_mm": 25, "elements": string(elements), "status": "Active",
	})
	stickerTestInsert(t, schema, defaultTemplateID, "StickerTemplate", map[string]interface{}{
		"code": defaultTemplateID, "name": "Fallback Tag", "is_default": true,
		"label_width_mm": 50, "label_height_mm": 30, "elements": string(elements), "status": "Active",
	})

	// Exact category match (case-insensitive, trimmed).
	t.Run("category match", func(t *testing.T) {
		tmpl, err := ResolveStickerTemplate(tenantID, "  earrings ")
		if err != nil {
			t.Fatalf("ResolveStickerTemplate: %v", err)
		}
		if tmpl == nil || tmpl.ID != earringTemplateID {
			t.Fatalf("expected the Earring Tag template, got %+v", tmpl)
		}
	})

	// No match on category -> falls back to the is_default template.
	t.Run("default fallback", func(t *testing.T) {
		tmpl, err := ResolveStickerTemplate(tenantID, "Necklaces")
		if err != nil {
			t.Fatalf("ResolveStickerTemplate: %v", err)
		}
		if tmpl == nil || tmpl.ID != defaultTemplateID {
			t.Fatalf("expected the default template, got %+v", tmpl)
		}
	})

	// No match and (temporarily) no default -> nil, not an error - this is
	// the pre-Stage-52-compatible "use the hardcoded layout" signal.
	t.Run("nil when nothing resolves", func(t *testing.T) {
		stickerTestCleanup(schema, []string{defaultTemplateID})
		defer stickerTestInsert(t, schema, defaultTemplateID, "StickerTemplate", map[string]interface{}{
			"code": defaultTemplateID, "name": "Fallback Tag", "is_default": true,
			"label_width_mm": 50, "label_height_mm": 30, "elements": string(elements), "status": "Active",
		})
		tmpl, err := ResolveStickerTemplate(tenantID, "Necklaces")
		if err != nil {
			t.Fatalf("ResolveStickerTemplate: %v", err)
		}
		if tmpl != nil {
			t.Fatalf("expected nil when no category matches and no default exists, got %+v", tmpl)
		}
	})
}

func TestPrintStickersForDocumentGroupsByCategoryAndLogsSource(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const (
		earringSKU     = "TEST52-EARRING-SKU"
		necklaceSKU    = "TEST52-NECKLACE-SKU"
		grnID          = "TEST52-GRN-PRINT"
		printerCode    = "TEST52-PRINTER"
		earringTmplID  = "TEST52-TMPL-EARRING-2"
		necklaceTmplID = "TEST52-TMPL-NECKLACE-2"
	)
	ids := []string{earringSKU, necklaceSKU, grnID, printerCode, earringTmplID, necklaceTmplID}
	stickerTestCleanup(schema, ids)
	defer stickerTestCleanup(schema, ids)

	stickerTestInsert(t, schema, earringSKU, "Item", map[string]interface{}{"code": earringSKU, "name": "Gold Earring", "category": "Earrings", "hsn_code": "7113"})
	stickerTestInsert(t, schema, necklaceSKU, "Item", map[string]interface{}{"code": necklaceSKU, "name": "Gold Necklace", "category": "Necklace", "hsn_code": "7113"})
	stickerTestInsert(t, schema, printerCode, "Printer", map[string]interface{}{"code": printerCode, "name": "Test Printer", "status": "Active", "printer_language": "ZPL"})

	elements, _ := json.Marshal([]map[string]interface{}{{"id": "e1", "field": "name", "x_mm": 2, "y_mm": 2, "w_mm": 30, "h_mm": 6}})
	stickerTestInsert(t, schema, earringTmplID, "StickerTemplate", map[string]interface{}{
		"code": earringTmplID, "name": "Earring Tag", "categories": "Earrings",
		"label_width_mm": 40, "label_height_mm": 25, "elements": string(elements), "status": "Active",
	})
	stickerTestInsert(t, schema, necklaceTmplID, "StickerTemplate", map[string]interface{}{
		"code": necklaceTmplID, "name": "Necklace Tag", "categories": "Necklace",
		"label_width_mm": 60, "label_height_mm": 40, "elements": string(elements), "status": "Active",
	})

	receivedJSON, _ := json.Marshal([]map[string]interface{}{
		// Necklace line listed first on the GRN, to prove the returned labels
		// are re-sorted by category rather than left in received_items order.
		{"sku": necklaceSKU, "qty": 2, "accepted_qty": 2},
		{"sku": earringSKU, "qty": 5, "accepted_qty": 5},
	})
	stickerTestInsert(t, schema, grnID, "GRN", map[string]interface{}{
		"code": grnID, "location": "TEST52-LOC", "received_items": string(receivedJSON),
	})

	labels, err := PrintStickersForDocument(tenantID, "GRN", grnID, printerCode, "system", "", nil, nil)
	if err != nil {
		t.Fatalf("PrintStickersForDocument: %v", err)
	}
	if len(labels) != 2 {
		t.Fatalf("expected 2 labels, got %d: %+v", len(labels), labels)
	}
	if labels[0].Category != "Earrings" || labels[0].TemplateID != earringTmplID {
		t.Errorf("expected Earrings first (alphabetical) with the Earring template, got %+v", labels[0])
	}
	if labels[1].Category != "Necklace" || labels[1].TemplateID != necklaceTmplID {
		t.Errorf("expected Necklace second with the Necklace template, got %+v", labels[1])
	}
	if labels[0].Qty != 5 {
		t.Errorf("expected earring label Qty=5 (accepted_qty), got %d", labels[0].Qty)
	}
	if labels[1].Qty != 2 {
		t.Errorf("expected necklace label Qty=2 (accepted_qty), got %d", labels[1].Qty)
	}

	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".sticker_print_log WHERE source_doctype = 'GRN' AND source_doc_id = $1", grnID).Scan(&count); err != nil {
		t.Fatalf("query sticker_print_log: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 sticker_print_log rows tagged with this GRN, got %d", count)
	}

	// Selecting a single SKU is the "print one line" path - same function,
	// a length-1 selection.
	single, err := PrintStickersForDocument(tenantID, "GRN", grnID, printerCode, "system", "", []string{earringSKU}, nil)
	if err != nil {
		t.Fatalf("PrintStickersForDocument (single SKU): %v", err)
	}
	if len(single) != 1 || single[0].SKU != earringSKU {
		t.Fatalf("expected exactly the earring line, got %+v", single)
	}

	// copyOverrides takes precedence over the document's own accepted qty.
	overridden, err := PrintStickersForDocument(tenantID, "GRN", grnID, printerCode, "system", "", []string{earringSKU}, map[string]int{earringSKU: 9})
	if err != nil {
		t.Fatalf("PrintStickersForDocument (override): %v", err)
	}
	if len(overridden) != 1 || overridden[0].Qty != 9 {
		t.Fatalf("expected the copy override (9) to win over accepted_qty (5), got %+v", overridden)
	}
}
