package server

import (
	"custom_erp/db"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestManufacturingRoutesCheckTheRole (193): moduleGate only says the client
// has manufacturing. Every production posting and lookup must also check the
// role, so a Cashier is refused before any order is read, and a Super Admin
// gets past the check (to a not-found or validation answer for the bogus id).
func TestManufacturingRoutesCheckTheRole(t *testing.T) {
	db.InitDB(testConnStr())
	body := `{"order_id":"MFG193-NONE","id":"MFG193-NONE","qty":1,"seq":1,"sku":"MFG193-NONE","actual_cost":1}`
	cases := []struct {
		name    string
		method  string
		url     string
		handler http.HandlerFunc
	}{
		{"partial-complete", "POST", "/api/v1/manufacturing/partial-complete", handlePartialCompleteProductionOrder},
		{"scrap", "POST", "/api/v1/manufacturing/scrap", handlePostProductionScrap},
		{"rework", "POST", "/api/v1/manufacturing/rework", handleSendProductionToRework},
		{"confirm-operation", "POST", "/api/v1/manufacturing/confirm-operation", handleConfirmProductionOperation},
		{"actual-cost", "POST", "/api/v1/manufacturing/actual-cost", handleRecordActualProductionCost},
		{"subcontract receive", "POST", "/api/v1/manufacturing/subcontract-order/receive", handleReceiveSubcontractOrder},
		{"issue-material", "POST", "/api/v1/manufacturing/issue-material", handleIssueProductionMaterial},
		{"complete", "POST", "/api/v1/manufacturing/complete", handleCompleteProductionOrder},
		{"mrp-suggestions", "GET", "/api/v1/manufacturing/mrp-suggestions?location=MFG193-NONE", handleMRPSuggestions},
		{"schedule", "GET", "/api/v1/manufacturing/schedule?location=MFG193-NONE", handleGetProductionSchedule},
		{"active-bom", "GET", "/api/v1/manufacturing/active-bom?item=MFG193-NONE", handleActiveBOMForItem},
		{"confirmations", "GET", "/api/v1/manufacturing/confirmations?order_id=MFG193-NONE", handleListProductionConfirmations},
	}
	for _, c := range cases {
		for role, refused := range map[string]bool{"Cashier": true, "Super Admin": false} {
			var r *http.Request
			if c.method == "POST" {
				r = httptest.NewRequest(c.method, c.url, strings.NewReader(body))
			} else {
				r = httptest.NewRequest(c.method, c.url, nil)
			}
			r.Header.Set("Resolved-Tenant-ID", "default")
			r.Header.Set("Resolved-Role", role)
			w := httptest.NewRecorder()
			c.handler(w, r)
			if refused && w.Code != http.StatusForbidden {
				t.Errorf("%s as %s: %d %s, want 403", c.name, role, w.Code, w.Body.String())
			}
			if !refused && w.Code == http.StatusForbidden {
				t.Errorf("%s as %s: refused (%s), want past the role check", c.name, role, w.Body.String())
			}
		}
	}
}
