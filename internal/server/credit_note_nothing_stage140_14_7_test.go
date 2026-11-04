package server

import (
	"custom_erp/db"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStage140147CreditNoteWithNothingToCreditSaysSo: raising credit notes
// for an order with no invoice answers 200 with a message the screen shows,
// not a bare "issued".
func TestStage140147CreditNoteWithNothingToCreditSaysSo(t *testing.T) {
	db.InitDB(testConnStr())
	r := httptest.NewRequest("POST", "/api/v1/orders/S140147-NOINV/credit-notes", strings.NewReader(`{"reason_code":"CANCELLED"}`))
	r.SetPathValue("id", "S140147-NOINV")
	r.Header.Set("Resolved-Tenant-ID", "default")
	r.Header.Set("Resolved-User-ID", "admin")
	w := httptest.NewRecorder()
	handleOrderCreditNotes(w, r)
	var out struct {
		Status  string   `json:"status"`
		Message string   `json:"message"`
		IDs     []string `json:"credit_note_ids"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if out.Message != "No posted invoice on this order - nothing to credit" || out.Status == "issued" || len(out.IDs) != 0 {
		t.Errorf("answer %+v, want the nothing-to-credit message", out)
	}
}
