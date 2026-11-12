package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// TestStage73D18ScheduleClaimedOnce is 73.D.18: the schedule workers claim a
// due schedule in one UPDATE, so a Run now on the same tick and the worker
// never both deliver it.
func TestStage73D18ScheduleClaimedOnce(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	const id = "TEST73D18-SCHED"
	cleanup := func() { db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id) }
	cleanup()
	defer cleanup()
	today := time.Now().Format("2006-01-02")
	raw, _ := json.Marshal(map[string]interface{}{"report_id": "grn-waiting", "frequency": "Daily", "next_run_date": today})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ScheduledReport', $2, 'Active', 'admin')", id, raw); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if _, ok := claimDueSchedule(schema, "ScheduledReport", id, today); !ok {
		t.Fatalf("a due schedule nobody is running must be claimed")
	}
	if _, ok := claimDueSchedule(schema, "ScheduledReport", id, today); ok {
		t.Fatalf("a second claim of a running schedule must fail")
	}
	var busy *ReportScheduleBusyError
	if _, err := ClaimReportScheduleRun(tenantID, "report", id, "admin", RoleSuperAdmin); !errors.As(err, &busy) {
		t.Fatalf("Run now while the worker runs it must be refused as busy, got %v", err)
	}

	// A Running mark past the hard timeout is a run that died: claimable.
	stale := time.Now().Add(-reportRunHardTimeout - time.Minute).Format(time.RFC3339)
	db.DB.Exec("UPDATE "+schema+".documents SET data = jsonb_set(data, '{last_run_at}', to_jsonb($1::text)) WHERE id = $2", stale, id)
	if _, ok := claimDueSchedule(schema, "ScheduledReport", id, today); !ok {
		t.Fatalf("a schedule whose run died must be claimable again")
	}

	// Not due: never claimed.
	db.DB.Exec("UPDATE "+schema+".documents SET data = data || '{\"last_run_status\":\"Delivered\",\"next_run_date\":\"2999-01-01\"}'::jsonb WHERE id = $1", id)
	if _, ok := claimDueSchedule(schema, "ScheduledReport", id, today); ok {
		t.Fatalf("a schedule that is not due must not be claimed")
	}
}
