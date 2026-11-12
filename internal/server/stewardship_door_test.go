package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"

	"github.com/lib/pq"
)

// Review of 47.13.4: through the real save and delete doors, a client's claim
// about where a value came from is ignored, and the register cannot be edited.
func TestStewardshipDoorsIgnoreTheClaim(t *testing.T) {
	db.InitDB(testConnStr())
	const attr, item = "QA_STW_47134", "QA-STW-ITM"
	valueID := engines.StewardshipValueID(item, attr)
	entryID := engines.StewardshipEntryID(attr)
	clean := func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = ANY($1)`, pq.Array([]string{attr, item, valueID, entryID}))
	}
	clean()
	t.Cleanup(clean)
	insert := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')`, id, doctype, raw); err != nil {
			t.Fatal(err)
		}
	}
	insert(attr, "ProductAttributeDef", map[string]interface{}{"code": attr, "label": "QA", "value_type": "Data", "status": "Active"})
	insert(item, "Item", map[string]interface{}{"code": item, "name": "QA", "status": "Active"})
	insert(valueID, "ProductAttributeValue", map[string]interface{}{"code": valueID, "item": item, "attribute": attr, "value": "Red", "status": "Active"})
	insert(entryID, engines.StewardshipDoctype, map[string]interface{}{"code": entryID, "attribute": attr, "system_of_record": "Person", "system_ref": "alice",
		"data_owner": "alice", "data_steward": "alice", "confidence": "High", "survivorship": "System of record wins", "status": "Active"})

	engines.ResetLiveUserStateCache()
	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	call := func(method, doctype, id string, body map[string]interface{}) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		path := "/api/v1/doc/" + doctype
		if id != "" {
			path += "/" + id
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", "default")
		req.SetPathValue("doctype", doctype)
		if id != "" {
			req.SetPathValue("id", id)
		}
		rec := httptest.NewRecorder()
		apiMiddleware(handleGenericDoc)(rec, req)
		return rec
	}

	spoof := call(http.MethodPost, "ProductAttributeValue", "", map[string]interface{}{"code": "QA-STW-NEW", "item": item, "attribute": attr, "value": "Blue", "status": "Active",
		"source": "Person", "source_ref": "alice"})
	if spoof.Code == http.StatusOK || spoof.Code == http.StatusCreated || !strings.Contains(spoof.Body.String(), "PIM-9151") {
		t.Fatalf("a claimed source got through: %d %s", spoof.Code, spoof.Body.String())
	}
	if del := call(http.MethodDelete, "ProductAttributeValue", valueID, nil); !strings.Contains(del.Body.String(), "PIM-9151") {
		t.Fatalf("deleting a stewarded value: %d %s", del.Code, del.Body.String())
	}
	if del := call(http.MethodDelete, engines.StewardshipDoctype, entryID, nil); !strings.Contains(del.Body.String(), "PIM-9150") {
		t.Fatalf("deleting the register: %d %s", del.Code, del.Body.String())
	}
	if save := call(http.MethodPost, engines.StewardshipDoctype, "", map[string]interface{}{"code": "x", "attribute": attr, "system_of_record": "ERP", "status": "Active"}); !strings.Contains(save.Body.String(), "PIM-9150") {
		t.Fatalf("saving the register: %d %s", save.Code, save.Body.String())
	}
}

// A saved notification matrix can be larger than 64 KiB; the limit must not refuse it.
func TestReadJSONBodyTakesALargeMatrix(t *testing.T) {
	var v map[string]string
	big := `{"a":"` + strings.Repeat("x", 100<<10) + `"}`
	rec := httptest.NewRecorder()
	if !readJSONBody(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(big)), &v) || len(v["a"]) != 100<<10 {
		t.Fatalf("100 KB refused: %d", rec.Code)
	}
	over := `{"a":"` + strings.Repeat("x", 300<<10) + `"}`
	rec = httptest.NewRecorder()
	if readJSONBody(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(over)), &v) {
		t.Fatal("300 KB must be refused")
	}
}
