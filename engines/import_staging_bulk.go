package engines

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"custom_erp/db"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/lib/pq"
)

// 297.2: a staged import of any size that never fails part-way. Every step
// of a batch - reading the file, checking it, committing it, reconciling it -
// works a chunk at a time and saves how far it got in the same transaction as
// the chunk's rows, so the step carries on from there after a closed tab, a
// dropped line or a server restart, and no row is staged, checked or written
// twice.
//
// A small batch (importSyncRowsMax rows or fewer) runs its step inside the
// request, as before. A larger one runs as a background job in slices of
// about importSliceBudget: the job yields between slices so one import never
// holds a worker, and its progress shows in the user's tasks list, where they
// can leave and come back to it. A large file comes in through a resumable
// upload (import_upload_resumable.go) and is read straight from disk, never
// whole into memory.
//
// The batch record's "work" holds the running step (ImportBatchWork). Only the
// step that owns it - the job named by its job_id, or an "inline:" run - may
// write it, so a discarded batch or a second run stops the first at its next
// chunk instead of both writing.

const (
	importBatchJobType = "import_batch"
	importSyncRowsMax  = 2000             // rows a step runs inside the request
	importSliceBudget  = 20 * time.Second // one background slice
	importPurgeChunk   = 20000            // staged rows removed per statement
	importWorkListMax  = 100              // issues and warnings kept on a running step
	importInlineStale  = 2 * time.Minute  // a running step not heard from in this long has died
	importRowMaxBytes  = 8 << 20          // one row of a file; longer means an unclosed quote
	importReadBlock    = 4 << 20          // bytes per encoding scan / conversion read
	importAnalyzeFrom  = 20000            // rows a commit writes before it refreshes the table's figures, then each doubling
)

// Chunk sizes, and a test seam: importSliceChunks, when set, ends a
// background slice after that many chunks instead of on time, so tests slice
// small files the way a large one is sliced.
var (
	importStreamChunk    = 5000 // rows read from the file per transaction
	importReconcileChunk = 5000 // rows reconciled per pass
	importSliceChunks    = 0
)

// Import steps.
const (
	importStepStage     = "stage"
	importStepValidate  = "validate"
	importStepCommit    = "commit"
	importStepReconcile = "reconcile"
)

// ImportBatchWork is a step running in slices and how far it has got.
type ImportBatchWork struct {
	Step     string `json:"step"`
	JobID    string `json:"job_id"`
	UploadID string `json:"upload_id,omitempty"`
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	Store    string `json:"store,omitempty"`
	// Reading the file: its encoding ("" until scanned), how far the scan and
	// any conversion to UTF-8 got, and the byte offset reached in the text.
	Enc       string `json:"enc,omitempty"`
	ScanAt    int64  `json:"scan_at,omitempty"`
	ConvAt    int64  `json:"conv_at,omitempty"`
	OutAt     int64  `json:"out_at,omitempty"`
	Converted bool   `json:"converted,omitempty"`
	Offset    int64  `json:"offset,omitempty"`
	// Cursor is the last staged row number a step has finished.
	Cursor     int                   `json:"cursor,omitempty"`
	Done       int64                 `json:"done"`
	Total      int64                 `json:"total"`
	Acc        *importTotalsAcc      `json:"acc,omitempty"`
	Rec        *ImportBatchReconcile `json:"rec,omitempty"`
	IssuesMore int                   `json:"issues_more,omitempty"`
	Warnings   []string              `json:"warnings,omitempty"`
	Error      string                `json:"error,omitempty"`
	StartedAt  string                `json:"started_at,omitempty"`
	Beat       string                `json:"beat,omitempty"`
}

// errImportWorkGone: the batch's work is no longer this run's - it was
// discarded, expired or taken over - so this run stops without writing.
var errImportWorkGone = errors.New("import batch work moved on")

func importNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// ---- running totals ----------------------------------------------------------

// importTotalsAcc is importFileTotals kept as a running sum, so a batch read or
// checked in slices ends with the same totals, to the last digit, as one read
// whole: the columns are the doctype's Number fields in field order, each
// summed in row order, and only those with a value appear.
type importTotalsAcc struct {
	Cols []importTotalsCol `json:"cols"`
}

type importTotalsCol struct {
	Field    string  `json:"field"`
	Label    string  `json:"label"`
	Read     float64 `json:"read"`
	Accepted float64 `json:"accepted"`
	Seen     bool    `json:"seen,omitempty"`
}

func newImportTotalsAcc(fields []FieldMeta) *importTotalsAcc {
	a := &importTotalsAcc{Cols: []importTotalsCol{}}
	for _, f := range fields {
		if f.Fieldtype == "Number" {
			a.Cols = append(a.Cols, importTotalsCol{Field: f.Fieldname, Label: f.Label})
		}
	}
	return a
}

func (a *importTotalsAcc) add(row map[string]interface{}, accepted bool) {
	for i := range a.Cols {
		v, ok := importNumber(row[a.Cols[i].Field])
		if !ok {
			continue
		}
		a.Cols[i].Seen = true
		a.Cols[i].Read += v
		if accepted {
			a.Cols[i].Accepted += v
		}
	}
}

func (a *importTotalsAcc) totals() []ImportColumnTotal {
	out := make([]ImportColumnTotal, 0, len(a.Cols))
	for _, c := range a.Cols {
		if !c.Seen {
			continue
		}
		r, ac := roundControl(c.Read), roundControl(c.Accepted)
		out = append(out, ImportColumnTotal{Field: c.Field, Label: c.Label, Read: r, Accepted: ac, Rejected: roundControl(r - ac)})
	}
	return out
}

// ---- owning the work -----------------------------------------------------------

type importQuerier interface {
	importExec
	QueryRow(query string, args ...interface{}) *sql.Row
}

