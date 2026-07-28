package engines

import (
	"bytes"
	"context"
	"custom_erp/db"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Stage 20.37: async/scheduled export for heavy reports, reusing the
// existing outbox-style ticker/worker pattern (engines/outbox.go,
// patchintake.go) rather than a new job-queue dependency. A ReportExportJob
// is created Pending and returned immediately; the worker below runs the
// actual report and stores the generated CSV directly in the job's own
// JSONB document - the same "no new file-storage mechanism" trick
// Stage 15.2's ImportJob.error_csv already uses.

// CreateReportExportJob queues a report to run in the background and
// returns the job id immediately - the point of "async" for a report heavy
// enough that running it inline would block the request.
func CreateReportExportJob(tenantID, reportID, role string, params map[string]string, userID string) (string, error) {
	if _, ok := reportRegistry[reportID]; !ok {
		return "", fmt.Errorf("unknown report %q", reportID)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	jobID := fmt.Sprintf("RPTEXP-%d", time.Now().UnixNano())
	data := map[string]interface{}{
		"id": jobID, "code": jobID, "report_id": reportID, "requested_role": role,
		"params": string(paramsJSON), "status": "Pending",
	}
	marshaled, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'ReportExportJob', $2, 'Pending', $3)`, schema),
		jobID, marshaled, userID); err != nil {
		return "", err
	}
	return jobID, nil
}

// GetReportExportJob returns a job's current status, (once Completed) its
// generated CSV, and (once Failed) the REPORT-0285 code processReportExportJobs
// tagged on it.
func GetReportExportJob(tenantID, jobID string) (status string, csvBytes []byte, code string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, "", err
	}
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'ReportExportJob' AND id = $1`, schema), jobID).Scan(&dataStr); err != nil {
		return "", nil, "", fmt.Errorf("export job not found: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return "", nil, "", err
	}
	status, _ = data["status"].(string)
	code, _ = data["code"].(string)
	if csvStr, ok := data["csv"].(string); ok {
		csvBytes = []byte(csvStr)
	}
	return status, csvBytes, code, nil
}

// StartReportExportWorker polls every tenant schema for Pending
// ReportExportJob documents and runs them, same ticker shape as
// StartOutboxWorker/StartPatchIntakeWorker.
func StartReportExportWorker(ctx context.Context, interval time.Duration) {
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
					log.Printf("[REPORT_EXPORT] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					processReportExportJobs(schema)
				}
			}
		}
	}()
}

func processReportExportJobs(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'ReportExportJob' AND status = 'Pending'`, schema))
	if err != nil {
		log.Printf("[REPORT_EXPORT] query failed for %s: %v", schema, err)
		return
	}
	type pendingJob struct {
		id   string
		data map[string]interface{}
	}
	var jobs []pendingJob
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			continue
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			// 24.18: a nil data map would otherwise panic later in this
			// same function ("assignment to entry in nil map" on
			// job.data["status"] = newStatus) - an unrecovered panic inside
			// this background worker's goroutine would crash the whole
			// process, not just fail one job. Substitute an empty map so
			// the job still runs (and gets marked Failed via RunReport's
			// own error path) instead.
			log.Printf("[REPORT_EXPORT] corrupt ReportExportJob %s: %v", id, err)
			data = map[string]interface{}{}
		}
		jobs = append(jobs, pendingJob{id: id, data: data})
	}
	rows.Close()

	for _, job := range jobs {
		reportID, _ := job.data["report_id"].(string)
		role, _ := job.data["requested_role"].(string)
		paramsStr, _ := job.data["params"].(string)
		var params map[string]string
		if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
			log.Printf("[REPORT_EXPORT] corrupt params for job %s: %v", job.id, err)
		}

		newStatus := "Completed"
		csvText := ""
		def, resultRows, _, err := RunReport(schema, reportID, role, "", params)
		if err != nil {
			// REPORT-0285 ("Export job failed") is this async path's own
			// distinct catalog code, deliberately separate from REPORT-0162
			// ("Report generation failed") which RunReport itself attaches
			// for the *synchronous* handleRunReport caller - same
			// underlying def.Run() error, different surfaced scenario
			// depending on which path hit it.
			newStatus = "Failed"
			job.data["error"] = err.Error()
			job.data["code"] = "REPORT-0285"
		} else {
			csvText, err = reportRowsToCSV(*def, resultRows)
			if err != nil {
				newStatus = "Failed"
				job.data["error"] = err.Error()
				job.data["code"] = "REPORT-0285"
			} else {
				job.data["csv"] = csvText
			}
		}
		job.data["status"] = newStatus
		marshaled, err := json.Marshal(job.data)
		if err != nil {
			log.Printf("[REPORT_EXPORT] marshal failed for job %s: %v", job.id, err)
			continue
		}
		if _, err := db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ReportExportJob' AND id = $3`, schema),
			marshaled, newStatus, job.id); err != nil {
			log.Printf("[REPORT_EXPORT] update failed for job %s: %v", job.id, err)
		}
	}
}

// reportRowsToCSV renders a report's rows as CSV using its own column
// metadata for header labels and a stable column order (map iteration
// order is undefined in Go, so a report's declared Columns order is what
// keeps the exported CSV's columns consistent run to run).
func reportRowsToCSV(def ReportDefinition, rows []map[string]interface{}) (string, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	headers := make([]string, len(def.Columns))
	keys := make([]string, len(def.Columns))
	for i, c := range def.Columns {
		headers[i] = c.Label
		keys[i] = c.Key
	}
	if err := w.Write(headers); err != nil {
		return "", err
	}
	for _, row := range rows {
		record := make([]string, len(keys))
		for i, k := range keys {
			record[i] = fmt.Sprintf("%v", row[k])
		}
		if err := w.Write(record); err != nil {
			return "", err
		}
	}
	w.Flush()
	return buf.String(), nil
}
