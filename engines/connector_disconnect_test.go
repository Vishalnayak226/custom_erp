package engines

import (
	"context"
	"custom_erp/db"
	"strings"
	"testing"
)

// 49.8.5 (rest): Unicommerce records its credential's last use like the other
// connectors, and Disconnect removes a Shopify channel's webhooks at the shop
// (a stubbed Shopify here) - a failed removal is reported, never blocking.

func credential4985bSetup(t *testing.T) string {
	t.Helper()
	schema := credential4985Setup(t)
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.channel_credentials WHERE channel_code LIKE 'T4985B-%'`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.documents WHERE doctype='Channel' AND id LIKE 'T4985B-%'`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.unicommerce_credentials WHERE store_code LIKE 'T4985B-%'`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.unicommerce_inventory_sync WHERE store_code LIKE 'T4985B-%'`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.integration_event_outbox WHERE event_name LIKE 'unicommerce.%' AND payload->>'store_code' LIKE 'T4985B-%'`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.audit_logs WHERE action='CONNECTOR_DISCONNECTED' AND details LIKE '%T4985B-%'`)
	}
	cleanup()
	t.Cleanup(cleanup)
	return schema
}

func TestStage4985bUnicommerceLastUseIsStamped(t *testing.T) {
	schema := credential4985bSetup(t)
	if err := SaveUnicommerceCredentialBy("default", "T4985B-LIVE", "key", "secret", "https://uc.example", "owner4985b", ""); err != nil {
		t.Fatal(err)
	}
	if err := SaveUnicommerceCredentialBy("default", "T4985B-OLD", "key", "secret", "https://uc.example", "owner4985b", "2020-01-31"); err != nil {
		t.Fatal(err)
	}
	if err := SyncUnicommerceInventory("default", "T4985B-SKU", "T4985B-LIVE", 3); err != nil {
		t.Fatal(err)
	}
	if err := SyncUnicommerceInventory("default", "T4985B-SKU", "T4985B-OLD", 4); err != nil {
		t.Fatal(err)
	}
	eventStatus := func(store string) string {
		var s string
		_ = db.DB.QueryRow(`SELECT status FROM `+schema+`.integration_event_outbox WHERE event_name = 'unicommerce.inventory.sync' AND payload->>'store_code' = $1 ORDER BY created_at DESC LIMIT 1`, store).Scan(&s)
		return s
	}
	// The worker takes ten events a pass, oldest first; pass until both are taken.
	for i := 0; i < 20 && (eventStatus("T4985B-LIVE") == "Pending" || eventStatus("T4985B-OLD") == "Pending"); i++ {
		processUnicommerceOutbox(schema)
	}
	if got := eventStatus("T4985B-LIVE"); got != "Dispatched" {
		t.Fatalf("a live credential's event is %q, want Dispatched", got)
	}
	if got := eventStatus("T4985B-OLD"); got != "Failed" {
		t.Fatalf("an expired credential's event is %q, want Failed - it must not be sent", got)
	}
	rows, err := GetUnicommerceCredentials("default")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]map[string]interface{}{}
	for _, r := range rows {
		seen[r["store_code"].(string)] = r
	}
	if live := seen["T4985B-LIVE"]; live == nil || live["last_used_at"] == "" || live["saved_by"] != "owner4985b" {
		t.Fatalf("the live store's last use is not shown: %v", live)
	}
	if old := seen["T4985B-OLD"]; old == nil || old["last_used_at"] != "" || old["expired"] != true {
		t.Fatalf("an expired credential is not used, so no last use: %v", old)
	}
	var logged string
	_ = db.DB.QueryRow(`SELECT COALESCE(l.error_message, '') FROM ` + schema + `.integration_event_log l JOIN ` + schema + `.integration_event_outbox o ON o.id = l.event_id
		WHERE o.payload->>'store_code' = 'T4985B-OLD' ORDER BY l.created_at DESC LIMIT 1`).Scan(&logged)
	if !strings.Contains(logged, "expired") {
		t.Fatalf("the refusal is not in the event log: %q", logged)
	}
}

