package engines

import (
	"custom_erp/db"
	"errors"
	"testing"
)

// Stage 75.3.3: a store-limited user's report shows only their stores' rows,
// and a run or an export asking for another store is refused; a role with
// company-wide store scope, and an internal run, see every store.
func TestStage7533ReportStoreScope(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID, user, reportID = "default", "manager1", "test-store-scope-7533"
	before, err := GetUserStoreAccess(tenantID, user)
	if err != nil {
		t.Skipf("no %s user in this database: %v", user, err)
	}
	if err := SetUserStoreAccess(tenantID, user, StoreAccessHome, nil, "test"); err != nil {
		t.Fatalf("pin %s to the home store: %v", user, err)
	}
	defer SetUserStoreAccess(tenantID, user, before.Access, before.Stores, "test")
	access, limited := reportStoreScope(tenantID, "Store Manager", user)
	if !limited || access.Home == "" {
		t.Fatalf("a Store Manager on their home store must be store-limited, got %+v limited=%v", access, limited)
	}
	home := access.Home

	if _, ok := reportRegistry[reportID]; !ok {
		RegisterReport(ReportDefinition{ID: reportID, Label: "Store scope test", Category: "Inventory",
			Columns: []ReportColumn{{Key: "location", Label: "Store"}, {Key: "qty", Label: "Qty"}},
			Params:  []ReportParam{{Key: "location", Label: "Store", Type: "text"}},
			Run: func(string, map[string]string) ([]map[string]interface{}, error) {
				return []map[string]interface{}{{"location": home, "qty": 1}, {"location": "OTHER-7533", "qty": 2}, {"location": "", "qty": 3}}, nil
			}})
	}
	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}

	_, rows, _, err := RunReport(tenantID, reportID, "Store Manager", user, nil)
	if err != nil || len(rows) != 1 || rows[0]["location"] != home {
		t.Fatalf("store-limited run: want only %s's row, got %v (%v)", home, rows, err)
	}
	if _, _, _, err := RunReport(tenantID, reportID, "Store Manager", user, map[string]string{"location": "OTHER-7533"}); code(err) != "REPORT-0987" {
		t.Fatalf("asking for another store: want REPORT-0987, got %v", err)
	}
	if _, err := CreateReportExportJob(tenantID, reportID, "Store Manager", map[string]string{"location": "OTHER-7533"}, user); code(err) != "REPORT-0987" {
		t.Fatalf("exporting another store: want REPORT-0987, got %v", err)
	}
	if _, rows, _, err := RunReport(tenantID, reportID, "Accountant", "acc-7533", nil); err != nil || len(rows) != 3 {
		t.Fatalf("a role with company-wide store scope sees every store: %v (%v)", rows, err)
	}
	if _, rows, _, err := RunReport(tenantID, reportID, "Store Manager", "", nil); err != nil || len(rows) != 3 {
		t.Fatalf("an internal run (no user) is not cut: %v (%v)", rows, err)
	}
}
