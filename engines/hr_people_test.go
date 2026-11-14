package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

const t170Prefix = "T170E-"

func t170Setup(t *testing.T) (tenantID, schema string) {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID = "default"
	schema, _ = db.GetTenantSchema(tenantID)
	clean := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE '" + t170Prefix + "%'")
	}
	clean()
	t.Cleanup(clean)
	for _, k := range []string{SettingManagerTeamScope, SettingExitClearanceRequired} {
		key := k
		t.Cleanup(func() { _ = SetSetting("default", key, "false", "test") })
	}
	return
}

func t170Code(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	if v, ok := err.(*ValidationError); ok {
		return v.Code
	}
	t.Fatalf("expected a coded error, got %v", err)
	return ""
}

func TestPositionAndManagerLoopsAreRefused(t *testing.T) {
	tenantID, schema := t170Setup(t)
	insertHRJourneyDoc(t, schema, t170Prefix+"POS-A", "Position", "Active", map[string]interface{}{"code": t170Prefix + "POS-A", "reports_to": t170Prefix + "POS-B"})
	insertHRJourneyDoc(t, schema, t170Prefix+"POS-B", "Position", "Active", map[string]interface{}{"code": t170Prefix + "POS-B"})
	err := ValidateTransactionalRules(tenantID, "Position", t170Prefix+"POS-B", "Active", nil, map[string]interface{}{"code": t170Prefix + "POS-B", "reports_to": t170Prefix + "POS-A"})
	if c := t170Code(t, err); c != "HR-1700" {
		t.Fatalf("position loop: %q", c)
	}
	insertHRJourneyDoc(t, schema, t170Prefix+"E1", "Employee", "Active", map[string]interface{}{"code": t170Prefix + "E1", "manager": t170Prefix + "E2"})
	insertHRJourneyDoc(t, schema, t170Prefix+"E2", "Employee", "Active", map[string]interface{}{"code": t170Prefix + "E2"})
	err = ValidateTransactionalRules(tenantID, "Employee", t170Prefix+"E2", "Active", nil, map[string]interface{}{"code": t170Prefix + "E2", "manager": t170Prefix + "E1"})
	if c := t170Code(t, err); c != "HR-1701" {
		t.Fatalf("manager loop: %q", c)
	}
	err = ValidateTransactionalRules(tenantID, "Employee", t170Prefix+"E1", "Active", nil, map[string]interface{}{"code": t170Prefix + "E1", "manager": t170Prefix + "E1"})
	if c := t170Code(t, err); c != "HR-1701" {
		t.Fatalf("own manager: %q", c)
	}
}

func TestManagerTeamScope(t *testing.T) {
	tenantID, schema := t170Setup(t)
	insertHRJourneyDoc(t, schema, t170Prefix+"MGR", "Employee", "Active", map[string]interface{}{"code": t170Prefix + "MGR", "user_id": "t170e-mgr"})
	insertHRJourneyDoc(t, schema, t170Prefix+"REP", "Employee", "Active", map[string]interface{}{"code": t170Prefix + "REP", "manager": t170Prefix + "MGR"})
	insertHRJourneyDoc(t, schema, t170Prefix+"OTH", "Employee", "Active", map[string]interface{}{"code": t170Prefix + "OTH"})
	mine := map[string]interface{}{"employee_id": t170Prefix + "REP"}
	theirs := map[string]interface{}{"employee_id": t170Prefix + "OTH"}

	visible := func(role string, data map[string]interface{}) bool {
		s, err := ResolveSessionScope(tenantID, "Leave", SessionScope{Role: role, UserID: "t170e-mgr"})
		if err != nil {
			t.Fatal(err)
		}
		cs, _ := ScopeConstraints("Leave", s)
		return DocumentMatchesScope(cs, data) == ""
	}
	_ = SetSetting(tenantID, SettingManagerTeamScope, "false", "test")
	if visible(RoleCashier, mine) || !visible(RoleStoreManager, theirs) {
		t.Fatal("setting off: scope must be exactly as before (Cashier own only, Store Manager everything)")
	}
	_ = SetSetting(tenantID, SettingManagerTeamScope, "true", "test")
	if !visible(RoleStoreManager, mine) {
		t.Fatal("setting on: a direct report's leave must be visible to the manager")
	}
	if visible(RoleStoreManager, theirs) {
		t.Fatal("someone else's leave must stay hidden")
	}
	if err := ApprovalTeamRefusal(tenantID, "Leave", theirs, "t170e-mgr", RoleStoreManager); t170Code(t, err) != "HR-1730" {
		t.Fatalf("approving outside the team: %v", err)
	}
	if err := ApprovalTeamRefusal(tenantID, "Leave", mine, "t170e-mgr", RoleStoreManager); err != nil {
		t.Fatalf("approving a report's leave: %v", err)
	}
	if err := ApprovalTeamRefusal(tenantID, "Leave", theirs, "hr1", "HR/Admin"); err != nil {
		t.Fatalf("HR is never limited to a team: %v", err)
	}
}

