package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestStage47183ImportFilesExpire: after platform.import_file_retention_days
// a scheduled import's processed CSVs are deleted from its drop folder and
// an old import job loses its raw rows and error file (its counts, totals
// and lineage stay); fresh files and jobs are untouched; 0 is a hold.
func TestStage47183ImportFilesExpire(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	drop := t.TempDir()
	old, fresh := filepath.Join(drop, "imported", "old.csv"), filepath.Join(drop, "imported", "fresh.csv")
	failedOld := filepath.Join(drop, "failed", "old.csv")
	waiting := filepath.Join(drop, "waiting.csv") // not yet processed: never swept
	for _, p := range []string{old, fresh, failedOld, waiting} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("a,b\n1,2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stale := time.Now().Add(-40 * 24 * time.Hour)
	for _, p := range []string{old, failedOld, waiting} {
		if err := os.Chtimes(p, stale, stale); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ('SCH-47183', 'PIMImportSchedule', $1, 'Active', 'system')`,
		`{"id":"SCH-47183","name":"drop","source_path":`+jsonString(drop)+`}`)
	jobData := `{"id":"%s","doctype_name":"Item","status":"Completed","total_rows":2,"success_rows":1,"failed_rows":1,
		"error_csv":"row_number,message\n2,bad\n","source_header":["sku","qty"],"open_failed_rows":[{"row":2,"cells":["X","y"],"message":"bad"}],"resumable":true,
		"control_totals":{"rows_read":2,"rows_accepted":1,"rows_rejected":1,"balanced":true},"rules_version":"test","source":{"sha256":"abc","size_bytes":10}}`
	exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by, created_at) VALUES ('IMPJOB47183OLD', 'ImportJob', $1, 'Completed', 'system', now() - interval '40 days')`, fmt.Sprintf(jobData, "IMPJOB47183OLD"))
	exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by, created_at) VALUES ('IMPJOB47183NEW', 'ImportJob', $1, 'Completed', 'system', now() - interval '1 day')`, fmt.Sprintf(jobData, "IMPJOB47183NEW"))
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	jobHas := func(id, key string) bool {
		var has bool
		db.DB.QueryRow(`SELECT data ? $2 FROM `+schema+`.documents WHERE id = $1`, id, key).Scan(&has)
		return has
	}

	// 0 = a hold: nothing goes.
	if err := SetSetting(tenantID, "platform.import_file_retention_days", "0", "test"); err != nil {
		t.Fatal(err)
	}
	if res, err := SweepImportFileRetention(tenantID); err != nil || res.Files != 0 || res.Jobs != 0 || !exists(old) || !jobHas("IMPJOB47183OLD", "open_failed_rows") {
		t.Fatalf("hold: swept %+v (err %v), old file present %v", res, err, exists(old))
	}

	// 30 days: the stale processed files and the old job's raw rows go.
	if err := SetSetting(tenantID, "platform.import_file_retention_days", "30", "test"); err != nil {
		t.Fatal(err)
	}
	res, err := SweepImportFileRetention(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 2 || res.Jobs != 1 {
		t.Fatalf("swept %+v, want 2 files and 1 job", res)
	}
	if exists(old) || exists(failedOld) || !exists(fresh) || !exists(waiting) {
		t.Fatalf("files: old %v, failed/old %v, fresh %v, waiting %v", exists(old), exists(failedOld), exists(fresh), exists(waiting))
	}
	if jobHas("IMPJOB47183OLD", "open_failed_rows") || jobHas("IMPJOB47183OLD", "error_csv") || jobHas("IMPJOB47183OLD", "source_header") {
		t.Fatalf("the old job still holds raw rows")
	}
	if !jobHas("IMPJOB47183OLD", "control_totals") || !jobHas("IMPJOB47183OLD", "source") || !jobHas("IMPJOB47183OLD", "raw_rows_expired_at") {
		t.Fatalf("the old job lost its totals or lineage, or does not say its rows expired")
	}
	if !jobHas("IMPJOB47183NEW", "open_failed_rows") || !jobHas("IMPJOB47183NEW", "error_csv") {
		t.Fatalf("the fresh job was stripped")
	}
	summary, err := ImportJobSummary(tenantID, "IMPJOB47183OLD")
	if err != nil || summary["resumable"] != false || summary["raw_rows_expired_at"] == nil {
		t.Fatalf("summary of the expired job: %v (err %v)", summary, err)
	}
	// A second run finds nothing more.
	if res, err := SweepImportFileRetention(tenantID); err != nil || res.Files != 0 || res.Jobs != 0 {
		t.Fatalf("second sweep: %+v (err %v)", res, err)
	}

	// Legal hold keeps everything whatever the setting says.
	exec(`UPDATE public.tenants SET legal_hold = TRUE WHERE tenant_id = $1`, tenantID)
	stale2 := filepath.Join(drop, "imported", "old2.csv")
	os.WriteFile(stale2, []byte("a\n"), 0o644)
	os.Chtimes(stale2, stale, stale)
	if res, err := SweepImportFileRetention(tenantID); err != nil || res.Files != 0 || !exists(stale2) {
		t.Fatalf("legal hold: swept %+v (err %v)", res, err)
	}
}

// TestStage47183ExpiredSandboxIsSuspendedThenPurgedTheOrdinaryWay: once a
// sandbox's expiry passes the hourly sweep suspends it (refused like any
// suspended client, on its evidence trail), a reset brings it back, and its
// purge goes through the ordinary deprovision + purge path.
func TestStage47183ExpiredSandboxIsSuspendedThenPurgedTheOrdinaryWay(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName, _, err := ProvisionSandboxTenant("0.1.0-test", 5)
	if err != nil {
		t.Fatalf("ProvisionSandboxTenant: %v", err)
	}
	defer dropLifecycleTenant(tenantID, schemaName)
	expire := func() {
		t.Helper()
		if _, err := db.DB.Exec(`UPDATE public.tenants SET sandbox_expires_at = CURRENT_TIMESTAMP - interval '1 day' WHERE tenant_id = $1`, tenantID); err != nil {
			t.Fatal(err)
		}
		InvalidateTenantLifecycleCache(tenantID)
	}
	status := func() string {
		t.Helper()
		life, err := GetTenantLifecycle(tenantID)
		if err != nil {
			t.Fatal(err)
		}
		return life.Status
	}

	// Not expired: the sweep leaves it alone.
	if _, err := SweepExpiredSandboxTenants(); err != nil {
		t.Fatal(err)
	}
	if status() != TenantStatusActive {
		t.Fatalf("an unexpired sandbox was touched: %s", status())
	}

	expire()
	if _, err := SweepExpiredSandboxTenants(); err != nil {
		t.Fatal(err)
	}
	if status() != TenantStatusSuspended {
		t.Fatalf("expired sandbox is %s, want suspended", status())
	}
	if ok, _ := TenantIsOperational(tenantID); ok {
		t.Fatalf("a suspended sandbox must not be operational")
	}
	if names := eventNames(t, tenantID); len(names) == 0 || names[0] != TenantEventSuspended {
		t.Fatalf("expected the suspension on the evidence trail, got %v", names)
	}
	// Idempotent: already suspended, nothing more happens.
	if _, err := SweepExpiredSandboxTenants(); err != nil || status() != TenantStatusSuspended {
		t.Fatalf("second sweep: %v / %s", err, status())
	}

	// A reset sets a new expiry and brings the sandbox back.
	if err := ResetSandboxTenant(tenantID, 5); err != nil {
		t.Fatalf("ResetSandboxTenant: %v", err)
	}
	if status() != TenantStatusActive {
		t.Fatalf("after a reset the sandbox is %s, want active", status())
	}
	if expired, _ := IsSandboxExpired(tenantID); expired {
		t.Fatalf("reset sandbox still reads as expired")
	}

	// Expired again: suspended, then purged by the ordinary path.
	expire()
	if _, err := SweepExpiredSandboxTenants(); err != nil || status() != TenantStatusSuspended {
		t.Fatalf("re-expiry: %v / %s", err, status())
	}
	if _, err := RequestTenantDeprovision(tenantID, "tester", "sandbox expired and not reset", 0); err != nil {
		t.Fatalf("RequestTenantDeprovision: %v", err)
	}
	rep, err := PurgeTenant(tenantID, "tester", "pg_dump s3://erp-backups/sandbox.dump")
	if err != nil {
		t.Fatalf("PurgeTenant: %v", err)
	}
	if !rep.Clean {
		t.Fatalf("residue after purge: %+v", rep)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
