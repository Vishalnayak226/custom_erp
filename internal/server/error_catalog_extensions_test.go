package server

import (
	"custom_erp/db"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReceiptExtensionErrorsAreActionableClientErrors(t *testing.T) {
	// The real envelope logs catalog entries, as the existing API error tests do.
	db.InitDB(testConnStr())
	for _, code := range []string{"GOODSR-0096", "GOODSR-0097"} {
		entry, ok := errorCatalog[code]
		if !ok || entry.UserAction == "" || !entry.Blocking || entry.Retryable {
			t.Fatalf("invalid receipt catalog entry %s", code)
		}
		rec := httptest.NewRecorder()
		writeAPIError(rec, httptest.NewRequest(http.MethodPost, "/api/v1/doc/GRN", nil), code, "receipt validation detail")
		if rec.Code != 422 || !strings.Contains(rec.Body.String(), code) || !strings.Contains(rec.Body.String(), entry.UserAction) {
			t.Fatalf("%s: status %d, body %s", code, rec.Code, rec.Body.String())
		}
	}
}
