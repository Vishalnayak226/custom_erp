package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"testing"
)

// Stage 148.30: a salesman sees only their own earnings in the store app, and
// the route refuses while the module is off.
func TestStage14830StoreAppEarningsAreOwnOnly(t *testing.T) {
	db.InitDB(testConnStr())
	u1, c1 := seedStage47User(t, "Cashier", "HO")
	defer c1()
	u2, c2 := seedStage47User(t, "Cashier", "HO")
	defer c2()
	u3, c3 := seedStage47User(t, "Cashier", "HO")
	defer c3()
	tok1, tok3 := stage47Token(u1, "Cashier", "HO"), stage47Token(u3, "Cashier", "HO")
	sm1, sm2 := "SM-T14830-"+u1, "SM-T14830-"+u2
	for _, p := range []struct{ id, sm, user string }{{"PR1" + u1, sm1, u1}, {"PR2" + u2, sm2, u2}} {
		raw, _ := json.Marshal(map[string]interface{}{"salesman_id": p.sm, "user": p.user, "status": "Active"})
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'IncentiveProfile', $2, 'Active', 'system')`, p.id, raw); err != nil {
			t.Fatal(err)
		}
		defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, p.id)
	}
	for sm, amt := range map[string]int64{sm1: 1111, sm2: 2222} {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.sales_incentive_entries (kind, source_doctype, source_id, line_no, salesman_id, sku, amount_paise)
			VALUES ('Accrual', 'SalesInvoice', $1, 0, $2, 'X', $3)`, "T14830-"+sm, sm, amt); err != nil {
			t.Fatal(err)
		}
		defer db.DB.Exec(`DELETE FROM tenant_default.sales_incentive_entries WHERE salesman_id = $1`, sm)
	}
	h := apiMiddleware(moduleGate(engines.ModuleSalesIncentives, handleStoreAppEarnings))

	// Off: refused.
	_ = engines.SetModuleEntitlement("default", engines.ModuleSalesIncentives, false, "test")
	if rec := doRequest(t, h, http.MethodGet, "/api/v1/store-app/earnings", tok1, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("module off = %d %s, want 403", rec.Code, rec.Body.String())
	}
	if err := engines.SetModuleEntitlement("default", engines.ModuleSalesIncentives, true, "test"); err != nil {
		t.Fatal(err)
	}
	defer engines.SetModuleEntitlement("default", engines.ModuleSalesIncentives, false, "test")

	rec := doRequest(t, h, http.MethodGet, "/api/v1/store-app/earnings?salesman_id="+sm2, tok1, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("own earnings = %d %s", rec.Code, rec.Body.String())
	}
	var st engines.IncentiveStatement
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if !st.Linked || st.Unpaid != 11.11 || len(st.Entries) != 1 || st.Entries[0].Amount != 11.11 {
		t.Fatalf("salesman 1 sees %+v, want only their own 11.11 (asking for another salesman's id changes nothing)", st)
	}
	rec = doRequest(t, h, http.MethodGet, "/api/v1/store-app/earnings", tok3, nil)
	st = engines.IncentiveStatement{}
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if rec.Code != http.StatusOK || st.Linked || len(st.Entries) != 0 {
		t.Fatalf("a login with no salesman profile sees %d %+v", rec.Code, st)
	}
}
