package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// TestErrorEnvelopeCarriesDetailAndUserAction covers Stage 30.2.4. The
// defect: the envelope sent only the catalog's generic UserMessage, so a
// missing HSN code reached the user as "Tax configuration is missing for this
// transaction. Please contact administrator." - no item name, no field name,
// and an instruction to contact an administrator shown to an administrator.
// Both the engine's own specific message and the catalog's UserAction (which
// is populated on all 302 rows) were being discarded.
//
// A database connection is needed even though nothing here reads business
// data: catalog rows with LogRequired/AuditRequired set write a log row as a
// side effect of being returned (logForEntry), and db.DB is a package global.
func TestErrorEnvelopeCarriesDetailAndUserAction(t *testing.T) {
	db.InitDB(testConnStr())

	decode := func(rec *httptest.ResponseRecorder) map[string]interface{} {
		var body map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("response is not JSON (%q): %v", rec.Body.String(), err)
		}
		return body
	}

	t.Run("an engine ValidationError's own message rides along as detail", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/pos/checkout", nil)
		engineErr := &engines.ValidationError{
			Code:    "ADMINC-0034",
			Message: "item 'QA-TSHIRT-01' is missing hsn_code - required before it can be sold or purchased",
		}
		writeEngineError(rec, req, engineErr, http.StatusUnprocessableEntity)

		body := decode(rec)
		if body["code"] != "ADMINC-0034" {
			t.Fatalf("code = %v, want ADMINC-0034", body["code"])
		}
		detail, _ := body["detail"].(string)
		if !strings.Contains(detail, "QA-TSHIRT-01") || !strings.Contains(detail, "hsn_code") {
			t.Fatalf("detail must name the item and the field, got %q", detail)
		}
		if action, _ := body["user_action"].(string); action == "" {
			t.Fatalf("user_action is empty - the catalog populates it on every row")
		}
		if headline, _ := body["error"].(string); headline == "" {
			t.Fatalf("the catalog headline must still be present")
		}
	})

	t.Run("META-0199 reaches the user with the field and its allowed values", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/doc/PurchaseOrder", nil)
		engineErr := &engines.ValidationError{
			Code:    "META-0199",
			SubFor:  "Status",
			Message: `Field "Status" value "Shipped" is not in allowed list (Draft,Approved,Closed)`,
		}
		writeEngineError(rec, req, engineErr, http.StatusUnprocessableEntity)

		detail, _ := decode(rec)["detail"].(string)
		if !strings.Contains(detail, "Status") || !strings.Contains(detail, "Draft,Approved,Closed") {
			t.Fatalf("META-0199 detail must name the field and the allowed values, got %q", detail)
		}
	})

	t.Run("a plain catalog error still carries the catalog's user_action", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item/nope", nil)
		writeAPIError(rec, req, "GLOBAL-0004", "")

		body := decode(rec)
		if _, present := body["detail"]; present {
			t.Fatalf("detail must be omitted when there is none, got %v", body["detail"])
		}
		if action, _ := body["user_action"].(string); action == "" {
			t.Fatalf("user_action is empty for GLOBAL-0004")
		}
	})

	t.Run("a duplicated detail is not repeated under the headline", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item/nope", nil)
		entry := errorCatalog["GLOBAL-0004"]
		writeAPIErrorDetail(rec, req, "GLOBAL-0004", "", entry.UserMessage)

		if _, present := decode(rec)["detail"]; present {
			t.Fatalf("a detail identical to the headline should be dropped, not shown twice")
		}
	})

	t.Run("a 5xx still withholds the raw internal message", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item", nil)
		writeAPIErrorGeneric(rec, req, http.StatusInternalServerError,
			`pq: character with byte sequence 0xe4 0xbd 0xa0 has no equivalent in encoding "WIN1252"`)

		body := decode(rec)
		serialized := rec.Body.String()
		if strings.Contains(serialized, "WIN1252") {
			t.Fatalf("raw driver detail leaked to the client: %s", serialized)
		}
		if action, _ := body["user_action"].(string); action == "" {
			t.Fatalf("even a withheld-detail 5xx should tell the user what to do next")
		}
	})
}
