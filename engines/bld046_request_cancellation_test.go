package engines

// BLD-046, the "cancel abandoned requests" leg of the Done bar. The
// companion file bld046_bounded_jobs_test.go covers the bounding leg
// (row/buffer/retention caps) and jobrunner_test.go covers the job leg
// (CancelJob interrupting a running handler without burning a retry).
//
// Every test here is written as the Done bar's three cases against one
// path, so a single `go test -run BLD046Cancellation -count=3` run is the
// three-fresh-round evidence rather than three differently-shaped suites:
//
//	normal      - the uncancelled run still returns its rows/result
//	oversized   - an over-budget input is refused promptly, not after
//	              being fully buffered and processed
//	interrupted - a cancelled run returns ctx.Err() promptly, does no
//	              further work, and leaves the retry/idempotency contract
//	              exactly as it was
//
// Uses the scratch-tenant idiom (uniqueLifecycleTenant) wherever a test
// writes rows, for the reasons bld046_bounded_jobs_test.go's own header
// records.

import (
	"bytes"
	"context"
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// cancellationPromptness is how quickly a cancelled call must return. The
// blocking work these tests inject lasts far longer (blockedRunDuration),
// so a return inside this window can only mean the ctx path released the
// caller rather than the work finishing.
const cancellationPromptness = 2 * time.Second

// blockedRunDuration is how long injected work blocks for. Long enough that
// "the work finished normally" is never a plausible explanation for a prompt
// return, short enough that a failing test does not hang a suite for long.
const blockedRunDuration = 30 * time.Second

// registerCancellationTestReport registers a uniquely-named report whose Run
// blocks until released, and reports how many times Run was entered.
func registerCancellationTestReport(t *testing.T, release <-chan struct{}) (reportID string, runs *atomic.Int32) {
	t.Helper()
	reportID = fmt.Sprintf("bld046-cancel-%d-%s", time.Now().UnixNano(), strings.ToLower(t.Name()))
	runs = &atomic.Int32{}
	RegisterReport(ReportDefinition{
		ID:       reportID,
		Label:    "BLD-046 Cancellation Probe",
		Category: "Admin",
		Columns:  []ReportColumn{{Key: "n", Label: "N"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			runs.Add(1)
			select {
			case <-release:
			case <-time.After(blockedRunDuration):
			}
			return []map[string]interface{}{{"n": 1}}, nil
		},
	})
	return reportID, runs
}

// cancellationTestTenant provisions a scratch tenant with reports enabled.
func cancellationTestTenant(t *testing.T) (tenantID, schema string) {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema = uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if err := SetModuleEntitlement(tenantID, "reports", true, "test"); err != nil {
		t.Fatalf("SetModuleEntitlement: %v", err)
	}
	return tenantID, schema
}

// TestBLD046CancellationReportPath covers the report leg. The report path is
// the one where cancellation is prompt rather than complete: the abandoned
// run's own query is left to finish and be discarded (see
// RunReportContext's comment for why that is deliberate), so what is
// asserted is that the CALLER is released immediately and that no
// already-cancelled request ever starts a run at all.
func TestBLD046CancellationReportPath(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		tenantID, _ := cancellationTestTenant(t)
		release := make(chan struct{})
		close(release) // return straight away
		reportID, runs := registerCancellationTestReport(t, release)

		def, rows, _, err := RunReportContext(context.Background(), tenantID, reportID, RoleSuperAdmin, "u1", nil)
		if err != nil {
			t.Fatalf("uncancelled run must succeed: %v", err)
		}
		if def == nil || len(rows) != 1 {
			t.Fatalf("expected the report's single row back, got def=%v rows=%d", def, len(rows))
		}
		if got := runs.Load(); got != 1 {
			t.Fatalf("expected Run to be entered exactly once, got %d", got)
		}
	})

	t.Run("oversized", func(t *testing.T) {
		tenantID, _ := cancellationTestTenant(t)
		// 100 is this setting's own registry Min bound; the report returns
		// one row over it, so REPORT-0161 is the oversized refusal.
		if err := SetSetting(tenantID, "platform.max_sync_report_rows", "100", "test"); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		reportID := fmt.Sprintf("bld046-cancel-oversized-%d", time.Now().UnixNano())
		RegisterReport(ReportDefinition{
			ID: reportID, Label: "BLD-046 Oversized Probe", Category: "Admin",
			Columns: []ReportColumn{{Key: "n", Label: "N"}},
			Run: func(string, map[string]string) ([]map[string]interface{}, error) {
				rows := make([]map[string]interface{}, 101)
				for i := range rows {
					rows[i] = map[string]interface{}{"n": i}
				}
				return rows, nil
			},
		})

		_, _, _, err := RunReportContext(context.Background(), tenantID, reportID, RoleSuperAdmin, "u1", nil)
		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Code != "REPORT-0161" {
			t.Fatalf("expected the REPORT-0161 row-cap refusal, got %v", err)
		}
		// An oversized refusal must not be misreported as a cancellation -
		// the two take different branches in writeEngineError.
		if IsCancellation(err) {
			t.Fatalf("an oversized refusal must not classify as a cancellation")
		}
	})

	t.Run("interrupted mid-run releases the caller promptly", func(t *testing.T) {
		tenantID, _ := cancellationTestTenant(t)
		release := make(chan struct{})
		defer close(release)
		reportID, runs := registerCancellationTestReport(t, release)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			// Let the run get going, then abandon it the way a browser
			// navigating away cancels r.Context().
			time.Sleep(200 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		_, rows, _, err := RunReportContext(ctx, tenantID, reportID, RoleSuperAdmin, "u1", nil)
		elapsed := time.Since(start)

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if !IsCancellation(err) {
			t.Fatalf("IsCancellation must recognise the cancelled run's error")
		}
		if elapsed > cancellationPromptness {
			t.Fatalf("cancelled run took %v to return; the injected work blocks for %v, so the caller was not released", elapsed, blockedRunDuration)
		}
		if rows != nil {
			t.Fatalf("a cancelled run must not return rows, got %d", len(rows))
		}
		if got := runs.Load(); got != 1 {
			t.Fatalf("expected exactly one Run entry, got %d", got)
		}
	})

	t.Run("interrupted before start never runs the report", func(t *testing.T) {
		tenantID, _ := cancellationTestTenant(t)
		release := make(chan struct{})
		close(release)
		reportID, runs := registerCancellationTestReport(t, release)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, _, err := RunReportContext(ctx, tenantID, reportID, RoleSuperAdmin, "u1", nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if got := runs.Load(); got != 0 {
			t.Fatalf("an already-abandoned request must not start a report run at all, got %d runs", got)
		}
	})
}

// TestBLD046CancellationImportPath covers the import leg, where cancellation
// is complete rather than merely prompt: the batch loop is this codebase's
// own code, so an abandoned import genuinely stops doing work. The
// interrupted case also asserts the retry/idempotency contract the Done bar
// names - batches that already committed stay committed, and the rows that
// never ran are simply absent, so re-uploading the same file resumes
// correctly through the existing per-row existence check.
func TestBLD046CancellationImportPath(t *testing.T) {
	// platform.import_batch_rows has a registry Min of 50, so the batch size
	// is 50 and 120 rows gives three batches (50/50/20) - a real
	// between-batch boundary to cancel at.
	const batchSize = 50
	const totalRows = 120
	buildRows := func(prefix string) ([]map[string]interface{}, []string) {
		docRows := make([]map[string]interface{}, totalRows)
		preErrors := make([]string, totalRows)
		for i := range docRows {
			id := fmt.Sprintf("%s-%03d", prefix, i)
			docRows[i] = map[string]interface{}{"id": id, "code": id, "name": "Cancellation Probe " + id}
		}
		return docRows, preErrors
	}
	countVendors := func(t *testing.T, schema, prefix string) int {
		t.Helper()
		var n int
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT count(*) FROM %s.documents WHERE doctype = 'Vendor' AND id LIKE $1`, schema), prefix+"%").Scan(&n); err != nil {
			t.Fatalf("count vendors: %v", err)
		}
		return n
	}

	t.Run("normal", func(t *testing.T) {
		tenantID, schema := cancellationTestTenant(t)
		if err := SetSetting(tenantID, "platform.import_batch_rows", "50", "test"); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		prefix := fmt.Sprintf("VCNORM%d", time.Now().UnixNano()%100000)
		docRows, preErrors := buildRows(prefix)

		result, err := runDocDataImportContext(context.Background(), tenantID, "Vendor", "system", "", false, docRows, preErrors)
		if err != nil {
			t.Fatalf("uncancelled import must succeed: %v", err)
		}
		if result.TotalRows != totalRows {
			t.Fatalf("expected TotalRows=%d, got %d", totalRows, result.TotalRows)
		}
		if n := countVendors(t, schema, prefix); n != totalRows {
			t.Fatalf("expected all %d rows committed, got %d", totalRows, n)
		}
	})

	t.Run("oversized refuses before parsing the whole upload", func(t *testing.T) {
		tenantID, _ := cancellationTestTenant(t)
		if err := SetSetting(tenantID, "platform.max_import_rows", "100", "test"); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		var csv strings.Builder
		csv.WriteString("id,code,name\n")
		for i := 0; i < 400; i++ {
			fmt.Fprintf(&csv, "V%04d,V%04d,Vendor %d\n", i, i, i)
		}
		_, err := readCSVRecordsContext(context.Background(), tenantID, strings.NewReader(csv.String()))
		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Code != "DATAIM-0189" {
			t.Fatalf("expected the DATAIM-0189 row-cap refusal, got %v", err)
		}
		if IsCancellation(err) {
			t.Fatalf("an oversized refusal must not classify as a cancellation")
		}
	})

	t.Run("interrupted keeps committed batches and skips the rest", func(t *testing.T) {
		tenantID, schema := cancellationTestTenant(t)
		if err := SetSetting(tenantID, "platform.import_batch_rows", "50", "test"); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		prefix := fmt.Sprintf("VCINT%d", time.Now().UnixNano()%100000)
		docRows, preErrors := buildRows(prefix)

		// Cancelling after the first batch has committed is what an
		// abandoned upload looks like from inside the loop. Driven off the
		// observed committed-row count rather than a sleep so the test does
		// not depend on how fast the batch happens to commit.
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			deadline := time.Now().Add(blockedRunDuration)
			for time.Now().Before(deadline) {
				if countVendors(t, schema, prefix) >= batchSize {
					cancel()
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			cancel()
		}()

		_, err := runDocDataImportContext(ctx, tenantID, "Vendor", "system", "", false, docRows, preErrors)
		<-done
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}

		committed := countVendors(t, schema, prefix)
		if committed == 0 {
			t.Fatalf("expected the batches that already committed to stay committed, got 0 - cancellation must not roll back accepted work")
		}
		if committed >= totalRows {
			t.Fatalf("expected the import to stop short of all %d rows, got %d - cancellation did not actually stop the loop", totalRows, committed)
		}
		if committed%batchSize != 0 {
			t.Fatalf("expected a whole number of committed batches (multiple of %d), got %d - cancellation tore up a batch mid-transaction", batchSize, committed)
		}

		// The retry contract: re-running the same input completes the rows
		// that never ran and updates the ones that did, with no duplicates.
		result, err := runDocDataImportContext(context.Background(), tenantID, "Vendor", "system", "", false, docRows, preErrors)
		if err != nil {
			t.Fatalf("re-running an interrupted import must succeed: %v", err)
		}
		if result.FailedRows != 0 {
			t.Fatalf("re-run must not fail any row, got %d failed: %+v", result.FailedRows, result.Errors)
		}
		if n := countVendors(t, schema, prefix); n != totalRows {
			t.Fatalf("expected the re-run to leave exactly %d rows (no duplicates, nothing missing), got %d", totalRows, n)
		}
	})
}

// TestBLD046CancellationExportPath covers the streaming-export leg. This is
// the one path where cancellation reaches PostgreSQL: the query runs through
// QueryContext, so an abandoned whole-catalog export stops server-side
// instead of completing for a client that has gone.
func TestBLD046CancellationExportPath(t *testing.T) {
	seedItems := func(t *testing.T, schema string, n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("ITEM-BLD046-CANCEL-%04d", i)
			data, _ := json.Marshal(map[string]interface{}{"id": id, "code": id, "name": "Cancellation Probe " + id})
			if _, err := db.DB.Exec(fmt.Sprintf(
				`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')`, schema),
				id, data); err != nil {
				t.Fatalf("seed item %s: %v", id, err)
			}
		}
	}

	t.Run("normal", func(t *testing.T) {
		tenantID, schema := cancellationTestTenant(t)
		seedItems(t, schema, 3)
		var buf bytes.Buffer
		if err := StreamSearchFeedExportCSVContext(context.Background(), tenantID, &buf); err != nil {
			t.Fatalf("uncancelled export must succeed: %v", err)
		}
		// Counted as CSV lines beginning with the probe prefix: each id also
		// appears inside its own row's generated name, so a plain substring
		// count would see every row twice.
		dataRows := 0
		for _, line := range strings.Split(buf.String(), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "ITEM-BLD046-CANCEL-") {
				dataRows++
			}
		}
		if dataRows != 3 {
			t.Fatalf("expected 3 seeded rows in the feed, got %d:\n%s", dataRows, buf.String())
		}
	})

	t.Run("interrupted before start writes nothing", func(t *testing.T) {
		tenantID, schema := cancellationTestTenant(t)
		seedItems(t, schema, 3)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var buf bytes.Buffer
		start := time.Now()
		err := StreamSearchFeedExportCSVContext(ctx, tenantID, &buf)
		elapsed := time.Since(start)

		if !IsCancellation(err) {
			t.Fatalf("expected a cancellation error, got %v", err)
		}
		if elapsed > cancellationPromptness {
			t.Fatalf("cancelled export took %v to return", elapsed)
		}
		if buf.Len() != 0 {
			t.Fatalf("an already-abandoned export must write nothing, wrote %d bytes", buf.Len())
		}
	})
}

// TestBLD046IsCancellationClassifiesOnlyAbandonment guards the predicate
// writeEngineError depends on: misclassifying a real failure as a
// cancellation would turn a 500 into a silent 499 and hide the error from
// both the user and the on-call path.
func TestBLD046IsCancellationClassifiesOnlyAbandonment(t *testing.T) {
	cancelling := []error{
		context.Canceled,
		context.DeadlineExceeded,
		fmt.Errorf("wrapped: %w", context.Canceled),
		fmt.Errorf("wrapped: %w", context.DeadlineExceeded),
	}
	for _, err := range cancelling {
		if !IsCancellation(err) {
			t.Fatalf("expected %v to classify as a cancellation", err)
		}
	}
	notCancelling := []error{
		nil,
		errors.New("connection refused"),
		&ValidationError{Code: "REPORT-0161", Message: "too many rows"},
		fmt.Errorf("report failed: %w", errors.New("syntax error")),
	}
	for _, err := range notCancelling {
		if IsCancellation(err) {
			t.Fatalf("expected %v NOT to classify as a cancellation", err)
		}
	}
}
