package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// Ask 170 group G: expense limits, advances, timesheets, milestones, project
// revenue and service SLA.

func g170Fix(t *testing.T) *wsFix {
	t.Helper()
	f := newWSFix(t, false)
	f.insert("EMP-G", "Employee", "Active", "system", map[string]interface{}{"code": "EMP-G", "name": "G", "grade": "Senior", "cost_rate": 500.0, "status": "Active"})
	f.insert("PRJ-G", "Project", "Active", "system", map[string]interface{}{"code": "PRJ-G", "name": "Proj", "status": "Active", "customer": "CUST-G",
		"contract_value": 100000.0, "estimated_cost": 50000.0, "rev_rec_method": "Milestone"})
	return f
}

func (f *wsFix) set(key, val string) {
	f.t.Helper()
	if err := SetSetting(f.tenantID, key, val, "test"); err != nil {
		f.t.Fatal(err)
	}
}

func (f *wsFix) docStatus(doctype, id string) (string, map[string]interface{}) {
	f.t.Helper()
	var raw, st string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data::text, status FROM %s.documents WHERE doctype=$1 AND id=$2`, f.schema), doctype, id).Scan(&raw, &st); err != nil {
		f.t.Fatal(err)
	}
	var d map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &d)
	return st, d
}

func (f *wsFix) glNet(account, project string) int64 {
	var n int64
	q := fmt.Sprintf(`SELECT COALESCE(SUM(debit-credit),0) FROM %s.gl_postings WHERE account_code=$1 AND COALESCE(project,'')=$2`, f.schema)
	_ = db.DB.QueryRow(q, account, project).Scan(&n)
	return n
}

func TestExpenseLimitGradeBeatsCategoryAndRefuseModeBlocks(t *testing.T) {
	f := g170Fix(t)
	f.insert("CAT-T", "ExpenseCategory", "Active", "system", map[string]interface{}{"code": "TRV", "name": "Travel", "limit_per_claim": 1000.0, "status": "Active",
		"grade_limits": []interface{}{map[string]interface{}{"grade": "Senior", "limit": 5000.0}}})
	claim := func(amt float64, emp string) map[string]interface{} {
		return map[string]interface{}{"category": "Travel", "amount": amt, "employee_id": emp}
	}
	// grade limit 5000 applies to the Senior employee, 1000 to others
	if over, lim, _ := expenseClaimOverLimit(f.tenantID, claim(4000, "EMP-G")); over || lim != 5000 {
		t.Fatalf("senior 4000 over=%v lim=%v", over, lim)
	}
	if over, _, _ := expenseClaimOverLimit(f.tenantID, claim(4000, "NOBODY")); !over {
		t.Fatal("plain 4000 should be over 1000")
	}
	if over, _, _ := expenseClaimOverLimit(f.tenantID, map[string]interface{}{"category": "Meals", "amount": 99999.0}); over {
		t.Fatal("a category with no limit is never over")
	}
	// approval mode (default): saves, routed to the approver role
	if err := validateExpenseClaimLimit(f.tenantID, claim(4000, "NOBODY")); err != nil {
		t.Fatalf("approval mode must not refuse: %v", err)
	}
	f.set("expense.over_limit_approver_role", "Accountant")
	if r := expenseOverLimitApproverRole(f.tenantID, claim(4000, "NOBODY")); r != "Accountant" {
		t.Fatalf("approver role = %q", r)
	}
	if r := expenseOverLimitApproverRole(f.tenantID, claim(500, "NOBODY")); r != "" {
		t.Fatalf("within limit routed to %q", r)
	}
	f.set("expense.limit_action", "refuse")
	wantCode(t, validateExpenseClaimLimit(f.tenantID, claim(4000, "NOBODY")), "EXP-1700")
	if err := validateExpenseClaimLimit(f.tenantID, claim(4000, "EMP-G")); err != nil {
		t.Fatalf("within the grade limit: %v", err)
	}
	if r := expenseOverLimitApproverRole(f.tenantID, claim(4000, "NOBODY")); r != "" {
		t.Fatal("refuse mode does not route")
	}
}

func TestEmployeeAdvancePaysOnceAndNetsAgainstClaimOnlyWhenOn(t *testing.T) {
	f := g170Fix(t)
	adv := func(id string, amt float64) {
		f.insert(id, "StaffAdvance", "Draft", "system", map[string]interface{}{"employee_id": "EMP-G", "amount": amt, "advance_date": BusinessToday(), "status": "Draft"})
	}
	adv("ADV-1", 3000)
	if err := PayEmployeeAdvance(f.tenantID, "ADV-1"); err != nil {
		t.Fatal(err)
	}
	wantCode(t, PayEmployeeAdvance(f.tenantID, "ADV-1"), "EXP-1702") // replay
	if d, c := stage140GL(t, f.schema, EmployeeAdvanceDoctype, "ADV-1", "1600"); d != 300000 || c != 0 {
		t.Fatalf("1600 d=%d c=%d, want one debit of 300000", d, c)
	}
	if d, c := stage140GL(t, f.schema, EmployeeAdvanceDoctype, "ADV-1", "1100"); d != 0 || c != 300000 {
		t.Fatalf("1100 d=%d c=%d", d, c)
	}
	wantCode(t, PayEmployeeAdvance(f.tenantID, "NOPE"), "EXP-1701")
	// setting OFF: claim pays in full, advance untouched
	f.insert("CL-OFF", "ExpenseClaim", "Verified", "system", map[string]interface{}{"employee_id": "EMP-G", "category": "Travel", "amount": 1000.0, "gst_amount": 0.0, "purpose": "x"})
	if _, err := PayExpenseClaim(f.tenantID, "CL-OFF"); err != nil {
		t.Fatal(err)
	}
	if _, d := f.docStatus("StaffAdvance", "ADV-1"); numFromInterface(d["settled_amount"]) != 0 {
		t.Fatalf("netting off but advance settled %v", d["settled_amount"])
	}
	// setting ON: partial netting 1000 of 3000, cash leg shrinks, then the rest
	f.set("expense.net_employee_advance", "true")
	f.insert("CL-ON", "ExpenseClaim", "Verified", "system", map[string]interface{}{"employee_id": "EMP-G", "category": "Travel", "amount": 1000.0, "gst_amount": 0.0, "purpose": "x"})
	cash, err := PayExpenseClaim(f.tenantID, "CL-ON")
	if err != nil {
		t.Fatal(err)
	}
	if cash != 0 {
		t.Fatalf("cash paid %v, want 0 (fully netted)", cash)
	}
	if st, d := f.docStatus("StaffAdvance", "ADV-1"); st != "Paid" || numFromInterface(d["settled_amount"]) != 1000 {
		t.Fatalf("advance %s settled %v", st, d["settled_amount"])
	}
	f.insert("CL-ON2", "ExpenseClaim", "Verified", "system", map[string]interface{}{"employee_id": "EMP-G", "category": "Travel", "amount": 3000.0, "gst_amount": 0.0, "purpose": "x"})
	cash, err = PayExpenseClaim(f.tenantID, "CL-ON2")
	if err != nil || cash != 1000 {
		t.Fatalf("second claim cash %v err %v, want 1000 paid and 2000 netted", cash, err)
	}
	if st, d := f.docStatus("StaffAdvance", "ADV-1"); st != "Settled" || numFromInterface(d["settled_amount"]) != 3000 {
		t.Fatalf("advance %s settled %v", st, d["settled_amount"])
	}
	// balance report reconciles: nothing open on the 1600 account for the advance
	rows, _ := EmployeeAdvanceBalances(f.tenantID)
	if len(rows) != 1 || numFromInterface(rows[0]["balance"]) != 0 {
		t.Fatalf("balances %v", rows)
	}
	if n := f.glNet("1600", ""); n != 0 {
		t.Fatalf("employee advances account net %d, want 0 after full settlement", n)
	}
}

func TestTimesheetCostsProjectOnceAndFreezes(t *testing.T) {
	f := g170Fix(t)
	ts := func(id string, hours float64) {
		f.insert(id, "Timesheet", "Approved", "system", map[string]interface{}{"employee_id": "EMP-G", "project": "PRJ-G", "work_date": BusinessToday(), "hours": hours})
	}
	ts("TS-1", 4)
	if err := CostTimesheet(f.tenantID, "TS-1"); err != nil {
		t.Fatal(err)
	}
	if err := CostTimesheet(f.tenantID, "TS-1"); err != nil { // replay
		t.Fatal(err)
	}
	if n := f.glNet("5450", "PRJ-G"); n != 200000 {
		t.Fatalf("project labour cost %d, want 200000 once", n)
	}
	if n := f.glNet("5460", ""); n != -200000 {
		t.Fatalf("absorbed credit %d", n)
	}
	// concurrent replay still costs once
	ts("TS-2", 2)
	done := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { done <- CostTimesheet(f.tenantID, "TS-2") }()
	}
	for i := 0; i < 4; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if n := f.glNet("5450", "PRJ-G"); n != 300000 {
		t.Fatalf("after concurrent replay %d, want 300000", n)
	}
	// no cost rate
	f.insert("EMP-NR", "Employee", "Active", "system", map[string]interface{}{"code": "EMP-NR", "name": "NR", "status": "Active"})
	f.insert("TS-3", "Timesheet", "Approved", "system", map[string]interface{}{"employee_id": "EMP-NR", "project": "PRJ-G", "work_date": BusinessToday(), "hours": 1.0})
	wantCode(t, CostTimesheet(f.tenantID, "TS-3"), "FIN-1721")
	// frozen and range checks
	_, prior := f.docStatus("Timesheet", "TS-1")
	wantCode(t, validateTimesheetSave(f.tenantID, "TS-1", "Approved", prior, prior), "FIN-1720")
	bad := map[string]interface{}{"employee_id": "EMP-G", "project": "PRJ-G", "work_date": BusinessToday(), "hours": 25.0}
	wantCode(t, validateTimesheetSave(f.tenantID, "", "", nil, bad), "FIN-1720")
	bad["hours"] = 3.0
	if err := validateTimesheetSave(f.tenantID, "", "", nil, bad); err != nil {
		t.Fatal(err)
	}
	bad["project"] = "NO-SUCH"
	wantCode(t, validateTimesheetSave(f.tenantID, "", "", nil, bad), "FIN-1720")
}

func TestMilestoneCompletesOnceAndBudgetVsActualReconciles(t *testing.T) {
	f := g170Fix(t)
	f.insert("MS-1", "ProjectMilestone", "Planned", "system", map[string]interface{}{"project": "PRJ-G", "name": "Phase 1", "percent": 30.0, "billable": "Yes", "status": "Planned"})
	inv, err := CompleteProjectMilestone(f.tenantID, "MS-1")
	if err != nil || inv == "" {
		t.Fatalf("inv %q err %v", inv, err)
	}
	inv2, err := CompleteProjectMilestone(f.tenantID, "MS-1")
	if err != nil || inv2 != inv {
		t.Fatalf("replay gave %q err %v, want %q", inv2, err, inv)
	}
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype='SalesInvoice' AND data->>'project_milestone_id'='MS-1'`, f.schema)).Scan(&n)
	if n != 1 {
		t.Fatalf("%d invoices for one milestone", n)
	}
	if st, d := f.docStatus("SalesInvoice", inv); st != "Draft" || numFromInterface(d["total_amount"]) != 30000 {
		t.Fatalf("invoice %s %v", st, d["total_amount"])
	}
	f.insert("MS-2", "ProjectMilestone", "Planned", "system", map[string]interface{}{"project": "PRJ-G", "name": "No amount", "billable": "No", "status": "Planned"})
	f.insert("PRJ-X", "Project", "Active", "system", map[string]interface{}{"code": "PRJ-X", "status": "Active"})
	f.insert("MS-3", "ProjectMilestone", "Planned", "system", map[string]interface{}{"project": "PRJ-X", "name": "Nothing", "status": "Planned"})
	wantCode(t, func() error { _, e := CompleteProjectMilestone(f.tenantID, "MS-3"); return e }(), "FIN-1722")
	// budget vs actual: cost a timesheet then compare
	f.insert("TS-B", "Timesheet", "Approved", "system", map[string]interface{}{"employee_id": "EMP-G", "project": "PRJ-G", "work_date": BusinessToday(), "hours": 2.0})
	if err := CostTimesheet(f.tenantID, "TS-B"); err != nil {
		t.Fatal(err)
	}
	f.insert("BUD-1", "ProjectBudget", "Active", "system", map[string]interface{}{"project": "PRJ-G", "status": "Active",
		"lines": []interface{}{map[string]interface{}{"cost_type": "Labour", "amount": 5000.0}}})
	rows, err := ProjectBudgetVsActual(f.tenantID, "PRJ-G")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if fmt.Sprint(r["cost_type"]) == "Labour" {
			found = true
			if numFromInterface(r["budget"]) != 5000 || numFromInterface(r["actual"]) != 1000 {
				t.Fatalf("labour row %v", r)
			}
		}
	}
	if !found {
		t.Fatalf("no Labour row in %v", rows)
	}
	wantCode(t, validateProjectBudgetSave(map[string]interface{}{"project": "PRJ-G", "lines": []interface{}{}}), "FIN-1723")
}