func TestDisciplinaryCaseReaders(t *testing.T) {
	tenantID, schema := t170Setup(t)
	insertHRJourneyDoc(t, schema, t170Prefix+"EMP", "Employee", "Active", map[string]interface{}{"code": t170Prefix + "EMP", "user_id": "t170e-emp"})
	insertHRJourneyDoc(t, schema, t170Prefix+"WIT", "Employee", "Active", map[string]interface{}{"code": t170Prefix + "WIT", "user_id": "t170e-wit"})
	insertHRJourneyDoc(t, schema, t170Prefix+"STR", "Employee", "Active", map[string]interface{}{"code": t170Prefix + "STR", "user_id": "t170e-str"})
	payload := map[string]interface{}{"employee_id": t170Prefix + "EMP", "named_people": t170Prefix + "WIT"}
	if err := ValidateTransactionalRules(tenantID, DisciplinaryCaseDoctype, t170Prefix+"DSC", "", nil, payload); err != nil {
		t.Fatal(err)
	}
	can := func(role, user string) bool {
		s, err := ResolveSessionScope(tenantID, DisciplinaryCaseDoctype, SessionScope{Role: role, UserID: user})
		if err != nil {
			t.Fatal(err)
		}
		if s.HasGlobalScope(ScopeSelf, DisciplinaryCaseDoctype) {
			return true
		}
		cs, _ := ScopeConstraints(DisciplinaryCaseDoctype, s)
		return DocumentMatchesScope(cs, payload) == ""
	}
	if !can("HR/Admin", "admin") || !can(RoleStoreManager, "t170e-wit") || !can(RoleStoreManager, "t170e-emp") {
		t.Fatal("HR and the people named on the case must read it")
	}
	if can(RoleStoreManager, "t170e-str") || can(RoleCashier, "nobody") {
		t.Fatal("a stranger must not read a disciplinary case")
	}
}

