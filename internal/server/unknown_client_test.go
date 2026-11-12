package server

import (
	"bytes"
	"custom_erp/db"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 49.3.2a (user, 2026-10-28: "Fix it"): a sign-in naming a client code nobody
// registered is refused with SAAS-0188 - it used to land on the default
// client. The default client and a registered one still reach the handler.
func TestStage4932aSignInWithUnknownClientCodeIsRefused(t *testing.T) {
	db.InitDB(testConnStr())
	login := func(header, query string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"username": "t4932a-nobody", "password": "wrong-password-4932a"})
		path := "/api/v1/login"
		if query != "" {
			path += "?tenant_id=" + query
		}
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if header != "" {
			req.Header.Set("X-Tenant-ID", header)
		}
		req.RemoteAddr = mfaTestIP() + ":4932"
		rec := httptest.NewRecorder()
		apiMiddleware(handleLogin)(rec, req)
		return rec
	}
	code := func(rec *httptest.ResponseRecorder) string {
		var env struct {
			Code  string `json:"code"`
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		json.Unmarshal(rec.Body.Bytes(), &env)
		if env.Code != "" {
			return env.Code
		}
		return env.Error.Code
	}

	for _, tc := range []struct{ header, query string }{{"t4932a-mistyped", ""}, {"", "t4932a-mistyped"}} {
		rec := login(tc.header, tc.query)
		if rec.Code != http.StatusUnprocessableEntity || code(rec) != "SAAS-0188" {
			t.Errorf("unknown client %+v: %d %s, want 422 SAAS-0188", tc, rec.Code, rec.Body.String())
		}
	}
	// The default client still reaches the sign-in handler (a wrong password,
	// not an unknown client).
	if rec := login("default", ""); rec.Code == http.StatusUnprocessableEntity && code(rec) == "SAAS-0188" {
		t.Errorf("default client refused as unknown: %s", rec.Body.String())
	}
	// A registered client does too.
	const id = "t4932a_known"
	db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, id)
	if _, err := db.DB.Exec(`INSERT INTO public.tenants (tenant_id, schema_name, name) VALUES ($1, 'tenant_t4932a_known', 'Stage 49.3.2a')`, id); err != nil {
		t.Fatalf("register client: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, id)
	if rec := login(id, ""); code(rec) == "SAAS-0188" {
		t.Errorf("registered client refused as unknown: %s", rec.Body.String())
	}
}