func TestProjectRevenueRecognitionIsSettingGatedOnceAMonthAndReconciles(t *testing.T) {
	f := g170Fix(t)
	period := time.Now().In(businessLocation()).Format("2006-01")
	if _, err := RunProjectRevenue(f.tenantID, period); err == nil {
		t.Fatal("setting off must refuse")
	} else {
		wantCode(t, err, "FIN-1724")
	}
	f.set("projects.revenue_recognition", "true")
	wantCode(t, func() error { _, e := RunProjectRevenue(f.tenantID, "bad"); return e }(), "FIN-1724")
	// 30% milestone completed, unbilled: asset 30000
	f.insert("MS-R", "ProjectMilestone", "Planned", "system", map[string]interface{}{"project": "PRJ-G", "name": "P", "percent": 30.0, "billable": "No", "status": "Planned"})
	if _, err := CompleteProjectMilestone(f.tenantID, "MS-R"); err != nil {
		t.Fatal(err)
	}
	res, err := RunProjectRevenue(f.tenantID, period)
	if err != nil || len(res) != 1 || res[0].Asset != 30000 {
		t.Fatalf("res %+v err %v", res, err)
	}
	if _, err := RunProjectRevenue(f.tenantID, period); err != nil { // replay same month
		t.Fatal(err)
	}
	if n := f.glNet("1330", "PRJ-G"); n != 3000000 {
		t.Fatalf("contract asset %d, want 3000000 once", n)
	}
	if n := f.glNet("4100", "PRJ-G"); n != -3000000 {
		t.Fatalf("revenue %d", n)
	}
	// concurrent runs of a fresh month book it once
	next := time.Now().In(businessLocation()).AddDate(0, 1, 0).Format("2006-01")
	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() { _, e := RunProjectRevenue(f.tenantID, next); done <- e }()
	}
	for i := 0; i < 3; i++ {
		if e := <-done; e != nil {
			t.Fatal(e)
		}
	}
	var entries int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype='ProjectRevenueEntry' AND data->>'project'='PRJ-G' AND data->>'period'=$1`, f.schema), next).Scan(&entries)
	if entries != 1 {
		t.Fatalf("%d entries for one project-month", entries)
	}
	if n := f.glNet("1330", "PRJ-G"); n != 3000000 {
		t.Fatalf("no change expected next month, asset %d", n)
	}
	// cost to complete: 25% of estimated cost spent => 25% of contract earned
	f.insert("PRJ-C", "Project", "Active", "system", map[string]interface{}{"code": "PRJ-C", "status": "Active", "contract_value": 80000.0, "estimated_cost": 40000.0, "rev_rec_method": "Cost to complete"})
	f.insert("EMP-C", "Employee", "Active", "system", map[string]interface{}{"code": "EMP-C", "cost_rate": 1000.0, "status": "Active"})
	f.insert("TS-C", "Timesheet", "Approved", "system", map[string]interface{}{"employee_id": "EMP-C", "project": "PRJ-C", "work_date": BusinessToday(), "hours": 10.0})
	if err := CostTimesheet(f.tenantID, "TS-C"); err != nil {
		t.Fatal(err)
	}
	if _, err := RunProjectRevenue(f.tenantID, period); err != nil {
		t.Fatal(err)
	}
	if n := f.glNet("1330", "PRJ-C"); n != 2000000 {
		t.Fatalf("cost-to-complete asset %d, want 2000000 (25%% of 80000)", n)
	}
}

func TestServiceSLAStampsDueTimesAndEscalatesOnce(t *testing.T) {
	f := g170Fix(t)
	f.insert("TK-1", "ServiceTicket", "Open", "system", map[string]interface{}{"priority": "Critical", "subject": "down", "status": "Open"})
	if s, e, err := RunServiceSLA(f.tenantID, f.schema, time.Now().Add(100*time.Hour)); err != nil || s != 0 || e != 0 {
		t.Fatalf("setting off: %d %d %v", s, e, err)
	}
	f.set("service.sla_enabled", "true")
	f.set("service.sla_escalation_to", "user:boss")
	s, e, err := RunServiceSLA(f.tenantID, f.schema, time.Now())
	if err != nil || s != 1 || e != 0 {
		t.Fatalf("first sweep: %d %d %v", s, e, err)
	}
	_, d := f.docStatus("ServiceTicket", "TK-1")
	if strField(d, "first_response_due") == "" || strField(d, "resolve_due") == "" {
		t.Fatalf("due times missing: %v", d)
	}
	late := time.Now().Add(30 * 24 * time.Hour)
	if _, e, err = RunServiceSLA(f.tenantID, f.schema, late); err != nil || e != 1 {
		t.Fatalf("late sweep escalated %d err %v", e, err)
	}
	if _, e, _ = RunServiceSLA(f.tenantID, f.schema, late.Add(time.Hour)); e != 0 {
		t.Fatalf("second late sweep escalated again (%d)", e)
	}
	_, d = f.docStatus("ServiceTicket", "TK-1")
	if strField(d, "assigned_to") != "boss" || strField(d, "sla_escalated_at") == "" {
		t.Fatalf("escalation not recorded: %v", d)
	}
	if _, err := parseServiceSLAHours("Low=0"); err == nil {
		t.Fatal("zero hours must be refused")
	}
	if h := settingChangeHooks[SettingServiceSLAEscalateTo]; h == nil || h("", "", "someone", "") == nil {
		t.Fatal("a bad escalation target must be refused")
	}
}

// 170.30 / 170.31: who sees whose advance and timesheet. A Cashier and a Store
// Manager see only their own advance; a Cashier only their own timesheet. A
// Store Manager sees the team's timesheets (they approve them) and an
// Accountant every advance and timesheet (they pay and cost them).
func TestStaffAdvanceAndTimesheetWrongRole(t *testing.T) {
	colleague := map[string]interface{}{"employee_id": "EMP-9"}
	own := map[string]interface{}{"employee_id": "EMP-7"}
	cases := []struct {
		doctype, role string
		seesColleague bool
	}{
		{"StaffAdvance", RoleCashier, false},
		{"StaffAdvance", RoleStoreManager, false},
		{"StaffAdvance", "Accountant", true},
		{"Timesheet", RoleCashier, false},
		{"Timesheet", RoleStoreManager, true},
		{"Timesheet", "Accountant", true},
	}
	for _, c := range cases {
		cons, known := ScopeConstraints(c.doctype, session(c.role, "u", "HO", "EMP-7"))
		if !known {
			t.Fatalf("%s must be in the scope registry", c.doctype)
		}
		if failed := DocumentMatchesScope(cons, own); failed != "" {
			t.Errorf("%s / %s: own record refused (%q)", c.doctype, c.role, failed)
		}
		failed := DocumentMatchesScope(cons, colleague)
		if c.seesColleague && failed != "" {
			t.Errorf("%s / %s: a colleague's record must be visible, got %q", c.doctype, c.role, failed)
		}
		if !c.seesColleague && failed != ScopeSelf {
			t.Errorf("%s / %s: a colleague's record must be refused, got %q", c.doctype, c.role, failed)
		}
	}
	// No Employee link: no rows, not all rows.
	cons, _ := ScopeConstraints("StaffAdvance", session(RoleCashier, "u", "HO", ""))
	if DocumentMatchesScope(cons, own) != ScopeSelf {
		t.Error("a session with no Employee link must match no advance")
	}
}
