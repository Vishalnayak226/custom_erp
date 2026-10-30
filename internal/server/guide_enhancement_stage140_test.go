package server

import "testing"

// 140.12.9: posting and settling money documents is a finance role's.
func TestStage140FinancePostingNeedsFinanceRole(t *testing.T) {
	routes := []string{
		"POST /api/v1/finance/sales-invoice/{id}/post",
		"POST /api/v1/finance/sales-invoice/{id}/settle",
		"POST /api/v1/finance/credit-note/{id}/post",
		"POST /api/v1/finance/bank-reconcile",
		"POST /api/v1/finance/journal-voucher",
		"POST /api/v1/procurement/vendor-invoice/pay",
	}
	for _, route := range routes {
		if ok, _, found := checkRouteCapability(route, "Cashier"); !found || ok {
			t.Fatalf("%s: Cashier allowed=%v (classified=%v), want refused", route, ok, found)
		}
		for _, role := range []string{"Admin", "Accountant", "Accounts Receivable"} {
			if ok, _, _ := checkRouteCapability(route, role); !ok {
				t.Fatalf("%s: %s refused, want allowed", route, role)
			}
		}
	}
}
