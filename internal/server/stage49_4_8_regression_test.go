package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Stage 49.4.8: browser / API security regressions not covered elsewhere -
// CORS that never reflects a stranger's origin, method-override headers that
// are ignored, SQL / filter injection through the record list, and server-kept
// fields that a create cannot set. (DOM XSS: 134.8; uploads and traversal:
// 134.4; oversized bodies: 49.4.4; clickjacking and sniffing: 49.4.5.)
func TestStage49BrowserAPIRegressions(t *testing.T) {
	db.InitDB(testConnStr())
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	token := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))
	send := func(method, path string, body interface{}, header map[string]string) *httptest.ResponseRecorder {
		var rd *strings.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		} else {
			rd = strings.NewReader("")
		}
		req := httptest.NewRequest(method, path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = mfaTestIP() + ":40000"
		for k, v := range header {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// CORS: a stranger's origin is never echoed back.
	if rec := send("GET", "/api/v1/doc/Brand?limit=1", nil, map[string]string{"Origin": "https://evil.example"}); rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("CORS reflected an unlisted origin: %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}

	// A record to aim at.
	const id = "SEC4948-DEPT"
	db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Department', '{"code":"SEC4948-DEPT","name":"Sec 49.4.8","status":"Active"}', 'Active', 'admin')`, id); err != nil {
		t.Fatal(err)
	}

	// Method override: a GET that asks to be a DELETE stays a GET.
	for _, h := range []string{"X-HTTP-Method-Override", "X-Method-Override", "X-HTTP-Method"} {
		send("GET", "/api/v1/doc/Department/"+id, nil, map[string]string{h: "DELETE"})
	}
	var alive int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.documents WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&alive)
	if alive != 1 {
		t.Fatal("a method-override header deleted the record")
	}

	// SQL / filter injection: a hostile search is just text; a hostile filter
	// name is refused; neither is a server error or leaks every row.
	rec := send("GET", "/api/v1/doc/Department?q="+url.QueryEscape("' OR '1'='1")+"&count=true", nil, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Total-Count") != "0" {
		t.Errorf("an injection-shaped search: %d, total %s; want 200 and no match", rec.Code, rec.Header().Get("X-Total-Count"))
	}
	if rec := send("GET", "/api/v1/doc/Department?"+url.QueryEscape("name' OR '1'='1")+"=x", nil, nil); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("an injection-shaped filter name: %d, want 422", rec.Code)
	}

	// Mass assignment: who made a record and its version are the server's.
	const newID = "SEC4948-DEPT-NEW"
	db.DB.Exec(`DELETE FROM tenant_default.documents WHERE data->>'name' = 'Sec 49.4.8 new'`)
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE data->>'name' = 'Sec 49.4.8 new'`)
	created := send("POST", "/api/v1/doc/Department", map[string]interface{}{"code": newID, "name": "Sec 49.4.8 new", "status": "Active", "created_by": "someone-else", "version": 99}, nil)
	if created.Code >= 300 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var createdBy string
	var version int
	db.DB.QueryRow(`SELECT created_by, version FROM tenant_default.documents WHERE data->>'name' = 'Sec 49.4.8 new'`).Scan(&createdBy, &version)
	if createdBy != "admin" || version == 99 {
		t.Errorf("a create set server-kept fields: created_by %q, version %d", createdBy, version)
	}
}
