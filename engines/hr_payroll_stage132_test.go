package engines

import (
	"custom_erp/db"
	"errors"
	"testing"
	"time"
)

// Stage 132.13 (s, r): found by Guide's HRMS flow on the released build.

func cleanupT132Payroll(schema string) {
	db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'Payslip' AND document_id LIKE 'T132%'")
	db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'Payslip' AND document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'Payslip' AND data->>'employee_id' = 'T132R-EMP')")
	db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T132%' OR (doctype = 'Payslip' AND data->>'employee_id' = 'T132R-EMP')")
}

// A 0.75% ESI of 15,000 is 112.50: the payslip must post, and its journal balance to the paisa.
func TestStage132PayslipWithPaisePostsAndBalances(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanupT132Payroll(schema)
	defer cleanupT132Payroll(schema)
	day := time.Now().AddDate(0, 0, -4).Format("2006-01-02")
	insertTestDoc(t, schema, "T132P-EMP", "Employee", map[string]interface{}{"name": "Test Paise", "bank_account_no": "000132"})
	insertTestDoc(t, schema, "T132P-PS1", "Payslip", map[string]interface{}{
		"employee_id": "T132P-EMP", "period_from": day, "period_to": day,
		"gross_pay": 15000.0, "pf_deduction": 1800.0, "esi_deduction": 112.5, "pt_deduction": 200.0,
		"tds_deduction": 0.0, "loan_deduction": 0.0, "net_pay": 12887.5})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Draft' WHERE id = 'T132P-PS1'")

	if err := PostPayslipToGL("default", "T132P-PS1", "system"); err != nil {
		t.Fatalf("a payslip with paise did not post: %v", err)
	}
	var dr, cr, esi, net int64
	db.DB.QueryRow("SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_type = 'Payslip' AND document_id = 'T132P-PS1'").Scan(&dr, &cr)
	db.DB.QueryRow("SELECT COALESCE(SUM(credit),0) FROM " + schema + ".gl_postings WHERE document_type = 'Payslip' AND document_id = 'T132P-PS1' AND account_code = '2402'").Scan(&esi)
	db.DB.QueryRow("SELECT COALESCE(SUM(credit),0) FROM " + schema + ".gl_postings WHERE document_type = 'Payslip' AND document_id = 'T132P-PS1' AND account_code = '2400'").Scan(&net)
	if dr != 1500000 || cr != 1500000 {
		t.Errorf("journal debits %d / credits %d paise, want 1500000 each", dr, cr)
	}
	if esi != 11250 || net != 1288750 {
		t.Errorf("ESI credit %d paise (want 11250), net credit %d paise (want 1288750)", esi, net)
	}
}

// Deductions above gross: a clear refusal, never an unbalanced journal.
func TestStage132PayslipDeductionsOverGrossRefused(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	cleanupT132Payroll(schema)
	defer cleanupT132Payroll(schema)
	day := time.Now().AddDate(0, 0, -4).Format("2006-01-02")
	insertTestDoc(t, schema, "T132N-EMP", "Employee", map[string]interface{}{"name": "Test Over", "bank_account_no": "000133"})
	insertTestDoc(t, schema, "T132N-PS1", "Payslip", map[string]interface{}{
		"employee_id": "T132N-EMP", "period_from": day, "period_to": day,
		"gross_pay": 1000.0, "pf_deduction": 800.0, "pt_deduction": 300.0, "net_pay": 0.0})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Draft' WHERE id = 'T132N-PS1'")
	err := PostPayslipToGL("default", "T132N-PS1", "system")
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != "HRPAYR-1322" {
		t.Fatalf("want HRPAYR-1322, got %v", err)
	}
}

// Running payroll twice for one employee and month keeps one payslip; after it is
// posted, another run is refused.
func TestStage132PayrollRunTwiceKeepsOnePayslip(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	cleanupT132Payroll(schema)
	defer cleanupT132Payroll(schema)
	from := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	to := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	insertTestDoc(t, schema, "T132R-EMP", "Employee", map[string]interface{}{"name": "Test Rerun", "bank_account_no": "000134"})
	insertTestDoc(t, schema, "T132R-SAL", "SalaryStructure", map[string]interface{}{"employee_id": "T132R-EMP", "basic": 15000, "hra": 0, "other_allowances": 0, "pf_percent": 12, "esi_percent": 0, "pt_amount": 200, "effective_from": from})
	insertTestDoc(t, schema, "T132R-ATT", "Attendance", map[string]interface{}{"employee_id": "T132R-EMP", "date": to, "status": "Present"})

	first, err := RunPayroll("default", "T132R-EMP", from, to, "system")
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := RunPayroll("default", "T132R-EMP", from, to, "system")
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if second != first {
		t.Errorf("second run made payslip %s, want the same Draft %s refreshed", second, first)
	}
	var n int
	db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'Payslip' AND deleted_at IS NULL AND data->>'employee_id' = 'T132R-EMP'").Scan(&n)
	if n != 1 {
		t.Errorf("%d payslips for one employee and period, want 1", n)
	}
	if err := PostPayslipToGL("default", first, "system"); err != nil {
		t.Fatalf("post: %v", err)
	}
	_, err = RunPayroll("default", "T132R-EMP", from, to, "system")
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != "HRPAYR-1323" {
		t.Fatalf("a run after posting: want HRPAYR-1323, got %v", err)
	}
}
