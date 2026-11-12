package engines

import (
	"bufio"
	"custom_erp/db"
	"os"
	"strings"
	"testing"
)

// Stage 75.3.4: a register built from a declaration - master ids shown as
// names (one query per master), the date / store / status filters, an image
// column carrying the item's main image, totals flagged, and the store cut
// (75.3.3) applied through the hidden store key.
func TestStage7534RegisterBuilder(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	ids := []string{"C-7534", "SI-7534-A", "SI-7534-B", "PM-7534"}
	cleanup := func() {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	cleanup()
	defer cleanup()
	seed := func(id, doctype, status, data string) {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'admin')", id, doctype, data, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed("C-7534", "Customer", "Active", `{"code":"C-7534","name":"Register Buyer 7534"}`)
	seed("SI-7534-A", "SalesInvoice", "Approved", `{"invoice_number":"SI/R/0001","customer":"C-7534","location":"STORE-A-7534","total_amount":100,"sku_for_test":"SKU-7534","invoice_date":"2026-10-01"}`)
	seed("SI-7534-B", "SalesInvoice", "Paid", `{"invoice_number":"SI/R/0002","customer":"C-7534","location":"STORE-B-7534","total_amount":250,"invoice_date":"2026-10-05"}`)
	seed("PM-7534", "ProductMedia", "Active", `{"item":"SKU-7534","file_type":"image/png","media_role":"Main Image"}`)

	const id = "test-register-7534"
	if _, ok := reportRegistry[id]; !ok {
		RegisterDocumentRegister(RegisterSpec{ID: id, Label: "Register test", Category: "Finance", Doctype: "SalesInvoice", DateField: "invoice_date",
			Statuses: []string{"Approved", "Paid"},
			Columns: []RegisterColumn{
				{Key: "number", Label: "Invoice", Field: "invoice_number"},
				{Key: "customer", Label: "Customer", Field: "customer", Lookup: "Customer.name"},
				{Key: "image", Label: "Image", Field: "sku_for_test", Type: "image"},
				{Key: "amount", Label: "Amount", Field: "total_amount", Total: true},
			}})
	}
	run := func(p map[string]string) []map[string]interface{} {
		t.Helper()
		p["from"], p["to"] = "2026-09-30", "2026-10-31"
		_, rows, _, err := RunReport("default", id, "Super Admin", "admin", p)
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		var mine []map[string]interface{}
		for _, r := range rows {
			if n, _ := r["number"].(string); strings.HasPrefix(n, "SI/R/") {
				mine = append(mine, r)
			}
		}
		return mine
	}
	rows := run(map[string]string{})
	if len(rows) != 2 {
		t.Fatalf("both invoices: got %v", rows)
	}
	for _, r := range rows {
		if r["customer"] != "Register Buyer 7534" {
			t.Fatalf("customer id shown as its name: got %v", r["customer"])
		}
		if r["number"] == "SI/R/0001" && r["image"] != "PM-7534" {
			t.Fatalf("image column: want the main image's media id, got %v", r["image"])
		}
	}
	if rows := run(map[string]string{"location": "STORE-B-7534"}); len(rows) != 1 || rows[0]["number"] != "SI/R/0002" {
		t.Fatalf("store filter: %v", rows)
	}
	if rows := run(map[string]string{"status": "Approved"}); len(rows) != 1 || rows[0]["number"] != "SI/R/0001" {
		t.Fatalf("status filter: %v", rows)
	}
	def := reportRegistry[id]
	if def.StoreKey != "_store" || !def.Columns[3].Total || def.Columns[2].Type != "image" {
		t.Fatalf("definition: store key %q, columns %+v", def.StoreKey, def.Columns)
	}
	access := StoreAccess{Home: "STORE-A-7534", Stores: map[string]bool{"STORE-A-7534": true}}
	_, all, _, _ := RunReport("default", id, "Super Admin", "admin", map[string]string{"from": "2026-09-30", "to": "2026-10-31"})
	cut := filterReportRowsByStore(all, reportStoreKey(def), access)
	for _, r := range cut {
		if r["_store"] != "STORE-A-7534" {
			t.Fatalf("store cut through the hidden key let in %v", r)
		}
	}
}

// Stage 75.3.4: every catalog report's columns match the user's catalog
// (docs/specs/reports/catalog_columns.tsv: report_id, position, label, meaning).
// The spec arrives with 75.3.2; until then there is nothing to hold reports to.
func TestReportColumnsMatchCatalogSpec(t *testing.T) {
	f, err := os.Open("../docs/specs/reports/catalog_columns.tsv")
	if err != nil {
		t.Skip("docs/specs/reports/catalog_columns.tsv is not in the tree yet (75.3.2)")
	}
	defer f.Close()
	spec := map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Split(sc.Text(), "\t")
		if len(parts) < 3 || parts[0] == "report_id" || strings.HasPrefix(parts[0], "#") {
			continue
		}
		spec[parts[0]] = append(spec[parts[0]], strings.TrimSpace(parts[2]))
	}
	for id, labels := range spec {
		def, ok := reportRegistry[id]
		if !ok {
			continue // not built yet: report-catalog-coverage.md tracks it
		}
		if len(def.Columns) != len(labels) {
			t.Errorf("%s: %d columns, the catalog has %d (%v)", id, len(def.Columns), len(labels), labels)
			continue
		}
		for i, l := range labels {
			if !strings.EqualFold(def.Columns[i].Label, l) {
				t.Errorf("%s column %d: %q, the catalog says %q", id, i+1, def.Columns[i].Label, l)
			}
		}
	}
}
