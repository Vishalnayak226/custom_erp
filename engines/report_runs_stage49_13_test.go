package engines

import (
	"context"
	"custom_erp/db"
	"strings"
	"testing"
	"time"
)

// Stage 49.13.3: walking away from reports cannot pile up live runs. Each
// abandoned run still holds its goroutine and connection, so it still counts;
// past the bound a new run is refused up front (GLOBAL-0310), and the slots
// come back when the abandoned queries end.
func TestStage49133AbandonedReportRunsAreBounded(t *testing.T) {
	tenantID, _ := cancellationTestTenant(t)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	reportID, runs := registerCancellationTestReport(t, release)
	limit := reportRunsMaxFor(tenantID)

	for i := 0; i < limit; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(20 * time.Millisecond); cancel() }()
		if _, _, _, err := RunReportContext(ctx, tenantID, reportID, RoleSuperAdmin, "u1", nil); !IsCancellation(err) {
			t.Fatalf("abandoned run %d: %v, want a cancellation", i+1, err)
		}
	}
	if _, _, _, err := RunReportContext(context.Background(), tenantID, reportID, RoleSuperAdmin, "u1", nil); err != errReportRunsBusy {
		t.Fatalf("with %d abandoned runs still live: %v, want GLOBAL-0310", limit, err)
	}
	if got := int(runs.Load()); got != limit {
		t.Fatalf("%d runs started, want %d - the refused one must not start", got, limit)
	}

	close(release)
	released = true
	deadline := time.Now().Add(5 * time.Second)
	for {
		reportRuns.Lock()
		live := reportRuns.perTenant[tenantID]
		reportRuns.Unlock()
		if live == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d runs still counted after their queries ended", live)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, _, _, err := RunReportContext(context.Background(), tenantID, reportID, RoleSuperAdmin, "u1", nil); err != nil {
		t.Fatalf("after the abandoned runs ended: %v", err)
	}
}

// Stage 49.13.5 found: the nightly reconciliation ran gst-return-summary with
// no dates, so it was "could not run" everywhere. Required dates are now this
// month to date.
func TestStage49135ReconciliationFillsRequiredDates(t *testing.T) {
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	got := reconciliationParams("gst-return-summary", now)
	if got["start"] != "2026-10-01" || got["end"] != "2026-10-26" {
		t.Fatalf("params %v, want start 2026-10-01 and end 2026-10-26", got)
	}
	if len(reconciliationParams("no-such-report", now)) != 0 {
		t.Fatal("an unknown report gets no params")
	}
}

// Stage 49.13.7: an audit event the database refuses is counted with its
// reason (and pages, rate limited) - never only a local log line.
func TestStage49137LostAuditWriteIsVisible(t *testing.T) {
	db.InitDB(testConnStr())
	before, _, _ := AuditWriteLoss()
	LogAuditEvent("default", "tester49137", strings.Repeat("A", 300), "SUCCESS", "action longer than the column")
	after, reason, at := AuditWriteLoss()
	if after != before+1 || reason == "" || at.IsZero() {
		t.Fatalf("lost write not counted: %d -> %d, reason %q", before, after, reason)
	}
}
