package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// TestStage87POLineAttributes: a PO line's attributes are its design's values
// overlaid by its own combination values; the preview and the print both name
// one column per attribute, in the tenant's attribute order with any unknown
// code last, and key every line by the attribute's own code.
func TestStage87POLineAttributes(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	const size, metal, design = "TEST87-SIZE", "TEST87-METAL", "TEST87-DESIGN"
	ids := []string{size, metal, design, design + "-S", design + "-M"}
	cleanup := func() {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	cleanup()
	defer cleanup()
	ins := func(id, doctype string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	ins(size, "ItemAttribute", map[string]interface{}{"code": size, "label": "Test87 Size", "value_type": "Select", "value_options": "S,M", "status": "Active"})
	ins(metal, "ItemAttribute", map[string]interface{}{"code": metal, "label": "Test87 Metal", "value_type": "Select", "value_options": "Gold,Silver", "status": "Active"})
	// The design's attribute is stored with a lower-case key; the line must
	// still land under the attribute's own code.
	ins(design, "ProductFamily", map[string]interface{}{"code": design, "name": "Stage 87 Ring", "design_attributes": "test87-metal:Gold", "hsn_code": "7113", "gst_rate": 3})
	ins(design+"-S", "Item", map[string]interface{}{"code": design + "-S", "name": "Ring S", "family": design, "parent_product_code": design,
		"variant_option_values": size + ":S;test87_finish:Matte", "hsn_code": "7113", "gst_rate": 3})
	ins(design+"-M", "Item", map[string]interface{}{"code": design + "-M", "name": "Ring M", "family": design, "parent_product_code": design,
		"variant_option_values": size + ":M", "hsn_code": "7113", "gst_rate": 3})

	items, _ := json.Marshal([]POLineInput{{SKU: design + "-S", Qty: 2, Rate: 100}, {SKU: design + "-M", Qty: 1, Rate: 100}})
	p, err := PreviewPurchaseOrder(tenantID, map[string]interface{}{"items": string(items)})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	checkColumns := func(where string, cols []AttributeColumn) {
		t.Helper()
		pos := map[string]int{}
		for i, c := range cols {
			pos[c.Code] = i + 1
		}
		if pos[metal] == 0 || pos[size] == 0 || pos["test87_finish"] == 0 {
			t.Fatalf("%s: columns %+v miss one of %s / %s / test87_finish", where, cols, metal, size)
		}
		// Select attributes by label (Metal before Size); an unknown code last.
		if !(pos[metal] < pos[size] && pos[size] < pos["test87_finish"]) || pos["test87_finish"] != len(cols) {
			t.Fatalf("%s: column order %+v", where, cols)
		}
		if cols[pos["test87_finish"]-1].Label != "Test87 finish" {
			t.Fatalf("%s: an unknown code is labelled from its code, got %q", where, cols[pos["test87_finish"]-1].Label)
		}
	}
	checkColumns("preview", p.AttributeColumns)
	if a := p.Lines[0].Attributes; a[metal] != "Gold" || a[size] != "S" || a["test87_finish"] != "Matte" {
		t.Fatalf("line 1 attributes: %v", a)
	}
	if a := p.Lines[1].Attributes; a[metal] != "Gold" || a[size] != "M" || a["test87_finish"] != "" {
		t.Fatalf("line 2 attributes: %v", a)
	}

	d := &PrintDocument{}
	cache := map[string]map[string]interface{}{}
	d.addLine(tenantID, printLineIn{SKU: design + "-S", Qty: 2, Rate: 100}, cache, false)
	d.addLine(tenantID, printLineIn{SKU: design + "-M", Qty: 1, Rate: 100}, cache, false)
	d.orderAttributeColumns(tenantID)
	checkColumns("print", d.AttributeColumns)
	if a, _ := d.Lines[1]["attributes"].(map[string]string); a[metal] != "Gold" || a[size] != "M" {
		t.Fatalf("print line 2 attributes: %v", d.Lines[1]["attributes"])
	}
}
