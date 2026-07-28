package engines

import (
	"context"
	"custom_erp/db"
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
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[SCHEDULED_REPORT] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					processScheduledReports(schema)
				}
			}
		}
	}()
}

func processScheduledReports(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'ScheduledReport' AND status = 'Active'`, schema))
	if err != nil {
		log.Printf("[SCHEDULED_REPORT] query failed for %s: %v", schema, err)
		return
	}
	type dueSchedule struct {
		id   string
		data map[string]interface{}
	}
	var due []dueSchedule
	today := time.Now().Format("2006-01-02")
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
		nextRun, _ := data["next_run_date"].(string)
		if nextRun != "" && nextRun <= today {
			due = append(due, dueSchedule{id: id, data: data})
		}
	}
	rows.Close()

	for _, s := range due {
		reportID, _ := s.data["report_id"].(string)
		role, _ := s.data["requested_role"].(string)
		paramsStr, _ := s.data["params_json"].(string)
		var params map[string]string
		if paramsStr != "" {
			if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
				log.Printf("[SCHEDULED_REPORT] corrupt params_json for %s: %v", s.id, err)
			}
		}

		runStatus := "Delivered"
		def, resultRows, _, err := RunReport(schema, reportID, role, "", params)
		if err != nil {
			runStatus = "Failed: " + err.Error()
		} else if csvText, cerr := reportRowsToCSV(*def, resultRows); cerr != nil {
			runStatus = "Failed: " + cerr.Error()
		} else {
			recipientEmail, _ := s.data["recipient_email"].(string)
			webhookURL, _ := s.data["webhook_url"].(string)
			tx, terr := db.DB.Begin()
			if terr == nil {
				_ = db.SetSearchPath(tx, schema)
				if perr := PublishEvent(tx, schema, "report.scheduled_delivery", map[string]interface{}{
					"schedule_id": s.id, "report_id": reportID, "row_count": len(resultRows),
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

		nextRun, _ := s.data["next_run_date"].(string)
		frequency, _ := s.data["frequency"].(string)
		s.data["next_run_date"] = advanceScheduleDate(nextRun, frequency)
		s.data["last_run_at"] = time.Now().Format(time.RFC3339)
		s.data["last_run_status"] = runStatus
		marshaled, merr := json.Marshal(s.data)
		if merr != nil {
			log.Printf("[SCHEDULED_REPORT] marshal failed for %s: %v", s.id, merr)
			continue
		}
		if _, err := db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ScheduledReport' AND id = $2`, schema),
			marshaled, s.id); err != nil {
			log.Printf("[SCHEDULED_REPORT] update failed for %s: %v", s.id, err)
		}
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
