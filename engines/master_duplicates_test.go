package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNormalizeMasterName(t *testing.T) {
	for in, want := range map[string]string{
		"M/s. Sharma Textiles Pvt. Ltd.": "sharma textiles",
		"SHARMA TEXTILES LIMITED":        "sharma textiles",
		"Sharma & Sons":                  "sharma sons",
		"  blue  shirt! ":                "blue shirt",
	} {
		if got := normalizeMasterName(in); got != want {
			t.Errorf("normalizeMasterName(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"+91 98765 43210": "9876543210", "098765-43210": "9876543210", "12345": ""} {
		if got := phoneDigitsKey(in); got != want {
			t.Errorf("phoneDigitsKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// Stage 47.13.5: planted duplicate suppliers and items are found under every
// rule, unrelated look-alikes are not, and each group's golden record follows
// the documented order - transactions, validity, completeness, age - never
// the latest write.
func TestDuplicateSuppliersAndItems(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	n := 0
	insert := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		n++
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by, created_at) VALUES ($1, $2, $3, 'Active', 'system', $4)",
			id, doctype, raw, base.Add(time.Duration(n)*time.Hour)); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	g1 := synthGSTIN("29", 101)
	panB := synthGSTIN("29", 202)
	badB2 := "07" + panB[2:14] + "0"
	if ValidGSTINChecksum(badB2) {
		badB2 = "07" + panB[2:14] + "1"
	}
	insert("V-A1", "Vendor", map[string]interface{}{"code": "V-A1", "name": "Sharma Textiles Pvt. Ltd.", "gstin": g1, "contact_phone": "+91 98765 43210", "state": "Karnataka"})
	insert("V-A2", "Vendor", map[string]interface{}{"code": "V-A2", "name": "M/s Sharma Textiles", "contact_phone": "098765-43210"})
	insert("V-A3", "Vendor", map[string]interface{}{"code": "V-A3", "name": "SHARMA TEXTILES LIMITED", "gstin": " " + strings.ToLower(g1) + " "})
	insert("V-B1", "Vendor", map[string]interface{}{"code": "V-B1", "name": "Kumar Traders", "gstin": panB})
	insert("V-B2", "Vendor", map[string]interface{}{"code": "V-B2", "name": "Kumar Delhi Branch", "gstin": badB2})
	insert("V-C1", "Vendor", map[string]interface{}{"code": "V-C1", "name": "Unique Supplier", "gstin": synthGSTIN("29", 303)})
	insert("V-D1", "Vendor", map[string]interface{}{"code": "V-D1", "name": "Acme North", "contact_email": "Sales@Acme.example "})
	insert("V-D2", "Vendor", map[string]interface{}{"code": "V-D2", "name": "Acme South", "contact_email": "sales@acme.example"})
	// V-A3 is the one the business runs on: two orders name it.
	insert("PO-DUP-1", "PurchaseOrder", map[string]interface{}{"vendor": "V-A3"})
	insert("PO-DUP-2", "PurchaseOrder", map[string]interface{}{"vendor": "V-A3"})

	insert("I-1", "Item", map[string]interface{}{"code": "I-1", "name": "Blue Shirt", "barcode": "8901234567890", "hsn_code": "6109"})
	insert("I-2", "Item", map[string]interface{}{"code": "I-2", "name": "blue  shirt!", "barcode": " 8901234567890", "hsn_code": "6109"})
	insert("I-3", "Item", map[string]interface{}{"code": "I-3", "name": "Blue Shirt", "hsn_code": "6205"})
	insert("I-4", "Item", map[string]interface{}{"code": "I-4", "name": "Steel bottle", "gtin": "00012345678905", "hsn_code": "7323"})
	insert("I-5", "Item", map[string]interface{}{"code": "I-5", "name": "Bottle, steel 1L", "gtin": "00012345678905", "hsn_code": "7323", "mrp": 499})
	if _, err := db.DB.Exec("INSERT INTO " + schema + ".inventory_availability (sku, location_code, on_hand, available) VALUES ('I-2', 'HO', 5, 5)"); err != nil {
		t.Fatal(err)
	}

	type got struct {
		group  int
		golden bool
		rule   string
		why    string
	}
	collect := func(rows []MasterDuplicateRow) map[string]got {
		out := map[string]got{}
		for _, r := range rows {
			out[r.ID] = got{r.Group, r.Golden, r.GoldenRule, strings.Join(r.Reasons, ", ")}
		}
		return out
	}
	sup, err := FindDuplicateSuppliers(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	s := collect(sup)
	if _, found := s["V-C1"]; found {
		t.Error("a supplier with no duplicate was reported")
	}
	if _, found := s["Nike Corp"]; found {
		t.Error("the seeded supplier was reported")
	}
	if s["V-A1"].group == 0 || s["V-A1"].group != s["V-A2"].group || s["V-A1"].group != s["V-A3"].group {
		t.Errorf("Sharma group not joined: %+v", s)
	}
	if !s["V-A3"].golden || !strings.HasPrefix(s["V-A3"].rule, "Most transactions") {
		t.Errorf("V-A3 has the orders and should be kept: %+v", s["V-A3"])
	}
	if !strings.Contains(s["V-A1"].why, "Same GSTIN") || !strings.Contains(s["V-A1"].why, "Same phone") || !strings.Contains(s["V-A2"].why, "Same name") {
		t.Errorf("Sharma reasons: A1 %q, A2 %q", s["V-A1"].why, s["V-A2"].why)
	}
	if strings.Contains(s["V-A3"].why, "Same PAN") {
		t.Errorf("same GSTIN shown again as same PAN: %q", s["V-A3"].why)
	}
	if s["V-B1"].group == 0 || s["V-B1"].group != s["V-B2"].group || s["V-B1"].why != "Same PAN" {
		t.Errorf("Kumar registrations: %+v %+v", s["V-B1"], s["V-B2"])
	}
	if !s["V-B1"].golden || !strings.HasPrefix(s["V-B1"].rule, "Has a GSTIN") {
		t.Errorf("the valid GSTIN should be kept: %+v", s["V-B1"])
	}
	if !s["V-D1"].golden || s["V-D1"].rule != "Created first" || s["V-D2"].golden {
		t.Errorf("the original, not the later copy, is kept: %+v %+v", s["V-D1"], s["V-D2"])
	}

	items, err := FindDuplicateItems(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	it := collect(items)
	if _, found := it["I-3"]; found {
		t.Error("same name under another HSN is another product")
	}
	if it["I-1"].group == 0 || it["I-1"].group != it["I-2"].group || !strings.Contains(it["I-1"].why, "Same barcode") || !strings.Contains(it["I-1"].why, "Same name and HSN") {
		t.Errorf("shirt pair: %+v %+v", it["I-1"], it["I-2"])
	}
	if !it["I-2"].golden || !strings.HasPrefix(it["I-2"].rule, "Most transactions") {
		t.Errorf("the stocked item should be kept, not the latest: %+v", it["I-2"])
	}
	if !it["I-5"].golden || !strings.HasPrefix(it["I-5"].rule, "Most complete") || it["I-4"].why != "Same GTIN" {
		t.Errorf("bottle pair: %+v %+v", it["I-4"], it["I-5"])
	}

	// Through the report catalog, as a user runs it.
	_, rows, _, err := RunReport(tenantID, "duplicate-suppliers", RoleSuperAdmin, "system", nil)
	if err != nil || len(rows) != len(sup) {
		t.Fatalf("duplicate-suppliers report: %v, %d rows", err, len(rows))
	}
	keep := 0
	for _, r := range rows {
		if r["proposal"] == "Keep" {
			keep++
		}
	}
	if keep != 3 {
		t.Errorf("3 groups should propose 3 kept records, got %d", keep)
	}
	if _, rows, _, err = RunReport(tenantID, "duplicate-items", RoleSuperAdmin, "system", nil); err != nil || len(rows) != 4 {
		t.Errorf("duplicate-items report: %v, %d rows", err, len(rows))
	}
}
