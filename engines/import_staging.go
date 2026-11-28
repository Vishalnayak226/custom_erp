package engines

import (
	"bytes"
	"context"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/lib/pq"
)

// Stage 47.13.1: a staged import, inside PostgreSQL. The file is checked and
// parsed into import_staging_rows, transformed under a named rules version,
// validated and matched against what is already there, previewed, approved,
// committed and reconciled - each step its own call, each repeatable. It is
// the existing importer end to end: validation, merge-on-update, role and
// store checks all run in runDocDataImportOn (engines/import.go), the same
// core BulkImportCSV feeds, so a staged row is accepted or refused exactly as
// a directly imported one. The direct import is unchanged.
//
//	staging           a large file read in slices (297.2)  StageImportBatchFromUpload
//	parsed            upload safety, parse, normalize     StageImportBatch
//	validating        the same check, in slices (297.2)    RunImportBatchStep
//	validated         versioned transform, validate, match ValidateImportBatch
//	previewed         impact preview                       PreviewImportBatch
//	pending_approval  maker-checker (approval engine)      ApproveImportBatch
//	approved          a client with no rule approves here  ApproveImportBatch
//	committing        chunks, rows marked as they land     CommitImportBatch
//	committed         every valid row written              CommitImportBatch
//	reconciled        counts and totals tie out            ReconcileImportBatch
//	expired           staged rows removed                  SweepImportFileRetention
//
// The batch is an ImportBatch record in documents, because the approval
// engine works on documents; its rows are the table.
//
// 297.2: every step works a chunk at a time and records how far it got, so a
// large batch runs in the background (engines/import_staging_bulk.go) in
// slices that carry on after a restart, and a small one runs the same code
// inside the request.

const (
	ImportBatchDoctype    = "ImportBatch"
	ImportStagingMaxBytes = 5 << 20 // upload cap; the row cap is platform.max_import_rows
	importStageChunk      = 500     // rows per validation dry run
	importCommitChunk     = 200     // rows per commit transaction
	importCommitLease     = 10 * time.Minute
	importPreviewSamples  = 10
)

// Batch stages and staged-row statuses.
const (
	ImportStageStaging         = "staging" // 297.2: a large file being read in the background
	ImportStageParsed          = "parsed"
	ImportStageValidating      = "validating" // 297.2: a large batch being checked in the background
	ImportStageValidated       = "validated"
	ImportStagePreviewed       = "previewed"
	ImportStagePendingApproval = "pending_approval"
	ImportStageApproved        = "approved"
	ImportStageRejected        = "rejected"
	ImportStageCommitting      = "committing"
	ImportStageCommitted       = "committed"
	ImportStageReconciled      = "reconciled"
	ImportStageExpired         = "expired"

	importRowStaged    = "Staged"
	importRowValid     = "Valid"
	importRowInvalid   = "Invalid"
	importRowDuplicate = "Duplicate"
	importRowCommitted = "Committed"
	importRowFailed    = "Failed"
)

// ImportNaturalKeyRule is what a row is matched on: its id, or for a master
// (whose id is its code) the code. Fields the doctype marks unique are held by
// the same validation a direct import runs.
const ImportNaturalKeyRule = "id, or the code of a master"

// importCommitAfterChunk is a test seam for failure injection: called after
// each committed chunk, an error stops the commit there as a crash would.
var importCommitAfterChunk func(chunk int) error

type ImportBatchCounts struct {
	Rows      int `json:"rows"`
	Valid     int `json:"valid"`
	Invalid   int `json:"invalid"`
	Duplicate int `json:"duplicate"`
	New       int `json:"new"`
	Update    int `json:"update"`
	Committed int `json:"committed"`
	Failed    int `json:"failed"`
}

type ImportPreviewChange struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

type ImportPreviewSample struct {
	Row     int                   `json:"row"`
	ID      string                `json:"id,omitempty"`
	Changes []ImportPreviewChange `json:"changes,omitempty"`
	Message string                `json:"message,omitempty"`
}

type ImportBatchPreview struct {
	Counts     ImportBatchCounts     `json:"counts"`
	Totals     []ImportColumnTotal   `json:"totals,omitempty"`
	NewRows    []ImportPreviewSample `json:"new_rows,omitempty"`
	Updates    []ImportPreviewSample `json:"updates,omitempty"`
	Duplicates []ImportPreviewSample `json:"duplicates,omitempty"`
	Errors     []ImportPreviewSample `json:"errors,omitempty"`
	At         string                `json:"at"`
}

type ImportBatchReconcile struct {
	Balanced    bool                `json:"balanced"`
	RowsInFile  int                 `json:"rows_in_file"`
	Committed   int                 `json:"committed"`
	Rejected    int                 `json:"rejected"`
	Created     int                 `json:"created"`
	Updated     int                 `json:"updated"`
	KeysChecked int                 `json:"keys_checked"`
	KeysMissing int                 `json:"keys_missing"`
	Columns     []ImportColumnTotal `json:"columns,omitempty"`
	Issues      []string            `json:"issues,omitempty"`
	At          string              `json:"at"`
}

