package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 2026-10-10: the generic create's upsert had two silent failures, found
// recording the SOP BOM chapter. Recreating a deleted record's code reported
// success while the row stayed deleted (and invisible), and a create whose id
// belonged to a different record type overwrote that record's data.
func TestGenericCreateUpsertGuards(t *testing.T) {
	db.InitDB(testConnStr())
	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	engines.ResetLiveUserStateCache()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	bomID := "BOM-UPSERT-" + suffix
	otherID := "XTYPE-UPSERT-" + suffix
	itemID := "FG-UPSERT-" + suffix
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id IN ($1, $2, $3)`, bomID, otherID, itemID)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'admin')`, itemID, fmt.Sprintf(`{"code":%q,"name":"Upsert guard item"}`, itemID)); err != nil {
		t.Fatal(err)
	}

	createBOM := func(id string, qty int) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]interface{}{
			"id": id, "code": id, "parent_item": itemID,
			"components": fmt.Sprintf(`[{"sku":"RM-UPSERT","qty":%d}]`, qty), "status": "Active",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/doc/BOM", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.SetPathValue("doctype", "BOM")
		rec := httptest.NewRecorder()
		apiMiddleware(handleGenericDoc)(rec, req)
		return rec
	}

	t.Run("RecreatingADeletedCodeBringsItBack", func(t *testing.T) {
		if rec := createBOM(bomID, 1); rec.Code != http.StatusOK {
			t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
		}
		if _, err := db.DB.Exec(`UPDATE tenant_default.documents SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, bomID); err != nil {
			t.Fatal(err)
		}
		if rec := createBOM(bomID, 3); rec.Code != http.StatusOK {
			t.Fatalf("recreate: %d %s", rec.Code, rec.Body.String())
		}
		var deleted bool
		var components string
		if err := db.DB.QueryRow(`SELECT deleted_at IS NOT NULL, data->>'components' FROM tenant_default.documents WHERE id = $1`, bomID).Scan(&deleted, &components); err != nil {
			t.Fatal(err)
		}
		if deleted || components != `[{"sku":"RM-UPSERT","qty":3}]` {
			t.Errorf("want the record back with the new data, got deleted=%v components=%s", deleted, components)
		}
	})

	t.Run("ACreateNeverOverwritesAnotherRecordType", func(t *testing.T) {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Brand', '{"name":"Keep Me"}', 'Active', 'admin')`, otherID); err != nil {
			t.Fatal(err)
		}
		if rec := createBOM(otherID, 1); rec.Code != http.StatusConflict {
			t.Fatalf("want 409 for an id held by another record type, got %d %s", rec.Code, rec.Body.String())
		}
		var doctype, name string
		if err := db.DB.QueryRow(`SELECT doctype, data->>'name' FROM tenant_default.documents WHERE id = $1`, otherID).Scan(&doctype, &name); err != nil {
			t.Fatal(err)
		}
		if doctype != "Brand" || name != "Keep Me" {
			t.Errorf("the Brand record was changed: doctype=%s name=%s", doctype, name)
		}
	})
}
