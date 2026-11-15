package engines

import (
	"testing"
	"time"
)

// Ask 176.6: instrument calibration - the interval, the next due date the
// server sets, and the report of what is due.

func TestStage1766InstrumentIntervalIsWholeDays(t *testing.T) {
	f := g170Fix(t)
	for _, days := range []float64{0, 3651, 1.5, -30} {
		wantCode(t, ValidateTransactionalRules(f.tenantID, "Instrument", "", "", nil, map[string]interface{}{"name": "Gauge", "calibration_interval_days": days}), "QUALIT-1766")
	}
	if err := ValidateTransactionalRules(f.tenantID, "Instrument", "", "", nil, map[string]interface{}{"name": "Gauge", "calibration_interval_days": 180.0}); err != nil {
		t.Fatal(err)
	}
}

func TestStage1766RecordSetsNextDueAndIsNotEdited(t *testing.T) {
	f := g170Fix(t)
	f.insert("INS-1", "Instrument", "Active", "system", map[string]interface{}{"code": "INS-1", "name": "Scale", "calibration_interval_days": 180.0})
	f.insert("INS-R", "Instrument", "Retired", "system", map[string]interface{}{"code": "INS-R", "name": "Old", "calibration_interval_days": 30.0})
	f.insert("INS-0", "Instrument", "Active", "system", map[string]interface{}{"code": "INS-0", "name": "No interval"})
	rec := func(over map[string]interface{}) map[string]interface{} {
		p := map[string]interface{}{"instrument": "INS-1", "calibrated_on": "2026-01-10", "result": "Pass", "status": "Draft", "next_due_on": "2030-01-01"}
		for k, v := range over {
			p[k] = v
		}
		return p
	}

	pass := rec(nil)
	if err := ValidateTransactionalRules(f.tenantID, "CalibrationRecord", "", "", nil, pass); err != nil {
		t.Fatal(err)
	}
	if pass["next_due_on"] != "2026-07-09" {
		t.Fatalf("a Pass is due again after the interval, got %v", pass["next_due_on"])
	}
	fail := rec(map[string]interface{}{"result": "Fail"})
	if err := ValidateTransactionalRules(f.tenantID, "CalibrationRecord", "", "", nil, fail); err != nil {
		t.Fatal(err)
	}
	if fail["next_due_on"] != "2026-01-10" {
		t.Fatalf("a Fail is due again at once, got %v", fail["next_due_on"])
	}

	tomorrow := time.Now().In(BusinessLocation()).AddDate(0, 0, 2).Format("2006-01-02")
	for name, over := range map[string]map[string]interface{}{
		"no instrument":  {"instrument": "INS-NOPE"},
		"retired":        {"instrument": "INS-R"},
		"no interval":    {"instrument": "INS-0"},
		"bad date":       {"calibrated_on": "10/01/2026"},
		"future date":    {"calibrated_on": tomorrow},
		"no result":      {"result": ""},
		"result not P/F": {"result": "OK"},
	} {
		t.Run(name, func(t *testing.T) {
			wantCode(t, ValidateTransactionalRules(f.tenantID, "CalibrationRecord", "", "", nil, rec(over)), "QUALIT-1767")
		})
	}
	// A Fail on an instrument with no interval is still recorded.
	if err := ValidateTransactionalRules(f.tenantID, "CalibrationRecord", "", "", nil, rec(map[string]interface{}{"instrument": "INS-0", "result": "Fail"})); err != nil {
		t.Fatalf("a Fail on an instrument with no interval: %v", err)
	}
	wantCode(t, ValidateTransactionalRules(f.tenantID, "CalibrationRecord", "CAL-1", "Recorded", pass, rec(nil)), "QUALIT-1767")
}

func TestStage1766DueReportReadsTheLatestRecordedCalibration(t *testing.T) {
	f := g170Fix(t)
	inst := func(code, status string, days float64) {
		f.insert(code, "Instrument", status, "system", map[string]interface{}{"code": code, "name": code, "kept_at": "Lab", "calibration_interval_days": days})
	}
	cal := func(id, status, inst, on, result, next string) {
		f.insert(id, "CalibrationRecord", status, "system", map[string]interface{}{"instrument": inst, "calibrated_on": on, "result": result, "next_due_on": next})
	}
	inst("I-NEVER", "Active", 30)
	inst("I-OVER", "Active", 30)
	cal("C-OVER-OLD", "Recorded", "I-OVER", "2026-08-26", "Pass", "2026-09-25")
	cal("C-OVER", "Recorded", "I-OVER", "2026-09-25", "Pass", "2026-10-25")
	inst("I-SOON", "Active", 30)
	cal("C-SOON", "Recorded", "I-SOON", "2026-10-25", "Pass", "2026-11-24")
	inst("I-OK", "Active", 365)
	cal("C-OK", "Recorded", "I-OK", "2026-06-01", "Pass", "2027-06-01")
	cal("C-OK-DRAFT", "Draft", "I-OK", "2026-11-10", "Fail", "2026-11-10")
	inst("I-FAIL", "Active", 30)
	cal("C-FAIL", "Recorded", "I-FAIL", "2026-11-01", "Fail", "2026-11-01")
	inst("I-RET", "Retired", 30)

	rows, err := CalibrationDue(f.tenantID, "2026-11-14", false)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]interface{}{}
	for _, r := range rows {
		got[strField(r, "code")] = r
	}
	want := map[string][2]interface{}{
		"I-NEVER": {"Never calibrated", 0},
		"I-OVER":  {"Overdue", 20},
		"I-SOON":  {"Due within 30 days", 0},
		"I-FAIL":  {"Failed - recalibrate", 13},
	}
	if len(got) != len(want) {
		t.Fatalf("want %d rows, got %v", len(want), rows)
	}
	for code, w := range want {
		r := got[code]
		if r == nil || r["state"] != w[0] || r["days_overdue"] != w[1] {
			t.Fatalf("%s: want %v, got %v", code, w, r)
		}
	}
	if got["I-OVER"]["last_calibrated"] != "2026-09-25" {
		t.Fatalf("the latest record was not the one read: %v", got["I-OVER"])
	}

	all, err := CalibrationDue(f.tenantID, "2026-11-14", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("All shows every instrument in use, got %v", all)
	}
	for _, r := range all {
		if r["code"] == "I-OK" && r["state"] != "OK" {
			t.Fatalf("a draft record was read: %v", r)
		}
	}
}
