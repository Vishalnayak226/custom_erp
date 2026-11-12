package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Stage 162.3: a PUT on a dispatched challan answered a bare 405 while a
// posted document explained itself. Both now say what is locked and what to
// do instead, and nothing is saved.
func TestLockedRecordSaysWhyOnGenericPutAndEdit(t *testing.T) {
	db.InitDB(testConnStr())
	cleanup := func() { _, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id LIKE 'T1623-%'`) }
	cleanup()
	defer cleanup()
	seed := func(doctype, id, status, data string) {
		t.Helper()
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3::jsonb, $4, 'admin')`,
			id, doctype, data, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed("DeliveryChallan", "T1623-DC", "Dispatched", `{"status":"Dispatched","party":"Acme","remarks":"first"}`)
	seed("DeliveryChallan", "T1623-DC2", "Approved", `{"status":"Approved","party":"Acme","remarks":"first"}`)
	seed("SalesInvoice", "T1623-SI", "Approved", `{"status":"Approved","total_amount":100,"remarks":"first"}`)

	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	engines.ResetLiveUserStateCache()
	call := func(method, doctype, id string, payload map[string]interface{}) (int, string) {
		t.Helper()
		raw, _ := json.Marshal(payload)
		req := httptest.NewRequest(method, "/api/v1/doc/"+doctype+"/"+id, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.SetPathValue("doctype", doctype)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		apiMiddleware(handleGenericDoc)(rec, req)
		return rec.Code, rec.Body.String()
	}
	remarks := func(id string) string {
		var s string
		_ = db.DB.QueryRow(`SELECT COALESCE(data->>'remarks','') FROM tenant_default.documents WHERE id = $1`, id).Scan(&s)
		return s
	}

	// PUT on the dispatched challan: the locked message, naming return and cancel.
	code, body := call(http.MethodPut, "DeliveryChallan", "T1623-DC", map[string]interface{}{"remarks": "changed"})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "return") || !strings.Contains(body, "cancel") {
		t.Fatalf("PUT on a dispatched challan: %d %s, want 422 naming return and cancel", code, body)
	}
	// The usual save of it is refused the same way, and nothing is saved.
	code, body = call(http.MethodPost, "DeliveryChallan", "T1623-DC", map[string]interface{}{"id": "T1623-DC", "code": "T1623-DC", "purpose": "Job work", "location": "HO", "party_name": "Acme", "items": "[{\"sku\":\"X\",\"qty\":1}]", "party": "Acme", "status": "Dispatched", "remarks": "changed"})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "can no longer be edited") {
		t.Fatalf("save of a dispatched challan: %d %s, want the locked message", code, body)
	}
	if got := remarks("T1623-DC"); got != "first" {
		t.Fatalf("a refused edit changed the challan: remarks = %q", got)
	}
	// A challan not yet dispatched keeps the old answer for PUT (405).
	if code, _ := call(http.MethodPut, "DeliveryChallan", "T1623-DC2", map[string]interface{}{"remarks": "x"}); code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT on an approved challan: %d, want 405", code)
	}
	// One other posted document gets its message from the same place.
	code, body = call(http.MethodPut, "SalesInvoice", "T1623-SI", map[string]interface{}{"remarks": "x"})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "posted") {
		t.Fatalf("PUT on a posted invoice: %d %s, want 422 posted", code, body)
	}
	if got := remarks("T1623-SI"); got != "first" {
		t.Fatalf("PUT changed the invoice: %q", got)
	}
}