// ImportBatchData is the ImportBatch record's data. Status is the record's own
// (Draft, Pending Approval, Approved, Rejected, Committed, Reconciled,
// Expired); Stage is the finer step.
type ImportBatchData struct {
	ID               string                `json:"id"`
	Code             string                `json:"code"`
	DoctypeName      string                `json:"doctype_name"`
	Status           string                `json:"status"`
	Stage            string                `json:"stage"`
	FileName         string                `json:"file_name"`
	SizeBytes        int64                 `json:"size_bytes"`
	SHA256           string                `json:"sha256"`
	Encoding         string                `json:"encoding"`
	UploadedBy       string                `json:"uploaded_by"`
	UploadedAt       string                `json:"uploaded_at"`
	Headers          []string              `json:"headers"`
	RulesVersion     string                `json:"rules_version"`
	NaturalKey       string                `json:"natural_key"`
	Counts           ImportBatchCounts     `json:"counts"`
	FileTotals       []ImportColumnTotal   `json:"file_totals"`
	Preview          *ImportBatchPreview   `json:"preview"`
	Reconcile        *ImportBatchReconcile `json:"reconcile"`
	Declared         []ImportDeclaredTotal `json:"declared_totals,omitempty"`
	ImportJobID      string                `json:"import_job_id,omitempty"`
	Warnings         []string              `json:"warnings"`
	ApprovedBy       string                `json:"approved_by"`
	ApprovedAt       string                `json:"approved_at"`
	CommitLeaseUntil string                `json:"commit_lease_until"`
	CommitLeaseOwner string                `json:"commit_lease_owner"`
	CommitCreated    int                   `json:"commit_created"`
	CommitUpdated    int                   `json:"commit_updated"`
	CommittedAt      string                `json:"committed_at"`
	ExpiredAt        string                `json:"raw_rows_expired_at"`
	// Work (297.2) is a step running in slices: what it is, which job runs it
	// and how far it has got. Only the step itself writes it (saveImportBatchOn
	// leaves it alone), and it is cleared when the step ends.
	Work *ImportBatchWork `json:"work"`
}

// ImportBatchView is a batch as the API returns it.
type ImportBatchView struct {
	ImportBatchData
	RulesCurrent     bool `json:"rules_current"`
	AlreadyStaged    bool `json:"already_staged,omitempty"`
	AlreadyCommitted bool `json:"already_committed,omitempty"`
	// WorkLive: the batch's step (Work) is still running; a step that is not
	// has stopped and can be resumed (297.2).
	WorkLive bool `json:"work_live,omitempty"`
}

