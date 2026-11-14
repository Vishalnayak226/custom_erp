package engines

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"testing"

	"custom_erp/db"
)

// Ask 170.35 / 170.36 / 170.37: the GSTR-1 file, the GSTR-2B match and the
// LUT rule, each against a throwaway tenant.

func gstTestTenant(t *testing.T) (tenantID, schema string, doc func(id, doctype, status string, data map[string]interface{})) {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema = uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	for _, f := range []string{"migrations_stage170_36_gstr2b_reconcile.sql", "migrations_stage170_37_export_lut.sql"} {
		sql, err := os.ReadFile("../db/migrations/" + f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.DB.Exec(string(sql)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	doc = func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'tester')", id, doctype, b, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	return
}

func gstErrCode(err error) string {
	var v *ValidationError
	if errors.As(err, &v) {
		return v.Code
	}
	return ""
}

func TestGSTR1JSONSectionsAndReconciliation(t *testing.T) {
	tenantID, schema, doc := gstTestTenant(t)
	const seller, buyer, buyerOut = "29AAACE7520K1ZI", "29BBBCF1234L1ZK", "27BBBCF1234L1ZO"
	doc("LE-T", "LegalEntity", "Active", map[string]interface{}{"code": "LE-T", "name": "Test Seller", "gstin": seller, "state": "Karnataka", "address": "1 MG Road", "city": "Bengaluru", "pincode": "560001"})
	doc("LOC-T", "Location", "Active", map[string]interface{}{"code": "LOC-T", "name": "Test Store", "legal_entity": "LE-T", "address": "1 MG Road", "city": "Bengaluru", "pincode": "560001", "state": "Karnataka"})
	cust := func(id, gstin, state string) {
		doc(id, "Customer", "Active", map[string]interface{}{"code": id, "name": "Buyer " + id, "gstin": gstin, "address": "2 Road", "city": "City", "pincode": "560025", "state": state})
	}
	cust("C-B2B", buyer, "Karnataka")
	cust("C-OUT", buyerOut, "Maharashtra")
	cust("C-B2C", "", "Karnataka")
	cust("C-MH", "", "Maharashtra")
	cust("C-EXP", "", "Karnataka")
	doc("SKU-T1", "Item", "Active", map[string]interface{}{"code": "SKU-T1", "name": "Cotton shirt", "hsn_code": "610910", "gst_rate": 5, "uom": "PCS"})
	inv := func(id, number, c string, qty, rate, taxable, cgst, sgst, igst float64, extra map[string]interface{}) {
		d := map[string]interface{}{"invoice_number": number, "customer": c, "location": "LOC-T", "invoice_date": "2026-10-12",
			"items":        []map[string]interface{}{{"sku": "SKU-T1", "qty": qty, "rate": rate}},
			"total_amount": taxable + cgst + sgst + igst, "taxable_amount": taxable, "cgst": cgst, "sgst": sgst, "igst": igst, "total_tax": cgst + sgst + igst}
		for k, v := range extra {
			d[k] = v
		}
		doc(id, "SalesInvoice", "Approved", d)
	}
	inv("SI-1", "SI/0001", "C-B2B", 2, 1050, 2000, 50, 50, 0, nil)
	inv("SI-2", "SI/0002", "C-B2C", 2, 1050, 2000, 50, 50, 0, nil)
	inv("SI-3", "SI/0003", "C-OUT", 2, 1050, 2000, 0, 0, 100, nil)
	inv("SI-4", "SI/0004", "C-MH", 1, 315000, 300000, 0, 0, 15000, map[string]interface{}{"place_of_supply": "27"})
	inv("SI-5", "SI/0005", "C-EXP", 1, 1000, 1000, 0, 0, 0, map[string]interface{}{"export_type": "Export under LUT", "shipping_bill_no": "SB-77", "shipping_bill_date": "2026-10-13", "port_code": "INMAA1", "destination_country": "Kenya"})
	doc("CN-1", "CreditNote", "Posted", map[string]interface{}{"note_number": "CN/0001", "note_date": "2026-10-20", "customer_id": "C-B2B", "sales_invoice_id": "SI-1", "amount": 1050})

	// The ledger as posting leaves it: revenue on 4100, tax on 2200/2201/2202,
	// the export moved to 4112, the credit note reversing its share.
	gl := func(account string, debit, credit float64, ref string) {
		t.Helper()
		if _, err := db.DB.Exec("INSERT INTO "+schema+".gl_postings (posting_id, account_code, debit, credit, document_type, document_id, created_at) VALUES (gen_random_uuid(), $1, $2, $3, 'SalesInvoice', $4, '2026-10-15 12:00:00')",
			account, int64(debit*100), int64(credit*100), ref); err != nil {
			t.Fatalf("gl: %v", err)
		}
	}
	gl("4100", 0, 2000, "SI-1")
	gl("2200", 0, 50, "SI-1")
	gl("2201", 0, 50, "SI-1")
	gl("4100", 0, 2000, "SI-2")
	gl("2200", 0, 50, "SI-2")
	gl("2201", 0, 50, "SI-2")
	gl("4100", 0, 2000, "SI-3")
	gl("2202", 0, 100, "SI-3")
	gl("4100", 0, 300000, "SI-4")
	gl("2202", 0, 15000, "SI-4")
	gl("4100", 0, 1000, "SI-5")
	gl("4100", 1000, 0, "SI-5")
	gl("4112", 0, 1000, "SI-5")
	gl("4100", 1000, 0, "CN-1")
	gl("2200", 25, 0, "CN-1")
	gl("2201", 25, 0, "CN-1")

	res, err := BuildGSTR1(tenantID, seller, "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res.File)
	var f map[string]interface{}
	_ = json.Unmarshal(raw, &f)
	if f["gstin"] != seller || f["fp"] != "102026" {
		t.Fatalf("header: gstin %v fp %v", f["gstin"], f["fp"])
	}
	for _, sec := range []string{"b2b", "b2cl", "b2cs", "cdnr", "exp", "hsn", "doc_issue"} {
		if f[sec] == nil {
			t.Fatalf("section %s missing: %s", sec, raw)
		}
	}
	if f["cdnur"] != nil {
		t.Fatalf("no unregistered note was issued, yet cdnur is present: %s", raw)
	}
	body := string(raw)
	for _, want := range []string{`"ctin":"` + buyer + `"`, `"ctin":"` + buyerOut + `"`, `"inum":"SI/0001"`, `"inum":"SI/0004"`, `"exp_typ":"WOPAY"`, `"sbnum":"SB-77"`, `"nt_num":"CN/0001"`, `"hsn_sc":"610910"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("file lacks %s: %s", want, body)
		}
	}
	if n := len(f["b2b"].([]interface{})); n != 2 {
		t.Fatalf("b2b has %d buyers, want 2", n)
	}
	if strings.Contains(body, `"inum":"SI/0002"`) {
		t.Fatal("the B2C invoice SI/0002 is a b2cs aggregate, not a listed invoice")
	}

	// The file's totals are the ledger's for the month.
	sum, err := GetGSTReturnSummary(tenantID, "2026-10-01", "2026-10-31")
	if err != nil {
		t.Fatal(err)
	}
	r := res.Reconciliation
	if !r.Matches {
		t.Fatalf("file and ledger differ: %+v (warnings %v)", r, res.Warnings)
	}
	if math.Abs(r.FileTaxable-306000) > 0.01 || math.Abs(r.FileCGST-75) > 0.01 || math.Abs(r.FileSGST-75) > 0.01 || math.Abs(r.FileIGST-15100) > 0.01 {
		t.Fatalf("file totals: %+v", r)
	}
	if math.Abs(r.FileCGST-sum.OutputCGST) > 0.01 || math.Abs(r.FileSGST-sum.OutputSGST) > 0.01 || math.Abs(r.FileIGST-sum.OutputIGST) > 0.01 ||
		math.Abs(r.FileTaxable-(sum.TaxableValue+sum.NonTaxableValue)) > 0.01 {
		t.Fatalf("totals do not equal GetGSTReturnSummary: file %+v summary %+v", r, sum)
	}

	// A bad request is refused with the group code.
	if _, err := BuildGSTR1(tenantID, "29AAA", "2026-10"); gstErrCode(err) != "TAXGST-1700" {
		t.Fatalf("short GSTIN: %v", err)
	}
	if _, err := BuildGSTR1(tenantID, seller, "Oct 2026"); gstErrCode(err) != "TAXGST-1700" {
		t.Fatalf("bad month: %v", err)
	}
}

func TestGSTR2BImportAndMatch(t *testing.T) {
	tenantID, schema, doc := gstTestTenant(t)
	const sup = "29AAACE7520K1ZI"
	doc("V1", "Vendor", "Active", map[string]interface{}{"code": "V1", "name": "Supplier One", "gstin": sup})
	bill := func(id, number, date string, amount float64) {
		doc(id, "VendorInvoice", "Approved", map[string]interface{}{"vendor_id": "V1", "invoice_number": number, "invoice_date": date, "invoice_amount": amount})
	}
	bill("VI-1", "INV-0012", "2026-10-05", 1180)
	bill("VI-2", "B-77", "2026-10-06", 500)
	bill("VI-3", "C-9", "2026-10-07", 700)
	bill("VI-4", "BOOK-ONLY-1", "2026-10-08", 910)

	file := func(dLine string) []byte {
		return []byte(`{"data":{"rtnprd":"102026","docdata":{"b2b":[{"ctin":"` + sup + `","trdnm":"Supplier One","inv":[
			{"inum":"inv/12","dt":"05-10-2026","val":1180,"txval":1000,"cgst":90,"sgst":90},
			{"inum":"B77","dt":"06-10-2026","val":560,"txval":500,"cgst":30,"sgst":30},
			{"inum":"ZZ-5","dt":"08-10-2026","val":910,"txval":800,"igst":110},
			` + dLine + `]}]}}}`)
	}
	d1 := `{"inum":"D-1","dt":"09-10-2026","val":2360,"txval":2000,"cgst":180,"sgst":180}`
	res, err := ImportGSTR2B(tenantID, "2026-10", file(d1), "tester")
	if err != nil {
		t.Fatal(err)
	}
	// INV/12 = INV-0012 (number); B77 = B-77 but the amount differs; ZZ-5 has
	// another number than BOOK-ONLY-1 yet the same supplier, amount and date
	// range; D-1 is in no bill; C-9 is in no 2B.
	want := map[string]int{Gstr2bMatched: 2, Gstr2bMismatch: 1, Gstr2bNotInBook: 1, Gstr2bNotIn2B: 1}
	for k, n := range want {
		if res.Summary[k] != n {
			t.Fatalf("summary %v, want %v", res.Summary, want)
		}
	}
	status := func(number string) (id, st, decision string) {
		t.Helper()
		var raw []byte
		if err := db.DB.QueryRow("SELECT id, status, data FROM "+schema+".documents WHERE doctype = 'Gstr2bLine' AND deleted_at IS NULL AND data->>'doc_number' = $1", number).
			Scan(&id, &st, &raw); err != nil {
			t.Fatalf("line %s: %v", number, err)
		}
		var m map[string]interface{}
		_ = json.Unmarshal(raw, &m)
		decision, _ = m["decision"].(string)
		return
	}
	if _, st, _ := status("inv/12"); st != Gstr2bMatched {
		t.Fatalf("inv/12: %s", st)
	}
	if _, st, _ := status("B77"); st != Gstr2bMismatch {
		t.Fatalf("B77: %s", st)
	}
	if _, st, _ := status("C-9"); st != Gstr2bNotIn2B {
		t.Fatalf("C-9: %s", st)
	}
	id, st, _ := status("D-1")
	if st != Gstr2bNotInBook {
		t.Fatalf("D-1: %s", st)
	}

	// A decision survives a re-import of an unchanged line, and is dropped
	// for a line that changed.
	if err := SetGSTR2BDecision(tenantID, id, "Query raised", "asked supplier", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := SetGSTR2BDecision(tenantID, id, "Maybe", "", "tester"); gstErrCode(err) != "TAXGST-1712" {
		t.Fatalf("bad decision: %v", err)
	}
	res, err = ImportGSTR2B(tenantID, "2026-10", file(d1), "tester")
	if err != nil || res.Kept != 1 {
		t.Fatalf("re-import: %+v %v", res, err)
	}
	var lines int
	_ = db.DB.QueryRow("SELECT count(*) FROM " + schema + ".documents WHERE doctype = 'Gstr2bLine' AND deleted_at IS NULL AND data->>'source' = '2B'").Scan(&lines)
	if lines != 4 {
		t.Fatalf("re-import must replace, not add: %d lines", lines)
	}
	if _, _, dec := status("D-1"); dec != "Query raised" {
		t.Fatalf("unchanged line lost its decision: %q", dec)
	}
	d1b := `{"inum":"D-1","dt":"09-10-2026","val":2400,"txval":2040,"cgst":180,"sgst":180}`
	if _, err = ImportGSTR2B(tenantID, "2026-10", file(d1b), "tester"); err != nil {
		t.Fatal(err)
	}
	if _, _, dec := status("D-1"); dec != "" {
		t.Fatalf("a changed line must lose its decision, has %q", dec)
	}

	// A file for another month, and something that is not a 2B file.
	if _, err := ImportGSTR2B(tenantID, "2026-09", file(d1), "tester"); gstErrCode(err) != "TAXGST-1711" {
		t.Fatalf("wrong month: %v", err)
	}
	if _, err := ImportGSTR2B(tenantID, "2026-10", []byte(`{"hello":1}`), "tester"); gstErrCode(err) != "TAXGST-1710" {
		t.Fatalf("not a 2B file: %v", err)
	}
}

func TestNormalizeInvoiceNumber(t *testing.T) {
	for in, want := range map[string]string{"INV-0012": "INV12", "inv/12": "INV12", "A/007/2026": "A72026", "0100": "100", "": ""} {
		if got := NormalizeInvoiceNumber(in); got != want {
			t.Errorf("NormalizeInvoiceNumber(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExportUnderLUTRule(t *testing.T) {
	tenantID, _, doc := gstTestTenant(t)
	export := map[string]interface{}{"invoice_date": "2026-10-12", "export_type": ExportTypeLUT, "customer": "X", "location": "L"}
	check := func() error { return validateExportInvoice(tenantID, export) }

	// Setting off (the default): the register is only a record.
	if err := check(); err != nil {
		t.Fatalf("setting off must not refuse: %v", err)
	}
	if err := SetSetting(tenantID, SettingRequireLUTForExport, "true", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := check(); gstErrCode(err) != "TAXGST-1720" {
		t.Fatalf("no LUT: want TAXGST-1720, got %v", err)
	}
	// An invoice that is not under LUT is never held up by the register.
	if err := validateExportInvoice(tenantID, map[string]interface{}{"invoice_date": "2026-10-12", "export_type": "Export with payment of IGST"}); err != nil {
		t.Fatalf("payment of IGST: %v", err)
	}
	doc("LUT-1", "LutRegister", "Active", map[string]interface{}{"lut_number": "AD290926001234Z", "financial_year": "2026-27", "valid_from": "2026-04-01", "valid_to": "2027-03-31"})
	if err := check(); err != nil {
		t.Fatalf("a valid LUT covers 2026-10-12: %v", err)
	}
	export["invoice_date"] = "2027-04-02"
	if err := check(); gstErrCode(err) != "TAXGST-1720" {
		t.Fatalf("after the LUT ended: %v", err)
	}
	export["invoice_date"] = "2026-10-12"
	if _, err := db.DB.Exec("UPDATE " + tenantSchema(t, tenantID) + ".documents SET status = 'Withdrawn' WHERE id = 'LUT-1'"); err != nil {
		t.Fatal(err)
	}
	if err := check(); gstErrCode(err) != "TAXGST-1720" {
		t.Fatalf("a withdrawn LUT must not count: %v", err)
	}

	// The register keeps its dates in order.
	if err := validateLutRegister(map[string]interface{}{"valid_from": "2026-04-01", "valid_to": "2026-03-31"}); gstErrCode(err) != "TAXGST-1721" {
		t.Fatalf("dates: %v", err)
	}

	// What the tax invoice prints.
	doc("LUT-2", "LutRegister", "Active", map[string]interface{}{"lut_number": "AD290926009999Z", "valid_from": "2026-04-01", "valid_to": "2027-03-31"})
	fields := map[string]interface{}{}
	exportPrintFields(tenantID, map[string]interface{}{"invoice_date": "2026-10-12", "export_type": ExportTypeLUT, "shipping_bill_no": "SB-77", "shipping_bill_date": "2026-10-13", "port_code": "INMAA1", "destination_country": "Kenya"}, fields)
	decl, _ := fields["export_declaration"].(string)
	if !strings.Contains(decl, "export under LUT") || !strings.Contains(decl, "without payment of IGST") || !strings.Contains(decl, "AD2909260099") {
		t.Fatalf("declaration: %q", decl)
	}
	if det, _ := fields["export_details"].(string); !strings.Contains(det, "SB-77") || !strings.Contains(det, "INMAA1") || !strings.Contains(det, "Kenya") {
		t.Fatalf("details: %q", det)
	}
	plain := map[string]interface{}{}
	exportPrintFields(tenantID, map[string]interface{}{"invoice_date": "2026-10-12"}, plain)
	if len(plain) != 0 {
		t.Fatalf("a home sale prints no export lines: %v", plain)
	}
}

func tenantSchema(t *testing.T, tenantID string) string {
	t.Helper()
	s, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The save path runs the LUT rule: a Sales Invoice under LUT is refused by
// ValidateDocument itself, and a LUT with backwards dates cannot be saved.
func TestExportLUTRuleRunsOnSave(t *testing.T) {
	tenantID, _, _ := gstTestTenant(t)
	if err := ValidateDocument(tenantID, LutRegisterDoctype, map[string]interface{}{"lut_number": "L1", "financial_year": "2026-27", "status": "Active", "valid_from": "2026-04-01", "valid_to": "2026-03-01"}); gstErrCode(err) != "TAXGST-1721" {
		t.Fatalf("LUT dates on save: %v", err)
	}
	if err := SetSetting(tenantID, SettingRequireLUTForExport, "true", "tester"); err != nil {
		t.Fatal(err)
	}
	err := ValidateDocument(tenantID, "SalesInvoice", map[string]interface{}{"invoice_number": "SI/9", "customer": "C", "location": "L", "invoice_date": "2026-10-12", "status": "Draft", "export_type": ExportTypeLUT})
	if gstErrCode(err) != "TAXGST-1720" {
		t.Fatalf("an export under LUT with no LUT must be refused on save with TAXGST-1720, got %v", err)
	}
}
