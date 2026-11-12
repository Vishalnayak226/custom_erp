package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Stage 65.6: Pause/Resume and the free trial from the Control Panel -
// platform operators only, never on the operator's own client.
func TestControlPanelPauseResumeAndTrial(t *testing.T) {
	db.InitDB(testConnStr())
	t.Setenv("ERP_PLATFORM_OPERATORS", "default:ops")

	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	tenantID, schema := "cptrial"+suffix, "tenant_cptrial"+suffix
	defer func() {
		db.DB.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
		db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantID)
		db.DB.Exec(`DELETE FROM public.tenant_lifecycle_events WHERE tenant_id = $1`, tenantID)
		engines.InvalidateTenantLifecycleCache(tenantID)
	}()
	if _, err := engines.ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	call := func(user, target, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/platform/tenants/"+target+"/"+path, strings.NewReader(body))
		r.SetPathValue("tenant", target)
		r.Header.Set("Resolved-Tenant-ID", "default")
		r.Header.Set("Resolved-Username", user)
		r.Header.Set("Resolved-Role", engines.RoleSuperAdmin)
		rec := httptest.NewRecorder()
		if path == "status" {
			handleControlPanelTenantStatus(rec, r)
		} else {
			handleControlPanelTenantTrial(rec, r)
		}
		return rec
	}
	expect := func(rec *httptest.ResponseRecorder, want int, what string) {
		t.Helper()
		if rec.Code != want {
			t.Fatalf("%s: status %d, want %d; body %s", what, rec.Code, want, rec.Body.String())
		}
	}

	expect(call("someone", tenantID, "status", `{"action":"pause","reason":"x"}`), http.StatusForbidden, "non-operator pause")
	expect(call("someone", tenantID, "trial", `{"days":30}`), http.StatusForbidden, "non-operator trial")
	expect(call("ops", "default", "status", `{"action":"pause","reason":"x"}`), http.StatusConflict, "pausing own client")
	expect(call("ops", "default", "trial", `{"days":30}`), http.StatusConflict, "trial on own client")
	expect(call("ops", "no_such_client"+suffix, "status", `{"action":"pause","reason":"x"}`), http.StatusNotFound, "unknown client")
	expect(call("ops", tenantID, "status", `{"action":"pause","reason":"  "}`), http.StatusUnprocessableEntity, "pause without a reason")
	expect(call("ops", tenantID, "status", `{"action":"delete","reason":"x"}`), http.StatusUnprocessableEntity, "unknown action")

	expect(call("ops", tenantID, "status", `{"action":"pause","reason":"non-payment"}`), http.StatusOK, "pause")
	if ok, _ := engines.TenantIsOperational(tenantID); ok {
		t.Error("paused client is still operational")
	}
	expect(call("ops", tenantID, "status", `{"action":"pause","reason":"again"}`), http.StatusConflict, "pausing a paused client")
	expect(call("ops", tenantID, "status", `{"action":"resume","reason":"paid"}`), http.StatusOK, "resume")
	if ok, _ := engines.TenantIsOperational(tenantID); !ok {
		t.Error("resumed client is not operational")
	}

	expect(call("ops", tenantID, "trial", `{}`), http.StatusUnprocessableEntity, "trial without days")
	expect(call("ops", tenantID, "trial", `{"days":400}`), http.StatusUnprocessableEntity, "trial too long")
	rec := call("ops", tenantID, "trial", `{"days":30}`)
	expect(rec, http.StatusOK, "start trial")
	var got struct {
		TrialEndsAt *time.Time `json:"trial_ends_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.TrialEndsAt == nil {
		t.Fatalf("trial response %s: %v", rec.Body.String(), err)
	}

	cp, err := engines.GetControlPanel()
	if err != nil {
		t.Fatal(err)
	}
	var row *engines.ControlPanelClient
	for i := range cp.Clients {
		if cp.Clients[i].TenantID == tenantID {
			row = &cp.Clients[i]
		}
	}
	if row == nil || row.TrialEndsAt == nil || row.TrialEnded {
		t.Fatalf("control panel row = %+v, want a running trial", row)
	}

	expect(call("ops", tenantID, "trial", `{"days":0}`), http.StatusOK, "mark paid")
	if life, _ := engines.GetTenantLifecycle(tenantID); life.Status != engines.TenantStatusActive {
		t.Errorf("status after mark paid = %q", life.Status)
	}
	var ends *time.Time
	if err := db.DB.QueryRow(`SELECT trial_ends_at FROM public.tenants WHERE tenant_id = $1`, tenantID).Scan(&ends); err != nil || ends != nil {
		t.Errorf("trial_ends_at after mark paid = %v, %v; want NULL", ends, err)
	}
}