func TestHolidayCalendarCountsWorkingDays(t *testing.T) {
	tenantID, schema := t170Setup(t)
	insertHRJourneyDoc(t, schema, t170Prefix+"EMP", "Employee", "Active", map[string]interface{}{"code": t170Prefix + "EMP", "location": "T170LOC"})
	// 2030-01-07 is a Monday: Mon-Sun is 7 days.
	leave := func() map[string]interface{} {
		return map[string]interface{}{"employee_id": t170Prefix + "EMP", "from_date": "2030-01-07", "to_date": "2030-01-13", "days": float64(7)}
	}
	p := leave()
	if err := ValidateTransactionalRules(tenantID, "Leave", t170Prefix+"L1", "", nil, p); err != nil {
		t.Fatal(err)
	}
	if numFromInterface(p["days"]) != 7 {
		t.Fatalf("no calendar: days must stay as typed, got %v", p["days"])
	}
	insertHRJourneyDoc(t, schema, t170Prefix+"CAL", "HolidayCalendar", "Active", map[string]interface{}{
		"code": t170Prefix + "CAL", "location": "T170LOC", "weekly_off": "Saturday, Sunday", "status": "Active",
		"holidays": []interface{}{map[string]interface{}{"date": "2030-01-09", "name": "Test day"}}})
	p = leave()
	if err := ValidateTransactionalRules(tenantID, "Leave", t170Prefix+"L2", "", nil, p); err != nil {
		t.Fatal(err)
	}
	if numFromInterface(p["days"]) != 4 {
		t.Fatalf("calendar: 7 days less 2 weekly offs and 1 holiday = 4, got %v", p["days"])
	}
	off := map[string]interface{}{"employee_id": t170Prefix + "EMP", "from_date": "2030-01-12", "to_date": "2030-01-13", "days": float64(2)}
	if c := t170Code(t, ValidateTransactionalRules(tenantID, "Leave", t170Prefix+"L3", "", nil, off)); c != "HR-1733" {
		t.Fatalf("leave on days off only: %q", c)
	}
	att := map[string]interface{}{"employee_id": t170Prefix + "EMP", "date": "2030-01-12", "status": "Absent"}
	if err := ValidateTransactionalRules(tenantID, "Attendance", t170Prefix+"A1", "", nil, att); err != nil {
		t.Fatal(err)
	}
	if att["status"] != "WeeklyOff" {
		t.Fatalf("absence on a weekly off: %v", att["status"])
	}
}

func TestOfferAcceptCreatesDraftEmployee(t *testing.T) {
	tenantID, schema := t170Setup(t)
	insertHRJourneyDoc(t, schema, t170Prefix+"POS", "Position", "Active", map[string]interface{}{"code": t170Prefix + "POS", "title": "Cashier T", "department": "Retail"})
	insertHRJourneyDoc(t, schema, t170Prefix+"JOB", "JobOpening", "Open", map[string]interface{}{"code": t170Prefix + "JOB", "position": t170Prefix + "POS", "openings": float64(1), "location": "T170LOC"})
	insertHRJourneyDoc(t, schema, t170Prefix+"APL", "Applicant", "Screening", map[string]interface{}{"code": t170Prefix + "APL", "opening": t170Prefix + "JOB", "applicant_name": "Test Person"})
	insertHRJourneyDoc(t, schema, t170Prefix+"OFR", "JobOffer", "Sent", map[string]interface{}{"code": t170Prefix + "OFR", "applicant": t170Prefix + "APL", "joining_date": "2030-02-01"})
	_, err := AcceptJobOffer(tenantID, t170Prefix+"OFR", "admin")
	if c := t170Code(t, err); c != "HR-1705" {
		t.Fatalf("applicant not at Offer: %q", c)
	}
	db.DB.Exec("UPDATE "+schema+".documents SET status = 'Offer' WHERE id = $1", t170Prefix+"APL")
	res, err := AcceptJobOffer(tenantID, t170Prefix+"OFR", "admin")
	if err != nil {
		t.Fatal(err)
	}
	db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = '" + res.EmployeeID + "'")
	var st string
	db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", t170Prefix+"APL").Scan(&st)
	if st != "Hired" {
		t.Fatalf("applicant status %q", st)
	}
	if _, err := AcceptJobOffer(tenantID, t170Prefix+"OFR", "admin"); t170Code(t, err) != "HR-1704" {
		t.Fatalf("accepting twice: %v", err)
	}
	// Hired cannot be set by hand.
	if c := t170Code(t, ValidateTransactionalRules(tenantID, "Applicant", t170Prefix+"APL2", "Screening", nil, map[string]interface{}{"status": "Hired"})); c != "HR-1703" {
		t.Fatalf("Hired by hand: %q", c)
	}
}