func importBatchErr(code, format string, args ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

func importBatchView(b *ImportBatchData) *ImportBatchView {
	return &ImportBatchView{ImportBatchData: *b, RulesCurrent: b.RulesVersion == ImportRulesVersion}
}

// ---- upload safety ---------------------------------------------------------

// cp1252High maps the Windows-1252 bytes 0x80-0x9F that differ from Latin-1.
var cp1252High = map[byte]rune{0x80: 0x20AC, 0x82: 0x201A, 0x83: 0x0192, 0x84: 0x201E, 0x85: 0x2026,
	0x86: 0x2020, 0x87: 0x2021, 0x88: 0x02C6, 0x89: 0x2030, 0x8A: 0x0160, 0x8B: 0x2039, 0x8C: 0x0152,
	0x8E: 0x017D, 0x91: 0x2018, 0x92: 0x2019, 0x93: 0x201C, 0x94: 0x201D, 0x95: 0x2022, 0x96: 0x2013,
	0x97: 0x2014, 0x98: 0x02DC, 0x99: 0x2122, 0x9A: 0x0161, 0x9B: 0x203A, 0x9C: 0x0153, 0x9E: 0x017E, 0x9F: 0x0178}

// normalizeImportEncoding returns the file as UTF-8 without a byte-order mark,
// and what it was: UTF-8 (with or without BOM), UTF-16 with a BOM (Excel's
// "Unicode text"), or - when the bytes are not valid UTF-8 - Windows-1252,
// which is what Excel's plain "CSV" saves on Windows. Content that is not
// text is refused by CheckUpload before any of that.
func normalizeImportEncoding(b []byte) ([]byte, string, error) {
	switch {
	case len(b) >= 2 && ((b[0] == 0xFF && b[1] == 0xFE) || (b[0] == 0xFE && b[1] == 0xFF)):
		le := b[0] == 0xFF
		body := b[2:]
		if len(body)%2 != 0 {
			return nil, "", importBatchErr("DATAIM-0486", "the file is UTF-16 but its length is odd - save it again from Excel as CSV UTF-8")
		}
		u := make([]uint16, len(body)/2)
		for i := range u {
			if le {
				u[i] = uint16(body[2*i]) | uint16(body[2*i+1])<<8
			} else {
				u[i] = uint16(body[2*i])<<8 | uint16(body[2*i+1])
			}
		}
		out := []byte(string(utf16.Decode(u)))
		if !utf8.Valid(out) {
			return nil, "", importBatchErr("DATAIM-0486", "the file could not be read as text - save it again from Excel as CSV UTF-8")
		}
		return out, "utf-16", CheckUpload(UploadCSV, out)
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		out := b[3:]
		return out, "utf-8-bom", CheckUpload(UploadCSV, out)
	}
	if err := CheckUpload(UploadCSV, b); err != nil {
		return nil, "", err
	}
	if utf8.Valid(b) {
		return b, "utf-8", nil
	}
	var sb strings.Builder
	for _, c := range b {
		if r, ok := cp1252High[c]; ok {
			sb.WriteRune(r)
		} else {
			sb.WriteRune(rune(c))
		}
	}
	return []byte(sb.String()), "windows-1252", nil
}

// transformImportRow is the versioned transform: one row's raw cells, trimmed
// and made formula-safe, keyed by the lower-cased header. Anything that changes
// how a cell is read bumps ImportRulesVersion.
func transformImportRow(headers []string, raw []string) map[string]interface{} {
	out := make(map[string]interface{}, len(headers))
	for i, v := range raw {
		if i < len(headers) {
			out[headers[i]] = sanitizeCSVCell(strings.TrimSpace(v))
		}
	}
	return out
}

// ---- batch record ----------------------------------------------------------

func deriveImportStage(b *ImportBatchData, status string) {
	b.Status = status
	switch {
	case status == "Approved" && (b.Stage == ImportStagePendingApproval || b.Stage == ImportStagePreviewed):
		b.Stage = ImportStageApproved
	case status == "Rejected":
		b.Stage = ImportStageRejected
	}
}

func loadImportBatch(tenantID, id string) (*ImportBatchData, string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", err
	}
	var raw []byte
	var status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema),
		ImportBatchDoctype, id).Scan(&raw, &status)
	if err == sql.ErrNoRows {
		return nil, "", importBatchErr("DATAIM-0481", "import batch %s was not found", id)
	}
	if err != nil {
		return nil, "", err
	}
	b := &ImportBatchData{}
	if err := json.Unmarshal(raw, b); err != nil {
		return nil, "", err
	}
	deriveImportStage(b, status)
	return b, status, nil
}

// LoadImportBatch returns one batch; a batch id from another client is not found.
func LoadImportBatch(tenantID, id string) (*ImportBatchView, error) {
	b, _, err := loadImportBatch(tenantID, id)
	if err != nil {
		return nil, err
	}
	v := importBatchView(b)
	if b.Work != nil {
		if schema, err := db.GetTenantSchema(tenantID); err == nil {
			v.WorkLive = importWorkLive(schema, b.Work)
		}
	}
	return v, nil
}

type importExec interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// saveImportBatchOn writes the batch's own fields over the record's data (what
// the approval engine keeps on it survives) and, when status is given, its status.
func saveImportBatchOn(q importExec, schema string, b *ImportBatchData, status string) error {
	m := map[string]interface{}{}
	raw, _ := json.Marshal(b)
	_ = json.Unmarshal(raw, &m)
	delete(m, "status")
	delete(m, "work") // only the running step writes its own progress (297.2)
	patch, _ := json.Marshal(m)
	if status != "" {
		_, err := q.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = $3 AND id = $4`, schema),
			patch, status, ImportBatchDoctype, b.ID)
		return err
	}
	_, err := q.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = $2 AND id = $3`, schema),
		patch, ImportBatchDoctype, b.ID)
	return err
}

func saveImportBatch(tenantID string, b *ImportBatchData, status string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	return saveImportBatchOn(db.DB, schema, b, status)
}

