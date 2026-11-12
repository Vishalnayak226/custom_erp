package engines

import (
	"custom_erp/db"
	"math"
	"testing"
	"time"
)

// Stage 55.15: a claim with an advance pays out only what is still owed, and
// the advance clears 1600 - cash posted equals cash paid.
func TestExpensePaymentPostsAdvanceAgainstStaffAdvances(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'ExpenseClaim' AND document_id LIKE 'T5515-%'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T5515-%'")
	}
	cleanup()
	defer cleanup()
	insertTestDoc(t, schema, "T5515-CL1", "ExpenseClaim", map[string]interface{}{
		"employee_id": "T5515-E", "amount": 1000.0, "gst_amount": 180.0, "advance_adjusted": 300.0, "status": "Verified"})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Verified' WHERE id = 'T5515-CL1'")
	payable, err := PayExpenseClaim(tenantID, "T5515-CL1")
	if err != nil || payable != 880 {
		t.Fatalf("PayExpenseClaim = %v, %v (want 880)", payable, err)
	}
	got := map[string][2]int64{}
	rows, _ := db.DB.Query("SELECT account_code, debit, credit FROM " + schema + ".gl_postings WHERE document_type = 'ExpenseClaim' AND document_id = 'T5515-CL1'")
	for rows.Next() {
		var a string
		var d, c int64
		rows.Scan(&a, &d, &c)
		got[a] = [2]int64{d, c}
	}
	rows.Close()
	if got["5400"][0] != 100000 || got["1500"][0] != 18000 || got["1100"][1] != 88000 || got["1600"][1] != 30000 {
		t.Fatalf("expense payment legs: %v", got)
	}
	// Paying twice is refused, and posts nothing more.
	if _, err := PayExpenseClaim(tenantID, "T5515-CL1"); err == nil {
		t.Fatalf("a paid claim was paid again")
	}
	// An advance larger than the bill is refused before it is ever paid.
	if err := ValidateExpenseClaimControls(tenantID, map[string]interface{}{
		"expense_date": time.Now().Format("2006-01-02"), "employee_id": "T5515-E", "category": "Food",
		"amount": 100.0, "gst_amount": 0.0, "advance_adjusted": 500.0}); err == nil {
		t.Fatalf("an advance bigger than the bill was accepted")
	}
}

// Stage 55.14: a leave balance is the employee's own unless the role has HR's
// self scope.
func TestLeaveBalanceIsScopedToTheEmployee(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	cleanup := func() { db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T5514-%'") }
	cleanup()
	defer cleanup()
	year := time.Now().Format("2006")
	insertTestDoc(t, schema, "T5514-L1", "Leave", map[string]interface{}{"employee_id": "T5514-EMP", "leave_type": "Casual", "from_date": year + "-03-02", "to_date": year + "-03-04", "days": 3.0})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Approved' WHERE id = 'T5514-L1'")
	insertTestDoc(t, schema, "T5514-L2", "Leave", map[string]interface{}{"employee_id": "T5514-EMP", "leave_type": "Casual", "from_date": year + "-05-10", "to_date": year + "-05-10", "days": 1.0})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Applied' WHERE id = 'T5514-L2'")

	lines, err := GetLeaveBalance(tenantID, "T5514-EMP", year, SessionScope{Role: RoleSuperAdmin, UserID: "admin"})
	if err != nil {
		t.Fatalf("admin balance: %v", err)
	}
	for _, l := range lines {
		// 132.20: Remaining drops on approval only (12 - 3); Available also holds back the pending day.
		if l.LeaveType == "Casual" && (l.Approved != 3 || l.Pending != 1 || l.Remaining == nil || *l.Remaining != 9 || l.Available == nil || *l.Available != 8) {
			t.Fatalf("casual line %+v", l)
		}
		if l.LeaveType == "Unpaid" && l.Entitlement != nil {
			t.Fatalf("unpaid leave has a limit: %+v", l)
		}
	}
	if _, err := GetLeaveBalance(tenantID, "T5514-EMP", year, SessionScope{Role: RoleStoreManager, UserID: "manager1"}); err != nil {
		t.Fatalf("store manager (team scope): %v", err)
	}
	if _, err := GetLeaveBalance(tenantID, "T5514-EMP", year, SessionScope{Role: RoleCashier, UserID: "cashier1"}); validationCode(err) != "GLOBAL-0011" {
		t.Fatalf("a cashier read another employee's balance: %v", err)
	}
}

// Stage 55.16: an asset's lifecycle shows its schedule, the GL it posted and
// the depreciation the books do not hold.
func TestAssetLifecycleShowsScheduleAndPostings(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type IN ('Asset', 'Asset-Disposal') AND document_id LIKE 'T5516-%'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T5516-%'")
	}
	cleanup()
	defer cleanup()
	capDate := time.Now().AddDate(-1, 0, -10).Format("2006-01-02")
	insertTestDoc(t, schema, "T5516-A1", "Asset", map[string]interface{}{"code": "AST-T1", "category": "Furniture", "cost": 1200.0,
		"useful_life_years": 3.0, "capitalisation_date": capDate, "location": "HQ"})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Draft' WHERE id = 'T5516-A1'")
	if err := CapitalizeAsset(tenantID, "T5516-A1"); err != nil {
		t.Fatalf("capitalise: %v", err)
	}
	lc, err := GetAssetLifecycle(tenantID, "T5516-A1")
	if err != nil {
		t.Fatalf("lifecycle: %v", err)
	}
	total, current := 0.0, 0
	for _, s := range lc.Schedule {
		total += s.Depreciation
		if s.Current {
			current = s.Year
		}
	}
	if len(lc.Schedule) != 3 || math.Round(total*100) != 120000 || current != 2 || lc.Annual != 400 {
		t.Fatalf("schedule %+v (annual %d)", lc.Schedule, lc.Annual)
	}
	// Batch D: depreciation is posted monthly, so the gap is now the real
	// lag - a year of ended months nothing has run for yet.
	if len(lc.GL) != 2 || lc.Acquisition["source"] != "Entered by hand" || len(lc.Gaps) == 0 || len(lc.Depreciation.MonthsNotPosted) < 12 {
		t.Fatalf("lifecycle GL %v source %v gaps %v", lc.GL, lc.Acquisition["source"], lc.Gaps)
	}
}
