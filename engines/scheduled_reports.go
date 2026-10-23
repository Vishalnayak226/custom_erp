package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Stage 26.10.4 (Reports and BI Sprint): scheduled report delivery. Reuses
// Stage 20.37's async-export machinery (RunReport + reportRowsToCSV,
// engines/report_export.go) to actually run the report, and the existing
// outbox ticker (engines/outbox.go's PublishEvent/processOutbox) for the
// "drop" itself - same simulated-dispatch pattern every other outbound
// integration in this codebase already uses (no real SMTP/webhook HTTP
// client is introduced, matching the lightweight-first principle and
// avoiding an SSRF surface against arbitrary user-supplied webhook URLs).
// ScheduledReport is a plain registered doctype (see this Stage's migration
// file) - a user creates/lists/deletes one through the existing generic doc
// API exactly like ReportFilterPreset/ReportExportJob already do; this
// worker is the only thing that ever writes back to it, advancing
// next_run_date/last_run_at/last_run_status after each run.

// StartScheduledReportWorker polls every tenant schema for Active
// ScheduledReport documents whose next_run_date has arrived, same
// ticker/schema-fanout shape as StartReportExportWorker/StartOutboxWorker.
func StartScheduledReportWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() { // 134.7: one server runs each sweep
					continue
				}
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas("reports")
				if err != nil {
					log.Printf("[SCHEDULED_REPORT] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					processScheduledReports(ctx, schema)
				}
			}
		}
	}()
}