// Stage 162.7: the create routes of the eleven BRD commands carry the command
// key. Real handlers behind the same wrapper the routes use: a retry with the
// same key creates and moves nothing a second time.
func TestBRDCommandCreateRoutesReplayOnce(t *testing.T) {
	db.InitDB(testConnStr())
	const loc, sku = "T1627-LOC", "T1627-SKU"
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM tenant_default.gl_postings WHERE document_id IN (SELECT id FROM tenant_default.documents WHERE data->>'party_name' = 'T1627 party')`,
			`DELETE FROM tenant_default.documents WHERE doctype = 'DeliveryChallan' AND data->>'party_name' = 'T1627 party'`,
			`DELETE FROM tenant_default.documents WHERE id IN ('` + sku + `', '` + loc + `')`,
			`DELETE FROM tenant_default.inventory_availability WHERE location_code = '` + loc + `'`,
		} {
			_, _ = db.DB.Exec(q)
		}
	}
	cleanup()
	defer cleanup()
	for _, q := range []string{
		`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ('` + sku + `', 'Item', '{"code":"` + sku + `","name":"T1627","cost_price":50,"hsn_code":"7113","valuation":"VALUATED"}', 'Active', 'system')`,
		`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ('` + loc + `', 'Location', '{"code":"` + loc + `","name":"T1627 store","type":"Store"}', 'Active', 'system')`,
		`INSERT INTO tenant_default.inventory_availability (sku, location_code, on_hand, available) VALUES ('` + sku + `', '` + loc + `', 20, 20)`,
	} {
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	stamp := fmt.Sprint(time.Now().UnixNano())
	call := func(h http.HandlerFunc, path, key, body string) *httptest.ResponseRecorder {
		req := keyedRequest4947a(http.MethodPost, path, key, body)
		req.Header.Set("Resolved-Role", "Super Admin")
		req.Header.Set("Resolved-User-ID", "admin")
		req.SetPathValue("id", "none")
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}
	count := func() (n int) {
		_ = db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.documents WHERE doctype = 'DeliveryChallan' AND data->>'party_name' = 'T1627 party'`).Scan(&n)
		return
	}
	create := commandKeyed("inventory.challan.create", handleCreateDeliveryChallan)
	body := `{"location":"` + loc + `","purpose":"Non-returnable","party_name":"T1627 party","lines":[{"sku":"` + sku + `","qty":2}]}`
	first := call(create, "/api/v1/inventory/challans", "dc-"+stamp, body)
	if first.Code != http.StatusOK || count() != 1 {
		t.Fatalf("first create: %d %s (challans %d)", first.Code, first.Body.String(), count())
	}
	again := call(create, "/api/v1/inventory/challans", "dc-"+stamp, body)
	if again.Code != http.StatusOK || count() != 1 || again.Header().Get("Idempotent-Replayed") != "true" || again.Body.String() != first.Body.String() {
		t.Fatalf("a retried create must answer the first and add nothing: %d %s (challans %d)", again.Code, again.Body.String(), count())
	}
	var id string
	var parsed map[string]interface{}
	_ = json.Unmarshal(first.Body.Bytes(), &parsed)
	id, _ = parsed["id"].(string)
	// Dispatch twice with one key: one dispatch event, same answer.
	dispatch := commandKeyed("inventory.challan.dispatch", handleDispatchDeliveryChallan)
	dreq := func() *httptest.ResponseRecorder {
		req := keyedRequest4947a(http.MethodPost, "/api/v1/inventory/challans/"+id+"/dispatch", "dd-"+stamp, `{}`)
		req.Header.Set("Resolved-Role", "Super Admin")
		req.Header.Set("Resolved-User-ID", "admin")
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		dispatch(rec, req)
		return rec
	}
	d1, d2 := dreq(), dreq()
	if d1.Code != http.StatusOK || d2.Code != http.StatusOK || d2.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("dispatch replay: %d %s / %d %s", d1.Code, d1.Body.String(), d2.Code, d2.Body.String())
	}
	var events int
	_ = db.DB.QueryRow(`SELECT COALESCE(jsonb_array_length((data->>'challan_events')::jsonb), 0) FROM tenant_default.documents WHERE id = $1`, id).Scan(&events)
	if events != 1 {
		t.Fatalf("a replayed dispatch recorded %d events, want 1", events)
	}
	// A different key is a new challan.
	if other := call(create, "/api/v1/inventory/challans", "dc2-"+stamp, body); other.Code != http.StatusOK || count() != 2 {
		t.Fatalf("a new key is a new challan: %d (challans %d)", other.Code, count())
	}
}

// Every command of the eleven that this item names answers to a command key in
// the route table, so a future edit cannot quietly unwrap one.
func TestBRDCommandRoutesAreAllKeyed(t *testing.T) {
	rows, err := IdempotencyAuditRows("routes.go", ".")
	if err != nil {
		t.Fatal(err)
	}
	byRoute := map[string]IdempotencyAuditRow{}
	for _, row := range rows {
		byRoute[row.Route] = row
	}
	for _, route := range []string{
		"POST /api/v1/inventory/challans", "POST /api/v1/inventory/challans/{id}/dispatch", "POST /api/v1/inventory/challans/{id}/return", "POST /api/v1/inventory/challans/{id}/cancel",
		"POST /api/v1/inventory/material-requests", "POST /api/v1/inventory/material-requests/{id}/issue", "POST /api/v1/inventory/material-requests/{id}/return",
		"POST /api/v1/manufacturing/subcontract-order/send", "POST /api/v1/manufacturing/subcontract-order/receive", "POST /api/v1/manufacturing/subcontract-order/receive-lines", "POST /api/v1/manufacturing/subcontract-order/charge",
		"POST /api/v1/einvoice/documents/{id}/{action}", "POST /api/v1/gst/ewaybills/{doctype}/{id}/{action}", "POST /api/v1/print/document/{txn}/{id}/reprint-request",
		"POST /api/v1/grn-steps/{id}/post/{child}", "POST /api/v1/grn-steps/{id}/post-all", "POST /api/v1/grn-steps/{id}/pay", "POST /api/v1/procurement/purchase-order/{id}/send",
		"POST /api/v1/inventory/adjustments", "POST /api/v1/pos/advances", "POST /api/v1/pos/gift-card/issue",
	} {
		row, ok := byRoute[route]
		if !ok || !strings.HasPrefix(row.Key, "yes (command key") {
			t.Errorf("%s is not behind commandKeyed (audit says %q)", route, row.Key)
		}
	}
}
