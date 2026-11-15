package engines

import "testing"

// Ask 176.2: travel requests - the save rules, the link from an advance or a
// claim, and the report's estimate / advance / claimed figures.

func trq176(over map[string]interface{}) map[string]interface{} {
	p := map[string]interface{}{"code": "TRQ-1", "employee_id": "EMP-G", "purpose": "Vendor visit", "destination": "Pune",
		"from_date": "2026-11-20", "to_date": "2026-11-22", "amount": 10000.0, "advance_needed": 3000.0, "status": "Draft"}
	for k, v := range over {
		p[k] = v
	}
	return p
}

func TestStage1762TravelRequestSaveRules(t *testing.T) {
	f := g170Fix(t)
	if err := ValidateTransactionalRules(f.tenantID, "TravelRequest", "", "", nil, trq176(nil)); err != nil {
		t.Fatalf("a good request was refused: %v", err)
	}
	for name, over := range map[string]map[string]interface{}{
		"unknown employee":   {"employee_id": "EMP-NOPE"},
		"no destination":     {"destination": ""},
		"to before from":     {"to_date": "2026-11-19"},
		"no date":            {"from_date": ""},
		"cost zero":          {"amount": 0.0},
		"advance above cost": {"advance_needed": 10000.01},
		"negative advance":   {"advance_needed": -1.0},
	} {
		t.Run(name, func(t *testing.T) {
			wantCode(t, ValidateTransactionalRules(f.tenantID, "TravelRequest", "", "", nil, trq176(over)), "EXP-1703")
		})
	}
	// An approved request is what advances and claims rely on.
	wantCode(t, ValidateTransactionalRules(f.tenantID, "TravelRequest", "TRQ-1", "Approved", trq176(nil), trq176(nil)), "EXP-1703")
}

func TestStage1762OnlyAnApprovedRequestOfTheSameEmployeeCanBeNamed(t *testing.T) {
	f := g170Fix(t)
	f.insert("EMP-H", "Employee", "Active", "system", map[string]interface{}{"code": "EMP-H", "name": "H", "status": "Active"})
	f.insert("TRQ-D", "TravelRequest", "Draft", "system", trq176(map[string]interface{}{"code": "TRQ-D"}))
	f.insert("TRQ-A", "TravelRequest", "Approved", "system", trq176(map[string]interface{}{"code": "TRQ-A", "status": "Approved"}))

	advance := func(emp, req string) map[string]interface{} {
		return map[string]interface{}{"code": "ADV-1", "employee_id": emp, "amount": 3000.0, "status": "Draft", "travel_request": req}
	}
	wantCode(t, ValidateTransactionalRules(f.tenantID, "StaffAdvance", "", "", nil, advance("EMP-G", "TRQ-D")), "EXP-1704")
	wantCode(t, ValidateTransactionalRules(f.tenantID, "StaffAdvance", "", "", nil, advance("EMP-H", "TRQ-A")), "EXP-1704")
	wantCode(t, ValidateTransactionalRules(f.tenantID, "StaffAdvance", "", "", nil, advance("EMP-G", "TRQ-NOPE")), "EXP-1704")
	if err := ValidateTransactionalRules(f.tenantID, "StaffAdvance", "", "", nil, advance("EMP-G", "TRQ-A")); err != nil {
		t.Fatalf("the employee's approved request was refused: %v", err)
	}
	if err := ValidateTransactionalRules(f.tenantID, "StaffAdvance", "", "", nil, advance("EMP-G", "")); err != nil {
		t.Fatalf("an advance with no trip was refused: %v", err)
	}
	claim := map[string]interface{}{"employee_id": "EMP-G", "amount": 500.0, "travel_request": "TRQ-D"}
	wantCode(t, validateTravelRequestLink(f.tenantID, claim), "EXP-1704")
	claim["travel_request"] = "TRQ-A"
	if err := validateTravelRequestLink(f.tenantID, claim); err != nil {
		t.Fatalf("the claim's approved request was refused: %v", err)
	}
}

func TestStage1762ReportSetsEstimateAgainstAdvancesAndClaims(t *testing.T) {
	f := g170Fix(t)
	f.insert("TRQ-R", "TravelRequest", "Approved", "system", trq176(map[string]interface{}{"code": "TRQ-R", "status": "Approved"}))
	f.insert("ADV-R1", "StaffAdvance", "Paid", "system", map[string]interface{}{"code": "ADV-R1", "employee_id": "EMP-G", "amount": 3000.0, "travel_request": "TRQ-R"})
	f.insert("ADV-R2", "StaffAdvance", "Draft", "system", map[string]interface{}{"code": "ADV-R2", "employee_id": "EMP-G", "amount": 999.0, "travel_request": "TRQ-R"})
	f.insert("CLM-R1", "ExpenseClaim", "Approved", "system", map[string]interface{}{"code": "CLM-R1", "employee_id": "EMP-G", "amount": 4000.0, "gst_amount": 500.0, "travel_request": "TRQ-R"})
	f.insert("CLM-R2", "ExpenseClaim", "Draft", "system", map[string]interface{}{"code": "CLM-R2", "employee_id": "EMP-G", "amount": 777.0, "travel_request": "TRQ-R"})
	rows, err := TravelRequestSummary(f.tenantID, "Approved")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 approved request, got %v", rows)
	}
	r := rows[0]
	if r["estimated"] != 10000.0 || r["advance_paid"] != 3000.0 || r["claimed"] != 4500.0 || r["left"] != 5500.0 {
		t.Fatalf("figures: %v", r)
	}
	if rows, _ := TravelRequestSummary(f.tenantID, "Rejected"); len(rows) != 0 {
		t.Fatalf("status filter let through %v", rows)
	}
}

// 176.2 / 176.3: who sees whose trip and card. A Cashier sees only their own;
// a Store Manager the team's trips (they approve them) but only their own
// card; an Accountant every trip and card (they pay and match them).
func TestStage1762TravelRequestAndCardWrongRole(t *testing.T) {
	colleague := map[string]interface{}{"employee_id": "EMP-9"}
	own := map[string]interface{}{"employee_id": "EMP-7"}
	for _, c := range []struct {
		doctype, role string
		seesColleague bool
	}{
		{"TravelRequest", RoleCashier, false},
		{"TravelRequest", RoleStoreManager, true},
		{"TravelRequest", "Accountant", true},
		{"CorporateCard", RoleCashier, false},
		{"CorporateCard", RoleStoreManager, false},
		{"CorporateCard", "Accountant", true},
	} {
		cons, known := ScopeConstraints(c.doctype, session(c.role, "u", "HO", "EMP-7"))
		if !known {
			t.Fatalf("%s must be in the scope registry", c.doctype)
		}
		if failed := DocumentMatchesScope(cons, own); failed != "" {
			t.Errorf("%s / %s: own record refused (%q)", c.doctype, c.role, failed)
		}
		failed := DocumentMatchesScope(cons, colleague)
		if c.seesColleague != (failed == "") || (!c.seesColleague && failed != ScopeSelf) {
			t.Errorf("%s / %s: colleague's record, sees=%v, got %q", c.doctype, c.role, c.seesColleague, failed)
		}
	}
}
