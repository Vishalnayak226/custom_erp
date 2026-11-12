package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Stage 148.20: dimensions on a journal line, and required dimensions by
// account range.

type jd20 struct {
	schema, tenant string
}

func newJD20(t *testing.T) *jd20 {
	t.Helper()
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	f := &jd20{schema: schema, tenant: "default"}
	f.clean()
	t.Cleanup(f.clean)
	return f
}

func (f *jd20) clean() {
	db.DB.Exec("DELETE FROM " + f.schema + ".gl_postings WHERE document_type='JournalVoucher' AND document_id IN (SELECT id FROM " + f.schema + ".documents WHERE doctype='JournalVoucher' AND data->>'narration' LIKE 'TEST-JD20%')")
	db.DB.Exec("DELETE FROM " + f.schema + ".approval_log WHERE doctype='JournalVoucher' AND document_id IN (SELECT id FROM " + f.schema + ".documents WHERE doctype='JournalVoucher' AND data->>'narration' LIKE 'TEST-JD20%')")
	db.DB.Exec("DELETE FROM " + f.schema + ".documents WHERE doctype='JournalVoucher' AND data->>'narration' LIKE 'TEST-JD20%'")
	db.DB.Exec("DELETE FROM " + f.schema + ".documents WHERE id LIKE 'JD20-%'")
}

func (f *jd20) doc(t *testing.T, id, doctype, status string, data map[string]interface{}) {
	t.Helper()
	data["id"] = id
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec("INSERT INTO "+f.schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'system')", id, doctype, raw, status); err != nil {
		t.Fatal(err)
	}
}

func (f *jd20) post(t *testing.T, id string) {
	t.Helper()
	if err := SubmitForApproval(f.tenant, "JournalVoucher", id, "manager1", "Store Manager"); err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval(f.tenant, "JournalVoucher", id, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		t.Fatal(err)
	}
}

