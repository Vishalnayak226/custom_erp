package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStockAgeListWhenOffIsAnAnswer (193): the Quality screen asks for stock
// age inspections on every visit. With the check off, the list answers 200
// {"off": true} instead of a 422 in the error log; recording a decision while
// it is off is still refused with QUALIT-0602.
func TestStockAgeListWhenOffIsAnAnswer(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var adminID string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.users WHERE username = 'admin'`, schema)).Scan(&adminID); err != nil {
		t.Skipf("no admin user in the test database: %v", err)
	}
	old := engines.GetSettingString("default", engines.SettingAgeInspection)
	if err := engines.SetSetting("default", engines.SettingAgeInspection, "false", "test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engines.SetSetting("default", engines.SettingAgeInspection, old, "test") })

	call := func(method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/stock-age-inspections", strings.NewReader(body))
		r.Header.Set("Resolved-Tenant-ID", "default")
		r.Header.Set("Resolved-User-ID", adminID)
		r.Header.Set("Resolved-Role", "Super Admin")
		w := httptest.NewRecorder()
		handleStockAgeInspections(w, r)
		return w
	}
	if w := call("GET", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"off":true`) {
		t.Errorf("list while off: %d %s, want 200 with off:true", w.Code, w.Body.String())
	}
	if w := call("POST", `{"id":"SAI193-NONE","outcome":"Pass"}`); w.Code != 422 || !strings.Contains(w.Body.String(), "QUALIT-0602") {
		t.Errorf("decide while off: %d %s, want 422 QUALIT-0602", w.Code, w.Body.String())
	}
}
