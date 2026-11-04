package server

import (
	"custom_erp/db"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStage14812RateHistoryFollowsTheCostRule: the purchase rate is
// Item.standard_cost, so a role that may read items but not cost is refused.
func TestStage14812RateHistoryFollowsTheCostRule(t *testing.T) {
	db.InitDB(testConnStr())
	for role, want := range map[string]int{"Cashier": 403, "Store Manager": 200, "Super Admin": 200} {
		r := httptest.NewRequest("GET", "/api/v1/procurement/purchase-rate-history?sku=S14812-NONE", nil)
		r.Header.Set("Resolved-Tenant-ID", "default")
		r.Header.Set("Resolved-Role", role)
		w := httptest.NewRecorder()
		handlePurchaseRateHistory(w, r)
		if w.Code != want {
			t.Errorf("%s: %d %s, want %d", role, w.Code, w.Body.String(), want)
		}
	}
}

// TestStage14814CombineNeedsPOCreate: Combine makes purchase orders, so a role
// that cannot create one is refused before anything is read.
func TestStage14814CombineNeedsPOCreate(t *testing.T) {
	db.InitDB(testConnStr())
	r := httptest.NewRequest("POST", "/api/v1/procurement/combine-requisitions", strings.NewReader(`{"vendor_id":"S14814-NONE"}`))
	r.Header.Set("Resolved-Tenant-ID", "default")
	r.Header.Set("Resolved-Role", "Cashier")
	w := httptest.NewRecorder()
	handleCombineRequisitions(w, r)
	if w.Code != 403 {
		t.Errorf("Cashier: %d %s, want 403", w.Code, w.Body.String())
	}
}
