package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 146.5.12: typed product relationships (similar, complete the look,
// alternative, upsell, cross-sell, add-on), checked at ValidateDocument.

func s146512Setup(t *testing.T) string {
	t.Helper()
	schema, _, cleanup := pimTestFixture(t, "S146512")
	t.Cleanup(cleanup)
	pimInsertDoc(t, schema, "S146512-B", "Item", "Active", map[string]interface{}{"code": "S146512-B", "name": "b"})
	pimInsertDoc(t, schema, "S146512-C", "Item", "Active", map[string]interface{}{"code": "S146512-C", "name": "c"})
	return schema
}

func s146512Rel(item, other, typ string) map[string]interface{} {
	return map[string]interface{}{"item": item, "related_item": other, "relation_type": typ, "status": "Active"}
}

func TestStage146512RelationRules(t *testing.T) {
	schema := s146512Setup(t)
	if err := ValidateDocument("default", "ProductRelation", s146512Rel("S146512-ITEM", "S146512-B", "Upsell")); err != nil {
		t.Fatalf("valid relation refused: %v", err)
	}
	err := ValidateDocument("default", "ProductRelation", s146512Rel("S146512-B", "S146512-B", "Similar"))
	if ve, ok := err.(*ValidationError); !ok || ve.Code != "PIM-0981" {
		t.Fatalf("self link: want PIM-0981, got %v", err)
	}
	err = ValidateDocument("default", "ProductRelation", s146512Rel("S146512-ITEM", "S146512-B", "Wrong"))
	if err == nil {
		t.Fatal("unknown type passed")
	}
	pimInsertDoc(t, schema, "S146512-REL1", "ProductRelation", "Active", s146512Rel("S146512-ITEM", "S146512-B", "Upsell"))
	err = ValidateDocument("default", "ProductRelation", s146512Rel("S146512-ITEM", "S146512-B", "Upsell"))
	if ve, ok := err.(*ValidationError); !ok || ve.Code != "PIM-0982" {
		t.Fatalf("duplicate: want PIM-0982, got %v", err)
	}
	// the same pair under another type, and the other way round, are different relations
	for _, r := range []map[string]interface{}{s146512Rel("S146512-ITEM", "S146512-B", "Add-on"), s146512Rel("S146512-B", "S146512-ITEM", "Upsell")} {
		if err := ValidateDocument("default", "ProductRelation", r); err != nil {
			t.Errorf("%v: %v", r, err)
		}
	}
	// saving the existing row again is not a duplicate of itself
	own := s146512Rel("S146512-ITEM", "S146512-B", "Upsell")
	own["id"] = "S146512-REL1"
	if err := ValidateDocument("default", "ProductRelation", own); err != nil {
		t.Errorf("re-save of own row: %v", err)
	}
}

func TestStage146512ImportThroughBulkCSV(t *testing.T) {
	schema := s146512Setup(t)
	csv := "item,related_item,relation_type\nS146512-ITEM,S146512-B,Similar\nS146512-ITEM,S146512-C,Cross-sell\nS146512-ITEM,S146512-ITEM,Similar\n"
	res, err := BulkImportCSV("default", "ProductRelation", strings.NewReader(csv), "system", "HR/Admin", false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.documents WHERE doctype='ProductRelation' AND data->>'item'='S146512-ITEM' AND deleted_at IS NULL`).Scan(&n)
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.documents WHERE doctype='ProductRelation' AND data->>'item'='S146512-ITEM'`)
	})
	if n != 2 {
		t.Fatalf("stored %d relations, want 2 (self-link and duplicate refused): %+v", n, res)
	}
}

func TestStage146512ImportRefusesARelationThatExists(t *testing.T) {
	schema := s146512Setup(t)
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.documents WHERE doctype='ProductRelation' AND data->>'item'='S146512-ITEM'`)
	})
	csv := "item,related_item,relation_type\nS146512-ITEM,S146512-B,Similar\n"
	if res, err := BulkImportCSV("default", "ProductRelation", strings.NewReader(csv), "system", "HR/Admin", false); err != nil || res.FailedRows != 0 {
		t.Fatalf("first import: %v %+v", err, res)
	}
	res, err := BulkImportCSV("default", "ProductRelation", strings.NewReader(csv), "system", "HR/Admin", false)
	if err != nil || res.FailedRows != 1 {
		t.Fatalf("second import should refuse the repeat: %v %+v", err, res)
	}
}