// ListImportBatches returns the client's batches, newest first.
func ListImportBatches(tenantID string, limit int) ([]*ImportBatchView, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 500 {
		limit = 200
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL ORDER BY created_at DESC, id DESC LIMIT $2`, schema),
		ImportBatchDoctype, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ImportBatchView{}
	for rows.Next() {
		var raw []byte
		var status string
		if err := rows.Scan(&raw, &status); err != nil {
			return nil, err
		}
		b := &ImportBatchData{}
		if json.Unmarshal(raw, b) != nil {
			continue
		}
		deriveImportStage(b, status)
		b.Preview = nil // the list stays small; the batch screen has it
		v := importBatchView(b)
		if b.Work != nil {
			v.WorkLive = importWorkLive(schema, b.Work)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---- staged rows -----------------------------------------------------------

type ImportStagedRow struct {
	RowNo     int                    `json:"row_no"`
	Status    string                 `json:"status"`
	MatchKind string                 `json:"match_kind,omitempty"`
	MatchID   string                 `json:"match_id,omitempty"`
	Key       string                 `json:"natural_key,omitempty"`
	Errors    []string               `json:"errors,omitempty"`
	Data      map[string]interface{} `json:"data"`
}

const stagedRowCols = `row_no, status, match_kind, match_id, natural_key, errors, normalized`

func scanStagedRows(rows *sql.Rows) ([]ImportStagedRow, error) {
	defer rows.Close()
	var out []ImportStagedRow
	for rows.Next() {
		var r ImportStagedRow
		var norm, errs []byte
		var kind, mid, key sql.NullString
		if err := rows.Scan(&r.RowNo, &r.Status, &kind, &mid, &key, &errs, &norm); err != nil {
			return nil, err
		}
		r.MatchKind, r.MatchID, r.Key = kind.String, mid.String, key.String
		_ = json.Unmarshal(norm, &r.Data)
		if len(errs) > 0 {
			_ = json.Unmarshal(errs, &r.Errors)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListImportBatchRows pages a batch's staged rows, optionally one status.
func ListImportBatchRows(tenantID, batchID, status string, limit, offset int) ([]ImportStagedRow, error) {
	if _, _, err := loadImportBatch(tenantID, batchID); err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT `+stagedRowCols+` FROM %s.import_staging_rows
		WHERE batch_id = $1 AND ($2 = '' OR status = $2) ORDER BY row_no LIMIT $3 OFFSET $4`, schema), batchID, status, limit, offset)
	if err != nil {
		return nil, err
	}
	return scanStagedRows(rows)
}

func importRowCounts(schema, batchID string) (ImportBatchCounts, error) {
	var c ImportBatchCounts
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT status, COALESCE(match_kind, ''), COUNT(*) FROM %s.import_staging_rows WHERE batch_id = $1 GROUP BY 1, 2`, schema), batchID)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var status, kind string
		var n int
		if err := rows.Scan(&status, &kind, &n); err != nil {
			return c, err
		}
		c.Rows += n
		switch status {
		case importRowValid:
			c.Valid += n
		case importRowInvalid:
			c.Invalid += n
		case importRowDuplicate:
			c.Duplicate += n
		case importRowCommitted:
			c.Committed += n
		case importRowFailed:
			c.Failed += n
		}
		if status == importRowValid || status == importRowCommitted || status == importRowFailed {
			switch kind {
			case "new":
				c.New += n
			case "update":
				c.Update += n
			}
		}
	}
	return c, rows.Err()
}

// importFileTotals sums each Number column over docRows; accept says which
// rows count as accepted (nil: none).
func importFileTotals(fields []FieldMeta, docRows []map[string]interface{}, accept func(i int) bool) []ImportColumnTotal {
	c := newImportControls(fields, docRows)
	if accept != nil {
		for i := range docRows {
			if accept(i) {
				c.accept(i)
			}
		}
	}
	out := make([]ImportColumnTotal, 0, len(c.columns))
	for _, col := range c.columns {
		col.Read, col.Accepted = roundControl(col.Read), roundControl(col.Accepted)
		col.Rejected = roundControl(col.Read - col.Accepted)
		out = append(out, col)
	}
	return out
}

// ---- 1. upload, parse, normalize --------------------------------------------

// StageImportBatch checks the upload, parses it and stages its rows. The same
// file for the same record type is never staged twice: a repeat returns the
// batch it made (AlreadyStaged), unless that batch was rejected or expired.
func StageImportBatch(ctx context.Context, tenantID, doctype string, r io.Reader, fileName, userID, role string) (*ImportBatchView, error) {
	doctype = strings.TrimSpace(doctype)
	if doctype == "" || doctype == ImportBatchDoctype {
		return nil, importBatchErr("DATAIM-0481", "choose the record type to import")
	}
	fields, err := GetDocTypeMeta(tenantID, doctype)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, importBatchErr("DATAIM-0481", "%s is not a record type that can be imported", doctype)
	}
	body, err := io.ReadAll(io.LimitReader(r, ImportStagingMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > ImportStagingMaxBytes {
		return nil, importBatchErr("DATAIM-0480", "the file is larger than %d MB - split it into smaller files", ImportStagingMaxBytes>>20)
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var existing string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
		AND data->>'sha256' = $2 AND data->>'doctype_name' = $3 AND status NOT IN ('Rejected', 'Expired') ORDER BY created_at LIMIT 1`, schema),
		ImportBatchDoctype, digest, doctype).Scan(&existing)
	if err == nil {
		b, _, lerr := loadImportBatch(tenantID, existing)
		if lerr != nil {
			return nil, lerr
		}
		v := importBatchView(b)
		v.AlreadyStaged = true
		v.WorkLive = importWorkLive(schema, b.Work) // 297.2: that batch may still be working
		return v, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	text, encoding, err := normalizeImportEncoding(body)
	if err != nil {
		return nil, err
	}
	records, err := readCSVRecordsContext(ctx, tenantID, bytes.NewReader(text))
	if err != nil {
		return nil, err
	}
	headers := records[0]
	for i, h := range headers {
		headers[i] = strings.TrimSpace(strings.ToLower(h))
	}
	if missing := missingMandatoryColumns(tenantID, doctype, headers); len(missing) > 0 {
		return nil, importBatchErr("DATAIM-0164", "uploaded file is missing mandatory column(s): %s", strings.Join(missing, ", "))
	}

	id := NewDocIDCompact("IMPBAT")
	b := &ImportBatchData{
		ID: id, Code: id, DoctypeName: doctype, Stage: ImportStageParsed, Status: "Draft",
		FileName: fileName, SizeBytes: int64(len(body)), SHA256: digest, Encoding: encoding,
		UploadedBy: userID, UploadedAt: time.Now().UTC().Format(time.RFC3339), Headers: headers,
		RulesVersion: ImportRulesVersion, NaturalKey: ImportNaturalKeyRule,
		Counts: ImportBatchCounts{Rows: len(records) - 1},
	}
	docRows := make([]map[string]interface{}, len(records)-1)
	for i, rec := range records[1:] {
		docRows[i] = transformImportRow(headers, rec)
	}
	b.FileTotals = importFileTotals(fields, docRows, nil)
	data, _ := json.Marshal(b)

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema),
		id, ImportBatchDoctype, data, userID); err != nil {
		return nil, err
	}
	ins, err := tx.Prepare(fmt.Sprintf(`INSERT INTO %s.import_staging_rows (batch_id, row_no, raw, normalized, transform_version, status) VALUES ($1, $2, $3, $4, $5, 'Staged')`, schema))
	if err != nil {
		return nil, err
	}
	defer ins.Close()
	for i, rec := range records[1:] {
		rawCells := make(map[string]string, len(headers))
		for c, v := range rec {
			if c < len(headers) {
				rawCells[headers[c]] = v
			}
		}
		rawJSON, _ := json.Marshal(rawCells)
		normJSON, _ := json.Marshal(docRows[i])
		if _, err := ins.Exec(id, i+2, rawJSON, normJSON, ImportRulesVersion); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "IMPORT_BATCH_STAGED", "Success", fmt.Sprintf("batch=%s doctype=%s rows=%d sha256=%s", id, doctype, len(docRows), digest))
	return importBatchView(b), nil
}

