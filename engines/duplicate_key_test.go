package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// TestStage140141DuplicateImportIsAReadableRefusal: the CSV import path
// reaches the same unique index; its row error names the fields, not the
// driver's text.
func TestStage140141DuplicateImportIsAReadableRefusal(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	clean := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ReorderPointConfig' AND data->>'item' = 'S140141I-ITEM'")
	}
	clean()
	defer clean()
	csv := "code,item,location_code,lead_time_days,safety_stock_qty,status\n" +
		"S140141I-A,S140141I-ITEM,S140141I-LOC,2,5,Active\n" +
		"S140141I-B,S140141I-ITEM,S140141I-LOC,2,5,Active\n"
	res, err := BulkImportCSV(tenantID, "ReorderPointConfig", strings.NewReader(csv), "admin", RoleSuperAdmin, false)
	if err != nil {
		t.Fatalf("import returned a raw error: %v", err)
	}
	var all []string
	for _, e := range res.Errors {
		all = append(all, e.Message)
	}
	joined := strings.Join(all, " | ")
	if res.FailedRows == 0 || !strings.Contains(joined, "already exists") || strings.Contains(joined, "pq:") || strings.Contains(joined, "duplicate key") {
		t.Fatalf("import: failed=%d errors=%q, want a readable already-exists message", res.FailedRows, joined)
	}
	if !strings.Contains(joined, "Item") {
		t.Errorf("import error does not name the fields: %q", joined)
	}
}

func TestStage140141DuplicateKeyFieldsFromDetail(t *testing.T) {
	got := duplicateKeyFields(`Key (upper((data ->> 'item'::text)), upper((data ->> 'location_code'::text)))=(A, B) already exists.`)
	if len(got) != 2 || got[0] != "item" || got[1] != "location_code" {
		t.Fatalf("fields = %v", got)
	}
	if g := duplicateKeyFields(`Key (email)=(x@y) already exists.`); len(g) != 1 || g[0] != "email" {
		t.Fatalf("plain column = %v", g)
	}
}
