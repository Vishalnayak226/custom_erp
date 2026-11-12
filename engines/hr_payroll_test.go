package engines

import (
	"custom_erp/db"
	"encoding/json"
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

// Stage 132.20 (user decision 2026-10-26): loss of pay for the days marked
// Absent or on approved Unpaid leave, and only those - a month with one
// Present day and nothing else recorded is paid in full.
func TestStage132PayrollLossOfPay(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	clean := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Payslip' AND data->>'employee_id' IN ('T132L-EMP', 'T132M-EMP')")
		cleanupT132Payroll(schema)
	}
	clean()
	defer clean()
	for _, emp := range []string{"T132L-EMP", "T132M-EMP"} {
		insertTestDoc(t, schema, emp, "Employee", map[string]interface{}{"name": "Test " + emp, "bank_account_no": "000134"})
		insertTestDoc(t, schema, emp+"-SS", "SalaryStructure", map[string]interface{}{
			"employee_id": emp, "basic": 28000.0, "hra": 0.0, "other_allowances": 0.0, "pf_percent": 12.0, "esi_percent": 0.0, "pt_amount": 200.0, "effective_from": "2025-01-01"})
		db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Active' WHERE id = '" + emp + "-SS'")
		insertTestDoc(t, schema, emp+"-A1", "Attendance", map[string]interface{}{"employee_id": emp, "date": "2025-02-03", "status": "Present"})
	}
	// T132L: two days Absent and one day of approved Unpaid leave in February (28 days).
	insertTestDoc(t, schema, "T132L-EMP-A2", "Attendance", map[string]interface{}{"employee_id": "T132L-EMP", "date": "2025-02-04", "status": "Absent"})
	insertTestDoc(t, schema, "T132L-EMP-A3", "Attendance", map[string]interface{}{"employee_id": "T132L-EMP", "date": "2025-02-05", "status": "Absent"})
	insertTestDoc(t, schema, "T132L-EMP-LV", "Leave", map[string]interface{}{"employee_id": "T132L-EMP", "leave_type": "Unpaid", "from_date": "2025-02-10", "to_date": "2025-02-10", "days": 1.0})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Approved' WHERE id = 'T132L-EMP-LV'")

	slip := func(emp string) map[string]interface{} {
		t.Helper()
		id, err := RunPayroll("default", emp, "2025-02-01", "2025-02-28", "system")
		if err != nil {
			t.Fatalf("RunPayroll %s: %v", emp, err)
		}
		var raw []byte
		if err := db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE id = $1", id).Scan(&raw); err != nil {
			t.Fatalf("payslip %s: %v", id, err)
		}
		var d map[string]interface{}
		_ = json.Unmarshal(raw, &d)
		return d
	}
	l := slip("T132L-EMP")
	// 3 of 28 days unpaid: 28000 x 25/28 = 25000; PF 12% of the earned basic = 3000.
	if l["gross_pay"] != 25000.0 || l["pf_deduction"] != 3000.0 || l["lop_days"] != 3.0 || l["paid_days"] != 25.0 || l["lop_amount"] != 3000.0 {
		t.Errorf("loss of pay: gross %v pf %v lop_days %v paid_days %v lop_amount %v, want 25000 3000 3 25 3000", l["gross_pay"], l["pf_deduction"], l["lop_days"], l["paid_days"], l["lop_amount"])
	}
	if m := slip("T132M-EMP"); m["gross_pay"] != 28000.0 || m["lop_days"] != 0.0 {
		t.Errorf("one Present day and nothing else recorded: gross %v lop_days %v, want 28000 0 (an unrecorded day counts as worked)", m["gross_pay"], m["lop_days"])
	}
}
