package engines

import (
	"bufio"
	"bytes"
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
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
	// Stage 47.13.3: what the file held against what was posted.
	Controls *ImportControlTotals `json:"control_totals,omitempty"`
	controls *importControls
	// Stage 47.13.3: the rules the run used and the file it came from.
	RulesVersion string        `json:"rules_version,omitempty"`
	Source       *ImportSource `json:"source,omitempty"`

	// Stage 55.11: the source header and failed rows' original cells, kept
	// for the ImportJob so its failed rows can be resumed (import_resume.go).
	sourceHeader   []string
	sourceDoctype  string
	sourceTemplate string
	failedSource   []importFailedSourceRow
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
	// Stage 49.8.2: a plain number (a negative quantity or amount) is never a
	// formula, and an apostrophe would turn the column into text.
	if _, err := strconv.ParseFloat(strings.TrimSpace(trimmed), 64); err == nil {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

// safeCSVWriter is a csv.Writer for files people open in a spreadsheet:
// every cell goes through sanitizeCSVCell (Stage 49.8.2). Machine formats a
// bank or a channel reads (payment files) keep the plain csv.Writer.
type safeCSVWriter struct{ *csv.Writer }

func newSafeCSVWriter(w io.Writer) safeCSVWriter { return safeCSVWriter{csv.NewWriter(w)} }

func (s safeCSVWriter) Write(record []string) error {
	out := make([]string, len(record))
	for i, cell := range record {
		out[i] = sanitizeCSVCell(cell)
	}
	return s.Writer.Write(out)
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

// csvCancelCheckRows is how often readCSVRecordsContext consults ctx while
// parsing an upload (BLD-046).
const csvCancelCheckRows = 256

// maxImportErrorsRecorded (BLD-046) bounds how many individual row errors
// accumulate in one ImportResult/ImportJob. A large, mostly-invalid file
// would otherwise add one error entry per row with no cap, inflating both
// the JSON response and the error_csv blob RecordImportJob stores in the
// ImportJob document without bound. Rows past this count still fail and
// count toward FailedRows; only the itemized detail is capped, with one
// summary entry appended once (see runDocDataImport).
const maxImportErrorsRecorded = 1000

// recordImportError is the one place importBatch appends a row failure -
// the shared choke point that applies maxImportErrorsRecorded to every
// failure reason (pre-check, permission, validation, marshal, DB write)
// instead of needing the cap checked at each call site individually.
func recordImportError(result *ImportResult, rowNumber int, message string) {
	result.FailedRows++
	if len(result.Errors) >= maxImportErrorsRecorded {
		return
	}
	result.Errors = append(result.Errors, RowValidationError{RowNumber: rowNumber, Message: message})
}

// BulkImportCSV parses a CSV body, validates constraints, and inserts valid
// records in batched transactions (24.32). dryRun=true (Stage 15.2) runs the
// exact same validation/existence-check logic per row but every batch is
// rolled back instead of committed, so a preview can classify rows
// (create/update/reject) with zero risk of a partial write, without a
// second parsing codepath.
func BulkImportCSV(tenantID string, doctype string, r io.Reader, userID, role string, dryRun bool) (*ImportResult, error) {
	return BulkImportCSVContext(context.Background(), tenantID, doctype, r, userID, role, dryRun)
}

// BulkImportCSVContext is the BLD-046 cancellation-aware form. Cancellation
// is honoured at two points: while the upload is still being read (so an
// abandoned oversized file stops being buffered) and between committed
// batches (so an abandoned import stops doing work without ever tearing up
// a batch that already committed).
func BulkImportCSVContext(ctx context.Context, tenantID string, doctype string, r io.Reader, userID, role string, dryRun bool) (*ImportResult, error) {
	src := newImportSourceReader(r) // 47.13.3: lineage of the bytes as parsed
	records, err := readCSVRecordsContext(ctx, tenantID, src)
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
	res, err := runDocDataImportContext(ctx, tenantID, doctype, userID, role, dryRun, docRows, preErrors)
	if err == nil {
		attachFailedSourceRows(res, headers, records[1:], doctype, "")
		stampImportSource(res, src, userID)
	}
	return res, err
}

// readCSVRecords is the one CSV-parsing entry point BulkImportCSV and Stage
// 36.3's RunPIMImportTemplate both go through, so the empty-file check
// (DATAIM-0163) and the row cap below are enforced identically for a plain
// upload and a templated one instead of drifting into two slightly
// different behaviors.
//
// BLD-046: reads row-by-row (reader.Read() in a loop) instead of
// reader.ReadAll(), and rejects the file as soon as its data-row count
// crosses platform.max_import_rows rather than after first parsing the
// whole thing into memory. Before this, the only thing bounding a bulk
// import's memory use was the incidental global 2MB request-body cap
// (middleware.go) - not a deliberate row bound, so raising that byte cap (a
// plausible ask on its own, since 2MB is a small CSV) would have had no
// mitigation left underneath it.
func readCSVRecords(tenantID string, r io.Reader) ([][]string, error) {
	return readCSVRecordsContext(context.Background(), tenantID, r)
}

// readCSVRecordsContext adds the BLD-046 cancellation check to the parse
// loop. Checked every csvCancelCheckRows rows rather than every row: the
// check is cheap but not free, and a batch of a few hundred rows is already
// a fine granularity for abandoning a parse.
func readCSVRecordsContext(ctx context.Context, tenantID string, r io.Reader) ([][]string, error) {
	maxRows := GetSettingInt(tenantID, "platform.max_import_rows")
	// 134.4: the upload rule on what every CSV import reads - plain text,
	// judged from its first 64 KB (an empty file keeps DATAIM-0163 below).
	br := bufio.NewReaderSize(r, 64<<10)
	if head, _ := br.Peek(64 << 10); len(head) > 0 {
		if err := CheckUpload(UploadCSV, head); err != nil {
			return nil, err
		}
	}
	reader := csv.NewReader(br)
	var records [][]string
	for {
		if len(records)%csvCancelCheckRows == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV: %w", err)
		}
		records = append(records, record)
		if maxRows > 0 && len(records)-1 > maxRows {
			// -1 excludes the header row from the data-row count. Bails
			// immediately rather than continuing to read/buffer the rest of
			// an oversized file that is going to be rejected regardless.
			return nil, &ValidationError{Code: "DATAIM-0189", Message: fmt.Sprintf("uploaded file has more than %d data rows - split it into smaller files and import them separately", maxRows)}
		}
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
	return runDocDataImportContext(context.Background(), tenantID, doctype, userID, role, dryRun, docRows, preErrors)
}

// runDocDataImportContext is the one batching core both the plain-CSV and
// the templated import go through, so the cancellation point below covers
// both without either path checking for itself.
func runDocDataImportContext(ctx context.Context, tenantID, doctype, userID, role string, dryRun bool, docRows []map[string]interface{}, preErrors []string) (*ImportResult, error) {
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
	lk, err := newImportLookups(tenantID, schema, doctype, role)
	if err != nil {
		return nil, err
	}
	// Stage 47.13.3: the file's Number columns, read before validation
	// touches a row.
	result.controls = newImportControls(lk.validation.fields, docRows)
	for batchStart := 0; batchStart < len(docRows); batchStart += batchRows {
		// Checked between batches, never inside one. importBatch owns its own
		// transaction, so stopping here leaves every already-committed batch
		// committed and the current one untouched - the retry contract is
		// unchanged, because re-importing the same file re-runs the same
		// per-row existence check and updates instead of duplicating.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batchEnd := batchStart + batchRows
		if batchEnd > len(docRows) {
			batchEnd = len(docRows)
		}
		if err := importBatch(tenantID, schema, doctype, userID, role, dryRun,
			docRows[batchStart:batchEnd], preErrors[batchStart:batchEnd], batchStart+2, result, seenIDs, lk); err != nil {
			return nil, err
		}
	}

	// BLD-046: recordImportError stopped itemizing individual failures once
	// maxImportErrorsRecorded was reached, but kept counting FailedRows - one
	// honest summary entry replaces the silently-dropped detail so the
	// response accounts for every failed row instead of undercounting what
	// Errors shows.
	if result.FailedRows > len(result.Errors) {
		result.Errors = append(result.Errors, RowValidationError{
			RowNumber: 0,
			Message: fmt.Sprintf("%d additional row(s) also failed and are not itemized here (capped at %d) - fix the errors shown, re-run, and repeat for any rows still failing",
				result.FailedRows-len(result.Errors), maxImportErrorsRecorded),
		})
	}

	// Stage 93.1: an imported location gets its cost center the same way one
	// saved on screen does. Its row is committed already, so a failure here
	// is reported against the import, not the location.
	if !dryRun && doctype == "Location" {
		for _, id := range append(append([]string{}, result.CreatedIDs...), result.UpdatedIDs...) {
			if _, err := EnsureLocationCostCenter(tenantID, id, userID); err != nil {
				result.Errors = append(result.Errors, RowValidationError{Message: fmt.Sprintf("location %s was imported, but its cost center could not be made: %v", id, err)})
			}
		}
	}

	finishImportControls(result)
	return result, nil
}

// importLookups is everything importBatch used to re-read from the database
// for every row (49.4.4a): the doctype's fields, platform.field_max_length,
// the role's field permissions and whether the doctype is a Master - about
// 20 round trips per Item row, which put a 20,000-row file far past the
// request deadline. Resolved once per file instead, the same way batchRows
// is, so a mid-import config edit applies to the next file.
type importLookups struct {
	validation     *docValidationLookups
	permissions    map[string]FieldPermission
	permissionsErr error
	isMaster       bool
	isMasterErr    error
	itemDefaults   *ItemDefaultsMemo // Stage 89: one import's item-name / category lookups
	rules          importWriteRules
}

// importWriteRules (Stage 49.3.4 / 49.3.5) are what the file's rows may do to
// the records they name: add or change records of this type (asked only for an
// operator's role, as the field restrictions are), and whether the type is
// approval-gated. Applied by screenImportWrites.
type importWriteRules struct {
	checkRole            bool
	mayCreate, mayUpdate bool
	gated                bool
	err                  error
}

func newImportLookups(tenantID, schema, doctype, role string) (*importLookups, error) {
	fields, err := GetDocTypeMeta(tenantID, doctype)
	if err != nil {
		return nil, err
	}
	lk := &importLookups{validation: &docValidationLookups{
		fields:         fields,
		fieldMaxLength: GetSettingIntForSchema(schema, "platform.field_max_length"),
	}}
	if role != "" {
		lk.permissions, lk.permissionsErr = fieldPermissions(tenantID, role, doctype)
	}
	lk.isMaster, lk.isMasterErr = IsMasterDoctype(tenantID, doctype)
	lk.rules.gated, lk.rules.err = IsApprovalGated(tenantID, doctype)
	if role != "" && lk.rules.err == nil {
		lk.rules.checkRole = true
		if lk.rules.mayCreate, lk.rules.err = RoleMayOnDoctype(tenantID, role, doctype, "create"); lk.rules.err == nil {
			lk.rules.mayUpdate, lk.rules.err = RoleMayOnDoctype(tenantID, role, doctype, "update")
		}
	}
	return lk, nil
}

func (lk *importLookups) masterDoctype() (bool, error) { return lk.isMaster, lk.isMasterErr }

// ImportClearMarker is the cell that clears a field on an update import
// (Stage 47.13.2a / 49.3.4b). A blank cell keeps the stored value, so clearing
// has to be said: it starts with no formula character, so the formula-safe
// writer and reader (sanitizeCSVCell) pass it through unchanged, and no real
// value is spelt like it. Read case-insensitively; on a new record it is blank.
const ImportClearMarker = "<clear>"

func isImportClearMarker(s string) bool {
	return strings.EqualFold(strings.TrimSpace(s), ImportClearMarker)
}

// importUpdateTarget is the existing record a row names: its id, or - for a
// master, whose id is its code (applyMasterIDCodeInvariant) - its code.
func importUpdateTarget(docData map[string]interface{}, isMaster bool) string {
	if id := importCellText(docData, "id"); id != "" {
		return id
	}
	if isMaster {
		return importCellText(docData, "code")
	}
	return ""
}

// importCellText is a row's cell as trimmed text, "" when absent or null.
func importCellText(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	return ""
}

// storedImportTargets reads, and locks for the batch's transaction, the
// stored data of every existing record a batch's rows could name - one query
// per batch (49.4.4a), soft-deleted records left out as on the record form.
func storedImportTargets(tx *sql.Tx, doctype string, docRows []map[string]interface{}) (map[string]map[string]interface{}, error) {
	seen := map[string]bool{}
	var ids []string
	for _, row := range docRows {
		for _, key := range []string{"id", "code"} {
			if v := importCellText(row, key); v != "" && !seen[v] {
				seen[v] = true
				ids = append(ids, v)
			}
		}
	}
	out := map[string]map[string]interface{}{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(`SELECT id, data FROM documents WHERE doctype = $1 AND id = ANY($2::text[]) AND deleted_at IS NULL FOR UPDATE`, doctype, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		data := map[string]interface{}{}
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, fmt.Errorf("record %s could not be read: %v", id, err)
		}
		out[id] = data
	}
	return out, rows.Err()
}

// mergeImportUpdate (Stage 47.13.2a / 49.3.4b) is an update row laid over the
// record it names, the import's form of the record-form save's merge (73.D.8):
// a key the file does not carry - a column left out, or one the server keeps
// (bom_snapshot, gst_breakdown, payment history) - keeps its stored value; a
// blank cell keeps it too; ImportClearMarker clears it; any other cell wins.
func mergeImportUpdate(stored, row map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{}, len(stored)+len(row))
	for k, v := range stored {
		merged[k] = v
	}
	for k, v := range row {
		if s, isText := v.(string); isText {
			if strings.TrimSpace(s) == "" {
				if _, had := stored[k]; had {
					continue
				}
			} else if isImportClearMarker(s) {
				v = ""
			}
		}
		merged[k] = v
	}
	return merged
}

// blankImportClearMarkers turns the marker into a blank cell on a row that
// makes a new record - there is nothing to clear, and it is never stored.
func blankImportClearMarkers(row map[string]interface{}) {
	for k, v := range row {
		if s, isText := v.(string); isText && isImportClearMarker(s) {
			row[k] = ""
		}
	}
}

// importWrite is one validated row waiting for its batch's write.
type importWrite struct {
	rowNumber int
	id, data  string
	status    string // the row's own status value, if it carries one (49.3.5)
}

// screenImportWrites (Stage 49.3.4 / 49.3.5) refuses the rows of a batch the
// importer may not write, with one query for the records they name: adding a
// record needs create permission and changing one needs update (an operator's
// role only), a row never claims a decided status (Approved / Rejected) of an
// approval-gated type, and an approved record is changed on screen - where it
// goes back for approval - not rewritten by a file.
func screenImportWrites(tx *sql.Tx, doctype string, rules importWriteRules, writes []importWrite, result *ImportResult) []importWrite {
	if len(writes) == 0 {
		return writes
	}
	if rules.err != nil {
		for _, w := range writes {
			recordImportError(result, w.rowNumber, fmt.Sprintf("could not check what this import may write: %v", rules.err))
		}
		return nil
	}
	ids := make([]string, len(writes))
	for i, w := range writes {
		ids[i] = w.id
	}
	existing := map[string]string{} // id -> status, this doctype only
	rows, err := tx.Query(`SELECT id, COALESCE(status, '') FROM documents WHERE doctype = $1 AND id = ANY($2::text[])`, doctype, pq.Array(ids))
	if err == nil {
		for rows.Next() {
			var id, status string
			if err = rows.Scan(&id, &status); err != nil {
				break
			}
			existing[id] = status
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
	}
	if err != nil {
		for _, w := range writes {
			recordImportError(result, w.rowNumber, fmt.Sprintf("could not check the records this import names: %v", err))
		}
		return nil
	}
	kept := writes[:0]
	for _, w := range writes {
		prior, isUpdate := existing[w.id]
		switch {
		case rules.checkRole && !isUpdate && !rules.mayCreate:
			recordImportError(result, w.rowNumber, fmt.Sprintf("your role may not add %s records", doctype))
		case rules.checkRole && isUpdate && !rules.mayUpdate:
			recordImportError(result, w.rowNumber, fmt.Sprintf("%q already exists and your role may not change %s records", w.id, doctype))
		case rules.gated && isUpdate && prior == "Approved":
			recordImportError(result, w.rowNumber, fmt.Sprintf("%q is Approved - change it on screen, where it goes back for approval", w.id))
		default:
			if w.status != "" {
				if verr := decidedStatusWriteError(rules.gated, prior, w.status); verr != nil {
					recordImportError(result, w.rowNumber, verr.Error())
					continue
				}
			}
			kept = append(kept, w)
		}
	}
	return kept
}

// prefetchLinks answers every Link value in one batch with one query per
// Link field, replacing a lookup per row per Link field. The query is
// verifyDocumentExists's own condition. A Link back to the doctype being
// imported is left to the per-row check, because an earlier row of the same
// file can create its target. A failed prefetch only costs speed: those ids
// stay unanswered and the per-row check runs for them.
func (lk *importLookups) prefetchLinks(schema, doctype string, docRows []map[string]interface{}) {
	known := map[string]map[string]bool{}
	for _, f := range lk.validation.fields {
		if f.Fieldtype != "Link" || f.Options == "" || f.Options == doctype {
			continue
		}
		pending := map[string]bool{}
		var ids []string
		for _, row := range docRows {
			val, ok := row[f.Fieldname]
			if !ok || val == nil {
				continue
			}
			id := strings.TrimSpace(fmt.Sprintf("%v", val))
			_, done := known[f.Options][id]
			_, queued := pending[id]
			if id == "" || done || queued {
				continue
			}
			pending[id] = false
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			continue
		}
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents
			WHERE doctype = $1 AND id = ANY($2::text[]) AND status != 'Cancelled'`, schema), f.Options, pq.Array(ids))
		if err != nil {
			continue
		}
		ok := true
		for rows.Next() {
			var id string
			if rows.Scan(&id) != nil {
				ok = false
				break
			}
			pending[id] = true
		}
		if rows.Err() != nil {
			ok = false
		}
		rows.Close()
		if !ok {
			continue
		}
		if known[f.Options] == nil {
			known[f.Options] = map[string]bool{}
		}
		for id, exists := range pending {
			known[f.Options][id] = exists
		}
	}
	lk.validation.linkExists = known
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
		// Stage 51.8: `code` is server-generated for an Item import that
		// carries a `family` column, exactly as it is on the single-record
		// form (view-documents.js treats code as server-numbered the moment
		// a family is set), so demanding the column here contradicts the one
		// flow whose entire purpose is to have the SKU generated. Narrow on
		// purpose: for every other doctype, and for an Item import with no
		// family column, `code` stays mandatory - a plain Master row with no
		// code has nothing for ValidateDocument or the id = code invariant to
		// work from, and rejecting the file once up front is far better than
		// failing every row individually.
		if f.Fieldname == "code" && doctype == "Item" && present["family"] {
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
// importEdit is one existing record a batch changed, for its EDIT_ audit row.
type importEdit struct {
	id            string
	before, after map[string]interface{}
}

// importPriorData reads the stored data of the ids a batch is about to
// write, so an update can be audited field by field (49.5.4 b).
func importPriorData(tx *sql.Tx, doctype string, ids []string) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	rows, err := tx.Query(`SELECT id, data FROM documents WHERE doctype = $1 AND id = ANY($2)`, doctype, pq.Array(ids))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw []byte
		var data map[string]interface{}
		if rows.Scan(&id, &raw) == nil && json.Unmarshal(raw, &data) == nil {
			out[id] = data
		}
	}
	return out
}

func importBatch(tenantID, schema, doctype, userID, role string, dryRun bool, docRows []map[string]interface{}, preErrors []string, firstRowNumber int, result *ImportResult, seenIDs map[string]bool, lk *importLookups) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	// Stage 47.13.2a / 49.3.4b: a row that names an existing record updates
	// it - merged into the stored record (mergeImportUpdate), so a blank cell
	// keeps the stored value, a key the file does not carry survives, and only
	// ImportClearMarker clears. The records are read once per batch and locked
	// until it commits; the merged rows are what every check below reads.
	storedTargets, err := storedImportTargets(tx, doctype, docRows)
	if err != nil {
		return err
	}
	isMaster, _ := lk.masterDoctype()
	mergedRows := make([]map[string]interface{}, len(docRows))
	priorRows := make([]map[string]interface{}, len(docRows)) // the stored record an update row names
	for i, row := range docRows {
		if prior, ok := storedTargets[importUpdateTarget(row, isMaster)]; ok && preErrors[i] == "" {
			mergedRows[i] = mergeImportUpdate(prior, row)
			priorRows[i] = prior
		} else {
			blankImportClearMarkers(row)
			mergedRows[i] = row
		}
	}
	lk.prefetchLinks(schema, doctype, mergedRows)

	batchSuccessCount := 0
	var writes []importWrite
	for i, docData := range docRows {
		rowNumber := firstRowNumber + i

		if preErr := preErrors[i]; preErr != "" {
			recordImportError(result, rowNumber, preErr)
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
			permErr := lk.permissionsErr
			if permErr == nil {
				permErr = rejectRestrictedFieldWritesIn(lk.permissions, role, docData)
			}
			if permErr != nil {
				recordImportError(result, rowNumber, permErr.Error())
				continue
			}
		}

		// Is this row a create, or does it target an existing record by id?
		// Computed exactly the way the id-generation block below does it (an
		// "id" column that is present but blank counts as absent), because
		// the two Stage 51.8 passes immediately after this both have to make
		// the same call and must agree with it.
		rowIsCreate := true
		if existingID, hasID := docData["id"]; hasID && strings.TrimSpace(fmt.Sprintf("%v", existingID)) != "" {
			rowIsCreate = false
		}
		// The row as the file sent it decided the two answers above (field
		// permissions, create or update); from here on it is the merged record.
		sentStatus := strings.TrimSpace(strField(docData, "status"))
		docData = mergedRows[i]

		// Stage 51.8: an imported Item under a parent design gets its variant
		// values and the duplicate-combination check exactly as the
		// single-record save path does; its Combination ID is then the next
		// Item number, below (Stage 117). A no-op for a standalone Item, for
		// any other doctype, and for a row that already carries an explicit
		// code.
		if doctype == "Item" {
			// Stage 89: the item name's category / type / HSN / GST and the org's
			// defaults, as on the save path.
			if lk.itemDefaults == nil {
				lk.itemDefaults = NewItemDefaultsMemo()
			}
			if err := PrepareItemDefaultsMemo(tenantID, rowIsCreate, docData, lk.itemDefaults); err != nil {
				recordImportError(result, rowNumber, err.Error())
				continue
			}
			if err := PrepareItemVariantCode(tenantID, rowIsCreate, docData); err != nil {
				recordImportError(result, rowNumber, err.Error())
				continue
			}
		}

		// Stage 76.5: a master row with no code is numbered exactly as one
		// created on screen (V00001), instead of failing "code is required".
		// The number is drawn only once the row has passed validation, so a
		// refused row leaves no hole in the series; until then a placeholder
		// stands in for the mandatory code.
		needsNumber := false
		if rowIsCreate && strings.TrimSpace(strField(docData, "code")) == "" && strings.TrimSpace(strField(docData, "id")) == "" {
			if isMaster, _ := lk.masterDoctype(); (isMaster || IsServerNumberedMaster(doctype)) && !IsTypedCodeMaster(doctype) {
				needsNumber = true
				docData["code"] = "PENDING-NUMBER"
			}
		}

		// Perform field structure validation
		valErr := validateDocumentWith(tenantID, doctype, docData, lk.validation)
		if valErr != nil {
			recordImportError(result, rowNumber, valErr.Error())
			continue
		}
		// Stage 101.8: an erased customer is not imported back, and an import
		// cannot turn a customer's "no" into a "yes".
		if doctype == "Customer" {
			if err := CRMCheckImportedCustomer(tenantID, rowIsCreate, docData); err != nil {
				recordImportError(result, rowNumber, err.Error())
				continue
			}
			// 47.13.2a: against the stored record, so only a changed field
			// is stamped as coming from the import.
			crmImportStampSources(priorRows[i], docData, userID)
		}
		if needsNumber {
			delete(docData, "code")
			if err := PrepareMasterNumber(tenantID, "", doctype, true, docData); err != nil {
				recordImportError(result, rowNumber, err.Error())
				continue
			}
		}
		// Stage 49.3.4b: a PO's GST breakdown and totals, computed as its
		// record-form save computes them (ApplyPurchaseOrderTotals).
		if doctype == "PurchaseOrder" {
			if err := ApplyPurchaseOrderTotals(tenantID, docData); err != nil {
				recordImportError(result, rowNumber, err.Error())
				continue
			}
		}

		// Stage 51.8: restore the Master id = code invariant on this path too.
		// Stage 51.1 fixed it at handleGenericDoc and its comment claimed to
		// cover "every caller (UI, CSV import, API)", but CSV import does not
		// go through that handler - it lands here, where a row with no
		// explicit id falls through to GenerateSequence below and draws a
		// *separate* number from the same series its code came from. The
		// result is a bulk-imported Vendor/Item/Customer whose id and code
		// disagree, so every Link field pointing at it fails "record does
		// not exist" forever - 51.1's exact bug, on the one caller 51.1
		// believed it had already fixed. Runs after ValidateDocument so a
		// Master's mandatory `code` is already guaranteed present.
		if _, invErr := applyMasterIDCodeInvariant(rowIsCreate, docData, lk.masterDoctype); invErr != nil {
			recordImportError(result, rowNumber, fmt.Sprintf("could not resolve whether %s is a Master doctype: %v", doctype, invErr))
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
				recordImportError(result, rowNumber, fmt.Sprintf("duplicate id %q also appears earlier in this file", id))
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
			recordImportError(result, rowNumber, fmt.Sprintf("Failed to marshal JSON payload: %v", mErr))
			continue
		}
		writes = append(writes, importWrite{rowNumber: rowNumber, id: id, data: string(marshaled), status: sentStatus})
	}
	writes = screenImportWrites(tx, doctype, lk.rules, writes, result)
	writes = screenActionOnlyImportFields(tx, doctype, writes, result) // 49.5.6c

	// 49.4.4a: the batch's valid rows go in one statement instead of an
	// EXISTS query plus an upsert per row. RETURNING reports which ids were
	// created (xmax is 0 only on a freshly inserted row version) and which
	// updated - Stage 15.2's classification, still pre-write state
	// regardless of dryRun. seenIDs keeps an id from appearing twice here.
	//
	// ids are unique across every doctype in this table, so the update arm
	// only fires for a row of the same doctype; an id held by another record
	// type comes back missing and is refused instead of overwriting that
	// record's data (the generic create got the same guard in 7657453).
	var edits []importEdit
	if len(writes) > 0 {
		ids := make([]string, len(writes))
		datas := make([]string, len(writes))
		for i, w := range writes {
			ids[i], datas[i] = w.id, w.data
		}
		prior := importPriorData(tx, doctype, ids) // 49.5.4 (b)
		inserted := map[string]bool{}
		rows, execErr := tx.Query(`
			INSERT INTO documents (id, doctype, data, status, created_by)
			SELECT u.id, $1, u.data::jsonb, 'Active', $2
			FROM unnest($3::text[], $4::text[]) WITH ORDINALITY AS u(id, data, ord)
			ORDER BY u.ord
			ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, updated_at = CURRENT_TIMESTAMP
			WHERE documents.doctype = EXCLUDED.doctype
			RETURNING id, (xmax = 0)`, doctype, userID, pq.Array(ids), pq.Array(datas))
		if execErr == nil {
			for rows.Next() {
				var id string
				var created bool
				if execErr = rows.Scan(&id, &created); execErr != nil {
					break
				}
				inserted[id] = created
			}
			if execErr == nil {
				execErr = rows.Err()
			}
			rows.Close()
		}
		if execErr != nil {
			// The statement is all-or-nothing and the transaction is now
			// aborted, so every queued row failed and nothing commits.
			for _, w := range writes {
				recordImportError(result, w.rowNumber, fmt.Sprintf("Database write error: %v", execErr))
			}
			return nil
		}
		for _, w := range writes {
			created, written := inserted[w.id]
			if !written {
				recordImportError(result, w.rowNumber, fmt.Sprintf("id %q already belongs to a different record type - use another code", w.id))
				continue
			}
			result.SuccessRows++
			batchSuccessCount++
			result.controls.accept(w.rowNumber - 2) // row 2 is docRows[0]
			if created {
				result.CreatedIDs = append(result.CreatedIDs, w.id)
			} else {
				result.UpdatedIDs = append(result.UpdatedIDs, w.id)
				var next map[string]interface{}
				if before, ok := prior[w.id]; ok && json.Unmarshal([]byte(w.data), &next) == nil {
					edits = append(edits, importEdit{id: w.id, before: before, after: next})
				}
			}
		}
	}

	// Commit this batch if it has any successful rows - unless this is a
	// dry run, in which case the deferred tx.Rollback() above undoes this
	// batch's writes and nothing is actually persisted (Stage 15.2 preview).
	if !dryRun && batchSuccessCount > 0 {
		if err := tx.Commit(); err != nil {
			return err
		}
		// 49.5.4 (b): every record the import changed gets the same signed
		// EDIT_ row the record save writes, once the change is committed.
		for _, e := range edits {
			LogDocumentEdit(tenantID, userID, doctype, e.id, e.before, e.after, "CSV import")
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

	if doctype == "" && res.sourceDoctype != "" {
		doctype = res.sourceDoctype
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
	for k, v := range importJobSourceFields(res) {
		data[k] = v // Stage 55.11: failed rows kept for resume
	}
	if res.Controls != nil {
		data["control_totals"] = res.Controls // Stage 47.13.3
	}
	if res.RulesVersion != "" {
		data["rules_version"] = res.RulesVersion // 47.13.3: the rules it ran under
	}
	if res.Source != nil {
		data["source"] = res.Source // 47.13.3: file name, size, sha256, uploader
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
	headers, err := importTemplateHeaders(tenantID, doctype)
	if err != nil {
		return nil, err
	}
	return writeImportTemplateCSV(headers)
}

// importTemplateHeaders is GenerateCSVTemplate's header row, split out so
// the Stage 51.10 Item family template (pim_import_template.go) derives its
// columns from the same doctype meta instead of keeping a second list.
func importTemplateHeaders(tenantID string, doctype string) ([]string, error) {
	fields, err := GetDocTypeMeta(tenantID, doctype)
	if err != nil {
		return nil, err
	}

	var headers []string
	// Append ID always as the first column indicator
	headers = append(headers, "id")
	for _, f := range fields {
		if f.Fieldname == "id" || f.Fieldname == "status" {
			continue
		}
		headers = append(headers, f.Fieldname)
	}
	return headers, nil
}

func writeImportTemplateCSV(headers []string) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	row := make([]string, len(headers))
	for i := range headers {
		row[i] = sanitizeCSVCell(headers[i])
	}
	if err := writer.Write(row); err != nil {
		return nil, err
	}
	writer.Flush()
	return buf.Bytes(), nil
}