func shopifyChannel4985b(t *testing.T, schema, code, hookURL string) {
	t.Helper()
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1::text, 'Channel', jsonb_build_object('code', $1::text, 'name', 'T4985B shop', 'platform', 'Shopify', 'shopify', jsonb_build_object('sync_orders', true, 'webhook_url', $2::text, 'webhooks_registered_at', '2026-10-01T00:00:00Z')), 'Active', 'system')`, code, hookURL); err != nil {
		t.Fatal(err)
	}
	if err := SaveChannelCredentialBy("default", code, map[string]string{"access_token": "tok", "shop_domain": "demo.myshopify.com", "webhook_secret": "whsec"}, "owner4985b", nil); err != nil {
		t.Fatal(err)
	}
}

func TestStage4985bDisconnectDeregistersShopifyWebhooks(t *testing.T) {
	schema := credential4985bSetup(t)
	fake, _ := withFakeShopify(t)
	const code = "T4985B-SHOP"
	// The fake lists one subscription at this address and one of another app.
	shopifyChannel4985b(t, schema, code, "https://erp.example/api/v1/integration/shopify/webhook?t=new")
	res, err := DisconnectConnectorCredentialContext(context.Background(), "default", "channel", code, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if res.Warning != "" {
		t.Fatalf("a removal that worked must not warn: %q", res.Warning)
	}
	joined := strings.Join(res.Cleaned, ",")
	if !strings.Contains(joined, "1 Shopify webhook(s) removed") || !strings.Contains(joined, "credential") || !strings.Contains(joined, "webhook registration") {
		t.Fatalf("cleaned = %v", res.Cleaned)
	}
	fake.mu.Lock()
	deleted := fake.seen["whdel"]
	fake.mu.Unlock()
	if len(deleted) != 1 || deleted[0]["id"] != "gid://shopify/WebhookSubscription/1" {
		t.Fatalf("only this ERP's subscription may be removed, got %v", deleted)
	}
	if ok, _ := HasChannelCredential("default", code); ok {
		t.Fatal("the credential is still there")
	}
}

func TestStage4985bFailedDeregistrationDoesNotBlockDisconnect(t *testing.T) {
	schema := credential4985bSetup(t)
	withFakeShopify(t)
	// A shop that does not answer: nothing listens on port 1.
	shopifyGraphQLURL = func(string) string { return "http://127.0.0.1:1/graphql" }
	const code = "T4985B-DOWN"
	shopifyChannel4985b(t, schema, code, "https://erp.example/api/v1/integration/shopify/webhook?t=x")
	res, err := DisconnectConnectorCredentialContext(context.Background(), "default", "channel", code, "admin")
	if err != nil {
		t.Fatalf("a failed removal must not block the disconnect: %v", err)
	}
	if !strings.Contains(res.Warning, "Shopify's webhooks were not removed") || !strings.Contains(res.Warning, "Shopify admin") {
		t.Fatalf("the result must say the webhooks are still registered: %q", res.Warning)
	}
	if ok, _ := HasChannelCredential("default", code); ok {
		t.Fatal("the credential must go even when the shop does not answer")
	}
	var audited int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM `+schema+`.audit_logs WHERE action='CONNECTOR_DISCONNECTED' AND details LIKE $1`, "%code="+code+"%warning=%").Scan(&audited)
	if audited != 1 {
		t.Fatalf("the warning is not on the audit row (%d)", audited)
	}
	// A channel that never registered webhooks makes no call at all.
	const quiet = "T4985B-QUIET"
	shopifyChannel4985b(t, schema, quiet, "")
	if res, err := DisconnectConnectorCredentialContext(context.Background(), "default", "channel", quiet, "admin"); err != nil || res.Warning != "" {
		t.Fatalf("no webhooks registered, nothing to remove: %v %q", err, res.Warning)
	}
}

// The removal goes through the outside-calls gate: with every outside call
// stopped it is refused before it leaves, and the disconnect still completes.
func TestStage4985bDeregistrationRespectsTheOutsideCallsGate(t *testing.T) {
	schema := credential4985bSetup(t)
	fake, _ := withFakeShopify(t)
	shopifyGraphQLURL = func(string) string { return "https://demo.myshopify.com/admin/api/graphql.json" } // not loopback
	const code = "T4985B-GATED"
	shopifyChannel4985b(t, schema, code, "https://erp.example/api/v1/integration/shopify/webhook?t=g")
	if ok, _ := OutsideCallsAllowed(schema); ok {
		t.Skip("outside calls are allowed in this environment; the gate refusal cannot be observed without a real call")
	}
	res, err := DisconnectConnectorCredentialContext(context.Background(), "default", "channel", code, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Warning, "not removed") {
		t.Fatalf("a gated removal must be reported: %q", res.Warning)
	}
	fake.mu.Lock()
	n := len(fake.seen["whdel"])
	fake.mu.Unlock()
	if n != 0 {
		t.Fatal("nothing may reach the shop through a closed gate")
	}
}
