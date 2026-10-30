package engines

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"custom_erp/db"
)

// Stage 49.11.1: the three event classes the taxonomy named as missing -
// sensitive bulk read, invariant exception, verifier failure - each one
// audit_logs row at the place it happens.

func stage49111Rows(t *testing.T, action, status, detailsLike string) int {
	t.Helper()
	var n int
	if err := db.DB.QueryRow(`SELECT count(*) FROM tenant_default.audit_logs WHERE action = $1 AND status = $2 AND details LIKE $3`,
		action, status, detailsLike).Scan(&n); err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	return n
}

func stage49111Clean(t *testing.T, action, detailsLike string) {
	t.Helper()
	db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = $1 AND details LIKE $2`, action, detailsLike)
}

func TestStage49111SensitiveBulkReadIsAnAuditEvent(t *testing.T) {
	db.InitDB(testConnStr())
	mark := fmt.Sprintf("stage49111-%d", time.Now().UnixNano())
	like := "%" + mark + "%"
	t.Cleanup(func() { stage49111Clean(t, SensitiveReadAction, like) })

	// A role that may read payroll fields: one BULK row naming type, count and filter.
	LogSensitiveBulkRead(context.Background(), "default", "stage49111-hr", RoleSuperAdmin, "Employee", 7, "status=Active&"+mark)
	if got := stage49111Rows(t, SensitiveReadAction, "BULK", "Employee rows=7 fields=%"+like); got != 1 {
		t.Fatalf("super admin list of Employee: %d SENSITIVE_READ rows, want 1", got)
	}
	// A role the policy masks sees no sensitive field, so it is not a sensitive read.
	LogSensitiveBulkRead(context.Background(), "default", "stage49111-cashier", RoleCashier, "Employee", 7, "status=Active&"+mark)
	// A doctype with no sensitive field, and an empty page, write nothing.
	LogSensitiveBulkRead(context.Background(), "default", "stage49111-hr", RoleSuperAdmin, "SalesOrder", 7, mark)
	LogSensitiveBulkRead(context.Background(), "default", "stage49111-hr", RoleSuperAdmin, "Employee", 0, mark)
	if got := stage49111Rows(t, SensitiveReadAction, "BULK", like); got != 1 {
		t.Fatalf("after a masked role, a plain doctype and an empty page: %d rows, want still 1", got)
	}

	// A report with a sensitive column, read unmasked: one REPORT row; masked: none.
	columns := []ReportColumn{{Key: "employee", Label: "Employee"}, {Key: "net_pay", Label: "Net pay", Sensitive: true}}
	logSensitiveReportRead(context.Background(), "default", "stage49111-hr", RoleSuperAdmin, "payroll-register", columns, 12, map[string]string{"month": mark})
	logSensitiveReportRead(context.Background(), "default", "stage49111-cashier", RoleCashier, "payroll-register", columns, 12, map[string]string{"month": mark})
	if got := stage49111Rows(t, SensitiveReadAction, "REPORT", "report=payroll-register rows=12 columns=net_pay filter=month="+mark); got != 1 {
		t.Fatalf("payroll report read: %d REPORT rows, want 1", got)
	}
}

func TestStage49111InvariantExceptionIsAnAuditEvent(t *testing.T) {
	db.InitDB(testConnStr())
	docID := fmt.Sprintf("SI-49111-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		stage49111Clean(t, InvariantAction, "%"+docID+"%")
		stage49111Clean(t, InvariantAction, "rule=control_totals:"+docID+"%")
	})

	// A change to a posted invoice's money is refused AND recorded, naming the actor.
	release := BindPostingActor(RequestPostingActor("u-49111", "stage49111-accounts", "test", "PUT /api/v1/doc/SalesInvoice/{id}"))
	err := checkPostedAmountsFrozen("default", "SalesInvoice", docID, "Paid",
		map[string]interface{}{"total_amount": "100"}, map[string]interface{}{"total_amount": "200"})
	release()
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != "FIN-0844" {
		t.Fatalf("posted amount change: err=%v, want FIN-0844", err)
	}
	if got := stage49111Rows(t, InvariantAction, "REFUSED", "rule=posted_amounts_frozen SalesInvoice "+docID+"%"); got != 1 {
		t.Fatalf("refused change: %d INVARIANT/REFUSED rows, want 1", got)
	}
	var actor string
	if err := db.DB.QueryRow(`SELECT user_id FROM tenant_default.audit_logs WHERE action = $1 AND details LIKE $2 ORDER BY created_at DESC LIMIT 1`,
		InvariantAction, "%"+docID+"%").Scan(&actor); err != nil || actor != "stage49111-accounts" {
		t.Fatalf("refused row actor = %q (%v), want the bound posting actor", actor, err)
	}
	// An unchanged save of the same posted invoice is not an event.
	if err := checkPostedAmountsFrozen("default", "SalesInvoice", docID, "Paid",
		map[string]interface{}{"total_amount": "100"}, map[string]interface{}{"total_amount": "100"}); err != nil {
		t.Fatalf("unchanged save refused: %v", err)
	}
	if got := stage49111Rows(t, InvariantAction, "REFUSED", "%"+docID+"%"); got != 1 {
		t.Fatalf("after an unchanged save: %d rows, want still 1", got)
	}

	// A FLAGGED invariant (control totals disagreeing) is one row a day, not one per open.
	rule := "control_totals:" + docID
	LogInvariantEvent("default", "stage49111-accounts", rule, "FLAGGED", "detail 10.00 vs summary 9.00 (diff 1.00)")
	LogInvariantEvent("default", "stage49111-accounts", rule, "FLAGGED", "detail 10.00 vs summary 9.00 (diff 1.00)")
	if got := stage49111Rows(t, InvariantAction, "FLAGGED", "rule="+rule+" %"); got != 1 {
		t.Fatalf("flagged twice in a day: %d rows, want 1", got)
	}
}

func TestStage49111VerifierFailureIsAnAuditEvent(t *testing.T) {
	db.InitDB(testConnStr())
	mark := fmt.Sprintf("cp-49111-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		stage49111Clean(t, AuditVerifyAction, "%"+mark+"%")
	})

	// A clean run writes nothing.
	RecordAuditVerification("default", &AuditVerification{Intact: true, Verified: 3}, nil)
	// A break: FAILED, naming the rows and checkpoints.
	RecordAuditVerification("default", &AuditVerification{Intact: false, TamperedRows: []string{"row-" + mark}, CheckpointsBad: []string{mark}}, nil)
	if got := stage49111Rows(t, AuditVerifyAction, "FAILED", "1 tampered row(s) [row-"+mark+"], 1 bad checkpoint(s) ["+mark+"]%"); got != 1 {
		t.Fatalf("broken chain: %d AUDIT_VERIFY/FAILED rows, want 1", got)
	}
	// Could not run: UNVERIFIED, never a pass.
	RecordAuditVerification("default", nil, errors.New("keyring unreadable "+mark))
	if got := stage49111Rows(t, AuditVerifyAction, "UNVERIFIED", "%keyring unreadable "+mark+"%"); got != 1 {
		t.Fatalf("verifier could not run: %d AUDIT_VERIFY/UNVERIFIED rows, want 1", got)
	}

	// The Security pane shows the last result.
	p, err := GetSecurityPosture("default")
	if err != nil {
		t.Fatalf("posture: %v", err)
	}
	if p.LastVerify == nil || p.LastVerify.Outcome != "Could not run" {
		t.Fatalf("posture last verify = %+v, want the UNVERIFIED run as 'Could not run'", p.LastVerify)
	}
}
