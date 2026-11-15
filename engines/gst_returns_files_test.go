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
	for _, f := range []string{"migrations_stage170_35_gstr1_filed_rows.sql", "migrations_stage170_36_gstr2b_reconcile.sql", "migrations_stage170_37_export_lut.sql"} {
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

// 170.36: a 2B credit or debit note matches our vendor note of the opposite
// sign, and a bill from a vendor with no GSTIN in the books is matched by
// number and flagged instead of being left out.
func TestGSTR2BNotesAndUnregisteredVendor(t *testing.T) {
	tenantID, schema, doc := gstTestTenant(t)
	const sup, unreg = "29AAACE7520K1ZI", "27AAACU1111K1Z5"
	doc("V1", "Vendor", "Active", map[string]interface{}{"code": "V1", "name": "Supplier One", "gstin": sup})
	doc("V2", "Vendor", "Active", map[string]interface{}{"code": "V2", "name": "No GSTIN Yet"})
	doc("VI-U", "VendorInvoice", "Approved", map[string]interface{}{"vendor_id": "V2", "invoice_number": "U-41", "invoice_date": "2026-10-04", "invoice_amount": 1180})
	doc("VI-U2", "VendorInvoice", "Approved", map[string]interface{}{"vendor_id": "V2", "invoice_number": "U-42", "invoice_date": "2026-10-04", "invoice_amount": 300})
	note := func(id, doctype string, total, gst float64, at string) {
		doc(id, doctype, "Posted", map[string]interface{}{"note_number": id, "vendor_id": "V1", "amount": total - gst, "gst_amount": gst, "total_with_gst": total})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".gl_postings (posting_id, account_code, debit, credit, document_type, document_id, created_at) VALUES (gen_random_uuid(), '2100', 100, 0, $1, $2, $3)",
			doctype, id, at); err != nil {
			t.Fatal(err)
		}
	}
	note("DN-1", "DebitNote", 236, 36, "2026-10-10 11:00:00")        // answers the supplier's credit note
	note("VCN-1", "VendorCreditNote", 118, 18, "2026-10-12 11:00:00") // answers the supplier's debit note
	note("DN-2", "DebitNote", 59, 9, "2026-10-20 11:00:00")           // the supplier has not reported it
	note("DN-OLD", "DebitNote", 50, 0, "2026-09-20 11:00:00")         // another month

	file := []byte(`{"data":{"rtnprd":"102026","docdata":{
		"b2b":[{"ctin":"` + unreg + `","trdnm":"No GSTIN Yet","inv":[{"inum":"U/41","dt":"04-10-2026","val":1180,"txval":1000,"igst":180}]}],
		"cdnr":[{"ctin":"` + sup + `","trdnm":"Supplier One","nt":[
			{"ntnum":"SCN-7","typ":"C","dt":"11-10-2026","val":236,"txval":200,"cgst":18,"sgst":18},
			{"ntnum":"SDN-3","typ":"D","dt":"12-10-2026","val":118,"txval":100,"cgst":9,"sgst":9},
			{"ntnum":"SCN-8","typ":"C","dt":"12-10-2026","val":118,"txval":100,"cgst":9,"sgst":9}]}]}}}`)
	res, err := ImportGSTR2B(tenantID, "2026-10", file, "tester")
	if err != nil {
		t.Fatal(err)
	}
	// SCN-7 = DN-1 and SDN-3 = VCN-1 (amount and date); SCN-8 has VCN-1's
	// amount but is a credit note, so it stays unmatched; U/41 = VI-U by
	// number, flagged; DN-2 is in no 2B; VI-U2 (no GSTIN) is not listed.
	want := map[string]int{Gstr2bMatched: 2, Gstr2bMismatch: 1, Gstr2bNotInBook: 1, Gstr2bNotIn2B: 1}
	for k, n := range want {
		if res.Summary[k] != n {
			t.Fatalf("summary %v, want %v", res.Summary, want)
		}
	}
	line := func(number string) map[string]interface{} {
		t.Helper()
		var raw []byte
		if err := db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE doctype = 'Gstr2bLine' AND deleted_at IS NULL AND data->>'doc_number' = $1", number).Scan(&raw); err != nil {
			t.Fatalf("line %s: %v", number, err)
		}
		var m map[string]interface{}
		_ = json.Unmarshal(raw, &m)
		return m
	}
	for number, bill := range map[string]string{"SCN-7": "DN-1", "SDN-3": "VCN-1"} {
		if m := line(number); m["status"] != Gstr2bMatched || m["bill_id"] != bill {
			t.Fatalf("%s: %v", number, m)
		}
	}
	if m := line("SDN-3"); m["doc_type"] != "Debit Note" {
		t.Fatalf("typ D must read as a debit note: %v", m["doc_type"])
	}
	if m := line("SCN-8"); m["status"] != Gstr2bNotInBook {
		t.Fatalf("a credit note must not match a debit note: %v", m)
	}
	if m := line("U/41"); m["status"] != Gstr2bMismatch || m["bill_id"] != "VI-U" || !strings.Contains(m["reason"].(string), "no GSTIN in the books") {
		t.Fatalf("U/41: %v", m)
	}
	if m := line("DN-2"); m["doc_type"] != "Credit Note" || m["reason"] != "the supplier has not reported this note" {
		t.Fatalf("DN-2: %v", m)
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

// 170.35: till bills and a till return in the file, an exempt line in the nil
// table, a documents-issued row per number series, and the amendment tables
// once the month is marked filed and a filed bill or its customer changes.
func TestGSTR1TillSalesNilAndAmendments(t *testing.T) {
	tenantID, schema, doc := gstTestTenant(t)
	const seller, buyer, buyerNew = "29AAACE7520K1ZI", "29BBBCF1234L1ZK", "29CCCCF1234L1ZM"
	doc("LE-T", "LegalEntity", "Active", map[string]interface{}{"code": "LE-T", "name": "Test Seller", "gstin": seller, "state": "Karnataka", "address": "1 MG Road", "city": "Bengaluru", "pincode": "560001"})
	doc("LOC-T", "Location", "Active", map[string]interface{}{"code": "LOC-T", "name": "Test Store", "legal_entity": "LE-T", "address": "1 MG Road", "city": "Bengaluru", "pincode": "560001", "state": "Karnataka"})
	doc("C-B2B", "Customer", "Active", map[string]interface{}{"code": "C-B2B", "name": "Buyer", "gstin": buyer, "address": "2 Road", "city": "City", "pincode": "560025", "state": "Karnataka"})
	doc("SKU-T1", "Item", "Active", map[string]interface{}{"code": "SKU-T1", "name": "Cotton shirt", "hsn_code": "610910", "gst_rate": 5, "uom": "PCS"})
	doc("SKU-EX", "Item", "Active", map[string]interface{}{"code": "SKU-EX", "name": "Fresh milk", "hsn_code": "0401", "gst_rate": 0, "tax_treatment": TaxTreatmentExempt, "uom": "LTR"})

	gl := func(doctype, id, key, account string, debit, credit float64, at string) {
		t.Helper()
		if _, err := db.DB.Exec("INSERT INTO "+schema+".gl_postings (posting_id, account_code, debit, credit, document_type, document_id, idempotency_key, created_at) VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7)",
			account, int64(debit*100), int64(credit*100), doctype, id, doctype+":"+id+":"+key, at); err != nil {
			t.Fatalf("gl: %v", err)
		}
	}
	line := func(sku string, qty int, price float64) map[string]interface{} {
		return map[string]interface{}{"sku": sku, "qty": qty, "sale_price": price, "offer_share": 0}
	}
	// A walk-in bill with a shirt and an exempt line, and a bill to a registered buyer.
	doc("POS-1", "POSCart", "Completed", map[string]interface{}{"location": "LOC-T", "bill_number": "TB/0001", "customer_id": "",
		"items":         []map[string]interface{}{line("SKU-T1", 2, 1050), line("SKU-EX", 1, 400)},
		"gst_breakdown": map[string]interface{}{"interstate": false, "cgst": 50, "sgst": 50, "igst": 0, "taxable_amount": 2000, "exempt_amount": 400, "total_tax": 100}})
	gl("POSCart", "POS-1", "SALE_REVENUE", "4100", 0, 2000, "2026-09-10 06:00:00")
	gl("POSCart", "POS-1", "EXEMPT", "4110", 0, 400, "2026-09-10 06:00:00")
	gl("POSCart", "POS-1", "CGST", "2200", 0, 50, "2026-09-10 06:00:00")
	gl("POSCart", "POS-1", "SGST", "2201", 0, 50, "2026-09-10 06:00:00")
	doc("POS-2", "POSCart", "Completed", map[string]interface{}{"location": "LOC-T", "bill_number": "TB/0002", "customer_id": "C-B2B",
		"items":         []map[string]interface{}{line("SKU-T1", 1, 1050)},
		"gst_breakdown": map[string]interface{}{"interstate": false, "cgst": 25, "sgst": 25, "igst": 0, "taxable_amount": 1000, "total_tax": 50}})
	gl("POSCart", "POS-2", "SALE_REVENUE", "4100", 0, 1000, "2026-09-12 06:00:00")
	gl("POSCart", "POS-2", "CGST", "2200", 0, 25, "2026-09-12 06:00:00")
	gl("POSCart", "POS-2", "SGST", "2201", 0, 25, "2026-09-12 06:00:00")
	// One shirt of the walk-in bill comes back: its credit note, refund and GST reversal.
	doc("RR-1", "ReturnRequest", "Refunded", map[string]interface{}{"original_order_id": "POS-1", "credit_note_no": "CN/T0001", "credit_note_against": "POS-1",
		"items": []map[string]interface{}{{"sku": "SKU-T1", "qty": 1, "original_unit_price": 1050, "disposition": "Sellable"}}})
	doc("RF-1", "RefundRequest", "Completed", map[string]interface{}{"return_request_id": "RR-1", "amount": 1050})
	gl("RefundRequest", "RF-1", "REFUND", "4100", 1050, 0, "2026-09-20 06:00:00")
	gl("ReturnRequest", "RR-1", "GST_REVERSAL", "4100", 0, 50, "2026-09-20 06:00:00")
	gl("ReturnRequest", "RR-1", "GST_REVERSAL_CGST", "2200", 25, 0, "2026-09-20 06:00:00")
	gl("ReturnRequest", "RR-1", "GST_REVERSAL_SGST", "2201", 25, 0, "2026-09-20 06:00:00")

	build := func(month string) (*GSTR1Result, map[string]interface{}, string) {
		t.Helper()
		res, err := BuildGSTR1(tenantID, seller, month)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(res.File)
		var f map[string]interface{}
		_ = json.Unmarshal(raw, &f)
		return res, f, string(raw)
	}
	num := func(m interface{}, k string) float64 { v, _ := m.(map[string]interface{})[k].(float64); return v }

	res, f, body := build("2026-09")
	if len(res.Warnings) > 0 || !res.Reconciliation.Matches {
		t.Fatalf("warnings %v, reconciliation %+v", res.Warnings, res.Reconciliation)
	}
	if res.Counts["till_bills"] != 2 || res.Counts["till_returns"] != 1 {
		t.Fatalf("counts %v", res.Counts)
	}
	// The walk-in bill less its return is one B2CS row; the exempt line is not in it.
	b2cs, _ := f["b2cs"].([]interface{})
	if len(b2cs) != 1 || num(b2cs[0], "txval") != 1000 || num(b2cs[0], "camt") != 25 || num(b2cs[0], "samt") != 25 {
		t.Fatalf("b2cs: %v", f["b2cs"])
	}
	for _, want := range []string{`"ctin":"` + buyer + `"`, `"inum":"TB/0002"`, `"hsn_sc":"0401"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("file lacks %s: %s", want, body)
		}
	}
	nilInv, _ := f["nil"].(map[string]interface{})["inv"].([]interface{})
	exempt := 0.0
	for _, r := range nilInv {
		if r.(map[string]interface{})["sply_ty"] == "INTRAB2C" {
			exempt = num(r, "expt_amt")
		} else if num(r, "expt_amt") != 0 || num(r, "nil_amt") != 0 {
			t.Fatalf("nil row %v should be empty", r)
		}
	}
	if len(nilInv) != 1 || exempt != 400 { // only the supply types that have a value
		t.Fatalf("nil table: %v", f["nil"])
	}
	det, _ := f["doc_issue"].(map[string]interface{})["doc_det"].([]interface{})
	if len(det) != 2 {
		t.Fatalf("doc_issue: %v", f["doc_issue"])
	}
	bills := det[0].(map[string]interface{})["docs"].([]interface{})[0].(map[string]interface{})
	notes := det[1].(map[string]interface{})["docs"].([]interface{})[0].(map[string]interface{})
	if bills["from"] != "TB/0001" || bills["to"] != "TB/0002" || num(bills, "totnum") != 2 || notes["from"] != "CN/T0001" || num(det[1], "doc_num") != 5 {
		t.Fatalf("doc_issue series: %v", f["doc_issue"])
	}

	// Marking September keeps what it reported.
	if err := MarkGSTR1Filed(tenantID, "tester", "2026-09"); err != nil {
		t.Fatal(err)
	}
	var kept int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".gstr1_filed_rows WHERE filed_month = '2026-09-01'").Scan(&kept); err != nil || kept != 3 {
		t.Fatalf("filed rows: %d (%v)", kept, err)
	}
	if res, _, _ := build("2026-10"); res.Counts["amendments"] != 0 {
		t.Fatalf("nothing changed, yet %d amendments", res.Counts["amendments"])
	}

	// After filing, the buyer's GSTIN is corrected and the walk-in bill's price.
	if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = data || jsonb_build_object('gstin', $1::text), updated_at = CURRENT_TIMESTAMP WHERE id = 'C-B2B'", buyerNew); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`UPDATE ` + schema + `.documents SET data = jsonb_set(jsonb_set(data, '{items,0,sale_price}', '1155'), '{gst_breakdown}',
		'{"interstate": false, "cgst": 55, "sgst": 55, "igst": 0, "taxable_amount": 2200, "exempt_amount": 400, "total_tax": 110}'), updated_at = CURRENT_TIMESTAMP WHERE id = 'POS-1'`); err != nil {
		t.Fatal(err)
	}
	res, f, _ = build("2026-10")
	if res.Counts["amendments"] != 2 {
		t.Fatalf("amendments %d, warnings %v", res.Counts["amendments"], res.Warnings)
	}
	b2ba, _ := f["b2ba"].([]interface{})
	if len(b2ba) != 1 || b2ba[0].(map[string]interface{})["ctin"] != buyerNew {
		t.Fatalf("b2ba: %v", f["b2ba"])
	}
	inv := b2ba[0].(map[string]interface{})["inv"].([]interface{})[0].(map[string]interface{})
	if inv["oinum"] != "TB/0002" || inv["oidt"] != "12-09-2026" || inv["inum"] != "TB/0002" {
		t.Fatalf("b2ba invoice: %v", inv)
	}
	// September's B2CS row restated: the bill at its new price, less the return.
	b2csa, _ := f["b2csa"].([]interface{})
	if len(b2csa) != 1 {
		t.Fatalf("b2csa: %v", f["b2csa"])
	}
	row := b2csa[0].(map[string]interface{})
	itm := row["itms"].([]interface{})
	if row["omon"] != "092026" || row["sply_ty"] != "INTRA" || row["pos"] != "29" || len(itm) != 1 || num(itm[0], "txval") != 1200 || num(itm[0], "camt") != 30 {
		t.Fatalf("b2csa row: %v", row)
	}

	// Marking October records the amendments: they are reported once.
	if err := MarkGSTR1Filed(tenantID, "tester", "2026-10"); err != nil {
		t.Fatal(err)
	}
	if res, f, _ := build("2026-10"); res.Counts["amendments"] != 0 || f["b2ba"] != nil || f["b2csa"] != nil {
		t.Fatalf("after marking October: %d amendments, b2ba %v", res.Counts["amendments"], f["b2ba"])
	}

	// The walk-in bill turns out to be the registered buyer's: November lists
	// it in B2B (without its exempt line) and takes it out of B2C.
	if _, err := db.DB.Exec("UPDATE " + schema + ".documents SET data = data || '{\"customer_id\": \"C-B2B\"}'::jsonb, updated_at = CURRENT_TIMESTAMP WHERE id = 'POS-1'"); err != nil {
		t.Fatal(err)
	}
	res, f, _ = build("2026-11")
	b2b, _ := f["b2b"].([]interface{})
	if len(b2b) != 1 || f["b2csa"] == nil || len(res.Warnings) == 0 {
		t.Fatalf("B2C to B2B: b2b %v, b2csa %v, warnings %v", f["b2b"], f["b2csa"], res.Warnings)
	}
	inv = b2b[0].(map[string]interface{})["inv"].([]interface{})[0].(map[string]interface{})
	if itms := inv["itms"].([]interface{}); inv["inum"] != "TB/0001" || inv["idt"] != "10-09-2026" || len(itms) != 1 {
		t.Fatalf("B2C to B2B invoice: %v", inv)
	}
}