// importWorkLive is whether a step is still running the batch: an inline one
// heard from in the last importInlineStale, a job still queued, or a running
// one heard from (claimed, or a chunk done) as recently. A job that failed,
// was cancelled or dead-lettered is not, nor one whose server died mid-slice:
// its batch resumes, and a late slice of the dead run cannot save (saveWork).
func importWorkLive(schema string, w *ImportBatchWork) bool {
	if w == nil || w.JobID == "" {
		return false
	}
	if strings.HasPrefix(w.JobID, "inline:") {
		t, err := time.Parse(time.RFC3339Nano, w.Beat)
		return err == nil && time.Since(t) < importInlineStale
	}
	var status string
	var heard bool
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT status, COALESCE(updated_at > CURRENT_TIMESTAMP - make_interval(secs => $2), false)
		FROM %s.async_jobs WHERE id = $1`, schema), w.JobID, importInlineStale.Seconds()).Scan(&status, &heard)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		return true // unknown: do not start a second run
	}
	return status == "Pending" || (status == "Leased" && heard)
}

func importBusyErr(b *ImportBatchData) error {
	step := "a step"
	if b.Work != nil && b.Work.Step != "" {
		step = map[string]string{importStepStage: "reading the file", importStepValidate: "checking",
			importStepCommit: "committing", importStepReconcile: "reconciling"}[b.Work.Step]
	}
	return importBatchErr("DATAIM-0491", "batch %s is busy - %s is still running; its progress is in your tasks", b.ID, step)
}

// claimImportWork makes w the batch's work, unless a live step holds it. A
// dead step's commit lease is released with it. stage, when given, is set too.
func claimImportWork(q importExec, schema string, b *ImportBatchData, w *ImportBatchWork, stage string) error {
	prev := ""
	if b.Work != nil {
		if importWorkLive(schema, b.Work) {
			return importBusyErr(b)
		}
		prev = b.Work.JobID
	}
	w.Beat = importNow()
	wj, _ := json.Marshal(w)
	res, err := q.Exec(fmt.Sprintf(`UPDATE %s.documents SET updated_at = CURRENT_TIMESTAMP, data = data || jsonb_build_object('work', $3::jsonb)
		|| CASE WHEN $4 <> '' THEN jsonb_build_object('stage', $4::text) ELSE '{}'::jsonb END
		|| CASE WHEN $5 <> '' AND data->>'commit_lease_owner' = $5 THEN '{"commit_lease_until":"","commit_lease_owner":""}'::jsonb ELSE '{}'::jsonb END
		WHERE doctype = $1 AND id = $2 AND COALESCE(data->'work'->>'job_id', '') = $5`, schema),
		ImportBatchDoctype, b.ID, string(wj), stage, prev)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return importBusyErr(b)
	}
	if stage != "" {
		b.Stage = stage
	}
	b.Work = w
	return nil
}

// importStepRun is one run of a batch's work: inline (no deadline) or one
// background slice.
type importStepRun struct {
	ctx      context.Context
	tenantID string
	schema   string
	b        *ImportBatchData
	w        *ImportBatchWork
	deadline time.Time
	chunks   int // commit chunks this run, for the failure seam
	checks   int // slice-end checks this run, for importSliceChunks
}

func (r *importStepRun) outOfTime() bool {
	if r.deadline.IsZero() {
		return false
	}
	if importSliceChunks > 0 {
		r.checks++
		return r.checks > importSliceChunks
	}
	return time.Now().After(r.deadline)
}

// importCrashAt (313.3) is a test seam, nil in production. When set, a run
// calls it at each point a server could die: as a step starts ("start"),
// with a chunk read but not written ("chunk"), with a commit chunk's records
// written but its place not saved ("written"), with the chunk and its place
// saved ("saved"), and with the batch's ImportJob recorded but the reconcile
// not yet finished ("recorded"). An error ends the run there with nothing more
// written, as a crash would; import_crash_test.go lets the job's claim run
// out and checks the import still ends with the one-pass result.
var importCrashAt func(step, point string) error

func (r *importStepRun) crashPoint(point string) error {
	if importCrashAt == nil {
		return nil
	}
	return importCrashAt(r.w.Step, point)
}

func (r *importStepRun) progress() {
	if r.w.Total < r.w.Done {
		r.w.Total = r.w.Done
	}
	if !strings.HasPrefix(r.w.JobID, "inline:") {
		_ = ReportJobProgress(r.schema, r.w.JobID, r.w.Done, r.w.Total)
	}
}

// saveWork records how far the step got, in q's transaction; a run that no
// longer owns the work gets errImportWorkGone and its transaction is undone.
func (r *importStepRun) saveWork(q importExec) error {
	r.w.Beat = importNow()
	wj, _ := json.Marshal(r.w)
	res, err := q.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('work', $3::jsonb), updated_at = CURRENT_TIMESTAMP
		WHERE doctype = $1 AND id = $2 AND data->'work'->>'job_id' = $4`, r.schema), ImportBatchDoctype, r.b.ID, string(wj), r.w.JobID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errImportWorkGone
	}
	return nil
}

// yield saves where the step is and hands the worker back.
func (r *importStepRun) yield() error {
	if err := r.saveWork(db.DB); err != nil {
		return err
	}
	return ErrJobYield
}

// finish ends the step: the batch's fields, its status when given, and the
// next step (or none) as its work.
func (r *importStepRun) finish(q importExec, status string, next *ImportBatchWork) error {
	m := map[string]interface{}{}
	raw, _ := json.Marshal(r.b)
	_ = json.Unmarshal(raw, &m)
	delete(m, "status")
	if next != nil {
		next.Beat = importNow()
		m["work"] = next
	} else {
		m["work"] = nil
	}
	patch, _ := json.Marshal(m)
	res, err := q.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, status = COALESCE(NULLIF($2, ''), status), updated_at = CURRENT_TIMESTAMP
		WHERE doctype = $3 AND id = $4 AND data->'work'->>'job_id' = $5`, r.schema), string(patch), status, ImportBatchDoctype, r.b.ID, r.w.JobID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errImportWorkGone
	}
	if status != "" {
		r.b.Status = status
	}
	r.b.Work = next
	r.w = next
	return nil
}

// run works the step, and the one it leads to (reading leads to checking,
// committing to reconciling), until the work ends, the slice is up or it fails.
func (r *importStepRun) run() error {
	for r.w != nil {
		if err := r.crashPoint("start"); err != nil {
			return err
		}
		var err error
		switch r.w.Step {
		case importStepStage:
			err = r.stage()
		case importStepValidate:
			err = r.validate()
		case importStepCommit:
			err = r.commit()
		case importStepReconcile:
			err = r.reconcile()
		default:
			return JobStopError{fmt.Errorf("unknown import step %q", r.w.Step)}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// ---- the request side -------------------------------------------------------------

// importStepPrecheck is whether a batch may take a step; a commit of a batch
// already committed returns that batch instead.
func importStepPrecheck(b *ImportBatchData, status, step string) (*ImportBatchView, error) {
	switch step {
	case importStepValidate:
		if !importBatchEditable(b) {
			return nil, importBatchErr("DATAIM-0481", "batch %s is %s - it can only be checked before it goes for approval", b.ID, strings.ToLower(b.Stage))
		}
	case importStepCommit:
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
	case importStepReconcile:
		if b.Stage != ImportStageCommitted && b.Stage != ImportStageReconciled {
			return nil, importBatchErr("DATAIM-0481", "batch %s has not been committed (it is %s)", b.ID, strings.ToLower(b.Stage))
		}
	default:
		return nil, importBatchErr("DATAIM-0481", "%q is not an import step", step)
	}
	return nil, nil
}

func importStepStageName(step string) string {
	if step == importStepValidate {
		return ImportStageValidating
	}
	return ""
}

// runImportStepInline runs a step to its end inside the request.
func runImportStepInline(ctx context.Context, tenantID, batchID, step, userID, role string) (*ImportBatchView, error) {
	b, status, err := loadImportBatch(tenantID, batchID)
	if err != nil {
		return nil, err
	}
	if v, err := importStepPrecheck(b, status, step); v != nil || err != nil {
		return v, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var rb [8]byte
	_, _ = rand.Read(rb[:])
	w := &ImportBatchWork{Step: step, JobID: "inline:" + hex.EncodeToString(rb[:]), UserID: userID, Role: role,
		Store: SessionStoreFromContext(ctx), StartedAt: importNow()}
	fromStage := b.Stage
	if err := claimImportWork(db.DB, schema, b, w, importStepStageName(step)); err != nil {
		return nil, err
	}
	run := &importStepRun{ctx: ctx, tenantID: tenantID, schema: schema, b: b, w: w}
	if err := run.run(); err != nil {
		// Let go of the batch: the work, a commit lease this run took, and a
		// check's stage goes back to what it was.
		back := ""
		if step == importStepValidate {
			back = fromStage
		}
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || '{"work":null}'::jsonb
			|| CASE WHEN data->>'commit_lease_owner' = $3 THEN '{"commit_lease_until":"","commit_lease_owner":""}'::jsonb ELSE '{}'::jsonb END
			|| CASE WHEN $4 <> '' THEN jsonb_build_object('stage', $4::text) ELSE '{}'::jsonb END
			WHERE doctype = $1 AND id = $2 AND data->'work'->>'job_id' = $3`, schema), ImportBatchDoctype, b.ID, w.JobID, back)
		if errors.Is(err, errImportWorkGone) {
			return nil, importBatchErr("DATAIM-0491", "batch %s was changed while it was being worked on - open it again", b.ID)
		}
		return nil, err
	}
	return LoadImportBatch(tenantID, b.ID)
}

// RunImportBatchStep is how the screen asks for a step (validate, commit,
// reconcile): a small batch is done before the call returns; a large one is
// handed to a background job, and the batch comes back with its work set,
// for the screen to follow.
func RunImportBatchStep(ctx context.Context, tenantID, batchID, step, userID, role string) (*ImportBatchView, error) {
	b, status, err := loadImportBatch(tenantID, batchID)
	if err != nil {
		return nil, err
	}
	if v, err := importStepPrecheck(b, status, step); v != nil || err != nil {
		return v, err
	}
	rows := b.Counts.Rows
	if step == importStepCommit {
		rows = b.Counts.Valid
	}
	if rows <= importSyncRowsMax {
		return runImportStepInline(ctx, tenantID, batchID, step, userID, role)
	}
	w := &ImportBatchWork{Step: step, UserID: userID, Role: role, Store: SessionStoreFromContext(ctx), StartedAt: importNow()}
	if err := startImportJob(ctx, tenantID, b, w, importStepStageName(step)); err != nil {
		return nil, err
	}
	return LoadImportBatch(tenantID, b.ID)
}

