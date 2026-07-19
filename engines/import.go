package engines

import (
	"bytes"
	"custom_erp/db"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type ImportResult struct {
	TotalRows   int                  `json:"total_rows"`
	SuccessRows int                  `json:"success_rows"`
	FailedRows  int                  `json:"failed_rows"`
	Errors      []RowValidationError `json:"errors"`
	// CreatedIDs/UpdatedIDs (Stage 15.2, V2 §6.2/§16 Phase 3): populated on
	// every run (not just dryRun) via an existence check per row, so the
	// same struct serves both the plain import endpoint and the new
	// create/update/conflict/reject preview - additive, non-breaking for
	// existing consumers that only read TotalRows/SuccessRows/FailedRows/Errors.
	CreatedIDs []string `json:"created_ids,omitempty"`
	UpdatedIDs []string `json:"updated_ids,omitempty"`
	DryRun     bool     `json:"dry_run"`
}

type RowValidationError struct {
	RowNumber int    `json:"row_number"`
	Message   string `json:"message"`
}

// sanitizeCSVCell prevents spreadsheet applications from treating imported
// or exported ERP text as a formula. A leading apostrophe is their standard
// text escape and deliberately remains part of the stored/exported value.
func sanitizeCSVCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t")
	if trimmed == "" {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

// BulkImportCSV parses a CSV body, validates constraints, and inserts valid
// records inside a transaction. dryRun=true (Stage 15.2) runs the exact same
// validation/existence-check logic but never commits - the transaction is
// always rolled back via the existing deferred tx.Rollback(), so a preview
// can classify rows (create/update/reject) with zero risk of a partial
// write, without a second parsing codepath.
func BulkImportCSV(tenantID string, doctype string, r io.Reader, userID string, dryRun bool) (*ImportResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	reader := csv.NewReader(r)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %w", err)
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("CSV is empty or missing data rows")
	}

	headers := records[0]
	// Clean headers
	for i, h := range headers {
		headers[i] = strings.TrimSpace(strings.ToLower(h))
	}

	result := &ImportResult{
		TotalRows: len(records) - 1,
		DryRun:    dryRun,
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}

	for idx, row := range records[1:] {
		rowNumber := idx + 2
		docData := make(map[string]interface{})

		// Map headers to CSV values
		for colIdx, val := range row {
			if colIdx < len(headers) {
				fieldName := headers[colIdx]
				docData[fieldName] = sanitizeCSVCell(strings.TrimSpace(val))
			}
		}

		// Perform field structure validation
		valErr := ValidateDocument(tenantID, doctype, docData)
		if valErr != nil {
			result.FailedRows++
			result.Errors = append(result.Errors, RowValidationError{
				RowNumber: rowNumber,
				Message:   valErr.Error(),
			})
			continue
		}

		// Enforce unique constraints or generate ID if not supplied
		idVal, exists := docData["id"]
		var id string
		if exists && fmt.Sprintf("%v", idVal) != "" {
			id = fmt.Sprintf("%v", idVal)
		} else {
			// Generate dynamic sequence code or fallback uuid
			seqCode, seqErr := GenerateSequence(tenantID, doctype, "HQ", time.Now().Format("2006"))
			if seqErr != nil {
				// Fallback to random generator if prefix counter doesn't exist
				id = "REC" + fmt.Sprintf("%d", time.Now().UnixNano())
			} else {
				id = seqCode
			}
		}

		// Marshall data column
		marshaled, mErr := json.Marshal(docData)
		if mErr != nil {
			result.FailedRows++
			result.Errors = append(result.Errors, RowValidationError{
				RowNumber: rowNumber,
				Message:   fmt.Sprintf("Failed to marshal JSON payload: %v", mErr),
			})
			continue
		}

		// Create/update classification (Stage 15.2): checked before the
		// upsert below so it reflects pre-write state regardless of dryRun.
		var alreadyExists bool
		_ = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM documents WHERE doctype = $1 AND id = $2)`, doctype, id).Scan(&alreadyExists)

		// Insert document record
		query := fmt.Sprintf(`
			INSERT INTO documents (id, doctype, data, status, created_by)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, updated_at = CURRENT_TIMESTAMP`)
		_, execErr := tx.Exec(query, id, doctype, marshaled, "Active", userID)
		if execErr != nil {
			result.FailedRows++
			result.Errors = append(result.Errors, RowValidationError{
				RowNumber: rowNumber,
				Message:   fmt.Sprintf("Database write error: %v", execErr),
			})
			continue
		}

		result.SuccessRows++
		if alreadyExists {
			result.UpdatedIDs = append(result.UpdatedIDs, id)
		} else {
			result.CreatedIDs = append(result.CreatedIDs, id)
		}
	}

	// Commit transaction if there are any successful rows inserted - unless
	// this is a dry run, in which case the deferred tx.Rollback() above
	// undoes everything and nothing is actually written (Stage 15.2 preview).
	if !dryRun && result.SuccessRows > 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// RecordImportJob (Stage 15.2, V2 §6.2/§16 Phase 3) persists a completed
// (non-dry-run) import as an ImportJob document, including the failed-row
// detail as a downloadable CSV stored directly in the JSONB document -
// V2's job-tracking/audit requirement without a new file-storage mechanism.
func RecordImportJob(tenantID, doctype string, res *ImportResult, createdBy string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	errorCSV := ""
	if len(res.Errors) > 0 {
		var buf bytes.Buffer
		writer := csv.NewWriter(&buf)
		_ = writer.Write([]string{"row_number", "message"})
		for _, e := range res.Errors {
			_ = writer.Write([]string{fmt.Sprintf("%d", e.RowNumber), sanitizeCSVCell(e.Message)})
		}
		writer.Flush()
		errorCSV = buf.String()
	}

	status := "Completed"
	if res.FailedRows > 0 && res.SuccessRows == 0 {
		status = "Failed"
	}

	jobID := fmt.Sprintf("IMPJOB%d", time.Now().UnixNano())
	data := map[string]interface{}{
		"id":           jobID,
		"code":         jobID,
		"doctype_name": doctype,
		"status":       status,
		"total_rows":   res.TotalRows,
		"success_rows": res.SuccessRows,
		"failed_rows":  res.FailedRows,
		"error_csv":    errorCSV,
	}
	marshaled, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'ImportJob', $2, $3, $4)`, schema), jobID, marshaled, status, createdBy)
	if err != nil {
		return "", err
	}
	return jobID, nil
}

// GetImportJobErrorCSV returns the stored error_csv text for a completed
// ImportJob, ready to stream back with a Content-Disposition header.
func GetImportJobErrorCSV(tenantID, jobID string) ([]byte, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'ImportJob' AND id = $1`, schema), jobID).Scan(&dataStr)
	if err != nil {
		return nil, fmt.Errorf("import job not found: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, err
	}
	errorCSV, _ := data["error_csv"].(string)
	if errorCSV == "" {
		errorCSV = "row_number,message\n"
	}
	return []byte(errorCSV), nil
}

// GenerateCSVTemplate returns a dummy CSV buffer containing the headers for a doctype schema
func GenerateCSVTemplate(tenantID string, doctype string) ([]byte, error) {
	fields, err := GetDocTypeMeta(tenantID, doctype)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	var headers []string
	// Append ID always as the first column indicator
	headers = append(headers, "id")
	for _, f := range fields {
		if f.Fieldname == "id" || f.Fieldname == "status" {
			continue
		}
		headers = append(headers, f.Fieldname)
	}
	for i := range headers {
		headers[i] = sanitizeCSVCell(headers[i])
	}

	if err := writer.Write(headers); err != nil {
		return nil, err
	}
	writer.Flush()
	return buf.Bytes(), nil
}
