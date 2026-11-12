package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Stage 47.12.6: a sandbox client reads as a simulated "SANDBOX" environment,
// so the shell's existing banner marks it with no new UI code.
func TestEnvironmentEndpointMarksASandbox(t *testing.T) {
	db.InitDB(testConnStr())
	t.Setenv("ENV", "production")

	read := func(tenant string) map[string]interface{} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/environment", nil)
		req.Header.Set("Resolved-Tenant-ID", tenant)
		rec := httptest.NewRecorder()
		handleGetEnvironment(rec, req)
		var out map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%v: %s", err, rec.Body.String())
		}
		return out
	}

	id, schema, _, err := engines.ProvisionSandboxTenant("0.0-test", 5)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.DB.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
		db.DB.Exec("DELETE FROM public.tenants WHERE tenant_id = $1", id)
	}()

	sb := read(id)
	if sb["env"] != "SANDBOX" || sb["sandbox"] != true || sb["external_side_effects"] != false {
		t.Errorf("sandbox client: %v", sb)
	}
	real := read("default")
	if real["env"] == "SANDBOX" || real["sandbox"] == true {
		t.Errorf("a real client was marked a sandbox: %v", real)
	}
}