// ---- 2. versioned transform, validate, duplicate match ----------------------

// importBatchEditable: a batch can be checked again until it goes for approval.
func importBatchEditable(b *ImportBatchData) bool {
	switch b.Stage {
	case ImportStageParsed, ImportStageValidating, ImportStageValidated, ImportStagePreviewed:
		return b.Status == "Draft"
	}
	return false
}

// ValidateImportBatch checks a batch to its end inside the call (the screen
// sends a large one to the background through RunImportBatchStep). Each row is
// transformed again from its raw cells when it was read under older rules (the
// batch is stamped with ImportRulesVersion - the explicit way a batch staged
// under older rules is brought up to date), validated exactly as a direct
// import would (a dry run of the import core, nothing written) and matched:
// against earlier rows of the same file (a repeat of a key is a Duplicate) and
// against the records already there (update or new). See importStepRun.validate.
func ValidateImportBatch(ctx context.Context, tenantID, batchID, userID, role string) (*ImportBatchView, error) {
	return runImportStepInline(ctx, tenantID, batchID, importStepValidate, userID, role)
}

func importTruncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// ---- 3. impact preview -------------------------------------------------------

// importPreviewScan bounds how far into a batch's rows of one status the
// preview looks for its samples, so a batch of any size previews at once.
const importPreviewScan = 100000

