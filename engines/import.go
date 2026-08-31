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

// importBatchRows (24.32, loophole #9) bounds how many CSV rows share one
// transaction. Previously the whole file - potentially thousands of rows -
// ran inside a single transaction (held open for the entire parse/validate/
// write loop), which for a large import holds row locks on every touched
// document far longer than necessary and can contend with concurrent
// writers. Batching bounds worst-case lock hold time to one batch instead
// of the whole file; each batch's create/update existence check still only
// ever sees already-committed (non-dry-run) or already-rolled-back (dry
// run) state from prior batches, so the per-row "reflects pre-write state"
// guarantee Stage 15.2 established holds unchanged across batch boundaries.
const importBatchRows = 500

// BulkImportCSV parses a CSV body, validates constraints, and inserts valid
// records in batched transactions (24.32). dryRun=true (Stage 15.2) runs the
// exact same validation/existence-check logic per row but every batch is
// rolled back instead of committed, so a preview can classify rows
// (create/update/reject) with zero risk of a partial write, without a
// second parsing codepath.
func BulkImportCSV(tenantID string, doctype string, r io.Reader, userID, role string, dryRun bool) (*ImportResult, error) {
	records, err := readCSVRecords(r)
	if err != nil {
		return nil, err
	}

	headers := records[0]
	// Clean headers
	for i, h := range headers {
		headers[i] = strings.TrimSpace(strings.ToLower(h))
	}

	// DATAIM-0164 (Stage 25.5): "Excel mandatory column missing" - checked
	// once against the header row, before processing any data rows, rather
	// than letting every single row fail individually with GLOBAL-0001
	// once ValidateDocument notices the same missing column per-row.
	if missing := missingMandatoryColumns(tenantID, doctype, headers); len(missing) > 0 {
		return nil, &ValidationError{Code: "DATAIM-0164", Message: fmt.Sprintf("uploaded file is missing mandatory column(s): %s", strings.Join(missing, ", "))}
	}

	docRows := csvRowsToDocData(headers, records[1:])
	preErrors := make([]string, len(docRows))
	if strings.EqualFold(doctype, "Item") {
		// Stage 36.3.5: applies to every Item import, not only a templated
		// one - variant-parent-ordering is a property of the doctype, not of
		// how the file's columns happened to be named.
		preErrors = pimVariantParentPreflight(tenantID, docRows)
	}
	return runDocDataImport(tenantID, doctype, userID, role, dryRun, docRows, preErrors)
}

// readCSVRecords is the one CSV-parsing entry point BulkImportCSV and Stage
// 36.3's RunPIMImportTemplate both go through, so the empty-file check
// (DATAIM-0163) is enforced identically for a plain upload and a templated
// one instead of drifting into two slightly different messages.
func readCSVRecords(r io.Reader) ([][]string, error) {
	reader := csv.NewReader(r)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %w", err)
	}
	if len(records) < 2 {
		// DATAIM-0163 (Stage 25.5): "Excel template invalid" - a file with
		// no header row or no data rows can't be the doctype's own
		// generated template (GenerateCSVTemplate always emits at least a
		// header row), so this is exactly that scenario.
		return nil, &ValidationError{Code: "DATAIM-0163", Message: "CSV is empty or missing data rows"}
	}
	return records, nil
}

// csvRowsToDocData zips a header row against every data row to build the
// same fieldname->value map shape importBatch (now runDocDataImport) has
// always validated and inserted - split out so Stage 36.3's template path
// can build the identical shape from a remapped/transformed source instead
// of straight from CSV columns, and both feed the one shared batching core.
func csvRowsToDocData(headers []string, rows [][]string) []map[string]interface{} {
	out := make([]map[string]interface{}, len(rows))
	for i, row := range rows {
		docData := make(map[string]interface{})
		for colIdx, val := range row {
			if colIdx < len(headers) {
				docData[headers[colIdx]] = sanitizeCSVCell(strings.TrimSpace(val))
			}
		}
		out[i] = docData
	}
	return out
}

