package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// Stage 55.20: background report exports belong to whoever queued them.
//
// A ReportExportJob's CSV is the report as its requester was allowed to see
// it, so it is private to that person: only they (or a Super Admin) can list,
// read, download or cancel it. A Pending job can be cancelled - the export
// worker only ever picks up Pending jobs, so a Cancelled one is never run.

// ReportExportJobSummary is one row of "My exports".
type ReportExportJobSummary struct {
	ID          string            `json:"id"`
	ReportID    string            `json:"report_id"`
	ReportLabel string            `json:"report_label"`
	Status      string            `json:"status"`
	Params      map[string]string `json:"params"`
	Code        string            `json:"code,omitempty"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

// ReportExportJobVisible reports whether userID may see this job.
func ReportExportJobVisible(tenantID, jobID, userID, role string) (bool, error) {
	if HasFullAccess(role) {
		return true, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	var owner string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT created_by FROM %s.documents WHERE doctype = 'ReportExportJob' AND id = $1`, schema), jobID).Scan(&owner); err != nil {
		return false, nil
	}
	return owner == userID, nil
}

// ListMyReportExportJobs is the requester's own recent exports, newest first.
func ListMyReportExportJobs(tenantID, userID string, limit int) ([]ReportExportJobSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data, status, created_at::text, COALESCE(updated_at, created_at)::text FROM %s.documents
		WHERE doctype = 'ReportExportJob' AND created_by = $1 ORDER BY created_at DESC LIMIT $2`, schema), userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportExportJobSummary{}
	for rows.Next() {
		var s ReportExportJobSummary
		var dataStr string
		if err := rows.Scan(&s.ID, &dataStr, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(dataStr), &d) == nil {
			s.ReportID, _ = d["report_id"].(string)
			if code, _ := d["code"].(string); code != s.ID {
				s.Code = code // a Failed job's REPORT-xxxx code; otherwise code is the job id itself
			}
			s.Params = map[string]string{}
			if p, ok := d["params"].(string); ok {
				_ = json.Unmarshal([]byte(p), &s.Params)
			}
		}
		if def, ok := reportRegistry[s.ReportID]; ok {
			s.ReportLabel = def.Label
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CancelReportExportJob cancels the caller's own Pending export.
func CancelReportExportJob(tenantID, jobID, userID, role string) error {
	visible, err := ReportExportJobVisible(tenantID, jobID, userID, role)
	if err != nil {
		return err
	}
	if !visible {
		return fmt.Errorf("export job not found")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET status = 'Cancelled', data = jsonb_set(data, '{status}', '"Cancelled"'), updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'ReportExportJob' AND id = $1 AND status = 'Pending'`, schema), jobID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("only a Pending export can be cancelled - this one has already run")
	}
	return nil
}
