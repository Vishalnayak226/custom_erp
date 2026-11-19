package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 127.7: q_fields=@identity - what every link picker now sends - finds
// a record by its code, name or number, not by its status or category (the
// user's 94.24 bug, swept to every picker). The plain q keeps the any-field
// search for every other caller.
func TestSearchIdentityScope(t *testing.T) {
	db.InitDB(testConnStr())
	ids := []string{"T1277-KADA", "T1277-RING"}
	cleanup := func() {
		for _, id := range ids {
			_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
		}
	}
	cleanup()
	defer cleanup()
	for id, data := range map[string]string{
		"T1277-KADA": `{"code":"T1277-KADA","name":"Antique Kada","barcode":"QZXQ1277","status":"Active"}`,
		"T1277-RING": `{"code":"T1277-RING","name":"Plain Ring","category":"qzxq-cat","status":"Active"}`,
	} {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')`, id, data); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	engines.ResetLiveUserStateCache()
	list := func(query string) []string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item?"+query, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.SetPathValue("doctype", "Item")
		rec := httptest.NewRecorder()
		apiMiddleware(handleGenericDoc)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d %s", query, rec.Code, rec.Body.String())
		}
		var rows []map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &rows)
		var got []string
		for _, r := range rows {
			if c, _ := r["code"].(string); c == "T1277-KADA" || c == "T1277-RING" {
				got = append(got, c)
			}
		}
		return got
	}
	if got := list("q=qzxq&limit=50"); len(got) != 2 {
		t.Errorf("any-field search finds both: %v", got)
	}
	if got := list("q=qzxq&q_fields=@identity&limit=50"); len(got) != 1 || got[0] != "T1277-KADA" {
		t.Errorf("@identity finds the barcode, not the category: %v", got)
	}
	fields := engines.SearchIdentityFields("default", "Item")
	for _, f := range fields {
		if f == "status" || f == "category" {
			t.Errorf("@identity must not include %s: %v", f, fields)
		}
	}
}

// 230.3: a doctype's own code is identity, so a picker finds a bin by the
// bin_code people know (and the Bin picker shows), not only by its generated id.
func TestSearchIdentityOwnCode(t *testing.T) {
	db.InitDB(testConnStr())
	const id = "T2303-BIN"
	cleanup := func() { _, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id) }
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', '{"bin_code":"QZXQ2303-A1","zone":"qzxq2303-zone","status":"Active"}', 'Active', 'system')`, id); err != nil {
		t.Fatalf("insert bin: %v", err)
	}
	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	engines.ResetLiveUserStateCache()
	found := func(query string) bool {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Bin?"+query, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.SetPathValue("doctype", "Bin")
		rec := httptest.NewRecorder()
		apiMiddleware(handleGenericDoc)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d %s", query, rec.Code, rec.Body.String())
		}
		var rows []map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &rows)
		for _, r := range rows {
			if r["id"] == id {
				return true
			}
		}
		return false
	}
	if !found("q=QZXQ2303-A1&q_fields=@identity&limit=50") {
		t.Errorf("@identity must find a bin by its bin_code")
	}
	if found("q=qzxq2303-zone&q_fields=@identity&limit=50") {
		t.Errorf("@identity must not find a bin by its zone")
	}
	for doctype, want := range map[string]string{"Bin": "bin_code", "LPN": "lpn_code", "Hold": "hold_code"} {
		has := false
		for _, f := range engines.SearchIdentityFields("default", doctype) {
			has = has || f == want
		}
		if !has {
			t.Errorf("%s @identity must include %s: %v", doctype, want, engines.SearchIdentityFields("default", doctype))
		}
	}
}