func TestExitClearanceHoldsFinalPayOnlyWhenOn(t *testing.T) {
	tenantID, schema := t170Setup(t)
	gone := time.Now().AddDate(0, 0, -3).Format("2006-01-02")
	insertHRJourneyDoc(t, schema, t170Prefix+"EMP", "Employee", "Inactive", map[string]interface{}{"code": t170Prefix + "EMP", "status": "Inactive", "exit_date": gone})
	insertHRJourneyDoc(t, schema, t170Prefix+"OFF", "OnboardingChecklist", "In Progress", map[string]interface{}{"code": t170Prefix + "OFF", "employee_id": t170Prefix + "EMP", "checklist_type": "Offboarding", "status": "In Progress"})
	_ = SetSetting(tenantID, SettingExitClearanceRequired, "false", "test")
	if err := CheckExitClearance(tenantID, t170Prefix+"EMP"); err != nil {
		t.Fatalf("setting off must never block: %v", err)
	}
	_ = SetSetting(tenantID, SettingExitClearanceRequired, "true", "test")
	if c := t170Code(t, CheckExitClearance(tenantID, t170Prefix+"EMP")); c != "HR-1720" {
		t.Fatalf("open exit checklist: %q", c)
	}
	db.DB.Exec("UPDATE "+schema+".documents SET status = 'Completed', data = data || '{\"status\":\"Completed\"}' WHERE id = $1", t170Prefix+"OFF")
	if err := CheckExitClearance(tenantID, t170Prefix+"EMP"); err != nil {
		t.Fatalf("finished checklist: %v", err)
	}
}

func TestAnalyticsFoldsSmallGroups(t *testing.T) {
	acc := map[string]*analyticsAcc{"Big": {head: 9, join: 2, leave: 1}, "Tiny1": {head: 2}, "Tiny2": {head: 1, leave: 1}}
	rows := foldSmallGroups(acc, 5)
	if len(rows) != 2 || rows[0].Group != "Big" || rows[1].Group != "Other" || rows[1].Headcount != 3 || rows[1].Leavers != 1 {
		t.Fatalf("rows: %+v", rows)
	}
	for _, r := range rows {
		if r.Group == "Tiny1" || r.Group == "Tiny2" {
			t.Fatal("a small group was shown by name")
		}
	}
}

func TestCertificateReminderStampsOnce(t *testing.T) {
	tenantID, schema := t170Setup(t)
	_ = SetSetting(tenantID, SettingCertificateReminderDays, "30", "test")
	t.Cleanup(func() { _ = SetSetting(tenantID, SettingCertificateReminderDays, "30", "test") })
	soon := time.Now().AddDate(0, 0, 5).Format("2006-01-02")
	insertHRJourneyDoc(t, schema, t170Prefix+"TR", "TrainingRecord", "Active", map[string]interface{}{"employee_id": t170Prefix + "EMP", "valid_until": soon})
	list, err := ListExpiringCertificates(tenantID, 30)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range list {
		if c.RecordID == t170Prefix+"TR" {
			found = true
		}
	}
	if !found {
		t.Fatal("expiring certificate missing from the list")
	}
	if err := RunCertificateReminders(tenantID, schema); err != nil {
		t.Fatal(err)
	}
	var stamp string
	db.DB.QueryRow("SELECT COALESCE(data->>'expiry_alerted_for','') FROM "+schema+".documents WHERE id = $1", t170Prefix+"TR").Scan(&stamp)
	if stamp != soon {
		t.Fatalf("reminder not stamped: %q", stamp)
	}
	_ = SetSetting(tenantID, SettingCertificateReminderDays, "0", "test")
	db.DB.Exec("UPDATE "+schema+".documents SET data = data - 'expiry_alerted_for' WHERE id = $1", t170Prefix+"TR")
	if err := RunCertificateReminders(tenantID, schema); err != nil {
		t.Fatal(err)
	}
	db.DB.QueryRow("SELECT COALESCE(data->>'expiry_alerted_for','') FROM "+schema+".documents WHERE id = $1", t170Prefix+"TR").Scan(&stamp)
	if stamp != "" {
		t.Fatal("setting 0 must send nothing")
	}
}
