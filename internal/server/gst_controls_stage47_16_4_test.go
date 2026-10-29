package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Stage 47.16.4: through the generic record API - the path a tampered client
// or a script uses - a posted invoice cannot be cancelled or sent back to
// Draft by a save (its reversal is a credit note), a draft one still can be
// cancelled, and a till bill cannot be created or saved as Paid (only
// checkout takes the stock and posts the sale).
func TestStage47164GenericSaveCannotUnpostOrFakeASale(t *testing.T) {
	db.InitDB(testConnStr())
	const posted, draft, cart, faked = "T47164-SINV", "T47164-DRAFT", "T47164-CART", "T47164-FAKE"
	clean := func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id IN ($1, $2, $3, $4)`, posted, draft, cart, faked)
	}
	clean()
	defer clean()
	seed := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'admin')`, id, doctype, raw, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	invoice := map[string]interface{}{"invoice_number": posted, "customer": "T47164 Customer", "location": "HO", "total_amount": 1180, "taxable_amount": 1000}
	seed(posted, "SalesInvoice", "Approved", invoice)
	seed(draft, "SalesInvoice", "Draft", map[string]interface{}{"invoice_number": draft, "customer": "T47164 Customer", "location": "HO", "total_amount": 500})
	seed(cart, "POSCart", "Processing", map[string]interface{}{"cart_number": cart, "location": "HO", "payment_mode": "Cash", "amount_paid": 100, "items": []interface{}{}})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	token := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))
	statusOf := func(id string) string {
		var s string
		db.DB.QueryRow(`SELECT status FROM tenant_default.documents WHERE id = $1`, id).Scan(&s)
		return s
	}

	// SalesInvoice's transition map (Stage 29.8, strict) lets Approved move
	// to Cancelled with a reason - an admin-editable rule. The posted-
	// document rule refuses it whatever the map says; Draft is refused by
	// one or the other.
	for _, to := range []string{"Cancelled", "Draft"} {
		edit := map[string]interface{}{}
		for k, v := range invoice {
			edit[k] = v
		}
		edit["status"], edit["edit_reason"], edit["reason"] = to, "customer cancelled", "customer cancelled"
		rec := doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/SalesInvoice/"+posted, token, edit)
		if rec.Code != http.StatusUnprocessableEntity || (to == "Cancelled" && !strings.Contains(rec.Body.String(), "FIN-0844")) {
			t.Errorf("saving a posted invoice as %s: %d %s, want 422 (FIN-0844 for Cancelled)", to, rec.Code, rec.Body.String())
		}
	}
	// Nor deleted (Stage 74.1.5's rule, proven here for a tax invoice).
	if rec := doRequest(t, mux.ServeHTTP, http.MethodDelete, "/api/v1/doc/SalesInvoice/"+posted, token, nil); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("deleting a posted invoice: %d %s, want 422", rec.Code, rec.Body.String())
	}
	var deleted bool
	db.DB.QueryRow(`SELECT deleted_at IS NOT NULL FROM tenant_default.documents WHERE id = $1`, posted).Scan(&deleted)
	if s := statusOf(posted); s != "Approved" || deleted {
		t.Errorf("the posted invoice is now %s (deleted %v)", s, deleted)
	}

	rec := doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/SalesInvoice/"+draft, token,
		map[string]interface{}{"invoice_number": draft, "customer": "T47164 Customer", "location": "HO", "total_amount": 500, "status": "Cancelled", "reason": "raised in error"})
	if rec.Code != http.StatusOK || statusOf(draft) != "Cancelled" {
		t.Errorf("cancelling a draft invoice (nothing posted): %d %s, status %s", rec.Code, rec.Body.String(), statusOf(draft))
	}

	rec = doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/POSCart/"+cart, token,
		map[string]interface{}{"cart_number": cart, "location": "HO", "payment_mode": "Cash", "amount_paid": 100, "items": []interface{}{}, "status": "Paid"})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "GLOBAL-0019") || statusOf(cart) != "Processing" {
		t.Errorf("saving a till bill as Paid: %d %s, status %s - want GLOBAL-0019", rec.Code, rec.Body.String(), statusOf(cart))
	}
	rec = doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/POSCart", token,
		map[string]interface{}{"id": faked, "cart_number": faked, "location": "HO", "payment_mode": "Cash", "amount_paid": 100, "items": []interface{}{}, "status": "Paid"})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "GLOBAL-0019") {
		t.Errorf("creating a till bill as Paid: %d %s - want GLOBAL-0019", rec.Code, rec.Body.String())
	}
	var n int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND (id = $1 OR data->>'id' = $1)`, faked).Scan(&n)
	if n != 0 {
		t.Errorf("a Paid till bill was created through the record API")
	}
}