// ResumeImportBatch carries on a step whose run stopped - a job that failed
// or was cancelled, a server that went down mid-way - from where it got to.
func ResumeImportBatch(ctx context.Context, tenantID, batchID, userID, role string) (*ImportBatchView, error) {
	b, _, err := loadImportBatch(tenantID, batchID)
	if err != nil {
		return nil, err
	}
	if b.Work == nil {
		return nil, importBatchErr("DATAIM-0481", "batch %s has no step to carry on - it is %s", b.ID, strings.ToLower(b.Stage))
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if importWorkLive(schema, b.Work) {
		return nil, importBusyErr(b)
	}
	if b.Work.Step == importStepStage {
		if _, err := completeUpload(schema, b.Work.UserID, b.Work.UploadID); err != nil {
			return nil, err
		}
	}
	w := *b.Work
	w.UserID, w.Role, w.Store, w.Error = userID, role, SessionStoreFromContext(ctx), ""
	if b.Work.Step == importStepStage {
		w.UserID = b.Work.UserID // the upload is its sender's
	}
	if err := startImportJob(ctx, tenantID, b, &w, ""); err != nil {
		return nil, err
	}
	return LoadImportBatch(tenantID, b.ID)
}

// startImportJob queues the background job that runs w and makes w the
// batch's work, in one transaction: there is never a job without its work or
// work without its job.
func startImportJob(ctx context.Context, tenantID string, b *ImportBatchData, w *ImportBatchWork, stage string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fmt.Sprintf(`SELECT 1 FROM %s.documents WHERE doctype = $1 AND id = $2 FOR UPDATE`, schema), ImportBatchDoctype, b.ID); err != nil {
		return err
	}
	jobID, err := enqueueImportJob(ctx, tx, schema, tenantID, b)
	if err != nil {
		return err
	}
	w.JobID = jobID
	if err := claimImportWork(tx, schema, b, w, stage); err != nil {
		return err
	}
	return tx.Commit()
}

func enqueueImportJob(ctx context.Context, q importQuerier, schema, tenantID string, b *ImportBatchData) (string, error) {
	jobID, err := enqueueJobWith(q, schema, importBatchJobType, map[string]interface{}{"tenant_id": tenantID, "batch_id": b.ID},
		"", SafeCorrelationID(CorrelationIDFromContext(ctx)), jobActorFor(ctx))
	if err != nil {
		return "", err
	}
	label := truncateRunes("Import · "+b.DoctypeName+" · "+b.FileName, 200)
	if _, err := q.Exec(fmt.Sprintf(`UPDATE %s.async_jobs SET label = $1 WHERE id = $2`, schema), label, jobID); err != nil {
		return "", err
	}
	return jobID, nil
}

// ---- a large file, from a resumable upload ----------------------------------------