func TestStage14820LineDimensionsLandOnTheLedgerRows(t *testing.T) {
	f := newJD20(t)
	f.doc(t, "JD20-CC-A", "CostCenter", "Active", map[string]interface{}{"name": "A"})
	f.doc(t, "JD20-CC-HDR", "CostCenter", "Active", map[string]interface{}{"name": "H"})
	f.doc(t, "JD20-DEP", "Department", "Active", map[string]interface{}{"name": "D"})
	id, err := CreateJournalVoucher(f.tenant, "2026-07-25", "TEST-JD20 lines", []JournalVoucherLine{
		{AccountCode: "5100", Debit: 300, CostCenter: "JD20-CC-A", Department: "JD20-DEP", Store: "HO", Tag: "Q2", Narration: "rent"},
		{AccountCode: "5100", Debit: 200},
		{AccountCode: "1100", Credit: 500},
	}, "manager1", JournalVoucherOptions{CostCenter: "JD20-CC-HDR"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	f.post(t, id)
	rows, err := db.DB.Query("SELECT debit, COALESCE(cost_center,''), COALESCE(department,''), COALESCE(location,''), COALESCE(tag,''), COALESCE(line_narration,'') FROM "+f.schema+".gl_postings WHERE document_id=$1 AND account_code='5100' ORDER BY debit DESC", id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var d int64
		var cc, dep, loc, tag, nar string
		rows.Scan(&d, &cc, &dep, &loc, &tag, &nar)
		got = append(got, fmt.Sprintf("%d|%s|%s|%s|%s|%s", d, cc, dep, loc, tag, nar))
	}
	want := []string{"30000|JD20-CC-A|JD20-DEP|HO|Q2|rent", "20000|JD20-CC-HDR||||"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows %v, want %v", got, want)
	}
}

func TestStage14820SuspendedValueRefusedOnNewLines(t *testing.T) {
	f := newJD20(t)
	f.doc(t, "JD20-CC-OFF", "CostCenter", "Inactive", map[string]interface{}{"name": "Off"})
	_, err := CreateJournalVoucher(f.tenant, "2026-07-25", "TEST-JD20 suspended", []JournalVoucherLine{
		{AccountCode: "5100", Debit: 100, CostCenter: "JD20-CC-OFF"}, {AccountCode: "1100", Credit: 100},
	}, "manager1")
	ve, ok := err.(*ValidationError)
	if !ok || ve.Code != "FIN-0951" {
		t.Fatalf("want FIN-0951, got %v", err)
	}
	if strings.Contains(ve.Message, "JD20-CC-OFF") {
		t.Fatalf("message shows a raw id: %s", ve.Message)
	}
}

func TestStage14820RequiredDimensionByAccountRange(t *testing.T) {
	f := newJD20(t)
	lines := func(dep string) []JournalVoucherLine {
		return []JournalVoucherLine{{AccountCode: "5100", Debit: 100, Department: dep}, {AccountCode: "1100", Credit: 100}}
	}
	// Empty rule table: as today.
	if _, err := CreateJournalVoucher(f.tenant, "2026-07-25", "TEST-JD20 no rules", lines(""), "manager1"); err != nil {
		t.Fatalf("empty table must allow: %v", err)
	}
	f.doc(t, "JD20-R1", "JournalDimensionRule", "Active", map[string]interface{}{"kind": "Required", "account_from": "5000", "account_to": "5999", "dimension": "department", "status": "Active"})
	_, err := CreateJournalVoucher(f.tenant, "2026-07-25", "TEST-JD20 missing", lines(""), "manager1")
	ve, ok := err.(*ValidationError)
	if !ok || ve.Code != "FIN-0950" {
		t.Fatalf("want FIN-0950, got %v", err)
	}
	f.doc(t, "JD20-DEP2", "Department", "Active", map[string]interface{}{"name": "D"})
	if _, err := CreateJournalVoucher(f.tenant, "2026-07-25", "TEST-JD20 given", lines("JD20-DEP2"), "manager1"); err != nil {
		t.Fatalf("with department: %v", err)
	}
	// The header's department counts for every line.
	if _, err := CreateJournalVoucher(f.tenant, "2026-07-25", "TEST-JD20 header", lines(""), "manager1", JournalVoucherOptions{Department: "JD20-DEP2"}); err != nil {
		t.Fatalf("header department: %v", err)
	}
	// The exception list.
	f.doc(t, "JD20-X1", "JournalDimensionRule", "Active", map[string]interface{}{"kind": "Exempt", "account_from": "5100", "status": "Active"})
	if _, err := CreateJournalVoucher(f.tenant, "2026-07-25", "TEST-JD20 exempt", lines(""), "manager1"); err != nil {
		t.Fatalf("exempt account: %v", err)
	}
}

func TestStage14820RuleRefusedOnApprovalPosting(t *testing.T) {
	f := newJD20(t)
	id, err := CreateJournalVoucher(f.tenant, "2026-07-25", "TEST-JD20 late rule", []JournalVoucherLine{{AccountCode: "5100", Debit: 100}, {AccountCode: "1100", Credit: 100}}, "manager1")
	if err != nil {
		t.Fatal(err)
	}
	f.doc(t, "JD20-R2", "JournalDimensionRule", "Active", map[string]interface{}{"kind": "Required", "account_from": "5100", "dimension": "tag", "status": "Active"})
	if err := SubmitForApproval(f.tenant, "JournalVoucher", id, "manager1", "Store Manager"); err != nil {
		t.Fatal(err)
	}
	DecideApproval(f.tenant, "JournalVoucher", id, "admin", "HR/Admin", "HO", "Approved", "ok")
	var n int
	db.DB.QueryRow("SELECT count(*) FROM "+f.schema+".gl_postings WHERE document_id=$1", id).Scan(&n)
	if n != 0 {
		t.Fatalf("a rule added after saving must stop the posting, %d rows posted", n)
	}
}
