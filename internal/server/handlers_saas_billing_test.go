package server

import (
	"custom_erp/db"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 50.12: the client's Subscription screen reads GET /api/v1/saas/subscription.
// On a migrated database it answers 200 with a subscription (null when the
// client has none), a usage statement and an invoices list that is an array,
// never null. A missing column from a migration shows up here as a 500.
func TestMySubscriptionAnswersOnMigratedDB(t *testing.T) {
	db.InitDB(testConnStr())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/saas/subscription", nil)
	req.Header.Set("Resolved-Tenant-ID", "default")
	req.Header.Set("Resolved-Role", "Super Admin")
	req.Header.Set("Resolved-Username", "t5012")
	rec := httptest.NewRecorder()
	handleMySubscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"subscription", "usage", "invoices"} {
		if _, ok := out[k]; !ok {
			t.Fatalf("response has no %q: %s", k, rec.Body.String())
		}
	}
	if string(out["invoices"]) == "null" {
		t.Fatalf("invoices is null, want an array")
	}
}
