package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// End-to-end wiring for Stage 30.6's server-issued document numbers. The
// engines package unit-tests the numbering itself
// (engines/document_numbering_test.go); this proves PrepareDocumentNumber is
// actually reached through the real middleware and the real generic-doc
// handler, ahead of the mandatory-field validation it has to precede.
//
// RFQ is the doctype under test for the same reason Stage 29.8's transition
// test picked it: it has the lightest mandatory-field set of the numbered
// doctypes (code/description/quantity/status), so the test stays about
// numbering rather than about satisfying vendor, GST and Location validation.
func TestDocumentNumberIssuedByServer(t *testing.T) {
	db.InitDB(testConnStr())

	var createdIDs []string
	cleanup := func() {
		for _, id := range createdIDs {
			_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
		}
	}
	defer cleanup()

	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO")
	engines.ResetLiveUserStateCache()

	// Built by hand rather than via doRequest: handleGenericDoc reads the
	// doctype from the route's path value, which doRequest doesn't set.
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

	// A create that supplies no number at all - what every create screen now
	// sends - must come back with one issued from the RFQ series.
	first := create(t, map[string]interface{}{
		"description": "server-numbered fixture",
		"quantity":    1,
		"status":      "Draft",
	})
	firstID, _ := first["id"].(string)
	if !strings.HasPrefix(firstID, "RFQ/") {
		t.Fatalf("want an RFQ-series number, got %q", firstID)
	}

	// The number must also be stored on the document's own mandatory `code`
	// field, not just used as the row id - PrepareDocumentNumber runs before
	// ValidateDocument precisely so this field is populated in time.
	var storedCode string
	if err := db.DB.QueryRow(
		`SELECT data->>'code' FROM tenant_default.documents WHERE id = $1`, firstID).Scan(&storedCode); err != nil {
		t.Fatalf("re-read RFQ: %v", err)
	}
	if storedCode != firstID {
		t.Errorf("stored code %q should match the issued number %q", storedCode, firstID)
	}

	// Two identical submissions must produce two distinct documents. This is
	// the collision the change exists to close: when the number came from the
	// browser, two makers picking the same one meant the second save silently
	// replaced the first, because documents.id is the upsert key.
	second := create(t, map[string]interface{}{
		"description": "server-numbered fixture",
		"quantity":    1,
		"status":      "Draft",
	})
	secondID, _ := second["id"].(string)
	if secondID == firstID {
		t.Fatalf("two creates were issued the same number %q - one overwrote the other", firstID)
	}

	// An update addresses the document by path id and must keep its number
	// rather than being issued a fresh one.
	updateBody, err := json.Marshal(map[string]interface{}{
		"code":        firstID,
		"description": "amended description",
		"quantity":    2,
		"status":      "Draft",
	})
	if err != nil {
		t.Fatalf("marshal update: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/doc/RFQ/"+firstID, bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("doctype", "RFQ")
	req.SetPathValue("id", firstID)
	rec := httptest.NewRecorder()
	apiMiddleware(handleGenericDoc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update failed: status=%d body=%s", rec.Code, rec.Body.String())
	}

	var updatedCode, updatedDescription string
	if err := db.DB.QueryRow(
		`SELECT data->>'code', data->>'description' FROM tenant_default.documents WHERE id = $1`,
		firstID).Scan(&updatedCode, &updatedDescription); err != nil {
		t.Fatalf("re-read updated RFQ: %v", err)
	}
	if updatedCode != firstID {
		t.Errorf("an update must keep its issued number, got %q want %q", updatedCode, firstID)
	}
	if updatedDescription != "amended description" {
		t.Errorf("the update did not apply: description is %q", updatedDescription)
	}
}
