package engines

import (
	"context"
	"custom_erp/db"
	"fmt"
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

// 283.3: a report the user walked away from stops its query in Postgres, so
// its run slot and connection come back at once instead of when the query
// would have ended. The probe's query sleeps 30s; everything must be back
// within a few.
func TestReportRunAbandonedQueryStopsInPostgres(t *testing.T) {
	tenantID, _ := cancellationTestTenant(t)
	marker := fmt.Sprintf("report283_%d", time.Now().UnixNano())
	queryErr := make(chan error, 1)
	reportID := "report283-abandoned-" + marker
	RegisterReport(ReportDefinition{
		ID: reportID, Label: "283.3 Abandoned Query Probe", Category: "Admin",
		Columns: []ReportColumn{{Key: "n", Label: "N"}},
		Run: func(string, map[string]string) ([]map[string]interface{}, error) {
			_, err := db.DB.Exec(`SELECT pg_sleep(30) /* ` + marker + ` */`)
			queryErr <- err
			return []map[string]interface{}{{"n": 1}}, err
		},
	})
	active := func() (int, error) {
		var n int
		err := db.DB.QueryRow(`SELECT count(*) FROM pg_stat_activity
			WHERE state = 'active' AND pid <> pg_backend_pid() AND query LIKE '%' || $1 || '%'`, marker).Scan(&n)
		return n, err
	}
	running := func() int {
		t.Helper()
		n, err := active()
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// leave once the query is running in Postgres
		for i := 0; i < 200; i++ {
			if n, _ := active(); n > 0 {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		cancel()
	}()
	if _, _, _, err := RunReportContext(ctx, tenantID, reportID, RoleSuperAdmin, "u1", nil); !IsCancellation(err) {
		t.Fatalf("abandoned run: %v, want a cancellation", err)
	}
	select {
	case err := <-queryErr:
		if err == nil {
			t.Fatal("the abandoned report's query ran to the end")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the abandoned report's query was still running 5s after the user left")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		reportRuns.Lock()
		live := reportRuns.perTenant[tenantID]
		reportRuns.Unlock()
		if live == 0 && running() == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("5s after the user left: %d runs counted, %d queries active in Postgres", live, running())
		}
		time.Sleep(25 * time.Millisecond)
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
