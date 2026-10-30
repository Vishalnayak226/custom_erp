package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
	"time"
)

// 49.8.5: a credential's owner and last use are stamped, an expired one is
// refused, and Disconnect leaves no credential readable and no queued job.

func credential4985Setup(t *testing.T) string {
	t.Helper()
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var ready bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=$1 AND table_name='channel_credentials' AND column_name='last_used_at')`, schema).Scan(&ready); err != nil || !ready {
		t.Skip("db/migrations_stage49_8_5_credential_metadata.sql has not been applied")
	}
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.channel_credentials WHERE channel_code LIKE 'T4985-%'`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.pinelabs_credentials WHERE terminal_id LIKE 'T4985-%'`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.async_jobs WHERE payload->>'channel' LIKE 'T4985-%'`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.documents WHERE doctype='Channel' AND id LIKE 'T4985-%'`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.audit_logs WHERE action='CONNECTOR_DISCONNECTED' AND details LIKE '%T4985-%'`)
	}
	cleanup()
	t.Cleanup(cleanup)
	return schema
}

func TestStage4985OwnerAndLastUseStamped(t *testing.T) {
	schema := credential4985Setup(t)
	const code = "T4985-CHANNEL"
	if err := SaveChannelCredentialBy("default", code, map[string]string{"access_token": "t4985-secret"}, "owner4985", nil); err != nil {
		t.Fatal(err)
	}
	info, err := GetChannelCredentialInfo("default", code)
	if err != nil || !info.Configured || info.Owner != "owner4985" || info.LastUsedAt != "" || info.ExpiresAt != "" {
		t.Fatalf("after save: %+v %v", info, err)
	}
	// A connector read stamps the use; a second read within the hour keeps
	// the first stamp.
	if _, err := getChannelCredential("default", code); err != nil {
		t.Fatal(err)
	}
	first, _ := GetChannelCredentialInfo("default", code)
	if first.LastUsedAt == "" {
		t.Fatal("a read did not stamp last_used_at")
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := getChannelCredential("default", code); err != nil {
		t.Fatal(err)
	}
	if again, _ := GetChannelCredentialInfo("default", code); again.LastUsedAt != first.LastUsedAt {
		t.Fatalf("a second read within the hour re-stamped: %s then %s", first.LastUsedAt, again.LastUsedAt)
	}
	// A save by a job keeps the owner; a save by another user takes it over.
	if err := SaveChannelCredential("default", code, map[string]string{"access_token": "t4985-secret-2"}); err != nil {
		t.Fatal(err)
	}
	if kept, _ := GetChannelCredentialInfo("default", code); kept.Owner != "owner4985" {
		t.Fatalf("a save with no actor must keep the owner, got %q", kept.Owner)
	}
	if err := SaveChannelCredentialBy("default", code, map[string]string{"access_token": "t4985-secret-3"}, "other4985", nil); err != nil {
		t.Fatal(err)
	}
	if taken, _ := GetChannelCredentialInfo("default", code); taken.Owner != "other4985" {
		t.Fatalf("the latest saver owns it, got %q", taken.Owner)
	}
	// Expiry: a past date refuses the read; clearing it restores it.
	past := "2020-01-01"
	if err := SaveChannelCredentialBy("default", code, map[string]string{"access_token": "t4985-secret-3"}, "", &past); err != nil {
		t.Fatal(err)
	}
	if _, err := getChannelCredential("default", code); err == nil || !strings.Contains(err.Error(), "expired on 2020-01-01") {
		t.Fatalf("an expired credential must be refused, got %v", err)
	}
	if info, _ := GetChannelCredentialInfo("default", code); !info.Expired || info.ExpiresAt == "" {
		t.Fatalf("expired is shown: %+v", info)
	}
	none := ""
	if err := SaveChannelCredentialBy("default", code, map[string]string{"access_token": "t4985-secret-3"}, "", &none); err != nil {
		t.Fatal(err)
	}
	if _, err := getChannelCredential("default", code); err != nil {
		t.Fatalf("expiry cleared: %v", err)
	}
	if err := SaveChannelCredentialBy("default", code, nil, "", strPtr4985("next week")); err == nil {
		t.Fatal("a bad expiry must be refused")
	}
	_ = schema

	// Pine Labs: the same three on its own table, stamped by the terminal check.
	if err := SavePineLabsCredentialBy("default", "T4985-TERM", "key", "merchant", "https://plutus.example", "owner4985", "2099-12-31"); err != nil {
		t.Fatal(err)
	}
	rows, err := GetPineLabsCredentials("default")
	if err != nil {
		t.Fatal(err)
	}
	var found map[string]interface{}
	for _, r := range rows {
		if r["terminal_id"] == "T4985-TERM" {
			found = r
		}
	}
	if found == nil || found["saved_by"] != "owner4985" || found["expired"] != false || !strings.HasPrefix(found["expires_at"].(string), "2099-12-31") || found["last_used_at"] != "" {
		t.Fatalf("pine labs row: %v", found)
	}
}

func strPtr4985(s string) *string { return &s }

func TestStage4985DisconnectLeavesNoCredentialAndNoJob(t *testing.T) {
	schema := credential4985Setup(t)
	const code = "T4985-SHOP"
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, 'Channel', '{"code":"T4985-SHOP","name":"T4985 shop","platform":"Shopify","shopify":{"sync_orders":true,"webhook_url":"https://erp.example/hook","webhooks_registered_at":"2026-10-01T00:00:00Z","locations":{"1":"HO"}}}', 'Active', 'system')`, code); err != nil {
		t.Fatal(err)
	}
	if err := SaveChannelCredentialBy("default", code, map[string]string{"access_token": "t4985", "shop_domain": "t4985.myshopify.com"}, "owner4985", nil); err != nil {
		t.Fatal(err)
	}
	// 49.8.6: enqueue refuses a type with no handler; the server registers
	// shopify_status_push at boot, which this package's tests do not run.
	ensureTestJobHandler("shopify_status_push")
	jobID, err := EnqueueJob("default", "shopify_status_push", map[string]interface{}{"tenant_id": "default", "channel": code, "order_id": "T4985-ORD"}, "T4985-job")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DisconnectConnectorCredential("default", "channel", "T4985-NOT-THERE", "admin"); err == nil {
		t.Fatal("disconnecting nothing must say so")
	}
	cleaned, err := DisconnectConnectorCredential("default", "channel", code, "admin")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cleaned, ",")
	if !strings.Contains(joined, "credential") || !strings.Contains(joined, "queued job") || !strings.Contains(joined, "webhook registration") {
		t.Fatalf("cleaned = %v", cleaned)
	}
	if ok, _ := HasChannelCredential("default", code); ok {
		t.Fatal("the credential is still there")
	}
	if _, err := getChannelCredential("default", code); err == nil {
		t.Fatal("the credential is still readable")
	}
	var status string
	_ = db.DB.QueryRow(`SELECT status FROM `+schema+`.async_jobs WHERE id=$1`, jobID).Scan(&status)
	if status != "Cancelled" {
		t.Fatalf("the queued sync job is %q, want Cancelled", status)
	}
	var syncOrders, hasHook, hasLoc bool
	_ = db.DB.QueryRow(`SELECT (data->'shopify'->>'sync_orders')::bool, data->'shopify' ? 'webhooks_registered_at', data->'shopify'->'locations' ? '1' FROM `+schema+`.documents WHERE id=$1`, code).Scan(&syncOrders, &hasHook, &hasLoc)
	if hasHook || !syncOrders || !hasLoc {
		t.Fatalf("webhook stamp cleared, settings and mappings kept: hook=%v sync=%v loc=%v", hasHook, syncOrders, hasLoc)
	}
	var audited int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.audit_logs WHERE action='CONNECTOR_DISCONNECTED' AND details LIKE '%code=` + code + `%'`).Scan(&audited)
	if audited != 1 {
		t.Fatalf("audited %d times", audited)
	}
	// Pine Labs by terminal.
	if err := SavePineLabsCredentialBy("default", "T4985-TERM2", "key", "merchant", "https://plutus.example", "owner4985", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := DisconnectConnectorCredential("default", "pinelabs", "T4985-TERM2", "admin"); err != nil {
		t.Fatal(err)
	}
	var left int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.pinelabs_credentials WHERE terminal_id='T4985-TERM2'`).Scan(&left)
	if left != 0 {
		t.Fatal("the terminal credential is still there")
	}
	if _, err := DisconnectConnectorCredential("default", "nothing", code, "admin"); err == nil {
		t.Fatal("an unknown kind must be refused")
	}
}
