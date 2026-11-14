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
//	parsed            upload safety, parse, normalize     StageImportBatch
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
	ImportStageParsed          = "parsed"
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
	CommitCreated    int                   `json:"commit_created"`
	CommitUpdated    int                   `json:"commit_updated"`
	CommittedAt      string                `json:"committed_at"`
	ExpiredAt        string                `json:"raw_rows_expired_at"`
}

// ImportBatchView is a batch as the API returns it.
type ImportBatchView struct {
	ImportBatchData
	RulesCurrent     bool `json:"rules_current"`
	AlreadyStaged    bool `json:"already_staged,omitempty"`
	AlreadyCommitted bool `json:"already_committed,omitempty"`
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
	return importBatchView(b), nil
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
		out = append(out, importBatchView(b))
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

func allStagedRows(schema, batchID string) ([]ImportStagedRow, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT `+stagedRowCols+` FROM %s.import_staging_rows WHERE batch_id = $1 ORDER BY row_no`, schema), batchID)
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
	case ImportStageParsed, ImportStageValidated, ImportStagePreviewed:
		return b.Status == "Draft"
	}
	return false
}

// ValidateImportBatch re-runs the transform from the raw cells under today's
// rules (stamping ImportRulesVersion on the batch - the explicit way a batch
// staged under older rules is brought up to date), then validates every row
// exactly as a direct import would (a dry run of the import core, nothing
// written) and matches it: against earlier rows of the same file (a repeat of
// a key is a Duplicate) and against the records already there (update or new).
func ValidateImportBatch(ctx context.Context, tenantID, batchID, userID, role string) (*ImportBatchView, error) {
	b, _, err := loadImportBatch(tenantID, batchID)
	if err != nil {
		return nil, err
	}
	if !importBatchEditable(b) {
		return nil, importBatchErr("DATAIM-0481", "batch %s is %s - it can only be checked before it goes for approval", b.ID, strings.ToLower(b.Stage))
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	lk, err := newImportLookups(tenantID, schema, b.DoctypeName, role)
	if err != nil {
		return nil, err
	}
	if lk.isMasterErr != nil {
		return nil, lk.isMasterErr
	}
	fields, err := GetDocTypeMeta(tenantID, b.DoctypeName)
	if err != nil {
		return nil, err
	}
	if b.RulesVersion != ImportRulesVersion {
		if err := retransformImportBatch(schema, b); err != nil {
			return nil, err
		}
	}
	rows, err := allStagedRows(schema, b.ID)
	if err != nil {
		return nil, err
	}
	docRows := make([]map[string]interface{}, len(rows))
	for i := range rows {
		docRows[i] = rows[i].Data
	}

	type verdict struct {
		status, kind, matchID, key string
		errs                       []string
	}
	verdicts := make([]verdict, len(rows))
	firstOf := map[string]int{} // the first row with a key keeps it
	var keys []string
	for i := range rows {
		key := importUpdateTarget(docRows[i], lk.isMaster)
		verdicts[i].key = key
		if key == "" {
			continue
		}
		lower := strings.ToLower(key)
		if first, ok := firstOf[lower]; ok {
			verdicts[i].status = importRowDuplicate
			verdicts[i].errs = []string{fmt.Sprintf("repeats row %d of this file (%s)", rows[first].RowNo, key)}
			continue
		}
		firstOf[lower] = i
		keys = append(keys, key)
	}
	existing := map[string]string{}
	if len(keys) > 0 {
		q, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL AND id = ANY($2)`, schema), b.DoctypeName, pq.Array(keys))
		if err != nil {
			return nil, err
		}
		for q.Next() {
			var id string
			if q.Scan(&id) == nil {
				existing[strings.ToLower(id)] = id
			}
		}
		q.Close()
	}
	var pre []string
	if strings.EqualFold(b.DoctypeName, "Item") {
		pre = pimVariantParentPreflight(tenantID, docRows)
	}
	var eligible []int
	for i := range rows {
		v := &verdicts[i]
		if v.status == importRowDuplicate {
			v.kind = "duplicate"
			continue
		}
		v.kind = "new"
		if id, ok := existing[strings.ToLower(v.key)]; ok && v.key != "" {
			v.kind, v.matchID = "update", id
		}
		if pre != nil && pre[i] != "" {
			v.status, v.errs = importRowInvalid, []string{pre[i]}
			continue
		}
		eligible = append(eligible, i)
	}
	var warnings []string
	loadWarnings, loadErr := CheckImportLoadOrder(tenantID, b.DoctypeName)
	if loadErr != nil {
		return nil, loadErr
	}
	warnings = append(warnings, loadWarnings...)
	for start := 0; start < len(eligible); start += importStageChunk {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + importStageChunk
		if end > len(eligible) {
			end = len(eligible)
		}
		chunk := make([]map[string]interface{}, 0, end-start)
		for _, i := range eligible[start:end] {
			chunk = append(chunk, docRows[i])
		}
		res, err := runDocDataImportContext(ctx, tenantID, b.DoctypeName, userID, role, true, chunk, make([]string, len(chunk)))
		if err != nil {
			return nil, err
		}
		for _, e := range res.Errors {
			idx := e.RowNumber - 2
			if idx < 0 || idx >= len(chunk) {
				warnings = append(warnings, e.Message)
				continue
			}
			v := &verdicts[eligible[start+idx]]
			v.status = importRowInvalid
			v.errs = append(v.errs, e.Message)
		}
	}
	for i := range verdicts {
		if verdicts[i].status == "" {
			verdicts[i].status = importRowValid
		}
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	up, err := tx.Prepare(fmt.Sprintf(`UPDATE %s.import_staging_rows SET status = $3, match_kind = $4, match_id = $5, natural_key = $6, errors = $7 WHERE batch_id = $1 AND row_no = $2`, schema))
	if err != nil {
		return nil, err
	}
	defer up.Close()
	for i, v := range verdicts {
		var errJSON interface{}
		if len(v.errs) > 0 {
			j, _ := json.Marshal(v.errs)
			errJSON = string(j)
		}
		if _, err := up.Exec(b.ID, rows[i].RowNo, v.status, v.kind, v.matchID, importTruncate(v.key, 300), errJSON); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	counts, err := importRowCounts(schema, b.ID)
	if err != nil {
		return nil, err
	}
	b.Counts = counts
	b.FileTotals = importFileTotals(fields, docRows, func(i int) bool { return verdicts[i].status == importRowValid })
	b.Warnings = warnings
	b.Preview = nil
	b.Stage = ImportStageValidated
	b.RulesVersion = ImportRulesVersion
	if err := saveImportBatch(tenantID, b, ""); err != nil {
		return nil, err
	}
	return importBatchView(b), nil
}

func importTruncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// retransformImportBatch rebuilds every row's normalized cells from its raw
// cells under today's rules.
func retransformImportBatch(schema string, b *ImportBatchData) error {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT row_no, raw FROM %s.import_staging_rows WHERE batch_id = $1 ORDER BY row_no`, schema), b.ID)
	if err != nil {
		return err
	}
	type pair struct {
		no   int
		norm []byte
	}
	var out []pair
	for rows.Next() {
		var no int
		var raw []byte
		if err := rows.Scan(&no, &raw); err != nil {
			rows.Close()
			return err
		}
		cells := map[string]string{}
		_ = json.Unmarshal(raw, &cells)
		rec := make([]string, len(b.Headers))
		for i, h := range b.Headers {
			rec[i] = cells[h]
		}
		j, _ := json.Marshal(transformImportRow(b.Headers, rec))
		out = append(out, pair{no, j})
	}
	rows.Close()
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range out {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.import_staging_rows SET normalized = $3, transform_version = $4 WHERE batch_id = $1 AND row_no = $2`, schema),
			b.ID, p.no, p.norm, ImportRulesVersion); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- 3. impact preview -------------------------------------------------------