// runDocDataImport is the shared batching core every import path now feeds
// through: BulkImportCSV's plain-CSV rows, and Stage 36.3's
// RunPIMImportTemplate rows (remapped/transformed, plus a variant-parent
// preflight check for Item). preErrors is parallel to docRows - a non-empty
// entry means the row already failed before reaching this function (a
// transform error, a missing variant parent) and is recorded as a failure
// without ever being passed to ValidateDocument, while every row after it
// keeps the same row number it would have had either way.
func runDocDataImport(tenantID, doctype, userID, role string, dryRun bool, docRows []map[string]interface{}, preErrors []string) (*ImportResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	result := &ImportResult{
		TotalRows: len(docRows),
		DryRun:    dryRun,
	}

	// DATAIM-0166 (Stage 25.5): "Duplicate rows in upload" - tracked across
	// the whole file (not reset per batch) so a duplicate straddling a
	// batch boundary is still caught. Keyed on the row's own "id" column
	// when supplied; rows with no id are never flagged (they'd each get a
	// fresh generated code, so they can't collide with each other here).
	seenIDs := map[string]bool{}

	// Resolved once per import (not per batch): the batch size must stay
	// constant for the whole file, or a mid-import config edit would leave
	// the row-number arithmetic below inconsistent.
	batchRows := GetSettingInt(tenantID, "platform.import_batch_rows")
	if batchRows < 1 {
		batchRows = 1
	}
	for batchStart := 0; batchStart < len(docRows); batchStart += batchRows {
		batchEnd := batchStart + batchRows
		if batchEnd > len(docRows) {
			batchEnd = len(docRows)
		}
		if err := importBatch(tenantID, schema, doctype, userID, role, dryRun,
			docRows[batchStart:batchEnd], preErrors[batchStart:batchEnd], batchStart+2, result, seenIDs); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// missingMandatoryColumns compares a doctype's mandatory fields against the
// CSV's own header row.
func missingMandatoryColumns(tenantID, doctype string, headers []string) []string {
	fields, err := GetDocTypeMeta(tenantID, doctype)
	if err != nil {
		return nil
	}
	present := map[string]bool{}
	for _, h := range headers {
		present[h] = true
	}
	var missing []string
	for _, f := range fields {
		// "id" and "status" are system-managed: GenerateCSVTemplate above
		// deliberately omits both from the header row it emits, and
		// ValidateDocument's own mandatory check skips them for the same
		// reason (the backend generates an id and defaults status to
		// 'Active'). This check was the odd one out and demanded them
		// anyway, so **any** doctype declaring a mandatory `status` field
		// rejected its own generated template with DATAIM-0164 - 89 of them
		// as of Stage 34.1, including Brand, Color, Customer, Vendor,
		// Location and CycleCountLine, whose bulk import is the only way it
		// is meant to be filled at all (app.js:6505). Found while verifying
		// 34.1's CompetitorPrice import by literally following the
		// documented download-template-then-upload path.
		if f.Fieldname == "id" || f.Fieldname == "status" {
			continue
		}
		if f.Mandatory && !present[strings.ToLower(f.Fieldname)] {
			missing = append(missing, f.Fieldname)
		}
	}
	return missing
}

// importBatch runs one batch of rows inside its own transaction, committing
// (non-dry-run, at least one success in the batch) or rolling back (dry
// run, or a batch with zero successes) - the same per-transaction rule
// BulkImportCSV used to apply once for the whole file, now applied once per
// batch instead. docRows are already in fieldname->value shape (Stage 36.3:
// either a plain CSV row or a template-remapped/transformed one); preErrors
// is parallel to docRows and, when non-empty for a row, records that row as
// failed for the given reason without calling ValidateDocument at all.
func importBatch(tenantID, schema, doctype, userID, role string, dryRun bool, docRows []map[string]interface{}, preErrors []string, firstRowNumber int, result *ImportResult, seenIDs map[string]bool) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	batchSuccessCount := 0
	for i, docData := range docRows {
		rowNumber := firstRowNumber + i

		if preErr := preErrors[i]; preErr != "" {
			result.FailedRows++
			result.Errors = append(result.Errors, RowValidationError{
				RowNumber: rowNumber,
				Message:   preErr,
			})
			continue
		}

		// Stage 36.7.6: a CSV import writes fields exactly like the generic
		// single-document update path does, but had never been taught that
		// path's RejectRestrictedFieldWrites check - a role blocked from
		// editing a field on-screen could still push it through in bulk via
		// import. role is "" for the two system-initiated paths (the
		// unauthenticated import hook, the scheduled drop-directory worker),
		// which have no operator role to restrict against, so the check is
		// skipped there rather than issuing a query that would only ever
		// find zero rows for a role named "".
		if role != "" {
			if permErr := RejectRestrictedFieldWrites(tenantID, role, doctype, docData); permErr != nil {
				result.FailedRows++
				result.Errors = append(result.Errors, RowValidationError{
					RowNumber: rowNumber,
					Message:   permErr.Error(),
				})
				continue
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
			// DATAIM-0166 (Stage 25.5): the same id appearing twice in this
			// same uploaded file - distinct from an id that already exists
			// in the database (that's a legitimate update, tracked via
			// UpdatedIDs below), this is the file contradicting itself.
			if seenIDs[id] {
				result.FailedRows++
				result.Errors = append(result.Errors, RowValidationError{
					RowNumber: rowNumber,
					Message:   fmt.Sprintf("duplicate id %q also appears earlier in this file", id),
				})
				continue
			}
			seenIDs[id] = true
		} else {
			// Generate dynamic sequence code or fallback uuid.
			//
			// Stage 30.6: prefer the doctype's registered series so an
			// imported row lands in the same series as one created on screen.
			// Without this an imported PO was numbered from a "PurchaseOrder"
			// series that has no prefix_configs row at all, so it fell through
			// to GenerateSequence's defaults and produced
			// "PurchaseOrder/HQ/2026/000001" alongside the UI's "PO/HQ/26-27/000001".
			// Doctypes with no registered series keep the old behavior exactly.
			seriesKey := doctype
			seriesYear := time.Now().Format("2006")
			if key, ok := DocumentNumberSeriesKey(doctype); ok {
				seriesKey = key
				seriesYear = documentFinancialYear(time.Now())
			}
			seqCode, seqErr := GenerateSequence(tenantID, seriesKey, "HQ", seriesYear)
			if seqErr != nil {
				// Fallback to random generator if prefix counter doesn't exist
				id = NewDocIDCompact("REC")
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
		query := `
			INSERT INTO documents (id, doctype, data, status, created_by)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, updated_at = CURRENT_TIMESTAMP`
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
		batchSuccessCount++
		if alreadyExists {
			result.UpdatedIDs = append(result.UpdatedIDs, id)
		} else {
			result.CreatedIDs = append(result.CreatedIDs, id)
		}
	}

	// Commit this batch if it has any successful rows - unless this is a
	// dry run, in which case the deferred tx.Rollback() above undoes this
	// batch's writes and nothing is actually persisted (Stage 15.2 preview).
	if !dryRun && batchSuccessCount > 0 {
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
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

	jobID := NewDocIDCompact("IMPJOB")
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
