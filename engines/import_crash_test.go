package engines

import (
	"context"
	"custom_erp/db"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 313.3 - the crash matrix. A server can die at any point of an import job
// (importCrashAt names the points). Each test leaves a dead job the way a
// crash does - still claimed, nothing more written - lets the claim run out,
// and the next worker carries the job on. The import must end as a run with
// no crash ends: the same rows staged and checked, the same totals, every
// valid row written once, one ImportJob, and a reconcile that balances.

var errImportCrash = errors.New("313.3: the server died here")

// importCrashPoints is every step/point importCrashAt is called at.
var importCrashPoints = []string{
	"stage/start", "stage/chunk", "stage/saved",
	"validate/start", "validate/chunk", "validate/saved",
	"commit/start", "commit/chunk", "commit/written", "commit/saved",
	"reconcile/start", "reconcile/chunk", "reconcile/saved", "reconcile/recorded",
}

// crashImportRows has more valid rows than importSyncRowsMax, so the commit
// runs as a job as well as the read.
const crashImportRows = 2300

// crashRun is one file taken from upload to reconciled through the job runner.
type crashRun struct {
	t                        *testing.T
	tenantID, schema, prefix string
	died                     atomic.Bool
	deaths                   int
}

// csv is bulkCSV with the run's own id prefix, so several imports of the same
// rows share one client.
func (c *crashRun) csv() []byte {
	return []byte(strings.ReplaceAll(string(bulkCSV(crashImportRows)), "\nB-", "\n"+c.prefix+"-"))
}

// upload sends the file and stages it from the upload, which hands the read
// to a job.
func (c *crashRun) upload() string {
	c.t.Helper()
	upID := sendUpload(c.t, c.tenantID, "stg_maker", c.prefix+".csv", c.csv(), 16<<10)
	v, err := StageImportBatchFromUpload(context.Background(), c.tenantID, stagingDoctype, upID, "stg_maker", RoleSuperAdmin, "")
	if err != nil {
		c.t.Fatalf("%s: stage: %v", c.prefix, err)
	}
	if v.Work == nil || strings.HasPrefix(v.Work.JobID, "inline:") {
		c.t.Fatalf("%s: the read did not go to a job: %+v", c.prefix, v.Work)
	}
	return v.ID
}

func (c *crashRun) load(batchID string) *ImportBatchView {
	c.t.Helper()
	v, err := LoadImportBatch(c.tenantID, batchID)
	if err != nil {
		c.t.Fatal(err)
	}
	return v
}

// drive runs the batch's job a slice at a time until its work ends. When the
// server died in a slice, the job is left as a crash leaves it (still
// claimed, the attempt not counted) with its claim run out, so the next
// slice is the next worker carrying it on.
func (c *crashRun) drive(batchID string) *ImportBatchView {
	c.t.Helper()
	for i := 0; i < 500; i++ {
		v := c.load(batchID)
		if v.Work == nil {
			return v
		}
		if v.Work.Error != "" {
			c.t.Fatalf("%s: the step stopped: %+v (%s)", c.prefix, v.Work, jobNote(c.schema, v.Work))
		}
		c.died.Store(false)
		runClaimedJobs(c.schema)
		if c.died.Load() {
			c.deaths++
			if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.async_jobs SET status = 'Leased', leased_until = CURRENT_TIMESTAMP - interval '1 second',
				attempts = GREATEST(attempts - 1, 0), last_error = '', completed_at = NULL WHERE id = $1`, c.schema), v.Work.JobID); err != nil {
				c.t.Fatal(err)
			}
		}
	}
	c.t.Fatalf("%s: the import did not finish (%d deaths)", c.prefix, c.deaths)
	return nil
}

// commit previews and approves the checked batch and starts the commit,
// which runs as a job.
func (c *crashRun) commit(batchID string) *ImportBatchView {
	c.t.Helper()
	ctx := context.Background()
	if _, err := PreviewImportBatch(ctx, c.tenantID, batchID); err != nil {
		c.t.Fatalf("%s: preview: %v", c.prefix, err)
	}
	if _, err := ApproveImportBatch(ctx, c.tenantID, batchID, "stg_maker", RoleSuperAdmin); err != nil {
		c.t.Fatalf("%s: approve: %v", c.prefix, err)
	}
	v, err := RunImportBatchStep(ctx, c.tenantID, batchID, importStepCommit, "stg_maker", RoleSuperAdmin)
	if err != nil {
		c.t.Fatalf("%s: commit: %v", c.prefix, err)
	}
	if v.Work == nil || strings.HasPrefix(v.Work.JobID, "inline:") {
		c.t.Fatalf("%s: the commit of %d rows did not go to a job: %+v", c.prefix, v.Counts.Valid, v.Work)
	}
	return v
}

// all takes the file through every step. crash is asked at each point the
// server could die ("step/point"); yes ends the slice there.
func (c *crashRun) all(crash func(at string) bool) importCrashResult {
	c.t.Helper()
	importCrashAt = func(step, point string) error {
		if crash != nil && crash(step+"/"+point) {
			c.died.Store(true)
			return errImportCrash
		}
		return nil
	}
	defer func() { importCrashAt = nil }()
	id := c.upload()
	if v := c.drive(id); v.Stage != ImportStageValidated {
		c.t.Fatalf("%s: after the read and check: %s", c.prefix, v.Stage)
	}
	c.commit(id)
	return c.outcome(c.drive(id))
}

// importCrashResult is what an import ended with, the same for every run of
// the file whatever its prefix.
type importCrashResult struct {
	Counts           ImportBatchCounts
	FileTotals       []ImportColumnTotal
	Reconcile        ImportBatchReconcile
	Created, Updated int
	Rows             []stagedRowState
	Docs, ImportJobs int
	Qty, Sale        string
}

func (c *crashRun) outcome(v *ImportBatchView) importCrashResult {
	c.t.Helper()
	if v.Stage != ImportStageReconciled || v.Reconcile == nil || !v.Reconcile.Balanced {
		c.t.Fatalf("%s ended %s, reconcile %+v", c.prefix, v.Stage, v.Reconcile)
	}
	rec := *v.Reconcile
	rec.At = ""
	out := importCrashResult{Counts: v.Counts, FileTotals: v.FileTotals, Reconcile: rec,
		Created: v.CommitCreated, Updated: v.CommitUpdated, Rows: stagedRowStates(c.t, c.schema, v.ID)}
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM((data->>'qty')::numeric), 0)::text,
		COALESCE(SUM((data->>'sale_value')::numeric), 0)::text FROM %s.documents
		WHERE doctype = $1 AND id LIKE $2 AND deleted_at IS NULL`, c.schema), stagingDoctype, c.prefix+"-%").Scan(&out.Docs, &out.Qty, &out.Sale); err != nil {
		c.t.Fatal(err)
	}
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'ImportJob' AND data->>'batch_id' = $1`, c.schema), v.ID).Scan(&out.ImportJobs); err != nil {
		c.t.Fatal(err)
	}
	return out
}

func sameCrashResult(t *testing.T, name string, got, want importCrashResult) {
	t.Helper()
	if !reflect.DeepEqual(got.Rows, want.Rows) {
		t.Fatalf("%s: %d staged rows differ from the run with no crash (%d)", name, len(got.Rows), len(want.Rows))
	}
	got.Rows, want.Rows = nil, nil
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s ended\n  %+v\nthe run with no crash ended\n  %+v", name, got, want)
	}
}

// TestImportCrashMatrix (313.3): the server dies once at every point, then at
// random points, many times in one import; each ends as the run with no crash.
// IMPORT_CRASH_SEED replays a random run; every run logs its seed.
func TestImportCrashMatrix(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	uploadTestDir(t)
	bulkSlices(t, 500, 700)
	run := func(prefix string) *crashRun {
		return &crashRun{t: t, tenantID: tenantID, schema: schema, prefix: prefix}
	}

	ref := run("REF").all(nil)
	if ref.Docs != ref.Counts.Committed || ref.Created != ref.Counts.Committed || ref.Updated != 0 || ref.ImportJobs != 1 ||
		ref.Counts.Invalid == 0 || ref.Counts.Duplicate != 1 || ref.Counts.Failed != 0 || ref.Counts.Committed <= importSyncRowsMax {
		t.Fatalf("the run with no crash: %+v", ref)
	}

	// Once at every point: the first time each is reached.
	seen := map[string]bool{}
	every := run("EVERY")
	sameCrashResult(t, "dying once at every point", every.all(func(at string) bool {
		if seen[at] {
			return false
		}
		seen[at] = true
		return true
	}), ref)
	var missed []string
	for _, p := range importCrashPoints {
		if !seen[p] {
			missed = append(missed, p)
		}
	}
	if len(missed) > 0 || every.deaths != len(importCrashPoints) {
		var got []string
		for p := range seen {
			got = append(got, p)
		}
		sort.Strings(got)
		t.Fatalf("died %d times at %v; never reached %v", every.deaths, got, missed)
	}

	// At random points, up to 40 times.
	seed := time.Now().UnixNano()
	if s, err := strconv.ParseInt(os.Getenv("IMPORT_CRASH_SEED"), 10, 64); err == nil {
		seed = s
	}
	t.Logf("random crashes: IMPORT_CRASH_SEED=%d replays this run", seed)
	rng := rand.New(rand.NewSource(seed))
	random := run("RAND")
	deaths := 0
	got := random.all(func(string) bool {
		if deaths < 40 && rng.Float64() < 0.2 {
			deaths++
			return true
		}
		return false
	})
	sameCrashResult(t, fmt.Sprintf("dying at random (seed %d, %d deaths)", seed, random.deaths), got, ref)
	t.Logf("random run died %d times and matched", random.deaths)
}

// TestImportStalledWorkerWakesToNothing (313.3): a worker that stalls rather
// than dies - a long pause, a frozen machine - with a commit chunk read but not
// written. Its claim runs out, the next worker finishes the import, and then
// the first wakes and writes its chunk. Its rows are no longer Valid, so the
// write is undone, and its place is no longer its to save: nothing changes.
func TestImportStalledWorkerWakesToNothing(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	uploadTestDir(t)
	bulkSlices(t, 500, 700)
	c := &crashRun{t: t, tenantID: tenantID, schema: schema, prefix: "STALL"}
	id := c.upload()
	if v := c.drive(id); v.Stage != ImportStageValidated {
		t.Fatalf("after the read and check: %s", v.Stage)
	}
	jobID := c.commit(id).Work.JobID

	parked, wake := make(chan struct{}), make(chan struct{})
	var chunks atomic.Int32
	var woke, wroteAfterWaking atomic.Bool
	importCrashAt = func(step, point string) error {
		if step == importStepCommit && point == "chunk" && chunks.Add(1) == 3 {
			close(parked)
			<-wake
			woke.Store(true)
		}
		if woke.Load() && point == "written" {
			wroteAfterWaking.Store(true)
		}
		return nil
	}
	t.Cleanup(func() { importCrashAt = nil })

	runClaimedJobs(schema)
	runClaimedJobs(schema)
	stalled := make(chan struct{})
	go func() {
		defer close(stalled)
		runClaimedJobs(schema)
	}()
	select {
	case <-parked:
	case <-time.After(2 * time.Minute):
		t.Fatal("the third commit slice never read its chunk")
	}
	if v := c.load(id); v.Work == nil || v.Work.Done != 2*importCommitChunk {
		t.Fatalf("before the stall: %+v; want %d rows done", v.Work, 2*importCommitChunk)
	}

	// Its claim runs out; the next worker takes the job and finishes.
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.async_jobs SET leased_until = CURRENT_TIMESTAMP - interval '1 second' WHERE id = $1`, schema), jobID); err != nil {
		t.Fatal(err)
	}
	done := c.outcome(c.drive(id))
	if done.Docs != done.Counts.Committed || done.Created != done.Counts.Committed || done.Counts.Failed != 0 || done.ImportJobs != 1 {
		t.Fatalf("the next worker's import: %+v", done)
	}

	close(wake)
	select {
	case <-stalled:
	case <-time.After(2 * time.Minute):
		t.Fatal("the stalled worker never finished")
	}
	if !wroteAfterWaking.Load() {
		t.Fatal("the stalled worker did not get as far as writing its chunk, so the fence was not tried")
	}
	sameCrashResult(t, "after the stalled worker woke", c.outcome(c.load(id)), done)
	var status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.async_jobs WHERE id = $1`, schema), jobID).Scan(&status); err != nil || status != "Succeeded" {
		t.Fatalf("the job is %s (%v), want Succeeded", status, err)
	}
}
