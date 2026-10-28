package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 139.3: doc-find names the record type holding a number - and only to
// a session that could open that record itself. Anything else, including a
// number held at a store the session cannot see, is "not found".
func TestStage139DocFindAnswersOnlyWhatTheSessionCanOpen(t *testing.T) {
	db.InitDB(testConnStr())
	const id = "T139/HO/26-27/00042"
	db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesInvoice', $2, 'Draft', 'admin')`,
		id, `{"invoice_number":"T139/HO/26-27/00042","customer":"T139 Customer","location":"HO","total_amount":10}`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/doc-find", apiMiddleware(handleDocFind))
	find := func(token, number string) (int, map[string]string) {
		rec := doRequest(t, mux.ServeHTTP, http.MethodGet, "/api/v1/doc-find?id="+url.QueryEscape(number), token, nil)
		var body map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}

	admin := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))
	if code, body := find(admin, id); code != http.StatusOK || body["doctype"] != "SalesInvoice" || body["id"] != id {
		t.Fatalf("Super Admin finding %s: %d %v, want 200 SalesInvoice", id, code, body)
	}
	if code, _ := find(admin, "T139/NO/SUCH/1"); code != http.StatusNotFound {
		t.Errorf("an unknown number: %d, want 404", code)
	}

	here, cleanHere := seedStage47User(t, engines.RoleStoreManager, "HO")
	defer cleanHere()
	if code, body := find(stage47Token(here, engines.RoleStoreManager, "HO"), id); code != http.StatusOK || body["doctype"] != "SalesInvoice" {
		t.Errorf("a Store Manager at the invoice's own store: %d %v, want 200", code, body)
	}
	there, cleanThere := seedStage47User(t, engines.RoleStoreManager, "T139X")
	defer cleanThere()
	if code, body := find(stage47Token(there, engines.RoleStoreManager, "T139X"), id); code != http.StatusNotFound || body["doctype"] != "" {
		t.Errorf("a Store Manager at another store: %d %v, want 404 with no record type named", code, body)
	}
}
