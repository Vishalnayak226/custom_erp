package engines

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// Stage 146.2.8 - the till and the history show what is about to expire, what
// is still pending, and the store and cashier of each row; messages can say
// what a bill earned and the balance.
func TestStage14628TillAndHistoryShowExpiringPendingStoreCashier(t *testing.T) {
	e := newCRMTestEnv(t)
	c, _ := e.customer("Divya Nair", nil)
	soon, late, past := time.Now().AddDate(0, 0, 10), time.Now().AddDate(0, 0, 300), time.Now().AddDate(0, 0, -2)
	seed := func(pts int, exp *time.Time, ref string) {
		t.Helper()
		if err := insertLoyaltyLedgerEntryInSchema(e.schema, c, "Earn", pts, "POSCart", ref, exp); err != nil {
			t.Fatal(err)
		}
	}
	seed(100, &soon, "CART-OLD")
	seed(200, &late, "CART-MID")
	seed(40, &past, "CART-GONE") // already lapsed: not "soon"
	seed(50, &soon, "CART-NEW")
	e.exec(`UPDATE {s}.loyalty_point_ledger SET pending_until = NOW() + interval '5 days' WHERE customer_id = $1 AND reference_id = 'CART-NEW'`, c)

	ctx, err := TillCustomerContextFor(e.tenantID, c)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.ExpiringSoon != 100 {
		t.Errorf("expiring in 30 days: %d, want 100 (pending and lapsed points are not counted)", ctx.ExpiringSoon)
	}
	if ctx.Pending != 50 {
		t.Errorf("pending: %d, want 50", ctx.Pending)
	}
	if ctx.Balance != 340 {
		t.Errorf("balance %d, want 340", ctx.Balance)
	}
	// A balance smaller than the expiring lots can only lose what it has.
	e.exec(`INSERT INTO {s}.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id) VALUES ($1, 'Burn', 330, 'POSCart', 'CART-BURN')`, c)
	if ctx, _ = TillCustomerContextFor(e.tenantID, c); ctx.ExpiringSoon != 10 {
		t.Errorf("expiring is capped by the usable balance: %d, want 10", ctx.ExpiringSoon)
	}

	// History: the store the row belongs to and the cashier who made the bill.
	raw, _ := json.Marshal(map[string]interface{}{"location": "STORE-9"})
	e.exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ('CART-HIST', 'POSCart', $1, 'Paid', 'cashier.ravi')`, raw)
	e.exec(`INSERT INTO {s}.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id, location) VALUES ($1, 'Earn', 7, 'POSCart', 'CART-HIST', 'STORE-9')`, c)
	rows, err := GetLoyaltyLedger(e.tenantID, c)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.ReferenceID == "CART-HIST" {
			found = true
			if r.Location != "STORE-9" || r.CreatedBy != "cashier.ravi" {
				t.Errorf("history row store %q cashier %q, want STORE-9 / cashier.ravi", r.Location, r.CreatedBy)
			}
		}
		if r.ReferenceID == "CART-OLD" && r.CreatedBy != "" {
			t.Errorf("a row with no document behind it has no cashier, got %q", r.CreatedBy)
		}
	}
	if !found {
		t.Fatal("history row missing")
	}

	// Message variables: what the last bill earned and the usable balance.
	if !crmKnownVariable("points.earned") || !crmKnownVariable("points.balance") {
		t.Fatal("points.earned and points.balance must be known template variables")
	}
	e.exec(`INSERT INTO {s}.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id, created_at) VALUES ($1, 'Earn', 12, 'POSCart', 'CART-HIST', NOW()), ($1, 'Earn', 5, 'POSCart', 'CART-HIST', NOW())`, c)
	cust, err := crmLoadCustomer(e.schema, c)
	if err != nil {
		t.Fatal(err)
	}
	vars := crmCustomerVars(e.schema, cust)
	bal, _ := GetLoyaltyBalance(e.tenantID, c)
	if vars["points.earned"] != "24" {
		t.Errorf("points.earned %q, want 24 (all rows of the latest bill)", vars["points.earned"])
	}
	if vars["points.balance"] != fmt.Sprint(bal) {
		t.Errorf("points.balance %q, want %d", vars["points.balance"], bal)
	}
}
