package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Stage 55.11: an import resumes from its failed rows only - each original
// row imports once, a re-sent imported row is refused, a dry run changes
// nothing - and a schedule runs as its owner, never as "system".
func TestImportResumeFromFailedRowsAndScheduleOwner(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	brands := func() int {
		var n int
		db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'Brand' AND data->>'code' LIKE 'RSM-%' AND deleted_at IS NULL").Scan(&n)
		return n
	}
	codeOf := func(err error) string {
		if v, ok := err.(*ValidationError); ok {
			return v.Code
		}
		return ""
	}

	csvIn := "code,name,status\nRSM-1,One,Active\nRSM-2,Two,Bogus\nRSM-3,,Active\nRSM-4,Four,Active\n"
	res, err := BulkImportCSV(tenantID, "Brand", strings.NewReader(csvIn), "admin", RoleSuperAdmin, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.SuccessRows != 2 || res.FailedRows != 2 {
		t.Fatalf("seed import: %d ok %d failed, want 2/2 (%v)", res.SuccessRows, res.FailedRows, res.Errors)
	}
	jobID, err := RecordImportJob(tenantID, "Brand", res, "admin")
	if err != nil {
		t.Fatal(err)
	}
	sum, _ := ImportJobSummary(tenantID, jobID)
	if sum["open_failed_rows"] != 2 || sum["resumable"] != true {
		t.Fatalf("new job keeps its 2 failed rows: %v", sum)
	}
	failed, err := ImportJobFailedRowsCSV(tenantID, jobID)
	if err != nil || !strings.Contains(string(failed), "original_row,code,name,status,error") || !strings.Contains(string(failed), "3,RSM-2,Two,Bogus,") || !strings.Contains(string(failed), "4,RSM-3,,Active,") {
		t.Fatalf("failed rows download:\n%s %v", failed, err)
	}

	resume := func(body string, dry bool) (*ImportResult, string, error) {
		return ResumeImportJob(context.Background(), tenantID, jobID, strings.NewReader(body), "admin", RoleSuperAdmin, dry)
	}
	if _, child, err := resume("original_row,code,name,status\n3,RSM-2,Two,Active\n", true); err != nil || child != "" || brands() != 2 {
		t.Fatalf("a dry run writes nothing: child=%q brands=%d err=%v", child, brands(), err)
	}
	r, child, err := resume("original_row,code,name,status,error\n3,RSM-2,Two,Active,old reason\n4,RSM-3,,Active,old\n", false)
	if err != nil || child == "" || r.SuccessRows != 1 || r.FailedRows != 1 {
		t.Fatalf("first resume: %+v child=%q err=%v", r, child, err)
	}
	sum, _ = ImportJobSummary(tenantID, jobID)
	if sum["open_failed_rows"] != 1 || sum["resolved_rows"].(float64) != 1 {
		t.Fatalf("one fixed, one still open: %v", sum)
	}
	var from string
	db.DB.QueryRow("SELECT data->>'resumed_from' FROM "+schema+".documents WHERE id = $1", child).Scan(&from)
	if from != jobID {
		t.Fatalf("the resume job names its parent, got %q", from)
	}
	if _, _, err := resume("original_row,code,name,status\n3,RSM-2,Two,Active\n", false); codeOf(err) != "DATAIM-0471" {
		t.Fatalf("an imported row cannot be sent again: %v", err)
	}
	if _, _, err := resume("code,name,status\nRSM-3,Three,Active\n", false); codeOf(err) != "DATAIM-0471" {
		t.Fatalf("the original_row column is required: %v", err)
	}
	if _, _, err := resume("original_row,code,name,status\n4,RSM-3,Three,Active\n", false); err != nil {
		t.Fatal(err)
	}
	sum, _ = ImportJobSummary(tenantID, jobID)
	if sum["open_failed_rows"] != 0 || sum["status"] != "Completed" || sum["resumable"] != false || brands() != 4 {
		t.Fatalf("all rows in, exactly once: %v brands=%d", sum, brands())
	}

	// Schedule owner.
	raw, _ := json.Marshal(map[string]interface{}{"code": "SCH-1", "name": "Nightly", "owner": "ghost"})
	db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('SCH-1', 'PIMImportSchedule', $1, 'Active', 'admin')", raw)
	if _, _, err := PIMImportScheduleRunAs(tenantID, "SCH-1"); err == nil || !strings.Contains(err.Error(), "not an active user") {
		t.Fatalf("an inactive owner stops the schedule: %v", err)
	}
	db.DB.Exec("UPDATE " + schema + ".documents SET data = data - 'owner' WHERE id = 'SCH-1'")
	user, role, err := PIMImportScheduleRunAs(tenantID, "SCH-1")
	if err != nil || user != "admin" || role == "" {
		t.Fatalf("no owner falls back to the creator with their role: %q %q %v", user, role, err)
	}
	db.DB.Exec("UPDATE " + schema + ".documents SET created_by = 'system' WHERE id = 'SCH-1'")
	if _, _, err := PIMImportScheduleRunAs(tenantID, "SCH-1"); err == nil {
		t.Fatal("a schedule with no owner and a system creator must not run as system")
	}
}
