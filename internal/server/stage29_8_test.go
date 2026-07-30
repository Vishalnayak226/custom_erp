package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// End-to-end wiring for Stage 29.8. The engines package already unit-tests the
// two mechanisms themselves (engines/stage29_8_test.go); these prove they are
// actually reached through the real middleware and the real generic-doc
// handler, which is the part a unit test can't show.

func TestDeactivatedUserLosesLiveSession(t *testing.T) {
	db.InitDB(testConnStr())
	const userID = "stage298-http-user"

	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, userID)
		engines.ResetLiveUserStateCache()
	}
	cleanup()
	defer cleanup()

	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, role, status, location_code)
		VALUES ($1, $1, 'x', 'HR/Admin', 'Active', 'HO')`, userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	engines.ResetLiveUserStateCache()

	handler := apiMiddleware(handleGetDocTypes)
	token := engines.SignToken(userID, userID, "HR/Admin", "default", "HO")

	// Baseline: the token works while the account is Active.
	if rec := doRequest(t, handler, http.MethodGet, "/api/v1/meta/doctypes", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("active user should be served, status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Deactivate. The token itself is untouched and still cryptographically
	// valid and unexpired - before Stage 29.8 it would have kept working for
	// the rest of JWT_EXPIRY_HOURS.
	if _, err := db.DB.Exec(`UPDATE tenant_default.users SET status = 'Inactive' WHERE id = $1`, userID); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}
	engines.InvalidateLiveUserState("default", userID)

	rec := doRequest(t, handler, http.MethodGet, "/api/v1/meta/doctypes", token, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a deactivated user's live token must be rejected, status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "GLOBAL-0009" {
		t.Errorf("expected the same generic session-expired code as any other dead token (no leak about why), got %v", body["code"])
	}
}

func TestStatusTransitionMapEnforcedThroughGenericDocAPI(t *testing.T) {
	db.InitDB(testConnStr())
	const rfqID = "STAGE298-RFQ-TRANSITION"

	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, rfqID)
	}
	cleanup()
	defer cleanup()

	// An RFQ sitting in Draft. RFQ is one of the doctypes Stage 29.8 seeded a
	// matrix for and flagged strict (Draft -> Sent -> Closed), and it has the
	// lightest mandatory-field set of those, so this test stays about the
	// transition guard rather than about satisfying unrelated field validation.
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'RFQ', $2, 'Draft', 'system')`, rfqID,
		`{"code":"STAGE298-RFQ-TRANSITION","description":"transition guard fixture","quantity":1,"status":"Draft"}`); err != nil {
		t.Fatalf("seed RFQ: %v", err)
	}

	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO")
	engines.ResetLiveUserStateCache()

	// Built by hand rather than via doRequest: handleGenericDoc reads the
	// doctype from the route's path value, which doRequest doesn't set.
	post := func(t *testing.T, status string) *httptest.ResponseRecorder {
		t.Helper()
		payload, err := json.Marshal(map[string]interface{}{
			"id":          rfqID,
			"code":        "STAGE298-RFQ-TRANSITION",
			"description": "transition guard fixture",
			"quantity":    1,
			"status":      status,
		})
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/doc/RFQ", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.SetPathValue("doctype", "RFQ")
		rec := httptest.NewRecorder()
		apiMiddleware(handleGenericDoc)(rec, req)
		return rec
	}

	// Draft -> Closed is not in the matrix: it would close an RFQ that was
	// never actually sent to a vendor. Must be rejected with the catalog's own
	// "Invalid status transition" code, at the documented 422.
	rec := post(t, "Closed")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Draft->Closed should be rejected 422, status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "GLOBAL-0019" {
		t.Errorf("expected GLOBAL-0019 (Invalid status transition), got %v - body=%s", body["code"], rec.Body.String())
	}

	// The document must be untouched by the rejected write.
	var stored string
	if err := db.DB.QueryRow(`SELECT status FROM tenant_default.documents WHERE id = $1`, rfqID).Scan(&stored); err != nil {
		t.Fatalf("re-read RFQ: %v", err)
	}
	if stored != "Draft" {
		t.Fatalf("a rejected transition must not have been persisted, status is now %q", stored)
	}

	// Draft -> Sent IS in the matrix and must still go through, proving the
	// guard is a map and not a blanket block on every status edit.
	if rec := post(t, "Sent"); rec.Code != http.StatusOK {
		t.Fatalf("Draft->Sent is a listed transition and should succeed, status=%d body=%s", rec.Code, rec.Body.String())
	}
}
