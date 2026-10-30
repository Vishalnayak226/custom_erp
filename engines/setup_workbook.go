package engines

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"custom_erp/db"
)

// Stage 128: a Setup Workbook run - one upload applied to an organisation -
// is kept as an ImportJob document (doctype_name "SetupWorkbook"), so the
// Import Jobs list shows it beside the CSV imports and no table is added. It
// holds the uploaded file, so any earlier run can be downloaded and uploaded
// again, its progress while it runs, and the result of every row.

// SetupWorkbookDoctype is the ImportJob doctype_name a run is filed under.
const SetupWorkbookDoctype = "SetupWorkbook"

// BulkPurchaseDoctype files a Bulk Purchase Upload run (Stage 129) the same way.
const BulkPurchaseDoctype = "BulkPurchase"

// BulkSalesDoctype files a Sales Upload from Excel run (Stage 142.3) the same way.
const BulkSalesDoctype = "BulkSales"

// setupWorkbookStaleAfter: a run still "Running" this long after its last
// progress write was cut off (the server restarted mid-run). Re-uploading
// the same file finishes it, since applying a workbook twice changes nothing
// the second time.
const setupWorkbookStaleAfter = 10 * time.Minute

// CreateSetupWorkbookRun records a run that is about to start.
func CreateSetupWorkbookRun(tenantID, createdBy, fileName string, file []byte) (string, error) {
	return CreateWorkbookRun(tenantID, SetupWorkbookDoctype, createdBy, fileName, file)
}

// CreateWorkbookRun records a run of an uploaded workbook of the given kind
// (SetupWorkbookDoctype, BulkPurchaseDoctype) that is about to start.
func CreateWorkbookRun(tenantID, kind, createdBy, fileName string, file []byte) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	id := NewDocIDCompact("IMPJOB")
	data := map[string]interface{}{
		"id": id, "code": id, "doctype_name": kind, "status": "Running",
		"file_name": fileName, "file_b64": base64.StdEncoding.EncodeToString(file),
		"total_rows": 0, "success_rows": 0, "failed_rows": 0,
		"started_at": time.Now().UTC().Format(time.RFC3339),
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'ImportJob', $2, 'Running', $3)`, schema), id, raw, createdBy); err != nil {
		return "", err
	}
	return id, nil
}

// UpdateSetupWorkbookRun merges fields into a run; a "status" field also
// moves the document's own status.
func UpdateSetupWorkbookRun(tenantID, id string, fields map[string]interface{}) error {
	return UpdateWorkbookRun(tenantID, SetupWorkbookDoctype, id, fields)
}

// UpdateWorkbookRun is UpdateSetupWorkbookRun for a run of any kind.
func UpdateWorkbookRun(tenantID, kind, id string, fields map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	status, _ := fields["status"].(string)
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb,
		status = CASE WHEN $2 = '' THEN status ELSE $2 END, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3 AND doctype = 'ImportJob' AND data->>'doctype_name' = $4`, schema),
		raw, status, id, kind)
	return err
}

// ListSetupWorkbookRuns returns the latest runs, newest first, without
// their files.
func ListSetupWorkbookRuns(tenantID string, limit int) ([]map[string]interface{}, error) {
	return ListWorkbookRuns(tenantID, SetupWorkbookDoctype, limit)
}

// ListWorkbookRuns is ListSetupWorkbookRuns for runs of any kind.
func ListWorkbookRuns(tenantID, kind string, limit int) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT (data - 'file_b64')::text, created_by, created_at, COALESCE(updated_at, created_at)
		FROM %s.documents WHERE doctype = 'ImportJob' AND data->>'doctype_name' = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT $2`, schema), kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var raw, by string
		var created, updated time.Time
		if err := rows.Scan(&raw, &by, &created, &updated); err != nil {
			return nil, err
		}
		run := map[string]interface{}{}
		if err := json.Unmarshal([]byte(raw), &run); err != nil {
			continue
		}
		run["created_by"] = by
		run["created_at"] = created
		markStaleSetupWorkbookRun(run, updated)
		out = append(out, run)
	}
	return out, rows.Err()
}

// GetSetupWorkbookRun returns one run without its file.
func GetSetupWorkbookRun(tenantID, id string) (map[string]interface{}, error) {
	return GetWorkbookRun(tenantID, SetupWorkbookDoctype, id)
}

// GetWorkbookRun is GetSetupWorkbookRun for a run of any kind.
func GetWorkbookRun(tenantID, kind, id string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var raw, by string
	var created, updated time.Time
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT (data - 'file_b64')::text, created_by, created_at, COALESCE(updated_at, created_at)
		FROM %s.documents WHERE id = $1 AND doctype = 'ImportJob' AND data->>'doctype_name' = $2 AND deleted_at IS NULL`, schema),
		id, kind).Scan(&raw, &by, &created, &updated)
	if err != nil {
		return nil, fmt.Errorf("upload run %s not found", id)
	}
	run := map[string]interface{}{}
	if err := json.Unmarshal([]byte(raw), &run); err != nil {
		return nil, err
	}
	run["created_by"] = by
	run["created_at"] = created
	markStaleSetupWorkbookRun(run, updated)
	return run, nil
}

// GetSetupWorkbookRunFile returns the workbook a run was given.
func GetSetupWorkbookRunFile(tenantID, id string) (string, []byte, error) {
	return GetWorkbookRunFile(tenantID, SetupWorkbookDoctype, id)
}

// GetWorkbookRunFile is GetSetupWorkbookRunFile for a run of any kind.
func GetWorkbookRunFile(tenantID, kind, id string) (string, []byte, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, err
	}
	var name, b64 string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'file_name', ''), COALESCE(data->>'file_b64', '')
		FROM %s.documents WHERE id = $1 AND doctype = 'ImportJob' AND data->>'doctype_name' = $2 AND deleted_at IS NULL`, schema),
		id, kind).Scan(&name, &b64)
	if err != nil || b64 == "" {
		return "", nil, fmt.Errorf("upload run %s has no file", id)
	}
	file, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", nil, err
	}
	if name == "" {
		name = id + ".xlsx"
	}
	return name, file, nil
}

func markStaleSetupWorkbookRun(run map[string]interface{}, updated time.Time) {
	if run["status"] == "Running" && time.Since(updated) > setupWorkbookStaleAfter {
		run["status"] = "Interrupted"
		run["message"] = "The server stopped while this was being applied. Upload the same file again - rows already applied are left as they are."
	}
}