// PreviewImportBatch states what a commit would do before anything is written:
// how many records would be added and changed, which rows are repeats or
// errors, the Number columns' totals, and for the first changed records the
// fields that would change and from what.
func PreviewImportBatch(ctx context.Context, tenantID, batchID string) (*ImportBatchView, error) {
	b, _, err := loadImportBatch(tenantID, batchID)
	if err != nil {
		return nil, err
	}
	if !importBatchEditable(b) || b.Stage == ImportStageParsed {
		return nil, importBatchErr("DATAIM-0481", "batch %s must be checked first (it is %s)", b.ID, strings.ToLower(b.Stage))
	}
	if b.RulesVersion != ImportRulesVersion {
		return nil, importBatchErr("DATAIM-0482", "batch %s was checked under import rules %s; today's are %s - check it again", b.ID, b.RulesVersion, ImportRulesVersion)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := allStagedRows(schema, b.ID)
	if err != nil {
		return nil, err
	}
	p := &ImportBatchPreview{Counts: b.Counts, Totals: b.FileTotals, At: time.Now().UTC().Format(time.RFC3339)}
	var updIDs []string
	for _, r := range rows {
		if r.Status == importRowValid && r.MatchKind == "update" && len(updIDs) < importPreviewSamples {
			updIDs = append(updIDs, r.MatchID)
		}
	}
	stored := map[string]map[string]interface{}{}
	if len(updIDs) > 0 {
		q, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = $1 AND id = ANY($2)`, schema), b.DoctypeName, pq.Array(updIDs))
		if err != nil {
			return nil, err
		}
		for q.Next() {
			var id string
			var raw []byte
			if q.Scan(&id, &raw) == nil {
				m := map[string]interface{}{}
				_ = json.Unmarshal(raw, &m)
				stored[id] = m
			}
		}
		q.Close()
	}
	for _, r := range rows {
		switch {
		case r.Status == importRowValid && r.MatchKind == "new" && len(p.NewRows) < importPreviewSamples:
			p.NewRows = append(p.NewRows, ImportPreviewSample{Row: r.RowNo, ID: r.Key})
		case r.Status == importRowValid && r.MatchKind == "update" && len(p.Updates) < importPreviewSamples:
			p.Updates = append(p.Updates, ImportPreviewSample{Row: r.RowNo, ID: r.MatchID, Changes: importPreviewChanges(stored[r.MatchID], r.Data)})
		case r.Status == importRowDuplicate && len(p.Duplicates) < importPreviewSamples:
			p.Duplicates = append(p.Duplicates, ImportPreviewSample{Row: r.RowNo, ID: r.Key, Message: strings.Join(r.Errors, "; ")})
		case r.Status == importRowInvalid && len(p.Errors) < importPreviewSamples*2:
			p.Errors = append(p.Errors, ImportPreviewSample{Row: r.RowNo, ID: r.Key, Message: strings.Join(r.Errors, "; ")})
		}
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
// core, a chunk per transaction. A row is marked Committed in the same
// transaction that writes its record, so a repeat, a retry after an error and
// a resume after a crash write nothing twice: each takes only the rows still
// Valid. A committed batch asked again returns what it did. A chunk holding a
// row the importer now refuses (the data moved since the check) is retried a
// row at a time, so the rows that can go in do, and the others are marked
// Failed with the reason.
func CommitImportBatch(ctx context.Context, tenantID, batchID, actorID, actorRole string) (*ImportBatchView, error) {
	b, status, err := loadImportBatch(tenantID, batchID)
	if err != nil {
		return nil, err
	}
	if b.Stage == ImportStageCommitted || b.Stage == ImportStageReconciled {
		v := importBatchView(b)
		v.AlreadyCommitted = true
		return v, nil
	}
	if status != "Approved" {
		return nil, importBatchErr("DATAIM-0483", "batch %s is not approved (it is %s) - approve it before committing", b.ID, strings.ToLower(b.Stage))
	}
	if b.RulesVersion != ImportRulesVersion {
		return nil, importBatchErr("DATAIM-0482", "batch %s was checked under import rules %s; today's are %s - check it again", b.ID, b.RulesVersion, ImportRulesVersion)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	// One commit at a time: take the lease, or say another holds it.
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('commit_lease_until', $3::text, 'stage', 'committing')
		WHERE doctype = $1 AND id = $2 AND (COALESCE(data->>'commit_lease_until', '') = '' OR (data->>'commit_lease_until')::timestamptz < NOW())`, schema),
		ImportBatchDoctype, b.ID, time.Now().Add(importCommitLease).UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, importBatchErr("DATAIM-0484", "batch %s is being committed right now - wait for it to finish", b.ID)
	}
	releaseLease := func() {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || '{"commit_lease_until":""}'::jsonb WHERE doctype = $1 AND id = $2`, schema), ImportBatchDoctype, b.ID)
	}
	for chunk := 0; ; chunk++ {
		if err := ctx.Err(); err != nil {
			releaseLease()
			return nil, err
		}
		q, err := db.DB.Query(fmt.Sprintf(`SELECT `+stagedRowCols+` FROM %s.import_staging_rows WHERE batch_id = $1 AND status = 'Valid' ORDER BY row_no LIMIT $2`, schema), b.ID, importCommitChunk)
		if err != nil {
			releaseLease()
			return nil, err
		}
		rows, err := scanStagedRows(q)
		if err != nil {
			releaseLease()
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		if err := commitImportChunk(ctx, tenantID, schema, b, rows, actorID, actorRole); err != nil {
			releaseLease()
			return nil, err
		}
		if importCommitAfterChunk != nil {
			if err := importCommitAfterChunk(chunk); err != nil {
				releaseLease()
				return nil, err
			}
		}
	}
	b2, _, err := loadImportBatch(tenantID, b.ID)
	if err != nil {
		releaseLease()
		return nil, err
	}
	counts, err := importRowCounts(schema, b.ID)
	if err != nil {
		releaseLease()
		return nil, err
	}
	b2.Counts = counts
	b2.Stage = ImportStageCommitted
	b2.CommitLeaseUntil = ""
	b2.CommittedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveImportBatch(tenantID, b2, "Committed"); err != nil {
		releaseLease()
		return nil, err
	}
	LogAuditEvent(tenantID, actorID, "IMPORT_BATCH_COMMITTED", "Success", fmt.Sprintf("batch=%s doctype=%s committed=%d failed=%d", b.ID, b.DoctypeName, counts.Committed, counts.Failed))
	return ReconcileImportBatch(ctx, tenantID, b.ID, actorID)
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
// that does not stays Committed with the differences listed. It can be asked again.
func ReconcileImportBatch(ctx context.Context, tenantID, batchID, actorID string) (*ImportBatchView, error) {
	b, status, err := loadImportBatch(tenantID, batchID)
	if err != nil {
		return nil, err
	}
	if b.Stage != ImportStageCommitted && b.Stage != ImportStageReconciled {
		return nil, importBatchErr("DATAIM-0481", "batch %s has not been committed (it is %s)", b.ID, strings.ToLower(b.Stage))
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	fields, err := GetDocTypeMeta(tenantID, b.DoctypeName)
	if err != nil {
		return nil, err
	}
	rows, err := allStagedRows(schema, b.ID)
	if err != nil {
		return nil, err
	}
	rc := &ImportBatchReconcile{At: time.Now().UTC().Format(time.RFC3339), Created: b.CommitCreated, Updated: b.CommitUpdated, RowsInFile: len(rows)}
	docRows := make([]map[string]interface{}, len(rows))
	var committedKeys []string
	for i, r := range rows {
		docRows[i] = r.Data
		if r.Status == importRowCommitted {
			rc.Committed++
			committedKeys = append(committedKeys, importUpdateTarget(r.Data, false))
		} else {
			rc.Rejected++
		}
		if r.Status == importRowValid || r.Status == importRowStaged {
			rc.Issues = append(rc.Issues, fmt.Sprintf("row %d was never committed", r.RowNo))
		}
	}
	if rc.RowsInFile != b.Counts.Rows {
		rc.Issues = append(rc.Issues, fmt.Sprintf("the file had %d rows and %d are staged now", b.Counts.Rows, rc.RowsInFile))
	}
	if rc.Created+rc.Updated != rc.Committed {
		rc.Issues = append(rc.Issues, fmt.Sprintf("%d records written (%d new, %d changed) for %d rows committed", rc.Created+rc.Updated, rc.Created, rc.Updated, rc.Committed))
	}
	now := importFileTotals(fields, docRows, func(i int) bool { return rows[i].Status == importRowCommitted })
	rc.Columns = now
	for _, was := range b.FileTotals {
		// The upload total counted every row; Accepted is what validated.
		got := 0.0
		for _, n := range now {
			if n.Field == was.Field {
				got = n.Read
			}
		}
		if roundControl(got) != roundControl(was.Read) {
			rc.Issues = append(rc.Issues, fmt.Sprintf("%s totalled %v at upload and %v now", was.Label, was.Read, got))
		}
	}
	// Each committed row that names its record: that record is there.
	var named []string
	for _, k := range committedKeys {
		if k != "" {
			named = append(named, k)
		}
	}
	if len(named) > 0 {
		var n int
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(DISTINCT id) FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL AND id = ANY($2)`, schema),
			b.DoctypeName, pq.Array(named)).Scan(&n); err != nil {
			return nil, err
		}
		rc.KeysChecked = len(named)
		rc.KeysMissing = len(named) - n
		if rc.KeysMissing > 0 {
			rc.Issues = append(rc.Issues, fmt.Sprintf("%d committed record(s) are not in %s", rc.KeysMissing, b.DoctypeName))
		}
	}
	for _, d := range b.Declared {
		for _, n := range now {
			if n.Field == d.Field && roundControl(n.Accepted) != roundControl(d.Expected) {
				rc.Issues = append(rc.Issues, fmt.Sprintf("%s: stated %v, committed %v", firstNonEmpty(d.Label, d.Field), d.Expected, n.Accepted))
			}
		}
	}
	rc.Balanced = len(rc.Issues) == 0
	b.Reconcile = rc
	if err := recordStagedImportJob(tenantID, b, rc, actorID); err != nil {
		return nil, err
	}
	if rc.Balanced {
		b.Stage, status = ImportStageReconciled, "Reconciled"
	} else {
		b.Stage, status = ImportStageCommitted, "Committed"
	}
	if err := saveImportBatch(tenantID, b, status); err != nil {
		return nil, err
	}
	b.Status = status
	return importBatchView(b), nil
}

// DiscardImportBatch removes an uncommitted batch's staged rows and frees its
// file to be staged again. A committed or approved batch is not discarded.
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

// expireImportBatch removes a batch's staged rows (the file's contents) and
// marks it Expired. Its counts, totals, reconciliation and lineage stay.
func expireImportBatch(tenantID string, b *ImportBatchData, fromStatus string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.import_staging_rows WHERE batch_id = $1`, schema), b.ID); err != nil {
		return err
	}
	b.Stage = ImportStageExpired
	b.ExpiredAt = time.Now().UTC().Format(time.RFC3339)
	b.Warnings = append(b.Warnings, "expired from "+strings.ToLower(fromStatus))
	if err := saveImportBatchOn(tx, schema, b, "Expired"); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- 7. raw-file expiry ------------------------------------------------------------------

// sweepImportBatches expires batches created before cutoff that are not
// waiting on a person: abandoned drafts, rejected, committed and reconciled
// ones. A batch pending approval or approved is never swept.
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
		if err != nil {
			continue
		}
		if err := expireImportBatch(tenantID, b, p.status); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
