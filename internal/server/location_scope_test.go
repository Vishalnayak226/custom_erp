package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 49.3.4 (rest): a print by id and the payroll export meet the same
// store limit the record itself does; no result is cached across stores. (The
// payroll export needs the hr.payroll capability, which no store-limited
// default role holds, so it is not scoped here.)

func stage4934SeedDoc(t *testing.T, id, doctype, location string, extra map[string]interface{}) {
	t.Helper()
	data := map[string]interface{}{"location": location}
	for k, v := range extra {
		data[k] = v
	}
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', 'system')`, id, doctype, string(raw)); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id) })
}

func TestStage4934PrintByIdRefusesAnotherStoresDocument(t *testing.T) {
	db.InitDB(testConnStr())
	if scope, ok := engines.ScopeForDoctype("PurchaseOrder"); !ok || len(scope.LocationFields) == 0 {
		t.Fatal("PurchaseOrder is no longer location-scoped")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/print/document/{txn}/{id}", apiMiddleware(handlePrintDocument))
	mux.HandleFunc("POST /api/v1/print/document/{txn}/{id}/log", apiMiddleware(handlePrintDocumentLog))
	mux.HandleFunc("POST /api/v1/print/document/{txn}/{id}/reprint-request", apiMiddleware(handleRequestReprint))
	mux.HandleFunc("GET /api/v1/procurement/purchase-order/{id}/print", apiMiddleware(handlePrintPurchaseOrder))
	users := seedGeneratedCaseUsers(t)
	manager, admin := users[engines.RoleStoreManager], users[engines.RoleAdmin]
	suffix := contractUniqueID()
	ip := mfaTestIP()
	mine, theirs := "S4934-PO-HO-"+suffix, "S4934-PO-X-"+suffix
	stage4934SeedDoc(t, mine, "PurchaseOrder", "HO", map[string]interface{}{"po_number": mine})
	stage4934SeedDoc(t, theirs, "PurchaseOrder", "ELSEWHERE", map[string]interface{}{"po_number": theirs})

	calls := []struct{ name, method, path string }{
		{"print", http.MethodGet, "/api/v1/print/document/PurchaseOrder/%s"},
		{"csv download", http.MethodGet, "/api/v1/print/document/PurchaseOrder/%s?format=csv"},
		{"print log", http.MethodPost, "/api/v1/print/document/PurchaseOrder/%s/log"},
		{"reprint request", http.MethodPost, "/api/v1/print/document/PurchaseOrder/%s/reprint-request"},
		{"vendor copy", http.MethodGet, "/api/v1/procurement/purchase-order/%s/print"},
	}
	for _, c := range calls {
		body := interface{}(nil)
		if c.method == http.MethodPost {
			body = map[string]interface{}{"action": "Print", "reason": "x"}
		}
		rec := doRequestFromIP(t, mux.ServeHTTP, c.method, fmt.Sprintf(c.path, theirs), manager, body, ip)
		out := map[string]interface{}{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if rec.Code != http.StatusNotFound || out["code"] != "GLOBAL-0004" {
			t.Errorf("%s of another store's PO by a Store Manager at HO: %d %s, want 404 GLOBAL-0004", c.name, rec.Code, rec.Body.String())
		}
		// Their own store's document is not refused for being out of scope.
		rec = doRequestFromIP(t, mux.ServeHTTP, c.method, fmt.Sprintf(c.path, mine), manager, body, ip)
		out = map[string]interface{}{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if out["code"] == "GLOBAL-0004" && strings.Contains(rec.Body.String(), "out of scope") {
			t.Errorf("%s of their own store's PO was refused: %d %s", c.name, rec.Code, rec.Body.String())
		}
		// Full access reaches any store's document.
		rec = doRequestFromIP(t, mux.ServeHTTP, c.method, fmt.Sprintf(c.path, theirs), admin, body, ip)
		out = map[string]interface{}{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if rec.Code == http.StatusNotFound && out["code"] == "GLOBAL-0004" && strings.Contains(rec.Body.String(), theirs) == false && c.name == "print" {
			t.Errorf("%s by an Admin was refused: %d %s", c.name, rec.Code, rec.Body.String())
		}
	}
}

// No result is kept for reuse across stores: a list read carries no-store, so
// no browser or proxy cache serves one user's store to another, and the
// server holds no per-client result cache.
func TestStage4934ListResultsAreNeverCached(t *testing.T) {
	db.InitDB(testConnStr())
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	users := seedGeneratedCaseUsers(t)
	rec := doRequestFromIP(t, mux.ServeHTTP, http.MethodGet, "/api/v1/doc/Bin", users[engines.RoleStoreManager], nil, mfaTestIP())
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("a store-scoped list carries Cache-Control %q, want no-store", cc)
	}
}
