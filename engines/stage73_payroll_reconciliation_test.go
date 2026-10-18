package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

// Stage 73.2.17: gross -> deductions -> net -> bank -> GL.
func TestPayrollReconciliationTiesPayslipsToGLAndBank(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE (document_type = 'Payslip' AND document_id LIKE 'T7317-%') OR document_type = 'TestSalaryPay7317'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T7317-%'")
	}
	cleanup()
	defer cleanup()

	day := time.Now().AddDate(0, 0, -4).Format("2006-01-02")
	insertTestDoc(t, schema, "T7317-EMP", "Employee", map[string]interface{}{"name": "Test Payroll", "bank_account_no": "000111"})
	slip := func(id string, gross, pf, esi, pt, tds, loan, net float64) {
		t.Helper()
		insertTestDoc(t, schema, id, "Payslip", map[string]interface{}{
			"employee_id": "T7317-EMP", "period_from": day, "period_to": day,
			"gross_pay": gross, "pf_deduction": pf, "esi_deduction": esi, "pt_deduction": pt,
			"tds_deduction": tds, "loan_deduction": loan, "net_pay": net})
		db.DB.Exec("UPDATE "+schema+".documents SET status = 'Draft' WHERE id = $1", id)
	}
	slip("T7317-PS1", 30000, 1800, 225, 200, 1000, 500, 26275)
	slip("T7317-PS2", 20000, 1200, 150, 200, 0, 0, 18450)
	slip("T7317-PS3", 15000, 900, 0, 0, 0, 0, 14100) // left Draft
	for _, id := range []string{"T7317-PS1", "T7317-PS2"} {
		if err := PostPayslipToGL(tenantID, id, "system"); err != nil {
			t.Fatalf("post %s: %v", id, err)
		}
	}
	// Salary paid for PS1 from the bank (a journal today), and that bank leg
	// matched to the statement.
	if err := PostDoubleEntry(tenantID, "TestSalaryPay7317", "T7317-PAY", map[string]int64{"2400": 2627500}, map[string]int64{"1100": 2627500}, "", ""); err != nil {
		t.Fatalf("salary payment: %v", err)
	}
	insertTestDoc(t, schema, "T7317-BANK", "BankAccount", map[string]interface{}{"bank_name": "Test", "account_number": "7317", "gl_account_code": "1100"})
	insertTestDoc(t, schema, "T7317-L1", "BankStatementLine", map[string]interface{}{"bank_account": "T7317-BANK", "txn_date": time.Now().Format("2006-01-02"), "amount": 26275, "dr_cr": "Debit", "match_status": "Unmatched"})
	if err := MatchBankLine(tenantID, "T7317-L1", []string{glPostingID(t, schema, "TestSalaryPay7317", "T7317-PAY", "1100")}, "", "manager1"); err != nil {
		t.Fatalf("match salary payment: %v", err)
	}

	rows, err := GetPayrollReconciliation(tenantID, day, day)
	if err != nil {
		t.Fatalf("GetPayrollReconciliation: %v", err)
	}
	byStep := map[string]map[string]interface{}{}
	for _, r := range rows {
		byStep[r["step"].(string)] = r
	}
	want := map[string]float64{"gross": 50000, "pf": 3000, "esi": 375, "pt": 400, "tds": 1000, "loan": 500, "net": 44725}
	for step, amount := range want {
		r := byStep[step]
		if r == nil || r["source_total"] != amount || r["gl_total"] != amount || r["status"] != "Tied" {
			t.Fatalf("step %s: %v (want %v tied)", step, r, amount)
		}
	}
	if byStep["arithmetic"]["status"] != "Tied" {
		t.Fatalf("arithmetic: %v", byStep["arithmetic"])
	}
	if u := byStep["unposted"]; u["status"] != "Difference" || u["payslips"] != 1 || u["source_total"] != 14100.0 {
		t.Fatalf("unposted: %v", u)
	}
	if b := byStep["bank"]; b["status"] != "Tied" || b["source_total"].(float64) < 26275 {
		t.Fatalf("bank: %v", b)
	}
	if p := byStep["paid"]; p["gl_total"].(float64) < 26275 {
		t.Fatalf("paid: %v", p)
	}
	// Drill-through: the gross row opens exactly the posted payslips, the
	// unposted row the draft one.
	if d, _ := payrollReconciliationDrillDown(tenantID, "gross", day, day); len(d) != 2 {
		t.Fatalf("gross drill = %d payslips, want 2", len(d))
	}
	if d, _ := payrollReconciliationDrillDown(tenantID, "unposted", day, day); len(d) != 1 || d[0]["payslip"] != "T7317-PS3" {
		t.Fatalf("unposted drill = %v", d)
	}
}
