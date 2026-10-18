package engines

import (
	"bytes"
	"context"
	"custom_erp/db"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Stage 55.11 / 73.4.4 - resume a bulk import from its failed rows.
//
// An ImportJob used to keep only "row N: message", so fixing three bad rows
// out of 5,000 meant re-uploading the whole file and trusting the per-row
// existence check not to duplicate the 4,997 good ones (it cannot, for a
// master numbered on import). Now the job also keeps each failed row's
// original cells. "Failed rows" downloads them with the reason beside each;
// the user fixes them and resumes the SAME job with only those rows. Each
// original row can succeed once: a row already imported is refused if sent
// again, and rows that fail again stay open on the job with the new reason.

// importFailedSourceRow is one failed row as the job keeps it.
type importFailedSourceRow struct {
	Row     int      `json:"row"`
	Cells   []string `json:"cells"`
	Message string   `json:"message"`
}

// attachFailedSourceRows records, on a finished result, the source header and
// the cells of every failed row, keyed by the row number the errors carry
// (header = row 1, so data row i is row i+2). Called by both import
// producers (plain CSV and template) right after the run.
func attachFailedSourceRows(res *ImportResult, header []string, dataRows [][]string, doctype, template string) {
	if res == nil {
		return
	}
	res.sourceHeader = append([]string(nil), header...)
	res.sourceDoctype = doctype
	res.sourceTemplate = template
	res.failedSource = nil
	for _, e := range res.Errors {
		idx := e.RowNumber - 2
		if e.RowNumber < 2 || idx >= len(dataRows) {
			continue
		}
		res.failedSource = append(res.failedSource, importFailedSourceRow{
			Row: e.RowNumber, Cells: append([]string(nil), dataRows[idx]...), Message: e.Message,
		})
	}
}

// importJobSourceFields is what RecordImportJob adds to the job when the
// result carries source rows.
func importJobSourceFields(res *ImportResult) map[string]interface{} {
	if res == nil || res.sourceHeader == nil {
		return nil
	}
	open := res.failedSource
	if open == nil {
		open = []importFailedSourceRow{}
	}
	return map[string]interface{}{
		"source_doctype": res.sourceDoctype, "source_template": res.sourceTemplate,
		"source_header": res.sourceHeader, "open_failed_rows": open,
		"resumable": len(open) > 0,
	}
}

type importJobState struct {
	schema, id, doctype, template, status string
	header                                []string
	open                                  []importFailedSourceRow
	data                                  map[string]interface{}
}

func loadImportJobState(tenantID, jobID string) (*importJobState, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var raw []byte
	st := &importJobState{schema: schema, id: jobID}
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'ImportJob' AND id = $1`, schema), jobID).Scan(&raw, &st.status); err != nil {
		return nil, &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("import job %s not found", jobID)}
	}
	if err := json.Unmarshal(raw, &st.data); err != nil {
		return nil, err
	}
	st.doctype, _ = st.data["source_doctype"].(string)
	if st.doctype == "" {
		st.doctype, _ = st.data["doctype_name"].(string)
	}
	st.template, _ = st.data["source_template"].(string)
	if b, err := json.Marshal(st.data["source_header"]); err == nil {
		_ = json.Unmarshal(b, &st.header)
	}
	if b, err := json.Marshal(st.data["open_failed_rows"]); err == nil {
		_ = json.Unmarshal(b, &st.open)
	}
	return st, nil
}

// ImportJobTargetDoctype names the record type a job wrote, for the
// handler's permission check.
func ImportJobTargetDoctype(tenantID, jobID string) (string, error) {
	st, err := loadImportJobState(tenantID, jobID)
	if err != nil {
		return "", err
	}
	if st.doctype == "" && st.template != "" {
		_, target, _, err := fetchPIMImportTemplate(tenantID, st.template)
		if err != nil {
			return "", err
		}
		return target, nil
	}
	return st.doctype, nil
}

// ImportJobFailedRowsCSV returns the job's still-open failed rows: an
// original_row column, the source columns, and the reason.
func ImportJobFailedRowsCSV(tenantID, jobID string) ([]byte, error) {
	st, err := loadImportJobState(tenantID, jobID)
	if err != nil {
		return nil, err
	}
	if st.header == nil {
		return nil, &ValidationError{Code: "DATAIM-0470", Message: "this import job was recorded before failed rows were kept - re-upload its file instead"}
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(append(append([]string{"original_row"}, st.header...), "error"))
	for _, row := range st.open {
		cells := make([]string, len(st.header))
		for i := range cells {
			if i < len(row.Cells) {
				cells[i] = sanitizeCSVCell(row.Cells[i])
			}
		}
		_ = w.Write(append(append([]string{strconv.Itoa(row.Row)}, cells...), sanitizeCSVCell(row.Message)))
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// ResumeImportJob re-runs only the corrected failed rows of jobID. The upload
// is the failed-rows file (original_row + source columns; an error column is
// ignored). dryRun validates without writing and changes nothing on the job.
// Returns the run's result and, for a real run, the new ImportJob's id.
func ResumeImportJob(ctx context.Context, tenantID, jobID string, r io.Reader, userID, role string, dryRun bool) (*ImportResult, string, error) {
	bad := func(msg string) error { return &ValidationError{Code: "DATAIM-0471", Message: msg} }
	st, err := loadImportJobState(tenantID, jobID)
	if err != nil {
		return nil, "", err
	}
	if st.header == nil {
		return nil, "", &ValidationError{Code: "DATAIM-0470", Message: "this import job was recorded before failed rows were kept - re-upload its file instead"}
	}
	if len(st.open) == 0 {
		return nil, "", bad(fmt.Sprintf("import job %s has no failed rows left to resume", jobID))
	}

	// One resume at a time per job, so two uploads cannot both import row N.
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		return nil, "", err
	}
	defer conn.Close()
	lockKey := st.schema + "|import-resume|" + jobID
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext($1))`, lockKey); err != nil {
		return nil, "", err
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtext($1))`, lockKey)
	if st, err = loadImportJobState(tenantID, jobID); err != nil { // re-read under the lock
		return nil, "", err
	}

	records, err := readCSVRecordsContext(ctx, tenantID, r)
	if err != nil {
		return nil, "", err
	}
	header := records[0]
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	rowCol, ok := col["original_row"]
	if !ok {
		return nil, "", bad("the file needs the original_row column from the job's failed-rows download")
	}
	open := map[int]importFailedSourceRow{}
	for _, row := range st.open {
		open[row.Row] = row
	}
	sourceRecords := [][]string{st.header}
	var originals []int
	seen := map[int]bool{}
	for i, rec := range records[1:] {
		if rowCol >= len(rec) {
			return nil, "", bad(fmt.Sprintf("line %d has no original_row", i+2))
		}
		n, convErr := strconv.Atoi(strings.TrimSpace(rec[rowCol]))
		if convErr != nil {
			return nil, "", bad(fmt.Sprintf("line %d: original_row %q is not a row number", i+2, rec[rowCol]))
		}
		if seen[n] {
			return nil, "", bad(fmt.Sprintf("original row %d appears twice in this file", n))
		}
		if _, isOpen := open[n]; !isOpen {
			return nil, "", bad(fmt.Sprintf("original row %d is not an open failed row of job %s - it was imported already or never failed", n, jobID))
		}
		seen[n] = true
		cells := make([]string, len(st.header))
		for j, h := range st.header {
			if idx, has := col[strings.ToLower(strings.TrimSpace(h))]; has && idx < len(rec) {
				cells[j] = rec[idx]
			}
		}
		sourceRecords = append(sourceRecords, cells)
		originals = append(originals, n)
	}
	if len(originals) == 0 {
		return nil, "", bad("the file has no rows to resume")
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.WriteAll(sourceRecords)
	var res *ImportResult
	if st.template != "" {
		res, err = RunPIMImportTemplateContext(ctx, tenantID, st.template, &buf, userID, role, dryRun)
	} else {
		res, err = BulkImportCSVContext(ctx, tenantID, st.doctype, &buf, userID, role, dryRun)
	}
	if err != nil || dryRun {
		return res, "", err
	}

	// Map the run's row numbers back to the job's original rows. A row the
	// run did not report failed has been imported and leaves the open set.
	stillFailing := map[int]importFailedSourceRow{}
	for _, f := range res.failedSource {
		k := f.Row - 2
		if k >= 0 && k < len(originals) {
			stillFailing[originals[k]] = importFailedSourceRow{Row: originals[k], Cells: f.Cells, Message: f.Message}
		}
	}
	resolved := 0
	var newOpen []importFailedSourceRow
	for _, row := range st.open {
		if !seen[row.Row] {
			newOpen = append(newOpen, row)
			continue
		}
		if again, failed := stillFailing[row.Row]; failed {
			newOpen = append(newOpen, again)
			continue
		}
		resolved++
	}
	if newOpen == nil {
		newOpen = []importFailedSourceRow{}
	}

	childID, err := RecordImportJob(tenantID, st.doctype, res, userID)
	if err != nil {
		return res, "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('resumed_from', $2::text) WHERE id = $1 AND doctype = 'ImportJob'`, st.schema), childID, jobID); err != nil {
		return res, childID, err
	}
	history, _ := st.data["resumes"].([]interface{})
	patch, _ := json.Marshal(map[string]interface{}{
		"open_failed_rows": newOpen, "resumable": len(newOpen) > 0,
		"resolved_rows": int(numFromInterface(st.data["resolved_rows"])) + resolved,
		"resumes": append(history, map[string]interface{}{
			"job": childID, "at": time.Now().UTC().Format(time.RFC3339), "by": userID,
			"attempted": len(originals), "imported": resolved,
		}),
	})
	status := st.status
	if len(newOpen) == 0 {
		status = "Completed"
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = (data || $2::jsonb) || jsonb_build_object('status', $3::text), status = $3, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = 'ImportJob'`, st.schema),
		jobID, string(patch), status); err != nil {
		return res, childID, err
	}
	return res, childID, nil
}

// ImportJobSummary is the job as the resume screen shows it.
func ImportJobSummary(tenantID, jobID string) (map[string]interface{}, error) {
	st, err := loadImportJobState(tenantID, jobID)
	if err != nil {
		return nil, err
	}
	var createdBy string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(created_by, '') FROM %s.documents WHERE id = $1 AND doctype = 'ImportJob'`, st.schema), jobID).Scan(&createdBy)
	resumes, _ := st.data["resumes"].([]interface{})
	if resumes == nil {
		resumes = []interface{}{}
	}
	return map[string]interface{}{
		"id": jobID, "doctype": st.doctype, "template": st.template, "status": st.status,
		"total_rows": numFromInterface(st.data["total_rows"]), "success_rows": numFromInterface(st.data["success_rows"]),
		"failed_rows": numFromInterface(st.data["failed_rows"]), "open_failed_rows": len(st.open),
		"resolved_rows": numFromInterface(st.data["resolved_rows"]), "resumable": st.header != nil && len(st.open) > 0,
		"keeps_failed_rows": st.header != nil, "resumed_from": st.data["resumed_from"], "resumes": resumes,
		"created_by": createdBy,
	}, nil
}
