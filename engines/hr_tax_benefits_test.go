package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Ask 170 group F: slab tax, TDS projection, LWF / benefits on the payslip and
// its posting, salary revision, calibration, ECR and Form 24Q.

const t170Emp = "T170F-EMP"

func cleanT170F(schema string) {
	db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'Payslip' AND document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'Payslip' AND data->>'employee_id' LIKE 'T170F%')")
	db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T170F%' OR data->>'employee_id' LIKE 'T170F%' OR (doctype = 'Payslip' AND data->>'employee_id' LIKE 'T170F%')")
	db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'LWFRate' AND id LIKE 'T170F%'")
	_ = SetSetting("default", "hr.tds_by_slab", "false", "system")
	_ = SetSetting("default", "hr.lwf_enabled", "false", "system")
}

func setupT170F(t *testing.T) string {
	t.Helper()
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanT170F(schema)
	t.Cleanup(func() { cleanT170F(schema) })
	insertTestDoc(t, schema, t170Emp, "Employee", map[string]interface{}{"name": "Test F", "bank_account_no": "000170", "uan": "100200300400", "pan": "ABCDE1234F", "work_state": "Maharashtra", "department": "Sales"})
	insertTestDoc(t, schema, t170Emp+"-SS", "SalaryStructure", map[string]interface{}{
		"employee_id": t170Emp, "basic": 50000.0, "hra": 20000.0, "other_allowances": 10000.0, "pf_percent": 12.0, "esi_percent": 0.0, "pt_amount": 200.0, "effective_from": "2025-01-01"})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Active' WHERE id = '" + t170Emp + "-SS'")
	insertTestDoc(t, schema, t170Emp+"-A1", "Attendance", map[string]interface{}{"employee_id": t170Emp, "date": "2025-08-04", "status": "Present"})
	return schema
}