// StageImportBatchFromUpload stages a file sent as a resumable upload. The
// header is checked now - its columns, the record type's mandatory ones -
// and the batch comes back at once; its rows are read from disk by a
// background job, then checked, which the batch and the user's tasks show.
// The same file for the same record type is never staged twice.
func StageImportBatchFromUpload(ctx context.Context, tenantID, doctype, uploadID, userID, role, declaredRaw string) (*ImportBatchView, error) {
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
	decl, err := ParseImportDeclaredTotals(declaredRaw)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	u, err := scanUploadRow(tx.QueryRow(fmt.Sprintf(`SELECT `+uploadCols+` FROM %s.resumable_uploads WHERE id = $1 AND owner = $2 FOR UPDATE`, schema), uploadID, userID))
	if err != nil {
		return nil, err
	}
	if u.Purpose != "import" {
		return nil, importBatchErr("DATAIM-0489", "that upload is not an import file")
	}
	// Used: its file was read in full, by the batch the same request made
	// when its answer was lost (found below); otherwise it must be complete.
	if u.Status != uploadComplete && u.Status != uploadUsed {
		return nil, importBatchErr("DATAIM-0489", "that upload is %s, not complete - %d of %d bytes are here", strings.ToLower(u.Status), u.Received, u.Size)
	}

	var existing string
	err = tx.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
		AND data->>'sha256' = $2 AND data->>'doctype_name' = $3 AND status NOT IN ('Rejected', 'Expired') ORDER BY created_at LIMIT 1`, schema),
		ImportBatchDoctype, u.SHA256, doctype).Scan(&existing)
	if err == nil {
		_ = tx.Rollback()
		b, _, lerr := loadImportBatch(tenantID, existing)
		if lerr != nil {
			return nil, lerr
		}
		// This upload's bytes are not needed - unless that batch is the one
		// still reading them.
		if u.Status == uploadComplete && (b.Work == nil || b.Work.UploadID != uploadID) {
			markUploadUsed(schema, uploadID)
		}
		v := importBatchView(b)
		v.AlreadyStaged = true
		v.WorkLive = importWorkLive(schema, b.Work) // still reading: the Tasks bar picks it up
		return v, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	if u.Status == uploadUsed {
		return nil, importBatchErr("DATAIM-0489", "that upload has been read already - choose the file again")
	}

	hd, err := readImportFileHead(resumableUploadPath(schema, uploadID), u.Size)
	if err != nil {
		return nil, err
	}
	if missing := missingMandatoryColumns(tenantID, doctype, hd.headers); len(missing) > 0 {
		return nil, importBatchErr("DATAIM-0164", "uploaded file is missing mandatory column(s): %s", strings.Join(missing, ", "))
	}

	id := NewDocIDCompact("IMPBAT")
	b := &ImportBatchData{
		ID: id, Code: id, DoctypeName: doctype, Stage: ImportStageStaging, Status: "Draft",
		FileName: u.FileName, SizeBytes: u.Size, SHA256: u.SHA256, Encoding: hd.enc,
		UploadedBy: userID, UploadedAt: time.Now().UTC().Format(time.RFC3339), Headers: hd.headers,
		RulesVersion: ImportRulesVersion, NaturalKey: ImportNaturalKeyRule, Declared: decl,
	}
	jobID, err := enqueueImportJob(ctx, tx, schema, tenantID, b)
	if err != nil {
		return nil, err
	}
	b.Work = &ImportBatchWork{Step: importStepStage, JobID: jobID, UploadID: uploadID, UserID: userID, Role: role,
		Store: SessionStoreFromContext(ctx), Enc: hd.enc, Total: hd.estRows, StartedAt: importNow(), Beat: importNow()}
	data, _ := json.Marshal(b)
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema),
		id, ImportBatchDoctype, data, userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// The read runs in the background from now on: the answer says so, so the
	// screen can show it in the Tasks bar.
	v := importBatchView(b)
	v.WorkLive = importWorkLive(schema, b.Work)
	return v, nil
}

type importFileHead struct {
	headers []string
	enc     string // "" when the rest of the file must be scanned to tell
	estRows int64
}

// readImportFileHead reads the first 64 KB of an uploaded file: its byte
// order mark, the upload rule on plain text, and the header row.
func readImportFileHead(path string, size int64) (*importFileHead, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head := int64(64 << 10)
	if size < head {
		head = size
	}
	raw := make([]byte, head)
	n, err := io.ReadFull(f, raw)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	raw = raw[:n]
	whole := int64(n) >= size
	hd := &importFileHead{}
	var text []byte
	switch {
	case len(raw) >= 2 && ((raw[0] == 0xFF && raw[1] == 0xFE) || (raw[0] == 0xFE && raw[1] == 0xFF)):
		if (size-2)%2 != 0 {
			return nil, importBatchErr("DATAIM-0486", "the file is UTF-16 but its length is odd - save it again from Excel as CSV UTF-8")
		}
		hd.enc = "utf-16le"
		if raw[0] == 0xFE {
			hd.enc = "utf-16be"
		}
		text, _ = decodeUTF16Block(raw[2:], hd.enc == "utf-16le", whole)
		if err := CheckUpload(UploadCSV, text); err != nil {
			return nil, err
		}
	case len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF:
		hd.enc, text = "utf-8-bom", raw[3:]
		if err := CheckUpload(UploadCSV, text); err != nil {
			return nil, err
		}
	default:
		if err := CheckUpload(UploadCSV, raw); err != nil {
			return nil, err
		}
		cut := len(raw)
		if !whole {
			cut = utf8CompleteLen(raw)
		}
		switch {
		case !utf8.Valid(raw[:cut]):
			hd.enc, text = "windows-1252", decode1252(raw)
		case whole:
			hd.enc, text = "utf-8", raw
		default:
			text = raw[:cut] // the rest is scanned in the background
		}
	}
	cr := csv.NewReader(bytes.NewReader(text))
	cr.FieldsPerRecord = -1
	rec, err := cr.Read()
	if err != nil || (!whole && cr.InputOffset() >= int64(len(text))) {
		return nil, importBatchErr("DATAIM-0486", "the header row could not be read - save the file again from Excel as CSV UTF-8")
	}
	hd.headers = make([]string, len(rec))
	for i, h := range rec {
		hd.headers[i] = strings.TrimSpace(strings.ToLower(h))
	}
	body := text[cr.InputOffset():]
	lines := int64(bytes.Count(body, []byte{'\n'}))
	if whole && len(bytes.TrimSpace(body)) > 0 && !bytes.HasSuffix(body, []byte{'\n'}) {
		lines++
	}
	if !whole && len(text) > 0 {
		lines = lines * size / int64(len(raw))
	}
	hd.estRows = max(lines, 1)
	return hd, nil
}

// utf8CompleteLen is b without a rune cut off at its end.
func utf8CompleteLen(b []byte) int {
	end := len(b)
	for i := 1; i <= 3 && i <= end; i++ {
		c := b[end-i]
		if c < 0x80 {
			break
		}
		if utf8.RuneStart(c) {
			if !utf8.FullRune(b[end-i:]) {
				end -= i
			}
			break
		}
	}
	return end
}

// decodeUTF16Block decodes UTF-16 bytes to UTF-8. Unless atEOF, a trailing odd
// byte and a trailing high surrogate are left for the next block; used is how
// many input bytes were decoded.
func decodeUTF16Block(b []byte, le, atEOF bool) (out []byte, used int) {
	used = len(b) &^ 1
	u := make([]uint16, used/2)
	for i := range u {
		if le {
			u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		} else {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		}
	}
	if !atEOF && len(u) > 0 && u[len(u)-1] >= 0xD800 && u[len(u)-1] < 0xDC00 {
		u = u[:len(u)-1]
		used -= 2
	}
	return []byte(string(utf16.Decode(u))), used
}

func decode1252(b []byte) []byte {
	out := make([]byte, 0, len(b)+len(b)/8)
	for _, c := range b {
		if c < 0x80 {
			out = append(out, c)
			continue
		}
		r, ok := cp1252High[c]
		if !ok {
			r = rune(c)
		}
		out = utf8.AppendRune(out, r)
	}
	return out
}

// ---- step: read the file ----------------------------------------------------------

func (r *importStepRun) stage() error {
	w := r.w
	if w.Acc == nil {
		fields, err := GetDocTypeMeta(r.tenantID, r.b.DoctypeName)
		if err != nil {
			return err
		}
		w.Acc = newImportTotalsAcc(fields)
	}
	src := resumableUploadPath(r.schema, w.UploadID)
	if w.Enc == "" {
		if err := r.scanEncoding(src); err != nil {
			return err
		}
	}
	path, base := src, int64(0)
	switch w.Enc {
	case "utf-8-bom":
		base = 3
	case "utf-16le", "utf-16be", "windows-1252":
		if !w.Converted {
			if err := r.convertToUTF8(src); err != nil {
				return err
			}
		}
		path = resumableUploadText(r.schema, w.UploadID)
	}
	return r.stageRows(path, base)
}

// scanEncoding reads the file through once to tell UTF-8 from Windows-1252
// (Excel's plain "CSV"): one byte that is not UTF-8 makes it 1252.
func (r *importStepRun) scanEncoding(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, importReadBlock)
	for {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		if r.outOfTime() {
			return r.yield()
		}
		n, rerr := f.ReadAt(buf, r.w.ScanAt)
		if rerr != nil && rerr != io.EOF {
			return rerr
		}
		atEOF := rerr == io.EOF
		end := n
		if !atEOF {
			end = utf8CompleteLen(buf[:n])
		}
		if !utf8.Valid(buf[:end]) {
			r.w.Enc = "windows-1252"
			return nil
		}
		r.w.ScanAt += int64(end)
		if n == 0 || (atEOF && end == n) {
			r.w.Enc = "utf-8"
			return nil
		}
	}
}

// convertToUTF8 writes a UTF-16 or Windows-1252 file out as UTF-8 beside it,
// resumably: a run carries on from the last saved point, cutting off anything
// written after it.
func (r *importStepRun) convertToUTF8(src string) error {
	w := r.w
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(resumableUploadText(r.schema, w.UploadID), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	if err := out.Truncate(w.OutAt); err != nil {
		return err
	}
	if _, err := out.Seek(w.OutAt, io.SeekStart); err != nil {
		return err
	}
	utf16File := strings.HasPrefix(w.Enc, "utf-16")
	if utf16File && w.ConvAt == 0 {
		w.ConvAt = 2 // after the byte order mark
	}
	buf := make([]byte, importReadBlock)
	for {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		if r.outOfTime() {
			if err := out.Sync(); err != nil {
				return err
			}
			return r.yield()
		}
		n, rerr := in.ReadAt(buf, w.ConvAt)
		if rerr != nil && rerr != io.EOF {
			return rerr
		}
		atEOF := rerr == io.EOF
		var conv []byte
		used := n
		if utf16File {
			conv, used = decodeUTF16Block(buf[:n], w.Enc == "utf-16le", atEOF)
			if atEOF && used != n {
				return JobStopError{importBatchErr("DATAIM-0486", "the file is UTF-16 but its length is odd - save it again from Excel as CSV UTF-8")}
			}
		} else {
			conv = decode1252(buf[:n])
		}
		if len(conv) > 0 {
			if _, err := out.Write(conv); err != nil {
				return err
			}
		}
		w.ConvAt += int64(used)
		w.OutAt += int64(len(conv))
		if n == 0 || (atEOF && used == n) {
			break
		}
	}
	if err := out.Sync(); err != nil {
		return err
	}
	w.Converted = true
	return nil
}

// importLineGuard stops the CSV reader running past importRowMaxBytes in one
// row, which is what an opening quote never closed would make it do.
type importLineGuard struct {
	r       io.Reader
	n       int64
	limit   int64
	tripped bool
}

var errImportRowTooLong = errors.New("row too long")

func (g *importLineGuard) Read(p []byte) (int, error) {
	if g.n >= g.limit {
		g.tripped = true
		return 0, errImportRowTooLong
	}
	if room := g.limit - g.n; int64(len(p)) > room {
		p = p[:room]
	}
	n, err := g.r.Read(p)
	g.n += int64(n)
	return n, err
}

// readImportLine is the physical line starting at or after blank lines from
// at; skip is the bytes to step over it, newline included.
func readImportLine(f *os.File, at int64) (line string, skip int64, err error) {
	buf := make([]byte, 64<<10)
	var acc []byte
	pos := at
	for int64(len(acc)) < importRowMaxBytes {
		n, rerr := f.ReadAt(buf, pos)
		if rerr != nil && rerr != io.EOF {
			return "", 0, rerr
		}
		acc = append(acc, buf[:n]...)
		pos += int64(n)
		trimmed := bytes.TrimLeft(acc, "\r\n")
		if i := bytes.IndexByte(trimmed, '\n'); i >= 0 {
			lead := len(acc) - len(trimmed)
			return strings.TrimRight(string(trimmed[:i]), "\r"), int64(lead + i + 1), nil
		}
		if rerr == io.EOF {
			return string(trimmed), int64(len(acc)), nil
		}
	}
	return "", 0, io.ErrShortBuffer
}

type importStagedIn struct {
	raw, norm string
	data      map[string]interface{}
	bad       string // why the row could not be read; "" when it was
}

// stageRows reads rows from the UTF-8 text, a chunk per transaction: the rows
// go in by COPY with the offset reached and the running totals, so a run that
// stops anywhere carries on with the next row and no row is staged twice. A
// row that cannot be read - a stray quote, the wrong number of cells - is
// staged as an error row and the file read on; only a header that changed or
// a quote never closed stops it.
func (r *importStepRun) stageRows(path string, base int64) error {
	w, b := r.w, r.b
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var textSize int64
	if st, err := f.Stat(); err == nil {
		textSize = st.Size() - base
	}
	maxRows := int64(GetSettingInt(r.tenantID, "platform.max_staged_import_rows"))
	var cr *csv.Reader
	var guard *importLineGuard
	var readerAt int64
	open := func(at int64) error {
		if _, err := f.Seek(base+at, io.SeekStart); err != nil {
			return err
		}
		guard = &importLineGuard{r: bufio.NewReaderSize(f, 1<<20)}
		cr = csv.NewReader(guard)
		cr.FieldsPerRecord = -1
		cr.ReuseRecord = true
		readerAt = at
		return nil
	}
	read := func() ([]string, error) {
		guard.limit = cr.InputOffset() + importRowMaxBytes
		return cr.Read()
	}
	pos := func() int64 { return readerAt + cr.InputOffset() }
	if err := open(w.Offset); err != nil {
		return err
	}
	headers := b.Headers
	if w.Offset == 0 {
		rec, err := read()
		same := err == nil && len(rec) == len(headers)
		for i := 0; same && i < len(rec); i++ {
			same = strings.TrimSpace(strings.ToLower(rec[i])) == headers[i]
		}
		if !same {
			return JobStopError{importBatchErr("DATAIM-0486", "the file's header row is not the one checked at upload - upload it again")}
		}
	}
	for eof := false; !eof; {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		if r.outOfTime() {
			return r.yield()
		}
		batch := make([]importStagedIn, 0, importStreamChunk)
		for len(batch) < importStreamChunk {
			start := pos()
			rec, err := read()
			if err == io.EOF {
				eof = true
				break
			}
			rowNo := w.Done + 2 + int64(len(batch))
			if err != nil {
				if guard.tripped {
					return JobStopError{importBatchErr("DATAIM-0486", "row %d is longer than %d MB - a quote is probably not closed; fix that row and upload the file again", rowNo, importRowMaxBytes>>20)}
				}
				var pe *csv.ParseError
				if !errors.As(err, &pe) {
					return err
				}
				line, skip, lerr := readImportLine(f, base+start)
				if lerr != nil {
					return JobStopError{importBatchErr("DATAIM-0486", "row %d could not be read - fix it and upload the file again", rowNo)}
				}
				batch = append(batch, importBadRow(map[string]string{"_line": truncateRunes(line, 2000)},
					fmt.Sprintf("row %d could not be read (%v): %s", rowNo, pe.Err, truncateRunes(line, 80))))
				if err := open(start + skip); err != nil {
					return err
				}
				continue
			}
			cells := rec
			for len(cells) > len(headers) && strings.TrimSpace(cells[len(cells)-1]) == "" {
				cells = cells[:len(cells)-1]
			}
			rawCells := make(map[string]string, len(headers))
			for c, v := range cells {
				if c < len(headers) {
					rawCells[headers[c]] = v
				}
			}
			if len(cells) != len(headers) {
				rawCells["_line"] = truncateRunes(strings.Join(cells, ","), 2000)
				batch = append(batch, importBadRow(rawCells, fmt.Sprintf("row %d has %d cells and the header has %d - it was left out", rowNo, len(cells), len(headers))))
				continue
			}
			norm := transformImportRow(headers, cells)
			rj, _ := json.Marshal(rawCells)
			nj, _ := json.Marshal(norm)
			batch = append(batch, importStagedIn{raw: string(rj), norm: string(nj), data: norm})
		}
		if len(batch) == 0 {
			break
		}
		if maxRows > 0 && w.Done+int64(len(batch)) > maxRows {
			return JobStopError{importBatchErr("DATAIM-0189", "the file has more than %d rows - split it into smaller files and import them separately", maxRows)}
		}
		w.Offset = pos()
		if err := r.crashPoint("chunk"); err != nil {
			return err
		}
		if err := r.copyStaged(batch); err != nil {
			return err
		}
		if err := r.crashPoint("saved"); err != nil {
			return err
		}
		// Rows not yet read are not counted, so the bar's total is the rows
		// so far scaled by the share of the file read.
		if w.Offset > 0 && textSize > w.Offset {
			w.Total = w.Done * textSize / w.Offset
		}
		r.progress()
	}
	// Let the file go first: finishing marks the upload used, which removes
	// its bytes, and Windows cannot remove a file that is still open.
	_ = f.Close()
	return r.finishStage()
}

func importBadRow(raw map[string]string, msg string) importStagedIn {
	rj, _ := json.Marshal(raw)
	return importStagedIn{raw: string(rj), norm: "{}", bad: msg}
}

// copyStaged writes one chunk of rows and the step's place in one transaction.
func (r *importStepRun) copyStaged(batch []importStagedIn) error {
	tx, err := db.DB.BeginTx(r.ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(pq.CopyInSchema(r.schema, "import_staging_rows", "batch_id", "row_no", "raw", "normalized", "transform_version", "status", "match_kind", "errors"))
	if err != nil {
		return err
	}
	for i, in := range batch {
		rowNo := r.w.Done + 2 + int64(i)
		var status, kind, errs interface{} = importRowStaged, nil, nil
		if in.bad != "" {
			ej, _ := json.Marshal([]string{in.bad})
			status, kind, errs = importRowInvalid, "parse", string(ej)
		}
		if _, err := stmt.Exec(r.b.ID, rowNo, in.raw, in.norm, ImportRulesVersion, status, kind, errs); err != nil {
			stmt.Close()
			return err
		}
	}
	if _, err := stmt.Exec(); err != nil {
		stmt.Close()
		return err
	}
	if err := stmt.Close(); err != nil {
		return err
	}
	for _, in := range batch {
		if in.data != nil {
			r.w.Acc.add(in.data, false)
		}
	}
	r.w.Done += int64(len(batch))
	if err := r.saveWork(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *importStepRun) finishStage() error {
	w, b := r.w, r.b
	if w.Done == 0 {
		return JobStopError{importBatchErr("DATAIM-0163", "CSV is empty or missing data rows")}
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`ANALYZE %s.import_staging_rows`, r.schema))
	b.Counts = ImportBatchCounts{Rows: int(w.Done)}
	b.FileTotals = w.Acc.totals()
	b.Encoding = w.Enc
	var warnings []string
	var kept []ImportDeclaredTotal
	for _, d := range b.Declared {
		found := false
		for _, t := range b.FileTotals {
			if t.Field == d.Field {
				d.Label, found = t.Label, true
			}
		}
		if found {
			kept = append(kept, d)
		} else {
			warnings = append(warnings, fmt.Sprintf("the total stated for %s was left out - it is not a number column of this file", d.Field))
		}
	}
	b.Declared = kept
	b.Stage = ImportStageValidating
	upload := w.UploadID
	next := &ImportBatchWork{Step: importStepValidate, JobID: w.JobID, UserID: w.UserID, Role: w.Role, Store: w.Store,
		Warnings: warnings, Total: w.Done, StartedAt: importNow()}
	if err := r.finish(db.DB, "", next); err != nil {
		return err
	}
	markUploadUsed(r.schema, upload)
	LogAuditEvent(r.tenantID, next.UserID, "IMPORT_BATCH_STAGED", "Success", fmt.Sprintf("batch=%s doctype=%s rows=%d sha256=%s", b.ID, b.DoctypeName, b.Counts.Rows, b.SHA256))
	return nil
}

// ---- step: check ---------------------------------------------------------------------

type importCheckRow struct {
	no                         int
	data                       map[string]interface{}
	norm                       string // set when the row was transformed again
	parse                      bool
	status, kind, matchID, key string
	errs                       []string
}

// validate checks the batch importStageChunk rows at a time, each chunk's
// verdicts written with the step's place: the versioned transform, repeats
// of a key within the file (against this chunk and every earlier row),
// records already there, and a dry run of the import core.
func (r *importStepRun) validate() error {
	w, b := r.w, r.b
	lk, err := newImportLookups(r.tenantID, r.schema, b.DoctypeName, w.Role)
	if err != nil {
		return err
	}
	if lk.isMasterErr != nil {
		return lk.isMasterErr
	}
	if w.Acc == nil {
		fields, err := GetDocTypeMeta(r.tenantID, b.DoctypeName)
		if err != nil {
			return err
		}
		w.Acc = newImportTotalsAcc(fields)
		if w.Total == 0 {
			w.Total = int64(b.Counts.Rows)
		}
	}
	for {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		if r.outOfTime() {
			return r.yield()
		}
		q, err := db.DB.QueryContext(r.ctx, fmt.Sprintf(`SELECT row_no, raw, normalized, transform_version, COALESCE(match_kind, '')
			FROM %s.import_staging_rows WHERE batch_id = $1 AND row_no > $2 ORDER BY row_no LIMIT $3`, r.schema), b.ID, w.Cursor, importStageChunk)
		if err != nil {
			return err
		}
		var rows []*importCheckRow
		for q.Next() {
			var raw, norm []byte
			var tv, kind string
			cr := &importCheckRow{}
			if err := q.Scan(&cr.no, &raw, &norm, &tv, &kind); err != nil {
				q.Close()
				return err
			}
			if kind == "parse" {
				cr.parse = true
			} else if tv != ImportRulesVersion {
				cells := map[string]string{}
				_ = json.Unmarshal(raw, &cells)
				rec := make([]string, len(b.Headers))
				for i, h := range b.Headers {
					rec[i] = cells[h]
				}
				cr.data = transformImportRow(b.Headers, rec)
				nj, _ := json.Marshal(cr.data)
				cr.norm = string(nj)
			} else {
				_ = json.Unmarshal(norm, &cr.data)
			}
			rows = append(rows, cr)
		}
		q.Close()
		if err := q.Err(); err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		if err := r.crashPoint("chunk"); err != nil {
			return err
		}
		if err := r.validateChunk(lk.isMaster, rows); err != nil {
			return err
		}
		if err := r.crashPoint("saved"); err != nil {
			return err
		}
		r.progress()
	}
	counts, err := importRowCounts(r.schema, b.ID)
	if err != nil {
		return err
	}
	loadWarnings, err := CheckImportLoadOrder(r.tenantID, b.DoctypeName)
	if err != nil {
		return err
	}
	b.Counts = counts
	b.FileTotals = w.Acc.totals()
	b.Warnings = append(loadWarnings, w.Warnings...)
	b.Preview = nil
	b.Stage = ImportStageValidated
	b.RulesVersion = ImportRulesVersion
	return r.finish(db.DB, "", nil)
}

func (r *importStepRun) validateChunk(isMaster bool, rows []*importCheckRow) error {
	w, b := r.w, r.b
	var live []*importCheckRow
	for _, cr := range rows {
		if !cr.parse {
			live = append(live, cr)
		}
	}
	// Repeats: within this chunk, then against every earlier row.
	firstOf := map[string]*importCheckRow{}
	var lookup []string
	for _, cr := range live {
		cr.key = importUpdateTarget(cr.data, isMaster)
		if cr.key == "" {
			continue
		}
		lower := strings.ToLower(importTruncate(cr.key, 300))
		if first, ok := firstOf[lower]; ok {
			cr.status = importRowDuplicate
			cr.errs = []string{fmt.Sprintf("repeats row %d of this file (%s)", first.no, cr.key)}
			continue
		}
		firstOf[lower] = cr
		lookup = append(lookup, importTruncate(cr.key, 300))
	}
	if len(lookup) > 0 && w.Cursor > 0 {
		q, err := db.DB.QueryContext(r.ctx, fmt.Sprintf(`SELECT k.x, MIN(s.row_no) FROM unnest($2::text[]) AS k(x)
			JOIN %s.import_staging_rows s ON s.batch_id = $1 AND lower(s.natural_key) = lower(k.x) AND s.row_no <= $3
			GROUP BY k.x`, r.schema), b.ID, pq.Array(lookup), w.Cursor)
		if err != nil {
			return err
		}
		for q.Next() {
			var k string
			var first int
			if err := q.Scan(&k, &first); err != nil {
				q.Close()
				return err
			}
			if cr := firstOf[strings.ToLower(k)]; cr != nil && cr.status == "" {
				cr.status = importRowDuplicate
				cr.errs = []string{fmt.Sprintf("repeats row %d of this file (%s)", first, cr.key)}
			}
		}
		q.Close()
		if err := q.Err(); err != nil {
			return err
		}
	}
	// Records already there.
	var keys []string
	for _, cr := range live {
		if cr.key != "" && cr.status == "" {
			keys = append(keys, cr.key)
		}
	}
	existing := map[string]string{}
	if len(keys) > 0 {
		docs, err := importDocsByID(r.ctx, db.DB, r.schema+".documents", keys, false, false) // 313.2
		if err != nil {
			return err
		}
		for id, d := range docs {
			if d.live(b.DoctypeName) {
				existing[strings.ToLower(id)] = id
			}
		}
	}
	// An item variant's parent: in the database, earlier in this chunk or
	// earlier in the file.
	var pre []string
	if strings.EqualFold(b.DoctypeName, "Item") {
		docRows := make([]map[string]interface{}, len(live))
		var parents []string
		seen := map[string]bool{}
		for i, cr := range live {
			docRows[i] = cr.data
			if p := strings.TrimSpace(pimString(cr.data["parent_product_code"])); p != "" && !seen[p] {
				seen[p] = true
				parents = append(parents, p)
			}
		}
		known := map[string]bool{}
		if len(parents) > 0 && w.Cursor > 0 {
			q, err := db.DB.QueryContext(r.ctx, fmt.Sprintf(`SELECT DISTINCT k.x FROM unnest($2::text[]) AS k(x)
				JOIN %s.import_staging_rows s ON s.batch_id = $1 AND lower(s.natural_key) = lower(k.x) AND s.row_no <= $3
				 AND btrim(s.normalized->>'id') = k.x`, r.schema), b.ID, pq.Array(parents), w.Cursor)
			if err != nil {
				return err
			}
			for q.Next() {
				var id string
				if q.Scan(&id) == nil {
					known[id] = true
				}
			}
			q.Close()
		}
		pre = pimVariantParentPreflightKnown(r.tenantID, docRows, known)
	}
	var eligible []*importCheckRow
	for i, cr := range live {
		if cr.status == importRowDuplicate {
			cr.kind = "duplicate"
			continue
		}
		cr.kind = "new"
		if id, ok := existing[strings.ToLower(cr.key)]; ok && cr.key != "" {
			cr.kind, cr.matchID = "update", id
		}
		if pre != nil && pre[i] != "" {
			cr.status, cr.errs = importRowInvalid, []string{pre[i]}
			continue
		}
		eligible = append(eligible, cr)
	}
	if len(eligible) > 0 {
		chunk := make([]map[string]interface{}, len(eligible))
		for i, cr := range eligible {
			chunk[i] = cr.data
		}
		res, err := runDocDataImportContext(r.ctx, r.tenantID, b.DoctypeName, w.UserID, w.Role, true, chunk, make([]string, len(chunk)))
		if err != nil {
			return err
		}
		for _, e := range res.Errors {
			idx := e.RowNumber - 2
			if idx < 0 || idx >= len(eligible) {
				if len(w.Warnings) < importWorkListMax {
					w.Warnings = append(w.Warnings, e.Message)
				}
				continue
			}
			eligible[idx].status = importRowInvalid
			eligible[idx].errs = append(eligible[idx].errs, e.Message)
		}
	}
	nos := make([]int64, 0, len(live))
	var sts, kinds, mids, nks, errs, norms []string
	for _, cr := range live {
		if cr.status == "" {
			cr.status = importRowValid
		}
		ej := ""
		if len(cr.errs) > 0 {
			j, _ := json.Marshal(cr.errs)
			ej = string(j)
		}
		nos = append(nos, int64(cr.no))
		sts = append(sts, cr.status)
		kinds = append(kinds, cr.kind)
		mids = append(mids, cr.matchID)
		nks = append(nks, importTruncate(cr.key, 300))
		errs = append(errs, ej)
		norms = append(norms, cr.norm)
		w.Acc.add(cr.data, cr.status == importRowValid)
	}
	w.Cursor = rows[len(rows)-1].no
	w.Done += int64(len(rows))
	tx, err := db.DB.BeginTx(r.ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(nos) > 0 {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.import_staging_rows s SET status = v.st, match_kind = v.kind, match_id = v.mid,
			natural_key = v.nk, errors = NULLIF(v.errs, '')::jsonb, normalized = COALESCE(NULLIF(v.norm, '')::jsonb, s.normalized), transform_version = $9
			FROM unnest($2::int[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[]) AS v(no, st, kind, mid, nk, errs, norm)
			WHERE s.batch_id = $1 AND s.row_no = v.no`, r.schema),
			b.ID, pq.Array(nos), pq.Array(sts), pq.Array(kinds), pq.Array(mids), pq.Array(nks), pq.Array(errs), pq.Array(norms), ImportRulesVersion); err != nil {
			return err
		}
	}
	if err := r.saveWork(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- step: commit ---------------------------------------------------------------------

// takeCommitLease: one commit of a batch at a time. A run takes the lease when
// it is free, run out, or already its own (a resumed slice).
func (r *importStepRun) takeCommitLease() error {
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('commit_lease_until', $3::text, 'commit_lease_owner', $4::text, 'stage', 'committing')
		WHERE doctype = $1 AND id = $2 AND (COALESCE(data->>'commit_lease_until', '') = '' OR (data->>'commit_lease_until')::timestamptz < NOW()
		  OR data->>'commit_lease_owner' = $4)`, r.schema),
		ImportBatchDoctype, r.b.ID, time.Now().Add(importCommitLease).UTC().Format(time.RFC3339), r.w.JobID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return importBatchErr("DATAIM-0484", "batch %s is being committed right now - wait for it to finish", r.b.ID)
	}
	return nil
}

// commit writes the valid rows in file order, importCommitChunk per
// transaction, each row marked Committed with its record (see
// CommitImportBatch); the walk's place is only a shortcut, as a row already
// committed is no longer Valid.
func (r *importStepRun) commit() error {
	w := r.w
	if w.Total == 0 {
		w.Total = int64(r.b.Counts.Valid)
	}
	if err := r.takeCommitLease(); err != nil {
		return err
	}
	leased := time.Now()
	for {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		if r.outOfTime() {
			return r.yield()
		}
		if time.Since(leased) > time.Minute {
			if err := r.takeCommitLease(); err != nil {
				return err
			}
			leased = time.Now()
		}
		q, err := db.DB.QueryContext(r.ctx, fmt.Sprintf(`SELECT `+stagedRowCols+` FROM %s.import_staging_rows
			WHERE batch_id = $1 AND status = 'Valid' AND row_no > $2 ORDER BY row_no LIMIT $3`, r.schema), r.b.ID, w.Cursor, importCommitChunk)
		if err != nil {
			return err
		}
		rows, err := scanStagedRows(q)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		if err := r.crashPoint("chunk"); err != nil {
			return err
		}
		if err := commitImportChunk(r.ctx, r.tenantID, r.schema, r.b, rows, w.UserID, w.Role); err != nil {
			return err
		}
		if err := r.crashPoint("written"); err != nil {
			return err
		}
		chunk := r.chunks
		r.chunks++
		w.Cursor = rows[len(rows)-1].RowNo
		before := w.Done
		w.Done += int64(len(rows))
		if err := r.saveWork(db.DB); err != nil {
			return err
		}
		if err := r.crashPoint("saved"); err != nil {
			return err
		}
		r.progress()
		if importAnalyzeMark(before) != importAnalyzeMark(w.Done) {
			r.refreshDocStats()
		}
		if importCommitAfterChunk != nil {
			if err := importCommitAfterChunk(chunk); err != nil {
				return err
			}
		}
	}
	b, _, err := loadImportBatch(r.tenantID, r.b.ID)
	if err != nil {
		return err
	}
	counts, err := importRowCounts(r.schema, b.ID)
	if err != nil {
		return err
	}
	b.Counts = counts
	b.Stage = ImportStageCommitted
	b.CommitLeaseUntil, b.CommitLeaseOwner = "", ""
	b.CommittedAt = time.Now().UTC().Format(time.RFC3339)
	r.b = b
	next := &ImportBatchWork{Step: importStepReconcile, JobID: w.JobID, UserID: w.UserID, Role: w.Role, Store: w.Store,
		Total: int64(counts.Rows), StartedAt: importNow()}
	if err := r.finish(db.DB, "Committed", next); err != nil {
		return err
	}
	LogAuditEvent(r.tenantID, w.UserID, "IMPORT_BATCH_COMMITTED", "Success", fmt.Sprintf("batch=%s doctype=%s committed=%d failed=%d", b.ID, b.DoctypeName, counts.Committed, counts.Failed))
	return nil
}

// importAnalyzeMark counts the refresh marks n written rows have passed:
// importAnalyzeFrom, then each doubling. A commit refreshes the figures when
// a chunk moves it to the next mark - about ten times for 1 crore rows, and
// a resumed commit carries on from the mark it had reached.
func importAnalyzeMark(n int64) int {
	m := 0
	for t := int64(importAnalyzeFrom); n >= t; t *= 2 {
		m++
	}
	return m
}

// refreshDocStats re-reads the documents table's figures (297.2.5). A commit
// adds one doctype's rows far faster than autovacuum's 10% mark notices on a
// big table. Until it does, the planner takes that doctype for nearly empty
// and finds each chunk's existing rows by reading every row of the doctype
// written so far: on the 1-crore run the commit fell from 2,900 rows a second
// to 230, and one lookup took 49 s. ANALYZE samples the table in a few
// seconds and blocks no reads or writes; it waits 5 s at most for its lock,
// and a failure is logged, never the import's.
func (r *importStepRun) refreshDocStats() {
	tx, err := db.DB.BeginTx(r.ctx, nil)
	if err == nil {
		defer tx.Rollback()
		if _, err = tx.Exec(`SET LOCAL lock_timeout = '5s'`); err == nil {
			if _, err = tx.Exec(fmt.Sprintf(`ANALYZE %s.documents`, r.schema)); err == nil {
				err = tx.Commit()
			}
		}
	}
	if err != nil && r.ctx.Err() == nil {
		log.Printf("[IMPORT] batch %s: table figures not refreshed at %d rows: %v", r.b.ID, r.w.Done, err)
	}
}

// ---- step: reconcile ---------------------------------------------------------------------

// reconcile reads the staged rows back importReconcileChunk at a time and ties
// the batch out (see ReconcileImportBatch); the issues read so far ride on
// the step, at most importWorkListMax of them listed.
func (r *importStepRun) reconcile() error {
	w, b := r.w, r.b
	if w.Rec == nil {
		fields, err := GetDocTypeMeta(r.tenantID, b.DoctypeName)
		if err != nil {
			return err
		}
		w.Rec, w.Acc = &ImportBatchReconcile{}, newImportTotalsAcc(fields)
		if w.Total == 0 {
			w.Total = int64(b.Counts.Rows)
		}
	}
	rc := w.Rec
	for {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		if r.outOfTime() {
			return r.yield()
		}
		q, err := db.DB.QueryContext(r.ctx, fmt.Sprintf(`SELECT `+stagedRowCols+` FROM %s.import_staging_rows
			WHERE batch_id = $1 AND row_no > $2 ORDER BY row_no LIMIT $3`, r.schema), b.ID, w.Cursor, importReconcileChunk)
		if err != nil {
			return err
		}
		rows, err := scanStagedRows(q)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		if err := r.crashPoint("chunk"); err != nil {
			return err
		}
		var named []string
		for _, row := range rows {
			rc.RowsInFile++
			committed := row.Status == importRowCommitted
			if committed {
				rc.Committed++
				if k := importUpdateTarget(row.Data, false); k != "" {
					named = append(named, k)
				}
			} else {
				rc.Rejected++
			}
			if row.Status == importRowValid || row.Status == importRowStaged {
				if len(rc.Issues) < importWorkListMax {
					rc.Issues = append(rc.Issues, fmt.Sprintf("row %d was never committed", row.RowNo))
				} else {
					w.IssuesMore++
				}
			}
			w.Acc.add(row.Data, committed)
		}
		if len(named) > 0 {
			docs, err := importDocsByID(r.ctx, db.DB, r.schema+".documents", named, false, false) // 313.2
			if err != nil {
				return err
			}
			n := 0
			for _, d := range docs {
				if d.live(b.DoctypeName) {
					n++
				}
			}
			rc.KeysChecked += len(named)
			rc.KeysMissing += len(named) - n
		}
		w.Cursor = rows[len(rows)-1].RowNo
		w.Done += int64(len(rows))
		if err := r.saveWork(db.DB); err != nil {
			return err
		}
		if err := r.crashPoint("saved"); err != nil {
			return err
		}
		r.progress()
	}
	rc.At = time.Now().UTC().Format(time.RFC3339)
	rc.Created, rc.Updated = b.CommitCreated, b.CommitUpdated
	if w.IssuesMore > 0 {
		rc.Issues = append(rc.Issues, fmt.Sprintf("and %d more rows were never committed", w.IssuesMore))
	}
	if rc.RowsInFile != b.Counts.Rows {
		rc.Issues = append(rc.Issues, fmt.Sprintf("the file had %d rows and %d are staged now", b.Counts.Rows, rc.RowsInFile))
	}
	if rc.Created+rc.Updated != rc.Committed {
		rc.Issues = append(rc.Issues, fmt.Sprintf("%d records written (%d new, %d changed) for %d rows committed", rc.Created+rc.Updated, rc.Created, rc.Updated, rc.Committed))
	}
	now := w.Acc.totals()
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
	if rc.KeysMissing > 0 {
		rc.Issues = append(rc.Issues, fmt.Sprintf("%d committed record(s) are not in %s", rc.KeysMissing, b.DoctypeName))
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
	if b.ImportJobID == "" {
		// A run that stopped after recording the import job finds it again.
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'ImportJob' AND data->>'batch_id' = $1 LIMIT 1`, r.schema), b.ID).Scan(&b.ImportJobID)
	}
	if err := recordStagedImportJob(r.tenantID, b, rc, w.UserID); err != nil {
		return err
	}
	if err := r.crashPoint("recorded"); err != nil {
		return err
	}
	status := "Committed"
	b.Stage = ImportStageCommitted
	if rc.Balanced {
		b.Stage, status = ImportStageReconciled, "Reconciled"
	}
	return r.finish(db.DB, status, nil)
}

