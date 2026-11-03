package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// Stage 49.10.3: background export and job execution across two hostile
// clients. An export or job queued by A runs in A's schema only, its output
// is A's alone, and B can neither read it nor have a job run for it.

const stage49103JobType = "stage49103_probe"

var stage49103Seen = struct {
	sync.Mutex
	schemas []string
}{}

func stage49103Seed(t *testing.T, schema, marker string) {
	t.Helper()
	data, _ := json.Marshal(map[string]interface{}{"item_code": "S49103-" + marker, "name": "Colliding item " + marker})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ('S49103-SHARED', 'Item', $1, 'Active', 'system')`, schema), string(data)); err != nil {
		t.Fatalf("seed %s: %v", schema, err)
	}
}

func TestStage49103BackgroundExportAndJobsStayInTheirClient(t *testing.T) {
	aID, aSchema := cancellationTestTenant(t)
	bID, bSchema := cancellationTestTenant(t)
	const markerA, markerB = "MARKER-A-49103", "MARKER-B-49103"
	stage49103Seed(t, aSchema, markerA)
	stage49103Seed(t, bSchema, markerB)

	// --- export
	exportA, err := CreateReportExportJob(aID, "item-master-completeness", RoleSuperAdmin, nil, "ua")
	if err != nil {
		t.Fatal(err)
	}
	exportB, err := CreateReportExportJob(bID, "item-master-completeness", RoleSuperAdmin, nil, "ub")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	processReportExportJobs(ctx, aSchema)
	// A's run did not touch B's pending export.
	if st, _, _, err := GetReportExportJob(bID, exportB); err != nil || st != "Pending" {
		t.Fatalf("B's export after A's run: %q, %v - want Pending", st, err)
	}
	processReportExportJobs(ctx, bSchema)
	stA, csvA, _, err := GetReportExportJob(aID, exportA)
	if err != nil || stA != "Completed" {
		t.Fatalf("A's export: %q, %v", stA, err)
	}
	if !strings.Contains(string(csvA), markerA) || strings.Contains(string(csvA), markerB) {
		t.Fatalf("A's export carries the wrong client's rows: %s", csvA)
	}
	stB, csvB, _, err := GetReportExportJob(bID, exportB)
	if err != nil || stB != "Completed" || !strings.Contains(string(csvB), markerB) || strings.Contains(string(csvB), markerA) {
		t.Fatalf("B's export: %q, %v, %s", stB, err, csvB)
	}
	// A's export link, asked for by B: gone, as if it never existed.
	if _, _, _, err := GetReportExportJob(bID, exportA); err == nil {
		t.Fatal("B read A's export")
	}
	if visible, _ := ReportExportJobVisible(bID, exportA, "ua", RoleCashier); visible {
		t.Fatal("B is shown A's export")
	}
	if visible, _ := ReportExportJobVisible(aID, exportA, "ua", RoleSuperAdmin); !visible {
		t.Fatal("positive control: A cannot see its own export")
	}

	// --- job
	if _, ok := jobHandlers[stage49103JobType]; !ok {
		RegisterJobHandler(stage49103JobType, func(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
			stage49103Seen.Lock()
			stage49103Seen.schemas = append(stage49103Seen.schemas, schema+"|"+fmt.Sprint(job.Payload["note"]))
			stage49103Seen.Unlock()
			return map[string]interface{}{"ran_in": schema, "tenant": job.Payload["tenant_id"]}, nil
		})
	}
	stage49103Seen.Lock()
	stage49103Seen.schemas = nil
	stage49103Seen.Unlock()
	jobA, err := EnqueueJob(aID, stage49103JobType, map[string]interface{}{"note": "own"}, "")
	if err != nil {
		t.Fatal(err)
	}
	// A job queued for A that names B in its payload.
	crossA, err := EnqueueJob(aID, stage49103JobType, map[string]interface{}{"note": "cross", "tenant_id": bID}, "")
	if err != nil {
		t.Fatal(err)
	}
	runClaimedJobs(bSchema) // B's runner sees nothing of A's
	stage49103Seen.Lock()
	if len(stage49103Seen.schemas) != 0 {
		t.Fatalf("B's runner ran: %v", stage49103Seen.schemas)
	}
	stage49103Seen.Unlock()
	runClaimedJobs(aSchema)

	stage49103Seen.Lock()
	seen := append([]string(nil), stage49103Seen.schemas...)
	stage49103Seen.Unlock()
	if len(seen) != 1 || seen[0] != aSchema+"|own" {
		t.Fatalf("handler ran for %v, want only A's own job in %s", seen, aSchema)
	}
	var status, result string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status, COALESCE(result::text, '') FROM %s.async_jobs WHERE id = $1`, aSchema), jobA).Scan(&status, &result); err != nil || status != "Succeeded" || !strings.Contains(result, aSchema) || strings.Contains(result, bSchema) {
		t.Fatalf("A's job: %q %s %v", status, result, err)
	}
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.async_jobs WHERE id = $1`, aSchema), crossA).Scan(&status); err != nil || status == "Succeeded" {
		t.Fatalf("the job naming B ran: %q %v", status, err)
	}
	var n int
	db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.async_jobs WHERE job_type = $1`, bSchema), stage49103JobType).Scan(&n)
	if n != 0 {
		t.Fatalf("%d of A's jobs landed in B's queue", n)
	}
	// B's job list shows none of it.
	rows, err := ListJobs(bID, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(rows); strings.Contains(string(b), jobA) || strings.Contains(string(b), stage49103JobType) {
		t.Fatalf("B's job list shows A's job: %s", b)
	}
}
