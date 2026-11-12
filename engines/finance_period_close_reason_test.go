package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
	"time"
)

// TestClosingAPeriodNotReadyNeedsAReason (162.12): a period whose checklist
// has a failing check closes only with a typed reason of at least 10
// characters; the reason lands in the close log and the audit log. The hard
// blockers still refuse first, without asking for a reason.
func TestClosingAPeriodNotReadyNeedsAReason(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const name, holdID = "TEST-PERIOD-16212", "VI-16212-HOLD"
	var periodID string
	cleanup := func() {
		if periodID != "" {
			db.DB.Exec("DELETE FROM "+schema+".period_close_log WHERE period_id = $1", periodID)
		}
		db.DB.Exec("DELETE FROM "+schema+".accounting_periods WHERE period_name = $1", name)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", holdID)
	}
	cleanup()
	defer cleanup()

	// A vendor invoice in mismatch hold makes the checklist fail.
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'VendorInvoice', '{}', 'MismatchHold', 'system')", holdID); err != nil {
		t.Fatalf("hold invoice: %v", err)
	}
	// Far in the future, so no other test's postings fall inside it.
	start := time.Now().AddDate(30, 0, 0)
	periodID, err = CreateAccountingPeriod(tenantID, name, start.Format("2006-01-02"), start.AddDate(0, 0, 3).Format("2006-01-02"), "system")
	if err != nil {
		t.Fatalf("CreateAccountingPeriod: %v", err)
	}

	// Hard blocker first: unsigned tasks refuse with FIN-0501, not a reason prompt.
	if code := validationCode(CloseAccountingPeriodChecked(tenantID, periodID, "system", "")); code != "FIN-0501" {
		t.Fatalf("unsigned tasks: code %q, want FIN-0501", code)
	}
	signAllCloseTasksForTest(t, tenantID, periodID)

	status := func() string {
		var s string
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".accounting_periods WHERE id = $1", periodID).Scan(&s); err != nil {
			t.Fatalf("status: %v", err)
		}
		return s
	}
	for _, reason := range []string{"", "   ", "too short"} {
		err := CloseAccountingPeriodChecked(tenantID, periodID, "system", reason)
		if code := validationCode(err); code != "FIN-0507" {
			t.Fatalf("reason %q: code %q (%v), want FIN-0507", reason, code, err)
		}
		if !strings.Contains(err.Error(), "mismatch hold") {
			t.Fatalf("reason %q: the refusal does not name the failing check: %v", reason, err)
		}
		if s := status(); s != "Open" {
			t.Fatalf("reason %q: period is %s, want still Open", reason, s)
		}
	}

	const reason = "auditor signed the hold off for next month"
	if err := CloseAccountingPeriodChecked(tenantID, periodID, "system", reason); err != nil {
		t.Fatalf("close with a reason: %v", err)
	}
	if s := status(); s != "Closed" {
		t.Fatalf("period is %s, want Closed", s)
	}
	var detail string
	if err := db.DB.QueryRow("SELECT detail FROM "+schema+".period_close_log WHERE period_id = $1 AND event = 'CLOSED' ORDER BY id DESC LIMIT 1", periodID).Scan(&detail); err != nil {
		t.Fatalf("close log: %v", err)
	}
	if !strings.Contains(detail, reason) || !strings.Contains(detail, "not ready") {
		t.Fatalf("close log detail %q lacks the reason", detail)
	}
	var n int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".audit_logs WHERE action = 'CLOSE_PERIOD_NOT_READY' AND details LIKE $1", "%"+name+"%"+reason+"%").Scan(&n); err != nil {
		t.Fatalf("audit: %v", err)
	}
	if n == 0 {
		t.Fatal("no CLOSE_PERIOD_NOT_READY audit row")
	}
}