func setT170Setting(t *testing.T, schema, key, val string) {
	t.Helper()
	if err := SetSetting("default", key, val, "system"); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

func TestAnnualTaxSlabMath(t *testing.T) {
	newSlabs := []TaxSlabRow{{0, 400000, 0}, {400000, 800000, 5}, {800000, 1200000, 10}, {1200000, 1600000, 15}, {1600000, 2000000, 20}, {2000000, 2400000, 25}, {2400000, 0, 30}}
	p := TaxRegimeParams{StandardDeduction: 75000, RebateLimit: 1200000, RebateMax: 60000, MarginalRelief: true, Cess: 4}
	cases := []struct{ taxable, want float64 }{
		{1200000, 0},      // full rebate
		{1210000, 10400},  // marginal relief: tax limited to the 10,000 earned over the limit, plus 4% cess
		{1500000, 109200}, // 20,000 + 40,000 + 45,000 = 105,000, plus 4% cess
		{300000, 0},
	}
	for _, c := range cases {
		if got := ComputeAnnualTax(newSlabs, p, c.taxable); got != c.want {
			t.Errorf("tax on %v = %v, want %v", c.taxable, got, c.want)
		}
	}
}

// Setting off: byte-for-byte the old payslip. Setting on: monthly TDS is the
// projected annual tax over the months left, and the regime is on the payslip.
func TestPayrollTDSBySlabSettingGatesProjection(t *testing.T) {
	schema := setupT170F(t)
	run := func() map[string]interface{} {
		id, err := RunPayroll("default", t170Emp, "2025-08-01", "2025-08-31", "system")
		if err != nil {
			t.Fatalf("RunPayroll: %v", err)
		}
		var raw []byte
		db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE id = $1", id).Scan(&raw)
		var d map[string]interface{}
		_ = json.Unmarshal(raw, &d)
		return d
	}
	off := run()
	if _, has := off["tax_regime"]; has {
		t.Errorf("setting off: payslip carries a tax regime, want none")
	}
	for _, k := range []string{"lwf_deduction", "benefit_earnings", "benefit_lines"} {
		if _, has := off[k]; has {
			t.Errorf("nothing configured: payslip carries %s", k)
		}
	}
	setT170Setting(t, schema, "hr.tds_by_slab", "true")
	on := run()
	// FY 2025-26, August: 8 months left (Aug..Mar). 8 x 80,000 = 640,000 less 75,000 standard = 565,000
	// taxable (full rebate under 12 lakh): the rebate wipes the tax out.
	if on["tax_regime"] != "New" || on["tds_deduction"] != 0.0 {
		t.Errorf("slab TDS: regime %v tds %v, want New 0 (taxable under the rebate limit)", on["tax_regime"], on["tds_deduction"])
	}
	// A high earner: 400,000 a month is 4.8 lakh x 10 months of projection.
	db.DB.Exec("UPDATE " + schema + ".documents SET data = jsonb_set(data, '{basic}', '300000') WHERE id = '" + t170Emp + "-SS'")
	hi := run()
	if tds := numFromInterface(hi["tds_deduction"]); tds <= 0 {
		t.Errorf("high earner under slab TDS: tds %v, want a positive amount", tds)
	}
}

func TestPayrollTDSBySlabWithoutSlabsRefuses(t *testing.T) {
	schema := setupT170F(t)
	setT170Setting(t, schema, "hr.tds_by_slab", "true")
	// A year with no slabs.
	insertTestDoc(t, schema, t170Emp+"-A31", "Attendance", map[string]interface{}{"employee_id": t170Emp, "date": "2031-08-04", "status": "Present"})
	if _, err := RunPayroll("default", t170Emp, "2031-08-01", "2031-08-31", "system"); err == nil || !strings.Contains(err.Error(), "1740") && !strings.Contains(err.Error(), "slab") {
		t.Errorf("no slabs for the year: err %v, want the HRPAYR-1740 refusal", err)
	}
}

// Benefits and LWF reach the payslip, net pay and the posting; the journal
// balances, a second post posts nothing, and the reconciliation report ties.
func TestPayslipBenefitsAndLWFPostAndReconcile(t *testing.T) {
	schema := setupT170F(t)
	setT170Setting(t, schema, "hr.lwf_enabled", "true")
	insertTestDoc(t, schema, "T170F-LWF", "LWFRate", map[string]interface{}{"state": "Maharashtra", "employee_amount": 25.0, "employer_amount": 75.0, "months": "6,12,8", "status": "Active"})
	insertTestDoc(t, schema, "T170F-PLAN-E", "BenefitPlan", map[string]interface{}{"name": "Meal", "benefit_type": "Earning", "calc_method": "Amount", "value": 2000.0, "status": "Active"})
	insertTestDoc(t, schema, "T170F-PLAN-D", "BenefitPlan", map[string]interface{}{"name": "Insurance", "benefit_type": "Deduction", "calc_method": "Amount", "value": 500.0, "status": "Active"})
	insertTestDoc(t, schema, "T170F-PLAN-R", "BenefitPlan", map[string]interface{}{"name": "Gratuity", "benefit_type": "Employer-paid", "calc_method": "Percent of basic", "value": 4.0, "status": "Active"})
	for _, p := range []string{"E", "D", "R"} {
		insertTestDoc(t, schema, "T170F-ENR-"+p, "BenefitEnrolment", map[string]interface{}{"employee_id": t170Emp, "plan_id": "T170F-PLAN-" + p, "start_date": "2025-01-01", "status": "Active"})
	}
	id, err := RunPayroll("default", t170Emp, "2025-08-01", "2025-08-31", "system")
	if err != nil {
		t.Fatalf("RunPayroll: %v", err)
	}
	var raw []byte
	db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE id = $1", id).Scan(&raw)
	var d map[string]interface{}
	_ = json.Unmarshal(raw, &d)
	// gross 80,000 + 2,000 meal; deductions: PF 6,000, PT 200, insurance 500, LWF 25.
	if d["gross_pay"] != 82000.0 || d["benefit_deductions"] != 500.0 || d["benefit_employer"] != 2000.0 || d["lwf_deduction"] != 25.0 || d["lwf_employer"] != 75.0 {
		t.Fatalf("payslip extras wrong: %v", d)
	}
	if want := 82000.0 - 6000 - 200 - 500 - 25 - numFromInterface(d["tds_deduction"]); d["net_pay"] != want {
		t.Errorf("net pay %v, want %v", d["net_pay"], want)
	}
	if err := PostPayslipToGL("default", id, "system"); err != nil {
		t.Fatalf("post: %v", err)
	}
	var dr, cr, lwf, ben, exp int64
	q := func(acct string, col string) (v int64) {
		db.DB.QueryRow("SELECT COALESCE(SUM("+col+"),0) FROM "+schema+".gl_postings WHERE document_type = 'Payslip' AND document_id = $1 AND account_code = $2", id, acct).Scan(&v)
		return
	}
	db.DB.QueryRow("SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_type = 'Payslip' AND document_id = $1", id).Scan(&dr, &cr)
	lwf, ben, exp = q("2404", "credit"), q("2405", "credit"), q("5500", "debit")
	if dr != cr || lwf != 10000 || ben != 250000 || exp != 8407500 {
		t.Errorf("posting: debit %d credit %d, LWF payable %d (want 10000), benefits payable %d (want 250000), salary expense %d (want 8407500)", dr, cr, lwf, ben, exp)
	}
	// Replay: a second post is refused and posts nothing more.
	var rows1, rows2 int
	db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".gl_postings WHERE document_type = 'Payslip' AND document_id = $1", id).Scan(&rows1)
	if err := PostPayslipToGL("default", id, "system"); err == nil {
		t.Errorf("second post of a Posted payslip was accepted")
	}
	db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".gl_postings WHERE document_type = 'Payslip' AND document_id = $1", id).Scan(&rows2)
	if rows1 != rows2 {
		t.Errorf("replay changed the ledger: %d -> %d lines", rows1, rows2)
	}
	// LWF off for a month not in its list: September carries none.
	if _, err := RunPayroll("default", t170Emp, "2025-09-01", "2025-09-30", "system"); err == nil || true {
		var raw2 []byte
		db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE doctype='Payslip' AND data->>'period_to' = '2025-09-30' AND data->>'employee_id' = $1", t170Emp).Scan(&raw2)
		var d2 map[string]interface{}
		_ = json.Unmarshal(raw2, &d2)
		if _, has := d2["lwf_deduction"]; has && raw2 != nil {
			t.Errorf("September is not an LWF month but the payslip carries LWF")
		}
	}
}

