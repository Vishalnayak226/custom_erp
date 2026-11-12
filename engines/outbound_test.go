package engines

import (
	"crypto/hmac"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestStage134OutboundGuard (49.8.4): the one list of addresses a
// client-typed URL may not reach, the connect-time refusal, redirects and
// the answer-size cap.
func TestStage134OutboundGuard(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700::1111": true, "::ffff:8.8.8.8": true, "64:ff9b::808:808": true, "2002:808:808::1": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.9": false, "192.168.1.1": false, "100.64.1.1": false,
		"169.254.169.254": false, "0.0.0.0": false, "198.18.0.1": false, "240.0.0.1": false, "224.0.0.1": false,
		"::1": false, "::": false, "::ffff:127.0.0.1": false, "::ffff:169.254.169.254": false, "64:ff9b::a00:1": false,
		"2002:a00:1::1": false, "fe80::1": false, "fd00::1": false, "ff02::1": false, "2001:db8::1": false,
	} {
		if got := publicAddress(net.ParseIP(addr)); got != want {
			t.Errorf("publicAddress(%s) = %v, want %v", addr, got, want)
		}
	}

	was := outboundAllowPrivate
	outboundAllowPrivate = false
	defer func() { outboundAllowPrivate = was }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	if _, err := newOutboundClient(2e9, false).Get(srv.URL); err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("a loopback address is refused when the connection is made: %v", err)
	}
	if _, err := newOutboundClient(2e9, false).Get(strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)); err == nil {
		t.Fatal("a name that resolves to loopback is refused too")
	}

	req := func(raw string) *http.Request { u, _ := url.Parse(raw); return &http.Request{URL: u} }
	if err := outboundRedirect(req("http://example.com/"), []*http.Request{req("https://example.com/")}); err == nil {
		t.Fatal("a redirect from https down to http is refused")
	}
	if err := outboundRedirect(req("https://example.com/4"), []*http.Request{req("https://a/"), req("https://b/"), req("https://c/")}); err == nil {
		t.Fatal("a fourth redirect is refused")
	}
	if err := outboundRedirect(req("https://example.com/2"), []*http.Request{req("https://example.com/")}); err != nil {
		t.Fatalf("an https redirect is followed: %v", err)
	}
	if _, err := readOutboundBody(strings.NewReader(strings.Repeat("x", 11)), 10); err == nil {
		t.Fatal("an answer over the cap is refused, not cut")
	}
	if b, err := readOutboundBody(strings.NewReader("0123456789"), 10); err != nil || len(b) != 10 {
		t.Fatalf("an answer at the cap is read whole: %d %v", len(b), err)
	}
}

// TestStage134WebhookSigningAndSecret: a delivery carries a timestamped
// signature over "<t>.<body>" beside the body-only one, and the queued job
// names the subscription instead of carrying its secret; a subscription
// switched off before delivery sends nothing.
func TestStage134WebhookSigningAndSecret(t *testing.T) {
	var header http.Header
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header, body = r.Header.Clone(), nil
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if _, err := sendWebhookHTTP(srv.URL, "s3cr3t", "t134.event", map[string]interface{}{"n": 1}, "JOB-134"); err != nil {
		t.Fatal(err)
	}
	ts := header.Get("X-Webhook-Timestamp")
	mac := hmac.New(sha256.New, []byte("s3cr3t"))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	if want := "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil)); ts == "" || header.Get("X-Webhook-Signature-256") != want {
		t.Fatalf("timestamped signature = %q, want %q", header.Get("X-Webhook-Signature-256"), want)
	}
	if !strings.HasPrefix(header.Get("X-Webhook-Signature"), "sha256=") {
		t.Fatal("the body-only signature stays for existing receivers")
	}

	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	raw, _ := json.Marshal(map[string]interface{}{"code": "WH-134", "name": "WH-134", "url": "https://example.invalid/hook", "secret": "s3cr3t", "event_pattern": "t134.*"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('WH-134', 'WebhookSubscription', $1, 'Active', 'system')", raw); err != nil {
		t.Fatal(err)
	}
	dispatchWebhooksForEvent(schema, "EV-134", "t134.event", map[string]interface{}{"n": 1}, "")
	var payload string
	if err := db.DB.QueryRow("SELECT payload::text FROM "+schema+".async_jobs WHERE job_type = $1 ORDER BY created_at DESC LIMIT 1", webhookDeliveryJobType).Scan(&payload); err != nil {
		t.Fatalf("queued delivery: %v", err)
	}
	if strings.Contains(payload, "s3cr3t") || !strings.Contains(payload, `"subscription_id": "WH-134"`) && !strings.Contains(payload, `"subscription_id":"WH-134"`) {
		t.Fatalf("the job names the subscription and holds no secret: %s", payload)
	}
	if _, err := db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Inactive' WHERE id = 'WH-134'"); err != nil {
		t.Fatal(err)
	}
	res, err := deliverWebhook(t.Context(), schema, Job{ID: "JOB-134-OFF", Payload: map[string]interface{}{"url": "https://example.invalid/hook", "subscription_id": "WH-134", "event_name": "t134.event"}})
	if err != nil || res["skipped"] != true {
		t.Fatalf("a subscription switched off before delivery sends nothing: %v %v", res, err)
	}
}