// BLD-046: takes the worker's ctx so a shutting-down process abandons an
// in-flight run instead of finishing one whose result nobody will collect.
// The job/schedule row is left untouched and is reclaimed on the next tick.
func processScheduledReports(ctx context.Context, schema string) {
	// BLD-046: see report_export.go's processReportExportJobs for why this
	// resolution (rather than passing schema itself into RunReport below)
	// matters - same wrong-tenant bug, same fix.
	tenantID, err := tenantIDForSchema(schema)
	if err != nil {
		log.Printf("[SCHEDULED_REPORT] could not resolve tenant for schema %s: %v", schema, err)
		return
	}
	// BLD-046: the due-date filter moved into SQL (data->>'next_run_date' is
	// a plain ISO 'YYYY-MM-DD' string, so lexicographic <= is a correct date
	// comparison) so the new LIMIT bounds actually-due work per tick instead
	// of bounding an arbitrary slice of every Active schedule regardless of
	// whether it's due - the previous unbounded fetch-then-filter-in-Go
	// shape would otherwise let a LIMIT here silently starve genuinely due
	// schedules sorted after not-yet-due ones.
	today := time.Now().Format("2006-01-02")
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'ScheduledReport' AND `+scheduleDueSQL+`
		 LIMIT $2`, schema),
		today, asyncReportWorkerBatchSize)
	if err != nil {
		LogBackgroundJobError("SCHEDULED_REPORT", "query", schema, err)
		return
	}
	type dueSchedule struct {
		id   string
		data map[string]interface{}
	}
	var due []dueSchedule
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			continue
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			log.Printf("[SCHEDULED_REPORT] corrupt ScheduledReport %s: %v", id, err)
			continue
		}
		due = append(due, dueSchedule{id: id, data: data})
	}
	rows.Close()

	for _, s := range due {
		// 73.D.18: claimed first, so a Run now on the same tick (or a second
		// server) cannot deliver it as well.
		if data, ok := claimDueSchedule(schema, "ScheduledReport", s.id, today); ok {
			runScheduledReport(ctx, schema, tenantID, s.id, "", data)
		}
	}
}

// claimDueSchedule (73.D.18) marks one due schedule Running in a single
// UPDATE that only succeeds if it is still due and not already running - the
// same claim Run now takes (ClaimReportScheduleRun) - and returns its data.
// A Running mark older than the hard run timeout is a run that died; it may
// be claimed again.
func claimDueSchedule(schema, doctype, id, today string) (map[string]interface{}, bool) {
	now := time.Now()
	var dataStr string
	err := db.DB.QueryRow(fmt.Sprintf(`UPDATE %s.documents
		SET data = data || jsonb_build_object('last_run_status', 'Running', 'last_run_at', $3::text), updated_at = CURRENT_TIMESTAMP
		WHERE doctype = $2 AND id = $4 AND `+scheduleDueSQL+`
		  AND (COALESCE(data->>'last_run_status', '') <> 'Running'
		       OR COALESCE(NULLIF(data->>'last_run_at', ''), '1970-01-01T00:00:00Z')::timestamptz < $5::timestamptz)
		RETURNING data`, schema), today, doctype, now.Format(time.RFC3339), id, now.Add(-reportRunHardTimeout).Format(time.RFC3339)).Scan(&dataStr)
	if err != nil {
		if err != sql.ErrNoRows {
			LogBackgroundJobError("SCHEDULED_REPORT", "claim", schema, err)
		}
		return nil, false
	}
	var data map[string]interface{}
	if json.Unmarshal([]byte(dataStr), &data) != nil {
		return nil, false
	}
	return data, true
}

// scheduleDueSQL is the one definition of "the worker runs this schedule on
// its next tick", shared by both schedule workers (this one and
// DashboardDigest's) and the Schedules view (report_schedule_view.go), so the
// screen can never disagree with what the worker will do. $1 is today's date
// as YYYY-MM-DD; columns are unqualified, so use it against one documents
// table. Stage 55.20 added deleted_at: a schedule deleted through the generic
// doc API keeps its Active status, and used to keep being delivered.
const scheduleDueSQL = `status = 'Active' AND deleted_at IS NULL
	AND data->>'next_run_date' IS NOT NULL AND data->>'next_run_date' != '' AND data->>'next_run_date' <= $1`

// runScheduledReport runs and delivers one ScheduledReport, then advances it.
// It is the worker's per-schedule step and also what "Run now" calls
// (report_schedule_view.go), so a manual run behaves exactly like a scheduled
// one. runBy is who asked - "" for the worker - and lands on the run log.
func runScheduledReport(ctx context.Context, schema, tenantID, id, runBy string, data map[string]interface{}) {
	reportID, _ := data["report_id"].(string)
	role, _ := data["requested_role"].(string)
	paramsStr, _ := data["params_json"].(string)
	var params map[string]string
	if paramsStr != "" {
		if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
			log.Printf("[SCHEDULED_REPORT] corrupt params_json for %s: %v", id, err)
		}
	}

	runStatus := "Delivered"
	def, resultRows, _, err := RunReportContext(ctx, tenantID, reportID, role, runBy, params)
	if err != nil {
		runStatus = "Failed: " + err.Error()
	} else if csvText, cerr := reportRowsToCSV(*def, resultRows); cerr != nil {
		runStatus = "Failed: " + cerr.Error()
	} else {
		recipientEmail, _ := data["recipient_email"].(string)
		webhookURL, _ := data["webhook_url"].(string)
		tx, terr := db.DB.Begin()
		if terr == nil {
			_ = db.SetSearchPath(tx, schema)
			if perr := PublishEvent(tx, schema, "report.scheduled_delivery", map[string]interface{}{
				"schedule_id": id, "report_id": reportID, "row_count": len(resultRows),
				"recipient_email": recipientEmail, "webhook_url": webhookURL, "csv": csvText,
			}); perr == nil {
				_ = tx.Commit()
			} else {
				_ = tx.Rollback()
				runStatus = "Failed: " + perr.Error()
			}
		} else {
			runStatus = "Failed: " + terr.Error()
		}
	}

	// Stage 55.20: the row count, for the Schedules view's last result. A
	// blank next_run_date (only reachable through Run now - the worker never
	// selects one) stays blank rather than starting a schedule nobody set.
	delete(data, "last_run_rows")
	if err == nil {
		data["last_run_rows"] = len(resultRows)
	}
	if nextRun, _ := data["next_run_date"].(string); nextRun != "" {
		frequency, _ := data["frequency"].(string)
		data["next_run_date"] = advanceScheduleDate(nextRun, frequency)
	}
	data["last_run_at"] = time.Now().Format(time.RFC3339)
	data["last_run_status"] = runStatus
	marshaled, merr := json.Marshal(data)
	if merr != nil {
		log.Printf("[SCHEDULED_REPORT] marshal failed for %s: %v", id, merr)
		return
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ScheduledReport' AND id = $2`, schema),
		marshaled, id); err != nil {
		log.Printf("[SCHEDULED_REPORT] update failed for %s: %v", id, err)
	}
}

// advanceScheduleDate moves a ScheduledReport's next_run_date forward past
// every period it missed (e.g. the server was down for a few days) in one
// step, landing on the next date that's genuinely in the future - it never
// re-delivers each individually-missed occurrence.
//
// Compares formatted calendar-date strings, not raw time.Time values: `t`
// comes from time.Parse("2006-01-02", ...), which lands on UTC midnight,
// while time.Now() carries the server's local time-of-day - t.After(today)
// on those two can read today's own UTC-midnight as "not after" a
// same-day local "now", making the loop stop one day short of a genuine
// future date depending on the server's timezone offset and time of day.
func advanceScheduleDate(from, frequency string) string {
	t, err := time.Parse("2006-01-02", from)
	if err != nil {
		t = time.Now()
	}
	todayStr := time.Now().Format("2006-01-02")
	for t.Format("2006-01-02") <= todayStr {
		switch frequency {
		case "Weekly":
			t = t.AddDate(0, 0, 7)
		case "Monthly":
			t = t.AddDate(0, 1, 0)
		default: // Daily
			t = t.AddDate(0, 0, 1)
		}
	}
	return t.Format("2006-01-02")
}