// PreviewImportBatch states what a commit would do before anything is written:
// how many records would be added and changed, which rows are repeats or
// errors, the Number columns' totals, and for the first changed records the
// fields that would change and from what.
func PreviewImportBatch(ctx context.Context, tenantID, batchID string) (*ImportBatchView, error) {
	b, _, err := loadImportBatch(tenantID, batchID)
	if err != nil {
		return nil, err
	}
	if !importBatchEditable(b) || b.Stage == ImportStageParsed || b.Stage == ImportStageValidating {
		return nil, importBatchErr("DATAIM-0481", "batch %s must be checked first (it is %s)", b.ID, strings.ToLower(b.Stage))
	}
	if b.RulesVersion != ImportRulesVersion {
		return nil, importBatchErr("DATAIM-0482", "batch %s was checked under import rules %s; today's are %s - check it again", b.ID, b.RulesVersion, ImportRulesVersion)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	sample := func(status, kind string, n int) ([]ImportStagedRow, error) {
		q, err := db.DB.QueryContext(ctx, fmt.Sprintf(`SELECT `+stagedRowCols+` FROM (SELECT `+stagedRowCols+` FROM %s.import_staging_rows
			WHERE batch_id = $1 AND status = $2 ORDER BY row_no LIMIT $5) s
			WHERE ($3 = '' OR match_kind = $3) ORDER BY row_no LIMIT $4`, schema), b.ID, status, kind, n, importPreviewScan)
		if err != nil {
			return nil, err
		}
		return scanStagedRows(q)
	}
	p := &ImportBatchPreview{Counts: b.Counts, Totals: b.FileTotals, At: time.Now().UTC().Format(time.RFC3339)}
	news, err := sample(importRowValid, "new", importPreviewSamples)
	if err != nil {
		return nil, err
	}
	for _, r := range news {
		p.NewRows = append(p.NewRows, ImportPreviewSample{Row: r.RowNo, ID: r.Key})
	}
	upds, err := sample(importRowValid, "update", importPreviewSamples)
	if err != nil {
		return nil, err
	}
	stored := map[string]map[string]interface{}{}
	if len(upds) > 0 {
		ids := make([]string, len(upds))
		for i, r := range upds {
			ids[i] = r.MatchID
		}
		docs, err := importDocsByID(ctx, db.DB, schema+".documents", ids, true, false) // 313.2
		if err != nil {
			return nil, err
		}
		for id, d := range docs {
			if d.doctype == b.DoctypeName {
				m := map[string]interface{}{}
				_ = json.Unmarshal(d.data, &m)
				stored[id] = m
			}
		}
	}
	for _, r := range upds {
		p.Updates = append(p.Updates, ImportPreviewSample{Row: r.RowNo, ID: r.MatchID, Changes: importPreviewChanges(stored[r.MatchID], r.Data)})
	}
	dups, err := sample(importRowDuplicate, "", importPreviewSamples)
	if err != nil {
		return nil, err
	}
	for _, r := range dups {
		p.Duplicates = append(p.Duplicates, ImportPreviewSample{Row: r.RowNo, ID: r.Key, Message: strings.Join(r.Errors, "; ")})
	}
	bad, err := sample(importRowInvalid, "", importPreviewSamples*2)
	if err != nil {
		return nil, err
	}
	for _, r := range bad {
		p.Errors = append(p.Errors, ImportPreviewSample{Row: r.RowNo, ID: r.Key, Message: strings.Join(r.Errors, "; ")})
	}
	b.Preview = p
	b.Stage = ImportStagePreviewed
	if err := saveImportBatch(tenantID, b, ""); err != nil {
		return nil, err
	}
	return importBatchView(b), nil
}

// importPreviewChanges lists the fields a row would change on a stored record,
// by the import's own update rule: a filled cell replaces, a blank cell keeps,
// ImportClearMarker clears.
func importPreviewChanges(stored, row map[string]interface{}) []ImportPreviewChange {
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []ImportPreviewChange
	for _, k := range keys {
		if k == "id" {
			continue
		}
		cell := importCellText(row, k)
		to := cell
		switch {
		case isImportClearMarker(cell):
			to = ""
		case cell == "":
			continue
		}
		from := ""
		if v, ok := stored[k]; ok && v != nil {
			from = fmt.Sprintf("%v", v)
		}
		if from != to {
			out = append(out, ImportPreviewChange{Field: k, From: from, To: to})
		}
	}
	return out
}

// ---- 4. approve ---------------------------------------------------------------

// ApproveImportBatch sends a previewed batch for approval through the
// maker-checker engine: the batch is an ImportBatch record, the client's
// approval rule for it names who decides, and the client's self-approval
// setting decides whether the person who staged it may. A client with no rule
// for ImportBatch approves the batch here, directly (nobody is configured to
// be the second person). Asking again for a batch already sent or approved
// changes nothing.
func ApproveImportBatch(ctx context.Context, tenantID, batchID, actorID, actorRole string) (*ImportBatchView, error) {
	b, status, err := loadImportBatch(tenantID, batchID)
	if err != nil {
		return nil, err
	}
	if status == "Pending Approval" || status == "Approved" {
		return importBatchView(b), nil
	}
	if b.Stage != ImportStagePreviewed || status != "Draft" {
		return nil, importBatchErr("DATAIM-0481", "batch %s must be previewed before it is approved (it is %s)", b.ID, strings.ToLower(b.Stage))
	}
	if b.RulesVersion != ImportRulesVersion {
		return nil, importBatchErr("DATAIM-0482", "batch %s was checked under import rules %s; today's are %s - check it again", b.ID, b.RulesVersion, ImportRulesVersion)
	}
	if b.Counts.Valid < 1 {
		return nil, importBatchErr("DATAIM-0485", "batch %s has no valid rows to import", b.ID)
	}
	if mm := importDeclaredMismatch(b); len(mm) > 0 {
		return nil, importBatchErr("DATAIM-0487", "the file does not tie to the totals stated - %s", strings.Join(mm, "; "))
	}
	approver, err := RequiredApproverRoleForAmount(tenantID, ImportBatchDoctype, 0)
	if err != nil {
		return nil, err
	}
	if approver != "" {
		if err := SubmitForApproval(tenantID, ImportBatchDoctype, b.ID, actorID, actorRole); err != nil {
			return nil, err
		}
		b.Stage = ImportStagePendingApproval
		if err := saveImportBatch(tenantID, b, ""); err != nil {
			return nil, err
		}
		b2, _, err := loadImportBatch(tenantID, b.ID)
		if err != nil {
			return nil, err
		}
		return importBatchView(b2), nil
	}
	b.Stage = ImportStageApproved
	b.ApprovedBy = actorID
	b.ApprovedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveImportBatch(tenantID, b, "Approved"); err != nil {
		return nil, err
	}
	_ = logApprovalAction(tenantID, ImportBatchDoctype, b.ID, "Approved", actorID, actorRole, 0, "no approval rule for ImportBatch")
	b.Status = "Approved"
	return importBatchView(b), nil
}

// ---- 5. idempotent commit --------------------------------------------------------

// CommitImportBatch writes an approved batch's valid rows through the import
// core, a chunk per transaction, then reconciles it - all inside the call (the
// screen sends a large batch to the background through RunImportBatchStep). A
// row is marked Committed in the same transaction that writes its record, so a
// repeat, a retry after an error and a resume after a crash write nothing
// twice: each takes only the rows still Valid. A committed batch asked again
// returns what it did. A chunk holding a row the importer now refuses (the data
// moved since the check) is retried a row at a time, so the rows that can go
// in do, and the others are marked Failed with the reason. See
// importStepRun.commit.
func CommitImportBatch(ctx context.Context, tenantID, batchID, actorID, actorRole string) (*ImportBatchView, error) {
	return runImportStepInline(ctx, tenantID, batchID, importStepCommit, actorID, actorRole)
}

// commitImportChunk writes one chunk atomically with its rows' Committed mark;
// if the chunk is refused it goes row by row.
func commitImportChunk(ctx context.Context, tenantID, schema string, b *ImportBatchData, rows []ImportStagedRow, actorID, actorRole string) error {
	err := writeImportChunk(ctx, tenantID, schema, b, rows, actorID, actorRole)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(rows) == 1 {
		return markImportRowFailed(schema, b.ID, rows[0].RowNo, err.Error())
	}
	for _, r := range rows {
		if err := writeImportChunk(ctx, tenantID, schema, b, []ImportStagedRow{r}, actorID, actorRole); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if ferr := markImportRowFailed(schema, b.ID, r.RowNo, err.Error()); ferr != nil {
				return ferr
			}
		}
	}
	return nil
}

