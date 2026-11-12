package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Stage 148.48: the PO screen's Save to store sends the state and, for a store
// with no Legal Entity, the Legal Entity picked beside it. The store keeps its
// rule (no state-only save), and the same save follows the role's edit right.
func TestStage14848SaveToStoreNeedsLegalEntityOnlyWhenMissing(t *testing.T) {
	db.InitDB(testConnStr())
	cleanup := func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id LIKE 'T14848-%'`)
	}
	cleanup()
	defer cleanup()
	for _, row := range []string{
		`('T14848-LE', 'LegalEntity', '{"code":"T14848-LE","legal_name":"T14848 Pvt Ltd","status":"Active"}', 'Active')`,
		`('T14848-ST', 'Location', '{"code":"T14848-ST","name":"T14848 store","type":"Store","status":"Active"}', 'Active')`,
	} {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ` + strings.TrimSuffix(row, ")") + `, 'admin')`); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	admin := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	noEdit, cleanupU := seedStage47User(t, "Employee Self-Service", "HO")
	defer cleanupU()
	engines.ResetLiveUserStateCache()
	save := func(token string, body map[string]interface{}) int {
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/doc/Location/T14848-ST", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.SetPathValue("doctype", "Location")
		req.SetPathValue("id", "T14848-ST")
		rec := httptest.NewRecorder()
		apiMiddleware(handleGenericDoc)(rec, req)
		return rec.Code
	}
	if code := save(admin, map[string]interface{}{"state": "Karnataka"}); code != http.StatusUnprocessableEntity {
		t.Errorf("state only on a store with no Legal Entity: %d, want 422 (the store's rule stands)", code)
	}
	if code := save(admin, map[string]interface{}{"state": "Karnataka", "legal_entity": "T14848-LE"}); code != http.StatusOK {
		t.Errorf("state with a Legal Entity: %d, want 200", code)
	}
	if code := save(stage47Token(noEdit, "Employee Self-Service", "HO"), map[string]interface{}{"state": "Goa", "legal_entity": "T14848-LE"}); code != http.StatusForbidden {
		t.Errorf("a role that may not edit stores: %d, want 403", code)
	}
}

// The screen's Save to store step carries the picker (no JS runner in the
// tests, so the contract is read from the source).
func TestStage14848ScreenOffersLegalEntityPicker(t *testing.T) {
	src, err := os.ReadFile("../../public/view-procurement.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, want := range []string{"data-po-le=", "legal_entity: le", "Legal entity"} {
		if !strings.Contains(s, want) {
			t.Errorf("view-procurement.js lacks %q", want)
		}
	}
}
