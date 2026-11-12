package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"testing"
)

// Stage 148.19: a bill with no PO is made only by its own route. Not even a
// full-access role may type one, or change what the engine booked, through the
// generic save; the new routes need the finance posting capability.
func TestStage14819ExpenseBillIsNotTypedAndRoutesAreGuarded(t *testing.T) {
	db.InitDB(testConnStr())
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("POST /api/v1/procurement/expense-bills", apiMiddleware(handleCreateExpenseBill))
	mux.HandleFunc("GET /api/v1/procurement/expense-bills", apiMiddleware(handleListExpenseBills))
	admin := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "", currentCredentialVersion("admin"))

	rec := doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/VendorInvoice", admin, map[string]interface{}{
		"invoice_number": "T14819-TYPED", "vendor_id": "V1", "invoice_amount": 500, "bill_kind": "Expense", "status": "Draft"})
	var got struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Code != "FIN-0933" {
		t.Errorf("typed expense bill: %d %s, want FIN-0933", rec.Code, rec.Body.String())
	}
	var typed int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.documents WHERE doctype = 'VendorInvoice' AND data->>'invoice_number' = 'T14819-TYPED'`).Scan(&typed)
	if typed != 0 {
		t.Errorf("%d typed bills were saved", typed)
	}

	// Off by default: the route refuses with FIN-0930 and the list says so.
	if err := engines.SetSetting("default", engines.SettingExpenseBills, "false", "t14819"); err != nil {
		t.Fatal(err)
	}
	rec = doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/procurement/expense-bills", admin, map[string]interface{}{
		"vendor_id": "V1", "invoice_number": "T14819-OFF", "lines": []map[string]interface{}{{"account": "5100", "amount": 10}}})
	got.Code = ""
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Code != "FIN-0930" {
		t.Errorf("off: %d %s, want FIN-0930", rec.Code, rec.Body.String())
	}
	// Holding finance.post is not booking a vendor bill: same grant as the document route.
	arUser, arCleanup := seedContractUser(t, "Accounts Receivable", "HO")
	defer arCleanup()
	ar := engines.SignToken(arUser, arUser, "Accounts Receivable", "default", "HO", 1)
	rec = doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/procurement/expense-bills", ar, map[string]interface{}{
		"vendor_id": "V1", "invoice_number": "T14819-AR", "lines": []map[string]interface{}{{"account": "5100", "amount": 10}}})
	got.Code = ""
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Code != "GLOBAL-0011" {
		t.Errorf("a role with no vendor bill grant: %d %s, want GLOBAL-0011", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, mux.ServeHTTP, http.MethodGet, "/api/v1/procurement/expense-bills", admin, nil)
	var list struct {
		Enabled bool `json:"enabled"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != http.StatusOK || list.Enabled {
		t.Errorf("list while off: %d %s", rec.Code, rec.Body.String())
	}
}
