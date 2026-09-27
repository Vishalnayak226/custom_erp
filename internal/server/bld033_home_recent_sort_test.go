package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// BLD-033's Home screen needs a "recent tasks" panel - what did this user
// touch last, not the generic doc engine's original stable "ORDER BY id"
// listing order. This proves the opt-in sort=recent parameter added to
// handleGenericDoc's list branch (handlers_core_doc_engine.go) actually
// reorders by updated_at DESC and surfaces it, while leaving the default,
// unparameterized list response byte-for-byte the shape every existing
// caller already depends on (no updated_at key, original id order).
//
// RFQ is the doctype under test for the same reason
// TestDocumentNumberIssuedByServer picked it: the lightest mandatory-field
// set of the numbered doctypes, so the test stays about sort/response shape
// rather than about satisfying unrelated validation.
func TestGenericDocListSortRecent(t *testing.T) {
	db.InitDB(testConnStr())

	var createdIDs []string
	cleanup := func() {
		for _, id := range createdIDs {
			_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
		}
	}
	defer cleanup()

	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	engines.ResetLiveUserStateCache()

	create := func(t *testing.T, payload map[string]interface{}) map[string]interface{} {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/doc/RFQ", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.SetPathValue("doctype", "RFQ")
		rec := httptest.NewRecorder()
		apiMiddleware(handleGenericDoc)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("create failed: status=%d body=%s", rec.Code, rec.Body.String())
		}
		var out map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response: %v (body=%s)", err, rec.Body.String())
		}
		if id, _ := out["id"].(string); id != "" {
			createdIDs = append(createdIDs, id)
		}
		return out
	}

	update := func(t *testing.T, id string, payload map[string]interface{}) {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/doc/RFQ/"+url.PathEscape(id), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.SetPathValue("doctype", "RFQ")
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		apiMiddleware(handleGenericDoc)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update failed: status=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	list := func(t *testing.T, query string) []map[string]interface{} {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/doc/RFQ"+query, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.SetPathValue("doctype", "RFQ")
		rec := httptest.NewRecorder()
		apiMiddleware(handleGenericDoc)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list failed: status=%d body=%s", rec.Code, rec.Body.String())
		}
		var out []map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode list response: %v (body=%s)", err, rec.Body.String())
		}
		return out
	}

	// A created first (oldest), B created second - both untouched again
	// after creation, so default id-order and recency order agree so far.
	a := create(t, map[string]interface{}{"description": "bld033-recent-a", "quantity": 1, "status": "Draft"})
	aID, _ := a["id"].(string)
	b := create(t, map[string]interface{}{"description": "bld033-recent-b", "quantity": 1, "status": "Draft"})
	bID, _ := b["id"].(string)
	if aID == "" || bID == "" || aID == bID {
		t.Fatalf("expected two distinct RFQ ids, got %q and %q", aID, bID)
	}

	// Touch A again - it is now the most recently updated of the two, even
	// though it has the OLDER id/creation order.
	update(t, aID, map[string]interface{}{"code": aID, "description": "bld033-recent-a-touched", "quantity": 2, "status": "Draft"})

	// Default listing (no sort param): unchanged shape and order - id order,
	// so A (created first) still sorts before B, and neither row carries the
	// new updated_at key at all.
	defaultRows := list(t, "?limit=200")
	defaultIndex := map[string]int{}
	for i, row := range defaultRows {
		if id, _ := row["id"].(string); id == aID || id == bID {
			defaultIndex[id] = i
			if _, present := row["updated_at"]; present {
				t.Errorf("default (unsorted) listing must not carry updated_at, got it on %q", id)
			}
		}
	}
	if defaultIndex[aID] >= defaultIndex[bID] {
		t.Errorf("default listing should keep id order (A before B), got A at %d, B at %d", defaultIndex[aID], defaultIndex[bID])
	}

	// sort=recent: A was touched after B was created, so A must now come
	// first, and both rows must carry a parseable updated_at.
	recentRows := list(t, "?sort=recent&limit=200")
	recentIndex := map[string]int{}
	for i, row := range recentRows {
		id, _ := row["id"].(string)
		if id != aID && id != bID {
			continue
		}
		recentIndex[id] = i
		raw, ok := row["updated_at"].(string)
		if !ok || raw == "" {
			t.Fatalf("sort=recent row %q missing updated_at", id)
		}
	}
	if _, ok := recentIndex[aID]; !ok {
		t.Fatalf("A (%s) missing from sort=recent listing", aID)
	}
	if _, ok := recentIndex[bID]; !ok {
		t.Fatalf("B (%s) missing from sort=recent listing", bID)
	}
	if recentIndex[aID] >= recentIndex[bID] {
		t.Errorf("sort=recent should surface the touched record (A) before B, got A at %d, B at %d", recentIndex[aID], recentIndex[bID])
	}
}
