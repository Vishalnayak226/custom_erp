package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStage140141DuplicateSaveIsAReadableRefusal: a second Reorder Point
// Config for an item and location that already has one is refused with a 409
// that names the fields, not a 500 GLOBAL-0302.
func TestStage140141DuplicateSaveIsAReadableRefusal(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	clean := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ReorderPointConfig' AND data->>'item' = 'S140141-ITEM'")
	}
	clean()
	defer clean()
	save := func(code string) (int, string) {
		raw, _ := json.Marshal(map[string]interface{}{"code": code, "item": "S140141-ITEM", "location_code": "S140141-LOC", "lead_time_days": 2, "safety_stock_qty": 5, "status": "Active"})
		r := httptest.NewRequest("POST", "/api/v1/doc/ReorderPointConfig", strings.NewReader(string(raw)))
		r.SetPathValue("doctype", "ReorderPointConfig")
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		r.Header.Set("Resolved-Role", engines.RoleSuperAdmin)
		r.Header.Set("Resolved-User-ID", "admin")
		r.Header.Set("Resolved-Username", "admin")
		r.Header.Set("Resolved-Location", "HO")
		w := httptest.NewRecorder()
		handleGenericDoc(w, r)
		return w.Code, w.Body.String()
	}
	if code, body := save("S140141-A"); code != 200 {
		t.Fatalf("first save: %d %s", code, body)
	}
	code, body := save("S140141-B")
	if code != 409 || !strings.Contains(body, "MASTER-1321") {
		t.Fatalf("duplicate save: %d %s, want 409 MASTER-1321", code, body)
	}
	for _, want := range []string{"Reorder Point Config", "Item", "Location"} {
		if !strings.Contains(body, want) {
			t.Errorf("refusal %s does not name %q", body, want)
		}
	}
	if strings.Contains(body, "GLOBAL-0302") || strings.Contains(body, "pq:") || strings.Contains(body, "idx_documents") {
		t.Errorf("refusal leaks a 500 or driver text: %s", body)
	}
}
