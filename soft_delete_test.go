package main

import (
	"custom_erp/db"
	"custom_erp/engines"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenericSoftDeleteAndMasterReactivation(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	const brandID = "SOFT-DELETE-BRAND"
	const contentID = "SOFT-DELETE-CONTENT"
	_, _ = db.DB.Exec("DELETE FROM tenant_default.documents WHERE id IN ($1, $2)", brandID, contentID)
	defer db.DB.Exec("DELETE FROM tenant_default.documents WHERE id IN ($1, $2)", brandID, contentID)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Brand', '{"code":"SOFT-DELETE-BRAND","name":"Soft Delete"}', 'Active', 'system'), ($2, 'ProductContent', '{"code":"SOFT-DELETE-CONTENT","product_id":"SOFT-DELETE-BRAND","language":"en","title":"Approved content"}', 'Approved', 'system')`, brandID, contentID); err != nil {
		t.Fatalf("seed soft-delete fixtures: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("POST /api/v1/doc/{doctype}/{id}/reactivate", apiMiddleware(handleReactivateMasterDocument))
	token := engines.SignToken("system", "system", "HR/Admin", "default", "HO")
	request := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	if rec := request(http.MethodDelete, "/api/v1/doc/Brand/"+brandID); rec.Code != http.StatusOK {
		t.Fatalf("soft delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := request(http.MethodGet, "/api/v1/doc/Brand/"+brandID); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted Brand GET status=%d, want 404", rec.Code)
	}
	var deletedAt interface{}
	if err := db.DB.QueryRow("SELECT deleted_at FROM tenant_default.documents WHERE id=$1", brandID).Scan(&deletedAt); err != nil || deletedAt == nil {
		t.Fatalf("expected tombstone after delete: value=%v err=%v", deletedAt, err)
	}
	if rec := request(http.MethodPost, "/api/v1/doc/Brand/"+brandID+"/reactivate"); rec.Code != http.StatusOK {
		t.Fatalf("reactivate status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := request(http.MethodGet, "/api/v1/doc/Brand/"+brandID); rec.Code != http.StatusOK {
		t.Fatalf("reactivated Brand GET status=%d", rec.Code)
	}
	if rec := request(http.MethodDelete, "/api/v1/doc/ProductContent/"+contentID); rec.Code != http.StatusBadRequest {
		t.Fatalf("approved transaction delete status=%d, want 400", rec.Code)
	}
}
