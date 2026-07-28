package server

import (
	"bytes"
	"custom_erp/db"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPurchaseRequisitionAutoNumberAndDescriptionCatalogue(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	const description = "__test_pr_handler_catalogue__ toner cartridge"
	const location = "__TEST_PR_HANDLER_LOCATION__"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM tenant_default.documents WHERE doctype = 'PurchaseRequisition' AND data->>'description' = $1", description)
		_, _ = db.DB.Exec("DELETE FROM tenant_default.documents WHERE doctype = 'PurchaseRequisitionDescription' AND data->>'description' = $1", description)
		_, _ = db.DB.Exec("DELETE FROM tenant_default.sequence_counters WHERE doc_type = 'PR' AND store_code = $1", location)
	}
	cleanup()
	defer cleanup()

	payload, _ := json.Marshal(map[string]interface{}{
		"code":         "requester-supplied-number-must-not-win",
		"description":  description,
		"quantity":     2,
		"department":   "Admin",
		"total_amount": 2500,
		"status":       "Draft",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/doc/PurchaseRequisition", bytes.NewReader(payload))
	req.Header.Set("Resolved-Tenant-ID", "default")
	req.Header.Set("Resolved-Role", "HR/Admin")
	req.Header.Set("Resolved-User-ID", "admin")
	req.Header.Set("Resolved-Location", location)
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/doc/{doctype}", handleGenericDoc)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create requisition status=%d body=%s", rec.Code, rec.Body.String())
	}

	var code string
	if err := db.DB.QueryRow("SELECT data->>'code' FROM tenant_default.documents WHERE doctype = 'PurchaseRequisition' AND data->>'description' = $1", description).Scan(&code); err != nil {
		t.Fatalf("read saved requisition: %v", err)
	}
	if code == "" || code == "requester-supplied-number-must-not-win" || !strings.Contains(code, location) {
		t.Fatalf("expected server-generated PR number containing %q, got %q", location, code)
	}
	var descriptionCount int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM tenant_default.documents WHERE doctype = 'PurchaseRequisitionDescription' AND data->>'description' = $1", description).Scan(&descriptionCount); err != nil {
		t.Fatalf("read description master: %v", err)
	}
	if descriptionCount != 1 {
		t.Fatalf("expected one saved description master, got %d", descriptionCount)
	}
}
