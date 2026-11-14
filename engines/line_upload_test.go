package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
)

// 85.E8 (77.14): an uploaded code resolves to its item by id, code or
// barcode; a code that is no item comes back not found, in its place; and a
// purchase order's row is checked against the item's design number - the
// design's number, else its code, else the reference the item holds.
func TestStage85E8LineCodesCarryTheDesignNumber(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() { db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id LIKE 'T85E8-%%'`, schema)) }
	cleanup()
	t.Cleanup(cleanup)
	for _, d := range []struct{ id, doctype, data string }{
		{"T85E8-FAM", "ProductFamily", `{"code": "T85E8-FAMC", "design_number": "DN-85E8"}`},
		{"T85E8-FAM2", "ProductFamily", `{"code": "T85E8-FAM2C"}`},
		{"T85E8-A", "Item", `{"code": "T85E8-A", "barcode": "T85E8-BC", "name": "A", "family": "T85E8-FAM"}`},
		{"T85E8-B", "Item", `{"code": "T85E8-B", "name": "B", "parent_product_code": "T85E8-FAMC"}`},
		{"T85E8-C", "Item", `{"code": "T85E8-C", "name": "C", "family": "T85E8-FAM2"}`},
		{"T85E8-D", "Item", `{"code": "T85E8-D", "name": "D", "family": "T85E8-GONE"}`},
		{"T85E8-E", "Item", `{"code": "T85E8-E", "name": "E"}`},
	} {
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3::jsonb, 'Active', 'admin')`, schema), d.id, d.doctype, d.data); err != nil {
			t.Fatalf("seed %s: %v", d.id, err)
		}
	}

	got, err := ResolveLineCodes(tenantID, []string{"T85E8-BC", "T85E8-B", "NO-SUCH-85E8", "T85E8-C", "T85E8-D", "T85E8-E"})
	if err != nil {
		t.Fatal(err)
	}
	want := []ResolvedLineCode{
		{Code: "T85E8-BC", SKU: "T85E8-A", Name: "A", Found: true, Design: "T85E8-FAM", DesignNumber: "DN-85E8"},
		{Code: "T85E8-B", SKU: "T85E8-B", Name: "B", Found: true, Design: "T85E8-FAM", DesignNumber: "DN-85E8"},
		{Code: "NO-SUCH-85E8"},
		{Code: "T85E8-C", SKU: "T85E8-C", Name: "C", Found: true, Design: "T85E8-FAM2", DesignNumber: "T85E8-FAM2C"},
		{Code: "T85E8-D", SKU: "T85E8-D", Name: "D", Found: true, Design: "T85E8-GONE", DesignNumber: "T85E8-GONE"},
		{Code: "T85E8-E", SKU: "T85E8-E", Name: "E", Found: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
