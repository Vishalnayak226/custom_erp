package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
)

// 49.3.4: the payroll export cut to a store-limited user's stores lists only
// the employees placed in those stores; uncut it lists everyone, and an
// employee with no location is left out of a cut export (fail closed).
func TestStage4934PayrollExportTakesTheStoreCut(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id LIKE 'T4934P-%%'`, schema))
	}
	cleanup()
	t.Cleanup(cleanup)
	ins := func(id, doctype string, data string) {
		t.Helper()
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3::jsonb, 'Active', 'admin')`, schema), id, doctype, data); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	for _, e := range []struct{ id, loc string }{{"T4934P-EMP-A", "T4934P-STORE-A"}, {"T4934P-EMP-B", "T4934P-STORE-B"}, {"T4934P-EMP-N", ""}} {
		ins(e.id, "Employee", fmt.Sprintf(`{"code": %q, "location": %q}`, e.id, e.loc))
		ins(e.id+"-ATT", "Attendance", fmt.Sprintf(`{"employee_id": %q, "date": "2031-03-02", "status": "Present"}`, e.id))
	}
	ids := func(rows []PayrollExportEntry) map[string]bool {
		out := map[string]bool{}
		for _, r := range rows {
			out[r.EmployeeID] = true
		}
		return out
	}

	cutRows, err := GetPayrollExportForStores(tenantID, "2031-03-01", "2031-03-31", []string{"T4934P-STORE-A"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(cutRows); !got["T4934P-EMP-A"] || got["T4934P-EMP-B"] || got["T4934P-EMP-N"] || len(got) != 1 {
		t.Fatalf("cut export listed %v, want only T4934P-EMP-A", got)
	}
	all, err := GetPayrollExportForStores(tenantID, "2031-03-01", "2031-03-31", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(all); !got["T4934P-EMP-A"] || !got["T4934P-EMP-B"] || !got["T4934P-EMP-N"] {
		t.Fatalf("uncut export listed %v, want all three", got)
	}
	none, err := GetPayrollExportForStores(tenantID, "2031-03-01", "2031-03-31", []string{}, true)
	if err != nil || len(none) != 0 {
		t.Fatalf("a user with no stores got %d rows (err %v), want none", len(none), err)
	}
}