// ---- the background job ------------------------------------------------------------------

func init() {
	RegisterJobHandler(importBatchJobType, runImportBatchJob)
	RegisterUserTask(importBatchJobType, "Import")
	// A slice is importSliceBudget. The bound is also how long a crashed
	// server's claim holds an import before the next worker carries it on by
	// itself, so it stays short.
	SetJobMaxRuntime(importBatchJobType, 5*time.Minute)
}

// runImportBatchJob runs one slice of a batch's work. A job that is no longer
// the batch's (discarded, taken over) ends quietly; a problem in the file or
// the batch ends it with the reason on the batch and in the user's tasks; a
// passing fault (the database, a restart) is retried and carries on.
func runImportBatchJob(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
	tenantID, _ := job.Payload["tenant_id"].(string)
	batchID, _ := job.Payload["batch_id"].(string)
	if purge, _ := job.Payload["purge"].(bool); purge {
		more, err := purgeImportBatchRows(ctx, schema, batchID, importSliceBudget)
		if err != nil {
			return nil, err
		}
		if more {
			return nil, ErrJobYield
		}
		return map[string]interface{}{"batch_id": batchID, "purged": true}, nil
	}
	stale := map[string]interface{}{"batch_id": batchID, "stale": true}
	b, _, err := loadImportBatch(tenantID, batchID)
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			return stale, nil
		}
		return nil, err
	}
	if b.Work == nil || b.Work.JobID != job.ID {
		return stale, nil
	}
	run := &importStepRun{ctx: WithSessionStore(ctx, b.Work.Store), tenantID: tenantID, schema: schema, b: b, w: b.Work,
		deadline: time.Now().Add(importSliceBudget)}
	err = run.run()
	switch {
	case err == nil:
		return map[string]interface{}{"batch_id": batchID, "stage": run.b.Stage}, nil
	case errors.Is(err, ErrJobYield):
		return nil, ErrJobYield
	case errors.Is(err, errImportWorkGone):
		return stale, nil
	}
	var ve *ValidationError
	var stop JobStopError
	if errors.As(err, &ve) || errors.As(err, &stop) {
		// Nothing a retry can change: say why on the batch, let go of a
		// commit lease, and end the job. The batch can be resumed or discarded.
		if w, _, lerr := loadImportBatch(tenantID, batchID); lerr == nil && w.Work != nil && w.Work.JobID == job.ID {
			w.Work.Error = truncateRunes(err.Error(), 500)
			wj, _ := json.Marshal(w.Work)
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('work', $3::jsonb)
				|| CASE WHEN data->>'commit_lease_owner' = $4 THEN '{"commit_lease_until":"","commit_lease_owner":""}'::jsonb ELSE '{}'::jsonb END
				WHERE doctype = $1 AND id = $2 AND data->'work'->>'job_id' = $4`, schema), ImportBatchDoctype, batchID, string(wj), job.ID)
		}
		if !errors.As(err, &stop) {
			return nil, JobStopError{err}
		}
		return nil, err
	}
	return nil, err
}

// ---- removing staged rows ------------------------------------------------------------------

// purgeImportBatchRows removes a batch's staged rows importPurgeChunk at a
// time for up to budget; more says some are left.
func purgeImportBatchRows(ctx context.Context, schema, batchID string, budget time.Duration) (more bool, err error) {
	deadline := time.Now().Add(budget)
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		res, err := db.DB.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %[1]s.import_staging_rows WHERE batch_id = $1 AND row_no IN
			(SELECT row_no FROM %[1]s.import_staging_rows WHERE batch_id = $1 ORDER BY row_no LIMIT $2)`, schema), batchID, importPurgeChunk)
		if err != nil {
			return false, err
		}
		if n, _ := res.RowsAffected(); n < importPurgeChunk {
			return false, nil
		}
		if time.Now().After(deadline) {
			return true, nil
		}
	}
}

