package server

// Stage 51.3's handler-level half. The engine-level half (override_count
// bookkeeping across a real switch) is engines.TestIndustryLockAndOverride,
// run against its own throwaway tenant schema to avoid the
// doctype_meta/doctype_fields pollution a real SwitchIndustryProfile call
// leaves behind in the shared tenant_default schema (see that test's own
// comment). This test only exercises handleSwitchIndustry's refusal path, so
// it never calls SwitchIndustryProfile at all - it seeds/removes a raw
// industry_lock row directly, leaving tenant_default exactly as it found it.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

func TestHandleSwitchIndustryRefusesWhenLocked(t *testing.T) {
	db.InitDB(testConnStr())

	userID, cleanupUser := seedStage47User(t, engines.RoleSuperAdmin, "HQ")
	defer cleanupUser()
	token := stage47Token(userID, engines.RoleSuperAdmin, "HQ")

	db.DB.Exec(`DELETE FROM tenant_default.industry_lock WHERE id = 1`)
	defer db.DB.Exec(`DELETE FROM tenant_default.industry_lock WHERE id = 1`)
	if _, err := db.DB.Exec(
		`INSERT INTO tenant_default.industry_lock (id, industry_code, set_by, set_at, override_count) VALUES (1, 'JEWELRY', 'someone-else', CURRENT_TIMESTAMP, 0)`,
	); err != nil {
		t.Fatalf("seed industry_lock: %v", err)
	}

	post := func(body map[string]interface{}) (int, map[string]interface{}) {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/industry", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		apiMiddleware(handleSwitchIndustry)(rec, req)
		var resp map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return rec.Code, resp
	}

	// Plain switch while locked: refused, no override attempted.
	if code, resp := post(map[string]interface{}{"industry_code": "FOOD_BEV"}); code != http.StatusConflict {
		t.Fatalf("plain switch while locked: status = %d, body = %v, want 409", code, resp)
	}

	// Override without a reason: rejected before it can touch anything.
	if code, resp := post(map[string]interface{}{"industry_code": "FOOD_BEV", "override": true}); code != http.StatusUnprocessableEntity {
		t.Fatalf("override with no reason: status = %d, body = %v, want 422", code, resp)
	}

	// A GET to the lock-status endpoint reflects the seeded row.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/industry/lock", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getRec := httptest.NewRecorder()
	apiMiddleware(handleGetIndustryLock)(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET lock status: status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
	var lock engines.IndustryLockStatus
	if err := json.Unmarshal(getRec.Body.Bytes(), &lock); err != nil {
		t.Fatalf("decode lock status: %v", err)
	}
	if !lock.Locked || lock.IndustryCode != "JEWELRY" || lock.SetBy != "someone-else" {
		t.Fatalf("unexpected lock status: %+v", lock)
	}
}
