package engines

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"custom_erp/db"
)

// 185.3: a payment link is made for what a posted invoice owes, against a
// local fake of Razorpay's API; Razorpay's signed paid webhook books the
// receipt once into clearing account 1101, settles the invoice and makes
// what is over the customer's advance.
func TestRazorpayPaymentLinks(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	ctx := context.Background()
	insert("C185RZ", "Customer", "Active", "system", map[string]interface{}{"code": "C185RZ", "name": "Link Customer", "phone": "9000000185"})
	for _, id := range []string{"SI-RZP1", "SI-RZP2"} {
		insert(id, "SalesInvoice", "Draft", "system", map[string]interface{}{"customer": "C185RZ", "location": "S140", "total_amount": 1000.0, "invoice_number": "INV-" + id})
		if _, err := PostSalesInvoice(tenantID, id, "acct1"); err != nil {
			t.Fatalf("post %s: %v", id, err)
		}
	}
	insert("SI-RZP3", "SalesInvoice", "Draft", "system", map[string]interface{}{"customer": "C185RZ", "location": "S140", "total_amount": 500.0})
	refused := func(err error, want string) {
		t.Helper()
		v, ok := err.(*ValidationError)
		if !ok || v.Code != paymentLinkErrorCode || !strings.Contains(v.Message, want) {
			t.Fatalf("got %v; want %s %q", err, paymentLinkErrorCode, want)
		}
	}
	_, err := CreateInvoicePaymentLink(ctx, tenantID, "SI-RZP1", "acct1")
	refused(err, "not set up")

	var mu sync.Mutex
	var made []map[string]interface{}
	var cancelled []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if user, pass, ok := r.BasicAuth(); !ok || user != "rzp_test_185" || pass != "sec185" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"BAD_REQUEST_ERROR","description":"Authentication failed"}}`))
			return
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/payment_links":
			var in map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&in)
			made = append(made, in)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": fmt.Sprintf("plink_185%d", len(made)), "short_url": fmt.Sprintf("https://rzp.io/i/t%d", len(made)), "status": "created", "amount": in["amount"]})
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/cancel"):
			cancelled = append(cancelled, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/payment_links/"), "/cancel"))
			_, _ = w.Write([]byte(`{"status":"cancelled"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	oldBase, oldPrivate := razorpayAPIBase, outboundAllowPrivate
	razorpayAPIBase, outboundAllowPrivate = server.URL, true // the fake binds to loopback
	// A sandbox rehearsal before a connector (162.19) is not what this tests.
	rehearsal := GetSettingBoolForSchema(schema, SettingRequireRehearsal)
	if err := SetSetting(tenantID, SettingRequireRehearsal, "false", "system"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		razorpayAPIBase, outboundAllowPrivate = oldBase, oldPrivate
		_ = SetSetting(tenantID, SettingRequireRehearsal, fmt.Sprint(rehearsal), "system")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".channel_credentials WHERE channel_code = $1", RazorpayCredentialCode)
	})

	refused(SaveRazorpayCredential(tenantID, "key185", "sec185", "whsec185", "acct1", ""), "rzp_")
	if err := SaveRazorpayCredential(tenantID, "rzp_test_185", "sec185", "whsec185", "acct1", ""); err != nil {
		t.Fatal(err)
	}
	rows, err := GetRazorpayCredentials(tenantID)
	if err != nil || len(rows) != 1 || rows[0]["key_id"] != "rzp_test_185" || rows[0]["key_secret"] != "set" {
		t.Fatalf("listed keys = %v %v; want the key ID and only that the secret is set", rows, err)
	}

	_, err = CreateInvoicePaymentLink(ctx, tenantID, "SI-RZP3", "acct1")
	refused(err, "is Draft")
	link, err := CreateInvoicePaymentLink(ctx, tenantID, "SI-RZP1", "acct1")
	if err != nil {
		t.Fatal(err)
	}
	if link.ID != "plink_1851" || link.Amount != 1000 || len(made) != 1 || made[0]["amount"] != 100000.0 || made[0]["currency"] != "INR" || made[0]["reference_id"] != "SI-RZP1-1" {
		t.Fatalf("link = %+v, sent %v; want 1,000.00 in paise, reference SI-RZP1-1", link, made)
	}
	if again, err := CreateInvoicePaymentLink(ctx, tenantID, "SI-RZP1", "acct1"); err != nil || again.ID != link.ID || len(made) != 1 {
		t.Fatalf("asking again made another link: %+v %v", again, err)
	}
	// 400 paid in cash meanwhile: the 1,000 link is cancelled and a 600 one made.
	if _, err := RecordCustomerReceipt(tenantID, "acct1", CustomerReceiptInput{Customer: "C185RZ", Location: "S140", Mode: "Cash", Amount: 400,
		Invoices: []ReceiptLine{{Ref: "SI-RZP1", Amount: 400}}}); err != nil {
		t.Fatal(err)
	}
	link, err = CreateInvoicePaymentLink(ctx, tenantID, "SI-RZP1", "acct1")
	if err != nil || link.ID != "plink_1852" || link.Amount != 600 || len(cancelled) != 1 || cancelled[0] != "plink_1851" || made[1]["reference_id"] != "SI-RZP1-2" {
		t.Fatalf("after the part payment: %+v %v, cancelled %v; want a 600 link and the old one cancelled", link, err, cancelled)
	}

	event := func(kind, linkID, payID string, paise int64) []byte {
		b, _ := json.Marshal(map[string]interface{}{"entity": "event", "event": kind, "payload": map[string]interface{}{
			"payment_link": map[string]interface{}{"entity": map[string]interface{}{"id": linkID, "status": strings.TrimPrefix(kind, "payment_link.")}},
			"payment":      map[string]interface{}{"entity": map[string]interface{}{"id": payID, "amount": paise, "currency": "INR", "method": "upi"}},
		}})
		return b
	}
	body := event("payment_link.paid", "plink_1852", "pay_185A", 60000)
	mac := hmac.New(sha256.New, []byte("whsec185"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	secrets := GetChannelWebhookSecrets(tenantID, RazorpayCredentialCode)
	if !VerifyWebhookHMACAny(body, sig, "", "hex", secrets...) || VerifyWebhookHMACAny(append(body, ' '), sig, "", "hex", secrets...) {
		t.Fatal("the webhook signature check does not tell a signed body from an altered one")
	}
	if res, err := HandleRazorpayWebhook(tenantID, event("payment_link.paid", "plink_other", "pay_185X", 100)); err != nil || res != "ignored" {
		t.Fatalf("a link no invoice holds: %q %v; want ignored", res, err)
	}
	res, err := HandleRazorpayWebhook(tenantID, body)
	if err != nil || !strings.HasPrefix(res, "receipt ") {
		t.Fatalf("paid webhook: %q %v", res, err)
	}
	number := strings.TrimPrefix(res, "receipt ")
	if again, err := HandleRazorpayWebhook(tenantID, body); err != nil || again != res {
		t.Fatalf("the webhook again: %q %v; want the same receipt", again, err)
	}
	if d, _ := stage140GL(t, schema, "CustomerReceipt", number, "1101"); d != 60000 {
		t.Fatalf("1101 debit = %d, want 60000", d)
	}
	var status, linkStatus, linkReceipt string
	_ = db.DB.QueryRow("SELECT status, data->'payment_link'->>'status', data->'payment_link'->>'receipt' FROM "+schema+".documents WHERE id = 'SI-RZP1'").Scan(&status, &linkStatus, &linkReceipt)
	if status != "Paid" || linkStatus != "paid" || linkReceipt != number {
		t.Fatalf("SI-RZP1 is %s, link %s receipt %s; want Paid, paid, %s", status, linkStatus, linkReceipt, number)
	}
	var receipts int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'CustomerReceipt' AND data->>'idempotency_key' = 'razorpay:pay_185A'").Scan(&receipts)
	if receipts != 1 {
		t.Fatalf("%d receipts for one payment, want 1", receipts)
	}

	// SI-RZP2: its link is paid in full after 300 came in some other way, so
	// 700 settles the invoice and 300 is the customer's advance.
	link2, err := CreateInvoicePaymentLink(ctx, tenantID, "SI-RZP2", "acct1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordCustomerReceipt(tenantID, "acct1", CustomerReceiptInput{Customer: "C185RZ", Location: "S140", Mode: "Cash", Amount: 300,
		Invoices: []ReceiptLine{{Ref: "SI-RZP2", Amount: 300}}}); err != nil {
		t.Fatal(err)
	}
	res, err = HandleRazorpayWebhook(tenantID, event("payment_link.paid", link2.ID, "pay_185B", 100000))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := LoadCustomerReceipt(tenantID, strings.TrimPrefix(res, "receipt "))
	if err != nil || rec.Allocated != 700 || rec.OnAccountAmt != 300 {
		t.Fatalf("over-payment receipt = %+v %v; want 700 to the invoice, 300 advance", rec, err)
	}
	// An expiry after the payment changes nothing.
	if _, err := HandleRazorpayWebhook(tenantID, event("payment_link.expired", link2.ID, "", 0)); err != nil {
		t.Fatal(err)
	}
	_ = db.DB.QueryRow("SELECT data->'payment_link'->>'status' FROM " + schema + ".documents WHERE id = 'SI-RZP2'").Scan(&linkStatus)
	if linkStatus != "paid" {
		t.Fatalf("SI-RZP2's link is %s after a late expiry, want paid", linkStatus)
	}
}
