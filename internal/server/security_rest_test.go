package server

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 49.11.2: a request refused at the route-capability check writes its
// GLOBAL-0011 event with the client address (the trusted-proxy rule's
// answer), a device summary, the capability it lacked and the policy
// fingerprint that decided it.
func TestStage49112RefusalCarriesSourceDeviceAndPolicy(t *testing.T) {
	db.InitDB(testConnStr())
	user, cleanup := seedStage47User(t, "Employee Self-Service", "HO")
	defer cleanup()
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE user_id = $1`, user)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/security/posture", nil)
	req.RemoteAddr = "198.51.100.7:40000"
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")
	req.Header.Set("Authorization", "Bearer "+stage47Token(user, "Employee Self-Service", "HO"))
	rec := httptest.NewRecorder()
	apiMiddleware(handleSecurityPosture)(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "GLOBAL-0011") {
		t.Fatalf("want 403 GLOBAL-0011, got %d %s", rec.Code, rec.Body.String())
	}
	var ip, device, capability, policy string
	if err := db.DB.QueryRow(`SELECT COALESCE(source_ip, ''), COALESCE(device, ''), COALESCE(capability, ''), COALESCE(policy_version, '')
		FROM tenant_default.audit_logs WHERE user_id = $1 AND action = 'GLOBAL-0011' ORDER BY created_at DESC LIMIT 1`, user).
		Scan(&ip, &device, &capability, &policy); err != nil {
		t.Fatalf("no GLOBAL-0011 event: %v", err)
	}
	if want := clientIP(req); ip != want || ip == "" {
		t.Errorf("source %q, want the trusted-proxy rule's %q", ip, want)
	}
	if device != "Chrome on Windows" || capability != "admin.audit-logs" || policy != policyVersion() {
		t.Errorf("device %q, capability %q, policy %q (want %q)", device, capability, policy, policyVersion())
	}
}

// The policy fingerprint is deterministic and short.
func TestStage49112PolicyVersionIsStable(t *testing.T) {
	a, b := computePolicyVersion(), computePolicyVersion()
	if a != b || !regexp.MustCompile(`^p[0-9a-f]{12}$`).MatchString(a) {
		t.Fatalf("policy version %q / %q", a, b)
	}
}

// Stage 49.11.4: a webhook with a bad signature is refused with the same
// generic answer as before and recorded for the detection sweep.
func TestStage49114WebhookRefusalIsRecorded(t *testing.T) {
	db.InitDB(testConnStr())
	channel := "t49114" + stage47UniqueID()
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = 'WEBHOOK_INBOUND' AND details = $1`, "source=shopify:"+channel)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shopify/webhook?channel="+channel, strings.NewReader(`{"id":1}`))
	req.Header.Set("Resolved-Tenant-ID", "default")
	req.Header.Set("X-Shopify-Hmac-Sha256", "bm90LWEtc2lnbmF0dXJl")
	rec := httptest.NewRecorder()
	handleShopifyWebhook(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Invalid webhook signature") {
		t.Fatalf("want 401 Invalid webhook signature, got %d %s", rec.Code, rec.Body.String())
	}
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.audit_logs WHERE action = 'WEBHOOK_INBOUND' AND status = 'SIGNATURE_REFUSED' AND details = $1`,
		"source=shopify:"+channel).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d SIGNATURE_REFUSED rows (%v), want 1", n, err)
	}
}