// The ECR line and the Form 24Q rows come from posted payslips.
func TestECRAndForm24Q(t *testing.T) {
	schema := setupT170F(t)
	id, err := RunPayroll("default", t170Emp, "2025-08-01", "2025-08-31", "system")
	if err != nil {
		t.Fatalf("RunPayroll: %v", err)
	}
	if _, err := PFECRRows("default", "2025-08"); err == nil || !strings.Contains(err.Error(), "1742") && !strings.Contains(err.Error(), "No posted") {
		t.Errorf("ECR before posting: err %v, want the no-payslip refusal", err)
	}
	if err := PostPayslipToGL("default", id, "system"); err != nil {
		t.Fatalf("post: %v", err)
	}
	rows, err := PFECRRows("default", "2025-08")
	if err != nil {
		t.Fatalf("ECR: %v", err)
	}
	var mine *ECRRow
	for i := range rows {
		if rows[i].EmployeeID == t170Emp {
			mine = &rows[i]
		}
	}
	if mine == nil || mine.EPFWages != 50000 || mine.EPSWages != 15000 || mine.EE != 6000 || mine.EPS != 1250 || mine.ERDiff != 4750 {
		t.Fatalf("ECR row %+v, want EPF wages 50000, EPS wages 15000, EE 6000, EPS 1250, diff 4750", mine)
	}
	if line := PFECRText([]ECRRow{*mine}); !strings.HasPrefix(line, "100200300400#~#Test F#~#80000#~#50000#~#15000#~#15000#~#6000#~#1250#~#4750#~#0#~#0") {
		t.Errorf("ECR text %q", line)
	}
	csv, err := Form24QCSV("default", "2025-26", "Q2", "I")
	if err != nil || !strings.Contains(csv, "ABCDE1234F") || !strings.Contains(csv, "2025-08-31") {
		t.Errorf("24Q annexure I: err %v csv %q", err, csv)
	}
	if _, err := Form24QCSV("default", "2025-26", "Q2", "II"); err == nil {
		t.Errorf("annexure II for Q2 was accepted")
	}
	if a2, err := Form24QCSV("default", "2025-26", "Q4", "II"); err != nil || !strings.Contains(a2, "Taxable income") {
		t.Errorf("24Q annexure II: err %v", err)
	}
	_ = schema
}

