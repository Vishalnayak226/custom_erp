package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Stage 49.3.4: an import row that adds a record needs create permission on
// its type and a row that changes one needs update - the same rule as the
// record save. A role holding update imports exactly as before.
func TestStage4934ImportRowsNeedTheRoleToAddOrChange(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	createOnly, updater := "MXCreateOnly"+suffix, "MXUpdater"+suffix
	code := "MXRB-" + suffix
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".role_permissions WHERE role IN ($1, $2)", createOnly, updater)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Brand' AND data->>'code' = $1", code)
	}
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec("INSERT INTO "+schema+".role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES ($1, 'Brand', TRUE, TRUE, FALSE, FALSE), ($2, 'Brand', TRUE, TRUE, TRUE, FALSE)", createOnly, updater); err != nil {
		t.Fatalf("seed permissions: %v", err)
	}
	run := func(role, name string) *ImportResult {
		t.Helper()
		res, err := BulkImportCSV("default", "Brand", strings.NewReader("code,name\n"+code+","+name+"\n"), "system", role, false)
		if err != nil {
			t.Fatalf("BulkImportCSV as %s: %v", role, err)
		}
		return res
	}
	if res := run(createOnly, "First"); res.SuccessRows != 1 {
		t.Fatalf("a create-only role adding a record: %+v", res.Errors)
	}
	if res := run(createOnly, "Changed"); res.SuccessRows != 0 || res.FailedRows != 1 || !strings.Contains(res.Errors[0].Message, "may not change") {
		t.Fatalf("a create-only role changing a record: %d ok %+v", res.SuccessRows, res.Errors)
	}
	if res := run(updater, "Changed"); res.SuccessRows != 1 || len(res.UpdatedIDs) != 1 {
		t.Fatalf("a role with update changing a record: %d ok %+v", res.SuccessRows, res.Errors)
	}
	if res := run("MXNoGrant"+suffix, "Again"); res.SuccessRows != 0 || !strings.Contains(res.Errors[0].Message, "may not change") {
		t.Fatalf("a role with no grant changing a record: %+v", res.Errors)
	}
	var name string
	db.DB.QueryRow("SELECT data->>'name' FROM "+schema+".documents WHERE doctype = 'Brand' AND data->>'code' = $1", code).Scan(&name)
	if name != "Changed" {
		t.Fatalf("stored name %q, want the updater's change only", name)
	}
}
