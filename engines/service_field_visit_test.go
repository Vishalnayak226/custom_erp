package engines

import (
	"testing"
	"time"
)

// Ask 176.4: field-service visits - an open ticket and an engineer, the
// check-in / check-out stamps, and the customer's sign-off on Done.

func visitFix176(t *testing.T) *wsFix {
	t.Helper()
	f := g170Fix(t)
	f.insert("TKT-O", "ServiceTicket", "Open", "system", map[string]interface{}{"code": "TKT-O", "subject": "AC not cooling"})
	f.insert("TKT-C", "ServiceTicket", "Closed", "system", map[string]interface{}{"code": "TKT-C", "subject": "Done"})
	return f
}

func visit176(over map[string]interface{}) map[string]interface{} {
	p := map[string]interface{}{"ticket": "TKT-O", "engineer": "EMP-G", "scheduled_on": "2026-11-14", "status": "Scheduled"}
	for k, v := range over {
		p[k] = v
	}
	return p
}

func TestStage1764VisitNeedsAnOpenTicketAnEngineerAndADate(t *testing.T) {
	f := visitFix176(t)
	if err := ValidateTransactionalRules(f.tenantID, "ServiceVisit", "", "", nil, visit176(nil)); err != nil {
		t.Fatalf("a good visit was refused: %v", err)
	}
	for name, over := range map[string]map[string]interface{}{
		"no ticket":      {"ticket": "TKT-NOPE"},
		"closed ticket":  {"ticket": "TKT-C"},
		"no engineer":    {"engineer": "EMP-NOPE"},
		"no date":        {"scheduled_on": ""},
		"unreadable in":  {"checked_in_at": "10:30am"},
		"unreadable out": {"checked_in_at": "2026-11-14 10:30", "checked_out_at": "soon"},
		"out before in":  {"checked_in_at": "2026-11-14 10:30", "checked_out_at": "2026-11-14 09:00"},
	} {
		t.Run(name, func(t *testing.T) {
			wantCode(t, ValidateTransactionalRules(f.tenantID, "ServiceVisit", "", "", nil, visit176(over)), "CRMPLT-1132")
		})
	}
	// A visit already on a ticket that closed since is still saved.
	prior := visit176(map[string]interface{}{"ticket": "TKT-C"})
	if err := ValidateTransactionalRules(f.tenantID, "ServiceVisit", "SV-1", "Scheduled", prior, visit176(map[string]interface{}{"ticket": "TKT-C", "notes": "x"})); err != nil {
		t.Fatalf("a visit on a ticket closed later was refused: %v", err)
	}
	// A Done visit is the record.
	done := visit176(map[string]interface{}{"status": "Done"})
	wantCode(t, ValidateTransactionalRules(f.tenantID, "ServiceVisit", "SV-1", "Done", done, done), "CRMPLT-1132")
}

func TestStage1764CheckInAndDoneStampTheTimes(t *testing.T) {
	f := visitFix176(t)
	in := visit176(map[string]interface{}{"status": "Checked in"})
	if err := ValidateTransactionalRules(f.tenantID, "ServiceVisit", "", "", nil, in); err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse(serviceVisitTimeLayout, strField(in, "checked_in_at")); err != nil {
		t.Fatalf("check-in was not stamped: %v", in)
	}

	wantCode(t, ValidateTransactionalRules(f.tenantID, "ServiceVisit", "", "", nil, visit176(map[string]interface{}{"status": "Done", "customer_signoff_name": "Ravi"})), "CRMPLT-1132")
	wantCode(t, ValidateTransactionalRules(f.tenantID, "ServiceVisit", "", "", nil, visit176(map[string]interface{}{"status": "Done", "checked_in_at": "2026-11-14 10:30"})), "CRMPLT-1132")

	typed := visit176(map[string]interface{}{"status": "Done", "checked_in_at": "2026-11-14T10:30", "checked_out_at": "2026-11-14 12:00", "customer_signoff_name": "Ravi"})
	if err := ValidateTransactionalRules(f.tenantID, "ServiceVisit", "", "", nil, typed); err != nil {
		t.Fatal(err)
	}
	if typed["checked_in_at"] != "2026-11-14 10:30" || typed["checked_out_at"] != "2026-11-14 12:00" {
		t.Fatalf("typed times: %v", typed)
	}

	stamped := visit176(map[string]interface{}{"status": "Done", "checked_in_at": "2020-01-01 09:00", "customer_signoff_name": "Ravi"})
	if err := ValidateTransactionalRules(f.tenantID, "ServiceVisit", "", "", nil, stamped); err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse(serviceVisitTimeLayout, strField(stamped, "checked_out_at")); err != nil {
		t.Fatalf("check-out was not stamped: %v", stamped)
	}
}
