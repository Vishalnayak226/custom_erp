package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"net/http"
	"strings"
	"testing"
)

// 245.11: a value the settings screen refuses is the caller's 4xx with the
// engine's own code and reason, and nothing in the batch is written. A bad
// Service SLA value used to pass the batch check, fail mid-save and come back
// as a 500 (GLOBAL-0302).
func TestSettingsSaveRefusesBadValueWithItsCode(t *testing.T) {
	db.InitDB(testConnStr())
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/settings", apiMiddleware(handleUpdateSettings))

	admin, drop := seedContractUser(t, engines.RoleAdmin, "HO")
	defer drop()
	token := engines.SignToken(admin, admin, engines.RoleAdmin, "default", "HO", currentCredentialVersion(admin))

	const escKey = engines.SettingServiceSLAEscalateTo
	before := engines.GetSettingString("default", escKey)
	defer func() {
		if engines.GetSettingString("default", escKey) != before {
			_ = engines.SetSetting("default", escKey, before, "test")
		}
	}()
	want := "user:t245-" + contractUniqueID()

	cases := []struct {
		name   string
		body   map[string]string
		status int
		code   string
		msg    string
	}{
		// A plain value check: refused before anything is written.
		{"sla hours", map[string]string{engines.SettingServiceSLAResponse: "Low=24,Medium=8,High=4,Critical=1-x", escKey: want}, http.StatusUnprocessableEntity, "FIN-1725", "whole hours"},
		// A change hook that needs the tenant (its roles): refused mid-save,
		// with the code its engine chose ("Approver not found").
		{"escalation role", map[string]string{engines.SettingApprovalEscalateToRole: "No Such Role 245"}, http.StatusNotFound, "APPROV-0157", "is not a role here"},
	}
	for _, c := range cases {
		rec := doRequest(t, mux.ServeHTTP, http.MethodPut, "/api/v1/admin/settings", token, c.body)
		b := rec.Body.String()
		if rec.Code != c.status || !strings.Contains(b, `"code":"`+c.code+`"`) || !strings.Contains(b, c.msg) {
			t.Errorf("%s: %d %s, want %d %s with %q", c.name, rec.Code, b, c.status, c.code, c.msg)
		}
	}
	if got := engines.GetSettingString("default", escKey); got == want {
		t.Errorf("the batch with a bad SLA value still saved %s=%s", escKey, got)
	}
}