func markImportRowFailed(schema, batchID string, rowNo int, msg string) error {
	j, _ := json.Marshal([]string{msg})
	_, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.import_staging_rows SET status = 'Failed', errors = $3 WHERE batch_id = $1 AND row_no = $2 AND status = 'Valid'`, schema), batchID, rowNo, string(j))
	return err
}

// writeImportChunk is one transaction: the rows written by the import core,
// marked Committed, and added to the batch's created / updated counts.
func writeImportChunk(ctx context.Context, tenantID, schema string, b *ImportBatchData, rows []ImportStagedRow, actorID, actorRole string) error {
	docRows := make([]map[string]interface{}, len(rows))
	nos := make([]int64, len(rows))
	for i, r := range rows {
		docRows[i] = r.Data
		nos[i] = int64(r.RowNo)
	}
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := runDocDataImportInTx(ctx, tx, tenantID, b.DoctypeName, actorID, actorRole, docRows, make([]string, len(docRows)))
	if err != nil {
		return err
	}
	if res.FailedRows > 0 {
		msg := "the importer refused this row"
		if len(res.Errors) > 0 {
			msg = res.Errors[0].Message
		}
		return fmt.Errorf("%s", msg)
	}
	marked, err := tx.Exec(fmt.Sprintf(`UPDATE %s.import_staging_rows SET status = 'Committed', committed_at = NOW()
		WHERE batch_id = $1 AND row_no = ANY($2) AND status = 'Valid'`, schema), b.ID, pq.Array(nos))
	if err != nil {
		return err
	}
	if n, _ := marked.RowsAffected(); int(n) != len(rows) {
		return fmt.Errorf("rows of this chunk were committed by another run")
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object(
			'commit_created', COALESCE((data->>'commit_created')::int, 0) + $3::int,
			'commit_updated', COALESCE((data->>'commit_updated')::int, 0) + $4::int)
		WHERE doctype = $1 AND id = $2`, schema), ImportBatchDoctype, b.ID, len(res.CreatedIDs), len(res.UpdatedIDs)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, e := range res.pendingEdits {
		LogDocumentEdit(tenantID, actorID, b.DoctypeName, e.id, e.before, e.after, "Staged import "+b.ID)
	}
	return nil
}

