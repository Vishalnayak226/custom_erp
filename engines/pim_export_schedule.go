package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// Stage 36.4.2: PIMExportSchedule delivers a PIMExportTemplate's output on a
// Daily/Weekly/Monthly cadence through the existing outbox
// (engines/outbox.go's PublishEvent/processOutbox) - the same
// simulated-dispatch pattern Stage 26.10.4's ScheduledReport already uses
// for email/webhook delivery, so this does not introduce a real SMTP/webhook
// HTTP client (and the SSRF surface an arbitrary user-supplied webhook URL
// would otherwise open).

// ValidatePIMExportScheduleDocument runs at ValidateDocument's shared exit.
func ValidatePIMExportScheduleDocument(tenantID string, payload map[string]interface{}) error {
	// 146.5.6: a schedule may run a feed instead of naming a template; the feed
	// carries the template, the group, the format and where the file goes.
	if feed := strings.TrimSpace(pimString(payload["feed"])); feed != "" {
		if db.DB != nil {
			if _, _, _, err := fetchPIMFeed(tenantID, feed); err != nil {
				return &ValidationError{Code: "META-0198", SubFor: "Feed", Message: err.Error()}
			}
		}
		if frequency := strings.TrimSpace(pimString(payload["frequency"])); frequency != "Daily" && frequency != "Weekly" && frequency != "Monthly" {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Frequency", Message: "frequency must be Daily, Weekly or Monthly"}
		}
		return validateISODate("Next Run Date", pimString(payload["next_run_date"]), true)
	}
	template := strings.TrimSpace(pimString(payload["export_template"]))
	if template == "" {
		return &ValidationError{Code: "GLOBAL-0001", SubFor: "Export Template", Message: "an export schedule needs a template"}
	}
	if db.DB != nil {
		if _, _, _, _, _, err := fetchPIMExportTemplate(tenantID, template); err != nil {
			return &ValidationError{Code: "META-0198", SubFor: "Export Template", Message: err.Error()}
		}
	}

	frequency := strings.TrimSpace(pimString(payload["frequency"]))
	if frequency != "Daily" && frequency != "Weekly" && frequency != "Monthly" {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Frequency", Message: "frequency must be Daily, Weekly or Monthly"}
	}
	if err := validateISODate("Next Run Date", pimString(payload["next_run_date"]), true); err != nil {
		return err
	}

	recipientEmail := strings.TrimSpace(pimString(payload["recipient_email"]))
	webhookURL := strings.TrimSpace(pimString(payload["webhook_url"]))
	if recipientEmail == "" && webhookURL == "" {
		return &ValidationError{Code: "GLOBAL-0001", SubFor: "Recipient Email", Message: "an export schedule needs a recipient email or a webhook URL to deliver to"}
	}
	return nil
}

// StartPIMExportScheduleWorker polls every tenant schema for Active
// PIMExportSchedules whose next_run_date has arrived - same ticker/
// schema-fanout shape as StartScheduledReportWorker/StartPIMImportScheduleWorker.
func StartPIMExportScheduleWorker(ctx context.Context, interval time.Duration) {
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
				schemas, err := sweepTenantSchemas("pim")
				if err != nil {
					log.Printf("[PIM_EXPORT_SCHEDULE] failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					processPIMExportSchedules(schema)
				}
			}
		}
	}()
}

func processPIMExportSchedules(schema string) {
	// BLD-046: due-date filter moved into SQL alongside a new per-tick LIMIT
	// (asyncReportWorkerBatchSize, report_export.go) - same fix shape as
	// scheduled_reports.go/dashboard.go, so a LIMIT bounds actually-due
	// schedules instead of an arbitrary slice of every Active one.
	today := BusinessToday()
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'PIMExportSchedule' AND status = 'Active' AND deleted_at IS NULL
		   AND data->>'next_run_date' IS NOT NULL AND data->>'next_run_date' != '' AND data->>'next_run_date' <= $1
		 LIMIT $2`, schema),
		today, asyncReportWorkerBatchSize)
	if err != nil {
		LogBackgroundJobError("PIM_EXPORT_SCHEDULE", "query", schema, err)
		return
	}
	type due struct {
		id   string
		data map[string]interface{}
	}
	var candidates []due
	for rows.Next() {
		var id, raw string
		if sErr := rows.Scan(&id, &raw); sErr != nil {
			continue
		}
		var data map[string]interface{}
		if uErr := json.Unmarshal([]byte(raw), &data); uErr != nil {
			continue
		}
		candidates = append(candidates, due{id: id, data: data})
	}
	rows.Close()
	if len(candidates) == 0 {
		return
	}

	tenantID, err := tenantIDForSchema(schema)
	if err != nil {
		return
	}
	for _, c := range candidates {
		runPIMExportSchedule(tenantID, schema, c.id, c.data)
	}
}

func runPIMExportSchedule(tenantID, schema, scheduleID string, data map[string]interface{}) {
	template := pimString(data["export_template"])
	runStatus := "Delivered"
	var csvBytes []byte
	var err error
	if GetSettingBool(tenantID, SettingFreezeExports) {
		// 146.5.6: the export freeze is enforced on requests; a schedule is not
		// one, so it stops here. The date still moves on: unfreezing sends no backlog.
		LogAuditEvent(tenantID, "system", "DATA_EXPORT", "REFUSED_FROZEN", "PIMExportSchedule "+scheduleID)
		runStatus = "Refused: exports are paused for this business"
	} else if feed := pimString(data["feed"]); feed != "" {
		// 146.5.6: the feed does the export, the delivery and the run record
		run, ferr := RunPIMFeed(tenantID, "system", feed)
		switch {
		case ferr != nil:
			runStatus = "Failed: " + ferr.Error()
		case run.Delivery == EmailFailed || run.Delivery == EmailNotSent:
			runStatus = run.Delivery + ": " + run.DeliveryMessage
		default:
			runStatus = run.Status
		}
		err = ferr
	} else if csvBytes, err = RunPIMExportTemplate(tenantID, template); err != nil {
		runStatus = "Failed: " + err.Error()
	} else {
		recipientEmail := pimString(data["recipient_email"])
		webhookURL := pimString(data["webhook_url"])
		tx, terr := db.DB.Begin()
		if terr == nil {
			_ = db.SetSearchPath(tx, schema)
			if perr := PublishEvent(tx, schema, "pim.export.scheduled_delivery", map[string]interface{}{
				"schedule_id": scheduleID, "export_template": template, "line_count": csvLineCount(csvBytes),
				"recipient_email": recipientEmail, "webhook_url": webhookURL, "csv": string(csvBytes),
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

	nextRun := pimString(data["next_run_date"])
	frequency := pimString(data["frequency"])
	data["next_run_date"] = advanceScheduleDate(nextRun, frequency)
	data["last_run_at"] = time.Now().Format(time.RFC3339)
	data["last_run_status"] = runStatus
	marshaled, merr := json.Marshal(data)
	if merr != nil {
		log.Printf("[PIM_EXPORT_SCHEDULE] marshal failed for %s: %v", scheduleID, merr)
		return
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'PIMExportSchedule' AND id = $2`, schema),
		marshaled, scheduleID); err != nil {
		log.Printf("[PIM_EXPORT_SCHEDULE] update failed for %s: %v", scheduleID, err)
	}
}

// csvLineCount counts newline-terminated lines (header included, if any) -
// a cheap eyeballing figure for the outbox event, not a substitute for
// actually parsing the CSV.
func csvLineCount(b []byte) int {
	count := 0
	for _, c := range b {
		if c == '\n' {
			count++
		}
	}
	return count
}
