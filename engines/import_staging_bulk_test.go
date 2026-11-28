package engines

import (
	"bytes"
	"context"
	"custom_erp/db"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 297.2 - a large import runs as a background job in slices. These tests cut
// every slice to one chunk (importSliceChunks) so a small file is sliced the
// way a 1-crore-row one is, stop the job part-way as a failed or dead job
// would, resume it, and check the result is the one-pass result: every row
// staged exactly once, the same checks, the same totals, every valid row
// written exactly once.

func bulkSlices(t *testing.T, stream, reconcile int) {
	t.Helper()
	prevStream, prevRec, prevSlice := importStreamChunk, importReconcileChunk, importSliceChunks
	importStreamChunk, importReconcileChunk, importSliceChunks = stream, reconcile, 1
	t.Cleanup(func() { importStreamChunk, importReconcileChunk, importSliceChunks = prevStream, prevRec, prevSlice })
}

// bulkCSV is n rows; every 97th has a qty that is not a number and the last
// repeats the first row's id.
func bulkCSV(n int) []byte {
	var b strings.Builder
	b.WriteString("id,cart_number,sku,qty,sale_value,status\n")
	for i := 0; i < n; i++ {
		qty := fmt.Sprint(i%7 + 1)
		if i%97 == 50 {
			qty = "abc"
		}
		fmt.Fprintf(&b, "B-%05d,C%d,SKU%d,%s,%d.25,Open\n", i, i%13, i, qty, i%500)
	}
	b.WriteString("B-00000,C0,SKU0,1,1,Open\n")
	return []byte(b.String())
}

// sliceImport runs the batch's job one slice at a time, up to n slices or
// until its work ends, and returns the batch.
func sliceImport(t *testing.T, tenantID, schema, batchID string, n int) *ImportBatchView {
	t.Helper()
	for i := 0; i < n; i++ {
		v, err := LoadImportBatch(tenantID, batchID)
		if err != nil {
			t.Fatal(err)
		}
		if v.Work == nil {
			return v
		}
		if !v.WorkLive {
			t.Fatalf("the step stopped by itself: %+v", v.Work)
		}
		runClaimedJobs(schema)
	}
	v, err := LoadImportBatch(tenantID, batchID)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// jobNote is how the batch's job stands, for a failure message.
func jobNote(schema string, w *ImportBatchWork) string {
	if w == nil {
		return "no work"
	}
	var status, lastErr string
	var attempts int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status, attempts, COALESCE(last_error, '') FROM %s.async_jobs WHERE id = $1`, schema), w.JobID).Scan(&status, &attempts, &lastErr); err != nil {
		return "job: " + err.Error()
	}
	return fmt.Sprintf("job %s, %d attempts, last error %q", status, attempts, lastErr)
}

// killImportJob ends the batch's job as one that failed for good would.
func killImportJob(t *testing.T, schema, jobID string) {
	t.Helper()
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.async_jobs SET status = 'DeadLettered', completed_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), jobID); err != nil {
		t.Fatal(err)
	}
}

type stagedRowState struct {
	No            int
	Status, Match string
}

func stagedRowStates(t *testing.T, schema, batchID string) []stagedRowState {
	t.Helper()
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT row_no, status, COALESCE(match_kind, '') FROM %s.import_staging_rows WHERE batch_id = $1 ORDER BY row_no`, schema), batchID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []stagedRowState
	for rows.Next() {
		var s stagedRowState
		if err := rows.Scan(&s.No, &s.Status, &s.Match); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestImportBulkSlicesSurviveAStopAndMatchOnePass(t *testing.T) {
	const n = 2300
	file := bulkCSV(n)
	ctx := context.Background()

	// One pass: the small-file path, every step inside the call.
	oneTenant, oneSchema := stagingTenant(t)
	one, err := StageImportBatch(ctx, oneTenant, stagingDoctype, bytes.NewReader(file), "bulk.csv", "stg_maker", RoleSuperAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if one, err = ValidateImportBatch(ctx, oneTenant, one.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	if one.Counts.Rows != n+1 || one.Counts.Invalid == 0 || one.Counts.Duplicate != 1 || one.Counts.Valid <= importSyncRowsMax {
		t.Fatalf("one-pass counts %+v: the file must have invalid rows, one duplicate and more valid rows than run inline", one.Counts)
	}

	// Sliced: a resumable upload, read, checked and committed by a job one
	// chunk per slice.
	tenantID, schema := stagingTenant(t)
	uploadTestDir(t)
	bulkSlices(t, 500, 700)
	upID := sendUpload(t, tenantID, "stg_maker", "bulk.csv", file, 16<<10)
	v, err := StageImportBatchFromUpload(ctx, tenantID, stagingDoctype, upID, "stg_maker", RoleSuperAdmin, "")
	if err != nil {
		t.Fatalf("stage from upload: %v", err)
	}
	if v.Stage != ImportStageStaging || v.Work == nil || v.Work.Step != importStepStage || strings.HasPrefix(v.Work.JobID, "inline:") {
		t.Fatalf("staged %s / %+v; want staging with a background job reading it", v.Stage, v.Work)
	}
	var label string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT label FROM %s.async_jobs WHERE id = $1`, schema), v.Work.JobID).Scan(&label); err != nil || label != "Import · "+stagingDoctype+" · bulk.csv" {
		t.Fatalf("job label %q (%v): the user's tasks name the import by it", label, err)
	}
	// The same upload staged again is the batch already made.
	if again, err := StageImportBatchFromUpload(ctx, tenantID, stagingDoctype, upID, "stg_maker", RoleSuperAdmin, ""); err != nil || !again.AlreadyStaged || again.ID != v.ID {
		t.Fatalf("staging the same upload twice: %v / %+v", err, again)
	}

	// One slice scans the encoding, the next two read a chunk each; then the
	// job dies.
	mid := sliceImport(t, tenantID, schema, v.ID, 3)
	if mid.Work == nil || mid.Work.Enc != "utf-8" || mid.Work.Done != 1000 || mid.Work.Total < mid.Work.Done || mid.Work.Offset == 0 {
		t.Fatalf("after three slices: %+v (%s); want UTF-8, 1000 rows read and an estimated total", mid.Work, jobNote(schema, mid.Work))
	}
	if got := len(stagedRowStates(t, schema, v.ID)); got != 1000 {
		t.Fatalf("%d rows staged after three slices, want 1000", got)
	}
	killImportJob(t, schema, mid.Work.JobID)
	dead, _ := LoadImportBatch(tenantID, v.ID)
	if dead.WorkLive {
		t.Fatal("a dead job's batch shows as still running")
	}
	if _, err := RunImportBatchStep(ctx, tenantID, v.ID, importStepValidate, "stg_maker", RoleSuperAdmin); err == nil {
		t.Fatal("a batch whose file is half read was checked")
	}
	res, err := ResumeImportBatch(ctx, tenantID, v.ID, "stg_checker", RoleSuperAdmin)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !res.WorkLive || res.Work.JobID == mid.Work.JobID || res.Work.Done != 1000 || res.Work.UserID != "stg_maker" {
		t.Fatalf("resumed %+v; want a new live job carrying on from row 1000 as the upload's sender", res.Work)
	}
	if _, err := ResumeImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); stagingCode(err) != "DATAIM-0491" {
		t.Fatalf("resume while running: %v, want DATAIM-0491", err)
	}

	// Reading leads to checking, in the same job.
	checked := sliceImport(t, tenantID, schema, v.ID, 40)
	if checked.Work != nil || checked.Stage != ImportStageValidated {
		t.Fatalf("after the job: %s / %+v; want validated, no work", checked.Stage, checked.Work)
	}
	if !reflect.DeepEqual(checked.Counts, one.Counts) {
		t.Fatalf("sliced counts %+v, one pass %+v", checked.Counts, one.Counts)
	}
	if !reflect.DeepEqual(checked.FileTotals, one.FileTotals) {
		t.Fatalf("sliced totals %+v, one pass %+v", checked.FileTotals, one.FileTotals)
	}
	sliced, whole := stagedRowStates(t, schema, v.ID), stagedRowStates(t, oneSchema, one.ID)
	if len(sliced) != n+1 || !reflect.DeepEqual(sliced, whole) {
		t.Fatalf("staged rows differ: %d sliced, %d one pass", len(sliced), len(whole))
	}
	if sliced[0].No != 2 || sliced[len(sliced)-1].No != n+2 {
		t.Fatalf("row numbers %d..%d, want 2..%d (the header is row 1)", sliced[0].No, sliced[len(sliced)-1].No, n+2)
	}
	var upStatus string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.resumable_uploads WHERE id = $1`, schema), upID).Scan(&upStatus)
	if upStatus != uploadUsed {
		t.Fatalf("upload is %s after its file was read, want Used", upStatus)
	}
	// Used means its bytes are gone: the read lets the file go before the
	// upload is marked (Windows keeps a file that is still open).
	if _, err := os.Stat(resumableUploadPath(schema, upID)); !os.IsNotExist(err) {
		t.Fatalf("upload %s is Used but its file is still on disk (stat: %v)", upID, err)
	}
	// The create sent again even now (its answer lost): the batch it made.
	if again, err := StageImportBatchFromUpload(ctx, tenantID, stagingDoctype, upID, "stg_maker", RoleSuperAdmin, ""); err != nil || !again.AlreadyStaged || again.ID != v.ID {
		t.Fatalf("staging a read upload again: %v / %+v", err, again)
	}

	// Commit in slices, stopped part-way and resumed: every valid row once.
	if _, err := PreviewImportBatch(ctx, tenantID, v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ApproveImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	c, err := RunImportBatchStep(ctx, tenantID, v.ID, importStepCommit, "stg_maker", RoleSuperAdmin)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if c.Work == nil || c.Work.Step != importStepCommit || !c.WorkLive {
		t.Fatalf("commit of %d rows did not go to a job: %+v", c.Counts.Valid, c.Work)
	}
	if _, err := RunImportBatchStep(ctx, tenantID, v.ID, importStepCommit, "stg_maker", RoleSuperAdmin); stagingCode(err) != "DATAIM-0491" {
		t.Fatalf("a second commit while one runs: %v, want DATAIM-0491", err)
	}
	cm := sliceImport(t, tenantID, schema, v.ID, 3)
	if cm.Work == nil || cm.Work.Done != 3*importCommitChunk {
		t.Fatalf("after three commit slices: %+v; want %d done", cm.Work, 3*importCommitChunk)
	}
	if got := countDocs(t, schema); got != 3*importCommitChunk {
		t.Fatalf("%d records after three slices, want %d", got, 3*importCommitChunk)
	}
	killImportJob(t, schema, cm.Work.JobID)
	if _, err := ResumeImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatalf("resume the commit: %v", err)
	}
	done := sliceImport(t, tenantID, schema, v.ID, 40)
	if done.Work != nil || done.Stage != ImportStageReconciled || done.Reconcile == nil || !done.Reconcile.Balanced {
		t.Fatalf("after the commit job: %s / work %+v / reconcile %+v", done.Stage, done.Work, done.Reconcile)
	}
	if got := countDocs(t, schema); got != one.Counts.Valid || done.CommitCreated != one.Counts.Valid {
		t.Fatalf("%d records, %d created; want every valid row (%d) exactly once", got, done.CommitCreated, one.Counts.Valid)
	}
}

// A batch discarded while its file is being read: the job stops at its next
// chunk, writes nothing more, and the upload is let go.
func TestImportBulkDiscardStopsTheJob(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	uploadTestDir(t)
	bulkSlices(t, 300, 300)
	ctx := context.Background()
	upID := sendUpload(t, tenantID, "stg_maker", "discard.csv", bulkCSV(1200), 64<<10)
	v, err := StageImportBatchFromUpload(ctx, tenantID, stagingDoctype, upID, "stg_maker", RoleSuperAdmin, "")
	if err != nil {
		t.Fatal(err)
	}
	jobID := v.Work.JobID
	sliceImport(t, tenantID, schema, v.ID, 1)
	if _, err := DiscardImportBatch(tenantID, v.ID, "stg_maker"); err != nil {
		t.Fatalf("discard while reading: %v", err)
	}
	for i := 0; i < 3; i++ {
		runClaimedJobs(schema)
	}
	var status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.async_jobs WHERE id = $1`, schema), jobID).Scan(&status); err != nil || status != "Succeeded" {
		t.Fatalf("the discarded batch's job is %s (%v), want ended", status, err)
	}
	if got := len(stagedRowStates(t, schema, v.ID)); got != 0 {
		t.Fatalf("%d staged rows left after a discard", got)
	}
	gone, _ := LoadImportBatch(tenantID, v.ID)
	if gone.Status != "Expired" || gone.Work != nil {
		t.Fatalf("discarded batch: %s / %+v", gone.Status, gone.Work)
	}
	if _, err := ResumeImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); stagingCode(err) != "DATAIM-0481" {
		t.Fatalf("resuming a discarded batch: %v, want DATAIM-0481", err)
	}
}

// Whether a step is still running: an inline step heard from lately, or a job
// still queued or running. Anything else has stopped and can be resumed.
func TestImportWorkLive(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	_ = tenantID
	fresh := time.Now().UTC().Format(time.RFC3339Nano)
	stale := time.Now().Add(-importInlineStale - time.Second).UTC().Format(time.RFC3339Nano)
	if !importWorkLive(schema, &ImportBatchWork{JobID: "inline:ab", Beat: fresh}) {
		t.Fatal("an inline step heard from now is not live")
	}
	if importWorkLive(schema, &ImportBatchWork{JobID: "inline:ab", Beat: stale}) {
		t.Fatal("an inline step not heard from in over two minutes is live")
	}
	if importWorkLive(schema, nil) || importWorkLive(schema, &ImportBatchWork{}) {
		t.Fatal("no work is live")
	}
	if importWorkLive(schema, &ImportBatchWork{JobID: "no-such-job"}) {
		t.Fatal("a job that does not exist is live")
	}
	jobID, err := enqueueJobWith(db.DB, schema, importBatchJobType, map[string]interface{}{"tenant_id": tenantID, "batch_id": "x"}, "", "", "stg_maker")
	if err != nil {
		t.Fatal(err)
	}
	for status, live := range map[string]bool{"Pending": true, "Leased": true, "Failed": false, "DeadLettered": false, "Cancelled": false, "Succeeded": false} {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.async_jobs SET status = $2 WHERE id = $1`, schema), jobID, status); err != nil {
			t.Fatal(err)
		}
		if got := importWorkLive(schema, &ImportBatchWork{JobID: jobID}); got != live {
			t.Fatalf("job %s: live %v, want %v", status, got, live)
		}
	}
	// Claimed by a server that then died: not heard from since, it is not
	// live, so the batch can be resumed without waiting out the lease.
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.async_jobs SET status = 'Leased', leased_until = CURRENT_TIMESTAMP + interval '5 minutes',
		updated_at = CURRENT_TIMESTAMP - interval '3 minutes' WHERE id = $1`, schema), jobID); err != nil {
		t.Fatal(err)
	}
	if importWorkLive(schema, &ImportBatchWork{JobID: jobID}) {
		t.Fatal("a job claimed 3 minutes ago and not heard from since is live")
	}
}

// A server that dies mid-slice leaves its job claimed. Not heard from, the
// batch offers Resume at once; left alone, the next worker carries the job on
// when the claim runs out. Either way every row is read exactly once.
// 297.2.5: the 1-crore commit slowed from 2,900 rows a second to 230 because
// the table's figures still called the new doctype empty. A commit refreshes
// them at 20,000 written rows and each doubling, about ten times for 1 crore.
func TestImportCommitRefreshesTableFigures(t *testing.T) {
	for _, c := range []struct {
		n    int64
		want int
	}{{0, 0}, {19999, 0}, {20000, 1}, {39999, 1}, {40000, 2}, {10000000, 9}} {
		if got := importAnalyzeMark(c.n); got != c.want {
			t.Errorf("importAnalyzeMark(%d) = %d, want %d", c.n, got, c.want)
		}
	}
	refreshes := 0
	for done := int64(0); done < 10000000; done += importCommitChunk {
		if importAnalyzeMark(done) != importAnalyzeMark(done+importCommitChunk) {
			refreshes++
		}
	}
	if refreshes != 9 {
		t.Fatalf("a 1-crore commit refreshes the figures %d times, want 9", refreshes)
	}

	tenantID, schema := stagingTenant(t)
	var before sql.NullTime
	if err := db.DB.QueryRow(`SELECT pg_stat_get_last_analyze_time($1::regclass)`, schema+".documents").Scan(&before); err != nil {
		t.Fatal(err)
	}
	r := &importStepRun{ctx: context.Background(), tenantID: tenantID, schema: schema,
		b: &ImportBatchData{ID: "IMPBAT-analyze-test"}, w: &ImportBatchWork{Done: importAnalyzeFrom}}
	r.refreshDocStats()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var after sql.NullTime
		if err := db.DB.QueryRow(`SELECT pg_stat_get_last_analyze_time($1::regclass)`, schema+".documents").Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after.Valid && (!before.Valid || after.Time.After(before.Time)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("refreshDocStats did not analyze %s.documents (last analyze %v)", schema, after)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestImportBulkServerDiesMidSlice(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	uploadTestDir(t)
	bulkSlices(t, 300, 300)
	ctx := context.Background()
	const n = 1200
	upID := sendUpload(t, tenantID, "stg_maker", "crash.csv", bulkCSV(n), 64<<10)
	v, err := StageImportBatchFromUpload(ctx, tenantID, stagingDoctype, upID, "stg_maker", RoleSuperAdmin, "")
	if err != nil {
		t.Fatal(err)
	}
	firstJob := v.Work.JobID
	setJob := func(set string) {
		t.Helper()
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.async_jobs SET `+set+` WHERE id = $1`, schema), firstJob); err != nil {
			t.Fatal(err)
		}
	}
	dies := func() {
		setJob(`status = 'Leased', leased_until = CURRENT_TIMESTAMP + interval '5 minutes', updated_at = CURRENT_TIMESTAMP - interval '3 minutes'`)
	}
	load := func() *ImportBatchView {
		t.Helper()
		b, err := LoadImportBatch(tenantID, v.ID)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	// One chunk (after the encoding scan, unless the upload settled it), and
	// the server dies holding the job.
	mid := sliceImport(t, tenantID, schema, v.ID, 1)
	if mid.Work != nil && mid.Work.Done == 0 {
		mid = sliceImport(t, tenantID, schema, v.ID, 1)
	}
	if mid.Work == nil || mid.Work.Done != 300 {
		t.Fatalf("before the crash: %+v (%s); want 300 rows read", mid.Work, jobNote(schema, mid.Work))
	}
	dies()
	if b := load(); b.WorkLive {
		t.Fatalf("a batch whose server died is shown running: %+v", b.Work)
	}
	// Left alone: nothing runs while the claim holds, then the next worker
	// carries on from row 300.
	runClaimedJobs(schema)
	if b := load(); b.Work == nil || b.Work.Done != 300 {
		t.Fatalf("a claimed job was run again: %+v", b.Work)
	}
	setJob(`leased_until = CURRENT_TIMESTAMP - interval '1 second'`)
	runClaimedJobs(schema)
	if b := load(); !b.WorkLive || b.Work.JobID != firstJob || b.Work.Done != 600 {
		t.Fatalf("after the claim ran out: %+v (%s); want the same job carrying on to 600", b.Work, jobNote(schema, b.Work))
	}

	// It dies again, and this time the user resumes it before the claim runs out.
	dies()
	res, err := ResumeImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin)
	if err != nil || res.Work == nil || res.Work.JobID == firstJob || res.Work.Done != 600 {
		t.Fatalf("resume after the crash: %v / %+v", err, res)
	}
	// The dead server's claim runs out: its job finds the batch taken over and ends.
	setJob(`leased_until = CURRENT_TIMESTAMP - interval '1 second'`)
	end := sliceImport(t, tenantID, schema, v.ID, 50)
	if end.Stage != ImportStageValidated || end.Work != nil {
		t.Fatalf("after the resume: stage %s, work %+v; want validated", end.Stage, end.Work)
	}
	var status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.async_jobs WHERE id = $1`, schema), firstJob).Scan(&status); err != nil || status != "Succeeded" {
		t.Fatalf("the dead server's job is %s (%v), want ended", status, err)
	}
	rows := stagedRowStates(t, schema, v.ID)
	if len(rows) != n+1 || end.Counts.Rows != n+1 {
		t.Fatalf("%d rows staged, counted %d; want %d, each once", len(rows), end.Counts.Rows, n+1)
	}
	for i, r := range rows {
		if r.No != i+2 {
			t.Fatalf("row %d is numbered %d: a row was read twice or skipped", i, r.No)
		}
	}
}

// TestImportBulkScale times a large import end to end at the real slice
// length: upload, read, check, commit, reconcile. It runs only when
// IMPORT_SCALE_ROWS is set (100000 is a lakh), so the release does not pay for
// it. It logs each step's time and rows a second, the longest slice and the
// largest heap seen, the evidence that a big file is worked in bounded pieces.
func TestImportBulkScale(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("IMPORT_SCALE_ROWS"))
	if n <= 0 {
		t.Skip("set IMPORT_SCALE_ROWS (e.g. 100000) to run")
	}
	tenantID, schema := stagingTenant(t)
	uploadTestDir(t)
	ctx := context.Background()
	var peak uint64
	var longest time.Duration
	slices := 0
	took := map[string]time.Duration{} // per step: stage, validate, commit, reconcile
	drive := func(step, id string) time.Duration {
		t.Helper()
		start, last, still := time.Now(), int64(-1), 0
		for {
			b, err := LoadImportBatch(tenantID, id)
			if err != nil {
				t.Fatal(err)
			}
			if b.Work == nil {
				return time.Since(start)
			}
			if !b.WorkLive || b.Work.Error != "" {
				t.Fatalf("%s stopped: %+v (%s)", step, b.Work, jobNote(schema, b.Work))
			}
			if b.Work.Done == last {
				if still++; still > 20 {
					t.Fatalf("%s is not moving: %+v (%s)", step, b.Work, jobNote(schema, b.Work))
				}
			} else {
				last, still = b.Work.Done, 0
			}
			s := time.Now()
			runClaimedJobs(schema)
			slices++
			d := time.Since(s)
			took[b.Work.Step] += d
			if d > longest {
				longest = d
			}
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			if m.HeapInuse > peak {
				peak = m.HeapInuse
			}
		}
	}
	rate := func(rows int, d time.Duration) string {
		return fmt.Sprintf("%s (%.0f rows/s)", d.Round(time.Millisecond), float64(rows)/d.Seconds())
	}

	file := bulkCSV(n)
	t0 := time.Now()
	upID := sendUpload(t, tenantID, "stg_maker", "scale.csv", file, ResumableUploadPieceMax)
	tUp := time.Since(t0)
	t0 = time.Now()
	v, err := StageImportBatchFromUpload(ctx, tenantID, stagingDoctype, upID, "stg_maker", RoleSuperAdmin, "")
	if err != nil {
		t.Fatal(err)
	}
	tCreate := time.Since(t0)
	tRead := drive("read and check", v.ID)
	b, err := LoadImportBatch(tenantID, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Stage != ImportStageValidated || b.Counts.Rows != n+1 {
		t.Fatalf("after reading: stage %s, %d rows; want validated, %d", b.Stage, b.Counts.Rows, n+1)
	}
	if _, err := PreviewImportBatch(ctx, tenantID, v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ApproveImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := RunImportBatchStep(ctx, tenantID, v.ID, importStepCommit, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	tCommit := drive("commit and reconcile", v.ID)
	done, err := LoadImportBatch(tenantID, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Stage != ImportStageReconciled || done.Reconcile == nil || !done.Reconcile.Balanced || done.CommitCreated != b.Counts.Valid {
		t.Fatalf("after the commit: %s, reconcile %+v, %d created of %d valid", done.Stage, done.Reconcile, done.CommitCreated, b.Counts.Valid)
	}
	if got := countDocs(t, schema); got != b.Counts.Valid {
		t.Fatalf("%d records, want %d", got, b.Counts.Valid)
	}
	t.Logf("%d rows, %.1f MB: upload %s; create %s; read+check %s; commit+reconcile %s (%d valid)",
		n+1, float64(len(file))/(1<<20), tUp.Round(time.Millisecond), tCreate.Round(time.Millisecond),
		rate(n+1, tRead), rate(b.Counts.Valid, tCommit), b.Counts.Valid)
	t.Logf("by step (slices started in it): read %s, check %s, commit %s, reconcile %s", took[importStepStage].Round(time.Millisecond),
		took[importStepValidate].Round(time.Millisecond), took[importStepCommit].Round(time.Millisecond), took[importStepReconcile].Round(time.Millisecond))
	t.Logf("%d slices, longest %s (budget %s); largest heap %.1f MB", slices, longest.Round(time.Millisecond), importSliceBudget, float64(peak)/(1<<20))
}