// ---- 6. reconcile ----------------------------------------------------------------------

// ReconcileImportBatch ties a committed batch out and records it on the batch:
// the file's rows = committed + rejected (invalid, repeated, failed); records
// created + updated = rows committed; each Number column's total, read again
// from the staged rows, equals the total taken at upload; and every committed
// row's key is a record that exists. A batch that ties out is Reconciled; one
// that does not stays Committed with the differences listed. It can be asked
// again. See importStepRun.reconcile.
func ReconcileImportBatch(ctx context.Context, tenantID, batchID, actorID string) (*ImportBatchView, error) {
	return runImportStepInline(ctx, tenantID, batchID, importStepReconcile, actorID, "")
}

// DiscardImportBatch removes an uncommitted batch's staged rows and frees its
// file to be staged again. A committed or approved batch is not discarded; a
// batch still being read or checked is, and its background step stops at its
// next chunk.
func DiscardImportBatch(tenantID, batchID, actorID string) (*ImportBatchView, error) {
	b, status, err := loadImportBatch(tenantID, batchID)
	if err != nil {
		return nil, err
	}
	switch b.Stage {
	case ImportStageCommitted, ImportStageReconciled, ImportStageCommitting, ImportStageExpired, ImportStageApproved:
		return nil, importBatchErr("DATAIM-0481", "batch %s is %s and cannot be discarded", b.ID, strings.ToLower(b.Stage))
	}
	if err := expireImportBatch(tenantID, b, status); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, actorID, "IMPORT_BATCH_DISCARDED", "Success", "batch="+b.ID)
	b.Status = "Expired"
	return importBatchView(b), nil
}

// expireImportBatch marks a batch Expired and removes its staged rows (the
// file's contents); its counts, totals, reconciliation and lineage stay. A
// step still running on it is let go. The first importPurgeChunk rows go with
// the mark; a larger batch's rest are removed by a background job (297.2).
func expireImportBatch(tenantID string, b *ImportBatchData, fromStatus string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	upload := ""
	if b.Work != nil {
		upload = b.Work.UploadID
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	b.Stage = ImportStageExpired
	b.ExpiredAt = time.Now().UTC().Format(time.RFC3339)
	b.Warnings = append(b.Warnings, "expired from "+strings.ToLower(fromStatus))
	if err := saveImportBatchOn(tx, schema, b, "Expired"); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || '{"work":null}'::jsonb WHERE doctype = $1 AND id = $2`, schema), ImportBatchDoctype, b.ID); err != nil {
		return err
	}
	res, err := tx.Exec(fmt.Sprintf(`DELETE FROM %[1]s.import_staging_rows WHERE batch_id = $1 AND row_no IN
		(SELECT row_no FROM %[1]s.import_staging_rows WHERE batch_id = $1 ORDER BY row_no LIMIT $2)`, schema), b.ID, importPurgeChunk)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	b.Work = nil
	if upload != "" {
		markUploadUsed(schema, upload)
	}
	if n, _ := res.RowsAffected(); n >= importPurgeChunk {
		// Failing to queue it leaves the rows to the daily sweep
		// (SweepImportStaging), which queues it again.
		if err := enqueueImportPurge(tenantID, schema, b.ID); err != nil {
			LogBackgroundJobError("IMPORT-FILE-SWEEP", "purge", schema, err)
		}
	}
	return nil
}

// ---- 7. raw-file expiry ------------------------------------------------------------------

// sweepImportBatches expires batches created before cutoff that are not
// waiting on a person: abandoned drafts, rejected, committed and reconciled
// ones. A batch pending approval or approved is never swept, nor one with a
// step still running.
func sweepImportBatches(tenantID, schema string, cutoff time.Time) (int64, error) {
	var tbl sql.NullString
	if err := db.DB.QueryRow(`SELECT to_regclass($1)::text`, schema+".import_staging_rows").Scan(&tbl); err != nil || !tbl.Valid {
		return 0, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, status FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL AND created_at < $2
		AND status IN ('Draft', 'Rejected', 'Committed', 'Reconciled')`, schema), ImportBatchDoctype, cutoff)
	if err != nil {
		return 0, err
	}
	type pend struct{ id, status string }
	var list []pend
	for rows.Next() {
		var p pend
		if rows.Scan(&p.id, &p.status) == nil {
			list = append(list, p)
		}
	}
	rows.Close()
	var n int64
	for _, p := range list {
		b, _, err := loadImportBatch(tenantID, p.id)
		if err != nil || importWorkLive(schema, b.Work) {
			continue
		}
		if err := expireImportBatch(tenantID, b, p.status); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
