package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// 57.26(c): a journal voucher saved through the generic route needs a finance
// role, as its own route does. The shipped seed grants Store Manager create on
// the doctype (26.6), so the doctype grant alone let a store post journals.
func TestJournalVoucherGenericSaveNeedsFinanceRole(t *testing.T) {
	db.InitDB(testConnStr())
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	const gate = "saved by a finance role"
	// No lines: anything past the role check refuses it, so nothing is created.
	body := map[string]interface{}{"voucher_date": "2026-11-14", "narration": "57.26c"}

	sm, dropSM := seedContractUser(t, "Store Manager", "HO")
	defer dropSM()
	rec := doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/JournalVoucher",
		engines.SignToken(sm, sm, "Store Manager", "default", "HO", currentCredentialVersion(sm)), body)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), gate) {
		t.Errorf("Store Manager: %d %s, want 403 %q", rec.Code, rec.Body.String(), gate)
	}

	acct, dropAcct := seedContractUser(t, "Accountant", "HO")
	defer dropAcct()
	rec = doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/JournalVoucher",
		engines.SignToken(acct, acct, "Accountant", "default", "HO", currentCredentialVersion(acct)), body)
	if strings.Contains(rec.Body.String(), gate) {
		t.Errorf("Accountant was refused by the finance-role check: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Code < 400 {
		t.Errorf("a voucher with no lines saved: %d %s", rec.Code, rec.Body.String())
	}

	// 245.5: /me/permissions tells the browser the same, so a Store Manager
	// whose seed grant says create is not offered New or Edit on a voucher.
	// 245.4: and the three records nobody makes on a form are own-screen.
	mux.HandleFunc("/api/v1/me/permissions", apiMiddleware(handleMyPermissions))
	perms := func(user, role string) map[string][]string {
		rec := doRequest(t, mux.ServeHTTP, http.MethodGet, "/api/v1/me/permissions",
			engines.SignToken(user, user, role, "default", "HO", currentCredentialVersion(user)), nil)
		var raw map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("%s permissions: %d %s", role, rec.Code, rec.Body.String())
		}
		out := map[string][]string{}
		for k, v := range raw {
			if list, ok := v.([]interface{}); ok {
				for _, s := range list {
					out[k] = append(out[k], fmt.Sprint(s))
				}
			}
		}
		return out
	}
	has := func(list []string, s string) bool {
		for _, x := range list {
			if x == s {
				return true
			}
		}
		return false
	}
	smPerms := perms(sm, "Store Manager")
	if has(smPerms["create"], "JournalVoucher") || has(smPerms["update"], "JournalVoucher") {
		t.Errorf("Store Manager is told it may save a journal voucher: create=%v", smPerms["create"])
	}
	for _, d := range []string{"AccessChangeRequest", "ClientDataRemoval", "DocumentReprint", "LogisticsBooking"} { // 246.4: the last
		if !has(smPerms["own_screen"], d) {
			t.Errorf("%s is not own-screen, so a list still offers New", d)
		}
	}
	if len(smPerms["create"]) == 0 {
		t.Errorf("Store Manager lost every create grant, not just the voucher's")
	}
}

// 57.26(d): a generic create refused after its number was drawn (here, a
// mandatory field left out) leaves the series where it was.
func TestRefusedGenericCreateLeavesNoNumberGap(t *testing.T) {
	db.InitDB(testConnStr())
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	admin := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))
	counter := func() int64 {
		var n int64
		db.DB.QueryRow(`SELECT COALESCE(SUM(current_val), 0) FROM tenant_default.sequence_counters WHERE doc_type = 'TKT'`).Scan(&n)
		return n
	}
	before := counter()
	rec := doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/ServiceTicket", admin,
		map[string]interface{}{"description": "no customer"})
	if rec.Code < 400 {
		t.Fatalf("a ticket with no customer saved: %d %s", rec.Code, rec.Body.String())
	}
	if after := counter(); after != before {
		t.Errorf("TKT counter %d -> %d after a refused save, want unchanged", before, after)
	}
}
