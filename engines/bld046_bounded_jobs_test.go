package engines

// BLD-046 (bounded imports, reports, exports and jobs) regression coverage.
// Uses the tenant_lifecycle_test.go scratch-tenant idiom
// (uniqueLifecycleTenant/ProvisionTenantSchema/dropLifecycleTenant) rather
// than the shared "default" dev tenant wherever a test writes rows or
// depends on a tenant-specific setting override - several of these tests
// exist specifically to prove a worker checks the REQUESTING tenant's own
// state, so running them against the one shared tenant every other test
// also touches would defeat the point (and risk the exact kind of
// cross-session pollution audit_archive_test.go's own history warns about).

import (
	"context"
	"bytes"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestProcessReportExportJobsUsesTheRequestingTenantsRowCap reproduces the
// bug this pass fixed: processReportExportJobs used to call
// RunReport(schema, ...) - passing the tenant's SCHEMA NAME where a
// tenant_id was expected. db.GetTenantSchema never finds a schema name as a
// tenant_id, so it silently fell back to "tenant_default", meaning every
// async report export checked tenant_default's row cap/module entitlements
// instead of the tenant that actually queued the job. A scratch tenant with
// its own 1-row cap running a report that returns 2 rows only fails
// (REPORT-0285) if its OWN cap is the one actually applied.
func TestProcessReportExportJobsUsesTheRequestingTenantsRowCap(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if err := SetModuleEntitlement(tenantID, "reports", true, "test"); err != nil {
		t.Fatalf("SetModuleEntitlement: %v", err)
	}
	// 100 is the registry's own Min bound for this setting (settingBound(100)
	// in settings_definitions.go) - the report below returns one row over it.
	if err := SetSetting(tenantID, "platform.max_sync_report_rows", "100", "test"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	reportID := fmt.Sprintf("bld046-test-report-%d", time.Now().UnixNano())
	RegisterReport(ReportDefinition{
		ID:       reportID,
		Label:    "BLD-046 Test Report",
		Category: "Admin",
		Columns:  []ReportColumn{{Key: "n", Label: "N"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows := make([]map[string]interface{}, 101)
			for i := range rows {
				rows[i] = map[string]interface{}{"n": i}
			}
			return rows, nil
		},
	})

	jobID, err := CreateReportExportJob(tenantID, reportID, "HR/Admin", nil, "system")
	if err != nil {
		t.Fatalf("CreateReportExportJob: %v", err)
	}

	processReportExportJobs(context.Background(), schema)

	status, _, code, err := GetReportExportJob(tenantID, jobID)
	if err != nil {
		t.Fatalf("GetReportExportJob: %v", err)
	}
	if status != "Failed" || code != "REPORT-0285" {
		t.Fatalf("expected the scratch tenant's own 100-row cap to reject a 101-row report (status=Failed, code=REPORT-0285); got status=%q code=%q - Completed here means processReportExportJobs is checking tenant_default's settings again, not this tenant's", status, code)
	}
}

// TestReadCSVRecordsEnforcesMaxImportRows covers the new row cap: a file at
// the configured cap is accepted, one row over is rejected with DATAIM-0189
// before any doctype validation runs.
func TestReadCSVRecordsEnforcesMaxImportRows(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	// 100 is the registry's own Min bound for this setting.
	if err := SetSetting(tenantID, "platform.max_import_rows", "100", "test"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	makeCSV := func(dataRows int) string {
		var b strings.Builder
		b.WriteString("id,name\n")
		for i := 0; i < dataRows; i++ {
			fmt.Fprintf(&b, "ROW%d,Name %d\n", i, i)
		}
		return b.String()
	}

	if _, err := readCSVRecords(tenantID, strings.NewReader(makeCSV(100))); err != nil {
		t.Fatalf("100 data rows against a cap of 100 should be accepted: %v", err)
	}

	_, err := readCSVRecords(tenantID, strings.NewReader(makeCSV(101)))
	if err == nil {
		t.Fatalf("101 data rows against a cap of 100 should be rejected")
	}
	verr, ok := err.(*ValidationError)
	if !ok || verr.Code != "DATAIM-0189" {
		t.Fatalf("expected a DATAIM-0189 ValidationError, got %v (%T)", err, err)
	}
}

// TestRunDocDataImportCapsItemizedErrorsWithASummary covers the error-list
// cap: every row still counts toward FailedRows, but only the first
// maxImportErrorsRecorded get an itemized entry, with one honest summary
// entry replacing the rest. Every docRow carries a non-empty preErrors
// entry, so importBatch never reaches ValidateDocument/the DB write for any
// row (batchSuccessCount stays 0, so its deferred tx.Rollback always fires)
// - safe to run against the shared "default" tenant without a scratch one.
func TestRunDocDataImportCapsItemizedErrorsWithASummary(t *testing.T) {
	db.InitDB(testConnStr())

	rowCount := maxImportErrorsRecorded + 5
	docRows := make([]map[string]interface{}, rowCount)
	preErrors := make([]string, rowCount)
	for i := range docRows {
		docRows[i] = map[string]interface{}{}
		preErrors[i] = "forced failure for this test"
	}

	result, err := runDocDataImport("default", "Vendor", "system", "", false, docRows, preErrors)
	if err != nil {
		t.Fatalf("runDocDataImport: %v", err)
	}
	if result.FailedRows != rowCount {
		t.Fatalf("expected FailedRows=%d, got %d", rowCount, result.FailedRows)
	}
	if len(result.Errors) != maxImportErrorsRecorded+1 {
		t.Fatalf("expected %d itemized errors plus one summary entry, got %d", maxImportErrorsRecorded+1, len(result.Errors))
	}
	summary := result.Errors[len(result.Errors)-1]
	if summary.RowNumber != 0 || !strings.Contains(summary.Message, "5 additional row(s)") {
		t.Fatalf("expected a summary entry accounting for the 5 uncounted rows, got %+v", summary)
	}
}

// TestSweepReportExportJobRetention covers the new retention sweep: an old
// terminal (Completed/Failed) job is deleted, a recent terminal job and an
// old-but-still-Pending job are both left alone.
func TestSweepReportExportJobRetention(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if err := SetSetting(tenantID, "platform.report_export_retention_days", "7", "test"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	insertJob := func(id, status string, updatedAt time.Time) {
		data, _ := json.Marshal(map[string]interface{}{"id": id, "code": id, "status": status})
		if _, err := db.DB.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by, updated_at) VALUES ($1, 'ReportExportJob', $2, $3, 'system', $4)`, schema),
			id, data, status, updatedAt); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	old := time.Now().AddDate(0, 0, -10)
	recent := time.Now().AddDate(0, 0, -1)
	insertJob("OLD-DONE", "Completed", old)
	insertJob("OLD-FAILED", "Failed", old)
	insertJob("RECENT-DONE", "Completed", recent)
	insertJob("OLD-PENDING", "Pending", old)

	n, err := SweepReportExportJobRetention(tenantID)
	if err != nil {
		t.Fatalf("SweepReportExportJobRetention: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 rows swept (the two old terminal jobs), got %d", n)
	}

	var remaining int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype = 'ReportExportJob'`, schema)).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("expected 2 rows remaining (recent-completed, old-pending), got %d", remaining)
	}
}

// TestStreamSearchFeedExportCSVWritesEveryActiveItem covers the streaming
// PIM search-feed export against a seeded item, using a scratch tenant so
// the row count is known exactly rather than depending on the shared dev
// tenant's own catalog size/content.
func TestStreamSearchFeedExportCSVWritesEveryActiveItem(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	itemData, _ := json.Marshal(map[string]interface{}{"id": "ITEM-BLD046-1", "code": "ITEM-BLD046-1", "name": "BLD-046 Test Item"})
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')`, schema),
		"ITEM-BLD046-1", itemData); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	var buf bytes.Buffer
	if err := StreamSearchFeedExportCSV(tenantID, &buf); err != nil {
		t.Fatalf("StreamSearchFeedExportCSV: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "item_code,name,title,short_desc,tags,family,category,completeness_score,has_main_image") {
		t.Fatalf("missing expected header row, got:\n%s", out)
	}
	if !strings.Contains(out, "ITEM-BLD046-1") || !strings.Contains(out, "BLD-046 Test Item") {
		t.Fatalf("expected the seeded item's row, got:\n%s", out)
	}
}
