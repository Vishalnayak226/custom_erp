package engines

import (
	"bytes"
	"custom_erp/db"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Stage 47.13.2a / 49.3.4b: an import update merges into the stored record.
// A blank cell keeps the stored value, keys that are not template columns
// (bom_snapshot, gst_breakdown, ...) survive, only ImportClearMarker clears a
// field, and an insert is what it always was.

func importedDoc(t *testing.T, id string) map[string]interface{} {
	t.Helper()
	var raw []byte
	if err := db.DB.QueryRow(`SELECT data FROM tenant_default.documents WHERE id = $1`, id).Scan(&raw); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	out := map[string]interface{}{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func importCSV(t *testing.T, doctype, body string) *ImportResult {
	t.Helper()
	res, err := BulkImportCSV("default", doctype, strings.NewReader(body), "admin", "", false)
	if err != nil {
		t.Fatalf("import %s: %v", doctype, err)
	}
	return res
}

func TestStage4713aImportUpdateKeepsWhatABlankCellLeavesOut(t *testing.T) {
	db.InitDB(testConnStr())
	n := time.Now().UnixNano() % 1_000_000_000
	v, w := fmt.Sprintf("T4713A-V%d", n), fmt.Sprintf("T4713A-W%d", n)
	t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id IN ($1, $2)`, v, w) })

	// An insert is unchanged: every cell as given, a blank cell stored blank,
	// and the marker - nothing to clear on a new record - stored blank too.
	res := importCSV(t, "Vendor", "code,name,status,state,city,contact_phone\n"+
		v+",Acme Traders,Active,Karnataka,Bengaluru,9876500010\n"+
		w+",Blank Traders,Active,Karnataka,,<clear>\n")
	if res.SuccessRows != 2 || len(res.CreatedIDs) != 2 {
		t.Fatalf("insert: %d ok, created %v, errors %+v", res.SuccessRows, res.CreatedIDs, res.Errors)
	}
	if got := importedDoc(t, w); got["city"] != "" || got["contact_phone"] != "" || got["name"] != "Blank Traders" {
		t.Errorf("inserted with a blank cell and the marker: city %q, phone %q, name %q", got["city"], got["contact_phone"], got["name"])
	}

	// What the server keeps on a record, which no template column carries.
	if _, err := db.DB.Exec(`UPDATE tenant_default.documents SET data = data || '{"gst_breakdown":{"total_tax":18,"cgst":9,"sgst":9},"bom_snapshot":"[{\"sku\":\"S1\",\"qty\":2}]"}'::jsonb WHERE id = $1`, v); err != nil {
		t.Fatal(err)
	}

	// An update: blank cells keep, the marker clears, a filled cell wins.
	res = importCSV(t, "Vendor", "id,code,name,status,state,city,contact_phone\n"+v+","+v+",,,,<clear>,9876500011\n")
	if res.SuccessRows != 1 || len(res.UpdatedIDs) != 1 {
		t.Fatalf("update: %d ok, updated %v, errors %+v", res.SuccessRows, res.UpdatedIDs, res.Errors)
	}
	got := importedDoc(t, v)
	if got["name"] != "Acme Traders" || got["state"] != "Karnataka" {
		t.Errorf("blank cells did not keep the stored values: name %q, state %q", got["name"], got["state"])
	}
	if got["city"] != "" {
		t.Errorf("%s did not clear the city: %q", ImportClearMarker, got["city"])
	}
	if phone, _ := got["contact_phone"].(string); !strings.Contains(phone, "9876500011") {
		t.Errorf("a filled cell did not replace the phone: %q", phone)
	}
	if gb, ok := got["gst_breakdown"].(map[string]interface{}); !ok || gb["total_tax"] != 18.0 {
		t.Errorf("gst_breakdown did not survive the update: %v", got["gst_breakdown"])
	}
	if got["bom_snapshot"] != `[{"sku":"S1","qty":2}]` {
		t.Errorf("bom_snapshot did not survive the update: %v", got["bom_snapshot"])
	}

	// A master named by its code alone is updated the same way.
	res = importCSV(t, "Vendor", "code,name,status,state,city\n"+v+",,,,Mysuru\n")
	if res.SuccessRows != 1 || len(res.UpdatedIDs) != 1 {
		t.Fatalf("update by code: %d ok, updated %v, errors %+v", res.SuccessRows, res.UpdatedIDs, res.Errors)
	}
	if got := importedDoc(t, v); got["city"] != "Mysuru" || got["name"] != "Acme Traders" || got["gst_breakdown"] == nil {
		t.Errorf("update by code: %v", got)
	}

	// A required field cannot be cleared: the row is refused, the record kept.
	res = importCSV(t, "Vendor", "id,code,name,status,state\n"+v+","+v+",<clear>,,\n")
	if res.FailedRows != 1 || len(res.Errors) == 0 || !strings.Contains(res.Errors[0].Message, "required") {
		t.Errorf("clearing a required field: %d ok, %d failed %+v", res.SuccessRows, res.FailedRows, res.Errors)
	}
	if got := importedDoc(t, v); got["name"] != "Acme Traders" {
		t.Errorf("a refused row changed the record: name %q", got["name"])
	}
}

// The marker passes the formula-safe writer and reader unchanged.
func TestStage4713aClearMarkerSurvivesTheFormulaSafeCSV(t *testing.T) {
	if sanitizeCSVCell(ImportClearMarker) != ImportClearMarker {
		t.Fatalf("sanitizeCSVCell(%q) = %q", ImportClearMarker, sanitizeCSVCell(ImportClearMarker))
	}
	var buf bytes.Buffer
	sw := newSafeCSVWriter(&buf)
	_ = sw.Write([]string{"id", "city"})
	_ = sw.Write([]string{"V1", ImportClearMarker})
	sw.Flush()
	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	rows := csvRowsToDocData(records[0], records[1:])
	if c, _ := rows[0]["city"].(string); !isImportClearMarker(c) {
		t.Errorf("the marker came back from a spreadsheet round trip as %q", c)
	}
	for _, real := range []string{"", "clear", "Clear Lake", "<clear", "'<clear>"} {
		if isImportClearMarker(real) {
			t.Errorf("%q is read as the clear marker", real)
		}
	}
}

// 49.3.4b: a PO import computes its GST breakdown and totals from its lines,
// as the record-form save does; an update import recomputes them and keeps
// everything else.
func TestStage4934bPOImportComputesGSTAndTotals(t *testing.T) {
	db.InitDB(testConnStr())
	const (
		entity = "T4934B-ENT"
		loc    = "T4934B-LOC"
		vendor = "T4934B-VEN"
		sku    = "T4934B-SKU"
		po     = "T4934B-PO"
	)
	clean := func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id LIKE 'T4934B-%'`) }
	clean()
	t.Cleanup(clean)
	insert := func(id, doctype string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'admin')`, id, doctype, raw); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insert(entity, "LegalEntity", map[string]interface{}{"code": entity, "name": "T4934B Entity", "gstin": "27AAPFU0939F1ZV"})
	insert(loc, "Location", map[string]interface{}{"code": loc, "name": "T4934B Store", "type": "Store", "legal_entity": entity})
	insert(vendor, "Vendor", map[string]interface{}{"code": vendor, "name": "T4934B Mumbai", "state": "Maharashtra"})
	insert(sku, "Item", map[string]interface{}{"code": sku, "name": "T4934B Tee", "hsn_code": "6109", "gst_rate": 5.0, "tax_treatment": "Taxable"})

	header := "id,code,po_number,vendor,vendor_id,items,target_warehouse,location,total_amount,status,gst_mode\n"
	res := importCSV(t, "PurchaseOrder", header+po+","+po+","+po+","+vendor+","+vendor+
		`,"[{""sku"":""`+sku+`"",""qty"":2,""rate"":100}]",`+loc+","+loc+",999,Draft,Exclusive\n")
	if res.SuccessRows != 1 {
		t.Fatalf("PO import: %d ok %+v", res.SuccessRows, res.Errors)
	}
	got := importedDoc(t, po)
	gb, ok := got["gst_breakdown"].(map[string]interface{})
	if !ok {
		t.Fatalf("an imported PO has no gst_breakdown: %v", got)
	}
	// Two lines of 100 before tax at 5%: 200 taxable, 10 tax, 210 to pay.
	if got["total_amount"] != 200.0 || got["grand_total"] != 210.0 || gb["taxable_amount"] != 200.0 {
		t.Errorf("imported PO totals: total_amount %v, grand_total %v, breakdown %v", got["total_amount"], got["grand_total"], gb)
	}

	// An update import that changes only the lines recomputes; the rest stays.
	res = importCSV(t, "PurchaseOrder", header+po+",,,,,"+
		`"[{""sku"":""`+sku+`"",""qty"":3,""rate"":100}]",,,,,`+"\n")
	if res.SuccessRows != 1 || len(res.UpdatedIDs) != 1 {
		t.Fatalf("PO update import: %d ok %+v", res.SuccessRows, res.Errors)
	}
	got = importedDoc(t, po)
	if got["total_amount"] != 300.0 || got["grand_total"] != 315.0 || got["vendor"] != vendor || got["gst_mode"] != "Exclusive" {
		t.Errorf("PO after an update import: total_amount %v, grand_total %v, vendor %v, mode %v", got["total_amount"], got["grand_total"], got["vendor"], got["gst_mode"])
	}
}
