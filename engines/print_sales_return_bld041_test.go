package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// BLD-041: a real return stores its lines as a JSON array (returns.go sets
// data["items"] = items), not as text. The Sales Return print read them as
// text and printed every customer return with no lines and a 0.00 refund; the
// older test passed only because it seeded the lines as a string.
func TestBLD041SalesReturnPrintsLinesStoredAsAnArray(t *testing.T) {
	db.InitDB(testConnStr())
	id := "BLD041-RET-" + stage4911Suffix()
	data, _ := json.Marshal(map[string]interface{}{
		"code": id, "request_type": "Customer Return", "return_location": "HO",
		"items": []map[string]interface{}{{"sku": "BLD041-SKU", "qty": 2, "original_unit_price": 59.0, "disposition": "Sellable"}},
	})
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'ReturnRequest', $2, 'Received', 'admin')`, id, data); err != nil {
		t.Fatal(err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)

	d, err := BuildPrintDocument("default", PrintTxnSalesReturn, id, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Lines) != 1 || d.Lines[0]["return_qty"] != 2.0 {
		t.Fatalf("a return with one line printed %d lines: %v", len(d.Lines), d.Lines)
	}
	if d.Totals["grand_total"] != 118 {
		t.Errorf("refund value printed as %v, want 118 (2 x 59.00)", d.Totals["grand_total"])
	}
}