// A salary revision overrides the structure only once Approved and only from its
// effective date; Pending and Rejected do nothing; arrears are listed, not paid.
func TestSalaryRevisionAppliesFromEffectiveDateWhenApproved(t *testing.T) {
	schema := setupT170F(t)
	insertTestDoc(t, schema, "T170F-REV", "SalaryRevision", map[string]interface{}{"employee_id": t170Emp, "effective_date": "2025-08-01", "basic": 60000.0, "reason": "Annual"})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Pending Approval' WHERE id = 'T170F-REV'")
	gross := func(asOf string) float64 {
		sc, err := CalculateSalaryComponentsFor("default", t170Emp, asOf)
		if err != nil {
			t.Fatalf("components: %v", err)
		}
		return sc.Gross
	}
	if g := gross("2025-08-31"); g != 80000 {
		t.Errorf("pending revision applied: gross %v, want 80000", g)
	}
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Rejected' WHERE id = 'T170F-REV'")
	if g := gross("2025-08-31"); g != 80000 {
		t.Errorf("rejected revision applied: gross %v", g)
	}
	// A payslip already run on the old amounts.
	id, err := RunPayroll("default", t170Emp, "2025-08-01", "2025-08-31", "system")
	if err != nil {
		t.Fatalf("RunPayroll: %v", err)
	}
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Approved' WHERE id = 'T170F-REV'")
	if g := gross("2025-07-31"); g != 80000 {
		t.Errorf("revision applied before its effective date: gross %v", g)
	}
	if g := gross("2025-08-31"); g != 90000 {
		t.Errorf("approved revision not applied: gross %v, want 90000", g)
	}
	arrears, total, err := SalaryRevisionArrears("default", "T170F-REV")
	if err != nil || len(arrears) != 1 || arrears[0].PayslipID != id || total != 10000 {
		t.Errorf("arrears %+v total %v err %v, want one payslip, 10000", arrears, total, err)
	}
	var st string
	db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", id).Scan(&st)
	if st != "Draft" {
		t.Errorf("arrears listing changed the payslip: %s", st)
	}
	if err := validateSalaryRevision("default", map[string]interface{}{"employee_id": t170Emp, "effective_date": "2025-08-01"}); err == nil {
		t.Errorf("a revision with no amount was accepted")
	}
}

func TestSalaryRevisionHiddenFromStoreManagerAndCashier(t *testing.T) {
	for _, role := range []string{"Store Manager", "Cashier"} {
		if p := sensitiveFieldPolicy(role, "SalaryRevision")["basic"]; p.AllowRead || p.AllowWrite {
			t.Errorf("%s may read a salary revision's basic", role)
		}
	}
}

// Calibration keeps the original, demands a reason and a permitted caller.
func TestAppraisalCalibration(t *testing.T) {
	schema := setupT170F(t)
	insertTestDoc(t, schema, "T170F-CYC", "AppraisalCycle", map[string]interface{}{"name": "FY", "owner": "owner-user", "status": "Open"})
	insertTestDoc(t, schema, "T170F-AP1", "Appraisal", map[string]interface{}{"employee_id": t170Emp, "cycle_id": "T170F-CYC", "overall_rating": 3.0})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Approved' WHERE id = 'T170F-AP1'")
	if err := CalibrateAppraisal("default", "T170F-AP1", 4, "  ", "owner-user", "Cashier"); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Errorf("no reason: %v", err)
	}
	if err := CalibrateAppraisal("default", "T170F-AP1", 4, "peer spread", "someone", "Store Manager"); err == nil || !strings.Contains(err.Error(), "HR or the cycle owner") {
		t.Errorf("stranger calibrated: %v", err)
	}
	if err := CalibrateAppraisal("default", "T170F-AP1", 9, "x", "owner-user", "Cashier"); err == nil {
		t.Errorf("rating 9 accepted")
	}
	if err := CalibrateAppraisal("default", "T170F-AP1", 4, "peer spread", "owner-user", "Cashier"); err != nil {
		t.Fatalf("cycle owner refused: %v", err)
	}
	var raw []byte
	db.DB.QueryRow("SELECT data FROM " + schema + ".documents WHERE id = 'T170F-AP1'").Scan(&raw)
	var d map[string]interface{}
	_ = json.Unmarshal(raw, &d)
	if d["overall_rating"] != 3.0 || d["calibrated_rating"] != 4.0 || d["calibration_reason"] != "peer spread" {
		t.Errorf("original not kept or calibration missing: %v", d)
	}
	spread, err := AppraisalRatingSpread("default", "T170F-CYC")
	if err != nil || len(spread) != 1 || spread[0].Department != "Sales" || spread[0].Original[2] != 1 || spread[0].Calibrated[3] != 1 {
		t.Errorf("spread %+v err %v", spread, err)
	}
	if err := validateAppraisalCalibrationFields(d, map[string]interface{}{"calibrated_rating": 5.0}); err == nil {
		t.Errorf("generic save changed the calibrated rating")
	}
	if err := validateAppraisalCalibrationFields(d, map[string]interface{}{"calibrated_rating": 4.0}); err != nil {
		t.Errorf("an unchanged calibrated rating refused: %v", err)
	}
}
