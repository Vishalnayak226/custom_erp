package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Stage 148.1: deleting a vendor bill is how a bill is cancelled today. It takes
// back the cost difference the bill booked, on the bill's next key, and a bill
// with money or a decision on it stays.
func TestStage1481DeletingABillReversesItsDifference(t *testing.T) {
	db.InitDB(testConnStr())
	run := fmt.Sprintf("%x", time.Now().UnixNano())
	sku, grn, bill, paid, pending := "T1481-I-"+run, "T1481-GRN-"+run, "T1481-VI-"+run, "T1481-VIP-"+run, "T1481-VIA-"+run
	ids := []string{sku, grn, bill, paid, pending}
	t.Cleanup(func() {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM tenant_default.gl_postings WHERE document_id = $1", id)
			db.DB.Exec("DELETE FROM tenant_default.documents WHERE id = $1", id)
		}
		db.DB.Exec("DELETE FROM tenant_default.item_cost WHERE item_code = $1", sku)
	})
	seed := func(id, doctype, status, data string) {
		t.Helper()
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')`, id, doctype, data, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed(sku, "Item", "Active", fmt.Sprintf(`{"code":%q,"name":%q}`, sku, sku))
	seed(grn, "GRN", "Approved", `{"received_items":"[]"}`)
	booked := fmt.Sprintf(`"grn_id":%q,"invoice_amount":1100,"taxable_amount":1100,"gst_amount":0,"bill_cost_adjust":{%q:10000},"bill_cost_posts":1,"bill_cost_on_hand":0,"bill_cost_sold":10000`, grn, sku)
	seed(bill, "VendorInvoice", "Matched", "{"+booked+"}")
	seed(paid, "VendorInvoice", "Matched", "{"+booked+`,"payments":[{"amount":100,"reference":"x"}]}`)
	seed(pending, "VendorInvoice", "Pending Approval", "{"+booked+"}")

	// The shipped `system` account is Inactive (SB-021), so act as a seeded admin.
	actor, dropActor := seedContractUser(t, "HR/Admin", "HO")
	defer dropActor()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	del := func(id string) *httptest.ResponseRecorder {
		token := engines.SignToken(actor, actor, "HR/Admin", "default", "HO", currentCredentialVersion(actor))
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/doc/VendorInvoice/"+id, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	gone := func(id string) bool {
		var deleted bool
		if err := db.DB.QueryRow(`SELECT deleted_at IS NOT NULL FROM tenant_default.documents WHERE id = $1`, id).Scan(&deleted); err != nil {
			t.Fatal(err)
		}
		return deleted
	}
	postings := func(id, key string) int {
		var n int
		if err := db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.gl_postings WHERE document_id = $1 AND idempotency_key = $2`, id, key).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if rec := del(bill); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if !gone(bill) {
		t.Error("the bill is still there")
	}
	if n := postings(bill, "VendorInvoice:"+bill+":BILL_DIFF:2"); n == 0 {
		t.Error("no reversal on the bill's next key")
	}
	var variance, suspense int64
	if err := db.DB.QueryRow(`SELECT
		COALESCE(SUM(debit - credit) FILTER (WHERE account_code = '5195'), 0), COALESCE(SUM(debit - credit) FILTER (WHERE account_code = '2100'), 0)
		FROM tenant_default.gl_postings WHERE document_id = $1`, bill).Scan(&variance, &suspense); err != nil {
		t.Fatal(err)
	}
	if variance != -10000 || suspense != 10000 {
		t.Errorf("reversal: 5195 %d, 2100 %d, want -10000 and 10000", variance, suspense)
	}
	for _, id := range []string{paid, pending} {
		rec := del(id)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "FIN-0910") {
			t.Errorf("%s: %d %s, want 422 FIN-0910", id, rec.Code, rec.Body.String())
		}
		if gone(id) {
			t.Errorf("%s went", id)
		}
		if n := postings(id, "VendorInvoice:"+id+":BILL_DIFF:2"); n != 0 {
			t.Errorf("%s reversed", id)
		}
	}
}