// enqueueImportPurge hands what is left of an expired batch's rows to a
// background job, unless one is already queued.
func enqueueImportPurge(tenantID, schema, batchID string) error {
	var one int
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT 1 FROM %s.async_jobs WHERE job_type = $1 AND status IN ('Pending', 'Leased')
		AND payload->>'batch_id' = $2 AND payload->>'purge' = 'true' LIMIT 1`, schema), importBatchJobType, batchID).Scan(&one)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = enqueueJobWith(db.DB, schema, importBatchJobType, map[string]interface{}{"tenant_id": tenantID, "batch_id": batchID, "purge": true},
		"", "", "system:import-purge")
	return err
}

// SweepImportStaging is the daily tidy of large imports: uploads left for
// over a day lose their bytes, and an expired batch with staged rows still
// there has them removed in the background.
func SweepImportStaging(tenantID string) (int64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	n, err := sweepResumableUploads(schema, time.Now())
	if err != nil {
		return n, err
	}
	var tbl sql.NullString
	if err := db.DB.QueryRow(`SELECT to_regclass($1)::text`, schema+".import_staging_rows").Scan(&tbl); err != nil || !tbl.Valid {
		return n, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT d.id FROM %[1]s.documents d WHERE d.doctype = $1 AND d.status = 'Expired'
		AND EXISTS (SELECT 1 FROM %[1]s.import_staging_rows s WHERE s.batch_id = d.id) LIMIT 50`, schema), ImportBatchDoctype)
	if err != nil {
		return n, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if err := enqueueImportPurge(tenantID, schema, id); err != nil {
			return n, err
		}
	}
	return n, nil
}
