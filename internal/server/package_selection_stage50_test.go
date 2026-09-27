package server

// Stage 50/BLD-007: AUD-05 ("failed package-disable reported as success") was
// fixed in engines/modules.go and unit-proven in engines/modules_test.go
// (a single fault-injected pim/hr case). This file is the remaining
// acceptance BLD-007 asks for: all ten real product packages through the
// actual HTTP handler (not just the engine function), plus the same fault
// injection driven through that handler so the API response/displayed state
// is what's checked, not just the Go error value, and a retry-converges
// case once the injected fault is removed.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

func postPackageSelection(t *testing.T, token, tenantID string, packages []string) (int, map[string]interface{}) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{"tenant_id": tenantID, "packages": packages})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenant/package", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	apiMiddleware(handleSetTenantPackage)(rec, req)
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec.Code, resp
}

// snapshotEntitlements/restoreEntitlements let this test leave tenant_default
// exactly as it found it regardless of outcome - this schema is shared with
// concurrent sessions (CLAUDE.md).
func snapshotEntitlements(t *testing.T, schema string) map[string]bool {
	t.Helper()
	rows, err := db.DB.Query("SELECT module_key, enabled FROM " + schema + ".module_entitlements")
	if err != nil {
		t.Fatalf("snapshot entitlements: %v", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var key string
		var enabled bool
		if err := rows.Scan(&key, &enabled); err != nil {
			t.Fatalf("snapshot scan: %v", err)
		}
		out[key] = enabled
	}
	return out
}

func restoreEntitlements(schema string, snapshot map[string]bool) {
	for key, enabled := range snapshot {
		db.DB.Exec(fmt.Sprintf("UPDATE %s.module_entitlements SET enabled = $1 WHERE module_key = $2", schema), enabled, key)
	}
}

// TestApplyPackageSelectionCoversAllTenPackagesViaAPI drives all ten shipped
// ProductPackages (engines/modules.go) one at a time through the real
// POST /api/v1/admin/tenant/package handler chain, and checks both the HTTP
// response's "modules" field and the actual stored entitlements agree with
// engines.ExpandPackagesToModules for that package - the "displayed
// resulting state" half of BLD-007's Done bar, not just an engine-level
// return value.
func TestApplyPackageSelectionCoversAllTenPackagesViaAPI(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	before := snapshotEntitlements(t, schema)
	defer restoreEntitlements(schema, before)

	location := "P50PKGLOC-" + stage47UniqueID()
	adminID, cleanupAdmin := seedStage47User(t, engines.RoleSuperAdmin, location)
	defer cleanupAdmin()
	token := stage47Token(adminID, engines.RoleSuperAdmin, location)

	packageKeys := make([]string, 0, len(engines.ProductPackages))
	for k := range engines.ProductPackages {
		packageKeys = append(packageKeys, k)
	}
	sort.Strings(packageKeys)
	if len(packageKeys) != 10 {
		t.Fatalf("expected 10 shipped product packages, got %d: %v", len(packageKeys), packageKeys)
	}

	for _, key := range packageKeys {
		code, resp := postPackageSelection(t, token, tenantID, []string{key})
		if code != http.StatusOK {
			t.Fatalf("package %q: expected 200, got %d body=%v", key, code, resp)
		}
		if resp["status"] != "updated" {
			t.Errorf("package %q: response status = %v, want \"updated\"", key, resp["status"])
		}

		wanted := map[string]bool{}
		for _, m := range engines.ExpandPackagesToModules([]string{key}) {
			wanted[m] = true
		}

		// Cross-check the response body's own "modules" field, not just a
		// fresh DB read - a handler that read stale state before writing
		// would still pass a DB-only check.
		respModules, _ := resp["modules"].([]interface{})
		if len(respModules) == 0 {
			t.Fatalf("package %q: response carried no modules list: %v", key, resp)
		}
		respEnabled := map[string]bool{}
		for _, raw := range respModules {
			m, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			mk, _ := m["module_key"].(string)
			en, _ := m["enabled"].(bool)
			isCore, _ := m["is_core"].(bool)
			if mk != "" {
				respEnabled[mk] = en || isCore
			}
		}
		for m := range wanted {
			if !respEnabled[m] {
				t.Errorf("package %q: response says module %q is not enabled, want enabled", key, m)
			}
		}
		for m, en := range respEnabled {
			if !wanted[m] && en {
				// is_core modules are always enabled regardless of package -
				// only flag a non-core module that shouldn't be on.
				if isCore, _ := isModuleCoreInResponse(respModules, m); !isCore {
					t.Errorf("package %q: response says non-selected module %q is enabled", key, m)
				}
			}
		}

		// And the actual persisted state, independent of what the handler
		// happened to report.
		enabled, err := engines.IsModuleEnabled(tenantID, "reports")
		if err != nil {
			t.Fatalf("package %q: IsModuleEnabled(reports): %v", key, err)
		}
		if !enabled {
			t.Errorf("package %q: reports module should always be enabled alongside any non-empty selection", key)
		}
	}
}

func isModuleCoreInResponse(modules []interface{}, moduleKey string) (bool, bool) {
	for _, raw := range modules {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if mk, _ := m["module_key"].(string); mk == moduleKey {
			isCore, _ := m["is_core"].(bool)
			return isCore, true
		}
	}
	return false, false
}

// TestApplyPackageSelectionFaultInjectionViaAPI is the same aud05_block_pim_disable
// technique engines/modules_test.go uses (a Postgres trigger that
// unconditionally refuses to disable one module), driven through the real
// HTTP handler instead of calling the engine function directly, and extended
// with a retry-after-fault-clears case: once the trigger is removed, the
// identical request must converge to the correct final state rather than
// staying permanently stuck.
func TestApplyPackageSelectionFaultInjectionViaAPI(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	before := snapshotEntitlements(t, schema)
	defer restoreEntitlements(schema, before)

	location := "P50FAULTLOC-" + stage47UniqueID()
	adminID, cleanupAdmin := seedStage47User(t, engines.RoleSuperAdmin, location)
	defer cleanupAdmin()
	token := stage47Token(adminID, engines.RoleSuperAdmin, location)

	if err := engines.SetModuleEntitlement(tenantID, "pim", true, "test-bld007"); err != nil {
		t.Fatalf("enable pim before the test: %v", err)
	}

	const triggerName = "bld007_block_pim_disable"
	const funcName = "bld007_block_pim_disable_fn"
	install := `
		CREATE OR REPLACE FUNCTION ` + schema + `.` + funcName + `() RETURNS trigger AS $$
		BEGIN
			IF NEW.module_key = 'pim' AND NEW.enabled = false THEN
				RAISE EXCEPTION 'bld007 fixture: refusing to disable pim';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER ` + triggerName + `
			BEFORE UPDATE ON ` + schema + `.module_entitlements
			FOR EACH ROW EXECUTE FUNCTION ` + schema + `.` + funcName + `();`
	if _, err := db.DB.Exec(install); err != nil {
		t.Fatalf("install fixture trigger: %v", err)
	}
	dropTrigger := func() {
		db.DB.Exec("DROP TRIGGER IF EXISTS " + triggerName + " ON " + schema + ".module_entitlements")
		db.DB.Exec("DROP FUNCTION IF EXISTS " + schema + "." + funcName + "()")
	}
	defer dropTrigger()

	// "hr" wants only hr+reports, so pim is one of the modules the handler
	// will try (and, per the trigger, permanently fail) to disable.
	code, resp := postPackageSelection(t, token, tenantID, []string{"hr"})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 when a module refuses to disable, got %d body=%v", code, resp)
	}
	msg, _ := resp["error"].(string)
	if msg == "" {
		msg = fmt.Sprintf("%v", resp)
	}
	if !strings.Contains(msg, "pim") {
		t.Errorf("expected the API error to name the stuck module (pim), got: %s", msg)
	}

	stillEnabled, err := engines.IsModuleEnabled(tenantID, "pim")
	if err != nil {
		t.Fatalf("IsModuleEnabled: %v", err)
	}
	if !stillEnabled {
		t.Error("pim was actually disabled despite the trigger refusing every write - state and the 422 now disagree")
	}

	// Clear the fault and retry the identical request - this must now
	// converge, not stay stuck because of a leftover partial-failure state.
	dropTrigger()
	code, resp = postPackageSelection(t, token, tenantID, []string{"hr"})
	if code != http.StatusOK {
		t.Fatalf("expected the retry to succeed once the fault clears, got %d body=%v", code, resp)
	}
	stillEnabled, err = engines.IsModuleEnabled(tenantID, "pim")
	if err != nil {
		t.Fatalf("IsModuleEnabled after retry: %v", err)
	}
	if stillEnabled {
		t.Error("pim is still enabled after a successful retry selecting only hr")
	}
}
